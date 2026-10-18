'use client'

import Link from 'next/link'
import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Clock, FileText, Lock, PenLine, Pin, RotateCcw, Trash, Trash2 } from 'lucide-react'
import {
  deleteAdminArticle,
  fetchAdminArticles,
  fetchArticleTrash,
  purgeArticle,
  restoreArticle,
  setAdminArticleStatus,
  ApiError,
} from '@/lib/api'
import type { TrashArticle } from '@/lib/api'
import { useNotify } from '@/components/toast'
import { Pagination } from '@/components/pagination'
import { AdminEmpty, AdminPageHeader } from '@/components/admin-page'
import { AdminTabPanel, AdminTabs } from '@/components/admin-tabs'
import { Reveal, hoverTapScale, useReveal } from '@/components/motion'
import { RowLoading } from '@/components/page-loader'
import type { Article } from '@/lib/types'

/**
 * 文章管理（含回收站）。
 *
 * 合并的理由：回收站装的就是 Article——同一张表、同一套删除/还原语义，
 * 此前却占两个侧栏条目。用户在「文章管理」删完文章，下一步必然是去回收站
 * 确认或还原，来回跳页只是多两次导航。
 *
 * 顶部的「全部 / 已发布 / 草稿 / 回收站」四个筛选项统一成一个 pill 组：
 * 前三个是状态筛选，第四个是软删除集合，视觉上同源、心智上也同源。
 */

const filters = [
  { key: 'all', label: '全部' },
  { key: 'published', label: '已发布' },
  { key: 'draft', label: '草稿' },
  { key: 'trash', label: '回收站', icon: Trash2 },
] as const

type FilterKey = (typeof filters)[number]['key']

function statusBadge(status: Article['status']) {
  if (status === 'published') {
    return (
      <span className="rounded-full bg-emerald-500/10 px-2.5 py-0.5 text-xs font-medium text-emerald-600 dark:text-emerald-400">
        已发布
      </span>
    )
  }
  if (status === 'scheduled') {
    // 灰色 badge：scheduled 是「等时间到自动发布」，还没真正上线，
    // 不与草稿的琥珀色混用——两者的区别是「写没写完」而不是「要不要提醒」
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-muted px-2.5 py-0.5 text-xs font-medium text-muted-foreground">
        <Clock className="h-3 w-3" />
        定时发布
      </span>
    )
  }
  return (
    <span className="rounded-full bg-amber-500/10 px-2.5 py-0.5 text-xs font-medium text-amber-600 dark:text-amber-400">
      草稿
    </span>
  )
}

