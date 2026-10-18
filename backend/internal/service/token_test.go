package service

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// =====================================================================
// 令牌 TTL 语义测试
//
// 「记住我」这条需求最容易出的错不是编译失败，而是**时长被悄悄吃掉**：
// 勾了记住我的用户过一周仍要重新登录。因此这里把三件事各自钉一条用例：
//   1. 不勾选 → 默认 TTL；
//   2. 勾选 → 长 TTL；
//   3. 刷新时**继承剩余时长**，而不是回到默认值。
// 第三条是回归的常见位置：Refresh 里顺手用 GeneratePair 就会退回默认值。
// =====================================================================

func TestGeneratePairTTLSelection(t *testing.T) {
	m := NewTokenManagerWithMax("secret", 15*time.Minute, 7*24*time.Hour, 90*24*time.Hour)

	def, err := m.GeneratePairWithTTL(1, "u", "user", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if def.SessionExpiresIn != int64((7 * 24 * time.Hour).Seconds()) {
		t.Errorf("默认会话时长 = %d，想要 7 天", def.SessionExpiresIn)
	}

	long, err := m.GeneratePairWithTTL(1, "u", "user", 0, RememberMeTTL)
	if err != nil {
		t.Fatal(err)
	}
	if long.SessionExpiresIn != int64((30 * 24 * time.Hour).Seconds()) {
		t.Errorf("记住我会话时长 = %d，想要 30 天", long.SessionExpiresIn)
	}
	// access 令牌的寿命不受记住我影响：它一直是 15 分钟。
	// 若这里跟着变长，等于把「短期凭据」的窗口拉大，与设计意图相反。
	if long.ExpiresIn != int64((15 * time.Minute).Seconds()) {
		t.Errorf("access 有效期 = %d，想要 15 分钟（不应受记住我影响）", long.ExpiresIn)
	}
}

// TestGeneratePairClampsToMax 验证超长请求被截断到上限而不是报错。
//
// 用户点「记住我一年」时，正确行为是给一个 90 天会话，而不是弹错误。
func TestGeneratePairClampsToMax(t *testing.T) {
	m := NewTokenManagerWithMax("secret", time.Minute, 7*24*time.Hour, 90*24*time.Hour)
	pair, err := m.GeneratePairWithTTL(1, "u", "user", 0, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if pair.SessionExpiresIn != int64((90 * 24 * time.Hour).Seconds()) {
		t.Errorf("超长请求应被截断到 90 天，实际 %d 秒", pair.SessionExpiresIn)
	}
}

// TestMaxNeverBelowDefault 验证上限不会被配成比默认值还小。
//
// max < refreshTTL 时若照抄 max，「记住我」会比不记住存活还短——明显的
// 配置事故，但不会有任何报错，只表现为用户勾了反而更早掉线。
func TestMaxNeverBelowDefault(t *testing.T) {
	m := NewTokenManagerWithMax("secret", time.Minute, 7*24*time.Hour, time.Hour)
	pair, err := m.GeneratePair(1, "u", "user", 0)
	if err != nil {
		t.Fatal(err)
	}
	if pair.SessionExpiresIn != int64((7 * 24 * time.Hour).Seconds()) {
		t.Errorf("上限小于默认值时应回落到默认，实际 %d 秒", pair.SessionExpiresIn)
	}
}

// TestRefreshInheritsRemainingTTL 验证刷新继承剩余时长（回归防护）。
//
// 旧实现用 GeneratePair（默认 TTL），会把「记住我 30 天」在第一次刷新后
// 打回 7 天：用户勾了记住我，一周后照样要重新登录，而且日志上看一切正常。
func TestRefreshInheritsRemainingTTL(t *testing.T) {
	m := NewTokenManagerWithMax("secret", 15*time.Minute, time.Hour, 30*24*time.Hour)

	// 模拟一个「已经过去一半寿命」的长会话令牌。
	ver := int64(0)
	claims := Claims{Ver: &ver}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(10 * 24 * time.Hour))
	remaining := claims.TTLOf(time.Now())
	if remaining < 9*24*time.Hour || remaining > 10*24*time.Hour {
		t.Fatalf("剩余时长计算异常：%v", remaining)
	}

	pair, err := m.GeneratePairWithTTL(1, "u", "user", 0, remaining)
	if err != nil {
		t.Fatal(err)
	}
	// 新令牌的时长应当接近 10 天而不是默认的 1 小时。
	if pair.SessionExpiresIn < int64((9 * 24 * time.Hour).Seconds()) {
		t.Errorf("刷新后会话时长被压缩为 %d 秒（应继承约 10 天）——"+
			"这正是「勾了记住我仍要频繁重登」的成因", pair.SessionExpiresIn)
	}
}

// TestTTLOfExpiredReturnsZero 验证过期令牌的剩余时长为 0。
//
// 负数会让调用方把 TTL 当成有效值传下去，生成一个「签出来就已过期」的
// 令牌，前端表现为无限重试刷新。
func TestTTLOfExpiredReturnsZero(t *testing.T) {
	ver := int64(0)
	claims := Claims{Ver: &ver}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
	if got := claims.TTLOf(time.Now()); got != 0 {
		t.Errorf("过期令牌剩余时长应为 0，实际 %v", got)
	}
}
