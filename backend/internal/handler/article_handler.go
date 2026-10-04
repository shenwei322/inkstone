package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/middleware"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
	"github.com/shenwei/inkstone/backend/internal/service"
)

type ArticleHandler struct {
	articles *service.ArticleService
	logs     *service.LogService
}

func NewArticleHandler(articles *service.ArticleService, logs *service.LogService) *ArticleHandler {
	return &ArticleHandler{articles: articles, logs: logs}
}

type articleRequest struct {
	Title      string   `json:"title" binding:"required"`
	Content    string   `json:"content" binding:"required"`
	Status     string   `json:"status"`
	CategoryID *uint    `json:"category_id"`
	Tags       []string `json:"tags"`
	Cover      string   `json:"cover"`
}

type articleUpdateRequest struct {
	Title      *string   `json:"title"`
	Content    *string   `json:"content"`
	Status     *string   `json:"status"`
	CategoryID **uint    `json:"category_id"`
	Tags       *[]string `json:"tags"`
	Cover      *string   `json:"cover"`
}

type articleResponse struct {
	ID          uint          `json:"id"`
	Title       string        `json:"title"`
	Slug        string        `json:"slug"`
	Content     string        `json:"content"`
	Status      string        `json:"status"`
	Cover       string        `json:"cover"`
	Views       int64         `json:"views"`
	Category    *categoryInfo `json:"category,omitempty"`
	Tags        []tagInfo     `json:"tags,omitempty"`
	PublishedAt *time.Time    `json:"published_at"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
	Author      authorInfo    `json:"author"`
}

type authorInfo struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
}

type categoryInfo struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type tagInfo struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func toArticleResponse(a *model.Article) articleResponse {
	resp := articleResponse{
		ID:          a.ID,
		Title:       a.Title,
		Slug:        a.Slug,
		Content:     a.Content,
		Status:      a.Status,
		Cover:       a.Cover,
		Views:       a.Views,
		PublishedAt: a.PublishedAt,
		CreatedAt:   a.CreatedAt,
		UpdatedAt:   a.UpdatedAt,
		Author: authorInfo{
			ID:       a.Author.ID,
			Username: a.Author.Username,
		},
	}
	if a.Category != nil {
		resp.Category = &categoryInfo{ID: a.Category.ID, Name: a.Category.Name, Slug: a.Category.Slug}
	}
	if len(a.Tags) > 0 {
		resp.Tags = make([]tagInfo, 0, len(a.Tags))
		for _, t := range a.Tags {
			resp.Tags = append(resp.Tags, tagInfo{ID: t.ID, Name: t.Name, Slug: t.Slug})
		}
	}
	return resp
}

// Create handles POST /articles. Requires authentication.
func (h *ArticleHandler) Create(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req articleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供标题和内容"})
		return
	}

	article, err := h.articles.Create(current.ID, service.ArticleInput{
		Title:      req.Title,
		Content:    req.Content,
		Status:     req.Status,
		CategoryID: req.CategoryID,
		TagNames:   req.Tags,
		Cover:      req.Cover,
	})
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryArticle, "创建文章", fmt.Sprintf("《%s》", req.Title), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryArticle, "创建文章",
		fmt.Sprintf("《%s》（#%d，%s）", article.Title, article.ID, statusLabel(article.Status)), true)
	c.JSON(http.StatusCreated, gin.H{"article": toArticleResponse(article)})
}

// Update handles PUT /articles/:id. Owner only.
func (h *ArticleHandler) Update(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}

	var req articleUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}

	// 更新与新建共用 articleLimit 限流（见 main.go 路由注册）。
	// 正文写入已在 service 层经 bluemonday 消毒；这里不需要人机验证——
	// 调用者已通过 Auth 鉴权，且编辑器保存草稿的交互不适合插入验证码。
	article, err := h.articles.Update(uint(id), current.ID, service.ArticleUpdate{
		Title:      req.Title,
		Content:    req.Content,
		Status:     req.Status,
		CategoryID: req.CategoryID,
		TagNames:   req.Tags,
		Cover:      req.Cover,
	})
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryArticle, "更新文章", fmt.Sprintf("文章 #%d", id), false)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在或无权操作"})
			return
		}
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryArticle, "更新文章",
		fmt.Sprintf("《%s》（#%d%s）", article.Title, article.ID, changedDetail(req)), true)
	c.JSON(http.StatusOK, gin.H{"article": toArticleResponse(article)})
}

// Delete handles DELETE /articles/:id. Owner only.
func (h *ArticleHandler) Delete(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}

	// 删除前先取标题，让审计日志记录删的是哪篇文章
	title := ""
	if article, err := h.articles.GetByID(uint(id)); err == nil {
		title = article.Title
	}
	if err := h.articles.Delete(uint(id), current.ID); err != nil {
		recordOp(h.logs, c, model.LogCategoryArticle, "删除文章", fmt.Sprintf("文章 #%d", id), false)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在或无权操作"})
			return
		}
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryArticle, "删除文章",
		fmt.Sprintf("《%s》（#%d）", title, id), true)
	c.Status(http.StatusNoContent)
}

// canViewArticle reports whether the current requester may read the article.
// Published articles are public; drafts are visible only to their author or an admin.
func canViewArticle(c *gin.Context, article *model.Article) bool {
	if article.Status == model.ArticlePublished {
		return true
	}
	current, ok := middleware.GetCurrentUser(c)
	return ok && (current.Role == model.RoleAdmin || current.ID == article.AuthorID)
}

// Get handles GET /articles/:id.
func (h *ArticleHandler) Get(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	article, err := h.articles.GetByID(uint(id))
	if err != nil {
		errorResponse(c, err)
		return
	}
	if !canViewArticle(c, article) {
		c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在"})
		return
	}
	_ = h.articles.IncrementViews(uint(id))
	article.Views++
	c.JSON(http.StatusOK, gin.H{"article": toArticleResponse(article)})
}

// GetBySlug handles GET /articles/slug/:slug.
func (h *ArticleHandler) GetBySlug(c *gin.Context) {
	article, err := h.articles.GetBySlug(c.Param("slug"))
	if err != nil {
		errorResponse(c, err)
		return
	}
	if !canViewArticle(c, article) {
		c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在"})
		return
	}
	_ = h.articles.IncrementViews(article.ID)
	article.Views++
	c.JSON(http.StatusOK, gin.H{"article": toArticleResponse(article)})
}

// List handles GET /articles with pagination and optional filters.
func (h *ArticleHandler) List(c *gin.Context) {
	page, pageSize := parseIntOr(c.Query("page"), 1), parseIntOr(c.Query("page_size"), 10)
	if pageSize > 50 {
		pageSize = 50
	}
	q := repository.ArticleQuery{
		Page:         page,
		PageSize:     pageSize,
		Status:       c.Query("status"),
		CategorySlug: c.Query("category"),
		TagSlug:      c.Query("tag"),
		Search:       c.Query("q"),
		OrderBy:      c.Query("order"),
	}
	if v := c.Query("author_id"); v != "" {
		if id, err := strconv.ParseUint(v, 10, 64); err == nil {
			q.AuthorID = uint(id)
		}
	}

	// Non-published statuses require authentication (viewing own drafts).
	if q.Status != "" && q.Status != model.ArticlePublished {
		current, authenticated := middleware.GetCurrentUser(c)
		if !authenticated {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "需要登录才能查看非公开文章"})
			return
		}
		q.AuthorID = current.ID
	}

	articles, total, err := h.articles.List(q)
	if err != nil {
		errorResponse(c, err)
		return
	}

	items := make([]articleResponse, 0, len(articles))
	for i := range articles {
		items = append(items, toArticleResponse(&articles[i]))
	}

	c.JSON(http.StatusOK, gin.H{
		"articles":  items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func parseIntOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 {
		return fallback
	}
	return v
}

// articleBrief 是相关文章 / 上下篇的轻量卡片字段：只含在别处展示所需的
// 元信息。不带 content（正文可能几十 KB，卡片用不到），不带 author /
// tags（related 是匿名可访问的公开接口，没必要为此多跑预加载查询）。
type articleBrief struct {
	ID          uint       `json:"id"`
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	Cover       string     `json:"cover"`
	Views       int64      `json:"views"`
	PublishedAt *time.Time `json:"published_at"`
}

func toArticleBrief(a *model.Article) articleBrief {
	return articleBrief{
		ID:          a.ID,
		Title:       a.Title,
		Slug:        a.Slug,
		Cover:       a.Cover,
		Views:       a.Views,
		PublishedAt: a.PublishedAt,
	}
}

// articleBriefOrNil 让「一侧不存在」的邻居在 JSON 里输出 null 而不是
// 一个零值对象——前端要靠 null 判断该不该渲染「没有下一篇」。
func articleBriefOrNil(a *model.Article) any {
	if a == nil {
		return nil
	}
	return toArticleBrief(a)
}

// RelatedBySlug handles GET /articles/slug/:slug/related — 相关文章推荐。
// 公开可读；草稿按与详情页相同的规则只有作者/管理员能看到（看不到时
// 统一返回 404，不暴露「这篇草稿存在」）。
func (h *ArticleHandler) RelatedBySlug(c *gin.Context) {
	article, err := h.articles.GetBySlug(c.Param("slug"))
	if err != nil {
		errorResponse(c, err)
		return
	}
	if !canViewArticle(c, article) {
		c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在"})
		return
	}

	limit := parseIntQuery(c, "limit", 4)
	related, err := h.articles.Related(article.ID, limit)
	if err != nil {
		errorResponse(c, err)
		return
	}

	items := make([]articleBrief, 0, len(related))
	for i := range related {
		items = append(items, toArticleBrief(&related[i]))
	}
	c.JSON(http.StatusOK, gin.H{"articles": items})
}

// NeighborsBySlug handles GET /articles/slug/:slug/neighbors — 上一篇 / 下一篇。
// prev 是更新的文章（往前翻），next 是更旧的文章（往后翻）。
func (h *ArticleHandler) NeighborsBySlug(c *gin.Context) {
	article, err := h.articles.GetBySlug(c.Param("slug"))
	if err != nil {
		errorResponse(c, err)
		return
	}
	if !canViewArticle(c, article) {
		c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在"})
		return
	}

	prev, next, err := h.articles.Neighbors(article.ID)
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"prev": articleBriefOrNil(prev),
		"next": articleBriefOrNil(next),
	})
}
