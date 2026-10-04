# 前端架构详解

Next.js（App Router）+ React 19 + TypeScript + Tailwind CSS v4 + React Query + GSAP + TipTap。

## 目录结构

```
frontend/app/                   # 访客站路由（App Router；含站内后台 app/admin/，19 页）
├── layout.tsx                # 根布局：Providers + Navbar + 壁纸 + Footer + SiteHead
├── page.tsx                  # 首页（文章列表 + 侧栏小工具）
├── login/page.tsx            # 登录
├── register/page.tsx         # 注册
├── me/page.tsx               # 用户中心（账户/我的文章/我的评论）
├── links/page.tsx            # 友情链接页
├── posts/[slug]/page.tsx     # 文章详情（正文 + 目录 + 分享 + 回顶）
├── p/[slug]/page.tsx         # 独立页面（3 种模板）
├── search/page.tsx           # 搜索页（?q=，独立 URL 便于 SEO）
├── archive/page.tsx          # 归档页（按年/月折叠）
├── categories/page.tsx       # 分类汇总
├── category/[slug]/page.tsx  # 分类详情
├── tags/page.tsx             # 标签云
├── tag/[slug]/page.tsx       # 标签详情
├── privacy/page.tsx          # 隐私政策
├── terms/page.tsx            # 用户协议
├── me/articles/new/page.tsx  # 普通用户投稿入口
├── me/articles/edit/[id]/    # 普通用户编辑入口
├── me/drafts/[id]/           # 草稿预览
├── not-found.tsx             # 404（同时覆盖全应用未匹配 URL）
├── error.tsx                 # 错误边界（恢复回调 prop 是 retry）
└── admin/                    # 站内管理后台（19 页）
    ├── layout.tsx            # 侧边栏（fixed 贴视口最左、w-52 全高、顶部避开导航栏 4rem）+ 权限守卫 + 深浅模式；内容区 max-w-5xl，文章新建/编辑路由（/admin/articles/new|edit）放宽 max-w-7xl
    ├── page.tsx              # 概览（统计卡 + 资源监控 + 趋势图）
    ├── users/                # 用户管理
    ├── articles/             # 文章管理 + 编辑器
    ├── tags/                 # 标签管理
    ├── pages/                # 页面管理
    ├── comments/             # 评论管理（含审核）
    ├── files/                # 文件管理
    ├── trash/                # 回收站（还原 / 彻底删除）
    ├── backups/              # 备份恢复（刻意不做一键还原）
    ├── sitemap/              # 站点地图（URL 列表 + 统计 + robots 预览）
    ├── links/                # 友情链接
    ├── appearance/           # 外观（菜单/小工具/侧栏位置）
    ├── security/             # 安全防护（验证码/限流/邮箱验证）
    ├── settings/             # 网站管理（站点信息/壁纸/SMTP）
    ├── logs/                 # 网站日志（统计卡片/多维筛选/详情展开/CSV 导出）
    └── about/                # 关于系统

frontend/components/          # 组件（页面组件单份存放于此）
frontend/lib/                 # 工具与状态（api.ts / types.ts / auth-context / ui.ts / seo.ts）
```

**新增共用组件**：`components/pagination.tsx`（后台列表分页，`onChange` 同时回传
page 与 pageSize，调用方不必自己推导「改每页条数要不要回第 1 页」）、
`components/term-article-list.tsx`（分类/标签详情共用）、`lib/seo.ts`
（`SITE_URL` / `resolveSlug` / `plainText` / `jsonLdString`）。

**摘要只用后端的 `excerpt`，前端不再剥正文**：
保存文章时后端就生成好并落库（`service.resolveExcerpt`），列表页每篇都要摘要，
现场剥标签等于把同样的字符串处理重复 N 遍。前端只保留
`fallbackExcerpt` 兜底极旧存量数据，且**按字符截断**——
`slice()` 按 UTF-16 码元，`'😀'.slice(0,1)` 会劈出半个代理对变乱码。

**`excerpt` 与 SEO 描述同源**：`posts/[slug]/page.tsx` 的 meta description 与
`post-detail.tsx` 的 JSON-LD description 都优先取 `article.excerpt`。
两处各算一套会让搜索引擎与社交卡片拿到不同描述。

**前台分页用「加载更多」而非 `useInfiniteQuery`**：`useQueries` 按页声明式查询 +
`loadedPages` state，第 1 页的 queryKey 与 SSR prefetch 完全一致，首屏仍命中服务端缓存；
翻页只是事件回调里的 setState，规避 `react-hooks/set-state-in-effect`。

**派生状态不用 effect 同步**（`app/search/page.tsx` 的做法）：把本地 state 记成
`{q: string, value: string}`，渲染时比对 `draft.q === q ? draft.value : q`——
URL 变了而输入框还停在旧词时，直接回用新 q。比 `useEffect(() => setDraft(q), [q])`
正确，否则触发 ESLint `set-state-in-effect` 报错。

**`article-editor.tsx` 的 `redirectBase` prop**：默认 `/admin`。普通用户从
`/me/articles/new` 发布成功后会被 `router.replace` 到该前缀下的编辑页。
不给这个 prop，普通用户发布完正好撞上 `admin/layout.tsx` 的整页拦截。

---

## 全局状态（Context）

### 1. SiteConfigProvider（`components/site-config-context.tsx`）
站点配置全局上下文，**应用启动时拉取一次 `/site-config`**。

```tsx
const site = useSiteConfig()
// site.siteName, site.siteLogo, site.navMenu, site.widgets
// site.sidebarPosition, site.wallpaper, site.captcha, site.emailCode
```

**⚠️ 新增 site-config 字段必须在这里手动解构**：
```tsx
captcha: {
  provider: (cfg.captcha?.provider ?? 'none') as CaptchaConfig['provider'],
  site_key: cfg.captcha?.site_key ?? '',
  geetest_captcha_id: cfg.captcha?.geetest_captcha_id ?? '',  // ← 曾漏过导致极验失效
  ...
}
```

