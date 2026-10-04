# =============================================================================
# InkStone 本地开发/演示一键启动
#
# 本机 PowerShell 执行策略较严，直接运行 .ps1 会被拦，请用：
#   powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\dev-start.ps1 [-UpdateDemo]
#
#   -UpdateDemo    演示模式：更新源指向仓库副本 .uptest（点「立即更新」只动副本）
#   -SkipFrontend  只起数据库 + 后端
#   -SkipBuild     跳过 go build（用已有的 server.exe）
#   -KeepData      演示模式下保留 .uptest\data（默认每次重建干净副本）
#
# 演示模式为什么用副本：
#   在线更新会**真实替换源码文件**。把 UPDATE_SOURCE_DIR 指向 D:\<repo>\.uptest，
#   就不会冲掉你正在开发的代码。
#
# 注意：本文件必须保存为 **UTF-8 with BOM**，否则 Windows PowerShell 5.1 会按
# ANSI 解析中文，直接报 "The string is missing the terminator"。
# =============================================================================
[CmdletBinding()]
param(
    [switch]$UpdateDemo,
    [switch]$SkipFrontend,
    [switch]$SkipBuild,
    [switch]$KeepData,          # 演示模式下保留 .uptest\data（默认重建干净副本）
    [string]$GoProxy = 'https://goproxy.cn,direct'
)

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path "$PSScriptRoot\..").Path
$backend = Join-Path $repo 'backend'
$frontend = Join-Path $repo 'frontend'
$demoSrc = Join-Path $repo '.uptest'

function Info($m) { Write-Host "[dev] $m" -ForegroundColor Cyan }
function Warn($m) { Write-Host "[dev] $m" -ForegroundColor Yellow }

# docker / docker compose 把进度写到 stderr，而 PowerShell 5.1 会把它包成 ErrorRecord；
# 在本机 ErrorActionPreference=Stop 下会直接中断脚本。这里做局部降级 + 静默 stderr，
# 只取退出码与 stdout。
function Invoke-Docker {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Args)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $out = & docker @Args 2>$null
        return [pscustomobject]@{ ExitCode = $LASTEXITCODE; Output = (($out | Out-String).Trim()) }
    }
    finally { $ErrorActionPreference = $prev }
}

# ---------- 1) 数据库 ----------
Info '检查 Docker / PostgreSQL …'
$dockerOk = $false
$dockerOk = (Invoke-Docker version --format '{{.Server.Version}}').ExitCode -eq 0
if (-not $dockerOk) {
    $exe = @('D:\tools\docker\Docker Desktop.exe', "$env:ProgramFiles\Docker\Docker\Docker Desktop.exe") |
        Where-Object { Test-Path $_ } | Select-Object -First 1
    if ($exe) {
        Info "启动 Docker Desktop：$exe"
        Start-Process $exe
        for ($i = 0; $i -lt 60; $i++) {
            Start-Sleep -Seconds 3
            if ((Invoke-Docker version --format '{{.Server.Version}}').ExitCode -eq 0) { $dockerOk = $true; break }
        }
    }
}
if (-not $dockerOk) { Warn 'Docker 不可用：请手动启动 Docker Desktop 后重试（后端依赖 PostgreSQL）' }
else {
    Push-Location $repo
    try {
        Invoke-Docker compose -f docker-compose.dev.yml up -d postgres | Out-Null
        for ($i = 0; $i -lt 30; $i++) {
            $health = (Invoke-Docker inspect -f '{{.State.Health.Status}}' blog-postgres).Output.Trim()
            if ($health -eq 'healthy') { break }
            Start-Sleep -Seconds 3
        }
        $health = (Invoke-Docker inspect -f '{{.State.Health.Status}}' blog-postgres).Output.Trim()
        if ($health -eq 'healthy') { Info 'PostgreSQL: healthy' } else { Warn "PostgreSQL 健康状态：$health" }
    }
    finally { Pop-Location }
}

