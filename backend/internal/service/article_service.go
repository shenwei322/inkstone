package service

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

var ErrForbidden = errors.New("forbidden")

type ArticleService struct {
	articles *repository.ArticleRepository
	taxonomy *repository.TaxonomyRepository
	// revisions 可为 nil：版本历史是增强功能，未注入时全部快照调用
	// 静默跳过（见 snapshot）。不为了一个可选功能改构造签名。
	revisions RevisionSnapshotter
}

// RevisionSnapshotter 是 ArticleService 依赖的最小版本历史接口。
// 定义为接口而不是直接用 *RevisionService：避免两个 service 循环依赖
// （RevisionService 也要用 ArticleRepository），也让单测能打桩。
type RevisionSnapshotter interface {
	Snapshot(article *model.Article, editorID uint, note string)
}

// NewArticleService 保持原有签名不变（不破坏现有调用方），
// 版本历史通过 SetRevisions 注入。
func NewArticleService(articles *repository.ArticleRepository, taxonomy *repository.TaxonomyRepository) *ArticleService {
	return &ArticleService{articles: articles, taxonomy: taxonomy}
}

// SetRevisions 注入版本历史能力。未调用时 Snapshot 全部为空操作。
func (s *ArticleService) SetRevisions(r RevisionSnapshotter) {
	s.revisions = r
}

// snapshot 在文章被改动前留一版历史。
//
// 防御写在这里而不是依赖 RevisionService.Snapshot 自己判 nil：
// RevisionSnapshotter 是接口，将来换实现（比如批量版、异步版）时，
// 这里的调用方仍保证传入的是可用的文章。契约由调用方守住，
// 不必每个实现各写一遍判空。
func (s *ArticleService) snapshot(article *model.Article, editorID uint, note string) {
	if s.revisions == nil || article == nil || article.ID == 0 {
		return
	}
	s.revisions.Snapshot(article, editorID, note)
}

type ArticleInput struct {
	Title      string
	Content    string
	Status     string
	CategoryID *uint
	TagNames   []string
	Cover      string
	Excerpt    string
	IsPinned   bool
	// ViewPassword 明文传入，由 service 哈希后存储。空串表示不设密码。
	ViewPassword string
	// ScheduledAt 定时发布时间，仅在 Status=scheduled 时有效。
	ScheduledAt *time.Time
}

type ArticleUpdate struct {
	Title      *string
	Content    *string
	Status     *string
	CategoryID **uint
	TagNames   *[]string
	Cover      *string
	Excerpt    *string
	IsPinned   *bool
	// ViewPassword 传空串表示「清除密码」；nil 表示本次未提交、保持原值。
	ViewPassword *string
	ScheduledAt  **time.Time
}

// ErrPasswordTooLong 密码长度上限：bcrypt 只取前 72 字节，超长部分被静默截断，
// 用户以为设了长密码其实只生效前 72 字节。提前拒绝比"存了但用不了"好。
var ErrPasswordTooLong = NewValidationError("文章密码最长 32 个字符")

func (s *ArticleService) Create(authorID uint, input ArticleInput) (*model.Article, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, NewValidationError("标题不能为空")
	}
	if len([]rune(title)) > 200 {
		return nil, NewValidationError("标题最长 200 个字符")
	}
	if strings.TrimSpace(input.Content) == "" {
		return nil, NewValidationError("内容不能为空")
	}

	status, err := normalizeArticleStatus(input.Status, input.ScheduledAt)
	if err != nil {
		return nil, err
	}

	if input.CategoryID != nil {
		if _, err := s.taxonomy.FindCategoryByID(*input.CategoryID); err != nil {
			return nil, NewValidationError("分类不存在")
		}
	}

	// 密码哈希放在校验分类之后：分类不存在时不该白算一次 bcrypt（慢哈希）。
	viewPassword, err := hashViewPassword(input.ViewPassword)
	if err != nil {
		return nil, err
	}

	article := &model.Article{
		AuthorID:     authorID,
		CategoryID:   input.CategoryID,
		Title:        title,
		Slug:         s.uniqueSlug(repository.Slugify(title), 0),
		Content:      SanitizeHTML(input.Content),
		Status:       status,
		Cover:        resolveCover(input.Cover, input.Content),
		Excerpt:      resolveExcerpt(input.Excerpt, input.Content),
		IsPinned:     input.IsPinned,
		ViewPassword: viewPassword,
		ScheduledAt:  input.ScheduledAt,
	}
	if status == model.ArticlePublished {
		now := time.Now()
		article.PublishedAt = &now
	}
	if err := s.articles.Create(article); err != nil {
		return nil, err
	}

	// 标签在事务外先解析成行：FindOrCreateTags 会插入新 tag，若后面的文章
	// 写入失败，最多留下一个无人引用的 tag（无害），比留下半篇文章好。
	var tags []model.Tag
	if len(input.TagNames) > 0 {
		var err error
		if tags, err = s.taxonomy.FindOrCreateTags(input.TagNames); err != nil {
			return nil, err
		}
	}
	if len(tags) > 0 {
		if err := s.articles.ReplaceTags(article, tags); err != nil {
			return nil, err
		}
	}
	return article, nil
}

