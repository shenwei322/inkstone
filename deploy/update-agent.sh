#!/usr/bin/env bash
# =============================================================================
# InkStone 宿主更新代理（Linux / macOS）
#
# 为什么需要它？
#   后台「一键更新」运行在后端容器里：它能下载源码包、校验、替换源码，
#   但没有 docker 权限，也没有 systemd，无法把新代码真正跑起来。
#   本脚本跑在宿主机上，负责最后一步：备份 → 重建 → 重启 → 回报结果。
#
# 用法：
#   1) 手动执行一次（处理当前待更新）：
#        INKSTONE_SOURCE_DIR=/opt/inkstone ./deploy/update-agent.sh --once
#   2) 常驻轮询（推荐，systemd 托管）：
#        INKSTONE_SOURCE_DIR=/opt/inkstone ./deploy/update-agent.sh
#      每分钟检查一次后台是否写入了待更新清单。
#
# 依赖：docker compose（容器部署）或 go + npm（二进制部署），以及 backend 的更新目录。
# =============================================================================
set -uo pipefail

# —— 配置（可用环境变量覆盖）——
SOURCE_DIR="${INKSTONE_SOURCE_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
UPDATE_DIR="${INKSTONE_UPDATE_DIR:-}"
COMPOSE_FILE="${INKSTONE_COMPOSE_FILE:-docker-compose.prod.yml}"
ENV_FILE="${INKSTONE_ENV_FILE:-.env}"
SERVICE_UNIT="${INKSTONE_SERVICE:-}"
SERVICE_USER="${INKSTONE_SERVICE_USER:-}"
AGENT_NAME="${INKSTONE_AGENT_NAME:-host-update-agent}"
INTERVAL="${INKSTONE_POLL_SECONDS:-60}"
BUILD_TIMEOUT="${INKSTONE_BUILD_TIMEOUT:-2700}"   # 45 分钟
DEPLOY_TYPE="${INKSTONE_DEPLOY_TYPE:-auto}"       # auto | docker | binary

# 应用容器名（docker 模式下用来探测健康状态）
BACKEND_CONTAINER="${INKSTONE_BACKEND_CONTAINER:-blog-backend}"

log() { printf '[update-agent %s] %s\n' "$(date '+%F %T')" "$*"; }
fail() { log "错误：$*"; }

# —— 定位后台写下的更新目录 ——
# 后端用 UPDATE_DIR（默认 <UPLOAD_DIR 的上级>/update）落盘，容器内通常是 /app/data/update，
# 对应宿主的 docker 数据卷。这里按顺序探测，找到含 pending-update.json 的那个。
resolve_update_dir() {
  if [ -n "$UPDATE_DIR" ]; then
    printf '%s' "$UPDATE_DIR"
    return
  fi
  for candidate in \
    "$SOURCE_DIR/data/update" \
    "$SOURCE_DIR/backend/data/update" \
    /opt/inkstone/data/update \
    /app/data/update; do
    if [ -f "$candidate/pending-update.json" ] || [ -d "$candidate" ]; then
      printf '%s' "$candidate"
      return
    fi
  done
  printf '%s' "$SOURCE_DIR/data/update"
}

# —— 判断部署形态 ——
detect_deploy_type() {
  if [ "$DEPLOY_TYPE" != "auto" ]; then
    printf '%s' "$DEPLOY_TYPE"
    return
  fi
  if [ -f "$SOURCE_DIR/$COMPOSE_FILE" ]; then
    printf 'docker'
    return
  fi
  printf 'binary'
}

# —— 写入结果文件（后端「系统更新」页会读取它）——
write_result() {
  local state="$1" success="$2" message="$3" duration_ms="$4" commit="$5" dir="$6"
  mkdir -p "$dir" 2>/dev/null || true
  cat >"$dir/update-result.json" <<JSON
{
  "state": "$state",
  "commit": "$commit",
  "success": $success,
  "message": "$(printf '%s' "$message" | sed 's/"/\\"/g')",
  "agent": "$AGENT_NAME",
  "finished_at": "$(date -Iseconds 2>/dev/null || date '+%Y-%m-%dT%H:%M:%S%z')",
  "duration_ms": $duration_ms
}
JSON
}

