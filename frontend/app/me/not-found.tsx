import Link from 'next/link'
import { Lock } from 'lucide-react'
import { PageTransition, Reveal } from '@/components/motion'

/**
 * /me 的兜底页。
 *
 * 触发场景有两类，都需要"引导去登录"而不是显示默认英文 404：
 *  1. 未登录就访问 /me（令牌缺失/过期后被判定无此内容）；
 *  2. 登录了但访问了 /me 下不存在的子路径。
 *
 * 做成服务端组件即可：这里只有静态文案与两个 Link，不需要交互状态。
 * 动画由 motion.tsx 的 client 组件承担（服务端组件里渲染 client 组件是允许的）。
 */
export default function MeNotFound() {
  return (
    <PageTransition>
      <div className="mx-auto max-w-3xl px-4 py-16 sm:py-24">
        <Reveal y={16} className="rounded-2xl border border-border bg-card p-8 text-center shadow-sm sm:p-12">
          <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-accent/10">
            <Lock className="h-8 w-8 text-accent" />
          </div>

          <h1 className="mt-6 text-2xl font-bold tracking-tight sm:text-3xl">这里需要先登录</h1>
          <p className="mx-auto mt-3 max-w-md text-sm leading-relaxed text-muted-foreground">
            个人中心只对已登录的用户开放。登录后即可管理自己的文章、评论与账号设置。
          </p>

          <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
            <Link
              href="/login"
              className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-5 py-2.5 text-sm font-medium text-white transition-transform hover:scale-105"
            >
              <Lock className="h-4 w-4" />
              去登录
            </Link>
            <Link
              href="/register"
              className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-card px-5 py-2.5 text-sm font-medium shadow-sm transition-colors hover:border-accent/40 hover:text-accent"
            >
              注册新账号
            </Link>
          </div>

          <p className="mt-6 text-xs text-muted-foreground/70">
            只想随便看看？回
            <Link href="/" className="mx-1 text-accent underline underline-offset-4">
              首页
            </Link>
            浏览公开文章。
          </p>
        </Reveal>
      </div>
    </PageTransition>
  )
}
