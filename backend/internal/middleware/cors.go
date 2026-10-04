package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowAll := len(allowedOrigins) == 0

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allowed := allowAll
		if !allowAll && origin != "" {
			for _, o := range allowedOrigins {
				if strings.EqualFold(strings.TrimSpace(o), origin) {
					allowed = true
					break
				}
			}
		}

		// 响应内容随 Origin 变化，必须声明 Vary —— 否则共享缓存/中间层可能
		// 把 A 源的 Access-Control-Allow-Origin 复用到 B 源（配合下方
		// Allow-Credentials: true 即为缓存投毒面）。
		c.Header("Vary", "Origin")

		if allowed && origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
			c.Header("Access-Control-Max-Age", "86400")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
