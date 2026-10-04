'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { CheckCircle2, Cpu, ShieldCheck } from 'lucide-react'
import { useSiteConfig } from './site-config-context'
import { useIsMounted } from '@/lib/use-mounted'
import { createDeferred } from '@/lib/deferred'
import type { CaptchaCredential } from '@/lib/api'

export type CaptchaScene = 'login' | 'register' | 'comment'

export interface LapCaptchaOptions {
  /**
   * 预热模式（登录/注册开启）：页面加载即把 widget 挂到屏幕外「渲染可见」容器，
   * 利用用户读表单/输密码的时间静默跑完 challenge → PoW → redeem；用户点提交
   * 时弹窗直接得到 done 实例，一点即过（省掉弹窗后 2.5s 触发等待 + 全部计算）。
   * 评论场景不开（文章页访客量大，避免每个访客都消耗一次 PoW 配额）。
   */
  prewarm?: boolean
}

/** lap-widget 自定义元素 solve 事件的 detail */
interface LapSolveDetail {
  token: string
}

/** lap-widget 自定义元素 error 事件的 detail */
interface LapErrorDetail {
  isLap?: boolean
  code?: string
  message?: string
}

interface Pending {
  resolve: (v: CaptchaCredential) => void
  reject: (e: Error) => void
  /** 自身 Promise：run() 被并发调用时复用它，避免覆盖后永远不 settle */
  promise: Promise<CaptchaCredential>
}

// Lap 全部上游（widget.js / wasm / challenge / redeem）都走 InkStone 后端的
// 同源代理（/api/v1/lap/*）：访客浏览器不直连 workers.dev，规避 DNS 污染/超时；
// 后端侧可用 lap_resolve_ip 固定 IP 兜底。
const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080/api/v1'
const LAP_PROXY = `${API_BASE}/lap`
const LAP_WIDGET_SCRIPT = `${LAP_PROXY}/widget.js`
const LAP_WASM_URL = `${LAP_PROXY}/wasm`

declare global {
  interface Window {
    /** widget.js 拉取 PoW WASM 的地址（默认 jsdelivr，我们改走本站代理） */
    LAP_CUSTOM_WASM_URL?: string
  }
}

/** widget.js 脚本按 URL 缓存：全站共享一份，重复注入无意义 */
const widgetScriptCache = new Map<string, Promise<void>>()

function ensureLapWidgetScript(url: string): Promise<void> {
  const cached = widgetScriptCache.get(url)
  if (cached) return cached
  const p = new Promise<void>((resolve, reject) => {
    const el = document.createElement('script')
    el.src = url
    el.async = true
    el.onload = () => resolve()
    el.onerror = () => {
      widgetScriptCache.delete(url)
      reject(new Error('验证组件加载失败，请刷新页面后重试'))
    }
    document.head.appendChild(el)
  })
  widgetScriptCache.set(url, p)
  return p
}

/**
 * 人机验证（Lap —— Cap 的 Cloudflare Workers 分支，工作量证明验证码）
 *
 * 两段式流程（Cap 设计）：挂载后由 mousemove/touchstart/keydown 触发静默
 * speculative（challenge → PoW → redeem，不派发事件）；**用户点击** widget
 * 才走 solve 快路径派发 solve 事件——若预热已提前完成 PoW，点击即过。
 *
 * provider 不是 lap 时本 hook 完全静默（不加载 widget.js），与
 * useGeetestCaptcha 互斥；由 components/captcha.tsx 的 useCaptcha 选用。
 */
