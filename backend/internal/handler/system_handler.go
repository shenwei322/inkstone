package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/service"
)

type SystemHandler struct {
	settings *service.SettingsService
	repoRoot string
}

func NewSystemHandler(settings *service.SettingsService, repoRoot string) *SystemHandler {
	return &SystemHandler{settings: settings, repoRoot: repoRoot}
}

// Info handles GET /api/v1/system/info.
func (h *SystemHandler) Info(c *gin.Context) {
	name, err := h.settings.Get(service.SettingSiteName)
	if err != nil {
		name = "Blog 平台"
	}
	c.JSON(http.StatusOK, gin.H{"info": service.BuildSystemInfo(name, h.repoRoot)})
}
