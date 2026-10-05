export interface User {
  id: number
  email: string
  username: string
  role: 'admin' | 'user'
  /** 是否开启两步验证（TOTP）。后端已支持，登录时需要额外提交验证码 */
  totp_enabled?: boolean
}

export interface TokenPair {
  access_token: string
  refresh_token: string
  expires_in: number
}

export interface AuthResponse {
  user: User
  token: TokenPair
}

export interface ArticleAuthor {
  id: number
  username: string
}

export interface TaxonomyItem {
  id: number
  name: string
  slug: string
}

export interface Article {
  id: number
  title: string
  slug: string
  content: string
  /** draft=作者主动不发布；published=已发布；scheduled=已决定发布、只等时间到 */
  status: 'draft' | 'published' | 'scheduled'
  cover?: string
  views: number
  category?: TaxonomyItem | null
  tags?: TaxonomyItem[]
  published_at: string | null
  created_at: string
  updated_at: string
  author: ArticleAuthor
  /** 作者手写的摘要；留空时后端会在响应里用正文现算一份返回 */
  excerpt?: string
  /** 是否置顶（仅管理员可设置）；列表排序时排在同批文章最前 */
  is_pinned?: boolean
  /** 定时发布时间（RFC3339）；仅 status='scheduled' 时有效，其余情况为 null */
  scheduled_at?: string | null
  /**
   * 是否设了访问密码。只表示「设没设」——任何响应都不会返回明文密码，
   * 明文只存在于创建/更新文章的请求体 view_password 字段里。
   */
  has_password?: boolean
}

export interface CategoryCount extends TaxonomyItem {
  article_count: number
}

export interface TagCount extends TaxonomyItem {
  article_count: number
}

export interface CommentItem {
  id: number
  article_id: number
  article_title?: string
  article_slug?: string
  /** 父评论 id：null/undefined 表示顶级评论，非空表示回复某条评论（楼中楼） */
  parent_id?: number | null
  /** 父评论作者展示名：用于「回复 @某某」，后端已算好游客/登录两种情况 */
  parent_author?: string
  content: string
  created_at: string
  /** 审核状态与游客邮箱、IP 只在后台接口下发（公开接口不含） */
  status?: 'pending' | 'approved' | 'rejected'
  guest_email?: string
  ip?: string
  author: {
    id: number
    /** 展示名：登录用户是用户名，游客是填写的昵称 */
    username: string
    /** 是否为游客评论：游客的 id 也是 0，不能用 id===0 代替此判断 */
    is_guest?: boolean
    /** 游客填写的个人网站（仅游客、且填写了才有） */
    url?: string
  }
}

/** 游客发表评论时提交的身份信息 */
export interface GuestCommentInput {
  guest_name: string
  guest_email?: string
  guest_url?: string
}

export interface ReactionStats {
  likes: number
  favorites: number
  liked?: boolean
  favorited?: boolean
}

export interface SiteSettings {
  allow_registration: boolean
  site_name: string
  site_description: string
  site_logo?: string
  site_favicon?: string
  sidebar_position?: 'right' | 'left'
  site_icp: string
  smtp_host: string
  smtp_port: string
  smtp_user: string
  smtp_from: string
  smtp_pass_set?: boolean
  upload_max_mb?: number
  upload_speed_kb?: number
  download_speed_kb?: number
  security_enabled?: boolean
  security_api_max?: number
  security_login_max?: number
  security_register_max?: number
  security_comment_max?: number
  security_block_minutes?: number
  email_code_on_register?: string
  email_code_on_login?: string
  geetest_enabled?: boolean
  geetest_captcha_id?: string
  geetest_captcha_key?: string
  geetest_captcha_key_set?: boolean
  geetest_on_login?: boolean
  geetest_on_register?: boolean
  geetest_on_comment?: boolean
  captcha_provider?: string
  lap_enabled?: boolean
  lap_api_endpoint?: string
  lap_site_key?: string
  lap_secret_key_set?: boolean
  lap_resolve_ip?: string
  lap_http_proxy?: string
  lap_on_login?: boolean
  lap_on_register?: boolean
  lap_on_comment?: boolean
  site_wallpaper?: string
  wallpaper_opacity?: string
  wallpaper_blur?: string
  article_sidebar?: string
  maintenance_mode?: boolean
  /** 游客（未登录访客）评论总开关 */
  guest_comment?: boolean
  /** 游客评论是否免于先审后发；false 时进待审核队列 */
  guest_comment_free?: boolean
  /** 游客评论是否必须填写邮箱 */
  guest_comment_email?: boolean
}

export interface ArticleListResponse {
  articles: Article[]
  total: number
  page: number
  page_size: number
}

export interface AdminStats {
  total_users: number
  total_articles: number
  published_articles: number
  draft_articles: number
}

export interface AdminUser {
  id: number
  email: string
  username: string
  role: 'admin' | 'user'
  status: 'active' | 'banned'
  created_at: string
  /**
   * 登录失败锁定的解除时刻（ISO 时间串），null 表示未锁定。
   *
   * 注意：后端 `GET /admin/users` 目前**没有**下发这个字段——
   * `internal/handler/admin_handler.go` 的 ListUsers 用显式 gin.H 白名单
   * 逐字段构造响应，其中没有 locked_until；而 `model.User.LockedUntil`
   * 的 json 标签是 `-`（不下发）。因此列表页现在拿不到它，解锁按钮不会出现。
   * 这里按「可选字段」声明，后端一旦补上即可生效，无需再改类型。
   * 已能拿到该字段的地方是 `GET /auth/me`（见 handler.toUserResponse）。
   */
  locked_until?: string | null
}

export interface AdminUserListResponse {
  users: AdminUser[]
  total: number
  page: number
  page_size: number
}

// ---------- 友链自助申请 ----------
export interface LinkApplication {
  id: number
  site_name: string
  url: string
  description: string
  icon_url: string
  email: string
  status: 'pending' | 'approved' | 'rejected'
  reason: string
  reviewed_by: number
  reviewed_at: string | null
  created_at: string
}

export interface SubmitLinkApplicationInput {
  site_name: string
  url: string
  description?: string
  icon_url?: string
  email?: string
}

/** 后台站点地图：单条 URL 记录 */
export interface SitemapEntry {
  type: 'home' | 'article' | 'page' | 'category' | 'tag'
  label: string
  loc: string
  lastmod?: string
  changefreq?: string
  priority?: string
}

/** 后台站点地图：分组（基础页面/文章/独立页/分类/标签） */
export interface SitemapGroup {
  type: string
  label: string
  count: number
  entries: SitemapEntry[]
}

/** 后台站点地图：整体数据（GET /admin/sitemap） */
export interface SitemapData {
  frontend_url: string
  sitemap_url: string
  robots: string
  groups: SitemapGroup[]
  total: number
}
