'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { CheckCircle2, ShieldCheck } from 'lucide-react'
import { useSiteConfig } from './site-config-context'
import { useIsMounted } from '@/lib/use-mounted'
import type { GeetestCredential } from '@/lib/api'

/** 极验 gt4.js 验证实例暴露的最小接口 */
interface GeetestInstance {
  appendTo: (el: HTMLElement) => void
  showBox: () => void
  reset: () => void
  onSuccess: (cb: () => void) => void
  onError: (cb: (err: { code: number; msg: string }) => void) => void
  onClose: (cb: () => void) => void
  getValidate: () => GeetestCredential
}

declare global {
  interface Window {
    initGeetest4?: (
      config: { captchaId: string; product?: 'popup' | 'float' | 'bind' },
      cb: (captcha: GeetestInstance) => void,
    ) => void
  }
}

export type GeetestScene = 'login' | 'register' | 'comment'

interface Pending {
  resolve: (v: GeetestCredential) => void
  reject: (e: Error) => void
}

const INIT_RETRY_MS = 50
const INIT_MAX_TRIES = 200 // 50ms × 200 次 ≈ 10 秒仍未就绪则放弃，调用方会提示刷新

/**
 * 人机验证（极验第四代行为验证）
 *
 * 页面加载时即初始化（gt4.js 需与业务页面同步加载，否则采集不到行为数据）。
 * 调用 run() 弹出本站验证弹窗，用户点击其中的极验入口按钮后由极验面板接管，
 * 验证完成后 resolve 出凭证供后端二次校验。
 *
 * 注意：GT4 的验证面板只能由用户真实点击入口按钮触发（无法用 JS 代弹），
 * 因此交互是两步——点登录 → 弹验证入口 → 用户点入口完成滑块/点选。
 */
export function useGeetestCaptcha(scene: GeetestScene) {
  const site = useSiteConfig()
  const { geetest } = site

  const enabled =
    site.loaded &&
    site.captchaProvider !== 'lap' &&
    site.captchaProvider !== 'pow' &&
    geetest.enabled &&
    Boolean(geetest.captcha_id) &&
    (scene === 'login'
      ? geetest.on_login
      : scene === 'register'
        ? geetest.on_register
        : geetest.on_comment)

  const instanceRef = useRef<GeetestInstance | null>(null)
  const pendingRef = useRef<Pending | null>(null)
  const [open, setOpen] = useState(false)
  const [succeeded, setSucceeded] = useState(false)
  const [closing, setClosing] = useState(false)
  const boxRef = useRef<HTMLDivElement>(null)

  // 带退场动画的关闭：先播放 0.22s 退出动画再卸载
  const closeDialog = useCallback(() => {
    setClosing(true)
    window.setTimeout(() => {
      setOpen(false)
      setClosing(false)
      setSucceeded(false)
    }, 220)
  }, [])

  useEffect(() => {
    if (!enabled) return
    let cancelled = false
    let tries = 0
    const tryInit = () => {
      if (cancelled) return
      if (typeof window !== 'undefined' && typeof window.initGeetest4 === 'function') {
        window.initGeetest4(
          { captchaId: geetest.captcha_id, product: 'popup' },
          (captcha) => {
            if (cancelled) return
            instanceRef.current = captcha
            captcha.onSuccess(() => {
              const pending = pendingRef.current
              const credential = captcha.getValidate()
              // 必须 reset：否则实例停留在「已通过」状态，入口按钮一直显示
              // 验证成功，后续场景再次验证时不会再触发 onSuccess，提交会永久挂起
              captcha.reset()
              pendingRef.current = null
              // 先展示「验证成功」状态，约 1 秒后播放退场动画自动关闭
              setSucceeded(true)
              window.setTimeout(() => closeDialog(), 1000)
              pending?.resolve(credential)
            })
            captcha.onError(() => {
              const pending = pendingRef.current
              captcha.reset()
              pendingRef.current = null
              closeDialog()
              pending?.reject(new Error('人机验证加载失败，请刷新页面后重试'))
            })
            captcha.onClose(() => {
              // 用户关闭验证面板（未完成）；reset 让实例恢复可再次验证
              const pending = pendingRef.current
              captcha.reset()
              pendingRef.current = null
              closeDialog()
              pending?.reject(new Error('人机验证已取消'))
            })
          },
        )
        return
      }
      // gt4.js 尚未加载完成，稍后重试
      if (++tries > INIT_MAX_TRIES) return
      setTimeout(tryInit, INIT_RETRY_MS)
    }
    tryInit()
    return () => {
      cancelled = true
      instanceRef.current = null
    }
  }, [enabled, geetest.captcha_id, closeDialog])

  // 极验入口只需挂载一次：reset() 会重置实例内部状态，之后再次 appendTo
  // 会访问已清空的内部属性而抛错；reset 后入口 DOM 仍保留，恢复为未验证态。
  const appendedRef = useRef(false)
  useEffect(() => {
    if (!open) return
    const captcha = instanceRef.current
    const box = boxRef.current
    if (!captcha || !box) return
    // React 重建了容器 DOM（如 HMR/重挂载）导致入口丢失时，允许重新挂载
    const hasEntry = box.querySelector('[class*="geetest_captcha"]') !== null
    if (appendedRef.current && hasEntry) return
    if (appendedRef.current && !hasEntry) appendedRef.current = false
    captcha.appendTo(box)
    appendedRef.current = true
  }, [open])

  // 弹窗打开时锁 body 滚动 + ESC 关闭（与全站 Modal 体验一致）
  useEffect(() => {
    if (!open) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
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

  const run = useCallback((): Promise<GeetestCredential> => {
    const captcha = instanceRef.current
    if (!captcha) {
      return Promise.reject(new Error('人机验证组件未加载完成，请刷新页面后重试'))
    }
    return new Promise((resolve, reject) => {
      pendingRef.current = { resolve, reject }
      setOpen(true)
    })
  }, [])

  // 弹窗容器常驻 DOM（display 切换显隐）：极验入口挂载后不能重复 appendTo，
  // 卸载再挂载会丢入口 DOM，reset 也无法补救
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
        className={`w-[340px] max-w-full rounded-2xl border border-border bg-card p-6 text-center shadow-2xl shadow-black/25${
          closing ? ' captcha-closing-card' : ''
        }`}
      >
        {/* 默认内容：验证中（成功后整体隐藏，但入口容器保留 DOM） */}
        <div style={{ display: succeeded ? 'none' : 'block' }}>
          <div className="captcha-check-pop mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-accent/10 text-accent">
            <ShieldCheck className="h-6 w-6" strokeWidth={2} />
          </div>
          <p className="mt-3 text-sm font-semibold">人机验证</p>
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            请点击下方按钮完成验证，通过后将自动继续
          </p>
          <div ref={boxRef} data-geetest-box="true" className="mt-5 flex min-h-[52px] items-center justify-center" />
          <button
            type="button"
            onClick={() => {
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

        {/* 验证成功状态 */}
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
    // 必须 portal 到 body：调用方页面（如友链申请表单）外层常是带 transform 动画的
    // motion.div，内联渲染 fixed 遮罩会被 transform 包含块困住——inset-0 只覆盖
    // 表单卡片区域，出现「只有提交窗口模糊」；与 components/modal.tsx 同策略。
    dialog: mounted ? createPortal(dialogNode, document.body) : null,
  }
}
