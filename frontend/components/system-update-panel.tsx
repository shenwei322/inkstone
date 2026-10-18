'use client'

import { useEffect, useRef } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  ArrowDownToLine,
  CheckCircle2,
  Clock,
  ExternalLink,
  GitCommitHorizontal,
  History,
  Info,
  RefreshCw,
  Server,
  ShieldAlert,
  Terminal,
} from 'lucide-react'
import {
  ApiError,
  checkSystemUpdate,
  fetchUpdateStatus,
  type UpdateStatus,
} from '@/lib/api'
import { useNotify } from '@/components/toast'
import { Reveal, hoverTapScale } from '@/components/motion'
import { RowLoading, Spinner } from '@/components/page-loader'
import { AdminEmpty } from '@/components/admin-page'
import { badgeDanger, badgeSuccess, badgeWarning, formatSize } from '@/lib/ui'

/**
 * 「系统更新」面板（原 `/admin/system-update` 页的正文）。
 *
 * 抽成组件是为了并入 `/admin/system` 的「版本与更新」标签页——
 * 它与「数据备份」是同一件事（运维维护）的两个方面，此前占两个侧栏条目。
 *
 * 操作按钮（刷新状态 / 检查更新）**留在面板内部**而不是页头 actions：
 * 页头的 actions 是整页共用的，接到 tab 上就得把 mutation 提升到父组件，
 * 换来的是「切到备份 tab 后，页头还挂着检查更新按钮」这种错位。
 */

function formatTime(value?: string) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

/** 版本对比说明：commits 模式看落后提交数；releases 模式看版本号 */
function versionCompareLabel(version?: UpdateStatus['version'], source?: string) {
  if (!version) return '点击「检查更新」获取'
  if (source === 'releases') {
    if (!version.latest_version) return '点击「检查更新」获取'
    if (version.update_available) {
      return `当前 ${version.current_version || '未知版本'} → 最新 ${version.latest_version}`
    }
    return `当前版本 ${version.current_version || version.latest_version} 已是最新`
  }
  if (!version.latest?.hash) return '点击「检查更新」获取'
  const behind = version.behind ?? -1
  return behind >= 0 ? `落后 ${behind} 个提交` : '无法确定落后提交数'
}

