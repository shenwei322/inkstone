# API 接口清单

所有接口前缀 `/api/v1`。认证方式：请求头 `Authorization: Bearer <access_token>`。

## 全局中间件链

```
请求 → gin.Logger → gin.Recovery → SecurityHeaders → TrafficStats
     → CORS → [api/v1 分组] rateLimitMiddle（全局限流 600/分钟/IP）
     → 路由级中间件（Auth / RequireRole / 专属限流）
     → handler
```

| 中间件 | 作用域 | 说明 |
|---|---|---|
| `SecurityHeaders` | 全局 | X-Frame-Options 等安全响应头 |
| `TrafficStats` | 全局 | 异步记录流量到统计表 |
| `CORS` | 全局 | 白名单来自 `FRONTEND_URL` |
| `rateLimitMiddle` | `/api/v1` | 全局 API 限流，超限 429 |
| `registerLimit` | 注册/发验证码 | 注册专属限流（默认 20/小时） |
| `loginLimit` | 登录 | 登录专属限流（默认 30/15 分钟） |
| `articleLimit` | 创建文章 | 发文限流 |
| `commentLimit` | 发表评论 | 评论限流 |
| `OptionalAuth` | articles 分组 | 有 token 识别用户，无 token 匿名 |
| `Auth` | 需登录路由 | 强校验 + 实时查库校验封禁状态 |
| `RequireRole("admin")` | `/admin` 分组 | 管理员权限 |

## 鉴权说明

| 标记 | 含义 |
|---|---|
| 公开 | 无需登录 |
| 可选 | 带 token 则识别用户（OptionalAuth），否则匿名 |
| 登录 | 需要有效 access token |
| 管理员 | 需要 `role=admin` |

---

## 认证 `/auth`

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/auth/register` | 公开 | 注册。Body: `{email, username, password, captcha_token?, captcha_answer?, email_code?}` |
| POST | `/auth/login` | 公开 | 登录。Body: `{email, password, captcha_token?, captcha_answer?, email_code?}`。**email 字段可填邮箱或用户名** |
| POST | `/auth/refresh` | 公开 | 刷新令牌。Body: `{refresh_token}` |
| POST | `/auth/email-code` | 公开 | 发送邮箱验证码。Body: `{email, purpose: "register"\|"login"}` |
| GET | `/auth/me` | 登录 | 当前用户信息 |
| PUT | `/auth/password` | 登录 | 修改密码。Body: `{current_password, new_password}` |
| PUT | `/auth/profile` | 登录 | 修改用户名。Body: `{username}` |
| GET | `/auth/my-comments` | 登录 | 我的评论。Query: `?page=&page_size=` |

**注册/登录的验证码规则**：
- 人机验证按后台 `captcha_provider` + 场景开关决定是否必需；geetest 提交 `lot_number/captcha_output/pass_token/gen_time`，lap 提交 `lap_token`
- 邮箱验证码按后台开关决定是否必需
- 验证失败返回 400，提示中文原因

**登录限流**：失败次数超限返回 429；**成功登录会重置计数**。

---

## 文章 `/articles`

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/articles` | 可选 | 列表。Query: `?page=&page_size=&status=&category=&tag=&q=&order=` |
| GET | `/articles/:id` | 可选 | 详情（会自动 +1 浏览量） |
| GET | `/articles/slug/:slug` | 可选 | 按 slug 查详情（+1 浏览量） |
| GET | `/articles/:id/comments` | 公开 | 评论列表 |
| GET | `/articles/:id/reactions` | 可选 | 点赞/收藏统计（带 token 时返回用户是否已赞） |
| POST | `/articles` | 登录 | 创建。Body: `{title, content, status, category_id?, tags?, cover?, captcha_token?, captcha_answer?}` |
| PUT | `/articles/:id` | 登录 | 更新（仅作者）。同上字段均可选 |
| DELETE | `/articles/:id` | 登录 | 删除（仅作者） |
| POST | `/articles/:id/comments` | 登录 | 发表评论。Body: `{content, captcha_token?, captcha_answer?}` |
| POST | `/articles/:id/reactions` | 登录 | 点赞/收藏切换。Body: `{type: "like"\|"favorite"}` |

