'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { CheckCircle2, Cpu, MousePointer, ShieldCheck } from 'lucide-react'
import { fetchPowChallenge, type CaptchaCredential } from '@/lib/api'
import { useSiteConfig } from './site-config-context'
import { useIsMounted } from '@/lib/use-mounted'
import { createDeferred } from '@/lib/deferred'
import { solvePow } from '@/lib/pow'

export type CaptchaScene = 'login' | 'register' | 'comment'

interface Pending {
  resolve: (v: CaptchaCredential) => void
  reject: (e: Error) => void
  /** 自身 Promise：run() 被并发调用时复用它，避免覆盖后永远不 settle */
  promise: Promise<CaptchaCredential>
}

/** 本地交互事件单条：`{m|k|t}:{unix_ms}:{x}:{y}` */
function formatPowEvent(e: Event): string {
  const now = Date.now()
  const clamp = (v: number) => Math.max(-32767, Math.min(32767, Math.round(v)))
  if (e.type === 'keydown') return `k:${now}:0:0`
  if (e.type === 'touchstart') {
    const t = (e as TouchEvent).changedTouches?.[0]
    return `t:${now}:${clamp(t?.clientX ?? 0)}:${clamp(t?.clientY ?? 0)}`
  }
  const p = e as PointerEvent
  return `m:${now}:${clamp(p.clientX ?? 0)}:${clamp(p.clientY ?? 0)}`
}

/**
 * 采集 n 个本地交互事件（鼠标移动 / 触摸 / 按键），聚合成 signal 字符串。
 * 这些事件流是真人设备自然产生的——纯 HTTP 脚本（curl/requests）不带这个
 * 信息，必须额外复刻整套构造逻辑才能过服务端格式与时间窗校验。
 * cancelled() 为 true 或 30 秒未采集够时 reject。
 */
function collectPowSignal(n: number, cancelled: () => boolean): Promise<string> {
  return new Promise<string>((resolve, reject) => {
    const events: string[] = []
    let done = false

    const cleanup = () => {
      done = true
      window.removeEventListener('pointermove', onEvent)
      window.removeEventListener('keydown', onEvent)
      window.removeEventListener('touchstart', onEvent)
      window.clearInterval(poll)
      window.clearTimeout(timeout)
    }

    function onEvent(e: Event) {
      if (done || events.length >= n) return
      events.push(formatPowEvent(e))
      if (events.length >= n) {
        cleanup()
        resolve(events.join(','))
      }
    }

    const poll = window.setInterval(() => {
      if (done) return
      if (cancelled()) {
        cleanup()
        reject(new Error('__cancelled__'))
      }
    }, 200)
    const timeout = window.setTimeout(() => {
      if (done) return
      cleanup()
      reject(new Error('__signal_timeout__'))
    }, 30000)

    window.addEventListener('pointermove', onEvent, { passive: true })
    window.addEventListener('keydown', onEvent, { passive: true })
    window.addEventListener('touchstart', onEvent, { passive: true })
  })
}

/**
 * POW 人机验证（自研工作量证明 v2，零外部依赖）。
 *
 * 两阶段链路（消耗的都是用户本地资源）：
 *   1. **信号采集**：POST /pow/challenge 后，要求用户真实晃动鼠标/触摸/按键
 *      min_events 次（真人设备独有，脚本难伪造）；
 *   2. **本地计算**：构建 memoryMB 内存表并多轮「查表-混合」迭代 SHA-256
 *      找出前导零答案（内存带宽成本拖慢集群并行），完成后凭
 *      {challenge, nonce, signal} 继续业务请求。
 *
 * 与 lap/geetest 同一门面（components/captcha.tsx）：未选中时 enabled=false，
 * 不弹窗、不发任何请求。
 */
