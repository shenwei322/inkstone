'use client'

import Link from 'next/link'
import Image from 'next/image'
import { Suspense, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Eye, PenLine, Search, X } from 'lucide-react'
import { fetchArticles, fetchCategories, fetchTags } from '@/lib/api'
import type { Article, ArticleListResponse } from '@/lib/types'
import { PageLoading, RowLoading } from '@/components/page-loader'
import { PageTransition, StaggerList, StaggerItem, HoverLift, Reveal } from '@/components/motion'
import { useSiteConfig } from '@/components/site-config-context'
import { SiteSidebar } from '@/components/site-sidebar'

function ArticleCard({ article }: { article: Article }) {
  const date = article.published_at
    ? new Date(article.published_at).toLocaleDateString('zh-CN')
    : new Date(article.created_at).toLocaleDateString('zh-CN')

  // 封面：优先使用后台设置的封面，否则取正文中第一张图片；摘要由正文纯文本生成。
  // 用 useMemo 缓存，避免列表每次渲染都做多段正则。
  const { cover, excerpt } = useMemo(() => {
    const c =
      article.cover || (article.content.match(/<img[^>]+src="([^"]+)"/)?.[1] ?? '')
    const e = article.content
      .replace(/<[^>]*>/g, ' ')
      .replace(/\s+/g, ' ')
      .trim()
      .slice(0, 200)
    return { cover: c, excerpt: e }
  }, [article])

  return (
    <StaggerItem>
      <HoverLift>
        <Link href={`/posts/${article.slug}`} className="block">
          <article className="group relative flex flex-col overflow-hidden rounded-xl border border-border bg-card shadow-sm transition-all duration-300 hover:border-accent/30 hover:shadow-lg hover:shadow-accent/5 sm:flex-row sm:gap-5 sm:p-5">
            {/* 顶部 accent 渐变条：悬停时从左展开（scaleX，不做 width 动画） */}
            <div className="absolute inset-x-0 top-0 z-10 h-0.5 origin-left scale-x-0 bg-gradient-to-r from-accent to-purple-500 transition-transform duration-300 group-hover:scale-x-100" />

            {cover && (
              <div className="relative aspect-video w-full shrink-0 overflow-hidden bg-muted sm:aspect-auto sm:h-28 sm:w-44 sm:rounded-lg">
                <Image
                  src={cover}
                  alt={article.title}
                  fill
                  sizes="(max-width: 640px) 100vw, 176px"
                  className="object-cover transition-transform duration-500 group-hover:scale-110"
                  onError={(e) => {
                    ;(e.currentTarget.parentElement as HTMLElement).style.display = 'none'
                  }}
                />
              </div>
            )}

            <div className="flex min-w-0 flex-1 flex-col p-5 sm:p-0">
              <div className="flex items-start justify-between gap-3">
                <h2 className="text-lg font-semibold leading-snug tracking-tight text-balance transition-colors group-hover:text-accent">
                  {article.title}
                </h2>
                {article.category && (
                  <span className="shrink-0 rounded-full bg-accent/10 px-2.5 py-0.5 text-xs font-medium text-accent">
                    {article.category.name}
                  </span>
                )}
              </div>
              {/* 摘要：两行截断，行高放宽提升可读性 */}
              <p className="mt-2 line-clamp-2 text-sm leading-relaxed text-muted-foreground">
                {excerpt}
              </p>
              {/* 底部元信息：作者/日期/阅读量，标签贴右侧（仅 sm 以上） */}
              <div className="mt-4 flex flex-wrap items-center gap-x-3 gap-y-2 text-xs text-muted-foreground sm:mt-auto sm:pt-4">
                <span className="flex items-center gap-1.5">
                  <span className="flex h-6 w-6 items-center justify-center rounded-full bg-accent/10 text-[10px] font-bold text-accent">
                    {article.author.username.charAt(0).toUpperCase()}
                  </span>
                  <span className="font-medium text-foreground">{article.author.username}</span>
                </span>
                <span aria-hidden className="text-border">
                  ·
                </span>
                <time dateTime={article.published_at ?? article.created_at}>{date}</time>
                <span className="flex items-center gap-1">
                  <Eye className="h-3 w-3" />
                  {article.views}
                </span>
                {article.tags && article.tags.length > 0 && (
                  <span className="ml-auto hidden items-center gap-1.5 sm:flex">
                    {article.tags.slice(0, 3).map((tag) => (
                      <span
                        key={tag.id}
                        className="rounded border border-border bg-muted/40 px-1.5 py-0.5 transition-colors group-hover:border-accent/30"
                      >
                        {tag.name}
                      </span>
                    ))}
                  </span>
                )}
              </div>
            </div>
          </article>
        </Link>
      </HoverLift>
    </StaggerItem>
  )
}

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
        <div className="mx-auto max-w-5xl px-4 py-10">
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

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['articles', 'published', 1, category, tag, q],
    queryFn: () =>
      fetchArticles({
        page: 1,
        page_size: 20,
        category: category ?? undefined,
        tag: tag ?? undefined,
        q: q ?? undefined,
      }),
  })

  const categoriesQuery = useQuery({ queryKey: ['categories'], queryFn: fetchCategories })
  const tagsQuery = useQuery({ queryKey: ['tags'], queryFn: fetchTags })

  const hasFilter = Boolean(category || tag || q)

  return (
    <PageTransition>
      <div className={`mx-auto px-4 py-10 ${widgets.length > 0 ? 'max-w-7xl' : 'max-w-5xl'}`}>
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
              {data ? `${data.total} 篇` : ''}
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
                    data={data}
                    hasFilter={hasFilter}
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
                    data={data}
                    hasFilter={hasFilter}
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
            data={data}
            hasFilter={hasFilter}
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
  data,
  hasFilter,
}: {
  isLoading: boolean
  isError: boolean
  error: Error | null
  data?: ArticleListResponse
  hasFilter: boolean
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
  ) : data && data.articles.length > 0 ? (
    <StaggerList className="grid gap-4">
      {data.articles.map((article: Article) => (
        <ArticleCard key={article.id} article={article} />
      ))}
    </StaggerList>
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
