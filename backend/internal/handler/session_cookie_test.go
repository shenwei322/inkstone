package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/service"
)

// =====================================================================
// 会话 Cookie 在 HTTP 层的接线测试
//
// 这一组用例不连数据库，只验证「Cookie 被正确地写下 / 读走 / 清掉」。
// 值得单独测的理由：Cookie 相关的错误几乎都不报错——属性写错、
// 名字不匹配、读取时没回退，全部表现为「功能看起来正常但用户仍要
// 反复登录」，靠人工点页面很难定位。
// =====================================================================

func sessionRouter(h *AuthHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/api/v1/auth/logout", h.Logout)
	return r
}

// TestIssueSessionSetsCookie 验证登录成功后下发了带安全属性的会话 Cookie。
func TestIssueSessionSetsCookie(t *testing.T) {
	c, w := newTestContext(t)
	h := &AuthHandler{session: service.SessionCookieConfig{
		Name: "__Host-inkstone_session",
		TTL:  30 * 24 * time.Hour,
	}}
	h.issueSession(c, &service.TokenPair{
		AccessToken:      "A",
		RefreshToken:     "R",
		SessionExpiresIn: 30 * 24 * 3600,
	})

	ck := findResponseCookie(w, "__Host-inkstone_session")
	if ck == nil {
		t.Fatalf("登录后未下发会话 Cookie，实际=%v", w.Header().Values("Set-Cookie"))
	}
	if ck.Value != "R" {
		t.Errorf("Cookie 应携带 refresh 令牌，实际 %q", ck.Value)
	}
	if !ck.HttpOnly || !ck.Secure || ck.Path != "/" {
		t.Errorf("安全属性不完整：HttpOnly=%v Secure=%v Path=%q", ck.HttpOnly, ck.Secure, ck.Path)
	}
	// 带 Set-Cookie 的响应绝不能被中间层缓存后派发给下一个访客。
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("登录响应 Cache-Control = %q，想要 no-store", got)
	}
}

// TestIssueSessionSkippedWhenUnconfigured 验证未注入配置时不写 Cookie。
//
// 这条守的是「单元测试可直接构造 AuthHandler」这个便利：若零值配置也会
// 写出一枚名为空串的 Cookie，测试环境会出现难以理解的响应头。
func TestIssueSessionSkippedWhenUnconfigured(t *testing.T) {
	c, w := newTestContext(t)
	h := &AuthHandler{}
	h.issueSession(c, &service.TokenPair{RefreshToken: "R"})
	if len(w.Result().Cookies()) != 0 {
		t.Errorf("未配置会话 Cookie 时不应下发，实际=%v", w.Result().Cookies())
	}
}

// TestLogoutClearsCookie 验证登出会清掉会话 Cookie。
//
// 这是「点了退出登录，刷新页面又登上」的直接成因：只清 localStorage
// 清不掉 HttpOnly Cookie。
func TestLogoutClearsCookie(t *testing.T) {
	h := &AuthHandler{
		session: service.SessionCookieConfig{Name: "__Host-inkstone_session"},
	}
	r := sessionRouter(h)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("登出应返回 200，实际 %d body=%s", w.Code, w.Body.String())
	}
	var cleared []string
	for _, ck := range w.Result().Cookies() {
		if ck.MaxAge < 0 && ck.Value == "" {
			cleared = append(cleared, ck.Name)
		}
	}
	if len(cleared) == 0 {
		t.Fatalf("登出未清任何 Cookie，实际=%v", w.Header().Values("Set-Cookie"))
	}
	found := false
	for _, n := range cleared {
		if strings.Contains(n, "inkstone_session") {
			found = true
		}
	}
	if !found {
		t.Errorf("未清除会话 Cookie，实际清除=%v", cleared)
	}
}

func newTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	return c, w
}

func findResponseCookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, ck := range w.Result().Cookies() {
		if ck.Name == name {
			return ck
		}
	}
	return nil
}
