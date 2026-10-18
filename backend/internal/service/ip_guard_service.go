package service

import (
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

// =====================================================================
// IP 访问记录 + 封禁（IPGuardService）
//
// 三段职责，全部与请求路径解耦：
//
//  1. 记录（Observe）：请求结束后把「IP/路径/状态码/UA」推进内存队列，
//     由后台 worker 按「分钟桶」合并落库。请求线程只做一次 channel send，
//     队列满就直接丢弃——访问记录是**可丢弃**的统计数据，
//     绝不能因为它把正常请求拖慢或拖挂（与审计日志相反）。
//
//  2. 封禁判定（Banned）：中间件每个请求都要问一次，所以必须 O(1)。
//     生效中的封禁常驻内存（map），由 reloadLoop 每 30 秒与本实例的
//     封禁/解封操作即时刷新；判定时顺带做惰性过期，过期项当场不再拦截。
//
//  3. 自动封禁（autoBan）：内存里按 IP 做窗口计数，窗口内总量超过阈值
//     就写一条 source=auto 的封禁。多副本部署时各实例各自计数、各自写库，
//     最终以数据库里的封禁行为准（reload 会把它带进内存）。
//
// 与 security_* 限流的区别见 settings_service.go 里那组 SettingIPGuard* 的注释。
// =====================================================================

const (
	// accessQueueSize 是访问记录队列容量。按「分钟桶」合并后实际入队量
	// 远小于 QPS；4096 足以吸收突发，同时限制内存上限。
	accessQueueSize = 4096

	// banReloadInterval 是封禁表内存缓存的刷新间隔。
	// 30 秒的延迟意味着：本实例的手动封禁即时生效（Ban/Unban 直接 ReloadBans），
	// 而**别的实例**或直接改库的封禁最多迟 30 秒生效。
	banReloadInterval = 30 * time.Second

	// counterSweepInterval 清理长期无访问的计数条目，防止 map 无限增长。
	counterSweepInterval = 10 * time.Minute

	// maxCounterIPs 是内存计数器允许的 IP 条目上限。超过后拒绝为新 IP
	// 建立计数（不封禁、不计数），避免被海量源地址打爆内存。
	maxCounterIPs = 20000
)

// AccessTask 是一条待落库的访问记录。
//
// 字段导出且独立成类型（而不是让 service 直接依赖 gin.Context）：
// 记录入口只接收「已经解析好的事实」，于是中间件之外（例如补录脚本、
// 单测）也能构造同样的事件，无需伪造一整个 HTTP 上下文。
type AccessTask struct {
	IP        string
	Method    string
	Path      string
	Status    int
	UserAgent string
	Referer   string
	UserID    uint
	Username  string
	At        time.Time
}

// IPGuardService 见文件头部说明。
type IPGuardService struct {
	repo     *repository.IPRepository
	settings *SettingsService

	queue    chan AccessTask
	dropped  int64
	droppedM sync.Mutex

	banMu    sync.RWMutex
	bans     map[string]model.IPBan
	banLoadT time.Time

	counterMu sync.Mutex
	counters  map[string][]time.Time

	// cfg 让每个请求不必都走 SettingsService.All()。
	// SettingsService 自带缓存，但一次判定要拿 6 个键，
	// 这里再收一层，避免每个请求多次拷贝整张设置表。
	cfgMu      sync.RWMutex
	cfg        ipGuardConfig
	cfgLoadedT time.Time
}

type ipGuardConfig struct {
	enabled     bool
	autoBan     bool
	window      time.Duration
	max         int64
	banDuration time.Duration // 0 = 永久
	days        int
	whitelist   []netip.Prefix
}

// NewIPGuardService 装配并启动后台 goroutine。
func NewIPGuardService(repo *repository.IPRepository, settings *SettingsService) *IPGuardService {
	s := &IPGuardService{
		repo:     repo,
		settings: settings,
		queue:    make(chan AccessTask, accessQueueSize),
		bans:     make(map[string]model.IPBan),
		counters: make(map[string][]time.Time),
	}
	s.ReloadBans()
	// 设置保存后立刻让阈值快照失效：否则运维改完阈值要等最多 10 秒
	// 才生效，调参时会以为「保存没成功」。
	settings.OnUpdate(s.InvalidateConfig)
	go s.writeLoop()
	go s.reloadLoop()
	go s.cleanupLoop()
	return s
}

// ---------- 配置 ----------

// config 返回缓存的配置快照（10 秒内复用）。
func (s *IPGuardService) config() ipGuardConfig {
	s.cfgMu.RLock()
	if !s.cfgLoadedT.IsZero() && time.Since(s.cfgLoadedT) < 10*time.Second {
		c := s.cfg
		s.cfgMu.RUnlock()
		return c
	}
	s.cfgMu.RUnlock()

	cfg := ipGuardConfig{
		enabled: s.settings.BoolValue(SettingIPGuardEnabled, true),
		autoBan: s.settings.BoolValue(SettingIPGuardAutoBan, false),
	}
	windowMin := s.settings.IntValue(SettingIPGuardWindow, 5)
	if windowMin < 1 {
		windowMin = 1
	}
	if windowMin > 1440 {
		windowMin = 1440
	}
	cfg.window = time.Duration(windowMin) * time.Minute

	cfg.max = int64(s.settings.IntValue(SettingIPGuardMax, 600))
	if cfg.max < 1 {
		cfg.max = 1
	}

	banMin := s.settings.IntValue(SettingIPGuardBanMin, 60)
	if banMin < 0 {
		banMin = 0
	}
	cfg.banDuration = time.Duration(banMin) * time.Minute // 0 = 永久

	cfg.days = s.settings.IntValue(SettingIPGuardDays, 7)
	if cfg.days < 1 {
		cfg.days = 1
	}
	if cfg.days > 365 {
		cfg.days = 365
	}

	if raw, err := s.settings.Get(SettingIPGuardWhitelist); err == nil {
		cfg.whitelist = ParseIPPrefixes(raw)
	}

	s.cfgMu.Lock()
	s.cfg = cfg
	s.cfgLoadedT = time.Now()
	s.cfgMu.Unlock()
	return cfg
}

// InvalidateConfig 让下一次 config() 重新读取设置（后台保存设置后调用）。
func (s *IPGuardService) InvalidateConfig() {
	s.cfgMu.Lock()
	s.cfgLoadedT = time.Time{}
	s.cfgMu.Unlock()
	// 白名单可能刚改，封禁缓存也要跟着重载
	s.ReloadBans()
}

// ParseIPPrefixes 解析白名单文本：支持逗号 / 分号 / 换行 / 空格分隔，
// 条目可以是裸 IP 或 CIDR。非法条目忽略——后台输入框里多打一个字符
// 不应该让整份白名单失效（那等于悄悄撤销了保护）。
func ParseIPPrefixes(raw string) []netip.Prefix {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
	out := make([]netip.Prefix, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if strings.Contains(f, "/") {
			if p, err := netip.ParsePrefix(f); err == nil {
				out = append(out, p.Masked())
			}
			continue
		}
		if addr, err := netip.ParseAddr(f); err == nil {
			addr = addr.Unmap()
			out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return out
}

// whitelisted 报告 ip 是否命中白名单。
func (c *ipGuardConfig) whitelisted(ip string) bool {
	if len(c.whitelist) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(strings.Trim(strings.TrimSpace(ip), "[]"))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, p := range c.whitelist {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// ---------- 记录 ----------

// Observe 把一次请求记入队列（非阻塞，永不报错）。
//
// 调用点是中间件在 c.Next() 之后，因此能拿到最终状态码。
func (s *IPGuardService) Observe(t AccessTask) {
	cfg := s.config()
	if !cfg.enabled {
		return
	}
	t.IP = strings.TrimSpace(t.IP)
	if t.IP == "" {
		return
	}
	// 本地回环默认不记录：同机反代探活、开发期本机访问会把它刷屏，
	// 而它永远不该被封。例外：运维把回环显式写进白名单时照常记录
	// （见 isLoopbackWhitelisted 的注释）。
	if isLoopback(t.IP) && !isLoopbackWhitelisted(cfg) {
		return
	}
	if t.At.IsZero() {
		t.At = time.Now()
	}

	select {
	case s.queue <- t:
	default:
		s.droppedM.Lock()
		s.dropped++
		n := s.dropped
		s.droppedM.Unlock()
		if n == 1 || n%500 == 0 {
			log.Printf("[ipguard] 访问记录队列已满，累计丢弃 %d 条（数据库写入可能过慢）", n)
		}
	}

	// 自动封禁判定：只有开启时才做窗口计数，否则纯记录、不额外花费。
	if cfg.autoBan && !cfg.whitelisted(t.IP) {
		s.maybeAutoBan(t.IP, cfg)
	}
}

// isLoopback 判断是否本地回环地址。
func isLoopback(ip string) bool {
	addr, err := netip.ParseAddr(strings.Trim(strings.TrimSpace(ip), "[]"))
	if err != nil {
		return false
	}
	return addr.Unmap().IsLoopback()
}

// isLoopbackWhitelisted 报告回环是否被显式列入白名单。
// 语义：默认不记录回环（开发期噪声），但一旦运维把 127.0.0.1 写进白名单，
// 说明他明确想观察本机流量，此时照常记录。
func isLoopbackWhitelisted(cfg ipGuardConfig) bool {
	loop, err := netip.ParseAddr("127.0.0.1")
	if err != nil {
		return false
	}
	for _, p := range cfg.whitelist {
		if p.Contains(loop) {
			return true
		}
	}
	return false
}

// maybeAutoBan 在窗口计数超过阈值时写一条自动封禁。
func (s *IPGuardService) maybeAutoBan(ip string, cfg ipGuardConfig) {
	count := s.bumpCounter(ip, cfg.window)
	if int64(count) <= cfg.max {
		return
	}
	// 已在封禁中（可能是别的实例写的）：不重复写库
	if _, ok := s.Banned(ip); ok {
		return
	}

	reason := "访问频率超限（" + itoaInt(int64(count)) + " 次 / " +
		itoaInt(int64(cfg.window.Minutes())) + " 分钟，阈值 " + itoaInt(cfg.max) + "）"
	ban := model.IPBan{
		IP:        ip,
		Reason:    reason,
		Source:    model.IPBanSourceAuto,
		Hits:      int64(count),
		CreatedBy: "system",
	}
	if cfg.banDuration > 0 {
		exp := time.Now().Add(cfg.banDuration)
		ban.ExpiresAt = &exp
	}
	if err := s.repo.UpsertBan(&ban); err != nil {
		log.Printf("[ipguard] 自动封禁写入失败 ip=%s: %v", ip, err)
		return
	}
	log.Printf("[ipguard] 自动封禁 %s：%s（时长 %s）", ip, reason, humanDuration(cfg.banDuration))
	s.ReloadBans()
	s.resetCounter(ip)
}

// bumpCounter 递增 ip 的窗口计数并返回窗口内当前值。
func (s *IPGuardService) bumpCounter(ip string, window time.Duration) int {
	now := time.Now()
	cutoff := now.Add(-window)

	s.counterMu.Lock()
	defer s.counterMu.Unlock()

	hits := s.counters[ip]
	if hits == nil && len(s.counters) >= maxCounterIPs {
		// 计数器已饱和：不为新 IP 建档（也就不可能触发自动封禁），
		// 但访问记录仍会落库，运维可以在列表里看到它们。
		return 0
	}
	// 裁掉窗口外的旧命中
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	s.counters[ip] = kept
	return len(kept)
}

func (s *IPGuardService) resetCounter(ip string) {
	s.counterMu.Lock()
	delete(s.counters, ip)
	s.counterMu.Unlock()
}

// ---------- 封禁判定 ----------

// Banned 报告该 IP 当前是否处于生效中的封禁，并返回封禁详情。
//
// 白名单在这里也生效：即使库里有该 IP 的封禁行（比如先被封、后来才加白），
// 也一律放行。这是运维的逃生通道——把自己误封之后，
// 只要把自己的 IP 写进白名单就能立刻恢复，而不必先进后台去解封
// （被封的人根本进不了后台）。
func (s *IPGuardService) Banned(ip string) (model.IPBan, bool) {
	if ip == "" {
		return model.IPBan{}, false
	}
	if cfg := s.config(); cfg.whitelisted(ip) {
		return model.IPBan{}, false
	}
	s.banMu.RLock()
	ban, ok := s.bans[ip]
	s.banMu.RUnlock()
	if !ok {
		return model.IPBan{}, false
	}
	// 惰性过期：不依赖 reload 的时点，也不会因为 reload 延迟而多封几十秒
	if !ban.Active(time.Now()) {
		return model.IPBan{}, false
	}
	return ban, true
}

// ReloadBans 从数据库重建封禁内存缓存。
func (s *IPGuardService) ReloadBans() {
	if s.repo == nil {
		return
	}
	rows, err := s.repo.ActiveBans(5000)
	if err != nil {
		log.Printf("[ipguard] 加载封禁列表失败: %v", err)
		return
	}
	next := make(map[string]model.IPBan, len(rows))
	for _, b := range rows {
		next[b.IP] = b
	}
	s.banMu.Lock()
	s.bans = next
	s.banLoadT = time.Now()
	s.banMu.Unlock()
}

// BanCount 返回当前生效的封禁条数（后台概览用）。
func (s *IPGuardService) BanCount() int {
	s.banMu.RLock()
	defer s.banMu.RUnlock()
	return len(s.bans)
}

// ---------- 手动封禁 / 解封 ----------

// BanInput 是手动封禁的参数。
type BanInput struct {
	IP string
	// Minutes 为封禁分钟数；0 = 永久封禁
	Minutes int
	Reason  string
	Note    string
	By      string
}

// Ban 手动封禁一个 IP。
func (s *IPGuardService) Ban(in BanInput) (model.IPBan, error) {
	ip := strings.Trim(strings.TrimSpace(in.IP), "[]")
	if ip == "" {
		return model.IPBan{}, NewValidationError("请填写要封禁的 IP")
	}
	if net.ParseIP(ip) == nil {
		return model.IPBan{}, NewValidationError("IP 格式不正确（仅支持单个 IPv4 / IPv6 地址）")
	}
	// 白名单里的地址不允许被封：白名单的语义就是「永不拦截」，
	// 允许封禁会让运维在最需要它的时刻（比如自己误封自己）失去逃生通道。
	cfg := s.config()
	if cfg.whitelisted(ip) {
		return model.IPBan{}, NewValidationError("该 IP 在白名单中，无法封禁（请先从白名单移除）")
	}
	if in.Minutes < 0 {
		in.Minutes = 0
	}
	ban := model.IPBan{
		IP:        ip,
		Reason:    strings.TrimSpace(in.Reason),
		Source:    model.IPBanSourceManual,
		CreatedBy: strings.TrimSpace(in.By),
		Note:      strings.TrimSpace(in.Note),
	}
	if ban.Reason == "" {
		ban.Reason = "管理员手动封禁"
	}
	if in.Minutes > 0 {
		exp := time.Now().Add(time.Duration(in.Minutes) * time.Minute)
		ban.ExpiresAt = &exp
	}
	// 带上窗口内的实际请求数，让后台列表能直接看出「封它是因为它很吵」
	if hits, err := s.repo.HitsSince(ip, time.Now().Add(-cfg.window)); err == nil {
		ban.Hits = hits
	}
	if err := s.repo.UpsertBan(&ban); err != nil {
		return model.IPBan{}, err
	}
	s.resetCounter(ip)
	s.ReloadBans()
	return ban, nil
}

// Unban 解除某个 IP 的封禁。返回 false 表示本来就没有封禁记录。
func (s *IPGuardService) Unban(ip string) (bool, error) {
	ip = strings.Trim(strings.TrimSpace(ip), "[]")
	if ip == "" {
		return false, NewValidationError("请填写要解封的 IP")
	}
	affected, err := s.repo.DeleteBan(ip)
	if err != nil {
		return false, err
	}
	s.resetCounter(ip)
	s.ReloadBans()
	return affected > 0, nil
}

// ---------- 查询 ----------

// ListAccess 转发明细分页查询。
func (s *IPGuardService) ListAccess(q repository.IPAccessQuery) ([]model.IPAccessLog, int64, error) {
	return s.repo.ListAccess(q)
}

// ListBans 转发封禁分页查询。
func (s *IPGuardService) ListBans(q repository.IPBanQuery) ([]model.IPBan, int64, error) {
	return s.repo.ListBans(q)
}

// TopIPs 返回窗口内的 IP 排行。
func (s *IPGuardService) TopIPs(since time.Time, limit int) ([]repository.IPAggregate, error) {
	return s.repo.TopIPs(since, limit)
}

// Paths 返回某 IP 访问过的路径分布。
func (s *IPGuardService) Paths(ip string, since time.Time, limit int) ([]repository.IPPathStat, error) {
	return s.repo.DistinctPaths(ip, since, limit)
}

// TotalRequests 返回窗口内全站请求总量与不同 IP 数。
func (s *IPGuardService) TotalRequests(since time.Time) (int64, int64) {
	total, err := s.repo.TotalRequestsSince(since)
	if err != nil {
		log.Printf("[ipguard] 统计窗口请求总量失败: %v", err)
	}
	ips, err := s.repo.DistinctIPCount(since)
	if err != nil {
		log.Printf("[ipguard] 统计窗口 IP 数失败: %v", err)
	}
	return total, ips
}

// Window 返回配置的统计窗口（后台页面展示「近 N 分钟」用）。
func (s *IPGuardService) Window() time.Duration { return s.config().window }

// DefaultBanMinutes 返回后台配置的默认封禁分钟数（0 = 永久）。
func (s *IPGuardService) DefaultBanMinutes() int {
	return int(s.config().banDuration.Minutes())
}

// RetentionDays 返回访问明细保留天数。
func (s *IPGuardService) RetentionDays() int { return s.config().days }

// DroppedCount 返回因队列满而丢弃的记录数（可观测性）。
func (s *IPGuardService) DroppedCount() int64 {
	s.droppedM.Lock()
	defer s.droppedM.Unlock()
	return s.dropped
}

// ---------- 后台 goroutine ----------

// writeLoop 消费访问记录队列。
//
// 单条写入单独 recover：`for range` 里一次 panic 会让整个 worker 退出，
// 此后所有访问记录静默消失，而页面上看不出任何异常。
func (s *IPGuardService) writeLoop() {
	for task := range s.queue {
		s.writeSafely(task)
	}
}

func (s *IPGuardService) writeSafely(t AccessTask) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ipguard] 写入访问记录时 panic 已恢复（该条丢失）: %v", r)
		}
	}()
	err := s.repo.RecordAccess(model.IPAccessLog{
		IP:        t.IP,
		Method:    t.Method,
		Path:      t.Path,
		Status:    t.Status,
		UserAgent: t.UserAgent,
		Referer:   t.Referer,
		UserID:    t.UserID,
		Username:  t.Username,
		BucketAt:  t.At,
		CreatedAt: t.At,
	})
	if err != nil {
		log.Printf("[ipguard] 访问记录写入失败 ip=%s path=%s: %v", t.IP, t.Path, err)
	}
}

func (s *IPGuardService) reloadLoop() {
	for {
		time.Sleep(banReloadInterval)
		s.ReloadBans()
	}
}

// cleanupLoop 周期清理过期的访问明细与陈旧的封禁记录，
// 同时收敛内存计数器（长期无访问的 IP 从 map 里删掉）。
func (s *IPGuardService) cleanupLoop() {
	// 启动后先等一轮再清，避免每次重启都立刻打一次 DELETE
	time.Sleep(5 * time.Minute)
	for {
		s.sweepCounters()
		if s.repo != nil {
			days := s.RetentionDays()
			if n, err := s.repo.CleanupAccess(time.Now().AddDate(0, 0, -days)); err != nil {
				log.Printf("[ipguard] 清理访问明细失败: %v", err)
			} else if n > 0 {
				log.Printf("[ipguard] 已清理 %d 条超过 %d 天的访问明细", n, days)
			}
			// 过期的封禁保留 30 天供审计，再之后才删
			if n, err := s.repo.CleanupExpiredBans(time.Now().AddDate(0, 0, -30)); err != nil {
				log.Printf("[ipguard] 清理过期封禁失败: %v", err)
			} else if n > 0 {
				log.Printf("[ipguard] 已清理 %d 条过期超过 30 天的封禁记录", n)
			}
		}
		time.Sleep(counterSweepInterval)
	}
}

// sweepCounters 删除窗口内没有任何命中的计数器条目。
func (s *IPGuardService) sweepCounters() {
	window := s.config().window
	cutoff := time.Now().Add(-window)

	s.counterMu.Lock()
	for ip, hits := range s.counters {
		if len(hits) == 0 || hits[len(hits)-1].Before(cutoff) {
			delete(s.counters, ip)
		}
	}
	s.counterMu.Unlock()
}

// itoaInt 是 strconv.FormatInt 的最小替代，避免为两处格式化引入依赖。
func itoaInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// humanDuration 把时长格式化成「2 小时 30 分钟 / 永久」这类可读文本。
func humanDuration(d time.Duration) string {
	if d <= 0 {
		return "永久"
	}
	if d%time.Hour == 0 && d >= time.Hour {
		return itoaInt(int64(d.Hours())) + " 小时"
	}
	if d >= time.Hour {
		return itoaInt(int64(d.Hours())) + " 小时 " + itoaInt(int64(d.Minutes())%60) + " 分钟"
	}
	return itoaInt(int64(d.Minutes())) + " 分钟"
}
