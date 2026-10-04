package repository

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shenwei/inkstone/backend/internal/model"
	"gorm.io/gorm"
)

var (
	ErrNotFound      = errors.New("record not found")
	ErrEmailTaken    = errors.New("email already registered")
	ErrUsernameTaken = errors.New("username already taken")
	ErrInvalidInput  = errors.New("invalid input")
)

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// bootstrapAdminLockKey 是「空库首个用户授予 admin」这一判定的互斥键。
// 只需全库唯一，用一个固定魔数即可。
const bootstrapAdminLockKey int64 = 0x696E6B73746F6E65 // "inkstone"

// CreateFirstAdmin 在**同一事务**内完成「空库则授予 admin」的判定与插入。
//
// 为什么需要它：Register 原先用两次独立的 users.Count()——一次判注册开关、
// 一次定角色。空库上两个并发注册会读到同一个 count==0，于是**双双被授予
// admin**，构成权限提升。把「计数 → 定角色 → 插入」放进同一事务并持有
// pg_advisory_xact_lock，任一时刻只有一个请求能做出这个判定；锁随事务释放。
func (r *UserRepository) CreateFirstAdmin(user *model.User) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", bootstrapAdminLockKey).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.User{}).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			user.Role = model.RoleAdmin
		} else {
			user.Role = model.RoleUser
		}
		err := tx.Create(user).Error
		if err != nil {
			// 与 Create 保持一致的唯一冲突映射
			if uniqueField, ok := uniqueViolationField(err); ok {
				if uniqueField == "username" {
					return ErrUsernameTaken
				}
				return ErrEmailTaken
			}
		}
		return err
	})
}

func (r *UserRepository) Create(user *model.User) error {
	err := r.db.Create(user).Error
	if err != nil {
		if uniqueField, ok := uniqueViolationField(err); ok {
			if uniqueField == "username" {
				return ErrUsernameTaken
			}
			return ErrEmailTaken
		}
	}
	return err
}

// uniqueViolationField inspects a Postgres unique violation (SQLSTATE 23505)
// and extracts the constrained column name.
func uniqueViolationField(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch {
		case strings.Contains(pgErr.ConstraintName, "username"):
			return "username", true
		case strings.Contains(pgErr.ConstraintName, "email"):
			return "email", true
		}
		return "unknown", true
	}
	return "", false
}

