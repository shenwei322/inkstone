package handler

import (
	"errors"
	"net/http"
	"time"

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
	// session 描述如何写/清会话 Cookie。零值时退化为「不写 Cookie」，
	// 让只跑单元测试的路径不必构造完整配置。
	session service.SessionCookieConfig
}

func NewAuthHandler(auth *service.AuthService, emailCode *service.EmailCodeService, captcha *service.CaptchaService, limiter *middleware.SlidingLimiter, logs *service.LogService) *AuthHandler {
	return &AuthHandler{auth: auth, emailCode: emailCode, captcha: captcha, limiter: limiter, logs: logs}
}

// SetSessionCookieConfig 注入会话 Cookie 配置（main.go 在装配阶段调用）。
//
// 用 setter 而不是改构造函数签名：NewAuthHandler 在测试里被大量直接调用，
// 多一个参数会让所有既有测试一起改，收益为零。
func (h *AuthHandler) SetSessionCookieConfig(cfg service.SessionCookieConfig) {
	h.session = cfg
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
	// RememberMe 请求一个长会话（前端「记住我」勾选框）。
	// 缺省 false 时行为与升级前完全一致。
	RememberMe bool `json:"remember_me"`
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

	// 注册即登录：同样下发会话 Cookie，否则新用户注册完一刷新又要重新登录
	h.issueSession(c, pair)

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

// issueSession 把 refresh 令牌写进 HttpOnly 会话 Cookie，并把它写进响应的
// Cache-Control，确保中间层不会把「带 Set-Cookie 的登录响应」缓存下来
// 派发给下一个访客（那会让后来者直接拿到别人的会话）。
func (h *AuthHandler) issueSession(c *gin.Context, pair *service.TokenPair) {
	if h.session.Name == "" || pair == nil {
		return
	}
	cfg := h.session
	if pair.SessionExpiresIn > 0 {
		cfg.TTL = time.Duration(pair.SessionExpiresIn) * time.Second
	}
	service.SetRefreshCookie(c, cfg, pair.RefreshToken)
	c.Header("Cache-Control", "no-store")
}

// clearSession 删除会话 Cookie。
func (h *AuthHandler) clearSession(c *gin.Context) {
	if h.session.Name == "" {
		return
	}
	service.ClearSessionCookie(c, h.session)
	c.Header("Cache-Control", "no-store")
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
		RememberMe: req.RememberMe,
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

	// 下发 HttpOnly 会话 Cookie：浏览器每次访问自动携带，即便
	// localStorage 被清空（隐私模式 / 移动端存储回收 / 用户清站点数据）
	// 也仍能续期，这是「每次访问都要重新登录」的根治点。
	h.issueSession(c, pair)

	h.logs.Record(service.Entry{
		UserID:    user.ID,
		Username:  user.Username,
		Category:  model.LogCategoryAuth,
		Action:    "登录成功",
		Detail:    sessionDetail(user.Role, req.RememberMe),
		IP:        middleware.ClientIP(c),
		UserAgent: c.Request.UserAgent(),
		Success:   true,
	})

	c.JSON(http.StatusOK, gin.H{
		"user":  toUserResponse(user),
		"token": pair,
	})
}

// sessionDetail 生成登录日志里的会话说明。
//
// 「是否记住我」必须写进审计日志：会话时长从 7 天变成 30 天是安全相关的
// 状态变更，事后追查「这个账号为什么三个月没登录还能操作」时要能解释。
func sessionDetail(role string, rememberMe bool) string {
	if rememberMe {
		return "角色：" + role + "（记住我：长会话）"
	}
	return "角色：" + role
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	var req refreshRequest
	// 会话 Cookie 优先，请求体作为兼容回退：老客户端只发 body，
	// 新客户端（浏览器）Cookie 由浏览器自动携带。
	token := ""
	if v, err := service.ReadRefreshCookie(c, h.session); err == nil {
		token = v
	}
	if token == "" {
		if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 refresh_token"})
			return
		}
		token = req.RefreshToken
	}

	user, pair, err := h.auth.Refresh(token)
	if err != nil {
		// 刷新失败分两种，处置必须不同：
		//   - 令牌确实无效/被撤销/账号没了 → 清 Cookie，让浏览器别再带着它
		//     反复重试（这是不可恢复的登录态问题）；
		//   - 其他错误（库暂时不可用等）→ **保留** Cookie，否则一次数据库
		//     抖动会把所有在线用户的会话一起抹掉，症状正是「每次访问都要重新登录」。
		if errors.Is(err, service.ErrTokenRevoked) ||
			errors.Is(err, service.ErrUserBanned) ||
			errors.Is(err, service.ErrInvalidRefreshToken) ||
			errors.Is(err, service.ErrUserVanished) {
			h.clearSession(c)
		}
		recordOp(h.logs, c, model.LogCategoryAuth, "刷新令牌失败", "refresh token 无效或已过期", false)
		errorResponse(c, err)
		return
	}

	// 续期时同步续写 Cookie：会话 Cookie 的 Max-Age 必须跟着新令牌走，
	// 否则会出现「令牌有效但 Cookie 已被浏览器按旧时间丢弃」。
	h.issueSession(c, pair)

	c.JSON(http.StatusOK, gin.H{
		"user":  toUserResponse(user),
		"token": pair,
	})
}

// Logout handles POST /auth/logout —— 清掉浏览器侧的会话 Cookie。
//
// 这是**无状态**登出：服务端不维护会话表，无法只作废「一个」refresh 令牌
// （要作废全部设备请走改密码/强制下线，那条路会自增 TokenVersion）。
// 对「用户点了退出登录」这个场景，清 Cookie + 前端丢令牌已经足够：
// 攻击者若另有令牌副本，本来也不受这次登出影响。
func (h *AuthHandler) Logout(c *gin.Context) {
	h.clearSession(c)
	recordOp(h.logs, c, model.LogCategoryAuth, "退出登录", "已清除本机会话", true)
	c.JSON(http.StatusOK, gin.H{"message": "已退出登录"})
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
