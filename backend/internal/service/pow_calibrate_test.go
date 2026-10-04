package service

import (
	"fmt"
	"sort"
	"strconv"
	"testing"
	"time"
)

// =====================================================================
// POW 参数标定（v2：低噪声版）
//
// 上一版用「平均值 + 整体求解一次」来测，结果被 GC 与调度抖动淹没——
// 甚至算出「哈希开销为负」这种物理上不可能的数。本版改为：
//
//   1. 把两个成本分量拆开单独测：建表、单次摘要；
//   2. 每组重复多次取中位数（中位数对偶发停顿不敏感）；
//   3. 求解类测量预先 GC 并固定表复用，避免把分配抖动算进求解成本。
//
// 关键结构性事实（决定了优化方向）：
//
//	客户端：建表一次 + 搜索 16^difficulty 个 nonce
//	服务端：建表一次 + 校验 1 个 nonce
//
// memoryMB 只影响「建表一次」这个一次性开销，不进入搜索循环——对攻击者的
// 搜索成本几乎没有影响，却 100% 落在服务端每次校验上。
// rounds 正相反：它乘在每一个候选 nonce 上，客户端付 16^d 次，服务端只付 1 次。
//
// 因此明确的优化方向是：调小 memoryMB、调大 rounds。
// 两者都由挑战签发时的快照下发（响应里的 memory_mb / rounds），
// 改默认值属于纯配置变更，前后端无需同步升级。
// =====================================================================

// powMedian 跑 n 次取中位数。sink 用来吃掉被测函数的返回值，
// 否则纯函数调用会被编译器当作死代码整体消除。
func powMedian(n int, f func()) time.Duration {
	ds := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		t0 := time.Now()
		f()
		ds = append(ds, time.Since(t0))
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	return ds[len(ds)/2]
}

