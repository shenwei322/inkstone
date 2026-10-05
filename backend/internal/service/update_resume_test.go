package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shenwei/inkstone/backend/pkg/config"
)

// newResumeTestClient 造一个放行内网目标的下载客户端。
//
// 测试服务器跑在 127.0.0.1，而生产默认拒绝非公网目标（防借更新通道
// 打内网），所以这里必须显式放行，否则连不上 httptest。
func newResumeTestClient() *httpClient {
	return newHTTPClientWithGuard(5*time.Second, "", true)
}

// newMirrorTestService 造一个只关心镜像前缀配置的 UpdateService。
func newMirrorTestService(mirror string) *UpdateService {
	return &UpdateService{cfg: &config.Config{UpdateImageMirror: mirror}}
}

// =====================================================================
// 断点续传与镜像加速
//
// 背景：镜像包 265 MB，国内直连 GitHub 会在中途被掐断。改前失败即报错
// 并删掉半截文件，每次从 0 开始，实测 58.5 MB 就 context deadline
// exceeded，反复重来永远到不了终点。这里锁的是「断了能接着下」。
// =====================================================================

// flakyServer 是一个「下到第 N 字节就掐断」的服务器，用于模拟断流。
//
// 它按 Range 头提供续传能力（返回 206 + Content-Range），并记录收到的
// Range 请求，让测试能断言「确实发生了续传」而不是从头重下。
//
// 掐断只发生一次（firstFailAt）：真实网络的断流是偶发的。若每次请求都断，
// 就永远下不完，反而测不出「续传之后能成功」这个关键行为。
type flakyServer struct {
	content     []byte
	etag        string
	mu          sync.Mutex
	ranges      []string // 收到的 Range 头（按顺序）
	firstFailAt int64    // 首次请求发到该字节数后掐断；<=0 表示不掐断
	failed      bool     // 是否已经掐过一次
}

func (f *flakyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	rng := r.Header.Get("Range")
	if rng != "" {
		f.ranges = append(f.ranges, rng)
	}
	shouldFailThisTime := !f.failed && f.firstFailAt > 0
	if shouldFailThisTime {
		f.failed = true
	}
	f.mu.Unlock()

	total := int64(len(f.content))
	var start int64
	if rng != "" {
		if _, err := fmt.Sscanf(rng, "bytes=%d-", &start); err != nil || start < 0 || start >= total {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
	}

	// 条件请求：If-Range 与 ETag 不符时，按协议必须回整个文件（200）
	if rng != "" && f.etag != "" {
		if ir := r.Header.Get("If-Range"); ir != "" && ir != f.etag {
			start = 0
			rng = ""
		}
	}

	w.Header().Set("Accept-Ranges", "bytes")
	if f.etag != "" {
		w.Header().Set("ETag", f.etag)
	}
	if rng == "" {
		w.Header().Set("Content-Length", fmt.Sprint(total))
	} else {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, total-1, total))
		w.Header().Set("Content-Length", fmt.Sprint(total-start))
		w.WriteHeader(http.StatusPartialContent)
	}

	// 按块写，便于在指定位置掐断
	const chunk = 8 << 10
	sent := int64(0)
	for pos := start; pos < total; pos += chunk {
		end := pos + chunk
		if end > total {
			end = total
		}
		if _, err := w.Write(f.content[pos:end]); err != nil {
			return
		}
		sent += end - pos

		if shouldFailThisTime && sent >= f.firstFailAt {
			// 模拟连接被掐断：直接断开，不发完整响应
			if f2, ok := w.(http.Flusher); ok {
				f2.Flush()
			}
			if hj, ok := w.(http.Hijacker); ok {
				if conn, _, err := hj.Hijack(); err == nil {
					conn.Close()
				}
			}
			return
		}
		if f2, ok := w.(http.Flusher); ok {
			f2.Flush()
		}
	}
}