// FindByLogin looks a user up by email or username (email是唯一的，用户名也唯一).
func (r *UserRepository) FindByLogin(identifier string) (*model.User, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, ErrNotFound
	}
	var user model.User
	err := r.db.
		Where("LOWER(email) = ? OR LOWER(username) = ?", strings.ToLower(identifier), strings.ToLower(identifier)).
		First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) FindByID(id uint) (*model.User, error) {
	var user model.User
	err := r.db.First(&user, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// FindByEmail 只按邮箱精确查找（忽略大小写）。忘记密码流程需要它：
// FindByLogin 接受邮箱或用户名，会把"用户名恰好等于别人的邮箱"这种情况
// 也匹配上，重置密码必须只认邮箱这一条身份标识。
func (r *UserRepository) FindByEmail(email string) (*model.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, ErrNotFound
	}
	var user model.User
	err := r.db.Where("LOWER(email) = ?", email).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) Count() (int64, error) {
	var n int64
	err := r.db.Model(&model.User{}).Count(&n).Error
	return n, err
}

func (r *UserRepository) List(page, pageSize int, query string) ([]model.User, int64, error) {
	db := r.db.Model(&model.User{})
	if query != "" {
		escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(query)
		like := "%" + escaped + "%"
		db = db.Where("email ILIKE ? OR username ILIKE ?", like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var users []model.User
	err := db.Order("id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&users).Error
	return users, total, err
}

func (r *UserRepository) UpdateRole(id uint, role string) error {
	// 同时自增令牌代次：降级后旧令牌里的 admin 角色不再可用
	// （Auth 中间件已改为以库中角色为准，这里是纵深防御 + 兼容旧令牌）。
	res := r.db.Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{
			"role":          role,
			"token_version": gorm.Expr("token_version + 1"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *UserRepository) UpdateStatus(id uint, status string) error {
	// 封禁时自增令牌代次，让被封禁账号的 refresh 令牌立即不可用
	// （access 令牌由 Auth 中间件的每请求校验拦截）。
	res := r.db.Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":        status,
			"token_version": gorm.Expr("token_version + 1"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *UserRepository) UpdatePasswordHash(id uint, hash string) error {
	res := r.db.Model(&model.User{}).Where("id = ?", id).Update("password_hash", hash)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdatePasswordHashAndRevoke 改密码并自增 TokenVersion，使该用户已签发的
// 全部令牌（access 与 refresh）立即失效。
//
// 用表达式 `token_version + 1` 交给数据库自增，避免「读-改-写」竞态导致
// 两次并发修改只加一次。
func (r *UserRepository) UpdatePasswordHashAndRevoke(id uint, hash string) error {
	res := r.db.Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{
			"password_hash": hash,
			"token_version": gorm.Expr("token_version + 1"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// BumpTokenVersion 仅自增令牌代次（封禁 / 降级 / 强制下线时调用）。
func (r *UserRepository) BumpTokenVersion(id uint) error {
	res := r.db.Model(&model.User{}).Where("id = ?", id).
		Update("token_version", gorm.Expr("token_version + 1"))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteWithArticles 在同一事务内删除该用户的全部文章与其账号。
//
// 此前 handler 先调 articleRepo.DeleteByAuthor 再调 users.Delete，是两个独立
// 事务：第二步失败会留下「文章已全删、用户还在」的不一致状态。
// 事务内先确认用户存在，让「用户不存在」能返回 404 而不是误删文章。
func (r *UserRepository) DeleteWithArticles(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.User{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return ErrNotFound
		}
		// 按外键依赖顺序清理：评论/点赞/标签关联 → 文章
		if err := purgeArticlesByAuthor(tx, id); err != nil {
			return err
		}
		return tx.Delete(&model.User{}, id).Error
	})
}

func (r *UserRepository) UpdateUsername(id uint, username string) error {
	err := r.db.Model(&model.User{}).Where("id = ?", id).Update("username", username).Error
	if err != nil {
		if uniqueField, ok := uniqueViolationField(err); ok && uniqueField == "username" {
			return ErrUsernameTaken
		}
		return err
	}
	return nil
}

func (r *UserRepository) UpdateEmail(id uint, email string) error {
	err := r.db.Model(&model.User{}).Where("id = ?", id).Update("email", email).Error
	if err != nil {
		if uniqueField, ok := uniqueViolationField(err); ok && uniqueField == "email" {
			return ErrEmailTaken
		}
		return err
	}
	return nil
}

func (r *UserRepository) Delete(id uint) error {
	res := r.db.Delete(&model.User{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordFailedLogin 递增某账号的登录失败计数，达到阈值时写入锁定时刻。
// 返回 true 表示"本次失败触发了锁定"（调用方可据此给出不同提示）。
//
// 用 SQL 表达式自增而非 Go 侧读-改-写：并发失败会互相覆盖，计数偏小。
// 计数与锁定在同一事务内完成，避免"计数已达阈值但锁没写上"的窗口
// （那会让下一次失败才触发锁定，等于多送一次免费尝试）。
func (r *UserRepository) RecordFailedLogin(id uint, threshold int, lockout time.Duration) (bool, error) {
	if threshold < 1 {
		threshold = 1
	}
	if lockout <= 0 {
		lockout = time.Minute
	}
	var locked bool
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.User{}).Where("id = ?", id).
			Update("failed_login_count", gorm.Expr("failed_login_count + 1")).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.User{}).Where("id = ?", id).
			Select("failed_login_count").Scan(&count).Error; err != nil {
			return err
		}
		if count < int64(threshold) {
			return nil
		}
		locked = true
		return tx.Model(&model.User{}).Where("id = ?", id).
			Updates(map[string]any{
				"failed_login_count": 0,
				"locked_until":       time.Now().Add(lockout),
			}).Error
	})
	return locked, err
}

// ResetLoginState 登录成功后清零失败计数并解除锁定。
func (r *UserRepository) ResetLoginState(id uint) error {
	return r.db.Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{
			"failed_login_count": 0,
			"locked_until":       nil,
		}).Error
}

// UnlockUser 是管理员手动解除账号锁定（清零计数 + 清空锁定时刻）。
func (r *UserRepository) UnlockUser(id uint) error {
	res := r.db.Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{
			"failed_login_count": 0,
			"locked_until":       nil,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateTOTP 写入两步验证密钥与开关。enabled=false 表示"密钥已生成但尚未
// 启用"——绑定流程的中间态，用户提交一个有效码后才由 EnableTOTP 真正开启。
func (r *UserRepository) UpdateTOTP(id uint, secret string, enabled bool) error {
	res := r.db.Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{
			"totp_secret":  secret,
			"totp_enabled": enabled,
			"burned_codes": "",
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkTOTPCodeUsed 记录一个已消费的 TOTP 时间步（counter=unix秒/30）。
// burned 以逗号分隔保存最近若干步，超出保留窗口的旧值由本方法顺手清理，
// 避免这个字段无限增长。重放窗口与 TTL 一致，故保留个数很少。
func (r *UserRepository) MarkTOTPCodeUsed(id uint, counter int64) error {
	var u model.User
	if err := r.db.Select("id", "burned_codes").Where("id = ?", id).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	// 只保留当前步及之后的记录；更早的步已超出重放窗口，无需再比对。
	kept := make([]string, 0, 8)
	for _, part := range strings.Split(u.BurnedCodes, ",") {
		if part == "" {
			continue
		}
		if n, err := strconv.ParseInt(part, 10, 64); err == nil && n >= counter {
			kept = append(kept, part)
		}
	}
	kept = append(kept, strconv.FormatInt(counter, 10))
	return r.db.Model(&model.User{}).Where("id = ?", id).
		Update("burned_codes", strings.Join(kept, ",")).Error
}
