# =============================================================================
# InkStone 镜像包打包（Windows / PowerShell）
#
# 产物：dist\inkstone-images-<版本>.tar —— GitHub Releases 的镜像包资产。
# 后台「系统更新」（UPDATE_SOURCE=releases）与一键离线部署都消费这个文件：
#   docker load -i inkstone-images-<版本>.tar
#   docker compose --env-file .env -f docker-compose.offline.yml up -d
#
# 用法：
#   .\deploy\package-images.ps1                          # 默认版本 Beta1.27
#   .\deploy\package-images.ps1 -Version v1.28.0
#   $env:INKSTONE_PUBLIC_API_URL='https://blog.shenv.top/api/v1'; .\deploy\package-images.ps1
# =============================================================================
[CmdletBinding()]
param(
    [string]$Version = $(if ($env:INKSTONE_VERSION) { $env:INKSTONE_VERSION } else { 'Beta1.27' }),
    # 前端 API 地址：NEXT_PUBLIC_* 是构建期注入，打进镜像后改不了，按部署域名传
    [string]$ApiUrl = $(if ($env:INKSTONE_PUBLIC_API_URL) { $env:INKSTONE_PUBLIC_API_URL } else { 'https://blog.shenv.top/api/v1' }),
    # 站点自身地址：NEXT_PUBLIC_SITE_URL 也是构建期注入，用于 canonical / OG / JSON-LD。
    # 留空的后果是这些地址兜底成 http://localhost:3000，生产环境的 canonical
    # 会指向 localhost，被搜索引擎当成重复内容而拒绝收录。
    [string]$SiteUrl = $(if ($env:INKSTONE_PUBLIC_SITE_URL) { $env:INKSTONE_PUBLIC_SITE_URL } else { 'https://blog.shenv.top' }),
    [string]$OutDir = $(if ($env:INKSTONE_DIST_DIR) { $env:INKSTONE_DIST_DIR } else { 'dist' }),
    [switch]$NoLatest
)

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path "$PSScriptRoot\..").Path
$backendDir = Join-Path $repo 'backend'
$frontendDir = Join-Path $repo 'frontend'

function Need-Docker {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        throw '未找到 docker 命令（需要 Docker Desktop / Docker Engine）'
    }
    docker version --format '{{.Server.Version}}' 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'docker 引擎未运行，请先启动 Docker Desktop' }
}

function Invoke-Step {
    param([string]$Message, [scriptblock]$Block)
    Write-Host "==> $Message"
    & $Block
    if ($LASTEXITCODE -ne 0) { throw "$Message 失败（退出码 $LASTEXITCODE）" }
}

Need-Docker

$backendTags = @("inkstone-backend:$Version")
$frontendTags = @("inkstone-frontend:$Version")
if (-not $NoLatest) {
    # docker-compose.offline.yml 引用 :latest，回滚依赖各版本 tag 仍在镜像列表里
    $backendTags += 'inkstone-backend:latest'
    $frontendTags += 'inkstone-frontend:latest'
}

# 1) 后端（多阶段构建：golang 编译 → alpine 运行）
$backendArgs = @('build', '-t', $backendTags[0], '-f', (Join-Path $backendDir 'Dockerfile'), $backendDir)
foreach ($t in $backendTags[1..$backendTags.Count]) { $backendArgs += @('-t', $t) }
Invoke-Step "构建后端镜像 $($backendTags -join ' ')" { & docker @backendArgs }

# 2) 前端（NEXT_PUBLIC_API_URL / NEXT_PUBLIC_SITE_URL 构建期注入）
$frontendArgs = @('build', '-t', $frontendTags[0], '-f', (Join-Path $frontendDir 'Dockerfile'),
    '--build-arg', "NEXT_PUBLIC_API_URL=$ApiUrl",
    '--build-arg', "NEXT_PUBLIC_SITE_URL=$SiteUrl", $frontendDir)
foreach ($t in $frontendTags[1..$frontendTags.Count]) { $frontendArgs += @('-t', $t) }
Invoke-Step "构建前端镜像 $($frontendTags -join ' ')" { & docker @frontendArgs }

# 3) 导出镜像包
if (-not (Test-Path $OutDir)) { New-Item -ItemType Directory -Path $OutDir -Force | Out-Null }
$tarName = "inkstone-images-$Version.tar"
$tarPath = Join-Path (Resolve-Path $OutDir) $tarName
if (Test-Path $tarPath) { Remove-Item $tarPath -Force }

$saveArgs = @('save', '-o', $tarPath)
foreach ($t in ($backendTags + $frontendTags)) { $saveArgs += $t }
Invoke-Step "导出镜像包 $tarName" { & docker @saveArgs }

# 4) 校验值（发布时附在 Release 说明或资产名旁）
$hash = (Get-FileHash -Path $tarPath -Algorithm SHA256).Hash.ToLowerInvariant()
$size = (Get-Item $tarPath).Length
"$hash  $tarName" | Set-Content -Path "$tarPath.sha256" -Encoding ascii

Write-Host ''
Write-Host "完成：$tarPath"
Write-Host "大小：$([Math]::Round($size/1MB,1)) MB"
Write-Host "SHA256：$hash"
Write-Host ''
Write-Host '验证（另开一台机器）：'
Write-Host "  docker load -i `"$tarPath`""
Write-Host '  docker compose --env-file .env -f docker-compose.offline.yml up -d'
