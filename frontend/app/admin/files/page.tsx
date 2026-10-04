'use client'

import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import gsap from 'gsap'
import {
  Copy,
  Check,
  Download,
  File as FileIcon,
  FileArchive,
  FileImage,
  FileText,
  HardDrive,
  Search,
  Trash2,
  UploadCloud,
  Gauge,
} from 'lucide-react'
import {
  deleteFile,
  downloadFile,
  fetchAdminSettings,
  fetchFiles,
  updateAdminSettings,
  uploadFile,
  ApiError,
} from '@/lib/api'
import type { FileAssetItem } from '@/lib/api'
import { useNotify } from '@/components/toast'
import { PageLoading, RowLoading } from '@/components/page-loader'
import { PageTransition, Reveal, hoverTapScale, prefersReducedMotion } from '@/components/motion'
import { formatSize, inputClass } from '@/lib/ui'
import { Pagination } from '@/components/pagination'

function FileTypeIcon({ mime, name }: { mime: string; name: string }) {
  const ext = name.split('.').pop()?.toLowerCase() ?? ''
  if (mime.startsWith('image/')) return <FileImage className="h-5 w-5 text-sky-500" />
  if (['zip', 'rar', '7z', 'tar', 'gz'].includes(ext)) return <FileArchive className="h-5 w-5 text-amber-500" />
  if (['txt', 'md', 'json', 'log', 'csv'].includes(ext) || mime.startsWith('text/'))
    return <FileText className="h-5 w-5 text-emerald-500" />
  return <FileIcon className="h-5 w-5 text-muted-foreground" />
}

function TransferSettings() {
  const notify = useNotify()
  const queryClient = useQueryClient()

  const settingsQuery = useQuery({
    queryKey: ['admin', 'settings'],
    queryFn: fetchAdminSettings,
  })

  const serverSettings = settingsQuery.data?.settings as unknown as Record<string, unknown> | undefined
  // 表单值由「服务端值 + 用户本地编辑」派生：只在用户改动时记录覆盖值，
  // 避免把数据复制进 state（渲染期 setState 会多跑一轮渲染，且轮询回来的
  // 新对象引用容易把用户正在编辑的内容重置）。
  const [edited, setEdited] = useState<{ max?: string; up?: string; down?: string }>({})
  const form = {
    max: edited.max ?? String(serverSettings?.upload_max_mb ?? '50'),
    up: edited.up ?? String(serverSettings?.upload_speed_kb ?? '0'),
    down: edited.down ?? String(serverSettings?.download_speed_kb ?? '0'),
  }
  const setForm = (patch: { max?: string; up?: string; down?: string }) =>
    setEdited((prev) => ({ ...prev, ...patch }))

  const save = useMutation({
    mutationFn: () =>
      updateAdminSettings({
        upload_max_mb: Number(form.max || 50),
        upload_speed_kb: Number(form.up || 0),
        download_speed_kb: Number(form.down || 0),
      } as unknown as Record<string, unknown>),
    onSuccess: async () => {
      // 必须先等 refetch 完成，再清掉本地编辑标记。
      // 若先 setEdited({}) 再 invalidate，缓存旧值会在中间那一轮渲染里
      // 被读出来，用户会看到输入框闪回旧值。
      await queryClient.invalidateQueries({ queryKey: ['admin', 'settings'] })
      setEdited({})
      notify.success('传输设置已保存')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '保存失败'),
  })

  if (!serverSettings) {
    return <PageLoading minHeight="6rem" />
  }

  return (
    <div className="rounded-2xl border border-border bg-card p-5">
      <h2 className="flex items-center gap-2 text-sm font-semibold">
        <Gauge className="h-4 w-4 text-accent" />
        传输设置
      </h2>
      <div className="mt-4 grid gap-4 sm:grid-cols-3">
        <div className="space-y-1.5">
          <label className="text-sm font-medium">最大上传大小（MB）</label>
          <input
            type="number"
            min={1}
            value={form.max}
            onChange={(e) => setForm({ max: e.target.value })}
            className={inputClass}
          />
        </div>
        <div className="space-y-1.5">
          <label className="text-sm font-medium">上传限速（KB/s）</label>
          <input
            type="number"
            min={0}
            value={form.up}
            onChange={(e) => setForm({ up: e.target.value })}
            className={inputClass}
          />
          <p className="text-xs text-muted-foreground">0 表示不限速</p>
        </div>
        <div className="space-y-1.5">
          <label className="text-sm font-medium">下载限速（KB/s）</label>
          <input
            type="number"
            min={0}
            value={form.down}
            onChange={(e) => setForm({ down: e.target.value })}
            className={inputClass}
          />
          <p className="text-xs text-muted-foreground">0 表示不限速</p>
        </div>
      </div>
      <div className="mt-4 flex justify-end">
        <button
          type="button"
          onClick={() => save.mutate()}
          disabled={save.isPending}
          {...hoverTapScale}
          className="rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
        >
          {save.isPending ? '保存中...' : '保存传输设置'}
        </button>
      </div>
    </div>
  )
}

