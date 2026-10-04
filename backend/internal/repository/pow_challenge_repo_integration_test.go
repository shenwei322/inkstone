package repository

import (
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/shenwei/inkstone/backend/internal/model"
)

// =====================================================================
// POW 挑战仓储的集成测试（真实 PostgreSQL）
//
// 这些用例验证的是 SQL 语义本身，而不是 Go 逻辑：
//   1. DELETE ... RETURNING 能否真的返回被删的整行（GORM Scan 的行为）
//   2. TTL 过期判定的方向是否正确
//   3. 并发消费是否只有一个能拿到
//
// 第 1、2 条是真实踩过坑的地方：
//   - 曾写成 `issued_at > now()`（要求存于未来），
//     导致刚签发的挑战 100% 取不到——全部用户都会卡在验证环节；
//   - 也曾先 Exec 一条 DELETE 再 Raw 一条 RETURNING，第二条只会拿到零行。
// 所以这些用例是防回归的底线，不是锦上添花。
//
// 需要 INKSTONE_TEST_DSN 环境变量指向一个可写的测试库。
// 未设置时全部跳过（CI 未配数据库，不视为失败）。
// =====================================================================

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("INKSTONE_TEST_DSN")
	if dsn == "" {
		t.Skip("INKSTONE_TEST_DSN 未设置，跳过数据库集成测试")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("连不上测试库: %v", err)
	}
	// 独立 schema 命名空间：测试数据不污染开发库，
	// 也允许多个包同时跑各自的集成测试。
	if err := db.Migrator().DropTable(&model.PowChallenge{}); err != nil {
		t.Fatalf("清理表: %v", err)
	}
	if err := db.AutoMigrate(&model.PowChallenge{}); err != nil {
		t.Fatalf("建表: %v", err)
	}
	t.Cleanup(func() { _ = db.Migrator().DropTable(&model.PowChallenge{}) })
	return db
}

func TestRepoPowTakeReturnsDeletedRow(t *testing.T) {
	db := testDB(t)
	repo := NewPowChallengeRepository(db)

	now := time.Now()
	rec := &model.PowChallenge{
		Challenge:  "take0001",
		Difficulty: 4,
		MemMB:      8,
		Rounds:     4,
		MinEvents:  3,
		Scene:      "login",
		IssuedAt:   now,
		TTLSeconds: 600,
	}
	if err := repo.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 用签发后 1 秒的 now 调 Take：模拟"前端算完答案再提交"的时延。
	// 若过期判定方向写反（要求签发时间晚于当前），这里会拿到 nil。
	got, err := repo.Take("take0001", now.Add(time.Second))
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if got == nil {
		t.Fatal("刚签发的挑战应能取到——RETURNING 没返回行，检查 SQL 与列映射")
	}
	// 快照参数必须完整回来：Verify 全靠这些值重算
	if got.Difficulty != 4 || got.MemMB != 8 || got.Rounds != 4 || got.MinEvents != 3 {
		t.Errorf("参数快照不完整：%+v", got)
	}
	if got.Scene != "login" {
		t.Errorf("场景绑定丢失：%q", got.Scene)
	}

	// 一次性消费
	again, err := repo.Take("take0001", now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("第二次 Take: %v", err)
	}
	if again != nil {
		t.Error("挑战被消费了两次——重放防护失效")
	}
}

func TestRepoPowTakeRejectsExpired(t *testing.T) {
	db := testDB(t)
	repo := NewPowChallengeRepository(db)

	now := time.Now()
	rec := &model.PowChallenge{
		Challenge:  "expired01",
		TTLSeconds: 60, // 1 分钟
		IssuedAt:   now.Add(-2 * time.Minute),
	}
	if err := repo.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.Take("expired01", now)
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if got != nil {
		t.Error("已过期的挑战不应能被取走")
	}

	// 过期行应已被删除（DELETE 执行了，只是 WHERE 没匹配）
	var n int64
	if err := db.Model(&model.PowChallenge{}).Where("challenge = ?", "expired01").Count(&n).Error; err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Errorf("过期挑战仍留在表里（%d 行），应由 GC 清理", n)
	}
}

func TestRepoPowTakeMissingIsNotError(t *testing.T) {
	db := testDB(t)
	repo := NewPowChallengeRepository(db)

	// 没签发过的挑战：应返回 (nil, nil) 而不是报错。
	// 否则 Verify 会把「用户重复提交」变成 500。
	got, err := repo.Take("never-issued", time.Now())
	if err != nil {
		t.Fatalf("取不存在的挑战不应报错: %v", err)
	}
	if got != nil {
		t.Error("不存在的挑战应返回 nil")
	}
}

func TestRepoPowCountActive(t *testing.T) {
	db := testDB(t)
	repo := NewPowChallengeRepository(db)

	now := time.Now()
	live := &model.PowChallenge{Challenge: "live01", TTLSeconds: 600, IssuedAt: now}
	dead := &model.PowChallenge{Challenge: "dead01", TTLSeconds: 60, IssuedAt: now.Add(-2 * time.Minute)}
	if err := repo.Save(live); err != nil {
		t.Fatalf("Save live: %v", err)
	}
	if err := repo.Save(dead); err != nil {
		t.Fatalf("Save dead: %v", err)
	}

	n, err := repo.CountActive(now)
	if err != nil {
		t.Fatalf("CountActive: %v", err)
	}
	// 上限检查只数未过期的：否则过期行占额度，用户会一直看到"服务繁忙"
	if n != 1 {
		t.Errorf("活跃挑战数 = %d，想要 1", n)
	}

	deleted, err := repo.DeleteExpired(now)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if deleted != 1 {
		t.Errorf("清理条数 = %d，想要 1", deleted)
	}

	var left int64
	if err := db.Model(&model.PowChallenge{}).Count(&left).Error; err != nil {
		t.Fatalf("Count: %v", err)
	}
	if left != 1 {
		t.Errorf("清理后应剩 1 行，实际 %d", left)
	}
}

func TestRepoPowConcurrentTakeSingleWinner(t *testing.T) {
	db := testDB(t)
	repo := NewPowChallengeRepository(db)

	now := time.Now()
	if err := repo.Save(&model.PowChallenge{
		Challenge:  "race0001",
		TTLSeconds: 600,
		IssuedAt:   now,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 并发消费：DELETE...RETURNING 的行锁保证只有一个能删到。
	// 这条守住的是"同一个挑战不能被两个并发请求各验证一次"——
	// 若退化成 SELECT 再 DELETE，两个请求都会通过。
	const workers = 8
	type res struct {
		got *model.PowChallenge
		err error
	}
	results := make(chan res, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			<-start
			got, err := repo.Take("race0001", time.Now())
			results <- res{got, err}
		}()
	}
	close(start)

	wins := 0
	for i := 0; i < workers; i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("并发 Take 报错: %v", r.err)
		}
		if r.got != nil {
			wins++
		}
	}
	if wins != 1 {
		t.Errorf("并发取到 %d 次，想要恰好 1 次", wins)
	}
}
