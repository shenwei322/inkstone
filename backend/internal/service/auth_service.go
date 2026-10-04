package service

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserBanned         = errors.New("该账号已被封禁，请联系管理员")
	ErrRegistrationClosed = errors.New("网站已关闭注册，请联系管理员")
	ErrValidation         = errors.New("validation failed")
	// ErrTOTPRequired 表示该账号开启了两步验证、而本次登录没有提交有效码。
	// handler 据此返回 401 + need_totp 标记，前端转去渲染验证码输入框。
	ErrTOTPRequired  = errors.New("请输入两步验证码")
	ErrTOTPInvalid   = errors.New("两步验证码不正确")
	ErrResetCodeSent = errors.New("验证码已发送，请查收邮件")
)

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// 账户维度的登录失败锁定参数。与 IP 限流（middleware.SlidingLimiter）并存：
// IP 限流可被"猜 N 次 + 登一次自己的账号"重置预算绕过，账户计数则让
// 定向爆破在阈值后无条件失败，与来源 IP 无关。
const (
	maxFailedLogins    = 5
	loginLockoutWindow = 15 * time.Minute
)

type AuthService struct {
	users      *repository.UserRepository
	tokens     *TokenManager
	settings   *SettingsService
	emailCodes *EmailCodeService
}

func NewAuthService(users *repository.UserRepository, tokens *TokenManager, settings *SettingsService, emailCodes *EmailCodeService) *AuthService {
	return &AuthService{users: users, tokens: tokens, settings: settings, emailCodes: emailCodes}
}

type RegisterInput struct {
	Email    string
	Username string
	Password string
}

func (s *AuthService) Register(input RegisterInput) (*model.User, *TokenPair, error) {
	// First registered account always gets through (bootstrap admin).
	if count, err := s.users.Count(); err != nil || count > 0 {
		if !s.settings.AllowRegistration() {
			return nil, nil, ErrRegistrationClosed
		}
	}

	email := strings.ToLower(strings.TrimSpace(input.Email))
	username := strings.TrimSpace(input.Username)
	password := input.Password

	if !emailRegex.MatchString(email) {
		return nil, nil, NewValidationError("邮箱格式不正确")
	}
	if l := len([]rune(username)); l < 2 || l > 32 {
		return nil, nil, NewValidationError("用户名长度需在 2-32 个字符之间")
	}
	if err := CheckPasswordStrength(password, email, username); err != nil {
		return nil, nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, nil, err
	}

	// 角色由 repository 在持锁事务内决定：空库首个用户为 admin。
	// 不能在这里先 Count() 再决定——空库上两个并发注册会读到同一个
	// count==0，双双成为 admin（权限提升）。
	user := &model.User{
		Email:        email,
		Username:     username,
		PasswordHash: string(hash),
		Role:         model.RoleUser, // 兜底默认值，CreateFirstAdmin 会按空库判定覆盖
		Status:       model.StatusActive,
	}
	if err := s.users.CreateFirstAdmin(user); err != nil {
		switch {
		case errors.Is(err, repository.ErrEmailTaken):
			return nil, nil, NewValidationError("该邮箱已被注册")
		case errors.Is(err, repository.ErrUsernameTaken):
			return nil, nil, NewValidationError("该用户名已被占用")
		}
		return nil, nil, err
	}

	pair, err := s.tokens.GeneratePair(user.ID, user.Username, user.Role, user.TokenVersion)
	if err != nil {
		return nil, nil, err
	}
	return user, pair, nil
}

// LoginInput 是登录入参。Identifier 接受邮箱或用户名。
type LoginInput struct {
	Identifier string
	Password   string
	TOTPCode   string // 两步验证码；账号未启用 2FA 时忽略
}

