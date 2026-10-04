package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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
	Title        string     `json:"title" binding:"required"`
	Content      string     `json:"content" binding:"required"`
	Status       string     `json:"status"`
	CategoryID   *uint      `json:"category_id"`
	Tags         []string   `json:"tags"`
	Cover        string     `json:"cover"`
	Excerpt      string     `json:"excerpt"`
	IsPinned     bool       `json:"is_pinned"`
	ViewPassword string     `json:"view_password"`
	ScheduledAt  *time.Time `json:"scheduled_at"`
}

type articleUpdateRequest struct {
	Title        *string     `json:"title"`
	Content      *string     `json:"content"`
	Status       *string     `json:"status"`
	CategoryID   **uint      `json:"category_id"`
	Tags         *[]string   `json:"tags"`
	Cover        *string     `json:"cover"`
	Excerpt      *string     `json:"excerpt"`
	IsPinned     *bool       `json:"is_pinned"`
	ViewPassword *string     `json:"view_password"`
	ScheduledAt  **time.Time `json:"scheduled_at"`
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
	// AuthorID 单独列出而非只放在 author 对象里：stripLockedContent
	// 要判断"这是不是当前用户自己的文章"，用 author.id 可达同样目的，
	// 但后台与前台对 author 的序列化要求不同，单列一个字段更直接。
	AuthorID    uint       `json:"author_id"`
	Excerpt     string     `json:"excerpt"`
	IsPinned    bool       `json:"is_pinned"`
	ScheduledAt *time.Time `json:"scheduled_at"`
	// HasPassword 只告知"这篇设了访问密码"，绝不下发密码本身。
	// 前端据此渲染密码输入框。
	HasPassword bool `json:"has_password"`
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

// resolveExcerptForResponse 决定响应里返回的摘要。
//
// 显式摘要优先；为空时用正文现算一份。这里不在保存时兜底生成并写库，
// 是因为作者可能只想让某篇文章"列表显示这段、正文里没有"——
// 写库会把这份意图固化成数据，之后改正文时摘要不跟着变，反而更难维护。
func resolveExcerptForResponse(a *model.Article) string {
	if strings.TrimSpace(a.Excerpt) != "" {
		return a.Excerpt
	}
	return service.ExcerptFor(a.Content, 0)
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
		AuthorID:    a.AuthorID,
		Excerpt:     resolveExcerptForResponse(a),
		IsPinned:    a.IsPinned,
		ScheduledAt: a.ScheduledAt,
		HasPassword: a.ViewPassword != "",
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
		Title:        req.Title,
		Content:      req.Content,
		Status:       req.Status,
		CategoryID:   req.CategoryID,
		TagNames:     req.Tags,
		Cover:        req.Cover,
		Excerpt:      req.Excerpt,
		IsPinned:     req.IsPinned,
		ViewPassword: req.ViewPassword,
		ScheduledAt:  req.ScheduledAt,
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
		Title:        req.Title,
		Content:      req.Content,
		Status:       req.Status,
		CategoryID:   req.CategoryID,
		TagNames:     req.Tags,
		Cover:        req.Cover,
		Excerpt:      req.Excerpt,
		IsPinned:     req.IsPinned,
		ViewPassword: req.ViewPassword,
		ScheduledAt:  req.ScheduledAt,
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
//
// 除 published 外的一切状态（draft / scheduled）都只有作者与管理员可见。
// scheduled 走这条分支是正确的：定时发布还没到点的文章，
// 提前 5 分钟把人放进去看到正文不是"小的体验瑕疵"，是发布事故。
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
	// 密码校验必须同时覆盖 /:id 与 /slug/:slug 两个入口：
	// 只挡 slug 入口的话，知道 id 就能直接取到加密文章全文。
	if !h.articlePasswordOK(c, article) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":         "这篇文章需要访问密码",
			"need_password": true,
		})
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
	// 密码保护：未校验通过时只返回 401 + need_password，不返回正文。
	// （详见 articlePasswordOK 的说明）
	if !h.articlePasswordOK(c, article) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":         "这篇文章需要访问密码",
			"need_password": true,
		})
		return
	}
	_ = h.articles.IncrementViews(article.ID)
	article.Views++
	c.JSON(http.StatusOK, gin.H{"article": toArticleResponse(article)})
}

