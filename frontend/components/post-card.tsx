'use client'

import Link from 'next/link'
import Image from 'next/image'
import { useMemo } from 'react'
import { ChevronRight, Eye } from 'lucide-react'
import { HoverLift, StaggerItem } from '@/components/motion'
import type { Article } from '@/lib/types'

/**
 * fallbackExcerpt 在没有后端摘要时兜底从正文剥一段。
 *
 * 正常情况下走不到：保存文章时后端就生成好 excerpt 了。这条路径只服务
 * 极旧的存量数据（迁移前入库、或手动改库），此时不能显示空白卡片。
 *
 * 按字符截断而不是 slice()：slice 按 UTF-16 码元，
 * `'😀'.slice(0, 1)` 会劈出半个代理对变成乱码。
 */
function fallbackExcerpt(html: string): string {
  const text = html
    .replace(/<[^>]*>/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  if (!text) return ''
  return Array.from(text).slice(0, 120).join('')
}

/** 从正文里取第一张图作为兜底封面（后台没设封面时用） */
function firstImage(html: string): string {
  return html.match(/<img[^>]+src="([^"]+)"/)?.[1] ?? ''
}

interface PostCardProps {
  article: Article
  /** 是否显示封面块。搜索页要紧凑、一屏多看几条，传 false 即退化为纯文字卡片 */
  showCover?: boolean
  /**
   * 窄容器（侧栏在旁的 1fr 列）下标题字号降一档。
   * 由调用方决定，因为组件自己不知道父容器的可用宽度。
   */
  compact?: boolean
}

/**
 * Fuwari 风格文章卡片。
 *
 * 视觉结构（来自 saicaca/fuwari 的 PostCard.astro）：
 *   ┌──────────────────────────────┬─────────┐
 *   │ ▍标题                        │         │
 *   │ 📅 日期 ／ 📁 分类 ／ 🏷 标签 │  封面   │
 *   │ 摘要文字……                   │ (28%)   │
 *   │ 阅读量 · 作者                │         │
 *   └──────────────────────────────┴─────────┘
 *
 * 几个关键取舍：
 * - 封面是「绝对定位在右侧、上下各内缩 0.75rem」的独立圆角块，而不是
 *   撑满卡片一侧的整块——这让卡片四周留出一圈底色，封面像浮贴在上面。
 * - 无封面时右侧改为一条 3.25rem 宽的竖长「进入」按钮，而不是让文字撑满，
 *   这样有无封面的卡片高度与视线节奏保持一致。
 * - 标题左缘的 accent 短竖线用 CSS ::before 绘制（.article-card-title），
 *   不占布局空间；悬停时竖线拉长、标题变色、箭头从左侧滑入。
 */
