'use client'

import Link from 'next/link'
import Image from 'next/image'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, CalendarDays, EyeOff, PenLine, SearchX } from 'lucide-react'
import { fetchArticle } from '@/lib/api'
import { proseBody } from '@/lib/ui'
import { useAuth } from '@/lib/auth-context'
import { PageLoading } from '@/components/page-loader'
import { PageTransition, Reveal } from '@/components/motion'

/**
 * 草稿预览：给文章作者在发布前确认排版效果。
 *
 * 为什么单独一页而不是复用 /posts/:slug：访客站的文章详情走的是
 * `/articles/slug/:slug`（匿名请求），草稿在该接口上会被判定为不可见，
 * 于是作者在「我的文章」里点自己的草稿只会看到「文章不存在」。
 * 这里改走 `/articles/:id`（带鉴权），同一套权限判断对作者本人放行。
 */
export function DraftPreview({ id }: { id: number | null }) {
  const { user } = useAuth()

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['article', id],
    queryFn: () => fetchArticle(id!),
    enabled: id !== null,
  })

  if (isLoading) {
    return (
      <div className="mx-auto max-w-3xl px-4 py-12">
        <PageLoading minHeight="2.5rem" hint="加载草稿…" />
        <div className="mt-4">
          <PageLoading minHeight="20rem" hint="加载正文…" />
        </div>
      </div>
    )
  }

  if (isError || !data || id === null) {
    return (
      <Reveal y={0} className="mx-auto max-w-3xl px-4 py-24 text-center">
        <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-muted">
          <SearchX className="h-8 w-8 text-muted-foreground" />
        </div>
        <h1 className="mt-6 text-2xl font-bold">无法查看这篇草稿</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {error instanceof Error && error.message
            ? error.message
            : '它可能已被删除，或你不具备查看权限'}
        </p>
        <Link
          href="/me"
          className="mt-8 inline-flex items-center gap-1.5 rounded-lg bg-accent px-6 py-2.5 text-sm font-medium text-white transition-transform hover:scale-105"
        >
          <ArrowLeft className="h-4 w-4" />
          返回个人中心
        </Link>
      </Reveal>
    )
  }

  const article = data.article
  // 管理员走原来的后台编辑页（那是他们熟悉的工作台，带分类标签管理等完整能力）；
  // 普通作者没有 /admin 权限，layout 会整页拦下，因此给前台的编辑入口。
  const editHref =
    user?.role === 'admin' ? `/admin/articles/edit/${article.id}` : `/me/articles/edit/${article.id}`

  return (
    <PageTransition>
      <div className="mx-auto max-w-3xl px-4 py-12">
        {/* 草稿提示条：常驻可见，避免作者误以为已经发布 */}
        <Reveal
          y={0}
          duration={0.35}
          className="flex flex-wrap items-center gap-3 rounded-xl border border-amber-500/30 bg-amber-500/10 p-4"
        >
          <EyeOff className="h-5 w-5 shrink-0 text-amber-600 dark:text-amber-400" />
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium text-amber-700 dark:text-amber-300">
              草稿预览 · 仅你可见
            </p>
            <p className="mt-0.5 text-xs text-amber-700/80 dark:text-amber-300/80">
              这篇内容还没有发布，访客无法访问。
            </p>
          </div>
          <Link
            href={editHref}
            className="flex shrink-0 items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 transition-transform hover:scale-105"
          >
            <PenLine className="h-4 w-4" />
            继续编辑
          </Link>
        </Reveal>

        <Reveal
          y={16}
          duration={0.45}
          className="mt-6 rounded-2xl border border-border bg-card p-6 shadow-sm sm:p-10"
        >
          <h1 className="text-balance text-3xl font-bold leading-tight tracking-tight">
            {article.title || '（未命名草稿）'}
          </h1>
          <div className="mt-4 flex flex-wrap items-center gap-x-5 gap-y-2 text-sm text-muted-foreground">
            <span className="flex items-center gap-1.5">
              <CalendarDays className="h-4 w-4" />
              更新于 {new Date(article.updated_at).toLocaleString('zh-CN')}
            </span>
          </div>

          {article.cover && (
            <figure className="mt-6">
              {/* 与文章详情页一致：next/image + fill + sizes。
                  草稿封面同样是站点同源的 /uploads 地址，不需要额外域名白名单 */}
              <div className="relative aspect-video w-full overflow-hidden rounded-xl border border-border shadow-sm">
                <Image
                  src={article.cover}
                  alt={article.title}
                  fill
                  sizes="(max-width: 768px) 100vw, 768px"
                  className="object-cover"
                />
              </div>
            </figure>
          )}

          <article
            className={`${proseBody} mt-8`}
            dangerouslySetInnerHTML={{ __html: article.content }}
          />
        </Reveal>

        <div className="mt-6">
          <Link
            href="/me"
            className="inline-flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-accent"
          >
            <ArrowLeft className="h-4 w-4" />
            返回个人中心
          </Link>
        </div>
      </div>
    </PageTransition>
  )
}
