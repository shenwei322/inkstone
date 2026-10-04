package model

import "time"

// 分类层级上限。多深的层级都会渲染成缩进列表，但同时也会让后台选择框
// 变得难用。3 层覆盖「技术 / 后端 / Go」这类常见分法，再深就该拆站了。
const MaxCategoryDepth = 3

type Category struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"uniqueIndex;size:64;not null" json:"name"`
	Slug      string    `gorm:"uniqueIndex;size:64;not null" json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// ParentID 指向父分类，nil 表示顶级。分类层级用「一颗树」表达，
	// 不引用第三方树形库——这里只需要两级遍历，自己写更可控。
	ParentID *uint      `gorm:"index" json:"parent_id"`
	Parent   *Category  `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	Children []Category `gorm:"foreignKey:ParentID" json:"children,omitempty"`
}

// IsRoot 报告是否为顶级分类。
func (c *Category) IsRoot() bool { return c.ParentID == nil }

// Depth 返回该分类所处的层级（顶级为 1）。
// 由调用方在已构建好的树上调用；对孤立节点返回 1。
func (c *Category) Depth() int {
	depth := 1
	cur := c
	// 上限保护：脏数据一旦成环（parent 指向自己或更上层），遍历会死循环。
	// 走到 MaxCategoryDepth*2 就断，宁可少算也不挂住请求。
	for cur.Parent != nil && depth < MaxCategoryDepth*2 {
		cur = cur.Parent
		depth++
	}
	return depth
}

// CategoryNode 是分类树的一个节点。
// 直接在 Category 上挂 Children 会带来 JSON 循环引用风险（GORM Preload
// 有时会把父也带上），所以对外统一用这个 DTO。
type CategoryNode struct {
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	ParentID *uint  `json:"parent_id"`
	Depth    int    `json:"depth"`
	// ArticleCount 是该分类**自身**的直接文章数（不含子分类的文章），
	// 与后台页面显示一致。子分类合计由前端按需累加。
	ArticleCount int64          `json:"article_count"`
	Children     []CategoryNode `json:"children,omitempty"`
}
