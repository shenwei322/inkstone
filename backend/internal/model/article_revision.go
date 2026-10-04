package model

import "time"

// ArticleRevision 是文章的一个历史版本。
//
// 存在的理由：文章被反复编辑后，"上周那段论述是怎么写的"无法找回。
// 覆盖式更新让历史永久丢失，而作者经常需要回看或恢复某个版本
// （尤其是误删一大段、或想借鉴自己一个月前的写法）。
//
// 与 Article 分开存而不是在 Article 上加一列 JSON 历史：
//   - 版本数据量可能远大于文章本身（每次保存都存一份全文）；
//   - 拆表后主表查询不受影响，列表页不会因为历史堆积而变慢。
//
// 记录时机是**保存而非发布**：草稿阶段同样需要历史，
// 而"只在发布时记一版"会让发布前的所有编辑无从追溯。
type ArticleRevision struct {
	ID        uint     `gorm:"primaryKey" json:"id"`
	ArticleID uint     `gorm:"index;not null" json:"article_id"`
	Article   *Article `gorm:"foreignKey:ArticleID" json:"-"`
	// Version 是文章内的版本号，从 1 开始递增。
	// 用「第几版」而不是时间戳作为主标识：时间戳会重复，
	// 而管理员要的是「恢复到第 3 版」这种明确指令。
	Version int    `gorm:"not null" json:"version"`
	Title   string `gorm:"size:255;not null" json:"title"`
	Content string `gorm:"type:text;not null" json:"content"`
	Excerpt string `gorm:"type:text" json:"excerpt"`
	// EditorID 记录这次改动的作者。
	// 与 AuthorID 分开：管理员代改他人文章时，要能追溯是谁动的。
	EditorID uint `gorm:"not null" json:"editor_id"`
	Editor   User `gorm:"foreignKey:EditorID" json:"editor,omitempty"`
	// ChangeNote 是这次改动的说明，作者可填（「修正错别字」）。
	// 留空也允许——不是每次保存都值得写一句话。
	ChangeNote string    `gorm:"size:255" json:"change_note"`
	CreatedAt  time.Time `json:"created_at"`
}

// TableName 显式声明表名。GORM 默认会推导成 article_revisions（复数），
// 显式声明是为了与 Article、Page 等既有命名保持同一套规则，
// 也避免迁移时与预期表名不符。
func (ArticleRevision) TableName() string {
	return "article_revisions"
}

// MaxRevisionsKept 是每篇文章保留的版本数上限（导出供 repository 裁剪用）。
//
// 必须有上限：每版都存全文，一篇常改的文章几年下来能攒出几十 MB。
// 超限时删最旧的版本。50 版足够覆盖"回看一个月前的写法"这类真实需求，
// 再往前的版本作者自己也未必记得。
const MaxRevisionsKept = 50
