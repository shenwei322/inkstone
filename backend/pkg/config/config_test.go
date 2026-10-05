package config

import "testing"

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
