'use client'

import { useEffect, use } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { EditArticlePage } from '@/components/article-editor'
import { useAuth } from '@/lib/auth-context'
import { PageLoading } from '@/components/page-loader'

/**
 * 前台编辑自己的文章（普通用户）。
 *
 * 与后台 `PUT /articles/:id` 的权限一致：只有作者本人能改（后端在
 * service 层校验 AuthorID），所以这里不需要额外的角色判断，传错 id 会被
 * 后端拒绝并渲染「文章不存在或无权访问」。
 */
export default function MeEditArticlePage({
  params,
}: {
  params: Promise<{ id: string }>
}) {
  const { id } = use(params)
  const articleId = Number(id)
  const { user, loading } = useAuth()
  const router = useRouter()

  useEffect(() => {
    if (!loading && !user) router.push('/login')
  }, [loading, user, router])

  if (!Number.isFinite(articleId) || articleId <= 0) {
    return (
      <div className="mx-auto max-w-5xl px-4 py-24 text-center text-sm text-muted-foreground">
        无效的文章 ID，
        <Link href="/me" className="text-accent underline underline-offset-4">
          返回个人中心
        </Link>
      </div>
    )
  }

  if (loading || !user) {
    return (
      <div className="mx-auto max-w-5xl px-4 py-10">
        <PageLoading minHeight="3.5rem" className="rounded-lg" />
        <PageLoading minHeight="3rem" className="mt-4 rounded-lg" />
        <PageLoading minHeight="24rem" className="mt-4 rounded-lg" />
      </div>
    )
  }

  return <EditArticlePage id={articleId} redirectBase="/me" />
}
