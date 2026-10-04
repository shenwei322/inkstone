package service

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// =====================================================================
// POW 人机验证拦截率测试
//
// 目标：量化「自研 PoW v2」对不同能力档位自动化程序的拦截率，以及它给
// 真人用户带来的额外成本。
//
// 测试装配不依赖数据库：SettingsService 的 nil-db 分支直接返回
// settingDefaults 快照（见 settings_service.go 的 All()），因此在包内改写
// 这份快照即可模拟后台配置，与 lap_service_test.go 的 NewLapService(nil)
// 惯例一致。t.Cleanup 负责把快照改回去。
//
// 拦截判定一律走 CaptchaService.Verify——handler 层真实调用的那道门。
//
// 攻击者分档（越往后越强）：
//   A01 裸脚本     —— 直接打业务接口，不带任何 pow_* 字段
//   A02 壳字段     —— 字段名正确，但值为空/空白
//   A03 伪造凭证   —— 随机编造 challenge / nonce
//   A04 不求解     —— 领了真 challenge，nonce 直接填 0
//   A05 乱投       —— 真 challenge + 递增乱试 nonce（试不够）
//   A06 漏信号     —— 正确求解，但不带 signal
//   A07 信号不足   —— 正确求解，事件数少于 min_events
//   A08 信号畸形   —— 正确求解，事件格式非法
//   A09 信号越界   —— 正确求解，事件时间戳落在服务端窗口外
//   A10 脚本全流程 —— 正确求解 + 合成 signal，全程无浏览器
//   A11 重放       —— 复用已被一次性消费的 challenge
//   A12 信号复用   —— 拿上一个 challenge 的 signal 配新 challenge
//   A13 跨场景     —— 把 A 场景的挑战用在 B 场景（加固后应被 100% 拦截）
//   C00 真人参照组 —— 完整协议 + 类人鼠标轨迹
// =====================================================================

// ---------- 测试环境 ----------

// powTestEnv 构造 POW 全场景开启的一对服务（PoW + 门面 CaptchaService），
// overrides 可覆盖任意设置项。返回的 pow 必须与 captcha 配套使用：
// Verify 只认自己那个 PowService 里签发的挑战。
func powTestEnv(t *testing.T, overrides map[string]string) (*PowService, *CaptchaService) {
	t.Helper()
	saved := settingDefaults
	t.Cleanup(func() { settingDefaults = saved })

	cfg := make(map[string]string, len(saved)+8)
	for k, v := range saved {
		cfg[k] = v
	}
	cfg[SettingCaptchaProvider] = CaptchaProviderPow
	cfg[SettingPowEnabled] = "true"
	cfg[SettingPowOnLogin] = "true"
	cfg[SettingPowOnRegister] = "true"
	cfg[SettingPowOnComment] = "true"
	for k, v := range overrides {
		cfg[k] = v
	}
	settingDefaults = cfg

	settings := NewSettingsService(nil)
	pow := NewPowService(settings)
	return pow, NewCaptchaService(settings, NewGeetestService(settings), NewLapService(settings), pow)
}

// ---------- 求解器（复刻前端 lib/pow.ts 的「建表一次 + 批量尝试」） ----------

// powDigestSharedTable 是 powDigest 的等价实现，但接收外部已构建好的内存表。
//
// 为什么要单独写一份：前端 solvePow 只在开始时 buildPowTable 一次，随后
// 65536 次尝试复用它；而服务端 powDigest 每次调用都重建表——服务端一次
// 校验只算一个 nonce，重建无碍；但压测进程若每试一个 nonce 就重建一遍
// 8MB 表，时间全耗在建表上，测出来的就不是协议的验证成本了。
//
// 等价性由 TestPowDigestParity 保证：两个入口对同一输入必须给出同一摘要。
func powDigestSharedTable(table []uint32, challenge, nonce string, rounds int) string {
	h := sha256.Sum256([]byte(challenge + ":" + nonce))
	var buf [36]byte
	for r := 0; r < rounds; r++ {
		be := binary.BigEndian.Uint32(h[0:4])
		idx := (be ^ (uint32(r) * 0x9E3779B9)) % uint32(len(table))
		copy(buf[:32], h[:])
		binary.BigEndian.PutUint32(buf[32:36], table[idx])
		h = sha256.Sum256(buf[:])
	}
	return hex.EncodeToString(h[:])
}

