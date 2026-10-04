package repository

import (
	"errors"
	"strings"
	"time"

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

// Update 只写业务字段。
//
// 绝不能用 Save：Save 在主键非零时执行**全字段 UPDATE（含零值）**。这里的
// article 是「先 FindByID 读出来、改几个字段」得到的，读取之后若有访客
// IncrementViews，Save 就会把并发累加出来的 views 覆盖回旧值——日常
// 「编辑文章 + 访客浏览」交错即造成浏览量回滚。views 因此也必须排除在外。
// updated_at 交给 GORM 按约定自动维护。
func (r *ArticleRepository) Update(article *model.Article) error {
	res := r.db.Model(article).Updates(map[string]any{
		"title":        article.Title,
		"slug":         article.Slug,
		"content":      article.Content,
		"cover":        article.Cover,
		"status":       article.Status,
		"category_id":  article.CategoryID,
		"published_at": article.PublishedAt,
	})
	if res.Error != nil {
		return res.Error
	}
	// PostgreSQL 对「匹配到并执行 UPDATE」的行计数为 1（与值是否变化无关），
	// 因此 0 行只可能是文章不存在。
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateWithTags 在**同一事务**内更新文章字段并替换标签关联。
//
// 拆成两步的问题：Update 成功而 ReplaceTags 失败时，库里留下「新内容配旧标签」
// 的文章；Create 后 ReplaceTags 失败则留下无标签的半成品。二者都属于
// 只改对一半的中间态，管理员看到的却是错误提示。
func (r *ArticleRepository) UpdateWithTags(article *model.Article, tags []model.Tag) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(article).Updates(map[string]any{
			"title":        article.Title,
			"slug":         article.Slug,
			"content":      article.Content,
			"cover":        article.Cover,
			"status":       article.Status,
			"category_id":  article.CategoryID,
			"published_at": article.PublishedAt,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		if tags == nil {
			return nil // 未提交标签字段：保持原关联不动
		}
		return tx.Model(article).Association("Tags").Replace(tags)
	})
}

// CreateWithTags 在**同一事务**内插入文章并写入标签关联。
func (r *ArticleRepository) CreateWithTags(article *model.Article, tags []model.Tag) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(article).Error; err != nil {
			return err
		}
		if len(tags) > 0 {
			return tx.Model(article).Association("Tags").Replace(tags)
		}
		return nil
	})
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
	// 历史版本也必须真删：每版都存全文，文章没了还留着几十版正文，
	// 既占空间又让"彻底删除"名不副实。
	if err := tx.Where("article_id = ?", id).Delete(&model.ArticleRevision{}).Error; err != nil {
		return err
	}
	return tx.Exec("DELETE FROM article_tags WHERE article_id = ?", id).Error
}