# ---------- 2) 演示副本 ----------
if ($UpdateDemo) {
    if ((Test-Path $demoSrc) -and -not $KeepData) {
        Info '重建演示副本 .uptest …'
        Remove-Item $demoSrc -Recurse -Force -ErrorAction SilentlyContinue
    }
    if (-not (Test-Path (Join-Path $demoSrc 'backend\cmd\server\main.go'))) {
        Info '复制仓库到 .uptest（排除 node_modules/.next/.git/data）…'
        robocopy $repo $demoSrc /E /XD node_modules .next .git data .tools .uptest logs /NFL /NDL /NJH /NJS /NP | Out-Null
    }
    New-Item -ItemType Directory -Path (Join-Path $demoSrc 'data\update') -Force | Out-Null
    Info "演示副本就绪：$demoSrc"
}

# ---------- 3) 后端 ----------
Get-Process -Name server -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1

if (-not $SkipBuild) {
    Info '编译后端 …'
    $env:GOPROXY = $GoProxy; $env:GOSUMDB = 'off'
    Push-Location $backend
    try {
        & go build -o server.exe ./cmd/server
        if ($LASTEXITCODE -ne 0) { throw 'go build 失败' }
    }
    finally { Pop-Location }
}

$env:GIN_MODE = 'debug'
$env:UPDATE_ENABLED = if ($UpdateDemo) { 'true' } else { 'false' }
if ($UpdateDemo) {
    $env:UPDATE_SOURCE_DIR = $demoSrc
    $env:UPDATE_DIR = Join-Path $demoSrc 'data\update'
    $env:UPDATE_WAITING_AGENT = 'true'   # 只替换源码，重启交给宿主代理（本地演示不让它自动重启）
    Info "更新演示：源=$demoSrc（点「立即更新」只动副本）"
} else {
    Remove-Item Env:UPDATE_SOURCE_DIR -ErrorAction SilentlyContinue
    Remove-Item Env:UPDATE_DIR -ErrorAction SilentlyContinue
    Info '更新功能已关闭（加 -UpdateDemo 可开启演示）'
}

Info '启动后端 :8080 …'
Start-Process -FilePath (Join-Path $backend 'server.exe') -WorkingDirectory $backend `
    -RedirectStandardOutput (Join-Path $backend 'server-dev.log') `
    -RedirectStandardError (Join-Path $backend 'server-err.log') -WindowStyle Hidden
for ($i = 0; $i -lt 20; $i++) {
    Start-Sleep -Seconds 1
    try { $h = (Invoke-WebRequest 'http://127.0.0.1:8080/healthz' -UseBasicParsing -TimeoutSec 2).Content; if ($h) { break } } catch {}
}
try {
    $info = (Invoke-WebRequest 'http://127.0.0.1:8080/api/v1/system/info' -UseBasicParsing -TimeoutSec 5).Content | ConvertFrom-Json
    Info ("后端就绪：v{0}  commit={1}（{2}）" -f $info.info.version, $info.info.commit, $info.info.commit_source)
} catch { Warn '后端未在 20 秒内就绪，看 backend\server-err.log' }

# ---------- 4) 前端 ----------
if ($SkipFrontend) { Info '跳过前端'; exit 0 }

$up = Get-NetTCPConnection -LocalPort 3000 -State Listen -ErrorAction SilentlyContinue
if ($up) {
    Info '前端已在 :3000 运行，跳过启动'
} else {
    Info '启动前端 :3000 …'
    Start-Process -FilePath 'cmd.exe' -ArgumentList '/c', 'npm', 'run', 'dev', '>', '..\frontend-dev.log', '2>&1' `
        -WorkingDirectory $frontend -WindowStyle Hidden
    for ($i = 0; $i -lt 40; $i++) {
        Start-Sleep -Seconds 2
        if (Get-NetTCPConnection -LocalPort 3000 -State Listen -ErrorAction SilentlyContinue) { break }
    }
}

Write-Host ''
Info '========================================'
Info ' 访客站   http://127.0.0.1:3000'
Info ' 管理后台 http://127.0.0.1:3000/admin'
Info ' 系统更新 http://127.0.0.1:3000/admin/system-update'
Info ' 接口自检 http://127.0.0.1:8080/api/v1/system/info'
Info ' 日志     backend\server-dev.log / frontend-dev.log'
if ($UpdateDemo) { Info ' 演示副本 D:\...\.uptest\data\update（清单/备份/状态都在这）' }
Info '========================================'
