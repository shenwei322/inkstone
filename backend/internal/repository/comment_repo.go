package repository

import (
	"errors"

	"github.com/shenwei/inkstone/backend/internal/model"
	"gorm.io/gorm"
)

type CommentRepository struct {
	db *gorm.DB
}

func NewCommentRepository(db *gorm.DB) *CommentRepository {
	return &CommentRepository{db: db}
}

func (r *CommentRepository) Create(comment *model.Comment) error {
	if err := r.db.Create(comment).Error; err != nil {
		return err
	}
	// Reload with associations populated.
	return r.db.Preload("User").First(comment, comment.ID).Error
}

func (r *CommentRepository) FindByID(id uint) (*model.Comment, error) {
	var c model.Comment
	err := r.db.Preload("User").First(&c, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *CommentRepository) ListByArticle(articleID uint) ([]model.Comment, error) {
	var comments []model.Comment
	err := r.db.Preload("User").
		Where("article_id = ?", articleID).
		Order("created_at ASC").
		Find(&comments).Error
	return comments, err
}

func (r *CommentRepository) ListAll(page, pageSize int) ([]model.Comment, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var total int64
	if err := r.db.Model(&model.Comment{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var comments []model.Comment
	err := r.db.Preload("User").
		Preload("Article").
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&comments).Error
	return comments, total, err
}

func (r *CommentRepository) ListByUser(userID uint, page, pageSize int) ([]model.Comment, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var total int64
	if err := r.db.Model(&model.Comment{}).Where("user_id = ?", userID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var comments []model.Comment
	err := r.db.Preload("User").
		Preload("Article").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&comments).Error
	return comments, total, err
}

func (r *CommentRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// comments.parent_id 是自引用外键：父评论带着回复时直接删除会被数据库
		// 拒绝（SQLSTATE 23503）。先把回复的 parent_id 置空（提升为顶级评论，
		// 不丢内容），再删父评论本身。
		if err := tx.Model(&model.Comment{}).Where("parent_id = ?", id).
			Update("parent_id", nil).Error; err != nil {
			return err
		}
		res := tx.Delete(&model.Comment{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}
