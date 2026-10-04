'use client'

import { useEffect, useRef, useState } from 'react'
import gsap from 'gsap'
import { useMutation, useQuery } from '@tanstack/react-query'
import {
  ArrowDown,
  ArrowUp,
  Eye,
  FileStack,
  LayoutList,
  Link2,
  PanelLeft,
  PanelTop,
  Plus,
  Trash2,
} from 'lucide-react'
import { IconPicker } from '@/components/menu-icon'
import { Reveal, easeOut, hoverTapScale, prefersReducedMotion } from '@/components/motion'
import { updateAdminSettings, fetchAdminPages, ApiError } from '@/lib/api'
import { useNotify } from '@/components/toast'
import type { NavMenuItem, SidebarWidget, WidgetSocial, WidgetType } from '@/components/site-config-context'
import { fetchSiteConfig } from '@/lib/api'

const WIDGET_TYPES: { type: WidgetType; label: string; desc: string }[] = [
  { type: 'profile', label: '站长信息', desc: '头像 + 名字 + 简介 + 社交图标' },
  { type: 'weather', label: '天气', desc: '实时天气（Open-Meteo）' },
  { type: 'countdown', label: '节日倒计时', desc: '自动春节倒计时或自定义' },
  { type: 'clock', label: '实时时钟', desc: '动态日期时钟' },
  { type: 'stats', label: '站点统计', desc: '文章/分类/标签数' },
  { type: 'hitokoto', label: '一言', desc: '随机句子，可手动刷新' },
  { type: 'about', label: '关于本站', desc: '一段文字介绍' },
  { type: 'hot', label: '热门文章', desc: '按浏览量排行' },
  { type: 'tags', label: '标签云', desc: '展示所有标签' },
  { type: 'search', label: '搜索框', desc: '文章关键词搜索' },
  { type: 'html', label: '自定义 HTML', desc: '自由嵌入内容' },
]

const widgetTypeLabel = (t: WidgetType) =>
  WIDGET_TYPES.find((w) => w.type === t)?.label ?? t

const inputClass =
  'w-full rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none transition-all placeholder:text-muted-foreground/60 focus:border-accent focus:ring-2 focus:ring-accent/20'

function MoveButtons({
  index,
  count,
  onMove,
}: {
  index: number
  count: number
  onMove: (from: number, to: number) => void
}) {
  return (
    <div className="flex items-center">
      <button
        type="button"
        title="上移"
        disabled={index === 0}
        onClick={() => onMove(index, index - 1)}
        className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-30"
      >
        <ArrowUp className="h-4 w-4" />
      </button>
      <button
        type="button"
        title="下移"
        disabled={index === count - 1}
        onClick={() => onMove(index, index + 1)}
        className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-30"
      >
        <ArrowDown className="h-4 w-4" />
      </button>
    </div>
  )
}

