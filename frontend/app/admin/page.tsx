'use client'

import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import gsap from 'gsap'
import { CountUp, Reveal, hoverLift, prefersReducedMotion, useReveal } from '@/components/motion'
import { PageLoading, Spinner } from '@/components/page-loader'
import {
  Activity,
  Cpu,
  FileEdit,
  FileText,
  Globe,
  HardDrive,
  Lightbulb,
  MemoryStick,
  RefreshCw,
  Users,
} from 'lucide-react'
import { fetchAdminStats, fetchSystemResources, fetchTrafficTrend } from '@/lib/api'
import { formatSize } from '@/lib/ui'
import { MaintenanceToggle } from '@/components/maintenance-toggle'
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
      <div className="card-flat rounded-2xl border border-border bg-card p-5">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              <Activity className="h-4 w-4 text-accent" />
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
                <Tooltip {...tooltipStyle} formatter={(v, name) => [v as number, name === 'page_views' ? '浏览量' : '访客数'] as [number, string]} />
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

      <div className="card-flat rounded-2xl border border-border bg-card p-5">
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
                  formatter={(v, name) => [`${v} KB`, name === 'request' ? '上行' : '下行'] as [string, string]}
                />
                <Legend
                  formatter={(value) => (value === 'request' ? '上行流量' : '下行流量')}
                  wrapperStyle={{ fontSize: 12 }}
                />
                <Line
                  type="monotone"
                  dataKey="request"
                  stroke="#f59e0b"
                  strokeWidth={2}
                  dot={false}
                />
                <Line
                  type="monotone"
                  dataKey="response"
                  stroke="#10b981"
                  strokeWidth={2}
                  dot={false}
                />
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

function Gauge({ percent, color }: { percent: number; color: string }) {
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

  const barColor = (p: number) =>
    p >= 85 ? 'bg-red-500' : p >= 60 ? 'bg-amber-500' : 'bg-emerald-500'

  return (
    <section ref={sectionRef} className="card-flat mt-4 rounded-2xl border border-border bg-card p-5">
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
            <Gauge percent={cpu} color="var(--accent)" />
            <p className="mt-2 flex items-center gap-1.5 text-sm font-medium">
              <Cpu className="h-3.5 w-3.5 text-muted-foreground" />
              CPU 使用率
            </p>
          </div>

          {/* 内存 */}
          <div className="flex flex-col items-center">
            <Gauge percent={memPct} color="#a855f7" />
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
              <div
                ref={barRef}
                className={`h-full w-full origin-left rounded-full ${barColor(memPct)}`}
              />
            </div>
            <p className="mt-3 text-xs text-muted-foreground">
              协程数 {data?.goroutines ?? '—'}
            </p>
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

export default function AdminOverviewPage() {
  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'stats'],
    queryFn: fetchAdminStats,
    refetchInterval: 15000,
  })

  return (
    <div>
      <h1 className="text-2xl font-bold tracking-tight">概览</h1>
      <p className="mt-1 text-sm text-muted-foreground">平台数据与服务器状态</p>

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
            className="card-flat rounded-2xl border border-border bg-card p-5"
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
        <Lightbulb className="mt-0.5 h-4 w-4 shrink-0" />
        <span>提示：概览数据每 15 秒刷新，资源监控每 5 秒刷新，访问趋势每分钟刷新。</span>
      </Reveal>
    </div>
  )
}
