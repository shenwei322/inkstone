'use client'

import { FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import gsap from 'gsap'
import { ArrowLeft, Eye, History, Save, X } from 'lucide-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  createArticle,
  deleteAdminArticle,
  fetchArticle,
  fetchCategories,
  fetchTags,
  uploadImage,
  updateArticle,
  ApiError,
} from '@/lib/api'
import type { Article } from '@/lib/types'
import { PageTransition, Reveal, hoverTapScale, prefersReducedMotion, useReveal } from '@/components/motion'
import { PageLoading } from '@/components/page-loader'
import { MarkdownEditor } from '@/components/markdown-editor'
import { markdownToHtml, htmlToMarkdown } from '@/lib/markdown'
import { useNotify } from '@/components/toast'
import { useAuth } from '@/lib/auth-context'
import { ArticlePreview } from '@/components/article-preview'

function htmlToText(html: string): string {
  return html.replace(/<[^>]*>/g, ' ').replace(/&nbsp;/g, ' ')
}

/** 新建文章的本地草稿（防刷新/误关丢失；成功创建或发布后清除） */
const NEW_DRAFT_KEY = 'blog_article_new_draft'

interface LocalDraft {
  title: string
  markdown: string
  categoryId: number | null
  tags: string[]
  cover: string
}

/** 读取本浏览器缓存的未发布草稿；SSR 或存储不可用时返回 null */
function readLocalDraft(): LocalDraft | null {
  try {
    const raw = localStorage.getItem(NEW_DRAFT_KEY)
    if (!raw) return null
    const d = JSON.parse(raw) as Partial<LocalDraft> | null
    if (!d || (!d.title?.trim() && !d.markdown?.trim())) return null
    return {
      title: typeof d.title === 'string' ? d.title : '',
      markdown: typeof d.markdown === 'string' ? d.markdown : '',
      categoryId: typeof d.categoryId === 'number' ? d.categoryId : null,
      tags: Array.isArray(d.tags) ? d.tags.filter((t): t is string => typeof t === 'string') : [],
      cover: typeof d.cover === 'string' ? d.cover : '',
    }
  } catch {
    return null
  }
}

function writeLocalDraft(draft: LocalDraft) {
  try {
    localStorage.setItem(NEW_DRAFT_KEY, JSON.stringify(draft))
  } catch {
    // storage unavailable: keep editing in memory
  }
}

function clearLocalDraft() {
  try {
    localStorage.removeItem(NEW_DRAFT_KEY)
  } catch {
    // ignore persistence failure
  }
}

interface EditorShellProps {
  mode: 'new' | 'edit'
  article?: Article
}