function MenuTab() {
  const notify = useNotify()
  const [items, setItems] = useState<NavMenuItem[]>([])
  const [addPageOpen, setAddPageOpen] = useState(false)

  const pagesQuery = useQuery({
    queryKey: ['admin', 'pages'],
    queryFn: fetchAdminPages,
    enabled: addPageOpen,
  })

  useEffect(() => {
    fetchSiteConfig().then((raw) => {
      if (Array.isArray(raw.nav_menu)) setItems(raw.nav_menu as NavMenuItem[])
    })
  }, [])

  const save = useMutation({
    mutationFn: () =>
      updateAdminSettings({ nav_menu: items } as unknown as Record<string, unknown>),
    onSuccess: () => notify.success('导航菜单已保存，前台刷新即可看到'),
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '保存失败'),
  })

  const move = (from: number, to: number) => {
    setItems((arr) => {
      const next = [...arr]
      const [item] = next.splice(from, 1)
      next.splice(to, 0, item)
      return next
    })
  }

  const publishedPages = (pagesQuery.data?.pages ?? []).filter((p) => p.status === 'published')

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">
        自定义顶部导航栏菜单，留空则显示默认的「首页」。支持站内路径（如 /p/about）或完整外链，可为每项选择图标。
      </p>

      {items.length === 0 && (
        <div className="rounded-xl border border-dashed p-8 text-center text-sm text-muted-foreground">
          尚未自定义，前台显示默认菜单
        </div>
      )}

      {items.map((item, i) => (
        <Reveal
          key={`${item.label}-${i}`}
          y={8}
          className="flex flex-wrap items-center gap-2 rounded-2xl border border-border bg-card p-3"
        >
          <IconPicker
            value={item.icon}
            onChange={(icon) => setItems((arr) => arr.map((x, j) => (j === i ? { ...x, icon } : x)))}
          />
          <input
            value={item.label}
            onChange={(e) =>
              setItems((arr) => arr.map((x, j) => (j === i ? { ...x, label: e.target.value } : x)))
            }
            placeholder="菜单名称"
            className="w-28 rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-accent"
          />
          <input
            value={item.url}
            onChange={(e) =>
              setItems((arr) => arr.map((x, j) => (j === i ? { ...x, url: e.target.value } : x)))
            }
            placeholder="/p/about 或 https://..."
            className={`${inputClass} min-w-0 flex-1`}
          />
          <MoveButtons index={i} count={items.length} onMove={move} />
          <button
            type="button"
            title="删除"
            onClick={() => setItems((arr) => arr.filter((_, j) => j !== i))}
            className="rounded-md p-1.5 text-red-500 transition-colors hover:bg-red-500/10"
          >
            <Trash2 className="h-4 w-4" />
          </button>
        </Reveal>
      ))}

      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={() => setItems((arr) => [...arr, { label: '', url: '' }])}
          className="flex items-center gap-1.5 rounded-lg border border-dashed border-border px-4 py-2 text-sm text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent"
        >
          <Plus className="h-4 w-4" /> 添加菜单项
        </button>
        <button
          type="button"
          onClick={() => setAddPageOpen((o) => !o)}
          className="flex items-center gap-1.5 rounded-lg border border-dashed border-border px-4 py-2 text-sm text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent"
        >
          <FileStack className="h-4 w-4" /> 从页面添加
        </button>
        <button
          type="button"
          onClick={() =>
            setItems((arr) => [...arr, { label: '友情链接', url: '/links', icon: 'link' }])
          }
          className="flex items-center gap-1.5 rounded-lg border border-dashed border-border px-4 py-2 text-sm text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent"
        >
          <Link2 className="h-4 w-4" /> 添加友链页
        </button>
        <button
          type="button"
          onClick={() => save.mutate()}
          disabled={save.isPending}
          {...hoverTapScale}
          className="ml-auto rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
        >
          {save.isPending ? '保存中...' : '保存菜单'}
        </button>
      </div>

      {addPageOpen && (
        <Reveal
          y={8}
          className="rounded-2xl border border-border bg-card p-3"
        >
          <p className="mb-2 text-xs text-muted-foreground">
            选择已发布的页面（在页面管理中创建），将自动添加为菜单项
          </p>
          {publishedPages.length === 0 ? (
            <p className="px-1 py-3 text-sm text-muted-foreground">
              还没有已发布页面，先到「页面管理」创建
            </p>
          ) : (
            <div className="flex flex-wrap gap-2">
              {publishedPages.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => {
                    setItems((arr) => [...arr, { label: p.title, url: `/p/${p.slug}` }])
                    setAddPageOpen(false)
                  }}
                  className="rounded-lg border border-border px-3 py-1.5 text-sm transition-colors hover:border-accent/40 hover:text-accent"
                >
                  {p.title}
                </button>
              ))}
            </div>
          )}
        </Reveal>
      )}
    </div>
  )
}

function SocialsEditor({
  socials,
  onChange,
}: {
  socials: WidgetSocial[]
  onChange: (next: WidgetSocial[]) => void
}) {
  const update = (i: number, patch: Partial<WidgetSocial>) =>
    onChange(socials.map((s, j) => (j === i ? { ...s, ...patch } : s)))

  return (
    <div className="space-y-2 rounded-lg border border-dashed border-border p-3">
      <p className="text-xs text-muted-foreground">社交图标（点击图标切换，填写链接地址）</p>
      {socials.map((s, j) => (
        <div key={j} className="flex items-center gap-2">
          <IconPicker value={s.icon} onChange={(icon) => update(j, { icon })} />
          <input
            value={s.url}
            onChange={(e) => update(j, { url: e.target.value })}
            placeholder="https://..."
            className={`${inputClass} flex-1`}
          />
          <input
            value={s.label ?? ''}
            onChange={(e) => update(j, { label: e.target.value })}
            placeholder="名称（可选）"
            className="w-28 rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none transition-all placeholder:text-muted-foreground/60 focus:border-accent focus:ring-2 focus:ring-accent/20"
          />
          <button
            type="button"
            title="删除"
            onClick={() => onChange(socials.filter((_, k) => k !== j))}
            className="rounded-md p-1.5 text-red-500 transition-colors hover:bg-red-500/10"
          >
            <Trash2 className="h-4 w-4" />
          </button>
        </div>
      ))}
      <button
        type="button"
        onClick={() => onChange([...socials, { icon: 'github', url: '' }])}
        className="flex items-center gap-1.5 rounded-lg border border-dashed border-border px-3 py-1.5 text-xs text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent"
      >
        <Plus className="h-3.5 w-3.5" /> 添加社交图标
      </button>
    </div>
  )
}

