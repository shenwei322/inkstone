'use client'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useState } from 'react'
import { AuthProvider } from '@/lib/auth-context'
import { NotifyProvider } from '@/components/toast'
import { SiteConfigProvider } from '@/components/site-config-context'
import { ThemeProvider } from '@/components/theme'

export function Providers({ children }: { children: React.ReactNode }) {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 30 * 1000,
            retry: 1,
          },
        },
      }),
  )

  return (
    <QueryClientProvider client={queryClient}>
      {/* SiteConfigProvider 必须在外层：ThemeProvider 要用站点默认配色
          （accent_hue）作为「访客本地无选择」时的兜底，读不到配置就只能是硬编码值。 */}
      <SiteConfigProvider>
        <ThemeProvider>
          <AuthProvider>
            <NotifyProvider>{children}</NotifyProvider>
          </AuthProvider>
        </ThemeProvider>
      </SiteConfigProvider>
    </QueryClientProvider>
  )
}
