'use client'

import { useEffect, useRef, useState } from 'react'
import gsap from 'gsap'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Globe, Image as ImageIcon, ImagePlus, Mail, Send, ShieldCheck, Trash2 } from 'lucide-react'
import {
  fetchAdminSettings,
  sendTestMail,
  updateAdminSettings,
  uploadImage,
  ApiError,
} from '@/lib/api'
import { useNotify } from '@/components/toast'
import { SecretInput } from '@/components/secret-input'
import { useSiteConfigActions } from '@/components/site-config-context'
import { hoverTapScale, prefersReducedMotion, useReveal } from '@/components/motion'
import { PageLoading } from '@/components/page-loader'
import type { SiteSettings } from '@/lib/types'

function Toggle({
  checked,
  onChange,
  label,
  desc,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
  desc: string
}) {
  // 开关滑块：checked 变化时用 GSAP 位移（替代原 framer spring）
  const knobRef = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    if (knobRef.current && !prefersReducedMotion()) {
      gsap.to(knobRef.current, { x: checked ? 20 : 0, duration: 0.25, ease: 'power2.out' })
    }
  }, [checked])

  return (
    <div className="flex items-center justify-between gap-4 py-3">
      <div className="min-w-0">
        <p className="text-sm font-medium">{label}</p>
        <p className="mt-0.5 text-xs text-muted-foreground">{desc}</p>
      </div>
      <button
        type="button"
        onClick={() => onChange(!checked)}
        className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${
          checked ? 'bg-emerald-500' : 'bg-border'
        }`}
        role="switch"
        aria-checked={checked}
      >
        <span
          ref={knobRef}
          className="absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white shadow-sm"
        />
      </button>
    </div>
  )
}

function Section({
  icon,
  title,
  children,
}: {
  icon: React.ReactNode
  title: string
  children: React.ReactNode
}) {
  // 分区卡片：语义标签，用 useReveal 挂入场动画（不改 DOM 结构）
  const sectionRef = useRef<HTMLElement>(null)
  useReveal(sectionRef, { y: 16, duration: 0.4 })

  return (
    <section
      ref={sectionRef}
      className="rounded-xl border border-border bg-card"
    >
      <div className="flex items-center gap-2 border-b border-border px-5 py-3.5">
        {icon}
        <h2 className="text-sm font-semibold">{title}</h2>
      </div>
      <div className="px-5 py-4">{children}</div>
    </section>
  )
}

const inputClass =
  'w-full rounded-lg border border-border bg-background px-3.5 py-2.5 text-sm outline-none transition-all placeholder:text-muted-foreground/60 focus:border-accent focus:ring-2 focus:ring-accent/20'

function ImageField({
  label,
  desc,
  value,
  onChange,
  round,
}: {
  label: string
  desc: string
  value: string
  onChange: (url: string) => void
  round?: boolean
}) {
  const notify = useNotify()
  const fileRef = useRef<HTMLInputElement>(null)
  const upload = useMutation({
    mutationFn: (file: File) => uploadImage(file),
    onSuccess: (url) => {
      onChange(url)
      notify.success('图片已上传，记得点击保存设置')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '上传失败'),
  })

  return (
    <div className="space-y-1.5">
      <label className="text-sm font-medium">{label}</label>
      <div className="flex items-center gap-3">
        <div
          className={`flex h-14 w-14 shrink-0 items-center justify-center overflow-hidden border border-border bg-muted ${
            round ? 'rounded-full' : 'rounded-lg'
          }`}
        >
          {value ? (
            /* eslint-disable-next-line @next/next/no-img-element */
            <img src={value} alt={label} className="h-full w-full object-cover" />
          ) : (
            <ImagePlus className="h-5 w-5 text-muted-foreground/50" />
          )}
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-1.5">
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => fileRef.current?.click()}
              disabled={upload.isPending}
              className="rounded-lg border border-border px-3 py-1.5 text-xs font-medium transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
            >
              {upload.isPending ? '上传中...' : '上传图片'}
            </button>
            {value && (
              <button
                type="button"
                onClick={() => onChange('')}
                className="flex items-center gap-1 rounded-lg px-2 py-1.5 text-xs text-muted-foreground transition-colors hover:bg-red-500/10 hover:text-red-500"
              >
                <Trash2 className="h-3 w-3" /> 清除
              </button>
            )}
          </div>
          <p className="truncate text-xs text-muted-foreground">{desc}</p>
        </div>
        <input
          ref={fileRef}
          type="file"
          accept="image/jpeg,image/png,image/gif,image/webp,image/svg+xml"
          hidden
          onChange={(e) => {
            const file = e.target.files?.[0]
            if (file) upload.mutate(file)
            e.target.value = ''
          }}
        />
      </div>
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="或直接粘贴图片地址..."
        className={inputClass}
      />
    </div>
  )
}

export default function AdminSettingsPage() {
  const notify = useNotify()
  const { refresh: refreshSiteConfig } = useSiteConfigActions()
  const [form, setForm] = useState<SiteSettings | null>(null)
  const [smtpPass, setSmtpPass] = useState('')
  const [testTo, setTestTo] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['admin', 'settings'],
    queryFn: fetchAdminSettings,
  })

  useEffect(() => {
    if (!data?.settings) return
    const t = setTimeout(() => setForm({ ...data.settings }), 0)
    return () => clearTimeout(t)
  }, [data])

  const save = useMutation({
    mutationFn: () => {
      const extra = form as unknown as Record<string, unknown>
      const payload: Record<string, unknown> = {
        allow_registration: form!.allow_registration,
        site_name: form!.site_name,
        site_description: form!.site_description,
        site_logo: form!.site_logo ?? '',
        site_favicon: form!.site_favicon ?? '',
        site_icp: form!.site_icp,
        smtp_host: form!.smtp_host,
        smtp_port: form!.smtp_port,
        smtp_user: form!.smtp_user,
        smtp_from: form!.smtp_from,
        // 壁纸与文章页（这些 key 不在 SiteSettings 强类型里，单独透传）
        site_wallpaper: extra.site_wallpaper ?? '',
        wallpaper_opacity: extra.wallpaper_opacity ?? '100',
        wallpaper_blur: extra.wallpaper_blur ?? '0',
        article_sidebar: extra.article_sidebar ?? 'true',
        // 游客评论开关：payload 是白名单，漏了就永远保存不上
        // （后端会静默丢弃未提交的键，表现为"开了又变回关"）
        guest_comment: extra.guest_comment ?? false,
        guest_comment_free: extra.guest_comment_free ?? false,
        guest_comment_email: extra.guest_comment_email ?? false,
      }
      if (smtpPass) payload.smtp_pass = smtpPass
      return updateAdminSettings(payload)
    },
    onSuccess: (res) => {
      setForm(res.settings)
      setSmtpPass('')
      // 立即刷新站点配置：favicon、Logo、站点标题等前台即时生效
      refreshSiteConfig()
      // 通知其他标签页（前台页面）也刷新配置
      localStorage.setItem('site-config-reload', String(Date.now()))
      notify.success('设置已保存')
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '保存失败'),
  })

  const test = useMutation({
    mutationFn: () => sendTestMail(testTo),
    onSuccess: (res) => notify.success(res.message),
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '发送失败'),
  })

  if (isLoading || !form) {
    return (
      <div>
        <h1 className="text-2xl font-bold tracking-tight">网站管理</h1>
        <div className="mt-6">
          <PageLoading minHeight="8rem" />
        </div>
      </div>
    )
  }

  const update = <K extends keyof SiteSettings>(key: K, value: SiteSettings[K]) =>
    setForm((f) => (f ? { ...f, [key]: value } : f))

  return (
    <div>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">网站管理</h1>
          <p className="mt-1 text-sm text-muted-foreground">站点信息、注册开关与邮件服务配置</p>
        </div>
        <button
          onClick={() => save.mutate()}
          disabled={save.isPending}
          {...hoverTapScale}
          className="rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
        >
          {save.isPending ? '保存中...' : '保存设置'}
        </button>
      </div>

      <div className="mt-6 grid gap-4">
        <Section icon={<Globe className="h-4 w-4 text-accent" />} title="站点信息">
          <div className="grid gap-4 sm:grid-cols-2">
            <ImageField
              label="站点 Logo（导航栏）"
              desc="建议 128x128 以上正方形图，留空显示站点名首字"
              value={form.site_logo ?? ''}
              onChange={(url) => update('site_logo', url)}
              round
            />
            <ImageField
              label="浏览器图标 Favicon"
              desc="建议 64x64 正方形 PNG/ICO，留空使用默认"
              value={form.site_favicon ?? ''}
              onChange={(url) => update('site_favicon', url)}
            />
            <div className="space-y-1.5">
              <label className="text-sm font-medium">站点名称</label>
              <input
                value={form.site_name}
                onChange={(e) => update('site_name', e.target.value)}
                className={inputClass}
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-sm font-medium">ICP 备案号（页脚显示）</label>
              <input
                value={form.site_icp}
                onChange={(e) => update('site_icp', e.target.value)}
                placeholder="如：京ICP备xxxxxxxx号"
                className={inputClass}
              />
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <label className="text-sm font-medium">站点描述</label>
              <input
                value={form.site_description}
                onChange={(e) => update('site_description', e.target.value)}
                className={inputClass}
              />
            </div>
          </div>
        </Section>

        <Section icon={<ImageIcon className="h-4 w-4 text-sky-500" />} title="全站壁纸与文章页">
          <div className="grid gap-4 sm:grid-cols-2">
            <ImageField
              label="全站壁纸"
              desc="显示在页面背景，留空则使用纯色背景"
              value={(form as unknown as Record<string, string>).site_wallpaper ?? ''}
              onChange={(url) => update('site_wallpaper' as never, url as never)}
            />
            <div className="space-y-1.5">
              <label className="text-sm font-medium">壁纸不透明度（%）</label>
              <input
                type="number"
                min={5}
                max={100}
                value={(form as unknown as Record<string, string>).wallpaper_opacity ?? '100'}
                onChange={(e) => update('wallpaper_opacity' as never, e.target.value as never)}
                className={inputClass}
              />
              <p className="text-xs text-muted-foreground">建议 20-60，过高会影响文字可读性</p>
            </div>
            <div className="space-y-1.5">
              <label className="text-sm font-medium">壁纸模糊（px）</label>
              <input
                type="number"
                min={0}
                max={20}
                value={(form as unknown as Record<string, string>).wallpaper_blur ?? '0'}
                onChange={(e) => update('wallpaper_blur' as never, e.target.value as never)}
                className={inputClass}
              />
              <p className="text-xs text-muted-foreground">0 表示不模糊，2-8 效果较自然</p>
            </div>
            <div className="space-y-1.5">
              <label className="text-sm font-medium">文章页侧边栏</label>
              <select
                value={(form as unknown as Record<string, string>).article_sidebar ?? 'true'}
                onChange={(e) => update('article_sidebar' as never, e.target.value as never)}
                className={inputClass}
              >
                <option value="true">显示（右侧小工具）</option>
                <option value="false">隐藏（通栏阅读）</option>
              </select>
            </div>
          </div>
        </Section>

        <Section
          icon={<ShieldCheck className="h-4 w-4 text-emerald-500" />}
          title="注册与安全"
        >
          <div className="divide-y divide-border">
            <Toggle
              checked={form.allow_registration}
              onChange={(v) => update('allow_registration', v)}
              label="开放用户注册"
              desc="关闭后新用户将无法注册，已有用户不受影响"
            />
            <Toggle
              checked={form.guest_comment === true}
              onChange={(v) => update('guest_comment', v)}
              label="允许游客评论"
              desc="开启后未登录访客也能发表评论（需填写昵称）；建议同时关注下方的审核与验证码设置"
            />
            {/* 两个子开关只在总开关打开时才有意义，关掉总开关时一并收起，
                避免出现"审核游客评论=开，但游客评论=关"的迷惑组合 */}
            {form.guest_comment === true && (
              <>
                <Toggle
                  checked={form.guest_comment_free === true}
                  onChange={(v) => update('guest_comment_free', v)}
                  label="游客评论免审核"
                  desc="关闭时游客评论进入待审核队列，需在「评论管理」页放行后才公开——推荐保持关闭以防垃圾评论"
                />
                <Toggle
                  checked={form.guest_comment_email === true}
                  onChange={(v) => update('guest_comment_email', v)}
                  label="游客评论必填邮箱"
                  desc="邮箱不会公开展示，仅在后台审核时可见，便于必要时联系评论者"
                />
              </>
            )}
          </div>
        </Section>

        <Section icon={<Mail className="h-4 w-4 text-purple-500" />} title="邮件服务（SMTP）">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <label className="text-sm font-medium">SMTP 服务器</label>
              <input
                value={form.smtp_host}
                onChange={(e) => update('smtp_host', e.target.value)}
                placeholder="smtp.example.com"
                className={inputClass}
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-sm font-medium">端口</label>
              <input
                value={form.smtp_port}
                onChange={(e) => update('smtp_port', e.target.value)}
                placeholder="465"
                className={inputClass}
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-sm font-medium">账号</label>
              <input
                value={form.smtp_user}
                onChange={(e) => update('smtp_user', e.target.value)}
                placeholder="no-reply@example.com"
                className={inputClass}
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-sm font-medium">
                密码/授权码
                {form.smtp_pass_set && (
                  <span className="ml-2 text-xs text-emerald-600 dark:text-emerald-400">
                    已配置（留空保持不变）
                  </span>
                )}
              </label>
              <SecretInput
                key={`smtp-pass-${form.smtp_pass_set ? 'set' : 'unset'}`}
                value={smtpPass}
                onChange={setSmtpPass}
                isSet={form.smtp_pass_set}
                placeholder={form.smtp_pass_set ? '已保存，重新输入可覆盖' : '未设置'}
                className={inputClass}
              />
            </div>
            <div className="space-y-1.5 sm:col-span-2">
              <label className="text-sm font-medium">发件人显示（可选）</label>
              <input
                value={form.smtp_from}
                onChange={(e) => update('smtp_from', e.target.value)}
                placeholder="Blog 平台 &lt;no-reply@example.com&gt;"
                className={inputClass}
              />
            </div>
          </div>

          <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-border pt-4">
            <input
              type="email"
              value={testTo}
              onChange={(e) => setTestTo(e.target.value)}
              placeholder="收件邮箱，用于发送测试邮件"
              className="w-64 rounded-lg border border-border bg-background px-3.5 py-2 text-sm outline-none transition-all focus:border-accent focus:ring-2 focus:ring-accent/20"
            />
            <button
              type="button"
              onClick={() => {
                if (!testTo.trim()) {
                  notify.error('请填写收件邮箱')
                  return
                }
                test.mutate()
              }}
              disabled={test.isPending}
              {...hoverTapScale}
              className="flex items-center gap-1.5 rounded-lg border border-border px-4 py-2 text-sm font-medium transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
            >
              <Send className="h-3.5 w-3.5" />
              {test.isPending ? '发送中...' : '发送测试邮件'}
            </button>
            <span className="text-xs text-muted-foreground">保存设置后再测试</span>
          </div>
        </Section>
      </div>
    </div>
  )
}
