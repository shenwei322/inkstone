import type {
  Article,
  ArticleListResponse,
  AuthResponse,
  User,
  AdminStats,
  AdminUserListResponse,
  CategoryCount,
  TagCount,
  CommentItem,
  ReactionStats,
  SiteSettings,
  LinkApplication,
  SubmitLinkApplicationInput,
  SitemapData,
} from './types'

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080/api/v1'

const ACCESS_KEY = 'blog_access_token'
const REFRESH_KEY = 'blog_refresh_token'

export function getAccessToken(): string | null {
  if (typeof window === 'undefined') return null
  return localStorage.getItem(ACCESS_KEY)
}

export function saveTokens(access: string, refresh: string) {
  localStorage.setItem(ACCESS_KEY, access)
  localStorage.setItem(REFRESH_KEY, refresh)
}

export function clearTokens() {
  localStorage.removeItem(ACCESS_KEY)
  localStorage.removeItem(REFRESH_KEY)
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

interface RequestOptions {
  method?: string
  body?: unknown
  auth?: boolean
}

let refreshPromise: Promise<boolean> | null = null

export async function tryRefresh(): Promise<boolean> {
  if (typeof window === 'undefined') return false
  const refresh = localStorage.getItem(REFRESH_KEY)
  if (!refresh) return false

  // Coalesce concurrent refresh attempts into a single request.
  if (!refreshPromise) {
    refreshPromise = (async () => {
      try {
        const res = await fetch(`${API_BASE}/auth/refresh`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ refresh_token: refresh }),
        })
        if (!res.ok) return false
        const data = (await res.json()) as AuthResponse
        saveTokens(data.token.access_token, data.token.refresh_token)
        return true
      } catch {
        return false
      } finally {
        refreshPromise = null
      }
    })()
  }
  return refreshPromise
}

