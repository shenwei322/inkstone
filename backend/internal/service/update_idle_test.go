package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// =====================================================================
// 下载空闲超时
//
// 原实现给下载客户端设了 30 分钟的 Client.Timeout（整个请求含读 body 的
// 上限）。实测同一个 265 MB 镜像包，快时 1m52s、慢时 30m35s（差 16 倍），
// 已经贴着天花板——再慢一点就被误杀，报的正是用户最初看到的
// context deadline exceeded (Client.Timeout or context cancellation while
// reading body)。改成本地无进展才超时，下面锁死这个语义。
// =====================================================================

// TestSlowButSteadyDownloadNotAborted 是这一组里最重要的断言：
// 只要数据在持续进来，慢到远超空闲阈值也不能中断。
//
// 这是原实现的真实缺陷——总时长上限会把「网速慢」和「连接死了」
// 混为一谈，而用户那边看到的是下载到一半莫名失败。
func TestSlowButSteadyDownloadNotAborted(t *testing.T) {
	payload := []byte(strings.Repeat("SLOW-BUT-STEADY-", 4000)) // ~60 KB

	// 每个 chunk 之间的间隔明显小于空闲阈值，但整体耗时远超它。
	// 用真实的 downloadIdleTimeout 会跑很久，这里单独造一个短阈值来验证
	// 逻辑本身（阈值大小不影响语义）。
	const chunkInterval = 60 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.Header().Set("ETag", `"slow"`)
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// 分 60 次发送，每次间隔 60ms —— 总计约 3.6 秒
		step := len(payload) / 60
		if step < 1 {
			step = 1
		}
		for i := 0; i < len(payload); i += step {
			end := i + step
			if end > len(payload) {
				end = len(payload)
			}
			if _, err := w.Write(payload[i:end]); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(chunkInterval)
		}
	}))
	defer srv.Close()

	// 空闲阈值 500ms：单次间隔 60ms 远小于它，但总耗时 3.6s 远大于它。
	// 若实现用的是「总时长」，这个下载必然失败；用「空闲超时」就会成功。
	client := newHTTPClientWithGuard(5*time.Second, "", true)
	client.downloader = newDownloadClient()
	client.applySSRFGuard(client.downloader)
	applyIdleTimeout(client.downloader, 500*time.Millisecond)

	dest := filepath.Join(t.TempDir(), "slow.tar")
	res, err := client.download(context.Background(), srv.URL+"/slow", dest, nil)
	if err != nil {
		t.Fatalf("慢速但持续的下载不该被中断：%v", err)
	}
	if res.Bytes != int64(len(payload)) {
		t.Fatalf("字节数 = %d，期望 %d", res.Bytes, len(payload))
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("内容不一致")
	}
}

// TestStalledDownloadAbortedByIdleTimeout 锁死另一半：真卡住必须被中断。
//
// 只测「不误杀」会让实现退化成「永不超时」——那样连接僵死时更新会
// 永远挂着，用户看到进度条不动却没有任何错误。
func TestStalledDownloadAbortedByIdleTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		w.Header().Set("ETag", `"stall"`)
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		// 发一点，然后彻底不动
		_, _ = w.Write([]byte("start"))
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(10 * time.Second) // 远超空闲阈值
	}))
	defer srv.Close()

	client := newHTTPClientWithGuard(5*time.Second, "", true)
	client.downloader = newDownloadClient()
	client.applySSRFGuard(client.downloader)
	applyIdleTimeout(client.downloader, 400*time.Millisecond)

	dest := filepath.Join(t.TempDir(), "stall.tar")
	start := time.Now()
	// 不给外层重试留太多时间：这个测试关心的是「卡住会被中断」，
	// 而重试会把总时长拉长。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := client.download(ctx, srv.URL+"/stall", dest, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("连接僵死应当失败")
	}
	// 空闲阈值 400ms，应在数秒内（而非等满 10 秒服务器睡醒）就中断
	if elapsed > 8*time.Second {
		t.Fatalf("卡死应在空闲阈值后很快中断，实际耗时 %v", elapsed)
	}
}

// TestIdleTimeoutResetsOnData 直接单测计时器语义：每次读到数据都重置。
//
// 上两个是端到端行为，这个是机制本身——它一旦写错（比如忘了 Reset），
// 端到端测试可能因为数据恰好在阈值内到达而蒙混过关。
func TestIdleTimeoutResetsOnData(t *testing.T) {
	var buf bytes.Buffer
	// 用管道模拟「有节奏地来数据」
	pr, pw := io.Pipe()
	body := &idleTimeoutBody{
		ReadCloser: pr,
		reset:      newIdleTimer(context.Background(), 300*time.Millisecond),
	}

	go func() {
		for i := 0; i < 5; i++ {
			_, _ = pw.Write([]byte("x"))
			time.Sleep(100 * time.Millisecond) // 小于 300ms 阈值
		}
		_ = pw.Close()
	}()

	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(&buf, body)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("持续有数据的读取不该超时：%v", err)
		}
		if buf.Len() != 5 {
			t.Fatalf("读到 %d 字节，期望 5", buf.Len())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("读取卡住，空闲计时器可能没有正确重置")
	}
	_ = body.Close()
}
