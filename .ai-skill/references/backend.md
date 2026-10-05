# 后端架构详解

Go 1.27 + Gin + GORM + PostgreSQL。模块名 `github.com/shenwei/inkstone/backend`。

## 分层架构

```
cmd/server/main.go     ← 依赖装配 + 路由注册（唯一入口）
      ↓
internal/handler/      ← HTTP 层：绑定参数、调用 service、映射错误
      ↓
internal/service/      ← 业务逻辑：校验、权限、事务编排
      ↓
internal/repository/   ← 数据访问：GORM 查询
      ↓
internal/model/        ← 数据模型
```

**横切关注点**：
- `internal/middleware/` — Auth、CORS、限流、安全头、流量统计
- `pkg/config/` — 环境变量
- `pkg/mailer/` — SMTP 发信

---

## 各层职责与约定

### handler 层

```go
type FooHandler struct {
    foo *service.FooService
}

func NewFooHandler(foo *service.FooService) *FooHandler {
    return &FooHandler{foo: foo}
}

func (h *FooHandler) Create(c *gin.Context) {
    current, ok := middleware.GetCurrentUser(c)  // 拿当前用户
    if !ok {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
        return
    }

    var req fooRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "请填写完整信息"})
        return
    }

    result, err := h.foo.Create(current.ID, req.Field)
    if err != nil {
        errorResponse(c, err)   // 统一错误映射
        return
    }
    c.JSON(http.StatusCreated, gin.H{"foo": result})
}
```

**约定**：
- 请求结构体命名 `xxxRequest`，响应转换函数 `toXxxResponse()`
- 用 `binding:"required"` 做基础校验，业务校验放 service
- 路径参数用 `parseUintParam(c, "id", "无效的 ID")`
- 分页参数用 `parseIntQuery(c, "page", 1)`

### service 层

- 定义领域错误：`ErrForbidden`、`ErrInvalidCredentials`、`ErrUserBanned`、`ErrRegistrationClosed`
- 校验失败返回 `NewValidationError("中文提示")`
- 权限判断（如「只能改自己的文章」）在这里做
- 返回 `(*model.Xxx, error)`，不做 JSON 转换

### repository 层

- 定义哨兵错误：`ErrNotFound`、`ErrEmailTaken`、`ErrUsernameTaken`
- 唯一约束冲突用 `uniqueViolationField(err)` 解析 `pgconn.PgError` 的约束名
- 列表查询统一签名 `List(query) ([]model.X, int64, error)`（返回总数用于分页）

### model 层

GORM 结构体。**新增模型必须加入 `internal/repository/db.go` 的 AutoMigrate 列表**：

```go
db.AutoMigrate(
    &model.User{}, &model.Article{}, &model.Category{}, &model.Tag{},
    &model.Comment{}, &model.Reaction{}, &model.Setting{}, &model.Page{},
    &model.FriendLink{}, &model.FileAsset{}, &model.DailyStat{}, &model.VisitorDay{},
    &model.FriendLinkApplication{},
)
```

---

## 中间件详解

### Auth（`middleware/auth.go`）
```go
middleware.Auth(tokens, userStatusOK)
```
- 解析 `Authorization: Bearer <token>`
- 校验 JWT 有效性 + 类型必须是 `access`
- `userStatusOK` 回调**实时查库**校验用户未被封禁/删除（避免封禁后旧 token 仍可用），签名 `func(id uint) (middleware.UserStatus, bool)`；返回的**用户名与角色**会覆盖 token 里的同名 claim
  - ⚠️ **角色必须来自数据库**：若沿用 JWT 里的旧 role，管理员被降级后在其 refresh TTL 内仍能访问 `/admin/**`（`RequireRole` 比对的就是这个值）
- 通过后 `c.Set(ContextUserKey, CurrentUser{ID, Role, Username})`
- 用 `middleware.GetCurrentUser(c)` 取值
- JWT claims 内置 `uname`（用户名）与 `ver`（令牌代次）声明，`GeneratePair` 签名 `(userID, username, role, tokenVersion)`；**改动签发点必须同步四个参数**（Register/Login/Refresh）

### OptionalAuth
同 Auth，但无 token 或 token 无效时不拦截，仅匿名放行。用于文章列表等「登录后可见更多」的接口。
**同样接收 `UserChecker`**：封禁用户的令牌不会被采信（当作匿名），否则旧 token 过期前仍可读到自己的草稿。

### RequireRole
```go
middleware.RequireRole(model.RoleAdmin)
```
检查角色，非管理员返回 403。

