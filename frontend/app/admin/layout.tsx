'use client'

import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'
import {
  ArrowLeft,
  ChevronDown,
  FileStack,
  FileText,
  FolderOpen,
  FolderTree,
  Globe2,
  LayoutDashboard,
  Link2,
  Lock,
  Map,
  Menu,
  MessageSquare,
  Paintbrush,
  RefreshCw,
  ScrollText,
  Settings,
  ShieldCheck,
  Users,
  X,
} from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Presence, Reveal, useReveal } from '@/components/motion'
import { PageLoading } from '@/components/page-loader'
import { useAuth } from '@/lib/auth-context'
import { ThemeToggle } from '@/components/theme-toggle'
import { AccentPicker } from '@/components/accent-picker'

/**
 * 后台导航（Beta1.28 重排）。
 *
 * 此前是 18 项平铺：一屏塞不下，滚到底才能看到「网站日志」，
 * 而且功能之间没有亲疏关系——「回收站」和「关于系统」在视觉上是同级，
 * 但它们的使用频率差了两个数量级。
 *
 * 现在按**使用场景**分成四组，并把同源功能合并（详见各组注释）：
 *   概览                      （1 项，独立，最高频）
 *   内容   文章 / 页面 / 分类标签 / 评论 / 文件   （5 项）
 *   站点   友链 / 外观 / 地图                      （3 项）
 *   安全   安全防护 / IP 管理                      （2 项）
 *   系统   站点设置 / 系统维护 / 网站日志           （3 项）
 *
 * 分组标题是**折叠按钮**：默认全展开（一屏约 5×40px + 4×28px ≈ 312px，
 * 在 720p 下也放得下），点标题可收起不常用的组，让常用项留在视野里。
 * 折叠状态存 localStorage，刷新后保持——运维的习惯不该每次重来。
 */

interface NavItem {
  href: string
  label: string
  icon: LucideIcon
  /** 该路径下的子路由也算激活（默认 true；设为 false 用于精确匹配） */
  prefix?: boolean
}

interface NavGroup {
  key: string
  label: string
  items: NavItem[]
}

const navGroups: NavGroup[] = [
  {
    key: 'overview',
    label: '',
    items: [{ href: '/admin', label: '概览', icon: LayoutDashboard, prefix: false }],
  },
  {
    key: 'content',
    label: '内容',
    items: [
      // 「回收站」并入「文章管理」的筛选标签页：同一张表、同一套还原语义
      { href: '/admin/articles', label: '文章管理', icon: FileText },
      { href: '/admin/pages', label: '页面管理', icon: FileStack },
      // 「标签管理」扩写为「分类与标签」：分类的增删改接口一直存在，此前无入口
      { href: '/admin/taxonomy', label: '分类与标签', icon: FolderTree },
      { href: '/admin/comments', label: '评论管理', icon: MessageSquare },
      { href: '/admin/files', label: '文件管理', icon: FolderOpen },
    ],
  },
  {
    key: 'site',
    label: '站点',
    items: [
      { href: '/admin/links', label: '友情链接', icon: Link2 },
      { href: '/admin/appearance', label: '外观管理', icon: Paintbrush },
      { href: '/admin/sitemap', label: '站点地图', icon: Map },
    ],
  },
  {
    key: 'security',
    label: '安全',
    items: [
      // 配置项在「安全防护」，查看与动手在「IP 管理」，两页挨着才好对照
      { href: '/admin/security', label: '安全防护', icon: ShieldCheck },
      { href: '/admin/ip-access', label: 'IP 管理', icon: Globe2 },
    ],
  },
  {
    key: 'system',
    label: '系统',
    items: [
      // 用户管理归在「系统」而不是「内容」：它是权限与账号，不是内容运营
      { href: '/admin/users', label: '用户管理', icon: Users },
      { href: '/admin/settings', label: '站点设置', icon: Settings },
      // 「系统更新」+「备份恢复」合并为「系统维护」的两个标签页：
      // 运维看到「有新版本」后的第一步就是备份，两页本就该相邻
      { href: '/admin/system', label: '系统维护', icon: RefreshCw },
      { href: '/admin/logs', label: '网站日志', icon: ScrollText },
    ],
  },
]

const COLLAPSE_KEY = 'admin_nav_collapsed'

