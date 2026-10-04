# =============================================================================
# InkStone 宿主更新代理（Windows / PowerShell）
#
# 与 deploy/update-agent.sh 等价：后台「一键更新」替换源码后，
# 由本脚本在宿主机上完成 备份 → 重建 → 重启 → 回报结果。
#
# 用法：
#   单次处理：  .\deploy\update-agent.ps1 -Once
#   常驻轮询：  .\deploy\update-agent.ps1
#   指定源码：  .\deploy\update-agent.ps1 -SourceDir D:\inkstone
# =============================================================================
[CmdletBinding()]
param(
    [string]$SourceDir = $(if ($env:INKSTONE_SOURCE_DIR) { $env:INKSTONE_SOURCE_DIR } else { (Resolve-Path "$PSScriptRoot\..").Path }),
    [string]$UpdateDir = $env:INKSTONE_UPDATE_DIR,
    [string]$ComposeFile = $(if ($env:INKSTONE_COMPOSE_FILE) { $env:INKSTONE_COMPOSE_FILE } else { 'docker-compose.prod.yml' }),
    [string]$BackendContainer = $(if ($env:INKSTONE_BACKEND_CONTAINER) { $env:INKSTONE_BACKEND_CONTAINER } else { 'blog-backend' }),
    [string]$AgentName = $(if ($env:INKSTONE_AGENT_NAME) { $env:INKSTONE_AGENT_NAME } else { 'host-update-agent' }),
    # auto | docker | binary
    [string]$DeployType = $(if ($env:INKSTONE_DEPLOY_TYPE) { $env:INKSTONE_DEPLOY_TYPE } else { 'auto' }),
    [int]$IntervalSeconds = $(if ($env:INKSTONE_POLL_SECONDS) { [int]$env:INKSTONE_POLL_SECONDS } else { 60 }),
    [int]$BuildTimeoutSeconds = 2700,
    [switch]$Once
)

$ErrorActionPreference = 'Continue'

function Write-Log([string]$Message) {
    Write-Host ("[update-agent {0}] {1}" -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $Message)
}

function Resolve-UpdateDir {
    if ($UpdateDir) { return $UpdateDir }
    foreach ($candidate in @(
            (Join-Path $SourceDir 'data\update'),
            (Join-Path $SourceDir 'backend\data\update')
        )) {
        if (Test-Path $candidate) { return $candidate }
    }
    return (Join-Path $SourceDir 'data\update')
}

function Get-DeployType {
    if ($DeployType -ne 'auto') { return $DeployType }
    if (Test-Path (Join-Path $SourceDir $ComposeFile)) { return 'docker' }
    return 'binary'
}

function Write-Result {
    param([string]$State, [bool]$Success, [string]$Message, [int]$DurationMs, [string]$Commit, [string]$Dir)
    if (-not (Test-Path $Dir)) { New-Item -ItemType Directory -Path $Dir -Force | Out-Null }
    $payload = [ordered]@{
        state       = $State
        commit      = $Commit
        success     = $Success
        message     = $Message
        agent       = $AgentName
        finished_at = (Get-Date).ToString('o')
        duration_ms  = $DurationMs
    }
    $payload | ConvertTo-Json | Set-Content -Path (Join-Path $Dir 'update-result.json') -Encoding utf8
}

