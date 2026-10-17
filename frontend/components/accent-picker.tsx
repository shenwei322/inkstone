'use client'

import { useEffect, useRef, useState } from 'react'
import { Check, Palette, RotateCcw } from 'lucide-react'
import { useTheme } from './theme'
import { ACCENT_PRESETS, HUE_MAX, HUE_MIN, normalizeHue } from '@/lib/accent'

/**
 * 配色主题选择面板（下拉浮层）。
 *
 * 与 ThemeToggle（亮/暗切换）并列使用：那个是二态开关，这个是八选一 + 自定义色相，
 * 塞进同一个按钮会让交互变得含糊，所以独立成一个「调色板」按钮。
 */
export function AccentPicker({ className }: { className?: string }) {
  const { accent, setAccent, resetAccent } = useTheme()
  const [open, setOpen] = useState(false)
  // 滑块的即时值：拖动时只更新本地 state 让滑块跟手，松手（onChange 结束）
  // 才写入全局主题。若每次 input 都 setAccent，拖动过程会持续触发全局重渲染。
  const [dragHue, setDragHue] = useState<number | null>(null)
  const wrapRef = useRef<HTMLDivElement>(null)

  // 点击外部 / Esc 关闭
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const currentPresetId = accent.kind === 'preset' ? accent.id : null
  const customHue = dragHue ?? (accent.kind === 'custom' ? accent.hue : null)

  return (
    <div ref={wrapRef} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-label="配色主题"
        aria-expanded={open}
        title="配色主题"
        className={className}
      >
        <Palette className="h-4 w-4" />
      </button>

      {open && (
        <div
          className="absolute right-0 top-full z-50 mt-2 w-72 origin-top-right rounded-xl border border-border bg-card p-4 shadow-[var(--card-shadow-raised)] animate-scale-in"
          role="dialog"
          aria-label="配色主题"
        >
          <div className="mb-3 flex items-center justify-between">
            <p className="text-sm font-semibold">配色主题</p>
            <button
              type="button"
              onClick={() => {
                resetAccent()
                setDragHue(null)
              }}
              className="flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:text-accent"
              title="恢复默认配色"
            >
              <RotateCcw className="h-3 w-3" />
              重置
            </button>
          </div>

          {/* 预设色板 */}
          <div className="grid grid-cols-4 gap-2">
            {ACCENT_PRESETS.map((p) => {
              const active = currentPresetId === p.id
              return (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => {
                    setAccent({ kind: 'preset', id: p.id })
                    setDragHue(null)
                  }}
                  title={p.label}
                  aria-label={p.label}
                  aria-pressed={active}
                  className={`group relative flex h-9 items-center justify-center rounded-lg border transition-all ${
                    active
                      ? 'border-accent ring-2 ring-accent/25'
                      : 'border-border hover:border-accent/40'
                  }`}
                >
                  {/* 色块直接用该预设的 oklch 值：与正文主色同一套 L/C，所见即所得 */}
                  <span
                    className="block h-5 w-5 rounded-full"
                    style={{ background: `oklch(0.55 0.2 ${p.hue})` }}
                  />
                  {active && (
                    <Check className="absolute right-0.5 top-0.5 h-3 w-3 text-accent" />
                  )}
                </button>
              )
            })}
          </div>

          {/* 自定义色相 */}
          <div className="mt-4 border-t border-border pt-3">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-xs font-medium">自定义色相</span>
              <span className="text-xs tabular-nums text-muted-foreground">
                {customHue === null ? '未启用' : `${customHue}°`}
              </span>
            </div>
            {/* 用纯 CSS 渐变做色相条，不依赖 canvas/图片 */}
            <div className="relative">
              <div
                className="pointer-events-none absolute inset-x-0 top-1/2 h-2 -translate-y-1/2 rounded-full"
                style={{
                  background:
                    'linear-gradient(to right, oklch(0.6 0.18 0), oklch(0.6 0.18 60), oklch(0.6 0.18 120), oklch(0.6 0.18 180), oklch(0.6 0.18 240), oklch(0.6 0.18 300), oklch(0.6 0.18 360))',
                }}
              />
              <input
                type="range"
                min={HUE_MIN}
                max={HUE_MAX}
                step={1}
                value={customHue ?? 250}
                onChange={(e) => setDragHue(normalizeHue(Number(e.target.value)))}
                onPointerUp={() => {
                  if (dragHue !== null) setAccent({ kind: 'custom', hue: dragHue })
                }}
                onKeyUp={() => {
                  if (dragHue !== null) setAccent({ kind: 'custom', hue: dragHue })
                }}
                aria-label="自定义色相角度"
                className="relative h-6 w-full cursor-pointer appearance-none bg-transparent
                  [&::-webkit-slider-thumb]:h-4 [&::-webkit-slider-thumb]:w-4
                  [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full
                  [&::-webkit-slider-thumb]:border-2 [&::-webkit-slider-thumb]:border-white
                  [&::-webkit-slider-thumb]:bg-foreground [&::-webkit-slider-thumb]:shadow-md
                  [&::-moz-range-thumb]:h-4 [&::-moz-range-thumb]:w-4 [&::-moz-range-thumb]:rounded-full
                  [&::-moz-range-thumb]:border-2 [&::-moz-range-thumb]:border-white
                  [&::-moz-range-thumb]:bg-foreground"
              />
            </div>
            <p className="mt-2 text-[11px] leading-relaxed text-muted-foreground">
              色相决定「什么颜色」，明暗由亮/暗模式与固定的亮度值控制，
              因此任何色相下对比度都保持一致。
            </p>
          </div>
        </div>
      )}
    </div>
  )
}
