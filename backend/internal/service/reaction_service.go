package service

import (
	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

type ReactionService struct {
	reactions *repository.ReactionRepository
	articles  *repository.ArticleRepository
}

func NewReactionService(reactions *repository.ReactionRepository, articles *repository.ArticleRepository) *ReactionService {
	return &ReactionService{reactions: reactions, articles: articles}
}

var validReactionTypes = map[model.ReactionType]bool{
	model.ReactionLike:     true,
	model.ReactionFavorite: true,
}

func (s *ReactionService) Toggle(articleID, userID uint, typ model.ReactionType) (bool, int64, error) {
	if !validReactionTypes[typ] {
		return false, 0, NewValidationError("无效的反应类型")
	}
	if _, err := s.articles.FindByID(articleID); err != nil {
		return false, 0, err
	}
	return s.reactions.Toggle(articleID, userID, typ)
}

// FavoriteArticles 返回某用户的收藏夹（文章 + 总数），供「个人中心 → 收藏」使用。
func (s *ReactionService) FavoriteArticles(userID uint, page, pageSize int) ([]model.Article, int64, error) {
	return s.reactions.FavoriteArticles(userID, page, pageSize)
}

func (s *ReactionService) Stats(articleID uint, userID uint, hasUser bool) (*repository.ReactionStats, bool, bool, error) {
	if _, err := s.articles.FindByID(articleID); err != nil {
		return nil, false, false, err
	}
	stats, err := s.reactions.Counts(articleID)
	if err != nil {
		return nil, false, false, err
	}
	var liked, favorited bool
	if hasUser {
		liked, favorited, err = s.reactions.UserFlags(articleID, userID)
		if err != nil {
			return nil, false, false, err
		}
	}
	return stats, liked, favorited, nil
}
