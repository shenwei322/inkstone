package service

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// =====================================================================
// 浏览器会话 Cookie
//
// 背景：此前登录态只以 JSON 里的 access/refresh 令牌下发，前端把两者存进
// localStorage，完全依赖客户端 JS 在 401 时调 /auth/refresh 续期。这条链路
// 有三个真实缺陷，症状都是「刚登录，一刷新又要重新登录」：
//
//  1. **refresh 是一次性的、无重放的**。前端每次刷新都拿着同一个 refresh
//     令牌去换新令牌对，而服务端签发的 refresh 是**无状态**的：只要它还没
//     过期就能反复使用。反过来说，任何一次刷新失败（并发请求同时刷新、
//     刷新时网络抖动、Tab 在后台被冻结导致刷新请求被浏览器丢弃）都会让
//     前端 `clearTokens()` 把登录态直接抹掉，用户看到的就是掉线。
//
//  2. **localStorage 在需要「每次访问都保持登录」时并不可靠**。Safari /
//     iOS 的 7 天脚本可写存储清理、隐私模式、容器隔离、以及用户清站点数据，
//     都会让 localStorage 里的令牌凭空消失；Cookie 走的是另一套存储与
//     清理策略，且由浏览器在每次请求里自动携带。
//
//  3. **跨站部署取不到令牌**。访客站与后台跨源时（FRONTEND_URL /
//     ADMIN_URL 不同），前端拿到的仍是 JSON 令牌，一旦 localStorage 被清，
//     没有任何服务端侧的「你是谁」记录可用。
//
// 本文件提供的方案：把 **refresh 令牌**同时写进一枚 HttpOnly Cookie。
//   - HttpOnly：JS 读不到，XSS 也偷不走 refresh；
//   - Secure + Path=/（生产带 __Host- 前缀）：不可被子域/其他路径覆盖；
//   - SameSite=None + Secure：跨源部署（管理后台在独立域名）下仍会携带，
//     这也意味着必须配合 CSRF 防护——见 middleware.CSRFGuard 的说明。
//
// access 令牌**不**进 Cookie：它寿命只有 15 分钟，且中间件支持
// Authorization 头优先，继续放在前端内存/localStorage 里对现有代码最友好。
// =====================================================================

// ErrNoSessionCookie 表示请求里没有会话 Cookie。
var ErrNoSessionCookie = errors.New("no session cookie")

// SessionCookieConfig 描述如何写会话 Cookie。
type SessionCookieConfig struct {
	// Name 是 Cookie 名（生产为 __Host- 前缀版本）。
	Name string
	// Insecure 为 true 时降级：不加 Secure、SameSite 用 Lax。
	// 仅用于 http://localhost 开发环境。
	Insecure bool
	// TTL 是本次会话 Cookie 的存活时长（秒），与 refresh 令牌的有效期一致。
	TTL time.Duration
}

// sessionCookiePath 固定为根路径。
//
// 不能写成 /api：前端与管理后台可能挂在不同前缀下，且 __Host- 前缀要求
// Path 必须恰好是 "/"，否则浏览器直接拒绝该 Cookie。
const sessionCookiePath = "/"

// RefreshTokenCookieName 返回应使用的 Cookie 名。
func (c SessionCookieConfig) cookieName() string {
	if c.Name != "" {
		return c.Name
	}
	return "inkstone_session"
}

// SetRefreshCookie 把 refresh 令牌写进会话 Cookie。
//
// 同时会清掉另一套命名下的旧值：生产/开发切换（__Host- 前缀有无）时，
// 浏览器可能同时持有两枚同名不同前缀的 Cookie，服务端读取时拿到哪一个
// 取决于浏览器实现。主动清掉不用的那一枚，避免出现「刷新时好时坏」。
func SetRefreshCookie(c *gin.Context, cfg SessionCookieConfig, refreshToken string) {
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	maxAge := int(ttl.Seconds())

	sameSite := http.SameSiteNoneMode
	secure := true
	if cfg.Insecure {
		// 本地 http 环境：SameSite=None 必须搭 Secure，浏览器会拒收。
		// 退到 Lax——同站（localhost:3000 → localhost:8080 属于同站）够用。
		sameSite = http.SameSiteLaxMode
		secure = false
	}

	// __Host- 前缀的硬性要求：Secure、Path=/、且**不能**设置 Domain。
	// 这三条由浏览器校验，写错了整个 Cookie 会被静默丢弃。
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     cfg.cookieName(),
		Value:    refreshToken,
		Path:     sessionCookiePath,
		MaxAge:   maxAge,
		Expires:  time.Now().Add(ttl),
		Secure:   secure,
		HttpOnly: true,
		SameSite: sameSite,
	})

	// 清掉命名不匹配的那一枚，避免环境切换后残留。
	if stale := otherCookieName(cfg.cookieName()); stale != "" {
		ClearCookie(c, stale, secure)
	}
}

// ClearSessionCookie 让浏览器删除会话 Cookie（登出 / 令牌失效时调用）。
func ClearSessionCookie(c *gin.Context, cfg SessionCookieConfig) {
	ClearCookie(c, cfg.cookieName(), !cfg.Insecure)
	if stale := otherCookieName(cfg.cookieName()); stale != "" {
		ClearCookie(c, stale, !cfg.Insecure)
	}
}

// ClearCookie 删除指定名字的 Cookie。
//
// 过期时间必须同时给 MaxAge=-1 与过去的 Expires：只给其中一个时，
// 部分浏览器（老 Safari / 某些移动端 WebView）不会真正删除，
// 用户会看到「登出后刷新又登上了」。
func ClearCookie(c *gin.Context, name string, secure bool) {
	if name == "" {
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     sessionCookiePath,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteDefaultMode,
	})
}

// ReadRefreshCookie 读取会话 Cookie 里的 refresh 令牌。
//
// 找不到时返回 ErrNoSessionCookie，调用方据此回退到请求体里的 refresh_token
// ——兼容尚未升级的客户端，也让 curl 调试链路继续可用。
func ReadRefreshCookie(c *gin.Context, cfg SessionCookieConfig) (string, error) {
	names := []string{cfg.cookieName()}
	if stale := otherCookieName(cfg.cookieName()); stale != "" {
		names = append(names, stale)
	}
	for _, n := range names {
		if v, err := c.Cookie(n); err == nil && v != "" {
			return v, nil
		}
	}
	return "", ErrNoSessionCookie
}

// otherCookieName 返回与 name 对应的「另一套命名」，用于清理残留。
func otherCookieName(name string) string {
	switch {
	case name == "__Host-inkstone_session":
		return "inkstone_session"
	case name == "inkstone_session":
		return "__Host-inkstone_session"
	default:
		return ""
	}
}
