# =============================================================================
# InkStone 本机重建脚本（Windows 二进制部署）
#
# 由后端「系统更新」在替换源码后自动拉起（此时后端仍是旧进程）：
#   1. 等旧后端进程退出（Windows 上二进制被占用时无法覆盖）
#   2. 重新编译 Go 后端（必要时构建前端）
#   3. 重新拉起 server.exe
#   4. 把结果写进更新目录的 update-result.json
#
# 环境变量由后端注入：INKSTONE_TARGET / INKSTONE_REPO_ROOT /
#   INKSTONE_UPDATE_DIR / INKSTONE_SELF / INKSTONE_PID / INKSTONE_PORT
# =============================================================================
[CmdletBinding()]
param(
    [string]$RepoRoot = $(if ($env:INKSTONE_REPO_ROOT) { $env:INKSTONE_REPO_ROOT } else { (Resolve-Path "$PSScriptRoot\..\..").Path }),
    [string]$UpdateDir = $(if ($env:INKSTONE_UPDATE_DIR) { $env:INKSTONE_UPDATE_DIR } else { (Join-Path $RepoRoot 'data\update') }),
    [string]$Target = $env:INKSTONE_TARGET,
    [string]$SelfBin = $(if ($env:INKSTONE_SELF) { $env:INKSTONE_SELF } else { (Join-Path $RepoRoot 'backend\server.exe') }),
    [int]$WaitPid = $(if ($env:INKSTONE_PID) { [int]$env:INKSTONE_PID } else { 0 }),
    [int]$WaitLimitSeconds = 60
)

$ErrorActionPreference = 'Continue'

function Write-Log([string]$Message) {
    Write-Host ("[rebuild {0}] {1}" -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $Message)
}

function Write-Result([bool]$Success, [string]$Message, [int]$DurationMs) {
    if (-not (Test-Path $UpdateDir)) { New-Item -ItemType Directory -Path $UpdateDir -Force | Out-Null }
    [ordered]@{
        state       = 'done'
        commit      = $Target
        success     = $Success
        message     = $Message
        agent       = 'local-rebuild.ps1'
        finished_at = (Get-Date).ToString('o')
        duration_ms = $DurationMs
    } | ConvertTo-Json | Set-Content -Path (Join-Path $UpdateDir 'update-result.json') -Encoding utf8
}

# 1) 等旧进程退出
if ($WaitPid -gt 0) {
    $waited = 0
    while ($waited -lt $WaitLimitSeconds) {
        if (-not (Get-Process -Id $WaitPid -ErrorAction SilentlyContinue)) { break }
        Start-Sleep -Seconds 1
        $waited++
    }
    Write-Log "旧进程等待结束（${waited}s）"
}
Start-Sleep -Seconds 1

$started = Get-Date
# 更新目录可能还不存在（手工执行时）：先建出来，否则写 build.log 会失败并被误报成「编译失败」
if (-not (Test-Path $UpdateDir)) { New-Item -ItemType Directory -Path $UpdateDir -Force | Out-Null }
$logFile = Join-Path $UpdateDir 'build.log'
'' | Set-Content -Path $logFile -Encoding utf8

# 2) 编译
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Log '未找到 go 命令，无法编译'
    Write-Result $false '未找到 go 命令，无法编译' 0
    exit 1
}

Write-Log '编译后端'
Push-Location (Join-Path $RepoRoot 'backend')
try {
    # -tags timetzdata：把 IANA 时区库编进二进制。
    # Windows 上 Go 的 time.LoadLocation("Asia/Shanghai") 依赖系统时区注册表，
    # 某些环境（精简版 / 非中文区域格式）会报 "unknown time zone Asia/Shanghai" 直接启动失败，
    # 内嵌时区库后与宿主区域设置无关（体积 +约 450KB）。
    & go build -trimpath -tags timetzdata -o server.exe ./cmd/server *>> $logFile
    $code = $LASTEXITCODE
}
finally { Pop-Location }

if ($code -ne 0) {
    Write-Log "编译失败（$logFile）"
    Get-Content $logFile -Tail 20 | Write-Host
    Write-Result $false "go build 失败，日志：$logFile" ([int]((Get-Date) - $started).TotalMilliseconds)
    exit 1
}

$frontend = Join-Path $RepoRoot 'frontend'
if ((Test-Path (Join-Path $frontend 'package.json')) -and (Get-Command npm -ErrorAction SilentlyContinue)) {
    Write-Log '构建前端'
    Push-Location $frontend
    try {
        & npm ci *>> $logFile
        & npm run build *>> $logFile
        $frontCode = $LASTEXITCODE
    }
    finally { Pop-Location }
    if ($frontCode -ne 0) {
        Write-Log "前端构建失败（$logFile）"
        Get-Content $logFile -Tail 20 | Write-Host
        Write-Result $false "前端构建失败，日志：$logFile" ([int]((Get-Date) - $started).TotalMilliseconds)
        exit 1
    }
}

# 3) 重新拉起后端
if (Test-Path $SelfBin) {
    $backendDir = Split-Path $SelfBin -Parent
    Write-Log "启动后端：$SelfBin"
    Start-Process -FilePath $SelfBin -WorkingDirectory $backendDir `
        -RedirectStandardOutput (Join-Path $UpdateDir 'backend.log') `
        -RedirectStandardError (Join-Path $UpdateDir 'backend.err.log') `
        -WindowStyle Hidden
} else {
    Write-Log "未找到可执行文件 $SelfBin，请手工启动后端"
}

# 4) 记录部署版本（回滚任务不是上游提交，跳过写入）
if ($Target -and -not $Target.StartsWith('rollback:')) {
    [ordered]@{
        commit     = $Target
        updated_at = (Get-Date).ToString('o')
    } | ConvertTo-Json | Set-Content -Path (Join-Path $UpdateDir 'deployed-commit.json') -Encoding utf8
}
else {
    Write-Log "目标不是上游提交（$Target）：跳过部署记录写入"
}

Write-Result $true '本机编译与重启完成' ([int]((Get-Date) - $started).TotalMilliseconds)
Write-Log "完成：$Target"
