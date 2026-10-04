package service

import (
	"strings"
	"testing"
)

// =====================================================================
// 挑战场景绑定
//
// 背景：签发接口是统一的 /pow/challenge，且不区分场景。加固前挑战记录里
// 不记场景，于是可以为代价最低的场景批量签发挑战、再拿去打登录接口——
// 把 PoW 成本与实际攻击目标解耦。实测（pow_intercept_test.go 的 A13 档）
// 跨场景挪用的拦截率是 0%。
//
// 现状：签发时可声明 scene，挑战被钉在该场景上；空 scene 表示不绑定
// （兼容不带 body 的老前端）。
// =====================================================================

// TestPowSceneBindingRejectsCrossScene 跨场景使用必须被拒。
func TestPowSceneBindingRejectsCrossScene(t *testing.T) {
	pow, captcha := powTestEnv(t, map[string]string{
		SettingPowDifficulty: "1", // 便于真正解出，确保被拒原因是场景而非算力
	})

	// 为 login 签发
	ch, d, mb, rounds, me, _, err := pow.IssueFor("login")
	if err != nil {
		t.Fatalf("IssueFor: %v", err)
	}
	nonce := powSolveOne(ch, d, mb, rounds, 0)
	if nonce == "" {
		t.Fatal("难度 1 下未解出")
	}

	// 拿到 comment 场景去用：必须被拒
	err = captcha.Verify("comment", CaptchaParams{
		PowChallenge: ch, PowNonce: nonce, PowSignal: powMakeSignal("robot", me),
	})
	if err == nil {
		t.Fatal("login 签发的挑战被 comment 场景接受，场景绑定失效")
	}
	if !strings.Contains(err.Error(), "失效") {
		t.Errorf("拒绝原因应为「已失效」，实际: %v", err)
	}

	// 被拒后挑战已消费，换回 login 也不能用（防重放不被削弱）
	err = captcha.Verify("login", CaptchaParams{
		PowChallenge: ch, PowNonce: nonce, PowSignal: powMakeSignal("robot", me),
	})
	if err == nil {
		t.Error("被拒的挑战竟可二次使用，一次性消费语义被破坏")
	}
}

// TestPowSceneBindingAcceptsSameScene 本场景使用必须正常通过——
// 绑定不能误伤正常流程，否则用户会被永久卡住。
func TestPowSceneBindingAcceptsSameScene(t *testing.T) {
	pow, captcha := powTestEnv(t, map[string]string{
		SettingPowDifficulty: "1",
	})

	for _, scene := range []string{"login", "register", "comment"} {
		ch, d, mb, rounds, me, _, err := pow.IssueFor(scene)
		if err != nil {
			t.Fatalf("%s IssueFor: %v", scene, err)
		}
		nonce := powSolveOne(ch, d, mb, rounds, 0)
		if nonce == "" {
			t.Fatalf("%s 未解出", scene)
		}
		if err := captcha.Verify(scene, CaptchaParams{
			PowChallenge: ch, PowNonce: nonce, PowSignal: powMakeSignal("human", me),
		}); err != nil {
			t.Errorf("%s 场景用自己的挑战应通过，实际: %v", scene, err)
		}
	}
}

// TestPowSceneBindingCaseAndSpace 场景名做归一化：大小写与首尾空白不应
// 造成误拒（前端传 "Login" 也要能用）。
func TestPowSceneBindingCaseAndSpace(t *testing.T) {
	pow, captcha := powTestEnv(t, map[string]string{
		SettingPowDifficulty: "1",
	})

	ch, d, mb, rounds, me, _, err := pow.IssueFor("  LOGIN  ")
	if err != nil {
		t.Fatalf("IssueFor: %v", err)
	}
	nonce := powSolveOne(ch, d, mb, rounds, 0)
	if err := captcha.Verify("login", CaptchaParams{
		PowChallenge: ch, PowNonce: nonce, PowSignal: powMakeSignal("human", me),
	}); err != nil {
		t.Errorf("场景名归一化后应通过，实际: %v", err)
	}

	// 反向：小写签发、大写提交
	ch2, d2, mb2, r2, me2, _, err := pow.IssueFor("login")
	if err != nil {
		t.Fatalf("IssueFor: %v", err)
	}
	nonce2 := powSolveOne(ch2, d2, mb2, r2, 0)
	if err := captcha.Verify("LOGIN", CaptchaParams{
		PowChallenge: ch2, PowNonce: nonce2, PowSignal: powMakeSignal("human", me2),
	}); err != nil {
		t.Errorf("大写场景名提交应通过，实际: %v", err)
	}
}

// TestPowSceneUnboundBackwardCompatible 不绑定场景的挑战（老前端行为）
// 必须仍可跨场景使用，否则升级会打断进行中的老版本前端。
func TestPowSceneUnboundBackwardCompatible(t *testing.T) {
	pow, captcha := powTestEnv(t, map[string]string{
		SettingPowDifficulty: "1",
	})

	// Issue() 等价于不绑定场景
	ch, d, mb, rounds, me, _, err := pow.Issue()
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	nonce := powSolveOne(ch, d, mb, rounds, 0)
	if err := captcha.Verify("comment", CaptchaParams{
		PowChallenge: ch, PowNonce: nonce, PowSignal: powMakeSignal("human", me),
	}); err != nil {
		t.Errorf("未绑定场景的挑战应可跨场景使用（兼容旧前端），实际: %v", err)
	}
}