# —— 标记清单已处理（避免重复重建）——
mark_handled() {
  local dir="$1" commit="$2"
  if command -v python3 >/dev/null 2>&1; then
    python3 - "$dir/pending-update.json" <<'PY' 2>/dev/null || true
import json, sys
path = sys.argv[1]
with open(path, encoding='utf-8') as fh:
    data = json.load(fh)
data['state'] = 'done'
with open(path, 'w', encoding='utf-8') as fh:
    json.dump(data, fh, ensure_ascii=False, indent=2)
PY
  else
    printf '{"state":"done","commit":"%s"}\n' "$commit" >"$dir/pending-update.json"
  fi
}

# —— 从清单里读一个字符串字段（无 python 时的兜底实现）——
manifest_field() {
  local file="$1" key="$2"
  if command -v python3 >/dev/null 2>&1; then
    python3 - "$file" "$key" <<'PY' 2>/dev/null
import json, sys
with open(sys.argv[1], encoding='utf-8') as fh:
    print(json.load(fh).get(sys.argv[2], '') or '')
PY
    return
  fi
  sed -n "s/.*\"$key\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$file" | head -n1
}

# —— 把待更新清单里的变更翻译成还原动作 ——
# pending-update.json 的 changes 形如：
#   [{"path": "backend/go.mod", "action": "write", "existed": true, "bytes": 123}, ...]
# 还原规则（与后端 rollback 的语义保持一致）：
#   action=write & existed=true  → 从备份还原该文件
#   action=write & existed=false → 本次新增，删除它（否则留下新旧混合的代码树）
#   action=delete                → 备份里有就还原
restore_backup() {
  local source_dir="$1" backup_root="$2" manifest="$3"
  [ -d "$backup_root" ] || return 1

  if command -v python3 >/dev/null 2>&1 && [ -f "$manifest" ]; then
    if python3 - "$manifest" "$source_dir" "$backup_root" <<'PY'
import json, os, shutil, sys

manifest, source_dir, backup_root = sys.argv[1], sys.argv[2], sys.argv[3]
with open(manifest, encoding='utf-8') as fh:
    changes = json.load(fh).get('changes') or []

restored = removed = 0
for change in changes:
    rel = (change.get('path') or '').replace('\\', '/').strip('/')
    if not rel or rel == '..' or rel.startswith('../') or '/../' in rel:
        continue
    target = os.path.join(source_dir, rel)
    backup = os.path.join(backup_root, rel)
    action = change.get('action')
    if action == 'write' and not change.get('existed', False):
        if os.path.isfile(target):
            os.remove(target)
            removed += 1
        continue
    if os.path.isfile(backup):
        os.makedirs(os.path.dirname(target), exist_ok=True)
        shutil.copy2(backup, target)
        restored += 1
print(f"restored={restored} removed={removed}")
PY
    then
      return 0
    fi
    log "警告：按清单还原失败（清单可能损坏），退回按备份目录还原"
  fi

  # 没有 python3 时的兜底：只还原备份里有的文件（无法识别本次新增，属降级行为）
  local count=0
  while IFS= read -r rel; do
    case "$rel" in manifest.json) continue ;; esac
    if [ -e "$backup_root/$rel" ]; then
      mkdir -p "$(dirname "$source_dir/$rel")"
      cp -p "$backup_root/$rel" "$source_dir/$rel" && count=$((count + 1))
    fi
  done < <(cd "$backup_root" && find . -type f -printf '%P\n' 2>/dev/null)
  log "已从 $backup_root 还原 $count 个文件（无 python3：未清理本次新增文件）"
  [ "$count" -gt 0 ]
}

