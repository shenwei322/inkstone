'use client'

import gsap from 'gsap'
import { useEffect, useRef, useState, type CSSProperties, type HTMLAttributes, type ReactNode } from 'react'

/** 全站统一缓动（近似原 easeOutExpo [0.16,1,0.3,1] 的尾段缓冲） */
export const easeOut = 'expo.out'

/** 尊重系统「减少动效」偏好：命中时所有 GSAP 动画自动跳过或瞬时完成 */
export const prefersReducedMotion = () =>
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches

/**
 * 入场动画兜底：动画由 rAF 逐帧推进，若被节流中断会停在透明态导致内容不可见
 * （曾复现：opacity 卡在 0 不动，表现为“内容被白色遮挡”）。超时后强制清除内联样式恢复可见。
 * 正常动画早已播放完毕，此处只是保险，不影响观感。
 */
function revealFallback(el: HTMLElement, duration: number, delay: number) {
  const timer = setTimeout(
    () => {
      const cs = window.getComputedStyle(el)
      if (Number(cs.opacity) < 0.99 || (cs.transform !== 'none' && cs.transform !== '')) {
        gsap.set(el, { clearProps: 'opacity,transform' })
      }
    },
    Math.max(1200, (duration + delay) * 1000 + 1200),
  )
  return () => clearTimeout(timer)
}

let gsapPatched = false

/**
 * 远程桌面/无 GPU 环境里 transform 动画走 CPU 合成，循环动画常是主要卡顿源；
 * 标签页隐藏（切换/最小化）时暂停，恢复时继续，不影响观感。
 */
const loopingAnimations = new Set<gsap.core.Tween | gsap.core.Timeline>()

/** 创建循环动画并纳入全局管理 */
export function createLoop<T extends gsap.core.Tween | gsap.core.Timeline>(create: () => T): T {
  const anim = create()
  loopingAnimations.add(anim)
  return anim
}

/** 解除循环动画登记（组件卸载 cleanup 里调用） */
export function releaseLoop(anim: gsap.core.Tween | gsap.core.Timeline | null | undefined) {
  if (anim) loopingAnimations.delete(anim)
}

let visibilityBound = false
function bindLoopVisibility() {
  if (visibilityBound || typeof document === 'undefined') return
  visibilityBound = true
  document.addEventListener('visibilitychange', () => {
    for (const anim of loopingAnimations) {
      if (document.hidden) anim.pause()
      else anim.resume()
    }
  })
}

bindLoopVisibility()

/**
 * 远程桌面 / 浏览器窗口被遮挡时，Chrome 会节流 rAF；framer-motion 走 WAAPI 不受影响，
 * 而 GSAP 是 JS tween 逐帧推进——入场动画会永久停在透明态。补丁为每个从透明态
 * 起步的 tween 注册超时看门狗，超时未完成即强制恢复可见。
 */
function patchEntranceAnimations() {
  if (gsapPatched || typeof gsap.from !== 'function') return
  gsapPatched = true
  const rawFrom = gsap.from.bind(gsap)
  const rawFromTo = gsap.fromTo.bind(gsap)
  const guard = (target: unknown, vars: gsap.TweenVars) => {
    const list = Array.isArray(target) ? target : [target]
    const el = list[0] as HTMLElement | string | undefined
    const node =
      typeof el === 'string' ? document.querySelector<HTMLElement>(el) : el instanceof Element ? el : null
    if (!node || !vars) return undefined
    const startsHidden =
      typeof vars.opacity === 'number' && vars.opacity < 0.99
        ? true
        : 'y' in vars || 'x' in vars || 'scale' in vars || 'scaleX' in vars
    if (startsHidden) {
      return revealFallback(node, (vars.duration as number) ?? 0.35, (vars.delay as number) ?? 0)
    }
    return undefined
  }
  // 动画正常结束时即取消看门狗，避免定时器空等到超时
  const withCancel = (tween: gsap.core.Tween, cancel?: () => void) => {
    if (!cancel) return tween
    const prev = tween.eventCallback('onComplete')
    tween.eventCallback('onComplete', function (this: gsap.core.Tween) {
      cancel()
      if (typeof prev === 'function') prev.call(this)
    })
    return tween
  }
  gsap.from = ((target: gsap.TweenTarget, vars: gsap.TweenVars) => {
    const cancel = guard(target, vars)
    return withCancel(rawFrom(target, vars), cancel)
  }) as typeof gsap.from
  gsap.fromTo = ((
    target: gsap.TweenTarget,
    from: gsap.TweenVars,
    to: gsap.TweenVars,
  ) => {
    const cancel = guard(target, from)
    return withCancel(rawFromTo(target, from, to), cancel)
  }) as unknown as typeof gsap.fromTo
}