export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = 'GET', body, auth = false } = options

  const doFetch = async (token: string | null) => {
    const headers: Record<string, string> = {}
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    if (token) headers['Authorization'] = `Bearer ${token}`
    return fetch(`${API_BASE}${path}`, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
  }

  let token = auth ? getAccessToken() : null
  let res = await doFetch(token)

  // On 401 with auth, attempt one token refresh then retry.
  if (res.status === 401 && auth) {
    const refreshed = await tryRefresh()
    if (refreshed) {
      token = getAccessToken()
      res = await doFetch(token)
    } else {
      clearTokens()
    }
  }

  if (!res.ok) {
    let message = `请求失败 (${res.status})`
    try {
      const data = await res.json()
      if (data && typeof data.error === 'string') message = data.error
    } catch {
      // keep default message
    }
    throw new ApiError(res.status, message)
  }

  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

// ---------- Email Code API ----------

export interface EmailCodeConfig {
  on_register: boolean
  on_login: boolean
}

/** 极验第四代人机验证：前台配置（通过 /site-config 下发，不含密钥） */
export interface GeetestConfig {
  enabled: boolean
  on_login: boolean
  on_register: boolean
  on_comment: boolean
  captcha_id: string
}

/** 极验验证通过后的凭证，随登录/注册/评论提交给后端做二次校验 */
export interface GeetestCredential {
  lot_number: string
  captcha_output: string
  pass_token: string
  gen_time: string
}

/** Lap（Cap 的 Cloudflare Workers 分支）工作量证明验证码：前台配置（含 siteKey 与实例地址，不含密钥） */
export interface LapConfig {
  enabled: boolean
  on_login: boolean
  on_register: boolean
  on_comment: boolean
  site_key: string
  api_endpoint: string
}

/** POW（自研工作量证明）人机验证：前台配置（零外部依赖，无任何密钥） */
export interface PowConfig {
  enabled: boolean
  on_login: boolean
  on_register: boolean
  on_comment: boolean
  /** 难度：答案哈希前导零个数（十六进制位） */
  difficulty: number
  /** 挑战有效期（秒） */
  ttl_seconds: number
  /** 内存表大小（MB）：每次验证在本地实打实占用/访问的内存 */
  memory_mb: number
  /** 表查找-混合轮数 */
  rounds: number
  /** 需采集的本地交互事件数（鼠标/触摸/按键；0=不校验） */
  min_events: number
}

/** POW 挑战签发响应（POST /api/v1/pow/challenge，参数为签发时快照） */
export interface PowChallenge {
  challenge: string
  difficulty: number
  memory_mb: number
  rounds: number
  min_events: number
  ttl_seconds: number
}

/**
 * 人机验证凭证：按 captcha_provider 提交不同字段——
 * geetest 四元组 / lap_token / pow_challenge+pow_nonce+pow_signal（三选一），
 * 随登录/注册/评论提交给后端做二次校验。
 */
export type CaptchaCredential = Partial<GeetestCredential> & {
  lap_token?: string
  pow_challenge?: string
  pow_nonce?: string
  pow_signal?: string
}


/** 签发 POW 挑战（免登录）：拿到后本地计算满足难度的 nonce */
export function fetchPowChallenge() {
  return api<PowChallenge>('/pow/challenge', { method: 'POST' })
}

/** 发送邮箱验证码 */
export function sendEmailCode(email: string, purpose: 'register' | 'login') {
  return api<{ message: string }>('/auth/email-code', {
    method: 'POST',
    body: { email, purpose },
  })
}

// ---------- Auth API ----------

export function register(
  email: string,
  username: string,
  password: string,
  extra?: { email_code?: string } & CaptchaCredential,
) {
  return api<AuthResponse>('/auth/register', {
    method: 'POST',
    body: { email, username, password, ...extra },
  })
}

export function login(
  email: string,
  password: string,
  extra?: { email_code?: string } & CaptchaCredential,
) {
  return api<AuthResponse>('/auth/login', {
    method: 'POST',
    body: { email, password, ...extra },
  })
}

export function fetchMe() {
  return api<{ user: User }>('/auth/me', { auth: true })
}

// ---------- Articles API ----------

export interface ArticleListParams {
  page?: number
  page_size?: number
  status?: string
  author_id?: number
  category?: string
  tag?: string
  q?: string
  order?: string
}

export function fetchArticles(params: ArticleListParams = {}) {
  const search = new URLSearchParams()
  if (params.page) search.set('page', String(params.page))
  if (params.page_size) search.set('page_size', String(params.page_size))
  if (params.status) search.set('status', params.status)
  if (params.author_id) search.set('author_id', String(params.author_id))
  if (params.category) search.set('category', params.category)
  if (params.tag) search.set('tag', params.tag)
  if (params.q) search.set('q', params.q)
  if (params.order) search.set('order', params.order)
  const qs = search.toString()
  return api<ArticleListResponse>(`/articles${qs ? `?${qs}` : ''}`, {
    auth: Boolean(params.status && params.status !== 'published'),
  })
}

export function fetchArticle(id: number | string) {
  return api<{ article: Article }>(`/articles/${id}`, { auth: true })
}

export function fetchArticleBySlug(slug: string) {
  return api<{ article: Article }>(`/articles/slug/${encodeURIComponent(slug)}`)
}

export function createArticle(input: {
  title: string
  content: string
  status: string
  category_id?: number | null
  tags?: string[]
  cover?: string
}) {
  return api<{ article: Article }>('/articles', { method: 'POST', body: input, auth: true })
}

export function updateArticle(
  id: number,
  input: Partial<{
    title: string
    content: string
    status: string
    category_id: number | null
    tags: string[]
    cover: string
  }>,
) {
  return api<{ article: Article }>(`/articles/${id}`, { method: 'PUT', body: input, auth: true })
}

// ---------- Engagement API ----------

export function fetchCategories() {
  return api<{ categories: CategoryCount[] }>('/categories')
}

export function fetchTags() {
  return api<{ tags: TagCount[] }>('/tags')
}

// ---------- Admin Tags API ----------

export function createTag(name: string) {
  return api<{ tag: TagCount }>('/admin/tags', { method: 'POST', body: { name }, auth: true })
}

export function updateTag(id: number, name: string) {
  return api<{ tag: TagCount }>(`/admin/tags/${id}`, { method: 'PUT', body: { name }, auth: true })
}

export function deleteTag(id: number) {
  return api<void>(`/admin/tags/${id}`, { method: 'DELETE', auth: true })
}

export function fetchComments(articleId: number | string) {
  return api<{ comments: CommentItem[] }>(`/articles/${articleId}/comments`)
}

export function postComment(
  articleId: number | string,
  content: string,
  extra?: CaptchaCredential,
) {
  return api<{ comment: CommentItem }>(`/articles/${articleId}/comments`, {
    method: 'POST',
    body: { content, ...extra },
    auth: true,
  })
}

export function deleteComment(id: number) {
  return api<void>(`/comments/${id}`, { method: 'DELETE', auth: true })
}

export function fetchReactions(articleId: number | string) {
  return api<ReactionStats>(`/articles/${articleId}/reactions`, { auth: true })
}

export function toggleReaction(articleId: number | string, type: 'like' | 'favorite') {
  return api<{ active: boolean; count: number }>(`/articles/${articleId}/reactions`, {
    method: 'POST',
    body: { type },
    auth: true,
  })
}

// ---------- Admin API ----------

export type { AdminStats, AdminUser, AdminUserListResponse } from './types'

export function fetchAdminStats() {
  return api<AdminStats>('/admin/stats', { auth: true })
}

// ---------- Traffic & System Stats ----------

export interface TrendPoint {
  date: string
  page_views: number
  visitors: number
  bytes_in: number
  bytes_out: number
}

export interface SystemResource {
  cpu_percent: number
  mem_used_mb: number
  mem_total_mb: number
  mem_percent: number
  goroutines: number
  uptime_seconds: number
  app_mem_mb: number
}

export function fetchTrafficTrend(days = 30) {
  return api<{ points: TrendPoint[]; days: number }>(`/admin/stats/traffic?days=${days}`, {
    auth: true,
  })
}

export function fetchSystemResources() {
  return api<SystemResource>('/admin/stats/resources', { auth: true })
}

// ---------- Operation Logs API ----------

export interface OperationLog {
  id: number
  user_id: number
  username: string
  category: string
  action: string
  detail: string
  ip: string
  user_agent: string
  success: boolean
  created_at: string
}

export interface LogListResponse {
  logs: OperationLog[]
  total: number
  page: number
  page_size: number
}

export interface LogOverview {
  total: number
  today: number
  failed: number
  by_category: Record<string, number>
}

export interface LogQueryParams {
  page?: number
  page_size?: number
  category?: string
  username?: string
  q?: string
  from?: string
  to?: string
  success?: boolean
}

function buildLogQuery(params: LogQueryParams) {
  const search = new URLSearchParams()
  if (params.page) search.set('page', String(params.page))
  if (params.page_size) search.set('page_size', String(params.page_size))
  if (params.category) search.set('category', params.category)
  if (params.username) search.set('username', params.username)
  if (params.q) search.set('q', params.q)
  if (params.from) search.set('from', params.from)
  if (params.to) search.set('to', params.to)
  if (params.success !== undefined) search.set('success', String(params.success))
  return search.toString()
}

export function fetchLogs(params: LogQueryParams = {}) {
  const qs = buildLogQuery(params)
  return api<LogListResponse>(`/admin/logs${qs ? `?${qs}` : ''}`, { auth: true })
}

export function fetchLogOverview() {
  return api<{ overview: LogOverview }>('/admin/logs/overview', { auth: true })
}

// downloadLogs 按当前筛选条件导出 CSV 日志（401 时自动刷新 token 重试一次）。
export async function downloadLogs(params: LogQueryParams = {}): Promise<Blob> {
  const doFetch = async (token: string | null) => {
    const headers: Record<string, string> = {}
    if (token) headers['Authorization'] = `Bearer ${token}`
    const qs = buildLogQuery(params)
    return fetch(`${API_BASE}/admin/logs/export${qs ? `?${qs}` : ''}`, { headers })
  }

  let res = await doFetch(getAccessToken())
  if (res.status === 401) {
    if (await tryRefresh()) {
      res = await doFetch(getAccessToken())
    } else {
      clearTokens()
    }
  }
  if (!res.ok) {
    throw new ApiError(res.status, '导出日志失败')
  }
  return res.blob()
}

export function fetchAdminComments(params: { page?: number; page_size?: number } = {}) {
  const search = new URLSearchParams()
  if (params.page) search.set('page', String(params.page))
  if (params.page_size) search.set('page_size', String(params.page_size))
  const qs = search.toString()
  return api<{ comments: CommentItem[]; total: number }>(`/admin/comments${qs ? `?${qs}` : ''}`, {
    auth: true,
  })
}

export function deleteAdminComment(id: number) {
  return api<void>(`/admin/comments/${id}`, { method: 'DELETE', auth: true })
}

// ---------- Uploads API ----------

export async function uploadImage(file: File): Promise<string> {
  const form = new FormData()
  form.append('file', file)
  const token = getAccessToken()
  const headers: Record<string, string> = {}
  if (token) headers['Authorization'] = `Bearer ${token}`
  const res = await fetch(`${API_BASE}/uploads`, { method: 'POST', headers, body: form })
  if (!res.ok) {
    let message = `上传失败 (${res.status})`
    try {
      const data = await res.json()
      if (data && typeof data.error === 'string') message = data.error
    } catch {
      // keep default message
    }
    throw new ApiError(res.status, message)
  }
  const data = (await res.json()) as { url: string }
  return data.url
}

// ---------- Pages API ----------

export interface PageItem {
  id: number
  title: string
  slug: string
  content?: string
  template?: string
  status?: string
  sort_order?: number
  show_in_nav?: boolean
}

export function fetchPageBySlug(slug: string) {
  return api<{ page: PageItem }>(`/pages/${encodeURIComponent(slug)}`)
}

export function fetchAdminPages() {
  return api<{ pages: PageItem[] }>('/admin/pages', { auth: true })
}

export function fetchAdminPage(id: number) {
  return api<{ page: PageItem }>(`/admin/pages/${id}`, { auth: true })
}

export function createPage(input: Partial<PageItem>) {
  return api<{ page: PageItem }>('/admin/pages', { method: 'POST', body: input, auth: true })
}

export function updatePage(id: number, input: Partial<PageItem>) {
  return api<{ page: PageItem }>(`/admin/pages/${id}`, { method: 'PUT', body: input, auth: true })
}

export function deletePage(id: number) {
  return api<void>(`/admin/pages/${id}`, { method: 'DELETE', auth: true })
}

// ---------- Friend Links API ----------

export interface PublicFriendLink {
  id: number
  name: string
  url?: string
  masked_url: string
  icon_url?: string
  description?: string
  available: boolean
}

export interface AdminFriendLink {
  id: number
  name: string
  url: string
  check_url: string
  icon_url: string
  description: string
  sort_order: number
  available: boolean
  last_checked_at: string | null
  created_at: string
}

export interface FriendLinkInput {
  name: string
  url: string
  check_url?: string
  icon_url?: string
  description?: string
  sort_order?: number
}

export function fetchFriendLinks() {
  return api<{ links: PublicFriendLink[] }>('/links')
}

export function fetchAdminLinks() {
  return api<{ links: AdminFriendLink[] }>('/admin/links', { auth: true })
}

/** 添加/编辑友链前的预检：站点可达性 + 是否含本站反链 */
export interface LinkValidation {
  reachable: boolean
  status_code: number
  has_backlink: boolean
  backlink_host: string
  expected_hosts: string
  message: string
}

export function validateFriendLink(input: { url: string; check_url?: string }) {
  return api<{
    reachable: boolean
    status_code: number
    has_backlink: boolean
    backlink_host: string
    expected_hosts: string
    message: string
  }>('/admin/links/validate', { method: 'POST', body: input, auth: true })
}

export function createFriendLink(input: FriendLinkInput) {
  return api<{ link: AdminFriendLink }>('/admin/links', { method: 'POST', body: input, auth: true })
}

export function updateFriendLink(id: number, input: FriendLinkInput) {
  return api<{ link: AdminFriendLink }>(`/admin/links/${id}`, { method: 'PUT', body: input, auth: true })
}

export function deleteFriendLink(id: number) {
  return api<void>(`/admin/links/${id}`, { method: 'DELETE', auth: true })
}

export function checkAllFriendLinks() {
  return api<{ links: AdminFriendLink[]; checked: number }>('/admin/links/check', {
    method: 'POST',
    auth: true,
  })
}

export function checkFriendLink(id: number) {
  return api<{ available: boolean }>(`/admin/links/${id}/check`, { method: 'POST', auth: true })
}

// ---------- File Manager API ----------

export interface FileAssetItem {
  id: number
  stored_name: string
  original_name: string
  size: number
  mime_type: string
  url: string
  created_at: string
}

export interface FileListResponse {
  files: FileAssetItem[]
  total: number
  page: number
  page_size: number
  total_size: number
  max_upload_mb: number
}

export function fetchFiles(params: { page?: number; page_size?: number; q?: string } = {}) {
  const search = new URLSearchParams()
  if (params.page) search.set('page', String(params.page))
  if (params.page_size) search.set('page_size', String(params.page_size))
  if (params.q) search.set('q', params.q)
  const qs = search.toString()
  return api<FileListResponse>(`/admin/files${qs ? `?${qs}` : ''}`, { auth: true })
}

export function deleteFile(id: number) {
  return api<void>(`/admin/files/${id}`, { method: 'DELETE', auth: true })
}

/** 后台：站点地图数据（分组 URL 列表 + robots.txt 预览 + 统计） */
export function fetchSitemapData() {
  return api<{ data: SitemapData }>('/admin/sitemap', { auth: true })
}

/** Downloads a file through the authenticated API and triggers a browser save. */
export async function downloadFile(id: number, filename: string): Promise<void> {
  const token = getAccessToken()
  const headers: Record<string, string> = {}
  if (token) headers['Authorization'] = `Bearer ${token}`
  const res = await fetch(`${API_BASE}/admin/files/${id}/download`, { headers })
  if (!res.ok) {
    let message = `下载失败 (${res.status})`
    try {
      const data = await res.json()
      if (data && typeof data.error === 'string') message = data.error
    } catch {
      // keep default
    }
    throw new ApiError(res.status, message)
  }
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  // 延迟撤销，避免浏览器尚未开始下载时就失效导致空文件
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

/** Uploads a file with progress reporting (XHR, since fetch has no upload progress). */
export function uploadFile(
  file: File,
  onProgress?: (percent: number) => void,
): Promise<{ file: FileAssetItem }> {
  return new Promise((resolve, reject) => {
    const token = getAccessToken()
    const form = new FormData()
    form.append('file', file)

    const xhr = new XMLHttpRequest()
    xhr.open('POST', `${API_BASE}/admin/files`)
    if (token) xhr.setRequestHeader('Authorization', `Bearer ${token}`)
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) {
        onProgress(Math.round((e.loaded / e.total) * 100))
      }
    }
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(JSON.parse(xhr.responseText))
        } catch {
          reject(new ApiError(xhr.status, '响应解析失败'))
        }
      } else {
        let message = `上传失败 (${xhr.status})`
        try {
          const data = JSON.parse(xhr.responseText)
          if (data && typeof data.error === 'string') message = data.error
        } catch {
          // keep default
        }
        reject(new ApiError(xhr.status, message))
      }
    }
    xhr.onerror = () => reject(new ApiError(0, '网络错误，上传失败'))
    xhr.send(form)
  })
}

