'use client'

import { FormEvent, useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import gsap from 'gsap'
import { type CaptchaCredential } from '@/lib/api'
import { useAuth } from '@/lib/auth-context'
import { ApiError } from '@/lib/api'
import { useSiteConfig } from '@/components/site-config-context'
import { EmailCodeInput } from '@/components/email-code-input'
import { useCaptcha } from '@/components/captcha'
import { PageLoading } from '@/components/page-loader'
import { Reveal, hoverTapScale, prefersReducedMotion, useReveal } from '@/components/motion'
import { inputClass } from '@/lib/ui'

export default function RegisterPage() {
  const { register } = useAuth()
  const site = useSiteConfig()
  const router = useRouter()
  const captcha = useCaptcha('register')
  const [emailCode, setEmailCode] = useState('')
  // 复用全局站点配置（SiteConfigProvider）而不是再发一次 /site-config：
  // 此前这里自己 useQuery，既重复请求，又因首帧 data 为 undefined 而
  // 把 allow_registration 误判为 true（关闭注册时先渲染出表单再突然替换）。
  const registrationOpen = site.allowRegistration
  const [email, setEmail] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
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

  const doRegister = async (credential?: CaptchaCredential) => {
    setSubmitting(true)
    try {
      const newUser = await register(email, username, password, {
        email_code: emailCode,
        ...credential,
      })
      router.push(newUser.role === 'admin' ? '/admin' : '/')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '注册失败，请稍后重试')
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
      await doRegister(credential)
      return
    }
    await doRegister()
  }

    const fields = [
    { id: 'email', label: '邮箱', type: 'email', value: email, set: setEmail, placeholder: 'you@example.com', auto: 'email', min: undefined as number | undefined, max: undefined as number | undefined },
    { id: 'username', label: '用户名', type: 'text', value: username, set: setUsername, placeholder: '2-32 个字符', auto: 'username', min: 2, max: 32 },
    { id: 'password', label: '密码', type: 'password', value: password, set: setPassword, placeholder: '至少 8 个字符', auto: 'new-password', min: 8, max: 72 },
  ]

  // 配置未就绪时先显示加载态：否则会按默认值（开放注册）先渲染出表单，
  // 等配置回来再整页替换成「暂未开放注册」，造成布局跳变与无效填写。
  if (!site.loaded) {
    return (
      <div className="flex min-h-full items-center justify-center px-4 py-16">
        <PageLoading minHeight="12rem" hint="正在加载…" />
      </div>
    )
  }

  if (!registrationOpen) {
    return (
      <div className="relative flex min-h-full items-center justify-center px-4 py-16">
        <Reveal
          y={16}
          className="w-full max-w-sm rounded-2xl border border-border bg-card p-8 text-center shadow-xl shadow-black/5"
        >
          <h1 className="text-xl font-bold">暂未开放注册</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            网站已关闭用户注册，如需账号请联系站长。
          </p>
          <Link
            href="/login"
            className="mt-6 inline-block rounded-lg bg-accent px-6 py-2.5 text-sm font-medium text-white transition-transform hover:scale-105"
          >
            去登录
          </Link>
        </Reveal>
      </div>
    )
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
            <h1 className="text-2xl font-bold tracking-tight">创建账号</h1>
            <p className="mt-1.5 text-sm text-muted-foreground">
              已有账号？{' '}
              <Link href="/login" className="font-medium text-accent hover:underline underline-offset-4">
                直接登录
              </Link>
            </p>
          </Reveal>

          <form
            ref={formRef}
            onSubmit={handleSubmit}
            className="mt-8 space-y-5"
          >
            {fields.map((f, i) => (
              <Reveal
                key={f.id}
                x={-12}
                delay={0.2 + i * 0.05}
                duration={0.3}
                className="space-y-1.5"
              >
                <label htmlFor={f.id} className="text-sm font-medium">
                  {f.label}
                </label>
                <input
                  id={f.id}
                  type={f.type}
                  required
                  autoComplete={f.auto}
                  minLength={f.min}
                  maxLength={f.max}
                  value={f.value}
                  onChange={(e) => f.set(e.target.value)}
                  placeholder={f.placeholder}
                  className={inputClass}
                />
              </Reveal>
            ))}

            {site.emailCode.on_register && (
              <EmailCodeInput
                email={email}
                purpose="register"
                value={emailCode}
                onChange={setEmailCode}
              />
            )}

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
                  注册中...
                </span>
              ) : (
                '注册'
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
