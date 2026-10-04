# InkStone 数据库完整备份（Windows 版，pg_dump custom 格式）
#
# 与后台「备份与恢复」页的关系见 deploy/backup.sh 顶部说明：
# 后台导出 gzip JSON 快照（内容留档），本脚本产出可完整恢复的 pg_dump 备份。
#
# 用法：
#   pwsh -File deploy\backup.ps1 [-BackupDir <目录>]
# 参数/环境变量（都有默认值）：
#   BackupDir / BACKUP_DIR     备份输出目录（默认 <仓库>\data\backups\pg）
#   Keep / BACKUP_KEEP         保留份数（默认 14）
#   Container / BACKUP_CONTAINER  postgres 容器名（默认自动探测）
#   PgUser / POSTGRES_USER     数据库用户（默认 blog）
#   PgDb / POSTGRES_DB         数据库名（默认 blog_platform）
[CmdletBinding()]
param(
    [string]$BackupDir = $(if ($env:BACKUP_DIR) { $env:BACKUP_DIR } else { "" }),
    [int]$Keep = $(if ($env:BACKUP_KEEP) { [int]$env:BACKUP_KEEP } else { 14 }),
    [string]$Container = $(if ($env:BACKUP_CONTAINER) { $env:BACKUP_CONTAINER } else { "" }),
    [string]$PgUser = $(if ($env:POSTGRES_USER) { $env:POSTGRES_USER } else { "blog" }),
    [string]$PgDb = $(if ($env:POSTGRES_DB) { $env:POSTGRES_DB } else { "blog_platform" })
)

$ErrorActionPreference = "Stop"

$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
if ([string]::IsNullOrWhiteSpace($BackupDir)) {
    $BackupDir = Join-Path $RepoRoot "data\backups\pg"
}
if (-not (Test-Path $BackupDir)) {
    New-Item -ItemType Directory -Path $BackupDir -Force | Out-Null
}

$Stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$Out = Join-Path $BackupDir "inkstone-pg-$Stamp.dump"

if ([string]::IsNullOrWhiteSpace($Container)) {
    # 自动探测：取第一个名字里带 postgres 的容器。
    $found = docker ps --format "{{.Names}}" 2>$null | Where-Object { $_ -match "postgres" } | Select-Object -First 1
    if ($found) { $Container = $found }
}

if ($Container) {
    Write-Host "[backup] 使用容器 $Container"
    # docker exec 的标准输出直接重定向到文件。custom 格式是压缩二进制，
    # 不需要文本编码转换，用 .NET 流写入避免 PowerShell 管道改行尾。
    $proc = Start-Process -FilePath "docker" `
        -ArgumentList "exec", $Container, "pg_dump", "-U", $PgUser, "-d", $PgDb, "-Fc" `
        -NoNewWindow -Wait -PassThru -RedirectStandardOutput $Out
    if ($proc.ExitCode -ne 0) {
        Write-Host "[backup] 错误：docker exec pg_dump 失败（退出码 $($proc.ExitCode)）"
        exit 1
    }
} else {
    Write-Host "[backup] 未发现 postgres 容器，改用本机 pg_dump"
    if (-not (Get-Command pg_dump -ErrorAction SilentlyContinue)) {
        Write-Host "[backup] 错误：本机没有 pg_dump，且未找到 postgres 容器"
        exit 1
    }
    & pg_dump -U $PgUser -d $PgDb -Fc | Set-Content -Path $Out -Encoding Byte
}

if (-not (Test-Path $Out)) {
    Write-Host "[backup] 错误：备份文件未生成 $Out"
    exit 1
}
$size = (Get-Item $Out).Length
$mb = [math]::Round($size / 1MB, 2)
Write-Host "[backup] 已生成 $Out（$mb MB）"

if ($Keep -gt 0) {
    $all = Get-ChildItem -Path $BackupDir -Filter "inkstone-pg-*.dump" | Sort-Object Name -Descending
    if ($all.Count -gt $Keep) {
        Write-Host "[backup] 清理旧备份（保留 $Keep 份，当前 $($all.Count) 份）"
        $all | Select-Object -Skip $Keep | ForEach-Object {
            Remove-Item $_.FullName -Force
            Write-Host "[backup] 已删除 $($_.Name)"
        }
    }
}

Write-Host "[backup] 校验备份可读性..."
if ($Container) {
    $check = Start-Process -FilePath "docker" `
        -ArgumentList "exec", "-i", $Container, "pg_restore", "-l" `
        -NoNewWindow -Wait -PassThru -RedirectStandardInput $Out -RedirectStandardOutput "NUL"
    if ($check.ExitCode -ne 0) {
        Write-Host "[backup] 错误：备份文件校验失败（pg_restore -l 非零退出）"
        exit 1
    }
} else {
    & pg_restore -l $Out > $null
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[backup] 错误：备份文件校验失败（pg_restore -l 非零退出）"
        exit 1
    }
}
Write-Host "[backup] 校验通过"
