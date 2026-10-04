import { notFound } from 'next/navigation'
import { fetchCategories } from '@/lib/api'
import { TermArticleList, TermPageShell } from '@/components/term-article-list'

// 分类详情页。params 在 Next 15+ 是 Promise，服务端组件直接 await，
// 不需要客户端组件去 use() 解包（那会连带引入 Suspense 边界）。
export default async function CategoryDetailPage({
  params,
}: {
  params: Promise<{ slug: string }>
}) {
  const { slug } = await params
  let name = slug
  try {
    const { categories } = await fetchCategories()
    const hit = categories.find((c) => c.slug === slug)
    if (!hit) notFound()
    name = hit.name
  } catch {
    // 分类接口失败时不 404：文章列表可能仍能加载，页面向下走即可。
  }

  return (
    <TermPageShell>
      <TermArticleList kind="category" slug={slug} name={name} />
    </TermPageShell>
  )
}
