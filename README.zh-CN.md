# InkStone 砚石

[English](./README.md) | **简体中文**

**一个完全属于你自己的多用户博客平台。**

InkStone（砚石）把整套博客系统装进一次部署：后端 **Go + Gin + PostgreSQL**，前端 **Next.js + React**，外加一个功能完整的管理后台。Markdown 与富文本随写随切，站点外观、用户评论、版本升级都在后台点几下完成，全程不必改一行代码。

无论个人专栏还是多人协作的站点，InkStone 都想成为那种「五年后你还维护得动」的博客：没有插件泥潭，没有月费，也不会给你意外。

✍️ 双编辑器 · 🛡️ 三种人机验证 · 🔄 后台一键更新 · 🌏 中英双语文档

**Beta1.27** · [MIT 许可证](./LICENSE) · [github.com/shenwei322/inkstone](https://github.com/shenwei322/inkstone)

> **语言说明**：本文档为简体中文版，英文版见 [README.md](./README.md)。系统界面（访客站与后台）以及 `docs/`、`.ai-skill/` 文档目前均为简体中文——项目按中文优先设计，**尚未实现界面多语言（i18n）**。

## 特性

### 内容创作

- **双编辑器**：Markdown 编辑器与 Tiptap 富文本编辑器，写作过程中随时切换，互不丢格式
- **文章与页面**：草稿/发布状态管理、分类与标签、独立页面（关于、说明页等）
- **附件管理**：图片与文件统一入库，后台集中管理引用关系
- **评论与互动**：评论与嵌套回复、点赞与收藏，后台可集中管理

### 阅读体验

- 文章目录（TOC）自动生成、代码块语法高亮、回到顶部、一键分享
- 深色/浅色主题切换，支持自定义壁纸、站点图标与侧边栏小工具（含自定义 HTML）
- 基于 GSAP 的页面过渡与入场动效，移动端自适应

### 用户与安全

- 多用户体系：注册、登录、个人中心；角色分为 `admin` / `user`，中间件层做 RBAC 鉴权；可开启注册/登录邮箱验证码
- **JWT 双令牌**：access 15 分钟 / refresh 7 天；改密码、封禁、改角色会递增 `TokenVersion`，旧令牌立即失效
- **人机验证三选一**（后台一键切换）：
  - `lap`（默认）：同源代理 Cap 的 CF Workers 分支，浏览器零接触 workers.dev，支持 DNS 固定 IP 与 TUN 代理两级兜底
  - `pow`：自研工作量证明 v2，消耗访客本地资源（内存表 + 多轮哈希 + 真人交互信号），**服务器不通外网、不能用代理时依然可用**
  - `geetest`：极验 v4 行为验证
- 登录/注册限流、CORS 白名单、可信反向代理配置（防 `X-Forwarded-For` 伪造绕过限流）
- 正文 HTML 经 bluemonday 白名单消毒、侧边栏自定义 HTML 写入前消毒、友链健康检测带 SSRF 防护

### 运维与在线更新

- **后台一键在线更新**：检查上游 → 下载 → SHA256 校验 → 备份 → 原子替换 → 重建 → 重启，全过程可视化进度
- **两种更新源**：`commits`（对比提交 + 源码包）与 `releases`（GitHub Releases 的版本号 + 发布说明 + 镜像包资产）
- 镜像包模式安装 = `docker load` + `docker compose up -d`，**无需源码目录、无需重新构建**，离线部署同样适用
- 更新永不触碰站点数据：`data/`、`uploads/`、`files/`、任意层级 `.env*`、`node_modules/`、`.git/`、`.next/` 等路径自动跳过；替换失败自动回滚，回滚点可在页面一键还原
- 容器内没有 docker 权限也没关系：「重建 + 重启」交给宿主更新代理（`deploy/update-agent.sh` / `.ps1`）执行
- 操作日志、流量统计、维护模式开关、站点地图 / robots.txt / RSS（`sitemap.xml`、`feed.xml`）

### 管理后台

站点内置 `/admin` 后台，覆盖：仪表盘、文章、页面、分类标签、评论、友链、文件、用户、外观、安全防护、网站日志、站点地图、系统更新、关于系统。

## 技术栈

| 层次 | 技术选型 |
|---|---|
| 后端 | Go 1.27 · Gin 1.12 · GORM 1.31 · PostgreSQL 16（AutoMigrate 自动建表） |
| 认证 | golang-jwt v5 双令牌 + TokenVersion 代次撤销 |
| 前端 | Next.js 16.3（App Router）· React 19.2 · TypeScript 5 · Tailwind CSS v4 |
| 编辑器 | Tiptap 3（富文本）· marked + bluemonday（Markdown 渲染与消毒）· highlight.js |
| 数据与动效 | TanStack Query 5 · GSAP 3.15 · Recharts 3 · lucide-react |
| 部署 | Docker Compose（dev / offline / prod）· Nginx · 宿主更新代理 |

## 快速开始（本地开发）

```bash
git clone https://github.com/shenwei322/inkstone.git
cd inkstone

# 1. 启动数据库（PostgreSQL 16）
docker compose -f docker-compose.dev.yml up -d

# 2. 后端（默认 :8080，API 前缀 /api/v1）
cd backend && go run ./cmd/server

# 3. 前端访客站（默认 :3000）
cd frontend && npm install && npm run dev
```

几点提示：

- Go 依赖下载建议设置 `GOPROXY=https://goproxy.cn,direct`
- **Windows 上编译必须带 `-tags timetzdata`**，否则 DSN 里的 `TimeZone=Asia/Shanghai` 会报 `unknown time zone` 导致后端起不来
- 前端 `NEXT_PUBLIC_API_URL` 是**构建期注入**，改完要重新 build
- 后端启动报 `failed to connect database`，通常是 PostgreSQL 容器没在运行

代码改动后的验证清单：

```bash
# 后端
cd backend && gofmt -w . && go vet ./... && go build -tags timetzdata -o server.exe ./cmd/server

# 前端
cd frontend && npm run build && npx eslint app components lib --ext .ts,.tsx
```

## 部署

复制 `.env.example` 为 `.env` 并填写域名、数据库密码与 `JWT_SECRET`，然后按场景任选：

**方式一：源码构建（有服务器与源码）**

```bash
docker compose --env-file .env -f docker-compose.prod.yml up -d --build
```

**方式二：离线镜像包（推荐，无需构建）**

```bash
scp dist/inkstone-images-<版本>.tar root@<服务器IP>:/opt/
ssh root@<服务器IP> "docker load -i /opt/inkstone-images-<版本>.tar \
  && cd /opt/inkstone-deploy && docker compose -f docker-compose.offline.yml up -d"
```

镜像包由 `deploy/package-images.sh`（Linux/macOS）或 `deploy/package-images.ps1`（Windows）生成，同时产出 `.sha256` 校验文件。

**方式三：后台在线更新（部署后长期使用）**

在服务器上常驻更新代理，之后所有版本升级都在后台「系统更新」页完成：

```bash
# 更新目录必须是绑定挂载，宿主代理才拿得到待更新清单
INKSTONE_UPDATE_DIR=/opt/inkstone-deploy/data/update ./deploy/update-agent.sh
```

生产环境还需同步 `deploy/nginx/inkstone.conf`（其中 `sitemap.xml`、`robots.txt`、`feed.xml` 三个精确匹配 location 缺失会导致 SEO 端点 404）。

## 目录结构

```
backend/                 Go 后端
  cmd/server/main.go     入口：依赖注入 + 路由注册
  internal/handler/      HTTP 层（参数绑定、响应、错误映射）
  internal/service/      业务逻辑（含 update_*.go 在线更新）
  internal/repository/   GORM 数据访问
  internal/middleware/   鉴权 / CORS / 限流 / 安全头 / 流量统计
  internal/model/        数据模型
  pkg/config·mailer/     配置与 SMTP
frontend/                Next.js 访客站 + 站内后台
  app/                   路由页面，app/admin/ 为管理后台
  components/            可复用组件（编辑器、验证码、动效封装等）
  lib/api.ts             唯一的 API 客户端入口
deploy/                  部署资源
  nginx/inkstone.conf    生产 Nginx 配置
  update-agent.sh/.ps1   宿主更新代理
  package-images.sh/.ps1 镜像包打包脚本
  scripts/rebuild.sh/.ps1 本机部署重建脚本
docs/                    PoW 验证码拦截与优化报告
scripts/dev-start.ps1    本地一键启动
.ai-skill/               项目知识库（给 AI 模型读的开发文档）
```

## 文档

面向开发者与 AI 助手的技术文档在 [`.ai-skill/`](./.ai-skill/README.md)：关键事实与开发铁律（`SKILL.md`）、完整 API 清单、数据模型、设置项、前端组件与部署排障。改动代码后请同步更新对应文档。

## 提交规范

- 仓库地址：https://github.com/shenwei322/inkstone
- **提交说明必须使用中文**

## 许可证

[MIT](./LICENSE) © 2026 shenwei (shenwei322)
