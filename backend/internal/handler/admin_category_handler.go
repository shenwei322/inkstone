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

// AdminCategoryHandler 分类层级管理接口。
//
// 与 AdminTagHandler 同构：只做参数绑定、调 service、映射响应，
// 校验与错误文案都在 service.TaxonomyService。
type AdminCategoryHandler struct {
	categories *service.TaxonomyService
	logs       *service.LogService
}

func NewAdminCategoryHandler(categories *service.TaxonomyService, logs *service.LogService) *AdminCategoryHandler {
	return &AdminCategoryHandler{categories: categories, logs: logs}
}

type categoryRequest struct {
	Name     string `json:"name" binding:"required"`
	Slug     string `json:"slug"` // 空则按 name 生成
	ParentID uint   `json:"parent_id"`
}

// Create handles POST /admin/categories.
func (h *AdminCategoryHandler) Create(c *gin.Context) {
	var req categoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写分类名称"})
		return
	}
	slug := req.Slug
	if slug == "" {
		slug = repository.Slugify(req.Name)
	}
	cat, err := h.categories.CreateCategory(req.Name, slug, req.ParentID)
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryTaxonomy, "创建分类", req.Name, false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryTaxonomy, "创建分类",
		fmt.Sprintf("%s（#%d）", cat.Name, cat.ID), true)
	c.JSON(http.StatusCreated, gin.H{"category": toCategoryItem(cat)})
}

// Update handles PUT /admin/categories/:id.
func (h *AdminCategoryHandler) Update(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的分类 ID")
	if !ok {
		return
	}
	var req categoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写分类名称"})
		return
	}
	cat, err := h.categories.UpdateCategory(id, req.Name, req.ParentID)
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryTaxonomy, "更新分类", fmt.Sprintf("分类 #%d", id), false)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "分类不存在"})
			return
		}
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryTaxonomy, "更新分类",
		fmt.Sprintf("%s（#%d）", cat.Name, cat.ID), true)
	c.JSON(http.StatusOK, gin.H{"category": toCategoryItem(cat)})
}

// Delete handles DELETE /admin/categories/:id.
func (h *AdminCategoryHandler) Delete(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的分类 ID")
	if !ok {
		return
	}
	if err := h.categories.DeleteCategory(id); err != nil {
		recordOp(h.logs, c, model.LogCategoryTaxonomy, "删除分类", fmt.Sprintf("分类 #%d", id), false)
		if errors.Is(err, repository.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "分类不存在"})
			return
		}
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryTaxonomy, "删除分类", fmt.Sprintf("分类 #%d", id), true)
	c.Status(http.StatusNoContent)
}

// ListTree handles GET /categories/tree —— 公开的分类树。
//
// 与 ArticleHandler 的 /categories（扁平列表，供后台管理）分开：
// 前台导航和归档页需要带层级关系的树。
func (h *AdminCategoryHandler) ListTree(c *gin.Context) {
	tree, err := h.categories.CategoryTree()
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"categories": tree})
}

// toCategoryItem 把分类转成响应项。
// parent_id 是 *uint，直接序列化会得到数字或 null，前端两种都要兼容；
// 这里统一成「有父级返回数字、顶级返回 0」，让前端少一层判空。
func toCategoryItem(cat *model.Category) gin.H {
	parentID := uint(0)
	if cat.ParentID != nil {
		parentID = *cat.ParentID
	}
	return gin.H{
		"id":         cat.ID,
		"name":       cat.Name,
		"slug":       cat.Slug,
		"parent_id":  parentID,
		"created_at": cat.CreatedAt,
	}
}
