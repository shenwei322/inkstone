package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// =====================================================================
// IP 访问记录 / 封禁的数据访问层
//
// 这里刻意不引入「通用查询构造器」：IP 管理的查询形态只有三种
// （明细分页、按 IP 聚合、按 IP 取时间范围内总量），写清楚比抽象更有用。
// =====================================================================

const (
	// accessPathMaxRunes 与 model 里的 size:255 对齐。
	// 超长 path（攻击者可以发任意长的 URL）必须在这里先截断：
	// 直接写库会被 Postgres 拒绝，而一条坏的访问记录不该让整个请求链路报错。
	accessPathMaxRunes = 255
	accessUAMaxRunes   = 255
	accessRefererRunes = 255
)

type IPRepository struct {
	db *gorm.DB
}

func NewIPRepository(db *gorm.DB) *IPRepository {
	return &IPRepository{db: db}
}

// pathHash 是 path 的定长指纹，参与唯一索引（理由见 model.IPAccessLog.PathHash）。
func pathHash(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:16]) // 32 个十六进制字符
}

// BucketMinute 把时刻截断到分钟，作为聚合桶的键。
func BucketMinute(t time.Time) time.Time {
	return t.Truncate(time.Minute)
}

// RecordAccess 以「分钟桶」为单位累加一条访问记录。
//
// 语义：同一 IP、同一路径、同一分钟内多次请求 → 一行，Hits 递增。
// 第一次写入时带 Status / UA / Referer；后续命中只累加 Hits 并刷新 Status
// （状态码可能变化，比如先 200 后被封成 403 —— 展示最后一次更贴近现状）。
func (r *IPRepository) RecordAccess(entry model.IPAccessLog) error {
	entry.Path = truncateRunes(entry.Path, accessPathMaxRunes)
	entry.UserAgent = truncateRunes(entry.UserAgent, accessUAMaxRunes)
	entry.Referer = truncateRunes(entry.Referer, accessRefererRunes)
	entry.PathHash = pathHash(entry.Path)
	entry.BucketAt = BucketMinute(entry.BucketAt)
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "ip"}, {Name: "path_hash"}, {Name: "bucket_at"}},
		DoUpdates: clause.Assignments(map[string]any{
			"hits":   gorm.Expr("ip_access_logs.hits + 1"),
			"status": entry.Status,
		}),
	}).Create(&entry).Error
}

// IPAccessQuery 是明细分页的筛选条件。
type IPAccessQuery struct {
	IP       string
	Path     string
	From     *time.Time
	To       *time.Time
	Page     int
	PageSize int
}

func (r *IPRepository) applyAccessFilters(db *gorm.DB, q IPAccessQuery) *gorm.DB {
	if q.IP != "" {
		db = db.Where("ip = ?", strings.TrimSpace(q.IP))
	}
	if q.Path != "" {
		escaped := escapeLike(q.Path)
		db = db.Where("path ILIKE ?", "%"+escaped+"%")
	}
	if q.From != nil {
		db = db.Where("bucket_at >= ?", *q.From)
	}
	if q.To != nil {
		db = db.Where("bucket_at <= ?", *q.To)
	}
	return db
}

// ListAccess 返回访问明细分页（按桶时间倒序）。
func (r *IPRepository) ListAccess(q IPAccessQuery) ([]model.IPAccessLog, int64, error) {
	db := r.applyAccessFilters(r.db.Model(&model.IPAccessLog{}), q)

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 200 {
		q.PageSize = 30
	}

	var rows []model.IPAccessLog
	err := db.Order("bucket_at DESC, id DESC").
		Offset((q.Page - 1) * q.PageSize).
		Limit(q.PageSize).
		Find(&rows).Error
	return rows, total, err
}

// IPAggregate 是「按 IP 聚合」的一行结果，后台用它排「谁访问最多」。
type IPAggregate struct {
	IP        string    `json:"ip"`
	Requests  int64     `json:"requests"`
	Paths     int64     `json:"paths"`
	LastSeen  time.Time `json:"last_seen"`
	FirstSeen time.Time `json:"first_seen"`
	UserAgent string    `json:"user_agent"`
	Banned    bool      `json:"banned"`
}

