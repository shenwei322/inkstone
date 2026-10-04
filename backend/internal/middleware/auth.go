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

// Auth requires a valid access token. When checkUser is provided, the user is
// also verified against the database so banned/deleted accounts are rejected
// even while their access token is still unexpired.
func Auth(tokens *service.TokenManager, checkUser UserChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization header format"})
			return
		}

		claims, err := tokens.Parse(parts[1], service.TokenTypeAccess)
		if err != nil {
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
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.Next()
			return
		}
		parts := strings.SplitN(header, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != "" {
			if claims, err := tokens.Parse(parts[1], service.TokenTypeAccess); err == nil {
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
			}
		}
		c.Next()
	}
}
