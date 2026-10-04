package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

// ExportImportService 负责内容的导入导出（区别于 BackupService 的全站快照）。
//
// 两件事必须分开，理由：
//   - BackupService 是**运维**工具，输出 15 张表（含密码哈希、设置、日志）
//     的 gzip JSON，供同一套 InkStone 恢复；
//   - 本服务是**迁移**工具，只导出内容（文章/分类/标签），
//     输出人可读可改的 JSON，供站点搬迁、留档或与外部系统互转。
//
// 混用一个格式的后果：导入时要么把别人的用户表也灌进来（覆盖现有账号），
// 要么得逐字段判断该导什么，两种都容易出错。
type ExportImportService struct {
	articles *repository.ArticleRepository
	taxonomy *repository.TaxonomyRepository
}

func NewExportImportService(articles *repository.ArticleRepository, taxonomy *repository.TaxonomyRepository) *ExportImportService {
	return &ExportImportService{articles: articles, taxonomy: taxonomy}
}

// exportVersion 是导出格式版本。结构变化时递增，Import 据此拒绝
// 无法识别的版本，而不是猜着解析。
const exportVersion = 1

// maxArticleImportCount 限制单次导入的文章数。
// 每篇都要过 bluemoney 消毒 + slug 查询 + 标签解析，一次几千篇会让
// HTTP 请求超时，且中途失败时用户无法判断哪些已导入。
const maxArticleImportCount = 300

const exportBatchSize = 200

var (
	// ErrImportInvalid 表示文件不是本系统导出的内容包。
	ErrImportInvalid = errors.New("导入文件格式无效")
	// ErrImportTooMany 表示单次导入篇数超限。
	ErrImportTooMany = errors.New("单次导入文章过多")
)

// ExportBundle 是导出的内容包。
type ExportBundle struct {
	Version      int             `json:"version"`
	ExportedAt   time.Time       `json:"exported_at"`
	ArticleCount int             `json:"article_count"`
	Articles     []ExportArticle `json:"articles"`
	Categories   []string        `json:"categories"`
	Tags         []string        `json:"tags"`
}

// ExportArticle 是一篇文章的导出形态。
//
// 与 Article 模型的区别：分类与标签换成**名称**而不是 ID。
// ID 是另一套数据库的私有值，导出成数字等于把读者绑死在原库上；
// 名称才是内容本身。导入时按名称 find-or-create。
type ExportArticle struct {
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	Content     string     `json:"content"`
	Status      string     `json:"status"`
	Cover       string     `json:"cover"`
	Excerpt     string     `json:"excerpt"`
	IsPinned    bool       `json:"is_pinned"`
	Category    string     `json:"category"`
	Tags        []string   `json:"tags"`
	PublishedAt *time.Time `json:"published_at"`
}

// Export 导出全站内容为 JSON。
//
// 文章分批取（每批 200），而不是一次全取：几百篇文章每篇几 KB 正文，
// 全取会在内存里堆出几十 MB，且 List 一次拿太多会让 PostgreSQL 的
// 单次查询耗时失控。分批对导出方是透明的——最终仍需拼成单个文件。
func (s *ExportImportService) Export() (*ExportBundle, error) {
	bundle := &ExportBundle{Version: exportVersion, ExportedAt: time.Now()}

	var all []model.Article
	for page := 1; ; page++ {
		items, total, err := s.articles.List(repository.ArticleQuery{
			All: true, Page: page, PageSize: exportBatchSize,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if len(items) < exportBatchSize || int64(len(all)) >= total {
			break
		}
	}

	for i := range all {
		a := &all[i]
		bundle.Articles = append(bundle.Articles, ExportArticle{
			Title:       a.Title,
			Slug:        a.Slug,
			Content:     a.Content,
			Status:      a.Status,
			Cover:       a.Cover,
			Excerpt:     a.Excerpt,
			IsPinned:    a.IsPinned,
			Category:    categoryName(a.Category),
			Tags:        tagNames(a.Tags),
			PublishedAt: a.PublishedAt,
		})
	}
	bundle.ArticleCount = len(bundle.Articles)

	if cats, err := s.taxonomy.ListCategories(); err != nil {
		return nil, err
	} else {
		for _, c := range cats {
			bundle.Categories = append(bundle.Categories, c.Name)
		}
	}
	if tags, err := s.taxonomy.ListTags(); err != nil {
		return nil, err
	} else {
		for _, t := range tags {
			bundle.Tags = append(bundle.Tags, t.Name)
		}
	}
	return bundle, nil
}

func categoryName(c *model.Category) string {
	if c == nil {
		return ""
	}
	return c.Name
}

func tagNames(tags []model.Tag) []string {
	if len(tags) == 0 {
		return nil
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Name)
	}
	return out
}

// Parse 解析导入的 JSON。
//
// 版本不匹配时返回 ErrImportInvalid 而不是尽力解析：猜着解析会把
// 新格式的文件按旧结构读，产出静默错位的数据（比如把 excerpt 读成 title），
// 那种"导入成功但内容全错"比直接失败更难排查。
func Parse(data []byte) (*ExportBundle, error) {
	var bundle ExportBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, ErrImportInvalid
	}
	if bundle.Version != exportVersion {
		return nil, fmt.Errorf("%w：不支持的导出格式版本 %d（当前支持 %d）",
			ErrImportInvalid, bundle.Version, exportVersion)
	}
	return &bundle, nil
}

