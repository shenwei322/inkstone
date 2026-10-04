package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

// POW v2 人机验证（自研工作量证明，零外部依赖）。
//
// 协议（前端 lib/pow.ts + components/pow-captcha.tsx 实现，两端算法必须逐位一致）：
//  1. 前端  POST /api/v1/pow/challenge   领取随机 challenge 与本地资源参数快照
//  2. 前端  采集 min_events 个本地交互事件（鼠标/触摸/按键，带时间戳），
//     构建 xorshift128 内存表并多轮「查表-混合」迭代 SHA-256 找出前导零答案
//  3. 提交业务请求时带 {pow_challenge, pow_nonce, pow_signal}
//  4. 后端  同参数重建内存表重算校验 + signal 格式/时间窗校验，
//     通过后挑战立即作废（一次性消费）
//
// 防护能力（经 pow_intercept_test.go 实测，不要高估）：
//
//	真正有效的只有两条——
//	  - 一次性挑战：重放已消费的 challenge 一律拒绝（实测拦截率 100%）；
//	  - 工作量成本：难度 d 下每次尝试命中率 1/16^d，攻击者的成本随算力
//	    投入线性下降。投入正好等于期望尝试数时成功率约 63%，3 倍才算 95%。
//	    这是条软曲线，挡不住有算力的攻击者，只能抬高门槛。
//
//	不要指望的两条——
//	  - signal（本地交互信号）：只做格式与时间窗校验，而脚本刚请求过
//	    challenge，本来就大致知道签发时间，填当前时间即可构造出合法 signal。
//	    实测「脚本解题 + 合成 signal」的拦截率为 0%。它只是让真人多点一下鼠标
//	    的 UX 门槛，不是防机器人手段，不要在文档里当作防护能力宣传。
//	  - 内存表：表是「每挑战建一次」，不进入候选 nonce 的搜索循环，因此
//	    对攻击者的搜索成本几乎没有影响；而服务端每次校验都要重建一遍，
//	    成本几乎 100% 落在自己身上。默认已调成 1MB / 12 轮——
//	    小表省服务端，多轮（乘在每个候选上）才是抬高攻击者成本的那一项。
//	    GPU/ASIC 并行求解在现有构造下没有被真正削弱。
//
//	动态参数：memoryMB / rounds / minEvents 由后台随时调整，challenge
//	签发时快照，改参数立即对新挑战生效、不影响进行中的验证。
//
// ChallengeStore 是 POW 挑战的存储抽象。
//
// 生产用 repository.PowChallengeRepository（落 PostgreSQL），
// 测试用内存实现——本项目没有数据库测试基建（无 sqlmock/sqlite），
// 若 PowService 直接吃 *gorm.DB，每次验证都要连真库才能测。
//
// Take 返回 (*PowChallenge, error) 用 nil 表达「不存在」，而不是 bool：
// 与 GORM 的 Scan 零行行为对齐，仓储层与 service 层只需一套约定。
type ChallengeStore interface {
	Save(c *model.PowChallenge) error
	// Take 原子地取走并删除一条未过期的挑战，不存在返回 (nil, nil)。
	Take(challenge string, now time.Time) (*model.PowChallenge, error)
	// CountActive 返回仍未过期的挑战数（签发限额检查用）。
	CountActive(now time.Time) (int64, error)
	Delete(challenge string) error
	// DeleteExpired 清理已过期的挑战（按各自的 ttl_seconds 判定）。
	DeleteExpired(now time.Time) (int64, error)
}

// 设计约束：
//   - challenge 落库（pow_challenges 表）而非进程内 map：跨请求存活，
//     多副本部署下任意实例都要能消费别的实例签发的挑战；
//   - 挑战与签发它的场景绑定（见 IssueFor / Verify）：跨场景挪用一律拒绝；
//   - 开关语义与 lap/geetest 对称：场景未开启一律放行，绝不锁死用户。
type PowService struct {
	settings   *SettingsService
	challenges ChallengeStore

	mu     sync.Mutex
	lastGC time.Time
}

// 编译期断言：生产仓储必须实现接口。
var _ ChallengeStore = (*repository.PowChallengeRepository)(nil)