> 浏览器标签页图标（favicon）由 `components/site-head.tsx` 以 React 19 metadata hoist 渲染 `<link rel="icon">`（必须在 `SiteConfigProvider` 内层，且已删除 `app/icon.svg`/`app/favicon.ico` file convention）；站点 title 由根 `layout.tsx` 的 `generateMetadata` 从后端配置生成。原来在 context 里运行时改 DOM link / `document.title` 的方式在 React 19 下会被覆盖，导致后台改了前台不生效。

### 2. AuthProvider（`lib/auth-context.tsx`）
```tsx
const { user, loading, login, register, logout } = useAuth()
// login/register 返回 Promise<User>，可用于判断角色跳转
```

### 3. NotifyProvider（`components/toast.tsx`）
```tsx
const notify = useNotify()
notify.success('操作成功')
notify.error('失败了')
const ok = await notify.confirm({ title: '确定删除？', danger: true })
```
**禁止使用 `alert` / `confirm` / `window.prompt`**，全部走 `useNotify`。

---

## 数据获取（React Query）

```tsx
const { data, isLoading } = useQuery({
  queryKey: ['articles', 'published', page, category, tag, q],  // 依赖项都要进 key
  queryFn: () => fetchArticles({ page, page_size: 10, category, tag, q }),
})

const mutation = useMutation({
  mutationFn: (id: number) => deleteArticle(id),
  onSuccess: () => {
    queryClient.invalidateQueries({ queryKey: ['articles'] })  // 失效缓存触发重取
    notify.success('已删除')
  },
  onError: (e) => notify.error(e instanceof ApiError ? e.message : '操作失败'),
})
```

**queryKey 约定**（保持前缀一致便于批量失效）：
| 前缀 | 数据 |
|---|---|
| `['articles', ...]` | 文章列表 |
| `['article', id]` / `['article', 'slug', slug]` | 文章详情 |
| `['admin', ...]` | 所有管理端数据 |
| `['site-config']` | 站点配置 |
| `['categories']` / `['tags']` | 分类与标签 |
| `['reactions', id]` / `['comments', id]` | 互动数据 |
| `['me', ...]` | 用户中心数据 |

---

## API 客户端（`lib/api.ts`）

**所有 HTTP 请求必须经过这里**，不要直接 `fetch`（除了文件上传用 XHR 带进度）。

```ts
// 通用请求封装，自动处理 token、401 刷新、错误抛出
api<T>(path, { method, body, auth })
```

**关键机制**：
- `auth: true` 时自动带 `Authorization` 头
- 收到 401 会**自动尝试 refresh token**，成功后重放请求
- 失败抛出 `ApiError(status, message)`，message 是后端中文错误

**新增 API 的标准写法**：
```ts
export function fetchFoo(params: { page?: number } = {}) {
  const search = new URLSearchParams()
  if (params.page) search.set('page', String(params.page))
  const qs = search.toString()
  return api<{ foos: Foo[] }>(`/foos${qs ? `?${qs}` : ''}`, { auth: true })
}
```

**文件上传**（需要进度时用 XHR）：
```ts
export function uploadFile(file: File, onProgress?: (p: number) => void)
export function uploadImage(file: File): Promise<string>  // 返回 url
export function downloadFile(id: number, filename: string)  // 鉴权下载
```

**系统更新**（Beta1.27，见「系统更新页」）：
```ts
fetchUpdateStatus()                    // GET  /admin/system/update        只读本地状态，可高频轮询
checkSystemUpdate()                    // POST /admin/system/update/check  联网比对上游提交
applySystemUpdate(commit)              // POST /admin/system/update/apply  返回 202，异步执行
rollbackSystemUpdate(backupId)         // POST /admin/system/update/rollback
```

**系统信息**：`fetchSystemInfo()` 的返回新增 `commit` / `commit_at` / `commit_source`（运行中提交与来源）。

---

## 核心组件

### 富文本编辑器（`components/rich-editor.tsx`）
基于 TipTap 的 **Gutenberg 风格区块编辑器**。

功能：
- 工具栏：正文/H2/H3、加粗、斜体、链接、列表、引用
- `/` 斜杠命令插入区块（SlashCommandExtension）
- 空行左侧 `+` 按钮插块
- 浮动区块工具栏（跟随当前块）
- 内置弹窗（链接/图片 URL 输入，替代 `window.prompt`）

```tsx
<RichEditor content={html} onChange={setHtml} variant="plain" />
```
> `content` 和存储的都是 **HTML**（不是 Markdown）。`variant="plain"` 用于无边框的写作区。

### Markdown 编辑器（`components/markdown-editor.tsx`）
文章正文编辑器（textarea 自研方案，保存时转 HTML）。

```tsx
<MarkdownEditor value={markdown} onChange={setMarkdown} onSaveRequest={() => saveDraft.mutate()} />
```

功能清单：
- **工具栏**：加粗/斜体/删除线、H1-H3、有序/无序/任务列表、表格（3x3 模板）、引用、行内代码/代码块、链接、图片、分割线
- **快捷键**：`Ctrl/Cmd+B/I/K/U`、`Ctrl+Shift+X` 删除线、`Ctrl+F` 查找替换、`Ctrl+S` 保存（回调 `onSaveRequest`，阻止浏览器保存网页）
- **智能输入**：回车自动续写列表/任务列表（空项时结束列表）、`Tab`/`Shift+Tab` 缩进（多行整体）、行首 `#`/`>`/`-`/`1.` 按空格自动补全
- **图片**：工具栏上传、`Ctrl+V` 粘贴、拖入文件（`.md` 文件拖入插入文本内容）
- **预览增强**（DOM 后处理，仅影响显示，不入库）：
  - highlight.js 语法高亮（`highlight.js/lib/common` 按需语言；主题 CSS 在 `globals.css`，跟随系统深色）
  - 代码块 header：语言标签 + 一键复制按钮（`.md-codeblock`）
  - 任务列表复选框**可点击**，点击后反向改写 Markdown 源码（` [ ]` ↔ `[x]`）
