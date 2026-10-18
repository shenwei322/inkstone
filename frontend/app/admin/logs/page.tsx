'use client'

import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  CalendarDays,
  CheckCircle2,
  Download,
  FileText,
  KeyRound,
  Link2,
  MessageSquare,
  RefreshCw,
  ScrollText,
  Search,
  Server,
  Settings,
  Tags,
  Upload,
  User,
  XCircle,
} from 'lucide-react'
import { downloadLogs, fetchLogOverview, fetchLogs } from '@/lib/api'
import type { OperationLog } from '@/lib/api'
import { useNotify } from '@/components/toast'
import { Select } from '@/components/form-controls'
import { RowLoading, Spinner } from '@/components/page-loader'
import { AdminEmpty, AdminPageHeader } from '@/components/admin-page'

const PAGE_SIZE = 30

const CATEGORIES: { key: string; label: string; icon: typeof User }[] = [
  { key: '', label: '全部', icon: ScrollText },
  { key: 'auth', label: '登录认证', icon: KeyRound },
  { key: 'article', label: '文章', icon: FileText },
  { key: 'user', label: '用户', icon: User },
  { key: 'comment', label: '评论', icon: MessageSquare },
  { key: 'setting', label: '设置', icon: Settings },
  { key: 'file', label: '文件', icon: Upload },
  { key: 'link', label: '友链', icon: Link2 },
  { key: 'page', label: '页面', icon: FileText },
  { key: 'taxonomy', label: '分类标签', icon: Tags },
  { key: 'system', label: '系统', icon: Server },
]

// 时间范围 → from 参数（YYYY-MM-DD，本地时区）
const RANGES: { key: string; label: string }[] = [
  { key: '', label: '全部时间' },
  { key: 'today', label: '今天' },
  { key: '7d', label: '近 7 天' },
  { key: '30d', label: '近 30 天' },
]

const STATUS_OPTIONS: { key: string; label: string }[] = [
  { key: '', label: '全部结果' },
  { key: 'success', label: '仅成功' },
  { key: 'failed', label: '仅失败' },
]

// Select 组件用 { value, label }，这里做一次模块级转换，避免每次渲染新建数组
const RANGE_SELECT_OPTIONS = RANGES.map((o) => ({ value: o.key, label: o.label }))
const STATUS_SELECT_OPTIONS = STATUS_OPTIONS.map((o) => ({ value: o.key, label: o.label }))

function categoryLabel(key: string): string {
  return CATEGORIES.find((c) => c.key === key)?.label ?? key
}

