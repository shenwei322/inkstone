# 部署与运维

## 部署方式对比

| 方式 | 适用 | 命令 |
|---|---|---|
| **离线镜像**（推荐小内存 VPS） | 服务器只 `docker load`，不编译 | 见下方 |
| 源码构建 | 内存 ≥ 4G 的服务器 | `docker compose up -d --build` |
| 本地开发 | 开发者本机 | `docker compose -f docker-compose.dev.yml up -d` |

**为什么推荐离线镜像**：服务器编译 Go + npm 需要 2-4GB 内存，小 VPS 容易 OOM 导致 SSH 断连。离线方式只需 `docker load`（几百 MB、约 30 秒）。

---

## 离线镜像部署（完整流程）

### 1. 本地构建镜像

```powershell
# 后端
docker build -t inkstone-backend:latest "D:\blog-platform-release\blog-platform\backend"

# 前端（API 地址必须构建期注入，填你的域名）
docker build -t inkstone-frontend:latest `
  --build-arg NEXT_PUBLIC_API_URL=https://blog.shenv.top/api/v1 `
  "D:\blog-platform-release\blog-platform\frontend"

# 导出为 tar
docker save -o "D:\blog-platform-release\inkstone-images.tar" `
  inkstone-backend:latest inkstone-frontend:latest
```

### 2. 上传到服务器

```powershell
scp "D:\blog-platform-release\inkstone-images.tar" root@<服务器IP>:/opt/
scp "D:\blog-platform-release\inkstone-offline-deploy.zip" root@<服务器IP>:/opt/
```

### 3. 服务器加载启动

```bash
cd /opt
docker load -i inkstone-images.tar      # 约 30 秒
unzip -q -o inkstone-offline-deploy.zip
cd inkstone-deploy
cat .env                                 # 确认配置
docker compose up -d
docker compose ps
curl http://127.0.0.1:8080/healthz      # {"status":"ok"}
```

### 4. Nginx 反向代理 + HTTPS

```nginx
server {
    listen 80;
    server_name blog.shenv.top;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    http2 on;
    server_name blog.shenv.top;
    client_max_body_size 100M;

    ssl_certificate     /etc/nginx/ssl/blog.shenv.top.crt;
    ssl_certificate_key /etc/nginx/ssl/blog.shenv.top.key;
    ssl_protocols TLSv1.2 TLSv1.3;

    # API / 上传 / 文件 → 后端
    location /api/     { proxy_pass http://127.0.0.1:8080; include /etc/nginx/proxy_params; }
    location /uploads/ { proxy_pass http://127.0.0.1:8080; include /etc/nginx/proxy_params; }
    location /files/   { proxy_pass http://127.0.0.1:8080; include /etc/nginx/proxy_params; }

    # SEO feeds（后端 gin 根路由；精确匹配，避免落入前端 catch-all 404）
    location = /sitemap.xml { proxy_pass http://127.0.0.1:8080; include /etc/nginx/proxy_params; }
    location = /robots.txt  { proxy_pass http://127.0.0.1:8080; include /etc/nginx/proxy_params; }
    location = /feed.xml    { proxy_pass http://127.0.0.1:8080; include /etc/nginx/proxy_params; }

    # 其余 → 前端
    location / { proxy_pass http://127.0.0.1:3000; include /etc/nginx/proxy_params; }
}
```

**关键点**：
- `/api`、`/uploads`、`/files` **必须**指到后端 8080（否则图片 404、接口不通）
- `/sitemap.xml`、`/robots.txt`、`/feed.xml` 用 **`location =` 精确匹配**指到后端 8080
  （gin 根路由提供这三个 SEO 端点；若漏配会落进 `location /` → Next.js 无此路由 → 404，
  robots.txt 里宣传的 sitemap 地址失效、搜索引擎收录与 RSS 订阅全断）
- 完整可复制配置见仓库 `deploy/nginx/inkstone.conf`（与线上一致，改动先改仓库再同步服务器）。
  **漏同步线上 conf 是历史事故源**，改完必须 base64 推服务器 → `nginx -t` → `nginx -s reload`
- 前端容器端口只绑 `127.0.0.1`（`FRONTEND_BIND=127.0.0.1`），由 Nginx 对外
- 阿里云等要放行安全组 `80` + `443`

---

## 环境变量（后端）

