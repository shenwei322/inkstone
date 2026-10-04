---
name: inkstone-blog-platform
description: InkStone（砚台）多用户博客平台全栈开发指南。当需要在此代码库中开发、修改、调试功能时使用——包括后端 Go/Gin/GORM API、前端 Next.js 页面与组件、数据库模型、设置系统、认证授权、人机验证、主题外观、统计监控等。适用于新增功能、修复 bug、理解现有架构、扩展接口。
---

# InkStone 博客平台开发指南

Go + Gin + GORM + PostgreSQL 后端，Next.js 15 + React 19 前端，Docker 部署。

## 关键事实（先读这个）

| 项目 | 值 |
|---|---|
| 模块名 | `github.com/shenwei/inkstone/backend` |
| 当前版本 | `Beta1.27`（`AppVersion` 常量为准，见 `internal/service/system_service.go`） |
| 后端端口 | `8080`（API 前缀 `/api/v1`） |
| 前端端口 | `3000`（Next.js App Router，访客站） |
| 管理后台 | 博客站内 `/admin`（`frontend/app/admin`，含回收站与备份恢复） |
| 数据库 | PostgreSQL 16（GORM AutoMigrate 自动建表） |
| 认证 | JWT 双令牌（access 15min / refresh 7d）+ **TokenVersion 代次**（改密码/封禁/改角色即作废旧令牌） |
| 角色 | `admin` / `user`（RBAC 中间件） |
| 用户状态 | `active` / `banned` |
| 文章状态 | `draft` / `published` / `scheduled`（定时发布，到点由 `ScheduledPublisher` 每分钟扫描转 published） |
| 评论状态 | `pending` / `approved` / `rejected`（默认 `approved`；`pending` 由后台 `comment_audit` 或敏感词命中触发） |
| 敏感字段 | SMTP 密码、验证码密钥（API 只返回 `xxx_set` 布尔值，不下发明文） |
| 软删除 | Article / Page 有 `gorm.DeletedAt`。删除只置 `deleted_at` **不清关联**（还原时评论点赞还在）；彻底清关联只发生在 `purge` |
| 文章增强 | 摘要（`excerpt` 空则自动生成）、置顶（`is_pinned`，热门榜不掺）、访问密码（`view_password` 存 bcrypt，`json:"-"`）、定时发布（`scheduled_at`）、历史版本（`article_revisions`，上限 50 版） |
| 缓存策略 | `middleware.CachePolicy()`：默认 `private, no-cache`；`/uploads/*` 长缓存 immutable；文章/分类/标签/feed/sitemap/site-config `max-age=60` + stale-while-revalidate；≥400 一律 `no-store` |
| 在线更新 | 后台「系统更新」。两套源（`UPDATE_SOURCE`）：默认 `commits`（检查上游提交 → 下载源码镜像包 → 校验 → 备份 → 替换源码 → 宿主代理重建）；`releases`（GitHub Releases：tag 版本号 + 发布说明 + 镜像包资产 → 宿主代理 `docker load` + compose 重建）。代码在 `internal/service/update_*.go`（`update_release.go`  Releases 源）+ `deploy/update-agent.sh` / `.ps1` |
| 人机验证 | provider 三选一（`captcha_provider`）：`lap`（默认，Cap 的 CF Workers 分支）/ `pow`（自研工作量证明 v2：内存表+本地交互信号，零外部依赖，服务器不通外网/不能用代理也能用）/ `geetest`（极验四代）；场景与强度参数在后台「安全防护」页 |
| Markdown 渲染 | 编辑前端 `marked` → 保存 HTML（后端 bluemonday 消毒）；预览高亮用 `highlight.js/lib/common`（主题在 `globals.css`，仅前端 DOM 后处理，不入库） |
| 动画库 | GSAP 3.15（framer-motion 已彻底移除）；`components/motion.tsx` 封装 + `route-loader.tsx` 路由进度条 + `page-loader.tsx` 加载动画，**无 `.skeleton` 骨架图** |

## 目录导航

```
backend/
  cmd/server/main.go              # 入口：依赖注入 + 全部路由注册
  internal/handler/               # HTTP 层：参数绑定、调用 service、错误映射
  internal/handler/log_record.go  # recordOp：操作日志辅助（自动带当前用户/IP/UA）
  internal/service/               # 业务逻辑层
  internal/service/update_*.go    # 在线更新：检查/下载/解压/替换/回滚/重建（见 backend.md）
  internal/repository/            # GORM 数据访问层
  internal/middleware/            # Auth / CORS / 限流 / 安全头 / 流量统计
  internal/model/                 # 数据模型（GORM 结构体）
  pkg/config/                     # 环境变量配置
  pkg/mailer/                     # SMTP 发信
frontend/                         # 访客站（Next.js App Router）
  app/                            # Next.js 路由（页面）；app/admin/ = 站内管理后台（21 页）
  components/                     # 可复用组件
  lib/api.ts                      # 类型化 API 客户端（唯一 API 入口）
  lib/types.ts                    # 全部 TypeScript 类型
  lib/auth-context.tsx            # 认证上下文
  lib/ui.ts                       # 共享 UI 常量（inputClass、formatSize）
  components/site-config-context.tsx  # 站点配置全局上下文
  components/motion.tsx             # GSAP 动画封装（PageTransition/Reveal/Stagger/Presence/hoverTapScale 等）
  components/route-loader.tsx       # 路由切换 GSAP 顶部进度条（挂在根 layout）
  components/page-loader.tsx        # PageLoading / RowLoading / Spinner 加载动画（替代骨架图）
deploy/
  update-agent.sh / .ps1          # 宿主更新代理：读待更新清单 → 重建 → 重启 → 回报结果
  scripts/rebuild.sh / .ps1       # 本机（二进制/systemd）部署的重建脚本，由后端自动拉起
  nginx/inkstone.conf             # 生产 Nginx 配置（改完必须同步服务器）
```

