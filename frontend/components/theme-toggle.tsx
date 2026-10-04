'use client'

import { Moon, Sun } from 'lucide-react'
import { useSyncExternalStore } from 'react'
import { useTheme } from './theme'

const emptySubscribe = () => () => {}
const mountedSnapshot = () => true
// SSR 快照 false：避免服务端按默认 'light' 渲染月亮图标、
// 而客户端实际是暗色（太阳图标）导致的 hydration mismatch
const serverSnapshot = () => false

/**
 * 明暗主题切换按钮。图标在挂载后才渲染真实态（用 useSyncExternalStore 探测
 * mounted，不写 effect setState）；挂载前输出同尺寸占位，避免布局跳动。
 */
export function ThemeToggle({ className }: { className?: string }) {
  const { theme, toggleTheme } = useTheme()
  const mounted = useSyncExternalStore(emptySubscribe, mountedSnapshot, serverSnapshot)
  const dark = mounted && theme === 'dark'
  const label = dark ? '切换到亮色模式' : '切换到暗色模式'

  return (
    <button
      type="button"
      onClick={toggleTheme}
      aria-label={label}
      title={label}
      className={className}
    >
      {mounted ? (
        dark ? (
          <Sun key="sun" className="h-4 w-4 animate-scale-in" />
        ) : (
          <Moon key="moon" className="h-4 w-4 animate-scale-in" />
        )
      ) : (
        <span className="h-4 w-4" />
      )}
    </button>
  )
}
