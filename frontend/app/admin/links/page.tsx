'use client'

import { useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import gsap from 'gsap'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  CheckCircle2,
  ExternalLink,
  ImagePlus,
  Inbox,
  Link2,
  Pencil,
  Plus,
  RefreshCw,
  Trash2,
  XCircle,
} from 'lucide-react'
import {
  approveLinkApplication,
  checkAllFriendLinks,
  checkFriendLink,
  createFriendLink,
  deleteFriendLink,
  deleteLinkApplication,
  fetchAdminLinks,
  fetchLinkApplications,
  rejectLinkApplication,
  updateFriendLink,
  uploadImage,
  validateFriendLink,
  ApiError,
} from '@/lib/api'
import type {
  AdminFriendLink,
  FriendLinkInput,
  LinkValidation,
} from '@/lib/api'
import type { LinkApplication } from '@/lib/types'
import { useNotify } from '@/components/toast'
import { AdminEmpty, AdminPageHeader } from '@/components/admin-page'
import { Reveal, easeOut, hoverTapScale, prefersReducedMotion } from '@/components/motion'
import { Modal } from '@/components/modal'
import { RowLoading } from '@/components/page-loader'
import { inputClass, badgeSuccess, badgeWarning, badgeDanger } from '@/lib/ui'

interface DialogState {
  open: boolean
  editing: AdminFriendLink | null
}

type AdminTab = 'links' | 'applications'

