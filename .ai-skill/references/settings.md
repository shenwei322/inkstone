# 设置系统（站点配置）

所有站点级配置存在 `settings` 表（键值对），带 **30 秒内存缓存**。

定义在 `internal/service/settings_service.go`。

---

## 全部设置项

### 站点信息
| Key | 默认值 | 说明 |
|---|---|---|
| `site_name` | `InkStone` | 站点名称（导航栏/页脚/浏览器标题） |
| `site_description` | `InkStone — 现代化多用户博客系统` | 站点描述 |
| `site_logo` | `""` | Logo 图片 URL（空则显示首字母） |
| `site_favicon` | `""` | 浏览器图标 URL |
| `site_icp` | `""` | ICP 备案号（页脚显示） |

### 注册与外观
| Key | 默认值 | 说明 |
|---|---|---|
| `allow_registration` | `true` | 是否开放注册 |
| `site_wallpaper` | `""` | 全站壁纸 URL |
| `wallpaper_opacity` | `100` | 壁纸不透明度（5-100） |
| `wallpaper_blur` | `0` | 壁纸模糊 px（0-20） |
| `article_sidebar` | `true` | 文章页是否显示侧边栏 |
| `sidebar_position` | `right` | 侧边栏位置 `right` / `left` |

### 导航与侧边栏（JSON）
| Key | 默认值 | 结构 |
|---|---|---|
| `nav_menu` | `[]` | `[{label, url, icon?}]` |
| `sidebar_widgets` | `[]` | `[{type, title, content?, limit?, city?, avatar?, date?, eventName?, subtitle?, socials?: [{icon,url,label?}]}]` |

> ⚠️ `sidebar_widgets` 中 `type: "html"` 的 `content` 会被前台 `dangerouslySetInnerHTML`
> 注入到**每个访客页面**。`SettingsService.Update` 会调用 `sanitizeSidebarWidgets`
> → `SanitizeWidgetHTML`（bluemonday，白名单：基础排版 + a/img，不放行 class/id/iframe/script）。
> **新增任何写入该字段的路径都必须走同一消毒**，否则即为存储型 XSS。

### SMTP 邮件
| Key | 默认值 | 敏感 |
|---|---|---|
| `smtp_host` | `""` | |
| `smtp_port` | `465` | |
| `smtp_user` | `""` | |
| `smtp_pass` | `""` | ✅ 脱敏 |
| `smtp_from` | `""` | 发件人显示名 |