### Security（`middleware/security.go`）
- `SecurityHeaders()` — 全局安全响应头（X-Frame-Options 等）
- `SlidingLimiter` — 内存滑动窗口限流器
  - `Allow(key, limit, window)` — 是否允许
  - `Reset(key)` — 重置（登录成功后调用，避免误伤）
- `IPRateLimit(cfg)` — 按 IP 限流中间件，429 响应
- `ClientIP(c)` — 解析真实 IP（优先 X-Forwarded-For）

### TrafficStats（`middleware/stats.go`）
异步记录每请求流量到 `StatService`，不阻塞请求。

### CORS（`middleware/cors.go`）

单源白名单 + credentials。**访客站与独立后台双源**：`main.go` 传 `[]string{cfg.FrontendURL, cfg.AdminURL}`（去重后），后台跨源部署（API 指向另一域名）时才不会被拦；同源反代（默认）无感。
白名单来自 `FRONTEND_URL`。

### CachePolicy（`middleware/cache.go`）

全站此前没有任何 `Cache-Control`，后果有两端：中间层自行决定缓存行为
（有的把 `/auth/me` 连 token 一起缓存，换个账号看到上一个人的资料），
而真正可长缓存的 `/uploads` 反而每次回源。

策略是**默认不缓存 + 白名单长缓存**，不是反过来：

| 路径 | Cache-Control |
|---|---|
| `/uploads/*` | `public, max-age=31536000, immutable` |
| `/articles`、`/categories`、`/tags`、`/feed.xml`、`/sitemap`、`/site-config` | `public, max-age=60, stale-while-revalidate=300` |
| 其余（`/auth/me`、`/admin/**`、写操作） | `private, no-cache` |
| 任何 ≥400 响应 | `no-store` |

安全默认可预期：新增接口忘记配置时是"多走一次后端"，不会是"用户看到别人的数据"。
`/uploads` 敢长缓存是因为文件名带上传时间戳，内容不会原地变；文章正文改了 URL 不变，
所以**不能**长缓存。handler 已显式设过 `Cache-Control` 时不覆盖（文件下载带 ETag 那条路径）。

---

## 核心 Service 说明

### AuthService
| 方法 | 说明 |
|---|---|
| `Register(RegisterInput)` | 注册。检查注册开关；**首个用户自动成为 admin** |
| `Login(identifier, password)` | 登录。**identifier 支持邮箱或用户名**（`FindByLogin`）；封禁用户拒绝 |
| `Refresh(token)` | 刷新令牌，校验封禁状态 + **令牌代次一致**（代次不符返回 `ErrTokenRevoked`，用于改密码/封禁后立即作废） |
| `ChangePassword(userID, current, new)` | 验证当前密码 → bcrypt 哈希新密码 |
| `UpdateUsername(userID, name)` | 改用户名（唯一性校验） |

### ArticleService
| 方法 | 说明 |
|---|---|
| `Create(authorID, ArticleInput)` | 创建。`resolveCover()` 处理封面（空则取正文首图） |
| `Update(id, authorID, ArticleUpdate)` | 更新，校验作者身份 |
| `List(ArticleQuery)` | 列表，支持分类/标签/搜索/排序筛选 |
| `IncrementViews(id)` | 浏览量 +1 |
| `Related(articleID, limit)` | 相关文章（共同标签 ≥2 > 1 > 同分类），只取卡片字段 |
| `Neighbors(articleID)` | 上一篇（更新的）/ 下一篇（更旧的） |
| `Restore(id)` / `Purge(id)` / `Trash(page, pageSize)` | 回收站三件套 |

**文章新字段的判定规则**（`article_service.go`）：

| 字段 | 空值/默认 | 特殊规则 |
|---|---|---|
| `excerpt` | 空 = 自动从正文生成 | `resolveExcerpt`：作者填了就用作者的，**只有空白的摘要也算没填** |
| `is_pinned` | false | 排序加在 `is_pinned DESC` 最前；`order=views` 的热门榜**不掺置顶** |
| `view_password` | 空 = 不设密码 | 存 bcrypt 哈希，`json:"-"`；明文只在请求体出现；上限 32 字符（bcrypt 只取前 72 字节，超长静默失效）；更新时「传空串=清除、不传=保持」 |
| `scheduled_at` | nil | 仅 `status=scheduled` 有效；**传过去的时刻会被按已发布处理**，不让文章永远等在一个已过去的时刻 |

`ArticleHasPassword` 用于卡片：`articleCardColumns` 用
`CASE WHEN view_password IS NULL OR view_password = '' THEN false ELSE true END AS has_password`
算出布尔值，让相关/邻居卡片显示锁标识，而不必把哈希查出来。

