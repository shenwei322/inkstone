package service

import (
	"fmt"
	"strconv"
	"testing"
)

// 诊断用：直接统计「纯求解命中率」，完全绕开 Verify。
// 用来区分两种可能：(a) 求解/概率模型本身有偏差；(b) 求解对了但被 Verify
// 误判（那就是真 bug）。
func TestPowDiagBudgetHitRate(t *testing.T) {
	pow, _ := powTestEnv(t, map[string]string{SettingPowDifficulty: "3"})
	const expected = 4096
	for _, budget := range []int{4096, 12288} {
		const n = 400
		hit := 0
		totalTries := 0
		for i := 0; i < n; i++ {
			ch, d, mb, rounds, _, _, err := pow.Issue()
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			// 数一下实际用了多少次尝试才命中
			tbl := powTable(mb, ch)
			tries := 0
			ok := false
			for n2 := 0; n2 < budget; n2++ {
				tries = n2 + 1
				if leadingZeros(powDigestSharedTable(tbl, ch, strconv.Itoa(n2), rounds), d) {
					ok = true
					break
				}
			}
			if ok {
				hit++
				totalTries += tries
			}
		}
		theory := 1 - powBlockedTheory(budget, expected)
		fmt.Printf("budget=%-6d n=%d 命中率=%6.2f%% 理论=%6.2f%% 命中时平均尝试=%.0f（期望=%d）\n",
			budget, n, float64(hit)/float64(n)*100, theory*100,
			float64(totalTries)/float64(hit), expected)
	}

	// 大样本均匀性检验：用同一个挑战/同一张表，直接数「每尝试命中比例」。
	// 若底层哈希真的均匀，命中比例应为 1/4096 = 0.000244。
	const nAttempts = 4000000
	ch, d, mb, rounds, _, _, err := pow.Issue()
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	tbl := powTable(mb, ch)
	hits := 0
	for i := 0; i < nAttempts; i++ {
		if leadingZeros(powDigestSharedTable(tbl, ch, strconv.Itoa(i), rounds), d) {
			hits++
		}
	}
	p := float64(hits) / nAttempts
	want := 1.0 / float64(expected)
	sigma := powSqrt(float64(nAttempts) * want * (1 - want))
	fmt.Printf("均匀性：%d 次尝试命中 %d 次，实测比例 %.8f，期望 %.8f，偏差 %.2fσ\n",
		nAttempts, hits, p, want, (float64(hits)-float64(nAttempts)*want)/sigma)
	if dev := (float64(hits) - float64(nAttempts)*want) / sigma; dev > 4 || dev < -4 {
		t.Errorf("命中比例偏离均匀分布 %.2fσ，底层算法可能存在非均匀性", dev)
	}
}