// POW 参数边界与池上限。
const (
	powMaxDifficulty    = 6
	powMaxMemoryMB      = 32
	powMaxRounds        = 16
	powMaxMinEvents     = 10
	powMaxChallenges    = 10000
	powChallengeTTLMin  = 1
	powChallengeTTLMax  = 60
	powChallengeGCEvery = 2 * time.Minute
)

func NewPowService(settings *SettingsService, challenges ChallengeStore) *PowService {
	return &PowService{
		settings:   settings,
		challenges: challenges,
	}
}

// powSceneKey 把业务场景映射到对应的设置项开关（与 lapSceneKey 对称）。
func powSceneKey(action string) string {
	switch action {
	case "register":
		return SettingPowOnRegister
	case "comment":
		return SettingPowOnComment
	default:
		return SettingPowOnLogin
	}
}

// ---------- 参数读取（均带 clamp，越界回退默认值） ----------

// Difficulty 答案前导零个数（1-6，默认 4）。
func (s *PowService) Difficulty() int {
	d := s.settings.IntValue(SettingPowDifficulty, 4)
	if d < 1 || d > powMaxDifficulty {
		return 4
	}
	return d
}

// MemoryMB 内存表大小（1-32MB，默认 8）。
func (s *PowService) MemoryMB() int {
	m := s.settings.IntValue(SettingPowMemoryMB, 8)
	if m < 1 || m > powMaxMemoryMB {
		return 8
	}
	return m
}

// Rounds 表查找-混合轮数（1-16，默认 4）。
func (s *PowService) Rounds() int {
	r := s.settings.IntValue(SettingPowRounds, 4)
	if r < 1 || r > powMaxRounds {
		return 4
	}
	return r
}

// MinEvents 需采集的本地交互事件数（0-10，默认 3；0 = 关闭 signal 校验）。
func (s *PowService) MinEvents() int {
	n := s.settings.IntValue(SettingPowMinEvents, 3)
	if n < 0 || n > powMaxMinEvents {
		return 3
	}
	return n
}

// TTL 挑战有效期（1-60 分钟，越界回退默认 10 分钟）。
func (s *PowService) TTL() time.Duration {
	m := s.settings.IntValue(SettingPowTTLMinutes, 10)
	if m < powChallengeTTLMin || m > powChallengeTTLMax {
		return 10 * time.Minute
	}
	return time.Duration(m) * time.Minute
}

// Required 判断某个场景是否开启了 POW 验证（前台据此决定是否渲染组件）。
func (s *PowService) Required(action string) bool {
	return s.settings.BoolValue(SettingPowEnabled, false) &&
		s.settings.BoolValue(powSceneKey(action), false)
}

// PublicConfig 下发到前台的人机验证配置（含 v2 本地资源参数，均无密钥）。
func (s *PowService) PublicConfig() map[string]any {
	return map[string]any{
		"enabled":     s.settings.BoolValue(SettingPowEnabled, false),
		"on_login":    s.settings.BoolValue(SettingPowOnLogin, false),
		"on_register": s.settings.BoolValue(SettingPowOnRegister, false),
		"on_comment":  s.settings.BoolValue(SettingPowOnComment, false),
		"difficulty":  s.Difficulty(),
		"ttl_seconds": int(s.TTL().Seconds()),
		"memory_mb":   s.MemoryMB(),
		"rounds":      s.Rounds(),
		"min_events":  s.MinEvents(),
	}
}

// Issue 签发一个新挑战（32 字节随机数十六进制），并把当前本地资源参数快照
// 进记录——验证时以快照为准，后台改参数不影响进行中的挑战。
// 挑战池满时返回错误（调用方转 503，前端提示稍后重试）。
//
// 不绑定场景（scene 为空），仅供不区分场景的调用方与测试使用；
// handler 请用 IssueFor，把挑战钉在具体业务场景上。
func (s *PowService) Issue() (challenge string, difficulty, memoryMB, rounds, minEvents, ttlSeconds int, err error) {
	return s.IssueFor("")
}

// IssueFor 签发一个绑定到指定业务场景的挑战。
//
// 绑定是必要的：签发接口是统一的 /pow/challenge，若挑战不记场景，
// powChallengeHexLen 是签发的 challenge 十六进制长度：32 字节随机数
// 经 hex 编码后恒为 64 字符。Verify 用它来提前拒绝畸形输入。
const powChallengeHexLen = 64