# —— 重建 ——
run_build() {
  local type="$1" dir="$2"
  local log_file="$dir/build.log"
  : >"$log_file"

  if [ "$type" = "docker" ]; then
    local args=(-f "$SOURCE_DIR/$COMPOSE_FILE")
    [ -f "$SOURCE_DIR/$ENV_FILE" ] && args+=(--env-file "$SOURCE_DIR/$ENV_FILE")
    log "docker compose build（日志：$log_file）"
    ( cd "$SOURCE_DIR" && timeout "$BUILD_TIMEOUT" docker compose "${args[@]}" build ) >>"$log_file" 2>&1 || {
      fail "docker compose build 失败，最后 20 行："
      tail -n 20 "$log_file" >&2
      return 1
    }
    log "docker compose up -d"
    ( cd "$SOURCE_DIR" && docker compose "${args[@]}" up -d ) >>"$log_file" 2>&1 || {
      fail "docker compose up 失败，最后 20 行："
      tail -n 20 "$log_file" >&2
      return 1
    }
    return 0
  fi

  # 二进制部署：编译后端 + 前端，然后交给 systemd 重启
  command -v go >/dev/null 2>&1 || { fail "未找到 go 命令（二进制部署需要 Go 工具链）"; return 1; }
  log "go build（日志：$log_file）"
  # -tags timetzdata：内嵌时区库，避免精简环境 "unknown time zone Asia/Shanghai" 启动失败
  ( cd "$SOURCE_DIR/backend" && timeout "$BUILD_TIMEOUT" go build -trimpath -tags timetzdata -o server ./cmd/server ) >>"$log_file" 2>&1 || {
    fail "go build 失败，最后 20 行："
    tail -n 20 "$log_file" >&2
    return 1
  }
  if [ -f "$SOURCE_DIR/frontend/package.json" ] && command -v npm >/dev/null 2>&1; then
    log "npm ci && npm run build"
    ( cd "$SOURCE_DIR/frontend" && npm ci && timeout "$BUILD_TIMEOUT" npm run build ) >>"$log_file" 2>&1 || {
      fail "前端构建失败，最后 20 行："
      tail -n 20 "$log_file" >&2
      return 1
    }
  fi
  return 0
}

# —— 重启 ——
run_restart() {
  local type="$1"
  if [ "$type" = "docker" ]; then
    # compose up -d 已经滚动重启，这里只等健康检查
    for _ in $(seq 1 30); do
      if docker inspect -f '{{.State.Health.Status}}' "$BACKEND_CONTAINER" 2>/dev/null | grep -q healthy; then
        log "后端容器健康检查通过"
        return 0
      fi
      sleep 2
    done
    log "警告：后端容器未在 60 秒内变为 healthy，请手工执行 docker compose ps 确认"
    return 0
  fi

  if [ -n "$SERVICE_UNIT" ] && command -v systemctl >/dev/null 2>&1; then
    log "systemctl restart $SERVICE_UNIT"
    if [ -n "$SERVICE_USER" ]; then
      sudo -u "$SERVICE_USER" systemctl restart "$SERVICE_UNIT" 2>/dev/null || systemctl restart "$SERVICE_UNIT"
    else
      systemctl restart "$SERVICE_UNIT"
    fi
    sleep 2
    systemctl is-active --quiet "$SERVICE_UNIT" && log "服务已重启" || fail "服务未处于 active 状态"
    return 0
  fi

  log "未配置 INKSTONE_SERVICE，跳过自动重启：请手工启动服务（如 ./backend/server）"
  return 0
}

# —— 追加镜像包安装历史（最新在前，最多 10 条；回滚时按版本号找回本地包）——
save_release_version() {
  local dir="$1" version="$2" image_path="$3" image_name="$4" sha256="$5"
  local history="$dir/release-history.json"
  if command -v python3 >/dev/null 2>&1; then
    python3 - "$history" "$version" "$image_path" "$image_name" "$sha256" <<'PY' 2>/dev/null || \
    printf '{"version":"%s","image_path":"%s","image_name":"%s","sha256":"%s"}\n' "$version" "$image_path" "$image_name" "$sha256" >"$history"
import json, sys, time
path, version, image_path, image_name, sha256 = sys.argv[1:6]
try:
    with open(path, encoding='utf-8') as fh:
        items = json.load(fh) or []
except Exception:
    items = []
items = [i for i in items if isinstance(i, dict) and i.get('version') != version]
items.insert(0, {
    'version': version, 'image_path': image_path,
    'image_name': image_name, 'sha256': sha256,
    'updated_at': time.strftime('%Y-%m-%dT%H:%M:%S%z'),
})
with open(path, 'w', encoding='utf-8') as fh:
    json.dump(items[:10], fh, ensure_ascii=False, indent=2)
PY
  else
    printf '{"version":"%s","image_path":"%s","image_name":"%s","sha256":"%s"}\n' \
      "$version" "$image_path" "$image_name" "$sha256" >"$history"
  fi
}