### 人机验证
| Key | 默认值 | 说明 |
|---|---|---|
| `captcha_provider` | `lap` | `lap`（Lap 工作量证明，**默认，内置实例开箱即用**）/ `pow`（自研工作量证明，零外部依赖）/ `geetest`（极验第四代） |
| `lap_enabled` | `false` | Lap 总开关（零配置可用；后台一键开启） |
| `lap_api_endpoint` | `""` | 留空 = 内置默认实例（lap-serverless.2465813064.workers.dev）；含 siteKey 且以 / 结尾，自托管时填自己的。**后台不展示**（DB/env 覆盖） |
| `lap_site_key` | `""` | 留空 = 内置默认实例的 site key。**后台不展示** |
| `lap_secret_key` | `""` | 留空 = 内置默认实例的 secret（✅ 脱敏）；也可用环境变量 `INKSTONE_LAP_SECRET` 覆盖。**后台不展示** |
| `lap_resolve_ip` | `""` | DNS 覆盖：后端访问 Lap 实例时拨号固定 IP（DNS 被污染填真实 IP，可 DoH/`doh.pub` 查询）。**后台不展示** |
| `lap_http_proxy` | `""` | HTTP 代理：本机整段被阻断（TUN 黑洞 CF 段）时后端经代理访问（形如 `http://127.0.0.1:7897`，生产留空）。**后台不展示** |
| `lap_on_login` | `false` | 登录需人机验证 |
| `lap_on_register` | `false` | 注册需人机验证 |
| `lap_on_comment` | `false` | 评论/发表/友链申请需人机验证 |
| `geetest_enabled` | `false` | 极验总开关（provider=geetest 时生效） |
| `geetest_captcha_id` | `""` | 极验验证 ID（前台初始化用，非敏感） |
| `geetest_captcha_key` | `""` | 极验密钥（✅ 脱敏，仅服务端二次验证用） |
| `geetest_on_login` | `false` | 登录需人机验证 |
| `geetest_on_register` | `false` | 注册需人机验证 |
| `geetest_on_comment` | `false` | 评论/发表/友链申请需人机验证 |
| `pow_enabled` | `false` | POW 总开关（provider=pow 时生效；零外部依赖、无任何密钥） |
| `pow_on_login` | `false` | 登录需人机验证 |
| `pow_on_register` | `false` | 注册需人机验证 |
| `pow_on_comment` | `false` | 评论/发表/友链申请需人机验证 |
| `pow_difficulty` | `4` | 难度：答案哈希前导零个数（1-6；4 ≈ 1-3 秒（v2 默认参数下），6 需数十秒） |
| `pow_ttl_minutes` | `10` | 挑战有效期（分钟，1-60；过期未提交需重新领取） |
| `pow_memory_mb` | `8` | 本地内存表大小 MB（1-32）：每次验证在访客浏览器构建并随机访问，越大越拖慢算力集群并行 |
| `pow_rounds` | `4` | 表查找-混合轮数（1-16）：每轮一次随机查表 + 一次 SHA-256，线性拉高单次成本 |
| `pow_min_events` | `3` | 需采集的本地交互事件数（鼠标/触摸/按键，0-10；0=关闭该检查退化为纯算法） |
| `lap_enabled` | `false` | Lap 总开关（provider=lap 时生效） |
| `lap_api_endpoint` | `""` | Lap 实例地址（**含 siteKey 且以 / 结尾**，形如 `https://xxx.workers.dev/SITEKEY/`） |
| `lap_site_key` | `""` | Lap site key（前台 widget 初始化用，非敏感） |
| `lap_secret_key` | `""` | Lap secret（✅ 脱敏，仅服务端 siteverify 二次验证用） |
| `lap_resolve_ip` | `""` | DNS 覆盖：后端访问 Lap 实例时拨号固定 IP（DNS 被污染填真实 IP，可 DoH/`doh.pub` 查询） |
| `lap_http_proxy` | `""` | HTTP 代理：本机整段被阻断（TUN 黑洞 CF 段）时后端经代理访问（形如 `http://127.0.0.1:7897`，生产留空） |
| `lap_on_login` | `false` | 登录需人机验证 |
| `lap_on_register` | `false` | 注册需人机验证 |
| `lap_on_comment` | `false` | 评论/发表/友链申请需人机验证 |

### 邮箱验证码
| Key | 默认值 | 说明 |
|---|---|---|
| `email_code_on_register` | `false` | 注册需邮箱验证码 |
| `email_code_on_login` | `false` | 登录需邮箱验证码 |
| `email_code_ttl_minutes` | `10` | 验证码有效期（分钟） |

### 安全防护
| Key | 默认值 | 说明 |
|---|---|---|
| `security_enabled` | `true` | 限流总开关 |
| `security_login_max` | `30` | 登录上限（次/15 分钟/IP） |
| `security_register_max` | `20` | 注册上限（次/小时/IP） |
| `security_comment_max` | `30` | 评论/发文上限（次/10 分钟/IP） |
| `security_api_max` | `600` | API 上限（次/分钟/IP） |
| `security_block_minutes` | `15` | 封禁时长（保留参数） |

### 文件管理
| Key | 默认值 | 说明 |
|---|---|---|
| `upload_max_mb` | `50` | 最大上传大小（MB） |
| `upload_speed_kb` | `0` | 上传限速 KB/s（0=不限） |
| `download_speed_kb` | `0` | 下载限速 KB/s（0=不限） |

### 友链自助申请（Beta1.19）
| Key | 默认值 | 说明 |
|---|---|---|
| `friend_apply_enabled` | `true` | 前台「友情链接」页开放自助申请表单（关闭后仅后台手动添加） |
| `friend_links_title` | `友情链接` | 前台「友情链接」页标题（**后台友链管理页顶部的「页面显示内容」编辑**，/site-config 下发） |
| `friend_links_intro` | `""` | 前台「友情链接」页介绍文案（空 = 不显示介绍段，上限 200 字） |

### 人机验证默认开启说明
`lap_enabled` 与 `lap_on_login` 默认值均为 `true`：新部署零配置开箱即用（内置默认 Lap 实例），**管理员登录后台即带人机验证**。想关闭：后台「安全防护」页取消勾选，不要改代码默认值。

