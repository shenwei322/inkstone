'use client'

import { useEffect, useRef, useState, type ReactNode } from 'react'
import Link from 'next/link'
import { useQuery } from '@tanstack/react-query'
import gsap from 'gsap'
import {
  Cloud,
  CloudDrizzle,
  CloudFog,
  CloudLightning,
  CloudSnow,
  FileText,
  Folder,
  Quote,
  Search,
  Sun,
  Tag,
  Wind,
} from 'lucide-react'
import { fetchArticles, fetchCategories, fetchTags } from '@/lib/api'
import {
  Reveal,
  StaggerList,
  createLoop,
  hoverLift,
  prefersReducedMotion,
  releaseLoop,
  useReveal,
} from '@/components/motion'
import { PageLoading } from '@/components/page-loader'
import { MenuIcon } from '@/components/menu-icon'
import type { SidebarWidget } from '@/components/site-config-context'

/** 循环动画类型：rotate 旋转 / scale 脉冲 / y 浮动 / opacity 呼吸闪烁 / wiggle 摆动 */
type LoopKind = 'rotate' | 'scale' | 'y' | 'opacity' | 'wiggle'

/**
 * 无限循环动画容器：以命令式 GSAP 替代 framer 的 repeat: Infinity 动画。
 * kind/amount/duration 均为原始类型，effect 依赖稳定，父组件每秒重渲染也不会重启动画。
 */
function LoopAnim({
  kind,
  amount,
  duration = 2,
  className,
  children,
}: {
  kind: LoopKind
  amount?: number
  duration?: number
  className?: string
  children?: ReactNode
}) {
  const ref = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el || prefersReducedMotion()) return
    // createLoop：循环动画纳入全局管理，页面隐藏时自动暂停
    let tween: gsap.core.Tween
    if (kind === 'rotate') {
      tween = createLoop(() => gsap.to(el, { rotate: 360, duration, repeat: -1, ease: 'none' }))
    } else if (kind === 'scale') {
      const to = 1 + (amount ?? 0.08)
      tween = createLoop(() =>
        gsap.fromTo(el, { scale: 1 }, { scale: to, duration, repeat: -1, yoyo: true, ease: 'sine.inOut' }),
      )
    } else if (kind === 'y') {
      const to = -(amount ?? 4)
      tween = createLoop(() =>
        gsap.fromTo(el, { y: 0 }, { y: to, duration, repeat: -1, yoyo: true, ease: 'sine.inOut' }),
      )
    } else if (kind === 'opacity') {
      tween = createLoop(() =>
        gsap.fromTo(
          el,
          { opacity: 1 },
          { opacity: amount ?? 0.2, duration, repeat: -1, yoyo: true, ease: 'none' },
        ),
      )
    } else {
      tween = createLoop(() =>
        gsap.fromTo(
          el,
          { rotate: 0 },
          { rotate: 12, duration, repeat: -1, repeatDelay: 1, yoyo: true, ease: 'sine.inOut' },
        ),
      )
    }
    return () => {
      tween.kill()
      releaseLoop(tween)
    }
  }, [kind, amount, duration])
  return (
    <span ref={ref} className={className}>
      {children}
    </span>
  )
}

/** 数值变化时的淡入上移（key 变化即重放入场，替代 framer 的 motion.span key + initial/animate） */
function FadeIn({
  value,
  y = 6,
  duration = 0.35,
  className,
}: {
  value: ReactNode
  y?: number
  duration?: number
  className?: string
}) {
  const ref = useRef<HTMLSpanElement>(null)
  useReveal(ref, { y, duration })
  return (
    <span ref={ref} className={className}>
      {value}
    </span>
  )
}

/** 倒计时数字变化时的放大回弹（key 变化即重放） */
function PopNumber({ value }: { value: string }) {
  const ref = useRef<HTMLParagraphElement>(null)
  useReveal(ref, { y: 0, scale: 1.18, duration: 0.25 })
  return (
    <p ref={ref} className="text-xl font-bold tabular-nums">
      {value}
    </p>
  )
}

function WidgetCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-border bg-card p-5">
      <h3 className="mb-3 text-sm font-semibold tracking-wide">{title}</h3>
      {children}
    </div>
  )
}

