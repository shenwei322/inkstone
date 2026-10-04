package service

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

// checkInterval is how often the background worker looks for links whose
// health has not been verified recently.
const (
	checkInterval  = 6 * time.Hour
	checkThreshold = 24 * time.Hour
	checkTimeout   = 15 * time.Second
)

type LinkService struct {
	links     *repository.LinkRepository
	siteHosts []string // 本站域名（用于反链检查）
}

func NewLinkService(links *repository.LinkRepository, siteURL string) *LinkService {
	return &LinkService{links: links, siteHosts: hostCandidates(siteURL)}
}

// hostCandidates 把站点地址解析为可用于反链匹配的域名候选（含根域名与 www 变体）。
func hostCandidates(siteURL string) []string {
	u, err := url.Parse(strings.TrimSpace(siteURL))
	if err != nil {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return nil
	}
	out := []string{host}
	if strings.HasPrefix(host, "www.") {
		out = append(out, strings.TrimPrefix(host, "www."))
	} else {
		out = append(out, "www."+host)
	}
	return out
}

type LinkInput struct {
	Name        string
	URL         string
	CheckURL    string
	IconURL     string
	Description string
	SortOrder   int
}

func normalizeLinkInput(input *LinkInput) error {
	input.Name = strings.TrimSpace(input.Name)
	input.URL = strings.TrimSpace(input.URL)
	input.CheckURL = strings.TrimSpace(input.CheckURL)
	if input.Name == "" {
		return NewValidationError("网站名称不能为空")
	}
	if input.URL == "" {
		return NewValidationError("网站链接不能为空")
	}
	if !strings.HasPrefix(input.URL, "http://") && !strings.HasPrefix(input.URL, "https://") {
		return NewValidationError("网站链接需以 http:// 或 https:// 开头")
	}
	if input.CheckURL != "" &&
		!strings.HasPrefix(input.CheckURL, "http://") && !strings.HasPrefix(input.CheckURL, "https://") {
		return NewValidationError("检测页面需以 http:// 或 https:// 开头")
	}
	// 检测会让服务端主动请求这些地址，内网/保留地址一律拒绝
	// （后台自动巡检与手动「检测」都会走到 probeURL）。
	if err := RejectPrivateHost(input.URL); err != nil {
		return err
	}
	if input.CheckURL != "" {
		if err := RejectPrivateHost(input.CheckURL); err != nil {
			return err
		}
	}
	return nil
}

func (s *LinkService) Create(input LinkInput) (*model.FriendLink, error) {
	if err := normalizeLinkInput(&input); err != nil {
		return nil, err
	}
	link := &model.FriendLink{
		Name:        input.Name,
		URL:         input.URL,
		CheckURL:    input.CheckURL,
		IconURL:     input.IconURL,
		Description: input.Description,
		SortOrder:   input.SortOrder,
		Available:   true,
	}
	if err := s.links.Create(link); err != nil {
		return nil, err
	}
	go func() {
		// 后台探测可能 panic（网络/解析异常），加 recover 避免拖垮整个进程
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[link] CheckOne(%d) panic recovered: %v", link.ID, r)
			}
		}()
		s.CheckOne(link.ID)
	}()
	return link, nil
}

// LinkUpdateInput 是友链更新的输入。字段全部为指针，用于区分
// 「本次没提交这个字段」与「提交了空值」——值类型绑定做不到这一点，
// 会把未提交的字段静默清空（检测页/图标/简介丢失）。
type LinkUpdateInput struct {
	Name        *string
	URL         *string
	CheckURL    *string
	IconURL     *string
	Description *string
	SortOrder   *int
}

