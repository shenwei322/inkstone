package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/middleware"
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/service"
)

type AuthHandler struct {
	auth      *service.AuthService
	emailCode *service.EmailCodeService
	captcha   *service.CaptchaService
	limiter   *middleware.SlidingLimiter
	logs      *service.LogService
}

func NewAuthHandler(auth *service.AuthService, emailCode *service.EmailCodeService, captcha *service.CaptchaService, limiter *middleware.SlidingLimiter, logs *service.LogService) *AuthHandler {
	return &AuthHandler{auth: auth, emailCode: emailCode, captcha: captcha, limiter: limiter, logs: logs}
}

// resetAuthLimit clears the rate-limit budget for the caller after a
// successful authentication so normal usage (log in / out repeatedly,
// multiple tabs) is never throttled.
func (h *AuthHandler) resetAuthLimit(c *gin.Context) {
	if h.limiter == nil {
		return
	}
	if key := middleware.RateKey(c); key != "" {
		h.limiter.Reset(key)
	}
}

type registerRequest struct {
	Email     string `json:"email" binding:"required"`
	Username  string `json:"username" binding:"required"`
	Password  string `json:"password" binding:"required"`
	EmailCode string `json:"email_code"`
	service.CaptchaParams
}

type loginRequest struct {
	Email     string `json:"email" binding:"required"`
	Password  string `json:"password" binding:"required"`
	TOTPCode  string `json:"totp_code"`
	EmailCode string `json:"email_code"`
	service.CaptchaParams
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type userResponse struct {
	ID       uint   `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写完整的注册信息"})
		return
	}

	if err := h.captcha.Verify("register", req.CaptchaParams); err != nil {
		errorResponse(c, err)
		return
	}

	if err := h.emailCode.Verify("register", req.Email, req.EmailCode); err != nil {
		errorResponse(c, err)
		return
	}

	user, pair, err := h.auth.Register(service.RegisterInput{
		Email:    req.Email,
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		errorResponse(c, err)
		return
	}

	// 注册成功：清空该 IP 的计数，避免共享出口 IP 被误伤
	h.resetAuthLimit(c)

	h.logs.Record(service.Entry{
		UserID:    user.ID,
		Username:  user.Username,
		Category:  model.LogCategoryAuth,
		Action:    "新用户注册",
		Detail:    req.Email,
		IP:        middleware.ClientIP(c),
		UserAgent: c.Request.UserAgent(),
		Success:   true,
	})

	c.JSON(http.StatusCreated, gin.H{
		"user":  toUserResponse(user),
		"token": pair,
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写邮箱和密码"})
		return
	}

	if err := h.captcha.Verify("login", req.CaptchaParams); err != nil {
		errorResponse(c, err)
		return
	}

	if err := h.emailCode.Verify("login", req.Email, req.EmailCode); err != nil {
		errorResponse(c, err)
		return
	}

	user, pair, err := h.auth.Login(service.LoginInput{
		Identifier: req.Email,
		Password:   req.Password,
		TOTPCode:   req.TOTPCode,
	})
	if err != nil {
		// 两步验证缺码时单独标记：前端据此把密码框换成验证码输入框，
		// 而不是当作一次普通登录失败（否则用户会以为密码错了）。
		if errors.Is(err, service.ErrTOTPRequired) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error(), "need_totp": true})
			return
		}
		h.logs.Record(service.Entry{
			Category:  model.LogCategoryAuth,
			Action:    "登录失败",
			Detail:    req.Email,
			IP:        middleware.ClientIP(c),
			UserAgent: c.Request.UserAgent(),
			Success:   false,
		})
		errorResponse(c, err)
		return
	}

	// 登录成功：清空该 IP 的失败计数，避免正常用户被限流锁死
	h.resetAuthLimit(c)

	h.logs.Record(service.Entry{
		UserID:    user.ID,
		Username:  user.Username,
		Category:  model.LogCategoryAuth,
		Action:    "登录成功",
		Detail:    "角色：" + user.Role,
		IP:        middleware.ClientIP(c),
		UserAgent: c.Request.UserAgent(),
		Success:   true,
	})

	c.JSON(http.StatusOK, gin.H{
		"user":  toUserResponse(user),
		"token": pair,
	})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 refresh_token"})
		return
	}

	user, pair, err := h.auth.Refresh(req.RefreshToken)
	if err != nil {
		// 刷新令牌失败值得记录：可能是 token 伪造 / 过期重放的信号
		recordOp(h.logs, c, model.LogCategoryAuth, "刷新令牌失败", "refresh token 无效或已过期", false)
		errorResponse(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user":  toUserResponse(user),
		"token": pair,
	})
}

func (h *AuthHandler) Me(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	user, err := h.auth.GetUserByID(current.ID)
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": toUserResponse(user)})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required"`
}

// ChangePassword handles PUT /auth/password.
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写当前密码和新密码"})
		return
	}
	if err := h.auth.ChangePassword(current.ID, req.CurrentPassword, req.NewPassword); err != nil {
		recordOp(h.logs, c, model.LogCategoryAuth, "修改密码", "修改自己的登录密码", false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryAuth, "修改密码", "修改了自己的登录密码", true)
	c.JSON(http.StatusOK, gin.H{"message": "密码已更新"})
}