**详细文档：**
- 后端架构与分层约定 → `references/backend.md`
- 所有 API 接口清单 → `references/api.md`
- 前端组件与页面说明 → `references/frontend.md`
- 数据模型与数据库 → `references/data-models.md`
- 设置系统（站点配置项）→ `references/settings.md`
- 部署与运维 → `references/deployment.md`

## 开发铁律（必须遵守）

### 1. 后端严格分层，不跨层调用
```
handler → service → repository → model
```
- handler 只做**参数绑定 + 调用 service + 响应**
- 业务校验、权限判断全部在 **service**
- 数据库操作只在 **repository**
- 不要在 handler 里直接写 SQL/GORM 查询（唯一例外见 `internal/handler/admin_handler.go` 中已有的少量直接 repo 调用，属于历史遗留，新代码请走 service）

### 2. 错误处理约定
- 定义领域错误（如 `ErrForbidden`、`ErrNotFound`、`ErrUserBanned`）
- 用 `errorResponse(c, err)` 统一映射为 HTTP 响应（`internal/handler/errors.go`）
- 校验错误用 `NewValidationError("中文提示")` → 自动 400
- **所有面向用户的错误信息用中文**

### 3. 常用 handler 辅助函数
```go
parseUintParam(c, "id", "无效的 ID")   // 解析路径参数，失败自动返回 400
parseIntQuery(c, "page", 1)            // 解析查询参数带默认值
errorResponse(c, err)                  // 统一错误响应
```

### 4. 新增接口的标准流程
1. `internal/model/` 定义模型（如需新表，**必须加进 `db.go` 的 AutoMigrate 列表**）
2. `internal/repository/xxx_repo.go` 写数据访问
3. `internal/service/xxx_service.go` 写业务逻辑
4. `internal/handler/xxx_handler.go` 写 HTTP 处理
5. `cmd/server/main.go` 装配依赖 + 注册路由
6. 前端 `lib/api.ts` 加 API 函数，`lib/types.ts` 加类型

### 5. 敏感字段必须脱敏
新增密钥类设置项时：
1. 在 `settings_service.go` 的 `maskKeys` map 里登记
2. API 会自动返回 `xxx_set: bool`（表示是否已配置）
3. 前端用 `<SecretInput>` 组件展示（小眼睛切换 + 已保存显示 `*`）

### 6. 敏感设置的「空值保护」
`settings_service.Update()` 对 `maskKeys` 中的字段做了特殊处理：**提交空字符串 = 保持原值不变**（不会误删密钥）。新增敏感字段无需额外处理，登记 `maskKeys` 即可。

### 7. 改完代码必须同步更新 skill 文档
每次完成功能新增/bug 修复（跑通验证清单之外），**必须**同步更新文档：
- 本文件（`SKILL.md`）：关键事实、目录导航、开发约定、常见坑（踩到的新坑也要记录）
- 组件说明：`references/frontend.md`（新增/变更的组件、页面、状态管理逻辑）
- 按改动范围联动：`references/api.md`（接口）、`references/settings.md`（设置项）、`references/data-models.md`（表结构）、`references/backend.md`（服务/中间件）
- 全局 skill 目录与仓库内副本 `.ai-skill/` **两份都要改**，内容保持一致

## 前端约定

### 全局状态用 Context，服务端数据用 React Query
- 站点配置 → `useSiteConfig()`（`site-config-context.tsx`）
- 认证状态 → `useAuth()`（`auth-context.tsx`）
- 通知/确认弹窗 → `useNotify()`（`toast.tsx`），**不要用 `alert`/`confirm`/`window.prompt`**
- API 数据 → `useQuery` / `useMutation`，queryKey 用数组如 `['articles', 'published', page]`

### 样式规范
- 全部用 Tailwind CSS，深色模式用 `dark:` 前缀
- 卡片统一：`rounded-2xl border border-border bg-card shadow-sm`
- 输入框统一用 `lib/ui.ts` 的 `inputClass`
- **正文增强样式挂 `.prose-reading`，不要写 `.prose xxx`**：`proseBody` 里的 `prose-reading` 标记类专用于「只影响文章正文、不影响后台编辑器预览」的样式（阅读宽度 74ch、h2/h3 竖线、首段导语、外链箭头）。编辑器预览（`markdown-editor` / `rich-editor` / `article-preview`）用的是裸 `prose max-w-none`，所以增强了 `.prose` 会让后台编辑区跟着变样
- `.prose-reading` 与 `max-w-none` specificity 相同（都是单类），靠源码顺序决定胜负：`globals.css` 的 `@import "tailwindcss"` 在最前，规则写在文件后段即生效
- 动画统一用 `components/motion.tsx` 的 GSAP 封装：`PageTransition` / `Reveal` / `StaggerList` / `StaggerItem` / `HoverLift` / `InView` / `Presence` / `hoverTapScale` / `hoverLift` / `useReveal` / `CountUp`；**framer-motion 已彻底移除，禁止再引入**
- 路由切换由根 layout 的 `RouteLoader`（GSAP 顶部进度条）接管；页面数据 loading 态统一 `components/page-loader.tsx` 的 `PageLoading` / `RowLoading` / `Spinner`，**全站不使用 `.skeleton` 骨架图**
- **React Compiler 规则**：组件体内不要写 `useCallback`（`preserve-manual-memoization` 会报 error），动画函数提到模块级

### 图标用 lucide-react
**注意**：`Github` 等品牌图标在新版 lucide 已移除，用 `GitBranch` 等替代。使用前确认图标存在。

## 环境与运行

```bash
# 数据库（Docker）
docker compose -f docker-compose.dev.yml up -d

# 后端
cd backend && go run ./cmd/server

# 前端（访客站）
cd frontend && npm run dev
```

