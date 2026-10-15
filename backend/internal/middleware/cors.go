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
			// 白名单必须覆盖前端实际会发的所有请求头：漏一个就会在预检阶段
			// 被浏览器拦下，且错误只出现在预检响应里，排查起来很费劲。
			// X-Requested-With 是 axios / jQuery 类客户端默认会加的。
			c.Header("Access-Control-Allow-Headers",
				"Authorization, Content-Type, Accept, Origin, X-Requested-With, X-CSRF-Token")
			// 会话 Cookie 的失效需要前端能看到响应头里的信息时，这里必须
			// 显式暴露——默认 CORS 只放行少数几个简单响应头。
			c.Header("Access-Control-Expose-Headers", "Content-Disposition")
			c.Header("Access-Control-Max-Age", "86400")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
