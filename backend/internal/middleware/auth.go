package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/service"
)

const (
	ContextUserKey = "currentUser"
)

type CurrentUser struct {
	ID       uint
	Role     string
	Username string
}

// UserStatus 是一次账号校验的结果：数据库里的**当前**用户名与角色。
//
// Role 必须回传真实值（而不是沿用 JWT 里的旧 claim）：令牌签发后管理员可能
// 被降级、普通用户可能被提升，若角色取自信任令牌，`RequireRole` 就会在
// refresh TTL 内继续放行已被撤销的权限。
type UserStatus struct {
	Username string
	Role     string
}

// UserChecker returns (status, ok). ok=false rejects the token. It is called on
// every authenticated request so banned/deleted accounts are rejected even
// while their access token is still unexpired; the returned username/role
// overwrite the token claims so permission changes take effect immediately.
type UserChecker func(id uint) (UserStatus, bool)

// bearerToken 从请求里取出访问凭据（access 令牌，或回退到会话 Cookie 里的
// refresh 令牌）。
//
// 读取顺序是刻意的：**Authorization 头优先于会话 Cookie**。
// Cookie 里放的是 refresh 令牌，只有当请求没带（或带了无效）Bearer
// access 时才回退到 Cookie，用它与库中令牌代次比对。这样：
//   - 老客户端（只发头）行为完全不变；
//   - 新客户端 access 过期而 refresh 仍在时，请求仍能被识别，
//     不会出现「access 一过期，所有接口一起 401」的连锁掉线。
//
// 返回的 bool 表示该令牌是否来自 Cookie（调用方可能需要区别对待）。
func bearerToken(c *gin.Context, tokens *service.TokenManager, session service.SessionCookieConfig) (claims *service.Claims, fromCookie bool, ok bool) {
	header := c.GetHeader("Authorization")
	if header != "" {
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
			return nil, false, false
		}
		if cl, err := tokens.Parse(parts[1], service.TokenTypeAccess); err == nil {
			return cl, false, true
		}
		return nil, false, false
	}

	raw, err := service.ReadRefreshCookie(c, session)
	if err != nil {
		return nil, true, false
	}
	cl, err := tokens.Parse(raw, service.TokenTypeRefresh)
	if err != nil {
		return nil, true, false
	}
	return cl, true, true
}

// Auth requires a valid access token. When checkUser is provided, the user is
// also verified against the database so banned/deleted accounts are rejected
// even while their access token is still unexpired.
func Auth(tokens *service.TokenManager, checkUser UserChecker) gin.HandlerFunc {
	return AuthWithSession(tokens, checkUser, service.SessionCookieConfig{})
}

// AuthWithSession 与 Auth 相同，额外支持「用会话 Cookie 作为访问凭据」。
func AuthWithSession(tokens *service.TokenManager, checkUser UserChecker, session service.SessionCookieConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, _, ok := bearerToken(c, tokens, session)
		if !ok {
			if c.GetHeader("Authorization") == "" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
				return
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		if checkUser != nil {
			status, ok := checkUser(claims.UserID)
			if !ok {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "该账号已被封禁，请联系管理员"})
				return
			}
			// 用库里的真实用户名与角色覆盖令牌声明：降级/提升立即生效
			claims.Username = status.Username
			claims.Role = status.Role
		}

		c.Set(ContextUserKey, CurrentUser{ID: claims.UserID, Role: claims.Role, Username: claims.Username})
		c.Next()
	}
}

func GetCurrentUser(c *gin.Context) (CurrentUser, bool) {
	v, ok := c.Get(ContextUserKey)
	if !ok {
		return CurrentUser{}, false
	}
	user, ok := v.(CurrentUser)
	return user, ok
}

// RequireRole aborts unless the authenticated user has one of the allowed roles.
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := GetCurrentUser(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		for _, r := range roles {
			if user.Role == r {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
	}
}

// OptionalAuth parses the Bearer token when present and stores the current
// user, but lets the request continue anonymously otherwise.
//
// checkUser 与 Auth 语义一致：无效/被封禁的令牌不会被采信（当作匿名），
// 角色同样以数据库为准——否则被封禁的用户在其 token 过期前仍能通过
// canViewArticle 读到自己的草稿。
func OptionalAuth(tokens *service.TokenManager, checkUser UserChecker) gin.HandlerFunc {
	return OptionalAuthWithSession(tokens, checkUser, service.SessionCookieConfig{})
}

// OptionalAuthWithSession 同 OptionalAuth，额外接受会话 Cookie 作为凭据。
func OptionalAuthWithSession(tokens *service.TokenManager, checkUser UserChecker, session service.SessionCookieConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, _, ok := bearerToken(c, tokens, session)
		if !ok {
			c.Next()
			return
		}
		current := CurrentUser{ID: claims.UserID, Role: claims.Role, Username: claims.Username}
		if checkUser != nil {
			if status, ok := checkUser(claims.UserID); ok {
				current.Role = status.Role
				current.Username = status.Username
				c.Set(ContextUserKey, current)
			}
			// 校验不通过：保持匿名，不设置当前用户
		} else {
			c.Set(ContextUserKey, current)
		}
		c.Next()
	}
}
