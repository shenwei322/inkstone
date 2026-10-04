package model

import "time"

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

const (
	StatusActive = "active"
	StatusBanned = "banned"
)

type User struct {
	ID           uint   `gorm:"primaryKey" json:"id"`
	Email        string `gorm:"uniqueIndex;size:255;not null" json:"email"`
	Username     string `gorm:"uniqueIndex;size:64;not null" json:"username"`
	PasswordHash string `gorm:"size:255;not null" json:"-"`
	Role         string `gorm:"size:20;not null;default:user;index" json:"role"`
	Status       string `gorm:"size:20;not null;default:active;index" json:"status"`
	// TokenVersion 是令牌代次：改密码 / 封禁 / 强制下线时自增，
	// 旧令牌（refresh 与 access）因版本不匹配立即失效。
	// 计数器语义 —— 只增不减，避免回退到历史值让旧令牌复活。
	TokenVersion int64     `gorm:"not null;default:0" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (u *User) IsBanned() bool {
	return u.Status == StatusBanned
}
