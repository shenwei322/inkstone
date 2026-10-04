package handler

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/middleware"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/service"
)

// ExportHandler 暴露内容的导入导出。
//
// 与 BackupHandler 的分工见 service.ExportImportService 的说明：
// 那边是运维快照（15 张表、含账号与设置），这边是内容迁移（文章/分类/标签）。
type ExportHandler struct {
	exporter *service.ExportImportService
	logs     *service.LogService
}

func NewExportHandler(exporter *service.ExportImportService, logs *service.LogService) *ExportHandler {
	return &ExportHandler{exporter: exporter, logs: logs}
}

// Export handles GET /admin/system/export.
//
// 以附件形式返回 JSON：管理员点「导出内容」应当直接触发下载，
// 而不是让浏览器把一大段 JSON 渲染成白屏。
func (h *ExportHandler) Export(c *gin.Context) {
	bundle, err := h.exporter.Export()
	if err != nil {
		recordOp(h.logs, c, model.LogCategorySystem, "导出内容", "读取失败", false)
		errorResponse(c, err)
		return
	}
	data, err := service.Render(bundle)
	if err != nil {
		recordOp(h.logs, c, model.LogCategorySystem, "导出内容", "序列化失败", false)
		errorResponse(c, err)
		return
	}
	stamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("inkstone-content-%s.json", stamp)
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
	recordOp(h.logs, c, model.LogCategorySystem, "导出内容",
		fmt.Sprintf("%d 篇文章、%d 个分类、%d 个标签",
			bundle.ArticleCount, len(bundle.Categories), len(bundle.Tags)), true)
}

// maxImportBytes 限制导入文件大小。
// 300 篇文章的正文加在一起有几十 MB，1 MB 太小会误伤正常导出文件；
// 32 MB 足够容纳上限篇数，又不至于让一次上传把内存吃满。
const maxImportBytes = 32 << 20

// Import handles POST /admin/system/import.
//
// 只接受本系统导出的内容包（版本号必须匹配），见 service.Parse 的说明：
// 猜着解析比直接失败更危险。
func (h *ExportHandler) Import(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxImportBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		recordOp(h.logs, c, model.LogCategorySystem, "导入内容", "读取上传文件失败", false)
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取上传文件失败，文件可能超过 32 MB"})
		return
	}
	bundle, err := service.Parse(raw)
	if err != nil {
		recordOp(h.logs, c, model.LogCategorySystem, "导入内容", "解析失败", false)
		errorResponse(c, err)
		return
	}

	created, updated, skipped, err := h.exporter.Import(current.ID, bundle)
	if err != nil {
		recordOp(h.logs, c, model.LogCategorySystem, "导入内容",
			fmt.Sprintf("处理 %d 篇时中断", len(bundle.Articles)), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategorySystem, "导入内容",
		fmt.Sprintf("新建 %d、更新 %d、跳过 %d（共提交 %d）", created, updated, skipped, len(bundle.Articles)), true)
	c.JSON(http.StatusOK, gin.H{
		"created": created,
		"updated": updated,
		"skipped": skipped,
	})
}