export function usePowCaptcha(scene: CaptchaScene) {
  const site = useSiteConfig()
  const { pow } = site

  const enabled =
    site.loaded &&
    site.captchaProvider === 'pow' &&
    pow.enabled &&
    (scene === 'login'
      ? pow.on_login
      : scene === 'register'
        ? pow.on_register
        : pow.on_comment)

  /** solving：计算中；signal：等待本地交互信号 */
  const [open, setOpen] = useState(false)
  const [succeeded, setSucceeded] = useState(false)
  const [closing, setClosing] = useState(false)
  const [signalLeft, setSignalLeft] = useState(0)
  const [progress, setProgress] = useState(-1)

  const pendingRef = useRef<Pending | null>(null)
  /** 用户主动取消（弹窗关闭/卸载）时通知采集/求解停止 */
  const cancelledRef = useRef(false)

  // 带退场动画的关闭：先播放 0.22s 退出动画再卸载（与 lap 弹窗同节奏）
  const closeDialog = useCallback(() => {
    setClosing(true)
    window.setTimeout(() => {
      setOpen(false)
      setClosing(false)
      setSucceeded(false)
      setSignalLeft(0)
      setProgress(-1)
    }, 220)
  }, [])

  // 弹窗打开：领 challenge → 采集交互信号 → 本地计算 → 成功回执
  useEffect(() => {
    if (!open) return
    let cancelled = false
    cancelledRef.current = false

    void (async () => {
      try {
        // 带上场景：后端会把挑战绑定到该场景，跨场景挪用一律失效
        const ch = await fetchPowChallenge(scene)
        if (cancelled) return
        // 第一阶段：本地交互信号（min_events=0 时跳过，进入纯算法模式）
        let signal = ''
        if (ch.min_events > 0) {
          setSignalLeft(ch.min_events)
          signal = await collectPowSignal(ch.min_events, () => cancelled || cancelledRef.current)
          if (cancelled) return
        }
        setSignalLeft(0)
        // 第二阶段：内存表 + 多轮查表混合的本地计算
        setProgress(0)
        const nonce = await solvePow({
          challenge: ch.challenge,
          difficulty: ch.difficulty,
          memoryMB: ch.memory_mb,
          rounds: ch.rounds,
          onProgress: (p) => {
            if (!cancelled) setProgress(p)
          },
          cancelled: () => cancelled || cancelledRef.current,
        })
        if (cancelled) return
        setProgress(100)
        setSucceeded(true)
        const pending = pendingRef.current
        pendingRef.current = null
        // 先展示「验证成功」状态，短暂确认后播放退场动画自动关闭
        window.setTimeout(() => {
          if (cancelled) return
          closeDialog()
          pending?.resolve({ pow_challenge: ch.challenge, pow_nonce: nonce, pow_signal: signal })
        }, 500)
      } catch (err) {
        if (cancelled) return
        closeDialog()
        const pending = pendingRef.current
        pendingRef.current = null
        const msg = err instanceof Error ? err.message : ''
        const cancelledByUser = msg === '__cancelled__'
        const signalTimeout = msg === '__signal_timeout__'
        pending?.reject(
          new Error(
            cancelledByUser
              ? '人机验证已取消'
              : signalTimeout
                ? '未检测到鼠标/触摸操作，请重试'
                : '人机验证服务异常，请稍后重试',
          ),
        )
      }
    })()

    return () => {
      cancelled = true
      cancelledRef.current = true
    }
  // scene 参与 fetchPowChallenge(scene)：后端把挑战绑定到场景上，换场景必须
  // 重新领挑战。放进依赖数组，否则组件复用时仍会用旧场景的挑战、
  // 被后端判为跨场景挪用而失败。
  }, [open, closeDialog, scene])

  // 弹窗打开时锁 body 滚动 + ESC 关闭（与全站 Modal / lap 弹窗体验一致）
  useEffect(() => {
    if (!open) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      cancelledRef.current = true
      const pending = pendingRef.current
      pendingRef.current = null
      closeDialog()
      pending?.reject(new Error('人机验证已取消'))
    }
    window.addEventListener('keydown', onKey)
    return () => {
      document.body.style.overflow = prev
      window.removeEventListener('keydown', onKey)
    }
  }, [open, closeDialog])

  const run = useCallback(() => {
    // 并发调用必须复用同一个 pending。
    //
    // 此前这里直接 `pendingRef.current = { resolve, reject }`，若弹窗已经
    // 打开时用户又点了一次提交，后一次会**覆盖**前一次的 pending：被覆盖的
    // 那个 Promise 永远不 resolve 也不 reject，它的 await 永久挂起——
    // 表现为提交按钮卡在 loading 状态、再也点不动，只能刷新页面。
    //
    // 现在把 promise 一起存进 pending，重复调用直接拿到同一个 Promise，
    // 两个 await 由同一次验证明证/失败一起唤醒。
    const existing = pendingRef.current
    if (existing) return existing.promise

    const d = createDeferred<CaptchaCredential>()
    pendingRef.current = { resolve: d.resolve, reject: d.reject, promise: d.promise }
    cancelledRef.current = false
    setSignalLeft(pow.min_events)
    setOpen(true)
    return d.promise
  }, [pow.min_events])

  const waitingSignal = signalLeft > 0 && progress < 0
  // 进度条用 scaleX（origin-left）而非 width：避免每帧触发 layout 重排。
  const barScale = Math.max(progress, 0) / 100

  const dialogNode = (
    <div
      className={`fixed inset-0 z-[210] flex items-center justify-center bg-black/50 p-4 backdrop-blur-sm${
        closing ? ' captcha-closing-overlay' : ''
      }`}
      style={{ display: open ? 'flex' : 'none' }}
      aria-hidden={!open}
    >
      <div
        role="dialog"
        aria-modal="true"
        className={`w-[360px] max-w-full rounded-2xl border border-border bg-card p-6 text-center shadow-2xl shadow-black/25${
          closing ? ' captcha-closing-card' : ''
        }`}
      >
        <div style={{ display: succeeded ? 'none' : 'block' }}>
          <div className="captcha-check-pop mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-accent/10 text-accent">
            {waitingSignal ? (
              <MousePointer className="h-6 w-6" strokeWidth={2} />
            ) : (
              <Cpu className="h-6 w-6" strokeWidth={2} />
            )}
          </div>
          <p className="mt-3 text-sm font-semibold">人机验证</p>

          {waitingSignal ? (
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              请晃动鼠标或触摸屏幕（还需 {signalLeft} 次）
            </p>
          ) : (
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              {progress >= 100
                ? '计算完成'
                : `正在本地计算工作量证明… ${Math.max(progress, 0)}%`}
            </p>
          )}

          {/* 本地计算进度：scaleX 走合成器，不逐帧重排 */}
          <div className="mt-4 h-1.5 w-full overflow-hidden rounded-full bg-muted">
            <div
              className="h-full w-full origin-left rounded-full bg-accent transition-transform duration-200"
              style={{ transform: `scaleX(${barScale})` }}
            />
          </div>

          <p className="mt-3 flex items-center justify-center gap-1 text-[11px] text-muted-foreground">
            <ShieldCheck className="h-3 w-3" />
            计算与操作信号都在你的浏览器本地完成，不上传行为数据
          </p>
          <button
            type="button"
            onClick={() => {
              cancelledRef.current = true
              const pending = pendingRef.current
              pendingRef.current = null
              closeDialog()
              pending?.reject(new Error('人机验证已取消'))
            }}
            className="mt-3 text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            取消验证
          </button>
        </div>

        {succeeded && (
          <div className="animate-scale-in flex flex-col items-center gap-2 py-3">
            <CheckCircle2 className="captcha-check-pop h-11 w-11 text-emerald-500" strokeWidth={2} />
            <p className="text-sm font-medium text-emerald-600 dark:text-emerald-400">验证成功</p>
          </div>
        )}
      </div>
    </div>
  )

  const mounted = useIsMounted()

  return {
    enabled,
    run,
    // portal 到 body：调用方页面外层常是带 transform 动画的容器（如友链申请
    // 表单），内联 fixed 遮罩会被包含块困住（与 lap/Modal 同策略）
    dialog: mounted ? createPortal(dialogNode, document.body) : null,
  }
}
