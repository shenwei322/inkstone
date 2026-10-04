package repository

import (
	"time"

	"gorm.io/gorm"

	"github.com/shenwei/inkstone/backend/internal/model"
)

// PowChallengeRepository 负责 POW 挑战的持久化。
//
// 与内存 map 版最大的区别是消费语义：内存版是「SELECT 再 delete」两条
// 语句，中间有竞态窗口；这里一条 `DELETE ... RETURNING` 同时完成
// 存在性判断、过期判断与删除。两个请求带同一个挑战并发进来时，
// 只有一条 SQL 能删到行，另一个自然拿到零行——这比加锁更可靠。
type PowChallengeRepository struct {
	db *gorm.DB
}

func NewPowChallengeRepository(db *gorm.DB) *PowChallengeRepository {
	return &PowChallengeRepository{db: db}
}

// Save 写入一条新挑战。
func (r *PowChallengeRepository) Save(c *model.PowChallenge) error {
	return r.db.Create(c).Error
}

// Take 原子地取走一条挑战：已过期则删除并返回 nil，否则返回被删的记录。
//
// **无条件删除、再判过期**。早先的写法把过期条件写进 WHERE：
//
//	WHERE challenge = ? AND issued_at > now() - ttl
//
// 那样没过期的行会被取走删除，而已过期的行**谁都删不掉**——提交过期
// 挑战只会返回"已失效"，那一行仍留在表里，只能等每 2 分钟的 GC。
// 提交过期挑战是零成本的攻击面：表会无限堆积。
//
// 现在的写法先用 challenge 定位删行，再在读取时判断是否过期。
// 代价是过期挑战也走一次 RETURNING（多传几十字节），
// 换来的是一条极限情况下的清理路径。
//
// **过期判定不能写成 `issued_at > now()`**：那是要求签发时间晚于当前时刻，
// 刚签发的记录满足不了，实测 100% 取不到任何挑战。
//
// 显式写 `::timestamptz`：`make_interval` 返回 interval，
// `timestamptz - interval` 在参数化查询下类型推导不成立，
// 会报 `operator does not exist: timestamptz > interval`（SQLSTATE 42883）。
//
// 不预存算好的绝对过期时间戳，而是 issued_at + ttl_seconds 两个字段：
// 后台若调整 TTL，旧的挑战应仍按各自签发时的 TTL 判定。
//
// 只能有这一条 DELETE：RETURNING 已经返回被删的整行。
// 若先 Exec 一条 DELETE 再 Raw 一条 RETURNING，第一条已经把行删了，
// 第二条只会拿到零行，症状是「挑战明明刚签发却说不存在」。
func (r *PowChallengeRepository) Take(challenge string, now time.Time) (*model.PowChallenge, error) {
	var out model.PowChallenge
	err := r.db.Raw(`
		DELETE FROM pow_challenges
		WHERE challenge = ?
		RETURNING challenge, difficulty, mem_mb, rounds, min_events, scene, issued_at, ttl_seconds
	`, challenge).Scan(&out).Error
	if err != nil {
		return nil, err
	}
	// Scan 零行时 GORM 不报错，靠 Challenge 是否为空判断。
	// Challenge 是主键且签发时保证非空，空串只可能来自「没删到行」。
	if out.Challenge == "" {
		return nil, nil
	}
	// 过期判定放到删完之后：行已经删了，这里只决定要不要把参数交出去。
	if out.IssuedAt.Add(time.Duration(out.TTLSeconds) * time.Second).Before(now) {
		return nil, nil
	}
	return &out, nil
}

// CountActive 返回仍未过期的挑战数。
//
// 上限检查用这个而不是 Count：一条 10 分钟 TTL 的挑战过期后还占着额度，
// 需要被后台清理后才能释放，期间用户会看到「人机验证服务繁忙」。
//
// 过期判定同样必须按 ttl_seconds 折算，不能写成 `issued_at > now()`
// ——那是要求行存于未来，会把所有正常挑战判成过期。
func (r *PowChallengeRepository) CountActive(now time.Time) (int64, error) {
	var n int64
	err := r.db.Model(&model.PowChallenge{}).
		Where("issued_at > ?::timestamptz - make_interval(secs => ttl_seconds)", now).Count(&n).Error
	return n, err
}

// DeleteExpired 清理已过期的挑战，返回删除条数。
//
// 过期条件用各行的 ttl_seconds 折算，而不是外传一个统一时刻——
// 不同挑战的 TTL 不同（后台改过设置），统一时刻会把短 TTL 的行判成未过期。
func (r *PowChallengeRepository) DeleteExpired(now time.Time) (int64, error) {
	result := r.db.Where("issued_at <= ?::timestamptz - make_interval(secs => ttl_seconds)", now).
		Delete(&model.PowChallenge{})
	return result.RowsAffected, result.Error
}

// Delete 删除指定挑战（签发后立即发现参数无效等场景）。
func (r *PowChallengeRepository) Delete(challenge string) error {
	return r.db.Where("challenge = ?", challenge).Delete(&model.PowChallenge{}).Error
}
