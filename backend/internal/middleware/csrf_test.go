package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// =====================================================================
// CSRF 判定测试
//
// 这些用例锁定的都是「改一行就静默失守」的判定分支：CSRF 中间件写错时
// 不会有任何显式报错，只会表现为「要么攻击能过，要么正常用户被拒」。
// 因此每个分支都必须有一条断言钉住。
// =====================================================================

func csrfRouter(origins ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CSRFGuard(origins))
	r.POST("/api/v1/articles/1/comments", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})
	r.GET("/api/v1/articles", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func doCSRF(r *gin.Engine, method, path string, mutate func(*http.Request)) int {
	req := httptest.NewRequest(method, path, nil)
	if mutate != nil {
		mutate(req)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func withCookie(name string) func(*http.Request) {
	return func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: name, Value: "token"})
	}
}

func TestCSRFGuard(t *testing.T) {
	const session = "inkstone_session"

	t.Run("跨站标记一律拒绝", func(t *testing.T) {
		r := csrfRouter("https://blog.example.com")
		code := doCSRF(r, http.MethodPost, "/api/v1/articles/1/comments", func(req *http.Request) {
			withCookie(session)(req)
			// 即使 Origin 伪装成白名单，Sec-Fetch-Site 也会暴露跨站事实。
			// 这一条正是「只看 Origin」写法会漏掉的攻击路径。
			req.Header.Set("Origin", "https://blog.example.com")
			req.Header.Set("Sec-Fetch-Site", "cross-site")
		})
		if code != http.StatusForbidden {
			t.Errorf("Sec-Fetch-Site=cross-site 的写请求应被拒绝，得到 %d", code)
		}
	})

	t.Run("白名单来源放行", func(t *testing.T) {
		r := csrfRouter("https://blog.example.com")
		code := doCSRF(r, http.MethodPost, "/api/v1/articles/1/comments", func(req *http.Request) {
			withCookie(session)(req)
			req.Header.Set("Origin", "https://blog.example.com")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		})
		if code != http.StatusCreated {
			t.Errorf("白名单来源应放行，得到 %d", code)
		}
	})

	t.Run("未知来源拒绝", func(t *testing.T) {
		r := csrfRouter("https://blog.example.com")
		code := doCSRF(r, http.MethodPost, "/api/v1/articles/1/comments", func(req *http.Request) {
			withCookie(session)(req)
			req.Header.Set("Origin", "https://evil.example.net")
		})
		if code != http.StatusForbidden {
			t.Errorf("非白名单来源应被拒绝，得到 %d", code)
		}
	})

	t.Run("没有会话 Cookie 时不拦", func(t *testing.T) {
		// 非浏览器客户端（脚本/CLI）不携带 Cookie，也就没有 CSRF 可言。
		// 若这里拦截，会把所有 API 客户端一起挡掉。
		r := csrfRouter("https://blog.example.com")
		code := doCSRF(r, http.MethodPost, "/api/v1/articles/1/comments", func(req *http.Request) {
			req.Header.Set("Origin", "https://evil.example.net")
		})
		if code != http.StatusCreated {
			t.Errorf("无会话 Cookie 的请求不应被 CSRF 拦截，得到 %d", code)
		}
	})

	t.Run("GET 不拦", func(t *testing.T) {
		r := csrfRouter("https://blog.example.com")
		code := doCSRF(r, http.MethodGet, "/api/v1/articles", func(req *http.Request) {
			withCookie(session)(req)
			req.Header.Set("Origin", "https://evil.example.net")
		})
		if code != http.StatusOK {
			t.Errorf("安全方法不应被拦截，得到 %d", code)
		}
	})

	t.Run("仅 Referer 时按同源判定", func(t *testing.T) {
		r := csrfRouter("https://blog.example.com")
		code := doCSRF(r, http.MethodPost, "/api/v1/articles/1/comments", func(req *http.Request) {
			withCookie(session)(req)
			req.Header.Set("Referer", "https://evil.example.net/page")
		})
		if code != http.StatusForbidden {
			t.Errorf("Referer 指向站外应被拒绝，得到 %d", code)
		}

		code = doCSRF(r, http.MethodPost, "/api/v1/articles/1/comments", func(req *http.Request) {
			withCookie(session)(req)
			req.Header.Set("Referer", "https://blog.example.com/posts/1")
		})
		if code != http.StatusCreated {
			t.Errorf("Referer 指向站内应放行，得到 %d", code)
		}
	})

	t.Run("__Host- 前缀的会话 Cookie 同样被识别", func(t *testing.T) {
		// 生产会话名带 __Host- 前缀。若匹配逻辑只认不带前缀的名字，
		// 生产环境会静默不设防——这正是这条用例存在的理由。
		r := csrfRouter("https://blog.example.com")
		code := doCSRF(r, http.MethodPost, "/api/v1/articles/1/comments", func(req *http.Request) {
			withCookie("__Host-inkstone_session")(req)
			req.Header.Set("Origin", "https://evil.example.net")
		})
		if code != http.StatusForbidden {
			t.Errorf("带前缀的会话 Cookie 未被识别，得到 %d", code)
		}
	})
}