**`normalizeArticleStatus` 的三个分支**：draft/published 原样过；
scheduled 缺时间报错、时间已过则降级为 published；其余未知状态回落 draft。
回落 draft 而不是报错——一个拼错的状态值不能让新建文章失败。

`service/excerpt.go` 的 `ExcerptFor(content string, maxRunes int)` 生成摘要：
显式 excerpt 字段（当前模型无此列）> `<p>` 段落拼接（空段跳过）> 全文剥标签，
按 **rune** 截断加「…」（中文不能用字节）。`stripTagsLocally` 是与
`handler.stripHTMLTags` 逐字节等价的**有意复制**——service 层不能反向依赖 handler 包。

**封面逻辑**（`resolveCover`）：
```go
func resolveCover(explicit, content string) string {
    if cover := strings.TrimSpace(explicit); cover != "" {
        return cover  // 显式设置优先
    }
    return firstImageURL(content)  // 否则提取正文首个 <img src="...">
}
```

### CommentService（含审核、嵌套与游客评论）

`Create(articleID, identity CommentIdentity, parentID uint, content, ip string)` —
身份用结构体承载（`CommentIdentity{UserID *uint, GuestName, GuestEmail, GuestURL}`），
登录用户 `UserID` 非空、游客为空。**用结构体而不是继续加参数**：这个方法已因
parentID / ip 改过两次签名，每加一个字段就动一次签名与全部调用方。

`ListByArticle(articleID, viewerID)` 返回 approved 的 + viewer 自己的 pending。
**「自己的 pending」只对登录用户成立**：游客没有账号，无从在一次请求之外认出
自己刚发的那条，因此只能看到已过审的评论，提交后前端提示"等待审核"而不回显。

审核判定在 `applyModeration`，三条来源：`comment_audit` 开启、命中敏感词、
**或这是一条游客评论且 `guest_comment_free` 未开启**（默认未开启 → 游客评论
一律进待审队列）。游客没有账号可封、没有历史可追溯，默认先审后发是唯一
能在公开前挡住刷屏的位置。

敏感词匹配用 `wordSep = [\n\r,，;；|]+` 切分词表——后台 textarea 里
用户换行还是逗号全凭习惯，不做硬性规定。比对前内容与词都转小写。

`DeleteComment` 的越权判定必须**先判游客再比 ID**：

```go
if isAdmin { return s.comments.Delete(commentID) }
if comment.IsGuest() || comment.AuthorID() != userID { return ErrForbidden }
```

游客的 `AuthorID()` 是 0，未登录请求传进来的 `userID` 也是 0 —— 少了
`IsGuest()` 这一项，`0 != 0` 不成立，等于任何人可删任意游客评论。

通知是旁路：`CommentNotifier` 接口由 `pkg/mailer.CommentNotifier` 实现，
`article.AuthorID == comment.AuthorID()` 时跳过（游客返回 0，与真实作者
ID 必然不等，因此游客评论一定会通知作者），Email 为空时跳过，
发送失败只 `log.Printf`。评论者名字走 `comment.DisplayName()`：登录用户取
用户名，游客取昵称（缺失时回落「匿名访客」）。

**游客身份校验**（`normalizeIdentity`）：
- 登录用户：**清空全部 guest_* 字段**。请求体是客户端可控的，不清就等于
  允许任何登录用户把自己的评论伪装成游客留言。
- 游客：昵称必填（≤32 字符）；邮箱选填但要像邮箱，`guest_comment_email`
  开启时改为必填；网址选填且**必须是 http/https**——`javascript:` 之类会
  被 `<a href>` 直接执行，构成存储型 XSS（这里拒绝而不改写，让用户知道
  自己填的地址没被接受）。

### imageutil（`pkg/imageutil`）

零第三方依赖（只用标准库 image/jpeg/png/gif）。入口在 `handler/uploads_handler.go`：

```
写原图 → 超限则覆盖写压缩版 → 生成 *_thumb.<ext>
```

- `Fit(data, maxW, maxH, quality)`：只缩不放，已在上限内时**原样返回副本**
  （避免无谓二次有损压缩）。测试用 `bytes.Equal(out, data)` 区分这两种情况
- `resizeAreaAverage`：区域平均（box filter）缩小，高倍缩小不出锯齿
- WebP 按魔数可识别（`Kind` 返回 ok=true）但 Fit 明确返回 `ErrWebPUnsupported`
- 防护：`maxSourceBytes=32MB`、`maxDimension=20000` 像素——都在解码前拦，防 JPEG 解压炸弹

测试见 `pkg/imageutil/imageutil_test.go`。
**写缩放测试别把「测试自身选错像素」当成产品 bug**：32→8 是精确 4:1，
目标像素的源区间不会跨过黑白分界；用非整除的缩放比（如 32→7）才能真正验证取的是均值。

