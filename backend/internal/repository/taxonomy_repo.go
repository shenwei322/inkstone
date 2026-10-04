package repository

import (
	"errors"

	"github.com/shenwei/inkstone/backend/internal/model"
	"gorm.io/gorm"
)

// ErrTagNameTaken 表示标签名（或其 slug）与已有标签冲突。
var ErrTagNameTaken = errors.New("tag name already taken")

type CategoryCount struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	ArticleCount int64  `json:"article_count"`
}

type TagCount struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	ArticleCount int64  `json:"article_count"`
}

type TaxonomyRepository struct {
	db *gorm.DB
}

func NewTaxonomyRepository(db *gorm.DB) *TaxonomyRepository {
	return &TaxonomyRepository{db: db}
}

func (r *TaxonomyRepository) ListCategories() ([]CategoryCount, error) {
	var out []CategoryCount
	err := r.db.Table("categories").
		Select("categories.id, categories.name, categories.slug, COUNT(articles.id) as article_count").
		Joins("LEFT JOIN articles ON articles.category_id = categories.id").
		Group("categories.id").
		Order("article_count DESC, categories.name ASC").
		Scan(&out).Error
	return out, err
}

func (r *TaxonomyRepository) ListTags() ([]TagCount, error) {
	var out []TagCount
	err := r.db.Table("tags").
		Select("tags.id, tags.name, tags.slug, COUNT(article_tags.article_id) as article_count").
		Joins("LEFT JOIN article_tags ON article_tags.tag_id = tags.id").
		Group("tags.id").
		Order("article_count DESC, tags.name ASC").
		Scan(&out).Error
	return out, err
}

func (r *TaxonomyRepository) FindCategoryByID(id uint) (*model.Category, error) {
	var c model.Category
	err := r.db.First(&c, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateTag creates a tag (returns existing one when the slug already exists).
func (r *TaxonomyRepository) CreateTag(name string) (*model.Tag, error) {
	name = trimSpace(name)
	if name == "" {
		return nil, ErrInvalidInput
	}
	slug := Slugify(name)
	var existing model.Tag
	if err := r.db.Where("slug = ?", slug).First(&existing).Error; err == nil {
		return &existing, nil
	}
	tag := model.Tag{Name: name, Slug: slug}
	if err := r.db.Create(&tag).Error; err != nil {
		return nil, err
	}
	return &tag, nil
}

// UpdateTag renames a tag and refreshes its slug.
func (r *TaxonomyRepository) UpdateTag(id uint, name string) (*model.Tag, error) {
	name = trimSpace(name)
	if name == "" {
		return nil, ErrInvalidInput
	}
	var tag model.Tag
	if err := r.db.First(&tag, id).Error; err != nil {
		return nil, ErrNotFound
	}
	slug := Slugify(name)
	// 改名撞上已存在的 slug 时，uniqueIndex 会抛 23505；映射成可读错误，
	// 否则前端只会看到 500「internal server error」。
	var clash model.Tag
	if err := r.db.Where("slug = ? AND id <> ?", slug, id).First(&clash).Error; err == nil {
		return nil, ErrTagNameTaken
	}
	tag.Name = name
	tag.Slug = slug
	if err := r.db.Save(&tag).Error; err != nil {
		if uniqueField, ok := uniqueViolationField(err); ok && uniqueField == "slug" {
			return nil, ErrTagNameTaken
		}
		return nil, err
	}
	return &tag, nil
}

// DeleteTag removes a tag and its article associations.
func (r *TaxonomyRepository) DeleteTag(id uint) error {
	var tag model.Tag
	if err := r.db.First(&tag, id).Error; err != nil {
		return ErrNotFound
	}
	if err := r.db.Model(&tag).Association("Articles").Clear(); err != nil {
		// 关联表可能不存在于模型定义中，退化为直接清理连接表。
		// 这里必须检查错误：此前返回值被丢弃，清理失败会静默留下
		// article_tags 孤儿行，导致标签统计虚高。
		if execErr := r.db.Exec("DELETE FROM article_tags WHERE tag_id = ?", id).Error; execErr != nil {
			return execErr
		}
	}
	if err := r.db.Delete(&tag).Error; err != nil {
		return err
	}
	return nil
}

// FindOrCreateTags finds or creates the given tag names.
func (r *TaxonomyRepository) FindOrCreateTags(names []string) ([]model.Tag, error) {
	tags := make([]model.Tag, 0, len(names))
	for _, name := range names {
		name = trimSpace(name)
		if name == "" {
			continue
		}
		slug := Slugify(name)
		var t model.Tag
		err := r.db.Where("slug = ?", slug).First(&t).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			t = model.Tag{Name: name, Slug: slug}
			err = r.db.Create(&t).Error
		}
		if err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	// de-duplicate by id
	seen := make(map[uint]bool)
	unique := make([]model.Tag, 0, len(tags))
	for _, t := range tags {
		if !seen[t.ID] {
			seen[t.ID] = true
			unique = append(unique, t)
		}
	}
	return unique, nil
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
