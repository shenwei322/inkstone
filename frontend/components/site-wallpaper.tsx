'use client'

import { useEffect, useMemo, useState } from 'react'
import { useSiteConfig } from '@/components/site-config-context'

/**
 * 全站自定义壁纸：作为固定背景层铺在内容之下。
 *
 * 分层结构（自下而上）：
 *   1. 底色层：当前主题的背景色，图片解码完成前先顶上，避免首屏闪白；
 *   2. 图片层：object-cover 铺满，支持不透明度、模糊、饱和度；
 *   3. 遮罩层：半透明纯色，深色主题偏暗、浅色主题偏亮，
 *      让导航栏/卡片这些半透明表面在图上也保持文字对比度。
 *
 * 三层都放在一个 pointer-events-none / -z-10 的容器里，
 * 不影响任何内容的点击与层级。
 */
export function SiteWallpaper() {
  const site = useSiteConfig()
  // 已解码完成的壁纸地址；与当前地址一致才算「可显示」
  const [loadedUrl, setLoadedUrl] = useState('')

  // 图片地址变化时重新走一遍解码流程：
  // 先用 Image 预解码再显示，避免大图下载中途半截出现。
  // 用「记录已就绪的 URL」而不是布尔量，避免在 effect 里同步 setState：
  // 地址一变，loaded 自然为 false（旧 URL ≠ 新 URL），无需额外重置。
  useEffect(() => {
    if (!site.wallpaper) return
    let cancelled = false
    const img = new Image()
    img.decoding = 'async'
    img.onload = () => {
      if (!cancelled) setLoadedUrl(site.wallpaper)
    }
    img.onerror = () => {
      // 图片挂了就保持底色层，不要留一块透明窟窿
      if (!cancelled) setLoadedUrl('')
    }
    img.src = site.wallpaper
    return () => {
      cancelled = true
    }
  }, [site.wallpaper])

  // 参数统一在这里 clamp，防止后台填了越界值把页面弄坏
  const opacity = useMemo(
    () => Math.min(100, Math.max(0, site.wallpaperOpacity)) / 100,
    [site.wallpaperOpacity],
  )
  const blur = useMemo(
    () => Math.min(20, Math.max(0, site.wallpaperBlur)),
    [site.wallpaperBlur],
  )
  const overlay = useMemo(
    () => Math.min(100, Math.max(0, site.wallpaperOverlay)),
    [site.wallpaperOverlay],
  )
  const saturate = useMemo(
    () => Math.min(200, Math.max(0, site.wallpaperSaturate)),
    [site.wallpaperSaturate],
  )

  if (!site.wallpaper) return null

  // 当前地址已解码完成才算加载好；地址一换自动回到未加载状态
  const loaded = loadedUrl === site.wallpaper

  // 模糊会把图片边缘往内收，出现一圈透明边；放大一点点盖住
  const scale = blur > 0 ? 1 + blur / 40 : 1

  // 滤镜串：模糊 + 饱和度。saturate(100%) 是恒等变换，可省略。
  const filters: string[] = []
  if (blur > 0) filters.push(`blur(${blur}px)`)
  if (saturate !== 100) filters.push(`saturate(${saturate}%)`)

  return (
    <div
      aria-hidden
      className="pointer-events-none fixed inset-0 -z-10 overflow-hidden"
    >
      {/* 1. 底色层：主题背景色打底，图片没到位前不闪白 */}
      <div className="absolute inset-0 bg-background" />

      {/* 2. 图片层 */}
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img
        src={site.wallpaper}
        alt=""
        decoding="async"
        className={`absolute inset-0 h-full w-full object-cover transition-opacity duration-700 ${
          loaded ? 'opacity-100' : 'opacity-0'
        }`}
        style={{
          opacity,
          filter: filters.length > 0 ? filters.join(' ') : undefined,
          transform: `scale(${scale})`,
          transformOrigin: 'center',
        }}
      />

      {/* 3. 遮罩层：跟随主题决定暗/亮，强度由 wallpaper_overlay 控制 */}
      {overlay > 0 && (
        <div
          className="absolute inset-0"
          style={{
            /* var(--wallpaper-tone) 在 globals.css 里按 .dark 切换为黑/白 */
            backgroundColor: `color-mix(in srgb, var(--wallpaper-tone, #000) ${overlay}%, transparent)`,
          }}
        />
      )}

      {/* 4. 上下渐隐：页面顶端与底端向背景色过渡，滚动时不会有生硬切边 */}
      <div className="absolute inset-x-0 top-0 h-40 bg-gradient-to-b from-background/80 to-transparent" />
      <div className="absolute inset-x-0 bottom-0 h-40 bg-gradient-to-t from-background/80 to-transparent" />
    </div>
  )
}