**关键行为**：
- `status=draft` 查询需要登录，且只返回自己的草稿
- `order=views` 按浏览量排序（用于热门文章）
- `category`/`tag` 传 **slug**（不是 id）
- 创建草稿（`status=draft`）**不触发人机验证**，发布才触发
- `cover` 留空时后端自动提取正文第一张图片的 URL

**评论删除**：`DELETE /comments/:id` — 评论作者或管理员可删

---

## 分类与标签

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/categories` | 公开 | 分类列表（含文章数） |
| GET | `/tags` | 公开 | 标签列表（含文章数） |
| POST | `/admin/tags` | 管理员 | 创建标签。Body: `{name}` |
| PUT | `/admin/tags/:id` | 管理员 | 重命名标签。Body: `{name}` |
| DELETE | `/admin/tags/:id` | 管理员 | 删除标签（同时清理文章关联） |

> 文章创建/更新时传 `tags: ["标签名"]` 会自动 find-or-create。

---

## 页面 `/pages`（WordPress 式独立页面）

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/pages` | 公开 | 已发布页面列表 |
| GET | `/pages/:slug` | 公开 | 页面详情 |
| GET | `/admin/pages` | 管理员 | 全部页面（含草稿） |
| POST | `/admin/pages` | 管理员 | 创建 |
| GET | `/admin/pages/:id` | 管理员 | 详情 |
| PUT | `/admin/pages/:id` | 管理员 | 更新 |
| DELETE | `/admin/pages/:id` | 管理员 | 删除 |

Body 字段：`{title, content, template, status, sort_order, show_in_nav}`

**模板（template）**：
- `default` — 常规页（居中内容）
- `fullwidth` — 通栏页（大标题居中）
- `landing` — 自定义模板（整页 HTML，`{{content}}` 为内容占位符）

---

## 友情链接 `/links`

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/links` | 公开 | 友链列表。**失效站点不返回 url，只给 masked_url** |
| GET | `/admin/links` | 管理员 | 全部（含检测状态、检测时间） |
| POST | `/admin/links` | 管理员 | 创建。Body: `{name, url, check_url?, icon_url?, description?, sort_order?}` |
| PUT | `/admin/links/:id` | 管理员 | 更新 |
| DELETE | `/admin/links/:id` | 管理员 | 删除 |
| POST | `/admin/links/check` | 管理员 | 检测全部 |
| POST | `/admin/links/:id/check` | 管理员 | 检测单个 |

**检测机制**：后台协程每 6 小时巡检，仅检测「超过 24 小时未检测」的链接。探测逻辑：HEAD 优先 → 失败回退 GET，`status < 500` 视为可达。失效链接会被**脱敏**（`MaskURL`）且前端禁止跳转。

---

## 文件管理 `/admin/files`

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/admin/files` | 管理员 | 列表。Query: `?page=&page_size=&q=` |
| POST | `/admin/files` | 管理员 | 上传（multipart，字段名 `file`） |
| GET | `/admin/files/:id/download` | 管理员 | 下载（带限速 + 中文文件名支持） |
| DELETE | `/admin/files/:id` | 管理员 | 删除（同时删磁盘文件） |

**静态访问**：上传的文件通过 `GET /files/<stored_name>` 直接访问（用于文章中引用）。

**图片上传**（编辑器用）：`POST /uploads`（登录即可，非管理员专用），返回 `{url, filename}`。

---

## 站点地图 `/admin/sitemap`

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/admin/sitemap` | 管理员 | 站点地图数据：分组 URL 列表 + robots.txt 预览 + 统计 |

返回结构：
```json
{
  "data": {
    "frontend_url": "https://blog.shenv.top",
    "sitemap_url": "https://blog.shenv.top/sitemap.xml",
    "robots": "User-agent: *\nDisallow: /admin\nDisallow: /me\nSitemap: ...\n",
    "groups": [
      { "type": "article", "label": "文章", "count": 12, "entries": [
        { "type": "article", "label": "文章标题", "loc": ".../posts/xxx", "lastmod": "2026-09-27T00:00:00+08:00", "changefreq": "weekly", "priority": "0.8" }
      ]}
    ],
    "total": 30
  }
}
```

> 数据来源与公开的 `GET /sitemap.xml` 完全一致（handler 内 `collectEntries()` 单一数据源），
> 分组顺序：基础页面/文章/独立页/分类/标签。

---

## 站点配置 `/site-config`

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/site-config` | 公开 | 前端全局配置（导航菜单、小工具、验证码配置、壁纸、站点名等） |

