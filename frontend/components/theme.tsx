'use client'

import {
  createContext,
  useContext,
  useEffect,
  useSyncExternalStore,
  type ReactNode,
} from 'react'

import { THEME_STORAGE_KEY } from '@/lib/theme'

export type Theme = 'light' | 'dark'

/*
 * 用 useSyncExternalStore 管理主题（而非 useState + effect）：
 * ① SSR 快照固定为 'light'，客户端 hydration 后 React 自动用真实快照重渲染，
 *    避免「服务端亮 / 客户端暗」的 hydration mismatch；
 * ② 规避项目 ESLint 的 react-hooks/set-state-in-effect（effect 内不能同步 setState），
 *    主题切换是 DOM class + 外部 store 通知，不走组件 state。
 */
const listeners = new Set<() => void>()
let snapshot: Theme = 'light'

const emit = () => listeners.forEach((l) => l())
const subscribe = (l: () => void) => {
  listeners.add(l)
  return () => {
    listeners.delete(l)
  }
}

const isDarkApplied = () =>
  typeof document !== 'undefined' && document.documentElement.classList.contains('dark')

const getSnapshot = (): Theme => snapshot
// SSR 快照：内联脚本未执行、<html> 还没有 dark class 时的占位值
const getServerSnapshot = (): Theme => 'light'

// 客户端 bundle 执行即同步一次真实值：layout 里的内联脚本已按
// localStorage/系统偏好提前给 <html> 挂好 class，这里只需读取它。
if (typeof document !== 'undefined') {
  snapshot = isDarkApplied() ? 'dark' : 'light'
}

const apply = (t: Theme) => {
  const root = document.documentElement
  root.classList.toggle('dark', t === 'dark')
  root.style.colorScheme = t
}

const persist = (t: Theme) => {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, t)
  } catch {
    // 隐私模式 / 存储被禁用时忽略，主题仅在当前页面生效
  }
}

const readStored = (): Theme | null => {
  try {
    const v = localStorage.getItem(THEME_STORAGE_KEY)
    return v === 'dark' || v === 'light' ? v : null
  } catch {
    return null
  }
}

const setTheme = (t: Theme) => {
  if (snapshot === t) return
  snapshot = t
  persist(t)
  apply(t)
  emit()
}

const toggleTheme = () => setTheme(snapshot === 'dark' ? 'light' : 'dark')

interface ThemeContextValue {
  theme: Theme
  setTheme: (t: Theme) => void
  toggleTheme: () => void
}

const ThemeContext = createContext<ThemeContextValue>({
  theme: 'light',
  setTheme: () => {},
  toggleTheme: () => {},
})

export function ThemeProvider({ children }: { children: ReactNode }) {
  const theme = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)

  // 用户没有手动选择过（localStorage 无值）时，跟随系统偏好实时变化
  useEffect(() => {
    if (readStored()) return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (e: MediaQueryListEvent) => {
      const t: Theme = e.matches ? 'dark' : 'light'
      snapshot = t
      apply(t)
      emit()
    }
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])

  // 多标签页同步：另一个标签页切换主题后本地立即跟上
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key !== THEME_STORAGE_KEY) return
      const t = e.newValue === 'dark' || e.newValue === 'light' ? e.newValue : null
      if (t && t !== snapshot) {
        snapshot = t
        apply(t)
        emit()
      }
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [])

  return (
    <ThemeContext.Provider value={{ theme, setTheme, toggleTheme }}>
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme() {
  return useContext(ThemeContext)
}
