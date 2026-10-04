package model

import "time"

// PowChallenge 是已签发但尚未消费的 POW 挑战。
//
// 放一张表而不是进程内的 map：POW 的挑战要跨请求存活（前端先领挑战、
// 算几百毫秒到几秒、再提交），多副本部署下进程 A 签发的挑战很可能被
// 提交到进程 B。用内存 map 时 B 查不到，用户会莫名其妙地反复
// 「验证已失效，请重新验证」——错误提示指向客户端，根因却在服务端拓扑。
//
// 放表的代价可以忽略：POW 的本质是故意慢（客户端要真算），
// 多一次约 1ms 的数据库往返在总耗时里占比不到千分之一。
type PowChallenge struct {
	// Challenge 是主键：64 位十六进制随机串（32 字节 entropy）。
	Challenge string `gorm:"primaryKey;size:64"`

	// 以下六项是签发时刻的参数快照。验证时必须以快照为准，
	// 而不是重新读设置——后台随时调参数，若按新参数校验，
	// 领取挑战后改一次设置就会让所有进行中的验证失败。
	Difficulty int    `gorm:"not null;default:4"`
	MemMB      int    `gorm:"not null;default:8"`
	Rounds     int    `gorm:"not null;default:4"`
	MinEvents  int    `gorm:"not null;default:3"`
	Scene      string `gorm:"size:32;index"`

	// IssuedAt 与 TTLSeconds 一起判定过期。存秒数而不是
	// time.Duration：Duration 是 int64 纳秒，人工改库时看不出量级。
	IssuedAt time.Time `gorm:"not null;index"`
	// TTLSeconds 由签发时的设置决定（1-3600）。
	// 每次签发都按当时设置取值，而不是固定常量。
	TTLSeconds int `gorm:"not null"`
}

// TableName 显式声明，避免 GORM 按结构体名推导出复数表名。
func (PowChallenge) TableName() string { return "pow_challenges" }
