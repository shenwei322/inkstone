'use client'

import { useState } from 'react'
import Link from 'next/link'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Reveal } from '@/components/motion'
import { RowLoading } from '@/components/page-loader'
import { MessageSquare, Check, Trash2, X } from 'lucide-react'
import {
  deleteAdminComment,
  fetchAdminComments,
  setCommentStatus,
  ApiError,
} from '@/lib/api'
import { useNotify } from '@/components/toast'
import { Pagination } from '@/components/pagination'
import type { CommentItem } from '@/lib/types'

export default function AdminCommentsPage() {
  const notify = useNotify()
  const queryClient = useQueryClient()
  // 分页状态。此前 page 写死为 1 且没有翻页 UI，第 51 条评论起在后台完全点不到。
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'comments', page, pageSize],
    queryFn: () => fetchAdminComments({ page, page_size: pageSize }),
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteAdminComment(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin'] })
      notify.success('评论已删除')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '删除失败'),
  })

  // 审核：游客评论默认进待审队列，没有这个入口就无法放行
  const auditMutation = useMutation({
    mutationFn: ({ id, status }: { id: number; status: 'approved' | 'rejected' }) =>
      setCommentStatus(id, status),
    onSuccess: (_res, vars) => {
      queryClient.invalidateQueries({ queryKey: ['admin'] })
      // 角标数也要跟着变，否则侧栏一直显示"有 N 条待审"
      queryClient.invalidateQueries({ queryKey: ['pending-comments'] })
      notify.success(vars.status === 'approved' ? '已通过，评论现已公开' : '已驳回')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '审核失败'),
  })

  return (
    <div>
      <div>
        <h1 className="text-2xl font-bold tracking-tight">评论管理</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          {data ? `共 ${data.total} 条评论` : '加载中...'}
        </p>
      </div>

      {isLoading ? (
        <div className="mt-6">
          <RowLoading rows={4} />
        </div>
      ) : (data?.comments ?? []).length === 0 ? (
        <div className="mt-6 rounded-xl border border-dashed p-16 text-center">
          <MessageSquare className="mx-auto h-10 w-10 text-muted-foreground/50" />
          <p className="mt-4 text-muted-foreground">还没有任何评论</p>
        </div>
      ) : (
        <div className="mt-6 space-y-3">
          {(data?.comments ?? []).map((comment: CommentItem, i: number) => (
            <Reveal
              key={comment.id}
              y={12}
              delay={i * 0.04}
              duration={0.3}
              className="group flex items-start gap-3 rounded-2xl border border-border bg-card p-4 transition-colors hover:border-accent/30"
            >
              <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-accent/10 text-sm font-bold text-accent">
                {comment.author.username.charAt(0).toUpperCase()}
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <span className="font-medium">{comment.author.username}</span>
                  {/* 游客标记：昵称可能是「小明」，光看名字分不出是不是注册用户 */}
                  {comment.author.is_guest && (
                    <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                      访客
                    </span>
                  )}
                  {comment.status === 'pending' && (
                    <span className="rounded bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-medium text-amber-600 dark:text-amber-400">
                      待审核
                    </span>
                  )}
                  {comment.status === 'rejected' && (
                    <span className="rounded bg-red-500/10 px-1.5 py-0.5 text-[10px] font-medium text-red-500">
                      已驳回
                    </span>
                  )}
                  <span className="text-xs text-muted-foreground">
                    {new Date(comment.created_at).toLocaleString('zh-CN')}
                  </span>
                </div>
                {/* 游客邮箱与 IP 只在后台下发：审核时可能要联系本人或判断同源刷评 */}
                {comment.author.is_guest && (comment.guest_email || comment.ip) && (
                  <p className="mt-1 text-xs text-muted-foreground">
                    {comment.guest_email && <span>{comment.guest_email}</span>}
                    {comment.guest_email && comment.ip && <span className="mx-1.5">·</span>}
                    {comment.ip && <span>IP {comment.ip}</span>}
                  </p>
                )}
                <p className="mt-1.5 whitespace-pre-wrap break-words text-sm leading-relaxed">
                  {comment.content}
                </p>
                {comment.article_title && comment.article_slug && (
                  <p className="mt-1.5 text-xs text-muted-foreground">
                    评论于：
                    <Link
                      href={`/posts/${comment.article_slug}`}
                      className="text-foreground hover:text-accent transition-colors"
                    >
                      {comment.article_title}
                    </Link>
                  </p>
                )}
              </div>
              <div className="flex shrink-0 items-center gap-1">
                {/* 审核按钮只对待审评论显示：已通过的评论不需要"再次通过"，
                    一直在那儿反而让人以为状态没保存 */}
                {comment.status === 'pending' && (
                  <>
                    <button
                      onClick={() => auditMutation.mutate({ id: comment.id, status: 'approved' })}
                      disabled={auditMutation.isPending}
                      className="rounded-md p-2 text-muted-foreground transition-colors hover:bg-emerald-500/10 hover:text-emerald-600 disabled:opacity-50"
                      title="通过审核"
                      aria-label="通过审核"
                    >
                      <Check className="h-4 w-4" />
                    </button>
                    <button
                      onClick={() => auditMutation.mutate({ id: comment.id, status: 'rejected' })}
                      disabled={auditMutation.isPending}
                      className="rounded-md p-2 text-muted-foreground transition-colors hover:bg-amber-500/10 hover:text-amber-600 disabled:opacity-50"
                      title="驳回"
                      aria-label="驳回评论"
                    >
                      <X className="h-4 w-4" />
                    </button>
                  </>
                )}
                <button
                  onClick={async () => {
                    const ok = await notify.confirm({
                      title: '删除这条评论？',
                      message: '删除后无法恢复。',
                      confirmText: '删除',
                      danger: true,
                    })
                    if (ok) deleteMutation.mutate(comment.id)
                  }}
                  disabled={deleteMutation.isPending}
                  className="rounded-md p-2 text-muted-foreground transition-colors hover:bg-red-500/10 hover:text-red-500 disabled:opacity-50"
                  title="删除"
                  aria-label={comment.article_title ? `删除《${comment.article_title}》下的评论` : '删除评论'}
                >
                  <Trash2 className="h-4 w-4" />
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
    </div>
  )
}