// ---------- System / Account API ----------

export interface SystemInfo {
  name: string
  version: string
  go_version: string
  uptime: string
  author: string
  /** 运行中代码的提交（短哈希，未知为空） */
  commit?: string
  commit_at?: string
  /** 提交来源：ldflags / deployed / env */
  commit_source?: string
}

export function fetchSystemInfo() {
  return api<{ info: SystemInfo }>('/system/info')
}

// ---------- 在线更新（仅管理员） ----------

/** 一次提交的展示信息 */
export interface UpdateCommit {
  hash: string
  short: string
  message: string
  author: string
  date: string
  url: string
}

/** GitHub Release 资产（镜像包等） */
export interface UpdateReleaseAsset {
  name: string
  size: number
  url: string
  content_type: string
}

/** GitHub Release 详情（releases 更新源） */
export interface UpdateRelease {
  tag: string
  name: string
  /** 发布说明（Markdown 原文，前端渲染） */
  body: string
  url: string
  published_at: string
  prerelease: boolean
  draft: boolean
  assets: UpdateReleaseAsset[]
}

export interface UpdatePendingPreview {
  writes: number
  deletes: number
  bytes: number
}

export interface UpdatePending {
  hash: string
  short: string
  /** source（源码更新）/ release_image（镜像包更新） */
  kind: string
  /** releases 模式的目标版本号 */
  version: string
  downloaded: boolean
  apply_ready: boolean
  files: number
  bytes: number
  sha256: string
  source: string
  staged_at: string
  staged_dir: string
  image_path: string
  image_name: string
  compose_file: string
  preview: UpdatePendingPreview | null
}

