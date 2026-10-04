import Link from 'next/link'
import { Tag as TagIcon } from 'lucide-react'
import { fetchTags } from '@/lib/api'
import { PageTransition, Reveal } from '@/components/motion'

// 标签汇总页（标签云）。字号按文章数分档，纯 CSS 缩放、不用 width 动画。
export default async function TagsPage() {
  let tags: { id: number; name: string; slug: string; article_count: number }[] = []
  let failed = false
  try {
    tags = (await fetchTags()).tags
  } catch {
    failed = true
  }

  // 文章数分三档，用于决定字号。1-2 篇小、3-5 篇中、6 篇以上大。
  const sizeOf = (n: number) => (n >= 6 ? 'text-lg' : n >= 3 ? 'text-base' : 'text-sm')

  return (
    <PageTransition>
      <div className="mx-auto max-w-3xl px-4 py-10">
        <h1 className="flex items-center gap-2 text-2xl font-bold tracking-tight">
          <TagIcon className="h-6 w-6 text-accent" /> 全部标签
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          {tags.length > 0 ? `共 ${tags.length} 个标签，字号表示文章数量` : '按关键词浏览文章'}
        </p>

        {failed ? (
          <Reveal y={16} className="mt-10 rounded-xl border border-dashed p-16 text-center">
            <p className="text-muted-foreground">标签加载失败，请稍后重试</p>
          </Reveal>
        ) : tags.length === 0 ? (
          <Reveal y={16} className="mt-10 rounded-xl border border-dashed p-16 text-center">
            <TagIcon className="mx-auto h-10 w-10 text-muted-foreground/50" />
            <p className="mt-4 text-muted-foreground">还没有创建标签</p>
          </Reveal>
        ) : (
          <Reveal y={12} className="mt-8 flex flex-wrap gap-2.5">
            {tags.map((t) => (
              <Link
                key={t.id}
                href={`/tag/${t.slug}`}
                title={`${t.name}（${t.article_count} 篇）`}
                className="inline-flex items-center gap-1.5 rounded-full border border-border bg-card px-3.5 py-1.5 font-medium shadow-sm transition-all duration-300 hover:-translate-y-0.5 hover:border-accent/40 hover:text-accent hover:shadow-md"
              >
                <span className={sizeOf(t.article_count)}>{t.name}</span>
                <span className="text-xs text-muted-foreground/70">{t.article_count}</span>
              </Link>
            ))}
          </Reveal>
        )}
      </div>
    </PageTransition>
  )
}
