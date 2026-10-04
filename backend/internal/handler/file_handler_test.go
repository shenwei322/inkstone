package handler

import (
	"strings"
	"testing"
)

// TestContentDispositionRejectsCRLF 锁定响应头注入防护：
// 文件名里的 CR/LF 必须被替换，否则可造成 Content-Disposition 头注入。
// 文件名来自用户上传，上游只用 filepath.Base 去目录，不剥离换行。
func TestContentDispositionRejectsCRLF(t *testing.T) {
	got := contentDisposition("evil\r\nX-Injected: 1.txt")

	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("响应头中不应出现 CR/LF：%q", got)
	}
	if !strings.Contains(got, "filename=") {
		t.Errorf("应保留 filename 参数：%q", got)
	}
	// 注入串里的换行被替换为下划线后，头部结构保持单行
	if strings.Contains(got, "X-Injected: 1") && strings.Contains(got, "\r\n") {
		t.Errorf("注入内容仍可影响头部结构：%q", got)
	}
}

// TestContentDispositionEscapesQuotes 引号与反斜杠不能提前闭合参数值。
func TestContentDispositionEscapesQuotes(t *testing.T) {
	got := contentDisposition(`a"b\c.txt`)
	if !strings.HasPrefix(got, `attachment; filename="`) {
		t.Fatalf("头部格式异常：%q", got)
	}
	// filename 段内除包裹引号外不应再有裸引号
	inner := strings.TrimSuffix(strings.TrimPrefix(got, `attachment; filename="`), `"; filename*=UTF-8''a%22b%5Cc.txt`)
	if strings.ContainsAny(inner, `"\`) {
		t.Errorf("引号/反斜杠未转义：%q", inner)
	}
}

// TestContentDispositionKeepsChinese 中文名走 filename* 编码，保留可读回退。
func TestContentDispositionKeepsChinese(t *testing.T) {
	got := contentDisposition("报告.pdf")
	if !strings.Contains(got, "filename*=UTF-8''") {
		t.Errorf("应包含 RFC 5987 的 filename* 参数：%q", got)
	}
	// ASCII 回退段里非 ASCII 字符被替换为下划线，但仍保留扩展名
	if !strings.Contains(got, ".pdf") {
		t.Errorf("扩展名应保留：%q", got)
	}
}