返回结构（部分）：
```json
{
  "site_name": "InkStone",
  "site_description": "...",
  "site_logo": "",
  "site_favicon": "",
  "site_icp": "",
  "allow_registration": true,
  "nav_menu": [{"label": "首页", "url": "/", "icon": "home"}],
  "sidebar_widgets": [{"type": "about", "title": "关于本站", "content": "..."}],
  "sidebar_position": "right",
  "site_wallpaper": "",
  "wallpaper_opacity": "100",
  "wallpaper_blur": "0",
  "article_sidebar": "true",
  "captcha": {
    "provider": "none",
    "site_key": "",
    "geetest_captcha_id": "",
    "on_register": false, "on_login": false, "on_comment": false, "on_article": false
  },
  "email_code": {"on_register": false, "on_login": false}
}
```

> **注意**：`captcha` 和 `email_code` 是嵌套对象，不是扁平的。

---

## 人机验证（POW 有独立签发端点，凭证随业务接口提交）

验证码凭证作为普通字段随登录/注册/评论/友链申请请求提交，由 handler 层的
CaptchaService 门面按 `captcha_provider` 设置校验；**POW 例外**——它需要一个
前置的 challenge 签发端点。

**支持 3 种提供方**：`geetest`（极验 v4）/ `lap`（Lap 工作量证明，Cap 的 Cloudflare Workers 分支）/ `pow`（自研工作量证明，零外部依赖）

**请求体凭证字段**（按 provider 只提交其一）：
- geetest：`lot_number` / `captcha_output` / `pass_token` / `gen_time`
- lap：`lap_token`（widget `solve` 事件产出的 `SITEKEY:ID:TOKEN`）
- pow：`pow_challenge` + `pow_nonce` + `pow_signal`（`pow_nonce` 是使 v2 算法（8MB 内存表 + 4 轮查表混合）输出摘要前 `difficulty` 位全为 0 的递增数字；`pow_signal` 是本地交互事件流 `m/k/t:unix_ms:x:y` 逗号连接，至少 `min_events` 条且在挑战签发时间窗内）

**`POST /pow/challenge`（公开，签发 POW 挑战）**：

| 项 | 值 |
|---|---|
| 方法/路径 | `POST /api/v1/pow/challenge` |
| 鉴权 | 无（验证发生在登录之前） |
| 限流 | 30 次/分钟/IP（`pow-challenge`） |
| 响应 | `{ "challenge": "64位hex", "difficulty": 4, "memory_mb": 8, "rounds": 4, "min_events": 3, "ttl_seconds": 600 }` |

参数为**签发时刻快照**（后台调整只影响之后的新挑战）。挑战一次性消费（校验通过即作废）+ TTL 过期失效，服务端内存存储（重启全部失效=用户重新验证一次，无正确性影响）；池上限 1 万，满时 503「服务繁忙」。

**`GET /site-config` 的人机验证字段**（`captcha_provider` + `geetest` / `lap` / `pow` 三套公开配置，均不含密钥——POW 本就没有密钥）：

```json
{
  "captcha_provider": "pow",
  "geetest": { "enabled": false, "on_login": false, "on_register": false, "on_comment": false, "captcha_id": "" },
  "lap": { "enabled": true, "on_login": true, "on_register": true, "on_comment": false, "site_key": "…", "api_endpoint": "https://xxx.workers.dev/SITEKEY/" },
  "pow": { "enabled": true, "on_login": true, "on_register": false, "on_comment": false, "difficulty": 4, "ttl_seconds": 600, "memory_mb": 8, "rounds": 4, "min_events": 3 }
}
```

前端按 `captcha_provider` 选用 `useGeetestCaptcha` / `useLapCaptcha` / `usePowCaptcha`（门面 `useCaptcha`）。
后端在「未配置密钥」或「服务不可达」时放行（避免锁死用户）。

---

## Lap 同源代理 `/lap`（公开）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/lap/widget.js`、`/lap/widget.compat.js`、`/lap/floating.js` | 转发到 Lap 实例同路径（Cache 5min） |
| GET | `/lap/wasm` | 转发到 jsdelivr 的 PoW WASM（Cache 5min） |
| POST | `/lap/{siteKey}/challenge`、`/lap/{siteKey}/redeem` | 转发到实例同路径（siteKey 须与配置一致，no-store） |

