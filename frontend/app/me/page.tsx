'use client'

import { useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import gsap from 'gsap'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Eye,
  FileText,
  KeyRound,
  Lock,
  MessageSquare,
  PenLine,
  Star,
  Trash2,
  UserRoundCog,
} from 'lucide-react'
import {
  changePassword,
  deleteComment,
  fetchArticles,
  fetchMyComments,
  fetchMyFavorites,
  updateProfile,
  ApiError,
} from '@/lib/api'
import { useAuth } from '@/lib/auth-context'
import { useNotify } from '@/components/toast'
import { PageLoading, RowLoading } from '@/components/page-loader'
import { PageTransition, Reveal, hoverTapScale, prefersReducedMotion } from '@/components/motion'
import { inputClass } from '@/lib/ui'
import type { Article } from '@/lib/types'


function AccountTab() {
  const { user } = useAuth()
  const notify = useNotify()
  const queryClient = useQueryClient()
  const [username, setUsername] = useState(user?.username ?? '')
  const [currentPw, setCurrentPw] = useState('')
  const [newPw, setNewPw] = useState('')

  useEffect(() => {
    if (user?.username) {
      const t = setTimeout(() => setUsername(user.username), 0)
      return () => clearTimeout(t)
    }
  }, [user?.username])

  const rename = useMutation({
    mutationFn: () => updateProfile(username),
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['admin'] })
      notify.success(`用户名已更新为「${res.user.username}」`)
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '修改失败'),
  })

  const changePw = useMutation({
    mutationFn: () => changePassword(currentPw, newPw),
    onSuccess: () => {
      setCurrentPw('')
      setNewPw('')
      notify.success('密码已更新，下次登录请使用新密码')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '修改失败'),
  })

  return (
    <div className="space-y-4">
      <Reveal
        y={12}
        duration={0.4}
        className="rounded-xl border border-border bg-card p-5"
      >
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <UserRoundCog className="h-4 w-4 text-accent" />
          个人资料
        </h2>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <label className="text-sm font-medium">用户名</label>
            <input
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              className={inputClass}
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-sm font-medium">邮箱（登录账号，不可修改）</label>
            <input value={user?.email ?? ''} disabled className={`${inputClass} opacity-60`} />
          </div>
        </div>
        <div className="mt-4 flex justify-end">
          <button
            type="button"
            onClick={() => {
              if (!username.trim()) {
                notify.error('用户名不能为空')
                return
              }
              rename.mutate()
            }}
            disabled={rename.isPending || username === user?.username}
            {...hoverTapScale}
            className="rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
          >
            {rename.isPending ? '保存中...' : '保存资料'}
          </button>
        </div>
      </Reveal>

      <Reveal
        y={12}
        delay={0.08}
        duration={0.4}
        className="rounded-xl border border-border bg-card p-5"
      >
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <KeyRound className="h-4 w-4 text-accent" />
          修改密码
        </h2>
        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <label className="text-sm font-medium">当前密码</label>
            <input
              type="password"
              value={currentPw}
              onChange={(e) => setCurrentPw(e.target.value)}
              placeholder="••••••••"
              className={inputClass}
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-sm font-medium">新密码（至少 8 位）</label>
            <input
              type="password"
              value={newPw}
              onChange={(e) => setNewPw(e.target.value)}
              placeholder="••••••••"
              className={inputClass}
            />
          </div>
        </div>
        <div className="mt-4 flex justify-end">
          <button
            type="button"
            onClick={() => {
              if (!currentPw || !newPw) {
                notify.error('请填写当前密码和新密码')
                return
              }
              changePw.mutate()
            }}
            disabled={changePw.isPending}
            {...hoverTapScale}
            className="rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
          >
            {changePw.isPending ? '提交中...' : '更新密码'}
          </button>
        </div>
      </Reveal>

      <Reveal
        y={12}
        delay={0.16}
        duration={0.4}
        className="flex items-start gap-2.5 rounded-xl border border-dashed border-border p-4 text-xs text-muted-foreground"
      >
        <Lock className="mt-0.5 h-4 w-4 shrink-0" />
        <span>账户安全提示：密码至少 8 位，建议混合字母、数字与符号；不要与其他网站使用相同密码。</span>
      </Reveal>
    </div>
  )
}