function WidgetsTab() {
  const notify = useNotify()
  const [widgets, setWidgets] = useState<SidebarWidget[]>([])
  const [addType, setAddType] = useState<WidgetType>('about')

  useEffect(() => {
    fetchSiteConfig().then((raw) => {
      if (Array.isArray(raw.sidebar_widgets)) setWidgets(raw.sidebar_widgets as SidebarWidget[])
    })
  }, [])

  const save = useMutation({
    mutationFn: () =>
      updateAdminSettings({ sidebar_widgets: widgets } as unknown as Record<string, unknown>),
    onSuccess: () => notify.success('侧边栏小工具已保存，前台刷新即可看到'),
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '保存失败'),
  })

  const move = (from: number, to: number) => {
    setWidgets((arr) => {
      const next = [...arr]
      const [item] = next.splice(from, 1)
      next.splice(to, 0, item)
      return next
    })
  }

  const addWidget = () => {
    const preset = WIDGET_TYPES.find((w) => w.type === addType)
    if (!preset) return
    const defaults: Record<string, Partial<SidebarWidget>> = {
      hot: { limit: 5 },
      tags: { limit: 20 },
      weather: { city: '北京' },
      countdown: { eventName: '', date: '' },
      profile: { content: '', avatar: '', subtitle: '', socials: [] },
    }
    setWidgets((arr) => [
      ...arr,
      { type: addType, title: preset.label, content: '', ...(defaults[addType] ?? {}) },
    ])
  }

  const updateWidget = (i: number, patch: Partial<SidebarWidget>) =>
    setWidgets((arr) => arr.map((w, j) => (j === i ? { ...w, ...patch } : w)))

  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">
        配置首页右侧边栏的小工具，可调整顺序与参数。不配置则首页为通栏布局。
      </p>

      {widgets.length === 0 && (
        <div className="rounded-xl border border-dashed p-8 text-center text-sm text-muted-foreground">
          尚未添加小工具
        </div>
      )}

      {widgets.map((w, i) => (
        <Reveal
          key={`${w.type}-${i}`}
          y={8}
          className="rounded-2xl border border-border bg-card p-4"
        >
          <div className="flex items-center gap-2">
            <span className="rounded-md bg-accent/10 px-2 py-1 text-xs font-medium text-accent">
              {widgetTypeLabel(w.type)}
            </span>
            <input
              value={w.title}
              onChange={(e) => updateWidget(i, { title: e.target.value })}
              placeholder="小工具标题"
              className="w-40 rounded-lg border border-border bg-background px-3 py-1.5 text-sm outline-none focus:border-accent"
            />
            <div className="ml-auto flex items-center">
              <MoveButtons index={i} count={widgets.length} onMove={move} />
              <button
                type="button"
                title="删除"
                onClick={() => setWidgets((arr) => arr.filter((_, j) => j !== i))}
                className="rounded-md p-1.5 text-red-500 transition-colors hover:bg-red-500/10"
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
          </div>

          <div className="mt-3">
            {w.type === 'about' && (
              <textarea
                value={w.content ?? ''}
                onChange={(e) => updateWidget(i, { content: e.target.value })}
                placeholder="介绍文字..."
                rows={3}
                className={`${inputClass} resize-y`}
              />
            )}
            {w.type === 'hot' && (
              <label className="flex items-center gap-2 text-sm text-muted-foreground">
                显示条数
                <input
                  type="number"
                  min={1}
                  max={20}
                  value={w.limit ?? 5}
                  onChange={(e) => updateWidget(i, { limit: Number(e.target.value) })}
                  className="w-20 rounded-lg border border-border bg-background px-3 py-1.5 text-sm outline-none focus:border-accent"
                />
              </label>
            )}
            {w.type === 'tags' && (
              <label className="flex items-center gap-2 text-sm text-muted-foreground">
                显示个数
                <input
                  type="number"
                  min={1}
                  max={50}
                  value={w.limit ?? 20}
                  onChange={(e) => updateWidget(i, { limit: Number(e.target.value) })}
                  className="w-20 rounded-lg border border-border bg-background px-3 py-1.5 text-sm outline-none focus:border-accent"
                />
              </label>
            )}
            {w.type === 'html' && (
              <textarea
                value={w.content ?? ''}
                onChange={(e) => updateWidget(i, { content: e.target.value })}
                placeholder="<p>支持 HTML...</p>"
                rows={4}
                className={`${inputClass} resize-y font-mono text-xs`}
              />
            )}
            {w.type === 'profile' && (
              <div className="space-y-2">
                <input
                  value={w.avatar ?? ''}
                  onChange={(e) => updateWidget(i, { avatar: e.target.value })}
                  placeholder="头像图片地址（可留空显示首字头像）"
                  className={inputClass}
                />
                <input
                  value={w.subtitle ?? ''}
                  onChange={(e) => updateWidget(i, { subtitle: e.target.value })}
                  placeholder="站长名字（留空则显示上方标题）"
                  className={inputClass}
                />
                <textarea
                  value={w.content ?? ''}
                  onChange={(e) => updateWidget(i, { content: e.target.value })}
                  placeholder="站长简介..."
                  rows={2}
                  className={`${inputClass} resize-y`}
                />
                <SocialsEditor
                  socials={w.socials ?? []}
                  onChange={(socials) => updateWidget(i, { socials })}
                />
              </div>
            )}
            {w.type === 'weather' && (
              <input
                value={w.city ?? ''}
                onChange={(e) => updateWidget(i, { city: e.target.value })}
                placeholder="城市名，如：北京 / 上海 / 广州"
                className={inputClass}
              />
            )}
            {w.type === 'countdown' && (
              <div className="grid gap-2 sm:grid-cols-2">
                <input
                  type="date"
                  value={w.date ?? ''}
                  onChange={(e) => updateWidget(i, { date: e.target.value })}
                  className={inputClass}
                />
                <input
                  value={w.eventName ?? ''}
                  onChange={(e) => updateWidget(i, { eventName: e.target.value })}
                  placeholder="节日名称（留空自动算春节）"
                  className={inputClass}
                />
              </div>
            )}
            {w.type === 'clock' && <p className="text-xs text-muted-foreground">无需额外配置</p>}
            {w.type === 'stats' && <p className="text-xs text-muted-foreground">无需额外配置</p>}
            {w.type === 'hitokoto' && <p className="text-xs text-muted-foreground">无需额外配置</p>}
            {w.type === 'search' && (
              <p className="text-xs text-muted-foreground">无需额外配置</p>
            )}
          </div>
        </Reveal>
      ))}

      <div className="flex flex-wrap items-center gap-2">
        <select
          value={addType}
          onChange={(e) => setAddType(e.target.value as WidgetType)}
          className="rounded-lg border border-border bg-background px-3 py-2 text-sm outline-none focus:border-accent"
        >
          {WIDGET_TYPES.map((t) => (
            <option key={t.type} value={t.type}>
              {t.label} — {t.desc}
            </option>
          ))}
        </select>
        <button
          type="button"
          onClick={addWidget}
          className="flex items-center gap-1.5 rounded-lg border border-dashed border-border px-4 py-2 text-sm text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent"
        >
          <Plus className="h-4 w-4" /> 添加小工具
        </button>
        <button
          type="button"
          onClick={() => save.mutate()}
          disabled={save.isPending}
          {...hoverTapScale}
          className="ml-auto rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
        >
          {save.isPending ? '保存中...' : '保存小工具'}
        </button>
      </div>
    </div>
  )
}

