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
	TokenVersion int64 `gorm:"not null;default:0" json:"-"`
	// FailedLoginCount / LockedUntil 是账户维度的登录失败计数与锁定时刻。
	//
	// 为什么需要：原有登录限流只按 IP 计数，且登录成功后会把该 IP 的计数清零
	// （见 handler.resetAuthLimit）。攻击者掺入一次自己的合法登录即可重置预算，
	// 于是"猜 N 次 + 登自己账号"的循环能无限重试。账户维度计数让定向爆破
	// 在阈值后直接失败，与来源 IP 无关。登录成功时清零。
	FailedLoginCount int        `gorm:"not null;default:0" json:"-"`
	LockedUntil      *time.Time `gorm:"index" json:"-"`
	// TOTPSecret / TOTPEnabled 是两步验证（TOTP，RFC 6238）的密钥与开关。
	// 密钥在启用前先写入但不置 Enabled——绑定流程是"生成密钥 → 用户扫码 →
	// 提交一个当前码 → 校验通过才启用"，避免密钥写错却直接开启把用户锁死。
	// BurnedCodes 记录已使用的 TOTP 时间步，防同一步内的码被重放。
	TOTPSecret  string    `gorm:"size:255;not null;default:''" json:"-"`
	TOTPEnabled bool      `gorm:"not null;default:false" json:"-"`
	BurnedCodes string    `gorm:"type:text;not null;default:''" json:"-"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (u *User) IsBanned() bool {
	return u.Status == StatusBanned
}

// IsLocked 报告账号当前是否处于登录失败锁定中。锁定时长为 0（永锁）
// 语义上不会出现——锁定总是带一个未来的解锁时刻。
func (u *User) IsLocked() bool {
	return u.LockedUntil != nil && time.Now().Before(*u.LockedUntil)
}

// LockRemainingMinutes 返回剩余锁定时长（向上取整分钟，至少 1），
// 用于给用户一个不会"显示 0 分钟还得再等"的提示。
func (u *User) LockRemainingMinutes() int {
	if u.LockedUntil == nil {
		return 0
	}
	d := time.Until(*u.LockedUntil)
	if d <= 0 {
		return 0
	}
	minutes := int((d + time.Minute - 1) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	return minutes
}
