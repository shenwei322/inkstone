'use client'

import { Fragment, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Activity,
  Ban,
  ChevronDown,
  ChevronRight,
  Globe2,
  RefreshCw,
  Search,
  ShieldOff,
  Timer,
  Unlock,
  Users,
} from 'lucide-react'
import { AdminEmpty, AdminPageHeader } from '@/components/admin-page'
import { useNotify } from '@/components/toast'
import {
  createIPBan,
  deleteIPBan,
  fetchIPAccess,
  fetchIPBans,
  fetchIPDetail,
  fetchIPOverview,
  fetchIPTop,
  type IPAccessEntry,
  type IPBanEntry,
} from '@/lib/api'

// =====================================================================
// IP 管理（/admin/ip-access）
//
// 三段信息按「先看全局 → 再看是谁 → 最后动手」排列：
//   1. 概览卡片：窗口、生效封禁数、当前窗口最忙的 IP
//   2. 高频 IP 表：按请求量排序，可展开看这个 IP 打了哪些路径
//   3. 封禁记录 + 访问明细：前者用于管理，后者用于取证
//
// 「当前筛选时段」用 minutes 表达而不是日期选择器：排查 CC 时关心的永远是
// 「最近 5 分钟 / 1 小时」，让运维手填日期反而更慢。
// =====================================================================

const WINDOW_OPTIONS = [
  { label: '近 5 分钟', value: 5 },
  { label: '近 1 小时', value: 60 },
  { label: '近 24 小时', value: 60 * 24 },
  { label: '近 7 天', value: 60 * 24 * 7 },
]

const PAGE_SIZE = 20

const inputClass =
  'w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none transition-colors focus:border-accent'

const btnSecondary =
  'flex items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50'

