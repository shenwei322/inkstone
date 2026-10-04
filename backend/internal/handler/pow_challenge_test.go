package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shenwei/inkstone/backend/internal/middleware"
	"github.com/shenwei/inkstone/backend/internal/service"
	"github.com/shenwei/inkstone/backend/internal/service/powstoretest"
)

// =====================================================================
// POW 挑战签发接口的 HTTP 层测试
//
// 关注点不在 PoW 算法本身（见 internal/service/pow_intercept_test.go），
// 而在它暴露出的两个真实攻击面：
//   1. POST /api/v1/pow/challenge 的 IP 限流是否真的按 IP 生效；
//   2. middleware.ClientIP 取 IP 的方式是否可被客户端伪造。
//
// 装配同样不依赖数据库：NewSettingsService(nil) 走全默认路径。
// =====================================================================

// powRouter 按 main.go 相同的方式装配 POW 挑战路由（含 IP 限流）。
//
// 同时把可信代理配成「本机 + 私网」——与 config.defaultTrustedProxies 的
// 默认值一致。测试里用公网地址（192.0.2.x / 203.0.113.x 等文档段）作
// RemoteAddr 即代表「不可信直连」，用私网地址即代表「可信反代」。
// 把挑战存储换成内存 fake：这条测试关注的是路由层的限流与
// 客户端 IP 解析，不需要真库。pow_challenge_repo 的 SQL 语义
// （DELETE...RETURNING 的一次性消费）另由独立测试覆盖。
func powRouter(limit int) (*gin.Engine, *service.PowService) {
	gin.SetMode(gin.TestMode)
	// 与生产默认一致；每个用例都重设，避免用例间相互污染全局解析器
	middleware.SetTrustedProxies([]string{
		"127.0.0.0/8", "::1/128",
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7",
	})
	settings := service.NewSettingsService(nil)
	pow := service.NewPowService(settings, powstoretest.New())
	h := NewPowHandler(pow)

	r := gin.New()
	// gin 自身的可信代理解析同样收窄，避免 c.ClientIP() 成为另一个绕过口
	_ = r.SetTrustedProxies(middleware.TrustedProxies())
	limiter := middleware.NewSlidingLimiter()
	api := r.Group("/api/v1", middleware.IPRateLimit(middleware.RateLimitConfig{
		Limiter: limiter,
		LimitFn: func() int { return limit },
		Window:  time.Minute,
		Message: "pow-challenge",
	}))
	api.POST("/pow/challenge", h.Challenge)
	return r, pow
}

func postChallenge(r *gin.Engine, xff string) *httptest.ResponseRecorder {
	return postChallengeFrom(r, xff, "")
}

// postChallengeFrom 允许指定 TCP 对端地址（RemoteAddr），用于测试
// 「可信代理 / 不可信直连」两种场景下转发头的采信差异。
// remoteAddr 为空时用 httptest 默认值（192.0.2.1，TEST-NET-1，非私网）。
func postChallengeFrom(r *gin.Engine, xff, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pow/challenge", nil)
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestPowChallengeEndpoint 接口契约：字段齐全、类型正确、难度与配置一致。
func TestPowChallengeEndpoint(t *testing.T) {
	r, _ := powRouter(30)

	w := postChallenge(r, "203.0.113.10")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d, body=%s", w.Code, w.Body.String())
	}

	var body struct {
		Challenge  string `json:"challenge"`
		Difficulty int    `json:"difficulty"`
		MemoryMB   int    `json:"memory_mb"`
		Rounds     int    `json:"rounds"`
		MinEvents  int    `json:"min_events"`
		TTLSeconds int    `json:"ttl_seconds"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	// 前端 lib/pow.ts 强依赖这几个字段的类型
	if body.Challenge == "" || body.Difficulty <= 0 || body.MemoryMB <= 0 ||
		body.Rounds <= 0 || body.TTLSeconds <= 0 {
		t.Errorf("挑战响应字段不完整: %+v", body)
	}
	if len(body.Challenge) != 64 {
		t.Errorf("challenge 应为 64 位十六进制（32 字节），实际 %d 位", len(body.Challenge))
	}
	// 默认配置：difficulty=4、memory=1MB、rounds=12、ttl=10min=600s
	// （memory/rounds 的取值依据见 service/pow_calibrate_test.go：
	//   建表成本几乎全落在服务端，轮数才真正抬高攻击者的每次尝试成本）
	if body.Difficulty != 4 || body.MemoryMB != 1 || body.Rounds != 12 || body.TTLSeconds != 600 {
		t.Errorf("默认参数与设置项不符: %+v", body)
	}
	if body.MinEvents != 3 {
		t.Errorf("默认 min_events 应为 3，实际 %d", body.MinEvents)
	}

	// 每次签发都必须是全新的、不重复的挑战
	seen := map[string]bool{body.Challenge: true}
	for i := 0; i < 20; i++ {
		w := postChallenge(r, "203.0.113.10")
		if w.Code != http.StatusOK {
			t.Fatalf("第 %d 次签发失败: %d", i, w.Code)
		}
		var b struct {
			Challenge string `json:"challenge"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
			t.Fatalf("第 %d 次响应解析失败: %v", i, err)
		}
		if seen[b.Challenge] {
			t.Fatal("签发出重复的 challenge，随机源可能有问题")
		}
		seen[b.Challenge] = true
	}
}