- **目录大纲 TOC**：`extractToc()`（`lib/markdown.ts`）提取标题，侧栏可点击跳转，随编辑区滚动高亮当前标题
- **全屏专注模式**：`fixed inset-0`，`Esc` 退出，锁定 body 滚动
- **滚动同步**：编辑区 ↔ 预览区按滚动比例双向同步（工具栏开关）
- **查找替换**：面板支持区分大小写/正则、上一个/下一个、替换当前、全部替换
- **状态栏**：`Ln/Col`、选中字数、总行数、总字符数、标题数

> 预览后处理放在 `useEffect` 里直接操作 DOM（hljs 高亮、checkbox 增强），
> 因此保存与后端存储的 HTML 保持纯净（仍由后端 bluemonday 消毒）。

### 文章编辑器（`components/article-editor.tsx`）
完整写作页（标题 + 分类 + 标签 + 封面 + 正文 + 状态）。

- **自动保存**：草稿模式停手 2 秒自动保存（开关可记忆）
- **标签选择器**：点选已有标签 or 手输新建
- **封面设置**：上传/URL，留空自动取正文首图
- **保存草稿**（Beta1.15 改语义）：`saveDraft` mutation（status: draft）；
  - 工具条「存草稿」按钮 + `Ctrl+S`（编辑器保存按钮）均触发它，**不再直接发布**（旧行为会把草稿直接发布，易惊吓）
  - new 模式：先落库为 draft，`router.replace` 跳转编辑页，之后由自动保存接管
- **本地草稿**（new 模式）：标题/正文等变更防抖 800ms 写入 `localStorage.blog_article_new_draft`；
  刷新/误关后进入新建页自动恢复并显示提示条（可一键丢弃）；成功创建/发布/删除草稿时清除
- **离开保护**：表单与「上次保存快照」（baseline state）比对，dirty 时刷新/关窗前弹浏览器确认；
  点「文章列表」返回时用 `notify.confirm` 拦截 Next 客户端路由
- 内部用 Markdown 状态，保存/自动保存时经 `markdownToHtml()` 转 HTML

> baseline 快照含 `{t, c, g, s, v}`（title/content/category/tags/cover），
> 任何保存动作（自动保存/存草稿/发布）成功后刷新，避免误报 dirty。

### 人机验证（`components/captcha.tsx` 门面 + 三个 provider hook）

页面统一调用门面 hook，由后台 `captcha_provider` 设置自动选用 provider：

```tsx
import { useCaptcha } from '@/components/captcha'
const captcha = useCaptcha('login') // 'login' | 'register' | 'comment'
// ...
{captcha.dialog}  // 常驻 DOM 的验证弹窗（portal 到 body，display 切换显隐）
```

- `useGeetestCaptcha`（`geetest-captcha.tsx`）：极验 GT4，动态加载 gt4.js，
  `getValidate()` 拿 `{lot_number, captcha_output, pass_token, gen_time}`。
  **enabled 条件排除 lap/pow 两个 provider**（`captchaProvider !== 'lap' && !== 'pow'`）
- `useLapCaptcha`（`lap-captcha.tsx`）：Lap 工作量证明，按 `lap.api_endpoint`
  推导并加载同实例 `widget.js`，挂载 `lap-widget` 自定义元素，监听其
  `solve`（detail.token）/ `error`（detail.message）事件拿 `lap_token`。
  每次打开弹窗都重建 widget 元素（PoW 完成后实例无法原地复位）。
  开启时即预加载脚本（与极验同策略）
- **Lap widget 的两段式流程（Cap 设计，勿改）**：
  1. **speculative 静默预跑**：挂载后由 `mousemove` / `touchstart` / `keydown`
     触发（Cap 的真人检测，listener 在 connectedCallback 绑定，触发一次后自解绑，
     之后 2.5s 发起 challenge）→ PoW → redeem 全部**静默**完成，widget 置为
     done（不派发任何事件，label 仍停在 initial 按钮态）
  2. **用户点击才 solve**：`solve()` 命中 done 快路径后派发 `solve` 事件
     （detail.token），一点即过。所以弹窗文案是「点击下方按钮即可通过」而非
     「自动继续」——speculative 只是让点击变成零延迟
  - **e2e 自动化**：必须 ① 显式移动/派发鼠标事件触发 speculative ② 等 redeem
    200 后**真实点击 widget 中心**（`page.mouse.click`；合成 `.click()` 无效，
    widget 监听 mousedown）才会派发 solve。漏任一步都永远等不到令牌
- **全链路走后端代理（2026-10 修复 DNS 污染事故）**：`lap-captcha.tsx` 不直连
  Lap 实例——widget.js / `LAP_CUSTOM_WASM_URL` / `data-lap-api-endpoint` 全部
  指向本站 `/api/v1/lap/*`（见 backend.md 的 LapProxyHandler 白名单）。
  访客浏览器与 workers.dev 零接触
- **PoW 预热（2026-10 提速）**：`useLapCaptcha(scene, { prewarm })`，门面
  `useCaptcha` 对 login/register 开启、comment 关闭（文章页访客量大不浪费
  配额）。预热把 widget 挂到**屏幕外但渲染可见**的容器
  （`fixed left-[-9999px] opacity-100`——`checkVisibility` 对非 `display:none`
  + opacity:1 返回 true，视口外无妨），用户填表单时 speculative 已静默完成；
  提交时弹窗直接**移动**该实例（DOM appendChild 保状态，不重建）→ 一点即过。
  成功后 `solvedRef` 标记销毁实例并重建预热（token 已消费防重放）；
  未消费的实例关闭时移回屏幕外保留 done。弹窗文案随 `progress` 事件显示
  「正在本地计算工作量证明… n%」，成功后展示 600ms 即关闭
- 三个 hook **provider 互斥**：未选中的 enabled=false，不加载任何外部脚本、
  不发任何请求（POW 连 challenge 都不领）

