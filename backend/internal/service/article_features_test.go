package service

import (
	"testing"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
)

func TestResolveExcerpt(t *testing.T) {
	content := "<p>这是一段用于测试的正文内容，长度足够触发摘要生成逻辑，需要超过一百二十个字符才能看到省略号出现，所以这里要多写一些字来凑够长度，继续继续继续继续继续继续。</p>"

	t.Run("作者填了摘要就优先用作者的", func(t *testing.T) {
		got := resolveExcerpt("  这是我的手写摘要  ", content)
		if got != "这是我的手写摘要" {
			t.Errorf("resolveExcerpt() = %q，想要 %q", got, "这是我的手写摘要")
		}
	})

	t.Run("只有空白的摘要等于没填", func(t *testing.T) {
		got := resolveExcerpt("   \n\t ", content)
		// 应该回落到从正文生成，而不是返回一串空白
		if got == "" {
			t.Error("空白摘要应回落到正文生成，不能返回空")
		}
		if got == "   \n\t " {
			t.Error("空白摘要不应原样返回")
		}
	})

	t.Run("没填则从正文生成", func(t *testing.T) {
		got := resolveExcerpt("", content)
		if got == "" {
			t.Fatal("空摘要应生成正文摘要")
		}
		if len([]rune(got)) > defaultExcerptRunes+1 {
			t.Errorf("摘要长度 %d 超过上限 %d", len([]rune(got)), defaultExcerptRunes)
		}
	})
}

func TestNormalizeArticleStatus(t *testing.T) {
	now := time.Now()

	t.Run("草稿与已发布原样通过", func(t *testing.T) {
		for _, s := range []string{model.ArticleDraft, model.ArticlePublished} {
			got, err := normalizeArticleStatus(s, nil)
			if err != nil || got != s {
				t.Errorf("normalizeArticleStatus(%q) = (%q, %v)", s, got, err)
			}
		}
	})

	t.Run("空状态回落草稿", func(t *testing.T) {
		got, err := normalizeArticleStatus("", nil)
		if err != nil || got != model.ArticleDraft {
			t.Errorf("空状态应回落草稿，得到 (%q, %v)", got, err)
		}
	})

	t.Run("未知状态也回落草稿", func(t *testing.T) {
		got, err := normalizeArticleStatus("weird", nil)
		if err != nil || got != model.ArticleDraft {
			t.Errorf("未知状态应回落草稿，得到 (%q, %v)", got, err)
		}
	})

	t.Run("定时发布缺少时间报错", func(t *testing.T) {
		if _, err := normalizeArticleStatus(model.ArticleScheduled, nil); err == nil {
			t.Error("scheduled 但没有 scheduled_at 应报错")
		}
	})

	t.Run("定时时间已过则直接按已发布", func(t *testing.T) {
		past := now.Add(-time.Hour)
		got, err := normalizeArticleStatus(model.ArticleScheduled, &past)
		if err != nil {
			t.Fatalf("过去时刻不该报错: %v", err)
		}
		// 关键行为：不能让文章永远等在一个已过去的时刻上
		if got != model.ArticlePublished {
			t.Errorf("已过去的时刻应按已发布处理，得到 %q", got)
		}
	})

	t.Run("未来时间保持 scheduled", func(t *testing.T) {
		future := now.Add(time.Hour)
		got, err := normalizeArticleStatus(model.ArticleScheduled, &future)
		if err != nil || got != model.ArticleScheduled {
			t.Errorf("未来时刻应保持 scheduled，得到 (%q, %v)", got, err)
		}
	})
}