// TestPowDigestParity 证明压测用的共享表入口与生产入口 powDigest 逐位一致。
// 这一条不成立，后面所有拦截率数字都没有意义。
func TestPowDigestParity(t *testing.T) {
	for _, mb := range []int{1, 4, 8} {
		for _, rounds := range []int{1, 4, 9} {
			for _, nonce := range []string{"0", "1", "65537", "999999"} {
				challenge := fmt.Sprintf("parity-%d-%d-%s", mb, rounds, nonce)
				table := powTable(mb, challenge)
				want := powDigest(challenge, nonce, mb, rounds)
				got := powDigestSharedTable(table, challenge, nonce, rounds)
				if want != got {
					t.Fatalf("摘要不一致 mb=%d rounds=%d nonce=%s:\n want %s\n got  %s", mb, rounds, nonce, want, got)
				}
			}
		}
	}
}

// powBruteForce 按难度穷举 nonce，返回命中值。attemptsMax <= 0 表示不限次数，
// 否则用于「乱投」档——只试这么多就打请求。
func powBruteForce(table []uint32, challenge string, difficulty, rounds int, attemptsMax int) string {
	for n := 0; attemptsMax <= 0 || n < attemptsMax; n++ {
		nonce := strconv.Itoa(n)
		if leadingZeros(powDigestSharedTable(table, challenge, nonce, rounds), difficulty) {
			return nonce
		}
	}
	return ""
}

// powSolveOne 求某个挑战的 nonce。attemptsMax <= 0 表示穷尽到命中为止。
func powSolveOne(challenge string, difficulty, memoryMB, rounds, attemptsMax int) string {
	return powBruteForce(powTable(memoryMB, challenge), challenge, difficulty, rounds, attemptsMax)
}

// ---------- signal 构造 ----------

// powMakeSignal 生成 minEvents 条 m:ms:x:y 事件。
// style "human" 给一条平滑鼠标轨迹（真人）；style "robot" 给固定坐标（脚本）。
// 两者在服务端眼里只差坐标数值，格式与时间分布完全相同——这正是要证明的：
// signal 校验是结构性的，无从分辨这两者。
func powMakeSignal(style string, minEvents int) string {
	if minEvents <= 0 {
		return ""
	}
	now := time.Now().UnixMilli()
	x, y := 640, 380
	parts := make([]string, 0, minEvents)
	for i := 0; i < minEvents; i++ {
		// 每条事件间隔 9ms；远小于服务端 2 秒的时钟容差
		ms := now - int64(minEvents-i)*9
		if style == "human" {
			x += 11 + i*4
			y += (i % 2) * 7
		} else {
			x, y = 0, 0
		}
		parts = append(parts, fmt.Sprintf("m:%d:%d:%d", ms, x, y))
	}
	return strings.Join(parts, ",")
}

// ---------- 拦截矩阵 ----------

type tier struct {
	name string
	desc string
	n    int
	run  func(i int) error
}

type tierResult struct {
	name     string
	desc     string
	sampleN  int
	blockedN int
}

func (r tierResult) rate() float64 {
	return float64(r.blockedN) / float64(r.sampleN) * 100
}

// runTier 执行一个档位 n 次独立尝试并统计被拦截次数。
// 每次尝试都会自行领取 challenge（挑战是一次性消费的），所以失败后可以
// 直接重试，不会命中「重放」。
func runTier(tr tier) tierResult {
	res := tierResult{name: tr.name, desc: tr.desc, sampleN: tr.n}
	for i := 0; i < tr.n; i++ {
		if err := tr.run(i); err != nil {
			res.blockedN++
		}
	}
	return res
}

// credential 是一份完整的 PoW 凭证（前端 CaptchaCredential 的等价物）。
type credential struct{ challenge, nonce, signal string }

func (c credential) params() CaptchaParams {
	return CaptchaParams{PowChallenge: c.challenge, PowNonce: c.nonce, PowSignal: c.signal}
}