type updateProfileRequest struct {
	Username string `json:"username" binding:"required"`
}

// UpdateProfile handles PUT /auth/profile.
func (h *AuthHandler) UpdateProfile(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req updateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写用户名"})
		return
	}
	if err := h.auth.UpdateUsername(current.ID, req.Username); err != nil {
		recordOp(h.logs, c, model.LogCategoryAuth, "修改个人资料", "用户名改为 "+req.Username, false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryAuth, "修改个人资料", "用户名改为 "+req.Username, true)
	user, err := h.auth.GetUserByID(current.ID)
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": toUserResponse(user)})
}

// ============ 忘记密码 ============

type forgotPasswordRequest struct {
	Email string `json:"email" binding:"required"`
	service.CaptchaParams
}

// ForgotPassword handles POST /auth/password/forgot.
//
// 无论邮箱是否注册都返回同一条成功提示（枚举防护见
// AuthService.RequestPasswordReset）。人机验证按 "login" 场景判定——
// 忘记密码是登录流程的延伸，共用同一个开关最简单也不意外。
func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var req forgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写邮箱"})
		return
	}
	if err := h.captcha.Verify("login", req.CaptchaParams); err != nil {
		errorResponse(c, err)
		return
	}
	if err := h.auth.RequestPasswordReset(req.Email); err != nil {
		recordOp(h.logs, c, model.LogCategoryAuth, "请求重置密码", req.Email, false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryAuth, "请求重置密码", req.Email, true)
	c.JSON(http.StatusOK, gin.H{"message": "如果该邮箱已注册，我们已发送重置验证码，请查收邮件"})
}

type resetPasswordRequest struct {
	Email       string `json:"email" binding:"required"`
	Code        string `json:"code" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// ResetPassword handles POST /auth/password/reset.
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req resetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写邮箱、验证码和新密码"})
		return
	}
	if err := h.auth.ResetPassword(req.Email, req.Code, req.NewPassword); err != nil {
		recordOp(h.logs, c, model.LogCategoryAuth, "重置密码", req.Email, false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryAuth, "重置密码", req.Email, true)
	c.JSON(http.StatusOK, gin.H{"message": "密码已重置，请使用新密码登录"})
}

// ============ 两步验证（TOTP） ============

type totpCodeRequest struct {
	Code     string `json:"code" binding:"required"`
	Password string `json:"password"`
}

// BeginTOTPSetup handles POST /auth/2fa/setup — 生成密钥并返回 otpauth URI。
func (h *AuthHandler) BeginTOTPSetup(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	secret, uri, err := h.auth.BeginTOTPSetup(current.ID)
	if err != nil {
		errorResponse(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"secret": secret, "otpauth_uri": uri})
}

// ConfirmTOTPSetup handles POST /auth/2fa/confirm — 提交第一个有效码后启用。
func (h *AuthHandler) ConfirmTOTPSetup(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req totpCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写验证码"})
		return
	}
	if err := h.auth.ConfirmTOTPSetup(current.ID, req.Code); err != nil {
		recordOp(h.logs, c, model.LogCategoryAuth, "开启两步验证", "提交的验证码未通过校验", false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryAuth, "开启两步验证", "已为本账号开启两步验证", true)
	c.JSON(http.StatusOK, gin.H{"message": "两步验证已开启"})
}

// DisableTOTP handles DELETE /auth/2fa — 需校验当前密码。
func (h *AuthHandler) DisableTOTP(c *gin.Context) {
	current, ok := middleware.GetCurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req totpCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写当前密码"})
		return
	}
	if err := h.auth.DisableTOTP(current.ID, req.Password); err != nil {
		recordOp(h.logs, c, model.LogCategoryAuth, "关闭两步验证", "密码校验未通过", false)
		errorResponse(c, err)
		return
	}
	recordOp(h.logs, c, model.LogCategoryAuth, "关闭两步验证", "已关闭两步验证", true)
	c.JSON(http.StatusOK, gin.H{"message": "两步验证已关闭"})
}

func toUserResponse(u *model.User) gin.H {
	return gin.H{
		"id":           u.ID,
		"email":        u.Email,
		"username":     u.Username,
		"role":         u.Role,
		"totp_enabled": u.TOTPEnabled,
		"locked_until": u.LockedUntil,
	}
}