- `usePowCaptcha`（`pow-captcha.tsx`）：**自研 POW v2，零外部依赖**（2026-10 为解决
  「服务器不能用代理、workers.dev 不可达」新增）。提交流：`run()` 开弹窗 →
  `fetchPowChallenge()` → **两阶段消耗本地资源** → resolve `{pow_challenge, pow_nonce,
  pow_signal}` 后业务请求照常提交。无预热、无 widget、无外部脚本
  - **阶段一·交互信号**：监听 pointermove/keydown/touchstart（passive），取前
    `min_events` 个事件（`m/k/t:unix_ms:x:y`，坐标 clamp ±32767）拼成 signal；
    弹窗文案「请晃动鼠标或触摸屏幕（还需 N 次）」，30 秒未采集够报错。
    真人设备独有——纯 curl/requests 脚本必须额外复刻整套构造逻辑
  - **阶段二·本地计算**：`solvePow()` 构建 `memory_mb` 内存表（Uint32Array，
    xorshift128 填充）并多轮「查表-混合」迭代，找出前导零答案
  - **`lib/pow.ts` 求解器**：优先 `crypto.subtle` 批量并发 digest（https/localhost，
    快数十倍），回退 `lib/sha256.ts` 纯 JS 实现（**http 部署也可用**——crypto.subtle
    只在安全上下文可用，这是写纯 JS 版的原因）。`digestNative` 的缓冲区**每次调用
    新建**（并发共享会互相覆盖）。算法口径与后端 `powDigest` 严格一致；**TS 的 `^`
    会把无符号转 int32，取 idx 必须 `>>> 0`**（否则两端不一致、登录 400）
  - **`lib/sha256.ts`**：`sha256Hex` / `sha256Bytes` / `sha256Raw`（POW 表混合必须
    字节级入口，不能走 TextDecoder 中转——会丢不可打印字节）/ `bytesToHex` /
    `leadingZerosOK`；已过 NIST 标准向量验证
  - 进度条 `origin-left` + `scaleX`（不用 width 动画，避免每帧 layout 重排）；
    计算分批 `setTimeout(0)` yield 保 UI 响应；取消经 `cancelledRef` 通知求解器停止
  - 弹窗动画复用 lap 同款 CSS keyframes（`captcha-closing-overlay` /
    `captcha-check-pop` / `animate-scale-in`），**不用 GSAP opacity**（rAF 节流坑）

弹窗**必须 `createPortal` 到 `document.body`**：调用方页面（如友链申请表单）外层常是
带动画（GSAP `transform`）的容器 div，内联渲染 `fixed inset-0` 会被 transform 包含块困住，
遮罩只覆盖表单卡片区域 → 「只有提交窗口模糊」。portal 用 `useSyncExternalStore`
感知挂载（SSR 首帧 false，hydration 后 true），避免 hydration mismatch 与
`react-hooks/set-state-in-effect` 规则

### 文章代码高亮与代码块增强（`components/code-highlight.ts`）

共享实现 `enhanceCodeBlocks(root, opts?)`：给容器内每个 `pre > code[class=language-x]`
包 wrapper + 头部（**语言标签 + 复制按钮**，`.md-codeblock` 系列样式见 globals.css，
已含 `.prose` 适配）并按 `language-*` 跑 hljs。另导出 `useCodeHighlight(ref, html)`
hook 包装给 React 正文容器用：

```tsx
const contentRef = useRef<HTMLDivElement>(null)
useCodeHighlight(contentRef, articleHtml) // ref 挂到 dangerouslySetInnerHTML 容器上
```

- **编辑器预览**（`markdown-editor.tsx`）与**文章正文**共用同一函数。复制交互：
  按钮内「复制 → 已复制」1.5s 回翻 + **成功 toast**（`onCopySuccess`，各处接
  `notify.success('代码已复制')`）；失败时编辑器接 `onCopyError` toast，
  文章页留空则按钮内显示「失败」
- 已接入：文章详情页、自定义页面三模板（hook 调用**必须在 early return 之前**）、
  后台实时预览（`article-preview.tsx`）。**新增长 HTML 正文容器处都要接**
- 行为约定（浏览器实测）：
  - 语言集 `highlight.js/lib/common`（含 go/ts/js/json/python/bash/yaml 等约 40 种）
  - `language-text`/`plaintext` 走 plaintext 高亮（零 token 无色，良性）
  - 真未知语言（`language-zzzunknown`）或无 `language-*` class → **原样不动**，不误判着色
  - 幂等：`.md-codeblock` 祖先检测 + `dataset.mdHighlighted`；React 重建 DOM 后自动重跑
  - 后端 `sanitize.go` 已全局放行 `class` 属性（`language-*` / `hljs-*` 不过滤）

### 侧边栏小工具（`components/sidebar-widgets.tsx`）
11 种小工具，通过 `type` 分发：

| type | 说明 | 配置项 |
|---|---|---|
| `about` | 关于本站 | content |
| `hot` | 热门文章（按浏览量） | limit |
| `tags` | 标签云 | limit |
| `search` | 搜索框 | — |
| `html` | 自定义 HTML | content |
| `profile` | 站长信息 | 头像（avatar）/ 名字（subtitle，留空回退 title）/ 简介（content）/ 社交图标（socials: {icon,url,label?}[]，icon 用 MenuIcon 图标名）+ 文章统计。居中布局：头像→名字→简介→社交图标行→统计；后台外观页 profile 编辑器含名字输入 + 社交图标增删（`SocialsEditor`，IconPicker 选图标 + URL + 名称），site-config-context 解构 subtitle/socials 白名单需同步维护 |
| `weather` | 天气（Open-Meteo，无需 Key） | city |
| `countdown` | 节日倒计时 | date, eventName |
| `clock` | 实时时钟 | — |
| `stats` | 站点统计 | — |
| `hitokoto` | 一言 | — |

```tsx
<WidgetRenderer widget={w} />
```

**样式约定**：所有小工具统一 `rounded-xl border border-border bg-card p-5`（白底卡片）。

### 通知与确认（`components/toast.tsx`）
- Toast 堆叠在**右下角**（`bottom-10 right-6`），4 秒自动消失
- 确认弹窗支持 `danger` 红样式，Enter 确认 / Esc 取消

