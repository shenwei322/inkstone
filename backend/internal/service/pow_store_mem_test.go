package service

import (
	"testing"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/service/powstoretest"
)

// =====================================================================
// 内存挑战存储的行为契约测试
//
// 生产实现是 repository.PowChallengeRepository（PostgreSQL 的
// DELETE ... RETURNING），本文件测的是 powstoretest.Store——
// service 与 handler 两层测试都靠它，所以它自己必须忠实复刻
// 生产语义：一次性消费、过期判定、并发互斥。
//
// 若这里的行为与生产实现不一致，上层测试会给出虚假的信心。
// =====================================================================

func newTestStore() *powstoretest.Store { return powstoretest.New() }

func sampleChallenge(name string, ttlSeconds int) *model.PowChallenge {
	return &model.PowChallenge{
		Challenge:  name,
		Difficulty: 3,
		MemMB:      1,
		Rounds:     2,
		MinEvents:  0,
		Scene:      "login",
		IssuedAt:   time.Now(),
		TTLSeconds: ttlSeconds,
	}
}

func TestMemStoreSaveAndTake(t *testing.T) {
	s := newTestStore()
	rec := sampleChallenge("aabb", 600)
	if err := s.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Take("aabb", time.Now())
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if got == nil {
		t.Fatal("刚存的挑战应能取到")
	}
	// 参数快照必须原样返回：验证时以快照为准，不是重读设置
	if got.Difficulty != 3 || got.MemMB != 1 || got.Rounds != 2 {
		t.Errorf("参数快照不一致：%+v", got)
	}
	if got.Scene != "login" {
		t.Errorf("场景绑定丢失：%q", got.Scene)
	}

	// 一次性消费：第二次取必须失败，否则同一个挑战能被无限重放
	again, err := s.Take("aabb", time.Now())
	if err != nil {
		t.Fatalf("第二次 Take 不该报错：%v", err)
	}
	if again != nil {
		t.Error("挑战被消费两次——重放防护失效")
	}
}

func TestMemStoreTakeMissing(t *testing.T) {
	s := newTestStore()
	got, err := s.Take("never-issued", time.Now())
	if err != nil {
		t.Fatalf("取不存在的挑战不该报错：%v", err)
	}
	if got != nil {
		t.Error("不存在的挑战应返回 nil")
	}
}

func TestMemStoreExpiry(t *testing.T) {
	s := newTestStore()
	_ = s.Save(sampleChallenge("expiring", 60))

	s.Expire("expiring")

	// 过期后 Take 必须返回 nil（与生产 SQL 的判过期等价），
	// 而且行必须已被删除——不能只判不过滤。
	// 否则提交过期挑战是零成本攻击面，表会无限堆积。
	got, err := s.Take("expiring", time.Now())
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if got != nil {
		t.Error("过期挑战不应通过验证")
	}
	if s.Len() != 0 {
		t.Errorf("过期挑战应被删除，剩余 %d 条", s.Len())
	}
}

// TestMemStoreTakeDeletesExpired 专门守住「过期也删」这条。
//
// 早先把过期条件写进 WHERE（删时顺带过滤），没过期的行会被取走删除，
// 已过期的行谁都删不掉，只能等每 2 分钟的 GC。攻击者反复提交同一个
// 过期挑战即可零成本堆积。生产 SQL 已改成无条件删除，这里同步守住。
func TestMemStoreTakeDeletesExpired(t *testing.T) {
	s := newTestStore()
	_ = s.Save(sampleChallenge("stale", 60))
	s.Expire("stale")

	// 反复提交同一个过期挑战
	for i := 0; i < 5; i++ {
		got, err := s.Take("stale", time.Now())
		if err != nil {
			t.Fatalf("第 %d 次 Take: %v", i+1, err)
		}
		if got != nil {
			t.Fatalf("第 %d 次 Take 应返回 nil", i+1)
		}
	}
	if s.Len() != 0 {
		t.Errorf("反复提交过期挑战后表应被清空，剩余 %d 条", s.Len())
	}
}

func TestMemStoreSaveSnapshotsCopy(t *testing.T) {
	s := newTestStore()
	rec := sampleChallenge("snap", 600)
	_ = s.Save(rec)

	// 存进去之后再改调用方的结构体，不能影响已存记录。
	// 这条守住「签发时快照」的语义：后台改设置不影响进行中的挑战。
	rec.Difficulty = 99
	rec.Scene = "register"

	got, _ := s.Take("snap", time.Now())
	if got == nil {
		t.Fatal("应能取到")
	}
	if got.Difficulty != 3 {
		t.Errorf("已存记录的难度被外部改动污染：%d", got.Difficulty)
	}
	if got.Scene != "login" {
		t.Errorf("已存记录的场景被外部改动污染：%q", got.Scene)
	}
}

func TestMemStoreCountActiveSkipsExpired(t *testing.T) {
	s := newTestStore()
	_ = s.Save(sampleChallenge("live", 600))
	_ = s.Save(sampleChallenge("dead", 600))
	s.Expire("dead")

	n, err := s.CountActive(time.Now())
	if err != nil {
		t.Fatalf("CountActive: %v", err)
	}
	// 限额检查必须只数未过期的：否则过期行占着额度，
	// 用户会在后台清理前一直看到「人机验证服务繁忙」
	if n != 1 {
		t.Errorf("活跃挑战数 = %d，想要 1（过期的那条不该计入）", n)
	}
	if s.Len() != 2 {
		t.Errorf("CountActive 不应删除记录，剩余 %d 条", s.Len())
	}
}

func TestMemStoreDeleteExpiredBefore(t *testing.T) {
	s := newTestStore()
	_ = s.Save(sampleChallenge("keep", 600))
	_ = s.Save(sampleChallenge("drop", 600))
	s.Expire("drop")

	n, err := s.DeleteExpired(time.Now())
	if err != nil {
		t.Fatalf("DeleteExpiredBefore: %v", err)
	}
	if n != 1 {
		t.Errorf("清理条数 = %d，想要 1", n)
	}
	if s.Len() != 1 {
		t.Errorf("应只剩未过期的 1 条，实际 %d", s.Len())
	}
	if _, err := s.Take("keep", time.Now()); err != nil {
		t.Error("未过期的挑战被误删")
	}
}

func TestMemStoreDelete(t *testing.T) {
	s := newTestStore()
	_ = s.Save(sampleChallenge("gone", 600))
	if err := s.Delete("gone"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if s.Len() != 0 {
		t.Error("Delete 未生效")
	}
	// 删不存在的不该报错（幂等），否则超限回滚路径会炸
	if err := s.Delete("gone"); err != nil {
		t.Errorf("重复 Delete 应幂等：%v", err)
	}
}

func TestMemStoreConcurrentTakeOnlyOneWins(t *testing.T) {
	s := newTestStore()
	_ = s.Save(sampleChallenge("race", 600))

	const workers = 16
	results := make(chan bool, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			<-start
			got, _ := s.Take("race", time.Now())
			results <- got != nil
		}()
	}
	close(start)

	wins := 0
	for i := 0; i < workers; i++ {
		if <-results {
			wins++
		}
	}
	// 并发只有一个能取到：这正是 DELETE...RETURNING 的语义，
	// 内存版若不互斥会让同一个挑战被算出多个答案
	if wins != 1 {
		t.Errorf("并发取到 %d 次，想要恰好 1 次", wins)
	}
}
