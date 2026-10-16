'use client'

import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Power } from 'lucide-react'
import { updateAdminSettings, ApiError } from '@/lib/api'
import { useNotify } from '@/components/toast'
import { useSiteConfig, useSiteConfigActions } from '@/components/site-config-context'
import { GsapReveal, GsapSwitch } from '@/components/gsap'

/**
 * 网站维护模式开关（后台概览页）。开启后非管理员访客看到全屏维护页，
 * 管理员不受影响。保存后刷新站点配置，使前台门禁即时生效。
 */
export function MaintenanceToggle() {
  const site = useSiteConfig()
  const { refresh } = useSiteConfigActions()
  const notify = useNotify()

  const [on, setOn] = useState(site.maintenanceMode)
  const [lastServer, setLastServer] = useState(site.maintenanceMode)

  // 站点配置异步加载或外部改动时同步开关状态（渲染期调整，避免 effect 内 setState）
  if (site.maintenanceMode !== lastServer) {
    setLastServer(site.maintenanceMode)
    setOn(site.maintenanceMode)
  }

  const mutation = useMutation({
    mutationFn: (next: boolean) => updateAdminSettings({ maintenance_mode: next }),
    onSuccess: (_data, next) => {
      refresh()
      notify.success(next ? '已开启维护模式，访客将看到维护页' : '已关闭维护模式，网站恢复正常访问')
    },
    onError: (e) => {
      setOn(site.maintenanceMode)
      notify.error(e instanceof ApiError ? e.message : '操作失败')
    },
  })

  const handleToggle = (next: boolean) => {
    if (mutation.isPending) return
    setOn(next)
    mutation.mutate(next)
  }

  return (
    <GsapReveal className="card-flat rounded-2xl border border-border bg-card p-5" y={12}>
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <div
            className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg transition-colors ${
              on ? 'bg-amber-500/10 text-amber-600 dark:text-amber-400' : 'bg-muted text-muted-foreground'
            }`}
          >
            <Power className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <h2 className="flex flex-wrap items-center gap-2 text-sm font-semibold">
              网站维护模式
              {on && (
                <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-[10px] font-medium text-amber-600 dark:text-amber-400">
                  已开启
                </span>
              )}
            </h2>
            <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
              开启后，除管理员外的访客将看到「网站维护中」页面；你仍可正常管理后台与浏览前台。
            </p>
          </div>
        </div>
        <GsapSwitch
          checked={on}
          onChange={handleToggle}
          disabled={mutation.isPending}
          label="切换网站维护模式"
        />
      </div>
    </GsapReveal>
  )
}