### 敏感输入（`components/secret-input.tsx`）
```tsx
<SecretInput value={pass} onChange={setPass} isSet={form.smtp_pass_set} />
```
- 右侧小眼睛切换明文/密文
- 已保存且未输入新值时显示 `••••••••••••`

### 动画工具（`components/motion.tsx`，GSAP 封装）
全站动画统一走 GSAP（`gsap` 3.15）；**framer-motion 已彻底移除**，禁止再引入。

```tsx
<PageTransition>...</PageTransition>              // 页面淡入
<Reveal y={16} delay={0.1}>卡片</Reveal>           // 通用入场（opacity + y/x/scale/delay/duration）
<StaggerList className="grid gap-4">
  <StaggerItem>卡片</StaggerItem>                   // 交错入场（StaggerItem 现为纯透传 div）
</StaggerList>
<HoverLift>悬浮上浮</HoverLift>                    // 悬停 y:-4 上浮
<InView>滚入视口淡入一次</InView>                   // 替代 whileInView + viewport once
<Presence show={open} y={8} scale={0.96}>弹层</Presence>  // 替代 AnimatePresence：退场播完自动卸载
<button {...hoverTapScale}>按钮</button>           // 替代 whileHover/whileTap：悬停放大、按压缩放
<div {...hoverLift}>悬浮元素</div>                  // 替代 whileHover={{y:-4}}
<CountUp value={1234} />                           // 数字滚动
```
语义标签（header/aside/article/span/p…）不要换标签，用 hook 挂动画：
```tsx
const ref = useRef<HTMLElement>(null)
useReveal(ref, { y: 16, duration: 0.4 })   // 挂载时 gsap.from
```
命令式场景（响应式 rotate/x、宽度进度条、路由 key 重挂的 tab 滑块）直接在
useEffect/事件回调里 `gsap.to/fromTo`，用 `prefersReducedMotion()` 守卫。

**⚠️ 入场动画铁律（务必遵守）**：
- **只允许从可见到动的动画**：入场一律用 `Reveal / InView / useReveal / StaggerList`（自带完成清理 `clearProps` + 超时看门狗）；
  **禁止**手搓 `gsap.from(el, { opacity: 0 })`——GSAP 是 rAF 逐帧 tween，远程桌面/窗口遮挡时 rAF 被
  浏览器节流会永久停在透明态，表现为「内容被白色遮挡」（framer 时代走 WAAPI 无此问题）。
  `motion.tsx` 已全局 patch `gsap.from/fromTo` 兜底，但新代码仍应按本铁律走封装组件
- **逐行/逐项列表**（表格 tr、管理列表项）：抽行子组件 + `useReveal(rowRef, { delay: i * 0.05 })`；
  不要把动画写进 ref 回调（每次父组件重渲染都会重播动画，反复闪烁）
- 悬停/按压缩放、进度条宽度、tab 滑块 scaleX 等**非可见性动画**可自由用 `gsap.to`，卡住无感
- **进度条一律 `scaleX` 不用 `width`**：`width` 动画每帧触发 layout 重排，长页面/远程桌面（CPU 合成）下是明显卡顿源；bar 容器 `w-full`/`overflow-hidden`，bar 加 `origin-left`，动画 `gsap.fromTo(bar,{scaleX:0},{scaleX:pct/100})`（route-loader / admin/page / admin/files 均已按此实现）。同理输入框 focus 展宽（navbar 搜索框）也别 `transition-all`+`focus:w-XX`，用 `transition-[border-color,box-shadow]` 只过渡颜色、宽度瞬时变化
- **循环动画必须走 `createLoop`**（`motion.tsx`）：`repeat:-1` 的 tween 纳入全局登记，`visibilitychange` 时页面隐藏自动 `pause()`、恢复 `resume()`——标签页切后台/最小化时不再空转烧 CPU；组件卸载 `kill + releaseLoop`。已接入：`page-loader.tsx`（PageLoading/RowLoading/Spinner）、`sidebar-widgets` 的 `LoopAnim`、`maintenance-gate` 的齿轮旋转、`links` 页 hero 图标浮动
- **blur(filter) 元素禁止做 transform 动画**：装饰光斑（`blur-xl` 等）scale/y/rotate 时每帧重绘整片模糊区域，远程桌面 CPU 合成下极重；一律改用 `opacity` 呼吸（`LoopAnim kind="opacity"`，走合成层）。侧栏 CountdownWidget 光斑已按此实现
- **滚动 handler 里禁止逐帧查 DOM**：rAF 回调中逐项 `getElementById` + `getBoundingClientRect` 会反复强制同步 layout（长文章滚动明显卡顿）；headings 等在 effect 内一次性缓存数组，回调里只读缓存（article-toc 已按此实现）
- **`RowLoading` 只渲染一个旋转环**：早期每行一个环，8 行同时跑 8 个 transform 动画在软渲染环境下明显卡顿；现在首行单环 + 后续行静态文字
- **长列表 stagger 封顶**：`StaggerList` 自动把 stagger 压到「总时长 ≤ duration+0.5s」（20+ 项时逐项 0.06 会拖 1.2s+，体感像卡住）；列表行动画 delay 同样手动 cap（如 users 页 `Math.min(index*0.04, 0.3)`）

**⚠️ 性能与规则**：
- 长列表逐项入场用 `StaggerList` 一次 timeline，**不要**给每个元素自建组件
- `Presence` 内部用「渲染期同步挂载 + GSAP onComplete 延迟卸载」规避 effect 内同步 setState
- React Compiler 规则（`preserve-manual-memoization`）：**组件体内不要写 `useCallback`**，会被 eslint 报 error；把函数提到模块级或放进 effect 内

### 加载动画与路由切换（`components/route-loader.tsx` + `components/page-loader.tsx`）
- **`RouteLoader`**（挂在根 layout `body` 顶部）：GSAP 顶部进度条。
  拦截站内 `<a>` 点击（capture）+ patch `history.pushState`（覆盖 `router.push`）+ `popstate`；
  `pathname` 变化即收尾；6s 兜底自动收起
