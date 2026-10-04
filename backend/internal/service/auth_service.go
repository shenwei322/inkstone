package service

import (
	"errors"
	"regexp"
	"strings"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserBanned         = errors.New("该账号已被封禁，请联系管理员")
	ErrRegistrationClosed = errors.New("网站已关闭注册，请联系管理员")
	ErrValidation         = errors.New("validation failed")
)

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

type AuthService struct {
	users    *repository.UserRepository
	tokens   *TokenManager
	settings *SettingsService
}

func NewAuthService(users *repository.UserRepository, tokens *TokenManager, settings *SettingsService) *AuthService {
	return &AuthService{users: users, tokens: tokens, settings: settings}
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
	if l := len(password); l < 8 || l > 72 {
		return nil, nil, NewValidationError("密码长度需在 8-72 个字符之间")
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

// Login accepts either the account email or the username as identifier.
func (s *AuthService) Login(identifier, password string) (*model.User, *TokenPair, error) {
	user, err := s.users.FindByLogin(identifier)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, err
	}
	if user.IsBanned() {
		return nil, nil, ErrUserBanned
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	pair, err := s.tokens.GeneratePair(user.ID, user.Username, user.Role, user.TokenVersion)
	if err != nil {
		return nil, nil, err
	}
	return user, pair, nil
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
	if l := len(newPw); l < 8 || l > 72 {
		return NewValidationError("新密码长度需在 8-72 个字符之间")
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
