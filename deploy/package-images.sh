#!/usr/bin/env bash
# =============================================================================
# InkStone 镜像包打包（Linux / macOS）
#
# 产物：dist/inkstone-images-<版本>.tar —— GitHub Releases 的镜像包资产。
# 后台「系统更新」（UPDATE_SOURCE=releases）与一键离线部署都消费这个文件：
#   docker load -i inkstone-images-<版本>.tar
#   docker compose --env-file .env -f docker-compose.offline.yml up -d
#
# 用法：
#   ./deploy/package-images.sh                          # 默认版本 Beta1.27
#   ./deploy/package-images.sh v1.28.0
#   INKSTONE_PUBLIC_API_URL=https://blog.shenv.top/api/v1 ./deploy/package-images.sh
# =============================================================================
set -euo pipefail

VERSION="${1:-${INKSTONE_VERSION:-Beta1.27}}"
# 前端 API 地址：NEXT_PUBLIC_* 是构建期注入，打进镜像后改不了，按部署域名传
API_URL="${INKSTONE_PUBLIC_API_URL:-https://blog.shenv.top/api/v1}"
OUT_DIR="${INKSTONE_DIST_DIR:-dist}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if ! command -v docker >/dev/null 2>&1; then
  echo "未找到 docker 命令（需要 Docker Engine）" >&2
  exit 1
fi
if ! docker version --format '{{.Server.Version}}' >/dev/null 2>&1; then
  echo "docker 引擎未运行或当前用户无权限" >&2
  exit 1
fi

BACKEND_TAGS=("inkstone-backend:${VERSION}" inkstone-backend:latest)
FRONTEND_TAGS=("inkstone-frontend:${VERSION}" inkstone-frontend:latest)

# 1) 后端（多阶段构建：golang 编译 → alpine 运行）
backend_args=(build -t "inkstone-backend:${VERSION}" -t inkstone-backend:latest
  -f "$REPO_ROOT/backend/Dockerfile" "$REPO_ROOT/backend")
echo "==> 构建后端镜像 ${BACKEND_TAGS[*]}"
docker "${backend_args[@]}"

# 2) 前端（NEXT_PUBLIC_API_URL 构建期注入）
frontend_args=(build -t "inkstone-frontend:${VERSION}" -t inkstone-frontend:latest
  --build-arg "NEXT_PUBLIC_API_URL=${API_URL}"
  -f "$REPO_ROOT/frontend/Dockerfile" "$REPO_ROOT/frontend")
echo "==> 构建前端镜像 ${FRONTEND_TAGS[*]}"
docker "${frontend_args[@]}"

# 3) 导出镜像包
mkdir -p "$REPO_ROOT/$OUT_DIR"
TAR_PATH="$REPO_ROOT/$OUT_DIR/inkstone-images-${VERSION}.tar"
rm -f "$TAR_PATH"
echo "==> 导出镜像包 inkstone-images-${VERSION}.tar"
docker save -o "$TAR_PATH" "${BACKEND_TAGS[@]}" "${FRONTEND_TAGS[@]}"

# 4) 校验值（发布时附在 Release 说明或资产名旁）
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$REPO_ROOT/$OUT_DIR" && sha256sum "inkstone-images-${VERSION}.tar" | tee "inkstone-images-${VERSION}.tar.sha256")
elif command -v shasum >/dev/null 2>&1; then
  (cd "$REPO_ROOT/$OUT_DIR" && shasum -a 256 "inkstone-images-${VERSION}.tar" | tee "inkstone-images-${VERSION}.tar.sha256")
fi

echo
echo "完成：$TAR_PATH（$(du -h "$TAR_PATH" | cut -f1)）"
echo
echo "验证（另开一台机器）："
echo "  docker load -i $TAR_PATH"
echo "  docker compose --env-file .env -f docker-compose.offline.yml up -d"
