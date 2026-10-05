# 数据模型

GORM 结构体，PostgreSQL 存储。**新增模型必须加入 `internal/repository/db.go` 的 AutoMigrate**。

## User（用户）

```go
type User struct {
    ID           uint      // 主键
    Email        string    // 唯一索引，登录用
    Username     string    // 唯一索引
    PasswordHash string    // bcrypt，JSON 不输出
    Role         string    // "admin" | "user"
    Status       string    // "active" | "banned"
    TokenVersion int64     // 令牌代次，JSON 不输出
    CreatedAt    time.Time
    UpdatedAt    time.Time
}
```

**TokenVersion（令牌代次）**：签发令牌时写入 `ver` claim，`Refresh` 校验一致性。
改密码、管理员重置密码、封禁、改角色都会 `+1`（用 SQL 表达式 `token_version + 1` 自增，避免读-改-写竞态），
使该用户已签发的全部 access/refresh 立即失效。旧令牌无 `ver` 字段时视为 0，与默认值一致，升级不会误伤。

**关键行为**：
- **首个注册用户自动为 admin**（`auth_service.Register` 中判断 `Count() == 0`）
- 封禁用户登录被拒；已登录用户的旧 token 也会被 Auth 中间件实时拦截
- 管理员可改邮箱/用户名/密码；**不能改自己的角色、不能封禁/删除自己**

## Article（文章）