// powPerCall 测「单次调用」的耗时：把 f 连续跑 reps 次放进一个计时区间再均摊。
//
// 必须这么做，因为 Windows 上 time.Now() 的实际分辨率约 0.5ms，
// 而单次 SHA-256 链只要 1~3µs——直接测一次的结果会被时钟粒度吞掉，
// 上面的 powMedian 就因此把摘要耗时测成了恒等于 0。
// 取多轮的中位数，兼顾「分辨率」与「抗偶发停顿」。
func powPerCall(reps, rounds int, f func()) time.Duration {
	ds := make([]time.Duration, 0, rounds)
	for i := 0; i < rounds; i++ {
		t0 := time.Now()
		for k := 0; k < reps; k++ {
			f()
		}
		ds = append(ds, time.Since(t0)/time.Duration(reps))
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	return ds[len(ds)/2]
}

var powSink string

// TestPowCalibrateComponents 分别量出「建表」「单次摘要」两个分量。
// 单位是每次调用的耗时，可用来推算任意 (memoryMB, rounds, difficulty) 组合。
func TestPowCalibrateComponents(t *testing.T) {
	const challenge = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	fmt.Println()
	fmt.Println("POW 成本分量标定（中位数；可据此推算任意参数组合）")
	fmt.Println("--------------------------------------------------------------------------")

	// 预热：让 xorshift 与 SHA-256 代码路径都进入稳定状态，避免第一组偏慢
	for i := 0; i < 3; i++ {
		powSink = powDigest(challenge, "warmup", 1, 4)
		powSink = powDigest(challenge, "warmup", 8, 4)
	}

	// 分量一：建表（只与 memoryMB 有关）
	buildCost := map[int]time.Duration{}
	for _, mb := range []int{1, 2, 4, 8, 16, 32} {
		m := mb
		d := powMedian(15, func() { powSink = digestWithTable(challenge, "x", powTable(m, challenge), 0) })
		buildCost[mb] = d
		fmt.Printf("建表 %2dMB：%8.3f ms\n", mb, float64(d.Microseconds())/1000)
	}

	fmt.Println()

	// 分量二：单次摘要（只与 rounds 有关；表先建好，复用）
	table := powTable(8, challenge)
	hashCost := map[int]time.Duration{}
	// 先测 rounds=0 的基线（只有一次 SHA-256，没有查表）
	base := powPerCall(20000, 7, func() {
		powSink = digestWithTable(challenge, "12345", table, 0)
	})
	fmt.Printf("摘要  0 轮：%8.3f µs  （基线：1 次 SHA-256）\n", float64(base.Nanoseconds())/1000)
	hashCost[0] = base
	for _, rounds := range []int{1, 2, 4, 8, 12, 16} {
		r := rounds
		d := powPerCall(20000, 7, func() {
			powSink = digestWithTable(challenge, "12345", table, r)
		})
		hashCost[rounds] = d
		fmt.Printf("摘要 %2d 轮：%8.3f µs  （含基线）\n", rounds, float64(d.Nanoseconds())/1000)
	}

	fmt.Println("--------------------------------------------------------------------------")

	// 分量三：一次完整求解 = 建表 + (期望尝试数 × 单次摘要)
	fmt.Println()
	fmt.Println("按分量推算的端到端成本（difficulty=4，期望 65536 次尝试）")
	fmt.Println("----------------------------------------------------------------------------------")
	fmt.Printf("%-8s %-8s %-14s %-14s %-12s\n", "表大小", "轮数", "客户端预估", "服务端预估", "服务端占比")
	fmt.Println("----------------------------------------------------------------------------------")
	expected := 65536.0
	for _, mb := range []int{1, 2, 4, 8} {
		for _, rounds := range []int{4, 8, 12, 16} {
			perAtt := float64(hashCost[rounds])
			build := float64(buildCost[mb])
			client := build + expected*perAtt
			server := build + perAtt // 服务端只校验一个 nonce
			fmt.Printf("%-8s %-8d %-14s %-14s %-12s\n",
				fmt.Sprintf("%dMB", mb), rounds,
				fmt.Sprintf("%.0f ms", client/1e6),
				fmt.Sprintf("%.3f ms", server/1e6),
				fmt.Sprintf("%.2f%%", server/client*100))
		}
	}
	fmt.Println("----------------------------------------------------------------------------------")
}

// TestPowTablePool 表缓冲池的正确性回归。
//
// 池是纯优化，绝不能改变结果：同样参数下取到的表必须与新建的表逐位相同。
// 这条同时盯着一个真实踩过的坑——acquirePowTable 的参数单位。
// 早先版本把「u32 个数」当成「MB 数」传进去，内部再乘 262144，
// 于是一次校验申请了 8TB 内存，进程直接 OOM 被杀。
// 现在接口只接受 MB，并且在越界时 panic 而不是去分配。
func TestPowTablePool(t *testing.T) {
	const challenge = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

	for _, mb := range []int{1, 2, 4, 8} {
		want := powTable(mb, challenge)

		// 反复取还，模拟多请求复用同一块缓冲
		for i := 0; i < 5; i++ {
			got := acquirePowTable(mb, challenge)
			if len(got) != len(want) {
				t.Fatalf("%dMB: 长度 %d，期望 %d", mb, len(got), len(want))
			}
			for k := range want {
				if got[k] != want[k] {
					t.Fatalf("%dMB: 第 %d 个元素 %d != %d（池污染）", mb, k, got[k], want[k])
				}
			}
			releasePowTable(got)
		}

		// 换了 challenge 也必须重新填，不能残留上一张表的内容
		other := acquirePowTable(mb, challenge)
		releasePowTable(other)
		want2 := powTable(mb, challenge+"x")
		got2 := acquirePowTable(mb, challenge+"x")
		for k := range want2 {
			if got2[k] != want2[k] {
				t.Fatalf("%dMB: 换 challenge 后表未重填（第 %d 个元素不符）", mb, k)
			}
		}
		releasePowTable(got2)

		// 池化路径与直算路径的摘要必须一致
		table := acquirePowTable(mb, challenge)
		viaPool := digestWithTable(challenge, "7", table, 8)
		releasePowTable(table)
		viaDirect := powDigest(challenge, "7", mb, 8)
		if viaPool != viaDirect {
			t.Errorf("%dMB: 池化摘要 %s != 直算摘要 %s", mb, viaPool, viaDirect)
		}
	}

	// 非正数返回 nil，不 panic
	for _, bad := range []int{-1, 0} {
		if got := acquirePowTable(bad, challenge); got != nil {
			t.Errorf("memMB=%d 应返回 nil，实际长度 %d", bad, len(got))
		}
	}
	// 越界必须 panic，而不是申请一个几 TB 的切片把进程打死
	for _, bad := range []int{powMaxMemoryMB + 1, 1 << 20} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("memMB=%d 应 panic", bad)
				}
			}()
			_ = acquirePowTable(bad, challenge)
		}()
	}
}

