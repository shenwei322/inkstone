import type { Metadata } from 'next'
import { DraftPreview } from './draft-preview'

/**
 * 草稿预览页。它只对作者本人（和管理员）可见，且依赖带鉴权的请求，
 * 因此**不做 SSR 预取**——服务端没有访问令牌，预取只会拿到 401。
 *
 * 同时明确禁止索引：爬虫不该收录一个大部分访客打不开的页面，
 * 那只会把未发布的标题暴露在搜索结果里。
 */
export const metadata: Metadata = {
  title: '草稿预览',
  robots: { index: false, follow: false },
}

export default async function DraftPreviewPage({
  params,
}: {
  params: Promise<{ id: string }>
}) {
  const { id } = await params
  const numeric = Number(id)
  // 非数字 id：交给客户端组件渲染错误态，不在服务端抛异常
  return <DraftPreview id={Number.isFinite(numeric) ? numeric : null} />
}