func TestOriginOf(t *testing.T) {
	cases := map[string]string{
		"https://a.example.com/p/1?q=2":  "https://a.example.com",
		"https://a.example.com":          "https://a.example.com",
		"http://localhost:3000/admin":    "http://localhost:3000",
		"javascript:alert(1)":            "",
		"":                               "",
		"https://user:pw@a.example.com/": "https://user:pw@a.example.com",
	}
	for in, want := range cases {
		if got := originOf(in); got != want {
			t.Errorf("originOf(%q) = %q，想要 %q", in, got, want)
		}
	}
}

// =====================================================================
// 以下三组用例对应一次代码审查中实测出的缺陷，各自锁定一个真实故障。
// 它们的共同点是「坏掉时不会有任何报错」：要么放过了攻击，要么拒掉了
// 正常用户，都只能靠断言发现。
// =====================================================================

// TestSessionBuildingPathsExempt 锁定「登录不该被自己的 CSRF 拦掉」。
//
// 实测过的故障：用户在会话过期后打开登录页，浏览器仍带着那枚旧 Cookie，
// 登录请求被 CSRF 判成「可疑跨站写请求」返回 403。
// 用户看到的是「密码正确但登录失败」，且清了缓存才会恢复 —— 极难自查。
//
// 这里特意用「非白名单 Origin + 带会话 Cookie」的最坏组合，
// 确保豁免来自路径判断而不是碰巧没触发校验。
func TestSessionBuildingPathsExempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	paths := []string{
		"/api/v1/auth/login",
		"/api/v1/auth/register",
		"/api/v1/auth/refresh",
		"/api/v1/auth/password/forgot",
		"/api/v1/auth/password/reset",
		"/api/v1/auth/email-code",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			r := gin.New()
			r.Use(CSRFGuard([]string{"https://blog.example.com"}))
			r.POST(p, func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

			code := doCSRF(r, http.MethodPost, p, func(req *http.Request) {
				// 最坏组合：带残留会话 Cookie，且来源不在白名单。
				withCookie("inkstone_session")(req)
				req.Header.Set("Origin", "https://stale.example.net")
			})
			if code != http.StatusOK {
				t.Errorf("%s 应豁免 CSRF（否则用户会话过期后无法重新登录），得到 %d", p, code)
			}
		})
	}
}

// TestNonSessionBuildingPathNotExempt 确认豁免没有扩大化。
//
// 前缀匹配式的实现会让 /api/v1/auth/login-history 这类只读接口
// 意外获得豁免。这里用同一前缀下的假路径确认它仍然受保护。
func TestNonSessionBuildingPathNotExempt(t *testing.T) {
	r := csrfRouter("https://blog.example.com")
	code := doCSRF(r, http.MethodPost, "/api/v1/articles/1/comments", func(req *http.Request) {
		withCookie("inkstone_session")(req)
		req.Header.Set("Origin", "https://evil.example.net")
	})
	if code != http.StatusForbidden {
		t.Errorf("普通写端点仍必须受 CSRF 保护，得到 %d", code)
	}
}

