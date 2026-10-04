# InkStone

**English** | [简体中文](./README.zh-CN.md)

**A self-hosted, multi-user blogging platform you own end to end.**

InkStone (砚石, "inkstone") packs an entire blog system into one deployable stack —
**Go + Gin + PostgreSQL** on the backend, **Next.js + React** on the frontend, plus a
complete admin dashboard. Write in Markdown or rich text and switch between the two at
will, restyle the site, manage users and comments, and upgrade versions online — all
without ever touching the code.

Personal column or multi-author publication, InkStone is built to be the blog you can
still maintain five years from now: no plugin sprawl, no monthly fees, no surprises.

✍️ Two editors · 🛡️ Three CAPTCHA providers · 🔄 One-click online updates · 🌏 Bilingual docs

**Beta1.27** · [MIT License](./LICENSE) · [github.com/shenwei322/inkstone](https://github.com/shenwei322/inkstone)

> **Language note**: the visitor site, the admin dashboard, and the `docs/` + `.ai-skill/` documentation currently ship with Simplified Chinese copy only — the project is Chinese-first by design. Application-level i18n (including an English UI) is **not implemented yet**; this README is bilingual, the product itself is not.

## Features

### Content authoring

- **Two editors, one article**: a Markdown editor and a Tiptap rich-text editor — switch between them mid-draft without losing formatting
- **Articles & pages**: draft/published workflow, categories and tags, standalone pages (about, custom landing pages)
- **Media management**: images and files stored with tracked references, managed from a single admin screen
- **Comments & engagement**: comments with nested replies, likes and favorites, all manageable from the dashboard

### Reading experience

- Automatic table of contents, syntax highlighting, back-to-top, and one-click sharing
- Dark/light theme toggle, custom wallpaper, custom site icon, and configurable sidebar widgets (including sanitized custom HTML)
- GSAP-powered page transitions and entrance animations; fully responsive on mobile

### Users & security

- Multi-user system: registration, login, and a personal center; `admin` / `user` roles with RBAC enforced in middleware; optional email verification codes for registration and login
- **Dual-token JWT**: 15-minute access token, 7-day refresh token; changing a password, banning a user, or changing a role increments `TokenVersion`, invalidating all previously issued tokens immediately
- **Human verification with three interchangeable providers** (switchable in the dashboard):
  - `lap` (default): first-party proxy to Cap's Cloudflare Workers deployment — the browser never contacts `workers.dev` directly, with DNS-pinned-IP and TUN-proxy fallbacks
  - `pow`: a self-contained proof-of-work v2 scheme that spends the visitor's local CPU (memory-hard table, multi-round hashing, real-interaction signals) — **works even when the server has no outbound internet access and cannot use a proxy**
  - `geetest`: Geetest v4 behavioral verification
- Rate limiting on login/registration, CORS allowlist, and a trusted-reverse-proxy setting that prevents spoofed `X-Forwarded-For` headers from bypassing rate limits
- Defense in depth: article HTML sanitized through a bluemonday allowlist, sidebar custom HTML sanitized on write, and SSRF-guarded link health checks

### Operations & online updates

- **One-click updates from the dashboard**: check upstream → download → SHA256 verify → backup → atomic swap → rebuild → restart, with live progress reporting
- **Two update sources**: `commits` (compare commits, download a source archive) and `releases` (GitHub Releases — version tags, release notes, and image-package assets)
- Image-package installs run `docker load` + `docker compose up -d`: **no source tree required, no rebuild needed**, and fully offline-capable
- Updates never touch your data: `data/`, `uploads/`, `files/`, any-level `.env*`, `node_modules/`, `.git/`, `.next/` and similar paths are skipped automatically; a failed swap rolls back on its own, and rollback points stay restorable from the dashboard
- No Docker access inside the container? The "rebuild + restart" step is delegated to a host-side update agent (`deploy/update-agent.sh` / `.ps1`)
- Audit logs, traffic analytics, maintenance mode, sitemap / robots.txt / RSS (`sitemap.xml`, `feed.xml`)

### Admin dashboard

A built-in `/admin` dashboard covers: overview, articles, pages, categories and tags, comments, friend links, files, users, appearance, security, site logs, sitemap, system update, and about.

## Tech stack

| Layer | Stack |
|---|---|
| Backend | Go 1.27 · Gin 1.12 · GORM 1.31 · PostgreSQL 16 (schema via AutoMigrate) |
| Auth | golang-jwt v5 dual tokens + `TokenVersion` generation-based revocation |
| Frontend | Next.js 16.3 (App Router) · React 19.2 · TypeScript 5 · Tailwind CSS v4 |
| Editors | Tiptap 3 (rich text) · marked + bluemonday (Markdown rendering and sanitizing) · highlight.js |
| Data & motion | TanStack Query 5 · GSAP 3.15 · Recharts 3 · lucide-react |
| Deployment | Docker Compose (dev / offline / prod) · Nginx · host-side update agent |

## Quick start (local development)

```bash
git clone https://github.com/shenwei322/inkstone.git
cd inkstone

# 1. Start the database (PostgreSQL 16)
docker compose -f docker-compose.dev.yml up -d

# 2. Backend (defaults to :8080, API prefix /api/v1)
cd backend && go run ./cmd/server

# 3. Frontend visitor site (defaults to :3000)
cd frontend && npm install && npm run dev
```

A few notes:

- For Go module downloads in mainland China, set `GOPROXY=https://goproxy.cn,direct`
- **On Windows you must build with `-tags timetzdata`**, otherwise `TimeZone=Asia/Shanghai` in the DSN fails with `unknown time zone` and the backend will not start
- `NEXT_PUBLIC_API_URL` on the frontend is **injected at build time** — rebuild after changing it
- If the backend exits with `failed to connect database`, the PostgreSQL container is usually not running

Verification checklist after code changes:

```bash
# Backend
cd backend && gofmt -w . && go vet ./... && go build -tags timetzdata -o server.exe ./cmd/server

# Frontend
cd frontend && npm run build && npx eslint app components lib --ext .ts,.tsx
```

## Deployment

Copy `.env.example` to `.env` and fill in your domain, database password, and `JWT_SECRET`, then pick one of the following:

**Option 1: Build from source (server with the source tree)**

```bash
docker compose --env-file .env -f docker-compose.prod.yml up -d --build
```

**Option 2: Offline image package (recommended, no build required)**

```bash
scp dist/inkstone-images-<version>.tar root@<server-ip>:/opt/
ssh root@<server-ip> "docker load -i /opt/inkstone-images-<version>.tar \
  && cd /opt/inkstone-deploy && docker compose -f docker-compose.offline.yml up -d"
```

Image packages are produced by `deploy/package-images.sh` (Linux/macOS) or `deploy/package-images.ps1` (Windows), which also emit a `.sha256` checksum file.

**Option 3: Online updates from the dashboard (for long-running deployments)**

Run the update agent on the host; every later upgrade then happens from the dashboard's "System Update" page:

```bash
# The update directory must be a bind mount so the host agent can read the pending-update manifest
INKSTONE_UPDATE_DIR=/opt/inkstone-deploy/data/update ./deploy/update-agent.sh
```

For production you must also keep `deploy/nginx/inkstone.conf` in sync — the three exact-match locations for `sitemap.xml`, `robots.txt`, and `feed.xml` are required, or those SEO endpoints return 404.

## Project structure

```
backend/                 Go backend
  cmd/server/main.go     Entry point: dependency wiring + route registration
  internal/handler/      HTTP layer (binding, responses, error mapping)
  internal/service/      Business logic (update_*.go implements online updates)
  internal/repository/   GORM data access
  internal/middleware/   Auth / CORS / rate limiting / security headers / traffic stats
  internal/model/        Data models
  pkg/config·mailer/     Configuration and SMTP
frontend/                Next.js visitor site + built-in dashboard
  app/                   Route pages; app/admin/ is the dashboard
  components/            Reusable components (editors, captcha, motion wrappers, …)
  lib/api.ts             The single API client entry point
deploy/                  Deployment assets
  nginx/inkstone.conf    Production Nginx config
  update-agent.sh/.ps1   Host-side update agent
  package-images.sh/.ps1 Image-package build scripts
  scripts/rebuild.sh/.ps1 Rebuild script for native deployments
docs/                    PoW captcha interception and optimization reports
scripts/dev-start.ps1    One-command local startup
.ai-skill/               Project knowledge base (developer docs for AI assistants, in Chinese)
```

## Documentation

Developer and AI-assistant documentation lives in [`.ai-skill/`](./.ai-skill/README.md) (written in Simplified Chinese): key facts and development rules (`SKILL.md`), the complete API reference, data models, settings, frontend components, and deployment troubleshooting. Please update the relevant document alongside code changes.

## Contributing

- Repository: https://github.com/shenwei322/inkstone
- **Commit messages must be written in Simplified Chinese** (project convention)

## License

[MIT](./LICENSE) © 2026 shenwei (shenwei322)