| 变量 | 默认值 | 说明 |
|---|---|---|
| `APP_ENV` | `development` | `production` 关闭调试日志、启用 release 模式 |
| `PORT` | `8080` | 监听端口 |
| `DB_HOST` | `localhost` | PostgreSQL 地址（容器内填 `postgres`） |
| `DB_PORT` | `5432` | |
| `DB_USER` | `blog` | |
| `DB_PASSWORD` | `blog_dev_password` | **生产必改** |
| `DB_NAME` | `blog_platform` | |
| `JWT_SECRET` | `dev-only-...` | **生产必改**，64 位随机串 |
| `FRONTEND_URL` | `http://localhost:3000` | CORS 白名单 + RSS 链接 |
| `PUBLIC_API_URL` | `""` | 上传文件访问前缀（反代场景留空用相对路径） |
| `UPLOAD_DIR` | `./data/uploads` | 图片存储目录 |
| `FILES_DIR` | `./data/files` | 文件管理存储目录 |
| `INKSTONE_DISABLE_CAPTCHA` | — | 设为 `1` 全局停用验证码（应急） |
| `INKSTONE_LAP_SECRET` | 内置默认值 | 覆盖内置 Lap 实例的 siteverify secret（生产建议设置；未设置时用代码内置默认实例，零配置可用） |
| `UPDATE_ENABLED` | `true` | 在线更新总开关（离线镜像部署默认 `false`） |
| `UPDATE_REPO_URL` | GitHub 仓库地址 | 上游仓库，用于比对提交与生成网页链接 |
| `UPDATE_BRANCH` | `main` | 跟踪分支 |
| `UPDATE_SOURCE` | `commits` | 更新源协议：`commits`（默认，源码包替换源码）/ `releases`（GitHub Releases 镜像包） |
| `UPDATE_MIRROR` | `https://gh-proxy.com/https://github.com/{owner}/{name}/archive/{commit}.tar.gz` | 源码包镜像模板（`{owner}` `{name}` `{commit}` `{short}` `{branch}` `{ref}` `{repo}` 占位） |
| `UPDATE_GITHUB_API` | `https://api.github.com` | 元数据接口基址（可指向自建代理） |
| `UPDATE_RELEASES_API` | `{UPDATE_GITHUB_API}/repos/{owner}/{name}/releases/latest` | releases 模式的 Release 接口模板（`{api}` `{owner}` `{name}` `{repo}` `{branch}` 占位） |
| `UPDATE_IMAGE_ASSET` | `inkstone-images-.*\.tar$` | releases 模式镜像包资产名匹配（正则；多个命中优先版本号出现在文件名里的） |
| `UPDATE_IMAGE_MAX_MB` | `2048` | releases 模式镜像包大小上限（MB） |
| `UPDATE_COMPOSE_FILE` | 自动探测 | releases 模式安装用的 compose 文件（容器内路径相对 `UPDATE_SOURCE_DIR`；默认探测 `docker-compose.offline.yml` → `prod` → `.yml`） |
| `UPDATE_VERSION_FILE` | 自动探测 | 部署版本记录文件（releases 模式；默认 `data/deployed-version.json` 或 `UPDATE_DIR/deployed-version.json`） |
| `UPDATE_LATEST_API` / `UPDATE_COMMITS_API` / `UPDATE_COMPARE_API` | 空 | 自建更新服务器的三个接口模板；配了就完全不走 GitHub |
| `UPDATE_TOKEN` | 空 | 私有仓库 / 提高 API 限额用的 token |
| `UPDATE_SOURCE_DIR` | 自动探测仓库根 | **待替换的源码目录**（容器内路径），探测失败必须显式设置 |
| `UPDATE_DIR` | `<UPLOAD_DIR 上级>/update` | 更新工作目录：源码包、备份、状态、待更新清单 |
| `UPDATE_DEPLOYED_FILE` | 自动探测 | 部署记录文件路径（默认 `data/deployed-commit.json`） |
| `UPDATE_REMOTE` | `origin` | 写入待更新清单的 git remote 名（宿主代理回退用） |
| `UPDATE_WAITING_AGENT` | `true` | `true`=替换源码后等宿主代理重建；`false`=后端自己 `docker compose` 或本机编译 |
| `INKSTONE_SERVICE` | 空 | 本机重建 / 宿主代理要重启的 systemd 单元名（如 `inkstone`） |
| `INKSTONE_SOURCE_DIR` | 脚本所在仓库根 | 宿主更新代理用的源码目录 |
| `INKSTONE_UPDATE_DIR` | `<源码>/data/update` | 宿主更新代理读取待更新清单的目录 |
| `INKSTONE_DEPLOY_TYPE` | `auto` | 宿主代理部署形态：`auto` / `docker` / `binary` |
| `INKSTONE_BUILD_TIMEOUT` | `2700` | 宿主侧构建超时（秒） |