/* ---------- 热门文章 ---------- */

function HotWidget({ limit, title }: { limit?: number; title: string }) {
  const { data } = useQuery({
    queryKey: ['articles', 'hot', limit],
    queryFn: () => fetchArticles({ page: 1, page_size: limit ?? 5, order: 'views' }),
  })
  const articles = data?.articles ?? []
  return (
    <WidgetCard title={title}>
      {articles.length === 0 ? (
        <p className="text-sm text-muted-foreground">暂无数据</p>
      ) : (
        <ol className="space-y-2.5">
          {articles.map((a, i) => (
            <li key={a.id} className="flex items-start gap-2.5">
              <span
                className={`mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded text-[11px] font-bold ${
                  i < 3 ? 'bg-accent/15 text-accent' : 'bg-muted text-muted-foreground'
                }`}
              >
                {i + 1}
              </span>
              <Link
                href={`/posts/${a.slug}`}
                className="line-clamp-1 text-sm text-foreground/90 transition-colors hover:text-accent"
              >
                {a.title}
              </Link>
            </li>
          ))}
        </ol>
      )}
    </WidgetCard>
  )
}

/* ---------- 标签云 ---------- */

function TagsWidget({ limit, title }: { limit?: number; title: string }) {
  const { data } = useQuery({
    queryKey: ['tags'],
    queryFn: fetchTags,
  })
  const tags = (data?.tags ?? []).slice(0, limit ?? 20)
  return (
    <WidgetCard title={title}>
      {tags.length === 0 ? (
        <p className="text-sm text-muted-foreground">暂无标签</p>
      ) : (
        <div className="flex flex-wrap gap-1.5">
          {tags.map((t) => (
            <Link
              key={t.id}
              href={`/tag/${t.slug}`}
              className="rounded-md border border-border px-2 py-0.5 text-xs text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent"
            >
              {t.name}
              <span className="ml-1 opacity-60">{t.article_count}</span>
            </Link>
          ))}
        </div>
      )}
    </WidgetCard>
  )
}

/* ---------- 搜索 ---------- */

function SearchWidget({ title }: { title: string }) {
  return (
    <WidgetCard title={title}>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          const input = (e.currentTarget.elements.namedItem('q') as HTMLInputElement) ?? null
          if (input) window.location.href = input.value.trim() ? `/search?q=${encodeURIComponent(input.value.trim())}` : '/search'
        }}
        className="relative"
      >
        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <input
          name="q"
          placeholder="输入关键词回车搜索..."
          className="w-full rounded-lg border border-border bg-background py-2 pl-9 pr-3 text-sm outline-none transition-all focus:border-accent focus:ring-2 focus:ring-accent/20"
        />
      </form>
    </WidgetCard>
  )
}

/* ---------- HTML ---------- */

function HtmlWidget({ title, content }: { title: string; content?: string }) {
  return (
    <WidgetCard title={title}>
      <div
        className="text-sm leading-relaxed text-foreground/80 [&_a]:text-accent [&_img]:rounded-lg"
        dangerouslySetInnerHTML={{ __html: content || '' }}
      />
    </WidgetCard>
  )
}

/* ---------- 站长信息 ---------- */

