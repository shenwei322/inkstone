package service

import (
	"crypto/hmac"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"
	"time"
)

// MailSender 由外部注入（pkg/mailer 实现），避免 service 与 mailer 循环依赖。
type MailSender interface {
	Send(to, subject, body string) error
}

// EmailCodeService issues and verifies one-time codes sent by email.
// Codes are kept in memory (single-instance deployments); swapping in Redis
// only requires replacing the store below.
type EmailCodeService struct {
	settings *SettingsService
	mailer   MailSender
	mu       sync.Mutex
	codes    map[string]codeEntry
}

type codeEntry struct {
	code      string
	expiresAt time.Time
	// sentAt 记录签发时刻，用于固定的重发间隔判断。
	// 此前用 expiresAt-ttl()/2 反推，一旦管理员调大 TTL 就会把判断点前移，
	// 同一邮箱可被立即重复发信（邮件轰炸）。显式记录则不依赖 TTL 配置。
	sentAt   time.Time
	attempts int
}

// resendInterval 是同一邮箱两次发送之间的最小间隔。
const resendInterval = time.Minute

func NewEmailCodeService(settings *SettingsService, mailClient MailSender) *EmailCodeService {
	s := &EmailCodeService{
		settings: settings,
		mailer:   mailClient,
		codes:    make(map[string]codeEntry),
	}
	go s.cleanupLoop()
	return s
}

// Required reports whether the given action ("register" / "login") needs an
// email verification code.
func (s *EmailCodeService) Required(action string) bool {
	key := SettingEmailCodeOnRegister
	if action == "login" {
		key = SettingEmailCodeOnLogin
	}
	return s.settings.BoolValue(key, false)
}

func (s *EmailCodeService) ttl() time.Duration {
	m := s.settings.IntValue(SettingEmailCodeTTLMinutes, 10)
	if m < 1 || m > 60 {
		m = 10
	}
	return time.Duration(m) * time.Minute
}

// Send generates a 6-digit code and emails it to the address.
func (s *EmailCodeService) Send(email, purpose string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return NewValidationError("请先填写邮箱")
	}

	s.mu.Lock()
	// 固定间隔限流：与 TTL 配置解耦，改设置不会缩短静默期
	if entry, ok := s.codes[email]; ok && time.Since(entry.sentAt) < resendInterval {
		s.mu.Unlock()
		return NewValidationError("验证码发送过于频繁，请稍后再试")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		s.mu.Unlock()
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	s.codes[email] = codeEntry{
		code:      code,
		expiresAt: time.Now().Add(s.ttl()),
		sentAt:    time.Now(),
	}
	s.mu.Unlock()

	subject := "InkStone 邮箱验证码"
	body := fmt.Sprintf("您的验证码是：%s\n\n有效期 %d 分钟，请勿泄露给他人。\n\n—— InkStone",
		code, int(s.ttl().Minutes()))

	if err := s.mailer.Send(email, subject, body); err != nil {
		log.Printf("[emailcode] send to %s failed: %v", email, err)
		// 验证码已写入内存，但邮件没发出去 —— 清零并返回可读错误（400 而非 500）
		s.mu.Lock()
		delete(s.codes, email)
		s.mu.Unlock()
		return NewValidationError("验证码发送失败：" + err.Error())
	}
	log.Printf("[emailcode] code sent to %s (purpose=%s)", email, purpose)
	return nil
}

// Verify checks the submitted code for an email and action (consumed on success).
// action: "register" | "login"
func (s *EmailCodeService) Verify(action, email, code string) error {
	if !s.Required(action) {
		return nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	code = strings.TrimSpace(code)
	if code == "" {
		return NewValidationError("请输入邮箱验证码")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.codes[email]
	if !ok || time.Now().After(entry.expiresAt) {
		return NewValidationError("验证码已过期，请重新获取")
	}
	if entry.attempts >= 5 {
		delete(s.codes, email)
		return NewValidationError("尝试次数过多，请重新获取验证码")
	}
	// 常量时间比较：虽然 6 位空间 + 5 次尝试 + IP 限流已使时序侧信道
	// 不可利用，用 crypto/hmac.Equal 顺手硬化，且不增加复杂度。
	if !hmac.Equal([]byte(entry.code), []byte(code)) {
		entry.attempts++
		s.codes[email] = entry
		return NewValidationError("验证码不正确")
	}
	delete(s.codes, email)
	return nil
}

// Enabled reports whether any action requires email codes.
func (s *EmailCodeService) Enabled() bool {
	return s.Required("register") || s.Required("login")
}

// PublicConfig exposes which actions require email verification.
func (s *EmailCodeService) PublicConfig() map[string]any {
	return map[string]any{
		"on_register": s.Required("register"),
		"on_login":    s.Required("login"),
	}
}

func (s *EmailCodeService) cleanupLoop() {
	for range time.Tick(10 * time.Minute) {
		now := time.Now()
		s.mu.Lock()
		for k, v := range s.codes {
			if now.After(v.expiresAt) {
				delete(s.codes, k)
			}
		}
		s.mu.Unlock()
	}
}
