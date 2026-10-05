package service

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

// CommentNotifier 由外部注入（pkg/mailer 包装），让 service 不直接依赖
// mailer 包。发送失败只记日志——通知是旁路，不该让评论发不出去。
//
// commenter 传评论者用户名（留空表示未取到），让邮件正文里能看出是谁
// 留了言，作者不必再点进后台查。
type CommentNotifier interface {
	NotifyAuthor(articleTitle, articleSlug, authorEmail, commenter string)
}

type CommentService struct {
	comments *repository.CommentRepository
	articles *repository.ArticleRepository
	settings *SettingsService
	notifier CommentNotifier
}

func NewCommentService(comments *repository.CommentRepository, articles *repository.ArticleRepository, settings *SettingsService, notifier CommentNotifier) *CommentService {
	return &CommentService{comments: comments, articles: articles, settings: settings, notifier: notifier}
}

// CommentIdentity 描述一条评论的作者身份。
//
// 用结构体而不是给 Create 加一串参数：登录/游客两条路径互斥，
// 用 UserID != nil 就能表达，且新增字段不会再次改变方法签名
// （Create 此前已因 parentID / ip 改过两次签名）。
type CommentIdentity struct {
	// UserID 非空 = 已登录用户；为空 = 游客，此时读 Guest* 三个字段。
	UserID *uint
	// GuestName 游客昵称（必填，展示用）。
	GuestName string
	// GuestEmail 游客邮箱（选填，仅后台可见，用于联系）。
	GuestEmail string
	// GuestURL 游客个人网站（选填，公开展示，需消毒）。
	GuestURL string
}

// Create 发表评论。
//
// parentID > 0 表示回复某条评论（嵌套盖楼）。两个行为变化：
//   - 审核：开启 comment_audit 或命中敏感词时，状态置 pending（待人工审核），
//     仍写入成功——评论者看到"已提交，等待审核"，而不是报错；
//   - 通知：开启 comment_notify 且文章作者不是评论者本人时，邮件通知作者。
//
// identity 同时承载登录用户与游客两种身份，见 CommentIdentity 说明。
func (s *CommentService) Create(articleID uint, identity CommentIdentity, parentID uint, content, ip string) (*model.Comment, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, NewValidationError("评论内容不能为空")
	}
	if len([]rune(content)) > 1000 {
		return nil, NewValidationError("评论最长 1000 个字符")
	}

	identity, err := s.normalizeIdentity(identity)
	if err != nil {
		return nil, err
	}

	article, err := s.articles.FindByID(articleID)
	if err != nil {
		return nil, err
	}

	var parent *model.Comment
	if parentID > 0 {
		parent, err = s.comments.FindByID(parentID)
		if err != nil {
			return nil, NewValidationError("被回复的评论不存在")
		}
		if parent.ArticleID != articleID {
			// 跨文章回复会把两条无关讨论拼在一起，拒绝
			return nil, NewValidationError("只能回复同一篇文章下的评论")
		}
	}

	comment := &model.Comment{
		ArticleID: articleID,
		UserID:    identity.UserID,
		// ParentID 是 *uint（外键），不是 *Comment：GORM 存的是 id。
		// 取不到父评论就留 nil —— 顶级评论。
		ParentID: parentIDPtr(parent),
		Content:  content,
		IP:       ip,
	}
	// 游客身份列只在确实是游客时写入：登录用户即便请求体里带了
	// guest_name，也不能在库里留下第二套身份。
	if identity.UserID == nil {
		comment.GuestName = identity.GuestName
		comment.GuestEmail = identity.GuestEmail
		comment.GuestURL = identity.GuestURL
	}
	s.applyModeration(comment, content)
	if err := s.comments.Create(comment); err != nil {
		return nil, err
	}
	s.notifyAuthor(article, comment)
	return s.comments.FindByID(comment.ID)
}

