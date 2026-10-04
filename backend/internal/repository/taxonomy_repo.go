package repository

import (
	"errors"

	"github.com/shenwei/inkstone/backend/internal/model"
	"gorm.io/gorm"
)

// ErrTagNameConflict 表示标签名（或其 slug）与已有标签冲突。
var ErrTagNameConflict = errors.New("tag name already taken")

// 分类结构类错误。repository 层只返回可判定的错误，具体中文提示由
// service 层映射（分层原则：repository 不认识"用户看得懂的话"）。
var (
	ErrCategoryParentMissing = errors.New("parent category not found")
	ErrCategoryCycle         = errors.New("category would form a cycle")
	ErrCategoryTooDeep       = errors.New("category would exceed depth limit")
	ErrCategoryHasChildren   = errors.New("category still has children")
	ErrCategoryHasArticles   = errors.New("category still has articles")
)

type CategoryCount struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	ArticleCount int64  `json:"article_count"`
	// ParentID 顶级为 0。加这个字段是为了让前台/后台的分类下拉框能
	// 表达层级（用一个 repository 方法喂两个消费方，不必再查一次）。
	ParentID uint `json:"parent_id"`
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
		Select("categories.id, categories.name, categories.slug, COALESCE(categories.parent_id, 0) as parent_id, COUNT(articles.id) as article_count").
		// deleted_at 必须显式过滤：这里是原生 Table+Joins 写法，GORM 不会
		// 自动追加软删除条件（只有走 Model(&Article{}) 才会）。漏掉它的话，
		// 回收站里的文章仍会被计入分类文章数，用户看到"有 5 篇"却只列出 2 篇。
		Joins("LEFT JOIN articles ON articles.category_id = categories.id AND articles.deleted_at IS NULL").
		Group("categories.id").
		Order("article_count DESC, categories.name ASC").
		Scan(&out).Error
	return out, err
}

// ListCategoryTree 返回全部分类（含 parent_id），用于构建层级树。
// 返回扁平列表而不在此处建树：建树是纯内存运算，放 service 层更可测，
// 也让 repository 保持"只取数据"的单一职责。
func (r *TaxonomyRepository) ListCategoryTree() ([]model.Category, error) {
	var cats []model.Category
	err := r.db.Order("name ASC").Find(&cats).Error
	return cats, err
}

// FindOrCreateCategoryByName 按名称找分类，不存在则创建为顶级分类。
//
// 存在的原因：内容导入时，导出包里只有分类**名称**（ID 是另一套数据库的
// 私有值）。迁移过来的文章要挂上分类，就得按名称找或建。
//
// 为什么导入的分类一律建成顶级、不尝试恢复层级：导出格式只带名称，
// 不描述父子关系。硬猜层级比平铺更危险——猜错会把文章挂到错误的分类下，
// 而且事后看不出来。需要层级的话由管理员在后台手工调整。
func (r *TaxonomyRepository) FindOrCreateCategoryByName(name string) (*model.Category, error) {
	name = trimSpace(name)
	if name == "" {
		return nil, ErrInvalidInput
	}
	var cat model.Category
	if err := r.db.Where("name = ?", name).First(&cat).Error; err == nil {
		return &cat, nil
	}
	slug := Slugify(name)
	return r.CreateCategory(name, slug, 0)
}

// CreateCategory 创建分类。parentID 为 0 表示顶级。
func (r *TaxonomyRepository) CreateCategory(name, slug string, parentID uint) (*model.Category, error) {
	cat := model.Category{Name: name, Slug: slug}
	if parentID > 0 {
		parent, err := r.FindCategoryByID(parentID)
		if err != nil {
			return nil, ErrCategoryParentMissing
		}
		if parent.ParentID != nil {
			return nil, ErrCategoryTooDeep
		}
		pid := parent.ID
		cat.ParentID = &pid
	}
	if err := r.db.Create(&cat).Error; err != nil {
		return nil, err
	}
	return &cat, nil
}

// UpdateCategory 更新分类名称与父级。
//
// 三个不能允许的改法，都会破坏树的完整性：
//   - 把父级指向自己 → 成环，遍历 depth 时死循环
//   - 把父级指向自己的后代 → 同样成环
//   - 把父级改成一个有父级的分类 → 超出两级子分类上限
func (r *TaxonomyRepository) UpdateCategory(id uint, name string, parentID uint) (*model.Category, error) {
	cat, err := r.FindCategoryByID(id)
	if err != nil {
		return nil, err
	}
	if parentID > 0 {
		if parentID == id || r.IsDescendant(parentID, id) {
			return nil, ErrCategoryCycle
		}
		parent, err := r.FindCategoryByID(parentID)
		if err != nil {
			return nil, ErrCategoryParentMissing
		}
		if parent.ParentID != nil {
			return nil, ErrCategoryTooDeep
		}
		pid := parent.ID
		cat.ParentID = &pid
	} else {
		cat.ParentID = nil
	}
	cat.Name = name
	if err := r.db.Save(cat).Error; err != nil {
		return nil, err
	}
	return cat, nil
}

