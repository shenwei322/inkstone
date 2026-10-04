package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/service"
)

type LinkHandler struct {
	links *service.LinkService
	logs  *service.LogService
}

func NewLinkHandler(links *service.LinkService, logs *service.LogService) *LinkHandler {
	return &LinkHandler{links: links, logs: logs}
}

// publicLinkResponse hides the real URL when a link is unreachable.
func publicLinkResponse(link *model.FriendLink) gin.H {
	resp := gin.H{
		"id":          link.ID,
		"name":        link.Name,
		"icon_url":    link.IconURL,
		"description": link.Description,
		"available":   link.Available,
		"masked_url":  service.MaskURL(link.URL),
	}
	if link.Available {
		resp["url"] = link.URL
	}
	return resp
}

func adminLinkResponse(link *model.FriendLink) gin.H {
	return gin.H{
		"id":              link.ID,
		"name":            link.Name,
		"url":             link.URL,
		"check_url":       link.CheckURL,
		"icon_url":        link.IconURL,
		"description":     link.Description,
		"sort_order":      link.SortOrder,
		"available":       link.Available,
		"last_checked_at": link.LastCheckedAt,
		"created_at":      link.CreatedAt,
	}
}

// ListPublic handles GET /links — unreachable links are masked and not clickable.
func (h *LinkHandler) ListPublic(c *gin.Context) {
	links, err := h.links.List()
	if err != nil {
		errorResponse(c, err)
		return
	}
	items := make([]gin.H, 0, len(links))
	for i := range links {
		items = append(items, publicLinkResponse(&links[i]))
	}
	c.JSON(http.StatusOK, gin.H{"links": items})
}

type validateLinkRequest struct {
	URL      string `json:"url"`
	CheckURL string `json:"check_url"`
}

// Validate handles POST /admin/links/validate — pre-flight check before adding
// a friend link: reachability + whether the target page links back to this site.
func (h *LinkHandler) Validate(c *gin.Context) {
	var req validateLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供网站链接"})
		return
	}
	c.JSON(http.StatusOK, h.links.Validate(req.URL, req.CheckURL))
}

type linkRequest struct {
	Name        string `json:"name" binding:"required"`
	URL         string `json:"url" binding:"required"`
	CheckURL    string `json:"check_url"`
	IconURL     string `json:"icon_url"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

// linkUpdateRequest 用指针绑定，区分「未提交」与「提交空值」。
// 值类型绑定时未提交字段会变成零值，进而把检测页/图标/简介静默清空。
type linkUpdateRequest struct {
	Name        *string `json:"name"`
	URL         *string `json:"url"`
	CheckURL    *string `json:"check_url"`
	IconURL     *string `json:"icon_url"`
	Description *string `json:"description"`
	SortOrder   *int    `json:"sort_order"`
}

// ListAdmin handles GET /admin/links.
func (h *LinkHandler) ListAdmin(c *gin.Context) {
	links, err := h.links.List()
	if err != nil {
		errorResponse(c, err)
		return
	}
	items := make([]gin.H, 0, len(links))
	for i := range links {
		items = append(items, adminLinkResponse(&links[i]))
	}
	c.JSON(http.StatusOK, gin.H{"links": items})
}

// Create handles POST /admin/links.
func (h *LinkHandler) Create(c *gin.Context) {
	var req linkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写网站名称与链接"})
		return
	}
	link, err := h.links.Create(service.LinkInput{
		Name:        req.Name,
		URL:         req.URL,
		CheckURL:    req.CheckURL,
		IconURL:     req.IconURL,
		Description: req.Description,
		SortOrder:   req.SortOrder,
	})
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryLink, "添加友链", fmt.Sprintf("%s（%s）", req.Name, req.URL), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryLink, "添加友链",
		fmt.Sprintf("%s（#%d，%s）", link.Name, link.ID, link.URL), true)
	c.JSON(http.StatusCreated, gin.H{"link": adminLinkResponse(link)})
}

// Update handles PUT /admin/links/:id.
func (h *LinkHandler) Update(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	var req linkUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写网站名称与链接"})
		return
	}
	link, err := h.links.Update(uint(id), service.LinkUpdateInput{
		Name:        req.Name,
		URL:         req.URL,
		CheckURL:    req.CheckURL,
		IconURL:     req.IconURL,
		Description: req.Description,
		SortOrder:   req.SortOrder,
	})
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryLink, "更新友链", fmt.Sprintf("友链 #%d", id), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryLink, "更新友链",
		fmt.Sprintf("%s（#%d，%s）", link.Name, link.ID, link.URL), true)
	c.JSON(http.StatusOK, gin.H{"link": adminLinkResponse(link)})
}

// Delete handles DELETE /admin/links/:id.
func (h *LinkHandler) Delete(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	// 删除前先取名称，让审计日志记录删的是哪条友链
	name := ""
	if links, err := h.links.List(); err == nil {
		for i := range links {
			if links[i].ID == uint(id) {
				name = links[i].Name
				break
			}
		}
	}
	if err := h.links.Delete(uint(id)); err != nil {
		recordOp(h.logs, c, model.LogCategoryLink, "删除友链", fmt.Sprintf("友链 #%d", id), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryLink, "删除友链",
		fmt.Sprintf("%s（#%d）", name, id), true)
	c.Status(http.StatusNoContent)
}

// CheckAll handles POST /admin/links/check — manual full scan.
func (h *LinkHandler) CheckAll(c *gin.Context) {
	links, err := h.links.CheckAll()
	if err != nil {
		errorResponse(c, err)
		return
	}
	items := make([]gin.H, 0, len(links))
	for i := range links {
		items = append(items, adminLinkResponse(&links[i]))
	}
	c.JSON(http.StatusOK, gin.H{"links": items, "checked": len(items)})
}

// CheckOne handles POST /admin/links/:id/check.
func (h *LinkHandler) CheckOne(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	available, err := h.links.CheckOne(uint(id))
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"available": available})
}