func TestHashViewPassword(t *testing.T) {
	t.Run("空串返回空串（不设密码）", func(t *testing.T) {
		got, err := hashViewPassword("")
		if err != nil || got != "" {
			t.Errorf("空密码应返回空串，得到 (%q, %v)", got, err)
		}
	})

	t.Run("超长密码被拒绝", func(t *testing.T) {
		long := ""
		for i := 0; i < 33; i++ {
			long += "密"
		}
		if _, err := hashViewPassword(long); err == nil {
			t.Error("超过 32 字符应报错（bcrypt 只取前 72 字节，超长部分静默失效）")
		}
	})

	t.Run("哈希后不再是明文", func(t *testing.T) {
		hashed, err := hashViewPassword("secret123")
		if err != nil {
			t.Fatal(err)
		}
		if hashed == "secret123" {
			t.Error("绝不能在响应与日志里出现明文密码")
		}
	})
}

func TestCheckViewPassword(t *testing.T) {
	t.Run("未设密码的文章任何输入都通过", func(t *testing.T) {
		a := &model.Article{}
		if !CheckViewPassword(a, "") || !CheckViewPassword(a, "whatever") {
			t.Error("未设密码的文章应直接放行")
		}
	})

	t.Run("正确密码通过", func(t *testing.T) {
		hashed, _ := hashViewPassword("letmein")
		a := &model.Article{ViewPassword: hashed}
		if !CheckViewPassword(a, "letmein") {
			t.Error("正确密码应通过")
		}
	})

	t.Run("错误密码被拒", func(t *testing.T) {
		hashed, _ := hashViewPassword("letmein")
		a := &model.Article{ViewPassword: hashed}
		if CheckViewPassword(a, "wrong") {
			t.Error("错误密码应被拒绝")
		}
	})

	t.Run("空密码对加密文章被拒", func(t *testing.T) {
		hashed, _ := hashViewPassword("letmein")
		a := &model.Article{ViewPassword: hashed}
		if CheckViewPassword(a, "") {
			t.Error("空密码不能打开加密文章")
		}
	})
}

// fakePublisher 是 ArticlePublisher 的测试替身。
// returned 模拟"只发布一次"：真实 repository 用 UPDATE 保证幂等，
// 首次调用返回到期文章、之后返回空。
type fakePublisher struct {
	returned []model.Article
	err      error
	calls    int
}

func (f *fakePublisher) PublishDue(now time.Time) ([]model.Article, error) {
	f.calls++
	// 模拟"只返回一次"：真实 repository 用 UPDATE 保证幂等，
	// 这里首次返回到期的文章、之后返回空。
	out := f.returned
	f.returned = nil
	if f.err != nil {
		return nil, f.err
	}
	return out, nil
}

func TestScheduledPublisher(t *testing.T) {
	t.Run("扫描到到期文章就发布", func(t *testing.T) {
		fake := &fakePublisher{returned: []model.Article{{ID: 1, Title: "定时文"}}}
		p := NewScheduledPublisher(fake)
		p.RunOnceForTest()
		if fake.calls != 1 {
			t.Errorf("PublishDue 应被调用 1 次，实际 %d", fake.calls)
		}
	})

	t.Run("没有到期文章不报错", func(t *testing.T) {
		fake := &fakePublisher{}
		p := NewScheduledPublisher(fake)
		p.RunOnceForTest()
		if fake.calls != 1 {
			t.Errorf("PublishDue 应被调用 1 次，实际 %d", fake.calls)
		}
	})

	t.Run("失败不 panic，下次仍会重试", func(t *testing.T) {
		fake := &fakePublisher{err: errBoom}
		p := NewScheduledPublisher(fake)
		p.RunOnceForTest()
		p.RunOnceForTest()
		if fake.calls != 2 {
			t.Errorf("失败后应继续扫描，共 %d 次", fake.calls)
		}
	})

	t.Run("Stop 可重复调用", func(t *testing.T) {
		p := NewScheduledPublisher(&fakePublisher{})
		p.Stop()
		p.Stop() // 重复 close channel 会 panic，必须安全
	})
}

var errBoom = &boomError{}

type boomError struct{}

func (*boomError) Error() string { return "boom" }