export function PostCard({ article, showCover = true, compact = false }: PostCardProps) {
  const date = article.published_at
    ? new Date(article.published_at).toLocaleDateString('zh-CN')
    : new Date(article.created_at).toLocaleDateString('zh-CN')

  // 封面：优先后台设置的，否则取正文第一张图。
  // 用 useMemo 缓存，避免列表每次渲染都做多段正则。
  const { cover, excerpt } = useMemo(() => {
    const c = article.cover || firstImage(article.content)
    return {
      cover: showCover ? c : '',
      excerpt: article.excerpt?.trim() || fallbackExcerpt(article.content),
    }
  }, [article, showCover])

  const tags = article.tags?.slice(0, 3) ?? []

  return (
    <StaggerItem>
      <HoverLift y={-2}>
        <Link href={`/posts/${article.slug}`} className="group block" aria-label={article.title}>
          <article className="article-card card-base border border-border shadow-[var(--card-shadow-flat)] hover:border-accent/30 hover:shadow-[var(--card-shadow-hover)]">
            {/* 置顶标记：Fuwari 的置顶项做法，角标贴在左上角 */}
            {article.is_pinned && (
              <span className="absolute left-0 top-0 z-30 rounded-br-lg bg-accent/15 px-2 py-0.5 text-[10px] font-bold text-accent">
                置顶
              </span>
            )}

            {/* 正文区：桌面端为封面让出 28% 宽度（+0.75rem 间隙）；
                无封面时让给 3.25rem 的进入按钮 */}
            <div className="relative w-full px-5 pb-5 pt-5 md:py-7 md:pl-9 md:pr-2">
              <div className={cover ? 'md:w-[calc(100%-var(--cover-width)-0.75rem)]' : 'md:w-[calc(100%-3.25rem-0.75rem)]'}>
                {/* 标题 + 悬停滑入的箭头 */}
                <h2
                  className={`article-card-title relative font-bold leading-snug tracking-tight text-90 transition-colors group-hover:text-accent ${
                    compact ? 'text-xl md:text-2xl' : 'text-2xl md:text-3xl'
                  }`}
                >
                  {article.title}
                  <ChevronRight className="article-card-arrow ml-1 inline-block h-[1.4em] w-[1.4em] translate-y-0.5 text-accent" />
                </h2>

                {/* 元信息：Fuwari 的 meta-icon + 「/」分隔 */}
                <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2">
                  <div className="with-divider flex items-center">
                    <span className="meta-icon">
                      <CalendarIcon />
                    </span>
                    <span className="text-50 whitespace-nowrap text-sm font-medium">
                      <time dateTime={article.published_at ?? article.created_at}>{date}</time>
                    </span>
                  </div>

                  {article.category && (
                    <div className="with-divider flex items-center">
                      <span className="meta-icon">
                        <FolderIcon />
                      </span>
                      {/* 注意：整张卡片已由外层 Link 包裹，这里不能再套 <a>——
                          嵌套链接是非法 HTML，会引发 hydration 报错。
                          因此分类/标签只用文本 + 悬停变色表达，不用链接。 */}
                      <span className="text-50 whitespace-nowrap text-sm font-medium transition-colors group-hover:text-accent">
                        {article.category.name}
                      </span>
                    </div>
                  )}

                  {tags.length > 0 && (
                    <div className="with-divider flex items-center">
                      <span className="meta-icon">
                        <TagIcon />
                      </span>
                      <span className="flex flex-nowrap items-center">
                        {tags.map((tag, index) => (
                          <span key={tag.id} className="flex items-center">
                            {index > 0 && (
                              <span className="mx-1.5 text-sm text-30">/</span>
                            )}
                            <span className="text-50 whitespace-nowrap text-sm font-medium transition-colors group-hover:text-accent">
                              {tag.name}
                            </span>
                          </span>
                        ))}
                      </span>
                    </div>
                  )}
                </div>

                {/* 摘要 */}
                {excerpt && (
                  <p className="text-75 mt-3.5 line-clamp-2 pr-4 text-sm leading-relaxed md:line-clamp-3">
                    {excerpt}
                  </p>
                )}

                {/* 底部：阅读量 + 作者（Fuwari 的字数/阅读时长位置） */}
                <div className="text-30 mt-4 flex items-center gap-4 text-sm transition-colors">
                  <span className="flex items-center gap-1">
                    <Eye className="h-3.5 w-3.5" />
                    {article.views} 阅读
                  </span>
                  <span className="text-30">·</span>
                  <span className="truncate">{article.author.username}</span>
                </div>
              </div>
            </div>

            {/* 封面块：桌面端绝对定位在右侧、上下内缩；移动端在卡片顶部通栏 */}
            {cover ? (
              <span className="article-card-cover mx-4 mt-4 -mb-0 block max-h-[40vw] md:mx-0 md:mt-0 md:max-h-none">
                <span className="article-card-cover-overlay" />
                <span className="absolute inset-0 z-20 flex items-center justify-center">
                  <ChevronRight className="article-card-cover-arrow h-12 w-12 text-white" />
                </span>
                <Image
                  src={cover}
                  alt={article.title}
                  fill
                  sizes="(max-width: 768px) 100vw, 28vw"
                  className="object-cover"
                  onError={(e) => {
                    // 图挂了就整块隐藏，避免留一个破图占位
                    ;(e.currentTarget.parentElement as HTMLElement).style.display = 'none'
                  }}
                />
              </span>
            ) : (
              /* 无封面：右侧竖长的「进入」按钮 */
              <span className="article-card-enter">
                <ChevronRight className="h-8 w-8 text-accent" />
              </span>
            )}
          </article>
        </Link>
      </HoverLift>
    </StaggerItem>
  )
}

/* ---------- 元信息图标（内联 SVG，尺寸对齐 lucide 的 1rem 视觉重量） ---------- */

function CalendarIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="h-4 w-4">
      <rect x="3" y="4" width="18" height="18" rx="2" />
      <path d="M16 2v4M8 2v4M3 10h18" />
    </svg>
  )
}

function FolderIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="h-4 w-4">
      <path d="M4 20a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h4l2 3h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2z" />
    </svg>
  )
}

function TagIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="h-4 w-4">
      <path d="M12.586 2.586A2 2 0 0 0 11.172 2H4a2 2 0 0 0-2 2v7.172a2 2 0 0 0 .586 1.414l8.704 8.704a2.426 2.426 0 0 0 3.42 0l6.58-6.58a2.426 2.426 0 0 0 0-3.42z" />
      <circle cx="7.5" cy="7.5" r="1.5" fill="currentColor" />
    </svg>
  )
}