function MyArticlesTab() {
  const { user } = useAuth()

  const draftsQuery = useQuery({
    queryKey: ['me', 'articles', 'draft'],
    queryFn: () => fetchArticles({ page: 1, page_size: 50, status: 'draft' }),
  })
  const publishedQuery = useQuery({
    queryKey: ['me', 'articles', 'published'],
    queryFn: () => fetchArticles({ page: 1, page_size: 50 }),
  })
  // scheduled 要单独查：列表接口空 status 只返回已发布，status='draft'
  // 的查询又不含它。不补这个查询，用户定时发布的文章在「我的文章」
  // 里根本看不到，也就无从确认它什么时候上线。
  const scheduledQuery = useQuery({
    queryKey: ['me', 'articles', 'scheduled'],
    queryFn: () => fetchArticles({ page: 1, page_size: 50, status: 'scheduled' }),
  })

  const mine = (publishedQuery.data?.articles ?? [])
    .filter((a) => a.author.id === user?.id)
    .concat(draftsQuery.data?.articles ?? [])
    .concat(scheduledQuery.data?.articles ?? [])

  if (draftsQuery.isLoading || publishedQuery.isLoading || scheduledQuery.isLoading) {
    return <RowLoading rows={3} />
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <p className="text-sm text-muted-foreground">共 {mine.length} 篇</p>
        {/* 投稿入口指向 /me/articles/new 而不是 /admin/articles/new：
            admin/layout 会把非 admin 用户整页拦下，普通作者点进去只会看到权限提示 */}
        <Link
          href="/me/articles/new"
          className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 transition-transform hover:scale-105"
        >
          <PenLine className="h-4 w-4" />
          写文章
        </Link>
      </div>

      {mine.length === 0 ? (
        <div className="rounded-xl border border-dashed p-12 text-center text-muted-foreground">
          还没有写过文章，点右上角「写文章」开始第一篇
        </div>
      ) : (
        mine.map((article: Article, i) => {
          // 已发布走公开 slug 页；草稿与定时发布都走带鉴权的 id 预览页
          // （/me/drafts/:id，作者本人免密可看自己的 scheduled 文章）。
          // 草稿复用 /posts/:slug 会因匿名请求拿到 404「文章不存在」。
          const detailHref =
            article.status === 'published' ? `/posts/${article.slug}` : `/me/drafts/${article.id}`
          return (
            <Reveal
              key={article.id}
              y={10}
              delay={i * 0.04}
              duration={0.3}
              className="flex items-center justify-between rounded-xl border border-border bg-card px-5 py-4"
            >
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <Link
                    href={detailHref}
                    className="truncate font-medium transition-colors hover:text-accent"
                  >
                    {article.title}
                  </Link>
                  {article.status === 'published' ? (
                    <span className="rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs text-emerald-600 dark:text-emerald-400">
                      已发布
                    </span>
                  ) : article.status === 'scheduled' ? (
                    <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                      定时发布
                    </span>
                  ) : (
                    <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-xs text-amber-600 dark:text-amber-400">
                      草稿
                    </span>
                  )}
                </div>
                <p className="mt-1 flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                  <span className="flex items-center gap-1">
                    <Eye className="h-3 w-3" />
                    {article.views}
                  </span>
                  <span>
                    更新于 {new Date(article.updated_at).toLocaleDateString('zh-CN')}
                  </span>
                  {article.status === 'scheduled' && article.scheduled_at && (
                    <span>
                      定于 {new Date(article.scheduled_at).toLocaleString('zh-CN')} 发布
                    </span>
                  )}
                </p>
              </div>
              <div className="ml-4 shrink-0 text-xs text-muted-foreground">
                {/* 已发布给「查看」、草稿与定时发布给「预览」，与标题链接同一落点 */}
                <Link href={detailHref} className="transition-colors hover:text-accent">
                  {article.status === 'published' ? '查看' : '预览'}
                </Link>
              </div>
            </Reveal>
          )
        })
      )}
    </div>
  )
}

