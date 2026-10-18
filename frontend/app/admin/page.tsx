'use client'

import Link from 'next/link'
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import gsap from 'gsap'
import {
  Boxes,
  Code2,
  Cpu,
  Database,
  ExternalLink,
  FileEdit,
  FileText,
  Gauge,
  GitCommitHorizontal,
  Globe,
  HardDrive,
  Info,
  MemoryStick,
  MessageSquare,
  Palette,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  User,
  Users,
} from 'lucide-react'
import {
  CountUp,
  Reveal,
  hoverLift,
  prefersReducedMotion,
  useReveal,
} from '@/components/motion'
import { PageLoading, Spinner } from '@/components/page-loader'
import { AdminPageHeader } from '@/components/admin-page'
import {
  fetchAdminStats,
  fetchSystemInfo,
  fetchSystemResources,
  fetchTrafficTrend,
} from '@/lib/api'
import { formatSize } from '@/lib/ui'
import { MaintenanceToggle } from '@/components/maintenance-toggle'
import { useSiteConfig } from '@/components/site-config-context'
import { PieChart, Pie, Cell, ResponsiveContainer } from 'recharts'
import {
  Area,
  AreaChart,
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'

const cards = [
  { key: 'total_users', label: '总用户数', icon: Users, color: 'bg-blue-500/10 text-blue-600 dark:text-blue-400' },
  { key: 'total_articles', label: '文章总数', icon: FileText, color: 'bg-purple-500/10 text-purple-600 dark:text-purple-400' },
  { key: 'published_articles', label: '已发布', icon: Globe, color: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' },
  { key: 'draft_articles', label: '草稿箱', icon: FileEdit, color: 'bg-amber-500/10 text-amber-600 dark:text-amber-400' },
] as const

/** 图表悬浮提示样式 */
const tooltipStyle = {
  contentStyle: {
    background: 'var(--card)',
    border: '1px solid var(--border)',
    borderRadius: 10,
    fontSize: 12,
    color: 'var(--foreground)',
  },
  labelStyle: { color: 'var(--muted-foreground)' },
}

function shortDate(d: string): string {
  return d.slice(5) // MM-DD
}

function TrafficCharts() {
  const [days, setDays] = useState(30)
  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'traffic', days],
    queryFn: () => fetchTrafficTrend(days),
    refetchInterval: 60000,
    refetchOnWindowFocus: false,
  })

  const points = data?.points ?? []
  const totalPV = points.reduce((sum, p) => sum + p.page_views, 0)
  const totalUV = points.reduce((sum, p) => sum + p.visitors, 0)
  const totalBytes = points.reduce((sum, p) => sum + p.bytes_in + p.bytes_out, 0)

  // 图表分区为语义 section：用 useReveal 挂入场动画（不改 DOM 结构）
  const sectionRef = useRef<HTMLElement>(null)
  useReveal(sectionRef, { y: 16, delay: 0.2, duration: 0.45 })

  return (
    <section ref={sectionRef} className="mt-4 space-y-4">
      <div className="card-flat rounded-xl border border-border bg-card p-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              <RefreshCw className="h-4 w-4 text-accent" />
              访问趋势
            </h2>
            <p className="mt-1 text-xs text-muted-foreground">
              近 {days} 天：{totalPV} 次浏览 · {totalUV} 位访客
            </p>
          </div>
          <div className="flex items-center gap-1 rounded-lg border border-border bg-background p-1">
            {[7, 30, 90].map((d) => (
              <button
                key={d}
                onClick={() => setDays(d)}
                className={`rounded-md px-2.5 py-1 text-xs transition-colors ${
                  days === d ? 'bg-accent text-white' : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                {d} 天
              </button>
            ))}
          </div>
        </div>

        <div className="mt-4 h-56">
          {isLoading ? (
            <PageLoading minHeight="4rem" />
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={points} margin={{ top: 4, right: 8, left: -18, bottom: 0 }}>
                <defs>
                  <linearGradient id="pvFill" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="var(--accent)" stopOpacity={0.35} />
                    <stop offset="100%" stopColor="var(--accent)" stopOpacity={0.02} />
                  </linearGradient>
                  <linearGradient id="uvFill" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#a855f7" stopOpacity={0.3} />
                    <stop offset="100%" stopColor="#a855f7" stopOpacity={0.02} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" vertical={false} />
                <XAxis
                  dataKey="date"
                  tickFormatter={shortDate}
                  tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }}
                  axisLine={false}
                  tickLine={false}
                  minTickGap={20}
                />
                <YAxis
                  allowDecimals={false}
                  tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }}
                  axisLine={false}
                  tickLine={false}
                />
                <Tooltip
                  {...tooltipStyle}
                  formatter={(v, name) =>
                    [v as number, name === 'page_views' ? '浏览量' : '访客数'] as [number, string]
                  }
                />
                <Legend
                  formatter={(value) => (value === 'page_views' ? '浏览量' : '访客数')}
                  wrapperStyle={{ fontSize: 12 }}
                />
                <Area
                  type="monotone"
                  dataKey="page_views"
                  stroke="var(--accent)"
                  strokeWidth={2}
                  fill="url(#pvFill)"
                />
                <Area
                  type="monotone"
                  dataKey="visitors"
                  stroke="#a855f7"
                  strokeWidth={2}
                  fill="url(#uvFill)"
                />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </div>
      </div>

      <div className="card-flat rounded-xl border border-border bg-card p-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              <HardDrive className="h-4 w-4 text-accent" />
              流量趋势
            </h2>
            <p className="mt-1 text-xs text-muted-foreground">
              近 {days} 天总流量：{formatSize(totalBytes)}
            </p>
          </div>
        </div>
        <div className="mt-4 h-48">
          {isLoading ? (
            <PageLoading minHeight="4rem" />
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <LineChart
                data={points.map((p) => ({
                  date: p.date,
                  request: Number((p.bytes_in / 1024).toFixed(1)),
                  response: Number((p.bytes_out / 1024).toFixed(1)),
                }))}
                margin={{ top: 4, right: 8, left: -18, bottom: 0 }}
              >
                <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" vertical={false} />
                <XAxis
                  dataKey="date"
                  tickFormatter={shortDate}
                  tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }}
                  axisLine={false}
                  tickLine={false}
                  minTickGap={20}
                />
                <YAxis
                  tick={{ fontSize: 11, fill: 'var(--muted-foreground)' }}
                  axisLine={false}
                  tickLine={false}
                  unit="KB"
                />
                <Tooltip
                  {...tooltipStyle}
                  formatter={(v, name) =>
                    [`${v} KB`, name === 'request' ? '上行' : '下行'] as [string, string]
                  }
                />
                <Legend
                  formatter={(value) => (value === 'request' ? '上行流量' : '下行流量')}
                  wrapperStyle={{ fontSize: 12 }}
                />
                <Line type="monotone" dataKey="request" stroke="#f59e0b" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="response" stroke="#10b981" strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          )}
        </div>
        <p className="mt-2 text-xs text-muted-foreground/70">
          说明：流量按请求头/响应体字节统计，图片等静态资源由反向代理直接返回时不计入。
        </p>
      </div>
    </section>
  )
}

/** 资源用量环形图。命名避开 lucide 的 `Gauge` 图标——技术栈卡片要用那个图标。 */
function ResourceGauge({ percent, color }: { percent: number; color: string }) {
  const data = [
    { name: 'used', value: Math.max(0, Math.min(100, percent)) },
    { name: 'free', value: Math.max(0, 100 - Math.min(100, percent)) },
  ]
  return (
    <div className="relative h-28 w-28">
      <ResponsiveContainer width="100%" height="100%">
        <PieChart>
          <Pie
            data={data}
            dataKey="value"
            innerRadius={38}
            outerRadius={50}
            startAngle={90}
            endAngle={-270}
            stroke="none"
          >
            <Cell fill={color} />
            <Cell fill="var(--muted)" />
          </Pie>
        </PieChart>
      </ResponsiveContainer>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        <span className="text-xl font-bold tabular-nums">{percent.toFixed(0)}%</span>
      </div>
    </div>
  )
}

function ResourceMonitor() {
  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: ['admin', 'resources'],
    queryFn: fetchSystemResources,
    refetchInterval: 5000,
    refetchOnWindowFocus: false,
  })

  const cpu = data?.cpu_percent ?? 0
  const memPct = data?.mem_percent ?? 0

  // 内存占用进度条：scaleX 补间（合成层，避免 width 动画每帧 layout 重排）
  const barRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const bar = barRef.current
    if (!bar) return
    if (prefersReducedMotion()) {
      gsap.set(bar, { scaleX: Math.min(100, memPct) / 100 })
      return
    }
    gsap.fromTo(
      bar,
      { scaleX: 0 },
      { scaleX: Math.min(100, memPct) / 100, duration: 0.6, ease: 'power2.out' },
    )
  }, [memPct])

  // 资源分区为语义 section：用 useReveal 挂入场动画（不改 DOM 结构）
  const sectionRef = useRef<HTMLElement>(null)
  useReveal(sectionRef, { y: 16, delay: 0.3, duration: 0.45 })

  const barColor = (p: number) => (p >= 85 ? 'bg-red-500' : p >= 60 ? 'bg-amber-500' : 'bg-emerald-500')

  return (
    <section ref={sectionRef} className="card-flat mt-4 rounded-xl border border-border bg-card p-5">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="flex items-center gap-2 text-sm font-semibold">
            <Cpu className="h-4 w-4 text-accent" />
            服务器资源
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">每 5 秒自动刷新</p>
        </div>
        <button
          onClick={() => refetch()}
          className="flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-accent"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          刷新
        </button>
      </div>

      {isLoading ? (
        <PageLoading minHeight="10rem" className="mt-4" />
      ) : (
        <div className="mt-4 grid gap-6 sm:grid-cols-2 lg:grid-cols-4">
          {/* CPU */}
          <div className="flex flex-col items-center">
            <ResourceGauge percent={cpu} color="var(--accent)" />
            <p className="mt-2 flex items-center gap-1.5 text-sm font-medium">
              <Cpu className="h-3.5 w-3.5 text-muted-foreground" />
              CPU 使用率
            </p>
          </div>

          {/* 内存 */}
          <div className="flex flex-col items-center">
            <ResourceGauge percent={memPct} color="#a855f7" />
            <p className="mt-2 flex items-center gap-1.5 text-sm font-medium">
              <MemoryStick className="h-3.5 w-3.5 text-muted-foreground" />
              内存占用
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {data ? `${data.mem_used_mb.toFixed(0)} / ${data.mem_total_mb.toFixed(0)} MB` : '—'}
            </p>
          </div>

          {/* 应用内存 */}
          <div className="rounded-lg border border-border bg-background p-4">
            <p className="text-xs text-muted-foreground">应用内存</p>
            <p className="mt-1 text-2xl font-bold tabular-nums">
              {data ? `${data.app_mem_mb.toFixed(1)}` : '—'}
              <span className="ml-1 text-sm font-normal text-muted-foreground">MB</span>
            </p>
            <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-muted">
              <div ref={barRef} className={`h-full w-full origin-left rounded-full ${barColor(memPct)}`} />
            </div>
            <p className="mt-3 text-xs text-muted-foreground">协程数 {data?.goroutines ?? '—'}</p>
          </div>

          {/* 运行时长 */}
          <div className="rounded-lg border border-border bg-background p-4">
            <p className="text-xs text-muted-foreground">服务运行时长</p>
            <p className="mt-1 text-2xl font-bold tabular-nums">
              {data
                ? data.uptime_seconds >= 86400
                  ? `${Math.floor(data.uptime_seconds / 86400)} 天`
                  : data.uptime_seconds >= 3600
                    ? `${Math.floor(data.uptime_seconds / 3600)} 小时`
                    : `${Math.floor(data.uptime_seconds / 60)} 分钟`
                : '—'}
            </p>
            <p className="mt-3 text-xs text-muted-foreground">
              内存状态：
              {memPct >= 85 ? '紧张' : memPct >= 60 ? '偏高' : '正常'}
            </p>
          </div>
        </div>
      )}
    </section>
  )
}

/* 技术栈：版本号与 package.json / go.mod 保持一致。
   这里显式写出主版本，是为了让「关于」区在排查问题时真的有用 ——
   用户报障时最常问的就是「你跑的哪个版本」。 */
const techStack = [
  { name: 'Go + Gin + GORM', desc: 'Go 1.27 · Gin 1.12 · GORM 1.31', icon: Code2, color: 'text-cyan-500' },
  { name: 'PostgreSQL', desc: '关系型数据库，软删除与事务保障', icon: Database, color: 'text-blue-500' },
  { name: 'Next.js 16', desc: 'App Router · React 19 · Turbopack', icon: Gauge, color: 'text-foreground' },
  { name: 'Tailwind CSS 4', desc: 'oKLCH 色彩体系 + 语义令牌', icon: Palette, color: 'text-sky-500' },
] as const

/* 功能特性：按「作者会在后台实际用到什么」分组，而不是罗列技术名词。 */
const features = [
  {
    icon: FileText,
    title: '写作与发布',
    desc: 'TipTap 区块编辑器、文章版本历史、定时发布、草稿与回收站',
  },
  {
    icon: MessageSquare,
    title: '互动系统',
    desc: '评论审核、楼中楼回复、点赞收藏、浏览统计、RSS 与站点地图',
  },
  {
    icon: User,
    title: '多用户与权限',
    desc: 'RBAC 角色、注册开关、账号封禁、TOTP 双因素',
  },
  {
    icon: Palette,
    title: '外观自定义',
    desc: '导航菜单、侧边栏小工具、壁纸、oKLCH 主题色、导航下滑隐藏',
  },
  {
    icon: ShieldCheck,
    title: '安全防护',
    desc: 'HttpOnly 会话、CSRF 校验、速率限制、SSRF 拦截、三种验证码可选',
  },
  {
    icon: Boxes,
    title: '运维能力',
    desc: '数据备份、导入导出、操作日志、IP 访问记录与封禁、版本提示',
  },
] as const

/** 技术栈卡片：入场 + 悬停右移（替代原 motion.div initial/animate/whileHover） */
function TechItem({ item, delay }: { item: (typeof techStack)[number]; delay: number }) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    if (prefersReducedMotion()) {
      // 减少动效时直接落到终态，别留下半透明（见下方 fromTo 的注释）
      gsap.set(el, { opacity: 1, x: 0 })
      return
    }
    /* 用 fromTo 而不是 from：
       from 只声明「起点」，终点依赖 GSAP 自己记住的当前值。父级 section 的
       useReveal 在同一批里也会写同一个元素的 transform/opacity，两边抢同名
       属性时会有一方被覆盖 —— 实测结果是动画被打断在 opacity≈0.1 的半透明态，
       卡片文字几乎看不见。
       显式写出终态 opacity: 1 / x: 0 后，无论谁先谁后，最后一帧都完全可见。 */
    gsap.fromTo(
      el,
      { opacity: 0, x: -12 },
      { opacity: 1, x: 0, duration: 0.35, delay, ease: 'expo.out', overwrite: 'auto' },
    )
  }, [delay])

  return (
    <div
      ref={ref}
      className="flex items-center gap-3 rounded-lg border border-border bg-background px-4 py-3"
      onMouseEnter={(e) => {
        if (prefersReducedMotion()) return
        gsap.to(e.currentTarget, { x: 3, duration: 0.25, ease: 'power2.out', overwrite: 'auto' })
      }}
      onMouseLeave={(e) => {
        if (prefersReducedMotion()) return
        gsap.to(e.currentTarget, { x: 0, duration: 0.25, ease: 'power2.out', overwrite: 'auto' })
      }}
    >
      <item.icon className={`h-5 w-5 shrink-0 ${item.color}`} />
      <div className="min-w-0">
        <p className="text-sm font-medium">{item.name}</p>
        <p className="text-xs text-muted-foreground">{item.desc}</p>
      </div>
    </div>
  )
}

/** 提交来源的中文说明：来源不同，可信度不同，排查时要能分辨。 */
const commitSourceLabel: Record<string, string> = {
  ldflags: '编译期注入',
  deployed: '部署记录',
  env: '环境变量',
}

/**
 * 关于本系统（原独立「关于系统」页）——已并入概览页底部。
 *
 * 合并的理由：这个页面全是静态介绍与版本信息，管理员的实际访问频率接近零，
 * 但它占了侧栏最后一行，「关于」这种信息性内容与「概览」天然同源。
 * 概览页末屏本来就是「补充信息」，放这里不打扰日常操作。
 */
function AboutSection() {
  const site = useSiteConfig()
  const infoQuery = useQuery({ queryKey: ['system', 'info'], queryFn: fetchSystemInfo })
  const info = infoQuery.data?.info

  const introRef = useRef<HTMLElement>(null)
  const stackRef = useRef<HTMLElement>(null)
  const featureRef = useRef<HTMLElement>(null)
  useReveal(introRef, { y: 16, delay: 0.1, duration: 0.45 })
  useReveal(stackRef, { y: 16, delay: 0.18, duration: 0.45 })
  useReveal(featureRef, { y: 16, delay: 0.26, duration: 0.45 })

  return (
    <div className="mt-8">
      <h2 className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
        <Info className="h-4 w-4" />
        关于本系统
      </h2>

      {/* Hero */}
      <Reveal
        y={16}
        duration={0.5}
        className="relative mt-3 overflow-hidden rounded-xl border border-border bg-gradient-to-br from-accent/10 via-card to-purple-500/10 p-6"
      >
        <div className="pointer-events-none absolute -right-10 -top-10 h-32 w-32 rounded-full bg-accent/15 blur-3xl" />
        <div className="relative flex flex-wrap items-center gap-4">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img
            src={site.siteLogo || '/logo.svg'}
            alt="InkStone"
            className="h-16 w-16 rounded-2xl shadow-lg shadow-accent/30"
          />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <p className="text-2xl font-bold tracking-tight">{site.siteName}</p>
              <span className="rounded-full bg-accent/15 px-2.5 py-0.5 text-xs font-bold text-accent">
                v{info?.version ?? '…'}
              </span>
            </div>
            <p className="mt-1 text-sm text-muted-foreground">
              InkStone · 砚台 — 一方承载文字的多用户博客系统
            </p>
          </div>
          <div className="ml-auto text-right text-xs text-muted-foreground">
            <p>作者：{info?.author ?? 'shenwei'}</p>
            <p className="mt-0.5">已稳定运行 {info?.uptime ?? '—'}</p>
            <p className="mt-0.5 flex items-center justify-end gap-1">
              <GitCommitHorizontal className="h-3.5 w-3.5" />
              {info?.commit ? (
                <>
                  <code className="rounded bg-muted px-1.5 py-0.5">{info.commit}</code>
                  {info.commit_source && commitSourceLabel[info.commit_source] && (
                    <span className="text-[11px] opacity-70">（{commitSourceLabel[info.commit_source]}）</span>
                  )}
                </>
              ) : (
                '提交未知'
              )}
            </p>
          </div>
        </div>

        {/* 版本与更新入口：系统信息接口同时下发运行中的提交哈希 */}
        <div className="relative mt-5 flex flex-wrap items-center gap-3 rounded-xl border border-border bg-background/70 px-4 py-3">
          <RefreshCw className="h-4 w-4 shrink-0 text-accent" />
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium">版本与更新</p>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {info?.commit
                ? `当前运行提交 ${info.commit}${
                    info.commit_source === 'deployed'
                      ? '（来自部署记录）'
                      : info.commit_source === 'ldflags'
                        ? '（编译期注入）'
                        : info.commit_source === 'env'
                          ? '（环境变量注入）'
                          : ''
                  }`
                : '未取得版本提交，可到「系统维护」检查上游最新版本'}
            </p>
          </div>
          <Link
            href="/admin/system"
            className="shrink-0 rounded-lg border border-border px-3.5 py-2 text-sm font-medium transition-colors hover:border-accent/40 hover:text-accent"
          >
            前往系统维护
          </Link>
        </div>
      </Reveal>

      {/* 加载失败提示：接口挂了不能让页面看起来「一切正常但全是占位符」 */}
      {infoQuery.isError && (
        <div className="mt-4 rounded-lg border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-600 dark:text-amber-400">
          无法读取系统信息（
          {infoQuery.error instanceof Error ? infoQuery.error.message : '未知错误'}）。
          上方以占位符显示，重新加载页面可再试。
        </div>
      )}

      {/* 系统介绍 */}
      <section ref={introRef} className="card-base card-flat mt-4 border border-border p-5">
        <h3 className="flex items-center gap-2 text-sm font-semibold">
          <Info className="h-4 w-4 text-accent" />
          系统介绍
        </h3>
        <p className="mt-3 text-sm leading-relaxed text-muted-foreground">
          InkStone（中文名「砚台」）是一套前后端分离的多用户博客平台：后端基于 Go（Gin + GORM）提供
          RESTful API，前端基于 Next.js 16（App Router）与 React 19。支持多用户写作、评论互动、
          点赞收藏、独立页面、主题外观自定义与站点配置。认证走 HttpOnly 会话 Cookie 并配套 CSRF 校验，
          权限基于 RBAC；外观层使用 oKLCH 色彩体系，改一个色相变量即可整站换色且亮度锁定。
          部署采用 Docker Compose，可运行在任何 VPS 上。由 shenwei 设计与开发。
        </p>
      </section>

      {/* 技术栈 */}
      <section ref={stackRef} className="card-base card-flat mt-4 border border-border p-5">
        <h3 className="flex items-center gap-2 text-sm font-semibold">
          <Code2 className="h-4 w-4 text-accent" />
          技术栈
        </h3>
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          {techStack.map((t, i) => (
            <TechItem key={t.name} item={t} delay={0.25 + i * 0.06} />
          ))}
        </div>
        <p className="mt-3 flex items-center gap-1.5 text-xs text-muted-foreground">
          <Sparkles className="h-3.5 w-3.5" />
          运行环境：{info?.go_version ?? '—'}
          {info?.commit_at && (
            <span className="ml-2 opacity-70">
              · 该提交部署于 {new Date(info.commit_at).toLocaleString('zh-CN')}
            </span>
          )}
        </p>
      </section>

      {/* 功能特性 */}
      <section ref={featureRef} className="card-base card-flat mt-4 border border-border p-5">
        <h3 className="text-sm font-semibold">功能特性</h3>
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          {features.map((f, i) => (
            <Reveal
              key={f.title}
              y={10}
              delay={0.32 + i * 0.06}
              className="rounded-lg border border-border bg-background p-4"
            >
              <div className="flex items-center gap-2">
                <f.icon className="h-4 w-4 text-accent" />
                <p className="text-sm font-medium">{f.title}</p>
              </div>
              <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{f.desc}</p>
            </Reveal>
          ))}
        </div>
      </section>

      <p className="mt-4 flex flex-wrap items-center gap-2 text-xs text-muted-foreground/70">
        <ExternalLink className="h-3.5 w-3.5" />
        项目主页：
        <a
          href="https://github.com/shenwei322/inkstone"
          target="_blank"
          rel="noreferrer"
          className="text-accent hover:underline"
        >
          github.com/shenwei322/inkstone
        </a>
        · MIT License
      </p>
    </div>
  )
}

export default function AdminOverviewPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'stats'],
    queryFn: fetchAdminStats,
    refetchInterval: 15000,
  })

  return (
    <div>
      <AdminPageHeader title="概览" description={<>平台数据与服务器状态</>} />

      <div className="mt-6">
        <MaintenanceToggle />
      </div>

      <div className="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {cards.map((card, i) => (
          <Reveal
            key={card.key}
            y={20}
            delay={i * 0.08}
            duration={0.45}
            className="card-flat rounded-xl border border-border bg-card p-5"
            {...hoverLift}
          >
            <div className={`flex h-10 w-10 items-center justify-center rounded-lg ${card.color}`}>
              <card.icon className="h-5 w-5" />
            </div>
            <p className="mt-4 text-3xl font-bold tracking-tight">
              {isLoading ? (
                <Spinner className="h-9 w-9" />
              ) : (
                <CountUp value={data?.[card.key] ?? 0} />
              )}
            </p>
            <p className="mt-1 text-sm text-muted-foreground">{card.label}</p>
          </Reveal>
        ))}
      </div>

      <ResourceMonitor />
      <TrafficCharts />

      <Reveal
        y={20}
        delay={0.35}
        duration={0.45}
        className="mt-6 flex items-start gap-2.5 rounded-xl border border-dashed border-border p-6 text-sm text-muted-foreground"
      >
        <Info className="mt-0.5 h-4 w-4 shrink-0" />
        <span>提示：概览数据每 15 秒刷新，资源监控每 5 秒刷新，访问趋势每分钟刷新。</span>
      </Reveal>

      <AboutSection />
    </div>
  )
}
