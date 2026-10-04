package repository

import (
	"errors"
	"strings"

	"github.com/shenwei/inkstone/backend/internal/model"
	"gorm.io/gorm"
)

type ArticleQuery struct {
	AuthorID     uint
	Status       string
	All          bool
	CategorySlug string
	TagSlug      string
	Search       string
	OrderBy      string // empty = published_at DESC; "views" = hot articles
	Page         int
	PageSize     int
}

type ArticleRepository struct {
	db *gorm.DB
}

func NewArticleRepository(db *gorm.DB) *ArticleRepository {
	return &ArticleRepository{db: db}
}

func (r *ArticleRepository) Create(article *model.Article) error {
	return r.db.Create(article).Error
}

func (r *ArticleRepository) Update(article *model.Article) error {
	return r.db.Save(article).Error
}

// purgeArticleRelations 清理一篇文章的所有关联行（评论 / 点赞收藏 / 标签关联）。
// articles 被 comments、reactions、article_tags 的外键引用，直接 DELETE 会被
// 数据库拒绝（SQLSTATE 23503），必须先清关联再删文章。
func purgeArticleRelations(tx *gorm.DB, id uint) error {
	if err := tx.Where("article_id = ?", id).Delete(&model.Comment{}).Error; err != nil {
		return err
	}
	if err := tx.Where("article_id = ?", id).Delete(&model.Reaction{}).Error; err != nil {
		return err
	}
	return tx.Exec("DELETE FROM article_tags WHERE article_id = ?", id).Error
}

func (r *ArticleRepository) Delete(id, authorID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := purgeArticleRelations(tx, id); err != nil {
			return err
		}
		result := tx.Where("id = ? AND author_id = ?", id, authorID).Delete(&model.Article{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *ArticleRepository) DeleteAny(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := purgeArticleRelations(tx, id); err != nil {
			return err
		}
		result := tx.Delete(&model.Article{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *ArticleRepository) DeleteByAuthor(authorID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var ids []uint
		if err := tx.Model(&model.Article{}).Where("author_id = ?", authorID).
			Pluck("id", &ids).Error; err != nil {
			return err
		}
		for _, id := range ids {
			if err := purgeArticleRelations(tx, id); err != nil {
				return err
			}
		}
		return tx.Where("author_id = ?", authorID).Delete(&model.Article{}).Error
	})
}

func (r *ArticleRepository) CountAll() (int64, error) {
	var n int64
	err := r.db.Model(&model.Article{}).Count(&n).Error
	return n, err
}

func (r *ArticleRepository) CountByStatus(status string) (int64, error) {
	var n int64
	err := r.db.Model(&model.Article{}).Where("status = ?", status).Count(&n).Error
	return n, err
}

func (r *ArticleRepository) FindByID(id uint) (*model.Article, error) {
	var article model.Article
	err := r.db.Preload("Author").Preload("Category").Preload("Tags").First(&article, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &article, nil
}

func (r *ArticleRepository) FindBySlug(slug string) (*model.Article, error) {
	var article model.Article
	err := r.db.Preload("Author").Preload("Category").Preload("Tags").
		Where("slug = ?", slug).First(&article).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &article, nil
}

// IncrementViews atomically bumps the view counter.
func (r *ArticleRepository) IncrementViews(id uint) error {
	return r.db.Model(&model.Article{}).Where("id = ?", id).
		UpdateColumn("views", gorm.Expr("views + 1")).Error
}

// ReplaceTags swaps the tag association for an article.
func (r *ArticleRepository) ReplaceTags(article *model.Article, tags []model.Tag) error {
	return r.db.Model(article).Association("Tags").Replace(tags)
}

// escapeLike neutralizes LIKE wildcards in user input so searches match
// literal characters instead of patterns.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	return strings.ReplaceAll(s, "_", "\\_")
}

func (r *ArticleRepository) List(q ArticleQuery) ([]model.Article, int64, error) {
	db := r.db.Model(&model.Article{})

	if q.AuthorID > 0 {
		db = db.Where("articles.author_id = ?", q.AuthorID)
	}
	switch {
	case q.Status != "":
		db = db.Where("articles.status = ?", q.Status)
	case !q.All:
		db = db.Where("articles.status = ?", model.ArticlePublished)
	}
	if q.CategorySlug != "" {
		// Subquery filters avoid row multiplication (and inflated counts)
		// when combining multiple filters in one query.
		db = db.Where("articles.category_id IN (SELECT id FROM categories WHERE slug = ?)", q.CategorySlug)
	}
	if q.TagSlug != "" {
		db = db.Where("articles.id IN (SELECT article_id FROM article_tags WHERE tag_id IN (SELECT id FROM tags WHERE slug = ?))", q.TagSlug)
	}
	if q.Search != "" {
		like := "%" + escapeLike(q.Search) + "%"
		db = db.Where("articles.title ILIKE ? OR articles.content ILIKE ?", like, like)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := normalizePage(q.Page, q.PageSize)
	var articles []model.Article
	order := "articles.published_at DESC NULLS LAST, articles.id DESC"
	if q.OrderBy == "views" {
		order = "articles.views DESC, articles.id DESC"
	}
	err := db.Preload("Author").
		Preload("Category").
		Preload("Tags").
		Order(order).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&articles).Error
	if err != nil {
		return nil, 0, err
	}
	return articles, total, nil
}

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	return page, pageSize
}

// Slugify converts a title into a URL-safe slug. ASCII letters/digits are
// kept, spaces and punctuation become hyphens, CJK and other letters are
// preserved as-is.
func Slugify(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	lastHyphen := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		case r == ' ' || r == '-' || r == '_' || r == '.':
			if !lastHyphen && b.Len() > 0 {
				b.WriteRune('-')
				lastHyphen = true
			}
		default:
			b.WriteRune(r)
			lastHyphen = false
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "article"
	}
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}