patchEntranceAnimations()

/**
 * 语义标签的入场动效 hook：给已有标签（header/aside/article/button 等）挂 ref 即可。
 * 用法：const ref = useRef<HTMLElement>(null); useReveal(ref, { y: 16 })
 */
export function useReveal<T extends HTMLElement>(
  ref: React.RefObject<T | null>,
  opts: {
    y?: number
    x?: number
    scale?: number
    scaleX?: number
    opacity?: number
    duration?: number
    delay?: number
  } = {},
) {
  const { y = 16, x, scale, scaleX, opacity = 0, duration = 0.35, delay = 0 } = opts
  useEffect(() => {
    const el = ref.current
    if (!el || prefersReducedMotion()) return
    const from: gsap.TweenVars = { opacity, clearProps: 'opacity,transform' }
    if (y !== 0) from.y = y
    if (x !== undefined) from.x = x
    if (scale !== undefined) from.scale = scale
    if (scaleX !== undefined) from.scaleX = scaleX
    gsap.from(el, { ...from, duration, delay, ease: easeOut })
  }, [ref, y, x, scale, scaleX, opacity, duration, delay])
}

/**
 * 页面过渡：挂载时淡入。
 */
export function PageTransition({ children, className }: { children: ReactNode; className?: string }) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el || prefersReducedMotion()) return
    gsap.from(el, { opacity: 0, duration: 0.25, ease: easeOut, clearProps: 'opacity,transform' })
  }, [])
  return (
    <div ref={ref} className={className}>
      {children}
    </div>
  )
}

/**
 * 通用入场动效：opacity 0→1 + 可选位移/缩放。
 * 替代 motion.div 的 initial/animate/transition。
 */
export function Reveal({
  children,
  className,
  style,
  y = 16,
  x,
  scale,
  delay = 0,
  duration = 0.35,
}: {
  children: ReactNode
  className?: string
  style?: CSSProperties
  y?: number
  x?: number
  scale?: number
  delay?: number
  duration?: number
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el || prefersReducedMotion()) return
    const from: gsap.TweenVars = { opacity: 0, clearProps: 'opacity,transform' }
    if (y !== 0) from.y = y
    if (x !== undefined) from.x = x
    if (scale !== undefined) from.scale = scale
    gsap.from(el, { ...from, duration, delay, ease: easeOut })
  }, [y, x, scale, delay, duration])
  return (
    <div ref={ref} className={className} style={style}>
      {children}
    </div>
  )
}

/**
 * 列表 stagger：挂载时子元素依次淡入上移。
 * 与 framer 的 StaggerList/StaggerItem variants 用法一致，StaggerItem 现为纯透传 div。
 */
export function StaggerList({
  children,
  className,
  y = 18,
  stagger = 0.06,
  duration = 0.35,
}: {
  children: ReactNode
  className?: string
  y?: number
  stagger?: number
  duration?: number
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el || prefersReducedMotion()) return
    const items = gsap.utils.toArray<HTMLElement>(el.children)
    if (items.length === 0) return
    // 长列表 cap 总时长：20+ 项时逐项 0.06 会拖到 1.2s+，体感像卡住；封顶 0.5s 内全部入场
    const effStagger = Math.min(stagger, 0.5 / items.length)
    gsap.from(items, {
      opacity: 0,
      y,
      duration,
      ease: easeOut,
      stagger: effStagger,
      clearProps: 'opacity,transform',
    })
    // stagger 的每个子元素都可能各自停在透明态，逐个子元素注册看门狗
    const total = duration + effStagger * items.length
    const cancels = items.map((item) => revealFallback(item, total, 0))
    return () => cancels.forEach((c) => c())
  }, [y, stagger, duration])
  return (
    <div ref={ref} className={className}>
      {children}
    </div>
  )
}

