package service

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/shenwei/inkstone/backend/internal/model"
	"gorm.io/gorm"
)

// Setting definitions: key -> description of accepted values.
const (
	SettingAllowRegistration = "allow_registration" // "true"/"false"
	SettingSiteName          = "site_name"          // string
	SettingSiteDescription   = "site_description"   // string
	SettingSiteLogo          = "site_logo"          // image URL, empty = default letter mark
	SettingSiteFavicon       = "site_favicon"       // image URL, empty = default
	SettingNavMenu           = "nav_menu"           // JSON array of menu items
	SettingSidebarWidgets    = "sidebar_widgets"    // JSON array of widgets
	SettingSidebarPosition   = "sidebar_position"   // "right" (default) | "left"
	SettingICP               = "site_icp"           // string
	SettingSMTPHost          = "smtp_host"
	SettingSMTPPort          = "smtp_port" // string digits
	SettingSMTPUser          = "smtp_user"
	SettingSMTPPass          = "smtp_pass"
	SettingSMTPFrom          = "smtp_from"         // From header, e.g. "Blog <no-reply@x.com>"
	SettingUploadMaxMB       = "upload_max_mb"     // 文件管理：最大上传大小（MB）
	SettingUploadSpeedKB     = "upload_speed_kb"   // 文件管理：上传限速（KB/s，0=不限）
	SettingDownloadSpeedKB   = "download_speed_kb" // 文件管理：下载限速（KB/s，0=不限）

	// 安全防护
	SettingSecurityEnabled      = "security_enabled"       // 主开关
	SettingSecurityLoginMax     = "security_login_max"     // 登录失败次数上限/15 分钟
	SettingSecurityRegisterMax  = "security_register_max"  // 注册次数上限/小时
	SettingSecurityCommentMax   = "security_comment_max"   // 评论次数上限/10 分钟
	SettingSecurityAPIMax       = "security_api_max"       // 单 IP API 请求上限/分钟
	SettingSecurityBlockMinutes = "security_block_minutes" // 触发后封禁时长（分钟）

	// 邮箱验证码
	SettingEmailCodeOnRegister = "email_code_on_register" // 注册需邮箱验证码
	SettingEmailCodeOnLogin    = "email_code_on_login"    // 登录需邮箱验证码
	SettingEmailCodeTTLMinutes = "email_code_ttl_minutes" // 验证码有效期（分钟）

	// 人机验证（极验第四代行为验证）
	SettingGeetestEnabled    = "geetest_enabled"     // 总开关
	SettingGeetestCaptchaID  = "geetest_captcha_id"  // 极验验证 ID（前台初始化用，非敏感）
	SettingGeetestCaptchaKey = "geetest_captcha_key" // 极验密钥（仅服务端二次验证用，敏感字段）
	SettingGeetestOnLogin    = "geetest_on_login"    // 登录需人机验证
	SettingGeetestOnRegister = "geetest_on_register" // 注册需人机验证
	SettingGeetestOnComment  = "geetest_on_comment"  // 评论/发表需人机验证

	// 人机验证 provider：决定前台加载哪套验证组件、后端走哪条校验链路。
	// 默认 lap：内置默认实例（lap_defaults.go）开箱即用，无需后台配置密钥；
	// 老部署在后台可随时切回 geetest。
	SettingCaptchaProvider = "captcha_provider" // "lap"（默认）| "geetest"

	// 人机验证（Lap —— Cap 的 Cloudflare Workers 分支，工作量证明验证码）
	SettingLapEnabled     = "lap_enabled"      // 总开关
	SettingLapAPIEndpoint = "lap_api_endpoint" // Lap 实例地址（含 siteKey，形如 https://xxx.workers.dev/SITEKEY/）
	SettingLapSiteKey     = "lap_site_key"     // Lap site key（前台 widget 初始化用，非敏感）
	SettingLapSecretKey   = "lap_secret_key"   // Lap secret（仅服务端 siteverify 二次验证用，敏感字段）
	SettingLapResolveIP   = "lap_resolve_ip"   // DNS 覆盖：访问 Lap 实例时拨号固定 IP（本机 DNS 被污染时填真实 IP）
	SettingLapHTTPProxy   = "lap_http_proxy"   // HTTP 代理：服务器无法直连 Lap 实例时经代理访问（形如 http://127.0.0.1:7897，生产留空）
	SettingLapOnLogin     = "lap_on_login"     // 登录需人机验证
	SettingLapOnRegister  = "lap_on_register"  // 注册需人机验证
	SettingLapOnComment   = "lap_on_comment"   // 评论/发表需人机验证

	// 人机验证（POW —— 自研工作量证明，零外部依赖，服务器/本机通用）
	SettingPowEnabled    = "pow_enabled"     // 总开关（默认 false，后台按需开启）
	SettingPowDifficulty = "pow_difficulty"  // 难度：答案哈希前导零个数（十六进制位，1-6，越大越难；默认 4）
	SettingPowTTLMinutes = "pow_ttl_minutes" // 挑战有效期（分钟，1-60；默认 10）
	SettingPowOnLogin    = "pow_on_login"    // 登录需人机验证
	SettingPowOnRegister = "pow_on_register" // 注册需人机验证
	SettingPowOnComment  = "pow_on_comment"  // 评论/发表需人机验证
	// POW v2：本地资源参数（签发时快照进挑战，服务端只校验一次）
	//
	// 这两个参数的取舍由实测标定决定（见 pow_calibrate_test.go）：
	// 建表是「每挑战一次」的开销，而服务端每次校验都要重建一遍表；
	// 轮数则乘在每一个候选 nonce 上，客户端要付 16^difficulty 次、服务端只付 1 次。
	// 也就是说 memoryMB 的成本几乎全压在服务端，却几乎不增加攻击者的搜索成本。
	//
	// 实测（difficulty=4，每次 40 个挑战取均值）：
	//   8MB / 4 轮：服务端 2.09 ms，客户端 ~60 ms
	//   1MB / 12 轮：服务端 0.22 ms（快 9.7 倍），客户端 ~60 ms，且攻击者每个
	//                候选的成本从 5 次 SHA-256 提高到 13 次（贵 2.6 倍）
	// 因此默认值取「小表 + 多轮」：服务端更便宜、攻击者更贵。
	SettingPowMemoryMB  = "pow_memory_mb"  // 内存表大小 MB（1-32，默认 1）：建表开销几乎全落在服务端，不宜调大
	SettingPowRounds    = "pow_rounds"     // 表查找-混合轮数（1-16，默认 12）：每轮一次随机查表 + 一次 SHA-256，是攻击者每次尝试的真实成本
	SettingPowMinEvents = "pow_min_events" // 需采集的本地交互事件数（0-10，默认 3；0=关闭 signal 校验）。注意：signal 只是 UX 门槛，不构成对机器人的防护

	// 站点外观
	SettingSiteWallpaper    = "site_wallpaper"    // 全站壁纸图片地址
	SettingWallpaperOpacity = "wallpaper_opacity" // 壁纸不透明度（0-100）
	SettingWallpaperBlur    = "wallpaper_blur"    // 壁纸模糊（px）
	SettingArticleSidebar   = "article_sidebar"   // 文章页是否显示侧边栏："true"/"false"

	// 站点状态
	SettingMaintenanceMode = "maintenance_mode" // "true"/"false" 全站维护（关闭）模式

	// 友链自助申请（默认开放，后台审核）
	SettingFriendApplyEnabled = "friend_apply_enabled" // "true"/"false" 前台开放友链自助申请

	// 友情链接页面显示内容（后台 → 安全防护旁边的友链管理页编辑，/site-config 下发）
	SettingFriendLinksTitle = "friend_links_title" // 页面标题（默认「友情链接」）
	SettingFriendLinksIntro = "friend_links_intro" // 页面介绍文案（空 = 不显示介绍段）
)

