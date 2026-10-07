# Release Ops

**Turn new upstream releases into tickets, automatically.**

[![Dev CI](https://github.com/mdg-labs/release-ops/actions/workflows/dev.yml/badge.svg?branch=dev)](https://github.com/mdg-labs/release-ops/actions/workflows/dev.yml)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)
[![GHCR](https://img.shields.io/badge/GHCR-ghcr.io%2Fmdg--labs%2Frelease--ops-333?logo=docker&logoColor=white)](https://github.com/mdg-labs/release-ops/pkgs/container/release-ops)
[![Docs](https://img.shields.io/badge/docs-mdg--labs.github.io%2Frelease--ops-0A7EA4)](https://mdg-labs.github.io/release-ops/)

Release Ops is a self-hosted release monitor with a web UI. It polls releases on **GitHub**, **GitLab**, **Gitea**, **Forgejo** and **Codeberg**, and creates a ticket in **Kaneo**, **Jira** or **Linear** when a real new release ships. Everything runs in one Docker container: Go API, polling, auth and the Next.js UI.

## Why

Keeping dependencies, base images and upstream tools current usually means watching release pages or wading through notification noise. Release Ops watches the projects you care about and puts each new release where your team already plans work, as a ticket, with no duplicate tickets piling up for releases nobody has handled yet.

<!-- Screenshot placeholder: dashboard / repos overview -->

## Features

### Release sources

| Source   | Instance                              | Token    |
| -------- | ------------------------------------- | -------- |
| GitHub   | github.com                            | Optional |
| GitLab   | gitlab.com or self-managed (base URL) | Required |
| Gitea    | Self-hosted (base URL)                | Optional |
| Forgejo  | Self-hosted (base URL)                | Optional |
| Codeberg | codeberg.org                          | Optional |

Release Ops tracks the latest stable release of each monitored repo.

### Ticket integrations

| Integration | Instance                              | Ticket target | Auth              |
| ----------- | ------------------------------------- | ------------- | ----------------- |
| Kaneo       | Kaneo Cloud or self-hosted (base URL) | Project       | API key           |
| Jira        | Your Jira site (base URL)             | Project key   | Email + API token |
| Linear      | linear.app                            | Team          | API key           |

One integration can serve several ticket projects, each with its own status mapping. Any source can be combined with any ticket integration, per monitored repo.

### Behaviour

- **No ticket spam** — the first poll of a repo only records a baseline; tickets are created on a real tag change
- **Open-ticket policies** — per ticket project: `supersede` (default; new ticket, old one commented and moved to a superseded status), `merge` (update the open ticket) or `skip_if_open`
- **Status mapping** — map each ticket project's open, done, cancelled and superseded statuses; open tickets are checked live before acting
- **Custom ticket templates** — per-project title, description and supersede comment
- **Scheduled and manual polling** — interval set in the UI, plus manual runs on demand
- **Notifications** — [Shoutrrr](https://github.com/containrrr/shoutrrr) targets (Slack, ntfy, Discord, …) on create, error and supersede events
- **Admin UI** — repos, integrations, ticket projects, notifications, poll interval and users (invitation-based, no public sign-up)
- **Single-container deploy** — SQLite on a volume; no Redis, Postgres or multi-service Compose; credentials encrypted at rest

## Quick start

**Full operator guide:** [docs/getting-started.md](docs/getting-started.md)

### Docker Compose (recommended)

```bash
cp .env.example .env
# Edit .env — set SESSION_SECRET, APP_ENCRYPTION_KEY, and optional BOOTSTRAP_ADMIN_* vars
#   SESSION_SECRET:     openssl rand -base64 32
#   APP_ENCRYPTION_KEY: openssl rand -hex 32

docker compose up -d
```

### Docker run

Uses the same `.env` file as Compose:

```bash
docker run -d --name release-ops \
  -p 3000:3000 \
  -v release-ops-data:/data \
  --env-file .env \
  --restart unless-stopped \
  ghcr.io/mdg-labs/release-ops:latest
```

Keep `APP_ENCRYPTION_KEY` safe and stable: stored credentials can't be decrypted without it.

Open [http://localhost:3000](http://localhost:3000) and sign in with your bootstrap admin credentials. Remove `BOOTSTRAP_ADMIN_*` after the first successful login.

Pre-built images: `ghcr.io/mdg-labs/release-ops:latest` (release) and `:nightly` (dev branch). All environment variables are listed in `.env.example` and [spec §9](https://github.com/mdg-labs/release-ops/blob/dev/docs/specs.html).

## Self-hosted vs cloud

Release Ops is free to self-host under the [AGPL-3.0](LICENSE). A hosted version may be offered in the future.

## Upgrading

The database migrations were reset to a single baseline. A `/data` volume created by an image from before that reset cannot be opened by this version: stop the container, remove the old `app.db` (or the whole volume) and start again to create a fresh database. This applies once; later releases add migrations on top of the baseline and apply them automatically on start, after saving a snapshot under `/data/snapshots`.

## Documentation

**Published site:** [mdg-labs.github.io/release-ops](https://mdg-labs.github.io/release-ops/)

| Document                                                                                | Description                                                      |
| --------------------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| [Introduction](https://mdg-labs.github.io/release-ops/)                                 | What Release Ops is, what it is not and who it is for            |
| [Getting started](https://mdg-labs.github.io/release-ops/getting-started/)              | Docker Compose setup, env vars, admin bootstrap, troubleshooting |
| [Product spec](https://github.com/mdg-labs/release-ops/blob/dev/docs/specs.html)        | Architecture, APIs, providers, MVP acceptance criteria (repo)    |
| [Tech stack](https://github.com/mdg-labs/release-ops/blob/dev/docs/stack.html)          | Go, Next.js, COSS, SQLite, CI/CD (repo)                          |
| [Database schema](https://github.com/mdg-labs/release-ops/blob/dev/db/schema.sql)       | SQLite `app.db` tables, canonical DDL (repo)                     |
| [MVP checklist](https://github.com/mdg-labs/release-ops/blob/dev/docs/mvp-checklist.md) | Release sign-off against all 15 acceptance criteria (repo)       |
| [Roadmap](https://github.com/mdg-labs/release-ops/blob/dev/docs/roadmap.html)           | Implementation phases (`docs/roadmap.json`, repo)                |

The published site covers installation and the product overview; the spec, stack, schema, checklist and roadmap live in the repository only.

Operator quick reference (repo): [docs/getting-started.md](docs/getting-started.md).  
Build the docs site locally: `npm run docs:sync && npm run dev:docs`.

## Development

Prerequisites: Go 1.26+, Node.js 22+.

```bash
npm install
go test ./...
npm test && npm run lint && npm run typecheck
```

| Command                           | Purpose                                       |
| --------------------------------- | --------------------------------------------- |
| `npm run dev`                     | Next.js dev server (`apps/web`, port 3000)    |
| `npm run dev:docs`                | Starlight docs site (`apps/docs`, port 4321)  |
| `npm run docs:build`              | Sync + build static docs for GitHub Pages     |
| `go run ./cmd/server`             | Go API + poll scheduler (port 8080)           |
| `make db-migration name=<change>` | Generate migration from `db/schema.sql` changes |
| `make sqlc-generate`              | Regenerate typed SQL from `queries/`          |

Local Go server expects `SESSION_SECRET`, `APP_ENCRYPTION_KEY`, and `APP_DB_PATH` (see [spec §9](https://github.com/mdg-labs/release-ops/blob/dev/docs/specs.html)).

## Architecture

```
Browser → Next.js (:3000) → /api/go/* proxy → Go API (:8080, loopback)
                              ↓
                         SQLite app.db (/data)
```

- **Go** — REST API, session auth, poll scheduler, provider clients, credential encryption
- **Next.js** — COSS UI, React Query, next-intl; proxies API calls and forwards session cookies
- **SQLite** — single `app.db` file; Go is the only writer

See [spec §2 — Architecture](https://github.com/mdg-labs/release-ops/blob/dev/docs/specs.html) for the full contract.

## CI/CD

GitHub Actions on `dev` and `main`: Go lint/test/build, web lint/test/typecheck, `db:check`, Docker image push to GHCR. See [spec §11](https://github.com/mdg-labs/release-ops/blob/dev/docs/specs.html).

## Attribution

Release Ops is built with these open-source projects (among others). Thank you to their authors and maintainers.

### Backend (Go)

| Project                                                       | Role                  |
| ------------------------------------------------------------- | --------------------- |
| [chi](https://github.com/go-chi/chi)                          | HTTP router           |
| [alexedwards/scs](https://github.com/alexedwards/scs)         | Session management    |
| [sqlite-migrate](https://github.com/mdg-labs/sqlite-migrate)  | Database migrations   |
| [sqlc](https://sqlc.dev/)                                     | Typed SQL queries     |
| [modernc.org/sqlite](https://gitlab.com/cznic/sqlite)         | Pure-Go SQLite driver |
| [robfig/cron](https://github.com/robfig/cron)                 | Poll scheduler        |
| [google/uuid](https://github.com/google/uuid)                 | UUID generation       |
| [containrrr/shoutrrr](https://github.com/containrrr/shoutrrr) | Notification delivery |

### Frontend (Next.js)

| Project                                                          | Role                                          |
| ---------------------------------------------------------------- | --------------------------------------------- |
| [Next.js](https://nextjs.org/)                                   | App framework (App Router, standalone output) |
| [React](https://react.dev/)                                      | UI library                                    |
| [COSS UI](https://coss.com/ui) / [Base UI](https://base-ui.com/) | Component system (`@base-ui/react`)           |
| [TanStack Query](https://tanstack.com/query)                     | Server state                                  |
| [TanStack Table](https://tanstack.com/table)                     | Data tables                                   |
| [next-intl](https://next-intl.dev/)                              | Internationalization                          |
| [Tailwind CSS](https://tailwindcss.com/)                         | Styling                                       |
| [Lucide](https://lucide.dev/)                                    | Icons                                         |

### Tooling & infrastructure

| Project                                               | Role               |
| ----------------------------------------------------- | ------------------ |
| [Vitest](https://vitest.dev/)                         | Web unit tests     |
| [golangci-lint](https://golangci-lint.run/)           | Go static analysis |
| [Docker](https://www.docker.com/)                     | Container image    |
| [GitHub Actions](https://github.com/features/actions) | CI/CD              |
| [SQLite](https://www.sqlite.org/)                     | Embedded database  |

Full dependency lists: `go.mod`, `apps/web/package.json`, and the [tech stack doc](https://github.com/mdg-labs/release-ops/blob/dev/docs/stack.html).

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for branches, commit format and local checks. All contributions require agreeing to the [Contributor License Agreement](CLA.md).

## License

Copyright (C) 2026 Michael Guggenbichler (MDG Labs). Licensed under AGPL-3.0-only — see [LICENSE](LICENSE).