// normalizeIdentity 校验并规整评论者身份。
//
// 游客昵称必填：没有账号也没有名字的评论在后台无法分辨谁是谁，
// 作者想回复都无处称呼。邮箱与网址选填，但填了就要像个样子。
func (s *CommentService) normalizeIdentity(id CommentIdentity) (CommentIdentity, error) {
	if id.UserID != nil {
		// 登录用户：清空游客字段，防止两条身份路径混用
		return CommentIdentity{UserID: id.UserID}, nil
	}

	name := strings.TrimSpace(id.GuestName)
	if name == "" {
		return id, NewValidationError("请填写昵称")
	}
	if len([]rune(name)) > 32 {
		return id, NewValidationError("昵称最长 32 个字符")
	}

	email := strings.TrimSpace(id.GuestEmail)
	if email != "" && !emailPattern.MatchString(email) {
		return id, NewValidationError("邮箱格式不正确")
	}
	// 后台可要求游客必填邮箱（guest_comment_email）。默认不要求：
	// 强制留邮箱会劝退一部分只想说句话的访客。
	if email == "" && s.guestEmailRequired() {
		return id, NewValidationError("请填写邮箱")
	}

	url := strings.TrimSpace(id.GuestURL)
	if url != "" {
		// 只接受 http/https：javascript: 之类的伪协议会被 <a href> 直接
		// 执行，构成存储型 XSS。这里拒绝而不是改写，让用户明确知道
		// 自己填的地址没被接受。
		if !isHTTPURL(url) {
			return id, NewValidationError("个人网站需以 http:// 或 https:// 开头")
		}
		if len(url) > 255 {
			return id, NewValidationError("个人网站地址过长")
		}
	}

	return CommentIdentity{
		GuestName:  name,
		GuestEmail: email,
		GuestURL:   url,
	}, nil
}