---

## Dockerfile 关键点（国内网络环境）

```dockerfile
# 后端：必须用国内 Go 代理，否则 go mod download 超时
FROM docker.m.daocloud.io/library/golang:1.27-alpine AS builder
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=off
```

```dockerfile
# 前端：必须用国内 npm 源
FROM docker.m.daocloud.io/library/node:22-alpine AS builder
RUN npm config set registry https://registry.npmmirror.com
```

**基础镜像全部走 `docker.m.daocloud.io` 镜像源**（Docker Hub 在国内被墙）。

---

## docker-compose 编排

```yaml
services:
  postgres:
    image: docker.m.daocloud.io/library/postgres:16-alpine
    environment:
      POSTGRES_PASSWORD: ${DB_PASSWORD:?请在 .env 设置}
    volumes: [postgres_data:/var/lib/postgresql/data]
    healthcheck: pg_isready

  backend:
    build: ./backend            # 或 image: inkstone-backend:latest
    depends_on: { postgres: { condition: service_healthy } }
    environment:
      DB_HOST: postgres
      JWT_SECRET: ${JWT_SECRET:?请在 .env 设置}
    volumes:
      - uploads_data:/app/data
    ports: ["${BACKEND_BIND:-127.0.0.1}:8080:8080"]

  frontend:
    image: inkstone-frontend:latest
    ports: ["${FRONTEND_BIND:-127.0.0.1}:3000:3000"]
```

**数据卷**：`postgres_data`（数据库）、`uploads_data`（上传文件）。

---

## 在线更新系统（Beta1.27）

后台「后台管理 → 系统更新」（`/admin/system-update`）可以一键把站点升级到上游最新提交。
链路：**检查上游提交 → 下载源码镜像包 → SHA256 校验 → 安全解压 → 逐文件备份 → 原子替换源码 → 触发重建**。

### 为什么需要宿主更新代理

后端跑在容器里：它能下载源码包、校验、替换源码，但**没有 docker 权限，也没有 systemd**，
没法把新代码真正跑起来。最后一步「重建 + 重启」交给宿主机的代理脚本：

```
后台点「立即更新」
  → 后端下载源码包（UPDATE_MIRROR）→ 校验 → 解压到 /app/data/update/staging/<commit>
  → 备份被覆盖的文件 → 写入宿主的源码目录（/app/src → 宿主 ./）
  → 写 /app/data/update/pending-update.json
    （该目录被绑定挂载到宿主 ./data/update，所以宿主代理读得到）
宿主更新代理（默认每 60 秒轮询）
  → 读 pending-update.json → docker compose build && up -d
  → 写回 update-result.json + deployed-commit.json
  → 后台页面显示「更新完成」
```

### 配置（源码构建部署）

`docker-compose.prod.yml` 已内置两处挂载，**两者缺一不可**：

| 容器内路径 | 宿主路径 | 作用 |
|---|---|---|
| `/app/src` | `./`（仓库根） | 更新时替换源码文件 |
| `/app/data/update` | `./data/update` | 宿主代理读取待更新清单与写回结果 |

> 更新目录必须是**绑定挂载**：如果它落在命名卷（`uploads_data`）里，宿主拿不到稳定路径，
> 代理就永远读不到清单，页面会一直停在「等待宿主代理」。这也是把 `UPDATE_DIR`
> 默认值设为 `/app/data/update` 并单独绑定挂载的原因。

部署步骤：

```bash
# 1) 确认后端容器能看到宿主源码树
docker compose --env-file .env -f docker-compose.prod.yml exec backend ls /app/src
# 2) 确认更新目录确实是绑定挂载（Source 应是 /opt/inkstone/data/update，不是 /var/lib/docker/volumes/...）
docker inspect blog-backend --format '{{range .Mounts}}{{.Source}} -> {{.Destination}}{{"\n"}}{{end}}'
# 3) 单次验证宿主代理
sudo INKSTONE_SOURCE_DIR=/opt/inkstone INKSTONE_UPDATE_DIR=/opt/inkstone/data/update \
  ./deploy/update-agent.sh --once
```

### 常驻代理（systemd，推荐）