```go
type Article struct {
    ID          uint
    AuthorID    uint        // 外键 → User
    Author      User        // 预加载
    CategoryID  *uint       // 可空，外键 → Category
    Category    *Category
    Title       string
    Slug        string      // 唯一索引，由标题生成
    Content     string      // HTML（富文本）
    Status      string      // "draft" | "published"
    Cover       string      // 封面图 URL（空则前端取正文首图）
    Views       int64       // 浏览量
    PublishedAt *time.Time  // 首次发布时写入
    Tags        []Tag       // 多对多，中间表 article_tags
    Excerpt     string      // 作者手写摘要，留空 = 后端从正文生成
    IsPinned    bool        // 置顶（列表排最前；order=views 的热门榜不掺）
    ViewPassword string     // 访问密码的 bcrypt 哈希，空 = 不设密码（json:"-"）
    ScheduledAt *time.Time  // 定时发布时间，仅 status=scheduled 时有效
    HasPassword bool        // 查询期计算的布尔值，不落库（gorm:"-"）
    // 软删除：删除只是置 deleted_at，进入回收站可还原；
    // 彻底删除走 Unscoped()。recycle bin 与备份都要能覆盖到。
    DeletedAt gorm.DeletedAt
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

**文章状态**：`draft` / `published` / `scheduled`。
`scheduled` 与 draft 的区别是「已决定发布、只等时间到」，后台列表据此区分
「还没写完」和「等发布中」。`scheduled_at` 传过去时刻会被按已发布处理。

**注意**：
- `Content` 存 **HTML**（不是 Markdown），前端用 `dangerouslySetInnerHTML` 渲染
- `Slug` 由 `repository.Slugify(title)` 生成（中文保留）
- `Cover` 后端 `resolveCover()` 自动处理：显式优先，否则提取正文首个 `<img src="...">`
- **软删除后关联不清空**：还原时评论/点赞必须还在。彻底清关联只发生在 `Purge`
- **密码保护必须守住 5 个出口**，漏一个就白设：`GET /slug/:slug`、
  `GET /:id`、列表的 `content`、RSS 全文，且作者本人免密。详见 api.md

## ArticleRevision（文章历史版本）

```go
type ArticleRevision struct {
    ID         uint
    ArticleID  uint      // 外键 → Article
    Version    int       // 文章内版本号，从 1 递增
    Title      string
    Content    string    // 完整正文，不是摘要
    Excerpt    string
    EditorID   uint      // 这次改动的作者（与 AuthorID 分开：管理员代改要能追溯）
    ChangeNote string    // 改动说明，可空
    CreatedAt  time.Time
}
```

**保留上限**：`model.MaxRevisionsKept = 50`，超出删最旧的。
每版都存全文，一篇常改的文章几年下来能攒出几十 MB。

**写入时机**：每次 `Update` 保存**前**（此时 `article` 还是旧内容，
之后就被覆盖）。草稿阶段同样留版——「只在发布时记一版」会让发布前的
所有编辑无从追溯。内容与标题都没变时不留（自动保存会周期性触发）。

## PowChallenge（POW 人机验证挑战）

```go
type PowChallenge struct {
    Challenge  string    // 主键：64 位十六进制随机串
    Difficulty int       // 签发时刻的参数快照
    MemMB      int       // 后台随时调参数只影响之后签发的新挑战
    Rounds     int
    MinEvents  int
    Scene      string    // 签发场景（login/register/comment），空 = 未绑定
    IssuedAt   time.Time // 签发时间
    TTLSeconds int       // 与该挑战绑定的有效期
}
```

**为什么必须落库**：challenge 跨请求存活（前端领完要真算几百毫秒到几秒），
多副本部署下进程 A 签发的挑战很可能被负载均衡分到进程 B。内存 map 时
B 必然查不到，用户会反复看到「验证已失效」——错误指向客户端，根因在拓扑。

**存 `IssuedAt` + `TTLSeconds` 而不是算好的绝对过期时刻**：
后台调整 TTL 时，已签发的挑战应仍按各自签发时的 TTL 判定。

**过期判定**：`IssuedAt.Add(TTLSeconds).Before(now)`。
方向反了（写成 "签发时间晚于当前时刻"）会让所有新挑战直接失效。

## EmailCode（邮箱验证码）

```go
type EmailCode struct {
    Email     string    // 主键之一
    Purpose   string    // 主键之一：login / register / reset_password
    CodeHash  string    // SHA-256，不存明文
    Attempts  int       // 累计错误次数，达上限作废
    ExpiresAt time.Time // 算好的绝对过期时刻
    SentAt    time.Time // 实际发信时刻（重发间隔判断用）
}
```

**主键是 (email, purpose) 而非 email**：同一邮箱可能并行发起「登录」
与「重置密码」两个流程。只按 email 作主键时，后申请的那枚会顶掉前一枚，
用户拿着先收到的那枚来校验必然失败。

**存 SHA-256 而非 bcrypt**：6 位数字空间只有 10^6，慢哈希挡不住枚举。
真正的防护是 `Attempts`（上限 5，把成本推回邮件通道）与 IP 限流。

**存绝对过期时刻而非 duration**：验证码的 TTL 会随后台设置改变，
但已发出的码必须始终按发出时的有效期判定。

`repository.MaxEmailCodeAttempts` 是尝试上限，由 SQL 在 Consume 里判定，
所以常量与实现同层——service 只是引用它。

## Category / Tag（分类与标签）

```go
type Category struct {
    ID        uint
    Name      string   // 唯一索引
    Slug      string   // 唯一索引
    ParentID  *uint    // 层级（树形），nil = 顶级
    Parent    *Category
    Children  []Category
    CreatedAt, UpdatedAt time.Time
}

