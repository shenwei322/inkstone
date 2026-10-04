package model

import "time"

// EmailCode 是发往邮箱的一次性验证码。
//
// 与 POW 挑战同理：必须放共享存储。验证码跨请求存活（用户要去邮箱
// 收信、复制、再回来填），多副本部署下进程 A 发的码很可能由进程 B 校验。
// 内存 map 时 B 查不到，用户会看到「验证码已过期」——明明刚收到的码。
//
// 主键是 (email, purpose)：同一邮箱可以同时有一枚登录码和一枚重置密码码，
// 用户可能两个流程并行发起。若只用 email 作主键，后申请的那枚会顶掉
// 前一枚，用户拿着先收到的码来校验会莫名失败。
type EmailCode struct {
	Email   string `gorm:"size:255;primaryKey"`
	Purpose string `gorm:"size:32;primaryKey"`

	// CodeHash 存哈希而非明文：库里不落可直接读出的凭证。
	// 用 SHA-256 足够——6 位数字空间只有 10^6，bcrypt 的慢哈希在这里
	// 帮不上忙（暴力枚举本就可行），真正防住重试的是 Attempts 与 IP 限流。
	CodeHash string `gorm:"size:64;not null"`

	// Attempts 累计错误次数，达到上限即作废该码。
	// 只靠 6 位空间 + 5 次尝试仍不够：攻击者可重新申请不断续期。
	// 作废要求用户重新走一遍发信流程，成本回到邮件通道上。
	Attempts int `gorm:"not null;default:0"`

	// ExpiresAt 是**算好的绝对过期时刻**（而不是 expires-duration）。
	// 与 POW 挑战存 issued_at + ttl 不同：验证码的 TTL 会随后台设置改变，
	// 但已发出的码应始终按发出时的有效期走。算出来存死最直白。
	ExpiresAt time.Time `gorm:"not null;index"`

	// SentAt 记录实际发信成功时刻，用于固定的重发间隔判断。
	// 不能用 ExpiresAt 反推：管理员调大 TTL 会把判断点前移，
	// 同一邮箱可被立即重复发信（邮件轰炸）。
	SentAt time.Time `gorm:"not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// TableName 显式声明，避免 GORM 按结构体名推导。
func (EmailCode) TableName() string { return "email_codes" }