/** 剩余时间的人类可读形式。永久封禁由后端给 -1，这里统一显示「永久」。 */
function formatRemaining(seconds: number) {
  if (seconds < 0) return '永久'
  if (seconds === 0) return '已过期'
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d} 天 ${h} 小时`
  if (h > 0) return `${h} 小时 ${m} 分钟`
  if (m > 0) return `${m} 分钟`
  return '不到 1 分钟'
}

function formatTime(raw: string | null | undefined) {
  if (!raw) return '-'
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return raw
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

/** 状态码着色：4xx 橙、5xx 红、2xx 绿，扫描行为一眼可辨。 */
function statusClass(status: number) {
  if (status >= 500) return 'text-red-500'
  if (status >= 400) return 'text-amber-500'
  if (status >= 300) return 'text-sky-500'
  return 'text-emerald-600 dark:text-emerald-400'
}

export default function AdminIPAccessPage() {
  const notify = useNotify()

  const [windowMinutes, setWindowMinutes] = useState(5)
  const [keyword, setKeyword] = useState('')
  const [query, setQuery] = useState('')
  const [expanded, setExpanded] = useState<string | null>(null)
  const [accessPage, setAccessPage] = useState(1)
  const [banPage, setBanPage] = useState(1)
  const [busy, setBusy] = useState<string | null>(null)

  const [manualIP, setManualIP] = useState('')
  const [manualMinutes, setManualMinutes] = useState('')
  const [manualReason, setManualReason] = useState('')

  const overviewQuery = useQuery({
    queryKey: ['admin', 'ip', 'overview', windowMinutes],
    queryFn: () => fetchIPOverview(windowMinutes),
    refetchOnWindowFocus: false,
  })

  const topQuery = useQuery({
    queryKey: ['admin', 'ip', 'top', windowMinutes],
    queryFn: () => fetchIPTop({ window: windowMinutes, limit: 20 }),
    refetchOnWindowFocus: false,
  })

  const accessQuery = useQuery({
    queryKey: ['admin', 'ip', 'access', query, windowMinutes, accessPage],
    queryFn: () =>
      fetchIPAccess({
        // 后端明细接口按 from/to 过滤；这里用「近 N 分钟」换算成 from，
        // 让明细与上面的排行口径保持一致（同一时段）。
        ip: query || undefined,
        page: accessPage,
        page_size: PAGE_SIZE,
        from: new Date(Date.now() - windowMinutes * 60_000).toISOString(),
      }),
    refetchOnWindowFocus: false,
  })

  const bansQuery = useQuery({
    queryKey: ['admin', 'ip', 'bans', banPage],
    queryFn: () => fetchIPBans({ page: banPage, page_size: PAGE_SIZE }),
    refetchOnWindowFocus: false,
  })

  const detailQuery = useQuery({
    queryKey: ['admin', 'ip', 'detail', expanded, windowMinutes],
    queryFn: () => fetchIPDetail(expanded as string, windowMinutes),
    enabled: Boolean(expanded),
    refetchOnWindowFocus: false,
  })

  const overview = overviewQuery.data?.overview
  const topIPs = topQuery.data?.top ?? []
  const access = accessQuery.data?.access ?? []
  const accessTotal = accessQuery.data?.total ?? 0
  // bans 在多个 useMemo 里当依赖用，必须自己也是稳定引用，
  // 否则 `?? []` 每次渲染都新建数组，下方的 useMemo 等于白做。
  const bans = useMemo(() => bansQuery.data?.bans ?? [], [bansQuery.data])
  const bansTotal = bansQuery.data?.total ?? 0

  const activeBans = useMemo(() => bans.filter((b) => b.active).length, [bans])

  const refreshAll = () => {
    void overviewQuery.refetch()
    void topQuery.refetch()
    void accessQuery.refetch()
    void bansQuery.refetch()
    if (expanded) void detailQuery.refetch()
  }

  const doBan = async (ip: string, minutes: number | undefined, reason: string) => {
    setBusy(ip)
    try {
      const res = await createIPBan({ ip, minutes, reason: reason || undefined })
      notify.success(res.message || `已封禁 ${ip}`)
      refreshAll()
    } catch (e) {
      notify.error(e instanceof Error ? e.message : '封禁失败，请重试')
    } finally {
      setBusy(null)
    }
  }

  const doUnban = async (ip: string) => {
    setBusy(ip)
    try {
      const res = await deleteIPBan(ip)
      notify.success(res.message || `已解除 ${ip} 的封禁`)
      refreshAll()
    } catch (e) {
      notify.error(e instanceof Error ? e.message : '解封失败，请重试')
    } finally {
      setBusy(null)
    }
  }

  const submitManual = () => {
    const ip = manualIP.trim()
    if (!ip) {
      notify.error('请填写要封禁的 IP')
      return
    }
    // 空字符串 → undefined → 后端取默认时长；"0" → 永久封禁；其它按数字解析
    const trimmed = manualMinutes.trim()
    const minutes = trimmed === '' ? undefined : Math.max(0, Number(trimmed) || 0)
    void doBan(ip, minutes, manualReason.trim())
    setManualIP('')
    setManualReason('')
  }

  const banByIP = useMemo(() => {
    const map = new Map<string, IPBanEntry>()
    for (const b of bans) {
      if (b.active) map.set(b.ip, b)
    }
    return map
  }, [bans])

  const accessTotalPages = Math.max(1, Math.ceil(accessTotal / PAGE_SIZE))
  const bansTotalPages = Math.max(1, Math.ceil(bansTotal / PAGE_SIZE))

  return (
    <div>
      <AdminPageHeader
        title="IP 管理"
        icon={Globe2}
        description={
          <>
            记录访客 IP 与访问路径，支持按频率自动封禁与手动封禁。明细默认保留{' '}
            {overview?.retention_days ?? 7} 天，可在「安全防护」页调整阈值。
          </>
        }
        actions={
          <>
            <button onClick={refreshAll} className={btnSecondary}>
              <RefreshCw
                className={`h-4 w-4 ${overviewQuery.isFetching || topQuery.isFetching ? 'animate-spin' : ''}`}
              />
              刷新
            </button>
          </>
        }
      />

      {/* 概览卡片 */}
      <div className="mt-5 grid grid-cols-2 gap-3 sm:grid-cols-4">
        {[
          {
            label: '窗口请求数',
            value: overview?.window_requests ?? 0,
            sub: `近 ${overview?.window_minutes ?? windowMinutes} 分钟 · ${overview?.window_ips ?? 0} 个 IP`,
            icon: Activity,
          },
          {
            label: '最忙的 IP',
            value: overview?.busiest_ip || '—',
            sub: overview?.busiest_ip ? `${overview.busiest_hits} 次请求` : '暂无记录',
            icon: Users,
            mono: true,
          },
          {
            label: '生效封禁',
            value: overview?.active_bans ?? 0,
            sub: `含自动与手动，本页 ${activeBans} 条生效`,
            icon: Ban,
          },
          {
            label: '丢弃记录',
            value: overview?.dropped_records ?? 0,
            sub: '队列满时的丢弃数，正常为 0',
            icon: Timer,
          },
        ].map((card) => (
          <div key={card.label} className="card-flat rounded-xl border border-border bg-card p-4">
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <card.icon className="h-3.5 w-3.5" />
              {card.label}
            </div>
            <div
              className={`mt-2 truncate text-xl font-semibold ${card.mono ? 'font-mono text-base' : ''}`}
              title={String(card.value)}
            >
              {card.value}
            </div>
            <div className="mt-0.5 truncate text-xs text-muted-foreground/80">{card.sub}</div>
          </div>
        ))}
      </div>

      {/* 时段 + 搜索 */}
      <div className="mt-5 flex flex-wrap items-center gap-2">
        <div className="flex flex-wrap items-center gap-1.5">
          {WINDOW_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              onClick={() => {
                setWindowMinutes(opt.value)
                setAccessPage(1)
              }}
              className={`rounded-lg border px-3 py-1.5 text-sm transition-colors ${
                windowMinutes === opt.value
                  ? 'border-accent bg-accent/10 text-accent'
                  : 'border-border text-muted-foreground hover:border-accent/40 hover:text-accent'
              }`}
            >
              {opt.label}
            </button>
          ))}
        </div>
        <div className="relative ml-auto w-full sm:w-64">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground/60" />
          <input
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                setQuery(keyword.trim())
                setAccessPage(1)
              }
            }}
            placeholder="按 IP 精确筛选，回车确认"
            className={`${inputClass} pl-8`}
          />
        </div>
      </div>

      {/* 手动封禁 */}
      <section className="card-flat mt-5 rounded-xl border border-border bg-card p-4">
        <h2 className="flex items-center gap-1.5 text-sm font-semibold">
          <ShieldOff className="h-4 w-4 text-accent" />
          手动封禁
        </h2>
        <div className="mt-3 grid gap-2 sm:grid-cols-[minmax(0,1fr)_140px_minmax(0,1.4fr)_auto]">
          <input
            value={manualIP}
            onChange={(e) => setManualIP(e.target.value)}
            placeholder="IP 地址，如 203.0.113.9"
            className={`${inputClass} font-mono`}
          />
          <input
            value={manualMinutes}
            onChange={(e) => setManualMinutes(e.target.value)}
            placeholder="时长（分钟）"
            inputMode="numeric"
            className={inputClass}
            title="留空 = 使用后台默认时长；填 0 = 永久封禁"
          />
          <input
            value={manualReason}
            onChange={(e) => setManualReason(e.target.value)}
            placeholder="封禁原因（可选，会显示在封禁列表）"
            className={inputClass}
          />
          <button
            onClick={submitManual}
            disabled={busy !== null}
            className="flex items-center justify-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition-opacity hover:opacity-90 disabled:opacity-50"
          >
            <Ban className="h-4 w-4" />
            封禁
          </button>
        </div>
        <p className="mt-2 text-xs text-muted-foreground/80">
          时长留空使用「安全防护」页的默认值，填 0 表示永久封禁。白名单内的地址无法封禁。
        </p>
      </section>

      {/* 高频 IP */}
      <section className="mt-6">
        <h2 className="flex items-center gap-1.5 text-sm font-semibold">
          <Users className="h-4 w-4 text-accent" />
          高频 IP
          <span className="text-xs font-normal text-muted-foreground">
            （近 {windowMinutes} 分钟，按请求数排序，点击行展开路径分布）
          </span>
        </h2>

        {topQuery.isLoading ? (
          <div className="card-flat mt-3 rounded-xl border border-border px-4 py-10 text-center text-sm text-muted-foreground">
            加载中…
          </div>
        ) : topIPs.length === 0 ? (
          <AdminEmpty
            icon={Globe2}
            title="该时段还没有访问记录"
            hint="访问明细按「IP + 路径 + 分钟」合并写入，刚启动或流量很小时可能是空的"
          />
        ) : (
          <div className="card-flat mt-3 overflow-hidden rounded-xl border border-border bg-card">
            <table className="w-full table-fixed text-sm">
              <thead>
                <tr className="border-b border-border text-left text-xs text-muted-foreground">
                  <th className="w-8 px-2 py-2" />
                  <th className="px-3 py-2 font-medium">IP</th>
                  <th className="w-24 px-3 py-2 font-medium">请求数</th>
                  <th className="w-24 px-3 py-2 font-medium">路径数</th>
                  <th className="hidden px-3 py-2 font-medium sm:table-cell">最后访问</th>
                  <th className="w-32 px-3 py-2 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {topIPs.map((row) => {
                  const isOpen = expanded === row.ip
                  const ban = banByIP.get(row.ip)
                  const detail = isOpen ? detailQuery.data : undefined
                  return (
                    // Fragment 必须显式带 key：<>…</> 简写无法挂 key，
                    // 循环里会触发 React 的 missing key 警告。
                    <Fragment key={row.ip}>
                      <tr
                        onClick={() => setExpanded(isOpen ? null : row.ip)}
                        className="cursor-pointer border-b border-border/60 transition-colors last:border-0 hover:bg-accent/5"
                      >
                        <td className="px-2 py-2 text-muted-foreground">
                          {isOpen ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}
                        </td>
                        <td className="truncate px-3 py-2 font-mono text-xs" title={row.ip}>
                          {row.ip}
                          {row.banned ? (
                            <span className="ml-1.5 rounded bg-red-500/15 px-1.5 py-0.5 text-[10px] text-red-600 dark:text-red-400">
                              已封禁
                            </span>
                          ) : null}
                        </td>
                        <td className="px-3 py-2 tabular-nums">{row.requests}</td>
                        <td className="px-3 py-2 tabular-nums text-muted-foreground">{row.paths}</td>
                        <td className="hidden truncate px-3 py-2 text-xs text-muted-foreground sm:table-cell">
                          {formatTime(row.last_seen)}
                        </td>
                        <td className="px-3 py-2 text-right">
                          {row.banned ? (
                            <button
                              onClick={(e) => {
                                e.stopPropagation()
                                void doUnban(row.ip)
                              }}
                              disabled={busy === row.ip}
                              className={`${btnSecondary} ml-auto`}
                            >
                              <Unlock className="h-3.5 w-3.5" />
                              解封
                            </button>
                          ) : (
                            <button
                              onClick={(e) => {
                                e.stopPropagation()
                                void doBan(row.ip, undefined, '后台手动封禁（高频 IP 列表）')
                              }}
                              disabled={busy === row.ip}
                              className={`${btnSecondary} ml-auto`}
                            >
                              <Ban className="h-3.5 w-3.5" />
                              封禁
                            </button>
                          )}
                        </td>
                      </tr>
                      {isOpen ? (
                        <tr className="border-b border-border/60 bg-muted/20">
                          <td />
                          <td colSpan={5} className="px-3 py-3">
                            {detailQuery.isLoading ? (
                              <div className="text-xs text-muted-foreground">加载路径分布…</div>
                            ) : detail && detail.paths.length > 0 ? (
                              <div className="space-y-1">
                                <div className="mb-1.5 flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                                  <span>UA：{row.user_agent || '（空）'}</span>
                                  {ban ? (
                                    <span className="text-red-600 dark:text-red-400">
                                      封禁中 · {formatRemaining(ban.remaining_seconds)} · {ban.reason}
                                    </span>
                                  ) : null}
                                </div>
                                <div className="max-h-64 overflow-y-auto">
                                  {detail.paths.map((p) => (
                                    <div
                                      key={p.path}
                                      className="flex items-center gap-3 border-b border-border/40 py-1 text-xs last:border-0"
                                    >
                                      <span className={`w-9 shrink-0 tabular-nums ${statusClass(p.status)}`}>
                                        {p.status}
                                      </span>
                                      <span className="min-w-0 flex-1 truncate font-mono" title={p.path}>
                                        {p.path}
                                      </span>
                                      <span className="w-16 shrink-0 text-right tabular-nums text-muted-foreground">
                                        {p.requests} 次
                                      </span>
                                    </div>
                                  ))}
                                </div>
                              </div>
                            ) : (
                              <div className="text-xs text-muted-foreground">该时段内没有路径记录</div>
                            )}
                          </td>
                        </tr>
                      ) : null}
                    </Fragment>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* 封禁记录 */}
      <section className="mt-6">
        <h2 className="flex items-center gap-1.5 text-sm font-semibold">
          <Ban className="h-4 w-4 text-accent" />
          封禁记录
          <span className="text-xs font-normal text-muted-foreground">（含已过期，共 {bansTotal} 条）</span>
        </h2>

        {bansQuery.isLoading ? (
          <div className="card-flat mt-3 rounded-xl border border-border px-4 py-10 text-center text-sm text-muted-foreground">
            加载中…
          </div>
        ) : bans.length === 0 ? (
          <AdminEmpty
            icon={Ban}
            title="还没有封禁记录"
            hint="自动封禁需要先在「安全防护」页开启，或在上方手动封禁"
          />
        ) : (
          <>
            <div className="card-flat mt-3 overflow-hidden rounded-xl border border-border bg-card">
              <table className="w-full table-fixed text-sm">
                <thead>
                  <tr className="border-b border-border text-left text-xs text-muted-foreground">
                    <th className="w-36 px-3 py-2 font-medium">IP</th>
                    <th className="w-20 px-3 py-2 font-medium">来源</th>
                    <th className="px-3 py-2 font-medium">原因</th>
                    <th className="w-32 px-3 py-2 font-medium">剩余</th>
                    <th className="hidden w-28 px-3 py-2 font-medium md:table-cell">操作人</th>
                    <th className="w-24 px-3 py-2 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {bans.map((b) => (
                    <tr
                      key={b.id}
                      className="border-b border-border/60 text-sm last:border-0 hover:bg-accent/5"
                    >
                      <td className="truncate px-3 py-2 font-mono text-xs" title={b.ip}>
                        {b.ip}
                      </td>
                      <td className="px-3 py-2">
                        <span
                          className={`rounded px-1.5 py-0.5 text-[11px] ${
                            b.source === 'auto'
                              ? 'bg-amber-500/15 text-amber-600 dark:text-amber-400'
                              : 'bg-accent/15 text-accent'
                          }`}
                        >
                          {b.source === 'auto' ? '自动' : '手动'}
                        </span>
                      </td>
                      <td className="truncate px-3 py-2 text-xs text-muted-foreground" title={b.reason}>
                        {b.reason || '-'}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-xs">
                        {b.active ? (
                          <span className="text-red-600 dark:text-red-400">
                            {formatRemaining(b.remaining_seconds)}
                          </span>
                        ) : (
                          <span className="text-muted-foreground">已过期</span>
                        )}
                      </td>
                      <td className="hidden truncate px-3 py-2 text-xs text-muted-foreground md:table-cell">
                        {b.created_by || '-'}
                      </td>
                      <td className="px-3 py-2 text-right">
                        <button
                          onClick={() => void doUnban(b.ip)}
                          disabled={busy === b.ip || !b.active}
                          className={`${btnSecondary} ml-auto whitespace-nowrap`}
                        >
                          <Unlock className="h-3.5 w-3.5" />
                          解封
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {bansTotalPages > 1 ? (
              <div className="mt-3 flex items-center justify-center gap-3 text-sm">
                <button
                  onClick={() => setBanPage((p) => Math.max(1, p - 1))}
                  disabled={banPage <= 1}
                  className={btnSecondary}
                >
                  上一页
                </button>
                <span className="text-muted-foreground">
                  {banPage} / {bansTotalPages}
                </span>
                <button
                  onClick={() => setBanPage((p) => Math.min(bansTotalPages, p + 1))}
                  disabled={banPage >= bansTotalPages}
                  className={btnSecondary}
                >
                  下一页
                </button>
              </div>
            ) : null}
          </>
        )}
      </section>

      {/* 访问明细 */}
      <section className="mt-6">
        <h2 className="flex items-center gap-1.5 text-sm font-semibold">
          <Activity className="h-4 w-4 text-accent" />
          访问明细
          <span className="text-xs font-normal text-muted-foreground">
            （同一 IP 对同一路径每分钟合并为一行，累计次数见「次数」列）
          </span>
        </h2>

        {accessQuery.isLoading ? (
          <div className="card-flat mt-3 rounded-xl border border-border px-4 py-10 text-center text-sm text-muted-foreground">
            加载中…
          </div>
        ) : access.length === 0 ? (
          <AdminEmpty
            icon={Activity}
            title="该时段没有访问明细"
            hint={query ? `当前按 IP「${query}」筛选，可清空搜索框后回车` : '换个时段试试，或确认 IP 访问记录已在安全防护页开启'}
          />
        ) : (
          <>
            <div className="card-flat mt-3 overflow-x-auto rounded-xl border border-border bg-card">
              <table className="w-full min-w-[760px] table-fixed text-sm">
                <thead>
                  <tr className="border-b border-border text-left text-xs text-muted-foreground">
                    <th className="w-40 px-3 py-2 font-medium">时间</th>
                    <th className="w-36 px-3 py-2 font-medium">IP</th>
                    <th className="w-14 px-3 py-2 font-medium">方法</th>
                    <th className="px-3 py-2 font-medium">路径</th>
                    <th className="w-16 px-3 py-2 font-medium">状态</th>
                    <th className="w-16 px-3 py-2 font-medium">次数</th>
                    <th className="w-16 px-3 py-2 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {access.map((row: IPAccessEntry) => (
                    <tr
                      key={row.id}
                      className="border-b border-border/60 text-xs last:border-0 hover:bg-accent/5"
                    >
                      <td className="whitespace-nowrap px-3 py-1.5 text-muted-foreground">
                        {formatTime(row.bucket_at)}
                      </td>
                      <td className="truncate px-3 py-1.5 font-mono" title={row.ip}>
                        {row.ip}
                      </td>
                      <td className="px-3 py-1.5 text-muted-foreground">{row.method}</td>
                      <td className="truncate px-3 py-1.5 font-mono" title={row.path}>
                        {row.path}
                      </td>
                      <td className={`px-3 py-1.5 tabular-nums ${statusClass(row.status)}`}>{row.status}</td>
                      <td className="px-3 py-1.5 tabular-nums">{row.hits}</td>
                      <td className="px-3 py-1.5 text-right">
                        {banByIP.has(row.ip) ? (
                          <button
                            onClick={() => void doUnban(row.ip)}
                            disabled={busy === row.ip}
                            className="text-accent transition-opacity hover:underline disabled:opacity-50"
                          >
                            解封
                          </button>
                        ) : (
                          <button
                            onClick={() => void doBan(row.ip, undefined, '后台手动封禁（访问明细）')}
                            disabled={busy === row.ip}
                            className="text-muted-foreground transition-colors hover:text-red-500 disabled:opacity-50"
                          >
                            封禁
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {accessTotalPages > 1 ? (
              <div className="mt-3 flex items-center justify-center gap-3 text-sm">
                <button
                  onClick={() => setAccessPage((p) => Math.max(1, p - 1))}
                  disabled={accessPage <= 1}
                  className={btnSecondary}
                >
                  上一页
                </button>
                <span className="text-muted-foreground">
                  {accessPage} / {accessTotalPages}（共 {accessTotal} 条）
                </span>
                <button
                  onClick={() => setAccessPage((p) => Math.min(accessTotalPages, p + 1))}
                  disabled={accessPage >= accessTotalPages}
                  className={btnSecondary}
                >
                  下一页
                </button>
              </div>
            ) : null}
          </>
        )}
      </section>
    </div>
  )
}
