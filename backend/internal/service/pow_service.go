package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"time"
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
// 相对 v1（纯 SHA-256 循环）的升级——消耗的都是用户本地资源、且难以在
// 服务端机房批量复刻：
//   - 内存带宽：每次验证实打实构建并随机访问 memoryMB 大小的表，
//     GPU/ASIC 集群的并行优势被内存带宽瓶颈大幅削弱；
//   - 本地交互信号：真人设备自然产生鼠标/触摸/按键流，纯 HTTP 脚本
//     （curl/requests）必须额外复刻整套信号构造逻辑才能过格式校验；
//   - 动态参数：memoryMB / rounds / minEvents 由后台随时调整，challenge
//     签发时快照，改参数立即对新挑战生效、不影响进行中的验证。
//
// 设计约束：
//   - challenge 存内存（单实例部署的本项目 backend 只有一个实例），重启全部
//     失效——后果只是用户重新验证一次，不影响正确性；多副本部署需换共享存储；
//   - 开关语义与 lap/geetest 对称：场景未开启一律放行，绝不锁死用户。
type PowService struct {
	settings *SettingsService

	mu         sync.Mutex
	challenges map[string]*powChallenge // challenge -> 签发记录（含参数快照）
	lastGC     time.Time
}

// powChallenge 是已签发挑战的内存记录（参数在签发时刻快照，验证时以快照为准，
// 后台调整 memoryMB/rounds/minEvents/难度/TTL 只影响之后签发的新挑战）。
type powChallenge struct {
	difficulty int
	memMB      int
	rounds     int
	minEvents  int
	ttl        time.Duration
	issuedAt   time.Time
}

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

func NewPowService(settings *SettingsService) *PowService {
	return &PowService{
		settings:   settings,
		challenges: make(map[string]*powChallenge),
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
func (s *PowService) Issue() (challenge string, difficulty, memoryMB, rounds, minEvents, ttlSeconds int, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", 0, 0, 0, 0, 0, err
	}
	challenge = hex.EncodeToString(buf)

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.challenges) >= powMaxChallenges {
		return "", 0, 0, 0, 0, 0, NewValidationError("人机验证服务繁忙，请稍后重试")
	}
	rec := &powChallenge{
		difficulty: s.Difficulty(),
		memMB:      s.MemoryMB(),
		rounds:     s.Rounds(),
		minEvents:  s.MinEvents(),
		ttl:        s.TTL(),
		issuedAt:   now,
	}
	s.challenges[challenge] = rec
	s.gcLocked(now)

	// 服务端自己也要能用快照参数完成一次重算（与前台严格一致）
	return challenge, rec.difficulty, rec.memMB, rec.rounds, rec.minEvents, int(rec.ttl.Seconds()), nil
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

	// 一次性消费：按快照参数取出记录（TTL 过期同样视为失效）
	rec, ok := s.takeChallenge(challenge)
	if !ok {
		return NewValidationError("人机验证已失效，请重新验证")
	}

	// signal 校验（min_events=0 时跳过）：格式 + 时间窗 + 事件数
	if rec.minEvents > 0 {
		if err := verifyPowSignal(signal, rec.minEvents, rec.issuedAt, rec.ttl); err != nil {
			return err
		}
	}

	// 同参数重放本地资源计算（内存表 + 多轮查表混合），校验前导零
	digest := powDigest(challenge, nonce, rec.memMB, rec.rounds)
	if !leadingZeros(digest, rec.difficulty) {
		return NewValidationError("人机验证未通过，请重试")
	}
	return nil
}

// takeChallenge 取出挑战记录：存在且未过期返回记录（同时删除，防重放）；
// 不存在/已过期返回 false。
func (s *PowService) takeChallenge(challenge string) (*powChallenge, bool) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.challenges[challenge]
	if !ok {
		return nil, false
	}
	delete(s.challenges, challenge)
	if now.Sub(rec.issuedAt) > rec.ttl {
		return nil, false
	}
	return rec, true
}

// gcLocked 清理过期挑战（调用方需持有 s.mu；TTL 按各记录自己的快照）。
func (s *PowService) gcLocked(now time.Time) {
	if !s.lastGC.IsZero() && now.Sub(s.lastGC) < powChallengeGCEvery {
		return
	}
	for k, rec := range s.challenges {
		if now.Sub(rec.issuedAt) > rec.ttl {
			delete(s.challenges, k)
		}
	}
	s.lastGC = now
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
// 每次验证都完整重建 memoryMB 大小的表并随机访问——本地内存带宽成本在此。
func powDigest(challenge, nonce string, memMB, rounds int) string {
	table := powTable(memMB, challenge)
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

// powTable 用 xorshift128 填充内存表（两端必须同序同算法）。
func powTable(memMB int, challenge string) []uint32 {
	n := powTableLen(memMB)
	table := make([]uint32, n)
	x := newPowXorshift128(challenge)
	for i := 0; i < n; i++ {
		table[i] = x.next()
	}
	return table
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
