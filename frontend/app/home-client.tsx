'use client'

import Link from 'next/link'
import { Suspense, useState } from 'react'
import { useQueries, useQuery } from '@tanstack/react-query'
import { ChevronDown, PenLine, Rss, Search, X } from 'lucide-react'
import { fetchArticles, fetchCategories, fetchTags } from '@/lib/api'
import type { Article } from '@/lib/types'
import { PageLoading, RowLoading, Spinner } from '@/components/page-loader'
import { PageTransition, StaggerList, Reveal } from '@/components/motion'
import { useSiteConfig } from '@/components/site-config-context'
import { SiteSidebar } from '@/components/site-sidebar'
import { PostCard } from '@/components/post-card'

// 每页条数。与后台各列表页保持同一档位，后端上限是 50。
const PAGE_SIZE = 20

export function HomeClient({
  category,
  tag,
  q,
}: {
  category: string | null
  tag: string | null
  q: string | null
}) {
  return (
    <Suspense
      fallback={
        // 宽度对齐「有侧栏」这一默认配置（88rem），否则加载态比内容态窄，
        // 数据到达时会出现整块横向跳动。
        <div className="mx-auto max-w-[88rem] px-4 py-10">
          <RowLoading rows={3} />
        </div>
      }
    >
      <HomePage category={category} tag={tag} q={q} />
    </Suspense>
  )
}

function HomePage({ category, tag, q }: { category: string | null; tag: string | null; q: string | null }) {
  const site = useSiteConfig()
  const widgets = site.widgets.filter((w) => w.type && w.title)

  // 已加载到第几页。用「页数」而不是「追加的文章数组」：翻页只是 setState，
  // 数据由下面声明式的一组 query 各自负责，避免在 effect 里同步追加状态。
  const [loadedPages, setLoadedPages] = useState(1)

  const pageQueries = useQueries({
    queries: Array.from({ length: loadedPages }, (_, i) => {
      const page = i + 1
      return {
        // queryKey 与 app/page.tsx 的 SSR 预取保持一致（第 1 页），
        // 首屏才能命中服务端注入的缓存。
        queryKey: ['articles', 'published', page, category, tag, q],
        queryFn: () =>
          fetchArticles({
            page,
            page_size: PAGE_SIZE,
            category: category ?? undefined,
            tag: tag ?? undefined,
            q: q ?? undefined,
          }),
      }
    }),
  })

  const articles = pageQueries.flatMap((r) => r.data?.articles ?? [])
  const total = pageQueries[0]?.data?.total ?? 0
  const isLoading = pageQueries.some((r) => r.isLoading)
  const isError = pageQueries.some((r) => r.isError)
  const error = pageQueries.find((r) => r.isError)?.error ?? null
  // 只有最后一页还在飞时才算「加载更多中」——前面几页的 refetch 不该让按钮转圈。
  const isFetchingMore = pageQueries[loadedPages - 1]?.isFetching === true
  const hasMore = articles.length < total

  const categoriesQuery = useQuery({ queryKey: ['categories'], queryFn: fetchCategories })
  const tagsQuery = useQuery({ queryKey: ['tags'], queryFn: fetchTags })

  const hasFilter = Boolean(category || tag || q)

  return (
    <PageTransition>
      <div className={`mx-auto px-4 py-10 ${widgets.length > 0 ? 'max-w-[88rem]' : 'max-w-6xl'}`}>
        {/* 筛选时显示标题；默认「最新文章」不显示，避免冗余 */}
        {hasFilter && (
          <div className="mb-6 flex flex-wrap items-center gap-2">
            <h1 className="text-2xl font-bold tracking-tight">
              {category
                ? `分类：${(categoriesQuery.data?.categories ?? []).find((c) => c.slug === category)?.name ?? category}`
                : tag
                  ? `标签：${(tagsQuery.data?.tags ?? []).find((t) => t.slug === tag)?.name ?? tag}`
                  : q
                    ? `搜索：${q}`
                    : ''}
            </h1>
            <span className="text-sm font-normal text-muted-foreground">
              {total ? `${total} 篇` : ''}
            </span>
          </div>
        )}

        {widgets.length > 0 ? (
          <div
            className={`grid gap-8 ${
              site.sidebarPosition === 'left' ? 'lg:grid-cols-[280px_1fr]' : 'lg:grid-cols-[1fr_280px]'
            }`}
          >
            {site.sidebarPosition === 'left' ? (
              <>
                <SiteSidebar widgets={widgets} position="left" />
                <div className="min-w-0">
                  <ArticleListSection
                    isLoading={isLoading}
                    isError={isError}
                    error={error}
                    articles={articles}
                    total={total}
                    hasFilter={hasFilter}
                    hasMore={hasMore}
                    isFetchingMore={isFetchingMore}
                    onLoadMore={() => setLoadedPages((n) => n + 1)}
                    hasSidebar
                  />
                </div>
              </>
            ) : (
              <>
                <div className="min-w-0">
                  <ArticleListSection
                    isLoading={isLoading}
                    isError={isError}
                    error={error}
                    articles={articles}
                    total={total}
                    hasFilter={hasFilter}
                    hasMore={hasMore}
                    isFetchingMore={isFetchingMore}
                    onLoadMore={() => setLoadedPages((n) => n + 1)}
                    hasSidebar
                  />
                </div>
                <SiteSidebar widgets={widgets} position="right" />
              </>
            )}
          </div>
        ) : (
          <ArticleListSection
            isLoading={isLoading}
            isError={isError}
            error={error}
            articles={articles}
            total={total}
            hasFilter={hasFilter}
            hasMore={hasMore}
            isFetchingMore={isFetchingMore}
            onLoadMore={() => setLoadedPages((n) => n + 1)}
            hasSidebar={false}
          />
        )}
      </div>
    </PageTransition>
  )
}

