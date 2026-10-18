'use client'

import { useMemo, useState } from 'react'
import Link from 'next/link'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Check,
  ChevronRight,
  FolderTree,
  Pencil,
  Plus,
  Tag as TagIcon,
  Trash2,
  X,
} from 'lucide-react'
import {
  createCategory,
  createTag,
  deleteCategory,
  deleteTag,
  fetchCategoryTree,
  fetchTags,
  updateCategory,
  updateTag,
  ApiError,
} from '@/lib/api'
import type { CategoryNode, TagCount } from '@/lib/types'
import { useNotify } from '@/components/toast'
import { Reveal, hoverTapScale } from '@/components/motion'
import { RowLoading } from '@/components/page-loader'
import { AdminEmpty, AdminPageHeader } from '@/components/admin-page'
import { AdminTabPanel, AdminTabs } from '@/components/admin-tabs'
import { inputClass } from '@/lib/ui'

/**
 * 分类与标签管理（原「标签管理」页扩写）。
 *
 * 合并的理由：分类与标签是同一件事（组织文章的分类法）的两个颗粒度，
 * 后台却只有一个标签页——分类的增删改接口早就存在，前端却没有入口，
 * 只能在写文章的下拉框里选已有分类，建不了也改不了名。
 *
 * 用 tab 而不是同屏并排：两者都需要「新建输入框 + 列表」的结构，
 * 并排会出现两个几乎一样的输入框，反而更难分清在编辑哪一个。
 */

const tabItems = [
  { key: 'category', label: '分类', icon: FolderTree },
  { key: 'tag', label: '标签', icon: TagIcon },
] as const

export default function AdminTaxonomyPage() {
  const [tab, setTab] = useState<string>('category')
  const notify = useNotify()
  const queryClient = useQueryClient()

  const categoryQuery = useQuery({
    queryKey: ['categories', 'tree'],
    queryFn: fetchCategoryTree,
  })
  const tagQuery = useQuery({ queryKey: ['tags'], queryFn: fetchTags })

  // `?? []` 每次渲染都新建数组，直接当 useMemo 依赖等于白做——包一层固定引用
  const categories = useMemo(() => categoryQuery.data?.categories ?? [], [categoryQuery.data])
  const tags = useMemo(() => tagQuery.data?.tags ?? [], [tagQuery.data])

  // 分类总数要把子分类一起数进来：顶层 categories 只是森林的根们
  const categoryTotal = useMemo(() => {
    const count = (nodes: CategoryNode[]): number =>
      nodes.reduce((sum, n) => sum + 1 + count(n.children ?? []), 0)
    return count(categories)
  }, [categories])

  const invalidate = () => {
    // 分类改动会同时影响树、扁平列表（编辑器下拉）与前台导航
    queryClient.invalidateQueries({ queryKey: ['categories'] })
    queryClient.invalidateQueries({ queryKey: ['tags'] })
  }

  return (
    <div>
      <AdminPageHeader
        title="分类与标签"
        description={
          tab === 'category'
            ? `共 ${categoryTotal} 个分类 · 分类可多级嵌套，用于文章归档与前台导航`
            : `共 ${tags.length} 个标签 · 发文时可直接从这些标签中选择`
        }
      />

      <div className="mt-6">
        <AdminTabs items={tabItems} value={tab} onChange={setTab} />
      </div>

      <AdminTabPanel value={tab}>
        {tab === 'category' ? (
          <CategoryPanel
            categories={categories}
            isLoading={categoryQuery.isLoading}
            invalidate={invalidate}
            notify={notify}
          />
        ) : (
          <TagPanel tags={tags} isLoading={tagQuery.isLoading} invalidate={invalidate} notify={notify} />
        )}
      </AdminTabPanel>
    </div>
  )
}

/* ------------------------------------------------------------------ */
/* 分类面板                                                             */
/* ------------------------------------------------------------------ */

type Notify = ReturnType<typeof useNotify>

