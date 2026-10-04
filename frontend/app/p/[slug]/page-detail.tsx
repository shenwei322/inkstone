'use client'

import { useRef } from 'react'
import Link from 'next/link'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, SearchX } from 'lucide-react'
import { fetchPageBySlug } from '@/lib/api'
import { proseBody } from '@/lib/ui'
import { PageTransition, Reveal, useReveal } from '@/components/motion'
import { PageLoading } from '@/components/page-loader'
import { useCodeHighlight } from '@/components/code-highlight'
import { useSiteConfig } from '@/components/site-config-context'
import { useNotify } from '@/components/toast'

export function StaticPageDetail({ slug }: { slug: string }) {
  const site = useSiteConfig()
  const notify = useNotify()

  const { data, isLoading, isError } = useQuery({
    queryKey: ['page', slug],
    queryFn: () => fetchPageBySlug(slug),
  })

  // 语义标签 header/footer 的入场动效：useReveal 保留原 DOM 结构
  const headerRef = useRef<HTMLHeadingElement>(null)
  const footerRef = useRef<HTMLElement>(null)
  const fullwidthContentRef = useRef<HTMLDivElement>(null)
  const defaultContentRef = useRef<HTMLDivElement>(null)
  const templateContentRef = useRef<HTMLDivElement>(null)
  useReveal(headerRef, { duration: 0.55 })
  useReveal(footerRef, { duration: 0.55, delay: 0.4 })
  useReveal(fullwidthContentRef, { delay: 0.12, duration: 0.55 })
  useReveal(defaultContentRef, { delay: 0.12, duration: 0.55 })

  // 三个模板的正文都走 hljs 高亮 + 复制按钮（复制成功弹 toast）。
  // 必须在下方 early return 之前调用，保证 hooks 顺序稳定
  const pending = data?.page
  const copyOpts = { onCopySuccess: () => notify.success('代码已复制') }
  useCodeHighlight(templateContentRef, pending?.template === 'landing' ? pending.content ?? '' : '', copyOpts)
  useCodeHighlight(fullwidthContentRef, pending?.template === 'fullwidth' ? pending.content ?? '' : '', copyOpts)
  useCodeHighlight(
    defaultContentRef,
    pending && pending.template !== 'landing' && pending.template !== 'fullwidth' ? pending.content ?? '' : '',
    copyOpts,
  )

  if (isLoading) {
    return (
      <div className="mx-auto max-w-3xl px-4 py-12">
        {/* 加载态统一使用 GSAP 加载动画，替换原骨架图 */}
        <PageLoading minHeight="2.5rem" hint="加载标题…" />
        <div className="mt-8">
          <PageLoading minHeight="10rem" hint="加载内容…" />
        </div>
      </div>
    )
  }

  if (isError || !data) {
    return (
      <Reveal
        y={0}
        className="mx-auto max-w-3xl px-4 py-24 text-center"
      >
        <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-muted">
          <SearchX className="h-8 w-8 text-muted-foreground" />
        </div>
        <h1 className="mt-6 text-2xl font-bold">页面不存在</h1>
        <Link
          href="/"
          className="mt-8 inline-block rounded-lg bg-accent px-6 py-2.5 text-sm font-medium text-white transition-transform hover:scale-105"
        >
          返回首页
        </Link>
      </Reveal>
    )
  }

  const page = data.page

  // 自定义模板：整页 HTML。
  //
  // 历史 bug：这里曾写成 `content.replace(/\{\{content\}\}/g, content)`——
  // 用整段页面内容去替换页面内容里的占位符，等于自我替换。String.replace
  // 单次扫描不会真的无限展开，但输出体积直接翻倍：landing 模板本身就是整页
  // HTML，嵌套一层后 DOM 节点与样式计算量成倍增长。
  //
  // 更根本的问题是 Page 模型只有 content 一个字段（backend/internal/model/page.go），
  // 并不存在占位符语义里那个独立的"富文本正文"可填。因此这里把残留的
  // {{content}} 字面量移除，按作者原样渲染；page-admin.tsx 的说明文案已同步
  // 改为「整页 HTML，自行写全」，不再承诺会替换。
  if (page.template === 'landing') {
    const html = (page.content || '').replace(/\{\{content\}\}/g, '')
    return (
      <PageTransition>
        <div
          ref={templateContentRef}
          className="min-h-[60vh] overflow-x-auto px-4"
          dangerouslySetInnerHTML={{ __html: html }}
        />
      </PageTransition>
    )
  }

  // fullwidth：通栏无边框
  if (page.template === 'fullwidth') {
    return (
      <PageTransition>
        <div className="mx-auto max-w-6xl px-4 py-12">
          <h1
            ref={headerRef}
            className="text-center text-3xl font-bold tracking-tight sm:text-4xl"
          >
            {page.title}
          </h1>
          <div
            ref={fullwidthContentRef}
            className={`${proseBody} mx-auto mt-10`}
            dangerouslySetInnerHTML={{ __html: page.content ?? '' }}
          />
        </div>
      </PageTransition>
    )
  }

  // default：常规文章式布局
  return (
    <PageTransition>
      <div className="mx-auto max-w-3xl px-4 py-12">
        <header
          ref={headerRef}
          className="mb-8 border-b border-border pb-6"
        >
          <h1 className="text-3xl font-bold leading-tight tracking-tight sm:text-4xl">
            {page.title}
          </h1>
          <p className="mt-3 text-sm text-muted-foreground">{site.siteName}</p>
        </header>
        <div
          ref={defaultContentRef}
          className={proseBody}
          dangerouslySetInnerHTML={{ __html: page.content ?? '' }}
        />
        <footer
          ref={footerRef}
          className="mt-16 border-t border-border pt-8"
        >
          <Link
            href="/"
            className="group inline-flex items-center gap-2 text-sm text-muted-foreground transition-colors hover:text-accent"
          >
            <ArrowLeft className="h-4 w-4 transition-transform group-hover:-translate-x-1" /> 返回首页
          </Link>
        </footer>
      </div>
    </PageTransition>
  )
}