// emailPattern 是宽松的邮箱格式校验：只挡住明显不是邮箱的输入
// （防手误），不追求 RFC 5322 完备——过严的校验会把合法地址挡在门外，
// 而真正的验证手段是「找回密码时能否收到信」。
var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s.]+(\.[^@\s.]+)+$`)

// isHTTPURL 判断是否为 http/https 绝对地址。
func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// parentIDPtr 把取到的父评论转成外键指针。nil 父评论（顶级）返回 nil，
// 这样 GORM 写入 NULL 而不是 0 —— 0 会指向不存在的评论。
func parentIDPtr(parent *model.Comment) *uint {
	if parent == nil {
		return nil
	}
	id := parent.ID
	return &id
}

// applyModeration 决定一条新评论的初始状态。
//
// 敏感词命中不直接拒绝：拒绝会把拦截规则暴露给刷评者，他们换个写法就能绕过。
// 转人工审核同样能挡住内容，且不留"这个词被拦了"的信号。
//
// 游客评论额外受 guest_comment_free 控制：该开关**默认关闭**，即游客评论
// 一律进待审核队列。理由是游客没有账号可封、没有历史可追溯，一次刷屏
// 只能靠人事后清理；默认要求先审后发，把风险挡在公开之前。
func (s *CommentService) applyModeration(comment *model.Comment, content string) {
	if s.auditEnabled() || s.matchesSensitiveWord(content) {
		comment.Status = model.CommentPending
		return
	}
	// 敏感词与全站审核都没命中，但这是一条游客评论且未开启免审
	if comment.IsGuest() && !s.guestCommentFree() {
		comment.Status = model.CommentPending
		return
	}
	comment.Status = model.CommentApproved
}

func (s *CommentService) auditEnabled() bool {
	if s.settings == nil {
		return false
	}
	return s.settings.BoolValue(SettingCommentAudit, false)
}

// guestCommentFree 报告游客评论是否免于先审后发。
//
// 未配置时默认 false（要审核）：安全默认值应当落在"更严格"那一侧，
// 配置丢失或服务异常都不能让游客评论直接公开。
func (s *CommentService) guestCommentFree() bool {
	if s.settings == nil {
		return false
	}
	return s.settings.BoolValue(SettingGuestCommentFree, false)
}

// guestEmailRequired 报告游客是否必须填写邮箱。
//
// 未配置时默认 false（选填）——这是体验类开关而非安全类，
// 读不到配置时不该把一个原本能发出去的评论卡住。
func (s *CommentService) guestEmailRequired() bool {
	if s.settings == nil {
		return false
	}
	return s.settings.BoolValue(SettingGuestCommentEmail, false)
}

// matchesSensitiveWord 判断内容是否命中敏感词表。
// 词表按换行/逗号/分号切分，空白项跳过；比对前把内容与词都转小写，
// 中文无大小写但英文词需要。
func (s *CommentService) matchesSensitiveWord(content string) bool {
	if s.settings == nil {
		return false
	}
	raw, err := s.settings.Get(SettingCommentWords)
	if err != nil || strings.TrimSpace(raw) == "" {
		return false
	}
	lower := strings.ToLower(content)
	for _, w := range splitWords(raw) {
		if w != "" && strings.Contains(lower, strings.ToLower(w)) {
			return true
		}
	}
	return false
}

// splitWords 把词表原文切分成词。支持换行、中英文逗号、分号、竖线分隔，
// 因为后台textarea里用户换行还是逗号全凭习惯，不做硬性规定。
var wordSep = regexp.MustCompile(`[\n\r,，;；|]+`)

func splitWords(raw string) []string {
	parts := wordSep.Split(raw, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if w := strings.TrimSpace(p); w != "" {
			out = append(out, w)
		}
	}
	return out
}

func (s *CommentService) notifyAuthor(article *model.Article, comment *model.Comment) {
	if s.notifier == nil || s.settings == nil {
		return
	}
	if !s.settings.BoolValue(SettingCommentNotify, false) {
		return
	}
	// 作者自己评论自己的文章不发通知——否则每次自评都收一封邮件。
	// 用 AuthorID() 比较：游客评论返回 0，与任何真实作者 ID 都不等，
	// 因此游客评论一定会通知作者（这正是想要的）。
	if article.AuthorID == comment.AuthorID() {
		return
	}
	email := article.Author.Email
	if email == "" {
		return
	}
	// 评论者展示名交给 model 统一计算：登录用户取用户名，游客取昵称。
	// 此前这里固定传空串，游客评论会显示成「有人」。
	s.notifier.NotifyAuthor(article.Title, article.Slug, email, comment.DisplayName())
}

// ListByArticle returns comments visible to a viewer.
//
// viewerID 传当前登录用户（未登录传 0）。返回两部分：所有人可见的 approved
// 评论，加上 viewer 自己那些还没过审的 pending——否则作者以为评论丢了，
// 会反复重复提交。
//
// 注意「自己的 pending」只对登录用户成立：游客没有账号，也就无从在一次
// 请求之外认出「自己刚发的那条」，因此游客只能看到已过审的评论，
// 提交后前端提示"等待审核"而不回显。
func (s *CommentService) ListByArticle(articleID uint, viewerID uint) ([]model.Comment, error) {
	comments, err := s.comments.ListByArticle(articleID)
	if err != nil {
		return nil, err
	}
	out := make([]model.Comment, 0, len(comments))
	for _, c := range comments {
		if c.IsPublic() || (viewerID != 0 && c.AuthorID() == viewerID) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *CommentService) GetByID(id uint) (*model.Comment, error) {
	return s.comments.FindByID(id)
}

// Delete allows the comment author or an admin to remove a comment.
//
// 游客评论**只有管理员能删**：游客没有账号，无法证明"这条是我发的"。
// 若照搬 AuthorID() 比较，任何未登录请求传进来的 userID 都是 0，而游客
// 评论的 AuthorID() 同样是 0 —— 0 == 0 成立，等于给了所有人删除任意
// 游客评论的权限。这里必须先排除游客评论再比对账号。
func (s *CommentService) Delete(commentID, userID uint, isAdmin bool) error {
	comment, err := s.comments.FindByID(commentID)
	if err != nil {
		return err
	}
	if isAdmin {
		return s.comments.Delete(commentID)
	}
	if comment.IsGuest() || comment.AuthorID() != userID {
		return ErrForbidden
	}
	return s.comments.Delete(commentID)
}

// SetStatus 审核一条评论（approved / rejected）。
func (s *CommentService) SetStatus(commentID uint, status string) error {
	switch status {
	case model.CommentApproved, model.CommentRejected:
		return s.comments.UpdateStatus(commentID, status)
	default:
		return NewValidationError("无效的审核状态")
	}
}

func (s *CommentService) ListAll(page, pageSize int) ([]model.Comment, int64, error) {
	return s.comments.ListAll(page, pageSize)
}

func (s *CommentService) DeleteAny(commentID uint) error {
	return s.comments.Delete(commentID)
}

func (s *CommentService) ListByUser(userID uint, page, pageSize int) ([]model.Comment, int64, error) {
	return s.comments.ListByUser(userID, page, pageSize)
}

// CountPending 返回待审核评论数（后台菜单角标）。
func (s *CommentService) CountPending() (int64, error) {
	return s.comments.CountByStatus(model.CommentPending)
}
