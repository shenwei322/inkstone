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
  RotateCcw,
  Server,
  ShieldAlert,
  XCircle,
} from 'lucide-react'
import {
  ApiError,
  applySystemUpdate,
  checkSystemUpdate,
  fetchUpdateStatus,
  rollbackSystemUpdate,
  type UpdateStage,
  type UpdateStatus,
} from '@/lib/api'
import { useNotify } from '@/components/toast'
import { PageTransition, Reveal, hoverTapScale } from '@/components/motion'
import { RowLoading, Spinner } from '@/components/page-loader'
import { badgeDanger, badgeSuccess, badgeWarning, formatSize } from '@/lib/ui'

/** 阶段 → 中文名（后端返回英文阶段码，展示层统一在这里翻译） */
const PHASE_LABEL: Record<string, string> = {
  idle: '空闲',
  staging: '下载源码包',
  swapping: '替换源码',
  rebuilding: '重建服务',
  success: '已完成',
  failed: '失败',
  rolled_back: '已回滚',
}

/** releases 模式（镜像包更新）的阶段文案：没有「替换源码」，只有下载与安装 */
const PHASE_LABEL_RELEASE: Record<string, string> = {
  staging: '下载镜像包',
  swapping: '安装镜像包',
  rebuilding: '安装镜像包',
}

function phaseLabel(phase: string, source?: string) {
  if (source === 'releases') {
    return PHASE_LABEL_RELEASE[phase] ?? PHASE_LABEL[phase] ?? phase
  }
  return PHASE_LABEL[phase] ?? phase
}

/** 重建方式 → 说明 */
const MODE_LABEL: Record<string, string> = {
  waiting_agent: '宿主代理重建',
  in_place: '本机编译重启',
  docker: 'docker compose 重建',
  unavailable: '在线更新已关闭',
}

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

