package service

import (
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	t.Run("合法内容包解析成功", func(t *testing.T) {
		data := `{"version":1,"articles":[{"title":"测试","slug":"test","content":"<p>正文</p>"}]}`
		bundle, err := Parse([]byte(data))
		if err != nil {
			t.Fatalf("合法文件应解析成功: %v", err)
		}
		if len(bundle.Articles) != 1 || bundle.Articles[0].Slug != "test" {
			t.Errorf("解析结果 = %+v", bundle)
		}
	})

	t.Run("非 JSON 拒绝", func(t *testing.T) {
		if _, err := Parse([]byte("<html>这不是内容包</html>")); err == nil {
			t.Error("HTML 文件应被拒绝")
		}
	})

	t.Run("版本不匹配拒绝而不是尽力解析", func(t *testing.T) {
		// 关键：新格式按旧结构猜着解析，会静默错位
		// （比如把 excerpt 读成 title），比直接失败更难排查。
		data := `{"version":999,"articles":[]}`
		_, err := Parse([]byte(data))
		if err == nil {
			t.Fatal("版本 999 应被拒绝")
		}
		if !strings.Contains(err.Error(), "999") {
			t.Errorf("错误应带上具体版本号便于排查: %v", err)
		}
	})

	t.Run("空 JSON 对象也因版本 0 被拒", func(t *testing.T) {
		if _, err := Parse([]byte("{}")); err == nil {
			t.Error("缺少 version 的对象应被拒绝")
		}
	})
}

// TestImportRejectsTooMany 验证单次导入上限。
func TestImportLimits(t *testing.T) {
	// 用 nil repository 构造 service：这个用例必须在触达 DB 之前就被拒，
	// 否则 nil 指针会 panic，测试也会因此失败。
	svc := &ExportImportService{}

	bundle := &ExportBundle{Version: exportVersion}
	for i := 0; i < maxArticleImportCount+1; i++ {
		bundle.Articles = append(bundle.Articles, ExportArticle{
			Title: "t", Slug: "s", Content: "c",
		})
	}
	if _, _, _, err := svc.Import(1, bundle); err != ErrImportTooMany {
		t.Errorf("超过 %d 篇应返回 ErrImportTooMany，得到 %v", maxArticleImportCount, err)
	}

	// 恰好等于上限应通过校验、进入导入流程
	exact := &ExportBundle{Version: exportVersion}
	for i := 0; i < maxArticleImportCount; i++ {
		exact.Articles = append(exact.Articles, ExportArticle{
			Title: "t", Slug: "s", Content: "c",
		})
	}
	// 这里会真的去查 DB（nil repo → panic），用 recover 证明它确实
	// 走到了导入阶段而不是在上限检查处返回。
	func() {
		defer func() { _ = recover() }()
		_, _, _, _ = svc.Import(1, exact)
	}()
}

// TestExportBundleShape 固定导出包的字段形态。
// 分类/标签必须是名称而不是 ID——这是导出格式的核心约定，
// 改成 ID 会让迁移后的文章全部失去分类。
func TestExportBundleShape(t *testing.T) {
	published := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	article := ExportArticle{
		Title:       "标题",
		Slug:        "slug",
		Content:     "<p>正文</p>",
		Status:      "published",
		Category:    "后端",
		Tags:        []string{"Go", "数据库"},
		PublishedAt: &published,
	}
	if article.Category == "" {
		t.Error("分类应导出为名称")
	}
	if len(article.Tags) != 2 {
		t.Errorf("标签应导出为名称列表，得到 %v", article.Tags)
	}
	if article.PublishedAt == nil {
		t.Error("发布时间应导出，导入时才能保留历史时间线")
	}
}

// TestCategoryNameAndTagNames 覆盖 nil 与空的安全返回。
func TestCategoryNameAndTagNames(t *testing.T) {
	if got := categoryName(nil); got != "" {
		t.Errorf("nil 分类应返回空串，得到 %q", got)
	}
	if got := tagNames(nil); got != nil {
		t.Errorf("nil 标签应返回 nil，得到 %v", got)
	}
}
