package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRealGitHubImageDownload 是**联网**的真实下载验证，默认跳过。
//
// 用法（需要能访问 GitHub，耗时数分钟）：
//
//	INKSTONE_REAL_DOWNLOAD=1 go test ./internal/service/ -run TestRealGitHubImageDownload -v -timeout 30m
//
// 存在的理由：单测能证明「断流后会用 Range 接着下」，但证明不了
// 「在真实 GitHub + 加速前缀下，265 MB 的包能下完且 SHA-256 对得上」。
// 线上那个 58.5 MB 超时就是这么暴露的——只靠单测发现不了。
func TestRealGitHubImageDownload(t *testing.T) {
	if os.Getenv("INKSTONE_REAL_DOWNLOAD") != "1" {
		t.Skip("联网测试，设置 INKSTONE_REAL_DOWNLOAD=1 启用")
	}

	const (
		directURL = "https://github.com/shenwei322/inkstone/releases/download/Beta1.28/inkstone-images-Beta1.28.tar"
		// 加速前缀形式：开发机上 GitHub 被 Clash fake-ip 接管（解析到
		// 198.18.x.x 而被 SSRF 守卫拒绝），走加速站可以不依赖本机 DNS。
		proxyURL = "https://gh-proxy.com/https://github.com/shenwei322/inkstone/releases/download/Beta1.28/inkstone-images-Beta1.28.tar"
		wantSize = int64(277846528)
		// Beta1.28 的镜像包被替换过两次（都随修复重新发布）：
		//   ① 277816320 / 84a5445e…  首发（无断点续传）
		//   ② 277837312 / 3f91af55…  加断点续传 + 镜像加速
		//   ③ 277846528 / 4225b9bf…  下载超时改为空闲超时
		// 若这里与 Release 上的 digest 不一致，说明包被再次替换，需同步更新。
		wantSHA = "4225b9bf51c67b853be3c3c11cce8cdba868cdc3c05b0116e03d5bd5dc5f32bd"
	)

	rawURL := directURL
	if os.Getenv("INKSTONE_REAL_DOWNLOAD_VIA") == "proxy" {
		rawURL = proxyURL
	}

	svc := newTestUpdateService(t, t.TempDir())
	// 这个测试验证的是**下载逻辑本身**（续传/重试/完整性），
	// SSRF 守卫由 TestIsFakeIPRange 与 update_trust_test.go 单独覆盖。
	// 开发机的代理把 IP 换成了虚拟段，这里必须放行才能验证真实下载。
	svc.cfg.AllowPrivateHosts = true
	svc.client = newHTTPClientWithGuard(30*time.Second, "", true)

	dest := filepath.Join(t.TempDir(), "beta128.tar")
	var lastReport time.Time
	start := time.Now()

	res, err := svc.client.download(context.Background(), rawURL, dest, func(done, total int64) {
		// 每 5 秒报一次进度，便于观察是否卡住
		if time.Since(lastReport) > 5*time.Second {
			lastReport = time.Now()
			t.Logf("进度：%.1f / %.1f MB（已用 %s）",
				float64(done)/1024/1024, float64(total)/1024/1024, time.Since(start).Round(time.Second))
		}
	})
	if err != nil {
		t.Fatalf("真实下载失败：%v", err)
	}

	t.Logf("下载完成：%d 字节，用时 %s", res.Bytes, time.Since(start).Round(time.Second))
	if res.Bytes != wantSize {
		t.Fatalf("大小不符：%d，期望 %d", res.Bytes, wantSize)
	}
	if res.SHA256 != wantSHA {
		t.Fatalf("SHA-256 不符：%s，期望 %s", res.SHA256, wantSHA)
	}

	// 落盘内容也要对得上（防止「摘要对但文件坏」这类不可能但值得一验的情况）
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSHA {
		t.Fatalf("落盘文件 SHA-256 不符：%s", got)
	}
	t.Log("落盘文件校验通过")
}
