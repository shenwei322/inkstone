package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	ArticleDraft     = "draft"
	ArticlePublished = "published"
)

type Article struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	AuthorID    uint       `gorm:"index;not null" json:"author_id"`
	Author      User       `gorm:"foreignKey:AuthorID" json:"author,omitempty"`
	CategoryID  *uint      `gorm:"index" json:"category_id"`
	Category    *Category  `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
	Title       string     `gorm:"size:255;not null" json:"title"`
	Slug        string     `gorm:"uniqueIndex;size:255;not null" json:"slug"`
	Content     string     `gorm:"type:text;not null" json:"content"`
	Status      string     `gorm:"size:20;not null;default:draft;index" json:"status"`
	Cover       string     `gorm:"size:500" json:"cover"` // 封面图（留空则前端取正文首图）
	Views       int64      `gorm:"not null;default:0" json:"views"`
	PublishedAt *time.Time `json:"published_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Tags        []Tag      `gorm:"many2many:article_tags" json:"tags,omitempty"`
	// DeletedAt 让「删除文章」变成软删除（只写入删除时刻，GORM 自动在
	// 所有查询里追加 deleted_at IS NULL）。
	//
	// 为什么改：此前是真删——同一事务里先清评论/点赞/标签关联再删文章。
	// 误删一篇长文会连它积累的全部评论与点赞一起消失，且没有任何撤销窗口。
	// 软删后文章从列表、详情、sitemap、RSS 中一并消失（访客视角等同删除），
	// 但管理员可以在「回收站」还原，或确认无误后彻底删除。
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
