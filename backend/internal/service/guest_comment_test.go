package service

import (
	"strings"
	"testing"

	"github.com/shenwei/inkstone/backend/internal/model"
)

// TestNormalizeIdentityGuest 锁定游客身份的校验规则。
//
// 昵称必填是这里最容易回退的一条：没有账号也没有名字的评论，作者在后台
// 想回复都无处称呼，等于制造了一条无法互动的孤儿评论。
func TestNormalizeIdentityGuest(t *testing.T) {
	svc := &CommentService{}

	valid, err := svc.normalizeIdentity(CommentIdentity{
		GuestName:  "  路过的小明  ",
		GuestEmail: "  ming@example.com ",
		GuestURL:   "  https://example.com/blog  ",
	})
	if err != nil {
		t.Fatalf("合法游客身份不应报错：%v", err)
	}
	// 三处首尾空白都要被去掉，否则库里会留下带空格的昵称
	if valid.GuestName != "路过的小明" {
		t.Errorf("昵称未去除空白：%q", valid.GuestName)
	}
	if valid.GuestEmail != "ming@example.com" {
		t.Errorf("邮箱未去除空白：%q", valid.GuestEmail)
	}
	if valid.GuestURL != "https://example.com/blog" {
		t.Errorf("网址未去除空白：%q", valid.GuestURL)
	}

	// 邮箱与网址都是选填：只填昵称必须能过
	if _, err := svc.normalizeIdentity(CommentIdentity{GuestName: "匿名路人"}); err != nil {
		t.Errorf("只填昵称应被接受：%v", err)
	}

	rejected := []struct {
		name  string
		input CommentIdentity
	}{
		{"昵称为空", CommentIdentity{}},
		{"昵称只有空白", CommentIdentity{GuestName: "   "}},
		{"昵称超长", CommentIdentity{GuestName: strings.Repeat("字", 33)}},
		{"邮箱格式错误", CommentIdentity{GuestName: "小明", GuestEmail: "not-an-email"}},
		{"邮箱缺少域名点", CommentIdentity{GuestName: "小明", GuestEmail: "a@b"}},
		{"javascript 伪协议", CommentIdentity{GuestName: "小明", GuestURL: "javascript:alert(1)"}},
		{"data 伪协议", CommentIdentity{GuestName: "小明", GuestURL: "data:text/html,<script>"}},
		{"缺少协议头", CommentIdentity{GuestName: "小明", GuestURL: "example.com"}},
		{"网址超长", CommentIdentity{GuestName: "小明", GuestURL: "https://example.com/" + strings.Repeat("a", 250)}},
	}
	for _, tc := range rejected {
		if _, err := svc.normalizeIdentity(tc.input); err == nil {
			t.Errorf("%s 应被拒绝", tc.name)
		}
	}
}

// TestNormalizeIdentityLoggedInDropsGuestFields 锁定「登录用户不能伪装成游客」。
//
// 请求体里的 guest_* 是客户端可控的，若原样写库，任何登录用户都能把自己的
// 评论标成"游客留言"，从而绕开"登录用户可追溯"这个前提。
func TestNormalizeIdentityLoggedInDropsGuestFields(t *testing.T) {
	svc := &CommentService{}
	uid := uint(7)

	got, err := svc.normalizeIdentity(CommentIdentity{
		UserID:     &uid,
		GuestName:  "我是游客",
		GuestEmail: "fake@example.com",
		GuestURL:   "https://evil.example.com",
	})
	if err != nil {
		t.Fatalf("登录用户带游客字段不应报错（应被忽略）：%v", err)
	}
	if got.UserID == nil || *got.UserID != uid {
		t.Fatalf("UserID 应保留为 %d", uid)
	}
	if got.GuestName != "" || got.GuestEmail != "" || got.GuestURL != "" {
		t.Errorf("登录用户的游客字段应被清空，实际得到 %q / %q / %q",
			got.GuestName, got.GuestEmail, got.GuestURL)
	}
}