**封面逻辑**（`resolveCover`）：
```go
func resolveCover(explicit, content string) string {
    if cover := strings.TrimSpace(explicit); cover != "" {
        return cover  // 显式设置优先
    }
    return firstImageURL(content)  // 否则提取正文首个 <img src="...">
}
```

### 邮箱验证码存储（`model.EmailCode` / `repository/email_code_repo.go`）

与 POW 挑战同理：验证码必须放共享存储。用户要去邮箱收信、复制、再回来填，
这段时间足以让下一个请求落到另一个实例上。内存 map 版在多副本下会让用户看到
「验证码已过期」——明明刚收到的码。

**主键是 (email, purpose) 而不是 email**：同一邮箱可能并行发起「登录」和
「重置密码」。只按 email 作主键会让后申请的那枚顶掉前一枚，用户拿先收到的那枚
来校验必然失败，而提示只是「验证码不正确」。

`UpsertIfNotRecent` 一条 UPSERT 同时完成**重发间隔检查与写入**：
WHERE 条件限定「不存在记录，或已有记录已过间隔」。若拆成两次查询，
并发请求会同时通过检查、各发一封信，后写的顶掉先前的。集成测试里有
10 并发的用例守住这一点。

`Consume` 同样是**无条件删除再判条件**——过期条件写进 WHERE 的话，
过期的码谁都删不掉，还占着重发间隔，用户想重新申请会被挡回来而拿到的码其实已失效。

`EmailCodeService` 依赖 `EmailCodeStore` 接口（与 `ChallengeStore` 同样的理由），
生产用 `repository.EmailCodeRepository`，测试用内存 fake
（`email_code_fake_test.go`）。行为一致性由真库集成测试守住。

### POW 挑战存储（`model.PowChallenge` / `repository/pow_challenge_repo.go`）

POW 的 challenge 跨请求存活（前端先领、算几百毫秒到几秒、再提交），
**必须放共享存储**：内存 map 下进程 A 签发的挑战被提交到进程 B 时
必然查不到，症状是用户反复「验证已失效，请重新验证」，
错误提示指向客户端，根因在服务端拓扑。

`PowService` 只依赖 `ChallengeStore` 接口，不直接吃 `*gorm.DB`：
本项目没有数据库测试基建，接口让 service 测试能跑在内存实现上。
生产实现是 `repository.PowChallengeRepository`，测试共用
`internal/service/powstoretest` 的 `Store`（它刻意复刻生产的
**可观察行为**：取走即删、过期判定、并发互斥）。

`Take` 是本文件唯一的核心语义，三条都是踩过的坑：

| 写法 | 后果 |
|---|---|
| `WHERE issued_at > now()` | 要求行存于未来。**实测 100% 取不到任何挑战**，用户刚领挑战就被告知失效 |
| 过期条件写进 `WHERE` | 已过期的行谁都删不掉（DELETE 没匹配），只能等每 2 分钟的 GC。提交过期挑战是零成本攻击面，表会无限堆积 |
| 先 `Exec(DELETE)` 再 `Raw(... RETURNING)` | 第一条已把行删了，第二条只会拿到零行，症状是「刚签发却说不存在」 |

正确写法是**无条件 DELETE + RETURNING 拿整行，再在代码里判过期**。
`make_interval(secs => ...)` 返回 interval，与 timestamptz 相减在
参数化查询下类型推导不成立（SQLSTATE 42883），参数后要加 `::timestamptz`。

清理挂在签发路径上（`maybeGC`，距上次超过 2 分钟才真删）而不起独立
goroutine：多副本下每实例一个定时器会重复清理，挂签发路径则天然只
清理真正产生负载的实例。

集成测试见 `pow_challenge_repo_integration_test.go`，需要
`INKSTONE_TEST_DSN`（未设置自动跳过）。

### ScheduledPublisher（`service/scheduled_publisher.go`）

每分钟把到点的 `scheduled` 文章改成 `published`。

用轮询而不是数据库定时任务：项目只用 PostgreSQL，没装 pg_cron，
引入外部调度器会多一个部署依赖。发布延迟最多 1 分钟，博客场景可接受。

依赖定义成 `ArticlePublisher` 接口（`PublishDue(now)`）而非直接吃
`*ArticleRepository`：单测能打桩，不必为测一个 cron 循环连数据库。
失败**不退出循环**，下次 tick 重试——发布失败只是文章晚几分钟公开，
不该让 goroutine 死掉。`Stop()` 可重复调用（channel二次 close 会 panic）。