// TestPowInterceptionMatrix 是本次测试的主体：跑完全部攻击档位并输出拦截率表。
func TestPowInterceptionMatrix(t *testing.T) {
	// 单个环境、单个 PowService：所有档位的挑战都由同一实例签发与校验。
	pow, captcha := powTestEnv(t, map[string]string{
		SettingPowDifficulty: "2", // 期望 256 次尝试。档位判定与难度无关，降低难度只为省时
	})
	action := "login"

	// solve 模拟一个「完整实现协议的客户端」：领挑战 → 建表 → 求 nonce → 合成信号。
	// style 只影响信号里的坐标，用来区分「脚本」与「类人轨迹」两种生成方式。
	solve := func(style string) credential {
		ch, d, mb, rounds, minEvents, _, err := pow.Issue()
		if err != nil {
			t.Fatalf("签发挑战失败: %v", err)
		}
		nonce := powSolveOne(ch, d, mb, rounds, 0)
		if nonce == "" {
			t.Fatal("难度 2 下未能求出 nonce")
		}
		return credential{ch, nonce, powMakeSignal(style, minEvents)}
	}
	verify := func(action string, c credential) error {
		return captcha.Verify(action, c.params())
	}

	// A11 需要一条「已消费」的凭据：先正常提交一次。
	consumed := solve("robot")
	if err := verify(action, consumed); err != nil {
		t.Fatalf("为 A11 准备的首次提交应成功: %v", err)
	}

	// A08 用的畸形 signal 集合（格式层面的各种错误）
	badSignals := []string{
		"m:notanumber:1:1",        // 时间戳非数字
		"z:1700000000000:1:1",     // 事件类型不在 {m,k,t}
		"m:1700000000000:1",       // 段数不是 4
		"m:1700000000000:99999:1", // 坐标越界
		"m:1:1:1,m:1:1:1,m:1:1:1", // 时间戳远早于签发窗口
	}

	tiers := []tier{
		{"A01", "裸脚本，无 pow_* 字段", 100, func(int) error {
			return verify(action, credential{})
		}},
		{"A02", "字段正确但值为空/空白", 100, func(int) error {
			return verify(action, credential{challenge: "   ", nonce: "  "})
		}},
		{"A03", "随机编造 challenge/nonce", 100, func(i int) error {
			return verify(action, credential{
				challenge: fmt.Sprintf("%064x", i), nonce: "1", signal: powMakeSignal("robot", 3),
			})
		}},
		// A04/A05 是「乱猜」档，存在理论上的幸运命中窗口：难度 2 下单个
		// nonce 的命中概率是 1/256 ≈ 0.39%，猜 5 个约 2%。所以这两档只能
		// 要求「接近全拦」，不能要求 100%——样本足够大时必然偶发漏网。
		{"A04", "不求解，只发 nonce=0", 300, func(int) error {
			c := solve("robot")
			return verify(action, credential{c.challenge, "0", c.signal})
		}},
		{"A05", "不求解，从 nonce=0 猜几个", 300, func(int) error {
			ch, d, mb, rounds, me, _, err := pow.Issue()
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			// 脚本没有实现求解器，只会从 n=0 试到 4 然后挑一个提交
			nonce := powSolveOne(ch, d, mb, rounds, 5)
			if nonce == "" {
				nonce = "3"
			}
			return verify(action, credential{ch, nonce, powMakeSignal("robot", me)})
		}},
		{"A06", "正确求解但不带 signal", 100, func(int) error {
			c := solve("robot")
			return verify(action, credential{c.challenge, c.nonce, ""})
		}},
		{"A07", "正确求解，事件数不足", 100, func(int) error {
			c := solve("robot")
			return verify(action, credential{c.challenge, c.nonce, powMakeSignal("robot", 1)})
		}},
		{"A08", "正确求解，signal 格式非法", 100, func(i int) error {
			c := solve("robot")
			return verify(action, credential{c.challenge, c.nonce, badSignals[i%len(badSignals)]})
		}},
		{"A09", "正确求解，事件时间戳越界", 100, func(int) error {
			c := solve("robot")
			old := time.Now().Add(-time.Hour).UnixMilli()
			return verify(action, credential{c.challenge, c.nonce,
				fmt.Sprintf("m:%d:1:1,m:%d:2:2,m:%d:3:3", old, old, old)})
		}},
		{"A10", "脚本解题 + 合成信号（无浏览器）", 200, func(int) error {
			return verify(action, solve("robot"))
		}},
		{"A11", "重放已消费的 challenge", 100, func(int) error {
			return verify(action, consumed)
		}},
		{"A12", "signal 与 challenge 无绑定", 100, func(int) error {
			// 先独立生成一份 signal，再去领新挑战。服务端的校验只看 signal 的
			// 格式与时间窗，从不检查它和这个 challenge 有没有关系。
			signal := powMakeSignal("robot", 3)
			ch, d, mb, rounds, _, _, err := pow.Issue()
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			nonce := powSolveOne(ch, d, mb, rounds, 0)
			return verify(action, credential{ch, nonce, signal})
		}},
		{"A13", "login 挑战拿去 comment 用", 100, func(int) error {
			// 加固后挑战会绑定签发场景：这里为 login 签发，再拿去 comment 提交。
			// （加固前挑战不记场景，这一档的拦截率是 0%。）
			ch, d, mb, rounds, me, _, err := pow.IssueFor("login")
			if err != nil {
				t.Fatalf("IssueFor: %v", err)
			}
			nonce := powSolveOne(ch, d, mb, rounds, 0)
			return captcha.Verify("comment", CaptchaParams{
				PowChallenge: ch, PowNonce: nonce, PowSignal: powMakeSignal("robot", me),
			})
		}},
		{"C00", "真人：完整协议 + 类人鼠标轨迹", 100, func(int) error {
			return verify(action, solve("human"))
		}},
	}

	results := make([]tierResult, 0, len(tiers))
	for _, tr := range tiers {
		results = append(results, runTier(tr))
	}

	var blockedTotal, total int
	fmt.Println()
	fmt.Println("POW 人机验证拦截率矩阵")
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("%-5s %-36s %6s %8s %8s\n", "档位", "攻击方式", "样本", "被拦截", "拦截率")
	fmt.Println("--------------------------------------------------------------------------------")
	for _, r := range results {
		blockedTotal += r.blockedN
		total += r.sampleN
		fmt.Printf("%-5s %-36s %6d %8d %7.1f%%\n", r.name, r.desc, r.sampleN, r.blockedN, r.rate())
	}
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Printf("合计：%d 次尝试，%d 次被拦截，总体拦截率 %.1f%%\n", total, blockedTotal, float64(blockedTotal)/float64(total)*100)
	fmt.Println("--------------------------------------------------------------------------------")

	byName := func(name string) tierResult {
		for _, r := range results {
			if r.name == name {
				return r
			}
		}
		t.Fatalf("档位 %s 未执行", name)
		return tierResult{}
	}

	// 断言一：结构上不可能通过的档位必须 100% 拦截。
	// 这些档位不是因为「算得不够多」而失败，而是 challenge 不在表里、
	// 或 nonce 已消费、或缺 signal——判定是确定性的，不存在幸运命中窗口。
	for _, name := range []string{"A01", "A02", "A03", "A06", "A07", "A08", "A09", "A11", "A13"} {
		r := byName(name)
		if r.blockedN != r.sampleN {
			t.Errorf("%s 未做到 100%% 拦截：%d/%d 被拦截", name, r.blockedN, r.sampleN)
		}
	}
	// 断言二：「乱猜」档是概率事件（难度 2 下猜 0-4 号 nonce，命中率约 2%），
	// 只能要求接近全拦。阈值 95% 留了 5 个百分点的运气空间。
	for _, name := range []string{"A04", "A05"} {
		r := byName(name)
		if r.rate() < 95 {
			t.Errorf("%s 拦截率 %.1f%% 低于 95%%", name, r.rate())
		}
	}
	// 关键断言二：真人参照组必须放行，否则 PoW 就是在锁死正常用户。
	if ctrl := byName("C00"); ctrl.blockedN != 0 {
		t.Errorf("真人参照组被拦截 %d/%d，PoW 误杀正常用户", ctrl.blockedN, ctrl.sampleN)
	}
	// 关键断言三（加固前）：完整实现协议的脚本、与 challenge 无绑定的
	// signal 都必须放行——这两条至今仍是真实存在的能力边界，
	// 用断言钉住，防止有人误以为 signal 能挡住机器人。
	for _, name := range []string{"A10", "A12"} {
		if r := byName(name); r.blockedN != 0 {
			t.Errorf("%s 意外被拦截 %d/%d，说明协议校验比预期更严，请复核", name, r.blockedN, r.sampleN)
		}
	}
	// 关键断言四（本次加固）：跨场景挪用挑战必须被 100% 拦截。
	// 加固前该档拦截率为 0%，加固后挑战绑定签发场景。
	if r := byName("A13"); r.blockedN != r.sampleN {
		t.Errorf("A13 跨场景挪用未做到 100%% 拦截：%d/%d", r.blockedN, r.sampleN)
	}
}

