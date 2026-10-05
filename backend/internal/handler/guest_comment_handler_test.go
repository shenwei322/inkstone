package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/middleware"
	"github.com/shenwei/inkstone/backend/internal/model"
)

// =====================================================================
// 游客评论的身份与响应脱敏
//
// 三条最容易出事的链路（都不连数据库，直接读响应体断言）：
//   1. 登录用户提交 guest_* 字段时，不能把评论伪装成游客留言；
//   2. 公开接口的响应里绝不能出现游客邮箱；
//   3. 游客评论的 author.id 是 0，前端要靠 is_guest 而不是 id 来区分。
// =====================================================================

// TestCommentResponseHidesGuestEmail 锁定公开出口不含游客邮箱。
//
// 邮箱是访客的隐私信息，只允许出现在后台审核视图。若哪天有人把
// toCommentResponse 也改成带邮箱，这条测试会失败。
func TestCommentResponseHidesGuestEmail(t *testing.T) {
	const secretEmail = "guest-secret@example.com"
	comment := &model.Comment{
		ID:         1,
		ArticleID:  10,
		GuestName:  "路过的小明",
		GuestEmail: secretEmail,
		GuestURL:   "https://example.com",
		Content:    "写得好",
		Status:     model.CommentApproved,
		IP:         "203.0.113.7",
	}

	public := toCommentResponse(comment)
	blob, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("序列化公开响应失败：%v", err)
	}
	if bytes.Contains(blob, []byte(secretEmail)) {
		t.Error("公开响应里出现了游客邮箱")
	}
	if bytes.Contains(blob, []byte("203.0.113.7")) {
		t.Error("公开响应里出现了游客 IP")
	}

	// 后台视图是唯一允许带邮箱的出口
	adminBlob, err := json.Marshal(toCommentResponseAdmin(comment))
	if err != nil {
		t.Fatalf("序列化后台响应失败：%v", err)
	}
	if !bytes.Contains(adminBlob, []byte(secretEmail)) {
		t.Error("后台审核视图应包含游客邮箱（审核时可能需要联系本人）")
	}

	// 展示名与游客标记：前端靠这两个字段渲染
	author, ok := public["author"].(gin.H)
	if !ok {
		t.Fatalf("author 字段类型异常：%#v", public["author"])
	}
	if author["username"] != "路过的小明" {
		t.Errorf("游客展示名应为昵称，实际 %v", author["username"])
	}
	if author["is_guest"] != true {
		t.Error("游客评论的 is_guest 应为 true（前端不能靠 id==0 判断）")
	}
	if author["url"] != "https://example.com" {
		t.Errorf("游客填写的网站应下发，实际 %v", author["url"])
	}
}

// TestCommentResponseLoggedInUser 对照用例：登录用户的展示名取用户名，
// 且不带游客标记与网站字段。
func TestCommentResponseLoggedInUser(t *testing.T) {
	uid := uint(9)
	comment := &model.Comment{
		ID:        2,
		ArticleID: 10,
		UserID:    &uid,
		User:      &model.User{ID: uid, Username: "alice"},
		Content:   "第一条",
		Status:    model.CommentApproved,
	}

	public := toCommentResponse(comment)
	author, ok := public["author"].(gin.H)
	if !ok {
		t.Fatalf("author 字段类型异常：%#v", public["author"])
	}
	if author["username"] != "alice" {
		t.Errorf("展示名应为用户名，实际 %v", author["username"])
	}
	if author["is_guest"] != false {
		t.Error("登录用户评论的 is_guest 应为 false")
	}
	if _, hasURL := author["url"]; hasURL {
		t.Error("登录用户不应带游客网站字段")
	}
	if author["id"] != uid {
		t.Errorf("作者 ID 应为 %d，实际 %v", uid, author["id"])
	}
}

