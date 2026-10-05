'use client'

import { FormEvent, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import Link from 'next/link'
import Image from 'next/image'
import { useRouter } from 'next/navigation'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowLeft,
  CalendarDays,
  Eye,
  Heart,
  Lock,
  MessageSquare,
  Reply as ReplyIcon,
  Star,
  Tag as TagIcon,
  SearchX,
  Trash2,
  X,
} from 'lucide-react'
import {
  ApiError,
  deleteComment,
  fetchArticleBySlug,
  fetchArticleBySlugWithPassword,
  fetchComments,
  fetchReactions,
  postComment,
  toggleReaction,
  unlockArticle,
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
import { SITE_URL, jsonLdString, plainText } from '@/lib/seo'
import type { CommentItem } from '@/lib/types'

/** 游客身份在 localStorage 里的键名（只存昵称/邮箱/网址，不含凭据） */
const GUEST_IDENTITY_KEY = 'inkstone.guestIdentity'

interface GuestIdentity {
  name: string
  email: string
  url: string
}

/**
 * 读取已记住的游客身份。
 *
 * 作为 useSyncExternalStore 的 getSnapshot 使用，因此必须返回**引用稳定**的
 * 值：每次调用都新建对象会让 React 认为快照一直在变，进而无限重渲染。
 * 这里按原始字符串缓存，内容不变就返回同一个对象。
 */
let cachedIdentityRaw: string | null = null
let cachedIdentity: GuestIdentity | null = null

function readGuestIdentity(): GuestIdentity | null {
  if (typeof window === 'undefined') return null
  let raw: string | null = null
  try {
    raw = localStorage.getItem(GUEST_IDENTITY_KEY)
  } catch {
    // 隐私模式下读取 localStorage 会抛异常：当作没有记住过身份
    return null
  }
  if (raw === cachedIdentityRaw) return cachedIdentity
  cachedIdentityRaw = raw
  if (!raw) {
    cachedIdentity = null
    return null
  }
  try {
    const saved = JSON.parse(raw) as Partial<GuestIdentity>
    cachedIdentity = {
      name: typeof saved.name === 'string' ? saved.name : '',
      email: typeof saved.email === 'string' ? saved.email : '',
      url: typeof saved.url === 'string' ? saved.url : '',
    }
  } catch {
    // 本地存储损坏（用户手改/旧版本格式）时当作没有记忆值
    cachedIdentity = null
  }
  return cachedIdentity
}

// subscribe 是空实现：游客身份只会被本组件写入，React 无需订阅外部变更
function subscribeNoop() {
  return () => {}
}

/** 表单里预填的游客身份，供提交时复用 */
function rememberGuestIdentity(name: string, email: string, url: string) {
  try {
    localStorage.setItem(GUEST_IDENTITY_KEY, JSON.stringify({ name, email, url }))
    // 同步刷新缓存，避免下次读快照时拿到陈旧对象
    cachedIdentityRaw = null
    cachedIdentity = null
  } catch {
    // 隐私模式/配额满时写不进去：只影响下次是否预填，不该让评论失败
  }
}

/**
 * 评论树构建：把后端返回的平铺评论列表整理成「顶级评论 + 其下回复」两级结构。
 *
 * 三个必须先想清楚的点：
 *  1. parent_id 为空 / 0 / undefined 都算顶级——后端历史数据里几种形态都有；
 *  2. 父评论可能已被删除（后端删父评论时会把子评论的 parent_id 置空提升为
 *     顶级），上溯不到父节点时把这条当顶级渲染，否则整栋楼会跟着消失；
 *  3. 楼中楼（回复的回复）统一挂到它的顶级祖先下，保证页面只有两级缩进。
 *
 * 上溯带 seen 集合：parent_id 万一因脏数据成环会死循环。
 */
function buildCommentTree(
  list: CommentItem[],
): { top: CommentItem; replies: CommentItem[] }[] {
  const byId = new Map<number, CommentItem>()
  for (const c of list) byId.set(c.id, c)

  // 每条评论所属的顶级祖先 id
  const topOf = new Map<number, number>()
  for (const c of list) {
    const seen = new Set<number>()
    let cur = c
    let top = c
    for (;;) {
      if (seen.has(cur.id)) break // 脏数据成环：就地停下，cur 即顶级
      seen.add(cur.id)
      top = cur
      const pid = cur.parent_id
      if (!pid || !byId.has(pid)) break
      cur = byId.get(pid)!
    }
    topOf.set(c.id, top.id)
  }

  const nodes = new Map<number, { top: CommentItem; replies: CommentItem[] }>()
  for (const c of list) {
    const topId = topOf.get(c.id)!
    let node = nodes.get(topId)
    if (!node) {
      node = { top: byId.get(topId)!, replies: [] }
      nodes.set(topId, node)
    }
    if (topId !== c.id) node.replies.push(c)
  }

  const out = Array.from(nodes.values())
  // 顶级与楼内回复都按 id 升序：老的在上，符合「盖楼」的阅读习惯
  for (const n of out) n.replies.sort((a, b) => a.id - b.id)
  out.sort((a, b) => a.top.id - b.top.id)
  return out
}

interface CommentBubbleProps {
  comment: CommentItem
  isReply: boolean
  canDelete: boolean
  onDelete: (id: number) => void
  // 内联回复框的开关与内容由父组件持有：回复提交成功后要能统一清空并收起，
  // 否则过人机验证那一轮异步回来时，输入框还留着上次的文字。
  replyOpen: boolean
  replyText: string
  onOpenReply: (id: number) => void
  onCloseReply: () => void
  onReplyTextChange: (value: string) => void
  onSubmitReply: (parentId: number, text: string) => void
  replyPending: boolean
  // 未登录访客还没填昵称：回复框给出提示而不是让提交必然失败
  guestNeedsName: boolean
}

function CommentBubble({
  comment,
  isReply,
  canDelete,
  onDelete,
  replyOpen,
  replyText,
  onOpenReply,
  onCloseReply,
  onReplyTextChange,
  onSubmitReply,
  replyPending,
  guestNeedsName,
}: CommentBubbleProps) {
  const notify = useNotify()
  const submit = () => {
    if (!replyText.trim()) {
      notify.error('回复内容不能为空')
      return
    }
    onSubmitReply(comment.id, replyText.trim())
  }

  return (
    <div
      className={
        isReply
          ? 'rounded-xl border border-border bg-muted/30 p-3.5 transition-colors hover:border-accent/25 hover:bg-muted/50'
          : 'group flex gap-3 rounded-xl border border-border bg-muted/40 p-4 transition-colors hover:border-accent/25 hover:bg-muted/60'
      }
    >
      <span
        className={`flex shrink-0 items-center justify-center rounded-full bg-accent/10 font-bold text-accent ${
          isReply ? 'h-7 w-7 text-xs' : 'h-9 w-9 text-sm'
        }`}
      >
        {comment.author.username.charAt(0).toUpperCase()}
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          {comment.author.url ? (
            // 游客填了个人网站：昵称变成外链。rel 用 nofollow ugc —— 评论里的
            // 链接是用户内容，不该传递权重（防垃圾外链），也不能让目标页
            // 通过 window.opener 操作本页。
            <a
              href={comment.author.url}
              target="_blank"
              rel="nofollow ugc noopener noreferrer"
              className="text-sm font-medium text-accent hover:underline"
            >
              {comment.author.username}
            </a>
          ) : (
            <span className="text-sm font-medium">{comment.author.username}</span>
          )}
          {comment.author.is_guest && (
            <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
              访客
            </span>
          )}
          <span className="text-xs text-muted-foreground">
            {new Date(comment.created_at).toLocaleString('zh-CN')}
          </span>
          <div className="ml-auto flex items-center gap-1">
            <button
              type="button"
              onClick={() => (replyOpen ? onCloseReply() : onOpenReply(comment.id))}
              className="flex items-center gap-1 rounded-md px-1.5 py-1 text-xs text-muted-foreground transition-colors hover:text-accent"
              title={replyOpen ? '取消回复' : '回复这条评论'}
            >
              {replyOpen ? <X className="h-3.5 w-3.5" /> : <ReplyIcon className="h-3.5 w-3.5" />}
              {replyOpen ? '取消' : '回复'}
            </button>
            {canDelete && (
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
                      if (ok) onDelete(comment.id)
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
        </div>
        <p className="mt-1.5 whitespace-pre-wrap break-words text-sm leading-relaxed">
          {comment.content}
        </p>

        {/* 内联回复框：只对当前展开的那一条渲染，避免 N 个输入框同时存在 */}
        {replyOpen && (
          <form
            onSubmit={(e) => {
              e.preventDefault()
              submit()
            }}
            className="mt-3"
          >
            <textarea
              value={replyText}
              onChange={(e) => onReplyTextChange(e.target.value)}
              placeholder={`回复 ${comment.author.username}...`}
              rows={2}
              maxLength={1000}
              autoFocus
              className="w-full resize-y rounded-lg border border-border bg-background px-3.5 py-2.5 text-sm outline-none transition-all placeholder:text-muted-foreground/60 focus:border-accent focus:ring-2 focus:ring-accent/20"
            />
            <div className="mt-2 flex items-center justify-between gap-2">
              <span className="text-xs text-muted-foreground">
                {/* 游客信息不全时说明该去哪填：否则提交只会失败，
                    而失败原因（昵称/邮箱必填）在这个小框里看不出来 */}
                {guestNeedsName ? '请先在上方评论框填写昵称' : '回复将公开显示'}
              </span>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={onCloseReply}
                  className="rounded-lg border border-border px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={replyPending}
                  {...hoverTapScale}
                  className="rounded-lg bg-accent px-4 py-1.5 text-xs font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
                >
                  {replyPending ? '发布中...' : '发布回复'}
                </button>
              </div>
            </div>
          </form>
        )}
      </div>
    </div>
  )
}

/**
 * 加密文章的密码门：详情接口返回 401 + need_password 时渲染，而不是错误页。
 *
 * 解锁只负责校验（后端 unlock 端点不记会话），成功后把密码交还父组件，
 * 由查询层带 password 重新拉正文——否则重新拉到的还是 401，门会原地打转。
 */
function ArticlePasswordGate({
  slug,
  onUnlocked,
}: {
  slug: string
  onUnlocked: (password: string) => void
}) {
  const notify = useNotify()
  const [password, setPassword] = useState('')

  const unlock = useMutation({
    mutationFn: () => unlockArticle(slug, password),
    onSuccess: () => {
      onUnlocked(password)
      notify.success('密码正确，正在加载文章')
    },
    onError: (e) =>
      notify.error(e instanceof ApiError ? e.message : '解锁失败，请稍后重试'),
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!password) {
      notify.error('请输入访问密码')
      return
    }
    unlock.mutate()
  }

  return (
    <Reveal y={0} className="mx-auto max-w-3xl px-4 py-24 text-center">
      <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-accent/10">
        <Lock className="h-8 w-8 text-accent" />
      </div>
      <h1 className="mt-6 text-2xl font-bold">这篇文章需要访问密码</h1>
      <p className="mt-2 text-sm text-muted-foreground">
        输入作者设置的访问密码后即可阅读全文
      </p>
      <form onSubmit={submit} className="mx-auto mt-8 flex max-w-sm items-center gap-2">
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="请输入访问密码"
          autoFocus
          className="min-w-0 flex-1 rounded-lg border border-border bg-background px-4 py-2.5 text-sm outline-none transition-all placeholder:text-muted-foreground/60 focus:border-accent focus:ring-2 focus:ring-accent/20"
        />
        <button
          type="submit"
          disabled={unlock.isPending}
          {...hoverTapScale}
          className="shrink-0 rounded-lg bg-accent px-5 py-2.5 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
        >
          {unlock.isPending ? '解锁中...' : '解锁'}
        </button>
      </form>
    </Reveal>
  )
}

export function PostDetail({ slug }: { slug: string }) {
  const { user } = useAuth()
  const notify = useNotify()
  const router = useRouter()
  const queryClient = useQueryClient()
  const [commentText, setCommentText] = useState('')
  const site = useSiteConfig()
  const captcha = useCaptcha('comment')

  // 游客身份：未登录访客发表评论时填写。填过一次就记住（localStorage），
  // 让常来的访客不必每条评论都重打一遍昵称。
  //
  // 用 useSyncExternalStore 而不是 useEffect + setState 读取：后者会在
  // effect 体内同步 setState，触发 react-hooks/set-state-in-effect
  // （项目 ESLint 硬性拦截）。这个 hook 的 getServerSnapshot 返回 null，
  // 服务端渲染时必然是空值，客户端首次渲染后由 React 补齐，
  // 因此不会产生 hydration mismatch。
  const savedIdentity = useSyncExternalStore(
    subscribeNoop,
    readGuestIdentity,
    () => null,
  )
  const [guestName, setGuestName] = useState('')
  const [guestEmail, setGuestEmail] = useState('')
  const [guestUrl, setGuestUrl] = useState('')
  // 本地的"用户是否改过输入框"标记：改过之后就不再被记忆值覆盖，
  // 否则用户清空昵称时会被 localStorage 里的旧值顶回来。
  const [guestDirty, setGuestDirty] = useState(false)
  const effectiveGuest = guestDirty
    ? { name: guestName, email: guestEmail, url: guestUrl }
    : {
        name: guestName || savedIdentity?.name || '',
        email: guestEmail || savedIdentity?.email || '',
        url: guestUrl || savedIdentity?.url || '',
      }

  // 已解锁的访问密码（未解锁为 null）。放进 queryKey 是为了解锁后
  // 重新拉取时**一定**带上它：后端刻意不记「已解锁」会话
  // （见后端 articlePasswordOK 注释），不带 password 再拉只会又拿到
  // 401，密码门原地打转。明文只留在浏览器内存，不落 localStorage。
  const [unlockPassword, setUnlockPassword] = useState<string | null>(null)

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['article', 'slug', slug, unlockPassword ?? ''],
    queryFn: () =>
      unlockPassword
        ? fetchArticleBySlugWithPassword(slug, unlockPassword)
        : fetchArticleBySlug(slug),
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

  // 游客表单的必填判断，评论与回复共用同一套规则。
  // 邮箱是否必填由后台 guest_comment_email 决定，前端据此调整提示与校验，
  // 与后端 normalizeIdentity 的规则保持一致。
  const canGuestSubmit =
    effectiveGuest.name.trim().length > 0 &&
    (!site.guestCommentEmail || effectiveGuest.email.trim().length > 0)

  // 游客提交后是否要等审核：游客评论本身未开免审。
  // 这只是提示文案的预判，真实状态以响应里的 pending 标记为准。
  const guestCommentPending = site.guestCommentFree === false

  const addComment = useMutation({
    // 开启人机验证时先弹窗验证，通过后携带凭证提交
    mutationFn: async () => {
      const credential = captcha.enabled ? await captcha.run() : undefined
      // 游客才带身份字段：登录用户提交时后端会丢弃这些字段，
      // 这里也一并不传，避免服务端做无谓的解析。
      const guest = user
        ? {}
        : {
            guest_name: effectiveGuest.name.trim(),
            guest_email: effectiveGuest.email.trim() || undefined,
            guest_url: effectiveGuest.url.trim() || undefined,
          }
      return postComment(articleId!, commentText, { ...credential, ...guest })
    },
    onSuccess: () => {
      setCommentText('')
      // 记住游客身份，下次访问自动预填
      if (!user) {
        rememberGuestIdentity(
          effectiveGuest.name.trim(),
          effectiveGuest.email.trim(),
          effectiveGuest.url.trim(),
        )
      }
      queryClient.invalidateQueries({ queryKey: ['comments', articleId] })
      // 审核状态决定提示文案：说"已发布"而评论其实在待审队列里，
      // 访客会刷新页面找不到自己的评论，以为发丢了。
      if (guestCommentPending) {
        notify.success('评论已提交，等待管理员审核后公开')
      } else {
        notify.success('评论已发布')
      }
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

  // 内联回复框：replyTo 为被回复评论的 id，null 表示没有展开的回复框。
  // 与 replyText 一起放在组件级，提交成功后统一清空并收起。
  const [replyTo, setReplyTo] = useState<number | null>(null)
  const [replyText, setReplyText] = useState('')

  const addReply = useMutation({
    mutationFn: async (payload: { parentId: number; text: string }) => {
      // 与发表评论一致：开启人机验证时先弹窗验证，凭证随请求提交
      const credential = captcha.enabled ? await captcha.run() : undefined
      const guest = user
        ? {}
        : {
            guest_name: effectiveGuest.name.trim(),
            guest_email: effectiveGuest.email.trim() || undefined,
            guest_url: effectiveGuest.url.trim() || undefined,
          }
      return postComment(articleId!, payload.text, {
        ...credential,
        ...guest,
        parent_id: payload.parentId,
      })
    },
    onSuccess: () => {
      setReplyTo(null)
      setReplyText('')
      if (!user) {
        rememberGuestIdentity(
          effectiveGuest.name.trim(),
          effectiveGuest.email.trim(),
          effectiveGuest.url.trim(),
        )
      }
      queryClient.invalidateQueries({ queryKey: ['comments', articleId] })
      notify.success(guestCommentPending ? '回复已提交，等待管理员审核后公开' : '回复已发布')
    },
    onError: (e) => {
      // 用户主动关闭验证框时不打扰（与发表评论保持一致的口径）
      if (e instanceof Error && e.message === '人机验证已取消') return
      notify.error(e instanceof Error ? e.message : '回复失败')
    },
  })

  const handleSubmitReply = (parentId: number, text: string) => {
    // 未登录且未开启游客评论时提示登录；开启后回复同样对游客开放
    if (!user && !site.guestComment) {
      notify.error('请先登录')
      router.push('/login')
      return
    }
    if (!user && !canGuestSubmit) {
      notify.error('请填写昵称')
      return
    }
    addReply.mutate({ parentId, text })
  }

  // 文章目录：挂载后从正文 HTML 提取（SSR 无 document 返回空，避免 hydration 差异）
  const contentRef = useRef<HTMLDivElement>(null)
  const isMounted = useIsMounted()
  const articleHtml = data?.article.content ?? ''
  const tocItems = useMemo(() => (isMounted ? parseToc(articleHtml) : []), [isMounted, articleHtml])

  // Article 结构化数据：让搜索结果能展示标题、作者、发布时间与封面缩略图。
  //
  // 两个刻意的安排：
  //  1. 这个 memo 必须放在下面 isLoading / isError 的提前 return **之前**——
  //     hooks 不能条件调用，放在 return 之后会直接违反 React 规则；
  //  2. data 为空时返回 null：那种情况下构造出来的是空壳 schema，
  //     输出它比不输出更糟（搜索引擎会拿到一篇没有标题的文章）。
  const jsonLd = useMemo(() => {
    const a = data?.article
    if (!a) return null
    const pageUrl = `${SITE_URL}/posts/${encodeURIComponent(a.slug)}`
    return {
      '@context': 'https://schema.org',
      '@type': 'Article',
      headline: a.title,
      // 与 <head> 里的 meta description 保持同一来源：作者写了摘要就用摘要，
      // 否则剥正文。两处各算一套会让搜索引擎与社交卡片拿到不同描述。
      description: plainText(a.excerpt?.trim() || a.content, 110),
      // datePublished 用首次发布时间；草稿没有 published_at 时退回创建时间
      datePublished: a.published_at ?? a.created_at,
      dateModified: a.updated_at,
      author: { '@type': 'Person', name: a.author.username },
      publisher: { '@type': 'Organization', name: site.siteName },
      mainEntityOfPage: { '@type': 'WebPage', '@id': pageUrl },
      ...(a.cover ? { image: [a.cover] } : {}),
    }
  }, [data?.article, site.siteName])

  // 平铺评论列表 → 两级树。data.comments 引用在查询未刷新时是稳定的，
  // 因此这个 memo 不会每次渲染都重建。
  //
  // 位置与 jsonLd 同理：必须在下面 isLoading / isError 的提前 return 之前，
  // 否则就是条件调用 hook。commentsQuery 是否加载完都不影响——
  // 查询未返回时 comments 为空数组，构树结果为空，渲染逻辑自己会处理。
  //
  // comments 本身也用 useMemo 收着：直接写 `?? []` 的话每次渲染都是新数组，
  // exhaustive-deps 会警告下面的依赖不稳定。
  const comments = useMemo(() => commentsQuery.data?.comments ?? [], [commentsQuery.data])
  const commentTree = useMemo(() => buildCommentTree(comments), [comments])

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

  // 加密文章未解锁：后端返回 401 + need_password。与「文章不存在」
  // （404）分开处理——这里渲染密码门，不展示错误页。
  const needPassword =
    isError &&
    error instanceof ApiError &&
    error.status === 401 &&
    Boolean((error.data as { need_password?: boolean } | undefined)?.need_password)

  if (needPassword) {
    return (
      <ArticlePasswordGate
        slug={slug}
        onUnlocked={(pw) => {
          setUnlockPassword(pw)
          // 双保险：新的 queryKey 本身就会带密码重新拉取；这里再
          // invalidate 一次，把无密码那条 401 缓存也清掉。
          queryClient.invalidateQueries({ queryKey: ['article', 'slug', slug] })
        }}
      />
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
  // comments / commentTree 已在上方与其他 hook 一起声明（hooks 不能
  // 放在提前 return 之后），此处不再重复定义。

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
      {/* 结构化数据。只有成功取到文章才会走到这里（isLoading / isError 分支
          已提前 return），因此 jsonLd 非空即渲染。
          注入位置在外层容器内：Google 要求 script 出现在 body 内任意位置均可。 */}
      {jsonLd && (
        <script
          type="application/ld+json"
          // jsonLdString 已把 < > & 转成 \u 形式，正文里即使含
          // "</script>" 也不会提前闭合这个标签
          dangerouslySetInnerHTML={{ __html: jsonLdString(jsonLd) }}
        />
      )}
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

          {/* 正文。加密文章未解锁时 content 是空串（列表接口同样会抹掉），
              这时渲染「该文章已加密」占位卡片，避免正文位置突兀地空白一片 */}
          {article.content ? (
            <article
              ref={contentRef}
              className={proseBody}
              dangerouslySetInnerHTML={{ __html: article.content }}
            />
          ) : (
            article.has_password && (
              <div className="flex flex-col items-center rounded-xl border border-dashed border-border py-16 text-center text-muted-foreground">
                <Lock className="h-8 w-8" />
                <p className="mt-3 text-sm font-medium">该文章已加密</p>
                <p className="mt-1 text-xs">输入访问密码后即可阅读全文</p>
              </div>
            )
          )}

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

          {user || site.guestComment ? (
            <form
              onSubmit={(e) => {
                e.preventDefault()
                if (!commentText.trim()) {
                  notify.error('评论内容不能为空')
                  return
                }
                // 游客必须留昵称（后台要求时还需邮箱）：后端也会校验，
                // 这里先挡一道给出更快的反馈
                if (!user && !canGuestSubmit) {
                  notify.error(
                    effectiveGuest.name.trim() ? '请填写邮箱' : '请先填写昵称',
                  )
                  return
                }
                addComment.mutate()
              }}
              className="mt-4"
            >
              {/* 游客身份字段：只有未登录访客需要填，登录用户直接用账号身份 */}
              {!user && (
                <div className="mb-3 grid gap-2 sm:grid-cols-3">
                  <input
                    type="text"
                    value={effectiveGuest.name}
                    onChange={(e) => {
                      setGuestDirty(true)
                      setGuestName(e.target.value)
                    }}
                    placeholder="昵称（必填）"
                    maxLength={32}
                    autoComplete="nickname"
                    className="w-full rounded-lg border border-border bg-background px-3.5 py-2 text-sm outline-none transition-all placeholder:text-muted-foreground/60 focus:border-accent focus:ring-2 focus:ring-accent/20"
                  />
                  <input
                    type="email"
                    value={effectiveGuest.email}
                    onChange={(e) => {
                      setGuestDirty(true)
                      setGuestEmail(e.target.value)
                    }}
                    placeholder={
                      site.guestCommentEmail ? '邮箱（必填，不公开）' : '邮箱（选填，不公开）'
                    }
                    maxLength={255}
                    autoComplete="email"
                    className="w-full rounded-lg border border-border bg-background px-3.5 py-2 text-sm outline-none transition-all placeholder:text-muted-foreground/60 focus:border-accent focus:ring-2 focus:ring-accent/20"
                  />
                  <input
                    type="url"
                    value={effectiveGuest.url}
                    onChange={(e) => {
                      setGuestDirty(true)
                      setGuestUrl(e.target.value)
                    }}
                    placeholder="网站（选填）"
                    maxLength={255}
                    autoComplete="url"
                    className="w-full rounded-lg border border-border bg-background px-3.5 py-2 text-sm outline-none transition-all placeholder:text-muted-foreground/60 focus:border-accent focus:ring-2 focus:ring-accent/20"
                  />
                </div>
              )}
              <textarea
                value={commentText}
                onChange={(e) => setCommentText(e.target.value)}
                placeholder="写下你的评论..."
                rows={3}
                maxLength={1000}
                className="w-full resize-y rounded-lg border border-border bg-background px-4 py-3 text-sm outline-none transition-all placeholder:text-muted-foreground/60 focus:border-accent focus:ring-2 focus:ring-accent/20"
              />
              <div className="mt-2 flex flex-wrap items-center justify-between gap-2">
                <span className="text-xs text-muted-foreground">
                  {commentText.length}/1000
                  {/* 提前说明要审核：否则访客提交后刷新找不到评论，会以为发丢了 */}
                  {!user && guestCommentPending && commentText.trim() !== '' && (
                    <span className="ml-2">· 游客评论需管理员审核后公开</span>
                  )}
                </span>
                <div className="flex items-center gap-3">
                  {!user && (
                    <button
                      type="button"
                      onClick={() => router.push('/login')}
                      className="text-xs text-muted-foreground underline underline-offset-4 transition-colors hover:text-accent"
                    >
                      登录后评论
                    </button>
                  )}
                  <button
                    type="submit"
                    disabled={addComment.isPending}
                    {...hoverTapScale}
                    className="rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
                  >
                    {addComment.isPending ? '发布中...' : '发表评论'}
                  </button>
                </div>
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
              commentTree.map((node, i) => (
                <Reveal
                  key={node.top.id}
                  delay={i * 0.05}
                  duration={0.3}
                  className="space-y-2.5"
                >
                  <CommentBubble
                    comment={node.top}
                    isReply={false}
                    canDelete={Boolean(
                      user &&
                        (user.role === 'admin' ||
                          // 游客评论只能用管理员身份删：author.id 是 0，
                          // 与任何真实用户 id 都不等，这里显式排除以免将来
                          // 有人改用别的判据时误开删除入口。
                          (!node.top.author.is_guest && user.id === node.top.author.id)),
                    )}
                    onDelete={(id) => removeComment.mutate(id)}
                    replyOpen={replyTo === node.top.id}
                    replyText={replyText}
                    onOpenReply={setReplyTo}
                    onCloseReply={() => {
                      setReplyTo(null)
                      setReplyText('')
                    }}
                    onReplyTextChange={setReplyText}
                    onSubmitReply={handleSubmitReply}
                    replyPending={addReply.isPending}
                    guestNeedsName={!user && !canGuestSubmit}
                  />
                  {/* 楼内回复：左竖线 + 缩进，视觉上挂归属到顶级评论 */}
                  {node.replies.length > 0 && (
                    <div className="ml-4 space-y-2.5 border-l-2 border-border pl-4 sm:ml-6">
                      {node.replies.map((reply) => (
                        <CommentBubble
                          key={reply.id}
                          comment={reply}
                          isReply
                          canDelete={Boolean(
                            user &&
                              (user.role === 'admin' ||
                                (!reply.author.is_guest && user.id === reply.author.id)),
                          )}
                          onDelete={(id) => removeComment.mutate(id)}
                          replyOpen={replyTo === reply.id}
                          replyText={replyText}
                          onOpenReply={setReplyTo}
                          onCloseReply={() => {
                            setReplyTo(null)
                            setReplyText('')
                          }}
                          onReplyTextChange={setReplyText}
                          onSubmitReply={handleSubmitReply}
                          replyPending={addReply.isPending}
                          guestNeedsName={!user && !canGuestSubmit}
                        />
                      ))}
                    </div>
                  )}
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