// TestSameSiteIsDenied 锁定子域攻击面。
//
// same-site 表示「同站但不同源」，即**任意子域**。攻击者控制
// evil.example.com 时，可以以 same-site 身份向 blog.example.com
// 发起写请求，Cookie 照样会被带上。只拦 cross-site 会漏掉这条路。
func TestSameSiteIsDenied(t *testing.T) {
	r := csrfRouter("https://blog.example.com")
	code := doCSRF(r, http.MethodPost, "/api/v1/articles/1/comments", func(req *http.Request) {
		withCookie("inkstone_session")(req)
		// 刻意不给 Origin：这正是实测中绕过成功的组合。
		req.Header.Set("Sec-Fetch-Site", "same-site")
	})
	if code != http.StatusForbidden {
		t.Errorf("Sec-Fetch-Site=same-site 必须拒绝（子域可借此发起 CSRF），得到 %d", code)
	}
}

// TestNoOriginNoRefererWithCookieIsDenied 锁定「来源信息全缺」的可疑请求。
//
// 现代浏览器在所有写请求上都会带 Origin 或 Sec-Fetch-Site，因此
// 「带会话 Cookie 却什么来源信息都没有」极不寻常。
// 注意这只影响**带 Cookie**的请求：纯 API 客户端不带 Cookie，
// 在上游分支就已放行（见 TestNoCookieClientStillWorks）。
func TestNoOriginNoRefererWithCookieIsDenied(t *testing.T) {
	r := csrfRouter("https://blog.example.com")
	code := doCSRF(r, http.MethodPost, "/api/v1/articles/1/comments", func(req *http.Request) {
		withCookie("inkstone_session")(req)
	})
	if code != http.StatusForbidden {
		t.Errorf("带会话 Cookie 但无任何来源信息应按可疑拒绝，得到 %d", code)
	}
}

// TestNoCookieClientStillWorks 确认纯 API 客户端不受影响。
//
// 这是上一条的配套护栏：若为了收紧而把所有无来源头的请求都拦掉，
// 会把 curl、移动端原生调用全部挡死。区分点是「有没有带会话 Cookie」，
// 不是「有没有带 Origin」。
func TestNoCookieClientStillWorks(t *testing.T) {
	r := csrfRouter("https://blog.example.com")
	for _, mutate := range []func(*http.Request){
		nil, // 完全裸请求
		func(req *http.Request) { req.Header.Set("Origin", "https://evil.example.net") },
		func(req *http.Request) { req.Header.Set("Sec-Fetch-Site", "cross-site") },
	} {
		code := doCSRF(r, http.MethodPost, "/api/v1/articles/1/comments", mutate)
		if code != http.StatusCreated {
			t.Errorf("不带会话 Cookie 的客户端不应被 CSRF 拦截，得到 %d", code)
		}
	}
}

// TestHasSessionCookieMatchesExactNames 确认 Cookie 名按精确相等判断。
//
// 子串匹配会把 xinkstone_session 这类服务端根本不读的 Cookie 也算成会话，
// 结果是这些无关 Cookie 一出现，正常请求就被拒。
func TestHasSessionCookieMatchesExactNames(t *testing.T) {
	for _, ck := range []string{"__Host-inkstone_session", "inkstone_session"} {
		code := doCSRF(csrfRouter("https://blog.example.com"), http.MethodPost,
			"/api/v1/articles/1/comments", func(req *http.Request) {
				withCookie(ck)(req)
				req.Header.Set("Origin", "https://evil.example.net")
			})
		if code != http.StatusForbidden {
			t.Errorf("会话 Cookie %q 未被识别为会话，得到 %d", ck, code)
		}
	}

	// 名字只是包含子串的 Cookie 不是会话，不应触发拦截。
	code := doCSRF(csrfRouter("https://blog.example.com"), http.MethodPost,
		"/api/v1/articles/1/comments", func(req *http.Request) {
			withCookie("xinkstone_session")(req)
			req.Header.Set("Origin", "https://evil.example.net")
		})
	if code != http.StatusCreated {
		t.Errorf("非会话 Cookie 不应触发 CSRF 拦截，得到 %d", code)
	}
}
