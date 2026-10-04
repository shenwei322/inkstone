'use client'

import { useCallback, useSyncExternalStore } from 'react'

/**
 * 响应式媒体查询 hook。SSR 与首次客户端渲染都返回 false（避免 hydration 不一致），
 * hydration 完成后读取真实值并监听变化。
 *
 * 这里用 useSyncExternalStore 而不是 useState + useEffect 内同步 setState：
 * 项目启用了 react-hooks/set-state-in-effect 规则（见 use-mounted.ts 的说明），
 * effect 体内直接 setState 会报错；用外部 store 还能顺带消除「首帧闪一下
 * 移动端布局」的问题（桌面端 hydration 后不会先渲染成 false 再切换）。
 */
export function useMediaQuery(query: string): boolean {
  const subscribe = useCallback(
    (onStoreChange: () => void) => {
      const mql = window.matchMedia(query)
      mql.addEventListener('change', onStoreChange)
      return () => mql.removeEventListener('change', onStoreChange)
    },
    [query],
  )

  const getSnapshot = useCallback(() => window.matchMedia(query).matches, [query])

  return useSyncExternalStore(
    subscribe,
    getSnapshot,
    // SSR：媒体查询在服务端无意义，恒为 false，保证 hydration 一致
    () => false,
  )
}
