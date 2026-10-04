// Package powstoretest 提供 POW 挑战存储的内存实现，供各包测试使用。
//
// 为什么不放在 `_test.go` 里：Go 的 `_test.go` 文件只在**本包**测试编译时
// 可见，handler 包的测试拿不到 service 包测试文件里的符号。而这份实现
// 有两个消费者（service 的验证测试、handler 的路由测试），复制两份必然漂移。
//
// 为什么不放在 service 包里：那会进生产二进制。放在独立包后，
// 只有测试文件 import 它，生产构建不会带上。
//
// 它刻意复刻 PostgreSQL 版的**可观察行为**而非内部结构：
//   - Take 取走即删（一次性消费），并发只有一个能拿到
//   - 过期挑战按 IssuedAt 判定，过期后 Take 返回 (nil, nil)
//   - CountActive 只数未过期的
package powstoretest

import (
	"sync"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
)

// Store 是内存版的挑战存储。
type Store struct {
	mu    sync.Mutex
	items map[string]*model.PowChallenge
}

// New 返回一个空的存储。
func New() *Store {
	return &Store{items: make(map[string]*model.PowChallenge)}
}

// Save 写入一条挑战。
func (s *Store) Save(c *model.PowChallenge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 复制一份存：调用方若继续改传入的结构体，不能影响已存记录——
	// 这正是「签发时快照」要保证的语义。
	cp := *c
	s.items[c.Challenge] = &cp
	return nil
}

// isExpired 判断某条挑战在 now 时刻是否已过期。
//
// 抽成方法而不是四处写 `IssuedAt.Add(TTL).Before(now)`：过期口径
// 必须与生产 SQL 完全一致，写四遍就有四个机会写歪。
func isExpired(rec *model.PowChallenge, now time.Time) bool {
	return rec.IssuedAt.Add(time.Duration(rec.TTLSeconds) * time.Second).Before(now)
}

// Take 原子地取走一条挑战：已过期则删除并返回 nil，否则返回被删的记录。
//
// **无条件删除，再判过期**。与生产 SQL 的语义一致：即使挑战已过期，
// 被提交时也要把行删掉，否则过期行只能等 GC，而提交过期挑战是
// 零成本攻击面，表会无限堆积。
func (s *Store) Take(challenge string, now time.Time) (*model.PowChallenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.items[challenge]
	if !ok {
		return nil, nil
	}
	delete(s.items, challenge)
	if isExpired(rec, now) {
		return nil, nil
	}
	cp := *rec
	return &cp, nil
}

// CountActive 返回仍未过期的挑战数。
func (s *Store) CountActive(now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, rec := range s.items {
		if !isExpired(rec, now) {
			n++
		}
	}
	return n, nil
}

// Delete 删除指定挑战。
func (s *Store) Delete(challenge string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, challenge)
	return nil
}

// DeleteExpired 清理过期挑战。
func (s *Store) DeleteExpired(now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for k, rec := range s.items {
		if isExpired(rec, now) {
			delete(s.items, k)
			n++
		}
	}
	return n, nil
}

// Expire 把指定挑战的签发时间推到很久以前，等价于等它自然过期。
//
// 存在的理由：TTL 最短 1 分钟，测试不可能真等。改时间戳比 sleep 可靠，
// 也比「构造一个 TTL 为负数的设置」更贴近真实过期路径——后者走的是
// 参数 clamp 分支，测不到过期判定本身。
func (s *Store) Expire(challenge string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec, ok := s.items[challenge]; ok {
		rec.IssuedAt = time.Now().Add(-24 * time.Hour)
	}
}

// Len 返回当前记录数（含已过期未清理的），供测试断言清理行为。
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}
