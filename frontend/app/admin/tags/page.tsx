'use client'

import { useState } from 'react'
import Link from 'next/link'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Pencil, Plus, Tag as TagIcon, Trash2, X } from 'lucide-react'
import { createTag, deleteTag, fetchTags, updateTag, ApiError } from '@/lib/api'
import type { TagCount } from '@/lib/types'
import { useNotify } from '@/components/toast'
import { PageTransition, Reveal, hoverTapScale } from '@/components/motion'
import { RowLoading } from '@/components/page-loader'
import { inputClass } from '@/lib/ui'

export default function AdminTagsPage() {
  const notify = useNotify()
  const queryClient = useQueryClient()
  const [newName, setNewName] = useState('')
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editingName, setEditingName] = useState('')

  const { data, isLoading } = useQuery({ queryKey: ['tags'], queryFn: fetchTags })

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['tags'] })
    queryClient.invalidateQueries({ queryKey: ['categories'] })
  }

  const create = useMutation({
    mutationFn: (name: string) => createTag(name),
    onSuccess: (res) => {
      setNewName('')
      invalidate()
      notify.success(`标签「${res.tag.name}」已创建`)
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '创建失败'),
  })

  const rename = useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) => updateTag(id, name),
    onSuccess: () => {
      setEditingId(null)
      invalidate()
      notify.success('标签已更新')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '更新失败'),
  })

  const remove = useMutation({
    mutationFn: (id: number) => deleteTag(id),
    onSuccess: () => {
      invalidate()
      notify.success('标签已删除')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '删除失败'),
  })

  const tags = data?.tags ?? []

  return (
    <PageTransition>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">标签管理</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            共 {tags.length} 个标签 · 发文时可直接从这些标签中选择
          </p>
        </div>
      </div>

      {/* 新建标签 */}
      <Reveal
        y={12}
        duration={0.4}
        className="mt-6 flex flex-wrap items-center gap-2 rounded-2xl border border-border bg-card p-4"
      >
        <input
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && newName.trim()) create.mutate(newName.trim())
          }}
          placeholder="输入新标签名称，回车创建"
          className={`${inputClass} max-w-xs flex-1`}
        />
        <button
          type="button"
          onClick={() => {
            if (!newName.trim()) {
              notify.error('请输入标签名称')
              return
            }
            create.mutate(newName.trim())
          }}
          disabled={create.isPending}
          {...hoverTapScale}
          className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
        >
          <Plus className="h-4 w-4" />
          {create.isPending ? '创建中...' : '创建标签'}
        </button>
      </Reveal>

      {/* 标签列表 */}
      {isLoading ? (
        <div className="mt-4">
          <RowLoading rows={6} />
        </div>
      ) : tags.length === 0 ? (
        <div className="mt-4 rounded-xl border border-dashed p-16 text-center">
          <TagIcon className="mx-auto h-10 w-10 text-muted-foreground/50" />
          <p className="mt-4 text-muted-foreground">还没有标签，先创建一个吧</p>
        </div>
      ) : (
        <div className="mt-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
          {tags.map((tag: TagCount, i) => (
            <Reveal
              key={tag.id}
              y={8}
              delay={i * 0.03}
              duration={0.25}
              className="flex items-center gap-2 rounded-xl border border-border bg-card px-3.5 py-2.5 transition-colors hover:border-accent/40"
            >
              <TagIcon className="h-4 w-4 shrink-0 text-accent" />

              {editingId === tag.id ? (
                <>
                  <input
                    value={editingName}
                    onChange={(e) => setEditingName(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && editingName.trim()) {
                        rename.mutate({ id: tag.id, name: editingName.trim() })
                      }
                      if (e.key === 'Escape') setEditingId(null)
                    }}
                    autoFocus
                    className="min-w-0 flex-1 rounded-md border border-border bg-background px-2 py-1 text-sm outline-none focus:border-accent"
                  />
                  <button
                    onClick={() => editingName.trim() && rename.mutate({ id: tag.id, name: editingName.trim() })}
                    disabled={rename.isPending}
                    className="shrink-0 rounded-md p-1 text-emerald-600 transition-colors hover:bg-emerald-500/10 disabled:opacity-50"
                    title="保存"
                  >
                    <Check className="h-4 w-4" />
                  </button>
                  <button
                    onClick={() => setEditingId(null)}
                    className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted"
                    title="取消"
                  >
                    <X className="h-4 w-4" />
                  </button>
                </>
              ) : (
                <>
                  <Link
                    href={`/tag/${tag.slug}`}
                    className="min-w-0 flex-1 truncate text-sm font-medium transition-colors hover:text-accent"
                  >
                    {tag.name}
                  </Link>
                  <span className="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                    {tag.article_count}
                  </span>
                  <button
                    onClick={() => {
                      setEditingId(tag.id)
                      setEditingName(tag.name)
                    }}
                    className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                    title="重命名"
                  >
                    <Pencil className="h-3.5 w-3.5" />
                  </button>
                  <button
                    onClick={async () => {
                      const ok = await notify.confirm({
                        title: `删除标签「${tag.name}」？`,
                        message:
                          tag.article_count > 0
                            ? `该标签被 ${tag.article_count} 篇文章使用，删除后文章仍保留。`
                            : '删除后无法恢复。',
                        confirmText: '删除',
                        danger: true,
                      })
                      if (ok) remove.mutate(tag.id)
                    }}
                    disabled={remove.isPending}
                    className="shrink-0 rounded-md p-1 text-red-500 transition-colors hover:bg-red-500/10 disabled:opacity-50"
                    title="删除"
                    aria-label={`删除标签 ${tag.name}`}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </>
              )}
            </Reveal>
          ))}
        </div>
      )}
    </PageTransition>
  )
}