/** 读折叠状态：解析失败一律当作「全展开」，别让坏数据把整栏吃掉 */
function readCollapsed(): Record<string, boolean> {
  if (typeof window === 'undefined') return {}
  try {
    const raw = window.localStorage.getItem(COLLAPSE_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw) as unknown
    if (!parsed || typeof parsed !== 'object') return {}
    return parsed as Record<string, boolean>
  } catch {
    return {}
  }
}

/* 折叠状态的订阅源：用 useSyncExternalStore 而不是「effect 里 setState」。
 *
 * 项目开了 `react-hooks/set-state-in-effect`：`useEffect(() => setCollapsed(read()), [])`
 * 会直接报 error（同步 setState 触发级联渲染）。而 localStorage 正好是
 * 「外部系统」——useSyncExternalStore 就是为它准备的：
 * 服务端快照返回空对象（首帧全展开，不会 hydration mismatch），
 * 客户端快照读真实值，React 自己负责订阅与重渲染。
 *
 * 代价是快照函数必须返回**稳定引用**，否则 getSnapshot 每次新对象会让 React
 * 判定为无限更新并抛错。所以这里缓存最后一次解析结果，只有 setItem 时才换引用。
 */
let collapsedSnapshot: Record<string, boolean> = {}
let collapsedRaw: string | null = null
const collapsedListeners = new Set<() => void>()

const EMPTY_COLLAPSED: Record<string, boolean> = {}

function getCollapsedSnapshot(): Record<string, boolean> {
  let raw: string | null = null
  try {
    raw = window.localStorage.getItem(COLLAPSE_KEY)
  } catch {
    return EMPTY_COLLAPSED
  }
  // 字符串没变就复用同一个对象引用——这是 useSyncExternalStore 的硬要求
  if (raw === collapsedRaw) return collapsedSnapshot
  collapsedRaw = raw
  collapsedSnapshot = readCollapsed()
  return collapsedSnapshot
}

function subscribeCollapsed(onChange: () => void): () => void {
  collapsedListeners.add(onChange)
  const onStorage = (e: StorageEvent) => {
    // 只响应本键的跨标签页变化；别的键（如 token）刷新与本组件无关
    if (e.key === COLLAPSE_KEY || e.key === null) onChange()
  }
  window.addEventListener('storage', onStorage)
  return () => {
    collapsedListeners.delete(onChange)
    window.removeEventListener('storage', onStorage)
  }
}

function writeCollapsed(next: Record<string, boolean>) {
  try {
    const raw = JSON.stringify(next)
    window.localStorage.setItem(COLLAPSE_KEY, raw)
    // 同步更新缓存，让本标签页的 getSnapshot 立刻返回新引用
    collapsedRaw = raw
    collapsedSnapshot = next
  } catch {
    // 隐私模式下 localStorage 可能抛错：折叠状态丢失无所谓，别影响导航
  }
  for (const fn of collapsedListeners) fn()
}