// IsDescendant 报告 candidate 是否为 ancestor 的后代（含自身）。
// 用带步数上限的爬父链实现，避免脏数据成环时死循环。
func (r *TaxonomyRepository) IsDescendant(candidate, ancestor uint) bool {
	cur := candidate
	for i := 0; i < model.MaxCategoryDepth*2; i++ {
		if cur == ancestor {
			return true
		}
		var c model.Category
		if err := r.db.First(&c, cur).Error; err != nil {
			return false
		}
		if c.ParentID == nil {
			return false
		}
		cur = *c.ParentID
	}
	return false
}

// DeleteCategory 删除分类。
//
// 有子分类时直接删除会把子分类变成孤儿（parent_id 悬空），所以：
//   - 有子分类 → 拒绝删除，提示先处理子分类
//   - 有关联文章 → 拒绝删除，提示先迁移文章
//
// 管理员确实想整体删时，可以在后台先删文章/改分类。这里刻意不做
// 级联删除：分类被连带清掉文章属于不可逆误操作，代价比重试高得多。
func (r *TaxonomyRepository) DeleteCategory(id uint) error {
	if _, err := r.FindCategoryByID(id); err != nil {
		return err
	}
	var children int64
	if err := r.db.Model(&model.Category{}).Where("parent_id = ?", id).Count(&children).Error; err != nil {
		return err
	}
	if children > 0 {
		return ErrCategoryHasChildren
	}
	var articles int64
	// model.Article{} 而非裸 Article：软删除字段由 GORM 自动追加条件，
	// 所以回收站里的文章不计入"占用该分类"，删分类时不会误判。
	if err := r.db.Model(&model.Article{}).Where("category_id = ?", id).Count(&articles).Error; err != nil {
		return err
	}
	if articles > 0 {
		return ErrCategoryHasArticles
	}
	return r.db.Delete(&model.Category{}, id).Error
}

// CountCategoryChildren 返回直接子分类数量。
func (r *TaxonomyRepository) CountCategoryChildren(id uint) (int64, error) {
	var n int64
	err := r.db.Model(&model.Category{}).Where("parent_id = ?", id).Count(&n).Error
	return n, err
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
		return nil, ErrTagNameConflict
	}
	tag.Name = name
	tag.Slug = slug
	if err := r.db.Save(&tag).Error; err != nil {
		if uniqueField, ok := uniqueViolationField(err); ok && uniqueField == "slug" {
			return nil, ErrTagNameConflict
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

// MergeTags 把若干标签合并进 target，返回实际迁移的文章关联数。
//
// 这是标签治理的核心操作：站内文章一多，"Go/golang/Go语言" 三个标签
// 内容完全重叠，各自统计都不准。合并后来源标签被删除，文章关联迁到目标。
//
// 难点在 article_tags 的主键是 (article_id, tag_id) 复合主键：
// 一篇文章如果**同时**打了来源标签和目标标签，直接把 tag_id 改成目标，
// 就会撞上主键冲突（同一篇文章 + 同一个目标标签已存在）。
// 因此顺序必须是：先删掉那些会冲突的行，再改写剩余的，最后删来源标签。
func (r *TaxonomyRepository) MergeTags(targetID uint, sourceIDs []uint) (int64, error) {
	if len(sourceIDs) == 0 {
		return 0, ErrInvalidInput
	}
	filtered := make([]uint, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		if id == 0 || id == targetID {
			continue
		}
		filtered = append(filtered, id)
	}
	if len(filtered) == 0 {
		return 0, ErrInvalidInput
	}
	var target model.Tag
	if err := r.db.First(&target, targetID).Error; err != nil {
		return 0, ErrNotFound
	}

	var migrated int64
	err := r.db.Transaction(func(tx *gorm.DB) error {
		for _, sourceID := range filtered {
			var source model.Tag
			if err := tx.First(&source, sourceID).Error; err != nil {
				// 来源标签已被删（比如管理员开了两个标签页各删一次）：
				// 继续处理剩下的，而不是让整批失败。
				continue
			}

			// 1. 删掉「这篇文章已经有目标标签」的重复关联。
			//    不理会这些行的话，第 2 步的 UPDATE 会批量撞主键。
			dup := tx.Exec(`DELETE FROM article_tags
			                 WHERE tag_id = ?
			                   AND article_id IN (SELECT article_id FROM article_tags WHERE tag_id = ?)`,
				targetID, sourceID)
			if dup.Error != nil {
				return dup.Error
			}
			migrated += dup.RowsAffected

			// 2. 剩余关联迁到目标标签。
			moved := tx.Exec("UPDATE article_tags SET tag_id = ? WHERE tag_id = ?", targetID, sourceID)
			if moved.Error != nil {
				return moved.Error
			}
			migrated += moved.RowsAffected

			// 3. 删除已掏空的来源标签。
			if err := tx.Delete(&model.Tag{}, sourceID).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return migrated, nil
}