// Login accepts either the account email or the username as identifier.
//
// 账户维度的失败计数在这里累计，与 IP 限流并存（理由见 maxFailedLogins 注释）。
// "用户不存在"与"密码错误"返回同一个 ErrInvalidCredentials——不回吐
// "邮箱未注册"，否则本接口可直接用来枚举本站的注册用户。
func (s *AuthService) Login(in LoginInput) (*model.User, *TokenPair, error) {
	user, err := s.users.FindByLogin(in.Identifier)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, err
	}
	if user.IsBanned() {
		return nil, nil, ErrUserBanned
	}
	if user.IsLocked() {
		return nil, nil, NewValidationError(fmt.Sprintf(
			"登录失败次数过多，账号已锁定，请 %d 分钟后再试", user.LockRemainingMinutes()))
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Password)); err != nil {
		// 累计失败并在达阈值时锁定。计数写入失败只记日志：限流仍在 IP 层生效，
		// 不会因为这次写入失败就把账户防线整个放开。
		locked, lerr := s.users.RecordFailedLogin(user.ID, maxFailedLogins, loginLockoutWindow)
		if lerr != nil {
			log.Printf("[auth] 记录登录失败计数失败 user=%d: %v", user.ID, lerr)
		}
		if locked {
			return nil, nil, NewValidationError(fmt.Sprintf(
				"登录失败次数过多，账号已锁定 %d 分钟", int(loginLockoutWindow.Minutes())))
		}
		return nil, nil, ErrInvalidCredentials
	}
	// 两步验证放在密码校验之后：未启用 2FA 的账号完全不受影响，也不必在
	// 密码之前先泄露"该账号是否开了 2FA"。
	if user.TOTPEnabled {
		if strings.TrimSpace(in.TOTPCode) == "" {
			return nil, nil, ErrTOTPRequired
		}
		counter := TOTPCounter(time.Now())
		if !ValidateTOTP(user.TOTPSecret, in.TOTPCode, counter, ParseBurnedCodes(user.BurnedCodes)) {
			return nil, nil, ErrTOTPInvalid
		}
		// TOTP 每 30 秒一步，同一步内的码只能用一次，否则被截获的码在
		// ±1 步窗口内可反复重放。写库失败不阻断登录——保护本来就不是绝对的。
		if err := s.users.MarkTOTPCodeUsed(user.ID, counter); err != nil {
			log.Printf("[auth] 标记 TOTP 已用失败 user=%d: %v", user.ID, err)
		}
	}
	// 登录成功清零失败计数并解除锁定
	if err := s.users.ResetLoginState(user.ID); err != nil {
		log.Printf("[auth] 重置登录状态失败 user=%d: %v", user.ID, err)
	}
	pair, err := s.tokens.GeneratePair(user.ID, user.Username, user.Role, user.TokenVersion)
	if err != nil {
		return nil, nil, err
	}
	return user, pair, nil
}

// RequestPasswordReset 走"忘记密码"流程：给该地址发一封重置验证码。
//
// 邮箱不存在时同样返回成功——否则本接口可用来枚举注册用户。真正的拒绝
// 只在验证码发送层面（SMTP 未配置 / 发送失败）显式报错，让用户知道去
// 找管理员而不是反复点提交。
func (s *AuthService) RequestPasswordReset(email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if !emailRegex.MatchString(email) {
		// 格式都不对，没有必要进入发信流程
		return NewValidationError("邮箱格式不正确")
	}
	if _, err := s.users.FindByEmail(email); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			log.Printf("[auth] 密码重置请求命中未注册邮箱 %s（按成功处理，避免枚举）", email)
			return nil
		}
		return err
	}
	return s.emailCodes.Send(email, PurposeResetPassword)
}

// ResetPassword 用邮箱验证码设置新密码，成功后撤销该账号所有已签发令牌。
//
// 撤销是必须的：改密往往意味着"账号可能已泄露"，旧 refresh 令牌在 7 天内
// 仍然可用的话，这次重置就白做了。
func (s *AuthService) ResetPassword(email, code, newPassword string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if err := s.emailCodes.Verify(PurposeResetPassword, email, code); err != nil {
		return err
	}
	user, err := s.users.FindByEmail(email)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return NewValidationError("该邮箱未注册")
		}
		return err
	}
	if err := CheckPasswordStrength(newPassword, user.Email, user.Username); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePasswordHashAndRevoke(user.ID, string(hash)); err != nil {
		return err
	}
	// 重置密码同时解除失败锁定：用户已经证明自己控制着邮箱。
	return s.users.ResetLoginState(user.ID)
}

// BeginTOTPSetup 生成两步验证密钥并写入库（尚未启用），返回 otpauth URI
// 供前端渲染二维码。重复调用会换一个新密钥——用户扫到旧码也没关系，
// 只有最后提交的那个有效码对应的密钥会被启用。
func (s *AuthService) BeginTOTPSetup(userID uint) (string, string, error) {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return "", "", err
	}
	if user.TOTPEnabled {
		return "", "", NewValidationError("两步验证已开启，请先关闭再重新绑定")
	}
	secret, err := GenerateTOTPSecret()
	if err != nil {
		return "", "", err
	}
	issuer := s.settingStr(SettingSiteName, "InkStone")
	if issuer == "" {
		issuer = "InkStone"
	}
	if err := s.users.UpdateTOTP(userID, secret, false); err != nil {
		return "", "", err
	}
	return secret, TOTPAuthURI(issuer, user.Email, secret), nil
}

