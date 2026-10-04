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

// maxEmailCodeAttempts 的值必须与 service 的同名常量一致。
// 这里不能直接引用（会成环），复制并加一条测试守住同步：
// service 改了上限而这里不改，用例会测一个不成立的场景。
const maxEmailCodeAttempts = 5

// 用途常量在 service 包，repository 测试不能 import 它（会成环）。
// 复制一份：值必须与 service 的 PurposeLogin/PurposeResetPassword 一字不差，
// 否则这些用例测的是一个不会发生的场景。
const (
	testPurposeLogin         = "login"
	testPurposeResetPassword = "reset_password"
)

// =====================================================================
// 邮箱验证码仓储的集成测试（真实 PostgreSQL）
//
// 核心是 UPSERT 的原子性。若把「重发间隔检查」与「写入」拆成两次查询，
// 两个并发请求会同时通过检查、各发一封信，后写的那枚顶掉先前的——
// 用户拿第一枚来校验必然失败，而他看到的提示是"验证码不正确"。
// 这条链路的代价是用户侧完全无解，所以必须用真实库守住。
//
// 需要 INKSTONE_TEST_DSN，未设置则全部跳过。
// =====================================================================

func emailCodeTestDB(t *testing.T) *gorm.DB {
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
	if err := db.Migrator().DropTable(&model.EmailCode{}); err != nil {
		t.Fatalf("清理表: %v", err)
	}
	if err := db.AutoMigrate(&model.EmailCode{}); err != nil {
		t.Fatalf("建表: %v", err)
	}
	t.Cleanup(func() { _ = db.Migrator().DropTable(&model.EmailCode{}) })
	return db
}