# —— 镜像包更新（UPDATE_SOURCE=releases）——
# 与源码更新的区别：不动源码树、不做文件备份；docker load + compose up -d。
# 失败时旧镜像仍在本地，重新执行即可。
run_release_image() {
  local dir="$1" manifest="$2"
  local started_ms
  started_ms="$(($(date +%s) * 1000))"

  local version action image_path image_name compose expected
  version="$(manifest_field "$manifest" target_version)"
  [ -n "$version" ] || version="$(manifest_field "$manifest" commit)"
  action="$(manifest_field "$manifest" action)"
  image_path="$(manifest_field "$manifest" image_path)"
  image_name="$(manifest_field "$manifest" image_name)"
  compose="$(manifest_field "$manifest" compose_file)"
  expected="$(manifest_field "$manifest" image_sha256)"
  [ -n "$compose" ] || compose="$COMPOSE_FILE"

  log "发现镜像包更新：version=$version action=$action image=$image_name"

  if [ -z "$image_path" ] || [ ! -f "$image_path" ]; then
    write_result "done" false "镜像包不存在：$image_path（若路径是容器内路径，请在宿主机确认 UPDATE_DIR）" \
      "$(( $(date +%s) * 1000 - started_ms ))" "$version" "$dir"
    mark_handled "$dir" "$version"
    return 0
  fi

  # 对账 SHA-256：清单给了校验值就必须一致，防止损坏包被装入
  if [ -n "$expected" ]; then
    local actual
    actual="$(sha256sum "$image_path" | awk '{print $1}')"
    if [ "$actual" != "$expected" ]; then
      write_result "done" false "镜像包 SHA-256 校验失败（期望 $expected，实际 $actual）" \
        "$(( $(date +%s) * 1000 - started_ms ))" "$version" "$dir"
      mark_handled "$dir" "$version"
      return 0
    fi
    log "镜像包 SHA-256 校验通过"
  fi

  command -v docker >/dev/null 2>&1 || {
    write_result "done" false "宿主机没有 docker 命令，无法执行镜像包更新" \
      "$(( $(date +%s) * 1000 - started_ms ))" "$version" "$dir"
    mark_handled "$dir" "$version"
    return 0
  }

  local log_file="$dir/build.log"
  : >"$log_file"
  log "docker load -i $image_path"
  if ! docker load -i "$image_path" >>"$log_file" 2>&1; then
    tail -n 20 "$log_file" >&2
    write_result "done" false "docker load 失败，详见 $log_file" \
      "$(( $(date +%s) * 1000 - started_ms ))" "$version" "$dir"
    mark_handled "$dir" "$version"
    return 0
  fi

  if [ ! -f "$SOURCE_DIR/$compose" ]; then
    write_result "done" false "未找到 compose 编排文件：$SOURCE_DIR/$compose（可设 INKSTONE_COMPOSE_FILE 或 UPDATE_COMPOSE_FILE）" \
      "$(( $(date +%s) * 1000 - started_ms ))" "$version" "$dir"
    mark_handled "$dir" "$version"
    return 0
  fi
  local args=(-f "$SOURCE_DIR/$compose")
  [ -f "$SOURCE_DIR/$ENV_FILE" ] && args+=(--env-file "$SOURCE_DIR/$ENV_FILE")
  log "docker compose up -d（$compose）"
  if ! ( cd "$SOURCE_DIR" && docker compose "${args[@]}" up -d ) >>"$log_file" 2>&1; then
    tail -n 20 "$log_file" >&2
    write_result "done" false "docker compose up 失败，详见 $log_file" \
      "$(( $(date +%s) * 1000 - started_ms ))" "$version" "$dir"
    mark_handled "$dir" "$version"
    return 0
  fi

  # 等后端容器恢复健康（加载的是完整镜像包，重建是秒级；给足 60 秒容错）
  for _ in $(seq 1 30); do
    if docker inspect -f '{{.State.Health.Status}}' "$BACKEND_CONTAINER" 2>/dev/null | grep -q healthy; then
      log "后端容器健康检查通过"
      break
    fi
    sleep 2
  done

  save_release_version "$dir" "$version" "$image_path" "$image_name" "$expected"
  printf '{\n  "version": "%s",\n  "updated_at": "%s"\n}\n' \
    "$version" "$(date -Iseconds 2>/dev/null || date '+%Y-%m-%dT%H:%M:%S%z')" >"$dir/deployed-version.json"

  local ms=$(( $(date +%s) * 1000 - started_ms ))
  if [ "$action" = "rollback" ]; then
    write_result "done" true "已回滚并重启到版本 $version" "$ms" "$version" "$dir"
  else
    write_result "done" true "镜像包 $image_name 已加载，服务已重建（$version）" "$ms" "$version" "$dir"
  fi
  mark_handled "$dir" "$version"
  log "镜像包更新完成：$version"
  return 0
}