type Tag struct {
    ID        uint
    Name      string    // 唯一索引
    Slug      string    // 唯一索引
    Articles  []Article // 多对多（json:"-"）
    CreatedAt, UpdatedAt time.Time
}
```

**中间表**：`article_tags(article_id, tag_id)`

**自动创建**：文章提交 `tags: ["Go", "后端"]` 时，`FindOrCreateTags` 按 slug 查找，不存在则创建。

**分类层级上限**：`model.MaxCategoryDepth = 3`（顶级 + 两级子分类）。限制理由：更深的层级会让后台选择器与前台面包屑都难用。`TaxonomyRepository.UpdateCategory` 会拒绝三种破坏性改法——父级指向自己、父级指向自己的后代、父级本身还有父级。

**树构建位置**：在 `service.TaxonomyService.CategoryTree()`（纯内存运算、可单测），不在 repository。脏数据（`parent_id` 悬空或成环）一律提升为顶级，保证节点不会从列表里消失。

## Comment（评论）

```go
type Comment struct {
    ID        uint
    ArticleID uint       // 外键 → Article
    Article   *Article
    UserID    *uint      // 外键 → User；nil = 游客评论（见下）
    User      *User
    GuestName  string    // 游客昵称（必填，≤64）
    GuestEmail string    // 游客邮箱（选填，json:"-" 不公开）
    GuestURL   string    // 游客个人网站（选填，公开展示）
    ParentID  *uint      // 被回复的评论，nil = 顶级
    Parent    *Comment   // 自引用关联，Preload("Parent") 用
    Content   string     // text
    Status    string     // "pending" | "approved" | "rejected"，默认 approved
    IP        string     // 操作者 IP（json:"-"），审核时追溯用
    CreatedAt, UpdatedAt time.Time
}
```

**游客评论**：`UserID` 可空以支持未登录访客发表评论。

- 为什么是 `*uint` 而不是「0 表示游客」：`comments.user_id` 有指向 `users` 的外键，写 0 会去撞不存在的 `id=0` 用户，INSERT 直接失败（23503）。NULL 才是「无关联」的正确表达。
- **升级不需要手写 SQL**：从 `uint` 改成 `*uint` 时，AutoMigrate 会真的去掉 NOT NULL 约束。链路是 `MigrateColumn`（当前列 nullable=false，`field.NotNull`=false，`nullable == field.NotNull` 成立）→ `alterColumn=true` → postgres driver 执行 `ALTER TABLE ... DROP NOT NULL`。已部署实例直接升级即可。
- 三个游客列只在 `UserID == nil` 时有值；登录用户即便请求体带了 `guest_*` 也会被 `normalizeIdentity` 清空，不允许两套身份并存。
- 判定与展示统一走模型方法，**不要在调用方各写一套**：`IsGuest()`（是否游客）、`AuthorID()`（游客返回 0）、`DisplayName()`（登录用户取用户名 / 游客取昵称 / 缺失时回落「匿名访客」）。
- `GuestEmail` 是 `json:"-"`，只由 `toCommentResponseAdmin` 手动塞进后台响应；公开出口（`toCommentResponse`）一律不含邮箱与 IP。

**权限**：评论作者本人或管理员可删除（`CommentService.Delete(id, userID, isAdmin)`）。
游客评论**只有管理员能删**——游客无法证明"这条是我发的"，而 `AuthorID()` 的 0 与未登录请求的 userID 0 相等，必须先 `IsGuest()` 短路再比 ID。

**审核状态**（`model.CommentPending/Approved/Rejected`）：
- `approved` 是默认，直接公开
- `pending` 由三个条件触发：后台开了 `comment_audit`、内容命中敏感词表 `comment_words`、**或这是一条游客评论且 `guest_comment_free` 未开启**（默认未开启 → 游客评论一律先审后发）
- **敏感词命中不直接拒绝**：拒绝会向刷评者暴露"这个词被拦了"，换写法即可绕过；转人工审核同样挡得住内容，且不留信号
- 公开列表只返回 `approved` + **当前登录用户自己**的 `pending`（否则作者以为评论丢了会重复提交）。游客没有账号，因此看不到自己的 pending，前端在提交后提示"等待审核"而不回显

**嵌套回复**：`POST /articles/:id/comments` 接收 `parent_id`。跨文章回复会被拒绝（会把两条无关讨论拼在一起）。删除父评论时，回复的 `parent_id` 置空——提升为顶级，内容不丢。


## Reaction（点赞/收藏）

```go
type ReactionType string
const (
    ReactionLike     ReactionType = "like"
    ReactionFavorite ReactionType = "favorite"
)