```ini
# /etc/systemd/system/inkstone-update-agent.service
[Unit]
Description=InkStone host update agent
After=docker.service network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/inkstone
Environment=INKSTONE_SOURCE_DIR=/opt/inkstone
Environment=INKSTONE_UPDATE_DIR=/opt/inkstone/data/update
Environment=INKSTONE_DEPLOY_TYPE=docker
Environment=INKSTONE_COMPOSE_FILE=docker-compose.prod.yml
ExecStart=/opt/inkstone/deploy/update-agent.sh
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now inkstone-update-agent
journalctl -u inkstone-update-agent -f      # 观察更新过程
```

> 用绑定挂载后**不需要**再去猜数据卷路径：更新目录固定就是 `<仓库根>/data/update`。
> Windows 宿主用 `deploy\update-agent.ps1`（可挂到计划任务）。

### 二进制部署（无 docker）

后端会自动调用 `deploy/scripts/rebuild.sh`（Windows 为 `rebuild.ps1`）：先等旧进程退出，
再 `go build` + `npm run build`，然后 `systemctl restart $INKSTONE_SERVICE`
或直接拉起 `backend/server`。此时应设 `UPDATE_WAITING_AGENT=false`，并给后端配好 `INKSTONE_SERVICE`。

### 镜像包更新（UPDATE_SOURCE=releases，与上游只发 Release 的发布方式对齐）

适用场景：上游在 **GitHub Releases** 发版（tag 用语义化版本号 `vX.Y.Z`，资产里放
`inkstone-images-<tag>.tar` 镜像包），站点用 docker 部署。这种模式下**没有源码包可换**，
安装动作是 `docker load` + `docker compose up -d`：

```
后台点「立即更新」（目标 = Release tag，如 v1.28.0）
  → 后端 GET /repos/{owner}/{name}/releases/latest（或 /releases/tags/{tag}）
  → 版本比较：本地 deployed-version.json（缺失回落 AppVersion，兼容 Beta1.27 写法）
  → 从 assets 里按 UPDATE_IMAGE_ASSET 正则挑镜像包，下载到 UPDATE_DIR/images/
     （流式 + SHA-256；size 与 Release 资产声明不一致即失败）
  → 写 pending-update.json（kind=release_image，带 image_path/sha256/compose_file）
宿主更新代理（update-agent.sh / .ps1 均已支持 release_image 分支）
  → 校验 SHA-256 → docker load -i <镜像包> → docker compose up -d
  → 写回 deployed-version.json + release-history.json + update-result.json
```

要点：

- **离线镜像部署天然适配**：不需要源码目录、不需要 build，`docker-compose.offline.yml` 即可；
  把 `UPDATE_SOURCE=releases` 加进 `.env`，并保持 `UPDATE_WAITING_AGENT=true`（默认）；
- `UPDATE_COMPOSE_FILE` 默认自动探测 `docker-compose.offline.yml` → `prod` → `.yml`，
  也可显式指定（相对 `UPDATE_SOURCE_DIR` 的路径，会写进清单由宿主代理使用）；
- 版本比较见 `internal/service/update_release.go` 的 `parseVersionValue`：
  `Beta1.27` ≡ `v1.27.0`，预发布（`-rc1`）低于正式版；本地版本读取顺序
  `UPDATE_VERSION_FILE` → `UPDATE_DIR/deployed-version.json` → `data/` → `AppVersion`；
- **回滚点是安装历史**（`UPDATE_DIR/release-history.json`，保留最近 10 个版本的本地镜像包），
  不是源码备份：回滚 = 重新 load 旧镜像包 + compose up -d；
- 镜像包大小上限 `UPDATE_IMAGE_MAX_MB`（默认 2048），超限直接拒绝下载。

### 打包镜像包并发 Release（deploy/package-images.ps1 / .sh）

上游发版 = 一个 Release（tag 版本号）+ 资产 `inkstone-images-<版本>.tar`：

```powershell
# Windows（Docker Desktop 需先启动）
.\deploy\package-images.ps1 -Version v1.28.0
# 前端 API 地址按部署域名传（默认 https://blog.shenv.top/api/v1，构建期注入）
$env:INKSTONE_PUBLIC_API_URL='https://blog.shenv.top/api/v1'; .\deploy\package-images.ps1 -Version v1.28.0
```

```bash
# Linux / macOS
./deploy/package-images.sh v1.28.0
```