export interface UpdateStage {
  running: boolean
  /** idle / staging / swapping / rebuilding / success / failed / rolled_back */
  phase: string
  progress: number
  message: string
  started_at: string
  finished_at: string
  target: string
  error: string
  success: boolean
  files_changed: number
  backup_id: string
  rolled_back: boolean
  /** waiting_agent / in_place / docker */
  rebuild_mode: string
  rebuild_ms: number
  /** 已替换源码，等待宿主代理重建重启 */
  waiting_agent: boolean
  operator?: string
}

export interface UpdateVersion {
  current: UpdateCommit
  current_from: string
  /** 本地版本号（releases 模式；从部署版本记录读出，缺失时为 AppVersion） */
  current_version: string
  current_version_from: string
  deployed: { hash: string; short: string; updated_at: string; path: string }
  latest: UpdateCommit
  /** 上游最新版本号（releases 模式） */
  latest_version: string
  /** 上游最新 Release 详情（releases 模式） */
  release: UpdateRelease | null
  /** 检查快照的更新源：source / release_image */
  kind: string
  update_available: boolean
  /** 落后提交数，-1 表示未知 */
  behind: number
  compare_note: string
  changelog: UpdateCommit[]
  changelog_offset: number
  changelog_truncated: boolean
  changelog_from: string
  last_checked: string
  pending: UpdatePending | null
}