`PublishDue` 用一条 UPDATE 而非先查再逐条改：逐条改会在两个 tick 之间
重复发布同一批（扫到的是同一个集合），UPDATE 天然幂等。
`published_at` 只在为空时写 now——文章可能带着预计发布时间创建，
那才是它名义上的发布时间。

### RevisionService（`service/revision_service.go`）

文章历史版本。模型见 `model.ArticleRevision`，表 `article_revisions`。

- `Snapshot` 在**保存前**留版（`Update` 路径自动接入）。**失败只忽略不返回错误**：
  版本历史是增强功能，为它让保存失败是拿次要功能拖垮主要功能。
- 内容与标题都没变时**不留版**：自动保存周期性触发，留几十版
  一模一样的记录会把有用版本挤出 50 版上限。
- `Restore` 是「再存一版新的」而非直接覆盖——先给当前内容留版，
  否则"恢复到第 2 版"这个操作本身不可逆。
- `NextVersion` 用 `MAX(version)+1` 而非 `COUNT(*)+1`：删掉中间某一版后，
  COUNT 会让版本号回退与已有版本撞号。
- `pruneRevisions` 用一条子查询 DELETE 而非先查列表再逐条删：后者在并发
  保存时可能把刚写进去的新版本也列进待删集合。
- `ArticleService` 通过 `SetRevisions(RevisionSnapshotter)` 注入。
  未注入时全部快照静默跳过——不为了可选功能改 `NewArticleService` 签名。
  **防御写在调用方**（`snapshot` 里判 nil 与 ID==0），因为接口将来可能换实现，
  契约该由调用方保证，不必每个实现各写一遍判空。

### ExportImportService（`service/export_import_service.go`）

内容导入导出，与 `BackupService` 全站快照是两套东西（区别见 api.md）。
分类/标签在导出格式里是**名称**而非 ID——ID 是另一套库的私有值。

### 人机验证（`geetest_service.go` / `lap_service.go` / `pow_service.go` / `captcha_service.go`）

三层结构：三个 provider 实现 + 一个门面。

```
CaptchaService（captcha_service.go，handler 唯一入口）
  ├── GeetestService（geetest_service.go）极验第四代行为验证
  ├── LapService（lap_service.go）Lap 工作量证明（Cap 的 CF Workers 分支）
  └── PowService（pow_service.go）自研工作量证明（零外部依赖，内存挑战池）
```

```go
// 门面：按设置项 captcha_provider（"lap" 默认 | "pow" | "geetest"）分发
captcha.Required(action)              // "register"|"login"|"comment" 场景是否开启
captcha.Verify(action, CaptchaParams) // 按 provider 校验；CaptchaParams 同时携带三套凭证字段
captcha.PublicConfig()                // {provider, geetest:{...}, lap:{...}, pow:{...}} 下发 /site-config
```

`CaptchaParams` 请求体字段：极验 `{lot_number, captcha_output, pass_token, gen_time}`，
Lap `{lap_token}`，POW `{pow_challenge, pow_nonce, pow_signal}`；未选中的 provider 忽略。

**POW v2 协议**（`pow_service.go` + `pow_handler.go`，无任何外部请求）：
1. 前端 `POST /api/v1/pow/challenge` → 32 字节随机 challenge + 本地资源参数快照
   （`memory_mb` / `rounds` / `min_events` / `difficulty` / TTL，快照进挑战记录）
2. 前端两阶段（组件 `pow-captcha.tsx`）：
   a. **交互信号采集**：监听 pointermove/keydown/touchstart，取前 min_events 个
      事件（`{m|k|t}:{unix_ms}:{x}:{y}` 逗号连接）；b. **本地计算**：构建内存表并
      多轮「查表-混合」找前导零答案（求解器 `lib/pow.ts`）
3. 业务请求带 `{pow_challenge, pow_nonce, pow_signal}` 提交 → 后端同参数重算校验 +
   signal 格式/时间窗校验（事件数、ms 落在 [签发时刻-2s, 现在+2s]），通过即消费挑战
   - 挑战池上限 1 万（满 → 503），GC 每 2 分钟扫过期；
   - **内存存储 = 仅单实例有效**（单 backend 容器无碍，多副本部署需换共享存储）；
   - 难度 1-6 / 内存 1-32MB / 轮数 1-16 / 事件数 0-10，越界均回退默认值。
   - 算法口径（两端逐位一致，**任一侧改动必须同步另一侧并对拍**）：
     `TABLE_LEN = memoryMB*262144`；xorshift128 种子 = `SHA-256(challenge)` 前 16 字节
     （大端 4×u32）；`h = SHA-256(challenge:nonce)`；rounds 轮
     `idx = (BE32(h[0:4]) ^ (r*0x9E3779B9)) % TABLE_LEN; h = SHA-256(h || BE32(table[idx]))`；
     答案 = hex(h) 前 difficulty 位全 '0'。
     **前端 TS 陷阱**：`^` 会把操作数转 int32，`be32(...) ^ Math.imul(...)` 必须
     **`>>> 0`** 回无符号再取模，否则 idx 出负数与 Go 的 uint32 口径不一致
     （2026-10 实测：表现为登录 400「验证未通过」，而验证码算法两头"都觉得自己对"）。

