package service

// 人机验证 provider 取值（设置项 captcha_provider）。
const (
	CaptchaProviderGeetest = "geetest" // 极验第四代行为验证
	CaptchaProviderLap     = "lap"     // Lap（Cap 的 Cloudflare Workers 分支，工作量证明）
	CaptchaProviderPow     = "pow"     // 自研工作量证明（零外部依赖，服务器/本机通用）
)

// CaptchaParams 是前端提交的人机验证凭证。按 captcha_provider 取用不同字段：
// geetest 用内嵌的四元组，lap 用 LapToken（widget solve 事件产出的
// SITEKEY:ID:TOKEN），pow 用 PowChallenge+PowNonce+PowSignal（前端算出的
// PoW 答案 + 本地交互事件流）。请求体同时接收全部字段，未选中的 provider 忽略。
type CaptchaParams struct {
	GeetestParams
	LapToken     string `json:"lap_token"`     // Lap 令牌
	PowChallenge string `json:"pow_challenge"` // POW 挑战
	PowNonce     string `json:"pow_nonce"`     // POW 答案（满足难度前导零的 nonce）
	PowSignal    string `json:"pow_signal"`    // POW v2 本地交互事件流（m/k/t:ms:x:y 逗号连接）
}

// CaptchaService 是人机验证门面：按 captcha_provider 设置把校验分发到
// 具体 provider，供各 handler 统一调用；同时负责 /site-config 的
// 前台配置下发（provider + 两套配置，前端按 provider 选用）。
type CaptchaService struct {
	settings *SettingsService
	geetest  *GeetestService
	lap      *LapService
	pow      *PowService
}

func NewCaptchaService(settings *SettingsService, geetest *GeetestService, lap *LapService, pow *PowService) *CaptchaService {
	return &CaptchaService{settings: settings, geetest: geetest, lap: lap, pow: pow}
}

// Provider 返回当前启用的 provider，未配置或非法值回退极验（保持旧行为）。
func (s *CaptchaService) Provider() string {
	v, err := s.settings.Get(SettingCaptchaProvider)
	if err != nil {
		return CaptchaProviderGeetest
	}
	switch v {
	case CaptchaProviderLap:
		return CaptchaProviderLap
	case CaptchaProviderPow:
		return CaptchaProviderPow
	default:
		return CaptchaProviderGeetest
	}
}

// Required 判断某个场景是否开启了人机验证（按当前 provider 分发，
// 供 handler 层在业务校验前决定是否强制）。
func (s *CaptchaService) Required(action string) bool {
	switch s.Provider() {
	case CaptchaProviderLap:
		return s.lap.Required(action)
	case CaptchaProviderPow:
		return s.pow.Required(action)
	default:
		return s.geetest.Required(action)
	}
}

// PublicConfig 下发到前台的人机验证配置：provider 选择器 + 各 provider
// 的公开配置（均不含密钥）。前台只渲染/初始化当前 provider 那套。
func (s *CaptchaService) PublicConfig() map[string]any {
	return map[string]any{
		"provider": s.Provider(),
		"geetest":  s.geetest.PublicConfig(),
		"lap":      s.lap.PublicConfig(),
		"pow":      s.pow.PublicConfig(),
	}
}

// Verify 校验一次人机验证凭证（按当前 provider 分发到具体服务）。
func (s *CaptchaService) Verify(action string, p CaptchaParams) error {
	switch s.Provider() {
	case CaptchaProviderLap:
		return s.lap.Verify(action, p.LapToken)
	case CaptchaProviderPow:
		return s.pow.Verify(action, p.PowChallenge, p.PowNonce, p.PowSignal)
	default:
		return s.geetest.Verify(action, p.GeetestParams)
	}
}