// normalizeArticleStatus 校验状态值，并处理定时发布的落地时间。
//
// scheduled 允许传入过去的时刻：作者有时会为了"现在就发"而选一个刚过去的时间。
// 那种情况直接按已发布处理，避免文章永远等在一个已过去的时刻上——
// 发布器每分钟扫一次，但没必要让用户等那一分钟。
func normalizeArticleStatus(status string, scheduledAt *time.Time) (string, error) {
	switch status {
	case model.ArticleDraft, model.ArticlePublished:
		return status, nil
	case model.ArticleScheduled:
		if scheduledAt == nil || scheduledAt.IsZero() {
			return "", NewValidationError("定时发布需要指定发布时间")
		}
		if !scheduledAt.After(time.Now()) {
			return model.ArticlePublished, nil
		}
		return model.ArticleScheduled, nil
	default:
		return model.ArticleDraft, nil
	}
}

// hashViewPassword 把明文密码哈希后存储；空串返回空串（不设密码）。
func hashViewPassword(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if len([]rune(plain)) > 32 {
		return "", ErrPasswordTooLong
	}
	return HashPassword(plain)
}

// CheckViewPassword 校验文章访问密码。
// 未设密码的文章返回 true（无需密码即可访问）。
func CheckViewPassword(article *model.Article, plain string) bool {
	if article.ViewPassword == "" {
		return true
	}
	return CheckPassword(plain, article.ViewPassword)
}

