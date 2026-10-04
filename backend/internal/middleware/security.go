package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ---------- 滑动窗口限流器 ----------

// Context keys set by the rate-limit middleware.
const (
	ContextRateKey   = "rateLimitKey"
	ContextRateLimit = "rateLimitMax"
)

type windowEntry struct {
	hits []time.Time
}

type SlidingLimiter struct {
	mu      sync.Mutex
	entries map[string]*windowEntry
}

func NewSlidingLimiter() *SlidingLimiter {
	l := &SlidingLimiter{entries: make(map[string]*windowEntry)}
	go l.cleanupLoop()
	return l
}

// Allow records a hit for key and reports whether it stays within limit.
func (l *SlidingLimiter) Allow(key string, limit int, window time.Duration) bool {
	if limit <= 0 {
		return true
	}
	now := time.Now()
	cutoff := now.Add(-window)

	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entries[key]
	if !ok {
		e = &windowEntry{}
		l.entries[key] = e
	}
	// drop expired hits
	kept := e.hits[:0]
	for _, t := range e.hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	e.hits = kept

	if len(e.hits) >= limit {
		return false
	}
	e.hits = append(e.hits, now)
	return true
}

// Remaining returns how many hits are left in the current window.
func (l *SlidingLimiter) Remaining(key string, limit int, window time.Duration) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-window)
	e, ok := l.entries[key]
	if !ok {
		return limit
	}
	count := 0
	for _, t := range e.hits {
		if t.After(cutoff) {
			count++
		}
	}
	if count >= limit {
		return 0
	}
	return limit - count
}

// Reset clears the counters for a key (e.g. after a successful login).
func (l *SlidingLimiter) Reset(key string) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

func (l *SlidingLimiter) cleanupLoop() {
	for range time.Tick(10 * time.Minute) {
		cutoff := time.Now().Add(-2 * time.Hour)
		l.mu.Lock()
		for k, e := range l.entries {
			latest := time.Time{}
			for _, t := range e.hits {
				if t.After(latest) {
					latest = t
				}
			}
			if latest.Before(cutoff) {
				delete(l.entries, k)
			}
		}
		l.mu.Unlock()
	}
}

// ---------- 中间件 ----------

// 访客 IP 解析见 clientip.go（ClientIP / RemoteIP / SetTrustedProxies）。
// 旧实现无条件采信客户端自带的 X-Forwarded-For，可被逐请求伪造以作废全部
// IP 限流；现已改为「仅当直连对端属于可信代理网段时才读转发头」。

// RateLimitConfig describes a per-IP request budget.
type RateLimitConfig struct {
	Limiter *SlidingLimiter
	LimitFn func() int // dynamic limit (admin configurable)
	Window  time.Duration
	Message string
}

// IPRateLimit aborts with 429 when the caller exceeds the configured budget.
func IPRateLimit(cfg RateLimitConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := cfg.LimitFn()
		if limit <= 0 {
			c.Next()
			return
		}
		key := cfg.Message + ":" + ClientIP(c)
		// 暴露 key，便于处理成功后重置计数（例如登录成功）
		c.Set(ContextRateKey, key)
		c.Set(ContextRateLimit, limit)
		if !cfg.Limiter.Allow(key, limit, cfg.Window) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "操作过于频繁，请稍后再试",
			})
			return
		}
		c.Next()
	}
}

// RateKey returns the rate-limit key stored by IPRateLimit, if any.
func RateKey(c *gin.Context) string {
	if v, ok := c.Get(ContextRateKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// SecurityHeaders adds baseline hardening headers to every response.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-XSS-Protection", "1; mode=block")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		// HSTS：生产经 Nginx 443 代理；本地 http 开发时浏览器忽略该头
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Next()
	}
}