// TestPowChallengeSceneBinding 签发接口应接受并忽略未知/缺省场景，
// 保证老前端（不带 body）与带场景的新前端都能拿到挑战。
//
// 场景绑定的「拒绝跨场景使用」语义在 service 层验证更合适——那里能
// 直接改写 settingDefaults 打开 POW 开关，见
// service/pow_scene_test.go。本包无法打开开关，pow 默认关闭时
// Verify 会按设计直接放行（fail-open），断言不出任何东西。
func TestPowChallengeSceneBinding(t *testing.T) {
	r, _ := powRouter(1000)

	cases := []struct {
		name string
		body string
	}{
		{"带合法场景", `{"scene":"login"}`},
		{"带未知场景", `{"scene":"whatever"}`},
		{"空场景", `{"scene":""}`},
		{"非法 JSON", `{not json`},
		{"空 body", ``},
	}
	for _, c := range cases {
		var rd *strings.Reader
		if c.body == "" {
			rd = strings.NewReader("")
		} else {
			rd = strings.NewReader(c.body)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/pow/challenge", rd)
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.1.2.3:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s: 应正常签发，实际 %d body=%s", c.name, w.Code, w.Body.String())
			continue
		}
		var body struct {
			Challenge string `json:"challenge"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Errorf("%s: 响应不是合法 JSON: %v", c.name, err)
			continue
		}
		if len(body.Challenge) != 64 {
			t.Errorf("%s: challenge 长度异常 %d", c.name, len(body.Challenge))
		}
	}
}

// TestPowChallengeSceneViaHeader 场景也可以走 X-Pow-Scene 请求头，
// 便于不方便带 body 的调用方。
func TestPowChallengeSceneViaHeader(t *testing.T) {
	r, _ := powRouter(30)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pow/challenge", nil)
	req.Header.Set("X-Pow-Scene", "register")
	req.RemoteAddr = "10.1.2.3:1234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("带 X-Pow-Scene 头签发失败: %d", w.Code)
	}
}

// TestPowChallengeRateLimit 同 IP 应在窗口内被限流（30 次/分钟）。
func TestPowChallengeRateLimit(t *testing.T) {
	r, _ := powRouter(30)

	ok, limited := 0, 0
	for i := 0; i < 60; i++ {
		w := postChallenge(r, "198.51.100.7")
		switch w.Code {
		case http.StatusOK:
			ok++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Fatalf("第 %d 次请求出现意外状态码 %d", i, w.Code)
		}
	}
	if ok != 30 {
		t.Errorf("应放行 30 次，实际 %d 次", ok)
	}
	if limited != 30 {
		t.Errorf("应限流 30 次，实际 %d 次", limited)
	}
}

// TestPowChallengeRateLimitBypassViaXFF 是限流绕过漏洞的回归测试。
//
// 历史问题：middleware.ClientIP 无条件优先读取客户端自带的 X-Forwarded-For，
// 而 main.go 从未调用 router.SetTrustedProxies()。于是任何能直连后端的客户端
// 只要每次请求换一个 XFF 值，就能把 IP 限流完全作废（当时实测 200/200 全放行）。
//
// 现在的语义：直连对端不可信时，转发头一概不看。这里的 RemoteAddr 是
// 192.0.2.1（TEST-NET-1，非私网），等价于「公网直连部署」场景，因此伪造
// XFF 必须无效——所有请求共享同一个限流计数。
func TestPowChallengeRateLimitBypassViaXFF(t *testing.T) {
	const limit = 30
	r, _ := powRouter(limit)

	// 对照组：不带 XFF
	ctrlOK := 0
	for i := 0; i < 100; i++ {
		if postChallengeFrom(r, "", "192.0.2.1:1111").Code == http.StatusOK {
			ctrlOK++
		}
	}

	// 攻击组：每个请求带一个不同的 X-Forwarded-For，但来自同一个公网对端
	bypassOK := 0
	for i := 0; i < 200; i++ {
		xff := fmt.Sprintf("203.0.113.%d", i%250)
		if postChallengeFrom(r, xff, "192.0.2.1:2222").Code == http.StatusOK {
			bypassOK++
		}
	}

	fmt.Println()
	fmt.Println("POW 挑战签发限流（30 次/分钟）与 X-Forwarded-For 信任")
	fmt.Println("--------------------------------------------------------------------------")
	fmt.Printf("对照组（无 XFF，公网直连）      ：放行 %3d / 100 次，限流生效=%v\n", ctrlOK, ctrlOK <= limit)
	fmt.Printf("攻击组（逐请求伪造 XFF，公网直连）：放行 %3d / 200 次，限流被绕过=%v\n", bypassOK, bypassOK > limit)
	fmt.Println("--------------------------------------------------------------------------")

	if ctrlOK > limit {
		t.Errorf("对照组放行 %d 次，超过限制 %d", ctrlOK, limit)
	}
	if bypassOK > limit {
		t.Errorf("伪造 XFF 仍能突破限流：放行 %d 次（上限 %d），漏洞未修复", bypassOK, limit)
	}
}

// TestPowChallengeTrustedProxyHonoursXFF 是上面那条的反向场景：
// 当直连对端确实是可信代理（同机 Nginx）时，转发头必须被采信，
// 否则反代部署下所有访客会被算成同一个人，限流会误伤整体。
func TestPowChallengeTrustedProxyHonoursXFF(t *testing.T) {
	const limit = 30
	r, _ := powRouter(limit)

	// RemoteAddr 落在私网 → 视为可信代理转发的请求；每个访客 IP 独立配额
	ok := 0
	for i := 0; i < 100; i++ {
		xff := fmt.Sprintf("198.51.100.%d", i%250)
		if postChallengeFrom(r, xff, "10.1.2.3:5555").Code == http.StatusOK {
			ok++
		}
	}
	if ok != 100 {
		t.Errorf("可信代理转发的 100 个不同访客应全部放行，实际 %d", ok)
	}

	// 同一个访客 IP 连打，仍应被限流
	sameOK := 0
	for i := 0; i < 100; i++ {
		if postChallengeFrom(r, "198.51.100.200", "10.1.2.3:5555").Code == http.StatusOK {
			sameOK++
		}
	}
	if sameOK > limit {
		t.Errorf("同一访客 IP 放行 %d 次，超过限制 %d", sameOK, limit)
	}
}

// TestPowChallengeXFFSpoofBehindProxy 可信代理场景下，攻击者仍不能靠
// 「在最左侧塞伪造值」来伪造身份——链式 XFF 必须从右往左取第一个不可信地址。
func TestPowChallengeXFFSpoofBehindProxy(t *testing.T) {
	const limit = 30
	r, _ := powRouter(limit)

	// Nginx 追加真实访客 IP；攻击者在最左侧塞了变化的值。
	// 正确实现应始终解析出 198.51.100.77（右侧第一个不可信地址），
	// 于是 100 次请求共享一个配额，被限流。
	ok := 0
	for i := 0; i < 100; i++ {
		xff := fmt.Sprintf("1.2.3.%d, 198.51.100.77", i%250)
		if postChallengeFrom(r, xff, "10.1.2.3:6666").Code == http.StatusOK {
			ok++
		}
	}
	if ok > limit {
		t.Errorf("链式 XFF 左侧伪造值被采信：放行 %d 次（上限 %d）", ok, limit)
	}
}

// TestClientIPResolution 记录 ClientIP 的完整取值规则（加固后的语义）。
//
// 两条核心规则：
//  1. 直连对端不在可信网段 → 转发头一概不看，用 TCP 对端地址；
//  2. 对端可信 → 从右往左扫描 XFF，跳过可信代理，取第一个不可信地址。
func TestClientIPResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	middleware.SetTrustedProxies([]string{"127.0.0.0/8", "10.0.0.0/8", "192.168.0.0/16"})

	r := gin.New()
	r.GET("/whoami", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ip": middleware.ClientIP(c), "remote": middleware.RemoteIP(c)})
	})

	cases := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{
			"公网直连：伪造 XFF 被忽略",
			"192.0.2.1:12345",
			map[string]string{"X-Forwarded-For": "1.2.3.4"},
			"192.0.2.1",
		},
		{
			"公网直连：伪造 X-Real-IP 被忽略",
			"192.0.2.1:12345",
			map[string]string{"X-Real-IP": "5.6.7.8"},
			"192.0.2.1",
		},
		{
			"可信反代：采信 XFF 单个值",
			"10.1.2.3:5555",
			map[string]string{"X-Forwarded-For": "9.9.9.9"},
			"9.9.9.9",
		},
		{
			"可信反代：XFF 带空白字符",
			"10.1.2.3:5555",
			map[string]string{"X-Forwarded-For": "  8.8.8.8  "},
			"8.8.8.8",
		},
		{
			"可信反代：链式 XFF 取右侧第一个不可信地址",
			"10.1.2.3:5555",
			map[string]string{"X-Forwarded-For": "1.2.3.4, 198.51.100.7"},
			"198.51.100.7",
		},
		{
			"可信反代：多层可信代理全部跳过",
			"10.1.2.3:5555",
			map[string]string{"X-Forwarded-For": "198.51.100.7, 10.0.0.9, 192.168.1.1"},
			"198.51.100.7",
		},
		{
			"可信反代：XFF 全为可信地址时落回对端",
			"10.1.2.3:5555",
			map[string]string{"X-Forwarded-For": "10.0.0.9, 192.168.1.1"},
			"10.1.2.3",
		},
		{
			"可信反代：XFF 非法段被跳过，回退 X-Real-IP",
			"10.1.2.3:5555",
			map[string]string{"X-Forwarded-For": "not-an-ip", "X-Real-IP": "7.7.7.7"},
			"7.7.7.7",
		},
		{
			"可信反代：无任何转发头时用对端",
			"10.1.2.3:5555",
			nil,
			"10.1.2.3",
		},
		{
			"IPv6 对端",
			"[2001:db8::1]:443",
			map[string]string{"X-Forwarded-For": "1.2.3.4"},
			"2001:db8::1",
		},
	}

	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		req.RemoteAddr = c.remoteAddr
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		var body struct {
			IP     string `json:"ip"`
			Remote string `json:"remote"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: 解析失败 %v", c.name, err)
		}
		if body.IP != c.want {
			t.Errorf("%s: ClientIP=%q, 期望 %q", c.name, body.IP, c.want)
		}
		// RemoteIP 必须始终是不含端口的纯对端地址
		if body.Remote == "" || body.Remote == c.remoteAddr {
			t.Errorf("%s: RemoteIP=%q 未正确剥离端口", c.name, body.Remote)
		}
	}
}
