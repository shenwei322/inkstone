package repository

import (
	"errors"

	"github.com/shenwei/inkstone/backend/internal/model"
	"gorm.io/gorm"
)

type LinkRepository struct {
	db *gorm.DB
}

func NewLinkRepository(db *gorm.DB) *LinkRepository {
	return &LinkRepository{db: db}
}

func (r *LinkRepository) Create(link *model.FriendLink) error {
	return r.db.Create(link).Error
}

// UpdateFields 只更新调用方显式提交的字段。
//
// 为什么不能沿用 Update(link *model.FriendLink)（内部是 db.Save）：
// Save 在主键非零时执行**全字段 UPDATE（含零值）**，而 handler 用值类型绑定
// JSON，无法区分「管理员没提交 check_url」与「提交了空串」——于是只想改个
// 友链名字，就会把检测页地址、图标、简介静默清成空值。
func (r *LinkRepository) UpdateFields(id uint, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	res := r.db.Model(&model.FriendLink{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Update writes back a fully-loaded link (used by the background health check,
// where the object really did come from the database and every field is valid).
func (r *LinkRepository) Update(link *model.FriendLink) error {
	res := r.db.Save(link)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *LinkRepository) Delete(id uint) error {
	res := r.db.Delete(&model.FriendLink{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *LinkRepository) FindByID(id uint) (*model.FriendLink, error) {
	var link model.FriendLink
	err := r.db.First(&link, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *LinkRepository) List() ([]model.FriendLink, error) {
	var links []model.FriendLink
	err := r.db.Order("sort_order ASC, id ASC").Find(&links).Error
	return links, err
}