export interface UpdateHistoryItem {
  at: string
  from: string
  to: string
  result: string
  detail: string
  backup_id: string
  operator: string
}

export interface UpdateStatus {
  enabled: boolean
  repo_url: string
  repo_name: string
  branch: string
  /** commits（提交+源码包）/ releases（GitHub Releases+镜像包） */
  source: string
  /** waiting_agent / in_place / docker / unavailable */
  mode: string
  source_dir: string
  update_dir: string
  message: string
  version: UpdateVersion
  stage: UpdateStage | null
  history: UpdateHistoryItem[]
}

export interface UpdateBackup {
  id: string
  created_at: string
  path: string
}

/** 宿主更新代理是否已执行完毕（容器部署时由宿主脚本写回） */
export interface UpdateAgentResult {
  state: string
  commit: string
  success: boolean
  message: string
  agent: string
  finished_at: string
  duration_ms: number
}

export interface UpdateOverview {
  update: UpdateStatus
  backups: UpdateBackup[]
  agent: UpdateAgentResult | null
}

/** 读取更新状态（不联网，可安全高频轮询） */
export function fetchUpdateStatus() {
  return api<UpdateOverview>('/admin/system/update', { auth: true })
}

/** 联网检查上游最新提交与更新日志 */
export function checkSystemUpdate() {
  return api<{ update: UpdateStatus }>('/admin/system/update/check', {
    method: 'POST',
    auth: true,
  })
}

