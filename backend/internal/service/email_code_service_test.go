package service

import (
	"testing"
	"time"
)

// =====================================================================
// EmailCodeService 的行为测试
//
// 覆盖四条容易出错的链路：
//   1. Send 的重发间隔与发信失败的补偿
//   2. Verify 的正确/错误/过期/用途不匹配/超尝试上限
//
// 这些都是纯逻辑，不连数据库（store 是内存 fake）。
// =====================================================================

// TestEmailCodeSendSuccess 验证发送成功后码已就位。
func TestEmailCodeSendSuccess(t *testing.T) {
	store := newFakeEmailCodeStore()
	m := &fakeMailer{}
	svc := newTestEmailCodeService(store, m)

	if err := svc.Send("user@example.com", PurposeLogin); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if m.calls != 1 {
		t.Errorf("邮件发送次数 = %d，想要 1", m.calls)
	}
	rec, ok := store.records[fakeKey("user@example.com", PurposeLogin)]
	if !ok || rec == nil {
		t.Fatal("发送成功后应有码记录")
	}
	if len(rec.code) != 6 {
		t.Errorf("验证码长度 = %d，想要 6", len(rec.code))
	}
}

// TestEmailCodeResendInterval 验证重发间隔。
func TestEmailCodeResendInterval(t *testing.T) {
	store := newFakeEmailCodeStore()
	svc := newTestEmailCodeService(store, &fakeMailer{})

	if err := svc.Send("user@example.com", PurposeLogin); err != nil {
		t.Fatalf("首次 Send: %v", err)
	}
	first, _ := store.records[fakeKey("user@example.com", PurposeLogin)]

	err := svc.Send("user@example.com", PurposeLogin)
	if err == nil {
		t.Fatal("间隔内第二次发送应被拒")
	}
	// 码不能被覆盖：用户手里那枚还有效
	now, _ := store.records[fakeKey("user@example.com", PurposeLogin)]
	if now.code != first.code {
		t.Error("被拒的重发覆盖了原有验证码")
	}
}

// TestEmailCodeSendFailureInvalidates 验证发信失败会作废记录。
//
// 这是补偿路径：若不清理，用户点"重新发送"会被"过于频繁"挡回来，
// 而邮件其实从没送达——彻底卡死。
func TestEmailCodeSendFailureInvalidates(t *testing.T) {
	store := newFakeEmailCodeStore()
	m := &fakeMailer{failN: 1}
	svc := newTestEmailCodeService(store, m)

	err := svc.Send("user@example.com", PurposeLogin)
	if err == nil {
		t.Fatal("发信失败应返回错误")
	}
	if _, ok := store.records[fakeKey("user@example.com", PurposeLogin)]; ok {
		t.Error("发信失败应作废码，否则重发会被间隔挡死")
	}

	// 恢复后能立刻再发（间隔已被补偿清掉）
	m.failN = 0
	if err := svc.Send("user@example.com", PurposeLogin); err != nil {
		t.Errorf("补偿后应能立即重发: %v", err)
	}
}

// TestEmailCodeVerifySuccess 验证正确的码通过且被消费。
func TestEmailCodeVerifySuccess(t *testing.T) {
	store := newFakeEmailCodeStore()
	svc := newTestEmailCodeService(store, &fakeMailer{})

	if err := svc.Send("user@example.com", PurposeLogin); err != nil {
		t.Fatal(err)
	}
	rec := store.records[fakeKey("user@example.com", PurposeLogin)]

	// 开启 login 场景要求：默认关闭时 Verify 直接放行，测不到真逻辑
	settingDefaults[SettingEmailCodeOnLogin] = "true"
	t.Cleanup(func() { settingDefaults[SettingEmailCodeOnLogin] = "false" })

	if err := svc.Verify(PurposeLogin, "user@example.com", rec.code); err != nil {
		t.Fatalf("正确的码应通过: %v", err)
	}
	if _, ok := store.records[fakeKey("user@example.com", PurposeLogin)]; ok {
		t.Error("码应被一次性消费")
	}
}

// TestEmailCodeVerifyWrongCode 验证错误码被拒并累加尝试。
func TestEmailCodeVerifyWrongCode(t *testing.T) {
	store := newFakeEmailCodeStore()
	svc := newTestEmailCodeService(store, &fakeMailer{})

	if err := svc.Send("user@example.com", PurposeLogin); err != nil {
		t.Fatal(err)
	}
	settingDefaults[SettingEmailCodeOnLogin] = "true"
	t.Cleanup(func() { settingDefaults[SettingEmailCodeOnLogin] = "false" })

	err := svc.Verify(PurposeLogin, "user@example.com", "000000")
	if err == nil {
		t.Fatal("错误的码应被拒")
	}
	rec := store.records[fakeKey("user@example.com", PurposeLogin)]
	if rec.attempts != 1 {
		t.Errorf("错误尝试计数 = %d，想要 1", rec.attempts)
	}
}

