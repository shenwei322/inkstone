package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/middleware"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
	"github.com/shenwei/inkstone/backend/internal/service"
)

type TaxonomyHandler struct {
	taxonomy *repository.TaxonomyRepository
}

func NewTaxonomyHandler(taxonomy *repository.TaxonomyRepository) *TaxonomyHandler {
	return &TaxonomyHandler{taxonomy: taxonomy}
}

// ListCategories handles GET /categories.
func (h *TaxonomyHandler) ListCategories(c *gin.Context) {
	categories, err := h.taxonomy.ListCategories()
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"categories": categories})
}

// ListTags handles GET /tags.
func (h *TaxonomyHandler) ListTags(c *gin.Context) {
	tags, err := h.taxonomy.ListTags()
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": tags})
}

type CommentHandler struct {
	comments *service.CommentService
	tokens   *service.TokenManager
	captcha  *service.CaptchaService
	limiter  *middleware.SlidingLimiter
	logs     *service.LogService
	// guestEnabled 覆盖"是否允许游客评论"的判定来源。生产环境留 nil，
	// 走 captcha 服务持有的设置项；测试注入固定值以覆盖两条分支
	// （不开则整个功能形同虚设，只有测试能保证两条路都走得通）。
	guestEnabled func() bool
}

func NewCommentHandler(comments *service.CommentService, tokens *service.TokenManager, captcha *service.CaptchaService, limiter *middleware.SlidingLimiter, logs *service.LogService) *CommentHandler {
	return &CommentHandler{comments: comments, tokens: tokens, captcha: captcha, limiter: limiter, logs: logs}
}

type createCommentRequest struct {
	Content  string `json:"content" binding:"required"`
	ParentID uint   `json:"parent_id"` // 被回复的评论 ID，空 = 顶级评论
	// 游客身份字段。已登录用户提交时被忽略（见 CommentService.normalizeIdentity）——
	// 否则登录用户能把评论伪造成任何人写的。
	GuestName  string `json:"guest_name"`
	GuestEmail string `json:"guest_email"`
	GuestURL   string `json:"guest_url"`
	service.CaptchaParams
}

// Create handles POST /articles/:id/comments.
//
// 登录用户与游客共用这一个端点：登录态由 OptionalAuth 中间件解析，
// 拿到当前用户就走账号身份，否则按游客处理（需后台开启 guest_comment）。
func (h *CommentHandler) Create(c *gin.Context) {
	articleID, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	var req createCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "评论内容不能为空"})
		return
	}

	identity, ok := h.resolveIdentity(c, &req)
	if !ok {
		return
	}

	// 验证码对游客强制、对登录用户按场景设置：游客没有账号可封，
	// 人机验证是唯一能在提交前挡住脚本刷屏的手段。
	if err := h.captcha.Verify("comment", req.CaptchaParams); err != nil {
		errorResponse(c, err)
		return
	}
	comment, err := h.comments.Create(uint(articleID), identity, req.ParentID, req.Content, middleware.ClientIP(c))
	if err != nil {
		errorResponse(c, err)
		return
	}
	// 评论成功：重置该 IP 配额，避免正常用户被限流误伤
	if h.limiter != nil {
		if key := middleware.RateKey(c); key != "" {
			h.limiter.Reset(key)
		}
	}
	resp := toCommentResponse(comment)
	// 命中敏感词、开了审核、或游客评论未开免审时明确告知要等审核——
	// 否则用户以为发失败了，会反复重复提交。
	if comment.IsPending() {
		resp["pending"] = true
	}
	c.JSON(http.StatusCreated, gin.H{"comment": resp})
}

// resolveIdentity 决定这条评论以什么身份发表。
//
// 返回 false 表示已经写过响应，调用方直接返回即可。
func (h *CommentHandler) resolveIdentity(c *gin.Context, req *createCommentRequest) (service.CommentIdentity, bool) {
	if current, ok := middleware.GetCurrentUser(c); ok {
		id := current.ID
		// 登录用户：只认账号身份，请求体里的 guest_* 一律丢弃。
		// 不丢弃的话，任何登录用户都能把自己的评论伪装成游客留言
		// （绕过"登录用户可追溯"这一前提）。
		return service.CommentIdentity{UserID: &id}, true
	}

	// 未登录：是否允许游客评论由后台开关决定，默认关闭
	if !h.guestCommentEnabled() {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "登录后才能评论"})
		return service.CommentIdentity{}, false
	}
	return service.CommentIdentity{
		GuestName:  req.GuestName,
		GuestEmail: req.GuestEmail,
		GuestURL:   req.GuestURL,
	}, true
}

// guestCommentEnabled 报告是否允许未登录访客评论。
//
// 生产路径复用 captcha 服务持有的设置读取能力，避免给 handler 再注入一个
// service；取不到配置时返回 false（默认不允许）——权限类开关的失败方向
// 必须是拒绝。
func (h *CommentHandler) guestCommentEnabled() bool {
	if h.guestEnabled != nil {
		return h.guestEnabled()
	}
	if h.captcha == nil {
		return false
	}
	return h.captcha.BoolSetting(service.SettingGuestComment, false)
}

// List handles GET /articles/:id/comments.
//
// viewerID 已登录则连自己未过审的评论一起返回：否则作者看不到自己刚发的
// 审核中评论，会重复提交。
func (h *CommentHandler) List(c *gin.Context) {
	articleID, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	viewerID := uint(0)
	if current, ok := middleware.GetCurrentUser(c); ok {
		viewerID = current.ID
	}
	comments, err := h.comments.ListByArticle(uint(articleID), viewerID)
	if err != nil {
		errorResponse(c, err)
		return
	}
	items := make([]gin.H, 0, len(comments))
	for i := range comments {
		items = append(items, toCommentResponse(&comments[i]))
	}
	c.JSON(http.StatusOK, gin.H{"comments": items})
}

