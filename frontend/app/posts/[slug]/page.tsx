import type { Metadata } from 'next'
import { dehydrate, HydrationBoundary, QueryClient } from '@tanstack/react-query'
import { PostDetail } from './post-detail'
import { fetchArticleBySlug } from '@/lib/api'
import { SITE_URL, plainText, resolveSlug } from '@/lib/seo'

/**
 * 文章页的 <title> / meta / OG / canonical。
 *
 * 为什么必须自己取数：文章标题在数据库里，根布局的 generateMetadata
 * 只能拿到站点名，页面专属的元信息只能在各路由生成。
 *
 * 取不到文章时返回降级元信息而**不 throw**——throw 会让 Next 把整个页面
 * 判定为 500，而"文章不存在"本该是 404/200 的正常降级，页面组件那边
 * 已经渲染了"文章不存在"的兜底卡片。
 */
export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
  const slug = await resolveSlug(params)
  const url = `${SITE_URL}/posts/${encodeURIComponent(slug)}`

  let title = '文章不存在'
  let description: string | undefined
  let publishedTime: string | undefined
  let authors: string[] | undefined
  let images: string[] | undefined

  try {
    // 公开接口，不带 auth：爬虫与社交平台抓取时也没有令牌
    const { article } = await fetchArticleBySlug(slug)
    title = article.title
    description = plainText(article.content, 110)
    publishedTime = article.published_at ?? article.created_at
    authors = [article.author.username]
    if (article.cover) images = [article.cover]
  } catch {
    // 文章不存在或后端不可达：用降级元信息，页面本身照常渲染兜底卡片
  }

  return {
    title,
    description,
    openGraph: {
      type: 'article',
      title,
      description,
      url,
      publishedTime,
      authors,
      // 无封面时整个字段省略：OG 规范里空 images 数组不如没有
      ...(images ? { images } : {}),
    },
    twitter: {
      card: images ? 'summary_large_image' : 'summary',
      title,
      description,
      ...(images ? { images } : {}),
    },
    // canonical 交给具体文章地址：首页带 ?category= / ?tag= / ?q= 的
    // 多个 URL 渲染同一批内容时不会互相争夺权重
    alternates: { canonical: url },
  }
}

// 服务端预取文章，首屏直出正文（避免先骨架再客户端取数的等待）
export default async function PostPage({ params }: { params: Promise<{ slug: string }> }) {
  const slug = await resolveSlug(params)

  const queryClient = new QueryClient()
  await queryClient.prefetchQuery({
    queryKey: ['article', 'slug', slug],
    queryFn: () => fetchArticleBySlug(slug),
  })

  return (
    <HydrationBoundary state={dehydrate(queryClient)}>
      <PostDetail slug={slug} />
    </HydrationBoundary>
  )
}
