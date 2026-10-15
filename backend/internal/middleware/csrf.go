package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// =====================================================================
// 基于来源校验的 CSRF 防护
//
// 为什么必须有：会话凭据搬进 Cookie 之后，「浏览器自动带上凭据」这件事
// 从特性变成了攻击面——第三方站点只要让用户的浏览器发一个表单 POST 到
// 我们的接口，Cookie 就会跟着过去，等于替用户执行了操作。
//
// 本中间件沿用与现有 IP 解析（middleware.ClientIP）同一套「只信声明之外的
// 事实」的思路：**不检查浏览器声称的来源头，而是检查它必须携带且脚本
// 无法伪造的 Origin / Sec-Fetch-Site**。
//
// 判定顺序：
//  1. 明确标记为跨站（Sec-Fetch-Site: cross-site）→ 拒绝；
//  2. 有 Origin 头 → 必须在白名单内（Origin 由浏览器写入，页面脚本改不了）；
//  3. 只有 Referer → 取其 origin 判定；
//  4. 两者都没有 → 放行。这类请求来自非浏览器客户端（curl、CLI、
//     移动端原生请求），它们不携带 Cookie，也就没有 CSRF 可言；
//     真正的浏览器发跨站表单请求时**一定**会带 Origin。
//
// 只作用于「会带 Cookie 的写方法」：GET/HEAD/OPTIONS 不做拦截，
// 因为按定义它们不应改变服务端状态（若某个 GET 有副作用，那是那个接口
// 的问题，不能靠 CSRF 中间件兜住）。
// =====================================================================

// CSRFGuard 拒绝来源可疑的跨站写请求。
//
// allowedOrigins 与 CORS 使用同一份白名单，避免「CORS 允许了但 CSRF 拒绝」
// 或反过来的错位。
func CSRFGuard(allowedOrigins []string) gin.HandlerFunc {
	allowAll := len(allowedOrigins) == 0

	normalized := make([]string, 0, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if v := normalizeOrigin(o); v != "" {
			normalized = append(normalized, v)
		}
	}

	allowed := func(origin string) bool {
		o := normalizeOrigin(origin)
		if o == "" {
			return false
		}
		if allowAll {
			return true
		}
		for _, a := range normalized {
			if strings.EqualFold(a, o) {
				return true
			}
		}
		return false
	}

	return func(c *gin.Context) {
		if isSafeMethod(c.Request.Method) {
			c.Next()
			return
		}

		// 建立会话的端点必须豁免。
		//
		// 登录 / 注册 / 刷新 / 找回密码的作用是「用新会话取代旧会话」，
		// 攻击者没有密码就无法利用它们，因此它们不是 CSRF 的攻击面。
		// 反过来，对它们做来源校验会制造一个**比漏洞更糟的故障**：
		// 用户在会话过期后打开登录页，浏览器仍带着那枚旧 Cookie，
		// 登录请求会被自己的防护拦成 403 —— 想重新登录都做不到。
		if isSessionBuilding(c.Request.URL.Path) {
			c.Next()
			return
		}

		// 非浏览器客户端没有 Cookie，也就不存在被第三方站点代发的风险。
		// 判据是「请求里真的带了会话 Cookie」而不是 UA 或其它可伪造的头。
		if !hasSessionCookie(c) {
			c.Next()
			return
		}

		// Sec-Fetch-Site 是浏览器强制写入的，页面脚本无法伪造。
		// 它给出的信息比 Origin 更强：连「同站但跨源」都能区分出来。
		//
		// cross-site 与 same-site 都要拒绝。只拦 cross-site 会留下一个缺口：
		// same-site 表示「同站但不同源」，即**任意子域**。攻击者控制
		// evil.example.com 时，它能以 same-site 身份向 blog.example.com
		// 发起写请求，而 Cookie 照样会被浏览器带上。
		switch strings.ToLower(strings.TrimSpace(c.GetHeader("Sec-Fetch-Site"))) {
		case "cross-site", "same-site":
			abortCSRF(c)
			return
		}

		if origin := c.GetHeader("Origin"); origin != "" {
			if !allowed(origin) {
				abortCSRF(c)
				return
			}
			c.Next()
			return
		}

		if referer := c.GetHeader("Referer"); referer != "" {
			if !allowed(originOf(referer)) {
				abortCSRF(c)
				return
			}
			c.Next()
			return
		}

		// 走到这里说明：请求带了会话 Cookie，却既没有 Origin 也没有
		// Referer、还没有 Sec-Fetch-Site。
		//
		// 现代浏览器在 fetch / XHR / 表单 POST 上一律会带 Origin 或
		// Sec-Fetch-Site（Fetch Metadata 自 2020 年起为 Chromium、Firefox、
		// Safari 全量支持），所以这种组合极不寻常，按可疑处理。
		//
		// 纯 API 客户端（curl、移动端原生请求）不携带会话 Cookie，
		// 在更上面的 hasSessionCookie 分支就已放行，不会被这里误伤。
		abortCSRF(c)
	}
}

