'use client'

import { Suspense, useState } from 'react'
import { useSearchParams, useRouter } from 'next/navigation'
import { useQueries } from '@tanstack/react-query'
import { ArrowLeft, FileText, Search as SearchIcon } from 'lucide-react'
import { fetchArticles } from '@/lib/api'
import { PageLoading, Spinner } from '@/components/page-loader'
import { PageTransition, Reveal, StaggerList, StaggerItem } from '@/components/motion'
import Link from 'next/link'

const PAGE_SIZE = 20

// 列出文章的核心行（不含封面图，搜索页要紧凑、一屏能看更多条）。
function ResultRow({
  slug,
  title,
  excerpt,
  date,
  views,
}: {
  slug: string
  title: string
  excerpt: string
  date: string
  views: number
}) {
  return (
    <StaggerItem>
      <Link href={`/posts/${slug}`} className="block">
        <article className="rounded-xl border border-border bg-card p-5 shadow-sm transition-all duration-300 hover:border-accent/30 hover:shadow-lg hover:shadow-accent/5">
          <h2 className="text-base font-semibold leading-snug tracking-tight transition-colors hover:text-accent">
            {title}
          </h2>
          {excerpt && (
            <p className="mt-2 line-clamp-2 text-sm text-muted-foreground">{excerpt}</p>
          )}
          <div className="mt-3 flex items-center gap-3 text-xs text-muted-foreground/80">
            <span>{date}</span>
            <span className="inline-flex items-center gap-1">{views} 次阅读</span>
          </div>
        </article>
      </Link>
    </StaggerItem>
  )
}

export default function SearchPage() {
  // useSearchParams 需要 Suspense 边界：没有它，Next 在构建期会报
  // "useSearchParams() should be wrapped in a suspense boundary"，
  // 整个页面退化成客户端渲染（失去 SSR 的 SEO 首屏）。
  return (
    <Suspense fallback={<PageLoading minHeight="16rem" />}>
      <SearchPageInner />
    </Suspense>
  )
}

function SearchPageInner() {
  const params = useSearchParams()
  const router = useRouter()
  const q = (params.get('q') ?? '').trim()

  // 输入框用本地 state，避免每敲一个字就改 URL / 触发查询。
  // URL 上的 q 变化时（例如从导航栏搜索跳进来）要同步输入框——这里刻意
  // 不用 useEffect + setState（react-hooks/set-state-in-effect 会报错），
  // 而是记下「这个 draft 是为哪个 q 服务的」，渲染时对比：
  // 不一致说明 URL 变了而输入框还停在旧词，直接回用新 q。
  const [draft, setDraft] = useState<{ q: string; value: string }>({ q, value: q })
  const shown = draft.q === q ? draft.value : q

  const [pages, setPages] = useState<{ q: string; count: number }>({ q, count: 1 })

  // 同理，翻页状态也绑定到关键词：换了词就从头开始，
  // 否则会拿着旧词的页数去请求不存在的页。
  const loadedPages = pages.q === q ? pages.count : 1

  const pageQueries = useQueries({
    queries: Array.from({ length: q ? loadedPages : 0 }, (_, i) => {
      const page = i + 1
      return {
        queryKey: ['search', q, page],
        queryFn: () => fetchArticles({ page, page_size: PAGE_SIZE, q }),
      }
    }),
  })

  const articles = pageQueries.flatMap((r) => r.data?.articles ?? [])
  const total = pageQueries[0]?.data?.total ?? 0
  const isLoading = q !== '' && pageQueries.some((r) => r.isLoading)
  const isError = pageQueries.some((r) => r.isError)
  const isFetchingMore = pageQueries[loadedPages - 1]?.isFetching === true
  const hasMore = articles.length < total

  return (
    <PageTransition>
      <div className="mx-auto max-w-3xl px-4 py-10">
        <Link
          href="/"
          className="mb-6 inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-accent"
        >
          <ArrowLeft className="h-4 w-4" /> 返回首页
        </Link>

        <h1 className="flex items-center gap-2 text-2xl font-bold tracking-tight">
          <SearchIcon className="h-6 w-6 text-accent" /> 搜索文章
        </h1>

        <form
          className="mt-6 flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            const next = shown.trim()
            router.push(next ? `/search?q=${encodeURIComponent(next)}` : '/search')
          }}
        >
          <input
            type="search"
            value={shown}
            onChange={(e) => setDraft({ q, value: e.target.value })}
            placeholder="输入关键词，搜索标题或正文"
            aria-label="搜索关键词"
            className="h-11 flex-1 rounded-lg border border-input bg-background px-4 text-sm outline-none transition-[border-color,box-shadow] focus:border-accent focus:ring-2 focus:ring-accent/20"
          />
          <button
            type="submit"
            className="h-11 rounded-lg bg-accent px-5 text-sm font-medium text-white transition-transform hover:scale-105"
          >
            搜索
          </button>
        </form>

        {q === '' ? (
          <Reveal y={16} className="mt-10 rounded-xl border border-dashed p-16 text-center">
            <SearchIcon className="mx-auto h-10 w-10 text-muted-foreground/50" />
            <p className="mt-4 text-muted-foreground">输入关键词开始搜索</p>
          </Reveal>
        ) : isLoading ? (
          <div className="mt-10">
            <PageLoading minHeight="10rem" />
          </div>
        ) : isError ? (
          <Reveal y={16} className="mt-10 rounded-xl border border-dashed p-16 text-center">
            <p className="text-muted-foreground">搜索失败，请稍后重试</p>
          </Reveal>
        ) : articles.length === 0 ? (
          <Reveal y={16} className="mt-10 rounded-xl border border-dashed p-16 text-center">
            <FileText className="mx-auto h-10 w-10 text-muted-foreground/50" />
            <p className="mt-4 text-muted-foreground">没有找到与「{q}」相关的文章</p>
            <p className="mt-2 text-sm text-muted-foreground/70">
              换个更短的关键词试试，或去
              <Link href="/archive" className="mx-1 text-accent underline underline-offset-4">
                归档页
              </Link>
              按时间浏览
            </p>
          </Reveal>
        ) : (
          <>
            <p className="mt-6 text-sm text-muted-foreground">
              找到 <span className="font-medium text-foreground">{total}</span> 篇与「{q}」相关的文章
            </p>
            <StaggerList className="mt-4 grid gap-3">
              {articles.map((a) => (
                <ResultRow
                  key={a.id}
                  slug={a.slug}
                  title={a.title}
                  excerpt={a.content
                    .replace(/<[^>]*>/g, ' ')
                    .replace(/\s+/g, ' ')
                    .trim()
                    .slice(0, 120)}
                  date={
                    a.published_at
                      ? new Date(a.published_at).toLocaleDateString('zh-CN')
                      : new Date(a.created_at).toLocaleDateString('zh-CN')
                  }
                  views={a.views}
                />
              ))}
            </StaggerList>
            {hasMore && (
              <div className="mt-8 flex justify-center">
                <button
                  type="button"
                  onClick={() => setPages({ q, count: loadedPages + 1 })}
                  disabled={isFetchingMore}
                  className="inline-flex items-center gap-2 rounded-lg border border-border bg-card px-5 py-2.5 text-sm font-medium shadow-sm transition-colors hover:border-accent/40 hover:text-accent disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {isFetchingMore ? (
                    <>
                      <Spinner className="h-4 w-4" />
                      加载中…
                    </>
                  ) : (
                    `加载更多（还有 ${Math.max(total - articles.length, 0)} 条结果）`
                  )}
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </PageTransition>
  )
}