function ArticleListSection({
  isLoading,
  isError,
  error,
  articles,
  total,
  hasFilter,
  hasMore,
  isFetchingMore,
  onLoadMore,
  hasSidebar,
}: {
  isLoading: boolean
  isError: boolean
  error: Error | null
  articles: Article[]
  total: number
  hasFilter: boolean
  hasMore: boolean
  isFetchingMore: boolean
  onLoadMore: () => void
  /** 列表是否与侧边栏并排——并排时正文列更窄，卡片标题降一档字号 */
  hasSidebar: boolean
}) {
  return isLoading ? (
    <PageLoading minHeight="10rem" />
  ) : isError ? (
    <Reveal
      scale={0.96}
      className="rounded-xl border border-red-200 bg-red-50 p-8 text-center dark:border-red-900/50 dark:bg-red-950/30"
    >
      <p className="font-medium text-red-700 dark:text-red-300">加载失败</p>
      <p className="mt-1 text-sm text-red-600/70 dark:text-red-400/70">
        {error instanceof Error ? error.message : '请确认后端服务已启动'}
      </p>
    </Reveal>
  ) : articles.length > 0 ? (
    <>
      <StaggerList className="grid gap-4">
        {articles.map((article: Article) => (
          <PostCard key={article.id} article={article} compact={hasSidebar} />
        ))}
      </StaggerList>
      {/* 加载更多：此前首页写死 page 1 / 20 条且无翻页 UI，第 21 篇起访客
          永远看不到，sitemap 里的文章形同隐藏。这里把剩余篇数显式写出来，
          让用户知道还有内容可看。 */}
      {hasMore && (
        <div className="mt-8 flex justify-center">
          <button
            type="button"
            onClick={onLoadMore}
            disabled={isFetchingMore}
            className="inline-flex items-center gap-2 rounded-lg border border-border bg-card px-5 py-2.5 text-sm font-medium text-foreground transition-colors hover:border-accent/40 hover:text-accent disabled:cursor-not-allowed disabled:opacity-60"
          >
            {isFetchingMore ? (
              <>
                <Spinner className="h-4 w-4" />
                加载中…
              </>
            ) : (
              <>
                <ChevronDown className="h-4 w-4" />
                加载更多（还有 {Math.max(total - articles.length, 0)} 篇）
              </>
            )}
          </button>
        </div>
      )}

      {/* 订阅提示：翻到列表末尾的读者正是「想持续跟进」的人，
          这里给一处不打断阅读的 RSS 入口。href 用 /feed.xml ——
          dev / 直跑 next start 由 next.config 的 rewrite 转发到后端，
          生产由 Nginx 精确匹配接管，两条路径都通。 */}
      <div className="mt-10 flex flex-wrap items-center justify-center gap-2 text-sm text-muted-foreground">
        <Rss className="h-4 w-4" />
        <span>订阅本站更新</span>
        <a
          href="/feed.xml"
          className="font-medium text-accent underline underline-offset-4 transition-opacity hover:opacity-80"
        >
          RSS
        </a>
      </div>
    </>
  ) : (
    <Reveal
      y={16}
      className="rounded-xl border border-dashed p-16 text-center"
    >
      {hasFilter ? (
        <>
          <Search className="mx-auto h-10 w-10 text-muted-foreground/50" />
          <p className="mt-4 text-muted-foreground">没有符合条件的文章</p>
          <Link
            href="/"
            className="mt-4 inline-flex items-center gap-1.5 text-sm text-accent underline underline-offset-4"
          >
            <X className="h-3.5 w-3.5" /> 清除筛选
          </Link>
        </>
      ) : (
        <>
          <PenLine className="mx-auto h-10 w-10 text-muted-foreground/50" />
          <p className="mt-4 text-muted-foreground">还没有文章，来发布第一篇吧</p>
          <Link
            href="/admin/articles/new"
            className="mt-6 inline-block rounded-lg bg-accent px-6 py-2.5 text-sm font-medium text-white transition-transform hover:scale-105"
          >
            写文章
          </Link>
          </>
        )}
      </Reveal>
  )
}
