# AI Skill 文档索引

本目录是给 AI 模型阅读的项目知识库，用于快速理解 InkStone 博客平台。

## 阅读顺序建议

| 顺序 | 文件 | 内容 |
|---|---|---|
| 1️⃣ | [`SKILL.md`](./SKILL.md) | **先读这个** — 关键事实、分层约定、开发铁律、常见坑 |
| 2️⃣ | [`references/backend.md`](./references/backend.md) | 后端分层、中间件、各 Service 方法详解 |
| 3️⃣ | [`references/frontend.md`](./references/frontend.md) | 前端组件、页面、状态管理、样式规范 |
| 4️⃣ | [`references/api.md`](./references/api.md) | **完整 API 清单**（所有接口 + 参数 + 鉴权） |
| 5️⃣ | [`references/data-models.md`](./references/data-models.md) | 数据表结构、关系、字段说明 |
| 6️⃣ | [`references/settings.md`](./references/settings.md) | 全部设置项 + 新增设置项的流程 |
| 7️⃣ | [`references/deployment.md`](./references/deployment.md) | Docker 部署、Nginx、证书、排障 |

## 按任务查文档

| 我要… | 看这个 |
|---|---|
| 新增一个 API | `SKILL.md` 开发铁律 §4 + `api.md` + `backend.md` |
| 新增一个页面 | `frontend.md`「常见修改场景」 |
| 新增一个设置项 | `settings.md`「新增设置项的完整流程」 |
| 修改数据表 | `data-models.md`（注意加 AutoMigrate） |
| 排查部署问题 | `deployment.md`「排障速查」 |
| 理解验证码机制 | `backend.md` 的 CaptchaService + `frontend.md` 的 Captcha 组件 |
| 理解权限系统 | `SKILL.md` 关键事实 + `backend.md` 中间件详解 |

## 交流语言规范（DSH skill）

本仓库要求 AI **思考和回复全部用简体中文**。该规则由 DSH 项目级 skill 强制：

- 路径：`.dsh/skills/inkstone-chinese-only/SKILL.md`
- 机制：DSH 的 `dsh-skill-filesystem` 提供方会自动扫描 `<项目根>/.dsh/skills/`（rank 100），**无需注册、无需重启**，改动即时生效
- 生效方式：目录 bundle（`权限/<name>/SKILL.md`）或平铺文件（`<name>.md`），frontmatter 必填 kebab-case 的 `name` 与 `description`
- 注意：只发现**一层**目录，嵌套的 `**/SKILL.md` 不会被识别；`name` 非 kebab-case 会被静默丢弃

改动交流语言规范时，只需编辑该 skill 文件，不必改本文档。

## 项目速览

- **定位**：多用户博客平台（WordPress 替代品，中文优先）
- **后端**：Go 1.27 + Gin + GORM + PostgreSQL 16
- **前端**：Next.js + React 19 + TypeScript + Tailwind v4
- **核心功能**：文章/页面管理、富文本编辑器、评论互动、分类标签、友链检测、文件管理、外观自定义、验证码、流量统计
- **仓库**：https://github.com/shenwei234/inkstone

## 维护说明

代码变更后请同步更新对应文档：
- 新增 API → 更新 `api.md`
- 新增设置项 → 更新 `settings.md` 的表格
- 新增模型 → 更新 `data-models.md`
- 新增组件 → 更新 `frontend.md`
- 部署方式变化 → 更新 `deployment.md`