// isHexDigit 判断单个 ASCII 十六进制字符。
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// 攻击者可以用最便宜的场景批量领取挑战，再拿去打登录等更敏感的场景。
// 详见 powChallenge.scene 的注释。
func (s *PowService) IssueFor(scene string) (challenge string, difficulty, memoryMB, rounds, minEvents, ttlSeconds int, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", 0, 0, 0, 0, 0, err
	}
	challenge = hex.EncodeToString(buf)

	rec := &model.PowChallenge{
		Challenge:  challenge,
		Difficulty: s.Difficulty(),
		MemMB:      s.MemoryMB(),
		Rounds:     s.Rounds(),
		MinEvents:  s.MinEvents(),
		Scene:      normalizePowScene(scene),
		IssuedAt:   time.Now(),
		TTLSeconds: int(s.TTL().Seconds()),
	}
	if err := s.challenges.Save(rec); err != nil {
		return "", 0, 0, 0, 0, 0, err
	}

	// 限额按「未过期的挑战数」算，而不是总行数：
	// 已过期但还没被清掉的行不占额度，否则 TTL 内的正常高峰一旦
	// 攒够上限，后续用户会在后台清理前一直看到「服务繁忙」。
	active, err := s.challenges.CountActive(rec.IssuedAt)
	if err == nil && int(active) > powMaxChallenges {
		// 刚写进去的那条也计入，所以要 > 而不是 >=
		_ = s.challenges.Delete(challenge)
		return "", 0, 0, 0, 0, 0, NewValidationError("人机验证服务繁忙，请稍后重试")
	}
	s.maybeGC(rec.IssuedAt)

	// 服务端自己也要能用快照参数完成一次重算（与前台严格一致）
	return challenge, rec.Difficulty, rec.MemMB, rec.Rounds, rec.MinEvents, rec.TTLSeconds, nil
}

// normalizePowScene 把场景名归一化，避免大小写/空白差异造成误判。
func normalizePowScene(scene string) string {
	return strings.ToLower(strings.TrimSpace(scene))
}

// Verify 校验一次 POW v2 凭证 {challenge, nonce, signal}。
// 约定（与 lap/geetest 一致）：场景未开启一律放行；凭证缺失/格式错/过期/
// 答案不符/signal 不合规均拒绝（正常流程由前端在提交前完成计算与采集）。
func (s *PowService) Verify(action string, challenge, nonce, signal string) error {
	if !s.Required(action) {
		return nil
	}
	challenge = strings.TrimSpace(challenge)
	nonce = strings.TrimSpace(nonce)
	if challenge == "" || nonce == "" {
		return NewValidationError("请先完成人机验证")
	}
	// nonce 长度限制：防异常超长输入（正常数字递增远小于此）
	if len(nonce) > 128 {
		return NewValidationError("人机验证参数异常")
	}
	// challenge 必须是签发时的 64 位十六进制。此前只 TrimSpace、不限长，
	// 超长串会进 map 查找（string key 全量哈希，O(n) CPU）；顺带把格式异常的
	// 输入提前挡掉，而不是让它一路走到 takeChallenge 才判失效。
	if len(challenge) != powChallengeHexLen {
		return NewValidationError("人机验证参数异常")
	}
	for i := 0; i < len(challenge); i++ {
		if !isHexDigit(challenge[i]) {
			return NewValidationError("人机验证参数异常")
		}
	}

	// 一次性消费：按快照参数取出记录（TTL 过期同样视为失效）。
	// 取走与删atomicity由 SQL 的 DELETE...RETURNING 保证——两个请求
	// 带同一个挑战并发进来时只有一个能删到行。
	rec, err := s.challenges.Take(challenge, time.Now())
	if err != nil {
		return err
	}
	if rec == nil {
		return NewValidationError("人机验证已失效，请重新验证")
	}

	// 场景绑定校验：挑战只能在签发它的场景使用。
	// 注意取走即已删除——即使这里拒绝，挑战也不会被复用，
	// 跨场景尝试同样要付出一次完整的求解成本。
	if rec.Scene != "" && rec.Scene != normalizePowScene(action) {
		return NewValidationError("人机验证已失效，请重新验证")
	}

	ttl := time.Duration(rec.TTLSeconds) * time.Second

	// signal 校验（min_events=0 时跳过）：格式 + 时间窗 + 事件数
	if rec.MinEvents > 0 {
		if err := verifyPowSignal(signal, rec.MinEvents, rec.IssuedAt, ttl); err != nil {
			return err
		}
	}

	// 同参数重放本地资源计算（内存表 + 多轮查表混合），校验前导零。
	// 表通过池复用，避免每次校验都新分配一整张表。
	table := acquirePowTable(rec.MemMB, challenge)
	digest := digestWithTable(challenge, nonce, table, rec.Rounds)
	releasePowTable(table)
	if !leadingZeros(digest, rec.Difficulty) {
		return NewValidationError("人机验证未通过，请重试")
	}
	return nil
}