// Delete handles DELETE /comments/:id (author or admin).
func (h *CommentHandler) Delete(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	commentID, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	if err := h.comments.Delete(uint(commentID), current.ID, current.Role == model.RoleAdmin); err != nil {
		recordOp(h.logs, c, model.LogCategoryComment, "删除评论", fmt.Sprintf("评论 #%d", commentID), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryComment, "删除评论", fmt.Sprintf("评论 #%d", commentID), true)
	c.Status(http.StatusNoContent)
}

// MyComments handles GET /auth/my-comments.
func (h *CommentHandler) MyComments(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	comments, total, err := h.comments.ListByUser(current.ID, page, pageSize)
	if err != nil {
		errorResponse(c, err)
		return
	}
	items := make([]gin.H, 0, len(comments))
	for i := range comments {
		items = append(items, toCommentResponse(&comments[i]))
	}
	c.JSON(http.StatusOK, gin.H{"comments": items, "total": total, "page": page, "page_size": pageSize})
}

// toCommentResponse 把评论转成响应体。公开出口，**不含游客邮箱**。
func toCommentResponse(cm *model.Comment) gin.H {
	return commentResponse(cm, false)
}

// toCommentResponseAdmin 后台视图：额外带上游客邮箱，供审核时联系本人。
//
// 邮箱只在这一个出口下发（json:"-" 已挡住 model 层的自动序列化），
// 公开列表与「我的评论」都必须用不带邮箱的版本。
func toCommentResponseAdmin(cm *model.Comment) gin.H {
	return commentResponse(cm, true)
}

func commentResponse(cm *model.Comment, includeGuestEmail bool) gin.H {
	// 展示名与作者 ID 一律走 model 的方法：游客评论没有 User 记录，
	// 不能再用 cm.User.ID 判定（User 现在是指针，游客时为 nil）。
	name := cm.DisplayName()
	id := cm.AuthorID()

	author := gin.H{
		"id":       id,
		"username": name,
		// is_guest 让前端不必靠 id == 0 去猜：占位用户与游客的 id 都是 0，
		// 但两者语义不同，前端要能区分（游客评论不显示个人中心链接）。
		"is_guest": cm.IsGuest(),
	}
	if cm.IsGuest() {
		// 头像：游客用昵称首字符，前端据 is_guest 决定不渲染用户主页链接
		if cm.GuestURL != "" {
			author["url"] = cm.GuestURL
		}
	}

	resp := gin.H{
		"id":            cm.ID,
		"article_id":    cm.ArticleID,
		"article_title": "",
		"article_slug":  "",
		"content":       cm.Content,
		"created_at":    cm.CreatedAt,
		"author":        author,
	}
	if cm.Article != nil {
		resp["article_title"] = cm.Article.Title
		resp["article_slug"] = cm.Article.Slug
	}
	// 父评论作者名：前端"回复 @某某"要显示，游客也要能显示
	if cm.Parent != nil {
		resp["parent_id"] = cm.Parent.ID
		resp["parent_author"] = cm.Parent.DisplayName()
	}
	if includeGuestEmail {
		// 后台审核视图：状态与游客邮箱（登录用户无游客邮箱，留空）
		resp["status"] = cm.Status
		resp["guest_email"] = cm.GuestEmail
		resp["ip"] = cm.IP
	}
	return resp
}

// MyFavorites handles GET /auth/my-favorites — 当前用户的收藏夹。
//
// 收藏按钮此前是「只进不出」的：Toggle 能写入，却没有读取入口，用户点完
// 就再也找不到收藏的文章。这里把读路径补全，返回结构与文章列表保持一致，
// 前端可以直接复用列表组件。
func (h *ReactionHandler) MyFavorites(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	page := parseIntQuery(c, "page", 1)
	pageSize := parseIntQuery(c, "page_size", 20)
	articles, total, err := h.reactions.FavoriteArticles(current.ID, page, pageSize)
	if err != nil {
		errorResponse(c, err)
		return
	}
	items := make([]articleResponse, 0, len(articles))
	for i := range articles {
		items = append(items, toArticleResponse(&articles[i]))
	}
	c.JSON(http.StatusOK, gin.H{"articles": items, "total": total, "page": page, "page_size": pageSize})
}

type ReactionHandler struct {
	reactions *service.ReactionService
}

func NewReactionHandler(reactions *service.ReactionService) *ReactionHandler {
	return &ReactionHandler{reactions: reactions}
}

type toggleReactionRequest struct {
	Type string `json:"type" binding:"required"`
}

// Toggle handles POST /articles/:id/reactions.
func (h *ReactionHandler) Toggle(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "登录后才能点赞"})
		return
	}
	articleID, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	var req toggleReactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 type 字段"})
		return
	}
	active, count, err := h.reactions.Toggle(uint(articleID), current.ID, model.ReactionType(req.Type))
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"active": active, "count": count})
}

// Stats handles GET /articles/:id/reactions.
func (h *ReactionHandler) Stats(c *gin.Context) {
	articleID, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	current, hasUser := middleware.GetCurrentUser(c)
	stats, liked, favorited, err := h.reactions.Stats(uint(articleID), current.ID, hasUser)
	if err != nil {
		errorResponse(c, err)
		return
	}
	resp := gin.H{
		"likes":     stats.Likes,
		"favorites": stats.Favorites,
	}
	if hasUser {
		resp["liked"] = liked
		resp["favorited"] = favorited
	}
	c.JSON(http.StatusOK, resp)
}
