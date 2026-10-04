package service

import (
	"errors"
	"strings"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
	"golang.org/x/crypto/bcrypt"
)

var ErrEmailTaken = errors.New("该邮箱已被注册")
var ErrUsernameTaken = errors.New("该用户名已被占用")

type AdminService struct {
	users    *repository.UserRepository
	articles *repository.ArticleRepository
}

func NewAdminService(users *repository.UserRepository, articles *repository.ArticleRepository) *AdminService {
	return &AdminService{users: users, articles: articles}
}

// DeleteUserWithArticles 删除用户及其全部文章（单事务，保证一致性）。
// handler 不再分别调用两个 repo——那会产生「文章已删、用户还在」的中间态。
func (s *AdminService) DeleteUserWithArticles(id uint) error {
	return s.users.DeleteWithArticles(id)
}

// UpdateUserRole 修改角色，并自增令牌代次让旧令牌里的角色声明立即失效
// （Auth 中间件已以库中角色为准，这里是纵深防御）。
func (s *AdminService) UpdateUserRole(id uint, role string) error {
	if role != model.RoleAdmin && role != model.RoleUser {
		return NewValidationError("无效的角色值")
	}
	return s.users.UpdateRole(id, role)
}

type Stats struct {
	TotalUsers     int64 `json:"total_users"`
	TotalArticles  int64 `json:"total_articles"`
	PublishedCount int64 `json:"published_articles"`
	DraftCount     int64 `json:"draft_articles"`
}

func (s *AdminService) Stats() (*Stats, error) {
	st := &Stats{}
	var err error
	if st.TotalUsers, err = s.users.Count(); err != nil {
		return nil, err
	}
	if st.TotalArticles, err = s.articles.CountAll(); err != nil {
		return nil, err
	}
	if st.PublishedCount, err = s.articles.CountByStatus("published"); err != nil {
		return nil, err
	}
	if st.DraftCount, err = s.articles.CountByStatus("draft"); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *AdminService) SetArticleStatus(id uint, status string) (*model.Article, error) {
	if status != model.ArticleDraft && status != model.ArticlePublished {
		return nil, NewValidationError("无效的状态值")
	}
	article, err := s.articles.FindByID(id)
	if err != nil {
		return nil, err
	}
	if article.Status != model.ArticlePublished && status == model.ArticlePublished && article.PublishedAt == nil {
		now := time.Now()
		article.PublishedAt = &now
	}
	article.Status = status
	if err := s.articles.Update(article); err != nil {
		return nil, err
	}
	return article, nil
}

func (s *AdminService) ListUsers(page, pageSize int, query string) ([]model.User, int64, error) {
	return s.users.List(page, pageSize, query)
}

type CreateUserInput struct {
	Email    string
	Username string
	Password string
	Role     string
}

func (s *AdminService) CreateUser(input CreateUserInput) (*model.User, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	username := strings.TrimSpace(input.Username)

	if !emailRegex.MatchString(email) {
		return nil, NewValidationError("邮箱格式不正确")
	}
	if l := len([]rune(username)); l < 2 || l > 32 {
		return nil, NewValidationError("用户名长度需在 2-32 个字符之间")
	}
	if l := len(input.Password); l < 8 || l > 72 {
		return nil, NewValidationError("密码长度需在 8-72 个字符之间")
	}
	role := input.Role
	if role != model.RoleAdmin && role != model.RoleUser {
		role = model.RoleUser
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &model.User{
		Email:        email,
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		Status:       model.StatusActive,
	}
	if err := s.users.Create(user); err != nil {
		switch {
		case errors.Is(err, repository.ErrEmailTaken):
			return nil, NewValidationError("该邮箱已被注册")
		case errors.Is(err, repository.ErrUsernameTaken):
			return nil, NewValidationError("该用户名已被占用")
		}
		return nil, err
	}
	return user, nil
}

func (s *AdminService) SetUserStatus(id uint, status string) error {
	if status != model.StatusActive && status != model.StatusBanned {
		return NewValidationError("无效的状态值")
	}
	return s.users.UpdateStatus(id, status)
}

type UpdateUserInput struct {
	Email    *string
	Username *string
	Password *string
}

// UpdateUser lets an administrator change a user's email, username and/or
// password. Empty pointer fields are left untouched.
func (s *AdminService) UpdateUser(id uint, input UpdateUserInput) (*model.User, error) {
	user, err := s.users.FindByID(id)
	if err != nil {
		return nil, err
	}

	if input.Email != nil {
		email := strings.ToLower(strings.TrimSpace(*input.Email))
		if !emailRegex.MatchString(email) {
			return nil, NewValidationError("邮箱格式不正确")
		}
		if email != user.Email {
			if err := s.users.UpdateEmail(id, email); err != nil {
				if errors.Is(err, repository.ErrEmailTaken) {
					return nil, NewValidationError("该邮箱已被注册")
				}
				return nil, err
			}
		}
	}

	if input.Username != nil {
		username := strings.TrimSpace(*input.Username)
		if l := len([]rune(username)); l < 2 || l > 32 {
			return nil, NewValidationError("用户名长度需在 2-32 个字符之间")
		}
		if username != user.Username {
			if err := s.users.UpdateUsername(id, username); err != nil {
				if errors.Is(err, repository.ErrUsernameTaken) {
					return nil, NewValidationError("该用户名已被占用")
				}
				return nil, err
			}
		}
	}

	if input.Password != nil && *input.Password != "" {
		pw := *input.Password
		if l := len(pw); l < 8 || l > 72 {
			return nil, NewValidationError("密码长度需在 8-72 个字符之间")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		// 管理员重置密码同样作废该用户已签发的全部令牌
		if err := s.users.UpdatePasswordHashAndRevoke(id, string(hash)); err != nil {
			return nil, err
		}
	}

	return s.users.FindByID(id)
}