// sessionBuildingPaths 是「建立会话」类端点，不受 CSRF 来源校验约束。
//
// 用完整路径精确匹配而非前缀匹配：前缀匹配会让
// /api/v1/auth/login-history 这类只读接口意外获得豁免。
var sessionBuildingPaths = map[string]bool{
	"/api/v1/auth/login":           true,
	"/api/v1/auth/register":        true,
	"/api/v1/auth/refresh":         true,
	"/api/v1/auth/password/forgot": true,
	"/api/v1/auth/password/reset":  true,
	"/api/v1/auth/email-code":      true,
}

func isSessionBuilding(path string) bool {
	return sessionBuildingPaths[path]
}

func abortCSRF(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"error": "请求来源不被信任，已拒绝该操作",
	})
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// hasSessionCookie 判断请求是否携带了会话 Cookie。
//
// 只按名字精确匹配而不解析值：这里要回答的是「浏览器有没有自动带上凭据」，
// 值本身是否有效由鉴权负责——把两件事混在一处会让 CSRF 中间件依赖 token 解析，
// 反而容易在中间件顺序调整时出错。
//
// 不用 strings.Contains：子串匹配会把 xinkstone_session、inkstone_session_v2
// 这类**服务端根本不读**的 Cookie 也算成会话，导致它们触发拦截。
// 名字取精确相等，并同时覆盖带前缀与不带前缀两种生产/开发命名。
func hasSessionCookie(c *gin.Context) bool {
	for _, ck := range c.Request.Cookies() {
		if ck.Name == sessionCookieNameSecure || ck.Name == sessionCookieNameInsecure {
			return true
		}
	}
	return false
}

// 会话 Cookie 的两种可能命名，与 service 包的常量保持一致。
//
// 这里各自定义一份而不从 service 导入：middleware 依赖 service 的
// token 解析已经足够，再为两个字符串常量增加一条包依赖会让
// 「中间件只做请求层判断」的边界变模糊。两者由测试锁定一致性。
const (
	sessionCookieNameSecure   = "__Host-inkstone_session"
	sessionCookieNameInsecure = "inkstone_session"
)

// originOf 从 Referer 里取出 scheme://host[:port]。
func originOf(referer string) string {
	referer = strings.TrimSpace(referer)
	if referer == "" {
		return ""
	}
	// 手动切分而不是 url.Parse：Referer 可能带用户信息、路径、查询串，
	// 这里只需要前两段，手写更好读也不会被 "javascript:" 之类协议骗到。
	i := strings.Index(referer, "://")
	if i < 0 {
		return ""
	}
	rest := referer[i+3:]
	if j := strings.IndexAny(rest, "/?#"); j >= 0 {
		rest = rest[:j]
	}
	if rest == "" {
		return ""
	}
	return referer[:i+3] + rest
}

// normalizeOrigin 统一成 scheme://host[:port] 形式并去掉结尾斜杠，
// 让白名单比较不受「多一个斜杠」「大小写不同」影响。
func normalizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		raw = originOf(raw)
	}
	return strings.TrimRight(raw, "/")
}
