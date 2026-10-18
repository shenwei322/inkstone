'use client'

import type { ReactNode } from 'react'
import type { LucideIcon } from 'lucide-react'

/**
 * 后台页头（所有 /admin 页面共用）。
 *
 * 此前 16 个管理页各写一份 <h1> + <p>：字号一致但图标、换行、间距、右侧操作区
 * 各写各的——`items-center` 与 `items-start` 混用、操作区 gap-2 / gap-3 混用、
 * 带图标的三页（备份/回收站/站点地图）图标尺寸与着色也不统一。
 * 统一到这里后，页头结构只有一份，改一处全站生效。
 *
 * props 说明：
 * - icon：页头左侧图标，统一 24px + accent 着色（备份/回收站此前是默认色）
 * - description：允许传 ReactNode，个别页面要在描述里塞徽章 / 用量等行内信息
 * - actions：右侧操作区（保存、新建等主按钮），窄屏自动换行到标题下方
 */
export function AdminPageHeader({
  title,
  description,
  icon: Icon,
  actions,
}: {
  title: ReactNode
  description?: ReactNode
  icon?: LucideIcon
  actions?: ReactNode
}) {
  return (
    <div className="flex flex-wrap items-start justify-between gap-x-3 gap-y-2">
      <div className="min-w-0">
        <h1 className="flex items-center gap-2 text-2xl font-bold tracking-tight">
          {Icon ? <Icon className="h-6 w-6 shrink-0 text-accent" /> : null}
          <span className="min-w-0">{title}</span>
        </h1>
        {description ? (
          <div className="mt-1 text-sm text-muted-foreground">{description}</div>
        ) : null}
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  )
}

/**
 * 后台空状态（列表页共用）。
 *
 * 此前每个列表页各写一份「虚线框 + 图标 + 文案」，间距在 p-16 / py-16 之间漂移，
 * 也有的没写 border 颜色（落到默认 currentColor，深色下几乎看不见边框）。
 * 统一为：卡片阴影 + 虚线边框 + 居中图标文案，可选补充说明与操作按钮。
 */
export function AdminEmpty({
  icon: Icon,
  title,
  hint,
  action,
}: {
  icon?: LucideIcon
  title: ReactNode
  hint?: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="card-flat mt-6 flex flex-col items-center rounded-xl border border-dashed border-border px-6 py-16 text-center">
      {Icon ? <Icon className="h-10 w-10 text-muted-foreground/50" /> : null}
      <p className="mt-4 text-muted-foreground">{title}</p>
      {hint ? <p className="mt-1 text-sm text-muted-foreground/70">{hint}</p> : null}
      {action ? <div className="mt-4">{action}</div> : null}
    </div>
  )
}
