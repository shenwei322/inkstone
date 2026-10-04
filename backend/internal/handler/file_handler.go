package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/middleware"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/service"
)

type FileHandler struct {
	files     *service.FileService
	publicURL string
	logs      *service.LogService
}

func NewFileHandler(files *service.FileService, publicURL string, logs *service.LogService) *FileHandler {
	return &FileHandler{files: files, publicURL: publicURL, logs: logs}
}

func (h *FileHandler) toResponse(asset *model.FileAsset) gin.H {
	// Files are served statically at /files/<stored_name> by the backend
	// origin (PUBLIC_API_URL, e.g. http://host:8080). When it is not
	// configured we return a relative path so the browser resolves it
	// against the API host.
	base := trimTrailingSlash(h.publicURL)
	return gin.H{
		"id":            asset.ID,
		"stored_name":   asset.StoredName,
		"original_name": asset.OriginalName,
		"size":          asset.Size,
		"mime_type":     asset.MimeType,
		"url":           base + "/files/" + url.PathEscape(asset.StoredName),
		"created_at":    asset.CreatedAt,
	}
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// List handles GET /admin/files?page=&page_size=&q=
func (h *FileHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	files, total, err := h.files.List(page, pageSize, c.Query("q"))
	if err != nil {
		errorResponse(c, err)
		return
	}
	items := make([]gin.H, 0, len(files))
	for i := range files {
		items = append(items, h.toResponse(&files[i]))
	}
	totalSize, _ := h.files.TotalSize()
	c.JSON(http.StatusOK, gin.H{
		"files":         items,
		"total":         total,
		"page":          page,
		"page_size":     pageSize,
		"total_size":    totalSize,
		"max_upload_mb": h.files.MaxUploadBytes() >> 20,
	})
}

// Upload handles POST /admin/files (multipart, field name "file").
func (h *FileHandler) Upload(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要上传的文件"})
		return
	}
	src, err := fileHeader.Open()
	if err != nil {
		errorResponse(c, err)
		return
	}
	defer src.Close()

	asset, err := h.files.Save(fileHeader.Filename, fileHeader.Size, src, current.ID)
	if err != nil {
		recordOp(h.logs, c, model.LogCategoryFile, "上传文件", fmt.Sprintf("%s（%.1f KB）", fileHeader.Filename, float64(fileHeader.Size)/1024), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryFile, "上传文件",
		fmt.Sprintf("%s（%.1f KB，#%d）", asset.OriginalName, float64(asset.Size)/1024, asset.ID), true)
	c.JSON(http.StatusCreated, gin.H{"file": h.toResponse(asset)})
}

// Download handles GET /admin/files/:id/download with optional speed limit.
func (h *FileHandler) Download(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	asset, err := h.files.Get(uint(id))
	if err != nil {
		errorResponse(c, err)
		return
	}
	f, err := h.files.Open(asset)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "文件不存在或已被删除"})
		return
	}
	defer f.Close()

	c.Header("Content-Disposition", contentDisposition(asset.OriginalName))
	c.Header("Content-Type", asset.MimeType)
	c.Header("Content-Length", strconv.FormatInt(asset.Size, 10))
	c.Header("X-Content-Type-Options", "nosniff")

	if _, err := service.CopyWithLimit(c.Writer, f, h.files.DownloadSpeedKB()); err != nil {
		// Headers already sent; nothing else we can do but stop.
		return
	}
}

// Delete handles DELETE /admin/files/:id.
func (h *FileHandler) Delete(c *gin.Context) {
	id, ok := parseUintParam(c, "id", "无效的 ID")
	if !ok {
		return
	}
	// 删除前先取文件名，让审计日志记录删的是哪个文件
	name := ""
	if asset, err := h.files.Get(uint(id)); err == nil {
		name = asset.OriginalName
	}
	if err := h.files.Delete(uint(id)); err != nil {
		recordOp(h.logs, c, model.LogCategoryFile, "删除文件", fmt.Sprintf("文件 #%d", id), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryFile, "删除文件",
		fmt.Sprintf("%s（#%d）", name, id), true)
	c.Status(http.StatusNoContent)
}

// contentDisposition builds an attachment header safe for non-ASCII names.
//
// 必须逐字符过滤不可打印字符：文件名来自用户上传（Save 只用 filepath.Base
// 去目录，不剥离 CR/LF），若把 \r\n 带进响应头即可造成响应头注入。
// ASCII 可见区间（0x20-0x7E）之外、以及两个分隔符 " 与 \ 一律替换为 _。
func contentDisposition(name string) string {
	ascii := make([]rune, 0, len(name))
	for _, r := range name {
		if r >= 0x20 && r < 0x7F && r != '"' && r != '\\' {
			ascii = append(ascii, r)
		} else {
			ascii = append(ascii, '_')
		}
	}
	return fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s",
		string(ascii), url.PathEscape(name))
}