**开发环境注意事项**：
- 本项目开发机为 Windows，Docker Desktop 需要手动启动（`C:\Users\Administrator\AppData\Local\Programs\DockerDesktop\Docker Desktop.exe`）
- 若后端启动报 `failed to connect database`，通常是 Postgres 容器没运行
- Go 依赖下载需设置 `GOPROXY=https://goproxy.cn,direct`（本机已设置）

## 验证清单（改完代码必做）

```bash
# 后端
cd backend && gofmt -w . && go vet ./... && go build -tags timetzdata -o server.exe ./cmd/server
# timetzdata 不是可选项：Windows 上不带它，time.LoadLocation("Asia/Shanghai")
# 报 "unknown time zone Asia/Shanghai"，后端连库 DSN 解析直接失败起不来

# 前端
cd frontend && npm run build && npx eslint app components lib --ext .ts,.tsx
```

4. **文档同步**：按「开发铁律 §7」更新 SKILL.md（含仓库 `.ai-skill/` 副本）与 `references/frontend.md` 等组件说明。

以上四项全绿才算完成。ESLint 必须 0 错误 0 警告。

## 常见坑（踩过的）

| 坑 | 说明 |
|---|---|
| PowerShell 内联中文会损坏文件 | 用 Edit/Write 工具改文件，**不要用 PowerShell 字符串替换处理中文** |
| 前端新增 site-config 字段要双层透传 | `settings_service.Public()` 下发 + `site-config-context.tsx` 解构，缺一不可 |
| 设置保存前端要显式带上字段 | `frontend/app/admin/settings/page.tsx` 的 payload 是白名单，新增设置项必须手动加入 |
| 验证码配置无效应放行 | 见 `captcha_service.go` / `geetest_service.go` / `lap_service.go` / `pow_service.go`：未配置密钥/服务不可达时 `return nil`（避免锁死用户） |
| rAF gate 句柄禁用 useRef | 滚动监听「`if (raf) return; raf = requestAnimationFrame(...)`」的 gate 句柄必须是 effect 内**局部变量**（`article-toc.tsx` / `back-to-top.tsx` 同款）。用 `useRef` 时 StrictMode 先 cleanup（`cancelAnimationFrame`）再重放 effect，useRef 残留非零 id 让 gate 永久关闭 → setState 永不执行（症状：回顶按钮/目录高亮打死不出现，且无任何报错）。BackToTop 真实踩过：2026-10 修复 |
| Lap 同源代理与两段式流程 | 访客端全走后端代理 `/api/v1/lap/*`（widget.js/wasm/challenge/redeem 白名单转发），浏览器零接触 workers.dev；后端两级网络兜底 `lap_resolve_ip`（DNS 污染固定 IP）/ `lap_http_proxy`（TUN 黑洞 CF 段）。代理必须透传浏览器 UA（Go 默认 UA 被 CF 403）。Cap widget 的 PoW 是**静默 speculative**（mousemove/touchstart/keydown 触发，redeem 后不派发事件），**必须用户真实点击**（mousedown）才 solve——e2e 漏点击会永远卡 initial 且无报错。**Transport 必须 `DisableKeepAlives` + `LapDo` 重试一次**：本地代理（clash）切换节点后缓存坏连接 EOF 而 POST 不重试 → 持续 502 |
| POW（自研工作量证明）provider | `pow_service.go` 挑战存内存池（上限 1 万、一次性消费 + TTL、签发 30 次/分钟/IP 限流、参数签发时快照）。**v2 消耗用户本地资源**：8MB xorshift128 内存表（`pow_memory_mb`）+ 多轮「查表-混合」SHA-256（`pow_rounds`）+ 真人交互信号（`pow_min_events` 次鼠标/触摸/按键，时间窗校验）。前端 `lib/sha256.ts`（纯 JS）+ `lib/pow.ts`（crypto.subtle 优先、纯 JS 回退），难度 1-6 默认 4（v2 默认参数 ≈1-3s）。**challenge 存内存 = 仅单实例有效**（单 backend 容器无碍；多副本需共享存储）。服务器不通外网/不能用代理时选它 |
| Lap 零配置开箱即用 | `captcha_provider` 默认 `lap`；`lap_defaults.go` 内置默认实例（endpoint/siteKey/secret），DB 字段留空即回退内置值（`LapEffectiveConfig`），自托管在后台覆盖或设 `INKSTONE_LAP_SECRET`。场景开关默认关，后台一键开启。后台保存时 lap_secret_key 空值 = 保持原值（maskKeys 语义）→ **不会被内置默认悄悄顶掉**。**数据安全三层**：① 后台 security 页不渲染 5 个技术字段且 save payload 显式剔除（防误清空）② 后端 `lapHiddenKeys` 对 4 个技术键做空值保护（API 直调空串也保持原值）③ 配置丢失回退内置默认实例，验证码不会被打挂。代理端点做路径-方法配对（静态 GET / 交互 POST），畸形请求 404 |
| 验证码 provider 互斥 | `useCaptcha` 门面（`components/captcha.tsx`）按 `captcha_provider` 分发；`useGeetestCaptcha` 与 `useLapCaptcha` 两个 hook 都会被调用（React 规则），未选中的 `enabled=false` 不加载 gt4.js / widget.js。新增 provider 保持此模式 |
| lap-widget 一次性 | Lap 的 PoW widget 完成后内部状态为 done，**无法原地复位**；每次打开验证弹窗必须 `box.replaceChildren()` 重建 `lap-widget` 元素并重挂 `solve`/`error` 监听。widget.js 地址从 `lap_api_endpoint` 推导 origin + `/widget.js`，脚本按 URL 全局缓存，开启时即预加载 |
| Lap siteverify 不走 siteKey 路径 | 后端 siteverify URL 由 `lap_api_endpoint` 推导为 `origin + /siteverify`（文档终点列表写 `/:siteKey/siteverify` 是误导）；响应 `{success, error}`，"Invalid site key or secret" = 后台配置问题，按极验 `status:error` 同口径放行 |
| GT4 弹窗必须真人点入口 | 极验 v4 无法用 `showBox()`/synthetic click 程序代弹验证面板；必须渲染可见的入口按钮（`geetest_btn_click`）让用户真实点击。入口容器要在视口内且不裁剪。init 需显式 `product: 'popup'` |
| GT4 freeze_wait 卡死 | 点击入口后 class 停在 `geetest_boxShow geetest_freeze_wait`：多为极验后台该 captchaId 的域名白名单未含 `localhost` 或产品类型非「行为验证4.0 弹出式」。用官方 demo ID（7e111794121d87ca0959954f89580e1a）对照可秒判是 ID 配置还是代码问题 |
| GT4 二次校验必传 captcha_id | 极验 v4 `/validate` 请求必须带 `captcha_id`（放 URL query），缺了返回 `-50101 not captcha_id`（status:error 结构，不是 result:fail）→ 前端验证已通过、后端必报不通过。签名是 `HMAC-SHA256(key=captcha_key, msg=lot_number)` |
| 登录限流被误伤 | 登录成功会重置限流计数，失败才累计 |
| 部署镜像需用国内源 | Dockerfile 用 `docker.m.daocloud.io`，Go 用 goproxy.cn，npm 用 npmmirror |
| alpine 缺 tzdata 连不上库 | 运行阶段 `apk add tzdata`，否则 DSN 的 `TimeZone=Asia/Shanghai` 报 `unknown time zone`，容器反复重启 |
| alpine apk 官方源被墙 | `dl-cdn.alpinelinux.org` Permission denied；`sed` 换 `mirrors.aliyun.com/alpine` 再 apk add |
| 部署时容器自重建 | `docker compose up -d` 会替换 backend 容器自身，进程日志可能中断，最终以 `docker ps` / 站点表现为准 |
| 发版必改 AppVersion | `internal/service/system_service.go` 的 `AppVersion` 必须同步改，否则后台「关于系统」显示的版本与实际镜像不符 |
| PowerShell 跑 .ps1 中文乱码/解析错 | PS 5.1 需要 **UTF-8 BOM** 才能解析中文；用 Write/Edit 工具写 .ps1 后要用 .NET 补 BOM：`[System.IO.File]::WriteAllText($p,$raw,(New-Object System.Text.UTF8Encoding($true)))` |
| PS 脚本 here-string 易碎 | `@"..."@` 内嵌 `$(if ... {...})`、反引号转义易触发 ParserError；输出优先用逐行 Write-Host，逻辑用简单字符串 Contains 代替复杂正则 |
| 线上 nginx conf 与仓库漏同步 | `deploy/nginx/inkstone.conf` 一直有 SEO location（`= /sitemap.xml`、`= /robots.txt`、`= /feed.xml` 精确匹配转 8080），但服务器 `/etc/nginx/conf.d/inkstone.conf` 是漏同步的旧版（缺三个 location），`/sitemap.xml` 落进 `location /` 被 Next 当页面路由 → 404。修复：仓库 conf base64 推服务器 → `nginx -t` → `nginx -s reload`。**改 nginx 先改仓库再同步服务器**；验证用外网 curl（服务器 curl 自己域名受阿里云 hairpin NAT 限制恒返回 000，别误判故障） |
| 前端表单防轮询覆盖 | 用 `initializedRef` 做「只初始化一次」，否则 30s 轮询的新对象引用会把用户编辑中的表单重置 |
| 服务器 curl 自己公网域名 000 | 阿里云 hairpin NAT 限制，**不是服务故障**；验证用外网客户端或本地 `curl -H Host:` |
| 任务列表需要 checkbox 白名单 | Markdown `- [x]` 渲染出 `<input type="checkbox">`，bluemonday 默认剥离；`sanitize.go` 已单独放行 `input[type=checkbox][checked][disabled]` |
| 编辑器 setState-in-effect | 项目 ESLint 开启 `react-hooks/set-state-in-effect`，effect 内直接 setState 会报错；把清理动作移到事件回调里 |
| JWT 签发参数变了 | `GeneratePair(userID, username, role, tokenVersion)`：Claims 有 `uname` 与 `ver` 声明；改 `Auth` 中间件或签发逻辑时，`Register`/`Login`/`Refresh` 三处调用要同步 |
| 误以为角色来自令牌 | **角色以数据库为准**：`UserChecker` 返回 `middleware.UserStatus{Username, Role}`，`Auth`/`OptionalAuth` 都用它覆盖 claims。`RequireRole` 比对的是覆盖后的值——降级/提升立即生效。不要改回「只回用户名」的旧签名 |
| 令牌撤销 | `model.User.TokenVersion` 是计数器：改密码（`UpdatePasswordHashAndRevoke`）、管理员重置密码、封禁、改角色都会 `+1`；`Refresh` 比对 `claims.TokenVersionOf() != user.TokenVersion` 即拒绝。旧令牌无 `ver` 字段视为 0，与初始值一致，升级不会误伤 |
| 友链检测 SSRF | 探测客户端 `probeClient` 的 `DialContext` 会拒绝回环/私网/链路本地/CGNAT/保留地址（含 `169.254.169.254`），且 `Proxy: nil`。改 `link_service.go` 的传输层时不要绕过；`RejectPrivateHost` 是第一道提示性拦截，**不替代** DialContext（防 DNS rebinding） |
| 侧边栏自定义 HTML | `sidebar_widgets` 里 `type: "html"` 的 `content` 会 innerHTML 注入到每个访客页面，**写入时必须过 `SanitizeWidgetHTML`**（在 `SettingsService.Update` 内自动处理，测试见 `security_test.go`） |
| 操作日志禁止记录值 | 设置更新等日志只记 key 名列表（`settingDetail`），**绝不能把 SMTP 密码/验证码密钥等 value 写进日志** |
| CSV 导出必须 BOM | `c.Writer` 先写 `0xEF 0xBB 0xBF` 再写 csv，否则 Excel 打开中文乱码；`csv.Writer.UseCRLF=true` |
| blob 下载不能走 api() | `api()` 客户端只会 `res.json()`；文件下载要单独 `fetch + Bearer`（401 刷新重试）+ `URL.createObjectURL` |
| fixed 弹窗别内联在带动画的容器里 | 调用方外层（如友链申请表单）常是带 `transform` 动画的容器 div，内联 `fixed inset-0` 会被 transform 包含块困住，遮罩只覆盖卡片区域 → 人机验证「只有提交窗口模糊」。必须 `createPortal` 到 `document.body`（与 `modal.tsx` 一致）；portal 的 mounted 判断用 `useSyncExternalStore`（SSR 首帧 false），避免 hydration mismatch 与 effect 内 setState |
| React Compiler 禁 useCallback | `react-hooks/preserve-manual-memoization`：组件体内 `useCallback` 直接 error。RouteLoader 的做法：start/finish 提到模块级，effect 内重建 refs 对象调用 |
| Presence 延迟卸载 | 替代 AnimatePresence：渲染期 `if (show && !mounted) setMounted(true)` 同步挂载；退场播 GSAP，`onComplete` 里 setMounted(false)。setState 只允许出现在渲染期同步分支与异步回调，effect 体内同步 setState 会触发 `react-hooks/set-state-in-effect` |
| confirm 弹窗内容快照 | toast 的 confirm 用 `dialogShown` state 缓存内容：`closeConfirm` 只清 `confirmState`，Presence 退场期间 `dialogShown` 保留，内容不闪空；且 `show={!!confirmState}` 会丢 TS 收窄，读取一律 `dialogShown?.xxx` |
| GSAP 入场动画被 rAF 节流卡透明 | framer-motion 的 opacity/transform 走 WAAPI（合成器线程），GSAP 是 JS tween 逐帧（rAF）。**远程桌面/浏览器窗口被遮挡时 Chrome 节流 rAF**，`gsap.from({opacity:0})` 会永久停在透明态 → "内容被白色遮挡"（首页/管理表格都踩过）。`components/motion.tsx` 已内置双保险：①全局 patch `gsap.from/fromTo`，凡从透明态起步的 tween 自动注册超时看门狗（1.2s 后强制 clearProps 恢复可见）；②各入场组件自带 `revealFallback` + `clearProps`。**新写动画必须用 `Reveal/InView/useReveal/hoverTapScale`，不要手搓 `gsap.from(el, {opacity:0})`**；悬浮按钮（如 BackToTop）入场直接用 globals.css 的 `.animate-scale-in`（CSS keyframes 走合成器，连 1.2s 看门狗等待都没有），阈值切换还要做滞回（480 显示/320 隐藏）防临界闪烁 |
| toast 弹窗动画必须 CSS keyframes | `toast.tsx` 曾用 `gsap.from/to` 做 opacity 入场退场，rAF 节流时**卡在 opacity 0.32**——实色卡片半透明糊在右下角 = 「白雾遮挡」（2026-10 实测）。已改为 globals.css 的 `.toast-enter/.toast-leave`（keyframes 走合成器）+ `animationend` 移除 + 600ms 兜底。新增弹窗/浮层动画一律 CSS keyframes，勿再引入 gsap opacity |
| 列表行动画别写 ref 回调 | `<tr ref={(el) => { gsap.from(el, ...) }}>` 每次父组件重渲染都会重播动画（ref callback 每次渲染都重建）→ 反复闪烁。抽行子组件 + `useReveal(rowRef, ...)`（挂载只播一次），见 `app/admin/users/page.tsx` 的 `UserRow` |
| gin 同层通配符冲突 | `articles.GET("/:id")` 已存在时**不能**再注册 `articles.GET("/:slug/related")`——gin 不允许同层两个不同名通配段，**注册时直接 panic**（不是 404，是起不来）。按 slug 的二级路由一律挂 `/slug/:slug/...` 前缀 |
| `useSearchParams` 必须包 Suspense | 没有 Suspense 边界时 Next 构建期报 `useSearchParams() should be wrapped in a suspense boundary`，整个页面退化成客户端渲染，丢掉 SSR 的 SEO 首屏 |
| 渲染期禁调 `Date.now()` | `react-hooks/purity` 会拦（admin 用户列表判锁定真实踩过）。时间基准改用 `useQuery` 返回的 `dataUpdatedAt`——语义也更准：判定对应的是「这批数据显示时」的状态 |
| Next 16 的 `params` 是 Promise | 页面组件与 `generateMetadata` 都要 `await params`；`error.tsx` 的恢复回调 prop 叫 **`retry`** 不是 `reset`（写错 TS 不报错，点击时调用 undefined 直接抛） |
| `error.tsx` 的 reset 是旧 API | 唯一可靠来源是 `frontend/node_modules/next/dist/client/components/error-boundary.d.ts` |
| 根 `not-found.tsx` 已覆盖全应用 | 不需要启用 experimental `globalNotFound`。用 `global-not-found.tsx` 反而要自己包 `<html>/<body>`、自己引 globals.css，视觉会断裂 |
| Dockerfile 漏声明 ARG 会静默忽略 | `docker build --build-arg NEXT_PUBLIC_SITE_URL=x` 若 Dockerfile 里没写对应的 `ARG NEXT_PUBLIC_SITE_URL`，传入值被**静默丢弃**、仍用 ENV 默认值。改构建期变量必须同时改 Dockerfile |
| 原生 `Table().Joins()` 不过滤软删除 | GORM 只在走 `Model(&Article{})` 时才自动追加 `deleted_at IS NULL`。用 `db.Table("categories").Joins("LEFT JOIN articles ...")` 这类原生写法时，回收站里的文章仍会被统计进分类文章数 |
| `json:"-"` 字段要手动塞进响应 | `User.LockedUntil` 带 `json:"-"`，`/admin/users` 的 `ListUsers` 用显式 `gin.H` 白名单构造响应，不手写 `"locked_until"` 就不下发 → 前端「解锁」按钮永不显示（真实踩过，前端按契约写好了也看不到） |
| 树形结构用值切片要从深到浅挂 | `CategoryNode.Children` 是值切片，往父节点 `append` 会产生拷贝。先挂浅层会让后挂的深层子节点写进 map 里的父，而浅层那份拷贝已取走 → 子树丢失。必须按深度**从大到小**挂载 |
| `*uint` 外键别塞结构体指针 | `Comment.ParentID` 是 `*uint` 不是 `*Comment`。GORM 存的是 id，要用 `parentID := parent.ID; &parentID`，且 nil 父评论必须传 nil（不是 0，0 会指向不存在的记录） |
| POW 挑战必须落库 | challenge 跨请求存活，多副本下任意实例要能消费别的实例签发的挑战。用表 `pow_challenges`，代价可忽略（POW 本质就是故意慢） |
| POW 过期判定方向 | 必须写 `issued_at > now() - ttl`（签发时间足够新）。写成 `issued_at > now()` 是要求行存于未来，**实测 100% 取不到任何挑战** |
| POW 过期挑战也要删 | 过期条件写进 WHERE 的话，已过期的行谁都删不掉，只能等 2 分钟的 GC；提交过期挑战是零成本攻击面，表会无限堆积。要无条件删、再判过期 |
| `make_interval` 类型坑 | `make_interval()` 返回 interval，`timestamptz - interval` 在参数化查询下类型推导不成立，报 `operator does not exist: timestamptz > interval`（42883）。参数后加 `::timestamptz` |
| `DELETE ... RETURNING` 只能有一条 | 先 Exec 一条 DELETE 再 Raw 一条 RETURNING，第二条只会拿到零行。GORM 要用 `Raw().Scan()` 才拿得到被删的整行 |
| UPSERT 做「检查+写入」要保证原子 | 邮箱验证码的重发间隔若拆成两次查询，并发会同时通过检查、各发一封信。用 `INSERT ... ON CONFLICT DO UPDATE ... WHERE` 一条搞定，`RowsAffected=0` 即"被拒" |
| 一次性凭证必须无条件删 | 过期条件写进 DELETE 的 WHERE 时，过期的行谁都删不掉，还占着重发间隔。要无条件删、再判过期（POW 挑战与邮箱验证码都踩过） |
| 主键要含用途字段 | 邮箱验证码用 (email, purpose) 而非 email：同邮箱可能并行发起登录与重置密码，只按 email 会让后一枚顶掉前一枚 |
| repository 常量与 SQL 同层 | 尝试上限 `MaxEmailCodeAttempts` 由 SQL 判定，所以常量放 repository 而不是 service——避免两侧各持一份而漂移 |
| 跨包共用的测试 fake 要独立成包 | `_test.go` 的导出符号对其他包不可见。service 与 handler 都要用的 fake 放 `internal/service/powstoretest/` |
| `_test.go` 的导出符号不外泄 | 其他包的测试 import 不到本包 `_test.go` 里的函数。跨包共用的测试 fake 要放独立非测试包（如 `internal/service/powstoretest/`） |
| PowerShell 不能写含中文的文件 | `Set-Content`/`Out-File` 会把中文转成乱码并吞换行（实测写 Go 测试文件直接损坏）。**一律用 Edit/Write 工具**；`.ps1` 若必须产生，也要确认是 UTF-8 且有 BOM |
| SQL 语义必须连真实库验证 | `DELETE ... RETURNING`、`make_interval`、软删除过滤这些都不是 Go 单测能覆盖的。仓库带了 `INKSTONE_TEST_DSN` 的集成测试（未设置则跳过），改 SQL 前先跑一遍 |
| 快照/备份类接口放进「不失败」路径 | `RevisionService.Snapshot` 失败只 log 不返回 error：版本历史是增强功能，为它让文章保存失败是拿次要功能拖垮主要功能 |
| lucide 图标不吃 title | `<Pin title="x" />` 会 TS 报错：`Property 'title' does not exist`。tooltip 用 `aria-label`（全项目 24 处既有用法都是这个） |
| 摘要可能泄露加密正文 | `excerpt` 常由正文生成（前 120 字），等于文章开头。清加密内容时必须**连摘要一起清**——列表 `stripLockedContent`、卡片 `toArticleBrief` 都要挡 |
| 前端别重复算摘要 | 保存时后端已生成 `excerpt` 落库，列表页再剥一遍标签是重复劳动。且前端 `slice()` 按 UTF-16 码元，会劈坏 emoji；兜底也要按字符截断 |
| 复合主键表做 UPDATE 前先删冲突行 | `article_tags(article_id, tag_id)` 是复合主键。标签合并时直接把 `tag_id` 改成目标会撞 23505，必须先删「该文章已有目标标签」的重复行 |
| unknown 上不能取属性 | `res.json()` 返回 unknown，`typeof data.error === 'string'` 直接 TS2339。要先 `typeof data === 'object' && 'error' in data` 收窄 |
| 快照/备份类接口放进「不失败」路径 | `RevisionService.Snapshot` 失败只 log 不返回 error：版本历史是增强功能，为它让文章保存失败是拿次要功能拖垮主要功能 |
| 恢复不是覆盖而是再存一版 | `RevisionService.Restore` 先给当前内容留版再写旧内容。直接覆盖会让「恢复到第 2 版」这个操作本身不可逆 |
| 变量遮蔽包名 | `mailer := mailer.New(...)` 合法，但遮蔽后**无法再用 `mailer.Xxx`** 调该包其他函数。要么改名 `mailerSvc`，要么把新函数放包内实现 |
| GORM `Raw` 手写软删除条件 | `db.Raw(sql).Scan(...)` 没有模型上下文，**不会**自动追加 `deleted_at IS NULL`，必须显式写进 SQL |
| 评论/事件通知必须是旁路 | `mailer.CommentNotifier` 发送失败只 `log.Printf`，绝不返回 error——否则 SMTP 一挂评论就发不出去 |
| 动画卡顿四类源 | ①**`width` 动画**每帧 layout 重排（长页面/远程桌面 CPU 合成下卡死）——进度条一律 `origin-left` + `scaleX`；输入框 focus 展宽（如 navbar 搜索框 `w-44 → w-56`）不要写 `transition-all`，改 `transition-[border-color,box-shadow]` 瞬时展宽；②**循环动画空转**——`repeat:-1` 必须走 `createLoop`（页面隐藏自动 pause），多 ring（如 RowLoading 每行一环）合并为单环；③**blur(filter) 元素做 transform 动画**——每帧重绘模糊区域（CPU 合成下极重），装饰光斑只用 `opacity` 呼吸（侧栏倒计时光斑 `blur-xl` 已从 scale 改 opacity）；④**滚动 handler 逐帧查 DOM**——rAF 回调里逐项 `getElementById`+`getBoundingClientRect` 会反复强制同步 layout，headings 等 DOM 在 effect 内缓存（article-toc 已改）。长列表 stagger 总时长 cap 0.5s（`motion.tsx` 已内置） |
| render 阶段不能访问 ref | ESLint（react-hooks v6 新规则）报 `react-hooks/refs`：render 体、`useState` lazy initializer、`useRef(初始值)` 里读写 `xxxRef.current` 全部算违规。渲染期要用的派生数据存 `useState`（如编辑器的 baseline/恢复的本地草稿），ref 只用于事件 handler/effect 内的可变引用。DOM 派生数据（如文章目录）可用 `useMemo + DOMParser` 解析 props 里的 HTML 字符串，不触碰 ref |
| `prose` 类零效果 = Markdown 无样式 | 全站正文/编辑器预览依赖的 `prose` 来自 **@tailwindcss/typography 插件**；没装它（postcss.config 只有 @tailwindcss/postcss）时 `prose/prose-neutral/dark:prose-invert` 全部无效，表格退化成浏览器默认裸表。已在 `globals.css` 用 `@plugin "@tailwindcss/typography"` 引入，并定制 `.prose table` 框线/表头底色/斑马纹 |
| 改 `.prose` 会连累后台编辑器 | `globals.css` 里直接写 `.prose h2 {...}` 这类规则会**同时命中后台的编辑器预览**（`markdown-editor.tsx` / `rich-editor.tsx` / `article-preview.tsx` 用裸 `prose max-w-none`），编辑区样式会跟着变。文章正文专属的增强样式一律挂 `.prose-reading`（`proseBody` 里 `prose` 后紧跟的标记类），别再往全局 `.prose` 上加 |
| 运行时改 favicon 不生效 | React 19/Next 16 下用 querySelector/appendChild 改 `link[rel=icon]` 会被 metadata hoist 覆盖或清理。把 `<link rel="icon">` 渲染进组件树（`components/site-head.tsx`）交给 React 管理 |
| 后台改 favicon / 站点名前台不生效（2026-10 修，四个连环坑） | ①**`SiteHead` 必须渲染在 `SiteConfigProvider` 内层**——曾放在 `app/layout.tsx` 的 `</body>` 前而 `SiteConfigProvider` 在 `components/providers.tsx` 里，`useSiteConfig()` 拿到 `DEFAULT_CONFIG`（`siteFavicon: ''`），favicon 永远回退 `/icon.svg`。症状：head 里 `<link rel="icon" href="/icon.svg">` 而 DB 有值。现在 `<SiteHead />` 放在 `<Providers>` 内 `<MaintenanceGate>` 前。②**Next file convention 图标会与动态 link 抢占**——`app/icon.svg`、`app/favicon.ico` 会被 Next 自动注入静态 `<link rel="icon">`，与 hoist 的动态 link 并存（实测 head 出现 3 个 rel=icon，各浏览器取哪个不确定）。已删除这两个文件（**2026-11 曾复现回归**：文档写着已删但文件实际还在 git 跟踪里，导致上传 favicon 仍被抢占；改动后务必用构建产物自查 `rel="icon"` 只剩 1 个），默认图标走 `public/icon.svg`（SiteHead 的 `/icon.svg` 回退）。③**`<title>` 不要交给 React hoist，也不要用 `document.title =`**——React 19 的 hoist `<title>` 不复用 Next metadata 已注入的节点，head 出现多个 title 而 Chrome 只读第一个（静态 `InkStone`）；`document.title =` 赋值也会被 Next 的 metadata 管理覆盖（实测 navbar 已显示新站点名而 title 仍旧）。正解：`app/layout.tsx` 删掉静态 `export const metadata`，改用 `export async function generateMetadata()` fetch `${NEXT_PUBLIC_API_URL}/site-config` 取 `site_name`/`site_description`（`next: { revalidate: 30 }` 与后端 settings 30s 缓存对齐，try/catch 兜底不可 500）；副作用是全站从 Static/Dynamic 变 **ISR 30s**。④**改动最多 30s 才可见**：后端 `settings_service` 的 `loadValues` 有 30s 内存缓存（未命中才查库），排查"改了没生效"时先 `curl /api/v1/site-config` 确认接口返回值再怀疑前端。验证用 `msedge --headless=new --dump-dom --virtual-time-budget=15000` 抓 hydration 后的 head |
| 在线更新：容器内没有 docker 权限 | 后端能下载/校验/替换源码，但**不能重建自己**（容器里没有 docker socket、没有 systemd）。设计上把「重建 + 重启」交给宿主代理 `deploy/update-agent.sh`（或 `.ps1`）：后端写 `UPDATE_DIR/pending-update.json`，代理轮询后 `docker compose build && up -d`，再写回 `update-result.json` + `deployed-commit.json`。**别在 handler 里直接 exec docker**（模式是 `UPDATE_WAITING_AGENT=true`）；本机部署才走 `deploy/scripts/rebuild.sh` |
| 两套更新源：commits vs releases | `UPDATE_SOURCE` 决定协议。默认 `commits`：commits API 比提交 + codeload/`UPDATE_MIRROR` 源码包 → 替换源码 → 重建。`releases`：`/repos/{owner}/{name}/releases/latest` 取 `tag_name`（语义化，如 `v1.28.0`）+ `body`（发布说明）+ `assets` 里的镜像包（正则 `UPDATE_IMAGE_ASSET`，默认 `inkstone-images-.*\.tar$`）→ 下载到 `UPDATE_DIR/images/` → 清单 `kind=release_image` → 宿主代理 `docker load` + `compose up -d`（更新代理两个脚本都已支持）。版本比较在 `update_release.go` 的 `parseVersionValue`（兼容 `Beta1.27` 与 `v1.28.0`，预发布低于正式版），本地版本读 `deployed-version.json`（`versionPath()`），没有记录时回落 `AppVersion`。**releases 模式不走 `looksLikeHash`/commit 语义**：`Apply` 的目标是版本 tag，`pendingState.Kind=release_image`，回滚点是安装历史 `release-history.json` 里的旧镜像包（不是源码备份） |
| 发布镜像包 Release | 上游发版 = 打 Release（tag 版本号）+ 上传镜像包资产。打包用 `deploy/package-images.ps1`（Windows）/ `deploy/package-images.sh`（Linux）：构建 `inkstone-backend` / `inkstone-frontend` 的 `<版本>` 与 `latest` 两组 tag 并 `docker save` 成 `dist/inkstone-images-<版本>.tar`（附 `.sha256`）。tar 里同时含 `<版本>` 与 `latest` 两组 tag（回滚时旧版本 tag 才能留在镜像列表里，别只打 latest）。前端 `NEXT_PUBLIC_API_URL` 是**构建期注入**，用 `INKSTONE_PUBLIC_API_URL` / `-ApiUrl` 按部署域名传 |
| 更新别覆盖站点数据与密钥 | `update_swap.go` 的 `guardedPrefixes` + `protectedSegment` 保证 `data/`、`uploads/`、`files/`、`node_modules/`、`.git/`、`.update/`、`.next/`、`.tools/` 与任何层级的 `.env*` **永不被上游源码覆盖**。判定分两层：**顶层前缀匹配**（`guardedPrefixes`）保护 `uploads/`、`files/` 这类顶层目录；**路径段匹配**（`protectedSegment`）只列 `data`、`node_modules`、`.git`、`.update`、`.next`、`.tools`——**别把 `files`/`uploads` 加进段匹配**：仓库里真实存在 `frontend/app/admin/files/page.tsx`，加了会让上游对这个页面的改动被静默跳过（真实踩过）。新增受保护目录要同步三处（两个函数 + `skipSourceDir`）并在 `TestIsGuardedPath` 补用例 |
| 更新目录必须是绑定挂载 | 后端把待更新清单写在 `UPDATE_DIR`（默认 `/app/data/update`），宿主代理按 `INKSTONE_UPDATE_DIR` 轮询它。如果该目录落在**命名卷**里，宿主拿不到稳定路径 → 页面永远停在「等待宿主代理」。`docker-compose.prod.yml` 已加 `./data/update:/app/data/update` 绑定挂载，别删 |
| UpdateService 的 Mutex 不可重入 | `reportStage(func(st *UpdateStage){...})` 的闭包在**持锁**状态下执行：里面只能改 `st` 的字段，调用任何会加锁的方法（`Mode`/`rebuildMessage`/`snapshot`…）都会**死锁**。需要这类值先在闭包外算好（`rebuildHint := s.rebuildMessage()` 的写法就是踩坑后改的） |
| 已部署版本 vs 运行中版本 | `data/deployed-commit.json` 只说明「上一次部署到哪一版」，不代表进程已重启。判断更新是否生效要用 `GET /api/v1/system/info` 的 `commit`（优先级 `-ldflags BuildCommit` > 部署记录 > `INKSTONE_COMMIT`）。更新页的 `pending` 会在部署记录等于目标提交后自动收敛 |
| 更新后前端不生效 | 前端是**构建期**注入 `NEXT_PUBLIC_API_URL` 的产物：只替换源码不重新 build 前端镜像，页面仍是旧代码。宿主代理的 docker 模式已包含 `compose build`；手工更新别忘了 `--build` |
| 更新写入的文件属主是 root | 后端容器以 root 跑，写进 `./:/app/src` 的文件在宿主属主为 root，之后普通用户 `git pull`/`git status` 会报 dubious ownership。处理：`git config --global --add safe.directory <仓库>` + `sudo git`，或更新后 `chown -R` 回部署用户（详见 `references/deployment.md`） |
| 项目级 skill 只在 `.dsh/skills/` 一层生效 | DSH 的 `dsh-skill-filesystem` 自动扫描 `<项目根>/.dsh/skills/`（rank 100）与 `<项目根>/.agents/skills/`（rank 200），**无需注册、无需重启**，用 write/edit 改完即时生效。三条硬约束：①**只发现一层**——`<name>/SKILL.md` 或平铺 `<name>.md`，嵌套 `**/SKILL.md` 静默忽略；②frontmatter 的 `name` 必须是 kebab-case（`Bad_Name` 会被静默丢弃，无报错，只进日志）；③`description` 必填。想让规则**常驻生效**（而非等模型自己判断是否加载）时，把要点同时写进 `AGENTS.md`——skill 目录本身只提供「名字 + 截断描述」，靠模型判断该不该加载 |
