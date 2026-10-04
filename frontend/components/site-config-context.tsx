'use client'

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { fetchSiteConfig, type EmailCodeConfig, type GeetestConfig, type LapConfig, type PowConfig } from '@/lib/api'

export interface NavMenuItem {
  label: string
  url: string
  icon?: string
}

export type WidgetType =
  | 'about'
  | 'hot'
  | 'tags'
  | 'search'
  | 'html'
  | 'profile'
  | 'weather'
  | 'countdown'
  | 'clock'
  | 'stats'
  | 'hitokoto'

export interface WidgetSocial {
  icon: string
  url: string
  label?: string
}

export interface SidebarWidget {
  type: WidgetType
  title: string
  content?: string
  limit?: number
  city?: string
  avatar?: string
  date?: string
  eventName?: string
  /** 站长信息：显示的名字（留空则用 title） */
  subtitle?: string
  /** 站长信息：社交图标 */
  socials?: WidgetSocial[]
}

export interface SiteConfig {
  siteName: string
  siteDescription: string
  siteLogo: string
  siteFavicon: string
  siteIcp: string
  navMenu: NavMenuItem[]
  widgets: SidebarWidget[]
  sidebarPosition: 'left' | 'right'
  emailCode: EmailCodeConfig
  geetest: GeetestConfig
  lap: LapConfig
  pow: PowConfig
  /** 当前启用的验证码提供方：geetest | lap | pow */
  captchaProvider: string
  wallpaper: string
  wallpaperOpacity: number
  wallpaperBlur: number
  articleSidebar: boolean
  allowRegistration: boolean
  maintenanceMode: boolean
  /** 友情链接页显示内容（后台友链管理页编辑） */
  friendLinksTitle: string
  friendLinksIntro: string
  loaded: boolean
}

const DEFAULT_GEETEST: GeetestConfig = {
  enabled: false,
  on_login: false,
  on_register: false,
  on_comment: false,
  captcha_id: '',
}

const DEFAULT_LAP: LapConfig = {
  enabled: false,
  on_login: false,
  on_register: false,
  on_comment: false,
  site_key: '',
  api_endpoint: '',
}

const DEFAULT_POW: PowConfig = {
  enabled: false,
  on_login: false,
  on_register: false,
  on_comment: false,
  difficulty: 4,
  ttl_seconds: 600,
  memory_mb: 8,
  rounds: 4,
  min_events: 3,
}

const DEFAULT_CONFIG: SiteConfig = {
  siteName: 'InkStone',
  siteDescription: 'InkStone — 现代化多用户博客系统',
  siteLogo: '',
  siteFavicon: '',
  siteIcp: '',
  navMenu: [],
  widgets: [],
  sidebarPosition: 'right',
  emailCode: { on_register: false, on_login: false },
  geetest: DEFAULT_GEETEST,
  lap: DEFAULT_LAP,
  pow: DEFAULT_POW,
  captchaProvider: 'geetest',
  wallpaper: '',
  wallpaperOpacity: 100,
  wallpaperBlur: 0,
  articleSidebar: true,
  allowRegistration: true,
  maintenanceMode: false,
  friendLinksTitle: '友情链接',
  friendLinksIntro: '',
  loaded: false,
}

const SiteConfigContext = createContext<SiteConfig>(DEFAULT_CONFIG)

interface SiteConfigActions {
  /** 重新拉取站点配置（如后台改动设置后刷新前台状态） */
  refresh: () => void
}
const SiteConfigActionsContext = createContext<SiteConfigActions>({ refresh: () => {} })

interface RawSiteConfig {
  site_name?: string
  site_description?: string
  site_logo?: string
  site_favicon?: string
  site_icp?: string
  allow_registration?: boolean
  nav_menu?: unknown
  sidebar_widgets?: unknown
  sidebar_position?: string
  email_code?: Partial<EmailCodeConfig>
  geetest?: Partial<GeetestConfig>
  lap?: Partial<LapConfig>
  pow?: Partial<PowConfig>
  captcha_provider?: string
  site_wallpaper?: string
  wallpaper_opacity?: string
  wallpaper_blur?: string
  article_sidebar?: string
  maintenance_mode?: string
  friend_links_title?: string
  friend_links_intro?: string
}

function parseItems<T>(raw: unknown, validate: (item: unknown) => T | null): T[] {
  if (!Array.isArray(raw)) return []
  return raw
    .map((item) => validate(item))
    .filter((item): item is T => item !== null)
}