// articlePasswordOK 判断当前请求是否有权看这篇设了密码的文章。
//
// 作者本人永远免密（他要改自己的文章，不能把自己锁在外面）。
// 其余人需要带 password 查询参数，用 bcrypt 比对。
//
// 刻意不做"会话记住已解锁文章"：那需要在服务端存一份
// 「用户 × 文章」的解锁表，多一层状态、多一个过期策略，
// 而收益只是少输一次密码。选择简单方案。
func (h *ArticleHandler) articlePasswordOK(c *gin.Context, article *model.Article) bool {
	if article.ViewPassword == "" {
		return true
	}
	// 作者本人（或管理员改文章时）免密
	if current, ok := middleware.GetCurrentUser(c); ok {
		if current.ID == article.AuthorID {
			return true
		}
	}
	return service.CheckViewPassword(article, c.Query("password"))
}

// UnlockArticle handles GET /articles/slug/:slug/unlock?password=xxx.
//
// 与 GetBySlug 的 password 参数等价，单独一个端点的好处是语义明确：
// 前端"输密码"这一步可以单独 try/catch，不必把「文章取不到」和
// 「密码错了」两种失败混在一个 catch 里（两者的提示文案不同）。
func (h *ArticleHandler) UnlockArticle(c *gin.Context) {
	article, err := h.articles.GetBySlug(c.Param("slug"))
	if err != nil {
		errorResponse(c, err)
		return
	}
	if !canViewArticle(c, article) {
		c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在"})
		return
	}
	if article.ViewPassword == "" {
		c.JSON(http.StatusOK, gin.H{"unlocked": true, "has_password": false})
		return
	}
	if current, ok := middleware.GetCurrentUser(c); ok && current.ID == article.AuthorID {
		c.JSON(http.StatusOK, gin.H{"unlocked": true, "has_password": true})
		return
	}
	if service.CheckViewPassword(article, c.Query("password")) {
		c.JSON(http.StatusOK, gin.H{"unlocked": true, "has_password": true})
		return
	}
	c.JSON(http.StatusUnauthorized, gin.H{"error": "访问密码不正确"})
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
	h.stripLockedContent(c, items)

	c.JSON(http.StatusOK, gin.H{
		"articles":  items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// stripLockedContent 把未解锁文章的正文与摘要从列表响应里抹掉。
//
// 为什么必须做：列表接口返回的是完整 Content。如果不处理，访客在首页
// 拿到文章列表就等于拿到了所有加密文章的正文——密码保护形同虚设。
// 把 Content 清空并在前端显示"该文章已加密"，标题/封面等元信息保留
// （否则列表里会凭空少一篇，反而让人以为是数据丢了）。
//
// Excerpt 也要清：作者没手写摘要时，后端从正文生成它——
// 那段"摘要"就是正文前 120 字，留着等于把加密文章的开头公开。
//
// Content 就地改动而非重新构造：items 是值切片，改的是副本，
// 不会污染 repository 层的数据（下次请求照常取到完整正文）。
func (h *ArticleHandler) stripLockedContent(c *gin.Context, items []articleResponse) {
	current, ok := middleware.GetCurrentUser(c)
	for i := range items {
		if !items[i].HasPassword {
			continue
		}
		// 作者本人（自己的文章列表）保留正文与摘要
		if ok && current.ID == items[i].AuthorID {
			continue
		}
		items[i].Content = ""
		items[i].Excerpt = ""
	}
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
	// Excerpt 只在未加密时下发。摘要可能摘自正文——
	// 加密文章的摘要等于把内容透露出去。有密码时留空，
	// 前端显示「该文章已加密」而不是半段摘要。
	Excerpt string `json:"excerpt"`
	// HasPassword 让卡片也能显示锁标识。卡片接口用 Select 指定列，
	// ViewPassword 不在列里，只能靠查询期计算的布尔值。
	HasPassword bool `json:"has_password"`
}

func toArticleBrief(a *model.Article) articleBrief {
	brief := articleBrief{
		ID:          a.ID,
		Title:       a.Title,
		Slug:        a.Slug,
		Cover:       a.Cover,
		Views:       a.Views,
		PublishedAt: a.PublishedAt,
		HasPassword: a.HasPassword || a.ViewPassword != "",
	}
	if brief.HasPassword {
		return brief
	}
	brief.Excerpt = resolveExcerptForResponse(a)
	return brief
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