访客浏览器不直连 workers.dev（DNS 污染/超时），全部由后端代收；白名单外 404。
`siteverify` 不走代理（后端内部直连）。网络兜底设置见 settings.md 的
`lap_resolve_ip` / `lap_http_proxy`。

---
## 系统信息

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/system/info` | 公开 | 系统信息（名称、版本、Go 版本、运行时长、作者、**运行中提交 `commit` / `commit_at` / `commit_source`**） |
| GET | `/feed.xml` | 公开 | RSS 2.0 订阅源 |
| GET | `/healthz` | 公开 | 健康检查（Docker healthcheck 用） |

`commit_source` 取值：`ldflags`（编译期 `-X ...BuildCommit=` 注入，最准）> `deployed`（部署记录
`data/deployed-commit.json`）> `env`（`INKSTONE_COMMIT`）。三者皆无时 `commit` 为空串。

---

## 管理后台 `/admin`

### 统计

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/stats` | 概览计数（用户数/文章数/已发布/草稿） |
| GET | `/admin/stats/traffic?days=30` | 访问趋势（PV/UV + 流量字节，自动补零） |
| GET | `/admin/stats/resources` | 服务器 CPU/内存/协程/运行时长 |

### 用户管理

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/users?page=&page_size=&q=` | 用户列表（搜索邮箱/用户名） |
| POST | `/admin/users` | 创建用户。Body: `{email, username, password, role}` |
| PUT | `/admin/users/:id` | **修改邮箱/用户名/密码**。Body: `{email?, username?, password?}` |
| PUT | `/admin/users/:id/role` | 修改角色。Body: `{role}`（不能改自己） |
| PUT | `/admin/users/:id/status` | 封禁/解封。Body: `{status: "active"\|"banned"}`（不能封自己） |
| DELETE | `/admin/users/:id` | 删除用户（级联删文章，不能删自己） |

### 文章管理

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/articles?page=&page_size=&status=` | 全部文章（含草稿） |
| PUT | `/admin/articles/:id/status` | 上下架。Body: `{status}` |
| DELETE | `/admin/articles/:id` | 删除任意文章 |

### 评论管理

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/comments?page=&page_size=` | 全部评论（含所属文章） |
| DELETE | `/admin/comments/:id` | 删除评论 |

### 站点设置

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/settings` | 全部设置（敏感字段只返回 `xxx_set`：`smtp_pass_set`/`geetest_captcha_key_set`/`lap_secret_key_set`） |
| PUT | `/admin/settings` | 更新。Body: `{settings: {...}}`（**注意包装层**） |
| POST | `/admin/settings/test-mail` | 发送测试邮件。Body: `{to}` |

### 操作日志（Beta1.12 增强）

所有写操作（文章/用户/评论/设置/文件/友链/页面/标签/认证）均异步记录操作日志，**成功与失败都留痕**，保留最近 90 天。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/logs?page=&page_size=&category=&username=&q=&from=&to=&success=` | 分页查询。`from`/`to` 接受 `YYYY-MM-DD` 或 RFC3339；`success=true`/`false` 按结果筛选 |
| GET | `/admin/logs/overview` | 总览：`{total, today, failed, by_category}` |
| GET | `/admin/logs/stats` | 各分类日志数量（旧接口，保留兼容） |
| GET | `/admin/logs/export?category=&username=&q=&from=&to=&success=` | 按当前筛选条件导出 CSV（UTF-8 BOM，Excel 兼容，上限 5 万条） |

日志分类（category）：`auth` / `article` / `user` / `comment` / `setting` / `file` / `link` / `page` / `taxonomy` / `system` / `other`。

**导出示例**：
```bash
curl -H "Authorization: Bearer <token>" \
  "https://blog.example.com/api/v1/admin/logs/export?category=auth&from=2026-09-01" \
  -o logs.csv
