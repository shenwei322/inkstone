package service

import (
	"net"
	"testing"
)

// TestSSRFBypassVectors 专项测试各种已知的 SSRF 绕过表达法。
// 这些是安全测试的正确性验证，不是普通功能测试。
func TestSSRFBypassVectors(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want bool // true = 应判为"公网可访问"
	}{
		// IPv4-mapped IPv6：::ffff:127.0.0.1 是回环的另一种写法
		{"v4-mapped loopback", "::ffff:127.0.0.1", false},
		{"v4-mapped private", "::ffff:10.0.0.1", false},
		{"v4-mapped metadata", "::ffff:169.254.169.254", false},

		// IPv4-compatible IPv6（非 mapped）：::127.0.0.1、::10.0.0.1
		// Go 的 To4() 对这类地址返回 nil，会走 IPv6 分支！
		{"v4-compatible loopback", "::127.0.0.1", false},
		{"v4-compatible private", "::10.0.0.1", false},
		{"v4-compatible metadata", "::169.254.169.254", false},

		// IPv6 回环/链路本地/唯一本地的其他写法
		{"ipv6 loopback long", "0:0:0:0:0:0:0:1", false},
		{"unspecified v6", "::", false},

		// 真实公网
		{"public v4", "8.8.8.8", true},
		{"public v6", "2001:4860:4860::8888", true},
	}

	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("%s: 无法解析 IP %q", c.name, c.ip)
		}
		if got := isPublicIP(ip); got != c.want {
			t.Errorf("%s (%s): isPublicIP=%v，期望 %v —— 这是一个 SSRF 绕过！",
				c.name, c.ip, got, c.want)
		}
	}
}