function SidebarPositionTab() {
  const notify = useNotify()
  const [position, setPosition] = useState<'right' | 'left'>('right')

  useEffect(() => {
    fetchSiteConfig().then((raw) => {
      const rawPos = (raw as { sidebar_position?: string }).sidebar_position
      setPosition(rawPos === 'left' ? 'left' : 'right')
    })
  }, [])

  const save = useMutation({
    mutationFn: () => updateAdminSettings({ sidebar_position: position }),
    onSuccess: () => notify.success('侧边栏位置已保存，前台刷新即可看到'),
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '保存失败'),
  })

  const options = [
    {
      value: 'right' as const,
      label: '右侧边栏',
      desc: '文章列表在左，小工具在右（经典博客布局）',
      visual: (
        <div className="flex h-16 w-24 gap-1 rounded-md border border-border bg-background p-1">
          <div className="flex-1 rounded-sm bg-accent/20" />
          <div className="w-8 rounded-sm bg-accent" />
        </div>
      ),
    },
    {
      value: 'left' as const,
      label: '左侧边栏',
      desc: '小工具在左，文章列表在右',
      visual: (
        <div className="flex h-16 w-24 gap-1 rounded-md border border-border bg-background p-1">
          <div className="w-8 rounded-sm bg-accent" />
          <div className="flex-1 rounded-sm bg-accent/20" />
        </div>
      ),
    },
  ]

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        选择首页侧边栏小工具的显示位置。
      </p>
      <div className="grid gap-3 sm:grid-cols-2">
        {options.map((opt) => (
          <button
            key={opt.value}
            type="button"
            onClick={() => setPosition(opt.value)}
            className={`flex items-start gap-3 rounded-xl border p-4 text-left transition-all ${
              position === opt.value
                ? 'border-accent bg-accent/5 ring-2 ring-accent/20'
                : 'border-border hover:border-accent/40'
            }`}
          >
            {opt.visual}
            <span>
              <span className="block text-sm font-medium">{opt.label}</span>
              <span className="mt-0.5 block text-xs text-muted-foreground">{opt.desc}</span>
            </span>
          </button>
        ))}
      </div>
      <div className="flex justify-end">
        <button
          type="button"
          onClick={() => save.mutate()}
          disabled={save.isPending}
          {...hoverTapScale}
          className="rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
        >
          {save.isPending ? '保存中...' : '保存位置'}
        </button>
      </div>
    </div>
  )
}

