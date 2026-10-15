package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/service"
)

// =====================================================================
// 刷新失败的**状态码契约**
//
// 这一组用例守的是前端续期逻辑的判据：前端只有在拿到 401/403 时才认定
// 「会话真的没了」并清空登录态；拿到 5xx 会当作服务端临时故障保留登录态。
// 因此状态码本身是接口契约的一部分：
//
//   - 令牌无效/账号消失 → 401（不可恢复，前端该登出）；
//   - 数据库故障        → 500（可恢复，前端该保留登录态重试）。
//
// 曾经的实现把「令牌无效」用 errors.New 造了一个匿名错误，落进 errorResponse
// 的 default 分支返回 500。后果不是「状态码难看」，而是前端永远保留着一个
// 注定失败的登录态、每次访问都重试一次无效刷新——用户看到的正是
// 「每次访问都要重新登录」。所以这个契约必须有测试钉住。
// =====================================================================

func refreshRouter(err error) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	// 直接复用生产错误映射，避免测试里另写一份分支而失去意义。
	r.POST("/api/v1/auth/refresh", func(c *gin.Context) {
		errorResponse(c, err)
	})
	return r
}

func callRefresh(t *testing.T, err error) int {
	t.Helper()
	r := refreshRouter(err)
	body, _ := json.Marshal(map[string]string{"refresh_token": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestRefreshErrorStatusCodeContract(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"令牌无效必须是 401", service.ErrInvalidRefreshToken, http.StatusUnauthorized},
		{"账号已删除必须是 401", service.ErrUserVanished, http.StatusUnauthorized},
		{"令牌被撤销必须是 401", service.ErrTokenRevoked, http.StatusUnauthorized},
		{"账号被封禁必须是 403", service.ErrUserBanned, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := callRefresh(t, tc.err); got != tc.want {
				t.Errorf("状态码 = %d，想要 %d", got, tc.want)
			}
		})
	}
}

// TestRefreshErrorCarriesChineseMessage 验证失败响应带可读的中文提示。
//
// 前端会把 message 直接显示给用户。返回 "internal server error" 之类的
// 英文内部信息时，用户只知道「出错了」，不知道「该重新登录」，于是反复
// 刷新页面以为能恢复。
func TestRefreshErrorCarriesChineseMessage(t *testing.T) {
	r := refreshRouter(service.ErrInvalidRefreshToken)
	body, _ := json.Marshal(map[string]string{"refresh_token": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	msg, _ := got["error"].(string)
	if msg == "" {
		t.Fatal("响应缺少 error 字段")
	}
	if msg == "internal server error" {
		t.Error("不应把内部错误信息返回给用户")
	}
}