// TopIPs 按「时间窗口内的请求总量」倒序返回 IP 排行。
//
// 用 SUM(hits) 而不是 COUNT(*) —— 桶合并之后一行代表多次请求，
// COUNT(*) 只会得到「活跃分钟数」，把最该被看见的高频 IP 排到后面去。
//
// UA 取该 IP 最近一条记录：用 MAX(bucket_at) 关联回来会比
// DISTINCT ON 更直观，且能用上 (ip, bucket_at) 复合索引。
func (r *IPRepository) TopIPs(from time.Time, limit int) ([]IPAggregate, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	var rows []IPAggregate
	err := r.db.Model(&model.IPAccessLog{}).
		Select(`
			ip,
			SUM(hits) AS requests,
			COUNT(DISTINCT path_hash) AS paths,
			MAX(bucket_at) AS last_seen,
			MIN(bucket_at) AS first_seen,
			MAX(user_agent) AS user_agent
		`).
		Where("bucket_at >= ?", from).
		Group("ip").
		Order("requests DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	// 一次性把封禁态并进结果，避免前端再发一轮 N 次查询。
	banned, err := r.activeBanSet()
	if err != nil {
		return rows, nil // 封禁表读失败不该让访问排行整体失败
	}
	for i := range rows {
		rows[i].Banned = banned[rows[i].IP]
	}
	return rows, nil
}

// HitsSince 返回某 IP 在 [since, now] 内的请求总量，供自动封禁判定使用。
func (r *IPRepository) HitsSince(ip string, since time.Time) (int64, error) {
	var total int64
	err := r.db.Model(&model.IPAccessLog{}).
		Select("COALESCE(SUM(hits), 0)").
		Where("ip = ? AND bucket_at >= ?", ip, since).
		Scan(&total).Error
	return total, err
}

// TotalRequestsSince 返回窗口内全站请求总量（所有 IP 之和）。
//
// 不能拿 TopIPs 的结果累加来充当总量：那只是一个榜单，
// 榜单之外的 IP 的请求会被整个漏掉，运维看到「总量 = 首名」会以为统计坏了。
func (r *IPRepository) TotalRequestsSince(since time.Time) (int64, error) {
	var total int64
	err := r.db.Model(&model.IPAccessLog{}).
		Select("COALESCE(SUM(hits), 0)").
		Where("bucket_at >= ?", since).
		Scan(&total).Error
	return total, err
}

// DistinctIPCount 返回窗口内出现过的不同 IP 数量。
func (r *IPRepository) DistinctIPCount(since time.Time) (int64, error) {
	var total int64
	err := r.db.Model(&model.IPAccessLog{}).
		Distinct("ip").
		Where("bucket_at >= ?", since).
		Count(&total).Error
	return total, err
}

// IPPathStat 是某个 IP 在单个路径上的访问统计。
type IPPathStat struct {
	Path     string    `json:"path"`
	Requests int64     `json:"requests"`
	LastSeen time.Time `json:"last_seen"`
	// Status 是该路径出现过的最大状态码。攻击者扫目录时大量 404 会在这里
	// 直接显形（正常访客的 path 集合里几乎不会出现成片的 4xx）。
	Status int `json:"status"`
}

// DistinctPaths 返回某 IP 在窗口内访问过的路径（按次数倒序），
// 后台展开单个 IP 时用来回答「它到底在扫什么」。
func (r *IPRepository) DistinctPaths(ip string, since time.Time, limit int) ([]IPPathStat, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	type pathRow struct {
		Path      string    `json:"path"`
		Requests  int64     `json:"requests"`
		LastSeen  time.Time `json:"last_seen"`
		StatusMax int       `json:"status"`
	}
	var rows []pathRow
	err := r.db.Model(&model.IPAccessLog{}).
		Select("path, SUM(hits) AS requests, MAX(bucket_at) AS last_seen, MAX(status) AS status_max").
		Where("ip = ? AND bucket_at >= ?", ip, since).
		Group("path").
		Order("requests DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]IPPathStat, 0, len(rows))
	for _, row := range rows {
		out = append(out, IPPathStat{
			Path:     row.Path,
			Requests: row.Requests,
			LastSeen: row.LastSeen,
			Status:   row.StatusMax,
		})
	}
	return out, nil
}

// ---------- 封禁 ----------

// UpsertBan 写入或更新一条封禁（同 IP 只保留一行，重复封禁即为覆盖）。
func (r *IPRepository) UpsertBan(ban *model.IPBan) error {
	now := time.Now()
	if ban.CreatedAt.IsZero() {
		ban.CreatedAt = now
	}
	ban.UpdatedAt = now
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "ip"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"reason", "source", "hits", "expires_at", "created_by", "note", "updated_at",
		}),
	}).Create(ban).Error
}