var settingDefaults = map[string]string{
	SettingAllowRegistration: "true",
	SettingSiteName:          "InkStone",
	SettingSiteDescription:   "InkStone — 现代化多用户博客系统",
	SettingSiteLogo:          "",
	SettingSiteFavicon:       "",
	SettingNavMenu:           "[]",
	SettingSidebarWidgets:    "[]",
	SettingSidebarPosition:   "right",
	SettingICP:               "",
	SettingSMTPHost:          "",
	SettingSMTPPort:          "465",
	SettingSMTPUser:          "",
	SettingSMTPPass:          "",
	SettingSMTPFrom:          "",
	SettingUploadMaxMB:       "50",
	SettingUploadSpeedKB:     "0",
	SettingDownloadSpeedKB:   "0",

	SettingSecurityEnabled:      "true",
	SettingSecurityLoginMax:     "30",
	SettingSecurityRegisterMax:  "20",
	SettingSecurityCommentMax:   "30",
	SettingSecurityAPIMax:       "600",
	SettingSecurityBlockMinutes: "15",

	SettingEmailCodeOnRegister: "false",
	SettingEmailCodeOnLogin:    "false",
	SettingEmailCodeTTLMinutes: "10",

	SettingGeetestEnabled:    "false",
	SettingGeetestCaptchaID:  "",
	SettingGeetestCaptchaKey: "",
	SettingGeetestOnLogin:    "false",
	SettingGeetestOnRegister: "false",
	SettingGeetestOnComment:  "false",

	SettingCaptchaProvider: "lap",
	// Lap 零配置开箱即用：内置默认实例 + 登录场景默认开启人机验证
	SettingLapEnabled:     "true",
	SettingLapAPIEndpoint: "",
	SettingLapSiteKey:     "",
	SettingLapSecretKey:   "",
	SettingLapResolveIP:   "",
	SettingLapHTTPProxy:   "",
	SettingLapOnLogin:     "true",
	SettingLapOnRegister:  "false",
	SettingLapOnComment:   "false",

	// POW 默认关闭（零外部依赖，但需要后台显式开启；场景默认与 lap 对称）
	SettingPowEnabled:    "false",
	SettingPowDifficulty: "4",
	SettingPowTTLMinutes: "10",
	SettingPowOnLogin:    "true",
	SettingPowOnRegister: "false",
	SettingPowOnComment:  "false",
	// POW v2 本地资源参数：默认 1MB 表 + 12 轮查找 + 3 个交互事件。
	// 「小表 + 多轮」的依据见上面的标定注释与 pow_calibrate_test.go：
	// 表大小几乎只增加服务端成本，轮数才真正抬高攻击者的每次尝试成本。
	SettingPowMemoryMB:  "1",
	SettingPowRounds:    "12",
	SettingPowMinEvents: "3",

	SettingSiteWallpaper:      "",
	SettingWallpaperOpacity:   "100",
	SettingWallpaperBlur:      "0",
	SettingArticleSidebar:     "true",
	SettingMaintenanceMode:    "false",
	SettingFriendApplyEnabled: "true",
	SettingFriendLinksTitle:   "友情链接",
	SettingFriendLinksIntro:   "",
}

