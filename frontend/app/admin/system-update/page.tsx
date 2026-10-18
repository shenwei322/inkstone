'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { PageLoading } from '@/components/page-loader'

/**
 * 旧路由兼容：`/admin/system-update` → `/admin/system`。
 *
 * 这一页在 Beta1.29 并入了「系统维护」。留一个跳转而不是直接删掉目录，
 * 是因为后台地址会被收藏、也会出现在运维文档与日志的「前往该页」链接里，
 * 直接 404 会让人以为是故障。
 *
 * 用 `replace` 而非 `push`：不在浏览器历史里留下一个「只用来跳转」的中间页，
 * 否则用户点后退会闪一下又跳回来。
 *
 * 不带 `?tab=update` 是刻意的：目标页刻意不读 searchParams（会把整页拖成
 * CSR bailout），带过去也没人消费，只会让 URL 长出无意义的查询串。
 * 「版本与更新」本就是该页的默认标签页。
 */
export default function LegacySystemUpdateRedirect() {
  const router = useRouter()

  useEffect(() => {
    router.replace('/admin/system')
  }, [router])

  return <PageLoading minHeight="16rem" />
}
