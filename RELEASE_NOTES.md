# InkStone Beta1.27 更新包

版本号：Beta1.27
生成时间：2026-10-04
镜像包：inkstone-images-Beta1.27.tar（262.1 MB / 274869248 字节）
镜像包 SHA256：e31156a5116d6cde5f109821c7720462b4ef0ccae6e7fd361abdaa37ae7a0baf
包含镜像：inkstone-backend:latest（sha256:fce28338315284a2eacecf4f60ab8ba1cb713bd721f1b9630c4b83db23db09b4）、inkstone-frontend:latest（sha256:f1de71d5193d8ee2575baa7f7b5a038ccf40cbd8c2e8d60f969e8d678ca06766）

## 更新内容

### 在线更新（Beta1.27 主打能力）

- 后台新增「系统更新」页：检查上游 → 下载 → SHA256 校验 → 安全解压/备份 → 原子替换 → 重建 → 重启，全过程可视化进度，检查/更新/回滚均写入操作日志。
- 新增宿主更新代理 `deploy/update-agent.sh` / `deploy/update-agent.ps1`（常驻轮询待更新清单），容器内后端只负责下载与校验，「重建 + 重启」交给宿主执行，更新目录需绑定挂载（`./data/update`）。
- 更新永不触碰站点数据与密钥：`data/`、`uploads/`、`files/`、任意层级 `.env*`、`node_modules/`、`.git/`、`.next/` 等受保护路径替换时自动跳过；替换失败自动回滚，回滚点保留在页面上可一键还原。
- 后台侧边栏补齐：站点地图、安全防护、网站管理、网站日志、关于系统等管理页。

### 更新源支持 GitHub Releases（本次新增）

- 新增 `UPDATE_SOURCE=releases` 模式（默认仍为 `commits`，行为不变）：更新信息改为读取 GitHub Releases——`tag_name` 即版本号、`body` 即发布说明、`assets` 即镜像包资产，不再依赖提交记录与源码包。
- 版本比较语义化：`v1.28.0` / `Beta1.27` / `1.28` / `v1.28.0-rc1` 均可解析，预发布低于正式版；本地版本读部署版本记录，缺失时回落内置版本号。
- 镜像包资产按 `UPDATE_IMAGE_ASSET`（默认 `inkstone-images-.*\.tar$`）挑选，下载带进度与 `UPDATE_IMAGE_MAX_MB`（默认 2048 MB）上限，大小与 Release 资产声明对账、SHA-256 留档。
- 安装动作 = `docker load` + `docker compose up -d`：由宿主更新代理执行（含容器健康检查等待），无需源码目录、无需重新构建，离线镜像部署同样适用。
- 系统更新页改版：版本号维度对比、发布说明与资产列表展示、镜像包待安装进度卡片、可按历史版本回滚（重新加载旧镜像包）。
- 后台接口 `apply` 新增可选 `target` 字段（版本 tag），回滚参数改为安装历史版本号。

### 打包与工程化

- 新增镜像包打包脚本 `deploy/package-images.ps1`（Windows）/ `deploy/package-images.sh`（Linux/macOS）：一条命令构建 `inkstone-backend` / `inkstone-frontend` 的 `<版本>` 与 `latest` 两组 tag，并 `docker save` 成 `dist/inkstone-images-<版本>.tar`（附 `.sha256`），即本 Release 资产的来源。
- 修复 Windows 原生部署启动失败：后端 DSN 需要 IANA 时区 `Asia/Shanghai`，原生运行时找不到时区库会直接 `unknown time zone` 崩溃；构建脚本统一增加 `-tags timetzdata` 内嵌时区库。
- 文档同步：`.ai-skill` 的 SKILL / api / backend / deployment 参考、`.env.example`、docker-compose（offline/prod）均已补充 releases 模式配置项。

## 部署方式（任选）

方式一：scp + docker load

```bash
scp D:\images\inkstone-images-Beta1.27.tar root@<服务器IP>:/opt/
ssh root@<服务器IP> "docker load -i /opt/inkstone-images-Beta1.27.tar && cd /opt/inkstone-deploy && docker compose -f docker-compose.offline.yml up -d"
```

方式二：GitHub Release + 更新推送后台（全自动）

上传本镜像包为 Release 资产（tag 填版本号 Beta1.27），各实例将在下一个检查周期自动完成 下载 → 校验 → `docker load` + 容器重建，失败自动回滚。

## 回滚

- 自动：安装阶段镜像校验 / 容器健康检查失败 → 代理自动回滚到更新前版本。
- 手动：任一站后台「系统更新 → 更新历史 → 回滚到上一版本」，无需登录服务器。

部署前建议先备份数据库：

```bash
docker exec inkstone-postgres pg_dump -U blog blog_platform > /opt/backup_blog_pre-Beta1.27.sql
```

备注：nginx 配置需与仓库 `deploy/nginx/inkstone.conf` 同步（sitemap/robots/feed 三个精确匹配 location），否则 SEO 端点仍可能 404。
