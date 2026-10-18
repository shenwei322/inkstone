'use client'

import { useState } from 'react'
import { Database, RefreshCw } from 'lucide-react'
import { AdminPageHeader } from '@/components/admin-page'
import { AdminTabPanel, AdminTabs } from '@/components/admin-tabs'
import { SystemUpdatePanel } from '@/components/system-update-panel'
import { BackupPanel } from '@/components/backup-panel'

/**
 * 系统维护（合并原「系统更新」与「备份恢复」两页）。
 *
 * 合并的理由：这两件事在运维流程上是相邻的——看到「有新版本」之后第一件事
 * 就是备份，然后才在服务器上更新。此前它们分居侧栏中间与靠后两个位置，
 * 一次升级要来回跳两次页面，还容易漏掉备份这一步。
 *
 * 保留原有语义：后台**只提示版本**，不执行更新；备份**不提供一键还原**。
 * 这两条都是刻意设计，不因为合并页面而改变。
 *
 * 刻意**不读 URL 参数**决定初始 tab：那会让本页依赖 useSearchParams，
 * 在 App Router 下整页退化成 CSR bailout，必须再包一层 Suspense 才能静态化。
 * 旧路由 `/admin/backups` 只是跳到本页，多一次点击远比这层复杂度划算。
 */

const tabItems = [
  { key: 'update', label: '版本与更新', icon: RefreshCw },
  { key: 'backup', label: '数据备份', icon: Database },
] as const

export default function AdminSystemPage() {
  const [tab, setTab] = useState<string>('update')

  return (
    <div>
      <AdminPageHeader
        title="系统维护"
        description={
          tab === 'update'
            ? '检查上游新版本：后台只提示，更新需在服务器上手动执行'
            : '把站点内容导出为快照文件，供人工下载与长期留存'
        }
      />

      <div className="mt-6">
        <AdminTabs items={tabItems} value={tab} onChange={setTab} />
      </div>

      <AdminTabPanel value={tab}>
        {tab === 'update' ? <SystemUpdatePanel /> : <BackupPanel />}
      </AdminTabPanel>
    </div>
  )
}
