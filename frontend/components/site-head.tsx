'use client'

import { useSiteConfig } from './site-config-context'

/**
 * 站点级 favicon（浏览器标签页图标）。
 *
 * 用 React 19 的 metadata hoist 渲染 <link rel="icon">：
 * 旧实现（site-config-context 里运行时 querySelector + appendChild 改 link.href）
 * 在 Next 16 / React 19 下会被 metadata 管理覆盖或清理，导致后台改了 favicon
 * 前台不生效；渲染进组件树由 React hoist 到 head 才是标准做法，
 * 配置变化时 React 会同步更新 href，清空自定义时回退默认图标。
 *
 * 注意 1：本组件必须渲染在 SiteConfigProvider 内层（见 app/layout.tsx）。
 * 在 Provider 外 useSiteConfig() 只会拿到 DEFAULT_CONFIG，favicon 永远回退默认值。
 *
 * 注意 2：head 里只保留这一个 <link rel="icon">。app/icon.svg、app/favicon.ico
 * 这类 file convention 会由 Next 额外注入静态 icon link，与动态 link 并存会互相抢占。
 *
 * 注意 3：本组件不渲染 <title>。站点名由 layout 的 generateMetadata 从后端
 * 配置生成（静态 metadata 写死 InkStone 会导致后台改名不生效；客户端
 * document.title 赋值又会被 Next metadata 管理覆盖）。
 */
export function SiteHead() {
  const site = useSiteConfig()
  return <link rel="icon" href={site.siteFavicon || '/icon.svg'} />
}