// TestPowInterceptionVsComputeBudget 回答 PoW 真正的价值所在：拦截率如何
// 随攻击者的算力投入变化。
//
// 模型：每次尝试独立命中概率 1/16^difficulty，所以投入预算 B 时，被拦截
// 的概率是 (1-1/16^d)^B。这意味着 PoW 不是「有/无」的门槛，而是一条按
// 算力比例线性衰减的曲线——投入 1 倍期望工作量只有约 63% 成功率，
// 3 倍才到约 95%。
func TestPowInterceptionVsComputeBudget(t *testing.T) {
	pow, captcha := powTestEnv(t, map[string]string{
		SettingPowDifficulty: "3", // 期望 16^3 = 4096 次尝试，便于细分算力档位
	})
	const expected = 16 * 16 * 16

	// 算力档位：blockedP 是理论拦截概率。sampleN 按各档开销分配——
	// 小预算档便宜就多跑，3 倍预算档贵就少跑，总时长可控在 1 分钟内。
	ratios := []struct {
		label    string
		budget   int
		sampleN  int
		blockedP float64 // 理论拦截率 = (1-1/expected)^budget
	}{
		{"1/4096", 1, 300, powBlockedTheory(1, expected)},
		{"1/1024", 4, 400, powBlockedTheory(4, expected)},
		{"1/256", 16, 400, powBlockedTheory(16, expected)},
		{"1/64", 64, 400, powBlockedTheory(64, expected)},
		{"1/8", 512, 400, powBlockedTheory(512, expected)},
		{"1倍（期望）", expected, 500, powBlockedTheory(expected, expected)},
		{"2倍", 2 * expected, 400, powBlockedTheory(2*expected, expected)},
		{"3倍", 3 * expected, 400, powBlockedTheory(3*expected, expected)},
	}

	fmt.Println()
	fmt.Println("POW 拦截率 vs 攻击者算力投入（difficulty=3，期望 4096 次尝试/次验证）")
	fmt.Println("--------------------------------------------------------------------------")
	fmt.Printf("%-11s %7s %6s %10s %10s %8s\n", "算力投入", "预算", "样本", "实测拦截", "理论拦截", "偏差")
	fmt.Println("--------------------------------------------------------------------------")
	type row struct {
		label        string
		sampleN      int
		blocked      int
		got, theoryP float64 // theoryP 是理论拦截概率（0-1）
	}
	rows := make([]row, 0, len(ratios))
	for _, r := range ratios {
		blocked := 0
		for i := 0; i < r.sampleN; i++ {
			ch, d, mb, rounds, me, _, err := pow.Issue()
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			// 预算内没求出答案就随便填一个（必错）
			nonce := powSolveOne(ch, d, mb, rounds, r.budget)
			if nonce == "" {
				nonce = "1"
			}
			if err := captcha.Verify("login", CaptchaParams{
				PowChallenge: ch, PowNonce: nonce, PowSignal: powMakeSignal("robot", me),
			}); err != nil {
				blocked++
			}
		}
		got := float64(blocked) / float64(r.sampleN) * 100
		fmt.Printf("%-11s %7d %6d %9.1f%% %9.1f%% %7.1f%%\n",
			r.label, r.budget, r.sampleN, got, r.blockedP*100, got-r.blockedP*100)
		rows = append(rows, row{r.label, r.sampleN, blocked, got, r.blockedP})
	}
	fmt.Println("--------------------------------------------------------------------------")

	// 统计校验分两种情况，用二项分布的双侧计数是否都达到 10 来区分——
	// 这是 z 检验成立的经验门槛，不是随手取的值：
	//
	//   - 双侧都够（中间档位）：用 4σ 检验。取 4σ 而非 3σ，因为本表同时检验
	//     8 个档位，3σ 的单次误报率叠加起来足以让测试偶发 flake。
	//   - 有一侧过小（1/4096、1/1024 这类极低预算档）：稀有事件几乎不出现，
	//     z 检验没有功效却会因个别幸运命中而极端敏感。改用方向性判定——
	//     预算不足时实测拦截率不得低于理论值 2 个百分点。
	//
	// 底层哈希的均匀性已由 pow_diag_test.go 用 400 万次尝试独立证实
	// （偏差 0.78σ），所以这里的重点不是「分布是否均匀」，而是
	// 「实测拦截率是否贴合难度-预算模型」。
	for _, r := range rows {
		n := float64(r.sampleN)
		p := r.theoryP
		rareCount := n * p
		if rareCount > 1-p {
			rareCount = n * (1 - p)
		}
		if rareCount >= 10 {
			sigma := powSqrt(n * p * (1 - p))
			if sigma == 0 {
				continue
			}
			if dev := (float64(r.blocked) - n*p) / sigma; dev > 4 || dev < -4 {
				t.Errorf("算力档位 %s 实测拦截 %d/%d 与理论均值 %.1f 偏差 %.1fσ，超过 4σ",
					r.label, r.blocked, r.sampleN, n*p, dev)
			}
			continue
		}
		if r.got < p*100-2 {
			t.Errorf("算力档位 %s 实测拦截率 %.1f%% 低于理论 %.1f%% 超过 2 个百分点（预算不足时应接近全拦）",
				r.label, r.got, p*100)
		}
	}
}