// createCommentRequest 的游客字段绑定：确认 JSON tag 正确。
//
// 绑定标签与前端提交的字段名是两套代码，改一处忘另一处编译期查不出来，
// 症状是"游客明明填了昵称，后端却说昵称为空"。
func TestCreateCommentRequestGuestBinding(t *testing.T) {
	body := []byte(`{
		"content": "写得好",
		"parent_id": 3,
		"guest_name": "路过的小明",
		"guest_email": "ming@example.com",
		"guest_url": "https://example.com/blog"
	}`)

	r := gin.New()
	r.POST("/comments", func(c *gin.Context) {
		var req createCommentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"content":     req.Content,
			"parent_id":   req.ParentID,
			"guest_name":  req.GuestName,
			"guest_email": req.GuestEmail,
			"guest_url":   req.GuestURL,
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/comments", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，body=%s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败：%v", err)
	}
	if got["guest_name"] != "路过的小明" {
		t.Errorf("guest_name 绑定错误：%v", got["guest_name"])
	}
	if got["guest_email"] != "ming@example.com" {
		t.Errorf("guest_email 绑定错误：%v", got["guest_email"])
	}
	if got["guest_url"] != "https://example.com/blog" {
		t.Errorf("guest_url 绑定错误：%v", got["guest_url"])
	}
	if got["parent_id"] != float64(3) {
		t.Errorf("parent_id 绑定错误：%v", got["parent_id"])
	}
}

// resolveIdentity 是游客/登录身份的分岔口，这里验证它的选择逻辑。
//
// 用真实的 CommentHandler 实例，通过 gin.Context 注入"当前用户"来模拟
// 中间件已解析出登录态——不需要 tokens/limiter 等其余依赖。
func TestResolveIdentityPrefersLoggedInUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name        string
		loggedInID  uint // 0 表示未登录
		guestOn     bool
		wantGuest   bool
		wantStatus  int
		description string
	}{
		{
			name:        "登录用户忽略游客字段",
			loggedInID:  42,
			guestOn:     true,
			wantGuest:   false,
			wantStatus:  http.StatusOK,
			description: "登录用户即便提交 guest_name 也必须按账号身份记录",
		},
		{
			name:        "未登录且开关关闭",
			loggedInID:  0,
			guestOn:     false,
			wantStatus:  http.StatusUnauthorized,
			description: "默认配置下未登录不能评论",
		},
		{
			name:        "未登录且开关开启",
			loggedInID:  0,
			guestOn:     true,
			wantGuest:   true,
			wantStatus:  http.StatusOK,
			description: "开启 guest_comment 后游客可以评论",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &CommentHandler{}
			// 注入固定的开关值：生产走 captcha 持有的设置项，测试只需
			// 覆盖"开"与"关"两条分支
			h.guestEnabled = func() bool { return tc.guestOn }

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/articles/1/comments", nil)
			if tc.loggedInID != 0 {
				c.Set(middleware.ContextUserKey, middleware.CurrentUser{
					ID:       tc.loggedInID,
					Role:     model.RoleUser,
					Username: "alice",
				})
			}

			req := &createCommentRequest{
				Content:   "测试评论",
				GuestName: "伪装者",
				GuestURL:  "https://evil.example.com",
			}
			identity, ok := h.resolveIdentity(c, req)

			if tc.wantStatus == http.StatusUnauthorized {
				if ok {
					t.Fatal("未登录且开关关闭时应拒绝")
				}
				if w.Code != http.StatusUnauthorized {
					t.Errorf("状态码应为 401，实际 %d", w.Code)
				}
				return
			}

			if !ok {
				t.Fatalf("应放行，实际被拒绝：%s", w.Body.String())
			}
			if tc.wantGuest {
				if identity.UserID != nil {
					t.Error("游客身份的 UserID 应为 nil")
				}
				if identity.GuestName != "伪装者" {
					t.Errorf("游客昵称应被保留，实际 %q", identity.GuestName)
				}
			} else {
				if identity.UserID == nil || *identity.UserID != tc.loggedInID {
					t.Errorf("应使用登录用户 ID %d，实际 %v", tc.loggedInID, identity.UserID)
				}
				// 核心断言：登录用户的请求体游客字段不能被采纳
				if identity.GuestName != "" || identity.GuestURL != "" {
					t.Errorf("登录用户的游客字段应被丢弃，实际 name=%q url=%q",
						identity.GuestName, identity.GuestURL)
				}
			}
		})
	}
}
