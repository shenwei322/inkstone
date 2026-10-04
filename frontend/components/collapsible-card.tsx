'use client'

import { useState } from 'react'
import { ChevronDown } from 'lucide-react'

export interface CollapsibleCardProps {
  /** 标题（必填） */
  title: string
  /** 标题右侧图标（lucide 组件） */
  icon?: React.ComponentType<{ className?: string }>
  /** 标题下方副标题说明 */
  desc?: string
  /** 默认是否展开（默认 true） */
  defaultOpen?: boolean
  /** 标题栏右侧操作区（按钮等，点击不触发折叠） */
  actions?: React.ReactNode
  className?: string
  children: React.ReactNode
}

/**
 * 可折叠卡片：后台内容卡片的统一外壳。
 * 点击标题栏（或右侧箭头）收起/展开；箭头旋转用 CSS transform
 * （合成器线程），展开内容用现有 CSS keyframes 淡入（globals.css .animate-fade-in），
 * 不做高度动画——高度逐帧 layout 与项目动画规范冲突。
 * 折叠态仅存组件 state：刷新回到 defaultOpen，不读 localStorage（避免 SSR hydration 分歧）。
 */
export function CollapsibleCard({
  title,
  icon: Icon,
  desc,
  defaultOpen = true,
  actions,
  className = '',
  children,
}: CollapsibleCardProps) {
  const [open, setOpen] = useState(defaultOpen)

  return (
    <section className={`mt-6 overflow-hidden rounded-2xl border border-border bg-card shadow-sm ${className}`}>
      <header className="flex flex-wrap items-center gap-3 px-5 py-4">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          className="flex min-w-0 flex-1 items-center gap-2 text-left"
        >
          {Icon && <Icon className="h-4 w-4 shrink-0 text-accent" />}
          <span className="min-w-0">
            <h2 className="text-sm font-semibold">{title}</h2>
            {desc && <p className="mt-0.5 truncate text-xs text-muted-foreground">{desc}</p>}
          </span>
          <ChevronDown
            className={`ml-auto h-4 w-4 shrink-0 text-muted-foreground transition-transform duration-200 ${
              open ? '' : '-rotate-90'
            }`}
          />
        </button>
        {actions}
      </header>
      {open && <div className="animate-fade-in border-t border-border px-5 py-5">{children}</div>}
    </section>
  )
}
