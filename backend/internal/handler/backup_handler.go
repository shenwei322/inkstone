package handler

import (
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/service"
)

// BackupHandler 暴露后台「备份与恢复」：列出快照、手动创建、下载、删除。
//
// 恢复（把快照写回数据库）刻意**不做成网页入口**：这份 JSON 快照不是
// pg_restore 能直接消费的格式，写回需要校验表结构与主键冲突，做错会把
// 现有数据覆盖成半份状态。正确流程是下载快照、在宿主上人工核对后再导入
// （或直接用 deploy/backup.sh 的 pg_dump + pg_restore）。
type BackupHandler struct {
	backups *service.BackupService
	logs    *service.LogService
}

func NewBackupHandler(backups *service.BackupService, logs *service.LogService) *BackupHandler {
	return &BackupHandler{backups: backups, logs: logs}
}

type createBackupRequest struct {
	Reason string `json:"reason"`
}

// List handles GET /admin/system/backups.
func (h *BackupHandler) List(c *gin.Context) {
	files, err := h.backups.List()
	if err != nil {
		errorResponse(c, err)
		return
	}
	totalSize := int64(0)
	for _, f := range files {
		totalSize += f.Size
	}
	c.JSON(http.StatusOK, gin.H{
		"backups":    files,
		"total":      len(files),
		"total_size": totalSize,
	})
}

// Create handles POST /admin/system/backups.
func (h *BackupHandler) Create(c *gin.Context) {
	var req createBackupRequest
	// 允许空 body：管理员点「立即备份」时前端可以不传参。
	_ = c.ShouldBindJSON(&req)
	file, err := h.backups.Create(req.Reason)
	if err != nil {
		recordOp(h.logs, c, model.LogCategorySystem, "创建备份", "快照生成失败", false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategorySystem, "创建备份",
		"生成快照 "+file.Name+"（"+formatBackupSize(file.Size)+"）", true)
	c.JSON(http.StatusCreated, gin.H{"backup": file})
}

// Download handles GET /admin/system/backups/:name/download.
//
// 走 http.ServeFile 而不是读进内存：快照可能几十 MB，整份载入再写给响应
// 会平白多占一倍内存。
func (h *BackupHandler) Download(c *gin.Context) {
	name := c.Param("name")
	path, err := h.backups.Path(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "备份文件不存在"})
		return
	}
	recordOp(h.logs, c, model.LogCategorySystem, "下载备份", "下载快照 "+name, true)
	c.Header("Content-Type", "application/gzip")
	c.Header("Content-Disposition", "attachment; filename="+filepath.Base(path))
	http.ServeFile(c.Writer, c.Request, path)
}

// Delete handles DELETE /admin/system/backups/:name.
func (h *BackupHandler) Delete(c *gin.Context) {
	name := c.Param("name")
	if err := h.backups.Delete(name); err != nil {
		recordOp(h.logs, c, model.LogCategorySystem, "删除备份", "删除快照 "+name+" 失败", false)
		c.JSON(http.StatusNotFound, gin.H{"error": "备份文件不存在"})
		return
	}
	recordOp(h.logs, c, model.LogCategorySystem, "删除备份", "删除快照 "+name, true)
	c.Status(http.StatusNoContent)
}

// formatBackupSize 把字节数格式化成「1.2 MB」这种可读形式（操作日志用）。
func formatBackupSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	value := float64(n) / float64(div)
	suffix := []string{"KB", "MB", "GB", "TB"}[exp]
	return strconv.FormatFloat(value, 'f', 1, 64) + " " + suffix
}
