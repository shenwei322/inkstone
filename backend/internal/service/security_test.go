package service

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
)

// TestIsPublicIP 锁定 SSRF 防护的地址判定：内网/回环/链路本地（含云元数据）
// 一律不可访问，只有真正的公网地址放行。
func TestIsPublicIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1",       // 回环
		"::1",             // IPv6 回环
		"10.0.0.1",        // 私网 A
		"172.16.0.1",      // 私网 B
		"192.168.1.1",     // 私网 C
		"169.254.169.254", // 云元数据（链路本地）
		"0.0.0.0",         // 未指定
		"100.64.0.1",      // CGNAT
		"192.0.0.1",       // IETF 协议专用
		"198.18.0.1",      // 基准测试网段
		"240.0.0.1",       // 保留
		"255.255.255.255", // 广播
		"224.0.0.1",       // 组播
		"fc00::1",         // IPv6 唯一本地
		"fe80::1",         // IPv6 链路本地
	}
	for _, raw := range blocked {
		ip := net.ParseIP(raw)
		if ip == nil {
			t.Fatalf("测试用例 IP 无法解析：%s", raw)
		}
		if isPublicIP(ip) {
			t.Errorf("%s 应被判定为不可访问", raw)
		}
	}

	allowed := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700::1111"}
	for _, raw := range allowed {
		ip := net.ParseIP(raw)
		if !isPublicIP(ip) {
			t.Errorf("%s 应被判定为可访问", raw)
		}
	}
}

// TestRejectPrivateHost 校验友链地址的第一道拦截。
func TestRejectPrivateHost(t *testing.T) {
	// 字面量内网地址：必须拒绝
	for _, raw := range []string{
		"http://127.0.0.1/admin",
		"http://169.254.169.254/latest/meta-data/",
		"https://192.168.1.1/",
		"http://10.0.0.5:8080/",
		"http://[::1]/",
	} {
		if err := RejectPrivateHost(raw); err == nil {
			t.Errorf("%s 应被拒绝", raw)
		}
	}

	// localhost 域名解析到回环：必须拒绝
	if err := RejectPrivateHost("http://localhost:8080/"); err == nil {
		t.Error("localhost 应被拒绝（解析到回环地址）")
	}

	// 格式错误的地址：拒绝（而不是放行）
	if err := RejectPrivateHost("http:///nohost"); err == nil {
		t.Error("缺少主机名的地址应被拒绝")
	}
}

// TestSanitizeWidgetHTML 锁定后台自定义 HTML 的净化：
// 脚本、事件属性、iframe 必须被移除，正常排版标签保留。
func TestSanitizeWidgetHTML(t *testing.T) {
	danger := `<p>正常</p><script>alert(1)</script>` +
		`<img src=x onerror="alert(2)">` +
		`<a href="javascript:alert(3)">bad</a>` +
		`<iframe src="https://evil.example"></iframe>` +
		`<div onclick="alert(4)">click</div>`

	got := SanitizeWidgetHTML(danger)

	for _, forbidden := range []string{"<script", "onerror", "onclick", "javascript:", "<iframe"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("危险内容 %q 未被移除：%s", forbidden, got)
		}
	}
	if !strings.Contains(got, "<p>正常</p>") {
		t.Errorf("合法段落被误删：%s", got)
	}
}

// TestSanitizeSidebarWidgets 只处理 html 类型小工具，其余条目原样保留。
func TestSanitizeSidebarWidgets(t *testing.T) {
	input := `[{"type":"html","title":"x","content":"<p>a</p><script>alert(1)</script>"},` +
		`{"type":"profile","title":"p","content":"<b>纯文本字段不改</b>"}]`

	out, err := sanitizeSidebarWidgets(input)
	if err != nil {
		t.Fatalf("净化失败：%v", err)
	}
	if strings.Contains(out, "<script") {
		t.Errorf("脚本未被移除：%s", out)
	}

	var widgets []map[string]any
	if err := json.Unmarshal([]byte(out), &widgets); err != nil {
		t.Fatalf("输出不是合法 JSON：%v", err)
	}
	if len(widgets) != 2 {
		t.Fatalf("条目数应保持 2，实际 %d", len(widgets))
	}
	// 非 html 类型原样保留（它们不走 innerHTML）
	if widgets[1]["content"] != "<b>纯文本字段不改</b>" {
		t.Errorf("非 html 类型不应被改动：%v", widgets[1]["content"])
	}
}

// TestSanitizeSidebarWidgetsInvalidJSON 非法 JSON 应报错，避免把坏数据写库。
func TestSanitizeSidebarWidgetsInvalidJSON(t *testing.T) {
	if _, err := sanitizeSidebarWidgets("{not json"); err == nil {
		t.Error("非法 JSON 应返回错误")
	}
	if out, err := sanitizeSidebarWidgets(""); err != nil || out != "" {
		t.Errorf("空值应原样返回，得到 out=%q err=%v", out, err)
	}
}

// TestTokenVersionRevocation TokenVersion 是令牌撤销机制的核心：
// 声明缺失视为 0（兼容升级前的旧令牌），显式值原样返回。
func TestTokenVersionRevocation(t *testing.T) {
	// 旧令牌没有 ver 字段 → 视为 0，与默认 TokenVersion 一致，不会误伤
	var legacy Claims
	if legacy.TokenVersionOf() != 0 {
		t.Errorf("缺失 ver 应视为 0，得到 %d", legacy.TokenVersionOf())
	}

	ver := int64(7)
	current := Claims{Ver: &ver}
	if current.TokenVersionOf() != 7 {
		t.Errorf("应返回 7，得到 %d", current.TokenVersionOf())
	}

	zero := int64(0)
	explicit := Claims{Ver: &zero}
	if explicit.TokenVersionOf() != 0 {
		t.Errorf("显式 0 应返回 0，得到 %d", explicit.TokenVersionOf())
	}

	// 版本不匹配 = 已撤销
	userVersion := int64(8)
	if current.TokenVersionOf() == userVersion {
		t.Error("代次不一致时应判定为已撤销")
	}
}
