'use client'

import { useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'
import gsap from 'gsap'
import {
  ChevronDown,
  LayoutDashboard,
  LogOut,
  Menu,
  Moon,
  PenLine,
  Search,
  Sun,
  User,
  UserPlus,
  X,
} from 'lucide-react'
import { Presence, hoverTapScale, prefersReducedMotion, useReveal } from '@/components/motion'
import { Spinner } from '@/components/page-loader'
import { useAuth } from '@/lib/auth-context'
import { useSiteConfig } from '@/components/site-config-context'
import { MenuIcon } from '@/components/menu-icon'
import { useTheme } from '@/components/theme'
import { ThemeToggle } from '@/components/theme-toggle'

export function Navbar() {
  const { user, loading, logout } = useAuth()
  const site = useSiteConfig()
  const { theme } = useTheme()
  const pathname = usePathname()
  const router = useRouter()
  const [menuOpen, setMenuOpen] = useState(false)
  const [navOpen, setNavOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [lastPath, setLastPath] = useState(pathname)
  const searchInputRef = useRef<HTMLInputElement>(null)
  const headerRef = useRef<HTMLElement>(null)
  const chevRef = useRef<HTMLSpanElement>(null)
  const underlineRef = useRef<HTMLSpanElement>(null)
  const menuWrapRef = useRef<HTMLDivElement>(null)

  // 路由切换后关闭移动端菜单/用户菜单（渲染期调整，避免 effect 内 setState）
  if (pathname !== lastPath) {
    setLastPath(pathname)
    setNavOpen(false)
    setMenuOpen(false)
  }

  // 整条导航栏入场
  useReveal(headerRef, { y: -60, duration: 0.5 })

  // 导航激活下划线：随路由 key 重挂 + scaleX 入场
  useEffect(() => {
    if (underlineRef.current && !prefersReducedMotion()) {
      gsap.fromTo(underlineRef.current, { scaleX: 0 }, { scaleX: 1, duration: 0.3, ease: 'expo.out' })
    }
  }, [pathname])

  // 移动端抽屉打开时聚焦搜索框
  useEffect(() => {
    if (navOpen) searchInputRef.current?.focus()
  }, [navOpen])

  // 用户菜单箭头跟随展开状态旋转
  useEffect(() => {
    if (chevRef.current && !prefersReducedMotion()) {
      gsap.to(chevRef.current, { rotate: menuOpen ? 180 : 0, duration: 0.2 })
    }
  }, [menuOpen])

  // 用户菜单 hover：React 合成事件（onMouseEnter/Leave）+ 原生事件双保险。
  // React 19 下合成 mouseEnter 偶发不触发（曾出现头像 hover 菜单不显示），
  // 原生 listener 作为兜底，两条路径都是幂等设置。
  useEffect(() => {
    const el = menuWrapRef.current
    if (!el) return
    const enter = () => setMenuOpen(true)
    const leave = () => setMenuOpen(false)
    el.addEventListener('mouseenter', enter)
    el.addEventListener('mouseleave', leave)
    return () => {
      el.removeEventListener('mouseenter', enter)
      el.removeEventListener('mouseleave', leave)
    }
  }, [])

  // 移动端菜单打开时锁定背景滚动（后面的画面不动）
  useEffect(() => {
    if (!navOpen) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prev
    }
  }, [navOpen])

  const submitSearch = (value: string) => {
    const v = value.trim()
    setNavOpen(false)
    router.push(v ? `/?q=${encodeURIComponent(v)}` : '/')
  }

  const menuItems =
    site.navMenu.length > 0
      ? site.navMenu.map((m) => ({ label: m.label, href: m.url, icon: m.icon }))
      : [{ label: '首页', href: '/', icon: undefined as string | undefined }]

  const isActive = (href: string) => {
    if (href.startsWith('http')) return false
    return (
      pathname === href ||
      (href !== '/' && (pathname === `${href}/` || pathname.startsWith(`${href}/`)))
    )
  }

  const iconBtn =
    'inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border bg-card text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent active:scale-95'

  const menuLink =
    'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground'

  return (
    <header
      ref={headerRef}
      className="sticky top-0 z-50 border-b border-border bg-background/70 backdrop-blur-xl"
    >
      <div className="mx-auto flex h-16 max-w-5xl items-center justify-between px-4">
        <div className="flex items-center gap-8">
          <Link href="/" className="group flex items-center gap-2 text-lg font-bold tracking-tight">
            {site.siteLogo ? (
              /* eslint-disable-next-line @next/next/no-img-element */
              <img
                src={site.siteLogo}
                alt={site.siteName}
                className="h-8 w-8 rounded-lg object-cover transition-transform group-hover:scale-110"
              />
            ) : (
              <span
                onMouseEnter={(e) => gsap.to(e.currentTarget, { rotate: 12, scale: 1.15, duration: 0.25 })}
                onMouseLeave={(e) => gsap.to(e.currentTarget, { rotate: 0, scale: 1, duration: 0.25 })}
                className="flex h-8 w-8 items-center justify-center rounded-lg bg-accent text-sm font-black text-white"
              >
                {site.siteName.charAt(0).toUpperCase()}
              </span>
            )}
            <span className="transition-colors group-hover:text-accent">{site.siteName}</span>
          </Link>
          <nav className="hidden items-center gap-1 md:flex">
            {menuItems.map((item) => {
              const active = isActive(item.href)
              return (
                <Link
                  key={item.label + item.href}
                  href={item.href}
                  target={item.href.startsWith('http') ? '_blank' : undefined}
                  rel={item.href.startsWith('http') ? 'noreferrer' : undefined}
                  className={`relative rounded-md px-3 py-1.5 text-sm transition-colors ${
                    active ? 'text-foreground font-medium' : 'text-muted-foreground hover:text-foreground'
                  }`}
                >
                  <MenuIcon name={item.icon} className="mr-1 inline h-4 w-4 align-[-2px]" />
                  {item.label}
                  {active && (
                    <span
                      key={item.href + pathname}
                      ref={underlineRef}
                      className="absolute inset-x-3 -bottom-[13px] h-0.5 origin-left rounded-full bg-accent"
                    />
                  )}
                </Link>
              )
            })}
          </nav>
        </div>

        <div className="flex items-center gap-2 sm:gap-3">
          {/* 桌面搜索框（sm 起显示） */}
          <form
            onSubmit={(e) => {
              e.preventDefault()
              submitSearch(search)
            }}
            className="relative hidden sm:block"
          >
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <input
              name="q"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="搜索文章..."
                className="w-44 rounded-lg border border-border bg-card py-1.5 pl-9 pr-3 text-sm outline-none transition-[border-color,box-shadow] focus:w-56 focus:border-accent focus:ring-2 focus:ring-accent/20"
            />
          </form>

          <ThemeToggle className={iconBtn} />

          {loading ? (
            <Spinner className="hidden h-9 w-9 md:inline-flex" />
          ) : user ? (
            <div
              ref={menuWrapRef}
              className="relative hidden md:block"
              onMouseEnter={() => setMenuOpen(true)}
              onMouseLeave={() => setMenuOpen(false)}
            >
              <button
                onClick={() => setMenuOpen((o) => !o)}
                aria-label="用户菜单"
                className="flex items-center gap-2 rounded-full border border-border bg-card py-1 pl-1 pr-2.5 transition-colors hover:border-accent/40 sm:pr-3"
              >
                <span className="flex h-7 w-7 items-center justify-center rounded-full bg-accent/10 text-xs font-bold text-accent">
                  {user.username.charAt(0).toUpperCase()}
                </span>
                <span className="hidden text-sm font-medium sm:inline">{user.username}</span>
                <span
                  ref={chevRef}
                  className="hidden text-muted-foreground sm:inline"
                >
                  <ChevronDown className="h-3.5 w-3.5" />
                </span>
              </button>

              <Presence
                show={menuOpen}
                y={-8}
                scale={0.96}
                className="absolute right-0 top-full z-50 mt-2 w-56 origin-top-right overflow-hidden rounded-2xl border border-border bg-card/95 p-2 shadow-2xl shadow-black/10 backdrop-blur-xl"
              >
                <div className="mb-1 flex items-center gap-3 rounded-xl bg-muted/50 px-3 py-2.5">
                  <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-accent/10 text-sm font-bold text-accent">
                    {user.username.charAt(0).toUpperCase()}
                  </span>
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{user.username}</p>
                    <p className="truncate text-xs text-muted-foreground">{user.email}</p>
                  </div>
                </div>
                <Link href="/me" onClick={() => setMenuOpen(false)} className={menuLink}>
                  <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-muted text-foreground">
                    <User className="h-4 w-4" />
                  </span>
                  我的账户
                </Link>
                {/* 投稿入口对所有登录用户可见：后端 POST /articles 不限角色，
                    指向 /me/articles/new 而非 /admin/articles/new（后者会把
                    普通作者整页拦下） */}
                <Link
                  href="/me/articles/new"
                  onClick={() => setMenuOpen(false)}
                  className={menuLink}
                >
                  <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-muted text-foreground">
                    <PenLine className="h-4 w-4" />
                  </span>
                  写文章
                </Link>
                {user.role === 'admin' && (
                  <Link href="/admin" onClick={() => setMenuOpen(false)} className={menuLink}>
                    <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-muted text-foreground">
                      <LayoutDashboard className="h-4 w-4" />
                    </span>
                    后台管理
                  </Link>
                )}
                <div className="my-1 border-t border-border" />
                <button
                  onClick={() => {
                    setMenuOpen(false)
                    logout()
                    router.push('/')
                  }}
                  className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm text-red-500 transition-colors hover:bg-red-500/10"
                >
                  <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-red-500/10">
                    <LogOut className="h-4 w-4" />
                  </span>
                  退出登录
                </button>
              </Presence>
            </div>
          ) : (
            <div className="hidden items-center gap-2 md:flex">
              <Link
                href="/login"
                className="rounded-lg px-2.5 py-1.5 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
              >
                登录
              </Link>
              <Link
                href="/register"
                {...hoverTapScale}
                className="inline-block rounded-lg bg-accent px-3.5 py-1.5 text-sm font-medium text-white shadow-sm shadow-accent/30"
              >
                注册
              </Link>
            </div>
          )}

          <button
            type="button"
            onClick={() => setNavOpen((o) => !o)}
            aria-label="菜单"
            aria-expanded={navOpen}
            className={`${iconBtn} md:hidden`}
          >
            {navOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
          </button>
        </div>
      </div>

      {/* 手机端菜单：背景冻结 + 弹出式毛玻璃卡片 */}
      {/* 遮罩：背景变暗 + 模糊，点击关闭 */}
      <Presence
        show={navOpen}
        className="fixed inset-x-0 bottom-0 top-16 z-40 bg-black/40 backdrop-blur-sm md:hidden"
        onClick={() => setNavOpen(false)}
        aria-hidden
      >
        {''}
      </Presence>
      {/* 毛玻璃菜单面板：fixed 弹出式卡片（背景滚动已锁定） */}
      <Presence
        show={navOpen}
        y={-12}
        scale={0.98}
        className="fixed left-4 right-4 top-[4.5rem] z-50 max-h-[calc(100dvh-6rem)] overflow-y-auto rounded-2xl border border-border bg-card/95 p-3 shadow-2xl shadow-black/20 backdrop-blur-2xl md:hidden"
      >
        <div className="space-y-2">
          {/* 搜索：手机端统一收纳到更多菜单内（提交后自动收起） */}
          <form
            onSubmit={(e) => {
              e.preventDefault()
              submitSearch(search)
            }}
            className="relative"
          >
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <input
              ref={searchInputRef}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="搜索文章..."
              className="w-full rounded-xl border border-border bg-background py-2.5 pl-9 pr-3 text-sm outline-none transition-all focus:border-accent focus:ring-2 focus:ring-accent/20"
            />
          </form>

          <nav className="space-y-1">
            {menuItems.map((item) => {
              const active = isActive(item.href)
              return (
                <Link
                  key={item.label + item.href}
                  href={item.href}
                  target={item.href.startsWith('http') ? '_blank' : undefined}
                  rel={item.href.startsWith('http') ? 'noreferrer' : undefined}
                  onClick={() => setNavOpen(false)}
                  className={`flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm transition-colors ${
                    active
                      ? 'bg-accent/10 font-medium text-accent'
                      : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                  }`}
                >
                  <span
                    className={`flex h-8 w-8 items-center justify-center rounded-lg ${
                      active ? 'bg-accent/15 text-accent' : 'bg-muted text-foreground'
                    }`}
                  >
                    <MenuIcon name={item.icon} className="h-4 w-4" />
                  </span>
                  {item.label}
                </Link>
              )
            })}
          </nav>

          <div className="border-t border-border" />

          <div className="flex items-center justify-between rounded-xl bg-muted/50 px-3 py-2.5">
            <span className="flex items-center gap-3 text-sm text-muted-foreground">
              <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-card text-foreground">
                {theme === 'dark' ? (
                  <Sun className="h-4 w-4" />
                ) : (
                  <Moon className="h-4 w-4" />
                )}
              </span>
              外观模式
            </span>
            <ThemeToggle className="rounded-lg border border-border bg-card px-3 py-1.5 text-sm font-medium text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent active:scale-95" />
          </div>

          {user ? (
            <div className="space-y-1">
              <Link href="/me" onClick={() => setNavOpen(false)} className={menuLink}>
                <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-muted text-foreground">
                  <User className="h-4 w-4" />
                </span>
                我的账户
              </Link>
              {user.role === 'admin' && (
                <Link href="/admin" onClick={() => setNavOpen(false)} className={menuLink}>
                  <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-muted text-foreground">
                    <LayoutDashboard className="h-4 w-4" />
                  </span>
                  后台管理
                </Link>
              )}
              <button
                onClick={() => {
                  setNavOpen(false)
                  logout()
                  router.push('/')
                }}
                className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm text-red-500 transition-colors hover:bg-red-500/10"
              >
                <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-red-500/10">
                  <LogOut className="h-4 w-4" />
                </span>
                退出登录
              </button>
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-2">
              <Link
                href="/login"
                onClick={() => setNavOpen(false)}
                className="flex items-center justify-center gap-2 rounded-xl border border-border py-2.5 text-sm font-medium text-foreground transition-colors hover:border-accent/40 hover:text-accent"
              >
                <User className="h-4 w-4" />
                登录
              </Link>
              <Link
                href="/register"
                onClick={() => setNavOpen(false)}
                className="flex items-center justify-center gap-2 rounded-xl bg-accent py-2.5 text-sm font-medium text-white shadow-sm shadow-accent/30"
              >
                <UserPlus className="h-4 w-4" />
                注册
              </Link>
            </div>
          )}
        </div>
      </Presence>
    </header>
  )
}
