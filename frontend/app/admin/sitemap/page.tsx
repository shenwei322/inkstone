'use client'

import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Check,
  Copy,
  ExternalLink,
  FileCode2,
  Globe,
  ListTree,
  Map as MapIcon,
  Search,
  X,
} from 'lucide-react'
import { fetchSitemapData } from '@/lib/api'
import type { SitemapEntry, SitemapGroup } from '@/lib/types'
import { useNotify } from '@/components/toast'
import { PageTransition, Reveal, useReveal } from '@/components/motion'
import { PageLoading } from '@/components/page-loader'
import { inputClass } from '@/lib/ui'

/** 复制按钮：复制成功显示对勾 1.5s */
function CopyButton({ text, label }: { text: string; label?: string }) {
  const notify = useNotify()
  const [done, setDone] = useState(false)
  // 定时器句柄要跟踪：复制后 1.5s 内卸载组件，回调会向已卸载组件 setState
  const timerRef = useRef<number | null>(null)
  useEffect(
    () => () => {
      if (timerRef.current !== null) window.clearTimeout(timerRef.current)
    },
    [],
  )
  return (
    <button
      type="button"
      title={label ?? '复制'}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
          setDone(true)
          if (timerRef.current !== null) window.clearTimeout(timerRef.current)
          timerRef.current = window.setTimeout(() => {
            setDone(false)
            timerRef.current = null
          }, 1500)
          notify.success('已复制到剪贴板')
        } catch {
          notify.error('复制失败，请手动选择复制')
        }
      }}
      className="inline-flex items-center gap-1 rounded-md border border-border px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent"
    >
      {done ? <Check className="h-3.5 w-3.5 text-emerald-500" /> : <Copy className="h-3.5 w-3.5" />}
      {label}
    </button>
  )
}

function EntryRow({ entry }: { entry: SitemapEntry }) {
  const lastMod = entry.lastmod
    ? new Date(entry.lastmod).toLocaleString('zh-CN', { hour12: false })
    : '—'
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-border/60 px-3 py-2 text-sm last:border-b-0">
      <span className="min-w-0 flex-1 truncate font-medium" title={entry.label}>
        {entry.label}
      </span>
      <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted-foreground" title={entry.loc}>
        {entry.loc}
      </span>
      <span className="hidden w-24 shrink-0 text-right text-xs text-muted-foreground md:block">
        {entry.changefreq ?? '—'}
      </span>
      <span className="hidden w-12 shrink-0 text-right text-xs text-muted-foreground md:block">
        {entry.priority ?? '—'}
      </span>
      <span className="hidden w-36 shrink-0 text-right text-xs text-muted-foreground lg:block" title={entry.lastmod}>
        {lastMod}
      </span>
    </div>
  )
}

/** 分组卡片：语义 section，用 useReveal 挂入场动画（不改 DOM 结构） */
function GroupSection({ group, delay }: { group: SitemapGroup; delay: number }) {
  const ref = useRef<HTMLElement>(null)
  useReveal(ref, { y: 16, duration: 0.4, delay })

  return (
    <section
      ref={ref}
      className="overflow-hidden rounded-2xl border border-border bg-card shadow-sm"
    >
      <header className="flex items-center justify-between border-b border-border bg-muted/40 px-4 py-2.5">
        <h2 className="text-sm font-semibold">{group.label}</h2>
        <span className="rounded-full bg-accent/10 px-2.5 py-0.5 text-xs font-medium text-accent">
          {group.count}
        </span>
      </header>
      <div className="hidden items-center gap-x-3 border-b border-border/60 px-3 py-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground md:flex">
        <span className="min-w-0 flex-1">名称</span>
        <span className="min-w-0 flex-1">地址</span>
        <span className="w-24 shrink-0 text-right">频率</span>
        <span className="w-12 shrink-0 text-right">权重</span>
        <span className="hidden w-36 shrink-0 text-right lg:block">最后更新</span>
      </div>
      <div className="max-h-96 overflow-y-auto">
        {group.entries.map((e) => (
          <EntryRow key={e.loc} entry={e} />
        ))}
      </div>
    </section>
  )
}

