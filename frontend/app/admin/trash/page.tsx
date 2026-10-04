'use client'

import { useRef, useState } from 'react'
import Link from 'next/link'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, RotateCcw, Trash, Trash2 } from 'lucide-react'
import { PageTransition, useReveal } from '@/components/motion'
import { RowLoading } from '@/components/page-loader'
import { fetchArticleTrash, purgeArticle, restoreArticle, ApiError } from '@/lib/api'
import type { TrashArticle } from '@/lib/api'
import { useNotify } from '@/components/toast'
import { Pagination } from '@/components/pagination'

function statusBadge(status: string) {
  if (status === 'published') {
    return (
      <span className="rounded-full bg-emerald-500/10 px-2.5 py-0.5 text-xs font-medium text-emerald-600 dark:text-emerald-400">
        已发布
      </span>
    )
  }
  return (
    <span className="rounded-full bg-amber-500/10 px-2.5 py-0.5 text-xs font-medium text-amber-600 dark:text-amber-400">
      草稿
    </span>
  )
}

export default function AdminTrashPage() {
  const notify = useNotify()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'articles', 'trash', page, pageSize],
    queryFn: () => fetchArticleTrash({ page, page_size: pageSize }),
    refetchOnWindowFocus: false,
  })

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['admin', 'articles'] })
  }

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

  return (
    <PageTransition>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-bold tracking-tight">
            <Trash2 className="h-6 w-6" />
            回收站
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            删除的文章会先进入这里，可随时还原。彻底删除会连同该文的评论与点赞一起清除，且不可恢复。
          </p>
        </div>
        <Link
          href="/admin/articles"
          className="inline-flex items-center gap-1.5 rounded-lg border border-border px-3 py-2 text-sm text-muted-foreground transition-colors hover:text-accent"
        >
          <ArrowLeft className="h-4 w-4" />
          返回文章管理
        </Link>
      </div>

      {isLoading ? (
        <div className="mt-6">
          <RowLoading rows={4} />
        </div>
      ) : (data?.articles ?? []).length === 0 ? (
        <div className="mt-6 rounded-xl border border-dashed p-16 text-center">
          <Trash2 className="mx-auto h-10 w-10 text-muted-foreground/50" />
          <p className="mt-4 text-muted-foreground">回收站是空的</p>
          <p className="mt-1 text-sm text-muted-foreground/70">
            在文章管理页删除文章后，会先移到这里再等最终处理
          </p>
        </div>
      ) : (
        <div className="mt-6 overflow-x-auto rounded-2xl border border-border bg-card shadow-sm">
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
              {(data?.articles ?? []).map((a, i) => (
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
    </PageTransition>
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
      <td className="px-5 py-3.5">{statusBadge(a.status)}</td>
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
