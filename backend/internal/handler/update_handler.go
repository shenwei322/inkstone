package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/middleware"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/service"
)

// UpdateHandler 暴露「系统更新」相关接口，全部挂在 /admin 下（需管理员令牌）。
type UpdateHandler struct {
	updates *service.UpdateService
	logs    *service.LogService
}

func NewUpdateHandler(updates *service.UpdateService, logs *service.LogService) *UpdateHandler {
	return &UpdateHandler{updates: updates, logs: logs}
}

// Status handles GET /admin/system/update — 版本信息、进度、历史、备份。
//
// 这里不发任何外部请求：联网检查由 POST /admin/system/update/check 显式触发，
// 所以后台每次轮询进度都不会打到 GitHub。
func (h *UpdateHandler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"update":  h.updates.Status(),
		"backups": h.updates.Backups(),
		"agent":   h.updates.AgentResult(),
	})
}

// Check handles POST /admin/system/update/check — 联网检查上游最新版本与更新日志。
func (h *UpdateHandler) Check(c *gin.Context) {
	status, err := h.updates.Check(c.Request.Context())
	if err != nil {
		recordOp(h.logs, c, model.LogCategorySystem, "检查系统更新", err.Error(), false)
		errorResponse(c, err)
		return
	}

	detail := "已是最新版本"
	if status.Version.UpdateAvail {
		if status.Source == "releases" {
			detail = "发现新版本 " + status.Version.LatestVersion
		} else {
			detail = "发现新版本 " + status.Version.Latest.Short
			if status.Version.Behind > 0 {
				detail += "，落后 " + strconv.Itoa(status.Version.Behind) + " 个提交"
			}
		}
	}
	recordOp(h.logs, c, model.LogCategorySystem, "检查系统更新", detail, true)
	c.JSON(http.StatusOK, gin.H{"update": status})
}

// Apply handles POST /admin/system/update/apply — 一键更新到指定提交或版本。
//
// 返回 202：下载 + 构建是分钟级操作，真正的进度通过 Status 轮询获取。
//
// 目标标识：commits 模式传 commit 哈希；releases 模式传版本 tag（v1.28.0），
// 也接受语义等价的 target 字段（与前端字段名解耦，避免前端改不动后端）。
func (h *UpdateHandler) Apply(c *gin.Context) {
	var req struct {
		Commit  string `json:"commit"`  // 目标提交（留空 = 上游最新）
		Target  string `json:"target"`  // 目标版本（releases 模式；优先于 commit）
		Confirm bool   `json:"confirm"` // 必须显式确认：这是不可逆的高危操作
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if !req.Confirm {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先确认更新操作"})
		return
	}

	target := strings.TrimSpace(req.Target)
	if target == "" {
		target = strings.TrimSpace(req.Commit)
	}
	if target == "" {
		// 允许「不先点检查直接更新」：这里补一次检查确定目标
		status, err := h.updates.Check(c.Request.Context())
		if err != nil {
			recordOp(h.logs, c, model.LogCategorySystem, "系统更新", err.Error(), false)
			errorResponse(c, err)
			return
		}
		if status.Source == "releases" {
			target = status.Version.LatestVersion
		} else {
			target = status.Version.Latest.Hash
		}
		if target == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "未能确定上游最新版本，请先执行一次检查更新"})
			return
		}
	}

	if err := h.updates.Apply(c.Request.Context(), target, usernameOf(c)); err != nil {
		recordOp(h.logs, c, model.LogCategorySystem, "系统更新", err.Error(), false)
		errorResponse(c, err)
		return
	}

	recordOp(h.logs, c, model.LogCategorySystem, "系统更新",
		"开始更新到 "+target, true)
	c.JSON(http.StatusAccepted, gin.H{
		"message": "更新任务已启动，请在页面查看进度",
		"update":  h.updates.Status(),
	})
}

// Rollback handles POST /admin/system/update/rollback — 回滚到指定备份。
func (h *UpdateHandler) Rollback(c *gin.Context) {
	var req struct {
		BackupID string `json:"backup_id" binding:"required"`
		Confirm  bool   `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 backup_id"})
		return
	}
	if !req.Confirm {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先确认回滚操作"})
		return
	}

	if err := h.updates.Rollback(req.BackupID, usernameOf(c)); err != nil {
		recordOp(h.logs, c, model.LogCategorySystem, "回滚系统更新", err.Error(), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategorySystem, "回滚系统更新", "备份 "+req.BackupID, true)
	c.JSON(http.StatusOK, gin.H{
		"message": "已回滚源码，需重新构建并重启后生效",
		"update":  h.updates.Status(),
	})
}

// usernameOf 取当前登录用户名（更新审计要知道是谁点的）。
func usernameOf(c *gin.Context) string {
	if user, ok := middleware.GetCurrentUser(c); ok {
		return user.Username
	}
	return ""
}