// Delete 软删除一篇文章（仅作者本人可删）。
//
// 注意这里**不再**调用 purgeArticleRelations：软删后文章仍占着 comments /
// reactions / article_tags 的外键引用，若照旧先清关联，还原文章时那些评论
// 和点赞已经没了——等于换了一种方式丢数据。彻底清关联只发生在 Purge。
func (r *ArticleRepository) Delete(id, authorID uint) error {
	result := r.db.Where("id = ? AND author_id = ?", id, authorID).Delete(&model.Article{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Restore 把软删除的文章还原（清空 deleted_at）。
func (r *ArticleRepository) Restore(id uint) error {
	result := r.db.Unscoped().Model(&model.Article{}).Where("id = ?", id).
		Update("deleted_at", nil)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Purge 彻底删除一篇文章及其全部关联行（不可恢复）。用于回收站里的
// 「彻底删除」与删除用户时的连带清理。
// PublishDue 把 scheduled_at <= now 的 scheduled 文章改为 published。
//
// 返回实际发布的文章，供调用方写日志（发布是个低频但重要的事件，
// 没人盯着后台时，日志是唯一的追溯途径）。
//
// 两个细节：
//   - 用一条 UPDATE 完成，不先查再逐条改。逐条改会在两个 tick 之间
//     重复发布同一篇（扫描到的是同一个集合）， UPDATE 天然幂等。
//   - published_at 只在为空时写 now：文章可能带着"预计发布时间"创建，
//     那才是它名义上的发布时间，不该被改成实际发布那一刻。
func (r *ArticleRepository) PublishDue(now time.Time) ([]model.Article, error) {
	var due []model.Article
	if err := r.db.Where("status = ? AND scheduled_at IS NOT NULL AND scheduled_at <= ?",
		model.ArticleScheduled, now).Find(&due).Error; err != nil {
		return nil, err
	}
	if len(due) == 0 {
		return nil, nil
	}
	ids := make([]uint, 0, len(due))
	for _, a := range due {
		ids = append(ids, a.ID)
	}
	err := r.db.Model(&model.Article{}).
		Where("id IN ?", ids).
		Updates(map[string]any{
			"status":       model.ArticlePublished,
			"scheduled_at": nil,
			"published_at": gorm.Expr("COALESCE(published_at, ?)", now),
		}).Error
	if err != nil {
		return nil, err
	}
	return due, nil
}

func (r *ArticleRepository) Purge(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := purgeArticleRelations(tx, id); err != nil {
			return err
		}
		result := tx.Unscoped().Where("id = ?", id).Delete(&model.Article{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// DeleteAny 软删除任意文章（管理员操作，不限作者）。
// RowsAffected 为 0 时返回 ErrNotFound：管理员点删除时若文章已被删，
// 前端要提示「文章不存在」，而不是静默显示成功。
func (r *ArticleRepository) DeleteAny(id uint) error {
	result := r.db.Delete(&model.Article{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// BulkDelete 批量软删除文章，返回实际删除条数。
//
// 逐条删除不用事务的话，中途失败会留下「一半删了一半没删」的状态，
// 管理员无从判断哪些已经没了。这里用一条 UPDATE 天然原子。
func (r *ArticleRepository) BulkDelete(ids []uint) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := r.db.Where("id IN ?", ids).Delete(&model.Article{})
	return result.RowsAffected, result.Error
}

// BulkSetStatus 批量改文章状态，返回受影响条数。
//
// 补齐 published_at：把一批文章从草稿/定时改成已发布时，逐条调
// SetArticleStatus 会为每篇单独 SELECT 一次；一条 UPDATE 搞定。
// published_at 用 COALESCE 保证已有的发布时间不被改写——
// 那篇文章是比这批操作更早的真实发布，改写等于伪造时间线。
func (r *ArticleRepository) BulkSetStatus(ids []uint, status string, now time.Time) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	updates := map[string]any{"status": status, "scheduled_at": nil}
	if status == model.ArticlePublished {
		updates["published_at"] = gorm.Expr("COALESCE(published_at, ?)", now)
	}
	result := r.db.Model(&model.Article{}).Where("id IN ?", ids).Updates(updates)
	return result.RowsAffected, result.Error
}

func (r *ArticleRepository) DeleteByAuthor(authorID uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		return purgeArticlesByAuthor(tx, authorID)
	})
}

// purgeArticlesByAuthor 在给定事务内彻底删除某作者的全部文章及其关联行。
// 抽成可复用函数，以便「删除用户」在**同一事务**里连带清理文章
// （此前 handler 分两个事务调用，中途失败会留下不一致状态）。
//
// 这里必须是 Unscoped 的真删：删除账号本身就是彻底操作，若只软删文章，
// 这些文章会连同已删除用户的作者引用一起留在库里无法访问，而它们的
// 评论/点赞行也不会被清理。
func purgeArticlesByAuthor(tx *gorm.DB, authorID uint) error {
	var ids []uint
	if err := tx.Unscoped().Model(&model.Article{}).Where("author_id = ?", authorID).
		Pluck("id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := purgeArticleRelations(tx, id); err != nil {
			return err
		}
	}
	return tx.Unscoped().Where("author_id = ?", authorID).Delete(&model.Article{}).Error
}

// ListTrash 返回回收站里的文章（已软删除），按删除时间倒序。
// Unscoped() 绕开 GORM 的 deleted_at IS NULL 过滤，再手动限定非空。
func (r *ArticleRepository) ListTrash(page, pageSize int) ([]model.Article, int64, error) {
	db := r.db.Unscoped().Model(&model.Article{}).Where("deleted_at IS NOT NULL")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize = normalizePage(page, pageSize)
	var articles []model.Article
	err := db.Preload("Author").
		Order("deleted_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&articles).Error
	return articles, total, err
}

// CountTrash 返回回收站文章数（后台菜单角标用）。
func (r *ArticleRepository) CountTrash() (int64, error) {
	var n int64
	err := r.db.Unscoped().Model(&model.Article{}).
		Where("deleted_at IS NOT NULL").Count(&n).Error
	return n, err
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
	// 置顶优先：pinned 的排在所有未置顶之前。同一置顶级别内再按时间。
	// 用 is_pinned DESC 而不是拆两次查询——后者会让分页总数算错
	// （两段各自一页），前端翻到第二页会出现"上一页的置顶又出现"。
	order := "articles.is_pinned DESC, articles.published_at DESC NULLS LAST, articles.id DESC"
	if q.OrderBy == "views" {
		// 热门榜不掺置顶：那是"看谁阅读量高"，插一篇置顶进去会误导读者。
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

// articleCardColumns 是「文章卡片」类查询只需要的列集合。related /
// neighbors 这些只读接口不需要正文（content 可能很大），也不预加载
// Author / Tags，省掉关联查询与带宽。
// articleCardColumns 是卡片字段列表。
//
// view_password 只用来判断「这篇有没有设访问密码」（转成 has_password
// 布尔值），绝不下发哈希本身。相关/邻居卡片点进去可能要输密码，
// 提前在卡片上显示锁标识，比让人点完才知道要密码好。
const articleCardColumns = "articles.id, articles.title, articles.slug, articles.cover, articles.views, articles.published_at, articles.excerpt, " +
	"CASE WHEN articles.view_password IS NULL OR articles.view_password = '' THEN false ELSE true END AS has_password"

// relatedDefaultLimit / relatedMaxLimit 约束相关文章条目的入参区间：
// 相关推荐只用于页面侧栏，给太多既渲染不下也会让 SQL 多扫行。
const (
	relatedDefaultLimit = 4
	relatedMaxLimit     = 10
)

func normalizeRelatedLimit(limit int) int {
	switch {
	case limit <= 0:
		return relatedDefaultLimit
	case limit > relatedMaxLimit:
		return relatedMaxLimit
	default:
		return limit
	}
}

// relatedSQL 用一条语句找出「最相关」的已发布文章，不在 Go 里挨个遍历。
//
// 相关性排序由 shared_tags DESC 一次表达，对应的三档优先级：
//   - shared_tags >= 2：共同标签 2 个及以上，排最前（值越大越靠前）；
//   - shared_tags = 1 ：共同标签 1 个，其后；
//   - shared_tags = 0 ：只剩「同分类」这一种可能（WHERE 保证没有共同
//     标签时必然同分类才会入选），排在最后。
//
// 同档内部按 published_at 倒序（NULLS LAST 兼容管理后台改状态时
// published_at 漏设的脏数据）、再按 id 倒序与列表页保持一致。
//
// 用 Raw 而非 Model + Joins：SELECT 列表里要带一个以当前文章为参数的
// 相关子查询，写进 GORM 的 Select 字符串时占位符的展开顺序不好把控；
// Raw 的 ? 顺序即调用处参数顺序，最直观。Raw 不会像 Model 那样自动追加
// 软删除条件，因此 a.deleted_at IS NULL 与 a.status 都在 SQL 里显式声明。
var relatedSQL = `
SELECT a.id, a.title, a.slug, a.cover, a.views, a.published_at, a.excerpt,
       CASE WHEN a.view_password IS NULL OR a.view_password = '' THEN false ELSE true END AS has_password,
       (SELECT COUNT(*)
          FROM article_tags t1
          JOIN article_tags t2 ON t2.tag_id = t1.tag_id
         WHERE t1.article_id = ?
           AND t2.article_id = a.id) AS shared_tags
  FROM articles a
 WHERE a.id <> ?
   AND a.status = ?
   AND a.deleted_at IS NULL
   AND (
        a.category_id = (SELECT category_id FROM articles WHERE id = ? AND deleted_at IS NULL)
        OR EXISTS (
             SELECT 1
               FROM article_tags x1
               JOIN article_tags x2 ON x2.tag_id = x1.tag_id
              WHERE x1.article_id = ?
                AND x2.article_id = a.id
           )
       )
 ORDER BY shared_tags DESC, a.published_at DESC NULLS LAST, a.id DESC
 LIMIT ?`

// Related 返回与给定文章最相关的若干篇（limit 篇），不含自己。取不够
// limit 篇就少返回（例如文章没有标签也没有分类，或站内文章很少）。
//
// shared_tags 列只是为了 ORDER BY 引用计算别名，Article 模型里没有对应
// 字段，Scan 时 GORM 会直接忽略未匹配的列。
func (r *ArticleRepository) Related(articleID uint, limit int) ([]model.Article, error) {
	limit = normalizeRelatedLimit(limit)
	articles := make([]model.Article, 0, limit)
	err := r.db.Raw(relatedSQL,
		articleID,              // shared_tags 子查询：当前文章
		articleID,              // a.id <> ?
		model.ArticlePublished, // a.status = ?
		articleID,              // 同分类子查询：当前分类
		articleID,              // EXISTS：当前文章
		limit,                  // LIMIT
	).Scan(&articles).Error
	if err != nil {
		return nil, err
	}
	return articles, nil
}

// Neighbors 返回给定文章在「已发布文章按 published_at 倒序」序列中的
// 上一篇（更新的）与下一篇（更旧的），任一不存在时对应返回 nil。
//
// 「上一篇 / 下一篇」的语义（中文博客惯例，与英式 prev/next 相反，
// 改动命名或顺序前先读这段）：
//   - prev（上一篇）= 序列中排在前面的文章 = 发布时间**更新**的文章，
//     即「往前翻」看到的下一篇。published_at 倒序排列时，它落在当前
//     文章右侧更大的时间区间（published_at 相等时 id 更大者也算）。
//   - next（下一篇）= 序列中排在后面的文章 = 发布时间**更旧**的文章，
//     即「往后翻」看到的下一篇。
//
// published_at 为空的文章（草稿或后台改状态漏设时间）没有时间轴位置，
// 直接返回两个 nil。已软删的当前文章返回 ErrNotFound。
//
// 实现为两条 LIMIT 1 的轻量查询：整表按 published_at 排序再切片会把
// 全部文章读进内存，博客文章量上来后（数千篇纯文本 + 每篇几 KB 正文）
// 那是几十 MB 的无谓传输。
func (r *ArticleRepository) Neighbors(articleID uint) (prev, next *model.Article, err error) {
	var current model.Article
	if err := r.db.Select("articles.published_at").Take(&current, articleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	at := current.PublishedAt
	if at == nil {
		return nil, nil, nil
	}

	// prev：比当前文章「新」的已发布文章中最近的一篇。
	var prevArticle model.Article
	if err := r.db.Select(articleCardColumns).
		Where("articles.status = ? AND (articles.published_at > ? OR (articles.published_at = ? AND articles.id > ?))",
			model.ArticlePublished, at, at, articleID).
		Order("articles.published_at ASC, articles.id ASC").
		Limit(1).
		Find(&prevArticle).Error; err != nil {
		return nil, nil, err
	}
	if prevArticle.ID != 0 {
		prev = &prevArticle
	}

	// next：比当前文章「旧」的已发布文章中最近的一篇。
	var nextArticle model.Article
	if err := r.db.Select(articleCardColumns).
		Where("articles.status = ? AND (articles.published_at < ? OR (articles.published_at = ? AND articles.id < ?))",
			model.ArticlePublished, at, at, articleID).
		Order("articles.published_at DESC, articles.id DESC").
		Limit(1).
		Find(&nextArticle).Error; err != nil {
		return nil, nil, err
	}
	if nextArticle.ID != 0 {
		next = &nextArticle
	}

	return prev, next, nil
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