/** 一键更新（commits 模式传 commit；releases 模式传版本 tag，如 v1.28.0） */
export function applySystemUpdate(target: string) {
  return api<{ message: string; update: UpdateStatus }>('/admin/system/update/apply', {
    method: 'POST',
    body: { commit: target, target, confirm: true },
    auth: true,
  })
}

/** 回滚到指定备份 */
export function rollbackSystemUpdate(backupId: string) {
  return api<{ message: string; update: UpdateStatus }>('/admin/system/update/rollback', {
    method: 'POST',
    body: { backup_id: backupId, confirm: true },
    auth: true,
  })
}

export function changePassword(currentPassword: string, newPassword: string) {
  return api<{ message: string }>('/auth/password', {
    method: 'PUT',
    body: { current_password: currentPassword, new_password: newPassword },
    auth: true,
  })
}

export function updateProfile(username: string) {
  return api<{ user: User }>('/auth/profile', { method: 'PUT', body: { username }, auth: true })
}

export function fetchMyComments(params: { page?: number; page_size?: number } = {}) {
  const search = new URLSearchParams()
  if (params.page) search.set('page', String(params.page))
  if (params.page_size) search.set('page_size', String(params.page_size))
  const qs = search.toString()
  return api<{ comments: CommentItem[]; total: number }>(`/auth/my-comments${qs ? `?${qs}` : ''}`, {
    auth: true,
  })
}

// ---------- Site Settings API ----------

export function fetchSiteConfig() {
  return api<
    Partial<SiteSettings> & {
      config?: Partial<SiteSettings>
      nav_menu?: unknown
      sidebar_widgets?: unknown
    }
  >('/site-config').then((res) => {
    // Backend returns { config: {...} } — unwrap for flat consumption.
    const cfg = (res as { config?: Partial<SiteSettings> }).config
    const flat = cfg ?? (res as Partial<SiteSettings>)
    return Object.assign(flat, {
      nav_menu: (res as { nav_menu?: unknown }).nav_menu,
      sidebar_widgets: (res as { sidebar_widgets?: unknown }).sidebar_widgets,
    }) as Partial<SiteSettings> & { nav_menu?: unknown; sidebar_widgets?: unknown }
  })
}

