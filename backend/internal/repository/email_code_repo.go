package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"gorm.io/gorm"

	"github.com/shenwei/inkstone/backend/internal/model"
)

// MaxEmailCodeAttempts 是单枚验证码允许的错误次数上限。
//
// 放在 repository 层而不是 service：尝试上限最终由 SQL 判定
// （Consume 里 `attempts >= MaxEmailCodeAttempts`），值与实现同层，
// 避免调用方与服务层各持一份而漂移。service 层引用这个常量。
//
// 5 次不是随便定的：6 位数字空间 10^6，5 次盲猜命中概率约 5e-6。
// 真正的意义在于**把成本推回邮件通道**——作废后攻击者必须重新申请，
// 而申请有重发间隔与 IP 限流挡着。若允许无限次重试，
// 攻击者可在 TTL 内把这枚码的所有空间试完。
const MaxEmailCodeAttempts = 5

// EmailCodeRepository 负责邮箱验证码的持久化。
//
// 与内存 map 版的关键差别：**重发间隔的检查与写入必须原子**。
// 内存版是一把互斥锁包住「检查 + 写入」，天然原子；改成数据库后
// 若分成两次查询，两个并发请求会同时通过检查，各自发出一封信，
// 后写入的那枚顶掉先前的——用户拿着第一枚来校验必然失败。
//
// 这里用一条 UPSERT 同时完成检查与写入：WHERE 条件限定"不存在记录，
// 或已有记录已过重发间隔"，匹配不上就说明太频繁，一条都插不进去。
type EmailCodeRepository struct {
	db *gorm.DB
}

func NewEmailCodeRepository(db *gorm.DB) *EmailCodeRepository {
	return &EmailCodeRepository{db: db}
}

// HashEmailCode 把明文验证码转成存储用的哈希。
//
// 导出是因为 service 层校验时也要用同一函数哈希用户输入。
// 用 SHA-256 而非 bcrypt：6 位数字空间只有 10^6，慢哈希挡不住枚举，
// 真正的防护在 Attempts 与 IP 限流（见 model.EmailCode 注释）。
func HashEmailCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// UpsertIfNotRecent 写入一枚新验证码，但仅当该邮箱+用途满足：
//   - 此前没有记录，或
//   - 已有记录的 SentAt 早于 (now - resendInterval)
//
// 返回 (true, nil) 表示已写入；(false, nil) 表示发送过于频繁。
// 因为「检查」与「写入」是同一个语句，并发请求必然只有一个能成功。
//
// code 传明文，这里负责哈希——避免调用方不小心把明文存进别的路径。
//
// expiresAt 是调用方算好的绝对过期时刻（含当时的 TTL），
// 库里存死它，之后无论后台怎么调 TTL 都不影响已发出的码。
func (r *EmailCodeRepository) UpsertIfNotRecent(email, purpose, code string, sentAt, expiresAt time.Time, resendInterval time.Duration) (bool, error) {
	cutoff := sentAt.Add(-resendInterval)
	result := r.db.Exec(`
		INSERT INTO email_codes (email, purpose, code_hash, attempts, sent_at, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, 0, ?, ?, now(), now())
		ON CONFLICT (email, purpose) DO UPDATE
		SET code_hash = EXCLUDED.code_hash,
		    attempts   = 0,
		    sent_at    = EXCLUDED.sent_at,
		    expires_at = EXCLUDED.expires_at,
		    updated_at = now()
		WHERE email_codes.sent_at <= ?
	`, email, purpose, HashEmailCode(code), sentAt, expiresAt, cutoff)
	if result.Error != nil {
		return false, result.Error
	}
	// ON CONFLICT DO UPDATE 带 WHERE 时，条件不匹配则一行都不动，
	// RowsAffected 为 0。这正是"发送过于频繁"的信号。
	return result.RowsAffected > 0, nil
}

// Get 取某邮箱+用途的验证码记录，不存在返回 (nil, nil)。
func (r *EmailCodeRepository) Get(email, purpose string) (*model.EmailCode, error) {
	var rec model.EmailCode
	err := r.db.Where("email = ? AND purpose = ?", email, purpose).Take(&rec).Error
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &rec, nil
}

// Consume 校验并作废一枚验证码：比对一致且未过期则返回 true。
//
// **无条件删除**（不能把 expires_at 写进 WHERE）。过期时 DELETE 匹配
// 不上任何行，那一枚就永远留在表里，只能等 10 分钟的清理循环；
// 更糟的是它占着重发间隔——用户想重新申请会被挡回来，
// 而他手里那枚明明已经不能用了。与 POW 挑战是同一类坑。
//
// 删除与比对拆成两步的原因：RETURNING 会把整行带回来，省掉一次 SELECT。
// 两者仍在同一函数里，对调用方是原子的。
//
// 错误次数上限也在这一步判：达到上限的码即使比对正确也不放行，
// 要求用户重新走发信流程（那条路有间隔与 IP 限流挡着）。
func (r *EmailCodeRepository) Consume(email, purpose, codeHash string, now time.Time) (bool, error) {
	var rec model.EmailCode
	// Raw + Scan：DELETE...RETURNING 需要用 Raw()，Exec() 拿不到返回的列。
	err := r.db.Raw(`
		DELETE FROM email_codes
		WHERE email = ? AND purpose = ?
		RETURNING email, purpose, code_hash, attempts, sent_at, expires_at, created_at, updated_at
	`, email, purpose).Scan(&rec).Error
	if err != nil {
		return false, err
	}
	// 没有记录（从没申请过，或已被清掉）
	if rec.Email == "" {
		return false, nil
	}
	if now.After(rec.ExpiresAt) {
		return false, nil
	}
	if rec.Attempts >= MaxEmailCodeAttempts {
		return false, nil
	}
	// 常量时间由调用方或本层均可：这里直接比，反正记录已经删了
	return rec.CodeHash == codeHash, nil
}

// RecordFailure 记录一次错误尝试，返回该码累计的失败次数与是否已到上限。
//
// 与 Consume 分开：Consume 是"比对成功即删"，而失败路径要保留记录、
// 只累加计数。
//
// 用 UPDATE ... RETURNING 一步拿到新计数：若分成「读 attempts → +1 → 写回」，
// 两个并发请求会读到同一个旧值，写回同一个新值，实际少记一次，
// 等于变相放宽了尝试上限。
func (r *EmailCodeRepository) RecordFailure(email, purpose string, maxAttempts int) (int, bool, error) {
	var attempts int
	// Raw + Scan：UPDATE...RETURNING 需要用 Raw()，Exec() 拿不到返回的列。
	// RowAffected 为 0 表示没有记录（已过期被清掉，或从未申请）。
	row := r.db.Raw(`
		UPDATE email_codes
		SET attempts = attempts + 1, updated_at = now()
		WHERE email = ? AND purpose = ?
		RETURNING attempts
	`, email, purpose).Row()
	if err := row.Scan(&attempts); err != nil {
		return 0, false, err
	}
	return attempts, attempts >= maxAttempts, nil
}

// Invalidate 作废某邮箱+用途的验证码（用于错误次数过多、或发信失败的补偿）。
func (r *EmailCodeRepository) Invalidate(email, purpose string) error {
	return r.db.Where("email = ? AND purpose = ?", email, purpose).
		Delete(&model.EmailCode{}).Error
}

// ClearExpired 清理过期验证码，返回删除条数。
func (r *EmailCodeRepository) ClearExpired(now time.Time) (int64, error) {
	result := r.db.Where("expires_at <= ?", now).Delete(&model.EmailCode{})
	return result.RowsAffected, result.Error
}