```

### 系统更新（Beta1.27 新增）

后台「系统更新」页的全部接口。两套更新源（`UPDATE_SOURCE`，`update.source` 字段返回值）：

- **commits（默认）**：检查上游提交 → 下载源码镜像包 → SHA256 校验 → 安全解压 → 备份 → 原子替换源码 → 触发重建；
- **releases**：GitHub Releases（`tag_name` 版本号 + `body` 发布说明 + 镜像包资产）→ 下载镜像包 → 宿主代理 `docker load` + compose 重建（详见 `references/deployment.md`）。

所有操作归入 `system` 日志分类（成功与失败都留痕）。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/system/update` | 状态总览：`{update, backups, agent}`。**不联网**，前端可安全高频轮询 |
| POST | `/admin/system/update/check` | 联网检查上游最新版本（提交或 Release）并生成更新日志，返回最新 `update` |
| POST | `/admin/system/update/apply` | 启动一键更新。Body: `{commit?, target?, confirm:true}`（目标留空 = 上游最新；`target` 优先于 `commit`。commits 模式传 commit 哈希，releases 模式传版本 tag 如 `v1.28.0`）。成功返回 **202** + `{message, update}` |
| POST | `/admin/system/update/rollback` | 回滚。Body: `{backup_id, confirm:true}`（commits 模式为备份 ID；releases 模式为历史版本号） |

`update` 结构要点：

| 字段 | 说明 |
|---|---|
| `enabled` / `mode` / `message` | 是否可用 / 重建方式 / 面向管理员的说明 |
| `source` | `commits` / `releases`（决定下面所有版本字段的语义） |
| `mode` | `waiting_agent`（等宿主代理重建）/ `in_place`（本机编译重启）/ `docker` / `unavailable` |
| `version.current` | 运行中提交（含 `current_from` 来源） |
| `version.current_version` / `current_version_from` | 本地版本号（releases 模式；`version_file` / `app_version`） |
| `version.latest` | 上游最新提交（commits 模式）；releases 模式下 `hash` 填 tag |
| `version.latest_version` / `release` | 上游最新 Release 的版本号与详情（releases 模式：`tag/name/body/url/published_at/prerelease/draft/assets[]`） |
| `version.update_available` / `behind` / `compare_note` | 是否有更新 / 落后提交数（`-1` = 未知；releases 模式恒 `-1`）/ 判定说明 |
| `version.changelog` | 更新日志（commits 模式；releases 模式为空数组，发布说明在 `version.release.body`） |
| `version.pending` | 待生效更新。`kind=source`（源码已替换）或 `release_image`（镜像包已下载：`version/image_path/image_name/compose_file/bytes/sha256`） |
| `stage` | 实时进度：`phase`/`progress`/`message`/`error`/`files_changed`/`backup_id`/`waiting_agent` |
| `history` | 最近 20 条更新记录（含操作人、结果、备份 ID；releases 模式下 `from`/`to` 是版本号） |
| `backups` | commits 模式为源码备份点；releases 模式为安装历史里的镜像包（`id` = 版本号） |
| `agent` | 宿主更新代理写回的执行结果（无则 `null`） |

`stage.phase` 取值：`staging`（下载解压/下载镜像包）→ `swapping`（备份替换/镜像包就绪）→ `rebuilding` → `success` /
`failed` / `rolled_back`。前端在 `stage.running=true` 时以 2.5 秒间隔轮询本接口。

**设计约束**（改动时别破坏）：
- `GET` 状态接口绝不发外部请求，联网只在 `check` / `apply` 里发生；
- 下载地址只来自后端配置（`UPDATE_MIRROR` / 上游仓库 / Release 资产），**不接受请求体指定 URL**；
- 解压拒绝绝对路径、`..` 穿越、符号链接，并限制文件数与体积；
- 替换源码时**永不触碰** `data/`、`uploads/`、`files/`、`.env*`、`node_modules/`、`.git/`、`.update/`、`.next/`；
- 覆盖前逐文件备份到 `UPDATE_DIR/backups/<备份ID>/`，替换失败自动回滚；
- releases 模式的镜像包必须与 Release 资产声明的 `size` 对账，并按 SHA-256 校验（代理侧同样校验）。

---

## 统一响应格式

**成功**：直接返回数据对象，如 `{"article": {...}}`、`{"articles": [...], "total": 10}`

**失败**：`{"error": "中文错误信息"}`

**状态码**：
| 码 | 含义 |
|---|---|
| 200/201/204 | 成功 |
| 400 | 参数校验失败（`ValidationError`） |
| 401 | 未登录 / 令牌无效过期 |
| 403 | 无权限 / 账号被封禁 / 站点关闭注册 |
| 404 | 资源不存在 |
| 429 | 触发限流 |
| 500 | 服务器内部错误 |
