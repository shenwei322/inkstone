'use client'

import { useRef, useState } from 'react'
import Link from 'next/link'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Database, Download, HardDrive, Loader2, Trash2 } from 'lucide-react'
import { PageTransition, useReveal } from '@/components/motion'
import { RowLoading } from '@/components/page-loader'
import { createBackup, deleteBackup, downloadBackup, fetchBackups, ApiError } from '@/lib/api'
import type { BackupFile } from '@/lib/api'
import { useNotify } from '@/components/toast'
import { formatSize } from '@/lib/ui'

/** 备份原因 → 中文说明。后端目前只写 manual / update 两种值。 */
function reasonLabel(reason: string): string {
  switch (reason) {
    case 'manual':
      return '手动备份'
    case 'update':
      return '更新前自动'
    default:
      return reason || '—'
  }
}

export default function AdminBackupsPage() {
  const notify = useNotify()
  const queryClient = useQueryClient()
  const [downloading, setDownloading] = useState<string | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'system', 'backups'],
    queryFn: fetchBackups,
    refetchOnWindowFocus: false,
  })

  const createMutation = useMutation({
    mutationFn: () => createBackup('manual'),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'system', 'backups'] })
      notify.success('备份已生成')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '备份失败'),
  })

  const deleteMutation = useMutation({
    mutationFn: (name: string) => deleteBackup(name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin', 'system', 'backups'] })
      notify.success('备份已删除')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '删除失败'),
  })

  const handleDownload = async (name: string) => {
    setDownloading(name)
    try {
      // downloadBackup 内部自己完成 blob 下载与 <a download> 点击，
      // 不改它为普通 api() 调用——api() 只 res.json()，拿到二进制会失败。
      await downloadBackup(name)
    } catch {
      notify.error('下载备份失败，请重试')
    } finally {
      setDownloading(null)
    }
  }

  const backups = data?.backups ?? []

  return (
    <PageTransition>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-bold tracking-tight">
            <Database className="h-6 w-6" />
            备份与恢复
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            把站点内容导出为快照文件，供人工下载与长期留存
          </p>
        </div>
        <button
          onClick={() => createMutation.mutate()}
          disabled={createMutation.isPending}
          className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
        >
          {createMutation.isPending ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <Database className="h-4 w-4" />
          )}
          {createMutation.isPending ? '备份中…' : '立即备份'}
        </button>
      </div>

      {/* 汇总 */}
      <div className="mt-5 grid grid-cols-2 gap-3 sm:grid-cols-3">
        <div className="rounded-2xl border border-border bg-card px-4 py-3 shadow-sm">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Database className="h-3.5 w-3.5" />
            备份份数
          </div>
          <p className="mt-1 text-xl font-bold tabular-nums">
            {data ? data.total : '—'}
            <span className="ml-1 text-sm font-normal text-muted-foreground">份</span>
          </p>
        </div>
        <div className="rounded-2xl border border-border bg-card px-4 py-3 shadow-sm">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <HardDrive className="h-3.5 w-3.5" />
            总占用空间
          </div>
          <p className="mt-1 text-xl font-bold tabular-nums">
            {data ? formatSize(data.total_size) : '—'}
          </p>
        </div>
        <div className="rounded-2xl border border-border bg-card px-4 py-3 shadow-sm">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <Database className="h-3.5 w-3.5" />
            单份体积
          </div>
          <p className="mt-1 text-xl font-bold tabular-nums">
            {data && data.total > 0 ? formatSize(Math.round(data.total_size / data.total)) : '—'}
          </p>
        </div>
      </div>

      {/* 备份列表 */}
      {isLoading ? (
        <div className="mt-6">
          <RowLoading rows={3} />
        </div>
      ) : backups.length === 0 ? (
        <div className="mt-6 rounded-xl border border-dashed p-16 text-center">
          <Database className="mx-auto h-10 w-10 text-muted-foreground/50" />
          <p className="mt-4 text-muted-foreground">还没有任何备份</p>
          <p className="mt-1 text-sm text-muted-foreground/70">
            点击右上角「立即备份」生成第一份快照
          </p>
        </div>
      ) : (
        <div className="mt-6 overflow-x-auto rounded-2xl border border-border bg-card shadow-sm">
          <table className="w-full min-w-[620px] whitespace-nowrap text-sm">
            <thead>
              <tr className="border-b border-border text-left text-xs uppercase tracking-wider text-muted-foreground">
                <th className="px-5 py-3 font-medium">文件名</th>
                <th className="px-5 py-3 font-medium">大小</th>
                <th className="px-5 py-3 font-medium">创建时间</th>
                <th className="px-5 py-3 font-medium">来源</th>
                <th className="px-5 py-3 text-right font-medium">操作</th>
              </tr>
            </thead>
            <tbody>
              {backups.map((b, i) => (
                <BackupRow
                  key={b.name}
                  b={b}
                  index={i}
                  pending={deleteMutation.isPending || downloading !== null}
                  downloading={downloading === b.name}
                  onDownload={(name) => handleDownload(name)}
                  onDelete={(name) => deleteMutation.mutate(name)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* 恢复流程说明：刻意不做成一键还原 */}
      <div className="mt-6 rounded-2xl border border-border bg-card p-5 shadow-sm">
        <h2 className="text-sm font-semibold">如何恢复备份</h2>
        <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
          后台<strong className="text-foreground">不提供一键还原</strong>，这是有意的设计：导出的快照是
          JSON 格式的内容导出，并不是 <code className="rounded bg-muted px-1 py-0.5 text-[13px]">pg_restore</code>
          {' '}能直接消费的转储文件。把它直接灌回生产库会绕开本系统的入库校验与消毒逻辑，风险高于收益。
        </p>
        <ol className="mt-3 space-y-1.5 pl-5 text-sm leading-relaxed text-muted-foreground">
          <li className="list-decimal">
            在表格里点「下载」把快照文件取到本地，妥善留存（建议多地各存一份）。
          </li>
          <li className="list-decimal">
            需要回滚时，由运维在宿主上人工核对快照内容与目标版本后再行导入。
          </li>
          <li className="list-decimal">
            若要的是完整的数据库级备份/恢复，请直接使用{' '}
            <code className="rounded bg-muted px-1 py-0.5 text-[13px]">deploy/backup.sh</code>{' '}
            的 <code className="rounded bg-muted px-1 py-0.5 text-[13px]">pg_dump</code> 方案，
            它产出的是 PostgreSQL 原生转储，可用 <code className="rounded bg-muted px-1 py-0.5 text-[13px]">pg_restore</code> 还原。
          </li>
        </ol>
        <p className="mt-3 text-xs text-muted-foreground/80">
          提示：备份只包含数据库中的内容（文章、页面、评论、设置等），
          <code className="rounded bg-muted px-1 py-0.5">data/uploads</code> 与{' '}
          <code className="rounded bg-muted px-1 py-0.5">data/files</code> 里的媒体文件不在快照内，
          迁移或恢复时请连同这两个目录一起搬迁。备份相关操作可以在{' '}
          <Link href="/admin/logs" className="text-accent hover:underline">
            网站日志
          </Link>
          的「系统」分类里查到记录。
        </p>
      </div>
    </PageTransition>
  )
}

/** 备份表格行：独立组件，入场动画只在挂载时播放一次（useReveal 自带 rAF 兜底） */
function BackupRow({
  b,
  index,
  pending,
  downloading,
  onDownload,
  onDelete,
}: {
  b: BackupFile
  index: number
  pending: boolean
  downloading: boolean
  onDownload: (name: string) => void
  onDelete: (name: string) => void
}) {
  const notify = useNotify()
  const rowRef = useRef<HTMLTableRowElement>(null)
  useReveal(rowRef, { y: 10, duration: 0.35, delay: Math.min(index * 0.04, 0.3) })

  return (
    <tr
      ref={rowRef}
      className="border-b border-border/60 transition-colors last:border-0 hover:bg-muted/50"
    >
      <td className="max-w-[20rem] truncate px-5 py-3.5 font-mono text-[13px]" title={b.name}>
        {b.name}
      </td>
      <td className="px-5 py-3.5 text-muted-foreground tabular-nums">{formatSize(b.size)}</td>
      <td className="px-5 py-3.5 text-xs text-muted-foreground">
        {new Date(b.created_at).toLocaleString('zh-CN')}
      </td>
      <td className="px-5 py-3.5">
        <span className="rounded-full bg-muted px-2.5 py-0.5 text-xs font-medium text-muted-foreground">
          {reasonLabel(b.reason)}
        </span>
      </td>
      <td className="px-5 py-3.5 text-right">
        <div className="inline-flex items-center gap-1">
          <button
            onClick={() => onDownload(b.name)}
            disabled={pending}
            className="inline-flex items-center gap-1 rounded-md px-2.5 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-30"
          >
            <Download className={`h-3.5 w-3.5 ${downloading ? 'animate-pulse' : ''}`} />
            {downloading ? '下载中…' : '下载'}
          </button>
          <button
            onClick={async () => {
              const ok = await notify.confirm({
                title: `删除备份「${b.name}」？`,
                message: '该快照文件会被移除且无法恢复，已下载到本地的副本不受影响。',
                confirmText: '删除',
                danger: true,
              })
              if (ok) onDelete(b.name)
            }}
            disabled={pending}
            className="inline-flex items-center gap-1 rounded-md px-2.5 py-1.5 text-xs text-red-500 transition-colors hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-30"
          >
            <Trash2 className="h-3.5 w-3.5" />
            删除
          </button>
        </div>
      </td>
    </tr>
  )
}
