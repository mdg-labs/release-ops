# Release Ops

[![Dev CI](https://github.com/mdg-labs/release-ops/actions/workflows/dev.yml/badge.svg?branch=dev)](https://github.com/mdg-labs/release-ops/actions/workflows/dev.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Next.js](https://img.shields.io/badge/Next.js-15-000000?logo=next.js&logoColor=white)](https://nextjs.org/)
[![Docker](https://img.shields.io/badge/Docker-single%20image-2496ED?logo=docker&logoColor=white)](https://github.com/mdg-labs/release-ops/pkgs/container/release-ops)
[![GHCR](https://img.shields.io/badge/GHCR-ghcr.io%2Fmdg--labs%2Frelease--ops-333)](https://github.com/mdg-labs/release-ops/pkgs/container/release-ops)

Self-hosted release monitor with a web UI. Poll releases on **GitHub**, **GitLab**, **Gitea**, **Forgejo**, and **Codeberg**; create tickets in **Phasical**, **Jira**, or **Linear** when a real new release ships. One Docker container — Go API, polling, auth, and Next.js UI in a single image.

## Features

- **Release polling** — scheduled and manual runs; baseline on first sight (no ticket spam)
- **Ticket integrations** — Phasical, Jira, Linear with per-project status mapping and open-ticket policies
- **Notifications** — Shoutrrr (Slack, ntfy, Discord, …) on create, error, and supersede events
- **Admin UI** — repos, integrations, ticket projects, notifications, poll interval
- **Single-container deploy** — SQLite on a volume; no Redis, Postgres, or multi-service Compose

## Quick start

**Full operator guide:** [docs/getting-started.md](docs/getting-started.md)

```bash
cp .env.example .env
# Edit .env — set SESSION_SECRET, APP_ENCRYPTION_KEY, and optional BOOTSTRAP_ADMIN_* vars

docker compose up -d
```

Open [http://localhost:3000](http://localhost:3000) and sign in with your bootstrap admin credentials. Remove `BOOTSTRAP_ADMIN_*` from `.env` after the first successful login.

Pre-built images: `ghcr.io/mdg-labs/release-ops:latest` (release) and `:nightly` (dev branch).

## Documentation

| Document | Description |
|----------|-------------|
| [Getting started](docs/getting-started.md) | Docker Compose setup, env vars, admin bootstrap, troubleshooting |
| [Product spec](docs/specs.html) | Architecture, APIs, providers, MVP acceptance criteria |
| [Tech stack](docs/stack.html) | Go, Next.js, COSS, SQLite, CI/CD |
| [Database schema](docs/schema.html) | SQLite `app.db` tables (canonical DDL: `db/schema.sql`) |
| [MVP checklist](docs/mvp-checklist.md) | Release sign-off against all 15 acceptance criteria |
| [Roadmap](docs/roadmap.html) | Implementation phases (`docs/roadmap.json`) |

Browse all docs from [docs/index.html](docs/index.html).

## Development

Prerequisites: Go 1.25+, Node.js 22+, `sqlite3` + `sqldiff` (for `npm run db:check`).

```bash
npm install
go test ./...
npm test && npm run lint && npm run typecheck
```

| Command | Purpose |
|---------|---------|
| `npm run dev` | Next.js dev server (`apps/web`, port 3000) |
| `go run ./cmd/server` | Go API + poll scheduler (port 8080) |
| `make migrate-diff name=<change>` | Generate migration from `db/schema.sql` drift |
| `make migrate-up` | Apply pending migrations locally |
| `make sqlc-generate` | Regenerate typed SQL from `queries/` |

Local Go server expects `SESSION_SECRET`, `APP_ENCRYPTION_KEY`, and `APP_DB_PATH` (see [spec §9](docs/specs.html#env)).

## Architecture

```
Browser → Next.js (:3000) → /api/go/* proxy → Go API (:8080, loopback)
                              ↓
                         SQLite app.db (/data)
```

- **Go** — REST API, session auth, poll scheduler, provider clients, credential encryption
- **Next.js** — COSS UI, React Query, next-intl; proxies API calls and forwards session cookies
- **SQLite** — single `app.db` file; Go is the only writer

See [specs.html §2](docs/specs.html#architecture) for the full contract.

## CI/CD

GitHub Actions on `dev` and `main`: Go lint/test/build, web lint/test/typecheck, `db:check`, Docker image push to GHCR. See [spec §11](docs/specs.html#ci).

## Attribution

Release Ops is built with these open-source projects (among others). Thank you to their authors and maintainers.

### Backend (Go)

| Project | Role |
|---------|------|
| [chi](https://github.com/go-chi/chi) | HTTP router |
| [alexedwards/scs](https://github.com/alexedwards/scs) | Session management |
| [golang-migrate](https://github.com/golang-migrate/migrate) | Database migrations |
| [sqlc](https://sqlc.dev/) | Typed SQL queries |
| [modernc.org/sqlite](https://gitlab.com/cznic/sqlite) | Pure-Go SQLite driver |
| [robfig/cron](https://github.com/robfig/cron) | Poll scheduler |
| [google/uuid](https://github.com/google/uuid) | UUID generation |
| [containrrr/shoutrrr](https://github.com/containrrr/shoutrrr) | Notification delivery |

### Frontend (Next.js)

| Project | Role |
|---------|------|
| [Next.js](https://nextjs.org/) | App framework (App Router, standalone output) |
| [React](https://react.dev/) | UI library |
| [COSS UI](https://coss.com/ui) / [Base UI](https://base-ui.com/) | Component system (`@base-ui/react`) |
| [TanStack Query](https://tanstack.com/query) | Server state |
| [TanStack Table](https://tanstack.com/table) | Data tables |
| [next-intl](https://next-intl.dev/) | Internationalization |
| [Tailwind CSS](https://tailwindcss.com/) | Styling |
| [Lucide](https://lucide.dev/) | Icons |

### Tooling & infrastructure

| Project | Role |
|---------|------|
| [Vitest](https://vitest.dev/) | Web unit tests |
| [golangci-lint](https://golangci-lint.run/) | Go static analysis |
| [Docker](https://www.docker.com/) | Container image |
| [GitHub Actions](https://github.com/features/actions) | CI/CD |
| [SQLite](https://www.sqlite.org/) | Embedded database |

Full dependency lists: `go.mod`, `apps/web/package.json`, and [docs/stack.html](docs/stack.html).
