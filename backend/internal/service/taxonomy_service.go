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
		case errors.Is(err, repository.ErrTagNameConflict):
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

// maxMergeTags 限制单次合并的标签数。
// 合并要在事务里逐条 UPDATE + DELETE，数量过大时事务持有锁的时间过长，
// 会阻塞同一批文章的后台编辑。分批合并不影响结果。
const maxMergeTags = 50

// MergeTags 把 sourceIDs 指定的标签全部并入 targetID。
//
// 返回迁移的文章关联数（含清掉的重复行）——前端要显示
// 「已将 N 篇文章的标签迁移到『Go』」，这是管理员确认操作生效的依据。
//
// sourceIDs 与 targetID 相同时忽略那一项而非报错：管理界面里
// 把目标标签也勾进来源是常见误操作，提示「忽略目标标签本身」
// 比拒绝整批合并更好用。
func (s *TaxonomyService) MergeTags(targetID uint, sourceIDs []uint) (int64, error) {
	if targetID == 0 {
		return 0, NewValidationError("请选择目标标签")
	}
	clean := filterMergeSources(targetID, sourceIDs)
	if len(clean) == 0 {
		return 0, NewValidationError("请选择要合并的标签")
	}
	if len(clean) > maxMergeTags {
		return 0, NewValidationError("单次最多合并 50 个标签")
	}
	return s.taxonomy.MergeTags(targetID, clean)
}

// filterMergeSources 清洗合并来源列表：丢掉 0、丢掉目标自身、去重。
//
// 抽成纯函数是为了能单测：合并在 repository 里要跑 SQL，
// 而「哪些 id 会被排除」这种规则不该为了测它去连数据库。
func filterMergeSources(targetID uint, sourceIDs []uint) []uint {
	seen := make(map[uint]struct{}, len(sourceIDs))
	out := make([]uint, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		if id == 0 || id == targetID {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
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

/* ---------- 分类（含层级） ---------- */

// CategoryTree 返回分类树，以及每个分类自身的直接文章数。
//
// 树在这里构建而不是在 repository：repository 只负责"取数据"，
// 组装层级属于业务规则。构建过程是纯函数，可单独测。
func (s *TaxonomyService) CategoryTree() ([]model.CategoryNode, error) {
	cats, err := s.taxonomy.ListCategoryTree()
	if err != nil {
		return nil, err
	}
	counts, err := s.taxonomy.ListCategories()
	if err != nil {
		return nil, err
	}
	articleCount := make(map[uint]int64, len(counts))
	for _, c := range counts {
		articleCount[c.ID] = c.ArticleCount
	}
	return buildCategoryNodes(cats, articleCount), nil
}

// categoryDepth 计算分类所处层级（顶级为 1）。
//
// 脏数据一律判定为 1（顶级）：父分类不存在、父分类是自己、或父子互相
// 指向成环。这样处理有两个原因：
//   - 死循环：depth 上溯遇到环会永远走不完，必须有步数上限；
//   - 子树消失：若把 parent_id 悬空的节点丢掉，用户会以为整个分类被删了。
//
// 提升为顶级至少保证它仍在列表里可见，管理员能进去改。
func categoryDepth(nodes map[uint]*model.CategoryNode, id uint) int {
	// 步数上限用 MaxCategoryDepth*2：正常树最深 MaxCategoryDepth，
	// 多给一倍余量后仍未收敛即视为成环。
	maxSteps := model.MaxCategoryDepth * 2
	for step := 0; step <= maxSteps; step++ {
		node, ok := nodes[id]
		if !ok {
			// 走到不存在的祖先：起点节点的父链断了，按顶级算
			return 1
		}
		if node.ParentID == nil || *node.ParentID == id {
			return step + 1
		}
		id = *node.ParentID
	}
	return 1
}

// buildCategoryNodes 把扁平分类列表组装成树。
//
// 挂载顺序刻意「从最深层往最浅层」：
// CategoryNode.Children 是值切片，往父节点 append 会产生拷贝。若先挂浅层，
// 后挂的深层子节点写进的是 map 里的父，而浅层那份拷贝已经取走，
// 树就会丢子树。倒序挂载保证每次拷贝时子节点自身已完整。
func buildCategoryNodes(cats []model.Category, articleCount map[uint]int64) []model.CategoryNode {
	nodes := make(map[uint]*model.CategoryNode, len(cats))
	for i := range cats {
		c := cats[i]
		nodes[c.ID] = &model.CategoryNode{
			ID:           c.ID,
			Name:         c.Name,
			Slug:         c.Slug,
			ParentID:     c.ParentID,
			ArticleCount: articleCount[c.ID],
		}
	}

	depths := make(map[uint]int, len(cats))
	for id := range nodes {
		depths[id] = categoryDepth(nodes, id)
	}

	for d := model.MaxCategoryDepth; d >= 2; d-- {
		for _, c := range cats {
			if depths[c.ID] != d {
				continue
			}
			parent, ok := nodes[*c.ParentID]
			// 父缺失时该节点已被 categoryDepth 判为顶级，不会到这里
			if !ok {
				continue
			}
			child := *nodes[c.ID]
			child.Depth = d
			parent.Children = append(parent.Children, child)
		}
	}

	out := make([]model.CategoryNode, 0, len(cats))
	for _, c := range cats {
		if depths[c.ID] != 1 {
			continue
		}
		root := *nodes[c.ID]
		root.Depth = 1
		out = append(out, root)
	}
	return out
}

// CreateCategory 创建分类。parentID 为 0 表示顶级。
func (s *TaxonomyService) CreateCategory(name, slug string, parentID uint) (*model.Category, error) {
	name = strings.TrimSpace(name)
	slug = strings.TrimSpace(slug)
	if name == "" {
		return nil, NewValidationError("请填写分类名称")
	}
	if slug == "" {
		return nil, NewValidationError("请填写分类别名")
	}
	cat, err := s.taxonomy.CreateCategory(name, slug, parentID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrCategoryParentMissing):
			return nil, NewValidationError("父分类不存在")
		case errors.Is(err, repository.ErrCategoryTooDeep):
			return nil, NewValidationError("最多支持两级子分类")
		}
		return nil, err
	}
	return cat, nil
}

// UpdateCategory 重命名分类并调整父级。
func (s *TaxonomyService) UpdateCategory(id uint, name string, parentID uint) (*model.Category, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, NewValidationError("请填写分类名称")
	}
	cat, err := s.taxonomy.UpdateCategory(id, name, parentID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return nil, repository.ErrNotFound
		case errors.Is(err, repository.ErrCategoryParentMissing):
			return nil, NewValidationError("父分类不存在")
		case errors.Is(err, repository.ErrCategoryCycle):
			return nil, NewValidationError("不能把分类移动到它自己或它的子分类下")
		case errors.Is(err, repository.ErrCategoryTooDeep):
			return nil, NewValidationError("最多支持两级子分类")
		}
		return nil, err
	}
	return cat, nil
}

// DeleteCategory 删除分类。有子分类或有关联文章时返回可读错误。
func (s *TaxonomyService) DeleteCategory(id uint) error {
	if err := s.taxonomy.DeleteCategory(id); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			return repository.ErrNotFound
		case errors.Is(err, repository.ErrCategoryHasChildren):
			return NewValidationError("该分类下还有子分类，请先删除或移动子分类")
		case errors.Is(err, repository.ErrCategoryHasArticles):
			return NewValidationError("该分类下还有文章，请先移动或删除这些文章")
		}
		return err
	}
	return nil
}
