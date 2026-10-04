package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestRoutesEndpointShape 验证「路由清单」端点的响应形态。
//
// 它存在的意义：手写 OpenAPI 注释的问题是改代码忘改注释，文档慢慢失真。
// 从 gin 的路由树（Engine.Routes()）实时导出则不可能与实现脱节——
// 注册即出现，删除即消失。
//
// 这里只测它确实挂在 engine 上、且形状符合预期（数组 + 三个字段），
// 不验证每一条路由（那等于把 main.go 抄一遍）。
func TestRoutesEndpointShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/admin/system/routes", ListRoutes(r))

	// 故意多注册一条，确认清单真的来自路由树而不是硬编码
	r.GET("/api/v1/articles", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/routes", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，body=%s", w.Code, w.Body.String())
	}
	var got struct {
		Routes []struct {
			Method  string `json:"method"`
			Path    string `json:"path"`
			Handler string `json:"handler"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应: %v；body=%s", err, w.Body.String())
	}

	var sawArticles bool
	for _, rt := range got.Routes {
		if rt.Method == "" || rt.Path == "" {
			t.Errorf("路由条目缺字段：%+v", rt)
		}
		if rt.Path == "/api/v1/articles" && rt.Method == http.MethodGet {
			sawArticles = true
		}
	}
	if !sawArticles {
		t.Error("清单里没有测试注册的 /api/v1/articles——说明不是从路由树取的")
	}
}