function ProfileWidget({ widget }: { widget: SidebarWidget }) {
  const { data } = useQuery({
    queryKey: ['articles', 'profile-count'],
    queryFn: () => fetchArticles({ page: 1, page_size: 1 }),
  })
  const total = data?.total ?? 0
  const name = widget.subtitle?.trim() || widget.title
  const socials = (widget.socials ?? []).filter((s) => s.icon && s.url)

  return (
    <div
      {...hoverLift}
      className="relative overflow-hidden rounded-xl border border-border bg-card p-5 text-center"
    >
      <div className="pointer-events-none absolute -right-6 -top-6 h-24 w-24 rounded-full bg-accent/15 blur-2xl" />
      <div className="relative flex flex-col items-center">
        <div className="relative">
          {widget.avatar ? (
            /* eslint-disable-next-line @next/next/no-img-element */
            <img
              src={widget.avatar}
              alt={name}
              className="h-20 w-20 rounded-full border-2 border-accent/40 object-cover"
            />
          ) : (
            <div className="flex h-20 w-20 items-center justify-center rounded-full border-2 border-accent/40 bg-accent/15 text-2xl font-black text-accent">
              {name.charAt(0).toUpperCase()}
            </div>
          )}
          <LoopAnim
            kind="scale"
            amount={0.25}
            duration={2}
            className="absolute -right-0.5 top-1 h-3.5 w-3.5 rounded-full border-2 border-card bg-emerald-500"
          />
        </div>

        <p className="mt-3 truncate text-base font-semibold">{name}</p>
        <p className="text-xs text-muted-foreground">站长 · 在线</p>

        {widget.content && (
          <p className="mt-3 whitespace-pre-wrap text-sm leading-relaxed text-muted-foreground">
            {widget.content}
          </p>
        )}

        {socials.length > 0 && (
          <div className="mt-4 flex flex-wrap items-center justify-center gap-2">
            {socials.map((s, idx) => (
              <a
                key={`${s.icon}-${idx}`}
                href={s.url}
                target="_blank"
                rel="noopener noreferrer"
                title={s.label || s.icon}
                className="flex h-9 w-9 items-center justify-center rounded-full border border-border bg-background/60 text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent"
              >
                <MenuIcon name={s.icon} className="h-4 w-4" />
              </a>
            ))}
          </div>
        )}
      </div>

      <div className="relative mt-4 grid grid-cols-2 gap-2 text-center">
        <div className="rounded-lg bg-background/60 py-2">
          <p className="text-lg font-bold text-accent">
            <FadeIn key={total} value={total} />
          </p>
          <p className="text-[11px] text-muted-foreground">文章</p>
        </div>
        <div className="rounded-lg bg-background/60 py-2">
          <p className="text-lg font-bold text-purple-500">∞</p>
          <p className="text-[11px] text-muted-foreground">热爱</p>
        </div>
      </div>
    </div>
  )
}

/* ---------- 天气（Open-Meteo，无需 API Key） ---------- */

const WEATHER_CODES: Record<number, { desc: string; Icon: typeof Sun }> = {
  0: { desc: '晴', Icon: Sun },
  1: { desc: '基本晴', Icon: Sun },
  2: { desc: '多云', Icon: Cloud },
  3: { desc: '阴', Icon: Cloud },
  45: { desc: '雾', Icon: CloudFog },
  48: { desc: '雾凇', Icon: CloudFog },
  51: { desc: '小毛雨', Icon: CloudDrizzle },
  53: { desc: '毛雨', Icon: CloudDrizzle },
  55: { desc: '大毛雨', Icon: CloudDrizzle },
  61: { desc: '小雨', Icon: Cloud },
  63: { desc: '中雨', Icon: Cloud },
  65: { desc: '大雨', Icon: Cloud },
  71: { desc: '小雪', Icon: CloudSnow },
  73: { desc: '中雪', Icon: CloudSnow },
  75: { desc: '大雪', Icon: CloudSnow },
  80: { desc: '阵雨', Icon: Cloud },
  81: { desc: '强阵雨', Icon: Cloud },
  82: { desc: '暴雨', Icon: Cloud },
  95: { desc: '雷暴', Icon: CloudLightning },
  96: { desc: '雷暴冰雹', Icon: CloudLightning },
  99: { desc: '强雷暴', Icon: CloudLightning },
}

interface WeatherData {
  temperature: number
  humidity: number
  wind: number
  code: number
  city: string
}