function MyCommentsTab() {
  const notify = useNotify()
  const queryClient = useQueryClient()
  const { data, isLoading } = useQuery({
    queryKey: ['me', 'comments'],
    queryFn: () => fetchMyComments({ page: 1, page_size: 50 }),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteComment(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['me', 'comments'] })
      notify.success('评论已删除')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '删除失败'),
  })

  if (isLoading) {
    return <RowLoading rows={3} />
  }

  const comments = data?.comments ?? []
  if (comments.length === 0) {
    return (
      <div className="rounded-xl border border-dashed p-12 text-center text-muted-foreground">
        还没有发表过评论
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {comments.map((c, i) => (
        <Reveal
          key={c.id}
          y={10}
          delay={i * 0.04}
          duration={0.3}
          className="rounded-xl border border-border bg-card p-4"
        >
          <div className="flex items-center justify-between gap-2">
            <p className="text-xs text-muted-foreground">
              {c.article_slug ? (
                <Link
                  href={`/posts/${c.article_slug}`}
                  className="font-medium text-foreground transition-colors hover:text-accent"
                >
                  {c.article_title}
                </Link>
              ) : (
                (c.article_title ?? '文章')
              )}
              {' · '}
              {new Date(c.created_at).toLocaleString('zh-CN')}
            </p>
            <button
              onClick={async () => {
                const ok = await notify.confirm({
                  title: '删除这条评论？',
                  message: '删除后无法恢复。',
                  confirmText: '删除',
                  danger: true,
                })
                if (ok) deleteMutation.mutate(c.id)
              }}
              disabled={deleteMutation.isPending}
              className="text-muted-foreground transition-colors hover:text-red-500 disabled:opacity-50"
              title="删除"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          </div>
          <p className="mt-2 whitespace-pre-wrap text-sm leading-relaxed">{c.content}</p>
        </Reveal>
      ))}
    </div>
  )
}

