package handler

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
	"github.com/shenwei/inkstone/backend/internal/service"
)

// AdminTagHandler 只做参数绑定 / 调用 service / 响应映射；
// 标签的校验与业务逻辑在 service.TaxonomyService（分层铁律）。
type AdminTagHandler struct {
	tags *service.TaxonomyService
	logs *service.LogService
}

func NewAdminTagHandler(tags *service.TaxonomyService, logs *service.LogService) *AdminTagHandler {
	return &AdminTagHandler{tags: tags, logs: logs}
}

type tagRequest struct {
	Name string `json:"name" binding:"required"`
}

// Create handles POST /admin/tags.
func (h *AdminTagHandler) Create(c *gin.Context) {
	var req tagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写标签名称"})
		return
	}
	tag, err := h.tags.CreateTag(req.Name)
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryTaxonomy, "创建标签", req.Name, false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryTaxonomy, "创建标签",
		fmt.Sprintf("%s（#%d）", tag.Name, tag.ID), true)
	c.JSON(http.StatusCreated, gin.H{"tag": gin.H{
		"id":            tag.ID,
		"name":          tag.Name,
		"slug":          tag.Slug,
		"article_count": 0,
	}})
}

// Update handles PUT /admin/tags/:id.
func (h *AdminTagHandler) Update(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的标签 ID")
	if !ok {
		return
	}
	var req tagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写标签名称"})
		return
	}
	tag, err := h.tags.UpdateTag(id, req.Name)
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryTaxonomy, "更新标签", fmt.Sprintf("标签 #%d", id), false)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "标签不存在"})
			return
		}
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryTaxonomy, "更新标签",
		fmt.Sprintf("%s（#%d）", tag.Name, tag.ID), true)
	c.JSON(http.StatusOK, gin.H{"tag": gin.H{
		"id":   tag.ID,
		"name": tag.Name,
		"slug": tag.Slug,
	}})
}

// Delete handles DELETE /admin/tags/:id.
func (h *AdminTagHandler) Delete(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的标签 ID")
	if !ok {
		return
	}
	// 删除前先取名称，让审计日志记录删的是哪个标签
	name := h.tags.TagName(id)
	if err := h.tags.DeleteTag(id); err != nil {
		recordOp(h.logs, c, model.LogCategoryTaxonomy, "删除标签", fmt.Sprintf("标签 #%d", id), false)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "标签不存在"})
			return
		}
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryTaxonomy, "删除标签",
		fmt.Sprintf("%s（#%d）", name, id), true)
	c.Status(http.StatusNoContent)
}

// Merge handles POST /admin/tags/merge.
// Body: {"target_id": 3, "source_ids": [7,8]}
//
// 合并是不可逆的破坏性操作（来源标签被删除），但刻意不做二次确认弹窗：
// 那是前端的责任，后端只能拒绝非法请求。这里做的是把
// 「合并了多少篇文章的关联」写进操作日志——事后追溯时，
// 光有「执行了合并」四个字无法回答到底影响了哪些文章。
func (h *AdminTagHandler) Merge(c *gin.Context) {
	var req struct {
		TargetID  uint   `json:"target_id" binding:"required"`
		SourceIDs []uint `json:"source_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供目标标签与要合并的标签"})
		return
	}
	targetName := h.tags.TagName(req.TargetID)
	migrated, err := h.tags.MergeTags(req.TargetID, req.SourceIDs)
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryTaxonomy, "合并标签",
			fmt.Sprintf("并入 #%d，来源 %v", req.TargetID, req.SourceIDs), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryTaxonomy, "合并标签",
		fmt.Sprintf("并入「%s」（#%d），迁移 %d 个文章关联", targetName, req.TargetID, migrated), true)
	c.JSON(http.StatusOK, gin.H{"affected": migrated})
}