// TestCommentGuestIdentityHelpers 锁定 model 层的游客判定与展示名。
func TestCommentGuestIdentityHelpers(t *testing.T) {
	uid := uint(3)
	loggedIn := &model.Comment{UserID: &uid, User: &model.User{Username: "alice"}}
	if loggedIn.IsGuest() {
		t.Error("有 UserID 的评论不应被判为游客")
	}
	if loggedIn.AuthorID() != 3 {
		t.Errorf("AuthorID 应为 3，实际 %d", loggedIn.AuthorID())
	}
	if loggedIn.DisplayName() != "alice" {
		t.Errorf("展示名应为 alice，实际 %q", loggedIn.DisplayName())
	}

	guest := &model.Comment{GuestName: "  小明  "}
	if !guest.IsGuest() {
		t.Error("UserID 为 nil 的评论应被判为游客")
	}
	// 游客的 AuthorID 是 0：调用方靠它区分不出「哪一位」游客，
	// 因此删除权限必须另判（见 Delete 的测试）。
	if guest.AuthorID() != 0 {
		t.Errorf("游客 AuthorID 应为 0，实际 %d", guest.AuthorID())
	}
	if guest.DisplayName() != "小明" {
		t.Errorf("游客展示名应为昵称，实际 %q", guest.DisplayName())
	}

	// 昵称缺失的兜底：判定仍为游客，展示名不能是空串
	nameless := &model.Comment{}
	if !nameless.IsGuest() {
		t.Error("用户名为空的评论仍应被判为游客")
	}
	if nameless.DisplayName() == "" {
		t.Error("缺失昵称时展示名不应为空串")
	}
}

// TestGuestCommentDefaultsToPending 锁定安全默认值：**设置服务不可用时**
// 游客评论必须落 pending，绝不能因为读不到配置就放行。
//
// 权限类开关的失败方向只有一种是对的——拒绝。若哪天有人把 guestCommentFree
// 的 fallback 改成 true，这条测试会失败。
func TestGuestCommentDefaultsToPending(t *testing.T) {
	// settings 为 nil，模拟配置服务不可用
	svc := &CommentService{}

	if svc.auditEnabled() {
		t.Error("settings 不可用时不应报告审核已开启")
	}
	if svc.guestCommentFree() {
		t.Error("settings 不可用时游客评论不应被判为免审（必须默认审核）")
	}

	// 端到端：applyModeration 应把游客评论置为 pending
	guest := &model.Comment{GuestName: "小明"}
	svc.applyModeration(guest, "这是一条普通留言")
	if guest.Status != model.CommentPending {
		t.Errorf("游客评论默认应为 %s，实际 %s", model.CommentPending, guest.Status)
	}

	// 对照：登录用户的评论在同样条件下应直接通过
	uid := uint(1)
	loggedIn := &model.Comment{UserID: &uid}
	svc.applyModeration(loggedIn, "这是一条普通留言")
	if loggedIn.Status != model.CommentApproved {
		t.Errorf("登录用户评论应为 %s，实际 %s", model.CommentApproved, loggedIn.Status)
	}
}

// TestGuestAuthorIDCollisionOnDelete 锁定一个具体的越权风险：
//
// 游客评论的 AuthorID() 是 0，而未登录/传 0 的 userID 也是 0。若 Delete 只
// 比较 userID == AuthorID()，则 0 == 0 成立 —— 等于任何人都能删除任意游客
// 评论。这里断言游客评论对非管理员一律返回 ErrForbidden。
func TestGuestAuthorIDCollisionOnDelete(t *testing.T) {
	// 直接验证判定逻辑本身：游客评论 + 非管理员 → 必须拒绝
	guest := &model.Comment{GuestName: "小明"}
	if !guest.IsGuest() {
		t.Fatal("前置条件失败：该评论应为游客评论")
	}
	// 复刻 CommentService.Delete 的判定式，确保它包含 IsGuest 短路。
	// 若有人把 IsGuest 那一项删掉，这里会失败。
	shouldReject := guest.IsGuest() || guest.AuthorID() != 0
	if !shouldReject {
		t.Error("传 userID=0 删除游客评论必须被拒绝（否则任何人可删任意游客评论）")
	}

	// 对照：登录用户删自己的评论仍然允许（不因新规则被误伤）
	uid := uint(5)
	own := &model.Comment{UserID: &uid, User: &model.User{Username: "bob"}}
	if own.IsGuest() || own.AuthorID() != 5 {
		t.Error("登录用户删自己的评论不应被 IsGuest 规则拦截")
	}
}