function WeatherWidget({ widget }: { widget: SidebarWidget }) {
  const city = widget.city || '北京'
  const [weather, setWeather] = useState<WeatherData | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let cancelled = false
    async function load() {
      try {
        const geoRes = await fetch(
          `https://geocoding-api.open-meteo.com/v1/search?name=${encodeURIComponent(city)}&count=1&language=zh`,
        )
        const geo = (await geoRes.json()) as {
          results?: { latitude: number; longitude: number; name: string }[]
        }
        if (!geo.results?.length) throw new Error('no geo')
        const { latitude, longitude, name } = geo.results[0]
        const res = await fetch(
          `https://api.open-meteo.com/v1/forecast?latitude=${latitude}&longitude=${longitude}&current=temperature_2m,relative_humidity_2m,weather_code,wind_speed_10m&timezone=auto`,
        )
        const w = (await res.json()) as {
          current?: {
            temperature_2m: number
            relative_humidity_2m: number
            weather_code: number
            wind_speed_10m: number
          }
        }
        if (!w.current) throw new Error('no weather')
        if (cancelled) return
        setWeather({
          temperature: Math.round(w.current.temperature_2m),
          humidity: w.current.relative_humidity_2m,
          wind: Math.round(w.current.wind_speed_10m),
          code: w.current.weather_code,
          city: name,
        })
      } catch {
        if (!cancelled) setFailed(true)
      }
    }
    load()
    return () => {
      cancelled = true
    }
  }, [city])

  const info = weather ? WEATHER_CODES[weather.code] ?? WEATHER_CODES[0] : null

  return (
    <div className="relative overflow-hidden rounded-xl border border-border bg-card p-5">
      <div className="pointer-events-none absolute -left-8 -top-8 h-24 w-24 rounded-full bg-sky-400/15 blur-2xl" />
      <h3 className="relative mb-3 text-sm font-semibold tracking-wide">{widget.title}</h3>
      {failed ? (
        <p className="relative text-sm text-muted-foreground">天气服务暂不可用</p>
      ) : !weather || !info ? (
        /* 天气加载态：GSAP 加载动画替换原骨架图 */
        <div className="relative">
          <PageLoading minHeight="4rem" hint="加载天气…" />
        </div>
      ) : (
        <Reveal y={8} className="relative">
          <div className="flex items-center justify-between">
            <div>
              <p className="text-3xl font-bold tracking-tight">
                {weather.temperature}
                <span className="text-lg">°C</span>
              </p>
              <p className="mt-0.5 text-sm text-muted-foreground">
                {info.desc} · {weather.city}
              </p>
            </div>
            <LoopAnim kind="y" amount={4} duration={1.5}>
              <info.Icon className="h-12 w-12 text-sky-500" />
            </LoopAnim>
          </div>
          <div className="mt-4 flex items-center gap-4 text-xs text-muted-foreground">
            <span className="flex items-center gap-1">
              <Wind className="h-3.5 w-3.5" />
              {weather.wind} km/h
            </span>
            <span>湿度 {weather.humidity}%</span>
          </div>
        </Reveal>
      )}
    </div>
  )
}

/* ---------- 节日倒计时（内置春节表自动计算） ---------- */

const SPRING_FESTIVALS: { year: number; date: string }[] = [
  { year: 2026, date: '2026-02-17' },
  { year: 2027, date: '2027-02-06' },
  { year: 2028, date: '2028-01-26' },
  { year: 2029, date: '2029-02-13' },
  { year: 2030, date: '2030-02-03' },
  { year: 2031, date: '2031-01-23' },
  { year: 2032, date: '2032-02-11' },
  { year: 2033, date: '2033-01-31' },
  { year: 2034, date: '2034-02-19' },
  { year: 2035, date: '2035-02-08' },
]

function nextSpringFestival(): { date: Date; label: string } {
  const now = new Date()
  for (const f of SPRING_FESTIVALS) {
    const d = new Date(`${f.date}T00:00:00+08:00`)
    if (d.getTime() > now.getTime()) {
      return { date: d, label: `${f.year} 年春节` }
    }
  }
  return { date: new Date(`${SPRING_FESTIVALS[0].date}T00:00:00+08:00`), label: '春节' }
}

