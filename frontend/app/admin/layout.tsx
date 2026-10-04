'use client'

import { useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'
import {
  ArrowLeft,
  FileStack,
  FileText,
  FolderOpen,
  Info,
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
  Tag,
  Users,
  X,
} from 'lucide-react'
import { Presence, Reveal, useReveal } from '@/components/motion'
import { PageLoading } from '@/components/page-loader'
import { useAuth } from '@/lib/auth-context'
import { ThemeToggle } from '@/components/theme-toggle'

const navItems = [
  { href: '/admin', label: '概览', icon: LayoutDashboard },
  { href: '/admin/users', label: '用户管理', icon: Users },
  { href: '/admin/articles', label: '文章管理', icon: FileText },
  { href: '/admin/tags', label: '标签管理', icon: Tag },
  { href: '/admin/pages', label: '页面管理', icon: FileStack },
  { href: '/admin/comments', label: '评论管理', icon: MessageSquare },
  { href: '/admin/files', label: '文件管理', icon: FolderOpen },
  { href: '/admin/sitemap', label: '站点地图', icon: Map },
  { href: '/admin/links', label: '友情链接', icon: Link2 },
  { href: '/admin/appearance', label: '外观管理', icon: Paintbrush },
  { href: '/admin/security', label: '安全防护', icon: ShieldCheck },
  { href: '/admin/settings', label: '网站管理', icon: Settings },
  { href: '/admin/system-update', label: '系统更新', icon: RefreshCw },
  { href: '/admin/logs', label: '网站日志', icon: ScrollText },
  { href: '/admin/about', label: '关于系统', icon: Info },
]

export default function AdminLayout({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth()
  const router = useRouter()
  const pathname = usePathname()
  const [mobileNavOpen, setMobileNavOpen] = useState(false)
  const [lastPath, setLastPath] = useState(pathname)
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

  const renderNav = (closeOnClick: boolean) =>
    navItems.map((item) => {
      const active = item.href === '/admin' ? pathname === '/admin' : pathname.startsWith(item.href)
      return (
        <Link
          key={item.href}
          href={item.href}
          onClick={closeOnClick ? () => setMobileNavOpen(false) : undefined}
          className={`flex items-center gap-2.5 rounded-lg px-3 py-2 text-sm transition-colors duration-200 ${
            active
              ? 'bg-accent text-white shadow-sm shadow-accent/25'
              : 'text-muted-foreground hover:bg-muted hover:text-foreground'
          }`}
        >
          <item.icon className="h-4 w-4" />
          <span className="font-medium">{item.label}</span>
        </Link>
      )
    })

  // 文章新建/编辑是工作台页面：富文本 + 元信息同屏，内容区比其他后台页放宽
  const isArticleEditorRoute =
    pathname.startsWith('/admin/articles/new') || pathname.startsWith('/admin/articles/edit/')

  return (
    <div className="min-h-[calc(100dvh-4rem)]">
      {/* 移动端顶栏：标题 + 主题切换 + 汉堡按钮（<640px 显示） */}
      <div className="mb-4 flex items-center justify-between px-4 pt-4 sm:hidden">
        <p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">管理控制台</p>
        <div className="flex items-center gap-2">
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
        {/* 桌面端侧边栏：贴视口最左侧、全高固定（顶部避开导航栏 4rem） */}
        <aside
          ref={asideRef}
          className="fixed left-0 top-16 z-40 hidden h-[calc(100dvh-4rem)] w-52 shrink-0 flex-col overflow-y-auto border-r border-border bg-card p-3 sm:flex"
        >
          <p className="px-3 pb-2 pt-1 text-xs font-semibold uppercase tracking-widest text-muted-foreground">
            管理控制台
          </p>
          <nav className="mt-2 flex-1 space-y-1 overflow-y-auto">{renderNav(false)}</nav>
          <div className="mt-3 flex items-center justify-between border-t border-border px-3 pb-1 pt-3">
            <span className="text-sm text-muted-foreground">深浅模式</span>
            <ThemeToggle className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-border bg-card text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent active:scale-95" />
          </div>
          <div className="mt-3 border-t border-border px-3 pb-1 pt-3">
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
        <div className="mt-2 flex items-center justify-between border-t border-border px-3 pb-1 pt-3">
          <span className="text-sm text-muted-foreground">深浅模式</span>
          <ThemeToggle className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-border bg-card text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent active:scale-95" />
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