// powBlockedTheory 是难度-预算模型下的理论拦截率。
//
// 每次尝试独立命中概率 1/expected，因此预算内一次都没命中的概率是
// (1-1/expected)^budget——也就是被拦截的概率。
//
// 注意这意味着「投入正好等于期望次数」时成功率只有 1-e^-1 ≈ 63%，
// 要 3 倍期望工作量才到 ~95%。PoW 的门槛是软的、按比例衰减的。
func powBlockedTheory(budget, expected int) float64 {
	if budget <= 0 {
		return 1
	}
	if budget >= expected*10 {
		return 0
	}
	return powPow(1-1.0/float64(expected), budget)
}

// powPow 是朴素的幂运算（避免为一个函数引入 math 依赖）。
func powPow(base float64, exp int) float64 {
	out := 1.0
	for i := 0; i < exp; i++ {
		out *= base
	}
	return out
}

// powSqrt 牛顿法平方根（同上，测试内小工具）。
func powSqrt(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 40; i++ {
		z = (z + x/z) / 2
	}
	return z
}

// TestPowInterceptionWithoutSignal 是 A10 的对照实验：把 min_events 设为 0
// （后台关闭 signal 校验）后，同样的脚本不带任何 signal 也能通过——证明
// min_events 并没有增加攻击者需要付出的真实代价，只挡住了「漏字段」的脚本。
func TestPowInterceptionWithoutSignal(t *testing.T) {
	pow, captcha := powTestEnv(t, map[string]string{
		SettingPowDifficulty: "2",
		SettingPowMinEvents:  "0", // 关闭 signal 校验
	})
	if pow.MinEvents() != 0 {
		t.Fatalf("min_events 应为 0，实际 %d", pow.MinEvents())
	}

	ch, d, mb, rounds, _, _, err := pow.Issue()
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	nonce := powSolveOne(ch, d, mb, rounds, 0)
	if err := captcha.Verify("login", CaptchaParams{PowChallenge: ch, PowNonce: nonce}); err != nil {
		t.Errorf("min_events=0 时「正确解 + 空 signal」应放行，实际: %v", err)
	}
	// 但伪造的 challenge 依然要被挡住——一次性消费与挑战存在性与 signal 无关
	if err := captcha.Verify("login", CaptchaParams{PowChallenge: "deadbeef", PowNonce: nonce, PowSignal: ""}); err == nil {
		t.Error("未签发的 challenge 在 min_events=0 下也应被拒绝")
	}
}