export function StaggerItem({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={className}>{children}</div>
}

/** 悬停轻微上浮（替代 HoverLift / whileHover={{ y: -4 }}） */
export function HoverLift({
  children,
  className,
  y = -4,
  duration = 0.3,
}: {
  children: ReactNode
  className?: string
  y?: number
  duration?: number
}) {
  return (
    <div
      className={className}
      onPointerEnter={(e) => {
        if (prefersReducedMotion()) return
        gsap.to(e.currentTarget, { y, duration, ease: 'power2.out', overwrite: 'auto' })
      }}
      onPointerLeave={(e) => {
        if (prefersReducedMotion()) return
        gsap.to(e.currentTarget, { y: 0, duration, ease: 'power2.out', overwrite: 'auto' })
      }}
    >
      {children}
    </div>
  )
}

/** 可直接 spread 到 button/link 上的悬停放大 + 按压缩放（替代 whileHover/whileTap） */
export const hoverTapScale = {
  onMouseEnter: (e: React.MouseEvent<HTMLElement>) => {
    if (prefersReducedMotion()) return
    gsap.to(e.currentTarget, { scale: 1.03, duration: 0.2, ease: 'power2.out', overwrite: 'auto' })
  },
  onMouseLeave: (e: React.MouseEvent<HTMLElement>) => {
    if (prefersReducedMotion()) return
    gsap.to(e.currentTarget, { scale: 1, duration: 0.2, ease: 'power2.out', overwrite: 'auto' })
  },
  onMouseDown: (e: React.MouseEvent<HTMLElement>) => {
    if (prefersReducedMotion()) return
    gsap.to(e.currentTarget, { scale: 0.97, duration: 0.1, overwrite: 'auto' })
  },
  onMouseUp: (e: React.MouseEvent<HTMLElement>) => {
    if (prefersReducedMotion()) return
    gsap.to(e.currentTarget, { scale: 1.03, duration: 0.1, overwrite: 'auto' })
  },
} satisfies HTMLAttributes<HTMLElement>

/** 可直接 spread 到任意元素上的悬停上浮（替代 whileHover={{ y: -4 }}） */
export const hoverLift = {
  onPointerEnter: (e: React.PointerEvent<HTMLElement>) => {
    if (prefersReducedMotion()) return
    gsap.to(e.currentTarget, { y: -4, duration: 0.3, ease: 'power2.out', overwrite: 'auto' })
  },
  onPointerLeave: (e: React.PointerEvent<HTMLElement>) => {
    if (prefersReducedMotion()) return
    gsap.to(e.currentTarget, { y: 0, duration: 0.3, ease: 'power2.out', overwrite: 'auto' })
  },
} satisfies HTMLAttributes<HTMLElement>

/** 进入视口时淡入一次（替代 whileInView + viewport={{ once: true }}） */
export function InView({
  children,
  className,
  y = 16,
  duration = 0.5,
}: {
  children: ReactNode
  className?: string
  y?: number
  duration?: number
}) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    if (prefersReducedMotion()) return
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((en) => en.isIntersecting)) {
          gsap.from(el, {
            opacity: 0,
            y,
            duration,
            ease: easeOut,
            clearProps: 'opacity,transform',
          })
          io.disconnect()
        }
      },
      { threshold: 0.1 },
    )
    io.observe(el)
    return () => io.disconnect()
  }, [y, duration])
  return (
    <div ref={ref} className={className}>
      {children}
    </div>
  )
}

/**
 * 条件渲染 + GSAP 进出场动画（替代 AnimatePresence + motion.div）。
 * - show 由 false→true：立即挂载并播放入场
 * - show 由 true→false：播放退场，动画结束后自动卸载
 */
