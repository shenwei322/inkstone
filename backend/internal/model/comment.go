package model

import "time"

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
	UserID    uint     `gorm:"index;not null" json:"user_id"`
	User      User     `gorm:"foreignKey:UserID" json:"user,omitempty"`
	// ParentID 指向被回复的评论，形成两级或多级盖楼。为空表示顶级评论。
	// 删除父评论时会把回复的 parent_id 置空（提升为顶级），见 comment_repo.Delete。
	ParentID *uint  `gorm:"index" json:"parent_id"`
	Content  string `gorm:"type:text;not null" json:"content"`
	Status   string `gorm:"size:20;not null;default:approved;index" json:"status"`
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
