package config

import (
	"strings"
	"testing"
)

// TestNormalizeImageMirror 锁定镜像包加速前缀的归一化。
//
// 这条配置直接决定能不能拉到 265 MB 的镜像包（国内直连 GitHub 会在中途
// 被掐断），所以「怎么填等于关闭」必须明确且稳定。
func TestNormalizeImageMirror(t *testing.T) {
	cases := map[string]string{
		"https://gh-proxy.com":       "https://gh-proxy.com",
		"https://gh-proxy.com/":      "https://gh-proxy.com", // 去尾斜杠，避免双斜杠
		"  https://gh-proxy.com//  ": "https://gh-proxy.com",
		// 关闭加速的各种写法都要认：加速服务本身也会挂，
		// 运维需要不改代码就能切回直连
		"":       "",
		"none":   "",
		"off":    "",
		"direct": "",
		"0":      "",
		"false":  "",
		"NONE":   "", // 大小写不敏感
		"None":   "",
		// 自定义镜像站照常放行
		"https://mirror.example.com/gh": "https://mirror.example.com/gh",
	}
	for raw, want := range cases {
		if got := normalizeImageMirror(raw); got != want {
			t.Errorf("normalizeImageMirror(%q) = %q，期望 %q", raw, got, want)
		}
	}
}

// TestDefaultImageMirrorConfigured 确保默认值不是空的。
//
// 默认直连等于「国内开箱即用」这个目标直接落空——用户升级后会再次遇到
// 本次要修的那个 58.5 MB 超时。
func TestDefaultImageMirrorConfigured(t *testing.T) {
	if defaultImageMirror == "" {
		t.Fatal("镜像包加速前缀应有默认值，否则国内用户开箱即不可用")
	}
	if got := normalizeImageMirror(defaultImageMirror); got == "" {
		t.Fatal("默认值不应被 normalizeImageMirror 判为关闭")
	}
}

// TestSessionCookieNameByEnv 锁定「生产用 __Host- 前缀、开发降级」这条规则。
//
// 两边都错不起：
//   - 生产少了前缀 → 任意子域都能用 Set-Cookie 覆盖会话（Cookie 投毒）；
//   - 开发带了前缀 → 浏览器在 http://localhost 上静默丢弃整枚 Cookie，
//     表现为「本地登录成功但一刷新就掉线」，极难排查。
func TestSessionCookieNameByEnv(t *testing.T) {
	if got := SessionCookieName(false); got != SessionCookieNameSecure {
		t.Errorf("生产应使用带 __Host- 前缀的名字，得到 %q", got)
	}
	if got := SessionCookieName(true); got != SessionCookieNameInsecure {
		t.Errorf("开发应降级为不带前缀的名字，得到 %q", got)
	}
	if !strings.HasPrefix(SessionCookieNameSecure, "__Host-") {
		t.Error("生产 Cookie 名必须以 __Host- 开头，否则前缀保护不成立")
	}
	if strings.HasPrefix(SessionCookieNameInsecure, "__Host-") {
		t.Error("开发 Cookie 名不能带 __Host- 前缀，否则浏览器会丢弃它")
	}
}

// TestLoadProductionCookiePolicy 验证 APP_ENV=production 时前缀保护开启。
//
// 这是「配置文件里看起来一切正常，线上却少了防护」的典型位置：
// 降级开关如果判定条件写反（比如用 == 而不是 !=），只会在生产静默失守。
func TestLoadProductionCookiePolicy(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	cfg := Load()
	if cfg.SessionCookieInsecure {
		t.Error("生产环境不允许 Cookie 前缀降级")
	}
	if cfg.SessionCookieName != SessionCookieNameSecure {
		t.Errorf("生产会话 Cookie 名 = %q，想要 %q", cfg.SessionCookieName, SessionCookieNameSecure)
	}
	// 「记住我」的上限必须显著长于默认值，否则勾了等于没勾。
	if cfg.RememberTTL <= cfg.RefreshTTL {
		t.Errorf("RememberTTL(%v) 应大于 RefreshTTL(%v)", cfg.RememberTTL, cfg.RefreshTTL)
	}
}

func TestLoadDevelopmentCookiePolicy(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	cfg := Load()
	if !cfg.SessionCookieInsecure {
		t.Error("开发环境必须降级，否则 http://localhost 下浏览器会丢弃 Cookie")
	}
	if cfg.SessionCookieName != SessionCookieNameInsecure {
		t.Errorf("开发会话 Cookie 名 = %q，想要 %q", cfg.SessionCookieName, SessionCookieNameInsecure)
	}
}
