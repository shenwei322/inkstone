package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/service"
)

// =====================================================================
// IP 访问记录 + 封禁拦截
//
// 两个中间件，职责刻意分开：
//   IPBanGuard   — 前置拦截：命中的 IP 直接 403，不再进入路由与业务。
//   IPAccessRecorder — 后置记录：c.Next() 之后拿到最终状态码再入队。
//
// 拦截必须注册在记录**之前**：否则被封的 IP 每次请求仍会写一条访问记录，
// 反而把它自己刷成排行榜第一，干扰运维判断。
// =====================================================================

// banCheckPathPrefixes 是被排除在封禁拦截之外的路径。
//
// 为什么 healthz 必须放过：容器编排 / 负载均衡的健康检查来自固定内网地址，
// 一旦这个地址被封（比如它扫过别的路径），探活全部失败 → 编排会不停重启实例，
// 形成「自己被自己封到起不来」的死循环。
var banCheckPathPrefixes = []string{"/healthz"}

// IPBanGuard 在封禁生效时直接拒绝请求。
//
// 响应刻意不含任何内部信息：只告诉对方「被拦了」和「还有多久」，
// 不透露触发阈值或封禁来源（那等于把绕过参数交给对方）。
func IPBanGuard(guard *service.IPGuardService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if guard == nil {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		for _, p := range banCheckPathPrefixes {
			if strings.HasPrefix(path, p) {
				c.Next()
				return
			}
		}

		ip := ClientIP(c)
		ban, banned := guard.Banned(ip)
		if !banned {
			c.Next()
			return
		}

		// 剩余秒数让前端/运维能判断「等一会儿还是换 IP」；
		// 永久封禁的 RemainingSeconds 返回 -1，这里归一化成 0，
		// 避免响应里出现负数（Retry-After 也不接受负数）。
		remaining := ban.RemainingSeconds(time.Now())
		if remaining < 0 {
			remaining = 0
		}
		c.Header("Retry-After", strconv.FormatInt(remaining, 10))
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error":            "该 IP 已被限制访问",
			"ip_banned":        true,
			"retry_after_secs": remaining,
		})
	}
}

// IPAccessRecorder 在请求完成后把访问明细推给 IPGuardService。
//
// 静态资源（/uploads /files）也记录：CC 攻击往往先打图片。
// 明细表不会被图片刷爆 —— repository.RecordAccess 按「IP + 路径 + 分钟桶」
// 合并累加，同一张图被打一万次只会让 hits 变大，不会多出一万行。
func IPAccessRecorder(guard *service.IPGuardService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if guard == nil {
			c.Next()
			return
		}
		c.Next()

		// 拿最终状态码：c.Next() 之后 Writer.Status() 才是真实值
		task := service.AccessTask{
			IP:        ClientIP(c),
			Method:    c.Request.Method,
			Path:      c.Request.URL.Path,
			Status:    c.Writer.Status(),
			UserAgent: c.Request.UserAgent(),
			Referer:   c.Request.Referer(),
			At:        time.Now(),
		}
		if user, ok := GetCurrentUser(c); ok {
			task.UserID = user.ID
			task.Username = user.Username
		}
		guard.Observe(task)
	}
}
