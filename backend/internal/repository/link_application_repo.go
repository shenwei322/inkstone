package repository

import (
	"errors"
	"strings"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"gorm.io/gorm"
)

// isNotFound 统一判定 GORM 记录不存在。
func isNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

type LinkApplicationRepository struct {
	db *gorm.DB
}

func NewLinkApplicationRepository(db *gorm.DB) *LinkApplicationRepository {
	return &LinkApplicationRepository{db: db}
}

func (r *LinkApplicationRepository) Create(app *model.FriendLinkApplication) error {
	return r.db.Create(app).Error
}

// ClaimForReview 把申请从 pending **抢占式**地改为目标状态。
//
// 返回第二个值表示是否抢占成功（即这条申请确实还是 pending）。WHERE 里带
// status 条件让数据库完成「检查 + 修改」的原子判定，避免 check-then-act：
// 管理员双击审核按钮时两个请求都会读到 pending，各建一条友链，
// 结果是同一条申请产生重复友链。
func (r *LinkApplicationRepository) ClaimForReview(id uint, newStatus string, operatorID uint, reason string) (bool, error) {
	now := time.Now()
	res := r.db.Model(&model.FriendLinkApplication{}).
		Where("id = ? AND status = ?", id, model.LinkAppPending).
		Updates(map[string]any{
			"status":      newStatus,
			"reason":      reason,
			"reviewed_by": operatorID,
			"reviewed_at": &now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *LinkApplicationRepository) Update(app *model.FriendLinkApplication) error {
	return r.db.Save(app).Error
}

func (r *LinkApplicationRepository) FindByID(id uint) (*model.FriendLinkApplication, error) {
	var app model.FriendLinkApplication
	err := r.db.First(&app, id).Error
	if isNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &app, nil
}

func (r *LinkApplicationRepository) Delete(id uint) error {
	res := r.db.Delete(&model.FriendLinkApplication{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// List 按状态过滤返回申请（status 为空返回全部），新的在前。
func (r *LinkApplicationRepository) List(status string) ([]model.FriendLinkApplication, error) {
	var apps []model.FriendLinkApplication
	q := r.db.Order("id DESC")
	if s := strings.TrimSpace(status); s != "" {
		q = q.Where("status = ?", s)
	}
	err := q.Find(&apps).Error
	return apps, err
}

// FindActiveByURL 查找同一 URL 下尚未处理的申请（pending）。
// 规范化规则：小写协议与主机、去尾部斜杠，与 service 层保持一致。
func (r *LinkApplicationRepository) FindActiveByURL(normalizedURL string) (*model.FriendLinkApplication, error) {
	var app model.FriendLinkApplication
	err := r.db.Where("status = ?", model.LinkAppPending).
		Where("lower(url) = ? OR lower(url) = ? OR lower(url) = ? OR lower(url) = ?",
			normalizedURL, normalizedURL+"/", strings.TrimSuffix(normalizedURL, "/"), strings.TrimSuffix(normalizedURL, "/")+"/").
		Order("id DESC").First(&app).Error
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &app, nil
}

// PendingCount 待审核数量（后台 tab 徽标用）。
func (r *LinkApplicationRepository) PendingCount() (int64, error) {
	var count int64
	err := r.db.Model(&model.FriendLinkApplication{}).
		Where("status = ?", model.LinkAppPending).Count(&count).Error
	return count, err
}
