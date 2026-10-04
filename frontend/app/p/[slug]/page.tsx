import type { Metadata } from 'next'
import { dehydrate, HydrationBoundary, QueryClient } from '@tanstack/react-query'
import { StaticPageDetail } from './page-detail'
import { fetchPageBySlug } from '@/lib/api'
import { SITE_URL, plainText, resolveSlug } from '@/lib/seo'

/**
 * 独立页的元信息。
 *
 * 独立页有三种模板（default / fullwidth / landing），landing 的 content 是
 * 用户自己写的整页 HTML，摘要照常提取——那一串标签被剥掉后剩下的就是正文。
 * 取不到数据时同样降级而不 throw（理由见文章页 generateMetadata）。
 */
export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
  const slug = await resolveSlug(params)
  const url = `${SITE_URL}/p/${encodeURIComponent(slug)}`

  let title = '页面不存在'
  let description: string | undefined

  try {
    const { page } = await fetchPageBySlug(slug)
    title = page.title
    if (page.content) description = plainText(page.content, 110)
  } catch {
    // 页面不存在或后端不可达：降级元信息，页面组件渲染兜底卡片
  }

  return {
    title,
    description,
    openGraph: {
      // 独立页不是文章：type 用 website。它没有发布时间与作者概念，
      // 硬填 publishedTime 会让结构化数据与页面内容不符。
      type: 'website',
      title,
      description,
      url,
    },
    twitter: {
      card: 'summary',
      title,
      description,
    },
    alternates: { canonical: url },
  }
}

// 服务端预取独立页，首屏直出内容
export default async function StaticPage({ params }: { params: Promise<{ slug: string }> }) {
  const slug = await resolveSlug(params)

  const queryClient = new QueryClient()
  await queryClient.prefetchQuery({
    queryKey: ['page', slug],
    queryFn: () => fetchPageBySlug(slug),
  })

  return (
    <HydrationBoundary state={dehydrate(queryClient)}>
      <StaticPageDetail slug={slug} />
    </HydrationBoundary>
  )
}
