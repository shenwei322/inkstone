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
	sentAt time.Time
	// purpose 记录这个码是为哪个场景签发的。校验时比对用途：
	// 不比对的话，攻击者给自己邮箱申请一个"登录验证码"就能通过"重置密码"
	// 的校验，等于把改密凭证与登录凭证混为一谈。
	purpose  string
	attempts int
}

// 验证码用途。register / login 由后台开关控制是否必需；
// reset_password 恒为必需——忘记密码这条链路本身就以"能收到邮件"为身份证明。
const (
	PurposeRegister      = "register"
	PurposeLogin         = "login"
	PurposeResetPassword = "reset_password"
)

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

// Required reports whether the given action needs an email verification code.
func (s *EmailCodeService) Required(action string) bool {
	switch action {
	case PurposeLogin:
		return s.settings.BoolValue(SettingEmailCodeOnLogin, false)
	case PurposeResetPassword:
		// 恒定需要：忘记密码没有别的身份凭证可用。若关闭它，
		// 任何人都能凭一个邮箱地址把该账号的密码改掉。
		return true
	default:
		return s.settings.BoolValue(SettingEmailCodeOnRegister, false)
	}
}

// purposeLabel 返回某用途对应的邮件主题。文案按场景区分，避免用户在
// "只是想登录"时收到一封写着"重置密码"的邮件而不敢点。
func purposeLabel(purpose string) string {
	switch purpose {
	case PurposeLogin:
		return "登录验证码"
	case PurposeResetPassword:
		return "密码重置验证码"
	default:
		return "注册验证码"
	}
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
	if purpose == "" {
		purpose = PurposeRegister
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
		purpose:   purpose,
	}
	s.mu.Unlock()

	subject := "InkStone " + purposeLabel(purpose)
	body := fmt.Sprintf("您的%s是：%s\n\n有效期 %d 分钟，请勿泄露给他人。\n\n—— InkStone",
		purposeLabel(purpose), code, int(s.ttl().Minutes()))

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
	// 用途必须匹配：见 codeEntry.purpose 注释。
	if entry.purpose != action {
		return NewValidationError("验证码不正确")
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
