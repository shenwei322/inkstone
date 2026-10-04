#!/usr/bin/env bash
# =============================================================================
# InkStone 本机重建脚本（二进制 / systemd 部署）
#
# 由后端「系统更新」在替换源码后自动拉起（此时后端仍是旧进程）：
#   1. 等旧后端进程退出（不然二进制被占用/覆盖出问题）
#   2. 重新编译 Go 后端（必要时构建前端）
#   3. 交给 systemd 重启，或直接拉起 ./backend/server
#   4. 把结果写进更新目录的 update-result.json
#
# 环境变量由后端注入：INKSTONE_TARGET / INKSTONE_REPO_ROOT /
#   INKSTONE_UPDATE_DIR / INKSTONE_SELF / INKSTONE_PID / INKSTONE_SERVICE / INKSTONE_PORT
#
# 也可以手工执行：INKSTONE_REPO_ROOT=/opt/inkstone ./deploy/scripts/rebuild.sh
# =============================================================================
set -uo pipefail

REPO_ROOT="${INKSTONE_REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
UPDATE_DIR="${INKSTONE_UPDATE_DIR:-$REPO_ROOT/data/update}"
TARGET="${INKSTONE_TARGET:-}"
PID_TO_WAIT="${INKSTONE_PID:-}"
SELF_BIN="${INKSTONE_SELF:-$REPO_ROOT/backend/server}"
SERVICE_UNIT="${INKSTONE_SERVICE:-}"
PORT="${INKSTONE_PORT:-8080}"
BUILD_TIMEOUT="${INKSTONE_BUILD_TIMEOUT:-1800}"
WAIT_LIMIT="${INKSTONE_WAIT_SECONDS:-60}"

log() { printf '[rebuild %s] %s\n' "$(date '+%F %T')" "$*"; }

write_result() {
  local success="$1" message="$2" ms="$3"
  mkdir -p "$UPDATE_DIR" 2>/dev/null || true
  cat >"$UPDATE_DIR/update-result.json" <<JSON
{
  "state": "done",
  "commit": "$TARGET",
  "success": $success,
  "message": "$(printf '%s' "$message" | sed 's/"/\\"/g')",
  "agent": "local-rebuild.sh",
  "finished_at": "$(date -Iseconds 2>/dev/null || date '+%Y-%m-%dT%H:%M:%S%z')",
  "duration_ms": $ms
}
JSON
}

# 1) 等旧进程退出
if [ -n "$PID_TO_WAIT" ]; then
  waited=0
  while kill -0 "$PID_TO_WAIT" 2>/dev/null; do
    if [ "$waited" -ge "$WAIT_LIMIT" ]; then
      log "旧进程 $PID_TO_WAIT 在 ${WAIT_LIMIT}s 内未退出，继续构建"
      break
    fi
    sleep 1
    waited=$((waited + 1))
  done
  log "旧进程已退出（等待 ${waited}s）"
fi
sleep 1

started_ms=$(($(date +%s) * 1000))
mkdir -p "$UPDATE_DIR" 2>/dev/null || true
log_file="$UPDATE_DIR/build.log"
: >"$log_file"

# 2) 编译
if ! command -v go >/dev/null 2>&1; then
  write_result false "未找到 go 命令，无法编译" 0
  log "错误：未找到 go 命令"
  exit 1
fi

log "编译后端：$REPO_ROOT/backend/server"
# -tags timetzdata：内嵌 IANA 时区库。精简 Linux 镜像 / 非标准区域环境下
# time.LoadLocation("Asia/Shanghai") 会报 "unknown time zone" 导致启动失败
if ! ( cd "$REPO_ROOT/backend" && timeout "$BUILD_TIMEOUT" go build -trimpath -tags timetzdata -o server ./cmd/server ) >>"$log_file" 2>&1; then
  log "编译失败，最后 20 行日志："
  tail -n 20 "$log_file" >&2
  write_result false "go build 失败，日志：$log_file" "$(( $(date +%s) * 1000 - started_ms ))"
  exit 1
fi

if [ -f "$REPO_ROOT/frontend/package.json" ] && command -v npm >/dev/null 2>&1; then
  log "构建前端"
  if ! ( cd "$REPO_ROOT/frontend" && npm ci && timeout "$BUILD_TIMEOUT" npm run build ) >>"$log_file" 2>&1; then
    log "前端构建失败，最后 20 行日志："
    tail -n 20 "$log_file" >&2
    write_result false "前端构建失败，日志：$log_file" "$(( $(date +%s) * 1000 - started_ms ))"
    exit 1
  fi
fi

# 3) 重启
if [ -n "$SERVICE_UNIT" ] && command -v systemctl >/dev/null 2>&1; then
  log "systemctl restart $SERVICE_UNIT"
  systemctl restart "$SERVICE_UNIT"
  sleep 2
  if systemctl is-active --quiet "$SERVICE_UNIT"; then
    log "服务已重启"
  else
    log "警告：服务未处于 active 状态，请 systemctl status $SERVICE_UNIT 查看"
  fi
else
  log "未配置 INKSTONE_SERVICE，直接拉起后端进程"
  if [ ! -x "$SELF_BIN" ]; then
    chmod +x "$SELF_BIN" 2>/dev/null || true
  fi
  ( cd "$REPO_ROOT/backend" && setsid nohup "$SELF_BIN" >>"$UPDATE_DIR/backend.log" 2>&1 & )
  log "已拉起：$SELF_BIN（日志：$UPDATE_DIR/backend.log，端口 ${PORT}）"
fi

# 4) 记录部署版本（回滚任务不是上游提交，跳过写入）
case "$TARGET" in
  rollback:*|"")
    log "目标不是上游提交（$TARGET）：跳过部署记录写入"
    ;;
  *)
    printf '{\n  "commit": "%s",\n  "updated_at": "%s"\n}\n' \
      "$TARGET" "$(date -Iseconds 2>/dev/null || date '+%Y-%m-%dT%H:%M:%S%z')" >"$UPDATE_DIR/deployed-commit.json"
    ;;
esac

write_result true "本机编译与重启完成" "$(( $(date +%s) * 1000 - started_ms ))"
log "完成：$TARGET"