// maybeGC 定期清理过期的挑战行。
//
// 只在签发时顺带触发（上次 GC 超过 powChallengeGCEvery 才真删），
// 不起独立定时 goroutine：多副本下每个实例都跑一个定时器会重复清理，
// 而挂在签发路径上则天然只清理真正产生负载的实例。
// 失败只静默忽略——清不掉最多是多占几行，不影响正确性。
func (s *PowService) maybeGC(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lastGC.IsZero() && now.Sub(s.lastGC) < powChallengeGCEvery {
		return
	}
	s.lastGC = now
	_, _ = s.challenges.DeleteExpired(now)
}

// ---------- POW v2 核心算法（与前端 lib/pow.ts 严格一致，勿单侧改动） ----------

// powTableLen 内存表长度（u32 个数）：1MB = 262144 个 u32。
func powTableLen(memMB int) int { return memMB * 262144 }

// powXorshift128 是经典 xorshift128 伪随机发生器（32 位回绕运算）。
// 种子取 SHA-256(challenge) 前 16 字节（大端 4 个 u32）。
type powXorshift128 struct{ s [4]uint32 }

func newPowXorshift128(challenge string) *powXorshift128 {
	sum := sha256.Sum256([]byte(challenge))
	x := &powXorshift128{}
	for i := 0; i < 4; i++ {
		x.s[i] = binary.BigEndian.Uint32(sum[i*4 : i*4+4])
	}
	return x
}

func (x *powXorshift128) next() uint32 {
	t := x.s[0] ^ (x.s[0] << 11)
	x.s[0] = x.s[1]
	x.s[1] = x.s[2]
	x.s[2] = x.s[3]
	x.s[3] = x.s[3] ^ (x.s[3] >> 19) ^ t ^ (t >> 8)
	return x.s[3]
}

// powDigest 计算 POW v2 答案摘要（hex）：
//
//	h = SHA-256(challenge + ":" + nonce)
//	rounds 轮：idx = (BE32(h[0:4]) ^ (r*0x9E3779B9)) % TABLE_LEN
//	           h  = SHA-256(h || BE32(table[idx]))
//
// 注意：本函数每次都会重建 memoryMB 大小的表，调用方若已有表请用
// digestWithTable——服务端校验路径走的是后者（配合表缓冲池），
// 避免每次校验都新分配并重填一整张表。
func powDigest(challenge, nonce string, memMB, rounds int) string {
	return digestWithTable(challenge, nonce, powTable(memMB, challenge), rounds)
}