export function Presence({
  show,
  children,
  className,
  style,
  y,
  x,
  scale,
  duration = 0.24,
  exitDuration,
  delay = 0,
  ...rest
}: {
  show: boolean
  children: ReactNode
  className?: string
  style?: CSSProperties
  y?: number
  x?: number | string
  scale?: number
  duration?: number
  exitDuration?: number
  delay?: number
} & HTMLAttributes<HTMLDivElement>) {
  const [mounted, setMounted] = useState(show)
  const elRef = useRef<HTMLDivElement>(null)
  // 退场兜底定时器引用：重新打开时必须取消，否则「移出再快速移入」时
  // 上一次退场注册的延迟卸载会把新打开的面板误杀（hover 菜单打不开的关键 bug）
  const exitTimerRef = useRef<gsap.core.Tween | null>(null)

  // 挂载：不用渲染期 setState（React 19 在事件触发的父渲染路径下可能不同步重渲子组件，
  // 导致面板永远不显示）。用异步 effect 挂载，同时避开 effect 内同步 setState 规则。
  useEffect(() => {
    if (!show || mounted) return
    const t = setTimeout(() => setMounted(true), 0)
    return () => clearTimeout(t)
  }, [show, mounted])

  useEffect(() => {
    const el = elRef.current
    if (!el) return
    if (show) {
      // 取消上一次退场的兜底卸载定时器，避免误杀本次新打开的面板
      exitTimerRef.current?.kill()
      exitTimerRef.current = null
      if (prefersReducedMotion()) return
      gsap.killTweensOf(el)
      const from: gsap.TweenVars = { opacity: 0 }
      if (y !== undefined) from.y = y
      if (x !== undefined) from.x = x
      if (scale !== undefined) from.scale = scale
      gsap.fromTo(el, from, {
        opacity: 1,
        y: 0,
        x: 0,
        scale: 1,
        duration,
        delay,
        ease: easeOut,
        overwrite: 'auto',
        clearProps: 'opacity,y,x,scale',
      })
      // 入场看门狗由全局 patch（gsap.fromTo）统一注册，动画完成即取消
    } else {
      if (prefersReducedMotion()) {
        // 延迟一帧卸载：异步回调里 setState，避开 effect 内同步 setState
        gsap.delayedCall(0, () => setMounted(false))
        return
      }
      const to: gsap.TweenVars = { opacity: 0 }
      if (y !== undefined) to.y = y
      if (x !== undefined) to.x = x
      if (scale !== undefined) to.scale = scale
      const exitDur = exitDuration ?? Math.max(0.12, duration * 0.8)
      gsap.to(el, {
        ...to,
        duration: exitDur,
        ease: 'power2.in',
        overwrite: 'auto',
        onComplete: () => {
          setMounted(false)
          exitTimerRef.current = null
        },
      })
      // 兜底：tween 被中断时（rAF 不推进）超时也卸载，避免弹层卡死
      exitTimerRef.current?.kill()
      exitTimerRef.current = gsap.delayedCall(exitDur + 0.8, () => {
        setMounted(false)
        exitTimerRef.current = null
      })
    }
    return () => {
      exitTimerRef.current?.kill()
      exitTimerRef.current = null
    }
  }, [show, duration, exitDuration, delay, y, x, scale])

  if (!mounted) return null

  return (
    <div ref={elRef} className={className} style={style} {...rest}>
      {children}
    </div>
  )
}

/** 数字滚动动画（替代 framer-motion 的 animate() 计数） */
export function CountUp({ value, duration = 0.9 }: { value: number; duration?: number }) {
  const ref = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    if (prefersReducedMotion()) {
      el.textContent = String(value)
      return
    }
    const obj = { v: 0 }
    const tween = gsap.to(obj, {
      v: value,
      duration,
      ease: easeOut,
      onUpdate: () => {
        el.textContent = String(Math.round(obj.v))
      },
      onComplete: () => {
        el.textContent = String(value)
      },
    })
    // 兜底：tween 被中断时（rAF 不推进）超时直接落最终值，避免数字停在 0
    const timer = setTimeout(() => {
      el.textContent = String(value)
    }, (duration + 1) * 1000)
    return () => {
      tween.kill()
      clearTimeout(timer)
    }
  }, [value, duration])
  return <span ref={ref}>{value}</span>
}

