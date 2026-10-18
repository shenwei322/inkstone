'use client'

import { useEffect, useRef } from 'react'
import type { ReactNode } from 'react'
import gsap from 'gsap'
import type { LucideIcon } from 'lucide-react'
import { easeOut, prefersReducedMotion } from '@/components/motion'

export interface AdminTabItem {
  /** 标签唯一键，切换时回传给 onChange */
  key: string
  label: string
  /** 可选图标，正文区较宽时帮助快速定位 */
  icon?: LucideIcon
  /** 计数徽标（如「回收站 3」）；0 与 undefined 都不显示 */
  count?: number
}

/**
 * 后台分段标签页（一个页面装多块同源功能时使用）。
 *
 * 用法约定：**只在「同一个实体/同一件事」的不同视角下用它**——
 * 文章管理的「文章 / 回收站」、系统维护的「更新 / 备份」。
 * 不同领域的功能（如安全防护里的验证码与 IP 封禁）不要塞进 tab，
 * 那会让侧栏点进来的人一脸茫然地数标签页。
 *
 * 视觉与 `admin/articles` 原有的胶囊 tab 一致（accent 底 + scaleX 入场），
 * 这里把它提成共用组件，避免第三个页面再抄一遍。
 */
export function AdminTabs({
  items,
  value,
  onChange,
  className,
}: {
  items: readonly AdminTabItem[]
  value: string
  onChange: (key: string) => void
  className?: string
}) {
  // 选中胶囊：切换时以 scaleX 入场（替代 framer layoutId 滑动）。
  // 用 key 重挂元素让动画每次都从 0 开始，因此这里只需在 value 变化时触发。
  const pillRef = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    if (pillRef.current && !prefersReducedMotion()) {
      gsap.fromTo(pillRef.current, { scaleX: 0 }, { scaleX: 1, duration: 0.25, ease: easeOut })
    }
  }, [value])

  return (
    <div
      role="tablist"
      className={`flex items-center gap-1 overflow-x-auto rounded-lg border border-border bg-card p-1 ${className ?? ''}`}
    >
      {items.map((item) => {
        const active = item.key === value
        return (
          <button
            key={item.key}
            type="button"
            role="tab"
            aria-selected={active}
            onClick={() => onChange(item.key)}
            className={`relative flex shrink-0 items-center gap-1.5 rounded-md px-3.5 py-1.5 text-sm transition-colors ${
              active ? 'text-white' : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            {active && (
              <span
                key={`admin-tab-pill-${item.key}`}
                ref={pillRef}
                className="absolute inset-0 origin-left rounded-md bg-accent"
              />
            )}
            <span className="relative flex items-center gap-1.5">
              {item.icon ? <item.icon className="h-3.5 w-3.5" /> : null}
              {item.label}
              {item.count ? (
                <span
                  className={`rounded-full px-1.5 py-0.5 text-[11px] font-medium tabular-nums ${
                    active ? 'bg-white/20 text-white' : 'bg-muted text-muted-foreground'
                  }`}
                >
                  {item.count}
                </span>
              ) : null}
            </span>
          </button>
        )
      })}
    </div>
  )
}

/**
 * 标签页内容容器：统一切换时的淡入，避免点 tab 后内容「瞬移」。
 *
 * 用 key={value} 强制重挂：内容区里常带自己的 `useReveal` 入场动画，
 * 不重挂的话第二次切回来动画不会再播，视觉上像卡住了。
 */
export function AdminTabPanel({
  value,
  children,
}: {
  value: string
  children: ReactNode
}) {
  return (
    <div key={value} className="mt-4 animate-fade-in">
      {children}
    </div>
  )
}
