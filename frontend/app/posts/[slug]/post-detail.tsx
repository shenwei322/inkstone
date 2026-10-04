'use client'

import { useMemo, useRef, useState } from 'react'
import Link from 'next/link'
import Image from 'next/image'
import { useRouter } from 'next/navigation'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowLeft,
  CalendarDays,
  Eye,
  Heart,
  MessageSquare,
  Star,
  Tag as TagIcon,
  SearchX,
  Trash2,
} from 'lucide-react'
import {
  deleteComment,
  fetchArticleBySlug,
  fetchComments,
  fetchReactions,
  postComment,
  toggleReaction,
} from '@/lib/api'
import { proseBody } from '@/lib/ui'
import { useAuth } from '@/lib/auth-context'
import { useNotify } from '@/components/toast'
import { useSiteConfig } from '@/components/site-config-context'
import { useCaptcha } from '@/components/captcha'
import { useIsMounted } from '@/lib/use-mounted'
import { ArticleToc, parseToc } from '@/components/article-toc'
import { ArticleShare } from '@/components/article-share'
import { BackToTop } from '@/components/back-to-top'
import { useCodeHighlight } from '@/components/code-highlight'
import { PageTransition, Reveal, hoverTapScale, useReveal } from '@/components/motion'
import { PageLoading, RowLoading } from '@/components/page-loader'
import { SiteSidebar } from '@/components/site-sidebar'

