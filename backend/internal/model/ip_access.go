package model

import "time"

// =====================================================================
// IP 访问记录与封禁
//
// 与 model/stat.go 里的 VisitorDay 刻意分开：
//   VisitorDay 只存「IP 的哈希」，用于 UV 去重，是隐私保护优先的聚合数据；
//   这里存的是**明文 IP + 路径 + UA**，因为后台要能回答
//   「这个 IP 是谁、从哪来、打了哪些接口、要不要封」——哈希做不到这件事。
//
// 因此本表的保留期明显更短（默认 7 天，见 SettingIPGuardRetentionDays），
// 且写入走「分钟桶合并」而不是每请求一条（见 IPAccessLog 的说明）。
// =====================================================================

const (
	// IPBanSourceAuto 由访问频率超阈值自动触发
	IPBanSourceAuto = "auto"
	// IPBanSourceManual 由管理员在后台手动封禁
	IPBanSourceManual = "manual"
)

// IPAccessLog 是一条访客访问明细。
//
// 单请求一条记录会让表量级直接等于全站 QPS，因此写入侧做了「分钟桶合并」：
// 同一 IP、同一路径、同一分钟内只保留一行，命中次数累加到 Hits。
// 这样一行 = 一个 IP 在一分钟里对某个路径的真实请求数，
// 既不会把库打爆，也不会丢失「高频访问」这个最关键的判断依据。
type IPAccessLog struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	IP     string `gorm:"size:64;not null;index:idx_ip_logs_ip_time,priority:1;uniqueIndex:idx_ip_logs_bucket,priority:1" json:"ip"`
	Method string `gorm:"size:10" json:"method"`
	Path   string `gorm:"size:255;not null" json:"path"`
	// PathHash 参与唯一索引，而不是让 Path 原文进索引。
	// Postgres 的 btree 单行索引项上限约 2704 字节，而 path 是 255「字符」——
	// 中文路径按 UTF-8 算最多 1020 字节，叠上 ip 与时间戳后可能顶到上限，
	// 报错还是难以定位的 "index row size exceeds maximum"。
	// 存定长哈希既绕开该限制，也让索引更小更快。
	PathHash string `gorm:"size:32;not null;uniqueIndex:idx_ip_logs_bucket,priority:2" json:"-"`
	Status   int    `gorm:"not null;default:0" json:"status"`
	// Hits 是本分钟桶内该路径的请求次数。
	Hits int64 `gorm:"not null;default:1" json:"hits"`
	// BucketAt 是桶的起始时刻（截断到分钟），与 IP/PathHash 一起构成唯一键。
	// 唯一索引必须存在，否则 ON CONFLICT 累加就退化成插入重复行。
	BucketAt  time.Time `gorm:"not null;index:idx_ip_logs_ip_time,priority:2;uniqueIndex:idx_ip_logs_bucket,priority:3" json:"bucket_at"`
	UserAgent string    `gorm:"size:255" json:"user_agent"`
	Referer   string    `gorm:"size:255" json:"referer"`
	UserID    uint      `gorm:"index" json:"user_id"`
	Username  string    `gorm:"size:64" json:"username"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

func (IPAccessLog) TableName() string { return "ip_access_logs" }

// IPBan 是一条 IP 封禁记录（同一 IP 全表唯一，重复封禁走更新而不是新行）。
//
// ExpiresAt 为 nil 表示**永久封禁**；非 nil 且已过期时该行仍保留（便于审计
// 「这个 IP 曾经被封过」），但中间件不再拦截，后台列表会标成「已过期」。
// 之所以用「惰性过期」而不是到期删行：删除会丢掉封禁历史，
// 而「这个 IP 上周因为 CC 被自动封过 3 次」恰恰是判断要不要永久封的依据。
type IPBan struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	IP     string `gorm:"size:64;not null;uniqueIndex" json:"ip"`
	Reason string `gorm:"size:255" json:"reason"`
	Source string `gorm:"size:16;not null;default:manual;index" json:"source"`
	// Hits 是封禁时窗口内的请求数快照（自动封禁时填写，手动封禁为 0）
	Hits      int64      `gorm:"not null;default:0" json:"hits"`
	ExpiresAt *time.Time `gorm:"index" json:"expires_at"`
	// CreatedBy 是执行封禁的管理员用户名（自动封禁写 "system"）
	CreatedBy string    `gorm:"size:64" json:"created_by"`
	Note      string    `gorm:"size:255" json:"note"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (IPBan) TableName() string { return "ip_bans" }

// Active 报告该封禁在 now 时刻是否仍然生效。
func (b *IPBan) Active(now time.Time) bool {
	if b.ExpiresAt == nil {
		return true
	}
	return now.Before(*b.ExpiresAt)
}

// RemainingSeconds 返回剩余封禁秒数；永久封禁返回 -1，已过期返回 0。
func (b *IPBan) RemainingSeconds(now time.Time) int64 {
	if b.ExpiresAt == nil {
		return -1
	}
	if !now.Before(*b.ExpiresAt) {
		return 0
	}
	return int64(b.ExpiresAt.Sub(now).Seconds())
}
