package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	ArticleDraft     = "draft"
	ArticlePublished = "published"
	// ArticleScheduled 表示「等定时发布」。与 draft 的区别：
	// draft 是作者主动不发布，scheduled 是已决定发布、只等时间到。
	// 后台列表能据此区分「还没写完」和「等发布中」。
	ArticleScheduled = "scheduled"
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

	// Excerpt 是作者手写的摘要，留空时由后端从正文生成（service.ExcerptFor）。
	// 用它替代「每次都现场剥标签截断」：列表页每篇文章都要摘要，
	// 现场生成等于把同样的字符串处理重复 N 遍。
	Excerpt string `gorm:"type:text" json:"excerpt"`

	// IsPinned 置顶。排序时排在同批文章最前，仅影响展示顺序。
	IsPinned bool `gorm:"index;not null;default:false" json:"is_pinned"`

	// ViewPassword 是文章的访问密码哈希（bcrypt），空表示不设密码。
	//
	// 存哈希而不是明文：设了密码的文章，正文依然要通过详情接口取，
	// 明文密码一旦泄露，所有加密文章同时失守。json:"-" 让它在任何
	// 响应里都不出现，只通过 has_password 布尔值告知前端「这篇要密码」。
	ViewPassword string `gorm:"size:100" json:"-"`

	// ScheduledAt 定时发布时间。仅当 status=scheduled 时有效。
	// 到点由 PublishDue 扫描改为 published。
	ScheduledAt *time.Time `json:"scheduled_at"`

	// HasPassword 是查询期计算的「是否设了访问密码」，**不落库**
	// （gorm:"-"）。存在的理由：卡片接口（related / neighbors）用 Select
	// 指定列，拿不到 view_password 原值；而前端要在卡片上显示锁标识，
	// 不能为此把哈希查出来再传上去。由 articleCardColumns 的
	// CASE WHEN 输出，只在卡片路径上有值。
	HasPassword bool `gorm:"-" json:"-"`

	// DeletedAt 让「删除文章」变成软删除（只写入删除时刻，GORM 自动在
	// 所有查询里追加 deleted_at IS NULL）。
	//
	// 为什么改：此前是真删——同一事务里先清评论/点赞/标签关联再删文章。
	// 误删一篇长文会连它积累的全部评论与点赞一起消失，且没有任何撤销窗口。
	// 软删后文章从列表、详情、sitemap、RSS 中一并消失（访客视角等同删除），
	// 但管理员可以在「回收站」还原，或确认无误后彻底删除。
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
