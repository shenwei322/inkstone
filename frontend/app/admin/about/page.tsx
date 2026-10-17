'use client'

import { useEffect, useRef } from 'react'
import gsap from 'gsap'
import { useQuery } from '@tanstack/react-query'
import {
  Boxes,
  Code2,
  Database,
  FileText,
  Gauge,
  GitCommitHorizontal,
  Info,
  MessageSquare,
  Palette,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  User,
} from 'lucide-react'
import Link from 'next/link'
import { fetchSystemInfo } from '@/lib/api'
import { useSiteConfig } from '@/components/site-config-context'
import { Reveal, prefersReducedMotion, useReveal } from '@/components/motion'

/* 技术栈：版本号与 package.json / go.mod 保持一致。
   这里显式写出主版本，是为了让「关于」页在排查问题时真的有用 —— 
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
    desc: '备份恢复、数据导入导出、操作日志、在线更新与回滚',
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

export default function AdminAboutPage() {
  const site = useSiteConfig()
  const infoQuery = useQuery({ queryKey: ['system', 'info'], queryFn: fetchSystemInfo })
  const info = infoQuery.data?.info

  // 三个内容分区为语义 section：用 useReveal 挂入场动画（不改 DOM 结构）
  const introRef = useRef<HTMLElement>(null)
  const stackRef = useRef<HTMLElement>(null)
  const featureRef = useRef<HTMLElement>(null)
  useReveal(introRef, { y: 16, delay: 0.1, duration: 0.45 })
  useReveal(stackRef, { y: 16, delay: 0.18, duration: 0.45 })
  useReveal(featureRef, { y: 16, delay: 0.26, duration: 0.45 })

  return (
    <div>
      {/* Hero */}
      <Reveal
        y={16}
        duration={0.5}
        className="relative overflow-hidden rounded-xl border border-border bg-gradient-to-br from-accent/10 via-card to-purple-500/10 p-6"
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
              <h1 className="text-2xl font-bold tracking-tight">{site.siteName}</h1>
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
                    <span className="text-[11px] opacity-70">
                      （{commitSourceLabel[info.commit_source]}）
                    </span>
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
            <p className="text-sm font-medium">系统更新</p>
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
                : '未取得版本提交，可到系统更新页检查上游最新版本'}
            </p>
          </div>
          <Link
            href="/admin/system-update"
            className="shrink-0 rounded-lg border border-border px-3.5 py-2 text-sm font-medium transition-colors hover:border-accent/40 hover:text-accent"
          >
            前往系统更新
          </Link>
        </div>
      </Reveal>

      {/* 加载失败提示：接口挂了不能让页面看起来「一切正常但全是占位符」 */}
      {infoQuery.isError && (
        <div className="mt-4 rounded-lg border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-600 dark:text-amber-400">
          无法读取系统信息（{infoQuery.error instanceof Error ? infoQuery.error.message : '未知错误'}）。
          上方以占位符显示，重新加载页面可再试。
        </div>
      )}

      {/* 系统介绍 */}
      <section ref={introRef} className="card-base mt-4 border border-border p-5">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <Info className="h-4 w-4 text-accent" />
          系统介绍
        </h2>
        <p className="mt-3 text-sm leading-relaxed text-muted-foreground">
          InkStone（中文名「砚台」）是一套前后端分离的多用户博客平台：后端基于 Go（Gin + GORM）提供
          RESTful API，前端基于 Next.js 16（App Router）与 React 19。支持多用户写作、评论互动、
          点赞收藏、独立页面、主题外观自定义与站点配置。认证走 HttpOnly 会话 Cookie 并配套 CSRF 校验，
          权限基于 RBAC；外观层使用 oKLCH 色彩体系，改一个色相变量即可整站换色且亮度锁定。
          部署采用 Docker Compose，可运行在任何 VPS 上。由 shenwei 设计与开发。
        </p>
      </section>

      {/* 技术栈 */}
      <section ref={stackRef} className="card-base mt-4 border border-border p-5">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <Code2 className="h-4 w-4 text-accent" />
          技术栈
        </h2>
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
      <section ref={featureRef} className="card-base mt-4 border border-border p-5">
        <h2 className="text-sm font-semibold">功能特性</h2>
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
    </div>
  )
}
