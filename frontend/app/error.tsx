'use client'

import { AlertTriangle, RotateCcw } from 'lucide-react'
import Link from 'next/link'
import { PageTransition, Reveal } from '@/components/motion'

/**
 * 运行时错误边界（error boundary）。
 *
 * 作用与 not-found.tsx 互补：404 是"地址没有对应内容"，这里是"渲染过程中
 * 真的抛错了"（接口字段结构变了、组件里读了 undefined 等）。没有它的话，
 * 客户端渲染异常会显示一整页白屏，用户既不知道发生了什么也没有恢复手段。
 *
 * 刻意不做的事情：不在这里读 localStorage / window / 站点配置。错误边界
 * 本身可能就是因为这些基础设施出问题才触发的，再依赖它们只会二次抛错，
 * 把"可恢复的局部故障"放大成整个页面崩溃。
 *
 * 注意 prop 名是 **retry** 而不是老版本的 reset：本项目用的 Next 16 已改名
 * （见 node_modules/next/dist/client/components/error-boundary.d.ts 的
 * `retry: () => void`）。写 reset 时 TS 不报错（对象类型允许多余属性在解构
 * 场景下表现为 undefined），但点击按钮会调用 undefined 直接抛错。
 */
export default function Error({ error, retry }: { error: Error & { digest?: string }; retry: () => void }) {
  return (
    <PageTransition>
      <div className="mx-auto max-w-3xl px-4 py-16 sm:py-24">
        <Reveal y={16} className="rounded-2xl border border-border bg-card p-8 text-center shadow-sm sm:p-12">
          <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-red-500/10">
            <AlertTriangle className="h-8 w-8 text-red-500" />
          </div>

          <h1 className="mt-6 text-2xl font-bold tracking-tight sm:text-3xl">页面出错了</h1>
          <p className="mx-auto mt-3 max-w-md text-sm leading-relaxed text-muted-foreground">
            渲染这个页面时发生了异常。多数情况是临时故障，重试一次往往就能恢复；
            如果反复出现，请把下面的错误代码发给站点管理员。
          </p>

          {/* 错误信息不直接展示给访客：里面可能含接口地址、字段名等内部细节。
              digest 是 React 生成的短哈希，可以安全展示并用于日志检索。 */}
          {error.digest && (
            <p className="mt-4 font-mono text-xs text-muted-foreground/70">错误代码：{error.digest}</p>
          )}

          <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
            <button
              type="button"
              onClick={() => retry()}
              className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-5 py-2.5 text-sm font-medium text-white transition-transform hover:scale-105"
            >
              <RotateCcw className="h-4 w-4" />
              重试
            </button>
            <Link
              href="/"
              className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-card px-5 py-2.5 text-sm font-medium shadow-sm transition-colors hover:border-accent/40 hover:text-accent"
            >
              返回首页
            </Link>
          </div>

          {/* 开发环境下额外展示完整堆栈：生产只留 digest，避免泄露内部路径 */}
          {process.env.NODE_ENV === 'development' && error.message && (
            <pre className="mt-8 max-h-60 overflow-auto rounded-lg border border-border bg-muted/50 p-4 text-left text-xs leading-relaxed text-muted-foreground">
              {error.message}
            </pre>
          )}
        </Reveal>
      </div>
    </PageTransition>
  )
}