// jsonSettingKeys hold JSON arrays; they are decoded before leaving the API.
var jsonSettingKeys = map[string]bool{
	SettingNavMenu:        true,
	SettingSidebarWidgets: true,
}

func decodeJSONSetting(value string) any {
	if value == "" {
		return []any{}
	}
	var out any
	if err := json.Unmarshal([]byte(value), &out); err != nil {
		return []any{}
	}
	return out
}

// maskKeys are never exposed through the public API.
var maskKeys = map[string]bool{
	SettingSMTPPass:          true,
	SettingGeetestCaptchaKey: true,
	SettingLapSecretKey:      true,
}

// lapHiddenKeys 是后台不再展示的 Lap 技术配置（自托管场景仍可通过 DB /
// 环境变量覆盖）。Update 对它们做「空值保持原值」保护：任何渠道（含后台
// 误操作/API 直调）提交空串都不会把已配置的自托管值清空——配置丢失时
// 系统回退 lap_defaults.go 的内置默认实例，不会把人机验证打挂。
var lapHiddenKeys = map[string]bool{
	SettingLapAPIEndpoint: true,
	SettingLapSiteKey:     true,
	SettingLapResolveIP:   true,
	SettingLapHTTPProxy:   true,
}

type SettingsService struct {
	db *gorm.DB

	mu        sync.RWMutex
	cache     map[string]string
	cacheTime time.Time
}

func NewSettingsService(db *gorm.DB) *SettingsService {
	return &SettingsService{db: db}
}

// All returns every known setting, applying defaults for unset keys.
func (s *SettingsService) All() (map[string]string, error) {
	if s == nil || s.db == nil {
		// 未装配数据库（单测/极端启动顺序）：返回内置默认，绝不 panic
		return copyMap(settingDefaults), nil
	}
	s.mu.RLock()
	fresh := time.Since(s.cacheTime) < 30*time.Second
	cache := s.cache
	s.mu.RUnlock()

	if fresh && cache != nil {
		return copyMap(cache), nil
	}

	rows := []model.Setting{}
	if err := s.db.Find(&rows).Error; err != nil {
		return nil, err
	}
	values := make(map[string]string, len(settingDefaults))
	for k, v := range settingDefaults {
		values[k] = v
	}
	for _, row := range rows {
		values[row.Key] = row.Value
	}

	s.mu.Lock()
	s.cache = values
	s.cacheTime = time.Now()
	s.mu.Unlock()

	return copyMap(values), nil
}

// Get returns one setting value (with default fallback).
func (s *SettingsService) Get(key string) (string, error) {
	all, err := s.All()
	if err != nil {
		return "", err
	}
	return all[key], nil
}

// IntValue reads a numeric setting, falling back to fallback on parse errors.
func (s *SettingsService) IntValue(key string, fallback int) int {
	v, err := s.Get(key)
	if err != nil || v == "" {
		return fallback
	}
	n := 0
	neg := false
	// 超长数字串直接视为配置异常：int 溢出会回绕成负数或乱值，
	// 虽然上层对 POW/限流参数都有 clamp 兜底，但在这里挡掉更干净。
	const maxDigits = 18
	for i, ch := range v {
		if i == 0 && ch == '-' {
			neg = true
			continue
		}
		if ch < '0' || ch > '9' {
			return fallback
		}
		digitCount := i
		if neg {
			digitCount = i - 1
		}
		if digitCount >= maxDigits {
			return fallback
		}
		n = n*10 + int(ch-'0')
	}
	if neg {
		n = -n
	}
	return n
}