// TestDownloadResumesAfterInterruption 锁定「断流后从断点继续」。
//
// 这是本次修复的核心：不能让 265 MB 的包每断一次就从 0 重来。
func TestDownloadResumesAfterInterruption(t *testing.T) {
	content := []byte(strings.Repeat("INKSTONE-RESUME-TEST-", 20000)) // ~400 KB
	wantSum := sha256.Sum256(content)

	srv := httptest.NewServer(&flakyServer{
		content:     content,
		etag:        `"v1"`,
		firstFailAt: 100 << 10, // 约 100 KB 处掐断一次
	})
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "pkg.tar")
	client := newResumeTestClient()

	var lastDone int64
	res, err := client.download(context.Background(), srv.URL+"/pkg.tar", dest, func(done, total int64) {
		lastDone = done
	})
	if err != nil {
		t.Fatalf("下载应最终成功：%v", err)
	}
	if res.SHA256 != hex.EncodeToString(wantSum[:]) {
		t.Fatalf("SHA-256 不匹配：拼接后的内容被破坏")
	}
	if res.Bytes != int64(len(content)) {
		t.Fatalf("字节数 = %d，期望 %d", res.Bytes, len(content))
	}
	// 内容逐字节比对，确认没有错位
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("读回文件失败：%v", err)
	}
	if string(got) != string(content) {
		t.Fatal("落盘内容与源不一致（续传拼接错位）")
	}
	// 进度回调最后一次必须是「完整大小」，不能停在断点
	if lastDone != int64(len(content)) {
		t.Fatalf("最后一次进度 = %d，期望 %d", lastDone, len(content))
	}
}

// TestDownloadActuallyUsesRange 证明续传真的走了 Range，而不是悄悄重下。
//
// 只断言「最终成功」是不够的：把重试写成从头开始也能过。
func TestDownloadActuallyUsesRange(t *testing.T) {
	content := []byte(strings.Repeat("ABCDEFGH", 40000)) // 320 KB
	srv := httptest.NewServer(&flakyServer{
		content:     content,
		etag:        `"v2"`,
		firstFailAt: 120 << 10,
	})
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "pkg.tar")
	if _, err := newResumeTestClient().download(context.Background(), srv.URL+"/x", dest, nil); err != nil {
		t.Fatalf("下载失败：%v", err)
	}
	// flakyServer 会把收到的 Range 记下来；续传必然产生至少一个 Range 请求
	f, ok := srv.Config.Handler.(*flakyServer)
	if !ok {
		t.Fatal("handler 类型异常")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.ranges) == 0 {
		t.Fatal("没有收到任何 Range 请求——说明是整包重下，断点续传没生效")
	}
	t.Logf("续传 Range 请求：%v", f.ranges)
}

// TestDownloadDiscardsPartWhenNoValidator 是安全侧的关键断言：
// 服务器不给 ETag/Last-Modified 时**不允许**续传。
//
// 没有比对依据就无法确认远端资源没变。若强行续传，一旦这期间 Release
// 资产被换成另一个版本，就会拼出「前半段旧 + 后半段新」的包——长度正确、
// 能过大小的检查，直到 docker load 才炸，甚至悄悄装上错镜像。
func TestDownloadDiscardsPartWhenNoValidator(t *testing.T) {
	content := []byte(strings.Repeat("NOETAG", 30000))
	srv := httptest.NewServer(&flakyServer{
		content:     content,
		etag:        "", // 不给任何校验值
		firstFailAt: 50 << 10,
	})
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "pkg.tar")
	// 先造一个假的半截文件 + 无元数据
	if err := os.WriteFile(dest, content[:20000], 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := newResumeTestClient().download(context.Background(), srv.URL+"/x", dest, nil)
	if err != nil {
		t.Fatalf("下载失败：%v", err)
	}
	// 无论中间怎么处理，最终内容必须是完整正确的
	got, _ := os.ReadFile(dest)
	if string(got) != string(content) {
		t.Fatal("最终内容不完整")
	}
	if res.Bytes != int64(len(content)) {
		t.Fatalf("字节数 = %d，期望 %d", res.Bytes, len(content))
	}
}

// TestDownloadNoRetryOnClientError 锁定 4xx 不重试：
// 重试只会浪费时间并刷日志，403/404 不会因为多试几次就变成 200。
func TestDownloadNoRetryOnClientError(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	_, err := newResumeTestClient().download(context.Background(), srv.URL+"/404", filepath.Join(dir, "x"), nil)
	if err == nil {
		t.Fatal("404 应当失败")
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("404 请求了 %d 次，应当只请求 1 次（不该重试）", hits)
	}
}