export function useLapCaptcha(scene: CaptchaScene, opts: LapCaptchaOptions = {}) {
  const site = useSiteConfig()
  const { lap } = site

  const enabled =
    site.loaded &&
    site.captchaProvider === 'lap' &&
    lap.enabled &&
    Boolean(lap.site_key) &&
    Boolean(lap.api_endpoint) &&
    (scene === 'login'
      ? lap.on_login
      : scene === 'register'
        ? lap.on_register
        : lap.on_comment)

  const prewarm = enabled && opts.prewarm === true && Boolean(lap.site_key)

  const pendingRef = useRef<Pending | null>(null)
  /** 当前 lap-widget 实例（预热容器与弹窗容器之间移动，保留内部状态） */
  const widgetRef = useRef<HTMLElement | null>(null)
  /** solve 已成功（token 已消费，实例不可复用，关闭时必须重建） */
  const solvedRef = useRef(false)
  const prewarmBoxRef = useRef<HTMLDivElement>(null)
  const dialogBoxRef = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [succeeded, setSucceeded] = useState(false)
  const [closing, setClosing] = useState(false)
  const [progress, setProgress] = useState(-1)

  // 带退场动画的关闭：先播放 0.22s 退出动画再卸载
  const closeDialog = useCallback(() => {
    setClosing(true)
    window.setTimeout(() => {
      setOpen(false)
      setClosing(false)
      setSucceeded(false)
      setProgress(-1)
    }, 220)
  }, [])

  /** 创建 widget 实例并挂到指定容器（solve/error/progress 监听只挂一次） */
  const mountWidget = useCallback(
    (parent: HTMLElement) => {
      const widget = document.createElement('lap-widget')
      // challenge/redeem 同样走后端代理，浏览器全程不接触 workers.dev
      widget.setAttribute('data-lap-api-endpoint', `${LAP_PROXY}/${lap.site_key}/`)
      widget.setAttribute('data-lap-lang', 'zh-cn')
      widget.addEventListener('solve', (e) => {
        const token = (e as CustomEvent<LapSolveDetail>).detail?.token ?? ''
        if (!token) return
        const pending = pendingRef.current
        pendingRef.current = null
        solvedRef.current = true
        setProgress(100)
        // 先展示「验证成功」状态，短暂确认后播放退场动画自动关闭
        // （resolve 已同步派发，业务请求在后台继续跑，不等这 600ms）
        setSucceeded(true)
        window.setTimeout(closeDialog, 600)
        pending?.resolve({ lap_token: token })
      })
      widget.addEventListener('error', (e: Event) => {
        const detail = (e as CustomEvent<LapErrorDetail>).detail
        const pending = pendingRef.current
        pendingRef.current = null
        // 实例不可用（challenge/网络错误）：销毁等下次重建，不阻塞其他场景
        widget.remove()
        if (widgetRef.current === widget) widgetRef.current = null
        closeDialog()
        pending?.reject(new Error(detail?.message || '人机验证服务异常，请稍后重试'))
      })
      widget.addEventListener('progress', (e: Event) => {
        const p = (e as CustomEvent<{ progress: number }>).detail?.progress
        if (typeof p === 'number') setProgress(p)
      })
      parent.replaceChildren()
      parent.appendChild(widget)
      widgetRef.current = widget
      return widget
    },
    [lap.site_key, closeDialog],
  )

  /** 销毁当前实例（token 已消费或出错），供重建 */
  const disposeWidget = useCallback(() => {
    widgetRef.current?.remove()
    widgetRef.current = null
    solvedRef.current = false
    setProgress(-1)
  }, [])

  // 开启 Lap 时即预加载脚本 + 把 PoW 的 WASM 地址指到本站代理
  useEffect(() => {
    if (!enabled) return
    window.LAP_CUSTOM_WASM_URL = LAP_WASM_URL
    void ensureLapWidgetScript(LAP_WIDGET_SCRIPT)
  }, [enabled])

  // 预热（login/register）：widget 挂到屏幕外但「渲染可见」的容器
  // （checkVisibility 对 opacity:1 + 非 display:none 返回 true，视口外无妨），
  // 用户还在填表单时 PoW 已静默完成
  useEffect(() => {
    if (!prewarm) return
    const box = prewarmBoxRef.current
    if (!box || widgetRef.current) return
    void ensureLapWidgetScript(LAP_WIDGET_SCRIPT)
      .then(() => {
        if (prewarmBoxRef.current && !widgetRef.current) {
          mountWidget(prewarmBoxRef.current)
        }
      })
      .catch(() => {
        // 脚本加载失败：用户点提交时会走 dialog 路径再报错
      })
  }, [prewarm, mountWidget])

  // 弹窗打开：把预热实例（可能已 done）移进弹窗；没有则现场创建
  useEffect(() => {
    if (!open) return
    const box = dialogBoxRef.current
    if (!box) return
    let cancelled = false
    void ensureLapWidgetScript(LAP_WIDGET_SCRIPT)
      .then(() => {
        if (cancelled) return
        const existing = widgetRef.current
        if (existing) {
          // 移动而非重建：保留 speculative 已完成的状态（一点即过）
          box.appendChild(existing)
        } else {
          mountWidget(box)
        }
      })
      .catch(() => {
        if (cancelled) return
        const pending = pendingRef.current
        pendingRef.current = null
        closeDialog()
        pending?.reject(new Error('验证组件加载失败，请刷新页面后重试'))
      })
    return () => {
      cancelled = true
    }
  }, [open, mountWidget, closeDialog])

  // 弹窗关闭后：solve 成功过的实例必须销毁（token 已消费），否则下次
  // 打开会拿旧 token 被后端拒绝；未消费的实例移回屏幕外继续预热态
  useEffect(() => {
    if (open) return
    if (solvedRef.current) {
      disposeWidget()
      // 重新挂一个干净实例，保持下一轮登录的预热
      const box = prewarmBoxRef.current
      if (prewarm && box && !widgetRef.current) {
        void ensureLapWidgetScript(LAP_WIDGET_SCRIPT)
          .then(() => {
            if (prewarmBoxRef.current && !widgetRef.current) {
              mountWidget(prewarmBoxRef.current)
            }
          })
          .catch(() => {})
      }
      return
    }
    const widget = widgetRef.current
    const box = prewarmBoxRef.current
    if (prewarm && widget && box && widget.parentElement !== box) {
      box.appendChild(widget)
    }
  }, [open, prewarm, mountWidget, disposeWidget])

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

  const run = useCallback((): Promise<CaptchaCredential> => {
    // 并发调用复用同一个 pending：直接覆盖 pendingRef.current 会让被覆盖的
    // Promise 永远不 settle，其 await 永久挂起（提交按钮卡在 loading）。
    const existing = pendingRef.current
    if (existing) return existing.promise

    const d = createDeferred<CaptchaCredential>()
    // 先占住 pending：widget 脚本是异步加载的，加载期间再来的重复调用
    // 也必须拿到同一个 Promise，而不是各自开一个弹窗。
    pendingRef.current = { resolve: d.resolve, reject: d.reject, promise: d.promise }
    // 先确保 widget.js 就绪再开弹窗，避免自定义元素尚未定义时挂载失效
    ensureLapWidgetScript(LAP_WIDGET_SCRIPT)
      .then(() => setOpen(true))
      .catch((e) => {
        pendingRef.current = null
        d.reject(e instanceof Error ? e : new Error('人机验证组件加载失败，请刷新页面后重试'))
      })
    return d.promise
  }, [])

  const solving = progress >= 0 && progress < 100

  // 弹窗容器常驻 DOM（display 切换显隐）：lap-widget 打开时移入、关闭时移出，
  // 卸载再打开不丢状态
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
        {/* 默认内容：验证中（成功后整体隐藏，但容器保留 DOM） */}
        <div style={{ display: succeeded ? 'none' : 'block' }}>
          <div className="captcha-check-pop mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-accent/10 text-accent">
            <Cpu className="h-6 w-6" strokeWidth={2} />
          </div>
          <p className="mt-3 text-sm font-semibold">人机验证</p>
          <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
            {solving
              ? `正在本地计算工作量证明… ${progress}%`
              : '计算已在后台自动完成，点击下方按钮即可通过验证'}
          </p>
          {/* widget 自带浅色主题 UI，给一层白底容器保证任何站点配色下可读 */}
          <div
            ref={dialogBoxRef}
            data-lap-box="true"
            className="mt-5 flex min-h-[64px] items-center justify-center rounded-xl bg-white px-3 py-2"
          />
          <p className="mt-3 flex items-center justify-center gap-1 text-[11px] text-muted-foreground">
            <ShieldCheck className="h-3 w-3" />
            证明计算在你的浏览器本地进行，不上传行为数据
          </p>
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
    // 预热容器：屏幕外但保持渲染可见（opacity:1 / 非 display:none），
    // widget 的 checkVisibility 才会放行 speculative
    prewarmNode: prewarm ? (
      <div
        ref={prewarmBoxRef}
        aria-hidden
        className="pointer-events-none fixed left-[-9999px] top-0 h-px w-px opacity-100"
      />
    ) : null,
    // 必须 portal 到 body：调用方页面（如友链申请表单）外层常是带 transform 动画的
    // motion.div，内联渲染 fixed 遮罩会被 transform 包含块困住——inset-0 只覆盖
    // 表单卡片区域，出现「只有提交窗口模糊」；与 components/modal.tsx 同策略。
    dialog: mounted ? createPortal(dialogNode, document.body) : null,
  }
}
