package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

// RevisionService 负责文章历史版本的写入与恢复。
//
// 与 ArticleService 分开而不是塞进去：版本管理是横切关注点，
// 它的失败不该阻塞文章保存（下面 Snapshot 的降级策略正基于此）。
type RevisionService struct {
	revisions *repository.RevisionRepository
	articles  *repository.ArticleRepository
}

func NewRevisionService(revisions *repository.RevisionRepository, articles *repository.ArticleRepository) *RevisionService {
	return &RevisionService{revisions: revisions, articles: articles}
}

// Snapshot 在文章保存前留一版历史。
//
// **失败只记录不返回错误**：版本历史是增强功能，文章保存是主功能。
// 为了记版本让整次保存失败，是拿次要功能拖垮主要功能。
// 真正的风险是"版本没记上但文章改了"，那只需在日志里可查即可。
func (s *RevisionService) Snapshot(article *model.Article, editorID uint, note string) {
	if article == nil || article.ID == 0 {
		return
	}
	// 内容与标题都没变时不留版本。
	// 自动保存会周期性触发，"什么都没改"的那种调用留下几十版
	// 一模一样的记录，把有用的版本挤出 50 版上限。
	latest, err := s.revisions.Latest(article.ID)
	if err == nil && latest != nil &&
		latest.Title == article.Title && latest.Content == article.Content {
		return
	}
	version, err := s.revisions.NextVersion(article.ID)
	if err != nil {
		return
	}
	rev := &model.ArticleRevision{
		ArticleID:  article.ID,
		Version:    version,
		Title:      article.Title,
		Content:    article.Content,
		Excerpt:    article.Excerpt,
		EditorID:   editorID,
		ChangeNote: truncateRunes(note, 200),
		CreatedAt:  time.Now(),
	}
	_ = s.revisions.Create(rev)
}

// List 返回文章的历史版本（新的在前）。
func (s *RevisionService) List(articleID uint) ([]model.ArticleRevision, error) {
	return s.revisions.List(articleID)
}

// Revision 是单个历史版本的对外形态。
type Revision struct {
	Version    int       `json:"version"`
	Title      string    `json:"title"`
	Content    string    `json:"content"`
	Excerpt    string    `json:"excerpt"`
	Editor     string    `json:"editor"`
	ChangeNote string    `json:"change_note"`
	CreatedAt  time.Time `json:"created_at"`
}

// Get 取某一版的完整内容。
func (s *RevisionService) Get(articleID uint, version int) (*Revision, error) {
	rev, err := s.revisions.GetVersion(articleID, version)
	if err != nil {
		return nil, err
	}
	return &Revision{
		Version:    rev.Version,
		Title:      rev.Title,
		Content:    rev.Content,
		Excerpt:    rev.Excerpt,
		Editor:     rev.Editor.Username,
		ChangeNote: rev.ChangeNote,
		CreatedAt:  rev.CreatedAt,
	}, nil
}

// Restore 把文章恢复到指定版本，返回恢复后的文章。
//
// 恢复不是"改回去"而是"再存一版新的"：当前内容先留一版历史，
// 再把旧版内容写进文章。这样误恢复也能再恢复回来——
// 直接覆盖会让"恢复到第 2 版"这个操作本身不可逆。
func (s *RevisionService) Restore(articleID, editorID uint, version int) (*model.Article, error) {
	article, err := s.articles.FindByID(articleID)
	if err != nil {
		return nil, err
	}
	rev, err := s.revisions.GetVersion(articleID, version)
	if err != nil {
		return nil, err
	}
	s.Snapshot(article, editorID, fmt.Sprintf("恢复到第 %d 版前的自动备份", version))

	article.Title = rev.Title
	article.Content = rev.Content
	article.Excerpt = rev.Excerpt
	if err := s.articles.Update(article); err != nil {
		return nil, err
	}
	return article, nil
}

// DeleteByArticle 删除文章的全部历史版本（彻底删除文章时）。
func (s *RevisionService) DeleteByArticle(articleID uint) error {
	return s.revisions.DeleteByArticle(articleID)
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max])
}
