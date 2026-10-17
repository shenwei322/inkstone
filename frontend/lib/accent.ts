/**
 * 配色主题（accent）定义与持久化。
 *
 * 与「亮/暗模式」是两个正交的维度：
 *   - 亮暗模式 → <html> 的 .dark class（见 components/theme.tsx）
 *   - 配色主题 → <html> 上的 --hue / --hue-accent 内联变量
 * 两者组合出「暗色 + 青绿」这样的具体外观，互不覆盖。
 *
 * 为什么用色相而不用一整套色值：
 * globals.css 的颜色都由 oklch(... L C hue) 表达，L（亮度）与 C（彩度）
 * 是写死的，只有色相可变。于是「换主题色」＝改一个数字，
 * 而亮度对比度在所有主题下保持一致 —— 换任何色都不会出现某个主题下
 * 文字看不清的问题（这正是 oklch 相对 hex 的核心优势）。
 *
 * 放在独立纯模块（无 'use client'）：server component 的 app/layout.tsx 要
 * 用它拼首屏防闪烁内联脚本，client 组件又要读同一份预设表。
 */

export const ACCENT_STORAGE_KEY = 'inkstone-accent'

/** 预设配色：hue 取 oklch 色相角（0-360）。 */
export interface AccentPreset {
  /** 稳定的标识，存进 localStorage；不用 hue 值本身当 key，方便日后微调色相而不丢用户选择 */
  id: string
  label: string
  /** 浅色模式色相 */
  hue: number
  /** 深色模式色相；不填则用 hue。深色下通常略偏，让主色更跳（见 globals.css 的 250→265） */
  hueDark?: number
}

export const ACCENT_PRESETS: readonly AccentPreset[] = [
  { id: 'blue', label: '靛蓝', hue: 250, hueDark: 265 },
  { id: 'cyan', label: '青碧', hue: 195, hueDark: 205 },
  { id: 'green', label: '松绿', hue: 150, hueDark: 158 },
  { id: 'amber', label: '琥珀', hue: 65, hueDark: 75 },
  { id: 'orange', label: '赤橙', hue: 35, hueDark: 45 },
  { id: 'rose', label: '玫红', hue: 10, hueDark: 15 },
  { id: 'purple', label: '紫菫', hue: 305, hueDark: 315 },
  { id: 'slate', label: '玄灰', hue: 260, hueDark: 260 },
] as const

/** 默认主题：靛蓝，与 globals.css 里 :root 的 --hue: 250 一致 */
export const DEFAULT_ACCENT_ID = 'blue'

/** 自定义色相的合法范围（与 <input type="range"> 的 min/max 对应） */
export const HUE_MIN = 0
export const HUE_MAX = 360

/**
 * 用户选择的序列化形式：
 *   - 预设：'blue'
 *   - 自定义：'custom:213'
 * 用一个字符串存，避免 localStorage 里出现 JSON 解析失败导致主题丢失。
 */
export type AccentChoice = { kind: 'preset'; id: string } | { kind: 'custom'; hue: number }

export function parseAccentChoice(raw: string | null): AccentChoice | null {
  if (!raw) return null
  if (raw.startsWith('custom:')) {
    const hue = Number(raw.slice('custom:'.length))
    if (!Number.isFinite(hue)) return null
    return { kind: 'custom', hue: normalizeHue(hue) }
  }
  return ACCENT_PRESETS.some((p) => p.id === raw) ? { kind: 'preset', id: raw } : null
}

export function serializeAccentChoice(c: AccentChoice): string {
  return c.kind === 'custom' ? `custom:${normalizeHue(c.hue)}` : c.id
}

/** 把任意数字收进 0-360，并取整（避免内联样式里出现 213.00000001） */
export function normalizeHue(h: number): number {
  if (!Number.isFinite(h)) return 250
  return Math.round(((h % 360) + 360) % 360)
}

/** 把一个选择解析成实际要写到 <html> 上的两个色相值 */
export function resolveHue(choice: AccentChoice): { hue: number; hueDark: number } {
  if (choice.kind === 'custom') {
    // 自定义色相下，深色沿用同色相：用户既然精确指定了数字，
    // 再替他偏移会显得「我设的 213 显示出来不是 213」。
    return { hue: choice.hue, hueDark: choice.hue }
  }
  const preset = ACCENT_PRESETS.find((p) => p.id === choice.id) ?? ACCENT_PRESETS[0]
  return { hue: preset.hue, hueDark: preset.hueDark ?? preset.hue }
}

/**
 * 首屏防闪烁脚本（内联在 <head> 之前执行）。
 *
 * 必须在浏览器绘制前把 --hue 写好，否则会先闪一帧默认蓝再跳到用户选的色。
 * 与 app/layout.tsx 里现有的 THEME_INIT_SCRIPT 并列注入。
 *
 * 注意：服务端也不能渲染带 accent 的 style —— 用户的选择只存在 localStorage，
 * 服务端无从得知。所以这里写内联样式（最高优先级的 element style），
 * 直接覆盖 globals.css 里 :root 的默认值。
 *
 * @param siteHue 站点默认色相（后台可配）。仅当访客本地没有任何选择时使用；
 *                传 undefined 则退回 250。这样「站点默认主题」与「用户自选」
 *                的优先级是：用户自选 > 站点默认 > 内置 250。
 */
export function buildAccentInitScript(siteHue?: number): string {
  const key = JSON.stringify(ACCENT_STORAGE_KEY)
  const presets = JSON.stringify(
    Object.fromEntries(ACCENT_PRESETS.map((p) => [p.id, [p.hue, p.hueDark ?? p.hue]])),
  )
  const fallbackHue = normalizeHue(siteHue ?? ACCENT_PRESETS[0].hue)
  return `(function(){try{var k=${key};var P=${presets};var F=${fallbackHue};var raw=localStorage.getItem(k);var h=null,hd=null;
if(raw){if(raw.indexOf('custom:')===0){var n=Number(raw.slice(7));if(isFinite(n)){n=Math.round(((n%360)+360)%360);h=n;hd=n;}}
else if(P[raw]){h=P[raw][0];hd=P[raw][1];}}
if(h===null){h=F;hd=F;}
var r=document.documentElement;r.style.setProperty('--hue',String(h));r.style.setProperty('--hue-accent',String(hd));
}catch(e){}})();`
}