function LinkDialog({
  editing,
  onClose,
  onSaved,
}: {
  editing: AdminFriendLink | null
  onClose: () => void
  onSaved: () => void
}) {
  const notify = useNotify()
  const fileRef = useRef<HTMLInputElement>(null)
  const [form, setForm] = useState<FriendLinkInput>({
    name: editing?.name ?? '',
    url: editing?.url ?? '',
    check_url: editing?.check_url ?? '',
    icon_url: editing?.icon_url ?? '',
    description: editing?.description ?? '',
    sort_order: editing?.sort_order ?? 0,
  })
  const [validation, setValidation] = useState<LinkValidation | null>(null)

  const set = <K extends keyof FriendLinkInput>(key: K, value: FriendLinkInput[K]) => {
    setForm((f) => ({ ...f, [key]: value }))
    setValidation(null) // 改表单后上次预检结果失效
  }

  const upload = useMutation({
    mutationFn: (file: File) => uploadImage(file),
    onSuccess: (url) => {
      set('icon_url', url)
      notify.success('图标已上传，保存后生效')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '上传失败'),
  })

  const save = useMutation({
    mutationFn: () =>
      editing ? updateFriendLink(editing.id, form) : createFriendLink(form),
    onSuccess: () => {
      notify.success(editing ? '友链已更新' : '友链已添加')
      onSaved()
      onClose()
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '保存失败'),
  })

  // 预检：站点可达 + 是否含本站反链
  const check = useMutation({
    mutationFn: () =>
      validateFriendLink({ url: form.url, check_url: form.check_url || undefined }),
    onSuccess: (res) => {
      setValidation(res)
      if (!res.reachable) notify.error(res.message)
      else if (!res.has_backlink) notify.error(res.message)
      else notify.success(res.message)
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '检测失败'),
  })

  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!form.name.trim() || !form.url.trim()) {
      notify.error('请填写网站名称和网站链接')
      return
    }
    // 新增时要求先通过预检（可达即可，反链仅提示）
    if (!validation) {
      check.mutate()
      notify.error('请先点击「检测站点」确认对方网站可访问')
      return
    }
    if (!validation.reachable) {
      notify.error('站点无法访问，请检查链接后再添加')
      return
    }
    save.mutate()
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={editing ? '编辑友链' : '添加友链'}
      description="填写对方站点信息。添加前可「检测站点」确认可达，并检查对方页面是否已加本站反链。"
      width={560}
      footer={
        <div className="flex items-center justify-between gap-2">
          <button
            type="button"
            onClick={() => check.mutate()}
            disabled={check.isPending || !form.url.trim()}
            className="flex items-center gap-1.5 rounded-lg border border-border px-4 py-2 text-sm font-medium text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4 ${check.isPending ? 'animate-spin' : ''}`} />
            {check.isPending ? '检测中...' : '检测站点'}
          </button>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg border border-border px-4 py-2 text-sm font-medium transition-colors hover:bg-muted"
            >
              取消
            </button>
            <button
              type="submit"
              form="friend-link-form"
              {...hoverTapScale}
              disabled={save.isPending}
              className="rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
            >
              {save.isPending ? '保存中...' : editing ? '保存修改' : '添加友链'}
            </button>
          </div>
        </div>
      }
    >
      <form id="friend-link-form" onSubmit={submit} className="space-y-4">
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <label className="text-sm font-medium">网站名称 *</label>
            <input
              value={form.name}
              onChange={(e) => set('name', e.target.value)}
              placeholder="如：某某的博客"
              className={inputClass}
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-sm font-medium">排序（越小越靠前）</label>
            <input
              type="number"
              value={form.sort_order ?? 0}
              onChange={(e) => set('sort_order', Number(e.target.value))}
              className={inputClass}
            />
          </div>
        </div>

        <div className="space-y-1.5">
          <label className="text-sm font-medium">网站链接 *（点击跳转的地址）</label>
          <input
            value={form.url}
            onChange={(e) => set('url', e.target.value)}
            placeholder="https://friend-site.com"
            className={inputClass}
          />
        </div>

        <div className="space-y-1.5">
          <label className="text-sm font-medium">友链检测页面（留空则检测网站链接）</label>
          <input
            value={form.check_url}
            onChange={(e) => set('check_url', e.target.value)}
            placeholder="https://friend-site.com/links 或留空"
            className={inputClass}
          />
          <p className="text-xs text-muted-foreground">
            填写对方放置友链的页面（如 /links）。反链检测会在该页面查找本站域名。
          </p>
        </div>

        {/* 预检结果 */}
        {validation && (
          <Reveal
            y={-6}
            className={`flex items-start gap-2.5 rounded-lg border p-3 text-xs leading-relaxed ${
              validation.reachable
                ? validation.has_backlink
                  ? 'border-emerald-300 bg-emerald-50 text-emerald-700 dark:border-emerald-900/50 dark:bg-emerald-950/40 dark:text-emerald-300'
                  : 'border-amber-300 bg-amber-50 text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/40 dark:text-amber-300'
                : 'border-red-300 bg-red-50 text-red-700 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-300'
            }`}
          >
            {validation.reachable ? (
              validation.has_backlink ? (
                <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
              ) : (
                <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
              )
            ) : (
              <XCircle className="mt-0.5 h-4 w-4 shrink-0" />
            )}
            <div className="min-w-0 space-y-1">
              <p className="font-medium">{validation.message}</p>
              <p className="opacity-70">
                检测地址：{validation.backlink_host}
                {validation.status_code > 0 && ` · HTTP ${validation.status_code}`}
              </p>
              {validation.expected_hosts && (
                <p className="opacity-70">期望反链域名：{validation.expected_hosts}</p>
              )}
            </div>
          </Reveal>
        )}

        <div className="space-y-1.5">
          <label className="text-sm font-medium">网站图标</label>
          <div className="flex items-center gap-3">
            <div className="flex h-12 w-12 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-border bg-muted">
              {form.icon_url ? (
                /* eslint-disable-next-line @next/next/no-img-element */
                <img src={form.icon_url} alt="icon" className="h-full w-full object-cover" />
              ) : (
                <ImagePlus className="h-4 w-4 text-muted-foreground/50" />
              )}
            </div>
            <input
              value={form.icon_url}
              onChange={(e) => set('icon_url', e.target.value)}
              placeholder="图片地址，或点击右侧上传"
              className={inputClass}
            />
            <button
              type="button"
              onClick={() => fileRef.current?.click()}
              disabled={upload.isPending}
              className="shrink-0 rounded-lg border border-border px-3 py-2 text-xs font-medium transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
            >
              {upload.isPending ? '上传中...' : '上传'}
            </button>
            <input
              ref={fileRef}
              type="file"
              accept="image/*"
              hidden
              onChange={(e) => {
                const file = e.target.files?.[0]
                if (file) upload.mutate(file)
                e.target.value = ''
              }}
            />
          </div>
        </div>

        <div className="space-y-1.5">
          <label className="text-sm font-medium">简介（可选）</label>
          <input
            value={form.description}
            onChange={(e) => set('description', e.target.value)}
            placeholder="一句话介绍对方站点"
            className={inputClass}
          />
        </div>
      </form>
    </Modal>
  )
}

