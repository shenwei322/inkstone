package service

import (
	"errors"
	"sync"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

// =====================================================================
// EmailCodeService 的单元测试辅助（内存 fake）
//
// 用 fake 而非真实 repository：本项目没有数据库测试基建，而
// Verify/Send 的分支（过期、用途不匹配、尝试上限、发信失败的补偿）
// 都是纯逻辑，值得脱离数据库跑。
//
// 与真实 repository 的行为一致性由
// repository/email_code_repo_integration_test.go 在真库上守住。
// 两处若漂移，这里测过的行为在生产上不成立。
// =====================================================================

// fakeEmailCodeStore 复刻 EmailCodeRepository 的可观察行为。
type fakeEmailCodeStore struct {
	mu      sync.Mutex
	records map[string]*fakeEmailRecord
}

type fakeEmailRecord struct {
	code      string
	purpose   string
	expiresAt time.Time
	sentAt    time.Time
	attempts  int
}

func newFakeEmailCodeStore() *fakeEmailCodeStore {
	return &fakeEmailCodeStore{records: make(map[string]*fakeEmailRecord)}
}

func fakeKey(email, purpose string) string { return email + "|" + purpose }

// UpsertIfNotRecent 对应真实 SQL 的 UPSERT ... WHERE sent_at <= cutoff。
// 并发原子性由真实库的行锁保证（integration test 有并发用例），
// 内存版用互斥锁等价实现——service 层的用例本不关心这一点。
func (f *fakeEmailCodeStore) UpsertIfNotRecent(email, purpose, code string, sentAt, expiresAt time.Time, interval time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if rec, ok := f.records[fakeKey(email, purpose)]; ok && rec.sentAt.After(sentAt.Add(-interval)) {
		return false, nil
	}
	f.records[fakeKey(email, purpose)] = &fakeEmailRecord{
		code: code, purpose: purpose,
		expiresAt: expiresAt, sentAt: sentAt,
	}
	return true, nil
}

func (f *fakeEmailCodeStore) Get(email, purpose string) (*model.EmailCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[fakeKey(email, purpose)]
	if !ok {
		return nil, nil
	}
	return &model.EmailCode{
		Email: email, Purpose: rec.purpose,
		CodeHash: repoHashCode(rec.code), Attempts: rec.attempts,
		ExpiresAt: rec.expiresAt, SentAt: rec.sentAt,
	}, nil
}

// Consume 无条件删除再判条件，与真实 SQL 行为一致。
func (f *fakeEmailCodeStore) Consume(email, purpose, codeHash string, now time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[fakeKey(email, purpose)]
	if !ok {
		return false, nil
	}
	delete(f.records, fakeKey(email, purpose))
	if now.After(rec.expiresAt) {
		return false, nil
	}
	if rec.attempts >= maxEmailCodeAttempts {
		return false, nil
	}
	return rec.code != "" && repoHashCode(rec.code) == codeHash, nil
}

func (f *fakeEmailCodeStore) RecordFailure(email, purpose string, max int) (int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[fakeKey(email, purpose)]
	if !ok {
		return 0, false, nil
	}
	rec.attempts++
	return rec.attempts, rec.attempts >= max, nil
}

func (f *fakeEmailCodeStore) Invalidate(email, purpose string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.records, fakeKey(email, purpose))
	return nil
}

func (f *fakeEmailCodeStore) ClearExpired(now time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int64
	for k, rec := range f.records {
		if now.After(rec.expiresAt) {
			delete(f.records, k)
			n++
		}
	}
	return n, nil
}

// fakeMailer 记录发送次数，可配置前 N 次失败。
type fakeMailer struct {
	mu    sync.Mutex
	calls int
	failN int
}

func (m *fakeMailer) Send(to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.failN > 0 {
		m.failN--
		return errors.New("smtp 不可达")
	}
	return nil
}

// repoHashCode 用与生产相同的哈希函数，保证 fake 里存的码
// 能和 service 计算出的哈希对上。service 已 import repository，无环。
func repoHashCode(code string) string { return repository.HashEmailCode(code) }

func newTestEmailCodeService(store *fakeEmailCodeStore, m *fakeMailer) *EmailCodeService {
	// settings 传 nil：走 settingDefaults 分支，各场景开关默认关闭
	return &EmailCodeService{
		settings: NewSettingsService(nil),
		mailer:   m,
		codes:    store,
	}
}