// TestDownloadImageAssetCleansUpWhenAllURLsFail 锁死「所有地址都失败不留垃圾」。
//
// 注意清理职责的划分：download() 在失败时**刻意保留**半截文件，因为
// downloadImageAsset 可能换一个等价地址接着续传。真正的收尾（删文件 +
// 删续传元数据）在最外层——所有地址都试完还失败时。
func TestDownloadImageAssetCleansUpWhenAllURLsFail(t *testing.T) {
	// 两个地址都恒 502
	bad := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
	}
	s1, s2 := bad(), bad()
	defer s1.Close()
	defer s2.Close()

	svc := newTestUpdateService(t, t.TempDir())
	svc.imageURLOverride = []string{s1.URL + "/pkg.tar", s2.URL + "/pkg.tar"}

	asset := ReleaseAssetBrief{
		Name: "inkstone-images-Beta1.28.tar",
		Size: 1024,
		URL:  "https://github.com/o/r/releases/download/Beta1.28/inkstone-images-Beta1.28.tar",
	}
	_, _, err := svc.downloadImageAsset(context.Background(), "Beta1.28", ReleaseBrief{Tag: "Beta1.28"}, asset, func(int, string) {})
	if err == nil {
		t.Fatal("所有地址都 502 时应当失败")
	}
	if !strings.Contains(err.Error(), "2 个地址") {
		t.Fatalf("错误信息应说明尝试了几个地址，实际：%v", err)
	}
	dest := filepath.Join(svc.imageDir(), "Beta1.28-inkstone-images-Beta1.28.tar")
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("彻底失败后半截文件应被删除")
	}
	if _, statErr := os.Stat(dest + ".resume-meta"); !os.IsNotExist(statErr) {
		t.Fatal("续传元数据应一并清理")
	}
}

// TestDownloadServerErrorIsNotRetriedInPlace 锁定 5xx 不在原地退避重试。
//
// 这是实测教训：镜像加速站返回 502 时，原地重试 6 次要耗掉一分钟
// （2+4+8+16+30+30），而这期间本该立刻去试下一个地址。「服务端挂了」
// 不是瞬时抖动，重试同一地址没有意义。
func TestDownloadServerErrorIsNotRetriedInPlace(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	dir := t.TempDir()
	sw := time.Now()
	_, err := newResumeTestClient().download(context.Background(), srv.URL+"/502", filepath.Join(dir, "x"), nil)
	elapsed := time.Since(sw)
	if err == nil {
		t.Fatal("502 应当失败")
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("502 请求了 %d 次，应当只请求 1 次（立刻交给上层换地址）", hits)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("502 应在 3 秒内返回，实际耗时 %v（说明还在原地退避重试）", elapsed)
	}
}

// TestIsFakeIPRange 锁定 fake-ip 网段的识别范围。
//
// 这个判定只影响错误信息（告诉用户去改代理规则），不影响是否放行，
// 但范围必须准：判宽了会把真实内网目标说成"代理问题"，误导排查方向。
func TestIsFakeIPRange(t *testing.T) {
	cases := map[string]bool{
		"198.18.0.51":     true, // Clash 默认 fake-ip 起点
		"198.18.0.203":    true,
		"198.19.255.1":    true, // /15 的上半段
		"198.20.0.1":      false,
		"198.17.0.1":      false,
		"10.0.0.1":        false, // 真实内网，不该被说成代理问题
		"192.168.1.1":     false,
		"169.254.169.254": false, // 云元数据，必须保持"非公网"的原始措辞
		"8.8.8.8":         false,
	}
	for ipStr, want := range cases {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			t.Fatalf("测试数据有误：%s", ipStr)
		}
		if got := isFakeIPRange(ip); got != want {
			t.Errorf("isFakeIPRange(%s) = %v，期望 %v", ipStr, got, want)
		}
	}
	// IPv6 不该误判
	if isFakeIPRange(net.ParseIP("2001:db8::1")) {
		t.Error("IPv6 地址不该被当作 fake-ip")
	}
	if isFakeIPRange(nil) {
		t.Error("nil 应返回 false")
	}
}