export default function AdminSitemapPage() {
  const [filter, setFilter] = useState('')
  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'sitemap'],
    queryFn: fetchSitemapData,
  })

  // 页面标题区：语义 header，用 useReveal 挂入场动画（不改 DOM 结构）
  const headerRef = useRef<HTMLElement>(null)
  useReveal(headerRef, { y: 16, duration: 0.4 })

  const sitemap = data?.data
  const groups = useMemo(() => {
    if (!sitemap) return []
    const q = filter.trim().toLowerCase()
    if (!q) return sitemap.groups
    return sitemap.groups
      .map((g) => ({
        ...g,
        entries: g.entries.filter(
          (e) => e.label.toLowerCase().includes(q) || e.loc.toLowerCase().includes(q),
        ),
        count: g.entries.filter(
          (e) => e.label.toLowerCase().includes(q) || e.loc.toLowerCase().includes(q),
        ).length,
      }))
      .filter((g) => g.count > 0)
  }, [sitemap, filter])

  const counts = useMemo(() => {
    const map: Record<string, number> = {}
    sitemap?.groups.forEach((g) => {
      map[g.type] = g.count
    })
    return map
  }, [sitemap])

  return (
    <PageTransition>
      <div className="space-y-6">
        <header ref={headerRef}>
          <h1 className="flex items-center gap-2 text-2xl font-bold tracking-tight">
            <MapIcon className="h-6 w-6 text-accent" />
            站点地图
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            搜索引擎通过 sitemap.xml 发现站点内容。发布/更新文章、独立页后自动收录于此，无需手动操作。
          </p>
        </header>

        {isLoading || !sitemap ? (
          <PageLoading minHeight="16rem" />
        ) : (
          <>
            {/* 统计 + 入口 */}
            <Reveal
              y={16}
              delay={0.05}
              className="grid gap-4 sm:grid-cols-3"
            >
              <div className="rounded-2xl border border-border bg-card p-5 shadow-sm">
                <p className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
                  <ListTree className="h-4 w-4 text-accent" /> 收录 URL 总数
                </p>
                <p className="mt-2 text-3xl font-bold tracking-tight">{sitemap.total}</p>
                <p className="mt-2 flex flex-wrap gap-1.5 text-[11px] text-muted-foreground">
                  {Object.entries(counts).map(([type, n]) => (
                    <span key={type} className="rounded-full bg-muted px-2 py-0.5">
                      {type} · {n}
                    </span>
                  ))}
                </p>
              </div>

              <div className="rounded-2xl border border-border bg-card p-5 shadow-sm">
                <p className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
                  <Globe className="h-4 w-4 text-accent" /> sitemap.xml
                </p>
                <p className="mt-2 break-all font-mono text-xs">{sitemap.sitemap_url}</p>
                <div className="mt-3 flex flex-wrap gap-2">
                  <a
                    href={sitemap.sitemap_url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 rounded-md bg-accent px-2.5 py-1 text-xs font-medium text-white transition-opacity hover:opacity-95"
                  >
                    <ExternalLink className="h-3.5 w-3.5" /> 打开
                  </a>
                  <CopyButton text={sitemap.sitemap_url} label="复制地址" />
                </div>
              </div>

              <div className="rounded-2xl border border-border bg-card p-5 shadow-sm">
                <p className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
                  <FileCode2 className="h-4 w-4 text-accent" /> robots.txt
                </p>
                <pre className="mt-2 max-h-24 overflow-auto rounded-lg bg-muted/60 p-2 font-mono text-[11px] leading-relaxed text-muted-foreground">
                  {sitemap.robots}
                </pre>
                <div className="mt-3 flex flex-wrap gap-2">
                  <CopyButton text={sitemap.robots} label="复制内容" />
                </div>
              </div>
            </Reveal>

            {/* 过滤 */}
            <div className="relative max-w-md">
              <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <input
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="按名称或地址过滤…"
                className={`${inputClass} pl-9 pr-9`}
              />
              {filter && (
                <button
                  type="button"
                  onClick={() => setFilter('')}
                  aria-label="清空过滤"
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground transition-colors hover:text-foreground"
                >
                  <X className="h-4 w-4" />
                </button>
              )}
            </div>

            {/* 分组列表 */}
            <div className="space-y-4">
              {groups.length === 0 ? (
                <div className="rounded-2xl border border-dashed p-12 text-center text-sm text-muted-foreground">
                  没有匹配的 URL
                </div>
              ) : (
                groups.map((g, i) => (
                  <GroupSection key={g.type} group={g} delay={0.1 + i * 0.05} />
                ))
              )}
            </div>

            <p className="text-center text-xs text-muted-foreground">
              以上地址由后端按站点内容动态生成，随文章/页面发布自动增减；如需主动推送，可在搜索引擎站长平台提交 sitemap.xml 地址。
            </p>
          </>
        )}
      </div>
    </PageTransition>
  )
}
