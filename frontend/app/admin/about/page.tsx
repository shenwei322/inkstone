'use client'

import { useEffect, useRef } from 'react'
import gsap from 'gsap'
import { useQuery } from '@tanstack/react-query'
import {
  Code2,
  Database,
  FileText,
  Gauge,
  Info,
  MessageSquare,
  Palette,
  RefreshCw,
  ShieldCheck,
  User,
} from 'lucide-react'
import Link from 'next/link'
import { fetchSystemInfo } from '@/lib/api'
import { useSiteConfig } from '@/components/site-config-context'
import { Reveal, prefersReducedMotion, useReveal } from '@/components/motion'

const techStack = [
  { name: 'Go + Gin + GORM', desc: '高性能后端框架', icon: Code2, color: 'text-cyan-500' },
  { name: 'PostgreSQL', desc: '可靠的关系型数据库', icon: Database, color: 'text-blue-500' },
  { name: 'Next.js 15', desc: 'React 全栈前端框架', icon: Gauge, color: 'text-foreground' },
  { name: 'Tailwind CSS', desc: '原子化样式系统', icon: Palette, color: 'text-sky-500' },
]

const features = [
  { icon: FileText, title: '文章与页面', desc: '区块编辑器、草稿发布、自定义页面模板' },
  { icon: MessageSquare, title: '互动系统', desc: '评论、点赞收藏、浏览统计、RSS 订阅' },
  { icon: User, title: '多用户', desc: 'RBAC 权限、注册开关、用户封禁' },
  { icon: Palette, title: '外观自定义', desc: '导航菜单、侧边栏小工具、站点图标' },
  { icon: ShieldCheck, title: '安全', desc: 'JWT 双令牌、限流设计、操作确认' },
]

/** 技术栈卡片：入场 + 悬停右移（替代原 motion.div initial/animate/whileHover） */
function TechItem({ item, delay }: { item: (typeof techStack)[number]; delay: number }) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = ref.current
    if (!el || prefersReducedMotion()) return
    gsap.from(el, { opacity: 0, x: -12, duration: 0.35, delay, ease: 'expo.out' })
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
      <item.icon className={`h-5 w-5 ${item.color}`} />
      <div>
        <p className="text-sm font-medium">{item.name}</p>
        <p className="text-xs text-muted-foreground">{item.desc}</p>
      </div>
    </div>
  )
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
            <p className="mt-0.5">
              版本提交：
              {info?.commit ? (
                <code className="rounded bg-muted px-1.5 py-0.5">{info.commit}</code>
              ) : (
                '未知'
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

      {/* 系统介绍 */}
      <section
        ref={introRef}
        className="mt-4 rounded-2xl border border-border bg-card p-5"
      >
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <Info className="h-4 w-4 text-accent" />
          系统介绍
        </h2>
        <p className="mt-3 text-sm leading-relaxed text-muted-foreground">
          InkStone（中文名「砚台」）是一套前后端分离的多用户博客平台：后端基于 Go（Gin + GORM）提供
          RESTful API，前端基于 Next.js 15（App Router）。支持多用户写作、评论互动、点赞收藏、独立页面、
          主题外观自定义与站点配置，内置 JWT 双令牌认证与 RBAC 权限体系，
          部署采用 Docker Compose，可运行在任何 VPS 上。由 shenwei 设计与开发。
        </p>
      </section>

      {/* 技术栈 */}
      <section
        ref={stackRef}
        className="mt-4 rounded-2xl border border-border bg-card p-5"
      >
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <Code2 className="h-4 w-4 text-accent" />
          技术栈
        </h2>
        <div className="mt-3 grid gap-3 sm:grid-cols-2">
          {techStack.map((t, i) => (
            <TechItem key={t.name} item={t} delay={0.25 + i * 0.06} />
          ))}
        </div>
        <p className="mt-3 text-xs text-muted-foreground">运行环境：{info?.go_version ?? '—'}</p>
      </section>

      {/* 功能特性 */}
      <section
        ref={featureRef}
        className="mt-4 rounded-2xl border border-border bg-card p-5"
      >
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
