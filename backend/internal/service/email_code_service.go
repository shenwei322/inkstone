package service

import (
	"crypto/hmac"
	"crypto/rand"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"github.com/shenwei/inkstone/backend/internal/repository"
)

// MailSender 由外部注入（pkg/mailer 实现），避免 service 与 mailer 循环依赖。
type MailSender interface {
	Send(to, subject, body string) error
}

// EmailCodeStore 是邮箱验证码的存储抽象。
//
// 与 ChallengeStore 同理：service 只依赖接口，测试才能跑在内存实现上。
// 方法名刻意与 repository 的实现一一对应，便于比对行为。
type EmailCodeStore interface {
	// UpsertIfNotRecent 写入一枚新码，但仅当此前没有记录、
	// 或已有记录已过重发间隔。返回是否写入。
	UpsertIfNotRecent(email, purpose, code string, sentAt, expiresAt time.Time, resendInterval time.Duration) (bool, error)
	// Get 取记录，不存在返回 nil。
	Get(email, purpose string) (*model.EmailCode, error)
	// Consume 校验并作废一枚码：比对一致且未过期且未超尝试次数则返回 true。
	// 无论结果如何，记录都会被删除。
	Consume(email, purpose, codeHash string, now time.Time) (bool, error)
	// RecordFailure 累加一次错误尝试，返回新计数与是否已达上限。
	RecordFailure(email, purpose string, maxAttempts int) (int, bool, error)
	// Invalidate 作废某邮箱+用途的码。
	Invalidate(email, purpose string) error
	// ClearExpired 清理过期码。
	ClearExpired(now time.Time) (int64, error)
}

// 编译期断言：生产仓储必须实现接口。
var _ EmailCodeStore = (*repository.EmailCodeRepository)(nil)

// EmailCodeService issues and verifies one-time codes sent by email.
//
// 验证码存共享存储（email_codes 表）：用户要去邮箱收信、复制、再回来填，
// 这段时间足以让下一个请求落到另一个实例上。内存 map 版在多副本部署下
// 会让用户看到「验证码已过期」——明明刚收到的码。
type EmailCodeService struct {
	settings *SettingsService
	mailer   MailSender
	codes    EmailCodeStore
}

// 尝试上限由 repository 定义：它最终由 SQL 在 Consume 里判定，
// 值与实现同层可避免两侧各持一份而漂移。这里只是引用。
const maxEmailCodeAttempts = repository.MaxEmailCodeAttempts

// 验证码用途。register / login 由后台开关控制是否必需；
// reset_password 恒为必需——忘记密码这条链路本身就以"能收到邮件"为身份证明。
const (
	PurposeRegister      = "register"
	PurposeLogin         = "login"
	PurposeResetPassword = "reset_password"
)

// resendInterval 是同一邮箱两次发送之间的最小间隔。
// 固定值，不随后台 TTL 设置变化——否则调大 TTL 会让判断点前移，
// 同一邮箱可被立即重复发信（邮件轰炸）。
const resendInterval = time.Minute

func NewEmailCodeService(settings *SettingsService, mailClient MailSender, codes EmailCodeStore) *EmailCodeService {
	s := &EmailCodeService{
		settings: settings,
		mailer:   mailClient,
		codes:    codes,
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

	// 固定间隔限流与写入是同一个 UPSERT：WHERE 条件限定"不存在记录，
	// 或已有记录已过重发间隔"。并发请求必然只有一个能写进去——
	// 若拆成两次查询，两个请求会同时通过检查，各发一封信，
	// 后写的那枚顶掉先前的，用户拿第一枚来校验必然失败。
	//
	// 先随机再判断：若顺序反了，被限流挡住时 rand 已经消耗了熵，
	// 白做功。rand.Int 的失败概率极低，但放在这里语义也更清楚。
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return err
	}
	code := fmt.Sprintf("%06d", n.Int64())

	sentAt := time.Now()
	accepted, err := s.codes.UpsertIfNotRecent(
		email, purpose, code, sentAt, sentAt.Add(s.ttl()), resendInterval,
	)
	if err != nil {
		return err
	}
	if !accepted {
		return NewValidationError("验证码发送过于频繁，请稍后再试")
	}

	subject := "InkStone " + purposeLabel(purpose)
	body := fmt.Sprintf("您的%s是：%s\n\n有效期 %d 分钟，请勿泄露给他人。\n\n—— InkStone",
		purposeLabel(purpose), code, int(s.ttl().Minutes()))

	if err := s.mailer.Send(email, subject, body); err != nil {
		log.Printf("[emailcode] send to %s failed: %v", email, err)
		// 发信失败：作废记录，别让一个发不出去的码占着重发间隔。
		// 用户点"重新发送"若被"过于频繁"挡回来，而邮件其实从没送达，
		// 等于彻底卡死在这个环节。
		_ = s.codes.Invalidate(email, purpose)
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

	now := time.Now()
	codeHash := repository.HashEmailCode(code)

	// 先查一次，只为了给出更精确的提示。
	// 没有记录时不能直接走 Consume：那只会说「不正确」，
	// 用户分不清是"从没申请过"还是"输错了"。
	rec, err := s.codes.Get(email, action)
	if err != nil {
		return err
	}
	if rec == nil {
		return NewValidationError("验证码已过期，请重新获取")
	}
	if now.After(rec.ExpiresAt) {
		// 顺手作废：让用户能立刻重新申请，不被重发间隔挡回来。
		// Consume 自己也会删（无条件删除），但那条路要用户先提交一次；
		// 这里在提示时就清掉，体验更直接。
		_ = s.codes.Invalidate(email, action)
		return NewValidationError("验证码已过期，请重新获取")
	}
	// 用途不匹配：给自己邮箱申请的登录码不能用来重置密码
	// （见 EmailCode 主键设计，同邮箱可并行持有多个用途的码）。
	if rec.Purpose != action {
		return NewValidationError("验证码不正确")
	}

	// 常量时间比较：虽然 6 位空间 + 5 次尝试 + IP 限流已使时序侧信道
	// 不可利用，用 crypto/hmac.Equal 顺手硬化，且不增加复杂度。
	if !hmac.Equal([]byte(rec.CodeHash), []byte(codeHash)) {
		return s.recordFailure(email, action)
	}

	// 正确：作废。无条件删除，即使刚好在这一刻过期
	// （那种情况下 Consume 返回 false，但行也已被清掉）。
	if err := s.codes.Invalidate(email, action); err != nil {
		return err
	}
	return nil
}

// recordFailure 累加一次错误尝试，并在达到上限时作废该码。
func (s *EmailCodeService) recordFailure(email, purpose string) error {
	_, exhausted, err := s.codes.RecordFailure(email, purpose, maxEmailCodeAttempts)
	if err != nil {
		return err
	}
	if exhausted {
		// 作废：把成本推回邮件通道。用户重新申请要过重发间隔与 IP 限流，
		// 攻击者不能在同一枚码上把 10^6 的空间试完。
		_ = s.codes.Invalidate(email, purpose)
		return NewValidationError("尝试次数过多，请重新获取验证码")
	}
	return NewValidationError("验证码不正确")
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
		if _, err := s.codes.ClearExpired(time.Now()); err != nil {
			// 清理失败不该让循环退出：下次 tick 会再试。
			// 最坏情况只是多几行过期数据，不影响正确性。
			log.Printf("[emailcode] cleanup failed: %v", err)
		}
	}
}
