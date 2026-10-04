'use client'

import { useMemo, useState } from 'react'
import Link from 'next/link'
import { CalendarDays, ChevronDown, ChevronRight } from 'lucide-react'
import { useQueries } from '@tanstack/react-query'
import { fetchArticles } from '@/lib/api'
import { PageLoading, Spinner } from '@/components/page-loader'
import { PageTransition, Reveal } from '@/components/motion'

const PAGE_SIZE = 50
// 一次最多预取的页数。归档页通常要看完几年的文章，50×8=400 篇对
// 个人博客足够了；更多时显示「加载更多」而不是无限拉取。
const MAX_PAGES = 8

interface ArchiveItem {
  id: number
  slug: string
  title: string
  published_at: string | null
  created_at: string
  views: number
}

export default function ArchivePage() {
  const [loadedPages, setLoadedPages] = useState(1)

  const pageQueries = useQueries({
    queries: Array.from({ length: Math.min(loadedPages, MAX_PAGES) }, (_, i) => {
      const page = i + 1
      return {
        queryKey: ['archive', page],
        queryFn: () => fetchArticles({ page, page_size: PAGE_SIZE }),
      }
    }),
  })

  const articles = pageQueries.flatMap((r) => r.data?.articles ?? []) as ArchiveItem[]
  const total = pageQueries[0]?.data?.total ?? 0
  const isLoading = pageQueries.some((r) => r.isLoading)
  const isFetchingMore = pageQueries[loadedPages - 1]?.isFetching === true
  const hasMore = articles.length < total && loadedPages < MAX_PAGES

  // 按「年 → 月」分组。发布时间为空的条目归到创建时间的月份
  // （公开列表里状态是 published，理论上都有发布时间，这里只是兜底）。
  const groups = useMemo(() => {
    const byYear = new Map<string, Map<string, ArchiveItem[]>>()
    for (const a of articles) {
      const raw = a.published_at ?? a.created_at
      const d = new Date(raw)
      if (Number.isNaN(d.getTime())) continue
      const year = String(d.getFullYear())
      const month = String(d.getMonth() + 1).padStart(2, '0')
      let months = byYear.get(year)
      if (!months) {
        months = new Map<string, ArchiveItem[]>()
        byYear.set(year, months)
      }
      const bucket = months.get(month)
      if (bucket) bucket.push(a)
      else months.set(month, [a])
    }
    // Map 保持插入序（请求按时间倒序），所以天然是「新年份在前」
    return [...byYear.entries()].map(([year, months]) => ({
      year,
      count: [...months.values()].reduce((n, list) => n + list.length, 0),
      months: [...months.entries()],
    }))
  }, [articles])

  // 默认只展开最近 3 个月的分组，其余折叠——否则老博客一屏全是标题。
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const autoCollapsed = useMemo(() => {
    const keys = groups.flatMap((g) => g.months.map(([m]) => `${g.year}-${m}`))
    return new Set(keys.slice(3))
  }, [groups])
  const isCollapsed = (key: string) => {
    // collapsed 里显式记了「展开」的优先，其次用默认折叠规则
    if (collapsed.has(key)) return true
    if (collapsed.has(`open:${key}`)) return false
    return autoCollapsed.has(key)
  }

  return (
    <PageTransition>
      <div className="mx-auto max-w-3xl px-4 py-10">
        <h1 className="flex items-center gap-2 text-2xl font-bold tracking-tight">
          <CalendarDays className="h-6 w-6 text-accent" /> 文章归档
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {total > 0 ? `共 ${total} 篇文章，按发布时间归档` : '按发布时间浏览全部文章'}
        </p>

        {isLoading ? (
          <div className="mt-10">
            <PageLoading minHeight="12rem" />
          </div>
        ) : groups.length === 0 ? (
          <Reveal y={16} className="mt-10 rounded-xl border border-dashed p-16 text-center">
            <CalendarDays className="mx-auto h-10 w-10 text-muted-foreground/50" />
            <p className="mt-4 text-muted-foreground">还没有已发布的文章</p>
          </Reveal>
        ) : (
          <div className="mt-8 space-y-8">
            {groups.map((g) => (
              <section key={g.year}>
                <div className="sticky top-16 z-10 -mx-2 mb-3 flex items-baseline gap-2 bg-background/85 px-2 py-1 backdrop-blur">
                  <h2 className="text-xl font-bold tracking-tight">{g.year} 年</h2>
                  <span className="text-sm text-muted-foreground">{g.count} 篇</span>
                </div>
                <div className="space-y-4">
                  {g.months.map(([month, items]) => {
                    const key = `${g.year}-${month}`
                    const open = !isCollapsed(key)
                    return (
                      <div key={key} className="border-l-2 border-border pl-4">
                        <button
                          type="button"
                          onClick={() =>
                            setCollapsed((prev) => {
                              const next = new Set(prev)
                              if (open) next.add(key)
                              else next.add(`open:${key}`)
                              return next
                            })
                          }
                          aria-expanded={open}
                          className="flex w-full items-center gap-1.5 py-1 text-left text-sm font-semibold text-muted-foreground transition-colors hover:text-accent"
                        >
                          {open ? (
                            <ChevronDown className="h-4 w-4" />
                          ) : (
                            <ChevronRight className="h-4 w-4" />
                          )}
                          {month} 月
                          <span className="font-normal text-muted-foreground/70">
                            （{items.length}）
                          </span>
                        </button>
                        {open && (
                          <ul className="mt-1 space-y-1">
                            {items.map((a) => (
                              <li key={a.id}>
                                <Link
                                  href={`/posts/${a.slug}`}
                                  className="group flex items-baseline gap-3 rounded-lg px-2 py-1.5 transition-colors hover:bg-accent/5"
                                >
                                  <span className="shrink-0 text-xs tabular-nums text-muted-foreground/70">
                                    {new Date(a.published_at ?? a.created_at).getDate()} 日
                                  </span>
                                  <span className="min-w-0 flex-1 truncate text-sm transition-colors group-hover:text-accent">
                                    {a.title}
                                  </span>
                                  <span className="shrink-0 text-xs text-muted-foreground/60">
                                    {a.views} 阅
                                  </span>
                                </Link>
                              </li>
                            ))}
                          </ul>
                        )}
                      </div>
                    )
                  })}
                </div>
              </section>
            ))}

            {hasMore && (
              <div className="flex justify-center pt-2">
                <button
                  type="button"
                  onClick={() => setLoadedPages((n) => n + 1)}
                  disabled={isFetchingMore}
                  className="inline-flex items-center gap-2 rounded-lg border border-border bg-card px-5 py-2.5 text-sm font-medium shadow-sm transition-colors hover:border-accent/40 hover:text-accent disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {isFetchingMore ? (
                    <>
                      <Spinner className="h-4 w-4" />
                      加载中…
                    </>
                  ) : (
                    `加载更早的文章（还有 ${Math.max(total - articles.length, 0)} 篇）`
                  )}
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </PageTransition>
  )
}