function MyFavoritesTab() {
  const { data, isLoading } = useQuery({
    queryKey: ['me', 'favorites'],
    queryFn: () => fetchMyFavorites({ page: 1, page_size: 50 }),
  })

  if (isLoading) {
    return <RowLoading rows={3} />
  }

  const list = data?.articles ?? []
  if (list.length === 0) {
    return (
      <div className="rounded-xl border border-dashed p-12 text-center">
        <Star className="mx-auto h-8 w-8 text-muted-foreground/50" />
        <p className="mt-3 text-sm text-muted-foreground">还没有收藏文章</p>
        <p className="mt-1 text-xs text-muted-foreground">
          在文章页点「收藏」，之后就能在这里快速找回
        </p>
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {list.map((article, i) => (
        <Reveal
          key={article.id}
          y={10}
          delay={i * 0.04}
          duration={0.3}
          className="flex items-center justify-between rounded-xl border border-border bg-card px-5 py-4"
        >
          <div className="min-w-0">
            <Link
              href={`/posts/${article.slug}`}
              className="truncate font-medium transition-colors hover:text-accent"
            >
              {article.title}
            </Link>
            <p className="mt-1 flex items-center gap-3 text-xs text-muted-foreground">
              <span className="flex items-center gap-1">
                <Eye className="h-3 w-3" />
                {article.views}
              </span>
              <span>{article.author.username}</span>
              {/* 后端只返回文章本身，没有"收藏时间"字段，这里展示发布时间，
                  避免把 created_at 冒充成"收藏于"造成误导 */}
              <span>
                发布于{' '}
                {new Date(article.published_at ?? article.created_at).toLocaleDateString('zh-CN')}
              </span>
            </p>
          </div>
          <div className="ml-4 shrink-0 text-xs text-muted-foreground">
            <Link href={`/posts/${article.slug}`} className="transition-colors hover:text-accent">
              查看
            </Link>
          </div>
        </Reveal>
      ))}
    </div>
  )
}

export default function MePage() {
  const { user, loading } = useAuth()
  const router = useRouter()
  const [tab, setTab] = useState<'account' | 'articles' | 'comments' | 'favorites'>('account')

  useEffect(() => {
    if (!loading && !user) router.push('/login')
  }, [loading, user, router])

  // 选中标签的滑动底块（原 layoutId 共享布局动画，改用重挂载 + scaleX 入场）
  const pillRef = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    if (pillRef.current && !prefersReducedMotion()) {
      gsap.fromTo(pillRef.current, { scaleX: 0 }, { scaleX: 1, duration: 0.3, ease: 'expo.out' })
    }
  }, [tab])

  if (loading || !user) {
    return (
      <div className="mx-auto max-w-4xl px-4 py-12">
        <PageLoading minHeight="10rem" />
      </div>
    )
  }

  const tabs = [
    { key: 'account' as const, label: '账户安全', icon: UserRoundCog },
    { key: 'articles' as const, label: '我的文章', icon: FileText },
    { key: 'favorites' as const, label: '我的收藏', icon: Star },
    { key: 'comments' as const, label: '我的评论', icon: MessageSquare },
  ]

  return (
    <PageTransition>
      <div className="mx-auto max-w-4xl px-4 py-10">
        <Reveal
          y={16}
          duration={0.45}
          className="relative overflow-hidden rounded-xl border border-border bg-gradient-to-br from-accent/10 via-card to-purple-500/10 p-6"
        >
          <div className="pointer-events-none absolute -right-8 -top-8 h-28 w-28 rounded-full bg-accent/15 blur-2xl" />
          <div className="relative flex items-center gap-4">
            <div className="flex h-16 w-16 items-center justify-center rounded-full bg-accent/15 text-2xl font-black text-accent">
              {user.username.charAt(0).toUpperCase()}
            </div>
            <div>
              <h1 className="text-xl font-bold tracking-tight">{user.username}</h1>
              <p className="mt-0.5 text-sm text-muted-foreground">{user.email}</p>
            </div>
            {user.role === 'admin' && (
              <span className="ml-auto rounded-full bg-purple-500/10 px-3 py-1 text-xs font-medium text-purple-600 dark:text-purple-400">
                管理员
              </span>
            )}
          </div>
        </Reveal>

        <div className="mt-6 flex items-center gap-1 rounded-lg border border-border bg-card p-1">
          {tabs.map((t) => (
            <button
              key={t.key}
              onClick={() => setTab(t.key)}
              className={`relative flex items-center gap-1.5 rounded-md px-4 py-1.5 text-sm transition-colors ${
                tab === t.key ? 'text-white' : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {tab === t.key && (
                <span
                  key={t.key}
                  ref={pillRef}
                  className="absolute inset-0 origin-left rounded-md bg-accent"
                />
              )}
              <t.icon className="relative h-4 w-4" />
              <span className="relative">{t.label}</span>
            </button>
          ))}
        </div>

        <Reveal
          key={tab}
          y={12}
          duration={0.3}
          className="mt-4"
        >
          {tab === 'account' ? (
            <AccountTab />
          ) : tab === 'articles' ? (
            <MyArticlesTab />
          ) : tab === 'favorites' ? (
            <MyFavoritesTab />
          ) : (
            <MyCommentsTab />
          )}
        </Reveal>
      </div>
    </PageTransition>
  )
}