产物在 `dist/`：`inkstone-images-<版本>.tar`（含 `inkstone-backend:<版本>` / `latest`、
`inkstone-frontend:<版本>` / `latest` 四个 tag——**保留旧版本 tag 才能回滚**，别只打 latest）
和 `inkstone-images-<版本>.tar.sha256`。上传到 GitHub Releases：

```bash
gh release create v1.28.0 dist/inkstone-images-v1.28.0.tar \
  --title 'InkStone v1.28.0' --notes-file RELEASE_NOTES.md
# 已存在的 Release 补资产：
gh release upload v1.28.0 dist/inkstone-images-v1.28.0.tar --clobber
```

发布后后台「系统更新 → 检查更新」即可在「发布说明 + 资产」里看到该版本并一键安装。

### 部署到服务器（deploy/publish-server.ps1，方式一）

打包完成后一条命令部署（在仓库根目录、**自己的终端**执行；ssh/scp 的密码提示
只出现在该终端，脚本不保存任何凭据）：

```powershell
.\deploy\publish-server.ps1 -Server <服务器IP或域名> -User root
```

执行顺序：scp 传输镜像包 → 远程预检（docker / compose 文件 / .env，并记录升级前镜像快照到
`/opt/backup_blog_pre-<版本>-images.txt`）→ `pg_dump` 备份数据库到
`/opt/backup_blog_pre-<版本>.sql` → `docker load` → `docker compose --env-file .env -f
docker-compose.offline.yml up -d` → 容器健康检查 + 站点版本核对。

失败回滚：`docker tag <旧镜像ID> inkstone-backend:latest`（frontend 同理）后重跑
`compose up -d`；旧镜像 ID 就在上面的 images.txt 里。

常用参数：`-RemoteDir /opt/inkstone`（部署目录）、`-Port 22`、`-SiteUrl https://blog.shenv.top`、
`-ComposeFile docker-compose.offline.yml`、`-UseSudo`（非 root 用户）、`-SkipBackup`（跳过备份，不建议）。

### 更新行为约束（改动时别破坏）

- 只从 `UPDATE_MIRROR` / 上游仓库取包，**不接受请求体指定下载地址**；
- 解压拒绝绝对路径、`..` 穿越与符号链接，限制文件数（2 万）与解压体积（512 MB，按**实际写入**累计）；
- 替换源码时**永不触碰** `data/`、`uploads/`、`files/`、`.env*`、`node_modules/`、`.git/`、`.update/`、`.next/`、`.tools/`。
  判定分两层：`uploads/`、`files/` 等只按**顶层前缀**保护（因为 `frontend/app/admin/files/page.tsx` 是真实源码目录），
  `data`、`node_modules`、`.git`、`.update`、`.next`、`.tools` 以及任意层级的 `.env*` 按**路径段**保护；
- 覆盖前逐文件备份到 `UPDATE_DIR/backups/<提交>-<时间戳>/`（含 `manifest.json`），替换失败自动回滚；
- 每次更新都会在「系统更新 → 回滚」里留下备份点，可一键还原：清单里记了「哪些文件更新前已存在」，
  所以回滚会**同时删除本次新增的文件**，不会留下新旧混合的代码树（还原后仍需重建才生效）；
- 检查 / 更新 / 回滚全部写入操作日志的 `system` 分类。

### 一个必须知道的副作用：文件属主

后端容器以 **root** 运行，它写进 `./:/app/src` 的文件在宿主机上属主是 root。
如果之后在服务器上以普通用户执行 `git pull` / `git status`，会因属主不一致报
`dubious ownership` 或 Permission denied。两种处理方式：

```bash
# 方式一（推荐）：把仓库目录加进 git 安全目录，并用 sudo 操作
git config --global --add safe.directory /opt/inkstone
sudo git -C /opt/inkstone status

# 方式二：更新后把属主改回部署用户
sudo chown -R deploy:deploy /opt/inkstone
```

> 因此**不建议**把 `/app/src` 当日常开发目录用：它只承担「更新时被写入」的职责，
> 版本管理仍以 `git` 为准（更新过程本身不调用 git）。

### 更新排障

