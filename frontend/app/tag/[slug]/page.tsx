import { notFound } from 'next/navigation'
import { fetchTags } from '@/lib/api'
import { TermArticleList, TermPageShell } from '@/components/term-article-list'

// 标签详情页。与分类详情页同构，只差数据源与文案。
export default async function TagDetailPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params
  let name = slug
  try {
    const { tags } = await fetchTags()
    const hit = tags.find((t) => t.slug === slug)
    if (!hit) notFound()
    name = hit.name
  } catch {
    // 接口失败时不 404，理由同分类页。
  }

  return (
    <TermPageShell>
      <TermArticleList kind="tag" slug={slug} name={name} />
    </TermPageShell>
  )
}