function CategoryPanel({
  categories,
  isLoading,
  invalidate,
  notify,
}: {
  categories: CategoryNode[]
  isLoading: boolean
  invalidate: () => void
  notify: Notify
}) {
  const [newName, setNewName] = useState('')
  const [newParent, setNewParent] = useState<number>(0)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editingName, setEditingName] = useState('')

  // 父级下拉：把树拍平并按层级缩进，比递归渲染 <option> 更好读。
  //
  // 缩进用 `depth - 1`：后端的 depth **从 1 起算**（顶级分类 = 1），
  // 直接把 depth 当缩进级数会让顶级分类也缩进一格，看起来凭空多一层父级。
  const flatOptions = useMemo(() => {
    const out: { id: number; label: string }[] = []
    const walk = (nodes: CategoryNode[]) => {
      for (const n of nodes) {
        out.push({ id: n.id, label: `${'　'.repeat(Math.max(0, n.depth - 1))}${n.name}` })
        if (n.children?.length) walk(n.children)
      }
    }
    walk(categories)
    return out
  }, [categories])

  const create = useMutation({
    mutationFn: () => createCategory({ name: newName.trim(), parent_id: newParent }),
    onSuccess: (res) => {
      setNewName('')
      invalidate()
      notify.success(`分类「${res.category.name}」已创建`)
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '创建失败'),
  })

  const rename = useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) => updateCategory(id, { name }),
    onSuccess: () => {
      setEditingId(null)
      invalidate()
      notify.success('分类已更新')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '更新失败'),
  })

  const remove = useMutation({
    mutationFn: (id: number) => deleteCategory(id),
    onSuccess: () => {
      invalidate()
      notify.success('分类已删除')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '删除失败'),
  })

  const submit = () => {
    if (!newName.trim()) {
      notify.error('请输入分类名称')
      return
    }
    create.mutate()
  }

  return (
    <div>
      {/* 新建分类 */}
      <Reveal
        y={12}
        duration={0.4}
        className="card-flat flex flex-wrap items-center gap-2 rounded-xl border border-border bg-card p-4"
      >
        <input
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') submit()
          }}
          placeholder="输入新分类名称"
          className={`${inputClass} max-w-xs flex-1`}
        />
        <select
          value={newParent}
          onChange={(e) => setNewParent(Number(e.target.value))}
          className={`${inputClass} max-w-[12rem]`}
          aria-label="父级分类"
        >
          <option value={0}>作为顶级分类</option>
          {flatOptions.map((o) => (
            <option key={o.id} value={o.id}>
              {o.label}
            </option>
          ))}
        </select>
        <button
          type="button"
          onClick={submit}
          disabled={create.isPending}
          {...hoverTapScale}
          className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
        >
          <Plus className="h-4 w-4" />
          {create.isPending ? '创建中...' : '创建分类'}
        </button>
      </Reveal>

      {isLoading ? (
        <div className="mt-4">
          <RowLoading rows={5} />
        </div>
      ) : categories.length === 0 ? (
        <AdminEmpty
          icon={FolderTree}
          title="还没有分类"
          hint="分类用于文章归档与前台导航，比标签更「正式」；一个文章只能属于一个分类"
        />
      ) : (
        <div className="mt-4 space-y-1.5">
          {categories.map((node, i) => (
            <CategoryRow
              key={node.id}
              node={node}
              index={i}
              editingId={editingId}
              editingName={editingName}
              setEditingId={setEditingId}
              setEditingName={setEditingName}
              onRename={(id, name) => rename.mutate({ id, name })}
              onDelete={(id, name, count) => {
                notify
                  .confirm({
                    title: `删除分类「${name}」？`,
                    message:
                      count > 0
                        ? `该分类下有 ${count} 篇文章。删除后这些文章会变成「未分类」，不会一起被删掉。`
                        : '若该分类下有子分类，需先处理子分类。删除后无法恢复。',
                    confirmText: '删除',
                    danger: true,
                  })
                  .then((ok) => ok && remove.mutate(id))
              }}
              renamePending={rename.isPending}
              removePending={remove.isPending}
            />
          ))}
        </div>
      )}
    </div>
  )
}