**Lap 协议**（前端由实例的 widget.js 完成 challenge→redeem，后端只做 siteverify）：
1. widget POST `{endpoint}challenge`（endpoint = 后台配置的 `lap_api_endpoint`）
2. 浏览器 WASM 本地解 PoW → widget POST `{endpoint}redeem` 换出 `lap_token`
3. 后端 POST `{origin}/siteverify` body `{secret, response: lap_token}` → `{success, error}`
   （URL 由 endpoint 推导：`scheme://host/siteverify`，见 `lapSiteVerifyURL`）
   - `success:true` 通过；业务失败（token 无效/过期）→ 400 拒绝
   - "Invalid site key or secret"（后台配置问题）/ 网络错误 / 解析失败 → 放行并打日志
   - 本地 `claimTicket` 防同一令牌重复提交（10 分钟 TTL）


**Lap 网络兜底（2026-10 加，两层按设置动态生效）**：
- `lap_resolve_ip`：后端 Transport 的 DialContext 仅对 lap_api_endpoint 的 host 做固定 IP 拨号
  （DNS 被污染到假 IP 时用；TLS SNI 不变，jsdelivr 等 host 走正常解析）
- `lap_http_proxy`：本机到 Lap 实例整段被阻断（TUN 代理黑洞 Cloudflare 段）时，后端经
  HTTP 代理访问（开发机填 clash 端口；生产服务器留空直连）
- `lap_transport.go` 的 `NewLapHTTPClient` 同时服务 siteverify 与代理 handler。
  **`DisableKeepAlives: true` + `LapDo` 重试一次是必需的**：本地代理（clash）
  切换节点/重启后，Transport 缓存的 idle keep-alive 连接会 EOF，而 POST 不会被
  自动重试 → 前端持续报「验证组件加载失败」、后端 502（排查现象：curl 经代理
  通，但 Go 长驻进程仍 502）。禁用 idle 复用使每次请求新建连接，再加连接级
  错误重试一次（4xx/5xx 业务响应不重试）

**Lap 同源代理（`lap_proxy.go`，公开路由 `api.Any("/lap/*path")`）**：
访客浏览器**不直连** workers.dev（DNS 污染/超时是真实事故），全部经本站后端转发：
- GET `/lap/widget.js`、`/lap/widget.compat.js`、`/floating.js` → Lap 实例同路径（缓存 5min）
- GET `/lap/wasm` → jsdelivr PoW WASM（`LapWasmUpstream`，缓存 5min）
- POST `/lap/{siteKey}/challenge`、`/lap/{siteKey}/redeem` → 实例同路径（siteKey 须与配置一致，no-store）
- 白名单外一律 404；请求体 1MB 上限；**必须透传浏览器 UA**（Cloudflare 对 Go-http-client
  的 POST 直接 403 Blocked）；siteverify 不走代理（后端内部直连）
**容错设计（很重要，勿破坏）**：两套 provider 同一约定——
1. 场景未开启 → `return nil`（放行）
2. 密钥/端点未配置 → `return nil`（放行，打日志）
3. 第三方服务不可达 / 响应异常 → `return nil`（放行）
4. 只有「未完成验证 / 凭证无效 / 重放」才拒绝

环境变量 `INKSTONE_DISABLE_CAPTCHA=1` 可全局紧急停用验证码。

**极验 token 格式**：前端把 4 个字段 JSON 序列化后随请求提交，
后端用 `captcha_key` 算 `sign_token = HMAC-SHA256(lot_number, key)` 后调 `https://gcaptcha4.geetest.com/validate`。

### SettingsService
键值设置系统，**30 秒内存缓存**。

```go
All()                          // 全部（含默认值）
Get(key)                       // 单值
IntValue(key, fallback)        // 整数读取
BoolValue(key, fallback)       // 布尔读取
Update(map[string]any)         // 更新，自动失效缓存
Public()                       // 公开配置（排除 maskKeys）
AdminView()                    // 管理视图（maskKeys 转为 xxx_set）
```

