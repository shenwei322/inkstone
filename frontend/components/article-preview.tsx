'use client'

import { useRef } from 'react'
import { CalendarDays, Eye, Tag as TagIcon, User } from 'lucide-react'
import { useCodeHighlight } from './code-highlight'
import { useNotify } from './toast'

interface ArticlePreviewProps {
  title: string
  categoryName?: string | null
  tags: string[]
  authorName: string
  date: string
  views: number
  contentHtml: string
}

/** 按文章详情页的真实样式渲染预览(标题蓝竖线 + 信息栏 + 白卡正文) */
export function ArticlePreview({
  title,
  categoryName,
  tags,
  authorName,
  date,
  views,
  contentHtml,
}: ArticlePreviewProps) {
  const contentRef = useRef<HTMLDivElement>(null)
  const notify = useNotify()
  useCodeHighlight(contentRef, contentHtml, { onCopySuccess: () => notify.success('代码已复制') })

  return (
    <div className="mx-auto max-w-4xl px-4 py-8">
      {/* 标题卡 */}
      <div className="card-flat rounded-2xl border border-border bg-card p-6 sm:p-8">
        {categoryName && (
          <span className="inline-block rounded-full bg-accent/10 px-3 py-1 text-xs font-medium text-accent">
            {categoryName}
          </span>
        )}
        <div className="mt-3 flex items-stretch gap-3">
          <span className="w-1.5 shrink-0 rounded-full bg-accent" />
          <h1 className="text-3xl font-bold leading-tight tracking-tight sm:text-4xl">
            {title || '未命名文章'}
          </h1>
        </div>
        <div className="mt-4 flex flex-wrap items-center gap-x-6 gap-y-2 text-sm text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <User className="h-4 w-4" />
            {authorName}
          </span>
          <span className="flex items-center gap-1.5">
            <CalendarDays className="h-4 w-4" />
            {date}
          </span>
          <span className="flex items-center gap-1.5">
            <Eye className="h-4 w-4" />
            {views} 次阅读
          </span>
        </div>
      </div>

      {/* 正文白卡 */}
      <div className="card-flat mt-6 rounded-2xl border border-border bg-card p-6 sm:p-10">
        <div
          ref={contentRef}
          className="prose prose-neutral dark:prose-invert max-w-none overflow-x-auto prose-headings:font-semibold prose-a:text-accent prose-pre:bg-muted prose-code:bg-muted prose-code:rounded prose-code:px-1.5 prose-code:py-0.5 prose-code:text-[0.9em] prose-code:before:content-none prose-code:after:content-none prose-img:rounded-xl prose-blockquote:border-l-accent"
          dangerouslySetInnerHTML={{ __html: contentHtml || '<p class="text-muted-foreground">（暂无正文）</p>' }}
        />
        {tags.length > 0 && (
          <div className="mt-8 flex flex-wrap items-center gap-2 border-t border-border pt-6">
            <TagIcon className="h-3.5 w-3.5 text-muted-foreground" />
            {tags.map((t) => (
              <span key={t} className="rounded-md border border-border px-2 py-0.5 text-xs text-muted-foreground">
                {t}
              </span>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
