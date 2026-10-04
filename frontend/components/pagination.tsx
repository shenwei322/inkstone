'use client'

import { ChevronLeft, ChevronRight } from 'lucide-react'

export interface PaginationProps {
  /** 当前页码，从 1 开始 */
  page: number
  /** 每页条数 */
  pageSize: number
  /** 总条数（由后端返回的 total） */
  total: number
  /**
   * 翻页 / 改每页条数时的回调。
   * 两个参数一起给出，调用方无需自己推导「改了 pageSize 要不要回第 1 页」——
   * 这里改了 pageSize 一律回到第 1 页（否则可能停在不存在的页码上）。
   */
  onChange: (page: number, pageSize: number) => void
  /** 每页条数候选项；只有一项时隐藏选择器 */
  pageSizeOptions?: number[]
}

const DEFAULT_PAGE_SIZES = [20, 50, 100]

/**
 * 后台列表共用分页条。
 *
 * 抽出它的原因：此前各列表页把 page: 1 写死在 query 里、也没有翻页 UI，
 * 于是超过一页的数据在后台完全点不到（文章/用户/评论/文件都是 50 条上限）。
 *
 * 只有一页（或没有数据）时返回 null——分页控件此时不提供任何操作，
 * 摆出来只是视觉噪音。样式对齐后台既有的「上一页 / 下一页」按钮
 * （见 app/admin/logs/page.tsx）。
 */
export function Pagination({
  page,
  pageSize,
  total,
  onChange,
  pageSizeOptions = DEFAULT_PAGE_SIZES,
}: PaginationProps) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  if (total <= pageSize) return null

  const navButtonClass =
    'inline-flex items-center gap-1 rounded-lg border border-border px-3 py-1.5 transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-40'

  return (
    <div className="mt-5 flex flex-wrap items-center justify-center gap-3 text-sm">
      <button
        type="button"
        onClick={() => onChange(Math.max(1, page - 1), pageSize)}
        disabled={page <= 1}
        aria-label="上一页"
        className={navButtonClass}
      >
        <ChevronLeft className="h-4 w-4" />
        上一页
      </button>

      <span className="text-muted-foreground tabular-nums">
        第 {page} 页 / 共 {totalPages} 页
      </span>

      <button
        type="button"
        onClick={() => onChange(Math.min(totalPages, page + 1), pageSize)}
        disabled={page >= totalPages}
        aria-label="下一页"
        className={navButtonClass}
      >
        下一页
        <ChevronRight className="h-4 w-4" />
      </button>

      {pageSizeOptions.length > 1 && (
        <label className="flex items-center gap-1.5 text-muted-foreground">
          每页
          <select
            value={pageSize}
            onChange={(e) => onChange(1, Number(e.target.value))}
            aria-label="每页条数"
            className="rounded-lg border border-border bg-card px-2 py-1.5 text-sm outline-none transition-all focus:border-accent focus:ring-2 focus:ring-accent/20"
          >
            {pageSizeOptions.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
          条
        </label>
      )}
    </div>
  )
}