**关键机制**：
- `maskKeys` 中的字段（SMTP 密码、验证码密钥）API 只返回 `xxx_set` 布尔值
- **Update 时空值 = 保持原值**（防误删密钥）
- `jsonSettingKeys` 中的字段（nav_menu、sidebar_widgets）自动 JSON 编解码

### StatService
- `Record(bytesIn, bytesOut, clientIP)` — 异步队列记录流量（IP 存 SHA256 哈希，不存原始地址）
- `Trend(days)` — 返回 N 天趋势，**自动补零日期**
- `SystemResources()` — 系统资源（跨平台：`stat_linux.go` 读 /proc，`stat_windows.go` 用 PowerShell CIM）

**健壮性要点（Beta1.15 审查后补充）**：
- `main.go` 优雅停机（SIGTERM → `srv.Shutdown` 30s），保证更新停容器期间在途请求落库

**安全要点（Beta1.18 审查后补充）**：
- compose backend 同样 `cap_drop: ALL` + `no-new-privileges`（Go 静态服务无需任何 capability）
- `SecurityHeaders` 含 HSTS（max-age 1 年 + includeSubDomains）；JWT_SECRET<32 启动 Warn

### LogService（`service/log_service.go`，Beta1.12 增强）
```go
Record(Entry)   // 异步入队（1024 缓冲，满丢弃；Detail 截 500、UserAgent 截 250）
List(OperationLogQuery)   // 分页查询：category/username/keyword/from/to/success
Export(OperationLogQuery) // 全量导出（不分页，上限 5 万条）
Overview() (LogOverview)  // total/today/failed/by_category 总览
Stats()                   // 各分类计数（旧接口）
```
异步 worker 落库，`cleanupLoop` 每 24h 清理 90 天前日志。

**日志记录点**（Beta1.12 起全覆盖，handler 层统一走 `recordOp(logs, c, category, action, detail, success)`，自动带当前用户/IP/UA）：
- auth：注册成功、登录成功/失败、改密码、改资料、refresh 失败
- article：文章创建/更新/删除（更新日志含**变更字段明细**，如「变更：标题、内容」）
- user：创建/改资料/封禁/解禁/改角色/删除用户
- comment：用户删自己评论、管理员删评论
- setting：更新设置（**只记 key 名列表，绝不记录值**——设置含 SMTP 密码等密钥）、测试邮件
- file / link / page / taxonomy / system：增删改详情含标题/名称，更新前先查实体名
- Detail 长度防护：`Record` 按字符截断到 500（列宽上限），防长标题导致入库失败

### UpdateService（`service/update_*.go`，Beta1.27 新增）

在线更新的全部逻辑。文件划分：

| 文件 | 职责 |
|---|---|
| `update_service.go` | 状态机与状态文件（`UPDATE_DIR/update-state.json`）、`Status()` / `Check()`、版本信息组装、预览缓存 |
| `update_remote.go` | HTTP 客户端（`getJSON` / `download` 流式 + SHA256）、更新源响应解析（GitHub 形状 / 自建 JSON）、仓库地址与哈希工具 |
| `update_changelog.go` | 提交差异：compare 接口 → 提交列表分页回溯 → 空日志 + 说明（**绝不假装「已是最新」**） |
| `update_release.go` | **releases 更新源**：Releases 拉取与解析、版本号解析/比较（兼容 `Beta1.27` 与 `v1.28.0`）、镜像包资产挑选与下载、部署版本记录（`deployed-version.json`）与安装历史（`release-history.json`） |
| `update_archive.go` | 镜像包候选地址、下载 → 校验 → 安全解压（tar.gz / zip）→ 源码完整性校验 |
| `update_swap.go` | 变更计算、备份、原子替换（临时文件 + rename）、回滚、受保护路径 |
| `update_apply.go` | `Apply` / `Rollback` / 后台流程 `runUpdate`、阶段进度、历史、备份列表 |
| `update_apply_release.go` | **releases 更新源**执行流：`applyRelease` / `runUpdateRelease`（下载镜像包 → 清单 → 安装）、本机 `docker load` 安装、版本维度回滚 |
| `update_agent.go` | 宿主代理契约：`pendingManifest`（`pending-update.json`，`kind=source`/`release_image`）与 `RebuildResult`（`update-result.json`） |
| `update_rebuild.go` | 重建方式分发：`waiting_agent`（写清单）/ `docker`（compose build+up）/ `in_place`（拉起重建脚本） |
| `update_restart_unix.go` / `_windows.go` | 平台相关：`WaitForExit`（等旧进程退出）、`startDetachedRebuild`（脱离会话启动脚本） |