function CountdownWidget({ widget }: { widget: SidebarWidget }) {
  const auto = nextSpringFestival()
  const parsed = widget.date ? new Date(`${widget.date}T00:00:00+08:00`) : auto.date
  const target = Number.isNaN(parsed.getTime()) ? auto.date : parsed
  const label = widget.eventName || auto.label

  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => {
      if (document.visibilityState === 'visible') setNow(Date.now())
    }, 1000)
    return () => clearInterval(t)
  }, [])

  const diff = Math.max(0, target.getTime() - now)
  const cells: { value: number; unit: string }[] = [
    { value: Math.floor(diff / 86400000), unit: '天' },
    { value: Math.floor((diff % 86400000) / 3600000), unit: '时' },
    { value: Math.floor((diff % 3600000) / 60000), unit: '分' },
    { value: Math.floor((diff % 60000) / 1000), unit: '秒' },
  ]

  return (
    <div className="relative overflow-hidden rounded-xl border border-border bg-card p-5">
      {/* 装饰环旋转 / 光斑呼吸 / 鞭炮摆动：命令式 GSAP 循环动画替代 framer repeat: Infinity */}
      <LoopAnim
        kind="rotate"
        duration={24}
        className="pointer-events-none absolute -right-10 -top-10 h-28 w-28 rounded-full border-[6px] border-dashed border-red-500/20"
      />
      {/* 光斑只做 opacity 呼吸：blur(filter) 元素做 transform 动画时每帧重绘模糊区域，
          远程桌面（CPU 合成）下是明显卡顿源；opacity 走合成层 */}
      <LoopAnim
        kind="opacity"
        amount={0.55}
        duration={2.4}
        className="pointer-events-none absolute -bottom-6 -left-6 h-20 w-20 rounded-full bg-orange-400/15 blur-xl"
      />
      <h3 className="relative mb-1 flex items-center gap-2 text-sm font-semibold tracking-wide">
        <LoopAnim kind="wiggle" duration={1}>
          🧨
        </LoopAnim>
        距离{label}
      </h3>
      <p className="relative text-xs text-muted-foreground">
        {target.toLocaleDateString('zh-CN', { year: 'numeric', month: 'long', day: 'numeric' })}
      </p>
      <div className="relative mt-4 grid grid-cols-4 gap-2">
        {cells.map((c) => (
          <div key={c.unit} className="rounded-lg bg-background/70 py-2.5 text-center shadow-sm">
            <PopNumber key={c.value} value={String(c.value).padStart(2, '0')} />
            <p className="text-[11px] text-muted-foreground">{c.unit}</p>
          </div>
        ))}
      </div>
    </div>
  )
}

/* ---------- 实时时钟 ---------- */

function ClockWidget({ widget }: { widget: SidebarWidget }) {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const t = setInterval(() => {
      if (document.visibilityState === 'visible') setNow(new Date())
    }, 1000)
    return () => clearInterval(t)
  }, [])

  const hh = String(now.getHours()).padStart(2, '0')
  const mm = String(now.getMinutes()).padStart(2, '0')
  const ss = String(now.getSeconds()).padStart(2, '0')
  const week = ['日', '一', '二', '三', '四', '五', '六'][now.getDay()]

  return (
    <div className="relative overflow-hidden rounded-xl border border-border bg-card p-5">
      <div className="pointer-events-none absolute -right-8 -bottom-8 h-24 w-24 rounded-full bg-indigo-400/15 blur-2xl" />
      <h3 className="relative mb-2 text-sm font-semibold tracking-wide">{widget.title}</h3>
      <p className="relative text-4xl font-bold tabular-nums tracking-tight">
        {hh}
        <LoopAnim kind="opacity" duration={1} className="text-indigo-400">
          :
        </LoopAnim>
        {mm}
        <span className="text-xl text-muted-foreground">:{ss}</span>
      </p>
      <p className="relative mt-1 text-xs text-muted-foreground">
        {now.getFullYear()} 年 {now.getMonth() + 1} 月 {now.getDate()} 日 · 星期{week}
      </p>
    </div>
  )
}

/* ---------- 站点统计 ---------- */

