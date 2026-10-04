# =============================================================================
# InkStone 镜像包部署到服务器（方式一：scp + docker load + compose up -d）
#
# 用法（在仓库根目录执行）：
#   .\deploy\publish-server.ps1 -Server <服务器IP或域名> -User root
#
# 可选参数：
#   -Port 22                 SSH 端口
#   -RemoteDir /opt/inkstone 服务器部署目录（compose 文件与 .env 所在处）
#   -TarPath dist\inkstone-images-Beta1.27.tar   本地镜像包路径
#   -ComposeFile docker-compose.offline.yml      服务器上的编排文件
#   -SiteUrl https://blog.shenv.top              部署后核对的站点地址
#   -SkipBackup            跳过数据库备份（不建议）
#   -UseSudo               远程 docker 需要 sudo（非 root 用户时）
#
# 说明：ssh/scp 的密码提示只会在你自己的终端出现，本脚本不保存任何凭据。
# =============================================================================
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Server,
    [string]$User = 'root',
    [int]$Port = 22,
    [string]$RemoteDir = '/opt/inkstone',
    [string]$TarPath = 'dist\inkstone-images-Beta1.27.tar',
    [string]$ComposeFile = 'docker-compose.offline.yml',
    [string]$SiteUrl = 'https://blog.shenv.top',
    [switch]$SkipBackup,
    [switch]$UseSudo
)

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path "$PSScriptRoot\..").Path
$tarFull = if ([IO.Path]::IsPathRooted($TarPath)) { $TarPath } else { Join-Path $repo $TarPath }
if (-not (Test-Path $tarFull)) { throw "找不到镜像包：$tarFull（先执行 deploy\package-images.ps1）" }

$tarName = Split-Path $tarFull -Leaf
# 版本号从文件名推导：inkstone-images-<版本>.tar
$version = if ($tarName -match '^inkstone-images-(.+)\.tar$') { $Matches[1] } else { 'unknown' }
$sudo = if ($UseSudo) { 'sudo ' } else { '' }
$sshTarget = "$User@$Server"

function Invoke-Remote {
    param([string]$Title, [string]$Script)
    Write-Host ''
    Write-Host "== $Title ==" -ForegroundColor Cyan
    # 直接调用 ssh：密码提示由终端（/dev/tty）处理，输出实时可见
    & ssh -p $Port "$User@$Server" $Script
    if ($LASTEXITCODE -ne 0) {
        throw "$Title 失败（ssh 退出码 $LASTEXITCODE）。远程错误见上方输出。"
    }
}

# ---- 远程脚本：预检 + 备份（一次连接完成）----
$preflight = @'
set -e
echo "-- 环境 --"
__SUDO__docker --version
cd "__REMOTE_DIR__"
test -f "__COMPOSE_FILE__" && echo "编排文件：OK" || { echo "缺少 __COMPOSE_FILE__"; exit 1; }
test -f .env && echo ".env：OK" || { echo "缺少 .env（DB_PASSWORD/JWT_SECRET 等）"; exit 1; }
DB_USER=$(grep -E '^DB_USER=' .env | cut -d= -f2-); DB_USER=${DB_USER:-blog}
DB_NAME=$(grep -E '^DB_NAME=' .env | cut -d= -f2-); DB_NAME=${DB_NAME:-blog_platform}
echo "-- 当前容器 --"
__SUDO__docker ps --filter name=blog- --format '{{.Names}} | {{.Image}} | {{.Status}}'
echo "-- 升级前镜像快照（回滚用）--"
__SUDO__docker images --no-trunc --format '{{.Repository}}:{{.Tag}} {{.ID}}' | grep inkstone | tee /opt/backup_blog_pre-__VERSION__-images.txt
'@.Replace('__SUDO__', $sudo).Replace('__REMOTE_DIR__', $RemoteDir).Replace('__COMPOSE_FILE__', $ComposeFile).Replace('__VERSION__', $version)

$backup = @'
set -e
cd "__REMOTE_DIR__"
DB_USER=$(grep -E '^DB_USER=' .env | cut -d= -f2-); DB_USER=${DB_USER:-blog}
DB_NAME=$(grep -E '^DB_NAME=' .env | cut -d= -f2-); DB_NAME=${DB_NAME:-blog_platform}
BACKUP="/opt/backup_blog_pre-__VERSION__.sql"
__SUDO__docker exec blog-postgres pg_dump -U "$DB_USER" "$DB_NAME" > "$BACKUP"
ls -lh "$BACKUP"
echo "$BACKUP"
'@.Replace('__SUDO__', $sudo).Replace('__REMOTE_DIR__', $RemoteDir).Replace('__VERSION__', $version)

# ---- 远程脚本：load + up + 核对 ----
$deployRemote = @'
set -e
echo "-- docker load --"
__SUDO__docker load -i "__REMOTE_DIR__/__TAR__"
cd "__REMOTE_DIR__"
echo "-- compose up -d（__COMPOSE_FILE__）--"
__SUDO__docker compose --env-file .env -f "__COMPOSE_FILE__" up -d
echo "-- 等待 8 秒让容器起来 --"
sleep 8
__SUDO__docker ps --filter name=blog- --format '{{.Names}} | {{.Image}} | {{.Status}}'
echo "-- 健康检查 --"
__SUDO__docker exec blog-backend wget -qO- http://127.0.0.1:8080/healthz || { echo "后端健康检查失败"; exit 1; }
echo
echo "-- 版本核对（__SITE_URL__）--"
curl -fsS "__SITE_URL__/api/v1/system/info" || echo "（外网核对失败，可手动打开 __SITE_URL__ 确认）"
echo
echo "DONE"
'@.Replace('__SUDO__', $sudo).Replace('__REMOTE_DIR__', $RemoteDir).Replace('__TAR__', $tarName).Replace('__COMPOSE_FILE__', $ComposeFile).Replace('__SITE_URL__', $SiteUrl)

# ---- 执行 ----
Write-Host "目标：$sshTarget (端口 $Port)  目录：$RemoteDir" -ForegroundColor Green
Write-Host "镜像包：$tarFull（$([Math]::Round((Get-Item $tarFull).Length/1MB,1)) MB，版本 $version）" -ForegroundColor Green

Write-Host ''
Write-Host '== 传输镜像包 ==' -ForegroundColor Cyan
& scp -P $Port $tarFull "${sshTarget}:$RemoteDir/$tarName"
if ($LASTEXITCODE -ne 0) { throw "scp 失败（退出码 $LASTEXITCODE）" }

if ($SkipBackup) {
    Invoke-Remote -Title '远程预检' -Script $preflight
}
else {
    Invoke-Remote -Title '远程预检 + 数据库备份' -Script ($preflight + "`n" + $backup)
}

Invoke-Remote -Title '安装镜像包并重建容器' -Script $deployRemote

Write-Host ''
Write-Host "部署完成：$SiteUrl" -ForegroundColor Green
Write-Host "版本：$version"
Write-Host "回滚指引：更新前镜像快照已存于服务器 /opt/backup_blog_pre-$version-images.txt；"
Write-Host "  需要回滚时：docker tag <旧镜像ID> inkstone-backend:latest（frontend 同理）后重跑 compose up -d。"