```go
Status() UpdateStatus                                    // 只读本地状态，不联网（前端轮询用）
Check(ctx) (UpdateStatus, error)                         // 联网比对上游最新提交 + 生成更新日志
Apply(ctx, target, operator) error                       // 启动后台更新任务（校验后立即返回）
Rollback(backupID, operator) error                       // 用备份还原源码
Backups() []BackupInfo                                   // 可回滚的备份点（最新在前）
AgentResult() *RebuildResult                             // 宿主代理写回的执行结果
```

**两个版本概念别混淆**：`data/deployed-commit.json` 是「上一次部署到哪一版」；
`GET /api/v1/system/info` 的 `commit` 是「现在跑的是哪一版」——
优先级 `-ldflags BuildCommit` > 部署记录 > `INKSTONE_COMMIT`。
`buildVersionInfo()` 会在部署记录等于 pending 目标时**自动收敛 pending**（说明新进程已起来）。

**安全边界**（`update_swap.go` 的 `guardedPrefixes` / `isGuardedPath`）：
替换源码时永不触碰 `data/`、`uploads/`、`files/`、`node_modules/`、`.git/`、`.update/`、
`.next/`、`.tools/` 与任何层级的 `.env*`。解压侧拒绝绝对路径、`..` 穿越、符号链接，
并限制文件数（2 万）与解压体积（512 MB）。

**并发约束**：`reportStage(func(st *UpdateStage))` 的闭包在**持锁**状态执行，
里面**只能改 `st` 的字段**；调用任何会加锁的方法（`Mode` / `rebuildMessage` / `snapshot`）会死锁。

### LinkService
- `StartAutoCheck()` — 启动后台协程，每 6 小时检测「超过 24 小时未检测」的友链
- `probeURL()` — HEAD 优先 → GET 回退，`status < 500` 视为可达
- `MaskURL()` — 失效链接脱敏（`https://exa****.com`）

### FileService
- `Save()` — 流式保存 + 上传限速（`CopyWithLimit`）
- `CopyWithLimit(dst, src, kbPerSec)` — 分块 + 时间片节流
- 上传大小限制由 `upload_max_mb` 设置控制

### EmailCodeService
- 内存存储（单实例），6 位数字，10 分钟过期，5 次错误锁定
- 邮件发送通过 `MailSender` 接口注入（**避免 service → mailer 循环依赖**）

### SitemapHandler（`handler/sitemap_handler.go`）
- `collectEntries()` — **单一数据源**：首页/友链/文章(已发布)/独立页(已发布)/分类(有文章)/标签(有文章)，
  返回带 `type/label/loc/lastmod/changefreq/priority` 的条目列表
- `Sitemap`（`GET /sitemap.xml`，公开）与 `SiteMapData`（`GET /api/v1/admin/sitemap`，管理员）共用它
- `robotsBody()` — robots.txt 内容（放行 crawling、屏蔽 /admin 与 /me、指向 sitemap）
- 后台「站点地图」页只读展示，无写操作；内容随文章/页面发布动态变化，无需手动重建

---

## 已知设计取舍

| 取舍 | 原因 |
|---|---|
| 验证码用内存存储 | 单实例部署够用；多实例需换 Redis |
| 流量统计用内存队列 | 避免阻塞请求；队列满时丢弃 |
| IP 存哈希不存原值 | 隐私合规 |
| 设置缓存 30 秒 | 减少 DB 查询；写操作立即失效缓存 |
| 限流计数器内存化 | 无 Redis 依赖；重启清空（可接受） |
| 在线更新的重建交给宿主代理 | 后端容器没有 docker 权限与 systemd，无法重建自身；后端只负责下载/校验/替换源码，重建由 `deploy/update-agent.sh` 完成 |
| 在线更新不引入 git 依赖 | 用镜像 tarball 直接替换源码，服务器无需 git / 无需外网到 GitHub（走 `UPDATE_MIRROR`） |
| 更新状态落盘（JSON）而非入库 | 更新要在数据库可用之前就能工作（且更新失败时不该污染业务表） |

---

## 常见修改场景

### 新增一个设置项
1. `settings_service.go` 加常量 + `settingDefaults` 默认值
2. 敏感字段加进 `maskKeys`
3. 需要 JSON 的加进 `jsonSettingKeys`
4. `Public()` 里自动下发（除非在 maskKeys）
5. **前端 `site-config-context.tsx` 手动解构**（嵌套对象要单独处理）
6. **前端设置页 payload 白名单手动加字段**

### 新增一个 API
见 `SKILL.md` 的「新增接口的标准流程」。

### 调试数据库
```bash
docker exec blog-postgres psql -U blog -d blog_platform -c "SELECT * FROM settings LIMIT 10;"
```