// uniqueSlug 保证 slug 唯一：若已被其他文章占用，自动追加 -2、-3 … 后缀。
// excludeID 为当前文章 ID（更新标题时允许保留自身 slug）。
func (s *ArticleService) uniqueSlug(base string, excludeID uint) string {
	if base == "" {
		base = "article"
	}
	slug := base
	for i := 2; i < 1000; i++ {
		existing, err := s.articles.FindBySlug(slug)
		if err != nil || existing.ID == excludeID {
			return slug
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
	return slug
}

func (s *ArticleService) Update(articleID, authorID uint, update ArticleUpdate) (*model.Article, error) {
	article, err := s.articles.FindByID(articleID)
	if err != nil {
		return nil, err
	}
	if article.AuthorID != authorID {
		return nil, ErrForbidden
	}

	if update.Title != nil {
		title := strings.TrimSpace(*update.Title)
		if title == "" {
			return nil, NewValidationError("标题不能为空")
		}
		article.Title = title
		article.Slug = s.uniqueSlug(repository.Slugify(title), article.ID)
	}
	if update.Content != nil {
		if strings.TrimSpace(*update.Content) == "" {
			return nil, NewValidationError("内容不能为空")
		}
		article.Content = SanitizeHTML(*update.Content)
	}
	if update.Cover != nil {
		article.Cover = resolveCover(*update.Cover, article.Content)
	}
	if update.Excerpt != nil {
		article.Excerpt = resolveExcerpt(*update.Excerpt, article.Content)
	}
	if update.IsPinned != nil {
		article.IsPinned = *update.IsPinned
	}
	if update.ViewPassword != nil {
		// 传空串 = 清除密码（作者决定不再加密），nil = 本次未提交、保持原值。
		// 两种语义必须区分，否则前端"改标题不改密码"会把密码清掉。
		hashed, err := hashViewPassword(*update.ViewPassword)
		if err != nil {
			return nil, err
		}
		article.ViewPassword = hashed
	}
	if update.ScheduledAt != nil {
		article.ScheduledAt = *update.ScheduledAt
	}
	if update.Status != nil {
		status := *update.Status
		switch status {
		case model.ArticleDraft, model.ArticlePublished:
		case model.ArticleScheduled:
			// 改了发布时间却没带 scheduled_at 时沿用原值：编辑器里
			// 「等一下发布」和「改发布时间」通常是同一个面板里操作的。
			if article.ScheduledAt == nil {
				return nil, NewValidationError("定时发布需要指定发布时间")
			}
		default:
			return nil, NewValidationError("无效的状态值")
		}
		if article.Status != model.ArticlePublished && status == model.ArticlePublished {
			now := time.Now()
			article.PublishedAt = &now
			article.ScheduledAt = nil
		}
		article.Status = status
	}
	if update.CategoryID != nil {
		if *update.CategoryID == nil {
			article.CategoryID = nil
			article.Category = nil
		} else {
			if _, err := s.taxonomy.FindCategoryByID(**update.CategoryID); err != nil {
				return nil, NewValidationError("分类不存在")
			}
			article.CategoryID = *update.CategoryID
		}
	}

	// 保存前留一版历史。必须在改动落库**之前**调用——
	// 之后 article 已被覆盖，旧内容无从取回。
	//
	// 放在这里而不是 handler：任何走 Update 的路径（后台代改、
	// 将来的 API 客户端）都能自动获得版本历史，不必每个调用方记得。
	s.snapshot(article, authorID, "保存文章")

	// 标签在事务外解析成行；nil 表示「本次未提交标签字段」，保留原关联
	var tags []model.Tag
	if update.TagNames != nil {
		var err error
		if tags, err = s.taxonomy.FindOrCreateTags(*update.TagNames); err != nil {
			return nil, err
		}
	}
	if err := s.articles.UpdateWithTags(article, tags); err != nil {
		return nil, err
	}
	return s.articles.FindByID(articleID)
}

func (s *ArticleService) Delete(articleID, authorID uint) error {
	return s.articles.Delete(articleID, authorID)
}

// Restore 从回收站还原一篇文章。软删除期间 slug 仍被占用，还原后即可
// 重新访问；若期间有人用了相似标题新建文章拿到 -2 后缀，本文的 slug 不变。
func (s *ArticleService) Restore(articleID uint) error {
	return s.articles.Restore(articleID)
}

// Purge 彻底删除一篇文章及其评论/点赞/标签关联，不可恢复。
func (s *ArticleService) Purge(articleID uint) error {
	return s.articles.Purge(articleID)
}

// Trash 返回回收站文章与总数。
func (s *ArticleService) Trash(page, pageSize int) ([]model.Article, int64, error) {
	return s.articles.ListTrash(page, pageSize)
}

func (s *ArticleService) GetByID(id uint) (*model.Article, error) {
	return s.articles.FindByID(id)
}

func (s *ArticleService) GetBySlug(slug string) (*model.Article, error) {
	return s.articles.FindBySlug(slug)
}

// Related 返回与给定文章相关的其他已发布文章（相关性规则与 SQL 见
// repository.ArticleRepository.Related）。本层只做参数直传，无需额外校验：
// limit 的上下界裁剪在 repository 内完成。
func (s *ArticleService) Related(articleID uint, limit int) ([]model.Article, error) {
	return s.articles.Related(articleID, limit)
}

// Neighbors 返回上一篇（更新）与下一篇（更旧）的已发布文章，语义约定见
// repository.ArticleRepository.Neighbors 的注释。
func (s *ArticleService) Neighbors(articleID uint) (prev, next *model.Article, err error) {
	return s.articles.Neighbors(articleID)
}

func (s *ArticleService) IncrementViews(id uint) error {
	return s.articles.IncrementViews(id)
}

func (s *ArticleService) List(q repository.ArticleQuery) ([]model.Article, int64, error) {
	if q.Status != "" && q.Status != model.ArticleDraft && q.Status != model.ArticlePublished {
		q.Status = ""
	}
	return s.articles.List(q)
}

// resolveCover returns the explicit cover, falling back to the first image
// found in the article body so cards always have a thumbnail when possible.
func resolveCover(explicit, content string) string {
	if cover := strings.TrimSpace(explicit); cover != "" {
		return cover
	}
	return firstImageURL(content)
}

var imgSrcRegex = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)

// firstImageURL extracts the first <img src="..."> from HTML content.
func firstImageURL(content string) string {
	m := imgSrcRegex.FindStringSubmatch(content)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}
