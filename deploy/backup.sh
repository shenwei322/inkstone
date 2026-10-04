#!/usr/bin/env bash
# InkStone 数据库完整备份（pg_dump custom 格式，可 pg_restore）。
#
# 与后台「备份与恢复」页的关系：
#   - 后台页面导出的是 gzip JSON 快照，用于内容留档/比对，浏览器里一键完成；
#   - 本脚本产出的是 pg_dump custom 格式，是**可完整恢复**（含索引、约束、
#     序列、扩展）的备份，用于灾难恢复。
# 两者互补，建议 cron 每天跑本脚本、按需在后台手动做内容快照。
#
# 用法：
#   ./deploy/backup.sh [备份目录]
# 环境变量（都有默认值）：
#   BACKUP_DIR     备份输出目录（默认 <仓库>/data/backups/pg）
#   BACKUP_KEEP    保留份数（默认 14）
#   BACKUP_CONTAINER  运行中的 postgres 容器名（默认自动探测）
#   POSTGRES_USER / POSTGRES_DB  与 compose 中的值保持一致
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

BACKUP_DIR="${BACKUP_DIR:-$REPO_ROOT/data/backups/pg}"
BACKUP_KEEP="${BACKUP_KEEP:-14}"
POSTGRES_USER="${POSTGRES_USER:-blog}"
POSTGRES_DB="${POSTGRES_DB:-blog_platform}"
STAMP="$(date +%Y%m%d-%H%M%S)"
OUT="$BACKUP_DIR/inkstone-pg-$STAMP.dump"

mkdir -p "$BACKUP_DIR"

# 容器名未指定时自动探测：优先匹配 compose 项目名，其次匹配 postgres 镜像。
if [ -z "${BACKUP_CONTAINER:-}" ]; then
  BACKUP_CONTAINER="$(docker ps --format '{{.Names}}' \
    | grep -E 'postgres' | head -n1 || true)"
fi

if [ -n "$BACKUP_CONTAINER" ]; then
  echo "[backup] 使用容器 $BACKUP_CONTAINER"
  # 在容器内执行 pg_dump，标准输出重定向到宿主文件：
  # custom 格式（-Fc）本身是压缩的二进制，管道传输不会破坏它。
  docker exec "$BACKUP_CONTAINER" \
    pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc > "$OUT"
else
  echo "[backup] 未发现 postgres 容器，改用本机 pg_dump"
  command -v pg_dump >/dev/null 2>&1 || {
    echo "[backup] 错误：本机没有 pg_dump，且未找到 postgres 容器" >&2
    exit 1
  }
  pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc > "$OUT"
fi

SIZE="$(du -h "$OUT" | cut -f1)"
echo "[backup] 已生成 $OUT（$SIZE）"

# 保留最近 N 份，其余删除。按文件名排序即按时间排序（时间戳前缀）。
if [ "$BACKUP_KEEP" -gt 0 ]; then
  COUNT="$(ls -1 "$BACKUP_DIR"/inkstone-pg-*.dump 2>/dev/null | wc -l || true)"
  if [ "$COUNT" -gt "$BACKUP_KEEP" ]; then
    echo "[backup] 清理旧备份（保留 $BACKUP_KEEP 份，当前 $COUNT 份）"
    ls -1t "$BACKUP_DIR"/inkstone-pg-*.dump | tail -n +"$((BACKUP_KEEP + 1))" | \
      while read -r f; do rm -f "$f" && echo "[backup] 已删除 $f"; done
  fi
fi

# 完整性自检：custom 格式的备份可用 pg_restore -l 列出内容，
# 列不出来说明文件是坏的（磁盘满/管道中断），此时必须报错而非静默成功。
echo "[backup] 校验备份可读性..."
if [ -n "$BACKUP_CONTAINER" ]; then
  docker exec -i "$BACKUP_CONTAINER" pg_restore -l < "$OUT" > /dev/null
else
  pg_restore -l "$OUT" > /dev/null
fi
echo "[backup] 校验通过"