export function SystemUpdatePanel() {
  const notify = useNotify()
  const queryClient = useQueryClient()
  const autoChecked = useRef(false)

  // 先读本地状态（不联网），再按需自动检查一次
  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['admin', 'system', 'update'],
    queryFn: fetchUpdateStatus,
    // 后台不执行更新，没有进行中的任务需要轮询；状态只在进入页面与手动刷新时读取
  })

  const update: UpdateStatus | undefined = data?.update
  const version = update?.version

  const check = useMutation({
    mutationFn: checkSystemUpdate,
    onSuccess: (res) => {
      queryClient.setQueryData(['admin', 'system', 'update'], (prev: typeof data) =>
        prev ? { ...prev, update: res.update } : prev,
      )
      const info = res.update.version
      if (info.update_available) {
        notify.success(
          res.update.source === 'releases'
            ? `发现新版本 ${info.latest_version}，请按页面指引手动更新`
            : info.behind > 0
              ? `发现新版本，落后 ${info.behind} 个提交，请手动更新`
              : '发现新版本，请手动更新',
        )
      } else {
        notify.success('已是最新版本')
      }
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '检查更新失败'),
  })

  // 进入页面若还没检查过（或超过 10 分钟），自动检查一次
  useEffect(() => {
    if (!update || autoChecked.current) return
    const last = version?.last_checked ? new Date(version.last_checked).getTime() : 0
    const stale = !last || Date.now() - last > 10 * 60 * 1000
    if (!stale || update.enabled === false) return
    autoChecked.current = true
    check.mutate()
    // 只在首次拿到状态后触发一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [update, version?.last_checked])

  const agent = data?.agent

  if (isLoading) return <RowLoading rows={4} />
  if (!update) return <AdminEmpty title="无法读取更新状态，请刷新重试" />

  return (
    <div>
      {/* 面板工具条：状态摘要 + 两个操作 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          {update
            ? `上游 ${update.repo_name || '仓库'} · ${
                update.source === 'releases'
                  ? '更新源：GitHub Releases（镜像包）'
                  : `分支 ${update.branch}`
              } · 更新方式：手动（本页只提示新版本）`
            : '检查上游新版本：后台只提示，更新需在服务器上手动执行'}
        </p>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => refetch()}
            disabled={isFetching}
            {...hoverTapScale}
            className="flex items-center gap-1.5 rounded-lg border border-border bg-card px-3.5 py-2 text-sm font-medium transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4 ${isFetching ? 'animate-spin' : ''}`} />
            刷新状态
          </button>
          <button
            type="button"
            onClick={() => check.mutate()}
            disabled={check.isPending}
            {...hoverTapScale}
            className="flex items-center gap-1.5 rounded-lg bg-accent px-3.5 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 disabled:opacity-50"
          >
            {check.isPending ? <Spinner /> : <ArrowDownToLine className="h-4 w-4" />}
            检查更新
          </button>
        </div>
      </div>

      {/* 更新不可用时的说明（关闭开关 / commits 模式下未探测到源码目录）。
          releases 模式装的是 Release 镜像包，不需要源码目录，不在此告警。 */}
      {!update.enabled || (update.source !== 'releases' && !update.source_dir) ? (
        <Reveal y={12} className="mt-4 rounded-xl border border-amber-500/40 bg-amber-500/5 p-5">
          <div className="flex items-start gap-3">
            <ShieldAlert className="mt-0.5 h-5 w-5 shrink-0 text-amber-600 dark:text-amber-400" />
            <div className="min-w-0">
              <p className="text-sm font-semibold">
                {update.enabled ? '未能确定源码目录' : '新版本检查已关闭'}
              </p>
              <p className="mt-1 text-sm leading-relaxed text-muted-foreground">{update.message}</p>
              {!update.enabled ? (
                <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
                  这是配置项而非故障：把后端环境变量{' '}
                  <code className="rounded bg-muted px-1.5 py-0.5">UPDATE_ENABLED</code> 设为{' '}
                  <code className="rounded bg-muted px-1.5 py-0.5">true</code> 并重启后端即可启用。
                  为避免误覆盖正在开发的源码，本地演示建议把{' '}
                  <code className="rounded bg-muted px-1.5 py-0.5">UPDATE_SOURCE_DIR</code>{' '}
                  指向仓库副本。
                </p>
              ) : null}
            </div>
          </div>
        </Reveal>
      ) : null}

      {/* 版本对比 */}
      <Reveal y={14} duration={0.45} className="mt-4 rounded-xl border border-border bg-card p-5">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="flex items-center gap-2">
            <Server className="h-4 w-4 text-accent" />
            <h2 className="text-sm font-semibold">版本对比</h2>
            {version?.update_available ? (
              <span className={badgeWarning}>有可用更新</span>
            ) : version?.latest.hash ? (
              <span className={badgeSuccess}>已是最新</span>
            ) : (
              <span className={badgeWarning}>尚未检查</span>
            )}
            {version?.pending ? <span className={badgeWarning}>待生效</span> : null}
          </div>
          {/* 这里原本是「立即更新」按钮。后台已改为一律只提示：
              更新动作交给运维在服务器上执行，页面不再提供任何执行入口。 */}
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Info className="h-3.5 w-3.5 shrink-0" />
            后台只提示新版本，更新请按下方步骤手动执行
          </p>
        </div>

        <div className="mt-4 grid gap-4 sm:grid-cols-2">
          <CommitCard
            title="当前运行版本"
            hash={update.source === 'releases' ? version?.current_version : version?.current.hash}
            short={
              update.source === 'releases' ? version?.current_version || '——' : version?.current.short
            }
            date={update.source === 'releases' ? undefined : version?.current.date}
            url={update.source === 'releases' ? undefined : version?.current.url}
            linkLabel="查看提交"
            extra={
              update.source === 'releases'
                ? `来源：${
                    version?.current_version_from === 'version_file'
                      ? '部署版本记录'
                      : '后端 AppVersion（未写入版本记录）'
                  }`
                : version?.current_from === 'deployed'
                  ? '来源：部署记录'
                  : version?.current_from === 'ldflags'
                    ? '来源：编译期注入'
                    : version?.current_from === 'env'
                      ? '来源：环境变量'
                      : '来源：未知（建议让部署脚本写入 deployed-commit.json）'
            }
          />
          <CommitCard
            title={update.source === 'releases' ? '上游最新 Release' : '上游最新版本'}
            hash={update.source === 'releases' ? version?.latest_version : version?.latest.hash}
            short={
              update.source === 'releases'
                ? version?.latest_version || '——'
                : version?.latest.short
            }
            date={
              update.source === 'releases'
                ? version?.release?.published_at || version?.latest.date
                : version?.latest.date
            }
            url={update.source === 'releases' ? version?.release?.url : version?.latest.url}
            linkLabel={update.source === 'releases' ? '查看 Release' : '查看提交'}
            message={
              update.source === 'releases'
                ? version?.release?.name || version?.latest.message
                : version?.latest.message
            }
            author={update.source === 'releases' ? undefined : version?.latest.author}
            extra={versionCompareLabel(version, update.source)}
            highlight={version?.update_available}
          />
        </div>

        <p className="mt-3 text-xs text-muted-foreground">
          上次检查：{formatTime(version?.last_checked)} ·{' '}
          {update.source === 'releases'
            ? `更新目录：${update.update_dir || '未探测到'}（镜像包安装，无需源码目录）`
            : `源码目录：${update.source_dir || '未探测到'}`}
        </p>
        {version?.compare_note ? (
          <p className="mt-1 flex items-start gap-1.5 text-xs text-amber-600 dark:text-amber-400">
            <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
            {version.compare_note}
          </p>
        ) : null}
      </Reveal>

      {/* 有新版本时的醒目提示：后台只负责「告诉你该更新了」，不执行更新 */}
      {version?.update_available ? (
        <Reveal y={12} className="mt-4 rounded-xl border border-accent/40 bg-accent/5 p-5">
          <div className="flex items-start gap-3">
            <ArrowDownToLine className="mt-0.5 h-5 w-5 shrink-0 text-accent" />
            <div className="min-w-0 text-sm">
              <p className="font-semibold">
                {update.source === 'releases'
                  ? `发现新版本 ${version.latest_version || version.latest.short || ''}`
                  : `上游有 ${version.behind > 0 ? `${version.behind} 个` : '新'}提交`}
              </p>
              <p className="mt-1 leading-relaxed text-muted-foreground">
                后台不会自动更新，也不提供一键升级。请按下方「手动更新步骤」在服务器上执行；
                更新前建议先备份数据库与上传目录（同页「数据备份」标签可直接生成快照）。
              </p>
            </div>
          </div>
        </Reveal>
      ) : null}

      {/* 手动更新步骤：命令直接贴在这里，由运维复制到服务器执行 */}
      <Reveal y={12} className="mt-4 rounded-xl border border-border bg-card p-5">
        <div className="flex items-center gap-2">
          <Terminal className="h-4 w-4 text-accent" />
          <h2 className="text-sm font-semibold">手动更新步骤</h2>
        </div>
        <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
          在服务器上进入项目目录执行，按实际部署方式选一种。更新前请先备份数据库与{' '}
          <code className="rounded bg-muted px-1.5 py-0.5">data/</code> 目录。
        </p>

        <div className="mt-4 grid gap-4 lg:grid-cols-2">
          <div className="rounded-lg border border-border bg-background p-4">
            <p className="text-[11px] font-semibold uppercase tracking-widest text-muted-foreground">
              源码 / 二进制部署
            </p>
            <pre className="mt-2 overflow-x-auto whitespace-pre-wrap break-all rounded-md bg-muted px-3 py-2.5 font-mono text-xs leading-relaxed">{`cd /path/to/inkstone
git pull

# 后端
cd backend
go build -tags timetzdata -o server.exe ./cmd/server

# 前端
cd ../frontend
npm ci
npm run build

# 重启后端进程（systemd / 你的进程管理器）`}</pre>
          </div>
          <div className="rounded-lg border border-border bg-background p-4">
            <p className="text-[11px] font-semibold uppercase tracking-widest text-muted-foreground">
              Docker Compose 部署
            </p>
            <pre className="mt-2 overflow-x-auto whitespace-pre-wrap break-all rounded-md bg-muted px-3 py-2.5 font-mono text-xs leading-relaxed">{`cd /path/to/inkstone
git pull

# 源码构建
docker compose -f docker-compose.prod.yml up -d --build

# 或用预构建镜像
docker compose -f docker-compose.prod.yml pull
docker compose -f docker-compose.prod.yml up -d`}</pre>
          </div>
        </div>

        <p className="mt-3 flex items-start gap-1.5 text-xs text-muted-foreground">
          <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          更新完成后回到本页点「检查更新」，确认版本已追平上游。
        </p>
      </Reveal>

      {/* 待生效更新（源码已替换 / 镜像包已下载，等安装） */}
      {version?.pending ? (
        <Reveal y={12} className="mt-4 rounded-xl border border-accent/40 bg-accent/5 p-5">
          <div className="flex items-start gap-3">
            <Clock className="mt-0.5 h-5 w-5 shrink-0 text-accent" />
            <div className="min-w-0 text-sm">
              <p className="font-semibold">
                {version.pending.kind === 'release_image'
                  ? `已下载镜像包 ${version.pending.version || version.pending.short}，等待安装生效`
                  : `已暂存 ${version.pending.short}，等待重建生效`}
              </p>
              <p className="mt-1 leading-relaxed text-muted-foreground">
                {version.pending.kind === 'release_image'
                  ? `Release 镜像包 ${version.pending.image_name || version.pending.short} 已下载并校验完成，等待宿主更新代理 docker load 并重启。`
                  : `源码包已下载并替换完成${
                      version.pending.preview
                        ? `（写入 ${version.pending.preview.writes} 个文件${
                            version.pending.preview.deletes > 0
                              ? `，清理 ${version.pending.preview.deletes} 个`
                              : ''
                          }）`
                        : ''
                    }。`}
                {update.message}
              </p>
              <p className="mt-1 text-xs text-muted-foreground">
                {version.pending.kind === 'release_image' ? '镜像包' : '源码包'}{' '}
                {formatSize(version.pending.bytes ?? 0)} · SHA256{' '}
                {version.pending.sha256?.slice(0, 16) || '—'}… ·{' '}
                {version.pending.kind === 'release_image' ? '下载于' : '暂存于'}{' '}
                {formatTime(version.pending.staged_at)}
              </p>
              {agent ? (
                <p
                  className={`mt-2 flex items-center gap-1.5 text-xs ${
                    agent.success
                      ? 'text-emerald-600 dark:text-emerald-400'
                      : 'text-amber-600 dark:text-amber-400'
                  }`}
                >
                  {agent.success ? (
                    <CheckCircle2 className="h-3.5 w-3.5" />
                  ) : (
                    <AlertTriangle className="h-3.5 w-3.5" />
                  )}
                  宿主代理：{agent.message || agent.state} · {formatTime(agent.finished_at)}
                </p>
              ) : null}
            </div>
          </div>
        </Reveal>
      ) : null}

      {/* 更新日志 / Release 发布说明 */}
      <Reveal y={12} className="mt-4 rounded-xl border border-border bg-card p-5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="flex items-center gap-2 text-sm font-semibold">
            <GitCommitHorizontal className="h-4 w-4 text-accent" />
            {update.source === 'releases' ? '发布说明' : '更新日志'}
            {update.source !== 'releases' && version?.changelog?.length ? (
              <span className="rounded-full bg-muted px-2 py-0.5 text-xs font-normal text-muted-foreground">
                {version.changelog.length} 条
              </span>
            ) : null}
          </h2>
          {update.source !== 'releases' && version?.changelog_truncated ? (
            <span className="text-xs text-muted-foreground">
              仅展示最新 {version.changelog.length} 条，另有 {version.changelog_offset} 条更早的提交
            </span>
          ) : null}
        </div>

        {update.source === 'releases' ? (
          version?.release ? (
            <div className="mt-3 space-y-3">
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <span className="rounded-full bg-muted px-2 py-0.5 font-mono">
                  {version.release.tag}
                </span>
                {version.release.prerelease ? (
                  <span className="rounded-full bg-amber-500/10 px-2 py-0.5 text-amber-600 dark:text-amber-400">
                    预发布
                  </span>
                ) : null}
                <span>发布于 {formatTime(version.release.published_at)}</span>
                {version.release.url ? (
                  <a
                    href={version.release.url}
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-1 text-accent hover:underline"
                  >
                    在 GitHub 查看
                    <ExternalLink className="h-3 w-3" />
                  </a>
                ) : null}
              </div>
              {version.release.body ? (
                <p className="whitespace-pre-wrap break-words rounded-lg border border-border bg-background px-3 py-2.5 text-sm leading-relaxed">
                  {version.release.body}
                </p>
              ) : (
                <p className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
                  该 Release 没有发布说明
                </p>
              )}
              {version.release.assets.length > 0 ? (
                <div>
                  <p className="text-xs font-medium text-muted-foreground">
                    发布资产（{version.release.assets.length}）
                  </p>
                  <ul className="mt-1.5 space-y-1">
                    {version.release.assets.map((asset) => (
                      <li
                        key={asset.name}
                        className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-background px-3 py-1.5 text-xs"
                      >
                        <span className="font-mono">{asset.name}</span>
                        <span className="text-muted-foreground">
                          {asset.size > 0 ? formatSize(asset.size) : '大小未知'}
                          {asset.name.includes('inkstone-images') ? ' · 镜像包' : ''}
                        </span>
                      </li>
                    ))}
                  </ul>
                </div>
              ) : null}
            </div>
          ) : (
            <p className="mt-3 rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
              尚未获取到 Release 信息。点击「检查更新」拉取上游最新版本。
            </p>
          )
        ) : version?.changelog?.length ? (
          <ol className="mt-3 space-y-2">
            {version.changelog.map((commit) => (
              <li
                key={commit.hash}
                className="flex flex-wrap items-start gap-2 rounded-lg border border-border bg-background px-3 py-2.5"
              >
                <code className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-xs text-accent">
                  {commit.short}
                </code>
                <div className="min-w-0 flex-1">
                  <p className="break-words text-sm">{commit.message || '(无提交说明)'}</p>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    {commit.author} · {formatTime(commit.date)}
                  </p>
                </div>
                {commit.url ? (
                  <a
                    href={commit.url}
                    target="_blank"
                    rel="noreferrer"
                    className="shrink-0 rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-accent"
                    title="在上游查看该提交"
                  >
                    <ExternalLink className="h-3.5 w-3.5" />
                  </a>
                ) : null}
              </li>
            ))}
          </ol>
        ) : (
          <p className="mt-3 rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
            {version?.update_available
              ? '未能获取提交明细（可在后端配置 UPDATE_COMPARE_API / UPDATE_COMMITS_API）'
              : '暂无待应用的更新。点击「检查更新」比对上游最新提交。'}
          </p>
        )}
      </Reveal>

      {/* 更新历史 */}
      <Reveal y={12} className="mt-4 rounded-xl border border-border bg-card p-5">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <History className="h-4 w-4 text-accent" />
          更新历史
        </h2>
        {update.history.length === 0 ? (
          <p className="mt-3 rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
            还没有更新记录
          </p>
        ) : (
          <ul className="mt-3 space-y-2">
            {update.history.map((item, i) => (
              <li
                key={`${item.at}-${i}`}
                className="flex flex-wrap items-start gap-2 rounded-lg border border-border bg-background px-3 py-2.5 text-sm"
              >
                <span
                  className={
                    item.result === 'success'
                      ? badgeSuccess
                      : item.result === 'rolled_back'
                        ? badgeWarning
                        : badgeDanger
                  }
                >
                  {item.result === 'success' ? '成功' : item.result === 'rolled_back' ? '已回滚' : '失败'}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="break-words">
                    {item.from ? `${item.from} → ` : ''}
                    {item.to || '—'}
                    {item.operator ? ` · 操作人 ${item.operator}` : ''}
                  </p>
                  {item.detail ? (
                    <p className="mt-0.5 break-words text-xs text-muted-foreground">{item.detail}</p>
                  ) : null}
                </div>
                <span className="shrink-0 text-xs text-muted-foreground">{formatTime(item.at)}</span>
              </li>
            ))}
          </ul>
        )}
      </Reveal>
    </div>
  )
}

/** 单侧版本卡片 */
function CommitCard({
  title,
  hash,
  short,
  date,
  url,
  message,
  author,
  extra,
  linkLabel,
  highlight,
}: {
  title: string
  hash?: string
  short?: string
  date?: string
  url?: string
  linkLabel?: string
  message?: string
  author?: string
  extra?: string
  highlight?: boolean
}) {
  return (
    <div
      className={`rounded-xl border p-4 ${
        highlight ? 'border-accent/50 bg-accent/5' : 'border-border bg-background'
      }`}
    >
      <p className="text-xs font-medium text-muted-foreground">{title}</p>
      <div className="mt-1.5 flex flex-wrap items-center gap-2">
        <code className="rounded bg-muted px-2 py-1 text-sm font-medium">{short || '——'}</code>
        {url && hash ? (
          <a
            href={url}
            target="_blank"
            rel="noreferrer"
            className="text-xs text-accent hover:underline"
          >
            {linkLabel || '查看提交'}
          </a>
        ) : null}
      </div>
      {message ? <p className="mt-2 break-words text-sm">{message}</p> : null}
      <p className="mt-2 text-xs text-muted-foreground">
        {author ? `${author} · ` : ''}
        {date ? formatTime(date) : '时间未知'}
      </p>
      {extra ? <p className="mt-1 text-xs text-muted-foreground">{extra}</p> : null}
    </div>
  )
}
