package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// newCookieRecorder 返回一个可用来检查 Set-Cookie 头的 gin 上下文。
func newCookieRecorder(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	return c, w
}

func findCookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, ck := range w.Result().Cookies() {
		if ck.Name == name {
			return ck
		}
	}
	return nil
}

// TestSetRefreshCookieSecureAttributes 锁定生产环境下会话 Cookie 的安全属性。
//
// 这几项里任何一项写错都不会让功能失败——请求照样能跑通，登录照样"成功"——
// 但安全性会静默降级（XSS 能读走 refresh、子域能覆盖会话）。因此必须钉住：
//   - HttpOnly：JS 读不到，XSS 偷不走 refresh；
//   - Secure：只在 https 上发送；
//   - Path=/：__Host- 前缀的硬性要求，写错整枚 Cookie 会被浏览器丢弃；
//   - SameSite=None：跨源部署（管理后台独立域名）下仍会携带；
//   - 不设 Domain：同上，__Host- 前缀要求。
func TestSetRefreshCookieSecureAttributes(t *testing.T) {
	c, w := newCookieRecorder(t)
	cfg := SessionCookieConfig{Name: "__Host-inkstone_session", TTL: 30 * 24 * time.Hour}
	SetRefreshCookie(c, cfg, "REFRESH_TOKEN")

	ck := findCookie(w, "__Host-inkstone_session")
	if ck == nil {
		t.Fatalf("未下发会话 Cookie，实际头=%v", w.Header().Values("Set-Cookie"))
	}
	if !ck.HttpOnly {
		t.Error("HttpOnly 必须开启：否则 XSS 可直接读走 refresh 令牌")
	}
	if !ck.Secure {
		t.Error("Secure 必须开启：__Host- 前缀与跨站携带都要求它")
	}
	if ck.Path != "/" {
		t.Errorf("Path 必须为 /（__Host- 前缀硬性要求），实际 %q", ck.Path)
	}
	if ck.Domain != "" {
		t.Errorf("不得设置 Domain（__Host- 前缀硬性要求），实际 %q", ck.Domain)
	}
	if ck.SameSite != http.SameSiteNoneMode {
		t.Errorf("SameSite=None 用于跨源部署，实际 %v", ck.SameSite)
	}
	if ck.Value != "REFRESH_TOKEN" {
		t.Errorf("Cookie 值应为 refresh 令牌，实际 %q", ck.Value)
	}
	// MaxAge 必须跟着 TTL 走：写成固定值会让「记住我 30 天」的 Cookie
	// 在 7 天后被浏览器自动丢弃，而服务端认为它还该有效。
	if ck.MaxAge != int((30 * 24 * time.Hour).Seconds()) {
		t.Errorf("MaxAge = %d，想要 %d", ck.MaxAge, int((30 * 24 * time.Hour).Seconds()))
	}
}

// TestSetRefreshCookieInsecureModeForLocalDev 验证本地 http 开发会降级。
//
// 不降级的话，浏览器会静默丢弃带 Secure 的 Cookie，表现为
// 「本地登录成功但一刷新就掉线」——一个排查成本极高的症状。
func TestSetRefreshCookieInsecureModeForLocalDev(t *testing.T) {
	c, w := newCookieRecorder(t)
	cfg := SessionCookieConfig{Name: "inkstone_session", Insecure: true, TTL: time.Hour}
	SetRefreshCookie(c, cfg, "T")

	ck := findCookie(w, "inkstone_session")
	if ck == nil {
		t.Fatal("未下发会话 Cookie")
	}
	if ck.Secure {
		t.Error("本地 http 开发不应带 Secure，否则浏览器会丢弃整枚 Cookie")
	}
	if ck.SameSite != http.SameSiteLaxMode {
		t.Errorf("降级模式应使用 Lax，实际 %v", ck.SameSite)
	}
	if !ck.HttpOnly {
		t.Error("降级只影响 Secure/SameSite，HttpOnly 必须始终开启")
	}
}

// TestClearSessionCookieRemovesBothNames 验证登出会清掉两套命名。
//
// 只在生产/开发之间切换过环境（或从旧版本升级）时，浏览器里可能同时
// 存在带前缀与不带前缀的两枚 Cookie。只清一枚会留下「登出后仍然登录」。
func TestClearSessionCookieRemovesBothNames(t *testing.T) {
	c, w := newCookieRecorder(t)
	ClearSessionCookie(c, SessionCookieConfig{Name: "__Host-inkstone_session"})

	names := map[string]*http.Cookie{}
	for _, ck := range w.Result().Cookies() {
		names[ck.Name] = ck
	}
	for _, want := range []string{"__Host-inkstone_session", "inkstone_session"} {
		ck, ok := names[want]
		if !ok {
			t.Errorf("未下发 %s 的清除 Cookie", want)
			continue
		}
		if ck.MaxAge >= 0 {
			t.Errorf("%s 的 MaxAge 应为负值，实际 %d", want, ck.MaxAge)
		}
		if ck.Value != "" {
			t.Errorf("%s 的值应为空，实际 %q", want, ck.Value)
		}
	}
}

// TestReadRefreshCookieFallsBackToOtherName 验证读取时兼容另一套命名。
func TestReadRefreshCookieFallsBackToOtherName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	// 客户端只带了旧命名的 Cookie
	req.AddCookie(&http.Cookie{Name: "inkstone_session", Value: "OLD"})
	c.Request = req

	got, err := ReadRefreshCookie(c, SessionCookieConfig{Name: "__Host-inkstone_session"})
	if err != nil {
		t.Fatalf("应能回退到另一套命名，实际报错 %v", err)
	}
	if got != "OLD" {
		t.Errorf("读取值 = %q，想要 OLD", got)
	}
}

// TestReadRefreshCookieMissing 验证没有 Cookie 时返回专用错误。
//
// 调用方靠这个错误决定「回退到请求体里的 refresh_token」，
// 因此不能返回空串 + nil 这种无法区分「缺失」与「空值」的结果。
func TestReadRefreshCookieMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	if _, err := ReadRefreshCookie(c, SessionCookieConfig{Name: "inkstone_session"}); err == nil {
		t.Error("没有 Cookie 时应返回错误")
	}
}