// TestDownloadKeepsPartialOnFailure 锁定「失败时保留半截文件」这个契约。
//
// downloadImageAsset 的换地址逻辑依赖它：加速地址下到一半挂了、换成直连
// 地址时，已下载的字节应该还能用（同源时靠 ETag 判定继续，异源时
// downloadOnce 会自行放弃续传重下）。
//
// 关键前提：半截文件必须**带着续传元数据**（上一轮响应里的 ETag/
// Last-Modified）。没有元数据时 downloadOnce 会主动删掉它——那是另一条
// 安全规则（无依据不续传，防止拼出「前旧后新」的坏包），见
// TestDownloadDiscardsPartWhenNoValidator。
func TestDownloadKeepsPartialOnFailure(t *testing.T) {
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "pkg.tar")
	// 预置半截文件**并配上续传元数据**，模拟「上一轮断流后留下的现场」
	if err := os.WriteFile(dest, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := http.Header{}
	h.Set("ETag", `"v1"`)
	writeResumeMeta(dest, h)

	_, err := newResumeTestClient().download(context.Background(), srv.URL+"/502", dest, nil)
	if err == nil {
		t.Fatal("应当失败")
	}
	if _, statErr := os.Stat(dest); statErr != nil {
		t.Fatal("失败时半截文件应被保留，供上层换地址后续传")
	}
	// 元数据也要留着，否则换地址后失去 If-Range 依据、无法续传
	if e, _ := readResumeMeta(dest); e == "" {
		t.Fatal("续传元数据应一并保留")
	}
}

// TestImageAssetURLsWithPrefixMirrorOnly 锁定「加速只对 GitHub 域生效」。
//
// httptest 服务器是 127.0.0.1，用真实配置永远进不了加速分支，
// 所以这个函数把 prefix 显式传入，专门覆盖那条路径。
func TestImageAssetURLsWithPrefixMirrorOnly(t *testing.T) {
	svc := newTestUpdateService(t, t.TempDir())
	prefix := "https://gh-proxy.com"

	gh := "https://github.com/o/r/releases/download/Beta1.28/inkstone-images-Beta1.28.tar"
	got := svc.imageAssetURLsWithPrefix(gh, prefix)
	if len(got) != 2 || got[0] != prefix+"/"+gh || got[1] != gh {
		t.Fatalf("GitHub 地址应生成 [加速, 直连]，实际 %v", got)
	}

	// 内网地址不加速：套上去只会 404，反而弄坏唯一可用的地址
	internal := "http://10.0.0.5:8080/pkg.tar"
	got = svc.imageAssetURLsWithPrefix(internal, prefix)
	if len(got) != 1 || got[0] != internal {
		t.Fatalf("内网地址应只留直连，实际 %v", got)
	}

	// 空前缀 = 只直连
	if got = svc.imageAssetURLsWithPrefix(gh, ""); len(got) != 1 || got[0] != gh {
		t.Fatalf("关闭加速时应只有直连，实际 %v", got)
	}
	// 空前缀不应生成重复项
	if got = svc.imageAssetURLsWithPrefix("", prefix); len(got) != 0 {
		t.Fatalf("空地址应返回空，实际 %v", got)
	}
}

// TestDownloadImageAssetFallsBackToDirect 是本文件最重要的集成测试：
// 首选地址彻底不可用时，downloadImageAsset 必须换下一个地址并成功。
//
// 它直接驱动生产函数（而不是手写一遍循环），因此真正锁住了
// 「换地址重试」这条路径。
func TestDownloadImageAssetFallsBackToDirect(t *testing.T) {
	payload := []byte(strings.Repeat("IMAGE-TAR-CONTENT", 5000))
	sum := sha256.Sum256(payload)

	// 直连可用
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("ETag", `"direct"`)
		_, _ = w.Write(payload)
	}))
	defer direct.Close()

	// 加速地址恒 502
	var mirrorHits int32
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&mirrorHits, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer mirror.Close()

	svc := newTestUpdateService(t, t.TempDir())
	// 用本地地址冒充 GitHub 资产地址，让加速分支有机会被走到：
	// 直接把 imageAssetURLs 的判定绕过——测试的是「换地址」而非「该不该加速」。
	svc.imageURLOverride = []string{mirror.URL + "/pkg.tar", direct.URL + "/pkg.tar"}

	asset := ReleaseAssetBrief{
		Name: "inkstone-images-Beta1.28.tar",
		Size: int64(len(payload)),
		URL:  "https://github.com/o/r/releases/download/Beta1.28/inkstone-images-Beta1.28.tar",
	}
	res, _, err := svc.downloadImageAsset(context.Background(), "Beta1.28", ReleaseBrief{Tag: "Beta1.28"}, asset, func(int, string) {})
	if err != nil {
		t.Fatalf("应回退到直连并成功：%v", err)
	}
	if res.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("兜底下载的内容 SHA-256 不匹配")
	}
	if atomic.LoadInt32(&mirrorHits) == 0 {
		t.Fatal("应当先尝试过加速地址")
	}
}

