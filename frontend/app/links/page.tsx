'use client'

import { useEffect, useRef, useState } from 'react'
import { useQuery, useMutation } from '@tanstack/react-query'
import gsap from 'gsap'
import { AlertTriangle, ExternalLink, Link2, Loader2, Send, ShieldOff } from 'lucide-react'
import { fetchFriendLinks, submitLinkApplication, ApiError } from '@/lib/api'
import { useNotify } from '@/components/toast'
import { RowLoading } from '@/components/page-loader'
import { PageTransition, StaggerList, StaggerItem, HoverLift, InView, Reveal, hoverTapScale, prefersReducedMotion, createLoop, releaseLoop, useReveal } from '@/components/motion'
import { useSiteConfig } from '@/components/site-config-context'
import { useCaptcha } from '@/components/captcha'
import { inputClass } from '@/lib/ui'


/** 友链自助申请表单（审核通过后自动加入列表） */
function ApplyForm() {
  const notify = useNotify()
  const captcha = useCaptcha('comment')
  const [form, setForm] = useState({
    site_name: '',
    url: '',
    description: '',
    icon_url: '',
    email: '',
  })

  const submit = useMutation({
    mutationFn: async () => {
      // 开启评论级人机验证时先过验证（与评论同场景，一次配置两处生效）
      const credential = captcha.enabled ? await captcha.run() : undefined
      return submitLinkApplication(form, credential)
    },
    onSuccess: () => {
      notify.success('申请已提交，站长审核通过后将展示')
      setForm({ site_name: '', url: '', description: '', icon_url: '', email: '' })
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '提交失败，请稍后再试'),
  })

  return (
    <InView
      y={16}
      duration={0.4}
      className="mt-8 rounded-2xl border border-border bg-card p-6"
    >
      <h2 className="flex items-center gap-2 text-sm font-semibold">
        <Link2 className="h-4 w-4 text-accent" />
        申请加入友链
      </h2>
      <p className="mt-1 text-xs text-muted-foreground">
        填写站点信息提交申请，站长审核通过后自动展示（每日自动检测，失效暂停跳转）
      </p>
      <form
        className="mt-4 grid gap-4 sm:grid-cols-2"
        onSubmit={(e) => {
          e.preventDefault()
          submit.mutate()
        }}
      >
        <label className="block">
          <span className="text-sm font-medium">站点名称 *</span>
          <input
            value={form.site_name}
            onChange={(e) => setForm((f) => ({ ...f, site_name: e.target.value }))}
            maxLength={50}
            placeholder="你的站点名称"
            className={`${inputClass} mt-1`}
          />
        </label>
        <label className="block">
          <span className="text-sm font-medium">站点地址 *</span>
          <input
            value={form.url}
            onChange={(e) => setForm((f) => ({ ...f, url: e.target.value }))}
            placeholder="https://example.com"
            className={`${inputClass} mt-1`}
          />
        </label>
        <label className="block sm:col-span-2">
          <span className="text-sm font-medium">简介（可选，120 字内）</span>
          <input
            value={form.description}
            onChange={(e) => setForm((f) => ({ ...f, description: e.target.value }))}
            maxLength={120}
            placeholder="一句话介绍你的站点"
            className={`${inputClass} mt-1`}
          />
        </label>
        <label className="block">
          <span className="text-sm font-medium">图标地址（可选）</span>
          <input
            value={form.icon_url}
            onChange={(e) => setForm((f) => ({ ...f, icon_url: e.target.value }))}
            placeholder="https://example.com/icon.png"
            className={`${inputClass} mt-1`}
          />
        </label>
        <label className="block">
          <span className="text-sm font-medium">联系邮箱（可选）</span>
          <input
            value={form.email}
            onChange={(e) => setForm((f) => ({ ...f, email: e.target.value }))}
            placeholder="审核结果通知邮箱"
            className={`${inputClass} mt-1`}
          />
        </label>
        <div className="sm:col-span-2 flex items-center gap-3">
          <button
            type="submit"
            {...hoverTapScale}
            disabled={submit.isPending || !form.site_name.trim() || !form.url.trim()}
            className="inline-flex items-center gap-1.5 rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 transition-opacity hover:opacity-95 disabled:opacity-50"
          >
            {submit.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
            提交申请
          </button>
          {captcha.enabled && (
            <span className="text-xs text-muted-foreground">提交时需要完成人机验证</span>
          )}
        </div>
      </form>
      {captcha.dialog}
          {captcha.prewarmNode}
    </InView>
  )
}

export default function LinksPage() {
  const site = useSiteConfig()
  const { data, isLoading } = useQuery({
    queryKey: ['friend-links'],
    queryFn: fetchFriendLinks,
  })

  const links = data?.links ?? []

  // 页头入场（原 motion.header）
  const headerRef = useRef<HTMLElement>(null)
  useReveal(headerRef, { y: 20, duration: 0.5 })

  // 页头图标上下浮动：循环动画走 createLoop 纳管，页面隐藏时自动暂停
  const heroIconRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!heroIconRef.current || prefersReducedMotion()) return
    const tween = createLoop(() =>
      gsap.to(heroIconRef.current, {
        y: -4,
        duration: 1.5,
        repeat: -1,
        yoyo: true,
        ease: 'sine.inOut',
      }),
    )
    return () => {
      tween.kill()
      releaseLoop(tween)
    }
  }, [])

  // 页脚说明文字入场（原 motion.p，仅透明度）
  const noteRef = useRef<HTMLParagraphElement>(null)
  useReveal(noteRef, { y: 0, delay: 0.4 })

  return (
    <PageTransition>
      <div className="mx-auto max-w-4xl px-4 py-12">
        {/* 页头 */}
        <header
          ref={headerRef}
          className="relative overflow-hidden rounded-2xl border border-border bg-gradient-to-br from-accent/10 via-card to-purple-500/10 p-8 text-center"
        >
          <div className="pointer-events-none absolute -right-10 -top-10 h-32 w-32 rounded-full bg-accent/15 blur-3xl" />
          <div
            ref={heroIconRef}
            className="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-accent text-white shadow-lg shadow-accent/30"
          >
            <Link2 className="h-7 w-7" />
          </div>
          <h1 className="relative mt-4 text-2xl font-bold tracking-tight sm:text-3xl">
            {site.friendLinksTitle}
          </h1>
          <p className="relative mt-2 text-sm text-muted-foreground">
            {site.siteName} 的朋友们 · 共 {links.length} 个站点
          </p>
          {site.friendLinksIntro && (
            <p className="relative mt-3 max-w-xl text-sm leading-relaxed text-muted-foreground/90">
              {site.friendLinksIntro}
            </p>
          )}
        </header>

        {/* 列表 */}
        {isLoading ? (
          <div className="mt-8">
            <RowLoading rows={4} />
          </div>
        ) : links.length === 0 ? (
          <Reveal
            y={12}
            className="mt-8 rounded-xl border border-dashed p-16 text-center"
          >
            <Link2 className="mx-auto h-10 w-10 text-muted-foreground/50" />
            <p className="mt-4 text-muted-foreground">还没有添加友情链接</p>
          </Reveal>
        ) : (
          <StaggerList className="mt-8 grid gap-4 sm:grid-cols-2">
            {links.map((link) => (
              <StaggerItem key={link.id}>
                {link.available && link.url ? (
                  <HoverLift>
                    <a
                      href={link.url}
                      target="_blank"
                      rel="noreferrer"
                      className="group flex items-center gap-4 rounded-xl border border-border bg-card p-4 transition-all hover:border-accent/40 hover:shadow-lg hover:shadow-accent/5"
                    >
                      <LinkAvatar icon={link.icon_url} name={link.name} dim={false} />
                      <div className="min-w-0 flex-1">
                        <p className="flex items-center gap-1.5 truncate font-medium transition-colors group-hover:text-accent">
                          {link.name}
                          <ExternalLink className="h-3.5 w-3.5 opacity-0 transition-opacity group-hover:opacity-60" />
                        </p>
                        <p className="mt-0.5 truncate text-xs text-muted-foreground">
                          {link.description || link.masked_url}
                        </p>
                      </div>
                    </a>
                  </HoverLift>
                ) : (
                  /* 失效友链：禁止跳转 + 脱敏展示 */
                  <div
                    className="flex cursor-not-allowed items-center gap-4 rounded-xl border border-dashed border-border bg-muted/40 p-4 opacity-70"
                    title="该站点暂时无法访问，已暂停跳转"
                  >
                    <LinkAvatar icon={link.icon_url} name={link.name} dim />
                    <div className="min-w-0 flex-1">
                      <p className="flex items-center gap-1.5 truncate font-medium text-muted-foreground">
                        {link.name}
                        <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-amber-500/10 px-2 py-0.5 text-[10px] font-medium text-amber-600 dark:text-amber-400">
                          <AlertTriangle className="h-2.5 w-2.5" />
                          链接失效
                        </span>
                      </p>
                      <p className="mt-0.5 flex items-center gap-1 truncate text-xs text-muted-foreground/70">
                        <ShieldOff className="h-3 w-3 shrink-0" />
                        已脱敏：{link.masked_url}
                      </p>
                    </div>
                  </div>
                )}
              </StaggerItem>
            ))}
          </StaggerList>
        )}

        <p
          ref={noteRef}
          className="mt-10 text-center text-xs text-muted-foreground"
        >
          友情链接每日自动检测，失效站点将暂停跳转以保护访问安全
        </p>

        {/* 自助申请 */}
        <ApplyForm />
      </div>
    </PageTransition>
  )
}

function LinkAvatar({
  icon,
  name,
  dim,
}: {
  icon?: string
  name: string
  dim: boolean
}) {
  return (
    <span
      className={`flex h-11 w-11 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-border bg-background text-sm font-bold ${
        dim ? 'grayscale' : ''
      }`}
    >
      {icon ? (
        /* eslint-disable-next-line @next/next/no-img-element */
        <img
          src={icon}
          alt={name}
          className="h-full w-full object-cover"
          onError={(e) => {
            ;(e.currentTarget as HTMLImageElement).style.display = 'none'
          }}
        />
      ) : (
        <span className="text-accent">{name.charAt(0).toUpperCase()}</span>
      )}
    </span>
  )
}