// TestEmailCodeVerifyLockout 验证达到上限后作废。
//
// 5 次上限的意义是把成本推回邮件通道：攻击者不能在同一枚码上
// 把 10^6 的空间试完。
func TestEmailCodeVerifyLockout(t *testing.T) {
	store := newFakeEmailCodeStore()
	svc := newTestEmailCodeService(store, &fakeMailer{})

	if err := svc.Send("user@example.com", PurposeLogin); err != nil {
		t.Fatal(err)
	}
	correct := store.records[fakeKey("user@example.com", PurposeLogin)].code

	settingDefaults[SettingEmailCodeOnLogin] = "true"
	t.Cleanup(func() { settingDefaults[SettingEmailCodeOnLogin] = "false" })

	for i := 0; i < maxEmailCodeAttempts; i++ {
		_ = svc.Verify(PurposeLogin, "user@example.com", "111111")
	}

	if _, ok := store.records[fakeKey("user@example.com", PurposeLogin)]; ok {
		t.Error("达到上限后码应被作废")
	}
	// 即使现在输入正确的码也不该通过
	if err := svc.Verify(PurposeLogin, "user@example.com", correct); err == nil {
		t.Error("作废后的码不应通过")
	}
}

// TestEmailCodeVerifyExpired 验证过期码被拒且被清掉。
func TestEmailCodeVerifyExpired(t *testing.T) {
	store := newFakeEmailCodeStore()
	svc := newTestEmailCodeService(store, &fakeMailer{})

	now := time.Now()
	store.records[fakeKey("user@example.com", PurposeLogin)] = &fakeEmailRecord{
		code:      "123456",
		purpose:   PurposeLogin,
		expiresAt: now.Add(-time.Minute), // 已过期
		sentAt:    now.Add(-10 * time.Minute),
	}

	settingDefaults[SettingEmailCodeOnLogin] = "true"
	t.Cleanup(func() { settingDefaults[SettingEmailCodeOnLogin] = "false" })

	err := svc.Verify(PurposeLogin, "user@example.com", "123456")
	if err == nil {
		t.Fatal("过期码应被拒")
	}
	// 过期码要被清掉：否则用户重新申请会被重发间隔挡回来
	if _, ok := store.records[fakeKey("user@example.com", PurposeLogin)]; ok {
		t.Error("过期码应被清除，否则卡住重发")
	}
}

// TestEmailCodePurposeMismatch 验证用途不匹配被拒。
//
// 不校验用途的话，攻击者给自己邮箱申请"登录码"就能通过"重置密码"，
// 等于把两种不同权限的凭证混为一谈。
func TestEmailCodePurposeMismatch(t *testing.T) {
	store := newFakeEmailCodeStore()
	svc := newTestEmailCodeService(store, &fakeMailer{})

	if err := svc.Send("user@example.com", PurposeLogin); err != nil {
		t.Fatal(err)
	}
	code := store.records[fakeKey("user@example.com", PurposeLogin)].code

	// reset_password 恒为必需，无需改设置
	err := svc.Verify(PurposeResetPassword, "user@example.com", code)
	if err == nil {
		t.Fatal("登录用途的码不该通过重置密码校验")
	}
}

// TestEmailCodeNotRequired 验证场景未开启时直接放行。
//
// 与 lap/geetest/POW 对称的约定：开关未开绝不锁死用户。
func TestEmailCodeNotRequired(t *testing.T) {
	store := newFakeEmailCodeStore()
	svc := newTestEmailCodeService(store, &fakeMailer{})

	// login 默认关闭：不填码也应通过
	if err := svc.Verify(PurposeLogin, "user@example.com", ""); err != nil {
		t.Errorf("场景未开启应放行，得到 %v", err)
	}
}

// TestEmailCodeResetPasswordAlwaysRequired 验证忘记密码恒需验证码。
//
// 这条链路本身就以"能收到邮件"为身份证明，关掉它等于
// 任何人都能凭一个邮箱地址改掉该账号的密码。
// 后台为此没有提供开关——测试确认这一点不被后续改动破坏。
func TestEmailCodeResetPasswordAlwaysRequired(t *testing.T) {
	store := newFakeEmailCodeStore()
	svc := newTestEmailCodeService(store, &fakeMailer{})

	if !svc.Required(PurposeResetPassword) {
		t.Error("重置密码必须恒需验证码")
	}
}