// BoolValue reads a boolean setting ("true"/"false").
func (s *SettingsService) BoolValue(key string, fallback bool) bool {
	v, err := s.Get(key)
	if err != nil || v == "" {
		return fallback
	}
	return v == "true"
}

// AllowRegistration reports whether open registration is enabled.
func (s *SettingsService) AllowRegistration() bool {
	v, err := s.Get(SettingAllowRegistration)
	if err != nil {
		return true
	}
	return v == "true"
}

// Update persists a partial settings object (JSON body) and invalidates cache.
// Empty smtp_pass means "keep the stored password".
func (s *SettingsService) Update(payload map[string]any) error {
	for key, raw := range payload {
		if _, known := settingDefaults[key]; !known {
			continue
		}
		value, err := marshalValue(raw)
		if err != nil {
			return NewValidationError("设置项 " + key + " 值无效")
		}
		// 敏感字段（SMTP 密码 / 各类密钥）的空白值表示「保持原值不变」，
		// 避免用户在后台清空输入框时误删已保存的密钥。
		if maskKeys[key] && strings.TrimSpace(value) == "" {
			continue
		}
		// Lap 隐藏技术配置同样做空值保护：后台已不展示这些字段，任何
		// 空串提交都视为「无变更」，绝不清空已配置的自托管值。
		if lapHiddenKeys[key] && strings.TrimSpace(value) == "" {
			continue
		}
		// 侧边栏「自定义 HTML」小工具会被前台直接 innerHTML 注入到每个访客
		// 页面，必须在写入前消毒（这是设置项里唯一的 HTML 通道）。
		if key == SettingSidebarWidgets {
			sanitized, err := sanitizeSidebarWidgets(value)
			if err != nil {
				return NewValidationError("侧边栏小工具配置格式无效")
			}
			value = sanitized
		}
		setting := model.Setting{Key: key, Value: value, UpdatedAt: time.Now()}
		if err := s.db.Save(&setting).Error; err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.cacheTime = time.Time{}
	s.mu.Unlock()
	return nil
}

// sanitizeSidebarWidgets 净化侧边栏小工具 JSON 里的 HTML 内容。
//
// 只处理 type=="html" 的条目（其余类型是纯文本/URL 字段，不走 innerHTML）；
// 非数组或非法 JSON 时返回错误，避免把坏数据写进设置表。
func sanitizeSidebarWidgets(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return value, nil
	}
	var widgets []map[string]any
	if err := json.Unmarshal([]byte(value), &widgets); err != nil {
		return "", err
	}
	changed := false
	for _, w := range widgets {
		typ, _ := w["type"].(string)
		if typ != "html" {
			continue
		}
		content, ok := w["content"].(string)
		if !ok || content == "" {
			continue
		}
		if sanitized := SanitizeWidgetHTML(content); sanitized != content {
			w["content"] = sanitized
			changed = true
		}
	}
	if !changed {
		// 内容没变时原样返回，保持存储格式稳定
		return value, nil
	}
	raw, err := json.Marshal(widgets)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// Public returns non-sensitive settings for the frontend.
func (s *SettingsService) Public() (map[string]any, error) {
	all, err := s.All()
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(all))
	for k, v := range all {
		if maskKeys[k] {
			continue
		}
		if jsonSettingKeys[k] {
			out[k] = decodeJSONSetting(v)
			continue
		}
		if k == SettingAllowRegistration {
			out[k] = v == "true"
			continue
		}
		out[k] = v
	}
	return out, nil
}

// AdminView returns settings for the admin console. The SMTP password is
// never returned; callers rely on smtp_pass_set to know if one is stored.
func (s *SettingsService) AdminView() (map[string]any, error) {
	all, err := s.All()
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(all))
	for k, v := range all {
		// 敏感字段（SMTP 密码 / 各类密钥）不下发原值，只告知是否已设置
		if maskKeys[k] {
			out[k+"_set"] = v != ""
			continue
		}
		if jsonSettingKeys[k] {
			out[k] = decodeJSONSetting(v)
			continue
		}
		if k == SettingAllowRegistration {
			out[k] = v == "true"
			continue
		}
		out[k] = v
	}
	return out, nil
}

func copyMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func marshalValue(raw any) (string, error) {
	switch v := raw.(type) {
	case string:
		return v, nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case []any, map[string]any:
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	default:
		b, err := json.Marshal(raw)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}
