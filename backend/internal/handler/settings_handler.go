package handler

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/service"
	"github.com/shenwei/inkstone/backend/pkg/mailer"
)

type SettingsHandler struct {
	settings  *service.SettingsService
	mailer    *mailer.Mailer
	emailCode *service.EmailCodeService
	captcha   *service.CaptchaService
	logs      *service.LogService
}

func NewSettingsHandler(settings *service.SettingsService, mailClient *mailer.Mailer, emailCode *service.EmailCodeService, captcha *service.CaptchaService, logs *service.LogService) *SettingsHandler {
	return &SettingsHandler{settings: settings, mailer: mailClient, emailCode: emailCode, captcha: captcha, logs: logs}
}

// Get handles GET /admin/settings.
func (h *SettingsHandler) Get(c *gin.Context) {
	out, err := h.settings.AdminView()
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": out})
}

type updateSettingsRequest struct {
	Settings map[string]any `json:"settings" binding:"required"`
}

// Update handles PUT /admin/settings with a JSON object of key/value pairs.
func (h *SettingsHandler) Update(c *gin.Context) {
	var wrapper struct {
		Settings map[string]any `json:"settings" binding:"required"`
	}
	if err := c.ShouldBindJSON(&wrapper); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.settings.Update(wrapper.Settings); err != nil {
		recordOp(h.logs, c, model.LogCategorySetting, "更新设置", settingDetail(wrapper.Settings), false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategorySetting, "更新设置", settingDetail(wrapper.Settings), true)
	out, err := h.settings.AdminView()
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": out})
}

// settingDetail lists the changed setting keys — values are NEVER logged
// because many settings (SMTP password, captcha keys, tokens) are secrets.
func settingDetail(settings map[string]any) string {
	if len(settings) == 0 {
		return "（无变更项）"
	}
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return "变更项：" + strings.Join(keys, "、")
}

// SiteConfig handles GET /api/v1/site-config — non-sensitive settings for
// the frontend (registration switch, site name, etc).
// Cache-Control: no-store 必须设置：浏览器启发式缓存会让后台开启人机验证后，
// 前端仍读到旧的 geetest.enabled=false，导致不弹验证直接提交被拒。
func (h *SettingsHandler) SiteConfig(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	out, err := h.settings.Public()
	if err != nil {
		errorResponse(c, err)
		return
	}
	if h.emailCode != nil {
		out["email_code"] = h.emailCode.PublicConfig()
	}
	if h.captcha != nil {
		cfg := h.captcha.PublicConfig()
		// geetest 保持顶层键名不变（前台配置读取路径兼容），
		// 新增 lap / pow 配置与 captcha_provider 选择器
		out["geetest"] = cfg["geetest"]
		out["lap"] = cfg["lap"]
		out["pow"] = cfg["pow"]
		out["captcha_provider"] = cfg["provider"]
	}
	c.JSON(http.StatusOK, out)
}

type testMailRequest struct {
	To string `json:"to" binding:"required"`
}

// TestMail handles POST /admin/settings/test-mail.
func (h *SettingsHandler) TestMail(c *gin.Context) {
	var req testMailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写收件邮箱"})
		return
	}
	err := h.mailer.Send(req.To, "InkStone 测试邮件", "这是一封测试邮件，收到即说明 SMTP 配置成功。")
	if err != nil {
		recordOp(h.logs, c, model.LogCategorySetting, "发送测试邮件", "收件人 "+req.To, false)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	recordOp(h.logs, c, model.LogCategorySetting, "发送测试邮件", "收件人 "+req.To, true)
	c.JSON(http.StatusOK, gin.H{"message": "测试邮件已发送，请查收"})
}