function Save-Manifest {
    param([string]$Manifest, [string]$Commit)
    try {
        $data = Get-Content $Manifest -Raw -Encoding utf8 | ConvertFrom-Json
        $data.state = 'done'
        $data | ConvertTo-Json -Depth 8 | Set-Content -Path $Manifest -Encoding utf8
    }
    catch {
        "{`"state`":`"done`",`"commit`":`"$Commit`"}" | Set-Content -Path $Manifest -Encoding utf8
    }
}

function Invoke-Build {
    param([string]$Type, [string]$Dir)
    $logFile = Join-Path $Dir 'build.log'
    '' | Set-Content -Path $logFile -Encoding utf8

    if ($Type -eq 'docker') {
        $composeArgs = @('compose', '-f', (Join-Path $SourceDir $ComposeFile))
        $envFile = Join-Path $SourceDir '.env'
        if (Test-Path $envFile) { $composeArgs += @('--env-file', $envFile) }

        Write-Log 'docker compose build'
        & docker @composeArgs build *>> $logFile
        if ($LASTEXITCODE -ne 0) {
            Write-Log "docker compose build 失败（$logFile）"
            Get-Content $logFile -Tail 20 | Write-Host
            return $false
        }
        Write-Log 'docker compose up -d'
        & docker @composeArgs up -d *>> $logFile
        if ($LASTEXITCODE -ne 0) {
            Write-Log "docker compose up 失败（$logFile）"
            Get-Content $logFile -Tail 20 | Write-Host
            return $false
        }
        return $true
    }

    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        Write-Log '未找到 go 命令（二进制部署需要 Go 工具链）'
        return $false
    }
    Write-Log 'go build'
    Push-Location (Join-Path $SourceDir 'backend')
    try {
        # -tags timetzdata：内嵌时区库，避免 Windows 上 "unknown time zone Asia/Shanghai" 启动失败
        & go build -trimpath -tags timetzdata -o server.exe ./cmd/server *>> $logFile
        if ($LASTEXITCODE -ne 0) {
            Write-Log "go build 失败（$logFile）"
            Get-Content $logFile -Tail 20 | Write-Host
            return $false
        }
    }
    finally { Pop-Location }

    $frontend = Join-Path $SourceDir 'frontend'
    if ((Test-Path (Join-Path $frontend 'package.json')) -and (Get-Command npm -ErrorAction SilentlyContinue)) {
        Write-Log 'npm run build（前端）'
        Push-Location $frontend
        try {
            & npm ci *>> $logFile
            & npm run build *>> $logFile
            if ($LASTEXITCODE -ne 0) {
                Write-Log "前端构建失败（$logFile）"
                Get-Content $logFile -Tail 20 | Write-Host
                return $false
            }
        }
        finally { Pop-Location }
    }
    return $true
}

function Invoke-Restart {
    param([string]$Type)
    if ($Type -eq 'docker') {
        for ($i = 0; $i -lt 30; $i++) {
            $health = (& docker inspect -f '{{.State.Health.Status}}' $BackendContainer 2>$null)
            if ($health -match 'healthy') {
                Write-Log '后端容器健康检查通过'
                return $true
            }
            Start-Sleep -Seconds 2
        }
        Write-Log '警告：后端容器未在 60 秒内变为 healthy'
        return $true
    }

    # 二进制部署：本次由后端自己拉起的重建脚本负责重启，这里只做提示
    Write-Log '二进制部署：请确认后端进程已重新启动（可用 NSSM / 计划任务托管）'
    return $true
}

function Restore-Backup {
    param([string]$BackupRoot, [string]$Manifest)
    if (-not (Test-Path $BackupRoot)) { return $false }

    # 还原规则与后端 rollback 一致：
    #   write & existed=true  → 从备份还原
    #   write & existed=false → 本次新增，删除它（避免留下新旧混合的代码树）
    #   delete                → 备份里有就还原
    $changes = @()
    if ($Manifest -and (Test-Path $Manifest)) {
        try {
            $parsed = Get-Content $Manifest -Raw -Encoding utf8 | ConvertFrom-Json
            # JSON null 时 @($null).Count 是 1，会让下面的兜底分支误判
            if ($parsed -and $parsed.changes) { $changes = @($parsed.changes) }
        }
        catch { $changes = @() }
    }

    $restored = 0
    $removed = 0
    foreach ($change in $changes) {
        if (-not $change) { continue }
        $rel = ([string]$change.path).Replace('\', '/').Trim('/')
        if (-not $rel -or $rel -eq '..' -or $rel.StartsWith('../') -or $rel.Contains('/../')) { continue }
        $target = Join-Path $SourceDir $rel
        $backup = Join-Path $BackupRoot $rel

        if ($change.action -eq 'write' -and -not $change.existed) {
            if (Test-Path $target -PathType Leaf) { Remove-Item $target -Force; $removed++ }
            continue
        }
        if (Test-Path $backup -PathType Leaf) {
            $parent = Split-Path $target -Parent
            if (-not (Test-Path $parent)) { New-Item -ItemType Directory -Path $parent -Force | Out-Null }
            Copy-Item $backup $target -Force
            $restored++
        }
    }

    # 没有清单时的兜底：只还原备份里有的文件（无法识别本次新增）
    if ($changes.Count -eq 0) {
        Get-ChildItem -Path $BackupRoot -Recurse -File | ForEach-Object {
            $rel = $_.FullName.Substring($BackupRoot.Length).TrimStart('\', '/')
            if ($rel -eq 'manifest.json') { return }
            $target = Join-Path $SourceDir $rel
            $parent = Split-Path $target -Parent
            if (-not (Test-Path $parent)) { New-Item -ItemType Directory -Path $parent -Force | Out-Null }
            Copy-Item $_.FullName $target -Force
            $restored++
        }
    }

    Write-Log "还原完成：还原 $restored 个文件，删除 $removed 个本次新增文件"
    return ($restored -gt 0 -or $removed -gt 0)
}

function Invoke-ReleaseImage {
    param([string]$Dir, [object]$Manifest)
    # 镜像包更新（UPDATE_SOURCE=releases）：docker load 旧/新镜像包 → compose up -d → 写部署版本。
    # 与源码更新的区别：不动源码树、不备份文件；失败时旧镜像仍在，重跑即可。
    $started = Get-Date
    $version = [string]$Manifest.target_version
    if (-not $version) { $version = [string]$Manifest.commit }
    $action = [string]$Manifest.action
    $previous = [string]$Manifest.previous_version
    $imagePath = [string]$Manifest.image_path
    $imageName = [string]$Manifest.image_name
    $compose = [string]$Manifest.compose_file
    if (-not $compose) { $compose = $ComposeFile }

    Write-Log "发现镜像包更新：version=$version action=$action image=$imageName"

    if (-not $imagePath -or -not (Test-Path $imagePath -PathType Leaf)) {
        $ms = [int]((Get-Date) - $started).TotalMilliseconds
        Write-Result -State 'done' -Success $false -Message "镜像包不存在：$imagePath（若路径是容器内路径，请在宿主机上确认 UPDATE_DIR）" -DurationMs $ms -Commit $version -Dir $Dir
        Save-Manifest -Manifest (Join-Path $Dir 'pending-update.json') -Commit $version
        return
    }

    # 对账 SHA-256：清单给了校验值就必须一致，防止下载/拷贝过程中的损坏包被装入
    $expected = [string]$Manifest.image_sha256
    if ($expected) {
        $actual = (Get-FileHash -Path $imagePath -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $expected.ToLowerInvariant()) {
            $ms = [int]((Get-Date) - $started).TotalMilliseconds
            Write-Result -State 'done' -Success $false -Message "镜像包 SHA-256 校验失败（期望 $expected，实际 $actual）" -DurationMs $ms -Commit $version -Dir $Dir
            Save-Manifest -Manifest (Join-Path $Dir 'pending-update.json') -Commit $version
            return
        }
        Write-Log '镜像包 SHA-256 校验通过'
    }

    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        $ms = [int]((Get-Date) - $started).TotalMilliseconds
        Write-Result -State 'done' -Success $false -Message '宿主机没有 docker 命令，无法执行镜像包更新' -DurationMs $ms -Commit $version -Dir $Dir
        Save-Manifest -Manifest (Join-Path $Dir 'pending-update.json') -Commit $version
        return
    }

    $logFile = Join-Path $Dir 'build.log'
    '' | Set-Content -Path $logFile -Encoding utf8
    Write-Log "docker load -i $imagePath"
    & docker load -i $imagePath *>> $logFile
    if ($LASTEXITCODE -ne 0) {
        Write-Log "docker load 失败（$logFile）"
        Get-Content $logFile -Tail 20 | Write-Host
        $ms = [int]((Get-Date) - $started).TotalMilliseconds
        Write-Result -State 'done' -Success $false -Message "docker load 失败，详见 $logFile" -DurationMs $ms -Commit $version -Dir $Dir
        Save-Manifest -Manifest (Join-Path $Dir 'pending-update.json') -Commit $version
        return
    }

    $composePath = Join-Path $SourceDir $compose
    if (-not (Test-Path $composePath)) {
        $ms = [int]((Get-Date) - $started).TotalMilliseconds
        Write-Result -State 'done' -Success $false -Message "未找到 compose 编排文件：$composePath（可设 INKSTONE_COMPOSE_FILE 或 UPDATE_COMPOSE_FILE）" -DurationMs $ms -Commit $version -Dir $Dir
        Save-Manifest -Manifest (Join-Path $Dir 'pending-update.json') -Commit $version
        return
    }
    $composeArgs = @('compose', '-f', $composePath)
    $envFile = Join-Path $SourceDir '.env'
    if (Test-Path $envFile) { $composeArgs += @('--env-file', $envFile) }
    Write-Log "docker compose up -d（$compose）"
    & docker @composeArgs up -d *>> $logFile
    if ($LASTEXITCODE -ne 0) {
        Write-Log "docker compose up 失败（$logFile）"
        Get-Content $logFile -Tail 20 | Write-Host
        $ms = [int]((Get-Date) - $started).TotalMilliseconds
        Write-Result -State 'done' -Success $false -Message "docker compose up 失败，详见 $logFile" -DurationMs $ms -Commit $version -Dir $Dir
        Save-Manifest -Manifest (Join-Path $Dir 'pending-update.json') -Commit $version
        return
    }

    # 等待后端容器恢复健康（加载的是完整镜像包，重建是秒级；给足 60 秒容错）
    for ($i = 0; $i -lt 30; $i++) {
        $health = (& docker inspect -f '{{.State.Health.Status}}' $BackendContainer 2>$null)
        if ($health -match 'healthy') { break }
        Start-Sleep -Seconds 2
    }

    Save-ReleaseVersion -Dir $Dir -Version $version -ImagePath $imagePath -ImageName $imageName -Sha256 $expected
    [ordered]@{
        version    = $version
        updated_at = (Get-Date).ToString('o')
    } | ConvertTo-Json | Set-Content -Path (Join-Path $Dir 'deployed-version.json') -Encoding utf8

    $ms = [int]((Get-Date) - $started).TotalMilliseconds
    $message = if ($action -eq 'rollback') { "已回滚并重启到版本 $version" } else { "镜像包 $imageName 已加载，服务已重建（$version）" }
    Write-Result -State 'done' -Success $true -Message $message -DurationMs $ms -Commit $version -Dir $Dir
    Save-Manifest -Manifest (Join-Path $Dir 'pending-update.json') -Commit $version
    Write-Log "镜像包更新完成：$version"
}

function Save-ReleaseVersion {
    param([string]$Dir, [string]$Version, [string]$ImagePath, [string]$ImageName, [string]$Sha256)
    # 追加安装历史（最新在前，最多 10 条）：回滚时按版本号找回本地镜像包
    $historyFile = Join-Path $Dir 'release-history.json'
    $items = @()
    if (Test-Path $historyFile) {
        try {
            $parsed = Get-Content $historyFile -Raw -Encoding utf8 | ConvertFrom-Json
            if ($parsed) { $items = @($parsed) }
        }
        catch { $items = @() }
    }
    $entry = [ordered]@{
        version    = $Version
        image_path = $ImagePath
        image_name = $ImageName
        sha256     = $Sha256
        updated_at = (Get-Date).ToString('o')
    }
    # 同版本去重后置顶
    $kept = @()
    foreach ($item in $items) {
        if (-not $item) { continue }
        if ([string]$item.version -eq $Version) { continue }
        $kept += $item
    }
    $all = @($entry) + $kept
    if ($all.Count -gt 10) { $all = $all[0..9] }
    ($all | ConvertTo-Json -Depth 6) | Set-Content -Path $historyFile -Encoding utf8
}

function Invoke-Manifest {
    param([string]$Dir)
    $manifestPath = Join-Path $Dir 'pending-update.json'
    if (-not (Test-Path $manifestPath)) { return }

    try {
        $manifest = Get-Content $manifestPath -Raw -Encoding utf8 | ConvertFrom-Json
    }
    catch {
        Write-Log "待更新清单无法解析：$manifestPath"
        return
    }
    if ($manifest.state -ne 'pending') { return }

    # 镜像包更新走独立分支：不替换源码，docker load + compose up -d
    if ([string]$manifest.kind -eq 'release_image') {
        Invoke-ReleaseImage -Dir $Dir -Manifest $manifest
        return
    }

    $commit = [string]$manifest.commit
    $backupId = [string]$manifest.backup_id
    Write-Log "发现待更新：commit=$commit backup=$backupId"

    $started = Get-Date
    $type = Get-DeployType
    Write-Log "部署形态：$type"

    $backupRoot = Join-Path $Dir "backups\$backupId"
    if (-not (Invoke-Build -Type $type -Dir $Dir)) {
        if ($backupId -and (Test-Path $backupRoot)) {
            Write-Log '构建失败，还原源码后重建'
            Restore-Backup -BackupRoot $backupRoot -Manifest $manifestPath | Out-Null
            if (Invoke-Build -Type $type -Dir $Dir) {
                Invoke-Restart -Type $type | Out-Null
                $ms = [int]((Get-Date) - $started).TotalMilliseconds
                Write-Result -State 'done' -Success $false -Message '构建失败，已自动还原上一版本源码并重建' -DurationMs $ms -Commit $commit -Dir $Dir
                Save-Manifest -Manifest $manifestPath -Commit $commit
                return
            }
        }
        $ms = [int]((Get-Date) - $started).TotalMilliseconds
        Write-Result -State 'done' -Success $false -Message "构建失败，请查看 $Dir\build.log" -DurationMs $ms -Commit $commit -Dir $Dir
        Save-Manifest -Manifest $manifestPath -Commit $commit
        return
    }

    Invoke-Restart -Type $type | Out-Null

    # 记录部署版本：后端与「关于系统」页据此显示运行版本。
    # 回滚（commit=rollback:<备份ID>）不是一次上游提交，不能写进部署记录。
    if ($commit -and -not $commit.StartsWith('rollback:')) {
        [ordered]@{
            commit     = $commit
            updated_at = (Get-Date).ToString('o')
        } | ConvertTo-Json | Set-Content -Path (Join-Path $Dir 'deployed-commit.json') -Encoding utf8
    }
    else {
        Write-Log "目标不是上游提交（$commit）：跳过部署记录写入"
    }

    $ms = [int]((Get-Date) - $started).TotalMilliseconds
    Write-Result -State 'done' -Success $true -Message '重建与重启完成' -DurationMs $ms -Commit $commit -Dir $Dir
    Save-Manifest -Manifest $manifestPath -Commit $commit
    Write-Log "更新完成：$commit"
}

# —— 主循环 ——
$resolvedUpdateDir = Resolve-UpdateDir
Write-Log "源码目录：$SourceDir"
Write-Log "更新目录：$resolvedUpdateDir"
if ($Once) { Write-Log '单次模式' }

do {
    try { Invoke-Manifest -Dir $resolvedUpdateDir }
    catch { Write-Log "处理待更新时出错：$($_.Exception.Message)" }
    if (-not $Once) { Start-Sleep -Seconds $IntervalSeconds }
} while (-not $Once)