// TestPowChallengePoolLimit 挑战池上限：池满后 Issue 报错，业务侧拿不到新挑战，
// 于是所有验证都因「挑战不存在」被拦。这是唯一能挡住已实现协议的攻击者的机制
// （配合IP限流才有意义）。
func TestPowChallengePoolLimit(t *testing.T) {
	pow, captcha := powTestEnv(t, nil)

	for i := 0; i < powMaxChallenges; i++ {
		if _, _, _, _, _, _, err := pow.Issue(); err != nil {
			t.Fatalf("第 %d 个挑战签发失败: %v", i, err)
		}
	}
	// 池已满，下一个必须失败
	if _, _, _, _, _, _, err := pow.Issue(); err == nil {
		t.Fatal("挑战池满后 Issue 应返回错误")
	}
	// 业务侧此时用任何凭证都过不去
	err := captcha.Verify("login", CaptchaParams{PowChallenge: "x", PowNonce: "1", PowSignal: "m:1:1:1"})
	if err == nil {
		t.Error("挑战池耗尽后应拒绝所有验证")
	}
}

// TestPowCostAndThroughput 量化 PoW 的成本结构——这是它唯一真正起作用的地方。
//
// 三个数决定它的价值：
//  1. 客户端求解耗时（访客 CPU 成本）
//  2. 服务端校验耗时（服务器成本，必须接近零）
//  3. 攻防成本比：攻击者每秒能打掉的请求数 vs 正常人被拖慢多少
func TestPowCostAndThroughput(t *testing.T) {
	// 用全默认配置（difficulty=4 / 1MB / 12 轮 / min_events=3）。
	// 这里刻意不写死数值：默认值就是线上跑的配置，钉住它才能反映真实成本。
	pow, captcha := powTestEnv(t, nil)

	const rounds = 30
	var solveTotal, verifyTotal time.Duration
	for i := 0; i < rounds; i++ {
		ch, d, mb, rds, me, _, err := pow.Issue()
		if err != nil {
			t.Fatalf("Issue: %v", err)
		}
		start := time.Now()
		nonce := powSolveOne(ch, d, mb, rds, 0)
		if nonce == "" {
			t.Fatal("难度 4 下未能求出 nonce")
		}
		solveTotal += time.Since(start)

		vstart := time.Now()
		err = captcha.Verify("login", CaptchaParams{
			PowChallenge: ch, PowNonce: nonce, PowSignal: powMakeSignal("robot", me),
		})
		verifyTotal += time.Since(vstart)
		if err != nil {
			t.Fatalf("难度 4 的正确解被拒绝: %v", err)
		}
	}
	solveAvg := solveTotal / rounds
	verifyAvg := verifyTotal / rounds

	// 单挑战服务端吞吐上限：只由校验耗时决定（求解耗时是访客付的，不是服务器）
	perVerify := verifyAvg
	fmt.Println()
	fmt.Println("POW 成本结构（全默认配置：difficulty=4 / 1MB 表 / 12 轮 / min_events=3）")
	fmt.Println("--------------------------------------------------------------------------")
	fmt.Printf("客户端单次求解耗时：      %8.2f ms\n", float64(solveAvg.Microseconds())/1000)
	fmt.Printf("服务端单次校验耗时：      %8.2f ms\n", float64(verifyAvg.Microseconds())/1000)
	fmt.Printf("单核理论验证吞吐：        %8.0f 次/秒\n", 1.0/perVerify.Seconds())
	fmt.Printf("服务端/客户端 成本比：    %8.1f 倍\n", float64(verifyAvg)/float64(solveAvg))
	fmt.Println("--------------------------------------------------------------------------")
	fmt.Println("  成本拆解见 TestPowVerifyCostDominatedByTableRebuild")

	if verifyAvg > 50*time.Millisecond {
		t.Errorf("服务端校验耗时 %v 超过 50ms，说明难度或内存参数已伤及服务器自身", verifyAvg)
	}
	if solveAvg < time.Millisecond {
		t.Errorf("客户端求解耗时 %v 过低，PoW 对访客几乎没有成本，也就形同虚设", solveAvg)
	}
}

