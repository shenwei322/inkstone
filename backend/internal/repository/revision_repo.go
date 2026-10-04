package repository

import (
	"errors"

	"gorm.io/gorm"

	"github.com/shenwei/inkstone/backend/internal/model"
)

// ErrNoRevision 表示文章还没有任何历史版本。
var ErrNoRevision = errors.New("文章没有历史版本")

// RevisionRepository 负责文章历史版本的读写。
type RevisionRepository struct {
	db *gorm.DB
}

func NewRevisionRepository(db *gorm.DB) *RevisionRepository {
	return &RevisionRepository{db: db}
}

// List 返回某篇文章的历史版本，最新的在前。
//
// 不带 Content：版本列表只需要版本号、时间与改动说明。
// 每版都存全文，一次把几十版的正文全取回来是几十 MB 的无谓传输；
// 具体某一版的正文由 GetVersion 单独取。
func (r *RevisionRepository) List(articleID uint) ([]model.ArticleRevision, error) {
	var revisions []model.ArticleRevision
	err := r.db.Where("article_id = ?", articleID).
		Order("version DESC").
		Preload("Editor").
		Find(&revisions).Error
	return revisions, err
}

// Latest 取文章的最新一版，没有历史时返回 (nil, nil)。
//
// 单独一个方法而不是让调用方 List()[0]：List 会 Preload Editor
// 多跑一次 join，而 Snapshot 只需要比对标题与正文，
// 每次自动保存都要跑一次这个查询。
func (r *RevisionRepository) Latest(articleID uint) (*model.ArticleRevision, error) {
	var rev model.ArticleRevision
	err := r.db.Where("article_id = ?", articleID).
		Order("version DESC").Limit(1).Take(&rev).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &rev, nil
}

// GetVersion 取某一版的完整内容（含正文）。
func (r *RevisionRepository) GetVersion(articleID uint, version int) (*model.ArticleRevision, error) {
	var rev model.ArticleRevision
	err := r.db.Where("article_id = ? AND version = ?", articleID, version).
		Preload("Editor").First(&rev).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoRevision
		}
		return nil, err
	}
	return &rev, nil
}

// NextVersion 返回下一版应使用的版本号。
//
// 用 MAX(version)+1 而不是 COUNT(*)+1：删除中间的某一版后，
// COUNT 会让版本号回退，与已有版本撞号。
func (r *RevisionRepository) NextVersion(articleID uint) (int, error) {
	var max int
	err := r.db.Model(&model.ArticleRevision{}).
		Where("article_id = ?", articleID).
		Select("COALESCE(MAX(version), 0)").Scan(&max).Error
	if err != nil {
		return 0, err
	}
	return max + 1, nil
}

// Create 写入一个新版本，并裁掉超出上限的旧版本。
func (r *RevisionRepository) Create(rev *model.ArticleRevision) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(rev).Error; err != nil {
			return err
		}
		return pruneRevisions(tx, rev.ArticleID)
	})
}

// pruneRevisions 删除超出 maxRevisionsKept 的最旧版本。
//
// 用一条子查询 DELETE 而不是先查列表再逐条删：后者在并发保存时
// 可能把刚写进去的新版本也列进待删集合（读到的是同一批快照）。
func pruneRevisions(tx *gorm.DB, articleID uint) error {
	return tx.Exec(`
		DELETE FROM article_revisions
		 WHERE article_id = ?
		   AND id NOT IN (
		         SELECT id FROM (
		               SELECT id FROM article_revisions
		                WHERE article_id = ?
		                ORDER BY version DESC
		                LIMIT ?
		         ) keeped
		   )`,
		articleID, articleID, model.MaxRevisionsKept).Error
}

// DeleteByArticle 删除文章的全部历史版本（文章被彻底删除时调用）。
//
// 必须做：否则 Purge 文章后，历史表里仍留着那篇文章的几十版全文，
// 既占空间又让"彻底删除"名不副实。
func (r *RevisionRepository) DeleteByArticle(articleID uint) error {
	return r.db.Where("article_id = ?", articleID).Delete(&model.ArticleRevision{}).Error
}