func (s *LinkService) Update(id uint, input LinkUpdateInput) (*model.FriendLink, error) {
	link, err := s.links.FindByID(id)
	if err != nil {
		return nil, err
	}

	// 先按「已提交」的字段做归一化与校验，再只把这些字段交给更新，
	// 未提交的字段保持数据库里的原值。
	fields := map[string]any{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, NewValidationError("网站名称不能为空")
		}
		fields["name"] = name
		link.Name = name
	}
	if input.URL != nil {
		raw := strings.TrimSpace(*input.URL)
		if raw == "" {
			return nil, NewValidationError("网站链接不能为空")
		}
		if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
			return nil, NewValidationError("网站链接需以 http:// 或 https:// 开头")
		}
		// 检测会让服务端主动请求该地址，内网/保留地址一律拒绝
		if err := RejectPrivateHost(raw); err != nil {
			return nil, err
		}
		fields["url"] = raw
		link.URL = raw
	}
	if input.CheckURL != nil {
		raw := strings.TrimSpace(*input.CheckURL)
		if raw != "" {
			if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
				return nil, NewValidationError("检测页面需以 http:// 或 https:// 开头")
			}
			if err := RejectPrivateHost(raw); err != nil {
				return nil, err
			}
		}
		fields["check_url"] = raw
		link.CheckURL = raw
	}
	if input.IconURL != nil {
		fields["icon_url"] = strings.TrimSpace(*input.IconURL)
		link.IconURL = strings.TrimSpace(*input.IconURL)
	}
	if input.Description != nil {
		fields["description"] = strings.TrimSpace(*input.Description)
		link.Description = strings.TrimSpace(*input.Description)
	}
	if input.SortOrder != nil {
		fields["sort_order"] = *input.SortOrder
		link.SortOrder = *input.SortOrder
	}
	if len(fields) == 0 {
		return link, nil // 什么都没提交：原样返回，不产生一次空 UPDATE
	}
	if err := s.links.UpdateFields(id, fields); err != nil {
		return nil, err
	}
	return link, nil
}

// LinkValidation 是「添加友链」前的预检结果：站点是否可达、是否已加本站反链。
type LinkValidation struct {
	Reachable     bool   `json:"reachable"`
	StatusCode    int    `json:"status_code"`
	HasBacklink   bool   `json:"has_backlink"`
	BacklinkHost  string `json:"backlink_host"`  // 本次检测所用地址
	ExpectedHosts string `json:"expected_hosts"` // 期望在对方页面出现的本站域名
	Message       string `json:"message"`
}

// Validate 预检一个待添加的友链：探测可达性，并检查检测页面是否包含本站域名（反链）。
// 检查的是「检测页面」checkURL（留空则退回 url），因为友链通常挂在对方的友链页。
func (s *LinkService) Validate(rawURL, checkURL string) LinkValidation {
	target := strings.TrimSpace(checkURL)
	if target == "" {
		target = strings.TrimSpace(rawURL)
	}
	res := LinkValidation{BacklinkHost: target, ExpectedHosts: strings.Join(s.siteHosts, " / ")}
	if target == "" {
		res.Message = "请填写网站链接"
		return res
	}

	status, err := getStatus(target)
	if err != nil {
		res.Message = "无法访问该站点：" + err.Error()
		return res
	}
	res.StatusCode = status
	res.Reachable = status < 500
	if !res.Reachable {
		res.Message = "站点返回 " + http.StatusText(status) + "（" + strconv.Itoa(status) + "），无法访问"
		return res
	}

	// 可达再检查反链（需要正文，单独一次 GET）
	if len(s.siteHosts) > 0 {
		if body, ok := fetchBody(target); ok {
			lower := strings.ToLower(body)
			for _, h := range s.siteHosts {
				if strings.Contains(lower, strings.ToLower(h)) {
					res.HasBacklink = true
					break
				}
			}
		}
	}

	switch {
	case res.HasBacklink:
		res.Message = "站点可达，且页面包含本站反链，可以添加"
	case status >= 400:
		res.Message = "站点可达（返回 " + strconv.Itoa(status) + "），但未检测到本站反链"
	default:
		res.Message = "站点可达，但检测页面未包含本站域名（如已交换友链请确认检测页地址）"
	}
	return res
}

// getStatus 请求目标并返回 HTTP 状态码。
func getStatus(target string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	resp, err := probeClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 2048)
	return resp.StatusCode, nil
}

// fetchBody 读取目标页面正文（限制大小，避免下载大文件）。
func fetchBody(target string) (string, bool) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	resp, err := probeClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 1<<20) // 1MB 足够包含友链列表
	b, err := io.ReadAll(limited)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func (s *LinkService) Delete(id uint) error {
	return s.links.Delete(id)
}

func (s *LinkService) List() ([]model.FriendLink, error) {
	return s.links.List()
}

// CheckOne verifies a single link and persists the result.
func (s *LinkService) CheckOne(id uint) (bool, error) {
	link, err := s.links.FindByID(id)
	if err != nil {
		return false, err
	}
	target := link.CheckURL
	if target == "" {
		target = link.URL
	}
	available := probeURL(target)
	now := time.Now()
	link.Available = available
	link.LastCheckedAt = &now
	if err := s.links.Update(link); err != nil {
		return available, err
	}
	return available, nil
}

