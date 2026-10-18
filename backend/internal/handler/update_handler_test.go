package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 手动更新模式（UPDATE_MANUAL_ONLY，默认开启）下，后台不得执行任何更新动作。
//
// 这里故意把 updates 留成 nil：拒绝必须发生在任何 UpdateService 调用之前。
// 一旦有人把前置检查挪到后面（或误以为「先下载再判断」更友好），
// 这两个用例会直接 panic，而不是悄悄把源码替换掉。
func TestUpdateHandlerManualOnlyRejectsApply(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &UpdateHandler{manualOnly: true}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update/apply",
		strings.NewReader(`{"commit":"abc1234","target":"v1.28.0","confirm":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Apply(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("apply 应返回 403，实际 %d：%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "手动更新") {
		t.Errorf("拒绝信息应引导用户手动更新，实际：%s", w.Body.String())
	}
}

func TestUpdateHandlerManualOnlyRejectsRollback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &UpdateHandler{manualOnly: true}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update/rollback",
		strings.NewReader(`{"backup_id":"20261018-090000","confirm":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.Rollback(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("rollback 应返回 403，实际 %d：%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "手动") {
		t.Errorf("拒绝信息应说明改为手动操作，实际：%s", w.Body.String())
	}
}