export default function AdminLayout({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth()
  const router = useRouter()
  const pathname = usePathname()
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const [lastPath, setLastPath] = useState(pathname)
  // 折叠状态存在 localStorage（外部系统），用 useSyncExternalStore 订阅，
  // 避免 effect 内同步 setState（项目 ESLint 禁止）
  const collapsed = useSyncExternalStore(
    subscribeCollapsed,
    getCollapsedSnapshot,
    () => EMPTY_COLLAPSED,
  )
  const asideRef = useRef<HTMLElement>(null)

  useEffect(() => {
    if (!loading && !user) router.push('/login')
  }, [loading, user, router])

  // 抽屉打开时锁定 body 滚动
  useEffect(() => {
    if (!mobileNavOpen) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prev
    }
  }, [mobileNavOpen])

  // 路由切换后自动关闭移动端抽屉（渲染期调整，避免 effect 内 setState）
  if (pathname !== lastPath) {
    setLastPath(pathname)
    setMobileNavOpen(false)
  }

  // 桌面端侧边栏入场
  useReveal(asideRef, { x: -20, duration: 0.45 })

  if (loading) {
    return (
      <div className="mx-auto max-w-6xl px-4 py-16">
        <PageLoading minHeight="16rem" />
      </div>
    )
  }
  if (!user) return null

  if (user.role !== 'admin') {
    return (
      <div className="mx-auto flex max-w-md flex-col items-center px-4 py-24 text-center">
        <div className="flex h-16 w-16 items-center justify-center rounded-full bg-muted">
          <Lock className="h-8 w-8 text-muted-foreground" />
        </div>
        <h1 className="mt-6 text-xl font-bold">需要管理员权限</h1>
        <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
          管理后台仅对管理员开放。你可以浏览首页文章，如需权限请联系站长。
        </p>
        <Link
          href="/"
          className="mt-8 rounded-lg bg-accent px-6 py-2.5 text-sm font-medium text-white shadow-lg shadow-accent/25 transition-transform hover:scale-105"
        >
          返回首页
        </Link>
      </div>
    )
  }

  const toggleGroup = (key: string) => {
    writeCollapsed({ ...collapsed, [key]: !collapsed[key] })
  }

  const isActive = (item: NavItem) =>
    item.prefix === false ? pathname === item.href : pathname === item.href || pathname.startsWith(`${item.href}/`)

  /* 当前所在分组默认强制展开：即使用户收起过它，导航到该组内某页后
     也必须能看见高亮项，否则会「进了页面却找不到自己在哪儿」。 */
  const activeGroupKey = navGroups.find((g) => g.items.some(isActive))?.key

  const renderNav = (closeOnClick: boolean) =>
    navGroups.map((group) => {
      // 没有标题的单组（概览）直接平铺，不加折叠控件
      const isCollapsible = Boolean(group.label)
      const isCollapsed = isCollapsible && collapsed[group.key] && group.key !== activeGroupKey

      return (
        <div key={group.key} className={isCollapsible ? 'pt-2' : ''}>
          {isCollapsible ? (
            /* 分组标题：中文不用 uppercase / tracking-widest。
               那两个类是为拉丁字母设计的——中文没有大小写，uppercase 是空操作，
               而 tracking-widest 会把「内 容」拉开到几乎不成词。
               改用更小的字号 + 正常字距 + 更淡的颜色，靠层级而非字距区分。 */
            <button
              type="button"
              onClick={() => toggleGroup(group.key)}
              aria-expanded={!isCollapsed}
              className="group flex w-full items-center justify-between rounded-md px-3 py-1 text-xs font-medium text-muted-foreground/60 transition-colors hover:text-foreground"
            >
              <span>{group.label}</span>
              <ChevronDown
                className={`h-3 w-3 opacity-0 transition-all group-hover:opacity-100 ${
                  isCollapsed ? '-rotate-90' : ''
                }`}
              />
            </button>
          ) : null}

          {isCollapsed ? null : (
            <div className="mt-0.5 space-y-0.5">
              {group.items.map((item) => {
                const active = isActive(item)
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    onClick={closeOnClick ? () => setMobileNavOpen(false) : undefined}
                    className={`menu-item flex items-center gap-2.5 px-3 py-1.5 text-sm ${
                      active
                        ? 'menu-item-active bg-accent text-white'
                        : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                    }`}
                  >
                    <item.icon className="h-4 w-4" />
                    <span className="font-medium">{item.label}</span>
                  </Link>
                )
              })}
            </div>
          )}
        </div>
      )
    })

  // 文章新建/编辑是工作台页面：富文本 + 元信息同屏，内容区比其他后台页放宽
  const isArticleEditorRoute =
    pathname.startsWith('/admin/articles/new') || pathname.startsWith('/admin/articles/edit/')

  return (
    <div className="admin-surface min-h-[calc(100dvh-var(--navbar-offset))]">
      {/* 移动端顶栏：标题 + 主题切换 + 汉堡按钮（<640px 显示） */}
      <div className="mb-4 flex items-center justify-between px-4 pt-4 sm:hidden">
        <p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">管理控制台</p>
        <div className="flex items-center gap-2">
          <AccentPicker className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-card text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent active:scale-95" />
          <ThemeToggle className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-card text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent active:scale-95" />
          <button
            type="button"
            onClick={() => setMobileNavOpen(true)}
            aria-label="打开导航菜单"
            className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-card px-3 py-1.5 text-sm font-medium transition-colors hover:border-accent/40 hover:text-accent"
          >
            <Menu className="h-4 w-4" /> 菜单
          </button>
        </div>
      </div>

      <div className="flex">
        {/* 桌面端侧边栏：贴视口最左侧、全高固定。
            顶部偏移与高度都用 --navbar-offset（= 导航卡片高度），
            因此导航高度一改，侧栏自动跟随，不会出现缝隙或压盖。

            滚动策略：**外层不滚动，只有中间的 <nav> 滚动**。
            早先是把 overflow-y-auto 加在 aside 上，配合内部 flex-1 的 nav，
            结果 flex 分配失效、底部「深浅模式 / 返回前台」被挤出可视区
            （表现为「与下边栏重叠」）。现在改成 min-h-0 + 内层滚动，
            顶部标题与底部操作区始终钉在两端。 */}
        <aside
          ref={asideRef}
          className="admin-sidebar fixed left-0 top-[var(--navbar-offset)] z-40 hidden h-[calc(100dvh-var(--navbar-offset))] w-52 shrink-0 flex-col overflow-hidden border-r border-border bg-card p-3 sm:flex"
        >
          <p className="shrink-0 px-3 pb-2 pt-1 text-xs font-semibold uppercase tracking-widest text-muted-foreground">
            管理控制台
          </p>
          <nav className="mt-1 min-h-0 flex-1 overflow-y-auto overscroll-contain pb-1">
            {renderNav(false)}
          </nav>
          {/* 底部工具行：配色 + 深浅模式。
              侧栏是 overflow-hidden，且这一行贴着视口底部，
              所以配色面板要 up + start（向左上展开），否则会被裁掉。 */}
          <div className="mt-2 shrink-0 border-t border-border px-3 pb-1 pt-2">
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">配色主题</span>
              <AccentPicker
                placement="up"
                align="start"
                className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-border bg-card text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent active:scale-95"
              />
            </div>
            <div className="mt-2 flex items-center justify-between">
              <span className="text-sm text-muted-foreground">深浅模式</span>
              <ThemeToggle className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-border bg-card text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent active:scale-95" />
            </div>
          </div>
          <div className="mt-2 shrink-0 border-t border-border px-3 pb-2 pt-2">
            <Link
              href="/"
              className="flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              <ArrowLeft className="h-4 w-4" /> 返回前台
            </Link>
          </div>
        </aside>

        <div className="min-w-0 flex-1 sm:ml-52">
          <div
            className={`mx-auto px-4 py-6 sm:px-6 sm:py-8 ${
              isArticleEditorRoute ? 'max-w-7xl' : 'max-w-5xl'
            }`}
          >
            <Reveal duration={0.45}>{children}</Reveal>
          </div>
        </div>
      </div>

      {/* 移动端抽屉导航 */}
      {/* 抽屉遮罩：点击关闭 */}
      <Presence
        show={mobileNavOpen}
        duration={0.2}
        className="fixed inset-0 z-[120] bg-black/50 backdrop-blur-sm sm:hidden"
        onClick={() => setMobileNavOpen(false)}
      >
        {''}
      </Presence>
      {/* 侧滑抽屉面板 */}
      <Presence
        show={mobileNavOpen}
        x="-100%"
        duration={0.35}
        className="fixed inset-y-0 left-0 z-[120] flex w-72 max-w-[82vw] flex-col rounded-r-2xl border-r border-border bg-card p-3 shadow-2xl sm:hidden"
      >
        <div className="flex items-center justify-between px-3 pb-2 pt-1">
          <p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">管理控制台</p>
          <button
            type="button"
            onClick={() => setMobileNavOpen(false)}
            aria-label="关闭菜单"
            className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            <X className="h-5 w-5" />
          </button>
        </div>
        <nav className="min-h-0 flex-1 space-y-1 overflow-y-auto">{renderNav(true)}</nav>
        <div className="mt-2 shrink-0 border-t border-border px-3 pb-1 pt-3">
          <div className="flex items-center justify-between">
            <span className="text-sm text-muted-foreground">配色主题</span>
            <AccentPicker
              placement="up"
              align="start"
              className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-border bg-card text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent active:scale-95"
            />
          </div>
          <div className="mt-2 flex items-center justify-between">
            <span className="text-sm text-muted-foreground">深浅模式</span>
            <ThemeToggle className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-border bg-card text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent active:scale-95" />
          </div>
        </div>
        <div className="mt-2 border-t border-border px-3 pb-1 pt-3">
          <Link
            href="/"
            onClick={() => setMobileNavOpen(false)}
            className="flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-foreground"
          >
            <ArrowLeft className="h-4 w-4" /> 返回前台
          </Link>
        </div>
      </Presence>
    </div>
  )
}