/** 友链申请审核面板（通过→自动建链 / 拒绝（需原因）/ 删除） */
function LinkApplicationsPanel() {
  const notify = useNotify()
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState<'pending' | 'approved' | 'rejected' | ''>('')
  const [rejecting, setRejecting] = useState<LinkApplication | null>(null)
  const [rejectReason, setRejectReason] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'link-applications', filter],
    queryFn: () => fetchLinkApplications(filter || undefined),
    refetchInterval: 30000,
    refetchOnWindowFocus: false,
  })

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['admin', 'link-applications'] })
    queryClient.invalidateQueries({ queryKey: ['admin', 'links'] })
  }

  const approve = useMutation({
    mutationFn: (id: number) => approveLinkApplication(id),
    onSuccess: () => {
      invalidate()
      notify.success('已通过，友链已自动添加（后台探测可达性）')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '操作失败'),
  })

  const reject = useMutation({
    mutationFn: ({ id, reason }: { id: number; reason: string }) => rejectLinkApplication(id, reason),
    onSuccess: () => {
      invalidate()
      setRejecting(null)
      setRejectReason('')
      notify.success('已拒绝该申请')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '操作失败'),
  })

  const remove = useMutation({
    mutationFn: (id: number) => deleteLinkApplication(id),
    onSuccess: () => {
      invalidate()
      notify.success('申请记录已删除')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '删除失败'),
  })

  const apps = data?.applications ?? []
  const statusBadge = (status: LinkApplication['status']) =>
    status === 'pending' ? badgeWarning : status === 'approved' ? badgeSuccess : badgeDanger
  const statusText = (status: LinkApplication['status']) =>
    status === 'pending' ? '待审核' : status === 'approved' ? '已通过' : '已拒绝'

  const tabs: { key: typeof filter; label: string }[] = [
    { key: 'pending', label: '待审核' },
    { key: 'approved', label: '已通过' },
    { key: 'rejected', label: '已拒绝' },
    { key: '', label: '全部' },
  ]

  return (
    <div>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm text-muted-foreground">
          待审核 <b className="text-foreground">{data?.pending ?? 0}</b> 条
        </span>
        <div className="ml-auto flex flex-wrap gap-1 rounded-lg border border-border bg-muted/60 p-1">
          {tabs.map((t) => (
            <button
              key={t.key}
              onClick={() => setFilter(t.key)}
              className={`rounded-md px-3 py-1 text-xs font-medium transition-colors ${
                filter === t.key ? 'bg-card text-accent shadow-sm' : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
      </div>

      {isLoading ? (
        <div className="mt-4">
          <RowLoading rows={3} />
        </div>
      ) : apps.length === 0 ? (
        <AdminEmpty icon={Inbox} title={<>没有{filter ? '该状态的' : ''}申请记录</>} />
      ) : (
        <div className="mt-4 space-y-3">
          {apps.map((app: LinkApplication) => (
            <Reveal
              key={app.id}
              y={10}
              className="card-flat rounded-xl border border-border bg-card p-4 transition-colors hover:border-accent/30"
            >
              <div className="flex flex-wrap items-center gap-2">
                <p className="font-medium">{app.site_name}</p>
                <span className={statusBadge(app.status)}>{statusText(app.status)}</span>
                <span className="text-xs text-muted-foreground">
                  {new Date(app.created_at).toLocaleString('zh-CN')}
                </span>
              </div>
              <p className="mt-1 text-xs text-muted-foreground">
                {app.url}
                {app.email && ` · 联系：${app.email}`}
              </p>
              {app.description && <p className="mt-1 text-xs text-muted-foreground/80">{app.description}</p>}
              {app.status === 'rejected' && app.reason && (
                <p className="mt-1 text-xs text-red-500">拒绝原因：{app.reason}</p>
              )}
              <div className="mt-3 flex items-center gap-2">
                {app.status === 'pending' && (
                  <>
                    <button
                      onClick={() => approve.mutate(app.id)}
                      disabled={approve.isPending}
                      className="inline-flex items-center gap-1 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-white shadow-sm shadow-accent/25 transition-opacity hover:opacity-95 disabled:opacity-50"
                    >
                      <CheckCircle2 className="h-3.5 w-3.5" />
                      通过并添加
                    </button>
                    <button
                      onClick={() => {
                        setRejecting(app)
                        setRejectReason('')
                      }}
                      className="inline-flex items-center gap-1 rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:border-red-500/40 hover:text-red-500"
                    >
                      <XCircle className="h-3.5 w-3.5" />
                      拒绝
                    </button>
                  </>
                )}
                <button
                  onClick={async () => {
                    const ok = await notify.confirm({
                      title: `删除申请「${app.site_name}」？`,
                      message: '删除后无法恢复。',
                      confirmText: '删除',
                      danger: true,
                    })
                    if (ok) remove.mutate(app.id)
                  }}
                  className="ml-auto inline-flex items-center gap-1 rounded-md px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-red-500/10 hover:text-red-500"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  删除记录
                </button>
              </div>
            </Reveal>
          ))}
        </div>
      )}

      {rejecting && (
        <Modal open onClose={() => setRejecting(null)} title={`拒绝「${rejecting.site_name}」的申请`}>
          <div className="space-y-4">
            <label className="block">
              <span className="text-sm font-medium">拒绝原因（将展示给申请人）</span>
              <input
                value={rejectReason}
                onChange={(e) => setRejectReason(e.target.value)}
                placeholder="如：站点暂无法访问 / 内容不符 / 未添加本站友链"
                className={`${inputClass} mt-1`}
              />
            </label>
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setRejecting(null)}
                className="rounded-lg border border-border px-4 py-2 text-sm text-muted-foreground transition-colors hover:text-foreground"
              >
                取消
              </button>
              <button
                onClick={() => rejectReason.trim() && reject.mutate({ id: rejecting.id, reason: rejectReason.trim() })}
                disabled={!rejectReason.trim() || reject.isPending}
                className="inline-flex items-center gap-1.5 rounded-lg bg-red-500 px-4 py-2 text-sm font-medium text-white shadow-sm transition-opacity hover:opacity-95 disabled:opacity-50"
              >
                <XCircle className="h-4 w-4" />
                确认拒绝
              </button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  )
}

export default function AdminLinksPage() {
  const notify = useNotify()
  const queryClient = useQueryClient()
  const [dialog, setDialog] = useState<DialogState>({ open: false, editing: null })
  const [checkingId, setCheckingId] = useState<number | null>(null)
  const [tab, setTab] = useState<AdminTab>('links')

  // tab 高亮背景：切换时重挂并做 scaleX 入场（替代原 layoutId 共享布局动画）
  const tabBgRef = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    const el = tabBgRef.current
    if (el && !prefersReducedMotion()) {
      gsap.fromTo(el, { scaleX: 0 }, { scaleX: 1, duration: 0.3, ease: easeOut })
    }
  }, [tab])

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'links'],
    queryFn: fetchAdminLinks,
  })

  const appsQuery = useQuery({
    queryKey: ['admin', 'link-applications', 'pending'],
    queryFn: () => fetchLinkApplications('pending'),
    refetchOnWindowFocus: false,
  })
  const pendingCount = appsQuery.data?.pending ?? 0

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin', 'links'] })

  const remove = useMutation({
    mutationFn: (id: number) => deleteFriendLink(id),
    onSuccess: () => {
      invalidate()
      notify.success('友链已删除')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '删除失败'),
  })

  const checkAll = useMutation({
    mutationFn: checkAllFriendLinks,
    onSuccess: (res) => {
      invalidate()
      const broken = res.links.filter((l) => !l.available).length
      notify.success(`检测完成：${res.checked} 个站点，${broken} 个失效`)
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '检测失败'),
  })

  const checkOne = useMutation({
    mutationFn: (id: number) => checkFriendLink(id),
    onMutate: (id) => setCheckingId(id),
    onSettled: () => setCheckingId(null),
    onSuccess: (res) => {
      invalidate()
      notify.success(res.available ? '站点正常' : '站点无法访问，已标记失效')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '检测失败'),
  })

  const links = data?.links ?? []

  return (
    <div>
      <AdminPageHeader
        title="友情链接"
        description={
          <>
            {data ? `共 ${links.length} 个友链` : '加载中...'} · 每日自动检测，失效站点将禁止跳转并脱敏
          </>
        }
        actions={
          tab === 'links' ? (
            <>
              <Link
                href="/links"
                target="_blank"
                className="flex items-center gap-1.5 rounded-lg border border-border px-4 py-2 text-sm text-muted-foreground transition-colors hover:text-foreground"
              >
                <ExternalLink className="h-4 w-4" />
                查看页面
              </Link>
              <button
                onClick={() => checkAll.mutate()}
                disabled={checkAll.isPending || links.length === 0}
                {...hoverTapScale}
                className="flex items-center gap-1.5 rounded-lg border border-border px-4 py-2 text-sm font-medium transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
              >
                <RefreshCw className={`h-4 w-4 ${checkAll.isPending ? 'animate-spin' : ''}`} />
                {checkAll.isPending ? '检测中...' : '检测全部'}
              </button>
              <button
                onClick={() => setDialog({ open: true, editing: null })}
                {...hoverTapScale}
                className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white shadow-md shadow-accent/25"
              >
                <Plus className="h-4 w-4" />
                添加友链
              </button>
            </>
          ) : undefined
        }
      />

      {/* tab：友链管理 / 申请审核（待审核数徽标） */}
      <nav className="mt-5 flex w-fit flex-wrap gap-1 rounded-xl border border-border bg-muted/60 p-1">
        {([
          { key: 'links' as AdminTab, label: '友链管理', icon: <Link2 className="h-4 w-4" /> },
          { key: 'applications' as AdminTab, label: '申请审核', icon: <Inbox className="h-4 w-4" />, badge: pendingCount },
        ]).map((t) => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={`relative flex items-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium transition-colors ${
              tab === t.key ? 'text-accent' : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            {tab === t.key && (
              <span
                key={t.key}
                ref={tabBgRef}
                className="absolute inset-0 rounded-lg border border-border bg-card shadow-sm"
              />
            )}
            <span className="relative inline-flex items-center gap-1.5">
              {t.icon}
              {t.label}
              {'badge' in t && t.badge ? (
                <span className="relative rounded-full bg-accent/15 px-1.5 text-xs font-bold text-accent">
                  {t.badge}
                </span>
              ) : null}
            </span>
          </button>
        ))}
      </nav>

      {tab === 'applications' ? (
        <div className="mt-6">
          <LinkApplicationsPanel />
        </div>
      ) : (
      <>
      {isLoading ? (
        <div className="mt-6">
          <RowLoading rows={3} />
        </div>
      ) : links.length === 0 ? (
        <AdminEmpty icon={Plus} title="还没有友链，点击「添加友链」创建" />
      ) : (
        <div className="mt-6 space-y-3">
          {links.map((link: AdminFriendLink, i: number) => (
            <Reveal
              key={link.id}
              y={10}
              delay={i * 0.04}
              duration={0.3}
              className="card-flat flex flex-wrap items-center gap-4 rounded-xl border border-border bg-card p-4 transition-colors hover:border-accent/30"
            >
              <div className="flex h-11 w-11 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-border bg-background">
                {link.icon_url ? (
                  /* eslint-disable-next-line @next/next/no-img-element */
                  <img src={link.icon_url} alt={link.name} className="h-full w-full object-cover" />
                ) : (
                  <span className="text-sm font-bold text-accent">
                    {link.name.charAt(0).toUpperCase()}
                  </span>
                )}
              </div>

              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="truncate font-medium">{link.name}</p>
                  {link.available ? (
                    <span className="flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-600 dark:text-emerald-400">
                      <CheckCircle2 className="h-3 w-3" />
                      正常
                    </span>
                  ) : (
                    <span className="flex items-center gap-1 rounded-full bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-600 dark:text-amber-400">
                      <AlertTriangle className="h-3 w-3" />
                      失效
                    </span>
                  )}
                  <span className="text-xs text-muted-foreground">排序 {link.sort_order}</span>
                </div>
                <p className="mt-1 truncate text-xs text-muted-foreground">
                  {link.url}
                  {link.check_url && link.check_url !== link.url && ` · 检测页 ${link.check_url}`}
                </p>
                <p className="mt-0.5 text-xs text-muted-foreground/70">
                  {link.last_checked_at
                    ? `最后检测：${new Date(link.last_checked_at).toLocaleString('zh-CN')}`
                    : '尚未检测'}
                </p>
              </div>

              <div className="flex shrink-0 items-center gap-1 text-sm">
                <button
                  onClick={() => checkOne.mutate(link.id)}
                  disabled={checkingId === link.id}
                  className="flex items-center gap-1 rounded-md px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
                  title="立即检测"
                >
                  <RefreshCw className={`h-3.5 w-3.5 ${checkingId === link.id ? 'animate-spin' : ''}`} />
                  检测
                </button>
                <button
                  onClick={() => setDialog({ open: true, editing: link })}
                  className="flex items-center gap-1 rounded-md px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                >
                  <Pencil className="h-3.5 w-3.5" />
                  编辑
                </button>
                <button
                  onClick={async () => {
                    const ok = await notify.confirm({
                      title: `删除友链「${link.name}」？`,
                      message: '删除后无法恢复。',
                      confirmText: '删除',
                      danger: true,
                    })
                    if (ok) remove.mutate(link.id)
                  }}
                  disabled={remove.isPending}
                  className="flex items-center gap-1 rounded-md px-3 py-1.5 text-xs text-red-500 transition-colors hover:bg-red-500/10 disabled:opacity-50"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  删除
                </button>
              </div>
            </Reveal>
          ))}
        </div>
      )}

      {dialog.open && (
        <LinkDialog
          editing={dialog.editing}
          onClose={() => setDialog({ open: false, editing: null })}
          onSaved={invalidate}
        />
      )}
      </>
      )}
    </div>
  )
}