# —— 处理一个待更新清单 ——
process_manifest() {
  local dir="$1"
  local manifest="$dir/pending-update.json"
  [ -f "$manifest" ] || return 1

  local state commit backup_id staged
  state="$(manifest_field "$manifest" state)"
  [ "$state" = "pending" ] || return 1

  # 镜像包更新走独立分支：不替换源码，docker load + compose up -d
  if [ "$(manifest_field "$manifest" kind)" = "release_image" ]; then
    run_release_image "$dir" "$manifest"
    return 0
  fi

  commit="$(manifest_field "$manifest" commit)"
  backup_id="$(manifest_field "$manifest" backup_id)"
  staged="$(manifest_field "$manifest" staged_dir)"

  log "发现待更新：commit=$commit backup=$backup_id"
  local started_ms
  started_ms="$(($(date +%s) * 1000))"

  local type
  type="$(detect_deploy_type)"
  log "部署形态：$type"

  local backup_root="$dir/backups/$backup_id"
  if ! run_build "$type" "$dir"; then
    if [ -n "$backup_id" ] && [ -d "$backup_root" ]; then
      log "构建失败，尝试还原源码后重建"
      restore_backup "$SOURCE_DIR" "$backup_root" "$manifest" || true
      if run_build "$type" "$dir"; then
        run_restart "$type" || true
        local ms=$(( $(date +%s) * 1000 - started_ms ))
        write_result "done" false "构建失败，已自动还原上一版本源码并重建" "$ms" "$commit" "$dir"
        mark_handled "$dir" "$commit"
        return 0
      fi
    fi
    local ms=$(( $(date +%s) * 1000 - started_ms ))
    write_result "done" false "构建失败，请查看 $dir/build.log" "$ms" "$commit" "$dir"
    mark_handled "$dir" "$commit"
    return 0
  fi

  run_restart "$type" || true

  # 记录部署版本：后端与「关于系统」页据此显示运行版本。
  # 回滚（commit=rollback:<备份ID>）不是一次上游提交，不能写进部署记录，否则版本页会显示垃圾值。
  case "$commit" in
    rollback:*)
      log "回滚任务：跳过部署记录写入（保留原提交标识）"
      ;;
    "")
      log "清单缺少 commit 字段：跳过部署记录写入"
      ;;
    *)
      local deployed="$dir/deployed-commit.json"
      printf '{\n  "commit": "%s",\n  "updated_at": "%s"\n}\n' \
        "$commit" "$(date -Iseconds 2>/dev/null || date '+%Y-%m-%dT%H:%M:%S%z')" >"$deployed"
      ;;
  esac

  local ms=$(( $(date +%s) * 1000 - started_ms ))
  write_result "done" true "重建与重启完成" "$ms" "$commit" "$dir"
  mark_handled "$dir" "$commit"
  log "更新完成：$commit"

  # 清掉旧暂存目录，只保留刚用完的这个
  if [ -n "$staged" ] && [ -d "$dir/staging" ]; then
    find "$dir/staging" -mindepth 1 -maxdepth 1 -type d ! -name "$(basename "$staged")" -exec rm -rf {} + 2>/dev/null || true
  fi
  return 0
}

main() {
  local once=0
  [ "${1:-}" = "--once" ] && once=1

  local update_dir
  update_dir="$(resolve_update_dir)"
  log "源码目录：$SOURCE_DIR"
  log "更新目录：$update_dir"
  [ "$once" = "1" ] && log "单次模式"

  while true; do
    process_manifest "$update_dir" || true
    [ "$once" = "1" ] && break
    sleep "$INTERVAL"
  done
}

main "$@"
