'use client'

import Link from 'next/link'
import { FileText, GitBranch, Heart, Rss, ShieldCheck } from 'lucide-react'
import { useSiteConfig } from '@/components/site-config-context'
import { MenuIcon } from '@/components/menu-icon'
import { InView } from '@/components/motion'

export function SiteFooter() {
  const site = useSiteConfig()
  const year = new Date().getFullYear()

  // 页脚导航：优先使用自定义菜单，未配置时给默认项
  const links =
    site.navMenu.length > 0
      ? site.navMenu.map((m) => ({ label: m.label, href: m.url, icon: m.icon }))
      : [
          { label: '首页', href: '/', icon: 'home' },
          { label: '友情链接', href: '/links', icon: 'link' },
          { label: 'RSS 订阅', href: '/feed.xml', icon: 'rss' },
        ]

  return (
    /* 滚入视口淡入一次（替代 whileInView + viewport once）：InView 包裹整个 footer，footer 标签与 className 保持不变 */
    <InView>
      <footer className="relative mt-12 border-t border-border bg-card/60 backdrop-blur">
        {/* 顶部渐变装饰线 */}
        <div className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-accent/40 to-transparent" />

        <div className="mx-auto max-w-5xl px-4 py-5">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            {/* 品牌区 */}
            <div className="max-w-[13rem]">
              <Link href="/" className="group inline-flex items-center gap-2.5">
                {site.siteLogo ? (
                  /* eslint-disable-next-line @next/next/no-img-element */
                  <img
                    src={site.siteLogo}
                    alt={site.siteName}
                    className="h-7 w-7 rounded-md object-cover transition-transform group-hover:scale-110"
                  />
                ) : (
                  <span className="flex h-7 w-7 items-center justify-center rounded-md bg-accent text-xs font-black text-white">
                    {site.siteName.charAt(0).toUpperCase()}
                  </span>
                )}
                <span className="text-sm font-bold tracking-tight transition-colors group-hover:text-accent">
                  {site.siteName}
                </span>
              </Link>
              <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
                {site.siteDescription || '记录思考，分享创作'}
              </p>
              <div className="mt-3 flex items-center gap-1.5">
                <a
                  href="/feed.xml"
                  target="_blank"
                  rel="noreferrer"
                  title="RSS 订阅"
                  className="flex h-7 w-7 items-center justify-center rounded-md border border-border text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent"
                >
                  <Rss className="h-3.5 w-3.5" />
                </a>
                <a
                  href="https://github.com/shenwei234/inkstone"
                  target="_blank"
                  rel="noreferrer"
                  title="开源仓库"
                  className="flex h-7 w-7 items-center justify-center rounded-md border border-border text-muted-foreground transition-colors hover:border-accent/40 hover:text-accent"
                >
                  <GitBranch className="h-3.5 w-3.5" />
                </a>
              </div>
            </div>

            {/* 导航区 */}
            <div className="grid grid-cols-2 gap-x-8 gap-y-2 sm:gap-x-10">
              <p className="col-span-2 text-[11px] font-semibold uppercase tracking-widest text-muted-foreground">
                导航
              </p>
              {links.map((item) => (
                <Link
                  key={item.label + item.href}
                  href={item.href}
                  className="group inline-flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-accent"
                >
                  <MenuIcon name={item.icon} className="h-3 w-3 opacity-60 group-hover:opacity-100" />
                  {item.label}
                </Link>
              ))}
              {/* 固定法务入口。刻意不并进上面的 links 数组：那个数组在后台配置了
                  自定义导航菜单后会被整体替换，隐私政策 / 用户协议就消失了。
                  法务页面应当始终可达，所以在这里独立渲染。 */}
              <Link
                href="/privacy"
                className="group inline-flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-accent"
              >
                <ShieldCheck className="h-3 w-3 opacity-60 group-hover:opacity-100" />
                隐私政策
              </Link>
              <Link
                href="/terms"
                className="group inline-flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-accent"
              >
                <FileText className="h-3 w-3 opacity-60 group-hover:opacity-100" />
                用户协议
              </Link>
            </div>
          </div>

          {/* 底部分隔线 */}
          <div className="mt-4 flex flex-col items-center justify-between gap-2 border-t border-border pt-3 text-[11px] text-muted-foreground sm:flex-row">
            <p className="flex flex-wrap items-center justify-center gap-x-1.5 gap-y-1">
              <span>© {year} {site.siteName}</span>
              <span className="opacity-40">·</span>
              <span className="inline-flex items-center gap-1">
                Powered by
                <a
                  href="https://github.com/shenwei234/inkstone"
                  target="_blank"
                  rel="noreferrer"
                  className="font-medium text-foreground/80 transition-colors hover:text-accent"
                >
                  InkStone
                </a>
              </span>
            </p>

            <p className="flex flex-wrap items-center justify-center gap-x-2 gap-y-1">
              {site.siteIcp && (
                <>
                  <a
                    href="https://beian.miit.gov.cn/"
                    target="_blank"
                    rel="noreferrer"
                    className="transition-colors hover:text-foreground"
                  >
                    {site.siteIcp}
                  </a>
                  <span className="opacity-40">·</span>
                </>
              )}
              <span className="inline-flex items-center gap-1">
                Made with
                <Heart className="h-2.5 w-2.5 fill-current text-red-400" />
                by shenwei
              </span>
            </p>
          </div>
        </div>
      </footer>
    </InView>
  )
}