// digestWithTable 是摘要计算的核心：表由调用方提供（可复用、可池化）。
// powDigest 保证与前端 lib/pow.ts 逐位一致，本函数是它的表外置版本。
func digestWithTable(challenge, nonce string, table []uint32, rounds int) string {
	h := sha256.Sum256([]byte(challenge + ":" + nonce))
	if len(table) == 0 {
		return hex.EncodeToString(h[:])
	}
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

// powTable 用 xorshift128 填充内存表（两端必须同序同算法）。
func powTable(memMB int, challenge string) []uint32 {
	n := powTableLen(memMB)
	table := make([]uint32, n)
	fillPowTable(table, challenge)
	return table
}

// fillPowTable 往已有的切片里填表，不重新分配。
func fillPowTable(table []uint32, challenge string) {
	x := newPowXorshift128(challenge)
	for i := range table {
		table[i] = x.next()
	}
}

// powTablePool 复用建表用的底层数组。
//
// 服务端每次校验都要建一次表，而表大小可达 32MB。原先每次 make 一个新切片，
// 在高频校验下会把 GC 拖成主要开销。池化后同一块内存在请求间复用，
// 填表耗时不变，但分配与回收成本消失。
//
// 以 8MB（即 2M 个 u32）为分档上限缓存；更大的表不入池，避免长期占住大块内存。
const powTablePoolMaxLen = 8 * 262144

var powTablePool sync.Pool

// acquirePowTable 取出（或新建）一张填好的内存表。
//
// 参数是 memMB（MB 数）而不是 u32 个数——两者相差 262144 倍，
// 混淆一次的后果是一个几 TB 的分配请求。让接口只接受 MB，
// 内部的换算只此一处。
func acquirePowTable(memMB int, challenge string) []uint32 {
	if memMB <= 0 {
		return nil
	}
	// 防御：越界说明上游传错了单位/参数，夹住而不是让进程 OOM
	if memMB > powMaxMemoryMB {
		panic(fmt.Sprintf("pow: 内存表大小越界 memMB=%d > %d", memMB, powMaxMemoryMB))
	}
	n := powTableLen(memMB)
	if n > powTablePoolMaxLen {
		table := make([]uint32, n)
		fillPowTable(table, challenge)
		return table
	}
	if v := powTablePool.Get(); v != nil {
		if buf := v.([]uint32); cap(buf) >= n {
			table := buf[:n]
			fillPowTable(table, challenge)
			return table
		}
	}
	table := make([]uint32, n)
	fillPowTable(table, challenge)
	return table
}

func releasePowTable(table []uint32) {
	if len(table) == 0 || len(table) > powTablePoolMaxLen {
		return
	}
	// 截断后放回：池里只保留容量，长度在 acquire 时重新设定
	powTablePool.Put(table[:0]) //nolint:staticcheck // 复用大数组是有意为之
}

// leadingZeros 摘要前 difficulty 个十六进制位是否均为 '0'。
func leadingZeros(digest string, difficulty int) bool {
	if difficulty <= 0 {
		return true
	}
	for i := 0; i < difficulty && i < len(digest); i++ {
		if digest[i] != '0' {
			return false
		}
	}
	return len(digest) >= difficulty
}

// ---------- signal（本地交互事件流）校验 ----------

// verifyPowSignal 校验本地交互信号：
//   - 至少 min_events 个事件，格式 `{m|k|t}:{unix_ms}:{x}:{y}` 逗号连接；
//   - 每个事件时间戳必须落在 [challenge 签发时刻, 当前] 窗口内（服务端时钟），
//     且事件流跨度不超过挑战 TTL——纯 HTTP 脚本不带信息无从构造。
func verifyPowSignal(signal string, minEvents int, issuedAt time.Time, ttl time.Duration) error {
	signal = strings.TrimSpace(signal)
	if signal == "" {
		return NewValidationError("请先完成人机验证")
	}
	now := time.Now()
	events := strings.Split(signal, ",")
	if len(events) < minEvents {
		return NewValidationError("人机验证未完成，请晃动鼠标或触摸屏幕")
	}
	// 事件数上限：防伪造超长 signal 浪费解析
	if len(events) > powMaxMinEvents*4 {
		return NewValidationError("人机验证参数异常")
	}
	first, last := int64(0), int64(0)
	for i, ev := range events {
		parts := strings.Split(ev, ":")
		if len(parts) != 4 {
			return NewValidationError("人机验证参数异常")
		}
		switch parts[0] {
		case "m", "k", "t":
		default:
			return NewValidationError("人机验证参数异常")
		}
		ms, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return NewValidationError("人机验证参数异常")
		}
		x, err := strconv.Atoi(parts[2])
		if err != nil || x < -32768 || x > 32767 {
			return NewValidationError("人机验证参数异常")
		}
		y, err := strconv.Atoi(parts[3])
		if err != nil || y < -32768 || y > 32767 {
			return NewValidationError("人机验证参数异常")
		}
		evTime := time.UnixMilli(ms)
		if evTime.Before(issuedAt.Add(-2*time.Second)) || evTime.After(now.Add(2*time.Second)) {
			return NewValidationError("人机验证已失效，请重新验证")
		}
		if i == 0 {
			first = ms
		}
		last = ms
	}
	if last-first > ttl.Milliseconds() {
		return NewValidationError("人机验证已失效，请重新验证")
	}
	return nil
}