- **`PageLoading`**：区块/页面级加载动画（GSAP 双环旋转 + 墨点呼吸），`minHeight` 保持版面高度
- **`RowLoading rows={n}`**：行列表加载态（GSAP 驱动旋转环，替代旧的多行骨架）
- **`Spinner`**：inline 小号旋转环（GSAP）
- **全站不再使用 `.skeleton` 骨架图**（class 与 `@keyframes shimmer` 已从 globals.css 删除）：
  页面 `isLoading` 分支一律换成上述三个组件；路由切换期间只显示 RouteLoader 进度条，
  不会闪骨架

### 壁纸（`components/site-wallpaper.tsx`）
从 `site.wallpaper` 读取，固定背景层。有壁纸时 `body.has-wallpaper` 类生效，卡片变半透明（`globals.css`）。

### 菜单图标（`components/menu-icon.tsx`）
36 个 lucide 图标的注册表 + `IconPicker` 选择器。`MenuIcon({name})` 按名字渲染。
> **注意**：`Github` 等品牌图标已被新版 lucide 移除，用 `GitBranch` 替代。

---

## 页面说明

### 首页（`app/page.tsx`）
- 顶部 `max-w-7xl`，有侧边栏小工具时两栏布局
- 文章卡片：左封面（176×112）+ 右内容
- 搜索框在导航栏右侧（不在首页）
- 支持 `?category=`、`?tag=`、`?q=` 筛选

### 文章详情（`app/posts/[slug]/page.tsx`）
三层卡片结构：
1. **标题卡（`header`）**：分类徽章 + `w-1.5` accent 竖线标题（`text-balance`）+ 元信息栏；右上角 `bg-accent/5 blur-3xl` 装饰光斑
   （**只做 opacity，不做 transform**——blur 元素每帧 transform 重绘模糊区，CPU 合成下极重）
2. **正文卡**：封面（`figure` 占满卡宽 `aspect-video`）+ 正文（`proseBody`）+ 底部标签行
3. **互动卡**：点赞 / 收藏 / 分享（`ArticleShare`）/ 返回
4. **评论卡**：输入框 + 评论列表

元信息栏：作者首字母圆形头像 + 用户名 + `·` 分隔 + 日期 + 阅读量；分隔点在窄屏 `hidden`
（`flex-wrap` 折行后不留孤立圆点），`aria-hidden` 不给屏幕阅读器念。

评论列表项：`group` + `hover:border-accent/25`，删除按钮 `opacity-0` → `group-hover:opacity-100`
（`focus-visible:opacity-100` 保证键盘可达）——平时不干扰阅读，悬停才出现。

宽度：`max-w-4xl`（无侧栏）/ `max-w-7xl`（有侧栏，两列）

**文章目录（`components/article-toc.tsx`）**：`parseToc()` 在 `useMemo` 里对正文 HTML 做 DOMParser 提取 h1-h3
（`useIsMounted` gate：SSR 无 document 返回空，hydration 后出现），ArticleToc 组件负责补锚点 id + 滚动高亮 +
点击跳转。滚动高亮的 headings DOM 在 effect 内缓存一次（rAF 回调只读缓存，不逐帧 `getElementById`）。
布局联动：**目录贴「没有侧边栏的一侧」**——无侧栏→右侧（`max-w-5xl` 两列）、侧栏在右→目录在左、
侧栏在左→目录在右（`max-w-7xl` 三列）；正文无标题则整列隐藏退回两列/单列。

**分享（`components/article-share.tsx`）**：移动端优先 `navigator.share` 原生面板，桌面端下拉菜单
（复制链接/微博/Twitter/邮件）；`useIsMounted` 不需要——`navigator.share` 运行时判定即可。

**回顶（`components/back-to-top.tsx`）**：滚动超过 480px 显示 `fixed bottom-[calc(1.5rem+env(safe-area-inset-bottom))] right-6`
悬浮按钮（safe-area 防 iOS 手势条遮挡），点击平滑回顶。目前挂在文章页（组件通用，可按需全局挂载）。
**三个要点**：①**滞回阈值**（480 显示 / 320 隐藏）——单阈值在临界点来回滚动会反复挂载重播动画，图标闪烁；
②**入场用纯 CSS `.animate-scale-in` 不用 Presence**——GSAP opacity tween 在远程桌面 rAF 节流时停透明态，
表现为「滚动后图标半天不显示」；CSS keyframes 走合成器线程无此问题（`prefers-reduced-motion` 由 CSS 自动处理）；
③scroll 监听 rAF 节流 + 函数式 setState。hover 反馈：button 用 `hoverTapScale`，箭头图标另用
`group-hover:-translate-y-0.5` 微上移（两个元素互不冲突）。

### 站点 head（`components/site-head.tsx`）+ 站点 title（`app/layout.tsx` 的 `generateMetadata`）
- **favicon**：`site-head.tsx` 只渲染一个 `<link rel="icon">`（React 19 metadata hoist 到 head），`site_favicon` 有值用自定义图标、为空回退 `/icon.svg`（`public/icon.svg`）。两点硬约束：①**必须渲染在 `SiteConfigProvider` 内层**（`app/layout.tsx` 的 `<Providers>` 内、`<MaintenanceGate>` 前），在 Provider 外 `useSiteConfig()` 只拿到 `DEFAULT_CONFIG`→永远回退默认图标；②**`app/icon.svg`、`app/favicon.ico` 必须保持删除**（Next file convention 会额外注入静态 icon link，与动态 link 并存互相抢占，实测 head 出现 3 个 rel=icon）。**2026-11 复现过**：文档当时已写「已删除」，但这两个文件实际仍在 git 跟踪中（`git ls-files frontend/app/icon.svg frontend/app/favicon.ico` 有输出），上传 favicon 后 head 里 3 个 rel=icon 并存 → 自定义图标被静态图标抢占。删除后重新 build，head 收敛为 1 个 `<link rel="icon" href="/icon.svg">`。**改动后必须用构建产物自查**：`[regex]::Matches((Get-Content .next/server/app/admin/settings.html -Raw), '<link rel="icon"[^>]*>')` 应恰好命中 1 个（`_global-error.html` 为 0 属正常，错误页不挂业务组件树）。
- **站点 title/description**：`app/layout.tsx` 用 `export async function generateMetadata()` fetch `${NEXT_PUBLIC_API_URL}/site-config` 取 `site_name`/`site_description`，`next: { revalidate: 30 }`（与后端 settings 30s 缓存对齐）+ try/catch 兜底。**不要**在 SiteHead 里渲染 `<title>`（React 19 hoist title 不复用 Next 注入的节点→多个 title 而 Chrome 只读第一个），**也不要**在 context 里 `document.title =`（会被 Next metadata 管理覆盖，实测 navbar 已显示新名而 title 不变）。副作用：全站变为 **ISR 30s**。
- **不要再在 site-config-context 里用 querySelector/appendChild 改 favicon**——运行时 DOM 操作会被 React 19 metadata 管理覆盖/清理，后台改了前台不生效（踩过）。