func TestRepoEmailCodeUpsertEnforcesResendInterval(t *testing.T) {
	db := emailCodeTestDB(t)
	repo := NewEmailCodeRepository(db)

	base := time.Now()
	ok, err := repo.UpsertIfNotRecent("a@b.com", testPurposeLogin, "123456", base, base.Add(5*time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("首次写入: %v", err)
	}
	if !ok {
		t.Fatal("首枚码应写入成功（此前无记录）")
	}

	// 立刻再发：距 sentAt 不足 1 分钟，必须被拒
	ok, err = repo.UpsertIfNotRecent("a@b.com", testPurposeLogin, "654321", base.Add(10*time.Second), base.Add(5*time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("第二次写入报错: %v", err)
	}
	if ok {
		t.Error("重发间隔内应被拒绝——否则可被用于邮件轰炸")
	}

	// 库里必须还是第一枚码的内容
	rec, err := repo.Get("a@b.com", testPurposeLogin)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec == nil {
		t.Fatal("记录应存在")
	}
	if rec.CodeHash != HashEmailCode("123456") {
		t.Error("被拒的第二次写入不应覆盖第一枚码")
	}
}

func TestRepoEmailCodeUpsertAllowsAfterInterval(t *testing.T) {
	db := emailCodeTestDB(t)
	repo := NewEmailCodeRepository(db)

	base := time.Now()
	if _, err := repo.UpsertIfNotRecent("a@b.com", testPurposeLogin, "111111", base, base.Add(5*time.Minute), time.Minute); err != nil {
		t.Fatalf("首枚: %v", err)
	}
	// 距上次 90 秒 > 间隔 60 秒，应放行并覆盖
	ok, err := repo.UpsertIfNotRecent("a@b.com", testPurposeLogin, "222222", base.Add(90*time.Second), base.Add(90*time.Second).Add(5*time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("间隔后重发: %v", err)
	}
	if !ok {
		t.Error("超过重发间隔后应放行")
	}
	rec, _ := repo.Get("a@b.com", testPurposeLogin)
	if rec.CodeHash != HashEmailCode("222222") {
		t.Error("新码应覆盖旧码")
	}
	// 重发会重置尝试计数：用户重新拿到一枚干净的码
	if rec.Attempts != 0 {
		t.Errorf("重发应重置 attempts，实际 %d", rec.Attempts)
	}
}

// TestRepoEmailCodePurposeIsolation 验证同邮箱可并行持有不同用途的码。
//
// 这是把 purpose 放进主键的原因：用户可能同时发起"登录"和"重置密码"，
// 只按 email 作主键会让后申请的那枚顶掉前一枚。
func TestRepoEmailCodePurposeIsolation(t *testing.T) {
	db := emailCodeTestDB(t)
	repo := NewEmailCodeRepository(db)

	base := time.Now()
	for _, p := range []string{testPurposeLogin, testPurposeResetPassword} {
		if _, err := repo.UpsertIfNotRecent("a@b.com", p, "1000"+p[:2], base, base.Add(5*time.Minute), time.Minute); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}

	login, _ := repo.Get("a@b.com", testPurposeLogin)
	reset, _ := repo.Get("a@b.com", testPurposeResetPassword)
	if login == nil || reset == nil {
		t.Fatal("两枚码都应存在")
	}
	if login.CodeHash == reset.CodeHash {
		t.Error("不同用途被写成了同一枚码")
	}
}

func TestRepoEmailCodeConsumeSuccessDeletes(t *testing.T) {
	db := emailCodeTestDB(t)
	repo := NewEmailCodeRepository(db)

	base := time.Now()
	hash := HashEmailCode("424242")
	if _, err := repo.UpsertIfNotRecent("a@b.com", testPurposeLogin, "424242", base, base.Add(5*time.Minute), time.Minute); err != nil {
		t.Fatalf("写入: %v", err)
	}

	ok, err := repo.Consume("a@b.com", testPurposeLogin, hash, base.Add(time.Second))
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if !ok {
		t.Fatal("正确的码应被消费")
	}
	// 一次性：第二次必须失败
	ok, err = repo.Consume("a@b.com", testPurposeLogin, hash, base.Add(2*time.Second))
	if err != nil {
		t.Fatalf("第二次 Consume: %v", err)
	}
	if ok {
		t.Error("码被消费了两次——重放防护失效")
	}
}

func TestRepoEmailCodeConsumeExpired(t *testing.T) {
	db := emailCodeTestDB(t)
	repo := NewEmailCodeRepository(db)

	base := time.Now()
	if _, err := repo.UpsertIfNotRecent("a@b.com", testPurposeLogin, "424242", base, base.Add(time.Minute), time.Minute); err != nil {
		t.Fatalf("写入: %v", err)
	}

	// 2 分钟后校验：已过期
	ok, err := repo.Consume("a@b.com", testPurposeLogin, HashEmailCode("424242"), base.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if ok {
		t.Error("过期码不应通过")
	}
	// 过期码也应被删掉：用户重新申请时不被"重发间隔"挡死——
	// 那一行已经被清了，UPSERT 会走到 INSERT 分支
	rec, _ := repo.Get("a@b.com", testPurposeLogin)
	if rec != nil {
		t.Error("过期码应被删除，而不是留着占用重发间隔")
	}
}

func TestRepoEmailCodeAttemptsLockout(t *testing.T) {
	db := emailCodeTestDB(t)
	repo := NewEmailCodeRepository(db)

	base := time.Now()
	if _, err := repo.UpsertIfNotRecent("a@b.com", testPurposeLogin, "424242", base, base.Add(5*time.Minute), time.Minute); err != nil {
		t.Fatalf("写入: %v", err)
	}

	lastAttempts := 0
	for i := 0; i < maxEmailCodeAttempts; i++ {
		// 用错误的哈希触发失败累加
		attempts, exhausted, err := repo.RecordFailure("a@b.com", testPurposeLogin, maxEmailCodeAttempts)
		if err != nil {
			t.Fatalf("第 %d 次 RecordFailure: %v", i+1, err)
		}
		lastAttempts = attempts
		if i == maxEmailCodeAttempts-1 && !exhausted {
			t.Error("达到上限应标记 exhausted")
		}
	}
	if lastAttempts != maxEmailCodeAttempts {
		t.Errorf("累计 attempts = %d，想要 %d", lastAttempts, maxEmailCodeAttempts)
	}

	// 即使码本身正确，达到上限后也不该通过（Verify 层会作废）
	ok, err := repo.Consume("a@b.com", testPurposeLogin, HashEmailCode("424242"), time.Now())
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if ok {
		t.Error("尝试次数达上限后，正确的码也不应放行")
	}
}

func TestRepoEmailCodeConcurrentSendSingleWinner(t *testing.T) {
	db := emailCodeTestDB(t)
	repo := NewEmailCodeRepository(db)

	base := time.Now()
	const workers = 10
	results := make(chan bool, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		go func() {
			<-start
			// 用同一个 sentAt：并发时只有一个能插入，其余应被间隔挡下
			ok, err := repo.UpsertIfNotRecent("a@b.com", testPurposeLogin, "123456", base, base.Add(5*time.Minute), time.Minute)
			if err != nil {
				results <- false
				return
			}
			results <- ok
		}()
	}
	close(start)

	wins := 0
	for i := 0; i < workers; i++ {
		if <-results {
			wins++
		}
	}
	// 这条守住的是防邮件轰炸：若检查与写入不原子，
	// 并发会同时通过检查，每人一封信
	if wins != 1 {
		t.Errorf("并发发送有 %d 个成功，想要恰好 1 个", wins)
	}
}

func TestRepoEmailCodeClearExpired(t *testing.T) {
	db := emailCodeTestDB(t)
	repo := NewEmailCodeRepository(db)

	base := time.Now()
	for _, e := range []struct {
		email   string
		expires time.Time
	}{
		{"live@b.com", base.Add(5 * time.Minute)},
		{"dead@b.com", base.Add(-time.Minute)},
	} {
		// resendInterval 传 0：两行都要能写进去（第二次是不同邮箱，不受间隔限制）
		if _, err := repo.UpsertIfNotRecent(e.email, testPurposeLogin, "123456", base, e.expires, 0); err != nil {
			t.Fatalf("%s: %v", e.email, err)
		}
	}

	n, err := repo.ClearExpired(base)
	if err != nil {
		t.Fatalf("ClearExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("清理条数 = %d，想要 1", n)
	}
	if rec, _ := repo.Get("live@b.com", testPurposeLogin); rec == nil {
		t.Error("未过期的码被误删")
	}
}