// TestPowRecommendedConfig 在候选默认值上做一次真实端到端计时（中位数），
// 验证「按分量推算」的结论在实际调用路径上也成立。
func TestPowRecommendedConfig(t *testing.T) {
	type cfg struct {
		mb, rounds int
		label      string
	}
	cfgs := []cfg{
		{8, 4, "现状（8MB / 4 轮）"},
		{2, 12, "候选 A（2MB / 12 轮）"},
		{1, 12, "候选 B（1MB / 12 轮）"},
		{1, 16, "候选 C（1MB / 16 轮）"},
		{4, 8, "候选 D（4MB / 8 轮）"},
	}

	fmt.Println()
	fmt.Println("候选默认值端到端实测（difficulty=4；客户端 5 次、服务端 25 次取中位数）")
	fmt.Println("----------------------------------------------------------------------------------")
	fmt.Printf("%-22s %-14s %-14s %-12s %-14s\n",
		"配置", "客户端求解", "服务端校验", "服务端占比", "相对现状提速")
	fmt.Println("----------------------------------------------------------------------------------")

	var baseVerify time.Duration
	for i, c := range cfgs {
		mb, rounds := c.mb, c.rounds
		pow, captcha := powTestEnv(t, map[string]string{
			SettingPowDifficulty: "4",
			SettingPowMemoryMB:   strconv.Itoa(mb),
			SettingPowRounds:     strconv.Itoa(rounds),
		})

		// 客户端求解：建表 + 搜索，走完整路径（powSolveOne 每次自行建表）。
		// 注意：求解是「搜到为止」，尝试次数服从几何分布，单次样本方差极大
		// （运气好可能几百次就中，运气差要几万次）。所以这里只作数量级参考，
		// 权威的期望值以 TestPowCalibrateComponents 的分量模型为准。
		solveMed := powMedian(3, func() {
			ch, d, m, r, _, _, err := pow.Issue()
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			powSink = powSolveOne(ch, d, m, r, 0)
		})

		// 服务端校验：必须在计时区间之外先把解算好，否则量到的是「求解+校验」。
		// 挑战一次性消费，所以每轮都重新签发一个并预先破解。
		//
		// 同样受 Windows ~0.5ms 时钟粒度限制：单次校验只要几百微秒，
		// 直接测一次会被粒度吞成 0。因此把 vN 次校验放进同一个计时区间再均摊。
		const vN = 40
		const verifyRounds = 3
		type crafted struct{ ch, nonce, signal string }
		batches := make([][]crafted, 0, verifyRounds)
		for rnd := 0; rnd < verifyRounds; rnd++ {
			batch := make([]crafted, 0, vN)
			for k := 0; k < vN; k++ {
				ch, d, m, r, me, _, err := pow.Issue()
				if err != nil {
					t.Fatalf("Issue: %v", err)
				}
				nonce := powSolveOne(ch, d, m, r, 0)
				if nonce == "" {
					t.Fatalf("未解出")
				}
				batch = append(batch, crafted{ch, nonce, powMakeSignal("robot", me)})
			}
			batches = append(batches, batch)
		}
		var verifyMed time.Duration
		{
			best := time.Duration(1<<62 - 1)
			for _, batch := range batches {
				t0 := time.Now()
				for _, it := range batch {
					if err := captcha.Verify("login", CaptchaParams{
						PowChallenge: it.ch, PowNonce: it.nonce, PowSignal: it.signal,
					}); err != nil {
						t.Fatalf("校验失败: %v", err)
					}
				}
				if avg := time.Since(t0) / vN; avg < best {
					best = avg
				}
			}
			verifyMed = best
		}

		if i == 0 {
			baseVerify = verifyMed
		}
		fmt.Printf("%-22s %-14s %-14s %-12s %-14s\n",
			c.label,
			fmt.Sprintf("%.0f ms", float64(solveMed.Microseconds())/1000),
			fmt.Sprintf("%.3f ms", float64(verifyMed.Microseconds())/1000),
			fmt.Sprintf("%.2f%%", float64(verifyMed)/float64(solveMed)*100),
			fmt.Sprintf("%.1fx", float64(baseVerify)/float64(verifyMed)))
	}
	fmt.Println("----------------------------------------------------------------------------------")
}
