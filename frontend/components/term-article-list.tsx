'use client'

import { useState } from 'react'
import Link from 'next/link'
import { FolderOpen } from 'lucide-react'
import { useQueries } from '@tanstack/react-query'
import { fetchArticles } from '@/lib/api'
import { PageLoading, Spinner } from '@/components/page-loader'
import { PageTransition, Reveal, StaggerList, StaggerItem } from '@/components/motion'

const PAGE_SIZE = 20

// 分类/标签下的文章列表。抽成共用组件，/category/[slug] 与 /tag/[slug]
// 复用——两边的差异只是筛选参数名与标题文案。
export function TermArticleList({
  kind,
  slug,
  name,
}: {
  kind: 'category' | 'tag'
  slug: string
  name: string
}) {
  const [loadedPages, setLoadedPages] = useState(1)

  const pageQueries = useQueries({
    queries: Array.from({ length: loadedPages }, (_, i) => {
      const page = i + 1
      return {
        queryKey: [kind, slug, page],
        queryFn: () => fetchArticles({ page, page_size: PAGE_SIZE, [kind]: slug }),
      }
    }),
  })

  const articles = pageQueries.flatMap((r) => r.data?.articles ?? [])
  const total = pageQueries[0]?.data?.total ?? 0
  const isLoading = pageQueries.some((r) => r.isLoading)
  const isError = pageQueries.some((r) => r.isError)
  const isFetchingMore = pageQueries[loadedPages - 1]?.isFetching === true
  const hasMore = articles.length < total

  return (
    <>
      <h1 className="flex items-center gap-2 text-2xl font-bold tracking-tight">
        <FolderOpen className="h-6 w-6 text-accent" />
        {kind === 'category' ? '分类' : '标签'}：{name}
      </h1>
      <p className="mt-2 text-sm text-muted-foreground">
        {total > 0 ? `该${kind === 'category' ? '分类' : '标签'}下共 ${total} 篇文章` : '暂无文章'}
      </p>

      {isLoading ? (
        <div className="mt-10">
          <PageLoading minHeight="10rem" />
        </div>
      ) : isError ? (
        <Reveal y={16} className="mt-10 rounded-xl border border-dashed p-16 text-center">
          <p className="text-muted-foreground">加载失败，请稍后重试</p>
        </Reveal>
      ) : articles.length === 0 ? (
        <Reveal y={16} className="mt-10 rounded-xl border border-dashed p-16 text-center">
          <FolderOpen className="mx-auto h-10 w-10 text-muted-foreground/50" />
          <p className="mt-4 text-muted-foreground">这个{kind === 'category' ? '分类' : '标签'}下还没有文章</p>
          <Link
            href="/"
            className="mt-4 inline-flex items-center gap-1.5 text-sm text-accent underline underline-offset-4"
          >
            回到首页
          </Link>
        </Reveal>
      ) : (
        <>
          <StaggerList className="mt-6 grid gap-3">
            {articles.map((a) => (
              <StaggerItem key={a.id}>
                <Link href={`/posts/${a.slug}`} className="block">
                  <article className="rounded-xl border border-border bg-card p-5 shadow-sm transition-all duration-300 hover:border-accent/30 hover:shadow-lg hover:shadow-accent/5">
                    <h2 className="text-base font-semibold leading-snug tracking-tight transition-colors hover:text-accent">
                      {a.title}
                    </h2>
                    <p className="mt-2 line-clamp-2 text-sm text-muted-foreground">
                      {a.content
                        .replace(/<[^>]*>/g, ' ')
                        .replace(/\s+/g, ' ')
                        .trim()
                        .slice(0, 120)}
                    </p>
                    <div className="mt-3 flex items-center gap-3 text-xs text-muted-foreground/80">
                      <span>
                        {(a.published_at ?? a.created_at
                          ? new Date(a.published_at ?? a.created_at).toLocaleDateString('zh-CN')
                          : '') as string}
                      </span>
                      <span>{a.views} 次阅读</span>
                    </div>
                  </article>
                </Link>
              </StaggerItem>
            ))}
          </StaggerList>
          {hasMore && (
            <div className="mt-8 flex justify-center">
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
                  `加载更多（还有 ${Math.max(total - articles.length, 0)} 篇）`
                )}
              </button>
            </div>
          )}
        </>
      )}
    </>
  )
}

export function TermPageShell({ children }: { children: React.ReactNode }) {
  return (
    <PageTransition>
      <div className="mx-auto max-w-3xl px-4 py-10">{children}</div>
    </PageTransition>
  )
}
