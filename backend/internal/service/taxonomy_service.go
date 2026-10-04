package service

import (
	"errors"
	"strings"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

// TaxonomyService 收敛标签/分类的写操作与校验。
//
// 此前这些逻辑写在 handler（admin_tag_handler.go 直接持有 TaxonomyRepository），
// 违反项目「handler → service → repository」的分层铁律：handler 只应做参数绑定、
// 调用 service 与响应映射。
type TaxonomyService struct {
	taxonomy *repository.TaxonomyRepository
}

func NewTaxonomyService(taxonomy *repository.TaxonomyRepository) *TaxonomyService {
	return &TaxonomyService{taxonomy: taxonomy}
}

// CreateTag 新建标签（同名标签存在时返回既有记录）。
func (s *TaxonomyService) CreateTag(name string) (*model.Tag, error) {
	if err := validateTagName(name); err != nil {
		return nil, err
	}
	tag, err := s.taxonomy.CreateTag(name)
	if err != nil {
		if errors.Is(err, repository.ErrInvalidInput) {
			return nil, NewValidationError("请填写标签名称")
		}
		return nil, err
	}
	return tag, nil
}

// UpdateTag 重命名标签；名称冲突返回 400 而不是 500。
func (s *TaxonomyService) UpdateTag(id uint, name string) (*model.Tag, error) {
	if err := validateTagName(name); err != nil {
		return nil, err
	}
	tag, err := s.taxonomy.UpdateTag(id, name)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, repository.ErrNotFound
		case errors.Is(err, repository.ErrTagNameTaken):
			return nil, NewValidationError("该标签名称已存在")
		case errors.Is(err, repository.ErrInvalidInput):
			return nil, NewValidationError("请填写标签名称")
		}
		return nil, err
	}
	return tag, nil
}

// DeleteTag 删除标签（连带清理文章关联）。
func (s *TaxonomyService) DeleteTag(id uint) error {
	if err := s.taxonomy.DeleteTag(id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return repository.ErrNotFound
		}
		return err
	}
	return nil
}

// TagName 返回标签名称，供删除前的审计日志记录；取不到时返回空串。
func (s *TaxonomyService) TagName(id uint) string {
	tags, err := s.taxonomy.ListTags()
	if err != nil {
		return ""
	}
	for i := range tags {
		if tags[i].ID == id {
			return tags[i].Name
		}
	}
	return ""
}

func validateTagName(name string) error {
	if strings.TrimSpace(name) == "" {
		return NewValidationError("请填写标签名称")
	}
	return nil
}
