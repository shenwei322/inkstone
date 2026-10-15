'use client'

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react'
import {
  AUTH_EXPIRED_EVENT,
  clearTokens,
  fetchMe,
  getAccessToken,
  hasSessionHint,
  login as apiLogin,
  logout as apiLogout,
  register as apiRegister,
  saveTokens,
  tryRefresh,
  type CaptchaCredential,
} from './api'
import type { TokenPair, User } from './types'

interface AuthContextValue {
  user: User | null
  loading: boolean
  login: (
    email: string,
    password: string,
    extra?: { email_code?: string; remember_me?: boolean } & CaptchaCredential,
  ) => Promise<User>
  register: (
    email: string,
    username: string,
    password: string,
    extra?: { email_code?: string } & CaptchaCredential,
  ) => Promise<User>
  logout: () => void
  applyTokens: (token: TokenPair, user: User) => void
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined)

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  /**
   * 启动时恢复登录态。
   *
   * 关键在于「localStorage 里没有 access 令牌」**不等于**未登录：
   * 会话 Cookie 是 HttpOnly 的，服务端认识这个浏览器，但 JS 看不见。
   * 移动端 Safari 会清理脚本可写存储，桌面浏览器也可能被用户清站点数据，
   * 两种情况下 Cookie 都还在。因此这里按三级降级处理：
   *
   *   1. 有 access 令牌 → 直接问 /auth/me；
   *   2. 没令牌但有会话标记 → 先续期再问 /auth/me；
   *   3. 都没有 → 未登录（不发请求，避免给每个访客制造无意义的 401）。
   *
   * 只在这一步之后才允许渲染「未登录」界面，否则会出现
   * 「刷新页面时闪一下登录页，然后又变回已登录」的抖动。
   */
  useEffect(() => {
    let cancelled = false

    const settle = (next: User | null) => {
      if (cancelled) return
      setUser(next)
      setLoading(false)
    }

    const restore = async () => {
      const token = getAccessToken()
      if (!token && !hasSessionHint()) {
        // 无任何会话线索（令牌与会话标记都没有）：判定未登录。
        // 用一次微任务延后，避免在 effect 体内同步 setState
        // （项目 ESLint 硬性拦截 react-hooks/set-state-in-effect）。
        await Promise.resolve()
        settle(null)
        return
      }
      if (!token) {
        // 有服务端会话但本地令牌被清：先续期拿回 access 令牌。
        const refreshed = await tryRefresh()
        if (!refreshed) {
          settle(null)
          return
        }
      }
      try {
        const res = await fetchMe()
        settle(res.user)
      } catch {
        // fetchMe 内部已在 401 且刷新失败时清掉令牌并派发失效事件；
        // 这里只需保证不再显示「加载中」。
        settle(null)
      }
    }

    void restore()
    return () => {
      cancelled = true
    }
  }, [])

  // 会话在其它标签页/其它请求里失效时，同步清空内存里的 user。
  //
  // api.ts 只负责派发事件（它不该持有路由知识），跳不跳登录页由各页面
  // 自己决定：后台页会因 user 为空而自动跳转，前台只是回到访客状态。
  useEffect(() => {
    const onExpired = () => setUser(null)
    window.addEventListener(AUTH_EXPIRED_EVENT, onExpired)
    return () => window.removeEventListener(AUTH_EXPIRED_EVENT, onExpired)
  }, [])

  // 多个标签页共享同一条会话：一个标签页登出后，其它标签页应当同步退出，
  // 否则用户会看到「这边显示已登录、那边已经退出」的矛盾状态。
  useEffect(() => {
    const onStorage = () => {
      if (!getAccessToken() && !hasSessionHint()) setUser(null)
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [])

  const applyTokens = useCallback((token: TokenPair, nextUser: User) => {
    saveTokens(token.access_token, token.refresh_token, token.session_expires_in)
    setUser(nextUser)
  }, [])

  const login = useCallback(
    async (
      email: string,
      password: string,
      extra?: { email_code?: string; remember_me?: boolean } & CaptchaCredential,
    ) => {
      const res = await apiLogin(email, password, extra)
      applyTokens(res.token, res.user)
      return res.user
    },
    [applyTokens],
  )

  const register = useCallback(
    async (
      email: string,
      username: string,
      password: string,
      extra?: { email_code?: string } & CaptchaCredential,
    ) => {
      const res = await apiRegister(email, username, password, extra)
      applyTokens(res.token, res.user)
      return res.user
    },
    [applyTokens],
  )

  const logout = useCallback(() => {
    // 先清本地状态（UI 立刻响应），服务端清 Cookie 在后台完成。
    // 顺序很重要：反过来的话，网络慢时用户会觉得「点了退出没反应」。
    clearTokens()
    setUser(null)
    void apiLogout()
  }, [])

  const value = useMemo(
    () => ({ user, loading, login, register, logout, applyTokens }),
    [user, loading, login, register, logout, applyTokens],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