### 管理后台（博客站 `/admin` 全功能）
`frontend/app/admin/`（21 页，全部管理功能）：站内 `layout.tsx` 做**权限守卫**（未登录跳 `/login`，非管理员显示「需要管理员权限」）+ 侧边栏导航；navbar「后台管理」按钮、登录分流（admin 角色跳 `/admin`）、首页空态「写文章」都指向这里。侧边栏导航顺序（15 项）：概览/用户管理/文章管理/标签管理/页面管理/评论管理/文件管理/站点地图/友情链接/外观管理/安全防护/网站管理/系统更新/网站日志/关于系统。

#### 站点地图页（`frontend/app/admin/sitemap/page.tsx`）
- 数据：`useQuery(['admin','sitemap'], fetchSitemapData)`（GET `/admin/sitemap`）
- 统计卡：URL 总数 + 分组计数徽章；sitemap.xml 入口卡（打开/复制地址）；robots.txt 预览卡（复制内容）
- 分组列表：基础页面/文章/独立页/分类/标签（后端 `collectEntries()` 分组顺序），条目表格（名称/地址/频率/权重/最后更新）
- 顶部搜索框按名称或地址前端过滤（服务端数据一次性拿全）

#### 网站日志页（`frontend/app/admin/logs/page.tsx`）
- 列表样式：表格行（表头 + `divide-y` 分隔行），桌面端 `md:grid-cols-[18px_minmax(0,1fr)_150px_110px_130px]`（状态/操作+详情/时间/用户/IP），
  移动端 flex-wrap 两行堆叠；点击行展开完整详情（`whitespace-pre-wrap`）与 UA，未展开时详情 `truncate` + `title` 全文
- 统计卡片：日志总数 / 今日新增 / 失败操作 / 当前筛选数（数据来自 `GET /admin/logs/overview`）
- 筛选：分类 tab（带分类计数）+ 关键词搜索（操作/详情/IP，回车触发）+ 结果下拉（全部/仅成功/仅失败）+ 时间范围下拉（全部/今天/近 7 天/近 30 天 → `from=YYYY-MM-DD`）
- 列表：成功/失败图标、分类徽章、操作、详情（超 48 字折叠 + 「展开/收起」）、用户名#ID、IP、时间、UA（截断 + title 全文）
- 导出 CSV：`downloadLogs()` 用 `fetch + Bearer` 直接取 blob（**不能走 `api()` JSON 客户端**），401 时 `tryRefresh()` 刷新重试；通过 `a[download]` + `URL.createObjectURL` 触发浏览器下载
- 分页：每页 30 条

### 用户中心（`app/me/page.tsx`）
所有登录用户可用：账户安全（改用户名/密码）、我的文章、我的评论。

#### 系统更新页（`frontend/app/admin/system-update/page.tsx`，Beta1.27）
- 数据：`useQuery(['admin','system','update'], fetchUpdateStatus)`（GET `/admin/system/update`，**后端不联网**）；
  `refetchInterval` 按 `stage.running` 动态开关（进行中 2.5 秒轮询，空闲不轮询）
- 进入页面自动检查一次：本地 `version.last_checked` 为空或超过 10 分钟才触发 `checkSystemUpdate()`（`autoChecked` ref 保证只触发一次，避免循环）
- 检查更新：`checkSystemUpdate()` → 把返回的 `update` 直接 `setQueryData` 写回缓存（不额外请求）
- 一键更新：`applySystemUpdate(commit)` 返回 **202**，先 `notify.confirm()` 二次确认（提示落后提交数、预计替换文件数、可能短暂中断），随后靠轮询看进度
- 进度面板 `StagePanel`：阶段中文名（`PHASE_LABEL`）、进度条（`stage.progress`）、错误文案、变更文件数、备份 ID
- 「待生效」卡片：`version.pending` 存在时说明源码已替换、等宿主代理重建（`mode === 'waiting_agent'`）；
  宿主代理执行完毕会写回 `update-result.json` → 页面显示代理结果
- 回滚：`rollbackSystemUpdate(backupId)`（来自 `backups`，最新在前）
- 更新历史：`update.history`（后端保留最近 20 条）

---

## 样式规范

### 动画与感知性能
- **路由切换不显示骨架**：导航期间由 `RouteLoader` 顶部 GSAP 进度条提示；页面数据加载态统一
  `PageLoading` / `RowLoading` / `Spinner`（GSAP 动画）。`.skeleton` 与 `@keyframes shimmer` 已删除，
  **禁止新增骨架图**；首屏页头/按钮常驻、仅数据区显示加载动画，避免整块跳变
- **进度条动画用 `gsap.to(width)`**，`SPA` 内由 effect 按数据驱动；tab 滑块用路由/状态 `key` 重挂 + `scaleX` 入场
- **纯 CSS keyframes 保留**：列表逐项淡入（`.list-stagger > li`）、tab 切换淡入（`.tab-enter`）、
  极验成功对勾（`@keyframes captcha-check-pop`）等为轻量装饰；组件级交互动画一律 GSAP