export default function AdminFilesPage() {
  const notify = useNotify()
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState('')
  // 分页状态。此前 page 写死为 1 且没有翻页 UI，第 51 个文件起在后台完全点不到。
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const [progress, setProgress] = useState<number | null>(null)
  const [dragging, setDragging] = useState(false)
  const [copiedId, setCopiedId] = useState<number | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  // 「已复制」提示定时器句柄：列表页切换频繁，卸载时必须清理，
  // 否则回调会向已卸载组件 setState
  const copiedTimerRef = useRef<number | null>(null)

  // 上传进度条：scaleX 补间（合成层，避免 width 动画每帧 layout 重排）
  const progressBarRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const bar = progressBarRef.current
    if (!bar) return
    const target = Math.max(0, Math.min(100, progress ?? 0)) / 100
    if (prefersReducedMotion()) {
      gsap.set(bar, { scaleX: target })
      return
    }
    gsap.fromTo(bar, { scaleX: 0 }, { scaleX: target, duration: 0.2, ease: 'power2.out' })
  }, [progress])

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'files', query, page, pageSize],
    queryFn: () => fetchFiles({ page, page_size: pageSize, q: query || undefined }),
  })

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin', 'files'] })

  // 卸载时清掉「已复制」定时器，避免回调向已卸载组件 setState
  useEffect(
    () => () => {
      if (copiedTimerRef.current !== null) window.clearTimeout(copiedTimerRef.current)
    },
    [],
  )

  const upload = async (files: FileList | null) => {
    if (!files || files.length === 0) return
    const maxMb = data?.max_upload_mb ?? 50
    for (const file of Array.from(files)) {
      if (file.size > maxMb * 1024 * 1024) {
        notify.error(`「${file.name}」超过 ${maxMb} MB 限制`)
        continue
      }
      setProgress(0)
      try {
        await uploadFile(file, setProgress)
        notify.success(`「${file.name}」上传成功`)
      } catch (e) {
        notify.error(e instanceof ApiError ? e.message : '上传失败')
      } finally {
        setProgress(null)
      }
    }
    invalidate()
  }

  const remove = useMutation({
    mutationFn: (id: number) => deleteFile(id),
    onSuccess: () => {
      invalidate()
      notify.success('文件已删除')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '删除失败'),
  })

  const copyUrl = async (file: FileAssetItem) => {
    const full = file.url.startsWith('http') ? file.url : `${window.location.protocol}//${window.location.hostname}:8080${file.url}`
    try {
      await navigator.clipboard.writeText(full)
      setCopiedId(file.id)
      if (copiedTimerRef.current !== null) window.clearTimeout(copiedTimerRef.current)
      copiedTimerRef.current = window.setTimeout(() => {
        setCopiedId(null)
        copiedTimerRef.current = null
      }, 1500)
      notify.success('下载链接已复制')
    } catch {
      notify.error('复制失败，请手动复制：' + full)
    }
  }

  const files = data?.files ?? []

  return (
    <PageTransition>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">文件管理</h1>
          <p className="mt-1 flex flex-wrap items-center gap-x-3 text-sm text-muted-foreground">
            <span>{data ? `${data.total} 个文件` : '加载中...'}</span>
            {data && (
              <span className="flex items-center gap-1">
                <HardDrive className="h-3.5 w-3.5" />
                已用 {formatSize(data.total_size)}
              </span>
            )}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative w-full sm:w-auto">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  setQuery(search)
                  // 关键词一变就回第 1 页：否则可能停在一个已不存在的页码上。
                  setPage(1)
                }
              }}
              placeholder="搜索文件名，回车"
              className="w-full rounded-lg border border-border bg-card py-2 pl-9 pr-3 text-sm outline-none transition-all focus:border-accent focus:ring-2 focus:ring-accent/20 sm:w-52"
            />
          </div>
          <button
            onClick={() => fileRef.current?.click()}
            disabled={progress !== null}
            {...hoverTapScale}
            className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
          >
            <UploadCloud className="h-4 w-4" />
            {progress !== null ? `上传中 ${progress}%` : '上传文件'}
          </button>
          <input
            ref={fileRef}
            type="file"
            multiple
            hidden
            onChange={(e) => {
              upload(e.target.files)
              e.target.value = ''
            }}
          />
        </div>
      </div>

      {/* 拖拽上传区 */}
      <div
        onDragOver={(e) => {
          e.preventDefault()
          setDragging(true)
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault()
          setDragging(false)
          upload(e.dataTransfer.files)
        }}
        className={`mt-5 rounded-xl border-2 border-dashed p-6 text-center transition-colors ${
          dragging ? 'border-accent bg-accent/5' : 'border-border'
        }`}
      >
        <UploadCloud className={`mx-auto h-8 w-8 ${dragging ? 'text-accent' : 'text-muted-foreground/50'}`} />
        <p className="mt-2 text-sm text-muted-foreground">
          拖拽文件到这里上传，或点击右上角「上传文件」
        </p>
        <p className="mt-0.5 text-xs text-muted-foreground/70">
          单个文件最大 {data?.max_upload_mb ?? 50} MB
        </p>
        {progress !== null && (
            <div className="mx-auto mt-3 h-1.5 w-64 overflow-hidden rounded-full bg-muted">
              <div ref={progressBarRef} className="h-full w-full origin-left rounded-full bg-accent" />
            </div>
        )}
      </div>

      {/* 文件列表 */}
      {isLoading ? (
        <div className="mt-4">
          <RowLoading rows={4} />
        </div>
      ) : files.length === 0 ? (
        <div className="mt-4 rounded-xl border border-dashed p-12 text-center text-muted-foreground">
          {query ? '没有匹配的文件' : '还没有上传任何文件'}
        </div>
      ) : (
        <div className="mt-4 space-y-2">
          {files.map((file, i) => (
            <Reveal
              key={file.id}
              y={8}
              delay={i * 0.03}
              duration={0.25}
              className="flex flex-wrap items-center gap-3 rounded-2xl border border-border bg-card p-3.5 transition-colors hover:border-accent/30"
            >
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-muted">
                <FileTypeIcon mime={file.mime_type} name={file.original_name} />
              </div>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{file.original_name}</p>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  {formatSize(file.size)} · {new Date(file.created_at).toLocaleString('zh-CN')}
                </p>
              </div>
              <div className="flex shrink-0 items-center gap-1 text-xs">
                <button
                  onClick={() => copyUrl(file)}
                  className="flex items-center gap-1 rounded-md px-2.5 py-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                  title="复制下载链接（可直接在文章中引用）"
                >
                  {copiedId === file.id ? (
                    <Check className="h-3.5 w-3.5 text-emerald-500" />
                  ) : (
                    <Copy className="h-3.5 w-3.5" />
                  )}
                  复制链接
                </button>
                <button
                  onClick={async () => {
                    try {
                      await downloadFile(file.id, file.original_name)
                    } catch (e) {
                      notify.error(e instanceof ApiError ? e.message : '下载失败')
                    }
                  }}
                  className="flex items-center gap-1 rounded-md px-2.5 py-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                  title="下载"
                >
                  <Download className="h-3.5 w-3.5" />
                  下载
                </button>
                <button
                  onClick={async () => {
                    const ok = await notify.confirm({
                      title: `删除文件「${file.original_name}」？`,
                      message: '删除后无法恢复，已引用该文件的链接将失效。',
                      confirmText: '删除',
                      danger: true,
                    })
                    if (ok) remove.mutate(file.id)
                  }}
                  disabled={remove.isPending}
                  className="flex items-center gap-1 rounded-md px-2.5 py-1.5 text-red-500 transition-colors hover:bg-red-500/10 disabled:opacity-50"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  删除
                </button>
              </div>
            </Reveal>
          ))}
        </div>
      )}

      <Pagination
        page={page}
        pageSize={pageSize}
        total={data?.total ?? 0}
        onChange={(p, size) => {
          setPage(p)
          setPageSize(size)
        }}
      />

      <div className="mt-6">
        <TransferSettings />
      </div>
    </PageTransition>
  )
}