// ConfirmTOTPSetup 校验用户扫码后提交的第一个码，通过则真正启用两步验证。
// 先写密钥后启用，就是为了这一步能拦住"密钥没抄对却开启了 2FA"——
// 那种情况下用户会被永久挡在登录之外。
func (s *AuthService) ConfirmTOTPSetup(userID uint, code string) error {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return err
	}
	if user.TOTPEnabled {
		return NewValidationError("两步验证已开启")
	}
	if strings.TrimSpace(user.TOTPSecret) == "" {
		return NewValidationError("请先获取绑定密钥")
	}
	if !ValidateTOTP(user.TOTPSecret, code, TOTPCounter(time.Now()), nil) {
		return ErrTOTPInvalid
	}
	return s.users.UpdateTOTP(userID, user.TOTPSecret, true)
}

// DisableTOTP 关闭两步验证。需要校验当前密码：光凭一个已过期的 access
// 令牌不该能关掉账号的第二道防线。
func (s *AuthService) DisableTOTP(userID uint, currentPassword string) error {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return NewValidationError("当前密码不正确")
	}
	if err := s.users.UpdateTOTP(userID, "", false); err != nil {
		return err
	}
	// 关闭防护也该让已签发的令牌作废：这次操作可能来自一个已泄露的会话。
	return s.users.BumpTokenVersion(userID)
}

// settingStr 读取一个字符串设置项，取不到时用 fallback。
func (s *AuthService) settingStr(key, fallback string) string {
	v, err := s.settings.Get(key)
	if err != nil || strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

// ErrTokenRevoked 表示令牌已被撤销（改密码 / 强制下线 / 封禁后自增了代次）。
var ErrTokenRevoked = errors.New("令牌已失效，请重新登录")

// Refresh 用 refresh token 换取新令牌对。
//
// 校验两件事，缺一不可：
//  1. 账号当前可用（存在、未被封禁）——库为准；
//  2. 令牌代次与库中 TokenVersion 一致——改密码/强制下线后旧 refresh
//     与 access 全部立即失效（此前 refresh 无撤销机制，7 天内可反复重放）。
func (s *AuthService) Refresh(refreshToken string) (*model.User, *TokenPair, error) {
	claims, err := s.tokens.Parse(refreshToken, TokenTypeRefresh)
	if err != nil {
		return nil, nil, errors.New("invalid refresh token")
	}
	user, err := s.users.FindByID(claims.UserID)
	if err != nil {
		return nil, nil, errors.New("user no longer exists")
	}
	if user.IsBanned() {
		return nil, nil, ErrUserBanned
	}
	if claims.TokenVersionOf() != user.TokenVersion {
		return nil, nil, ErrTokenRevoked
	}
	pair, err := s.tokens.GeneratePair(user.ID, user.Username, user.Role, user.TokenVersion)
	if err != nil {
		return nil, nil, err
	}
	return user, pair, nil
}

func (s *AuthService) GetUserByID(id uint) (*model.User, error) {
	return s.users.FindByID(id)
}

// ChangePassword verifies the current password then stores the new hash.
func (s *AuthService) ChangePassword(userID uint, currentPw, newPw string) error {
	user, err := s.users.FindByID(userID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPw)); err != nil {
		return NewValidationError("当前密码不正确")
	}
	newPw = strings.TrimSpace(newPw)
	if err := CheckPasswordStrength(newPw, user.Email, user.Username); err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(newPw)) == nil {
		return NewValidationError("新密码不能与当前密码相同")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	// 改密码同时自增令牌代次：其他设备上已签发的 access/refresh 全部立即失效
	return s.users.UpdatePasswordHashAndRevoke(userID, string(hash))
}

// UpdateProfile allows a user to rename themselves.
func (s *AuthService) UpdateUsername(userID uint, username string) error {
	username = strings.TrimSpace(username)
	if l := len([]rune(username)); l < 2 || l > 32 {
		return NewValidationError("用户名长度需在 2-32 个字符之间")
	}
	if err := s.users.UpdateUsername(userID, username); err != nil {
		if errors.Is(err, repository.ErrUsernameTaken) {
			return NewValidationError("该用户名已被占用")
		}
		return err
	}
	return nil
}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func NewValidationError(msg string) error {
	return &ValidationError{Message: msg}
}