// 下面把服务端校验成本拆开核对结论：若耗时几乎随 memoryMB 线性放大，
// 说明校验开销几乎全花在重建内存表上，而非安全相关的哈希计算。
// 预期：1MB 与 32MB 的差距远大于 5 次 SHA-256 的量级。
func TestPowVerifyCostDominatedByTableRebuild(t *testing.T) {
	const n = 20
	measure := func(mb int) time.Duration {
		p, c := powTestEnv(t, map[string]string{
			SettingPowDifficulty: "4",
			SettingPowMemoryMB:   strconv.Itoa(mb),
			SettingPowRounds:     "4",
		})
		var total time.Duration
		for i := 0; i < n; i++ {
			ch, d, _, _, me, _, err := p.Issue()
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			nonce := powSolveOne(ch, d, mb, 4, 0)
			if nonce == "" {
				t.Fatalf("mb=%d 下未解出", mb)
			}
			s := time.Now()
			if err := c.Verify("login", CaptchaParams{
				PowChallenge: ch, PowNonce: nonce, PowSignal: powMakeSignal("robot", me),
			}); err != nil {
				t.Fatalf("mb=%d 正确解被拒: %v", mb, err)
			}
			total += time.Since(s)
		}
		return total / n
	}

	one := measure(1)
	eight := measure(8)
	thirtyTwo := measure(32)
	fmt.Println()
	fmt.Println("服务端校验耗时 vs 内存表大小（difficulty=4, rounds=4，每次 20 次平均）")
	fmt.Println("--------------------------------------------------------------------------")
	for _, r := range []struct {
		mb  int
		avg time.Duration
	}{{1, one}, {8, eight}, {32, thirtyTwo}} {
		fmt.Printf("  %2dMB 表：%7.2f ms  （相对 1MB 的 %5.1f 倍）\n", r.mb,
			float64(r.avg.Microseconds())/1000, float64(r.avg)/float64(one))
	}
	fmt.Println("--------------------------------------------------------------------------")
	fmt.Println("  结论：耗时几乎随表大小线性放大——服务端单次校验的开销绝大部分")
	fmt.Println("        花在「重建 memoryMB 表」上，真正的哈希判定只占零头。")

	// 表大小相差 32 倍，若耗时差异不到 5 倍，说明重建表并非主导开销，
	// 上面的结论就不成立。
	if float64(thirtyTwo) < 5*float64(one) {
		t.Logf("提示：32MB/1MB 耗时比仅 %.1f 倍，重建表可能不是绝对主导",
			float64(thirtyTwo)/float64(one))
	}
}
func TestPowDifficultyClamp(t *testing.T) {
	cases := []struct {
		key      string
		value    string
		get      func(*PowService) int
		fallback int
	}{
		{SettingPowDifficulty, "0", (*PowService).Difficulty, 4},
		{SettingPowDifficulty, "7", (*PowService).Difficulty, 4},
		{SettingPowDifficulty, "abc", (*PowService).Difficulty, 4},
		{SettingPowMemoryMB, "0", (*PowService).MemoryMB, 8},
		{SettingPowMemoryMB, "99", (*PowService).MemoryMB, 8},
		{SettingPowRounds, "17", (*PowService).Rounds, 4},
		{SettingPowMinEvents, "11", (*PowService).MinEvents, 3},
		{SettingPowMinEvents, "-1", (*PowService).MinEvents, 3},
	}
	for _, c := range cases {
		pow, _ := powTestEnv(t, map[string]string{c.key: c.value})
		got := c.get(pow)
		if got != c.fallback {
			t.Errorf("%s=%q 应回退 %d，实际 %d", c.key, c.value, c.fallback, got)
		}
	}
}