// FindBan 按 IP 取封禁记录；没封过返回 (nil, nil)。
func (r *IPRepository) FindBan(ip string) (*model.IPBan, error) {
	var ban model.IPBan
	err := r.db.Where("ip = ?", ip).First(&ban).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &ban, nil
}

// ActiveBans 返回当前生效的封禁（未过期或永久）。
//
// 中间件会把它整体装入内存缓存，因此这里条数需要有个上限：
// 正常站点同时生效的封禁是几十到几百条，取 5000 已远超实际；
// 超出时宁可截断也不要让一次缓存刷新把内存和数据库都拖住。
func (r *IPRepository) ActiveBans(limit int) ([]model.IPBan, error) {
	if limit <= 0 {
		limit = 5000
	}
	var rows []model.IPBan
	err := r.db.Where("expires_at IS NULL OR expires_at > ?", time.Now()).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// activeBanSet 是 ActiveBans 的集合版（只取 IP）。
func (r *IPRepository) activeBanSet() (map[string]bool, error) {
	rows, err := r.ActiveBans(5000)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(rows))
	for _, b := range rows {
		out[b.IP] = true
	}
	return out, nil
}

// IPBanQuery 是封禁列表的筛选条件。
type IPBanQuery struct {
	IP       string
	Source   string
	Active   *bool
	Page     int
	PageSize int
}

// ListBans 返回封禁列表分页（含已过期，前端会标注状态）。
func (r *IPRepository) ListBans(q IPBanQuery) ([]model.IPBan, int64, error) {
	db := r.db.Model(&model.IPBan{})
	if q.IP != "" {
		db = db.Where("ip = ?", strings.TrimSpace(q.IP))
	}
	if q.Source != "" {
		db = db.Where("source = ?", q.Source)
	}
	if q.Active != nil {
		if *q.Active {
			db = db.Where("expires_at IS NULL OR expires_at > ?", time.Now())
		} else {
			db = db.Where("expires_at IS NOT NULL AND expires_at <= ?", time.Now())
		}
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 || q.PageSize > 200 {
		q.PageSize = 30
	}

	var rows []model.IPBan
	err := db.Order("updated_at DESC, id DESC").
		Offset((q.Page - 1) * q.PageSize).
		Limit(q.PageSize).
		Find(&rows).Error
	return rows, total, err
}

// DeleteBan 按 IP 解封。返回受影响行数，0 表示本来就没封。
func (r *IPRepository) DeleteBan(ip string) (int64, error) {
	res := r.db.Where("ip = ?", ip).Delete(&model.IPBan{})
	return res.RowsAffected, res.Error
}

// CleanupAccess 删除早于 before 的访问明细，返回删除行数。
func (r *IPRepository) CleanupAccess(before time.Time) (int64, error) {
	res := r.db.Where("bucket_at < ?", before).Delete(&model.IPAccessLog{})
	return res.RowsAffected, res.Error
}

// CleanupExpiredBans 删除「已过期且超过保留期」的封禁记录。
// 只清很老的（默认 30 天前过期）——近期过期的记录仍有审计价值。
func (r *IPRepository) CleanupExpiredBans(before time.Time) (int64, error) {
	res := r.db.Where("expires_at IS NOT NULL AND expires_at < ?", before).Delete(&model.IPBan{})
	return res.RowsAffected, res.Error
}

// truncateRunes 按字符截断，避免切断 UTF-8 多字节序列。
func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
