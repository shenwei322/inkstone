'use client'

import Link from 'next/link'
import { useEffect, useRef, useState } from 'react'
import gsap from 'gsap'
import { Reveal, easeOut, hoverTapScale, prefersReducedMotion } from '@/components/motion'
import { RowLoading } from '@/components/page-loader'
import { PenLine, Trash2 } from 'lucide-react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  deleteAdminArticle,
  fetchAdminArticles,
  setAdminArticleStatus,
  ApiError,
} from '@/lib/api'
import { useNotify } from '@/components/toast'
import { Pagination } from '@/components/pagination'
import type { Article } from '@/lib/types'

const tabs = [
  { key: '', label: '全部' },
  { key: 'published', label: '已发布' },
  { key: 'draft', label: '草稿' },
] as const

function statusBadge(status: Article['status']) {
  return status === 'published' ? (
    <span className="rounded-full bg-emerald-500/10 px-2.5 py-0.5 text-xs font-medium text-emerald-600 dark:text-emerald-400">
      已发布
    </span>
  ) : (
    <span className="rounded-full bg-amber-500/10 px-2.5 py-0.5 text-xs font-medium text-amber-600 dark:text-amber-400">
      草稿
    </span>
  )
}

export default function AdminArticlesPage() {
  const [status, setStatus] = useState<'' | 'published' | 'draft'>('')
  // 分页状态。此前 page 写死为 1 且没有翻页 UI，第 51 篇起在后台完全点不到。
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const notify = useNotify()
  const queryClient = useQueryClient()

  // 标签页选中胶囊：切换时以 scaleX 入场（替代 framer layoutId 滑动）
  const pillRef = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    if (pillRef.current && !prefersReducedMotion()) {
      gsap.fromTo(pillRef.current, { scaleX: 0 }, { scaleX: 1, duration: 0.25, ease: easeOut })
    }
  }, [status])

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'articles', status, page, pageSize],
    queryFn: () => fetchAdminArticles({ page, page_size: pageSize, status: status || undefined }),
  })

  // 筛选条件变化必须回到第 1 页：否则可能停在一个已不存在的页码上，
  // 表现为"列表空了但总数没变"。
  const switchStatus = (next: '' | 'published' | 'draft') => {
    setStatus(next)
    setPage(1)
  }

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['admin'] })

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

  return (
    <div>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="text-2xl font-bold tracking-tight">文章管理</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              {data ? `共 ${data.total} 篇` : '加载中...'}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <div {...hoverTapScale}>
              <Link
                href="/admin/articles/new"
                className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white shadow-lg shadow-accent/25"
              >
                <PenLine className="h-4 w-4" />
                写文章
              </Link>
            </div>
            <div className="flex items-center gap-1 rounded-lg border border-border bg-card p-1">
          {tabs.map((t) => (
            <button
              key={t.key}
              onClick={() => switchStatus(t.key)}
              className={`relative rounded-md px-3.5 py-1.5 text-sm transition-colors ${
                status === t.key ? 'text-white' : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {status === t.key && (
                <span
                  key={`article-tab-pill-${status}`}
                  ref={pillRef}
                  className="absolute inset-0 origin-left rounded-md bg-accent"
                />
              )}
              <span className="relative">{t.label}</span>
            </button>
          ))}
            </div>
            <Link
              href="/admin/trash"
              className="inline-flex items-center gap-1.5 rounded-lg border border-border px-3 py-2 text-sm text-muted-foreground transition-colors hover:text-accent"
            >
              <Trash2 className="h-4 w-4" />
              回收站
            </Link>
          </div>
      </div>

      {isLoading ? (
        <div className="mt-6">
          <RowLoading rows={4} />
        </div>
      ) : (
        <div className="mt-6 space-y-2">
          {(data?.articles ?? []).map((a, i) => (
            <Reveal
              key={a.id}
              y={12}
              delay={i * 0.04}
              duration={0.3}
              className="group flex items-center justify-between rounded-2xl border border-border bg-card px-5 py-4 transition-colors hover:border-accent/30"
            >
              <div className="min-w-0">
                <div className="flex items-center gap-2.5">
                  <Link
                    href={`/posts/${a.slug}`}
                    className="truncate font-medium transition-colors hover:text-accent"
                  >
                    {a.title}
                  </Link>
                  {statusBadge(a.status)}
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
                  作者 {a.author.username} · 更新于{' '}
                  {new Date(a.updated_at).toLocaleString('zh-CN')}
                </p>
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
                        '文章将移入回收站，可在「回收站」页还原；彻底删除会连评论与点赞一起清除，且不可恢复。',
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
          {(data?.articles.length ?? 0) === 0 && (
            <Reveal
              y={0}
              className="rounded-xl border border-dashed py-16 text-center text-muted-foreground"
            >
              没有符合条件的文章
            </Reveal>
          )}
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
    </div>
  )
}
