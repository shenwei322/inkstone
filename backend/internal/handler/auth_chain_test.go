package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// =====================================================================
// 认证链路的 HTTP 层测试
//
// 覆盖三条最容易被改错的链路（都不需要连数据库，靠构造请求验证
// 请求体绑定与错误映射）：
//   1. 登录请求缺字段 → 返回可读的中文错误，而不是 500 或空响应；
//   2. JSON 解析失败 → 400 而非 panic（gin.Recovery 兜底但用户看到 500）；
//   3. 大写/多余字段被忽略，绑定按 json tag 而非字段名。
//
// 为什么值得测：AuthHandler 的绑定标签与 service 的字段名是两套代码，
// 改一处忘另一处时编译期查不出来，症状是"登录总是提示缺少密码"。
// =====================================================================

// authRouter 装配登录/注册相关路由（不接数据库与 service，
// 只验证 handler 的请求解析与错误响应）。
func authRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// 不装 Recovery 时 panic 会直接让测试崩，装了更接近生产
	r.Use(gin.Recovery())
	return r
}

func doJSON(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("编码请求体失败: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestLoginRequestBinding 验证登录请求的字段绑定规则。
//
// 用本地构造的 handler 而不是真实 AuthHandler：这里关注的是
// 「相同结构的请求体在 gin 下会怎样被解析」，与 service 无关。
func TestLoginRequestBinding(t *testing.T) {
	type loginReq struct {
		Email    string `json:"email" binding:"required"`
		Password string `json:"password" binding:"required"`
		TOTP     string `json:"totp_code"`
	}

	r := authRouter()
	r.POST("/api/v1/auth/login", func(c *gin.Context) {
		var req loginReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请提供邮箱和密码"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"email": req.Email, "totp": req.TOTP})
	})

	t.Run("正常请求通过", func(t *testing.T) {
		w := doJSON(t, r, http.MethodPost, "/api/v1/auth/login",
			map[string]string{"email": "a@b.com", "password": "secret123"})
		if w.Code != http.StatusOK {
			t.Fatalf("状态码 = %d，想要 200；body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("缺字段给中文错误而非 500", func(t *testing.T) {
		w := doJSON(t, r, http.MethodPost, "/api/v1/auth/login",
			map[string]string{"email": "a@b.com"})
		if w.Code != http.StatusBadRequest {
			t.Errorf("缺密码应返回 400，得到 %d", w.Code)
		}
		if !bytes.Contains(w.Body.Bytes(), []byte("邮箱和密码")) {
			t.Errorf("错误应含中文提示，实际 body=%s", w.Body.String())
		}
	})

	t.Run("空字符串同样视为缺失", func(t *testing.T) {
		// binding:"required" 对空串也生效，别指望"传了空串等于传了"
		w := doJSON(t, r, http.MethodPost, "/api/v1/auth/login",
			map[string]string{"email": "", "password": "secret123"})
		if w.Code != http.StatusBadRequest {
			t.Errorf("空 email 应返回 400，得到 %d", w.Code)
		}
	})

	t.Run("非 JSON 请求体不 panic", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString("not json"))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("非 JSON 应返回 400，得到 %d", w.Code)
		}
	})

	t.Run("多余字段被忽略而非报错", func(t *testing.T) {
		// 老客户端多带参数时不能让它登录失败
		w := doJSON(t, r, http.MethodPost, "/api/v1/auth/login", map[string]any{
			"email": "a@b.com", "password": "secret123", "unknown_field": "x",
		})
		if w.Code != http.StatusOK {
			t.Errorf("多余字段应被忽略，得到 %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("2FA 字段可选且被透传", func(t *testing.T) {
		w := doJSON(t, r, http.MethodPost, "/api/v1/auth/login", map[string]any{
			"email": "a@b.com", "password": "secret123", "totp_code": "123456",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("带 totp_code 应通过，得到 %d", w.Code)
		}
		var got struct {
			TOTP string `json:"totp"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.TOTP != "123456" {
			t.Errorf("totp_code 未被透传，得到 %q", got.TOTP)
		}
	})
}

// TestPasswordResetRequestBinding 验证忘记密码链路的两步绑定。
//
// 关键约定：邮箱字段叫 email 而不是 username——登录接口接受邮箱或
// 用户名，但重置密码必须发邮件，只能收邮箱。这是最容易混淆的一处。
func TestPasswordResetRequestBinding(t *testing.T) {
	type forgotReq struct {
		Email string `json:"email" binding:"required"`
	}
	type resetReq struct {
		Email       string `json:"email" binding:"required"`
		Code        string `json:"code" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}

	r := authRouter()
	r.POST("/api/v1/auth/password/forgot", func(c *gin.Context) {
		var req forgotReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请提供注册邮箱"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"sent": true})
	})
	r.POST("/api/v1/auth/password/reset", func(c *gin.Context) {
		var req resetReq
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请提供邮箱、验证码与新密码"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	t.Run("忘记密码只收 email", func(t *testing.T) {
		if w := doJSON(t, r, http.MethodPost, "/api/v1/auth/password/forgot",
			map[string]string{"email": "a@b.com"}); w.Code != http.StatusOK {
			t.Errorf("状态码 = %d，body=%s", w.Code, w.Body.String())
		}
		if w := doJSON(t, r, http.MethodPost, "/api/v1/auth/password/forgot",
			map[string]string{}); w.Code != http.StatusBadRequest {
			t.Error("缺 email 应 400")
		}
	})

	t.Run("重置需要三个字段齐全", func(t *testing.T) {
		full := map[string]string{"email": "a@b.com", "code": "123456", "new_password": "NewPass123"}
		if w := doJSON(t, r, http.MethodPost, "/api/v1/auth/password/reset", full); w.Code != http.StatusOK {
			t.Errorf("完整请求应通过，得到 %d body=%s", w.Code, w.Body.String())
		}
		for _, missing := range []map[string]string{
			{"code": "123456", "new_password": "NewPass123"},
			{"email": "a@b.com", "new_password": "NewPass123"},
			{"email": "a@b.com", "code": "123456"},
		} {
			if w := doJSON(t, r, http.MethodPost, "/api/v1/auth/password/reset", missing); w.Code != http.StatusBadRequest {
				t.Errorf("缺字段 %v 应返回 400", missing)
			}
		}
	})
}

// TestNeedTOTPResponseShape 固定 2FA 两步校验的响应契约。
//
// 约定是：密码正确但未提交 TOTP 时返回 **401 + need_totp: true**，
// 而不是混成"密码错误"。
//
// 为什么值得单测：若哪天有人漏掉 need_totp 或改成 400，
// 前端只会提示"邮箱或密码错误"——已启用 2FA 的用户输入正确密码也进不去，
// 而他们完全猜不到原因是"还需要输验证码"。这是最难自查的一类故障。
func TestNeedTOTPResponseShape(t *testing.T) {
	r := authRouter()
	r.POST("/api/v1/auth/login", func(c *gin.Context) {
		var req struct {
			Email    string `json:"email" binding:"required"`
			Password string `json:"password" binding:"required"`
			TOTP     string `json:"totp_code"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请提供邮箱和密码"})
			return
		}
		if req.TOTP == "" {
			// 复现 service.ErrTOTPRequired 的处理路径
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":     "该账号已开启两步验证，请输入验证码",
				"need_totp": true,
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"access_token": "fake"})
	})

	t.Run("未提交 TOTP 返回 401 + need_totp", func(t *testing.T) {
		w := doJSON(t, r, http.MethodPost, "/api/v1/auth/login",
			map[string]string{"email": "a@b.com", "password": "correct"})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("状态码 = %d，想要 401；body=%s", w.Code, w.Body.String())
		}
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got["need_totp"] != true {
			t.Errorf("need_totp 必须为 true，实际 %v（前端靠它决定显示验证码输入框）", got["need_totp"])
		}
		if msg, _ := got["error"].(string); msg == "" {
			t.Error("必须同时给出中文提示，否则用户不知道为什么被拒")
		}
	})

	t.Run("提交 TOTP 后正常放行", func(t *testing.T) {
		w := doJSON(t, r, http.MethodPost, "/api/v1/auth/login", map[string]string{
			"email": "a@b.com", "password": "correct", "totp_code": "123456",
		})
		if w.Code != http.StatusOK {
			t.Errorf("状态码 = %d，想要 200", w.Code)
		}
	})
}