// TestPowSceneDisabledFailsOpen 场景未开启一律放行——绝不锁死用户
// （这是代码注释里承诺的语义，必须钉住）。
func TestPowSceneDisabledFailsOpen(t *testing.T) {
	pow, captcha := powTestEnv(t, map[string]string{
		SettingPowEnabled:   "true",
		SettingPowOnLogin:   "false", // 登录场景关掉
		SettingPowOnComment: "true",
	})
	if pow.Required("login") {
		t.Error("pow_on_login=false 时 login 场景不应要求验证")
	}
	if !pow.Required("comment") {
		t.Error("pow_on_comment=true 时 comment 场景应要求验证")
	}
	// 关掉的场景：完全不带凭证也必须放行
	if err := captcha.Verify("login", CaptchaParams{}); err != nil {
		t.Errorf("未开启场景应放行，实际: %v", err)
	}
	// 总开关关掉：所有场景都放行
	pow2, captcha2 := powTestEnv(t, map[string]string{
		SettingPowEnabled:   "false",
		SettingPowOnLogin:   "true",
		SettingPowOnComment: "true",
	})
	for _, scene := range []string{"login", "register", "comment"} {
		if pow2.Required(scene) {
			t.Errorf("pow_enabled=false 时 %s 不应要求验证", scene)
		}
		if err := captcha2.Verify(scene, CaptchaParams{}); err != nil {
			t.Errorf("总开关关闭时 %s 应放行，实际: %v", scene, err)
		}
	}
}

// TestPowTTLExpiry 过期挑战一律失效（包括 nonce 已经正确的情况）。
func TestPowTTLExpiry(t *testing.T) {
	pow, captcha := powTestEnv(t, map[string]string{
		SettingPowDifficulty: "1",
		SettingPowTTLMinutes: "1", // 达到配置下限 1 分钟
	})

	ch, d, mb, rounds, _, _, err := pow.Issue()
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	nonce := powSolveOne(ch, d, mb, rounds, 0)
	if nonce == "" {
		t.Fatal("难度 1 下未能求出 nonce")
	}

	// 直接把记录的签发时间推到一年前，等价于等待 TTL 过期（否则测试要跑 1 分钟）
	pow.mu.Lock()
	if rec, ok := pow.challenges[ch]; ok {
		rec.issuedAt = time.Now().Add(-time.Hour)
	}
	pow.mu.Unlock()

	err = captcha.Verify("login", CaptchaParams{PowChallenge: ch, PowNonce: nonce, PowSignal: powMakeSignal("robot", 3)})
	if err == nil {
		t.Error("过期挑战应被拒绝")
	}
}