| 现象 | 原因 | 解决 |
|---|---|---|
| 页面提示「未能探测到源码目录」 | 容器内找不到仓库根 | 显式设置 `UPDATE_SOURCE_DIR`（如 `/app/src`）并挂载源码目录（releases 模式不依赖源码目录） |
| 检查更新报「更新源不可用」 | 服务器连不上 `api.github.com` | 保留 `UPDATE_MIRROR` 代理地址；或配 `UPDATE_LATEST_API` 指向自建/镜像接口 |
| releases 模式提示「没有找到镜像包资产」 | Release 资产名与 `UPDATE_IMAGE_ASSET` 不匹配 | 按发布实际命名调整正则（默认 `inkstone-images-.*\.tar$`） |
| 镜像包更新卡在「等待宿主代理」 | 代理没运行 / 读不到清单 | `systemctl status inkstone-update-agent`；核对 `INKSTONE_UPDATE_DIR` 是否指向数据卷里的 `update` 目录 |
| 代理日志报 `docker load` 失败 | 镜像包损坏 / 磁盘满 | 重新触发更新；核对 SHA-256 对账信息与 `df -h` |
| 更新后版本号没变 | 后端没重启（还是旧进程） | `docker compose restart backend`；用 `GET /api/v1/system/info` 看 `commit` 是否变化 |
| 想撤销这次更新 | — | 后台「系统更新 → 回滚」选备份点，再重新构建重启 |

---

## 常用运维命令

```bash
docker compose ps                      # 状态
docker compose logs -f backend         # 后端日志
docker compose logs --tail=50 backend  # 最后 50 行
docker compose restart backend         # 重启后端
docker compose down                    # 停止（数据保留）
docker compose up -d                   # 启动
docker compose up -d --build           # 重新构建启动
```

## 数据备份与恢复

```bash
# 备份数据库
docker exec blog-postgres pg_dump -U blog blog_platform > backup_$(date +%F).sql

# 备份上传文件
docker run --rm -v inkstone_uploads_data:/data -v $(pwd):/backup alpine \
  tar czf /backup/uploads_$(date +%F).tar.gz /data

# 恢复数据库
cat backup.sql | docker exec -i blog-postgres psql -U blog blog_platform
```

---

## 证书申请（acme.sh，无需 certbot）

```bash
curl https://get.acme.sh | sh -s email=your@email.com
~/.acme.sh/acme.sh --set-default-ca --server letsencrypt
mkdir -p /etc/nginx/ssl
~/.acme.sh/acme.sh --issue -d blog.shenv.top --nginx
~/.acme.sh/acme.sh --install-cert -d blog.shenv.top \
  --key-file /etc/nginx/ssl/blog.shenv.top.key \
  --fullchain-file /etc/nginx/ssl/blog.shenv.top.crt \
  --reloadcmd "systemctl reload nginx"
```

> **Alinux 注意事项**：官方 `get.docker.com` 脚本不支持 Alinux，需用 `dnf` 从阿里云源装 docker-ce（`sed -i 's/\$releasever/7/g' /etc/yum.repos.d/docker-ce.repo` 绕过版本号问题）。

---

## 首次部署初始化

1. 浏览器打开域名
2. 点「注册」→ **第一个注册的账号自动成为管理员**
3. 头像菜单 →「后台管理」
4. 后台 → 网站管理：配置站点名称、Logo、SMTP
5. 后台 → 安全防护：按需开启验证码

## 排障速查

| 现象 | 原因 | 解决 |
|---|---|---|
| `failed to connect database` | Postgres 容器没起 | `docker compose up -d postgres` |
| 页面能开但接口 404 | Nginx 没代理 `/api` | 检查反代配置 |
| 图片 404 | `/uploads` `/files` 没代理到 8080 | 补反代规则 |
| 登录报「操作过于频繁」 | 触发限流 | 等 15 分钟或调大 `security_login_max` |
| 极验提示未配置 | `geetest_captcha_id` 未传到前端 | 检查 `site-config-context.tsx` 解构 |
| 前端改了 API 地址不生效 | 构建期注入 | 必须重新 build 前端镜像 |
| `go mod download` 超时 | 缺 Go 代理 | Dockerfile 加 `GOPROXY=https://goproxy.cn,direct` |

## 项目关键路径

| 内容 | 路径 |
|---|---|
| 发布副本（含部署文件） | `D:\blog-platform-release\blog-platform\` |
| 离线部署配置包 | `D:\blog-platform-release\inkstone-offline-deploy.zip` |
| Docker 镜像包 | `D:\blog-platform-release\inkstone-images.tar` |
| 源码压缩包 | `D:\blog-platform-release\inkstone-latest.zip` |
| GitHub 仓库 | `https://github.com/shenwei234/inkstone` |
| 生产站点 | `https://blog.shenv.top` |