export default function AdminArticlesPage() {
  const [filter, setFilter] = useState<FilterKey>('all')
  // 分页状态。此前 page 写死为 1 且没有翻页 UI，第 51 篇起在后台完全点不到。
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const notify = useNotify()
  const queryClient = useQueryClient()

  const isTrash = filter === 'trash'

  const articlesQuery = useQuery({
    queryKey: ['admin', 'articles', filter, page, pageSize],
    queryFn: () =>
      fetchAdminArticles({
        page,
        page_size: pageSize,
        status: filter === 'all' || filter === 'trash' ? undefined : filter,
      }),
    // 回收站有独立端点，文章列表在回收站 tab 下不必请求
    enabled: !isTrash,
  })

  const trashQuery = useQuery({
    queryKey: ['admin', 'articles', 'trash', page, pageSize],
    queryFn: () => fetchArticleTrash({ page, page_size: pageSize }),
    enabled: isTrash,
    refetchOnWindowFocus: false,
  })

  const data = isTrash ? undefined : articlesQuery.data
  const trash = isTrash ? trashQuery.data : undefined
  const isLoading = isTrash ? trashQuery.isLoading : articlesQuery.isLoading
  const total = isTrash ? (trash?.total ?? 0) : (data?.total ?? 0)

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin', 'articles'] })

  /* 切换筛选一律回到第 1 页：否则可能停在一个已不存在的页码上，
     表现为「列表空了但总数没变」。 */
  const switchFilter = (next: FilterKey) => {
    setFilter(next)
    setPage(1)
  }

  const statusMutation = useMutation({
    mutationFn: ({ id, s }: { id: number; s: 'draft' | 'published' }) => setAdminArticleStatus(id, s),
    onSuccess: (_res, vars) => {
      invalidate()
      notify.success(vars.s === 'published' ? '文章已发布' : '已转为草稿')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '操作失败'),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteAdminArticle(id),
    onSuccess: () => {
      invalidate()
      notify.success('文章已移入回收站')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '删除失败'),
  })

  const restoreMutation = useMutation({
    mutationFn: (id: number) => restoreArticle(id),
    onSuccess: () => {
      invalidate()
      notify.success('文章已还原')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '还原失败'),
  })

  const purgeMutation = useMutation({
    mutationFn: (id: number) => purgeArticle(id),
    onSuccess: () => {
      invalidate()
      notify.success('文章已彻底删除')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '删除失败'),
  })

  const list = data?.articles ?? []
  const trashList = trash?.articles ?? []

  return (
    <div>
      <AdminPageHeader
        title="文章管理"
        description={
          isTrash
            ? '删除的文章会先进入这里，可随时还原。彻底删除会连同该文的评论与点赞一起清除，且不可恢复。'
            : data
              ? `共 ${data.total} 篇`
              : '加载中...'
        }
        actions={
          <div {...hoverTapScale}>
            <Link
              href="/admin/articles/new"
              className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white shadow-md shadow-accent/25"
            >
              <PenLine className="h-4 w-4" />
              写文章
            </Link>
          </div>
        }
      />

      <div className="mt-5">
        <AdminTabs items={filters} value={filter} onChange={(k) => switchFilter(k as FilterKey)} />
      </div>

      <AdminTabPanel value={filter}>
        {isLoading ? (
          <RowLoading rows={4} />
        ) : isTrash ? (
          trashList.length === 0 ? (
            <AdminEmpty
              icon={Trash2}
              title="回收站是空的"
              hint="在文章管理页删除文章后，会先移到这里再等最终处理"
            />
          ) : (
            <div className="card-flat overflow-x-auto rounded-xl border border-border bg-card">
              <table className="w-full min-w-[680px] whitespace-nowrap text-sm">
                <thead>
                  <tr className="border-b border-border text-left text-xs uppercase tracking-wider text-muted-foreground">
                    <th className="px-5 py-3 font-medium">标题</th>
                    <th className="px-5 py-3 font-medium">作者</th>
                    <th className="px-5 py-3 font-medium">删除时状态</th>
                    <th className="px-5 py-3 font-medium">删除时间</th>
                    <th className="px-5 py-3 text-right font-medium">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {trashList.map((a, i) => (
                    <TrashRow
                      key={a.id}
                      a={a}
                      index={i}
                      pending={restoreMutation.isPending || purgeMutation.isPending}
                      onRestore={(id) => restoreMutation.mutate(id)}
                      onPurge={(id) => purgeMutation.mutate(id)}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          )
        ) : list.length === 0 ? (
          <AdminEmpty
            icon={FileText}
            title="没有符合条件的文章"
            hint={
              filter === 'all'
                ? '写下的第一篇文章会出现在这里'
                : '换个筛选条件，或先去写一篇'
            }
          />
        ) : (
          <div className="space-y-2">
            {list.map((a, i) => (
              <Reveal
                key={a.id}
                y={12}
                delay={i * 0.04}
                duration={0.3}
                className="group flex items-center justify-between card-flat rounded-xl border border-border bg-card px-5 py-4 transition-colors hover:border-accent/30"
              >
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2.5">
                    <Link
                      href={`/posts/${a.slug}`}
                      className="truncate font-medium transition-colors hover:text-accent"
                    >
                      {a.title}
                    </Link>
                    {a.is_pinned && (
                      <Pin className="h-3.5 w-3.5 shrink-0 text-accent" aria-label="置顶文章" />
                    )}
                    {a.has_password && (
                      <Lock
                        className="h-3.5 w-3.5 shrink-0 text-muted-foreground"
                        aria-label="已设置访问密码"
                      />
                    )}
                    {statusBadge(a.status)}
                  </div>
                  <p className="mt-1 text-xs text-muted-foreground">
                    作者 {a.author.username} · 更新于{' '}
                    {new Date(a.updated_at).toLocaleString('zh-CN')}
                  </p>
                  {a.status === 'scheduled' && a.scheduled_at && (
                    <p className="mt-1 text-xs text-muted-foreground">
                      将于 {new Date(a.scheduled_at).toLocaleString('zh-CN')} 自动发布
                    </p>
                  )}
                </div>
                <div className="ml-4 flex shrink-0 items-center gap-1 text-xs">
                  <button
                    onClick={() =>
                      statusMutation.mutate({
                        id: a.id,
                        s: a.status === 'published' ? 'draft' : 'published',
                      })
                    }
                    disabled={statusMutation.isPending}
                    className="rounded-md px-3 py-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
                  >
                    {a.status === 'published' ? '转为草稿' : '发布'}
                  </button>
                  <Link
                    href={`/admin/articles/edit/${a.id}`}
                    className="rounded-md px-3 py-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                  >
                    编辑
                  </Link>
                  <button
                    onClick={async () => {
                      const ok = await notify.confirm({
                        title: `删除「${a.title}」？`,
                        message:
                          '文章将移入回收站，可在本页「回收站」筛选里还原；彻底删除会连评论与点赞一起清除，且不可恢复。',
                        confirmText: '移入回收站',
                        danger: true,
                      })
                      if (ok) deleteMutation.mutate(a.id)
                    }}
                    disabled={deleteMutation.isPending}
                    className="rounded-md px-3 py-1.5 text-red-500 transition-colors hover:bg-red-500/10 disabled:opacity-50"
                  >
                    删除
                  </button>
                </div>
              </Reveal>
            ))}
          </div>
        )}
      </AdminTabPanel>

      <Pagination
        page={page}
        pageSize={pageSize}
        total={total}
        onChange={(p, size) => {
          setPage(p)
          setPageSize(size)
        }}
      />
    </div>
  )
}

/** 回收站表格行：独立组件，入场动画只在挂载时播放一次（useReveal 自带 rAF 兜底） */
function TrashRow({
  a,
  index,
  pending,
  onRestore,
  onPurge,
}: {
  a: TrashArticle
  index: number
  pending: boolean
  onRestore: (id: number) => void
  onPurge: (id: number) => void
}) {
  const notify = useNotify()
  const rowRef = useRef<HTMLTableRowElement>(null)
  useReveal(rowRef, { y: 10, duration: 0.35, delay: Math.min(index * 0.04, 0.3) })

  return (
    <tr
      ref={rowRef}
      className="border-b border-border/60 transition-colors last:border-0 hover:bg-muted/50"
    >
      <td className="max-w-[22rem] truncate px-5 py-3.5 font-medium" title={a.title}>
        {a.title}
      </td>
      <td className="px-5 py-3.5 text-muted-foreground">{a.author.username}</td>
      <td className="px-5 py-3.5">{statusBadge(a.status as Article['status'])}</td>
      <td className="px-5 py-3.5 text-xs text-muted-foreground">
        {new Date(a.deleted_at).toLocaleString('zh-CN')}
      </td>
      <td className="px-5 py-3.5 text-right">
        <div className="inline-flex items-center gap-1">
          <button
            onClick={() => onRestore(a.id)}
            disabled={pending}
            className="inline-flex items-center gap-1 rounded-md px-2.5 py-1.5 text-xs text-emerald-600 transition-colors hover:bg-emerald-500/10 disabled:cursor-not-allowed disabled:opacity-30 dark:text-emerald-400"
          >
            <RotateCcw className="h-3.5 w-3.5" />
            还原
          </button>
          <button
            onClick={async () => {
              const ok = await notify.confirm({
                title: `彻底删除「${a.title}」？`,
                message:
                  '该文的评论与点赞会一起被清除，此操作不可恢复。如果只是想暂时下架，请用「还原」后再转为草稿。',
                confirmText: '彻底删除',
                danger: true,
              })
              if (ok) onPurge(a.id)
            }}
            disabled={pending}
            className="inline-flex items-center gap-1 rounded-md px-2.5 py-1.5 text-xs text-red-500 transition-colors hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-30"
          >
            <Trash className="h-3.5 w-3.5" />
            彻底删除
          </button>
        </div>
      </td>
    </tr>
  )
}
