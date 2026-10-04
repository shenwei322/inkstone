package service

import (
	"testing"
)

func TestValidateBulkIDs(t *testing.T) {
	t.Run("空列表报错", func(t *testing.T) {
		if _, err := validateBulkIDs(nil); err == nil {
			t.Error("空 id 列表应报错，而不是静默当成成功处理 0 篇")
		}
	})

	t.Run("只有 0 的列表也报错", func(t *testing.T) {
		if _, err := validateBulkIDs([]uint{0, 0}); err == nil {
			t.Error("全为 0 的 id 列表应报错")
		}
	})

	t.Run("丢弃 0 但保留有效 id", func(t *testing.T) {
		// 前端勾了行但没拿到 id（比如列表刷新过），
		// 不能因为一行脏数据让整批操作失败。
		got, err := validateBulkIDs([]uint{0, 5, 0, 7})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0] != 5 || got[1] != 7 {
			t.Errorf("清洗结果 = %v，想要 [5 7]", got)
		}
	})

	t.Run("去重", func(t *testing.T) {
		got, err := validateBulkIDs([]uint{3, 3, 3, 4})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Errorf("重复 id 未去重: %v", got)
		}
		// 去重不只为了好看：返回的 affected 会写进操作日志，
		// 不重复计数才不会让管理员以为删了比实际更多的文章。
	})

	t.Run("超限拒绝", func(t *testing.T) {
		tooMany := make([]uint, maxBulkIDs+1)
		for i := range tooMany {
			tooMany[i] = uint(i + 1)
		}
		if _, err := validateBulkIDs(tooMany); err == nil {
			t.Errorf("超过 %d 篇应拒绝（单条 UPDATE ... WHERE IN 会长时间持锁）", maxBulkIDs)
		}
	})

	t.Run("恰好等于上限可通过", func(t *testing.T) {
		exact := make([]uint, maxBulkIDs)
		for i := range exact {
			exact[i] = uint(i + 1)
		}
		got, err := validateBulkIDs(exact)
		if err != nil {
			t.Fatalf("恰好 %d 篇应通过: %v", maxBulkIDs, err)
		}
		if len(got) != maxBulkIDs {
			t.Errorf("应保留全部 %d 个 id，实际 %d", maxBulkIDs, len(got))
		}
	})
}
