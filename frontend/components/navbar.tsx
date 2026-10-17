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
  const menuWrapRef = useRef<HTMLDivElement>(null)

  // 路由切换后关闭移动端菜单/用户菜单（渲染期调整，避免 effect 内 setState）
  if (pathname !== lastPath) {
    setLastPath(pathname)
    setNavOpen(false)
    setMenuOpen(false)
  }

  // 后台是两栏布局（侧边栏贴视口左侧），导航改为通栏以与侧边栏对齐；
  // 前台维持 Fuwari 的居中窄卡片。详见 globals.css 的 .navbar-shell--full。
  // 位置需在自动隐藏 effect 之前 —— 那个 effect 要读它决定是否跳过。
  const isAdminRoute = pathname === '/admin' || pathname.startsWith('/admin/')

  // 整条导航栏入场
  useReveal(headerRef, { y: -60, duration: 0.5 })

  /* ---------------------------------------------------------------------------
     下滑隐藏 / 回滚显示
     ---------------------------------------------------------------------------
     规则（后台可配，见 settings_service.go 的三个 navbar_* 键）：
       - 向下滚过 navbarHideAfterPx 像素   → 隐藏
       - 向上滚过 navbarShowAfterPx 像素   → 显示
       - 回到顶部附近（< navbarShowAfterPx）→ 强制显示
     两个阈值不同，中间留出回差区间，轻微回滚不会让导航反复闪烁。

     实现要点：
       1. 用 rAF 节流，滚动回调只记方向，真正的 setState 合并到下一帧 —— 
          直接在高频 scroll 里 setState 会每帧触发 React 渲染。
       2. 只在 direction 真的变化时 setState，同方向滚动不触发重渲染。
       3. 隐藏时同步把焦点移出导航，否则键盘 Tab 会跳进一个看不见的导航栏
          （无障碍陷阱）。 */
  /* 只记录「滚动逻辑认为该隐藏」这一事实，是否真的隐藏由渲染期派生：
     showAutoHide = 开关打开 && 非后台路由。开关一关，effect 不再订阅滚动，
     派生值立刻为 false —— 不需要在 effect 里 setState 去复位
     （那会触发 React 的 set-state-in-effect 级联渲染告警）。 */
  const [scrollWantsHide, setScrollWantsHide] = useState(false)
  const showAutoHide = site.navbarAutoHide && !isAdminRoute
  const navHidden = showAutoHide && scrollWantsHide
  const lastScrollY = useRef(0)
  const ticking = useRef(false)

  useEffect(() => {
    // 后台控制台需要常驻导航、或开关关闭时不参与自动隐藏。
    // 这里不碰 state：关闭时 navHidden 已由 showAutoHide 派生成 false。
    if (!showAutoHide) return
    lastScrollY.current = window.scrollY
    let accum = 0 // 同方向累计位移，用于判断是否越过阈值

    const update = () => {
      ticking.current = false
      const y = window.scrollY
      const delta = y - lastScrollY.current
      lastScrollY.current = y

      // 方向变了就重置累计量，避免来回抖动时累计量跨越阈值
      if (delta > 0 !== accum > 0) accum = 0
      accum += delta

      if (y <= site.navbarShowAfterPx) {
        setScrollWantsHide(false)
        accum = 0
        return
      }
      if (accum > site.navbarHideAfterPx) {
        setScrollWantsHide(true)
        // 收起时把菜单关掉，否则浮层会跟着导航一起移出视口留下空白
        setMenuOpen(false)
        setNavOpen(false)
      } else if (accum < -site.navbarShowAfterPx) {
        setScrollWantsHide(false)
      }
    }

    const onScroll = () => {
      if (ticking.current) return
      ticking.current = true
      requestAnimationFrame(update)
    }

    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [showAutoHide, site.navbarHideAfterPx, site.navbarShowAfterPx])

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

  // Fuwari 式导航按钮：无边框、无底色，靠 .btn-plain 的 scale-animation 底衬
  // 做悬停反馈；图标按钮是正方，文字按钮用 px-5 的宽松内边距。
  const navBtn =
    'btn-plain scale-animation rounded-lg h-11 text-sm font-bold px-5'
  // 图标按钮：正方、常显底色（.nav-user-btn 给底色，见 globals.css）
  const iconBtn = 'btn-plain scale-animation nav-user-btn rounded-lg h-11 w-11'
  // 语义别名：右侧功能区的按钮都用它，替换原来的 bg-[var(--btn-regular-bg)] 写法
  const iconBtnSolid = iconBtn

  const menuLink =
    'menu-item flex items-center gap-3 px-3 py-2.5 text-sm text-muted-foreground hover:bg-muted hover:text-foreground'

  return (
    /* Fuwari 导航外层：不再是通栏铺满的横幅，而是「与主体内容同宽的圆角卡片」。
       外层 .navbar-shell 只负责 sticky 定位与水平居中，卡片表面落在 header 上，
       这样圆角与阴影不会被整宽背景拉平（具体规则见 globals.css）。 */
    <div
      className={`navbar-shell${isAdminRoute ? ' navbar-shell--full' : ''}${
        navHidden ? ' navbar-shell--hidden' : ''
      }`}
      aria-hidden={navHidden || undefined}
    >
      <header ref={headerRef} className="navbar-card">
        <div className="flex h-[4.5rem] items-center justify-between gap-2 px-3 sm:px-4">
          {/* 品牌区：Fuwari 用 btn-plain 包裹，整块可点且带悬停底衬 */}
          <Link href="/" className={`${navBtn} group gap-2 pl-3 pr-4`}>
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
            <span className="text-base font-bold tracking-tight text-accent">{site.siteName}</span>
          </Link>

          {/* 主导航：Fuwari 的按钮组，无下划线，靠底衬与 accent 文字表示激活 */}
          <nav className="hidden items-center gap-1 md:flex">
            {menuItems.map((item) => {
              const active = isActive(item.href)
              return (
                <Link
                  key={item.label + item.href}
                  href={item.href}
                  target={item.href.startsWith('http') ? '_blank' : undefined}
                  rel={item.href.startsWith('http') ? 'noreferrer' : undefined}
                  aria-current={active ? 'page' : undefined}
                  className={`${navBtn} gap-1.5 ${active ? 'current-theme-btn' : ''}`}
                >
                  <MenuIcon name={item.icon} className="h-4 w-4" />
                  {item.label}
                  {item.href.startsWith('http') && (
                    <span className="text-[0.7rem] opacity-40 transition-opacity group-hover:opacity-70">
                      ↗
                    </span>
                  )}
                </Link>
              )
            })}
          </nav>

          {/* 右侧功能区：搜索 / 主题 / 用户 / 移动端菜单 */}
          <div className="flex items-center gap-1 sm:gap-2">
          {/* 桌面搜索框（sm 起显示）：Fuwari 把它做成无边框的胶囊，
              底色与导航按钮一致，聚焦时才亮出 accent 描边 */}
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
              className="h-11 w-44 rounded-lg bg-[var(--btn-regular-bg)] py-0 pl-9 pr-3 text-sm outline-none transition-[width,box-shadow,background-color] placeholder:text-muted-foreground focus:w-56 focus:bg-card focus:ring-2 focus:ring-accent/25"
            />
          </form>

          <ThemeToggle className={iconBtnSolid} />

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
                aria-expanded={menuOpen}
                /* 不再叠加 bg-[var(--btn-regular-bg)]：常显底色会和 .btn-plain 的
                   scale 底衬（::before）叠成两层错位的蓝框（表现为「框不住头像」）。
                   改用 .nav-user-btn 统一在 CSS 里给一层底色 + hover 加深。 */
                className="nav-user-btn btn-plain scale-animation h-11 gap-2 rounded-lg py-0 pl-1 pr-2.5 sm:pr-3"
              >
                <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-accent/15 text-xs font-bold text-accent">
                  {user.username.charAt(0).toUpperCase()}
                </span>
                <span className="hidden text-sm font-medium sm:inline">{user.username}</span>
                <span ref={chevRef} className="hidden text-muted-foreground sm:inline">
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
                    // logout() 内部已做两件事的顺序保证：先清本地状态让 UI
                    // 立刻响应，再调服务端清 HttpOnly 会话 Cookie。
                    // 少了后一步就会出现「退出后刷新又被登录回去」。
                    logout()
                    router.push('/')
                  }}
                  className="menu-item flex w-full items-center gap-3 px-3 py-2.5 text-left text-sm text-red-500 hover:bg-red-500/10"
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
              <Link href="/login" className={`${navBtn} h-11 px-4 text-sm`}>
                登录
              </Link>
              <Link
                href="/register"
                {...hoverTapScale}
                className="inline-flex h-11 items-center rounded-lg bg-accent px-4 text-sm font-bold text-white shadow-sm shadow-accent/30"
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
            className={`${iconBtnSolid} navbar-mobile-only`}
          >
            {navOpen ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
          </button>
          </div>
        </div>
      </header>

      {/* 手机端菜单：背景冻结 + 弹出式毛玻璃卡片 */}
      {/* 遮罩：背景变暗 + 模糊，点击关闭 */}
      <Presence
        show={navOpen}
        className="pointer-events-auto fixed inset-x-0 bottom-0 top-0 z-40 bg-black/40 backdrop-blur-sm md:hidden"
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
        className="pointer-events-auto fixed left-3 right-3 top-[5.25rem] z-50 max-h-[calc(100dvh-6.5rem)] overflow-y-auto rounded-2xl border border-border bg-card/95 p-3 shadow-2xl shadow-black/20 backdrop-blur-2xl md:hidden"
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
                  className={`menu-item flex items-center gap-3 px-3 py-2.5 text-sm ${
                    active
                      ? 'menu-item-active bg-accent/10 font-medium text-accent'
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
                className="menu-item flex w-full items-center gap-3 px-3 py-2.5 text-left text-sm text-red-500 hover:bg-red-500/10"
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
    </div>
  )
}
