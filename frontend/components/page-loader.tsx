'use client'

import gsap from 'gsap'
import { useEffect, useRef } from 'react'
import { createLoop, prefersReducedMotion, releaseLoop } from '@/components/motion'

/** 内联小号旋转环（GSAP 驱动），用于替换 inline 小骨架占位 */
export function Spinner({ className }: { className?: string }) {
  const ref = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el || prefersReducedMotion()) return
    const tween = createLoop(() => gsap.to(el, { rotate: 360, duration: 0.9, repeat: -1, ease: 'none' }))
    return () => {
      tween.kill()
      releaseLoop(tween)
    }
  }, [])
  return (
    <span
      ref={ref}
      role="status"
      aria-label="加载中"
      className={`inline-block h-4 w-4 shrink-0 rounded-full border-2 border-accent/25 border-t-accent align-middle ${className ?? ''}`}
    />
  )
}

/**
 * 页面/区块加载动画：双环旋转 + 墨点呼吸。
 * 循环动画统一走 createLoop（页面隐藏时自动暂停），降低远程软渲染环境的开销。
 */
export function PageLoading({
  hint = '加载中…',
  minHeight,
  className,
}: {
  hint?: string
  minHeight?: string
  className?: string
}) {
  const ringRef = useRef<HTMLDivElement>(null)
  const ring2Ref = useRef<HTMLDivElement>(null)
  const dotRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (prefersReducedMotion()) return
    const tweens = [
      createLoop(() =>
        gsap.to(ringRef.current, { rotate: 360, duration: 1.4, repeat: -1, ease: 'none' }),
      ),
      createLoop(() =>
        gsap.fromTo(ring2Ref.current, { rotate: -360 }, { rotate: 0, duration: 2.1, repeat: -1, ease: 'none' }),
      ),
      createLoop(() =>
        gsap.fromTo(
          dotRef.current,
          { scale: 0.55, opacity: 0.5 },
          { scale: 1, opacity: 1, duration: 0.8, repeat: -1, yoyo: true, ease: 'sine.inOut' },
        ),
      ),
    ]
    return () => {
      tweens.forEach((t) => {
        t.kill()
        releaseLoop(t)
      })
    }
  }, [])

  return (
    <div
      className={`flex flex-col items-center justify-center gap-3.5 text-muted-foreground ${className ?? ''}`}
      style={minHeight ? { minHeight } : undefined}
      role="status"
      aria-live="polite"
    >
      <div className="relative h-11 w-11">
        <div
          ref={ringRef}
          className="absolute inset-0 rounded-full border-[2.5px] border-accent/20 border-t-accent"
        />
        <div
          ref={ring2Ref}
          className="absolute inset-1.5 rounded-full border-2 border-dashed border-accent/30 border-b-accent/70"
        />
        <div ref={dotRef} className="absolute inset-[15px] rounded-full bg-accent/75" />
      </div>
      <p className="text-xs font-medium tracking-[0.3em]">{hint}</p>
    </div>
  )
}

/**
 * 行列表的轻量加载态。
 * 性能考虑：仅渲染**一个**旋转环，后续行只显示文字——早期每行一个环，
 * 8 行时同时跑 8 个 transform 动画，远程桌面（CPU 合成）下明显卡顿。
 */
export function RowLoading({ rows = 5 }: { rows?: number }) {
  const ringRef = useRef<HTMLSpanElement>(null)

  useEffect(() => {
    const el = ringRef.current
    if (!el || prefersReducedMotion()) return
    const tween = createLoop(() => gsap.to(el, { rotate: 360, duration: 1, repeat: -1, ease: 'none' }))
    return () => {
      tween.kill()
      releaseLoop(tween)
    }
  }, [])

  return (
    <div className="space-y-2.5" role="status" aria-live="polite">
      {Array.from({ length: rows }, (_, i) => (
        <div
          key={i}
          className="card-flat flex h-14 items-center gap-3 rounded-xl border border-border bg-card px-4"
        >
          {i === 0 ? (
            <>
              <span
                ref={ringRef}
                className="h-5 w-5 shrink-0 rounded-full border-2 border-accent/20 border-t-accent"
              />
              <span className="text-xs text-muted-foreground">加载中…</span>
            </>
          ) : (
            <span className="text-xs text-muted-foreground">加载中…</span>
          )}
        </div>
      ))}
    </div>
  )
}
