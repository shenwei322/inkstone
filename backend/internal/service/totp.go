package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP 步长与码长。30 秒是 RFC 6238 的默认步长，也是 Google Authenticator
// 等主流 App 的默认值；改步长会让用户扫码后的 App 显示"错误的码"。
const (
	totpStep   = 30 * time.Second
	totpDigits = 6
	// totpWindow 是校验时允许前后偏移的步数。±1 步覆盖手机与服务器时钟
	// 偏差（实测普遍在 10 秒内）；窗口再放大会成倍放宽重放空间，收益递减。
	totpWindow = 1
)

// base32NoPadding 是 otpauth URI 规定的密钥编码：RFC 4648 base32，去掉 '='。
// 保留 padding 的编码会被 Google Authenticator 拒绝（识别不出密钥）。
var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret 生成一个新的 TOTP 密钥（160 bit，主流 App 支持的上限）。
func GenerateTOTPSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base32NoPadding.EncodeToString(buf), nil
}

// TOTPAuthURI 构造 otpauth URI，供前端渲染成二维码供 App 扫描。
//
// issuer 出现在 App 的账户列表里（如 "InkStone: a@b.com"），accountName 用于
// 区分同一 App 下的多个账号。
func TOTPAuthURI(issuer, accountName, secret string) string {
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(int(totpStep.Seconds())))
	return fmt.Sprintf("otpauth://totp/%s:%s?%s",
		url.PathEscape(issuer), url.PathEscape(accountName), q.Encode())
}

// ValidateTOTP 校验用户提交的 6 位码。counter 是当前时间步（unix秒 / 30），
// burned 里含该步时视为重放，直接拒绝。
//
// 窗口内按"距当前步越近越优先"的顺序尝试，先命中即返回，因此同一 App
// 在边界时刻生成的码不会被前后两步同时接受。
func ValidateTOTP(secret, code string, counter int64, burned []int64) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return false
	}
	for _, c := range burned {
		if c == counter {
			return false
		}
	}
	for _, c := range windowCounters(counter) {
		if totpCode(secret, c) == code {
			return true
		}
	}
	return false
}

// windowCounters 返回从中心步开始向外展开的待尝试步序（0, -1, +1, ...）。
func windowCounters(center int64) []int64 {
	out := make([]int64, 0, totpWindow*2+1)
	out = append(out, center)
	for i := int64(1); i <= totpWindow; i++ {
		out = append(out, center-i, center+i)
	}
	return out
}

// totpCode 按 RFC 4226 的动态截断算法计算某一步的码。
func totpCode(secret string, counter int64) string {
	key, err := base32NoPadding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return ""
	}
	// counter 为大端 8 字节。负数（时钟回拨）直接算不出有效码。
	var buf [8]byte
	c := uint64(counter)
	for i := 7; i >= 0; i-- {
		buf[i] = byte(c & 0xff)
		c >>= 8
	}
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// 动态截断：取最后一个字节的低 4 位作为偏移。
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]&0xff) << 16) |
		(uint32(sum[offset+2]&0xff) << 8) |
		uint32(sum[offset+3]&0xff)
	code := value % 1000000
	return fmt.Sprintf("%0*d", totpDigits, code)
}

// ParseBurnedCodes 把库里逗号分隔的已用步序号还原成数字切片。
func ParseBurnedCodes(raw string) []int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var n int64
		if _, err := fmt.Sscanf(p, "%d", &n); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// TOTPCounter 返回某个时刻对应的时间步。
func TOTPCounter(t time.Time) int64 {
	return t.Unix() / int64(totpStep/time.Second)
}