// Import 把内容包写入本站，返回 (新建篇数, 更新篇数)。
//
// **不做删除**：导入是"补内容"，不是"同步"。同步语义会在用户只想
// 合并两个站的文章时，把其中一边的内容删掉——那属于灾难级误操作。
//
// slug 已存在时更新而非跳过：重复导入同一个文件应当幂等，
// 这也让人能改完导出文件里的文章再导回来。
// 文章属于其他作者时跳过（接管他人内容越权）。
func (s *ExportImportService) Import(authorID uint, bundle *ExportBundle) (created, updated, skipped int, err error) {
	if len(bundle.Articles) > maxArticleImportCount {
		return 0, 0, 0, ErrImportTooMany
	}

	for _, item := range bundle.Articles {
		title := strings.TrimSpace(item.Title)
		content := strings.TrimSpace(item.Content)
		if title == "" || content == "" {
			// 跳过空标题/空内容的条目：不因一条脏数据中断整批导入，
			// 其余文章是有价值的。
			skipped++
			continue
		}
		c, u, skip, err := s.importArticle(authorID, item)
		if err != nil {
			return created, updated, skipped, err
		}
		created += c
		updated += u
		skipped += skip
	}
	return created, updated, skipped, nil
}

// importArticle 导入单篇文章，返回 (新建, 更新, 跳过)。
func (s *ExportImportService) importArticle(authorID uint, item ExportArticle) (int, int, int, error) {
	status := item.Status
	if status != model.ArticlePublished && status != model.ArticleDraft {
		// 未知状态一律按草稿：绝不能因为一个拼错的状态值
		// 把未完成的内容直接公开发布。
		status = model.ArticleDraft
	}

	input := ArticleInput{
		Title:      item.Title,
		Content:    item.Content,
		Status:     status,
		Cover:      item.Cover,
		Excerpt:    item.Excerpt,
		IsPinned:   item.IsPinned,
		CategoryID: s.resolveCategory(item.Category),
		TagNames:   item.Tags,
	}

	existing, err := s.articles.FindBySlug(item.Slug)
	if err != nil {
		// 查不到 = 新建。
		if cerr := s.createArticle(authorID, input, item.Slug, item.PublishedAt); cerr != nil {
			return 0, 0, 0, cerr
		}
		return 1, 0, 0, nil
	}
	if existing.AuthorID != authorID {
		// 别人的文章不覆盖：导入方可能只是访客，接管他人内容越权。
		// 跳过而非报错，让同批里属于导入方的文章照常写入。
		return 0, 0, 1, nil
	}
	if uerr := s.updateArticle(existing, input); uerr != nil {
		return 0, 0, 0, uerr
	}
	return 0, 1, 0, nil
}

// resolveCategory 按名称找分类，找不到则创建。返回 nil 表示这篇无分类。
//
// 创建失败时返回 nil 而不是中断导入：分类只是组织维度，
// 为它让整批内容导入失败不值得。文章会以"未分类"进入，事后再归。
func (s *ExportImportService) resolveCategory(name string) *uint {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	cat, err := s.taxonomy.FindOrCreateCategoryByName(name)
	if err != nil {
		return nil
	}
	return &cat.ID
}

func (s *ExportImportService) createArticle(authorID uint, input ArticleInput, wantSlug string, publishedAt *time.Time) error {
	article := &model.Article{
		AuthorID:   authorID,
		CategoryID: input.CategoryID,
		Title:      strings.TrimSpace(input.Title),
		Slug:       wantSlug,
		Content:    SanitizeHTML(input.Content),
		Status:     input.Status,
		Cover:      resolveCover(input.Cover, input.Content),
		Excerpt:    resolveExcerpt(input.Excerpt, input.Content),
		IsPinned:   input.IsPinned,
	}
	if article.Status == model.ArticlePublished {
		// 保留原站的发布时间：那是文章真实的历史时间，
		// 改成导入时刻会让归档页的时间线错乱。
		at := time.Now()
		if publishedAt != nil && !publishedAt.IsZero() {
			at = *publishedAt
		}
		article.PublishedAt = &at
	}
	if err := s.articles.Create(article); err != nil {
		return err
	}
	s.attachTags(article, input.TagNames)
	return nil
}

func (s *ExportImportService) updateArticle(existing *model.Article, input ArticleInput) error {
	existing.Title = strings.TrimSpace(input.Title)
	existing.Content = SanitizeHTML(input.Content)
	existing.Status = input.Status
	existing.Cover = resolveCover(input.Cover, input.Content)
	existing.Excerpt = resolveExcerpt(input.Excerpt, input.Content)
	existing.IsPinned = input.IsPinned
	existing.CategoryID = input.CategoryID
	if existing.Status == model.ArticlePublished && existing.PublishedAt == nil {
		now := time.Now()
		existing.PublishedAt = &now
	}
	if err := s.articles.Update(existing); err != nil {
		return err
	}
	s.attachTags(existing, input.TagNames)
	return nil
}

// attachTags 解析并关联标签。失败只忽略：标签缺失不影响文章正文可读，
// 为几行关联失败让整次导入报错不值得。
func (s *ExportImportService) attachTags(article *model.Article, names []string) {
	if len(names) == 0 {
		return
	}
	tags, err := s.taxonomy.FindOrCreateTags(names)
	if err != nil {
		return
	}
	_ = s.articles.ReplaceTags(article, tags)
}

// Render 把内容包序列化为带缩进的 JSON。
//
// 带缩进而非压缩：导出文件的用途之一就是让人看一眼、改几处再导回来，
// 压缩成一行既读不了也没法 diff。
func Render(bundle *ExportBundle) ([]byte, error) {
	return json.MarshalIndent(bundle, "", "  ")
}