// CheckAll verifies every link (manual trigger from the admin console).
func (s *LinkService) CheckAll() ([]model.FriendLink, error) {
	links, err := s.links.List()
	if err != nil {
		return nil, err
	}
	for i := range links {
		target := links[i].CheckURL
		if target == "" {
			target = links[i].URL
		}
		available := probeURL(target)
		now := time.Now()
		links[i].Available = available
		links[i].LastCheckedAt = &now
		if err := s.links.Update(&links[i]); err != nil {
			return nil, err
		}
	}
	return links, nil
}

// StartAutoCheck runs a lightweight worker: every checkInterval it verifies
// links whose last check is older than checkThreshold (daily cadence).
func (s *LinkService) StartAutoCheck() {
	go func() {
		// Small delay so the server is fully up before probing.
		time.Sleep(20 * time.Second)
		for {
			// checkDueLinks 里既有 DB 访问也有网络探测，任何一处 panic
			// 都会让这个常驻循环静默退出——友链状态从此停止更新且无人知晓。
			// 与一次性探测 goroutine（上面 CheckOne 的 recover）不同，
			// 这里必须自己兜住，否则没有第二方会发现它死了。
			s.checkDueLinksSafely()
			time.Sleep(checkInterval)
		}
	}()
}

// checkDueLinksSafely 跑一轮巡检，panic 只终止这一轮，下一轮照常继续。
func (s *LinkService) checkDueLinksSafely() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[linkcheck] 巡检 panic 已恢复，将在下一轮继续: %v", r)
		}
	}()
	s.checkDueLinks()
}

func (s *LinkService) checkDueLinks() {
	links, err := s.links.List()
	if err != nil {
		log.Printf("[linkcheck] list failed: %v", err)
		return
	}
	now := time.Now()
	for i := range links {
		link := &links[i]
		if link.LastCheckedAt != nil && now.Sub(*link.LastCheckedAt) < checkThreshold {
			continue
		}
		target := link.CheckURL
		if target == "" {
			target = link.URL
		}
		available := probeURL(target)
		checkedAt := time.Now()
		link.Available = available
		link.LastCheckedAt = &checkedAt
		if err := s.links.Update(link); err != nil {
			log.Printf("[linkcheck] update %d failed: %v", link.ID, err)
		}
	}
}

// RejectPrivateHost 校验 URL 的主机既不是字面量内网地址，也不是解析到内网
// 地址的域名。用于友链申请/录入的第一道拦截，给用户可读的中文提示。
//
// 注意：DNS 结果随时可能变化，真正决定连接目标的是探测传输层的
// guardedDialContext，本函数只负责尽早失败与友好提示。
func RejectPrivateHost(rawURL string) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return NewValidationError("站点地址格式不正确")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return NewValidationError("站点地址不能指向内网或保留地址")
		}
		return nil
	}
	// 域名：解析后逐IP校验（解析失败不在这里报错，交给后续探测判定可达性）
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil
	}
	if len(ips) == 0 {
		return nil
	}
	for _, ip := range ips {
		if !isPublicIP(ip) {
			return NewValidationError("站点地址不能指向内网或保留地址")
		}
	}
	return nil
}

var probeClient = &http.Client{
	Timeout:   checkTimeout,
	Transport: newProbeTransport(),
}

// newProbeTransport 构造带 SSRF 防护的传输层。
//
// 背景（真实可利用）：友链地址可由**任何访客**通过公开的 POST /link-applications
// 提交，管理员点「检测」或后台自动巡检就会让服务端请求该地址。此前 probeClient
// 只有超时、没有任何目标校验，于是可以借它探测内网服务与云元数据端点
// （http://169.254.169.254/…），并按响应码差异做端口扫描。
//
// 防护放在 DialContext（而非只查 URL 主机名）：域名解析结果可能被 DNS rebinding
// 在「校验时」与「连接时」指向不同 IP，只有在校验实际拨号的地址后才可信。
func newProbeTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy:                 nil, // 不走环境代理，避免代理解析绕过校验
		DialContext:           guardedDialContext(dialer),
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		DisableKeepAlives:     true,
	}
}

