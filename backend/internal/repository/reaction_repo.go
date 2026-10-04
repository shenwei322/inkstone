package repository

import (
	"errors"
	"fmt"
	"hash/fnv"

	"github.com/shenwei/inkstone/backend/internal/model"
	"gorm.io/gorm"
)

type ReactionRepository struct {
	db *gorm.DB
}

func NewReactionRepository(db *gorm.DB) *ReactionRepository {
	return &ReactionRepository{db: db}
}

// toggleLockKey 把 (article, user, type) 三元组折叠为 64 位 advisory 锁键。
// 用 NUL 作分隔符，避免 "1|23" 与 "12|3" 撞成同一个串；FNV-1a 分布均匀。
// 极小概率的哈希碰撞只会让两把无关的 toggle 互相多等一瞬，不影响正确性。
func toggleLockKey(articleID, userID uint, typ model.ReactionType) int64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d\x00%d\x00%s", articleID, userID, typ)
	return int64(h.Sum64()) // 位模式重解释为 bigint，pg_advisory_xact_lock 只要求不重复
}

// Toggle adds the reaction if absent, removes it if present. Returns whether
// the reaction is active afterwards plus the fresh count for that type.
//
// 并发正确性（重要，勿改回 ON CONFLICT DO NOTHING 版本）：
// 主键是 (user_id, article_id, type)。曾用 `INSERT ... ON CONFLICT DO NOTHING`
// 再按 RowsAffected 分支，它能消掉 23505，但**消不掉 toggle 交错**：
// 两个请求同时进来时 A 插入成功（RowsAffected=1 → active=true）、B 撞冲突
// （RowsAffected=0 → 走删除），于是 B 把 A 刚插入的行删掉——DB 里一行不剩，
// 而 A 已经向用户返回"已点赞"。前端显示的赞数与数据库不符，且无法自愈。
//
// 现在用 pg_advisory_xact_lock 把同一三元组的 toggle 串行化，锁随事务自动
// 释放；进入临界区后再 First 判定，语义确定。
func (r *ReactionRepository) Toggle(articleID, userID uint, typ model.ReactionType) (active bool, count int64, err error) {
	err = r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", toggleLockKey(articleID, userID, typ)).Error; err != nil {
			return err
		}

		var existing model.Reaction
		findErr := tx.Where("article_id = ? AND user_id = ? AND type = ?", articleID, userID, typ).
			First(&existing).Error
		switch {
		case findErr == nil:
			// 锁内已确认存在 → 取消
			if err := tx.Delete(&existing).Error; err != nil {
				return err
			}
			active = false
		case errors.Is(findErr, gorm.ErrRecordNotFound):
			// 锁内已确认不存在 → 点赞
			if err := tx.Create(&model.Reaction{
				ArticleID: articleID,
				UserID:    userID,
				Type:      typ,
			}).Error; err != nil {
				return err
			}
			active = true
		default:
			return findErr
		}

		// 计数与改动在同一个事务里读，保证返回给前端的数字就是提交后的值
		return tx.Model(&model.Reaction{}).
			Where("article_id = ? AND type = ?", articleID, typ).
			Count(&count).Error
	})
	return active, count, err
}

type ReactionStats struct {
	Likes     int64 `json:"likes"`
	Favorites int64 `json:"favorites"`
}

func (r *ReactionRepository) Counts(articleID uint) (*ReactionStats, error) {
	stats := &ReactionStats{}
	err := r.db.Model(&model.Reaction{}).
		Select("COALESCE(SUM(CASE WHEN type = 'like' THEN 1 ELSE 0 END), 0) as likes, COALESCE(SUM(CASE WHEN type = 'favorite' THEN 1 ELSE 0 END), 0) as favorites").
		Where("article_id = ?", articleID).
		Scan(stats).Error
	return stats, err
}

func (r *ReactionRepository) UserFlags(articleID, userID uint) (liked, favorited bool, err error) {
	var reactions []model.Reaction
	err = r.db.Where("article_id = ? AND user_id = ?", articleID, userID).Find(&reactions).Error
	if err != nil {
		return false, false, err
	}
	for _, reaction := range reactions {
		switch reaction.Type {
		case model.ReactionLike:
			liked = true
		case model.ReactionFavorite:
			favorited = true
		}
	}
	return liked, favorited, nil
}

// FavoriteArticles 返回某用户收藏的文章，按收藏时间倒序。
//
// 收藏按钮此前只能写不能读：Reaction 表里有 favorite 行，却没有任何按用户
// 聚合的查询，用户点完收藏就再也找不到那篇文章。这里补上读路径。
//
// 用 JOIN 而非两步查询：先取收藏再逐篇 FindByID 会有 N+1 次往返，
// 收藏夹页一次请求就够了。
func (r *ReactionRepository) FavoriteArticles(userID uint, page, pageSize int) ([]model.Article, int64, error) {
	base := r.db.Model(&model.Reaction{}).
		Where("reactions.user_id = ? AND reactions.type = ?", userID, model.ReactionFavorite)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var articles []model.Article
	err := r.db.
		Joins("JOIN reactions ON reactions.article_id = articles.id").
		Where("reactions.user_id = ? AND reactions.type = ?", userID, model.ReactionFavorite).
		Order("reactions.created_at DESC").
		Preload("Author").
		Preload("Category").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&articles).Error
	return articles, total, err
}
