'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { NewArticlePage } from '@/components/article-editor'
import { useAuth } from '@/lib/auth-context'
import { PageLoading } from '@/components/page-loader'

/**
 * 前台投稿入口（普通用户）。
 *
 * 为什么不在 /admin/articles/new：后台 layout 按角色把非 admin 整页拦下，
 * 而后端 `POST /articles` 本身只要求登录、不限角色——也就是说普通作者
 * 一直有权限发文，只是前台从来没有入口。这里补上那个入口。
 *
 * redirectBase="/me"：发布/存草稿后落回个人中心，而不是后台编辑页。
 */
export default function MeNewArticlePage() {
  const { user, loading } = useAuth()
  const router = useRouter()

  useEffect(() => {
    if (!loading && !user) router.push('/login')
  }, [loading, user, router])

  if (loading || !user) {
    return (
      <div className="mx-auto max-w-5xl px-4 py-10">
        <PageLoading minHeight="3.5rem" className="rounded-lg" />
        <PageLoading minHeight="3rem" className="mt-4 rounded-lg" />
        <PageLoading minHeight="24rem" className="mt-4 rounded-lg" />
      </div>
    )
  }

  return <NewArticlePage redirectBase="/me" />
}