export function fetchAdminSettings() {
  return api<{ settings: SiteSettings }>('/admin/settings', { auth: true })
}

export function updateAdminSettings(settings: Partial<SiteSettings> & { smtp_pass?: string }) {
  return api<{ settings: SiteSettings }>('/admin/settings', {
    method: 'PUT',
    body: { settings },
    auth: true,
  })
}

export function sendTestMail(to: string) {
  return api<{ message: string }>('/admin/settings/test-mail', {
    method: 'POST',
    body: { to },
    auth: true,
  })
}

export function fetchAdminUsers(params: { page?: number; page_size?: number; q?: string } = {}) {
  const search = new URLSearchParams()
  if (params.page) search.set('page', String(params.page))
  if (params.page_size) search.set('page_size', String(params.page_size))
  if (params.q) search.set('q', params.q)
  const qs = search.toString()
  return api<AdminUserListResponse>(`/admin/users${qs ? `?${qs}` : ''}`, { auth: true })
}

export function updateAdminUserRole(id: number, role: 'admin' | 'user') {
  return api<{ message: string }>(`/admin/users/${id}/role`, {
    method: 'PUT',
    body: { role },
    auth: true,
  })
}

/** 管理员修改用户资料（邮箱/用户名/密码，仅传需要修改的字段） */
export function updateAdminUser(
  id: number,
  input: { email?: string; username?: string; password?: string },
) {
  return api<{ user: User }>(`/admin/users/${id}`, { method: 'PUT', body: input, auth: true })
}

export function createAdminUser(input: {
  email: string
  username: string
  password: string
  role: 'admin' | 'user'
}) {
  return api<{ user: User }>('/admin/users', { method: 'POST', body: input, auth: true })
}

export function setUserStatus(id: number, status: 'active' | 'banned') {
  return api<{ message: string }>(`/admin/users/${id}/status`, {
    method: 'PUT',
    body: { status },
    auth: true,
  })
}

export function deleteAdminUser(id: number) {
  return api<void>(`/admin/users/${id}`, { method: 'DELETE', auth: true })
}

export function fetchAdminArticles(params: { page?: number; page_size?: number; status?: string } = {}) {
  const search = new URLSearchParams()
  if (params.page) search.set('page', String(params.page))
  if (params.page_size) search.set('page_size', String(params.page_size))
  if (params.status) search.set('status', params.status)
  const qs = search.toString()
  return api<ArticleListResponse>(`/admin/articles${qs ? `?${qs}` : ''}`, { auth: true })
}

export function setAdminArticleStatus(id: number, status: 'draft' | 'published') {
  return api<{ article: Article }>(`/admin/articles/${id}/status`, {
    method: 'PUT',
    body: { status },
    auth: true,
  })
}

export function deleteAdminArticle(id: number) {
  return api<void>(`/admin/articles/${id}`, { method: 'DELETE', auth: true })
}

// ---------- 友链自助申请 ----------

/** 访客提交友链申请（公开；人机验证凭证复用评论场景） */
export function submitLinkApplication(input: SubmitLinkApplicationInput, credential?: CaptchaCredential) {
  return api<{ message: string }>('/link-applications', {
    method: 'POST',
    body: { ...input, ...credential },
  })
}

/** 审核列表（status: pending/approved/rejected，空为全部） */
export function fetchLinkApplications(status?: string) {
  const qs = status ? `?status=${status}` : ''
  return api<{ applications: LinkApplication[]; pending: number }>(`/admin/link-applications${qs}`, { auth: true })
}

/** 通过申请（自动转为正式友链） */
export function approveLinkApplication(id: number) {
  return api<{ application: LinkApplication }>(`/admin/link-applications/${id}/approve`, {
    method: 'POST',
    auth: true,
  })
}

/** 拒绝申请（需填原因） */
export function rejectLinkApplication(id: number, reason: string) {
  return api<{ application: LinkApplication }>(`/admin/link-applications/${id}/reject`, {
    method: 'POST',
    body: { reason },
    auth: true,
  })
}

/** 删除申请记录 */
export function deleteLinkApplication(id: number) {
  return api<void>(`/admin/link-applications/${id}`, { method: 'DELETE', auth: true })
}