- **`prefers-reduced-motion: reduce` 全局降级**：`.animate-fade-*`/`.list-stagger`/`.tab-enter` 由 CSS 关闭；
  GSAP 侧由 `prefersReducedMotion()` 统一守卫（motion.tsx / page-loader.tsx 内已处理）

### 视觉层次约定
- **入场时机**：Section 用 `<InView>`（长页面进入视口才播）、悬停微浮用 `{...hoverLift}`；
  tab/导航高亮滑块用 `key` 重挂 + `gsap.fromTo(scaleX)`（单元素，非列表）

### 统一的设计令牌（`app/globals.css`）
```css
--background, --foreground, --muted, --muted-foreground,
--border, --card, --accent
```
Tailwind 中直接用 `bg-card`、`text-muted-foreground`、`border-border`、`text-accent` 等。

### 常用样式组合
```
卡片：    rounded-xl border border-border bg-card p-5
按钮(主)：rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25
按钮(次)：rounded-lg border border-border px-4 py-2 text-sm hover:border-accent/40 hover:text-accent
输入框：  lib/ui.ts 的 inputClass
加载态：  PageLoading / RowLoading / Spinner（components/page-loader.tsx，GSAP；skeleton 已移除）
```

### 正文排版（`lib/ui.ts` 的 `proseBody`）
文章正文（`app/posts/[slug]`、独立页 `app/p/[slug]`）统一用 `proseBody` 常量，**不要再手写 prose 类串**。约定：
- `prose-lg` + `prose-p:leading-[1.8]` + `text-pretty`：中文长文阅读舒适区
- 段间距用 **em**（`prose-p:my-[1.1em]`）而非固定 rem：标题/字号缩放时段落节奏同步缩放
- `prose-headings:scroll-mt-24`：目录锚点跳转不被 sticky 导航遮挡
- h1/h2/h3/h4 层递字号 + `prose-h2:border-b` 分隔线、引用浅底色圆角、图片边框阴影、代码块描边：结构层次
- `.prose table` 的框线/表头底色/斑马纹在 `globals.css` 单独定制；`::selection` 用 accent 底色

**`.prose-reading` 标记类**（`proseBody` 里紧跟在 `prose` 后）：`proseBody` 目前只被文章详情与独立页引用，
但**全站还有三处裸 `prose` 容器**（`markdown-editor.tsx` / `rich-editor.tsx` / `article-preview.tsx` 的编辑器预览），
它们共用 `@tailwindcss/typography` 的 `.prose` 基础样式。因此**只想作用于文章正文的增强样式一律挂 `.prose-reading`**，
直接写 `.prose xxx` 会连后台编辑区一起改：
- `max-width: 74ch` + `margin-inline: auto`：比 typography 默认 65ch 略宽，减少宽卡片里的孤行
- `h2`/`h3` 左侧 accent 竖线（`::before` 绘制，h3 更细更淡）：与文章头部视觉语言一致，且不占布局、不影响目录锚点 id
- 首段 `> p:first-of-type` 轻微放大 + 提亮：形成「导语」观感
- 外链 `a[href^="http"]::after` 加 `↗` 箭头提示会跳站外

> 注意：`.prose-reading` 与 `max-w-none` 同为单类选择器（specificity 相同），
> 靠**源码顺序**决定胜负——`globals.css` 里 `@import "tailwindcss"` 在最前，故这些规则写在文件后段即生效。

### 文章列表卡片（`app/home-client.tsx` 的 `ArticleCard`）
- 卡片用 `flex-col`（窄屏）/ `sm:flex-row`（宽屏，左封面 176×112）；封面 `object-cover` 悬停 `scale-110`
- 顶部 accent→purple 渐变细条：`origin-left scale-x-0` → `group-hover:scale-x-100`
  （**只做 transform 不做 width 动画**，避免每帧 layout 重排）
- 右侧内容区 `flex flex-col`，元信息行 `sm:mt-auto sm:pt-4`：宽屏时元信息贴卡片底部，多张卡片高度对齐
- 元信息：作者头像（首字母圆形）+ 用户名 · 日期 + 阅读量；标签贴右（`ml-auto`，仅 `sm` 以上显示，最多 3 个）

### 文章目录（`components/article-toc.tsx`）外观
目录卡片：`rounded-xl border border-border bg-card/60 p-3` 半透明卡 + `sticky top-24`；
条目 `truncate` + `title` 全文（长标题不撑破侧栏），高亮项 `border-accent text-accent`，悬停补 `border-border`。

### 深色模式
全部用 `dark:` 前缀，不需要额外配置（跟随系统）。

---

## 常见修改场景

### 新增一个页面
1. `app/新路径/page.tsx`
2. `'use client'`（需要交互时）
3. 用 `<PageTransition>` 包裹，数据 loading 分支用 `<PageLoading>` / `<RowLoading>`（**不要骨架图**）
4. 数据用 `useQuery` + `lib/api.ts` 的函数
5. 路由切换进度条由根 layout 的 `RouteLoader` 自动接管，新页面无需额外配置

### 新增一个管理页
1. 页面组件放 `frontend/components/`（需要跨页复用时）；页面文件 `frontend/app/admin/新路径/page.tsx` 写 `export { default } from '@/components/...'` reexport 或直接实现
2. 在 `frontend/app/admin/layout.tsx` 的 `navItems` 数组加菜单项（含 lucide 图标）
3. 页内用 `useNotify()` 做反馈、`notify.confirm()` 做删除确认（Promise 风格：`const ok = await notify.confirm({title, message, confirmText, danger})`）
4. API 函数加 `frontend/lib/api.ts`

### 修改站点配置字段
1. 后端 `settings_service.go` 加常量
2. `frontend/app/admin/settings/page.tsx` 的 **payload 白名单**手动加字段（关键！）
3. `site-config-context.tsx` 手动解构成字段
4. 使用处 `useSiteConfig()` 取值
5. 若该字段要被 admin 后台表单编辑，同步加进 `frontend/lib/types.ts` 的 `SiteSettings`（TS 报错的常见点）

### 调试站点配置
```ts
// 浏览器控制台
fetch('/api/v1/site-config').then(r => r.json()).then(console.log)
```
