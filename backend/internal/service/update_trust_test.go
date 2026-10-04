package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestParseChecksumsFile 覆盖 GNU sha256sum 的两种书写模式、注释、非法行。
func TestParseChecksumsFile(t *testing.T) {
	raw := strings.Join([]string{
		"# 这是注释",
		"",
		"abc123def456abc123def456abc123def456abc123def456abc123def456abcd  inkstone-images-v1.28.0.tar",
		"0000111122223333444455556666777788889999aaaabbbbccccddddeeeeffff  *inkstone-images-v1.27.0.tar",
		"tooshort  bad-name",
		"zzzz 999 888 777 666 555 444 333 222 111 000 999 888 777 666 555 not-hex-value.at",
		"onlyonefield",
	}, "\n")

	sums := parseChecksumsFile(raw)

	want := "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"
	if sums["inkstone-images-v1.28.0.tar"] != want {
		t.Fatalf("文本模式解析失败：%q", sums["inkstone-images-v1.28.0.tar"])
	}
	// 二进制模式的前导 * 必须被剥掉
	if sums["inkstone-images-v1.27.0.tar"] != "0000111122223333444455556666777788889999aaaabbbbccccddddeeeeffff" {
		t.Fatalf("二进制模式解析失败：%q", sums["inkstone-images-v1.27.0.tar"])
	}
	// 太短的行、非 hex、字段不足都应被忽略
	if _, ok := sums["bad-name"]; ok {
		t.Fatal("长度不足的摘要不应该被接受")
	}
	if _, ok := sums["not-hex-value.at"]; ok {
		t.Fatal("非十六进制摘要不应该被接受")
	}
	if _, ok := sums["onlyonefield"]; ok {
		t.Fatal("只有一个字段的行不应该被接受")
	}
}

// TestVerifyChecksum 确认：没有期望值时不报错、不一致时报错、一致时通过。
func TestVerifyChecksum(t *testing.T) {
	// 空期望值 = 没有信任根。此时不阻断（历史版本可能没发 checksums），
	// 但 verified 必须为 false，由调用方如实呈现给运维。
	if ok, err := verifyChecksum("deadbeef", ""); ok || err != nil {
		t.Fatalf("空期望值应返回 (false, nil)，得到 (%v, %v)", ok, err)
	}

	actual := strings.Repeat("a", 64)
	// 不一致必须报错，这是阻挡污染镜像的核心分支
	if _, err := verifyChecksum(actual, strings.Repeat("b", 64)); err == nil {
		t.Fatal("摘要不一致时必须报错")
	}
	// 大小写不同不算不一致（hex 摘要大小写等价）
	ok, err := verifyChecksum(actual, strings.ToUpper(actual))
	if !ok || err != nil {
		t.Fatalf("大小写不同的相同摘要应判定通过，得到 (%v, %v)", ok, err)
	}
	// 实际值不是合法 sha256 时报错
	if _, err := verifyChecksum("not-a-hash", strings.Repeat("c", 64)); err == nil {
		t.Fatal("非法的实际摘要必须报错")
	}
}

// TestExpectedChecksumPrefersConfig 确认显式配置优先于 Release 资产。
// 这是信任根的优先级约定：运维自己写下的期望值最可信。
func TestExpectedChecksumPrefersConfig(t *testing.T) {
	svc := newTestUpdateService(t, t.TempDir())
	svc.cfg.UpdateChecksum = strings.Repeat("d", 64)

	rel := ReleaseBrief{
		Tag: "v1.0.0",
		Assets: []ReleaseAssetBrief{
			{Name: "checksums.txt", URL: "http://127.0.0.1:1/should-not-be-reached"},
		},
	}
	want, ok := svc.expectedChecksum(nil, rel, "inkstone-images-v1.0.0.tar")
	if !ok || want != strings.Repeat("d", 64) {
		t.Fatalf("显式配置应优先生效，得到 (%q, %v)", want, ok)
	}
}

// TestIsHexString 是摘要解析的基础判定。
func TestIsHexString(t *testing.T) {
	if !isHexString(strings.Repeat("f", 64), 64) {
		t.Fatal("64 位小写 hex 应通过")
	}
	if !isHexString(strings.Repeat("A", 64), 64) {
		t.Fatal("64 位大写 hex 应通过")
	}
	if isHexString(strings.Repeat("f", 63), 64) {
		t.Fatal("长度不符应失败")
	}
	if isHexString(strings.Repeat("g", 64), 64) {
		t.Fatal("非 hex 字符应失败")
	}
}

// TestChecksumsFileRoundTrip 保证读文件路径同样可用（writePending 系列之外
// 的普通磁盘读取），避免临时文件相关的权限/路径问题被忽略。
func TestChecksumsFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "checksums.txt")
	body := strings.Repeat("1", 64) + "  some-asset.tar\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("写临时文件失败：%v", err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读临时文件失败：%v", err)
	}
	sums := parseChecksumsFile(string(raw))
	if sums["some-asset.tar"] != strings.Repeat("1", 64) {
		t.Fatalf("从磁盘读回的清单解析失败：%q", sums["some-asset.tar"])
	}
}

// TestSSRFGuardBlocksPrivateTarget 证明下载链路真的会拒绝内网目标。
//
// 这条测试是上面 Checksum 信任根之外的另一半：即便摘要是对的，也不该让
// 更新通道成为打到云元数据地址（169.254.169.254）或本机管理端口的跳板。
// allowPrivate=false 是生产默认值。
func TestSSRFGuardBlocksPrivateTarget(t *testing.T) {
	// 用一个监听在回环地址的服务器充当「被污染的上游」
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("payload"))
	}))
	defer server.Close()

	client := newHTTPClientWithGuard(5*time.Second, "", false)
	dest := filepath.Join(t.TempDir(), "pkg")
	_, err := client.download(context.Background(), server.URL+"/archive", dest, nil)
	if err == nil {
		t.Fatal("内网目标应被拒绝，但下载成功了")
	}
	if !strings.Contains(err.Error(), "已拒绝") {
		t.Fatalf("报错信息应说明是内网地址被拒，实际：%v", err)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatal("被拒绝的下载不应留下半截文件")
	}
}

// TestSSRFGuardAllowsPrivateWhenOptedIn 证明显式放行开关仍然可用
// （自建内网更新服务器是合法部署形态，不能一刀切封死）。
func TestSSRFGuardAllowsPrivateWhenOptedIn(t *testing.T) {
	payload := []byte("payload")
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write(payload)
	}))
	defer server.Close()

	client := newHTTPClientWithGuard(5*time.Second, "", true)
	dest := filepath.Join(t.TempDir(), "pkg")
	res, err := client.download(context.Background(), server.URL+"/archive", dest, nil)
	if err != nil {
		t.Fatalf("显式放行后内网目标应可下载：%v", err)
	}
	if gotPath != "/archive" {
		t.Fatalf("请求路径不对：%q", gotPath)
	}
	if res.Bytes != int64(len(payload)) {
		t.Fatalf("字节数不对：%d", res.Bytes)
	}
}