export default function AdminSystemUpdatePage() {
  const notify = useNotify()
  const queryClient = useQueryClient()
  const autoChecked = useRef(false)

  // 先读本地状态（不联网），再按需自动检查一次
  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['admin', 'system', 'update'],
    queryFn: fetchUpdateStatus,
    // 更新进行中时高频轮询，空闲时不必打扰后端
    refetchInterval: (query) => {
      const stage = query.state.data?.update?.stage
      return stage && stage.running ? 2500 : false
    },
  })

  const update: UpdateStatus | undefined = data?.update
  const version = update?.version
  const stage = update?.stage
  const running = Boolean(stage?.running)

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
            ? `发现新版本 ${info.latest_version}，可以更新`
            : info.behind > 0
              ? `发现新版本，落后 ${info.behind} 个提交`
              : '发现新版本，可以更新',
        )
      } else {
        notify.success('已是最新版本')
      }
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '检查更新失败'),
  })

  const apply = useMutation({
    mutationFn: (target: string) => applySystemUpdate(target),
    onSuccess: () => {
      notify.success(
        update?.source === 'releases' ? '更新任务已启动，正在下载镜像包' : '更新任务已启动，正在下载源码包',
      )
      queryClient.invalidateQueries({ queryKey: ['admin', 'system', 'update'] })
      refetch()
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '启动更新失败'),
  })

  const rollback = useMutation({
    mutationFn: (backupId: string) => rollbackSystemUpdate(backupId),
    onSuccess: (res) => {
      notify.success(res.message)
      queryClient.invalidateQueries({ queryKey: ['admin', 'system', 'update'] })
      refetch()
    },
    onError: (e) => notify.error(e instanceof ApiError ? e.message : '回滚失败'),
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

  // 更新结束（成功/失败）时刷新一次，确保拿到最终状态与历史
  const lastPhase = useRef<string>('')
  useEffect(() => {
    const phase = stage?.phase ?? ''
    if (phase && phase !== lastPhase.current && (phase === 'success' || phase === 'failed')) {
      refetch()
      queryClient.invalidateQueries({ queryKey: ['admin', 'logs'] })
    }
    lastPhase.current = phase
  }, [stage?.phase, refetch, queryClient])

  const backups = data?.backups ?? []
  const agent = data?.agent

  const onApply = async () => {
    const info = version
    if (!info) return
    // 先取出预览再判空：否则 info.pending 被收窄成 never，后续访问 .preview 会报错
    const pendingPreview = info.pending?.preview
    if (info.pending) {
      notify.error('已有待生效的更新，请等待宿主代理完成后再试')
      return
    }
    const isRelease = update?.source === 'releases'
    // releases 模式目标是版本 tag（v1.28.0），commits 模式是提交哈希
    const target = isRelease ? info.latest_version || info.latest.hash : info.latest.hash
    if (!target) {
      notify.error('请先点击「检查更新」获取上游最新版本')
      return
    }
    const imageAsset = info.release?.assets?.find((a) => a.name.includes('inkstone-images'))
    // 注意：confirm 弹窗把 message 当单行文本渲染（无 whitespace-pre），必须用「；」分隔
    const parts = isRelease
      ? [
          `将安装 Release ${target} 的镜像包${imageAsset ? `（${formatSize(imageAsset.size)}）` : ''}`,
          '安装过程为 docker load 镜像 + 重建容器，更新期间站点可能短暂中断',
          update?.mode === 'waiting_agent'
            ? '当前为容器部署，镜像包就绪后由宿主更新代理 docker load 并重启'
            : '当前由后端本机执行 docker load 并重建',
        ]
      : [
          info.behind > 0 ? `本次将应用上游 ${info.behind} 个新提交` : '本次将应用上游最新提交',
          pendingPreview ? `预计替换 ${pendingPreview.writes} 个文件（${formatSize(pendingPreview.bytes)}）` : '',
          '系统会先备份被覆盖的文件，失败自动回滚；更新期间站点可能短暂中断',
          update?.mode === 'waiting_agent'
            ? '当前为容器部署，替换源码后需由宿主更新代理完成重建与重启'
            : '',
        ]
    const ok = await notify.confirm({
      title: `确认更新到 ${isRelease ? target : info.latest.short || target.slice(0, 7)}？`,
      message: `${parts.filter(Boolean).join('；')}。`,
      confirmText: '立即更新',
      danger: true,
    })
    if (ok) apply.mutate(target)
  }

  const onRollback = async (backupId: string) => {
    const isRelease = update?.source === 'releases'
    const ok = await notify.confirm({
      title: `回滚到${isRelease ? '版本' : '备份'} ${backupId}？`,
      message: isRelease
        ? '将重新加载该版本保留在本地的镜像包并重启服务（需宿主代理或本机 docker 可用）。'
        : '将把源码恢复到该备份的状态，之后需要重新构建并重启才会生效。',
      confirmText: '确认回滚',
      danger: true,
    })
    if (ok) rollback.mutate(backupId)
  }

  return (
    <PageTransition>
      {/* 标题栏 */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">系统更新</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {update
              ? `上游 ${update.repo_name || '仓库'} · ${
                  update.source === 'releases'
                    ? '更新源：GitHub Releases（镜像包）'
                    : `分支 ${update.branch}`
                } · 重建方式：${MODE_LABEL[update.mode] ?? update.mode}`
              : '检查上游新版本、下载源码镜像包并一键升级'}
          </p>
        </div>
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
            disabled={check.isPending || running}
            {...hoverTapScale}
            className="flex items-center gap-1.5 rounded-lg border border-border bg-card px-3.5 py-2 text-sm font-medium transition-colors hover:border-accent/40 hover:text-accent disabled:opacity-50"
          >
            {check.isPending ? <Spinner /> : <ArrowDownToLine className="h-4 w-4" />}
            检查更新
          </button>
        </div>
      </div>

      {isLoading ? (
        <div className="mt-6">
          <RowLoading rows={4} />
        </div>
      ) : !update ? (
        <div className="mt-6 rounded-xl border border-dashed p-16 text-center text-muted-foreground">
          无法读取更新状态，请刷新重试
        </div>
      ) : (
        <>
          {/* 更新不可用时的说明（关闭开关 / commits 模式下未探测到源码目录）。
              releases 模式装的是 Release 镜像包，不需要源码目录，不在此告警。 */}
          {!update.enabled || (update.source !== 'releases' && !update.source_dir) ? (
            <Reveal y={12} className="mt-6 rounded-2xl border border-amber-500/40 bg-amber-500/5 p-5">
              <div className="flex items-start gap-3">
                <ShieldAlert className="mt-0.5 h-5 w-5 shrink-0 text-amber-600 dark:text-amber-400" />
                <div className="min-w-0">
                  <p className="text-sm font-semibold">
                    {update.enabled ? '未能确定源码目录' : '在线更新已关闭'}
                  </p>
                  <p className="mt-1 text-sm leading-relaxed text-muted-foreground">{update.message}</p>
                  {!update.enabled ? (
                    <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
                      这是配置项而非故障：把后端环境变量 <code className="rounded bg-muted px-1.5 py-0.5">UPDATE_ENABLED</code>{' '}
                      设为 <code className="rounded bg-muted px-1.5 py-0.5">true</code> 并重启后端即可启用。
                      为避免误覆盖正在开发的源码，本地演示建议把{' '}
                      <code className="rounded bg-muted px-1.5 py-0.5">UPDATE_SOURCE_DIR</code> 指向仓库副本。
                    </p>
                  ) : null}
                </div>
              </div>
            </Reveal>
          ) : null}

          {/* 版本对比 */}
          <Reveal y={14} duration={0.45} className="mt-6 rounded-2xl border border-border bg-card p-5">
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
              <SyncButton
                version={version}
                running={running}
                applying={apply.isPending}
                mode={update.mode}
                source={update.source}
                onApply={onApply}
              />
            </div>

            <div className="mt-4 grid gap-4 sm:grid-cols-2">
              <CommitCard
                title="当前运行版本"
                hash={update.source === 'releases' ? version?.current_version : version?.current.hash}
                short={
                  update.source === 'releases'
                    ? version?.current_version || '——'
                    : version?.current.short
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

          {/* 待生效更新（源码已替换 / 镜像包已下载，等安装） */}
          {version?.pending ? (
            <Reveal y={12} className="mt-4 rounded-2xl border border-accent/40 bg-accent/5 p-5">
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
                        agent.success ? 'text-emerald-600 dark:text-emerald-400' : 'text-amber-600 dark:text-amber-400'
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

          {/* 执行进度 */}
          {stage && (stage.running || stage.phase !== 'idle') ? (
            <Reveal y={12} className="mt-4 rounded-2xl border border-border bg-card p-5">
              <StagePanel stage={stage} source={update.source} />
            </Reveal>
          ) : null}

          {/* 更新日志 / Release 发布说明 */}
          <Reveal y={12} className="mt-4 rounded-2xl border border-border bg-card p-5">
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
                  仅展示最新 {version.changelog.length} 条，另有{' '}
                  {version.changelog_offset} 条更早的提交
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

          {/* 回滚 */}
          <Reveal y={12} className="mt-4 rounded-2xl border border-border bg-card p-5">
            <h2 className="flex items-center gap-2 text-sm font-semibold">
              <RotateCcw className="h-4 w-4 text-accent" />
              回滚
            </h2>
            <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
              {update.source === 'releases'
                ? '镜像包更新会保留最近安装过的镜像包在更新目录；回滚将重新加载所选版本的镜像并重启服务（需宿主代理或本机 docker 可用）。'
                : '每次更新前都会把将被覆盖的文件备份到更新目录。回滚只还原源码，仍需重新构建并重启才生效。'}
            </p>
            {backups.length === 0 ? (
              <p className="mt-3 rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
                {update.source === 'releases' ? '暂无可回滚的历史版本' : '暂无可回滚的备份'}
              </p>
            ) : (
              <ul className="mt-3 space-y-2">
                {backups.slice(0, 5).map((backup) => (
                  <li
                    key={backup.id}
                    className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-background px-3 py-2.5"
                  >
                    <div className="min-w-0">
                      <p className="truncate font-mono text-xs">{backup.id}</p>
                      <p className="text-xs text-muted-foreground">
                        {formatTime(backup.created_at)}
                        {update.source === 'releases' && backup.path
                          ? ` · ${backup.path}`
                          : ''}
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={() => onRollback(backup.id)}
                      disabled={rollback.isPending || running}
                      {...hoverTapScale}
                      className="flex shrink-0 items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-medium transition-colors hover:border-red-500/50 hover:text-red-500 disabled:opacity-50"
                    >
                      <RotateCcw className="h-3.5 w-3.5" />
                      回滚到此{update.source === 'releases' ? '版本' : '备份'}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </Reveal>

          {/* 更新历史 */}
          <Reveal y={12} className="mt-4 rounded-2xl border border-border bg-card p-5">
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
                      {item.result === 'success'
                        ? '成功'
                        : item.result === 'rolled_back'
                          ? '已回滚'
                          : '失败'}
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
        </>
      )}
    </PageTransition>
  )
}

/** 一键更新按钮：按状态给出不同文案与可用性 */
function SyncButton({
  version,
  running,
  applying,
  mode,
  source,
  onApply,
}: {
  version?: {
    update_available: boolean
    latest: { hash: string }
    pending: unknown
    behind: number
  }
  running: boolean
  applying: boolean
  mode: string
  source?: string
  onApply: () => void
}) {
  const disabled = running || applying || !version?.latest?.hash
  const label = applying
    ? '正在启动…'
    : running
      ? '更新进行中'
      : version?.update_available
        ? '立即更新到最新'
        : '重新检查后可更新'

  return (
    <div className="flex flex-col items-end gap-1">
      <button
        type="button"
        onClick={onApply}
        disabled={disabled}
        {...hoverTapScale}
        className="flex items-center gap-1.5 rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white shadow-md shadow-accent/25 transition-colors hover:bg-accent/90 disabled:opacity-50"
      >
        {applying || running ? <Spinner /> : <ArrowDownToLine className="h-4 w-4" />}
        {label}
      </button>
      <span className="text-xs text-muted-foreground">
        {source === 'releases'
          ? mode === 'waiting_agent'
            ? '镜像包就绪后由宿主代理 docker load 并重启'
            : '后端本机 docker load 并重建容器'
          : mode === 'waiting_agent'
            ? '替换源码后由宿主代理重建'
            : '将自动重建并重启服务'}
      </span>
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
        <code className="rounded bg-muted px-2 py-1 text-sm font-medium">
          {short || '——'}
        </code>
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

/** 进度面板：阶段 + 进度条 + 结果 */
function StagePanel({ stage, source }: { stage: UpdateStage; source?: string }) {
  const failed = stage.phase === 'failed'
  const done = stage.phase === 'success'
  const rolled = stage.phase === 'rolled_back'
  const tone = failed ? 'text-red-500' : done ? 'text-emerald-600 dark:text-emerald-400' : 'text-accent'

  return (
    <div>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          {failed ? (
            <XCircle className={`h-4 w-4 ${tone}`} />
          ) : done ? (
            <CheckCircle2 className={`h-4 w-4 ${tone}`} />
          ) : (
            <Spinner />
          )}
          <h2 className="text-sm font-semibold">
            {rolled ? '已回滚' : phaseLabel(stage.phase, source)}
          </h2>
          {stage.target ? (
            <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
              目标 {(stage.target || '').replace('rollback:', '回滚 ')}
            </span>
          ) : null}
        </div>
        <span className="text-xs text-muted-foreground">
          {formatTime(stage.finished_at || stage.started_at)}
          {stage.rebuild_ms > 0 ? ` · 重建耗时 ${(stage.rebuild_ms / 1000).toFixed(1)}s` : ''}
        </span>
      </div>

      {!rolled ? (
        <div className="mt-3 h-2 overflow-hidden rounded-full bg-muted">
          <div
            className={`h-full rounded-full transition-all duration-500 ${
              failed ? 'bg-red-500' : done ? 'bg-emerald-500' : 'bg-accent'
            }`}
            style={{ width: `${Math.min(100, Math.max(stage.running ? 4 : 0, stage.progress))}%` }}
          />
        </div>
      ) : null}

      <p className="mt-2 break-words text-sm text-muted-foreground">
        {stage.message || '—'}
      </p>
      {stage.error ? (
        <p className="mt-2 break-words rounded-lg border border-red-500/40 bg-red-500/5 px-3 py-2 text-xs text-red-500">
          {stage.error}
        </p>
      ) : null}
      {stage.files_changed > 0 ? (
        <p className="mt-1 text-xs text-muted-foreground">
          变更文件 {stage.files_changed} 个
          {stage.backup_id ? ` · 备份 ${stage.backup_id}` : ''}
          {stage.waiting_agent ? ' · 等待宿主代理重建' : ''}
        </p>
      ) : null}
    </div>
  )
}