export function PostDetail({ slug }: { slug: string }) {
  const { user } = useAuth()
  const notify = useNotify()
  const router = useRouter()
  const queryClient = useQueryClient()
  const [commentText, setCommentText] = useState('')
  const site = useSiteConfig()
  const captcha = useCaptcha('comment')

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['article', 'slug', slug],
    queryFn: () => fetchArticleBySlug(slug),
  })

  const articleId = data?.article.id

  const reactionsQuery = useQuery({
    queryKey: ['reactions', articleId],
    queryFn: () => fetchReactions(articleId!),
    enabled: Boolean(articleId),
  })

  const commentsQuery = useQuery({
    queryKey: ['comments', articleId],
    queryFn: () => fetchComments(articleId!),
    enabled: Boolean(articleId),
  })

  const toggle = useMutation({
    mutationFn: (type: 'like' | 'favorite') => toggleReaction(articleId!, type),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['reactions', articleId] }),
    onError: (e) => notify.error(e instanceof Error ? e.message : '操作失败'),
  })

  const addComment = useMutation({
    // 开启人机验证时先弹窗验证，通过后携带凭证提交
    mutationFn: async () => {
      const credential = captcha.enabled ? await captcha.run() : undefined
      return postComment(articleId!, commentText, credential)
    },
    onSuccess: () => {
      setCommentText('')
      queryClient.invalidateQueries({ queryKey: ['comments', articleId] })
      notify.success('评论已发布')
    },
    onError: (e) => {
      // 用户主动关闭验证框时不打扰
      if (e instanceof Error && e.message === '人机验证已取消') return
      notify.error(e instanceof Error ? e.message : '评论失败')
    },
  })

  const removeComment = useMutation({
    mutationFn: (id: number) => deleteComment(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['comments', articleId] })
      notify.success('评论已删除')
    },
    onError: (e) => notify.error(e instanceof Error ? e.message : '删除评论失败'),
  })

  // 文章目录：挂载后从正文 HTML 提取（SSR 无 document 返回空，避免 hydration 差异）
  const contentRef = useRef<HTMLDivElement>(null)
  const isMounted = useIsMounted()
  const articleHtml = data?.article.content ?? ''
  const tocItems = useMemo(() => (isMounted ? parseToc(articleHtml) : []), [isMounted, articleHtml])

  // 正文代码块 hljs 高亮 + 复制按钮（复制成功弹 toast）
  useCodeHighlight(contentRef, articleHtml, {
    onCopySuccess: () => notify.success('代码已复制'),
  })

  // 标题卡片为语义 header 标签：用 useReveal 挂入场动效，不改 DOM 结构
  const headerRef = useRef<HTMLElement>(null)
  useReveal(headerRef, { duration: 0.5 })
  useReveal(contentRef, { duration: 0.5 })

  if (isLoading) {
    return (
      <div className="mx-auto max-w-3xl px-4 py-12">
        {/* 加载态统一使用 GSAP 加载动画，替换原骨架图 */}
        <PageLoading minHeight="2.5rem" hint="加载标题…" />
        <div className="mt-4">
          <PageLoading minHeight="1rem" hint="加载摘要…" />
        </div>
        <div className="mt-10">
          <PageLoading minHeight="24rem" hint="加载正文…" />
        </div>
      </div>
    )
  }

  if (isError || !data) {
    return (
      <Reveal
        y={0}
        className="mx-auto max-w-3xl px-4 py-24 text-center"
      >
        <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-muted">
          <SearchX className="h-8 w-8 text-muted-foreground" />
        </div>
        <h1 className="mt-6 text-2xl font-bold">文章不存在</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {error instanceof Error ? error.message : '请检查链接是否正确'}
        </p>
        <Link
          href="/"
          className="mt-8 inline-block rounded-lg bg-accent px-6 py-2.5 text-sm font-medium text-white transition-transform hover:scale-105"
        >
          返回首页
        </Link>
      </Reveal>
    )
  }

  const article = data.article
  const date = article.published_at
    ? new Date(article.published_at).toLocaleDateString('zh-CN')
    : new Date(article.created_at).toLocaleDateString('zh-CN')
  const reactions = reactionsQuery.data
  const comments = commentsQuery.data?.comments ?? []

  const requireLogin = () => {
    if (!user) {
      notify.error('请先登录')
      router.push('/login')
      return true
    }
    return false
  }

  const widgets = site.widgets.filter((w) => w.type && w.title)
  const showSidebar = site.articleSidebar && widgets.length > 0

  // 目录贴「没有侧边栏的一侧」：无侧边栏→目录右；侧边栏在右→目录左；侧边栏在左→目录右
  // 注意：渲染顺序必须与 layoutClass 的 grid 列模板一致（先渲染的 DOM 落在第一列）
  // 只有带文字的标题才算「有目录」，与 ArticleToc 内部的过滤口径保持一致
  // （parseToc 不再剔除空标题，以保证 id 与真实 DOM 下标对齐）
  const showToc = tocItems.some((item) => item.text)
  const tocPosition: 'left' | 'right' =
    showSidebar && site.sidebarPosition !== 'left' ? 'left' : 'right'

  const layoutClass = showSidebar
    ? showToc
      ? site.sidebarPosition === 'left'
        ? 'max-w-7xl lg:grid lg:grid-cols-[280px_minmax(0,1fr)_220px] lg:gap-8'
        : 'max-w-7xl lg:grid lg:grid-cols-[220px_minmax(0,1fr)_280px] lg:gap-8'
      : site.sidebarPosition === 'left'
        ? 'max-w-7xl lg:grid lg:grid-cols-[280px_minmax(0,1fr)] lg:gap-10'
        : 'max-w-7xl lg:grid lg:grid-cols-[minmax(0,1fr)_280px] lg:gap-10'
    : showToc
      ? 'max-w-5xl lg:grid lg:grid-cols-[minmax(0,1fr)_220px] lg:gap-8'
      : 'max-w-4xl'

  return (
    <PageTransition>
      <div className={`mx-auto px-4 py-12 ${layoutClass}`}>
      {showSidebar && site.sidebarPosition === 'left' && (
        <SiteSidebar widgets={widgets} position="left" />
      )}
      {showToc && tocPosition === 'left' && (
        <ArticleToc items={tocItems} contentRef={contentRef} />
      )}
      <div className="min-w-0">
        {/* 顶部标题区：分类徽章 + 蓝色竖线标题 + 图标信息栏（圆角卡片） */}
        <header
          ref={headerRef}
          className="relative overflow-hidden rounded-2xl border border-border bg-card p-6 shadow-sm sm:p-8"
        >
          {/* 右上角装饰光斑：纯 opacity 呼吸（不做 transform，避免 CPU 合成下每帧重绘） */}
          <span
            aria-hidden
            className="pointer-events-none absolute -right-16 -top-16 h-40 w-40 rounded-full bg-accent/5 blur-3xl"
          />
          {article.category && (
            <Link
              href={`/?category=${article.category.slug}`}
              className="relative inline-block rounded-full bg-accent/10 px-3 py-1 text-xs font-medium text-accent transition-colors hover:bg-accent/20"
            >
              {article.category.name}
            </Link>
          )}
          <div className="relative mt-3 flex items-stretch gap-3">
            <span className="w-1.5 shrink-0 rounded-full bg-accent" />
            <h1 className="text-balance text-3xl font-bold leading-tight tracking-tight sm:text-4xl">
              {article.title}
            </h1>
          </div>
          {/* 元信息栏：图标 + 文字成组，组间用圆点分隔，窄屏自动折行 */}
          <div className="relative mt-5 flex flex-wrap items-center gap-x-5 gap-y-2 text-sm text-muted-foreground">
            <span className="flex items-center gap-1.5">
              <span className="flex h-6 w-6 items-center justify-center rounded-full bg-accent/10 text-[10px] font-bold text-accent">
                {article.author.username.charAt(0).toUpperCase()}
              </span>
              <Link href="/" className="font-medium text-foreground transition-colors hover:text-accent">
                {article.author.username}
              </Link>
            </span>
            <span aria-hidden className="hidden text-border sm:inline">
              ·
            </span>
            <span className="flex items-center gap-1.5">
              <CalendarDays className="h-4 w-4" />
              <time dateTime={article.published_at ?? article.created_at}>{date}</time>
            </span>
            <span aria-hidden className="hidden text-border sm:inline">
              ·
            </span>
            <span className="flex items-center gap-1.5">
              <Eye className="h-4 w-4" />
              {article.views} 次阅读
            </span>
          </div>
        </header>

        {/* 主体：白色圆角容器，居中封面 + 正文 */}
        <Reveal
          delay={0.05}
          duration={0.55}
          className="mt-6 rounded-2xl border border-border bg-card p-6 shadow-sm sm:p-10"
        >
          {/* 封面：仅设置了文章图片时展示，无封面不渲染占位图 */}
          {article.cover && (
            <figure className="mb-9">
              <div className="relative aspect-video w-full overflow-hidden rounded-xl border border-border shadow-sm">
                <Image
                  src={article.cover}
                  alt={article.title}
                  fill
                  sizes="(max-width: 1024px) 100vw, 768px"
                  className="object-cover"
                />
              </div>
            </figure>
          )}

          <article
            ref={contentRef}
            className={proseBody}
            dangerouslySetInnerHTML={{ __html: article.content }}
          />

          {article.tags && article.tags.length > 0 && (
            <div className="mt-10 flex flex-wrap items-center gap-2 border-t border-border pt-6">
              <TagIcon className="h-3.5 w-3.5 text-muted-foreground" />
              {article.tags.map((tag) => (
                <Link
                  key={tag.id}
                  href={`/?tag=${tag.slug}`}
                  className="rounded-md border border-border bg-muted/40 px-2 py-0.5 text-xs text-muted-foreground transition-colors hover:border-accent/40 hover:bg-accent/10 hover:text-accent"
                >
                  {tag.name}
                </Link>
              ))}
            </div>
          )}
        </Reveal>

        {/* 互动栏：点赞/收藏/分享，窄屏可换行，返回入口贴右侧 */}
        <Reveal
          delay={0.25}
          duration={0.45}
          className="mt-6 flex flex-wrap items-center gap-3 rounded-2xl border border-border bg-card p-4 shadow-sm"
        >
          <button
            type="button"
            onClick={() => {
              if (requireLogin()) return
              toggle.mutate('like')
            }}
            aria-pressed={Boolean(reactions?.liked)}
            className={`flex items-center gap-2 rounded-full border px-4 py-2 text-sm font-medium transition-all ${
              reactions?.liked
                ? 'border-red-300 bg-red-500/10 text-red-500'
                : 'border-border text-muted-foreground hover:border-red-300 hover:bg-red-500/5 hover:text-red-500'
            }`}
          >
            <Heart className={`h-4 w-4 transition-transform ${reactions?.liked ? 'fill-current scale-110' : ''}`} />
            {reactions?.likes ?? 0}
            <span className="sr-only">点赞</span>
          </button>
          <button
            type="button"
            onClick={() => {
              if (requireLogin()) return
              toggle.mutate('favorite')
            }}
            aria-pressed={Boolean(reactions?.favorited)}
            className={`flex items-center gap-2 rounded-full border px-4 py-2 text-sm font-medium transition-all ${
              reactions?.favorited
                ? 'border-amber-300 bg-amber-500/10 text-amber-500'
                : 'border-border text-muted-foreground hover:border-amber-300 hover:bg-amber-500/5 hover:text-amber-500'
            }`}
          >
            <Star className={`h-4 w-4 transition-transform ${reactions?.favorited ? 'fill-current scale-110' : ''}`} />
            {reactions?.favorites ?? 0}
            <span className="sr-only">收藏</span>
          </button>
          <ArticleShare title={article.title} />
          <Link
            href="/"
            className="ml-auto flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-accent"
          >
            <ArrowLeft className="h-4 w-4" /> 返回首页
          </Link>
        </Reveal>

        {/* Comments */}
        <section className="mt-6 rounded-2xl border border-border bg-card p-6 shadow-sm sm:p-8">
          <h2 className="flex items-center gap-2 text-lg font-bold tracking-tight">
            <MessageSquare className="h-5 w-5" />
            评论
            <span className="text-sm font-normal text-muted-foreground">({comments.length})</span>
          </h2>

          {user ? (
            <form
              onSubmit={(e) => {
                e.preventDefault()
                if (!commentText.trim()) {
                  notify.error('评论内容不能为空')
                  return
                }
                addComment.mutate()
              }}
              className="mt-4"
            >
              <textarea
                value={commentText}
                onChange={(e) => setCommentText(e.target.value)}
                placeholder="写下你的评论..."
                rows={3}
                maxLength={1000}
                className="w-full resize-y rounded-lg border border-border bg-background px-4 py-3 text-sm outline-none transition-all placeholder:text-muted-foreground/60 focus:border-accent focus:ring-2 focus:ring-accent/20"
              />
              <div className="mt-2 flex items-center justify-between">
                <span className="text-xs text-muted-foreground">{commentText.length}/1000</span>
                <button
                  type="submit"
                  disabled={addComment.isPending}
                  {...hoverTapScale}
                  className="rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
                >
                  {addComment.isPending ? '发布中...' : '发表评论'}
                </button>
              </div>
            </form>
          ) : (
            <div className="mt-4 rounded-lg border border-dashed p-5 text-center text-sm text-muted-foreground">
              <button
                onClick={() => router.push('/login')}
                className="font-medium text-accent underline underline-offset-4"
              >
                登录
              </button>
              后参与评论
            </div>
          )}

          <div className="mt-6 space-y-3">
            {commentsQuery.isLoading ? (
              /* 评论列表加载态：行加载动画替换原骨架图 */
              <RowLoading rows={2} />
            ) : comments.length === 0 ? (
              <p className="py-8 text-center text-sm text-muted-foreground">
                还没有评论，来抢沙发吧
              </p>
            ) : (
              comments.map((comment, i) => (
                <Reveal
                  key={comment.id}
                  delay={i * 0.05}
                  duration={0.3}
                  className="group flex gap-3 rounded-xl border border-border bg-muted/40 p-4 transition-colors hover:border-accent/25 hover:bg-muted/60"
                >
                  <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-accent/10 text-sm font-bold text-accent">
                    {comment.author.username.charAt(0).toUpperCase()}
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium">{comment.author.username}</span>
                      <span className="text-xs text-muted-foreground">
                        {new Date(comment.created_at).toLocaleString('zh-CN')}
                      </span>
                      {(user?.id === comment.author.id || user?.role === 'admin') && (
                        <button
                          onClick={() => {
                            notify
                              .confirm({
                                title: '删除这条评论？',
                                message: '删除后无法恢复。',
                                confirmText: '删除',
                                danger: true,
                              })
                              .then((ok) => {
                                if (ok) removeComment.mutate(comment.id)
                              })
                          }}
                          className="ml-auto text-muted-foreground opacity-0 transition-all hover:text-red-500 focus-visible:opacity-100 group-hover:opacity-100"
                          title="删除"
                          aria-label="删除评论"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                      )}
                    </div>
                    <p className="mt-1.5 whitespace-pre-wrap break-words text-sm leading-relaxed">
                      {comment.content}
                    </p>
                  </div>
                </Reveal>
              ))
            )}
          </div>
        </section>
      </div>
      {showSidebar && site.sidebarPosition !== 'left' && (
        <SiteSidebar widgets={widgets} position="right" />
      )}
      {showToc && tocPosition === 'right' && (
        <ArticleToc items={tocItems} contentRef={contentRef} />
      )}
      {captcha.dialog}
      <BackToTop />
      {captcha.prewarmNode}
      </div>
    </PageTransition>
  )
}
