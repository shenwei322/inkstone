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
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

**注意**：
- `Content` 存 **HTML**（不是 Markdown），前端用 `dangerouslySetInnerHTML` 渲染
- `Slug` 由 `repository.Slugify(title)` 生成（中文保留）
- `Cover` 后端 `resolveCover()` 自动处理：显式优先，否则提取正文首个 `<img src="...">`

## Category / Tag（分类与标签）

```go
type Category struct {
    ID   uint
    Name string   // 唯一索引
    Slug string   // 唯一索引
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

## Comment（评论）

```go
type Comment struct {
    ID        uint
    ArticleID uint       // 外键 → Article
    Article   *Article
    UserID    uint       // 外键 → User
    User      User
    ParentID  *uint      // 预留嵌套（当前 UI 未使用）
    Content   string     // text
    CreatedAt, UpdatedAt time.Time
}
```

**权限**：评论作者本人或管理员可删除（`CommentService.Delete(id, userID, isAdmin)`）。

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
