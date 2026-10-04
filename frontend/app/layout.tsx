import type { Metadata } from "next";
import Script from "next/script";
import "./globals.css";
import { Providers } from "@/components/providers";
import { MaintenanceGate } from "@/components/maintenance-gate";
import { Navbar } from "@/components/navbar";
import { SiteFooter } from "@/components/site-footer";
import { SiteWallpaper } from "@/components/site-wallpaper";
import { SiteHead } from "@/components/site-head";
import { RouteLoader } from "@/components/route-loader";
import { THEME_STORAGE_KEY } from "@/lib/theme";

const DEFAULT_META = {
  siteName: 'InkStone',
  siteDescription: 'InkStone — 现代化多用户博客系统',
}

/**
 * 从后端读取站点配置生成 <title>/<meta description>。
 *
 * 为什么不用静态 export const metadata：
 * 后台可改站点名称，静态值写死「InkStone」后前台标签栏永远不更新。
 * 客户端用 document.title 赋值也不行——会被 Next 的 metadata 管理覆盖
 * （head 里的 <title> 由 Next 注入，赋值后又被重置）。
 * revalidate 与后端 settings 的 30s 缓存对齐；后端不可达时回退默认值，
 * 保证页面不会因为站点配置接口故障而 500。
 */
/** 站点 <title>/<meta description>：从后端配置生成，后台改动刷新页面后生效 */
export async function generateMetadata(): Promise<Metadata> {
  const base = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:8080/api/v1'
  let siteName = DEFAULT_META.siteName
  let siteDescription = DEFAULT_META.siteDescription
  try {
    const res = await fetch(`${base}/site-config`, { next: { revalidate: 30 } })
    if (res.ok) {
      const cfg = (await res.json()) as { site_name?: string; site_description?: string }
      siteName = cfg.site_name?.trim() || siteName
      siteDescription = cfg.site_description?.trim() || siteDescription
    }
  } catch {
    // 后端不可达：用默认值
  }
  return {
    title: { default: siteName, template: '%s | InkStone' },
    description: siteDescription,
  }
}

/**
 * 首屏防闪烁：在任何渲染之前同步决定主题并给 <html> 挂 dark class。
 * 放在 body 最前面同步执行，浏览器绘制第一帧前 class 已就位，
 * 暗色用户刷新时不会先闪一下白屏（否则要等 React 挂载后的 effect 才切）。
 */
const THEME_INIT_SCRIPT = `(function(){try{var k=${JSON.stringify(THEME_STORAGE_KEY)};var v=localStorage.getItem(k);var d=v?v==='dark':window.matchMedia('(prefers-color-scheme: dark)').matches;var r=document.documentElement;r.style.colorScheme=d?'dark':'light';if(d)r.classList.add('dark');}catch(e){}})();`

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="zh-CN" className="h-full antialiased" suppressHydrationWarning>
      <body className="min-h-full flex flex-col">
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT_SCRIPT }} />
        {/* 路由切换加载动画：GSAP 进度条，替代骨架图闪烁 */}
        <RouteLoader />
        {/* 极验域名预热：提前完成 DNS/TLS 握手，缩短 gt4.js 与验证接口的建连等待 */}
        <link rel="preconnect" href="https://static.geetest.com" />
        <link rel="preconnect" href="https://gcaptcha4.geetest.com" crossOrigin="anonymous" />
        <link rel="dns-prefetch" href="https://static.geetest.com" />
        {/* 极验第四代行为验证：要求与业务页面同步初始化，用 beforeInteractive 加载 */}
        <Script
          src="https://static.geetest.com/v4/gt4.js"
          strategy="beforeInteractive"
        />
        <Providers>
          {/* 站点标题与 favicon：必须置于 SiteConfigProvider 内层（Providers 内），
              否则 useSiteConfig() 拿到的是 DEFAULT_CONFIG，后台改了 favicon 前台不生效。
              React 19 metadata hoist 到 head，配置变化即时同步 */}
          <SiteHead />
          <MaintenanceGate>
            <SiteWallpaper />
            <Navbar />
            <main className="flex-1">{children}</main>
            <SiteFooter />
          </MaintenanceGate>
        </Providers>
      </body>
    </html>
  );
}
