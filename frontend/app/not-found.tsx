'use client'

import { useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { ArrowLeft, Search as SearchIcon, SearchX } from 'lucide-react'
import { PageTransition, Reveal } from '@/components/motion'

/**
 * 站级 404。
 *
 * 为什么需要它：Next 的默认 404 是英文、无站点导航、无 branding，
 * 访客从失效外链或打错的地址进来会直接离开。这一页给出两条明确的
 * 出路——回首页，或就地搜索。
 *
 * 说明：这里不做成服务端组件，是因为搜索框需要本地 state 记录输入、
 * 并在提交时跳转到 /search。
 */
export default function NotFound() {
  const router = useRouter()
  const [draft, setDraft] = useState('')

  return (
    <PageTransition>
      <div className="mx-auto max-w-3xl px-4 py-16 sm:py-24">
        <Reveal y={16} className="rounded-2xl border border-border bg-card p-8 text-center shadow-sm sm:p-12">
          <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-accent/10">
            <SearchX className="h-8 w-8 text-accent" />
          </div>

          <h1 className="mt-6 text-3xl font-bold tracking-tight sm:text-4xl">页面不见了</h1>
          <p className="mx-auto mt-3 max-w-md text-sm leading-relaxed text-muted-foreground">
            你访问的地址可能已被删除、改名，或者从来就没有存在过。
            如果是文章链接失效，也可以试试下面的搜索。
          </p>

          {/* 就地搜索：提交后跳到搜索页，省掉"先回首页再找输入框"的一步 */}
          <form
            className="mx-auto mt-8 flex max-w-sm gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              const kw = draft.trim()
              router.push(kw ? `/search?q=${encodeURIComponent(kw)}` : '/search')
            }}
          >
            <input
              type="search"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder="搜索标题或正文"
              aria-label="搜索关键词"
              className="h-11 flex-1 rounded-lg border border-input bg-background px-4 text-sm outline-none transition-[border-color,box-shadow] focus:border-accent focus:ring-2 focus:ring-accent/20"
            />
            <button
              type="submit"
              className="inline-flex h-11 items-center gap-1.5 rounded-lg bg-accent px-5 text-sm font-medium text-white transition-transform hover:scale-105"
            >
              <SearchIcon className="h-4 w-4" />
              搜索
            </button>
          </form>

          <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
            <Link
              href="/"
              className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-card px-5 py-2.5 text-sm font-medium shadow-sm transition-colors hover:border-accent/40 hover:text-accent"
            >
              <ArrowLeft className="h-4 w-4" />
              返回首页
            </Link>
            <Link
              href="/search"
              className="inline-flex items-center gap-1.5 rounded-lg px-5 py-2.5 text-sm text-muted-foreground transition-colors hover:text-accent"
            >
              进入搜索页
            </Link>
          </div>
        </Reveal>
      </div>
    </PageTransition>
  )
}