// guardedDialContext 在建立连接前校验解析出的每个地址，命中内网/保留网段即拒绝。
func guardedDialContext(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("无效的目标地址：%s", addr)
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !isPublicIP(ip.IP) {
				return nil, fmt.Errorf("拒绝访问内网或保留地址：%s", host)
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("目标地址无法解析：%s", host)
		}
		// 逐个尝试已校验过的地址（全部通过校验才走到这里）
		var lastErr error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

// isPublicIP 判断地址是否为可安全访问的公网地址。
// 拒绝：回环、私网、链路本地（含云元数据 169.254.169.254）、CGNAT、
// 未指定地址、组播、IPv6 唯一本地地址等。
func isPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() ||
		ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// ---- 先处理"看起来像 IPv6、实际承载 IPv4"的地址 ----
	// Go 的 To4() 只识别 ::ffff:a.b.c.d（v4-mapped），对 ::a.b.c.d
	// （IPv4-compatible / v4-comparable）会返回 nil。后者会走到下面的 IPv6
	// 分支，而它不是 fc00::/7、也不是 IsLoopback/IsPrivate/IsLinkLocal，
	// 于是 ::127.0.0.1、::10.0.0.1、::169.254.169.254 全都被判成"公网"
	// ——这是一个真实的 SSRF 绕过（已由 ssrf_edge_test.go 复现）。
	//
	// 手工还原其内嵌 IPv4 后统一走下面的 IPv4 判定。
	if len(ip) == net.IPv6len && isZeros(ip[0:10]) && ip[10] == 0 && ip[11] == 0 {
		if v4 := net.IPv4(ip[12], ip[13], ip[14], ip[15]); !isPublicIP(v4) {
			return false
		}
	}
	// NAT64：64:ff9b::/96 内嵌 IPv4，系统会透明转换为 IPv4 出网
	if len(ip) == net.IPv6len && ip[0] == 0x00 && ip[1] == 0x64 &&
		ip[2] == 0xff && ip[3] == 0x9b {
		if v4 := net.IPv4(ip[12], ip[13], ip[14], ip[15]); !isPublicIP(v4) {
			return false
		}
	}

	if v4 := ip.To4(); v4 != nil {
		switch {
		// 100.64.0.0/10 CGNAT（运营商内网，可达宿主同网段服务）
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127:
			return false
		// 192.0.0.0/24 IETF 协议专用
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0:
			return false
		// 198.18.0.0/15 基准测试网段
		case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19):
			return false
		// 240.0.0.0/4 保留（含 255.255.255.255 广播）
		case v4[0] >= 240:
			return false
		}
		return true
	}
	// IPv6：唯一本地地址 fc00::/7 不在 IsPrivate 覆盖范围内
	if len(ip) == net.IPv6len && ip[0]&0xfe == 0xfc {
		return false
	}
	// IPv6 其他保留段：2001:db8::/32 文档、::/128 已由 IsUnspecified 覆盖
	if len(ip) == net.IPv6len && ip[0] == 0x20 && ip[1] == 0x01 &&
		ip[2] == 0x0d && ip[3] == 0xb8 {
		return false
	}
	return true
}

// isZeros 判断字节片是否全为 0（net.IP 的内部辅助，这里自行实现避免依赖标准库私有逻辑）。
func isZeros(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// probeURL returns whether the target responds. 2xx/3xx/4xx all count as
// "site exists" (many sites reject bots with 403); 5xx, timeouts and network
// errors are treated as unreachable.
// probeURL returns whether the target responds. HEAD is tried first (fast,
// low bandwidth); some servers reject HEAD, so it falls back to a ranged GET.
// 2xx/3xx/4xx all count as "site exists" (many sites reject bots with 403);
// 5xx, timeouts and network errors are treated as unreachable.
func probeURL(target string) bool {
	if target == "" {
		return true
	}

	if ok, err := probeOnce(http.MethodHead, target); err == nil {
		return ok
	}
	// HEAD 失败（部分服务器不支持）→ 用 GET 重试
	if ok, err := probeOnce(http.MethodGet, target); err == nil {
		return ok
	}
	return false
}

func probeOnce(method, target string) (bool, error) {
	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")

	resp, err := probeClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	// 只读少量 body 即可判定，避免下载整页
	_, _ = io.CopyN(io.Discard, resp.Body, 2048)
	return resp.StatusCode < 500, nil
}

// MaskURL desensitizes a URL for display: the host's middle characters are
// replaced, e.g. https://ex****.com. Returns "****" for unparsable input.
func MaskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "****"
	}
	parts := strings.Split(u.Host, ".")
	for i := range parts {
		label := parts[i]
		if i == len(parts)-1 && len(parts) > 1 {
			continue // keep TLD
		}
		if len(label) <= 2 {
			parts[i] = "**"
			continue
		}
		keep := 2
		if len(label) >= 5 {
			keep = 3
		}
		parts[i] = label[:keep] + strings.Repeat("*", 4)
	}
	return u.Scheme + "://" + strings.Join(parts, ".")
}