function StatsWidget({ title }: { title: string }) {
  const articlesQuery = useQuery({
    queryKey: ['articles', 'stats-total'],
    queryFn: () => fetchArticles({ page: 1, page_size: 1 }),
  })
  const categoriesQuery = useQuery({
    queryKey: ['categories'],
    queryFn: fetchCategories,
  })
  const tagsQuery = useQuery({ queryKey: ['tags'], queryFn: fetchTags })

  const stats = [
    { Icon: FileText, label: '文章', value: articlesQuery.data?.total ?? 0, color: 'text-blue-500' },
    { Icon: Folder, label: '分类', value: categoriesQuery.data?.categories.length ?? 0, color: 'text-purple-500' },
    { Icon: Tag, label: '标签', value: tagsQuery.data?.tags.length ?? 0, color: 'text-emerald-500' },
  ]

  return (
    <div className="rounded-xl border border-border bg-card p-5">
      <h3 className="mb-3 text-sm font-semibold tracking-wide">{title}</h3>
      {/* 统计卡：列表 stagger 入场 + 悬停上浮 */}
      <StaggerList className="grid grid-cols-3 gap-2">
        {stats.map((s) => (
          <div
            key={s.label}
            {...hoverLift}
            className="rounded-lg bg-muted/60 py-3 text-center"
          >
            <s.Icon className={`mx-auto h-4 w-4 ${s.color}`} />
            <FadeIn key={s.value} value={s.value} y={0} className="mt-1 text-lg font-bold" />
            <p className="text-[11px] text-muted-foreground">{s.label}</p>
          </div>
        ))}
      </StaggerList>
    </div>
  )
}

/* ---------- 一言 ---------- */

function HitokotoWidget({ widget }: { widget: SidebarWidget }) {
  const [quote, setQuote] = useState<{ text: string; from: string } | null>(null)
  const [refreshedAt, setRefreshedAt] = useState(0)

  useEffect(() => {
    let cancelled = false
    fetch('https://v1.hitokoto.cn/?c=i&c=k&c=d')
      .then((r) => r.json())
      .then((d: { hitokoto: string; from?: string }) => {
        if (!cancelled) setQuote({ text: d.hitokoto, from: d.from ?? '' })
      })
      .catch(() => {
        if (!cancelled) setQuote({ text: '凡是过往，皆为序章。', from: '莎士比亚' })
      })
    return () => {
      cancelled = true
    }
  }, [refreshedAt])

  return (
    <div className="rounded-xl border border-border bg-card p-5">
      <div className="mb-2 flex items-center justify-between">
        <h3 className="flex items-center gap-1.5 text-sm font-semibold tracking-wide">
          <Quote className="h-3.5 w-3.5 text-accent" />
          {widget.title}
        </h3>
        <button
          type="button"
          onClick={() => setRefreshedAt((n) => n + 1)}
          title="换一句"
          className="text-xs text-muted-foreground transition-colors hover:text-accent"
        >
          ↻ 换一句
        </button>
      </div>
      {quote ? (
        <Reveal key={quote.text} y={8} duration={0.35}>
          <p className="text-sm leading-relaxed text-foreground/85">「{quote.text}」</p>
          {quote.from && <p className="mt-2 text-right text-xs text-muted-foreground">—— {quote.from}</p>}
        </Reveal>
      ) : (
        /* 一言加载态：GSAP 加载动画替换原骨架图 */
        <div className="w-full">
          <PageLoading minHeight="3rem" hint="加载一言…" />
        </div>
      )}
    </div>
  )
}

/* ---------- 渲染分发 ---------- */

export function WidgetRenderer({ widget }: { widget: SidebarWidget }) {
  switch (widget.type) {
    case 'about':
      return (
        <WidgetCard title={widget.title}>
          <p className="whitespace-pre-wrap text-sm leading-relaxed text-muted-foreground">
            {widget.content || '站点简介'}
          </p>
        </WidgetCard>
      )
    case 'hot':
      return <HotWidget limit={widget.limit} title={widget.title} />
    case 'tags':
      return <TagsWidget limit={widget.limit} title={widget.title} />
    case 'search':
      return <SearchWidget title={widget.title} />
    case 'html':
      return <HtmlWidget title={widget.title} content={widget.content} />
    case 'profile':
      return <ProfileWidget widget={widget} />
    case 'weather':
      return <WeatherWidget widget={widget} />
    case 'countdown':
      return <CountdownWidget widget={widget} />
    case 'clock':
      return <ClockWidget widget={widget} />
    case 'stats':
      return <StatsWidget title={widget.title} />
    case 'hitokoto':
      return <HitokotoWidget widget={widget} />
    default:
      return null
  }
}
