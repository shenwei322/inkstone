'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { PageLoading } from '@/components/page-loader'

/**
 * 旧路由兼容：`/admin/backups` → `/admin/system`。
 * 与 `system-update/page.tsx` 同因，见那里的注释。
 *
 * 不带 tab 参数是刻意的：目标页刻意不读 searchParams（会把整页拖成
 * CSR bailout），这里带过去也没人消费，只会让 URL 长出无意义的查询串。
 */
export default function LegacyBackupsRedirect() {
  const router = useRouter()

  useEffect(() => {
    router.replace('/admin/system')
  }, [router])

  return <PageLoading minHeight="16rem" />
}