type Reaction struct {
    UserID    uint          // 复合主键
    ArticleID uint          // 复合主键
    Type      ReactionType  // 复合主键
    CreatedAt time.Time
}
```

**设计**：复合主键 `(user_id, article_id, type)` 天然防重复。`Toggle()` 存在则删、不存在则建。

## Page（独立页面）

```go
type Page struct {
    ID        uint
    Title     string
    Slug      string    // 唯一索引
    Content   string    // HTML
    Template  string    // "default" | "fullwidth" | "landing"
    Status    string    // "draft" | "published"
    SortOrder int
    ShowInNav bool
    CreatedAt, UpdatedAt time.Time
}
```

**模板**：
- `default` — 居中文章式
- `fullwidth` — 通栏大标题
- `landing` — 整页 HTML（`{{content}}` 为内容占位符）

## FriendLink（友情链接）

```go
type FriendLink struct {
    ID            uint
    Name          string
    URL           string     // 实际跳转地址
    CheckURL      string     // 检测地址（空则用 URL）
    IconURL       string     // 图标
    Description   string
    SortOrder     int
    Available     bool       // 最近检测是否可达
    LastCheckedAt *time.Time // 最后检测时间
    CreatedAt, UpdatedAt time.Time
}
```

**检测机制**：后台每 6 小时巡检 `LastCheckedAt` 超过 24 小时的链接。失效时公开接口**不返回 `url`**，只给 `masked_url`。

## FileAsset（文件管理）

```go
type FileAsset struct {
    ID           uint
    StoredName   string    // 随机存储名（唯一索引）
    OriginalName string    // 用户原始文件名
    Size         int64
    MimeType     string
    UploaderID   uint
    CreatedAt    time.Time
}
```

**存储**：`data/files/` 目录，静态路由 `/files/<stored_name>` 直接访问。

## Setting（设置键值）

```go
type Setting struct {
    Key       string    // 主键
    Value     string    // text（JSON 数组也序列化存这里）
    UpdatedAt time.Time
}
```

详见 `settings.md`。

## DailyStat / VisitorDay（流量统计）

```go
type DailyStat struct {
    Date      string    // "YYYY-MM-DD" 主键
    PageViews int64     // PV
    Visitors  int64     // UV（当日去重）
    BytesIn   int64     // 上传流量
    BytesOut  int64     // 下载流量
    UpdatedAt time.Time
}

type VisitorDay struct {
    Date   string  // 复合主键
    IPHash string  // 复合主键（IP 的 SHA256 哈希，不存原值）
}
```

> 隐私：IP 只存哈希，90 天后自动清理。

## OperationLog（操作日志，Beta1.12 增强）

```go
type OperationLog struct {
    ID        uint
    UserID    uint      // 操作者（登录失败等匿名场景为 0）
    Username  string    // 操作者用户名（固化快照，用户改名/删除后仍可追溯）
    Category  string    // auth/article/user/comment/setting/file/link/page/taxonomy/system/other
    Action    string    // 操作名，如「创建文章」「封禁用户」
    Detail    string    // 详情（截断 500 字符）；设置类只记 key 名，绝口不提值
    IP        string    // 客户端 IP
    UserAgent string    // UA（截断 250 字符）
    Success   bool      // 失败操作也留痕
    CreatedAt time.Time
}
```

**行为**：异步入队（1024 缓冲，满丢弃）→ worker 落库；每 24h 清理 90 天前记录。
后台页面 `/admin/logs`，导出走 `GET /admin/logs/export`（CSV，UTF-8 BOM，上限 5 万条）。

---

## 关系图

```
User ──1:N──> Article ──N:1──> Category
  │              │  │
  │              │  └──N:M──> Tag (article_tags)
  │              │
  ├──1:N──> Comment ──N:1──> Article
  │
  ├──N:M──> Reaction ──N:1──> Article
  │
  └──1:N──> FileAsset

Page          （独立，无外键）
FriendLink    （独立）
Setting       （键值对）
DailyStat / VisitorDay（统计）
```

---

## 数据库操作常用命令

```bash
# 进入数据库
docker exec -it blog-postgres psql -U blog -d blog_platform

# 常用查询
SELECT id, username, role, status FROM users;
SELECT id, title, status, views FROM articles LIMIT 10;
SELECT key, LEFT(value, 30) FROM settings;

# 提权为管理员
UPDATE users SET role='admin' WHERE email='xxx@example.com';

# 清空某用户文章
DELETE FROM articles WHERE author_id = <id>;

# 备份
docker exec blog-postgres pg_dump -U blog blog_platform > backup.sql
```
