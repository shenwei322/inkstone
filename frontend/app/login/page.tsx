'use client'

import { FormEvent, useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import gsap from 'gsap'
import { useAuth } from '@/lib/auth-context'
import { ApiError, type CaptchaCredential } from '@/lib/api'
import { useSiteConfig } from '@/components/site-config-context'
import { EmailCodeInput } from '@/components/email-code-input'
import { Checkbox } from '@/components/form-controls'
import { useCaptcha } from '@/components/captcha'
import { Reveal, hoverTapScale, prefersReducedMotion, useReveal } from '@/components/motion'
import { inputClass } from '@/lib/ui'

export default function LoginPage() {
  const { login } = useAuth()
  const site = useSiteConfig()
  const router = useRouter()
  const captcha = useCaptcha('login')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [emailCode, setEmailCode] = useState('')
  // 默认勾选：「每次访问都要重新登录」是这个页面最常被抱怨的问题，
  // 默认免登录符合博客站点的普遍预期。取消勾选则退化为会话级登录
  // （关掉浏览器即需重登），供共用电脑的场景使用。
  const [rememberMe, setRememberMe] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  // 表单入场（原 motion.form）
  const formRef = useRef<HTMLFormElement>(null)
  useReveal(formRef, { y: 12, delay: 0.25, duration: 0.4 })

  // 错误提示出现时横向滑入（原 motion.p，条件渲染故用命令式动画）
  const errorRef = useRef<HTMLParagraphElement>(null)
  useEffect(() => {
    if (!error || !errorRef.current || prefersReducedMotion()) return
    gsap.from(errorRef.current, { opacity: 0, x: -8, duration: 0.3, ease: 'expo.out' })
  }, [error])

  const doLogin = async (credential?: CaptchaCredential) => {
    setSubmitting(true)
    try {
      const loggedIn = await login(email, password, {
        email_code: emailCode,
        remember_me: rememberMe,
        ...credential,
      })
      router.push(loggedIn.role === 'admin' ? '/admin' : '/')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '登录失败，请稍后重试')
      setSubmitting(false)
    }
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)

    // 开启人机验证时先弹窗验证，通过后携带凭证提交
    if (captcha.enabled) {
      setSubmitting(true)
      let credential: CaptchaCredential
      try {
        credential = await captcha.run()
      } catch (err) {
        setSubmitting(false)
        // 用户主动关闭验证框时不展示错误提示
        const msg = err instanceof Error ? err.message : '人机验证未完成'
        if (msg !== '人机验证已取消') setError(msg)
        return
      }
      await doLogin(credential)
      return
    }
    await doLogin()
  }

    return (
    <div className="relative flex min-h-full items-center justify-center overflow-hidden px-4 py-16">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_at_center,rgba(37,99,235,0.06),transparent_65%)]" />
      <Reveal
        y={24}
        scale={0.97}
        duration={0.5}
        className="relative w-full max-w-sm"
      >
        <div className="rounded-2xl border border-border bg-card p-8 shadow-xl shadow-black/5">
          <Reveal y={12} delay={0.15} duration={0.4}>
            <h1 className="text-2xl font-bold tracking-tight">欢迎回来</h1>
            <p className="mt-1.5 text-sm text-muted-foreground">
              还没有账号？{' '}
              <Link href="/register" className="font-medium text-accent hover:underline underline-offset-4">
                立即注册
              </Link>
            </p>
          </Reveal>

          <form
            ref={formRef}
            onSubmit={handleSubmit}
            className="mt-8 space-y-5"
          >
            <div className="space-y-1.5">
              <label htmlFor="email" className="text-sm font-medium">
                邮箱
              </label>
              <input
                id="email"
                type="email"
                required
                autoComplete="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="you@example.com"
                className={inputClass}
              />
            </div>
            <div className="space-y-1.5">
              <label htmlFor="password" className="text-sm font-medium">
                密码
              </label>
              <input
                id="password"
                type="password"
                required
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="••••••••"
                className={inputClass}
              />
            </div>

            {site.emailCode.on_login && (
              <EmailCodeInput
                email={email}
                purpose="login"
                value={emailCode}
                onChange={setEmailCode}
              />
            )}

            {/* 「记住我」：勾选后 30 天免登录。文案刻意写明是「30 天」而不是
                含糊的「记住我」——用户需要知道这个勾选到底把登录态延长多久，
                这也是它与「不勾选」的唯一区别（后者关掉浏览器即失效）。
                控件本体用 components/form-controls 的 Checkbox：原生 checkbox
                各平台渲染差异大且无法跟随主题，该项目内所有勾选框统一走它。 */}
            <Checkbox
              checked={rememberMe}
              onChange={setRememberMe}
              label={
                <>
                  记住我
                  <span className="ml-1.5 text-xs font-normal text-muted-foreground">
                    （30 天内无需重新登录；共用电脑请取消勾选）
                  </span>
                </>
              }
            />

            {error && (
              <p
                ref={errorRef}
                className="rounded-lg border border-red-200 bg-red-50 px-3.5 py-2.5 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-300"
              >
                {error}
              </p>
            )}

            <button
              type="submit"
              disabled={submitting}
              {...hoverTapScale}
              className="w-full rounded-lg bg-accent py-2.5 text-sm font-medium text-white shadow-lg shadow-accent/25 disabled:opacity-60"
            >
              {submitting ? (
                <span className="inline-flex items-center gap-2">
                  <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-white/30 border-t-white" />
                  登录中...
                </span>
              ) : (
                '登录'
              )}
            </button>
          </form>
        </div>
      </Reveal>
      {captcha.dialog}
      {captcha.prewarmNode}
    </div>
  )
}
