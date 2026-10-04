package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	PageTemplateDefault   = "default"   // 带侧边栏的常规页
	PageTemplateFullwidth = "fullwidth" // 通栏无侧边栏
	PageTemplateLanding   = "landing"   // 自定义着陆页（HTML 自由渲染）
)

// Page is a WordPress-style standalone page (about, links, contact...).
type Page struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Title     string    `gorm:"size:255;not null" json:"title"`
	Slug      string    `gorm:"uniqueIndex;size:255;not null" json:"slug"`
	Content   string    `gorm:"type:text" json:"content"`
	Template  string    `gorm:"size:30;not null;default:default" json:"template"`
	Status    string    `gorm:"size:20;not null;default:published" json:"status"`
	SortOrder int       `gorm:"not null;default:0" json:"sort_order"`
	ShowInNav bool      `gorm:"not null;default:false" json:"show_in_nav"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// DeletedAt：与 Article 同理由的软删除。删独立页不写这一列的话，
	// 误删「关于本站」这种页面只能靠数据库备份找回。
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (p *Page) IsPublished() bool {
	return p.Status == "published"
}