function EditorShell({ mode, article }: EditorShellProps) {
  const router = useRouter()
  const queryClient = useQueryClient()
  // new 模式优先从本地草稿恢复（刷新/误关不丢内容）；恢复的 draft 供 banner 与基线使用
  const [restoredDraft] = useState<LocalDraft | null>(() =>
    mode === 'new' ? readLocalDraft() : null,
  )
  const [title, setTitle] = useState(restoredDraft?.title ?? '')
  // 编辑器内部用 Markdown；保存时转成 HTML 交给后端（与既有存储/渲染保持一致）
  const [markdown, setMarkdown] = useState(() =>
    restoredDraft ? restoredDraft.markdown : article?.content ? htmlToMarkdown(article.content) : '',
  )
  const content = useMemo(() => markdownToHtml(markdown), [markdown])
  const [viewSlug, setViewSlug] = useState<string | null>(article?.slug ?? null)
  const [categoryId, setCategoryId] = useState<number | null>(
    restoredDraft?.categoryId ?? article?.category?.id ?? null,
  )
  const [tags, setTags] = useState<string[]>(
    restoredDraft?.tags ?? (article?.tags ?? []).map((t) => t.name),
  )
  const [tagInput, setTagInput] = useState('')
  const [cover, setCover] = useState(restoredDraft?.cover ?? article?.cover ?? '')
  const [showCover, setShowCover] = useState(Boolean(restoredDraft?.cover ?? article?.cover))
  const uploadCoverRef = useRef<HTMLInputElement>(null)
  const notify = useNotify()
  const { user } = useAuth()
  const [previewOpen, setPreviewOpen] = useState(false)
  // 自动保存开关滑块：跟随开关状态平移（原 framer spring 命令式近似）
  const autoSaveKnobRef = useRef<HTMLSpanElement>(null)
  // 底部提示文案入场（行内语义标签，不包 div）
  const hintRef = useRef<HTMLParagraphElement>(null)
  useReveal(hintRef, { y: 0, delay: 0.4 })

  const categoriesQuery = useQuery({ queryKey: ['categories'], queryFn: fetchCategories })
  const tagsQuery = useQuery({ queryKey: ['tags'], queryFn: fetchTags })

  const addTag = () => {
    const name = tagInput.trim()
    if (!name) return
    if (tags.some((t) => t.toLowerCase() === name.toLowerCase())) {
      setTagInput('')
      return
    }
    setTags((t) => [...t, name])
    setTagInput('')
  }

  const removeTag = (name: string) => setTags((t) => t.filter((x) => x !== name))

  const [autoSavedAt, setAutoSavedAt] = useState<Date | null>(
    mode === 'edit' && article?.status === 'draft' ? new Date(article.updated_at) : null,
  )
  const [autoSaving, setAutoSaving] = useState(false)
  const [autoSaveEnabled, setAutoSaveEnabled] = useState(true)

  // 开关滑块平移：autoSaveEnabled 变化时滑到对应位置（替代 framer spring）
  useEffect(() => {
    const el = autoSaveKnobRef.current
    if (el && !prefersReducedMotion()) {
      gsap.to(el, { x: autoSaveEnabled ? 16 : 0, duration: 0.25, ease: 'power2.out' })
    }
  }, [autoSaveEnabled])
  // 基线快照：与自动保存/本地草稿的 snapshot 同构（含 cover），
  // 用于判断「是否有未保存更改」；new 模式以恢复的本地草稿为基线
  const [baseline, setBaseline] = useState(() =>
    JSON.stringify({
      t: restoredDraft?.title ?? article?.title ?? '',
      c: restoredDraft ? markdownToHtml(restoredDraft.markdown) : article?.content ?? '',
      g: restoredDraft?.categoryId ?? article?.category?.id ?? null,
      s: (restoredDraft?.tags ?? (article?.tags ?? []).map((t) => t.name)).join(','),
      v: restoredDraft?.cover ?? article?.cover ?? '',
    }),
  )
  // 当前表单快照：与 baseline 比对判断「有没有未保存更改」，用于离开提醒；保存成功后刷新基线。
  const currentSnapshot = () =>
    JSON.stringify({ t: title, c: content, g: categoryId, s: tags.join(','), v: cover })
  const isDirty = currentSnapshot() !== baseline
  const [restoredHint, setRestoredHint] = useState(Boolean(restoredDraft))

  // Restore saved preference after mount (default: on).
  useEffect(() => {
    const t = setTimeout(() => {
      try {
        setAutoSaveEnabled(localStorage.getItem('blog_autosave') !== 'off')
      } catch {
        // storage unavailable: keep default
      }
    }, 0)
    return () => clearTimeout(t)
  }, [])

  const toggleAutoSave = () => {
    const next = !autoSaveEnabled
    setAutoSaveEnabled(next)
    try {
      localStorage.setItem('blog_autosave', next ? 'on' : 'off')
    } catch {
      // ignore persistence failure
    }
    setBaseline(currentSnapshot())
    notify.success(next ? '自动保存已开启' : '自动保存已关闭')
  }

  const isPublished = mode === 'edit' && article?.status === 'published'

  const publish = useMutation({
    mutationFn: async () => {
      const body = {
        title,
        content,
        status: 'published',
        category_id: categoryId,
        tags,
        cover,
      }
      if (mode === 'edit' && article) {
        return updateArticle(article.id, body)
      }
      return createArticle(body)
    },
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['articles'] })
      queryClient.invalidateQueries({ queryKey: ['admin'] })
      queryClient.invalidateQueries({ queryKey: ['article', res.article.id] })
      setBaseline(currentSnapshot())
      if (mode === 'edit' && article) {
        setViewSlug(res.article.slug)
        notify.success(res.article.status === 'published' ? '文章已发布' : '已保存', res.article.slug)
      } else {
        clearLocalDraft()
        notify.success('文章已发布', res.article.slug)
        router.replace(`/admin/articles/edit/${res.article.id}`)
      }
    },
    onError: (err) => notify.error(err instanceof ApiError ? err.message : '发布失败，请稍后重试'),
  })

  // 保存草稿：Ctrl+S / 工具条「存草稿」。
  // 新建文章先落库为 draft 并跳转编辑页，之后由自动保存接管（不会直接发布）。
  const saveDraft = useMutation({
    mutationFn: async () => {
      const body = {
        title,
        content,
        status: 'draft',
        category_id: categoryId,
        tags,
        cover,
      }
      if (mode === 'edit' && article) {
        return updateArticle(article.id, body)
      }
      return createArticle(body)
    },
    onSuccess: (res) => {
      queryClient.invalidateQueries({ queryKey: ['articles'] })
      queryClient.invalidateQueries({ queryKey: ['admin'] })
      setBaseline(currentSnapshot())
      setAutoSavedAt(new Date())
      if (mode === 'edit' && article) {
        notify.success('草稿已保存')
      } else {
        clearLocalDraft()
        notify.success('草稿已保存，可继续编辑')
        router.replace(`/admin/articles/edit/${res.article.id}`)
      }
    },
    onError: (err) => notify.error(err instanceof ApiError ? err.message : '保存草稿失败'),
  })

  const trash = useMutation({
    mutationFn: () => deleteAdminArticle(article!.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin'] })
      notify.success('文章已删除')
      router.push('/admin/articles')
    },
    onError: (err) => notify.error(err instanceof ApiError ? err.message : '删除失败'),
  })

  // Auto-save: silently keeps drafts up to date while writing (edit mode only).
  useEffect(() => {
    if (!autoSaveEnabled) return
    if (mode !== 'edit' || article?.status !== 'draft') return
    const snapshot = JSON.stringify({ t: title, c: content, g: categoryId, s: tags.join(','), v: cover })
    if (snapshot === baseline) return
    if (!title.trim() || !htmlToText(content).trim()) return

    const timer = setTimeout(async () => {
      setAutoSaving(true)
      try {
        await updateArticle(article.id, {
          title,
          content,
          status: 'draft',
          category_id: categoryId,
          tags,
          cover,
        })
        setBaseline(snapshot)
        setAutoSavedAt(new Date())
      } catch {
        // silent failure: user can still publish manually
      } finally {
        setAutoSaving(false)
      }
    }, 2000)
    return () => clearTimeout(timer)
  }, [title, content, categoryId, tags, cover, mode, article, autoSaveEnabled, baseline])

  // 新建文章：内容防抖写入本地草稿（刷新/误关/误触返回后可从草稿恢复）
  useEffect(() => {
    if (mode !== 'new') return
    if (!title.trim() && !markdown.trim()) {
      clearLocalDraft()
      return
    }
    const timer = setTimeout(() => {
      writeLocalDraft({ title, markdown, categoryId, tags, cover })
    }, 800)
    return () => clearTimeout(timer)
  }, [mode, title, markdown, categoryId, tags, cover])

  // 有未保存更改时，刷新/关闭页面前弹浏览器确认（Next 客户端路由跳转由链接本身拦截）
  useEffect(() => {
    if (!isDirty) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [isDirty])

  // 全屏预览：锁背景滚动 + ESC 关闭
  useEffect(() => {
    if (!previewOpen) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setPreviewOpen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => {
      document.body.style.overflow = prev
      window.removeEventListener('keydown', onKey)
    }
  }, [previewOpen])

  const wordCount = htmlToText(content).replace(/\s+/g, '').length
  const readMinutes = Math.max(1, Math.round(wordCount / 400))

  // 实时预览用的派生数据
  const previewCategory =
    (categoriesQuery.data?.categories ?? []).find((c) => c.id === categoryId)?.name ?? article?.category?.name ?? null
  const previewAuthor = article?.author?.username ?? user?.username ?? '我'
  const [todayLabel] = useState(() => new Date().toLocaleDateString('zh-CN'))
  const previewDate =
    article?.published_at ?? article?.created_at
      ? new Date((article?.published_at ?? article?.created_at) as string).toLocaleDateString('zh-CN')
      : todayLabel
  const previewViews = article?.views ?? 0

  // 点击「发布/更新」
  const doPublish = () => {
    if (!title.trim()) {
      notify.error('给文章起个标题吧')
      return
    }
    if (!wordCount) {
      notify.error('先写一点正文，再发布')
      return
    }
    publish.mutate()
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    doPublish()
  }

  const saveIndicator = !autoSaveEnabled ? (
    <span className="text-xs text-muted-foreground">自动保存已关闭</span>
  ) : autoSaving ? (
    <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
      <span className="h-3 w-3 animate-spin rounded-full border-2 border-muted-foreground/30 border-t-muted-foreground" />
      保存中...
    </span>
  ) : autoSavedAt ? (
    <span className="text-xs text-emerald-600 dark:text-emerald-400">
      已自动保存 {autoSavedAt.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}
    </span>
  ) : isPublished ? (
    <span className="text-xs text-muted-foreground">已发布</span>
  ) : mode === 'new' ? (
    <span className="text-xs text-muted-foreground">草稿自动保存在本浏览器</span>
  ) : (
    <span className="text-xs text-muted-foreground">输入内容后自动保存</span>
  )

  const autoSaveSwitch = mode === 'edit' && article?.status === 'draft' && (
    <button
      type="button"
      onClick={toggleAutoSave}
      title={autoSaveEnabled ? '关闭自动保存' : '开启自动保存'}
      className="flex items-center gap-2 text-xs text-muted-foreground transition-colors hover:text-foreground"
    >
      <span
        className={`relative h-5 w-9 rounded-full transition-colors ${
          autoSaveEnabled ? 'bg-emerald-500' : 'bg-border'
        }`}
      >
        <span
          ref={autoSaveKnobRef}
          className="absolute top-0.5 left-0.5 h-4 w-4 rounded-full bg-white shadow-sm"
        />
      </span>
      自动保存
    </button>
  )

  const primaryLabel = mode === 'new' ? '发布文章' : isPublished ? '更新' : '发布'

  return (
    <form onSubmit={submit} className="min-h-[60vh]">
      {/* 顶部工具条 */}
      <div className="sticky top-16 z-40 -mx-4 border-b border-border bg-card/95 backdrop-blur">
        <div className="flex h-14 items-center justify-between px-4">
          <div className="flex min-w-0 items-center gap-3">
            <Link
              href="/admin/articles"
              onClick={async (e) => {
                if (!isDirty) return
                e.preventDefault()
                const ok = await notify.confirm({
                  title: '离开编辑页？',
                  message: '当前有未保存的更改，离开后可能丢失最近几秒的修改。',
                  confirmText: '离开',
                  danger: true,
                })
                if (ok) router.push('/admin/articles')
              }}
              className="flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              <ArrowLeft className="h-4 w-4 transition-transform hover:-translate-x-0.5" />
              <span className="hidden sm:inline">文章列表</span>
            </Link>
            <span className="hidden h-4 w-px bg-border sm:block" />
            <div className="hidden items-center gap-3 sm:flex">
              {autoSaveSwitch}
              {saveIndicator}
            </div>
          </div>

          <div className="flex shrink-0 items-center gap-2">
            {isPublished && viewSlug && (
              <Link
                href={`/posts/${viewSlug}`}
                target="_blank"
                className="rounded-md border border-border px-3.5 py-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
              >
                查看
              </Link>
            )}
            <button
              type="button"
              onClick={() => setPreviewOpen(true)}
              className="flex items-center gap-1.5 rounded-md border border-border px-3.5 py-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              <Eye className="h-3.5 w-3.5" />
              <span className="hidden sm:inline">预览</span>
            </button>
            <button
              type="button"
              onClick={() => saveDraft.mutate()}
              disabled={saveDraft.isPending || !title.trim() || !htmlToText(content).trim()}
              title="保存为草稿（Ctrl+S）"
              className="flex items-center gap-1.5 rounded-md border border-border px-3.5 py-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground disabled:opacity-50"
            >
              <Save className="h-3.5 w-3.5" />
              <span className="hidden sm:inline">存草稿</span>
            </button>
            <button
              type="button"
              {...hoverTapScale}
              onClick={doPublish}
              disabled={publish.isPending}
              className="flex items-center gap-1.5 rounded-md bg-accent px-5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
            >
              {publish.isPending ? '保存中...' : primaryLabel}
            </button>
          </div>
        </div>
      </div>

      {/* 编辑器主体：跟随后台布局的文章编辑页宽容器（max-w-7xl），不再额外限宽 */}
      <div className="px-4 py-8">
        <Reveal
          duration={0.45}
          className="rounded-xl border border-border bg-card p-6 sm:p-10"
        >
          {restoredHint && (
            <div className="mb-4 flex items-center gap-2 rounded-lg border border-accent/30 bg-accent/5 px-3 py-2 text-xs text-muted-foreground">
              <History className="h-3.5 w-3.5 shrink-0 text-accent" />
              <span className="min-w-0 flex-1">已恢复上次未发布的本地草稿</span>
              <button
                type="button"
                onClick={() => {
                  clearLocalDraft()
                  setRestoredHint(false)
                  setTitle('')
                  setMarkdown('')
                  setCategoryId(null)
                  setTags([])
                  setCover('')
                  setShowCover(false)
                  setBaseline(JSON.stringify({ t: '', c: '', g: null, s: '', v: '' }))
                }}
                className="shrink-0 rounded-md border border-border px-2 py-1 transition-colors hover:text-red-500"
              >
                丢弃
              </button>
            </div>
          )}
          <input
            type="text"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="给文章起个标题吧..."
            className="w-full border-none bg-transparent text-3xl font-semibold tracking-tight outline-none placeholder:text-muted-foreground/50 sm:text-4xl"
          />

          <div className="mt-5 space-y-3">
            <div className="flex flex-wrap items-center gap-2">
              <select
                value={categoryId ?? ''}
                onChange={(e) => setCategoryId(e.target.value ? Number(e.target.value) : null)}
                className="rounded-lg border border-border bg-background px-3 py-1.5 text-xs outline-none transition-colors focus:border-accent"
              >
                <option value="">未分类</option>
                {(categoriesQuery.data?.categories ?? []).map((cat) => (
                  <option key={cat.id} value={cat.id}>
                    {cat.name}
                  </option>
                ))}
              </select>

              {/* 已选标签 */}
              {tags.map((t) => (
                <span
                  key={t}
                  className="flex items-center gap-1 rounded-lg border border-accent/30 bg-accent/10 px-2 py-1 text-xs text-accent"
                >
                  {t}
                  <button type="button" onClick={() => removeTag(t)} title="移除">
                    <X className="h-3 w-3" />
                  </button>
                </span>
              ))}
              <input
                type="text"
                value={tagInput}
                onChange={(e) => setTagInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ',') {
                    e.preventDefault()
                    addTag()
                  }
                }}
                onBlur={addTag}
                placeholder="+ 新建标签，回车添加"
                className="w-36 rounded-lg border border-dashed border-border bg-transparent px-2.5 py-1 text-xs outline-none transition-colors focus:border-accent"
              />
            </div>

            {/* 文章封面 */}
            <div className="flex flex-wrap items-center gap-3 rounded-lg border border-dashed border-border px-3 py-2">
              <span className="text-xs font-medium text-muted-foreground">文章封面</span>
              {showCover ? (
                <>
                  <div className="h-12 w-20 shrink-0 overflow-hidden rounded-md border border-border bg-muted">
                    {cover ? (
                      /* eslint-disable-next-line @next/next/no-img-element */
                      <img src={cover} alt="封面" className="h-full w-full object-cover" />
                    ) : (
                      <span className="flex h-full items-center justify-center text-[10px] text-muted-foreground">
                        无
                      </span>
                    )}
                  </div>
                  <input
                    value={cover}
                    onChange={(e) => setCover(e.target.value)}
                    placeholder="图片地址，留空则自动使用正文第一张图"
                    className="min-w-0 flex-1 rounded-md border border-border bg-background px-2.5 py-1.5 text-xs outline-none focus:border-accent"
                  />
                  <button
                    type="button"
                    onClick={() => uploadCoverRef.current?.click()}
                    className="shrink-0 rounded-md border border-border px-2.5 py-1.5 text-xs transition-colors hover:border-accent/40 hover:text-accent"
                  >
                    上传
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setCover('')
                      setShowCover(false)
                    }}
                    className="shrink-0 rounded-md px-2 py-1.5 text-xs text-muted-foreground transition-colors hover:text-red-500"
                  >
                    移除
                  </button>
                </>
              ) : (
                <button
                  type="button"
                  onClick={() => setShowCover(true)}
                  className="rounded-md border border-border px-3 py-1.5 text-xs transition-colors hover:border-accent/40 hover:text-accent"
                >
                  + 设置封面
                </button>
              )}
              <input
                ref={uploadCoverRef}
                type="file"
                accept="image/*"
                hidden
                onChange={async (e) => {
                  const file = e.target.files?.[0]
                  e.target.value = ''
                  if (!file) return
                  try {
                    const url = await uploadImage(file)
                    setCover(url)
                    setShowCover(true)
                    notify.success('封面已上传，记得保存')
                  } catch (err) {
                    notify.error(err instanceof ApiError ? err.message : '上传失败')
                  }
                }}
              />
            </div>

            {/* 从已有标签中选择 */}
            {(tagsQuery.data?.tags ?? []).filter((t) => !tags.includes(t.name)).length > 0 && (
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-xs text-muted-foreground">选择已有标签：</span>
                {(tagsQuery.data?.tags ?? [])
                  .filter((t) => !tags.includes(t.name))
                  .slice(0, 20)
                  .map((t) => (
                    <button
                      key={t.id}
                      type="button"
                      onClick={() => setTags((arr) => [...arr, t.name])}
                      className="rounded-lg border border-border px-2 py-0.5 text-xs text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent"
                    >
                      + {t.name}
                      <span className="ml-1 opacity-60">{t.article_count}</span>
                    </button>
                  ))}
              </div>
            )}
          </div>

          <div className="mt-6 border-t border-border pt-2">
            <MarkdownEditor
              value={markdown}
              onChange={setMarkdown}
              onSaveRequest={() => {
                if (!saveDraft.isPending) saveDraft.mutate()
              }}
            />
          </div>

          <div className="mt-8 flex items-center justify-between border-t border-border pt-4 text-xs text-muted-foreground">
            <span>
              {wordCount} 字 · 约 {readMinutes} 分钟读完
            </span>
            <div className="flex items-center gap-4">
              <span className="sm:hidden">{autoSaveSwitch}</span>
              <span className="sm:hidden">{saveIndicator}</span>
              {mode === 'edit' && (
                <button
                  type="button"
                  onClick={async () => {
                    const ok = await notify.confirm({
                      title: '删除这篇文章？',
                      message: `「${article?.title}」将被永久删除，此操作无法撤销。`,
                      confirmText: '确认删除',
                      danger: true,
                    })
                    if (ok) trash.mutate()
                  }}
                  disabled={trash.isPending}
                  className="transition-colors hover:text-red-500 disabled:opacity-50"
                >
                  删除文章
                </button>
              )}
            </div>
          </div>
        </Reveal>

        <p
          ref={hintRef}
          className="mt-8 text-center text-xs text-muted-foreground"
        >
          {mode === 'new'
            ? '提示：内容会自动缓存在本浏览器，随时按 Ctrl+S 存为草稿，不怕丢。'
            : `提示：写完点右上角「${primaryLabel}」就能发表。草稿每 2 秒自动保存，Ctrl+S 可随时保存，不用怕丢。`}
        </p>
      </div>
      {/* 全屏实时预览 */}
      {previewOpen && (
        <div className="fixed inset-0 z-[200] overflow-y-auto bg-background">
          <div className="sticky top-0 z-10 flex items-center justify-between border-b border-border bg-background/90 px-4 py-3 backdrop-blur">
            <span className="flex items-center gap-2 text-sm font-medium">
              <Eye className="h-4 w-4 text-accent" /> 实时预览
            </span>
            <button
              type="button"
              onClick={() => setPreviewOpen(false)}
              className="flex items-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              <X className="h-4 w-4" /> 关闭预览
            </button>
          </div>
          <ArticlePreview
            title={title}
            categoryName={previewCategory}
            tags={tags}
            authorName={previewAuthor}
            date={previewDate}
            views={previewViews}
            contentHtml={content}
          />
        </div>
      )}
    </form>
  )
}

export function NewArticlePage() {
  return (
    <PageTransition>
      <EditorShell mode="new" />
    </PageTransition>
  )
}

export function EditArticlePage({ id }: { id: number }) {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['article', id],
    queryFn: () => fetchArticle(id),
  })

  if (isLoading) {
    return (
      <div className="mx-auto max-w-5xl px-4 py-10">
        <PageLoading minHeight="3.5rem" className="rounded-lg" />
        <PageLoading minHeight="3rem" className="mt-4 rounded-lg" />
        <PageLoading minHeight="24rem" className="mt-4 rounded-lg" />
      </div>
    )
  }
  if (isError || !data) {
    return (
      <Reveal
        y={0}
        className="mx-auto max-w-5xl px-4 py-24 text-center text-muted-foreground"
      >
        文章不存在或无权访问
      </Reveal>
    )
  }

  return (
    <PageTransition>
      <EditorShell key={data.article.id} mode="edit" article={data.article} />
    </PageTransition>
  )
}
