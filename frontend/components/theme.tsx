'use client'

import {
  createContext,
  useContext,
  useEffect,
  useSyncExternalStore,
  type ReactNode,
} from 'react'

import { THEME_STORAGE_KEY } from '@/lib/theme'
import { useSiteConfig } from '@/components/site-config-context'
import {
  ACCENT_STORAGE_KEY,
  DEFAULT_ACCENT_ID,
  parseAccentChoice,
  resolveHue,
  serializeAccentChoice,
  type AccentChoice,
} from '@/lib/accent'

export type Theme = 'light' | 'dark'

/*
 * 用 useSyncExternalStore 管理主题（而非 useState + effect）：
 * ① SSR 快照固定为 'light'，客户端 hydration 后 React 自动用真实快照重渲染，
 *    避免「服务端亮 / 客户端暗」的 hydration mismatch；
 * ② 规避项目 ESLint 的 react-hooks/set-state-in-effect（effect 内不能同步 setState），
 *    主题切换是 DOM class + 外部 store 通知，不走组件 state。
 *
 * 配色主题（accent）沿用同一套机制：两个维度各自一个 snapshot，
 * 各自的通知合并进同一个 listeners 集合 —— 订阅者收到通知后两个值都会重读，
 * 不会出现「换了色但亮暗状态没更新」这类半更新。
 */
const listeners = new Set<() => void>()
let snapshot: Theme = 'light'
let accentSnapshot: AccentChoice = { kind: 'preset', id: DEFAULT_ACCENT_ID }

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

const getAccentSnapshot = (): AccentChoice => accentSnapshot
/* SSR 与首次渲染都用默认主题：用户的选择只在 localStorage 里，
   服务端渲染不出正确值；内联脚本已在绘制前把 CSS 变量写好，
   所以首帧视觉正确，这里只需保证 hydration 不 mismatch。

   必须是**同一个对象引用**：useSyncExternalStore 用 Object.is 比较快照，
   每次调用都返回新对象会被判定为「状态一直变化」，触发无穷重渲染
   （React 报 getServerSnapshot should be cached to avoid an infinite loop）。 */
const ACCENT_SERVER_SNAPSHOT: AccentChoice = { kind: 'preset', id: DEFAULT_ACCENT_ID }
const getAccentServerSnapshot = (): AccentChoice => ACCENT_SERVER_SNAPSHOT

// 客户端 bundle 执行即同步一次真实值：layout 里的内联脚本已按
// localStorage/系统偏好提前给 <html> 挂好 class 与 CSS 变量，这里只需读取它。
if (typeof document !== 'undefined') {
  snapshot = isDarkApplied() ? 'dark' : 'light'
  try {
    const fromDom = document.documentElement.style.getPropertyValue('--hue').trim()
    const stored = parseAccentChoice(localStorage.getItem(ACCENT_STORAGE_KEY))
    // 优先用 localStorage（含 custom: 前缀的自定义色相，无法从 --hue 反推)
    if (stored) accentSnapshot = stored
    else if (fromDom) accentSnapshot = { kind: 'custom', hue: Number(fromDom) }
  } catch {
    // 隐私模式 / 存储被禁用：保持默认主题
  }
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

/* --------------------------------------------------------------------------
   配色主题
   -------------------------------------------------------------------------- */

const applyAccent = (choice: AccentChoice) => {
  const { hue, hueDark } = resolveHue(choice)
  const root = document.documentElement
  root.style.setProperty('--hue', String(hue))
  root.style.setProperty('--hue-accent', String(hueDark))
}

const persistAccent = (choice: AccentChoice) => {
  try {
    localStorage.setItem(ACCENT_STORAGE_KEY, serializeAccentChoice(choice))
  } catch {
    // 同上：存储不可用时仅在当前页面生效
  }
}

const setAccent = (choice: AccentChoice) => {
  accentSnapshot = choice
  persistAccent(choice)
  applyAccent(choice)
  emit()
}

/** 清除自定义主题，回到默认 */
const resetAccent = () => setAccent({ kind: 'preset', id: DEFAULT_ACCENT_ID })

interface ThemeContextValue {
  theme: Theme
  setTheme: (t: Theme) => void
  toggleTheme: () => void
  /** 当前配色主题（预设或自定义色相） */
  accent: AccentChoice
  setAccent: (c: AccentChoice) => void
  resetAccent: () => void
}

const ThemeContext = createContext<ThemeContextValue>({
  theme: 'light',
  setTheme: () => {},
  toggleTheme: () => {},
  accent: { kind: 'preset', id: DEFAULT_ACCENT_ID },
  setAccent: () => {},
  resetAccent: () => {},
})

export function ThemeProvider({ children }: { children: ReactNode }) {
  const theme = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)
  const accent = useSyncExternalStore(subscribe, getAccentSnapshot, getAccentServerSnapshot)
  const site = useSiteConfig()

  /* 站点默认配色兜底：
     首屏内联脚本不知道站点配置（那是接口返回的，脚本执行时还没有），
     所以它只能先写内置默认值。等配置到达后，对「本地确实没有自己的选择」
     的访客补写一次站点默认色。已经有选择的用户不受影响 —— 
     这正是「用户自选 > 站点默认 > 内置默认」这条优先级的落地点。 */
  useEffect(() => {
    if (!site.accentHue) return
    let hasOwnChoice = false
    try {
      hasOwnChoice = localStorage.getItem(ACCENT_STORAGE_KEY) !== null
    } catch {
      // 存储不可用：不能判断，按「无选择」处理
    }
    if (hasOwnChoice) return
    // 已在渲染前写好同名变量时无需重复写（避免每次配置刷新都触发布局失效）
    const current = document.documentElement.style.getPropertyValue('--hue').trim()
    if (current === String(site.accentHue)) return
    applyAccent({ kind: 'custom', hue: site.accentHue })
  }, [site.accentHue])

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

  // 多标签页同步：另一个标签页切换主题或配色后本地立即跟上
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === THEME_STORAGE_KEY) {
        const t = e.newValue === 'dark' || e.newValue === 'light' ? e.newValue : null
        if (t && t !== snapshot) {
          snapshot = t
          apply(t)
          emit()
        }
        return
      }
      if (e.key === ACCENT_STORAGE_KEY) {
        const choice = parseAccentChoice(e.newValue)
        if (!choice) return
        const same =
          accentSnapshot.kind === choice.kind &&
          (choice.kind === 'custom'
            ? accentSnapshot.kind === 'custom' && accentSnapshot.hue === choice.hue
            : accentSnapshot.kind === 'preset' && accentSnapshot.id === choice.id)
        if (same) return
        accentSnapshot = choice
        applyAccent(choice)
        emit()
      }
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [])

  return (
    <ThemeContext.Provider
      value={{ theme, setTheme, toggleTheme, accent, setAccent, resetAccent }}
    >
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme() {
  return useContext(ThemeContext)
}