// TestImageAssetURLsMirrorPrefix 锁定镜像加速地址的生成顺序与去重。
func TestImageAssetURLsMirrorPrefix(t *testing.T) {
	svc := newMirrorTestService("https://gh-proxy.com")
	raw := "https://github.com/o/r/releases/download/Beta1.28/inkstone-images-Beta1.28.tar"
	urls := svc.imageAssetURLs(raw)
	if len(urls) != 2 {
		t.Fatalf("应生成 2 个候选地址，实际 %d：%v", len(urls), urls)
	}
	if urls[0] != "https://gh-proxy.com/"+raw {
		t.Fatalf("首选应为加速地址，实际 %s", urls[0])
	}
	if urls[1] != raw {
		t.Fatalf("兜底应为直连地址，实际 %s", urls[1])
	}

	// 空前缀 = 只直连
	svc.cfg.UpdateImageMirror = ""
	if got := svc.imageAssetURLs(raw); len(got) != 1 || got[0] != raw {
		t.Fatalf("关闭加速时应只有直连地址，实际 %v", got)
	}

	// 非 GitHub 地址不套公网加速前缀（自建更新源套上去只会 404）
	if got := newMirrorTestService("https://gh-proxy.com").
		imageAssetURLs("http://192.168.1.9:8080/pkg.tar"); len(got) != 1 {
		t.Fatalf("内网地址不应套加速前缀，实际 %v", got)
	}

	// 空地址返回空
	if got := svc.imageAssetURLs(""); len(got) != 0 {
		t.Fatalf("空地址应返回空候选，实际 %v", got)
	}
}

// TestParseContentRange 锁定 Content-Range 解析。
// 它决定 206 响应下的总长度，解析错了进度条会显示成剩余部分。
func TestParseContentRange(t *testing.T) {
	cases := []struct {
		in     string
		want   int64
		wantOK bool
	}{
		{"bytes 100-999/1000", 1000, true},
		{"bytes 0-0/265000000", 265000000, true},
		{"bytes 0-99/*", 0, false}, // 总长未知
		{"", 0, false},
		{"garbage", 0, false},
		{"bytes 0-99/", 0, false},
	}
	for _, c := range cases {
		got, ok := parseContentRange(c.in)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Fatalf("parseContentRange(%q) = (%d, %v)，期望 (%d, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

// TestResumeMetaRoundTrip 锁定断点元数据的存取。
func TestResumeMetaRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "pkg.tar")

	// 空状态
	if e, m := readResumeMeta(dest); e != "" || m != "" {
		t.Fatalf("无元数据时应返回空串，实际 %q %q", e, m)
	}

	h := http.Header{}
	h.Set("ETag", `"abc"`)
	h.Set("Last-Modified", "Wed, 21 Oct 2026 07:28:00 GMT")
	writeResumeMeta(dest, h)

	e, m := readResumeMeta(dest)
	if e != `"abc"` || m != "Wed, 21 Oct 2026 07:28:00 GMT" {
		t.Fatalf("元数据读回不一致：%q %q", e, m)
	}

	clearResumeMeta(dest)
	if e, m := readResumeMeta(dest); e != "" || m != "" {
		t.Fatal("清理后不应再读到元数据")
	}
}

// TestIsGitHubURL 锁定加速前缀的适用范围判定。
func TestIsGitHubURL(t *testing.T) {
	cases := map[string]bool{
		"https://github.com/o/r/releases/download/v1/x.tar": true,
		"https://objects.githubusercontent.com/foo/bar":     true,
		"https://api.github.com/repos/o/r":                  true,
		"http://192.168.1.9:8080/pkg.tar":                   false,
		"https://example.com/github.com/fake":               false,
		"https://evil-github.com/x":                         false,
		"":                                                  false,
	}
	for in, want := range cases {
		if got := isGitHubURL(in); got != want {
			t.Fatalf("isGitHubURL(%q) = %v，期望 %v", in, got, want)
		}
	}
}
