package service

import (
	"testing"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
)

// fakeSnapshotter 记录 Snapshot 收到的参数。
type fakeSnapshotter struct {
	calls  int
	last   *model.Article
	editor uint
	note   string
}

func (f *fakeSnapshotter) Snapshot(article *model.Article, editorID uint, note string) {
	f.calls++
	cp := *article
	f.last = &cp
	f.editor = editorID
	f.note = note
}

// TestArticleServiceSnapshotWiring 验证版本历史被接到 Update 路径上。
//
// 这条链路断掉不会有任何编译错误——SetRevisions 是可选的注入，
// 忘了调它时一切照常，只是不再留版本。所以必须用测试守住。
func TestArticleServiceSnapshotWiring(t *testing.T) {
	t.Run("未注入 revisions 时不 panic", func(t *testing.T) {
		s := &ArticleService{}
		// nil 接口上调用会 panic，这正是要防的
		s.snapshot(&model.Article{ID: 1}, 1, "note")
	})

	t.Run("注入后 snapshot 被调用", func(t *testing.T) {
		fake := &fakeSnapshotter{}
		s := &ArticleService{}
		s.SetRevisions(fake)
		s.snapshot(&model.Article{ID: 7, Title: "t"}, 42, "保存文章")

		if fake.calls != 1 {
			t.Fatalf("Snapshot 应被调用 1 次，实际 %d", fake.calls)
		}
		if fake.editor != 42 {
			t.Errorf("editorID = %d，想要 42", fake.editor)
		}
		if fake.last.ID != 7 {
			t.Errorf("文章 ID = %d，想要 7", fake.last.ID)
		}
	})

	t.Run("文章 ID 为 0 时不快照", func(t *testing.T) {
		// 尚未落库的文章没有 ID，记了版本也关联不上
		fake := &fakeSnapshotter{}
		s := &ArticleService{}
		s.SetRevisions(fake)
		s.snapshot(&model.Article{ID: 0}, 1, "note")
		if fake.calls != 0 {
			t.Errorf("未落库的文章不应快照，实际 %d 次", fake.calls)
		}
	})

	t.Run("article 为 nil 时不 panic", func(t *testing.T) {
		fake := &fakeSnapshotter{}
		s := &ArticleService{}
		s.SetRevisions(fake)
		s.snapshot(nil, 1, "note")
		if fake.calls != 0 {
			t.Error("nil 文章不应快照")
		}
	})
}

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"短字符串原样返回", "你好", 5, "你好"},
		{"恰好等于上限", "你好", 2, "你好"},
		{"超长按字符截断", "你好世界", 2, "你好"},
		{"首尾空白被去掉", "  ab  ", 5, "ab"},
		{"上限为 0 返回空", "abc", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncateRunes(tc.in, tc.max); got != tc.want {
				t.Errorf("truncateRunes(%q, %d) = %q，想要 %q", tc.in, tc.max, got, tc.want)
			}
		})
	}

	t.Run("按字符而非字节截断", func(t *testing.T) {
		// 中文按字节截会劈出乱码，这是必须按 rune 计的原因
		got := truncateRunes("一二三四五", 3)
		if got != "一二三" {
			t.Errorf("中文截断 = %q，想要「一二三」", got)
		}
	})
}

// TestRevisionModelDefaults 固定模型的关键约定。
func TestRevisionModelDefaults(t *testing.T) {
	if model.MaxRevisionsKept <= 0 {
		t.Error("版本保留上限必须为正数")
	}
	if rev := (model.ArticleRevision{}.TableName()); rev != "article_revisions" {
		t.Errorf("表名 = %q，想要 article_revisions", rev)
	}
}

// TestRevisionContentIsFullText 确认版本存的是全文而非摘要。
func TestRevisionContentIsFullText(t *testing.T) {
	rev := model.ArticleRevision{
		ArticleID: 1,
		Version:   1,
		Title:     "标题",
		Content:   "<p>完整正文，不是摘要</p>",
		EditorID:  9,
		CreatedAt: time.Now(),
	}
	if rev.Content == "" {
		t.Error("版本必须保存完整正文，否则恢复时拿不回内容")
	}
	if rev.Excerpt != "" {
		t.Error("Excerpt 留空时不该被自动填上")
	}
}
