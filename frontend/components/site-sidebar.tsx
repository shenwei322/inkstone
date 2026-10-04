'use client'

import { useEffect, useState } from 'react'
import { PanelRight, X } from 'lucide-react'
import { WidgetRenderer } from '@/components/sidebar-widgets'
import { Presence } from '@/components/motion'
import { useMediaQuery } from '@/lib/use-media-query'
import type { SidebarWidget } from '@/components/site-config-context'

/**
 * 小工具的稳定 React key。
 *
 * 不能用数组下标：后台「外观」页支持上移/下移重排小工具，用 index 作 key
 * 会让 React 在重排后复用错误的组件实例，内部 state（时钟 tick、天气缓存）
 * 跟着错位。这里用「配置内容 + 只读序号」派生：内容相同的重复小工具
 * 再用序号区分，正常情况内容变化即视为另一个组件。
 */
function widgetKey(w: SidebarWidget, index: number): string {
  const content = `${w.type}|${w.title ?? ''}|${w.content ?? ''}|${w.city ?? ''}|${w.date ?? ''}`
  return `${content}|${index}`
}

/**
 * 站点侧边栏（小工具）。
 * - 桌面（lg+）：与原布局完全一致的 sticky 侧边栏，不做任何改动。
 * - 手机（<lg）：默认隐藏，屏幕边缘一个「侧边栏」浮动按钮，点击后从对应侧滑出抽屉。
 * 用媒体查询二选一渲染，小工具只挂载一份，桌面端样式零改动。
 */
export function SiteSidebar({
  widgets,
  position = 'right',
}: {
  widgets: SidebarWidget[]
  position?: 'left' | 'right'
}) {
  const isDesktop = useMediaQuery('(min-width: 1024px)')
  const [open, setOpen] = useState(false)
  const left = position === 'left'

  // 抽屉打开时锁定 body 滚动
  useEffect(() => {
    if (!open) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = prev
    }
  }, [open])

  // 桌面端：常规 sticky 侧边栏（与改动前一致）
  if (isDesktop) {
    return (
      <aside className={`h-fit space-y-4 lg:sticky lg:top-24${left ? ' lg:order-first' : ''}`}>
        {widgets.map((w, i) => (
          <WidgetRenderer key={widgetKey(w, i)} widget={w} />
        ))}
      </aside>
    )
  }

  // 手机端：边缘浮动按钮 + 侧滑抽屉
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-label="查看侧边栏"
        className={`fixed top-1/2 z-[105] -translate-y-1/2 lg:hidden ${
          left ? 'left-0 rounded-r-xl border-l-0' : 'right-0 rounded-l-xl border-r-0'
        } inline-flex flex-col items-center gap-1.5 border border-border bg-card px-1.5 py-3 text-muted-foreground shadow-md transition-colors hover:text-accent`}
      >
        <PanelRight className={`h-4 w-4 ${left ? 'rotate-180' : ''}`} />
        <span className="text-[10px] font-medium" style={{ writingMode: 'vertical-rl' }}>
          侧边栏
        </span>
      </button>

      {/* 抽屉遮罩：点击关闭 */}
      <Presence
        show={open}
        duration={0.2}
        className="fixed inset-0 z-[110] bg-black/50 backdrop-blur-sm lg:hidden"
        onClick={() => setOpen(false)}
      >
        {''}
      </Presence>
      {/* 侧滑抽屉面板 */}
      <Presence
        show={open}
        x={left ? '-100%' : '100%'}
        duration={0.35}
        className={`fixed inset-y-0 z-[115] flex w-72 max-w-[85vw] flex-col overflow-y-auto border-border bg-card p-4 shadow-2xl lg:hidden ${
          left ? 'left-0 border-r' : 'right-0 border-l'
        }`}
      >
        <div className="mb-3 flex items-center justify-between">
          <p className="text-sm font-semibold">侧边栏</p>
          <button
            type="button"
            onClick={() => setOpen(false)}
            aria-label="关闭侧边栏"
            className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            <X className="h-5 w-5" />
          </button>
        </div>
        <div className="space-y-4">
          {widgets.map((w, i) => (
            <WidgetRenderer key={widgetKey(w, i)} widget={w} />
          ))}
        </div>
      </Presence>
    </>
  )
}
