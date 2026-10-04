package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/middleware"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/service"
)

// RevisionHandler 暴露文章历史版本的查看与恢复。
//
// 只读列表 + 恢复，不提供删除单版：删中间一版会让版本号断档，
// 而删掉的那版正是作者可能想找回的内容。上限由
// model.MaxRevisionsKept 自动裁剪，不需要人工清理入口。
type RevisionHandler struct {
	revisions *service.RevisionService
	articles  *service.ArticleService
}

func NewRevisionHandler(revisions *service.RevisionService, articles *service.ArticleService) *RevisionHandler {
	return &RevisionHandler{revisions: revisions, articles: articles}
}

// List handles GET /articles/:id/revisions.
//
// 仅作者可用：历史版本含未发布的正文（草稿时期的措辞），
// 公开接口暴露它等于绕过"这篇文章还没发"这个前提。
func (h *RevisionHandler) List(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	if !h.ensureAuthor(c, uint(id)) {
		return
	}
	revisions, err := h.revisions.List(uint(id))
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revisions": revisions})
}

// Get handles GET /articles/:id/revisions/:version.
func (h *RevisionHandler) Get(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil || version < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的版本号"})
		return
	}
	if !h.ensureAuthor(c, uint(id)) {
		return
	}
	rev, err := h.revisions.Get(uint(id), version)
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revision": rev})
}

type restoreRevisionRequest struct {
	Version int `json:"version" binding:"required"`
}

// Restore handles POST /articles/:id/revisions/restore.
// Body: {"version": 3}
//
// 恢复后返回更新后的文章，前端可直接用它刷新编辑器——
// 再发一次 GET 会让用户在等待期间对着旧内容。
func (h *RevisionHandler) Restore(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req restoreRevisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供要恢复的版本号"})
		return
	}
	article, err := h.revisions.Restore(uint(id), current.ID, req.Version)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该版本不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"article": toArticleResponse(article)})
}

// ensureAuthor 校验当前用户是该文章的作者，否则直接写响应并返回 false。
func (h *RevisionHandler) ensureAuthor(c *gin.Context, id uint) bool {
	article, err := h.articles.GetByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在"})
		return false
	}
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return false
	}
	if current.ID != article.AuthorID && current.Role != model.RoleAdmin {
		// 统一返回 404 而非 403：不暴露"这篇文章存在但不是你的"
		// 这个信息本身对攻击者有用。
		c.JSON(http.StatusNotFound, gin.H{"error": "文章不存在"})
		return false
	}
	return true
}