**POW（自研）默认 `pow_enabled=false`**： Lap 依赖 workers.dev（服务器不通外网/不能用代理时不可达），POW 零外部依赖但需要显式开启。服务器网络受限时推荐 `captcha_provider=pow` + `pow_enabled=true`（后台「安全防护」页三选一卡片 + 场景开关 + 难度/TTL）。

---

## 三个关键机制

### 1. 敏感字段脱敏（`maskKeys`）

```go
var maskKeys = map[string]bool{
    SettingSMTPPass:          true,
    SettingGeetestCaptchaKey: true,
    SettingLapSecretKey:      true,
}
```

**效果**：
- `AdminView()` 返回 `xxx_set: bool`（是否已配置），**不下发明文**
- `Public()`（`/site-config`）**完全排除**这些字段
- 前端用 `<SecretInput isSet={form.xxx_set}>` 展示

### 2. 空值保护（重要）

`Update()` 对 `maskKeys` 字段做特殊处理：

```go
if maskKeys[key] && strings.TrimSpace(value) == "" {
    continue  // 空值 = 保持原值，不覆盖
}
```

**目的**：用户在后台清空密钥输入框点保存时，不会误删已配置的密钥。

### 3. JSON 字段自动编解码（`jsonSettingKeys`）

```go
var jsonSettingKeys = map[string]bool{
    SettingNavMenu:        true,
    SettingSidebarWidgets: true,
}
```

存储时序列化为字符串，`Public()`/`AdminView()` 时自动 `json.Unmarshal` 为数组。

---

## Service API

```go
All()                          // map[string]string，含默认值
Get(key) (string, error)
IntValue(key, fallback) int    // 数字读取
BoolValue(key, fallback) bool  // 布尔读取（"true"/"false"）
AllowRegistration() bool
Update(map[string]any) error   // 更新 + 失效缓存
Public() (map[string]any, error)     // 前端配置（排除 maskKeys）
AdminView() (map[string]any, error)  // 管理视图（maskKeys → xxx_set）
```

**缓存**：`All()` 结果缓存 30 秒；`Update()` 立即失效缓存（`cacheTime = time.Time{}`）。

---

## 新增设置项的完整流程

**1. 后端定义**（`settings_service.go`）
```go
// ① 加常量
const SettingFooBar = "foo_bar"

// ② 加默认值
var settingDefaults = map[string]string{
    SettingFooBar: "default-value",
}

// ③ 敏感字段加进 maskKeys（可选）
// ④ JSON 字段加进 jsonSettingKeys（可选）
```

**2. 公开下发**（`Public()`）
- 非 maskKeys 字段**自动**包含，无需改动
- 如需特殊处理（如布尔转换），在循环里加 `if k == SettingFooBar { ... }`

**3. 前端解构**（`site-config-context.tsx`）**必须手动加**
```tsx
interface RawSiteConfig {
    foo_bar?: string  // 加字段
}
// payload 构造里：
fooBar: cfg.foo_bar ?? 'default-value',
```

**4. 管理页保存**（`admin/settings/page.tsx`）**必须手动加进 payload 白名单**
```tsx
const payload: Record<string, unknown> = {
    // ...
    foo_bar: extra.foo_bar ?? 'default-value',  // 不加这行保存无效！
}
```

**5.（可选）安全防护页**
如果属于安全类设置，加到 `app/admin/security/page.tsx` 的 `SecurityForm` 接口和保存逻辑。

---

## 常见调试

```bash
# 查看当前设置
docker exec blog-postgres psql -U blog -d blog_platform -t -c \
  "SELECT key, LEFT(value, 40) FROM settings ORDER BY key;"

# 手动改设置（立即生效，需等缓存 30 秒或重启）
docker exec blog-postgres psql -U blog -d blog_platform -c \
  "UPDATE settings SET value='false' WHERE key='allow_registration';"

# 通过 API 查看（公开配置）
curl http://localhost:8080/api/v1/site-config

# 通过 API 查看（管理视图，需 token）
curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/admin/settings
```

## 环境变量应急开关

| 变量 | 作用 |
|---|---|
| `INKSTONE_DISABLE_CAPTCHA=1` | 全局停用所有验证码（应急，防锁死） |
