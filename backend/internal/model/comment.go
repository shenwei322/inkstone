package model

import (
	"strings"
	"time"
)

// 评论审核状态。
const (
	CommentPending  = "pending"  // 待审核（开启审核或命中敏感词）
	CommentApproved = "approved" // 已通过（默认，直接公开）
	CommentRejected = "rejected" // 已驳回（不公开，保留记录供追溯）
)

type Comment struct {
	ID        uint     `gorm:"primaryKey" json:"id"`
	ArticleID uint     `gorm:"index;not null" json:"article_id"`
	Article   *Article `gorm:"foreignKey:ArticleID" json:"article,omitempty"`
	// UserID 可空：游客评论没有账号，此时为 NULL。
	//
	// 用 *uint 而不是「0 表示游客」：0 在 PostgreSQL 里会去撞 users 表的
	// 外键约束（不存在 id=0 的用户），写入直接失败。NULL 才是「无关联」
	// 的正确表达，也让 user_id 外键约束继续对登录用户生效。
	UserID *uint `gorm:"index" json:"user_id"`
	User   *User `gorm:"foreignKey:UserID" json:"user,omitempty"`

	// 游客身份三列。仅当 UserID 为空时有值；登录用户的这些列恒为空，
	// 避免出现「既有账号又自称游客」的两套身份。
	GuestName  string `gorm:"size:64" json:"guest_name,omitempty"`
	GuestEmail string `gorm:"size:255" json:"-"` // 不公开：仅在后台审核视图可见
	GuestURL   string `gorm:"size:255" json:"guest_url,omitempty"`
	// ParentID 指向被回复的评论，形成两级或多级盖楼。为空表示顶级评论。
	// 删除父评论时会把回复的 parent_id 置空（提升为顶级），见 comment_repo.Delete。
	ParentID *uint `gorm:"index" json:"parent_id"`
	// Parent 是自引用关联，用于 Preload("Parent") 取回被回复的评论。
	//
	// 此前只有 ParentID 而缺这个字段，repository 里的 Preload("Parent")
	// 与 Preload("Parent.User") 一直没有生效（GORM 找不到该关联），
	// 前端因此拿不到 parent_author，"回复 @某某"永远显示不出来。
	Parent  *Comment `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	Content string   `gorm:"type:text;not null" json:"content"`
	Status  string   `gorm:"size:20;not null;default:approved;index" json:"status"`
	// IP 固化操作者地址（管理员审核时可判断是否同源刷评）。
	// 与 VisitorDay 只存哈希不同：这里需要可追溯，因此存原文，
	// 且不在任何公开接口里返回。
	IP        string    `gorm:"size:64" json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IsPending 报告评论是否尚未通过审核。
func (c *Comment) IsPending() bool { return c.Status == CommentPending }

// IsPublic 报告评论是否对外可见。只有 approved 的评论进入公开列表。
func (c *Comment) IsPublic() bool { return c.Status == CommentApproved }

// IsGuest 报告这条评论是否来自未登录访客。
//
// 判定只看 UserID 是否为空，不看 GuestName：即便昵称列因历史数据为空，
// 「没有账号」这个事实仍然成立，不该被显示成一条无主评论。
func (c *Comment) IsGuest() bool { return c.UserID == nil }

// AuthorID 返回评论作者的账号 ID（游客为 0）。
//
// 调用方需要「拿 ID 比较」的场景（如判断是否作者自评）用这个，
// 避免每处都写一遍 nil 检查。
func (c *Comment) AuthorID() uint {
	if c.UserID == nil {
		return 0
	}
	return *c.UserID
}

// DisplayName 返回评论者的展示名：登录用户用用户名，游客用其填写的昵称。
//
// 这是展示名的唯一来源——邮件通知、后台列表、前端都不应各自拼装，
// 否则「游客评论显示成空字符串」这类问题会在某个出口单独出现。
func (c *Comment) DisplayName() string {
	if c.UserID == nil {
		if name := strings.TrimSpace(c.GuestName); name != "" {
			return name
		}
		return "匿名访客"
	}
	if c.User != nil && c.User.Username != "" {
		return c.User.Username
	}
	return "未知用户"
}
