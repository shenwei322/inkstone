package repository

import (
	"testing"

	"github.com/shenwei/inkstone/backend/internal/model"
)

// TestToggleLockKeyUnambiguous 锁键必须无歧义：不同三元组不能撞出同一个
// 64 位键（撞了只会让无关 toggle 互相等待，不会错，但这里锁定设计意图）。
func TestToggleLockKeyUnambiguous(t *testing.T) {
	// 边界数字对：确认分隔符真的起了作用
	pairs := [][2]struct {
		a, u uint
		t    model.ReactionType
	}{
		{{1, 23, "like"}, {12, 3, "like"}},   // 数字边界错位
		{{1, 1, "like"}, {11, 1, "like"}},    // 前缀重叠
		{{1, 1, "like"}, {1, 1, "favorite"}}, // 类型不同
		{{100, 200, "like"}, {100, 200, "like"}},
	}
	seen := make(map[int64]string)
	for _, p := range pairs {
		k1 := toggleLockKey(p[0].a, p[0].u, p[0].t)
		k2 := toggleLockKey(p[1].a, p[1].u, p[1].t)
		if k1 == k2 && p[0] != p[1] {
			t.Errorf("锁键碰撞：%+v 与 %+v 都是 %d", p[0], p[1], k1)
		}
		// 确定性：同一输入必须拿到同一键（否则锁失效）
		if again := toggleLockKey(p[0].a, p[0].u, p[0].t); again != k1 {
			t.Errorf("锁键不确定：%d != %d", again, k1)
		}
		tag := string(p[0].t)
		if prev, ok := seen[k1]; ok && prev != tag {
			t.Errorf("键 %d 同时被 %q 与 %q 使用", k1, prev, tag)
		}
		seen[k1] = tag
	}
}