export default function AdminAppearancePage() {
  const [tab, setTab] = useState<'menu' | 'widgets' | 'sidebar'>('menu')
  const tabs = [
    { key: 'menu' as const, label: '顶部菜单', icon: LayoutList },
    { key: 'widgets' as const, label: '侧边栏小工具', icon: PanelLeft },
    { key: 'sidebar' as const, label: '侧边栏位置', icon: PanelTop },
  ]

  // tab 高亮背景：切换时重挂并做 scaleX 入场（替代原 layoutId 共享布局动画）
  const tabBgRef = useRef<HTMLSpanElement>(null)
  useEffect(() => {
    const el = tabBgRef.current
    if (el && !prefersReducedMotion()) {
      gsap.fromTo(el, { scaleX: 0 }, { scaleX: 1, duration: 0.3, ease: easeOut })
    }
  }, [tab])

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">外观管理</h1>
          <p className="mt-1 text-sm text-muted-foreground">自定义前台顶部菜单与侧边栏小工具</p>
        </div>
        <div className="flex items-center gap-1 rounded-lg border border-border bg-card p-1">
          {tabs.map((t) => (
            <button
              key={t.key}
              onClick={() => setTab(t.key)}
              className={`relative flex items-center gap-1.5 rounded-md px-3.5 py-1.5 text-sm transition-colors ${
                tab === t.key ? 'text-white' : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {tab === t.key && (
                <span
                  key={t.key}
                  ref={tabBgRef}
                  className="absolute inset-0 rounded-md bg-accent"
                />
              )}
              <t.icon className="relative h-4 w-4" />
              <span className="relative">{t.label}</span>
            </button>
          ))}
        </div>
      </div>

      <Reveal
        key={tab}
        y={12}
        duration={0.35}
        className="mt-6 rounded-2xl border border-border bg-card p-5"
      >
        {tab === 'menu' ? <MenuTab /> : tab === 'widgets' ? <WidgetsTab /> : <SidebarPositionTab />}
      </Reveal>

      <div className="mt-4 flex items-center gap-2 text-xs text-muted-foreground">
        <Eye className="h-3.5 w-3.5" />
        保存后刷新前台页面即可看到效果
      </div>
    </div>
  )
}
