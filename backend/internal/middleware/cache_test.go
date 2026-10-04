package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// TestCachePolicy 守住"默认不缓存 + 白名单长缓存"的策略。
//
// 最容易回归的地方是"新增接口忘了配置"——那种情况必须是 no-cache
// （多走一次后端），绝不能是 public（用户看到别人的数据）。
func TestCachePolicy(t *testing.T) {
	newRouter := func() *gin.Engine {
		r := gin.New()
		r.Use(CachePolicy())
		r.GET("/uploads/:name", func(c *gin.Context) {
			c.String(http.StatusOK, "img")
		})
		r.GET("/api/v1/articles", func(c *gin.Context) {
			c.String(http.StatusOK, "[]")
		})
		r.GET("/api/v1/site-config", func(c *gin.Context) {
			c.String(http.StatusOK, "{}")
		})
		r.GET("/api/v1/auth/me", func(c *gin.Context) {
			c.String(http.StatusOK, "{}")
		})
		r.GET("/healthz", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})
		return r
	}

	cases := []struct {
		name       string
		method     string
		path       string
		status     int
		wantPolicy string
	}{
		{"上传文件长缓存", http.MethodGet, "/uploads/a.png", 200, "public, max-age=31536000, immutable"},
		{"文章列表短缓存+后台刷新", http.MethodGet, "/api/v1/articles", 200, "public, max-age=60, stale-while-revalidate=300"},
		{"站点配置短缓存", http.MethodGet, "/api/v1/site-config", 200, "public, max-age=60, stale-while-revalidate=300"},
		{"当前用户资料绝不缓存", http.MethodGet, "/api/v1/auth/me", 200, "private, no-cache"},
		{"健康检查走默认私有", http.MethodGet, "/healthz", 200, "private, no-cache"},
		{"写操作响应不缓存", http.MethodPost, "/api/v1/articles", 200, "no-store"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRouter()
			w := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			r.ServeHTTP(w, req)

			got := w.Header().Get("Cache-Control")
			if got != tc.wantPolicy {
				t.Errorf("%s %s => Cache-Control = %q，想要 %q", tc.method, tc.path, got, tc.wantPolicy)
			}
		})
	}
}

// TestCachePolicyErrorResponses 验证 4xx/5xx 一律 no-store。
// 把一次 401 缓存住意味着：用户密码输错后，过一会儿改对了仍然看到 401。
func TestCachePolicyErrorResponses(t *testing.T) {
	r := gin.New()
	r.Use(CachePolicy())
	r.GET("/api/v1/auth/me", func(c *gin.Context) {
		c.Status(http.StatusUnauthorized)
	})
	r.GET("/api/v1/articles", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	for _, path := range []string{"/api/v1/auth/me", "/api/v1/articles"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s 的错误响应 Cache-Control = %q，想要 no-store", path, got)
		}
	}
}

// TestCachePolicyRespectsExistingHeader 验证 handler 已设置的头不被覆盖。
// 文件下载接口可能自己带 ETag + Cache-Control，中间件不该插手。
func TestCachePolicyRespectsExistingHeader(t *testing.T) {
	r := gin.New()
	r.Use(CachePolicy())
	r.GET("/api/v1/files/download", func(c *gin.Context) {
		c.Header("Cache-Control", "private, max-age=60")
		c.String(http.StatusOK, "binary")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/files/download", nil))
	if got := w.Header().Get("Cache-Control"); got != "private, max-age=60" {
		t.Errorf("已有的 Cache-Control 被覆盖成 %q", got)
	}
}
