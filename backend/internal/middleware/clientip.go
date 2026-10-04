package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// =====================================================================
// 访客 IP 解析
//
// 背景（这是一个真实可利用的漏洞，不是理论问题）：
//   原实现无条件优先读取客户端自带的 X-Forwarded-For，且 main.go 从未调用
//   router.SetTrustedProxies()。于是任何能直连后端的客户端，只要每个请求换
//   一个 XFF 值，就能把全部 IP 限流作废——实测 200/200 请求全部放行。
//
// 现方案：
//   1. 只有「直连对端」落在可信代理网段内，才采信转发头；
//   2. 采信时按从右往左的顺序扫描 XFF，跳过可信代理，取第一个不可信地址
//      ——最左侧的值是客户端可以随意伪造的，绝不能直接用；
//   3. 都不满足时回退到 TCP 对端地址。
// =====================================================================

// ipResolver 承载可信代理网段与解析逻辑。零值可用（等价于不信任任何代理）。
type ipResolver struct {
	mu       sync.RWMutex
	networks []*net.IPNet
}

var clientIPResolver = &ipResolver{}

// SetTrustedProxies 设置可信代理网段（CIDR 字符串）。解析失败的条目会被忽略；
// 传空切片表示不信任任何代理。
func SetTrustedProxies(cidrs []string) {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// 容忍裸 IP（自动补全为 /32 或 /128）
		if !strings.Contains(raw, "/") {
			if ip := net.ParseIP(raw); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				raw = raw + "/" + itoa(bits)
			}
		}
		if _, n, err := net.ParseCIDR(raw); err == nil {
			nets = append(nets, n)
		}
	}
	clientIPResolver.mu.Lock()
	clientIPResolver.networks = nets
	clientIPResolver.mu.Unlock()
}

// TrustedProxies 返回当前生效的可信代理网段（CIDR 字符串副本），
// 供 main.go 转交给 gin 的 SetTrustedProxies，保证两处判定口径一致。
func TrustedProxies() []string {
	clientIPResolver.mu.RLock()
	defer clientIPResolver.mu.RUnlock()
	out := make([]string, 0, len(clientIPResolver.networks))
	for _, n := range clientIPResolver.networks {
		out = append(out, n.String())
	}
	return out
}

func (r *ipResolver) isTrusted(ip net.IP) bool {
	if ip == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, n := range r.networks {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// hostOnly 从 "ip:port" 或纯 IP 中取出 IP 部分。
func hostOnly(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	// 没有端口（或 IPv6 无方括号）：直接当 IP 用
	return strings.Trim(addr, "[]")
}

// resolve 是解析核心：返回调用方 IP 字符串，判定失败时返回空串。
func (r *ipResolver) resolve(remoteAddr string, xff string, xRealIP string) string {
	peer := net.ParseIP(hostOnly(remoteAddr))

	// 对端不可信（含解析失败）：一律用 TCP 对端地址，转发头完全不看。
	if !r.isTrusted(peer) {
		if peer != nil {
			return peer.String()
		}
		return hostOnly(remoteAddr)
	}

	// 对端可信：从右往左扫描 XFF，跳过可信代理，取第一个不可信地址。
	// 这是 RFC 7239 / 主流反代的标准做法——最左侧的值由客户端提供，不可信。
	if xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			cand := net.ParseIP(strings.Trim(strings.TrimSpace(parts[i]), "[]"))
			if cand == nil {
				// 该段不是合法 IP：继续往左找，找不到就落回对端
				continue
			}
			if !r.isTrusted(cand) {
				return cand.String()
			}
		}
		// 全链都是可信代理：落到对端地址
	}

	if real := net.ParseIP(strings.Trim(strings.TrimSpace(xRealIP), "[]")); real != nil && !r.isTrusted(real) {
		return real.String()
	}

	if peer != nil {
		return peer.String()
	}
	return hostOnly(remoteAddr)
}

// ClientIP 返回本次请求的访客 IP。
//
// 注意：这里刻意不复用 gin 的 c.ClientIP()。gin 的取值同样依赖
// engine.trustedProxies，而且它的默认值是「信任所有代理」，容易在
// 忘记配置时静默失守。本函数自带判定，行为只由 SetTrustedProxies 决定。
func ClientIP(c *gin.Context) string {
	return clientIPResolver.resolve(
		c.Request.RemoteAddr,
		c.GetHeader("X-Forwarded-For"),
		c.GetHeader("X-Real-IP"),
	)
}

// RemoteIP 返回不计任何转发头、纯 TCP 层看到的对端地址。
// 用于「需要真实来源、不接受任何代理声明」的场景（如审计日志）。
func RemoteIP(c *gin.Context) string {
	return hostOnly(c.Request.RemoteAddr)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// 确保 http 包被引用（SetTrustedProxies 供 main.go 与 gin 对接时使用）。
var _ = http.StatusOK
