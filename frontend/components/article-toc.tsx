'use client'

import { useEffect, useRef, useState, type CSSProperties, type RefObject } from 'react'
import { ListTree } from 'lucide-react'

export interface TocItem {
  id: string
  text: string
  level: number
}

/**
 * 从文章 HTML 提取目录（h1-h3，文档序）。
 * 在 useMemo 里对 HTML 字符串做 DOMParser 解析——不读 ref、不 setState，
 * SSR 时 document 不存在返回空数组（客户端挂载后由 mounted gate 生效）。
 */
export function parseToc(html: string): TocItem[] {
  if (typeof document === 'undefined' || !html) return []
  const doc = new DOMParser().parseFromString(html, 'text/html')
  // 关键：不要在这里过滤空标题。
  // ArticleToc 会给真实 DOM 里 querySelectorAll('h1,h2,h3') 的第 i 个标题
  // 补上 items[i].id，若这里把空标题剔掉，下标就会与真实 DOM 错位一格
  // （正文中只要有 <h2></h2>，其后所有目录项都会跳错章节）。
  return Array.from(doc.querySelectorAll('h1, h2, h3')).map((h, i) => ({
    id: `toc-heading-${i}`,
    text: h.textContent?.trim() ?? '',
    level: Number(h.tagName.slice(1)),
  }))
}

/**
 * 文章目录（自动生成）。滚动时高亮当前章节、点击平滑跳转。
 * 由父组件根据 tocItems.length 决定布局（无侧边栏时贴空的一侧，无标题则整列隐藏）。
 *
 * 视觉部分（卡片实底、轨道竖线、激活段、层级缩进、两行截断）全部收在
 * app/globals.css 的「文章目录」一节里，这里只负责结构与状态。
 * 这样做的原因：Tailwind 的 bg-card/60、text-xs、tracking-widest 组合
 * 在壁纸模式下既透又散，逐处覆写会把类名写成一条长串，改观感还要翻组件。
 */
export function ArticleToc({ items, contentRef }: { items: TocItem[]; contentRef: RefObject<HTMLElement | null> }) {
  const [activeId, setActiveId] = useState(items[0]?.id ?? '')
  const navRef = useRef<HTMLElement | null>(null)

  // 给真实标题补锚点 id（纯 DOM 写操作，不涉及 state）
  useEffect(() => {
    const root = contentRef.current
    if (!root || items.length === 0) return
    const headings = root.querySelectorAll('h1, h2, h3')
    items.forEach((item, i) => {
      const el = headings[i]
      if (el && !el.id) el.id = item.id
    })
  }, [items, contentRef])

  // 滚动高亮：取视口上方最近的标题（在事件回调里 setState，非 effect 同步路径）。
  // headings 在 effect 内缓存一次——rAF 回调每帧执行，逐项 getElementById
  // 会反复强制同步 layout，长文章滚动时是明显卡顿源。
  useEffect(() => {
    if (items.length === 0) return
    let raf = 0
    const headings = items.map((item) => document.getElementById(item.id))
    const onScroll = () => {
      if (raf) return
      raf = requestAnimationFrame(() => {
        raf = 0
        let current = items[0]?.id ?? ''
        headings.forEach((el, i) => {
          if (el && el.getBoundingClientRect().top <= 120) current = items[i].id
        })
        setActiveId(current)
      })
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      window.removeEventListener('scroll', onScroll)
      if (raf) cancelAnimationFrame(raf)
    }
  }, [items])

  // 目录自身可滚动：章节多时让激活项留在可视区内。
  // 不用 scrollIntoView——它会连带滚动祖先（整页），只想动目录里那个小盒子。
  useEffect(() => {
    const nav = navRef.current
    if (!nav) return
    const el = nav.querySelector<HTMLElement>(`[data-toc-id="${activeId}"]`)
    if (!el) return
    const top = el.offsetTop
    const bottom = top + el.offsetHeight
    if (top < nav.scrollTop) {
      nav.scrollTop = Math.max(0, top - 8)
    } else if (bottom > nav.scrollTop + nav.clientHeight) {
      nav.scrollTop = bottom - nav.clientHeight + 8
    }
  }, [activeId])

  if (items.length === 0) return null
  // 只渲染有文字的标题；空标题仍留在 items 里以维持与 DOM 的下标对齐
  const visible = items.filter((item) => item.text)
  if (visible.length === 0) return null

  return (
    <aside className="hidden lg:block">
      <div className="article-toc-card card-flat sticky top-24 flex max-h-[calc(100vh-8rem)] flex-col overflow-hidden">
        <p className="article-toc-title">
          <ListTree className="h-3.5 w-3.5 shrink-0" aria-hidden />
          <span>目录</span>
          <span className="article-toc-count">{visible.length}</span>
        </p>
        <nav
          ref={navRef}
          aria-label="文章目录"
          className="article-toc-nav min-h-0 flex-1 overflow-y-auto pb-1.5 pr-1.5"
        >
          {visible.map((item) => {
            const active = activeId === item.id
            return (
              <a
                key={item.id}
                href={`#${item.id}`}
                title={item.text}
                aria-current={active ? 'true' : undefined}
                data-toc-id={item.id}
                data-toc-level={item.level}
                // 层级缩进交给 CSS 的 --toc-depth：h1→0、h2→1、h3→2，
                // 改缩进步长只动 globals.css 一处，不必回来改内联样式
                style={{ '--toc-depth': item.level - 1 } as CSSProperties}
                className="article-toc-item"
              >
                {item.text}
              </a>
            )
          })}
        </nav>
      </div>
    </aside>
  )
}
