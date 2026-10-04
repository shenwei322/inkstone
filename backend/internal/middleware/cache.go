package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CachePolicy 按路径与请求方式给响应加 Cache-Control。
//
// 为什么需要：此前全站没有任何 Cache-Control 头。后果有两端——
//   - 不带该头时，中间层（Nginx proxy_cache / CDN / 浏览器）自行决定缓存行为，
//     有的会把 `/api/v1/auth/me` 连 token 一起缓存，换个账号登录看到上一个人的资料；
//   - 真正可以长缓存的静态资源（/uploads 下的图片）反而每次回源，
//     首页首图反复从后端拉，白占带宽。
//
// 采取「默认不缓存 + 白名单长缓存」而不是反过来：安全默认可预期，
// 新增接口忘记配置时是"多走一次后端"，不会是"用户看到别人的数据"。
func CachePolicy() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		h := c.Writer.Header()
		if h.Get("Cache-Control") != "" {
			// handler 已显式设置过（比如文件下载带 ETag），不覆盖
			return
		}
		h.Set("Cache-Control", policyFor(c.Request.Method, c.FullPath(), c.Writer.Status()))
	}
}

// policyFor 返回该请求适用的 Cache-Control 值。
func policyFor(method, path string, status int) string {
	if status >= 400 {
		// 错误响应绝不缓存：把一次 401/500 缓存住，用户在修好之后仍然看到它。
		return "no-store"
	}
	if method != http.MethodGet && method != http.MethodHead {
		// 写操作的响应缓存会让"看起来保存成功"掩盖真正的失败；
		// 即使是 CREATE/PUT，也一律当作不可重用。
		return "no-store"
	}

	// 上传的静态文件：文件名带上传时间戳或哈希，内容不会原地变，
	// 可以长缓存。这里不要把 /uploads 之外的内容也长缓存——
	// 文章正文改了 URL 不变，长缓存会让读者一直看旧文。
	if strings.HasPrefix(path, "/uploads/") {
		return "public, max-age=31536000, immutable"
	}

	// 站点级公开内容（文章、分类、RSS、sitemap）：短缓存 + stale-while-revalidate。
	// max-age=60 让短时间内重复访问命中缓存；stale-while-revalidate 让
	// CDN 在后台异步取新内容，用户不等待。news sitemap 之类要求新鲜度高的
	// 场景，60 秒的滞后在可接受范围内。
	switch {
	case strings.HasPrefix(path, "/api/v1/articles"),
		strings.HasPrefix(path, "/api/v1/categories"),
		strings.HasPrefix(path, "/api/v1/tags"),
		strings.HasPrefix(path, "/api/v1/feed.xml"),
		strings.HasPrefix(path, "/api/v1/sitemap"),
		strings.HasPrefix(path, "/api/v1/site-config"):
		return "public, max-age=60, stale-while-revalidate=300"
	}

	// 其余一律私有且不缓存：/auth/me、/admin/**、/articles（带 token 的草稿列表）
	// 都可能因用户而异。no-cache 而非 no-store：允许协商缓存（ETag），
	// 但每次必须回源确认，不会把用户 A 的响应喂给用户 B。
	return "private, no-cache"
}
