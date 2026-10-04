/**
 * 主题持久化的 localStorage key。
 *
 * 放在独立纯模块（无 'use client'）：server component 的 app/layout.tsx 需要它拼
 * 首屏防闪烁内联脚本，而 client 的 components/theme.tsx 又要读写同一 key。
 * 若直接放在 'use client' 的 theme.tsx 里，server 端 import 到的导出会是
 * undefined（React client reference 只转发组件，不转发值）。
 */
export const THEME_STORAGE_KEY = 'inkstone-theme'
