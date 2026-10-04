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
}

func NewArticleService(articles *repository.ArticleRepository, taxonomy *repository.TaxonomyRepository) *ArticleService {
	return &ArticleService{articles: articles, taxonomy: taxonomy}
}

type ArticleInput struct {
	Title      string
	Content    string
	Status     string
	CategoryID *uint
	TagNames   []string
	Cover      string
}

type ArticleUpdate struct {
	Title      *string
	Content    *string
	Status     *string
	CategoryID **uint
	TagNames   *[]string
	Cover      *string
}

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

	status := input.Status
	if status != model.ArticleDraft && status != model.ArticlePublished {
		status = model.ArticleDraft
	}

	if input.CategoryID != nil {
		if _, err := s.taxonomy.FindCategoryByID(*input.CategoryID); err != nil {
			return nil, NewValidationError("分类不存在")
		}
	}

	article := &model.Article{
		AuthorID:   authorID,
		CategoryID: input.CategoryID,
		Title:      title,
		Slug:       s.uniqueSlug(repository.Slugify(title), 0),
		Content:    SanitizeHTML(input.Content),
		Status:     status,
		Cover:      resolveCover(input.Cover, input.Content),
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
	if update.Status != nil {
		status := *update.Status
		if status != model.ArticleDraft && status != model.ArticlePublished {
			return nil, NewValidationError("无效的状态值")
		}
		if article.Status != model.ArticlePublished && status == model.ArticlePublished {
			now := time.Now()
			article.PublishedAt = &now
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

func (s *ArticleService) GetByID(id uint) (*model.Article, error) {
	return s.articles.FindByID(id)
}

func (s *ArticleService) GetBySlug(slug string) (*model.Article, error) {
	return s.articles.FindBySlug(slug)
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