function formatDate(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function rangeToFrom(range: string): string | undefined {
  if (!range) return undefined
  if (range === 'today') return formatDate(new Date())
  const days = range === '7d' ? 7 : 30
  const d = new Date()
  d.setDate(d.getDate() - days)
  return formatDate(d)
}

export default function AdminLogsPage() {
  const notify = useNotify()

  const [category, setCategory] = useState('')
  const [keyword, setKeyword] = useState('')
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState('')
  const [range, setRange] = useState('')
  const [page, setPage] = useState(1)
  const [expandedID, setExpandedID] = useState<number | null>(null)
  const [exporting, setExporting] = useState(false)

  const from = rangeToFrom(range)

  const logsQuery = useQuery({
    queryKey: ['admin', 'logs', category, query, status, range, page],
    queryFn: () =>
      fetchLogs({
        page,
        page_size: PAGE_SIZE,
        category: category || undefined,
        q: query || undefined,
        from,
        success: status ? status === 'success' : undefined,
      }),
    refetchOnWindowFocus: false,
  })

  const overviewQuery = useQuery({
    queryKey: ['admin', 'logs', 'overview'],
    queryFn: fetchLogOverview,
    refetchOnWindowFocus: false,
  })

  const logs = logsQuery.data?.logs ?? []
  const total = logsQuery.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const overview = overviewQuery.data?.overview

  const handleExport = async () => {
    setExporting(true)
    try {
      const blob = await downloadLogs({
        category: category || undefined,
        q: query || undefined,
        from,
        success: status ? status === 'success' : undefined,
      })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `inkstone-logs-${formatDate(new Date())}.csv`
      document.body.appendChild(a)
      a.click()
      a.remove()
      URL.revokeObjectURL(url)
      notify.success('日志已导出为 CSV 文件')
    } catch (e) {
      notify.error(e instanceof Error ? `导出失败：${e.message}` : '导出失败，请重试')
    } finally {
      setExporting(false)
    }
  }

  return (
    <div>
      <AdminPageHeader
        title="网站日志"
        description="记录管理员与用户的关键操作，保留最近 90 天，支持按条件筛选与导出"
        actions={
          <>
            <button
              onClick={handleExport}
              disabled={exporting}
              className="flex items-center gap-1.5 rounded-lg border border-border px-4 py-2 text-sm text-muted-foreground transition-colors hover:text-accent disabled:opacity-50"
            >
              <Download className={`h-4 w-4 ${exporting ? 'animate-pulse' : ''}`} />
              {exporting ? '导出中…' : '导出 CSV'}
            </button>
            <button
              onClick={() => logsQuery.refetch()}
              className="flex items-center gap-1.5 rounded-lg border border-border px-4 py-2 text-sm text-muted-foreground transition-colors hover:text-accent"
            >
              <RefreshCw className={`h-4 w-4 ${logsQuery.isFetching ? 'animate-spin' : ''}`} />
              刷新
            </button>
          </>
        }
      />

      {/* 统计卡片 */}
      <div className="mt-5 grid grid-cols-2 gap-3 sm:grid-cols-4">
        {[
          { label: '日志总数', value: overview?.total, icon: ScrollText },
          { label: '今日新增', value: overview?.today, icon: CalendarDays },
          { label: '失败操作', value: overview?.failed, icon: XCircle },
          {
            label: '当前筛选',
            value: total,
            icon: Search,
          },
        ].map((card) => (
          <div
            key={card.label}
            className="card-flat rounded-xl border border-border bg-card px-4 py-3"
          >
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <card.icon className="h-3.5 w-3.5" />
              {card.label}
            </div>
            <p className="mt-1 text-xl font-bold tabular-nums">
              {card.value === undefined ? (
                <Spinner className="h-5 w-5" />
              ) : (
                card.value
              )}
            </p>
          </div>
        ))}
      </div>

      {/* 分类筛选 + 搜索 + 状态/时间 */}
      <div className="mt-5 space-y-3">
        <div className="flex flex-wrap items-center gap-1.5">
          {CATEGORIES.map((c) => {
            const count = c.key ? overview?.by_category[c.key] : overview?.total
            return (
              <button
                key={c.key}
                onClick={() => {
                  setCategory(c.key)
                  setPage(1)
                }}
                className={`flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                  category === c.key
                    ? 'bg-accent text-white'
                    : 'border border-border text-muted-foreground hover:text-foreground'
                }`}
              >
                <c.icon className="h-3.5 w-3.5" />
                {c.label}
                {count !== undefined && count > 0 && <span className="opacity-70">{count}</span>}
              </button>
            )
          })}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-56 flex-1 sm:max-w-xs">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  setQuery(keyword)
                  setPage(1)
                }
              }}
              placeholder="搜索操作 / 详情 / IP，回车"
              className="w-full rounded-lg border border-border bg-card py-2 pl-9 pr-3 text-sm outline-none transition-all focus:border-accent focus:ring-2 focus:ring-accent/20"
            />
          </div>
          <Select
            value={status}
            onChange={(v) => {
              setStatus(v)
              setPage(1)
            }}
            options={STATUS_SELECT_OPTIONS}
            ariaLabel="操作结果筛选"
            className="min-w-[7.5rem]"
          />
          <Select
            value={range}
            onChange={(v) => {
              setRange(v)
              setPage(1)
            }}
            options={RANGE_SELECT_OPTIONS}
            ariaLabel="时间范围筛选"
            className="min-w-[7.5rem]"
          />
        </div>
      </div>

      {/* 日志列表（紧凑列表样式：每行一条，点击展开完整详情与 UA） */}
      {logsQuery.isLoading ? (
        <div className="mt-5">
          <RowLoading rows={8} />
        </div>
      ) : logs.length === 0 ? (
        <AdminEmpty icon={ScrollText} title="暂无符合条件的日志记录" />
      ) : (
        <div className="card-flat mt-5 overflow-hidden rounded-xl border border-border bg-card">
          {/* 表头（桌面端） */}
          <div className="hidden border-b border-border bg-muted/40 px-4 py-2 text-xs font-medium text-muted-foreground md:grid md:grid-cols-[18px_minmax(0,1fr)_150px_110px_130px] md:gap-3">
            <span />
            <span>操作</span>
            <span>时间</span>
            <span>用户</span>
            <span>IP</span>
          </div>
          <div className="divide-y divide-border/60">
            {logs.map((log: OperationLog) => {
              const expanded = expandedID === log.id
              return (
                <div
                  key={log.id}
                  onClick={() => setExpandedID(expanded ? null : log.id)}
                  className="cursor-pointer px-4 py-2.5 transition-colors hover:bg-muted/40 md:grid md:grid-cols-[18px_minmax(0,1fr)_150px_110px_130px] md:items-center md:gap-3"
                >
                  {/* 状态图标 */}
                  <span className={`hidden md:inline-flex ${log.success ? 'text-emerald-500' : 'text-red-500'}`}>
                    {log.success ? <CheckCircle2 className="h-4 w-4" /> : <XCircle className="h-4 w-4" />}
                  </span>

                  {/* 操作 + 分类 + 失败徽章 + 详情 */}
                  <div className="min-w-0">
                    <div className="flex min-w-0 items-center gap-1.5">
                      <span className={`md:hidden ${log.success ? 'text-emerald-500' : 'text-red-500'}`}>
                        {log.success ? <CheckCircle2 className="h-3.5 w-3.5" /> : <XCircle className="h-3.5 w-3.5" />}
                      </span>
                      <span className="truncate text-sm font-medium">{log.action}</span>
                      <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                        {categoryLabel(log.category)}
                      </span>
                      {!log.success && (
                        <span className="shrink-0 rounded bg-red-500/10 px-1.5 py-0.5 text-[10px] font-medium text-red-500">
                          失败
                        </span>
                      )}
                    </div>
                    {log.detail && (
                      <p
                        className={`mt-0.5 text-xs text-muted-foreground ${
                          expanded ? 'whitespace-pre-wrap break-all' : 'truncate'
                        }`}
                        title={expanded ? undefined : log.detail}
                      >
                        {log.detail}
                      </p>
                    )}
                    {expanded && log.user_agent && (
                      <p className="mt-0.5 break-all text-[10px] text-muted-foreground/60">
                        UA：{log.user_agent}
                      </p>
                    )}
                  </div>

                  {/* 时间 */}
                  <span className="mt-1 block text-xs tabular-nums text-muted-foreground md:mt-0 md:text-[13px]">
                    {new Date(log.created_at).toLocaleString('zh-CN')}
                  </span>

                  {/* 用户 */}
                  <span className="mt-0.5 block truncate text-xs text-muted-foreground md:mt-0 md:text-[13px]">
                    {log.username ? (
                      <>
                        {log.username}
                        <span className="opacity-60"> #{log.user_id}</span>
                      </>
                    ) : (
                      '—'
                    )}
                  </span>

                  {/* IP */}
                  <span className="mt-0.5 block truncate font-mono text-xs text-muted-foreground md:mt-0">
                    {log.ip || '—'}
                  </span>
                </div>
              )
            })}
          </div>
        </div>
      )}

      {/* 分页 */}
      {total > PAGE_SIZE && (
        <div className="mt-5 flex items-center justify-center gap-3 text-sm">
          <button
            onClick={() => setPage((p) => Math.max(1, p - 1))}
            disabled={page <= 1}
            className="rounded-lg border border-border px-3 py-1.5 transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-40"
          >
            上一页
          </button>
          <span className="text-muted-foreground">
            {page} / {totalPages}
          </span>
          <button
            onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
            disabled={page >= totalPages}
            className="rounded-lg border border-border px-3 py-1.5 transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-40"
          >
            下一页
          </button>
        </div>
      )}
    </div>
  )
}
