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
}

func NewCommentHandler(comments *service.CommentService, tokens *service.TokenManager, captcha *service.CaptchaService, limiter *middleware.SlidingLimiter, logs *service.LogService) *CommentHandler {
	return &CommentHandler{comments: comments, tokens: tokens, captcha: captcha, limiter: limiter, logs: logs}
}

type createCommentRequest struct {
	Content  string `json:"content" binding:"required"`
	ParentID uint   `json:"parent_id"` // 被回复的评论 ID，空 = 顶级评论
	service.CaptchaParams
}

// Create handles POST /articles/:id/comments.
func (h *CommentHandler) Create(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "登录后才能评论"})
		return
	}
	articleID, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	var req createCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "评论内容不能为空"})
		return
	}
	if err := h.captcha.Verify("comment", req.CaptchaParams); err != nil {
		errorResponse(c, err)
		return
	}
	comment, err := h.comments.Create(uint(articleID), current.ID, req.ParentID, req.Content, middleware.ClientIP(c))
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
	// 命中敏感词或开了审核时明确告知要等审核——否则用户以为发失败了，
	// 会反复重复提交。
	if comment.IsPending() {
		resp["pending"] = true
	}
	c.JSON(http.StatusCreated, gin.H{"comment": resp})
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

func toCommentResponse(cm *model.Comment) gin.H {
	user := gin.H{"id": 0, "username": "已注销"}
	if cm.User.ID != 0 {
		user = gin.H{"id": cm.User.ID, "username": cm.User.Username}
	}
	articleTitle := ""
	articleSlug := ""
	if cm.Article != nil {
		articleTitle = cm.Article.Title
		articleSlug = cm.Article.Slug
	}
	return gin.H{
		"id":            cm.ID,
		"article_id":    cm.ArticleID,
		"article_title": articleTitle,
		"article_slug":  articleSlug,
		"content":       cm.Content,
		"created_at":    cm.CreatedAt,
		"author":        user,
	}
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