/** 单行分类（含子分类递归）。缩进用 depth 而不是嵌套 div，避免深层嵌套后宽度塌陷。 */
function CategoryRow({
  node,
  index,
  editingId,
  editingName,
  setEditingId,
  setEditingName,
  onRename,
  onDelete,
  renamePending,
  removePending,
}: {
  node: CategoryNode
  index: number
  editingId: number | null
  editingName: string
  setEditingId: (id: number | null) => void
  setEditingName: (name: string) => void
  onRename: (id: number, name: string) => void
  onDelete: (id: number, name: string, count: number) => void
  renamePending: boolean
  removePending: boolean
}) {
  const editing = editingId === node.id

  return (
    <>
      <Reveal
        y={8}
        delay={Math.min(index * 0.03, 0.2)}
        duration={0.25}
        className="card-flat flex items-center gap-2 rounded-xl border border-border bg-card px-3.5 py-2.5 transition-colors hover:border-accent/40"
        style={{ marginLeft: Math.max(0, node.depth - 1) * 20 }}
      >
        {node.depth > 1 ? (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground/50" />
        ) : null}
        <FolderTree
          className={`h-4 w-4 shrink-0 ${node.depth <= 1 ? 'text-accent' : 'text-muted-foreground'}`}
        />

        {editing ? (
          <>
            <input
              value={editingName}
              onChange={(e) => setEditingName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && editingName.trim()) onRename(node.id, editingName.trim())
                if (e.key === 'Escape') setEditingId(null)
              }}
              autoFocus
              className="min-w-0 flex-1 rounded-md border border-border bg-background px-2 py-1 text-sm outline-none focus:border-accent"
            />
            <button
              onClick={() => editingName.trim() && onRename(node.id, editingName.trim())}
              disabled={renamePending}
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
              href={`/category/${node.slug}`}
              className="min-w-0 flex-1 truncate text-sm font-medium transition-colors hover:text-accent"
            >
              {node.name}
            </Link>
            <span className="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
              {node.article_count}
            </span>
            <button
              onClick={() => {
                setEditingId(node.id)
                setEditingName(node.name)
              }}
              className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              title="重命名"
            >
              <Pencil className="h-3.5 w-3.5" />
            </button>
            <button
              onClick={() => onDelete(node.id, node.name, node.article_count)}
              disabled={removePending}
              className="shrink-0 rounded-md p-1 text-red-500 transition-colors hover:bg-red-500/10 disabled:opacity-50"
              title="删除"
              aria-label={`删除分类 ${node.name}`}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          </>
        )}
      </Reveal>

      {(node.children ?? []).map((child, i) => (
        <CategoryRow
          key={child.id}
          node={child}
          index={index + i + 1}
          editingId={editingId}
          editingName={editingName}
          setEditingId={setEditingId}
          setEditingName={setEditingName}
          onRename={onRename}
          onDelete={onDelete}
          renamePending={renamePending}
          removePending={removePending}
        />
      ))}
    </>
  )
}

/* ------------------------------------------------------------------ */
/* 标签面板                                                             */
/* ------------------------------------------------------------------ */

function TagPanel({
  tags,
  isLoading,
  invalidate,
  notify,
}: {
  tags: TagCount[]
  isLoading: boolean
  invalidate: () => void
  notify: Notify
}) {
  const [newName, setNewName] = useState('')
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editingName, setEditingName] = useState('')

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

  return (
    <div>
      <Reveal
        y={12}
        duration={0.4}
        className="card-flat flex flex-wrap items-center gap-2 rounded-xl border border-border bg-card p-4"
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

      {isLoading ? (
        <div className="mt-4">
          <RowLoading rows={6} />
        </div>
      ) : tags.length === 0 ? (
        <AdminEmpty icon={TagIcon} title="还没有标签，先创建一个吧" />
      ) : (
        <div className="mt-4 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
          {tags.map((tag, i) => (
            <Reveal
              key={tag.id}
              y={8}
              delay={i * 0.03}
              duration={0.25}
              className="card-flat flex items-center gap-2 rounded-xl border border-border bg-card px-3.5 py-2.5 transition-colors hover:border-accent/40"
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
                    onClick={() =>
                      editingName.trim() && rename.mutate({ id: tag.id, name: editingName.trim() })
                    }
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
    </div>
  )
}