export function SiteConfigProvider({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<SiteConfig>(DEFAULT_CONFIG)

  const loadConfig = useCallback(() => {
    fetchSiteConfig()
      .then((raw) => {
        const cfg = raw as RawSiteConfig
        const next: SiteConfig = {
          siteName: cfg.site_name || DEFAULT_CONFIG.siteName,
          siteDescription: cfg.site_description || DEFAULT_CONFIG.siteDescription,
          siteLogo: cfg.site_logo || '',
          siteFavicon: cfg.site_favicon || '',
          siteIcp: cfg.site_icp || '',
          navMenu: parseItems(cfg.nav_menu, (item) => {
            const m = item as { label?: string; url?: string; icon?: string }
            if (m && typeof m.label === 'string' && typeof m.url === 'string') {
              return {
                label: m.label,
                url: m.url,
                icon: typeof m.icon === 'string' ? m.icon : undefined,
              }
            }
            return null
          }),
          widgets: parseItems(cfg.sidebar_widgets, (item) => {
            const w = item as {
              type?: string
              title?: string
              content?: string
              limit?: number
              city?: string
              avatar?: string
              date?: string
              eventName?: string
              subtitle?: string
              socials?: unknown
            }
            if (w && typeof w.type === 'string' && typeof w.title === 'string') {
              return {
                type: w.type as WidgetType,
                title: w.title,
                content: typeof w.content === 'string' ? w.content : '',
                limit: typeof w.limit === 'number' ? w.limit : undefined,
                city: typeof w.city === 'string' ? w.city : undefined,
                avatar: typeof w.avatar === 'string' ? w.avatar : undefined,
                date: typeof w.date === 'string' ? w.date : undefined,
                eventName: typeof w.eventName === 'string' ? w.eventName : undefined,
                subtitle: typeof w.subtitle === 'string' ? w.subtitle : undefined,
                socials: parseItems(w.socials, (s) => {
                  const o = s as { icon?: string; url?: string; label?: string }
                  if (o && typeof o.icon === 'string' && typeof o.url === 'string') {
                    return {
                      icon: o.icon,
                      url: o.url,
                      label: typeof o.label === 'string' ? o.label : undefined,
                    }
                  }
                  return null
                }),
              }
            }
            return null
          }),
          allowRegistration: cfg.allow_registration !== false,
          sidebarPosition: cfg.sidebar_position === 'left' ? 'left' : 'right',
          emailCode: {
            on_register: cfg.email_code?.on_register ?? false,
            on_login: cfg.email_code?.on_login ?? false,
          },
          geetest: {
            enabled: cfg.geetest?.enabled === true,
            on_login: cfg.geetest?.on_login === true,
            on_register: cfg.geetest?.on_register === true,
            on_comment: cfg.geetest?.on_comment === true,
            captcha_id: cfg.geetest?.captcha_id ?? '',
          },
          lap: {
            enabled: cfg.lap?.enabled === true,
            on_login: cfg.lap?.on_login === true,
            on_register: cfg.lap?.on_register === true,
            on_comment: cfg.lap?.on_comment === true,
            site_key: cfg.lap?.site_key ?? '',
            api_endpoint: cfg.lap?.api_endpoint ?? '',
          },
          pow: {
            enabled: cfg.pow?.enabled === true,
            on_login: cfg.pow?.on_login === true,
            on_register: cfg.pow?.on_register === true,
            on_comment: cfg.pow?.on_comment === true,
            difficulty: typeof cfg.pow?.difficulty === 'number' ? cfg.pow.difficulty : DEFAULT_POW.difficulty,
            ttl_seconds: typeof cfg.pow?.ttl_seconds === 'number' ? cfg.pow.ttl_seconds : DEFAULT_POW.ttl_seconds,
            memory_mb: typeof cfg.pow?.memory_mb === 'number' ? cfg.pow.memory_mb : DEFAULT_POW.memory_mb,
            rounds: typeof cfg.pow?.rounds === 'number' ? cfg.pow.rounds : DEFAULT_POW.rounds,
            min_events: typeof cfg.pow?.min_events === 'number' ? cfg.pow.min_events : DEFAULT_POW.min_events,
          },
          captchaProvider: cfg.captcha_provider === 'lap' ? 'lap' : cfg.captcha_provider === 'pow' ? 'pow' : 'geetest',
          wallpaper: cfg.site_wallpaper ?? '',
          wallpaperOpacity: Number(cfg.wallpaper_opacity ?? 100) || 100,
          wallpaperBlur: Number(cfg.wallpaper_blur ?? 0) || 0,
          articleSidebar: cfg.article_sidebar !== 'false',
          maintenanceMode: cfg.maintenance_mode === 'true',
          friendLinksTitle: cfg.friend_links_title || '友情链接',
          friendLinksIntro: cfg.friend_links_intro ?? '',
          loaded: true,
        }
        setConfig(next)

        // 有壁纸时给 body 加标记类，便于样式做半透明处理
        if (next.wallpaper) {
          document.body.classList.add('has-wallpaper')
        } else {
          document.body.classList.remove('has-wallpaper')
        }
        // 注意：浏览器标题与 favicon 由 components/site-head.tsx 以 React 19
        // metadata hoist 方式渲染，此处不再直接操作 DOM link（会被 React 覆盖）
      })
      .catch(() => setConfig((c) => ({ ...c, loaded: true })))
  }, [])

  useEffect(() => {
    loadConfig()
  }, [loadConfig])

  // 其他标签页（如后台保存设置）写入标记后，本地重新拉取配置，
  // 保证 favicon、站点标题等后台改动在前台立即同步，无需手动刷新
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === 'site-config-reload') loadConfig()
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [loadConfig])

  const actions = useMemo<SiteConfigActions>(() => ({ refresh: loadConfig }), [loadConfig])

  return (
    <SiteConfigContext.Provider value={config}>
      <SiteConfigActionsContext.Provider value={actions}>{children}</SiteConfigActionsContext.Provider>
    </SiteConfigContext.Provider>
  )
}

export function useSiteConfig() {
  return useContext(SiteConfigContext)
}

export function useSiteConfigActions() {
  return useContext(SiteConfigActionsContext)
}
