# Release Ops

**Dependency updates as pull requests and tickets, powered by Renovate.**

[![Dev CI](https://github.com/mdg-labs/release-ops/actions/workflows/dev.yml/badge.svg?branch=dev)](https://github.com/mdg-labs/release-ops/actions/workflows/dev.yml)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)
[![GHCR](https://img.shields.io/badge/GHCR-ghcr.io%2Fmdg--labs%2Frelease--ops-333?logo=docker&logoColor=white)](https://github.com/mdg-labs/release-ops/pkgs/container/release-ops)
[![Docs](https://img.shields.io/badge/docs-mdg--labs.github.io%2Frelease--ops-0A7EA4)](https://mdg-labs.github.io/release-ops/)

Release Ops is a self-hosted dependency-update bot with a web UI. One **GitHub App** runs [Renovate](https://docs.renovatebot.com/) for every repository it is installed on and opens the update pull requests as its own bot user. Release Ops turns every update PR into a tracked ticket in **Kaneo**, **Jira** or **Linear**, and a **dashboard** lists the open and merged update PRs and the jobs behind them. You sign in with GitHub and get an **admin** or **viewer** role.

It runs as two services in one Docker Compose project: the app (Go API and Next.js UI) and the worker (Renovate).

> **Status:** v2 is in development on this branch and is not released; this README describes v2.0.0. Running Renovate in the worker, GitHub sign-in and the admin and viewer roles are not implemented yet and land before the v2.0.0 release. Until then, sign-in is by email and password.

## Why

Renovate is the best engine for keeping dependencies current, but its pull requests are easy to lose track of: they pile up in repositories, and nobody plans work around them. Release Ops keeps Renovate as the engine and adds what teams are missing around it: each update PR gets exactly one ticket in the tracker where your team already plans work, the ticket follows the PR (updated, superseded, done on merge), and one dashboard shows what is open across your repositories. There is no per-repository bot setup: one GitHub App, created for you from a first-start setup page.

<!-- Screenshot placeholder: dashboard of update PRs -->

## Features

### Update PRs and GitHub

| Feature                    | What it does                                                                                                                                                             |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Renovate-driven update PRs | For every repository the App is installed on and that has a Renovate config, the pinned, unmodified Renovate opens the update PRs as the App's bot user                  |
| One GitHub App             | Created from `/setup` through GitHub's manifest flow; it is the bot, the webhook sender and the login provider. One installation per instance                            |
| Webhooks within seconds    | Ticking a Renovate checkbox in a PR or in the Dependency Dashboard starts a run within seconds. Merged and closed PRs, pushes to the default branch and new repos do too |
| Job queue                  | One job per repository at a time, a concurrency cap (default 2) and a slot reserved for webhook-triggered jobs                                                           |
| Enable repo                | For a repository without a Renovate config, an admin action opens a PR that adds a `renovate.json` extending your preset                                                 |
| Global Renovate config     | A file in git (or mounted into the worker), shown read-only in the UI                                                                                                    |

### Ticket integrations

| Integration | Instance                              | Ticket target | Auth              |
| ----------- | ------------------------------------- | ------------- | ----------------- |
| Kaneo       | Kaneo Cloud or self-hosted (base URL) | Project       | API key           |
| Jira        | Your Jira site (base URL)             | Project key   | Email + API token |
| Linear      | linear.app                            | Team          | API key           |

One integration can serve several ticket projects, each with its own status mapping. Each repository uses the installation's default ticket project or its own override.

### Update lifecycle

- **One ticket per update PR** — listing all its dependencies, rendered from the ticket project's templates; Renovate's config-migration PR, onboarding PRs and PRs without dependencies get none
- **The ticket follows the PR** — a new version of the same PR updates the ticket and adds a note; a PR replaced by a new one supersedes the old ticket and links the new one; a merged PR moves its ticket to done; a PR closed unmerged moves it to cancelled
- **Hands-off when you edit** — a ticket whose status was changed by hand is left alone
- **No duplicate tickets** — creation is at-most-once, even across timeouts and restarts
- **Ticket link on the PR** — one bot comment, edited in place and never re-posted; nothing is written into the PR body
- **Ticket filter** — per ticket project: update types, labels, skip automerged PRs (default: every non-automerged PR)
- **Status mapping and templates** — map each ticket project's open, done, cancelled and superseded statuses, and customise the ticket title and description

### Dashboard and access

- **Dashboard** — open and recently merged update PRs with repository, age, update type, ticket link and status, and a link to the repository's Dependency Dashboard
- **Jobs and problems** — job history with log excerpts, and a Problems view for job errors, ticket errors, rate limits and lost App permissions
- **GitHub login with roles** — **admin** and **viewer**, derived from GitHub: organization owners and the optional admin team are admins; other organization members (or the optional viewer team) are viewers; on a personal-account installation only the account owner signs in. A viewer sees only the repositories they can access on GitHub
- **Operations** — Prometheus `/metrics`, `/healthz` and `/readyz` endpoints; credentials and the App's private key encrypted at rest

## Quick start

Release Ops runs as **two services**: `app` (the only one that publishes a port) and `worker` (Renovate, no database access and no Docker socket). Both images are released with the same tag and upgraded together: `ghcr.io/mdg-labs/release-ops` and `ghcr.io/mdg-labs/release-ops-worker` (`:latest` for a release, `:nightly` for the dev branch).

1. Copy `.env.example` to `.env` and set the required variables:

   ```bash
   cp .env.example .env
   #   SESSION_SECRET:     openssl rand -base64 32
   #   APP_ENCRYPTION_KEY: openssl rand -hex 32
   #   WORKER_SECRET:      openssl rand -base64 32   (the same value for the app and the worker)
   #   APP_URL:            the browser-facing URL, e.g. https://release-ops.example.com
   #   INTERNAL_ADDR:      bind address of the internal listener the worker connects to, e.g. :9090 (Compose network only)
   #
   # Optional:
   #   WEBHOOK_URL:        the public URL GitHub delivers webhooks to, e.g. https://release-ops.example.com/hooks/github
   #                       (unset: the webhook is inactive and the instance polls, degraded; see the note below)
   ```

2. Start both services:

   ```bash
   docker compose up -d
   ```

3. **First-start setup.** While no GitHub App exists the instance is in setup mode. Read the one-time setup token from the app's log (`docker compose logs app`), or set `SETUP_TOKEN` yourself.
4. Open `APP_URL/setup`, enter the token and choose who owns the App (a personal account or an organization). Confirm on GitHub, then install the App on the repositories you want updated.
5. Sign in with GitHub. The first admin is the organization owner (or the account owner), or the GitHub login pinned with `BOOTSTRAP_ADMIN_GITHUB_LOGIN`.
6. Add a ticket integration and a ticket project, set the installation's default ticket project in Settings, and make sure each repository has a Renovate config (or use **Enable repo**).

Keep `APP_ENCRYPTION_KEY` safe and stable: the stored credentials and the App's private key can't be decrypted without it. Back up `app.db` with `VACUUM INTO` or a volume snapshot, not by copying the file alone (the database is in WAL mode); see [spec §10](https://github.com/mdg-labs/release-ops/blob/dev/docs/specs.html).

> **A public webhook URL is recommended.** GitHub must be able to reach `WEBHOOK_URL` (`/hooks/github`) for runs to start within seconds of a ticked checkbox. Without a public URL (NAT, a home lab) leave `WEBHOOK_URL` unset: the instance then **falls back to polling, which is degraded** — it reacts in minutes rather than seconds and a tick made during a run can be missed. Only `/hooks/github` and the UI (including the OAuth callback) need to be exposed.

All environment variables are listed in `.env.example` and in [spec §9](https://github.com/mdg-labs/release-ops/blob/dev/docs/specs.html).

## Renovate and licensing

Release Ops is powered by [Renovate](https://github.com/renovatebot/renovate), the open-source dependency-update engine, and works with it as follows:

- **Unmodified, separate process.** The worker image is built on the official Renovate image, pinned to one version per release. Renovate runs unmodified as a separate process; Release Ops talks to it only through its environment, its configuration files and its logs and report.
- **Two AGPL-3.0 projects.** Renovate is licensed AGPL-3.0, and so is Release Ops ([LICENSE](LICENSE)).
- **Where to find the source.** The worker image ships a NOTICE with the licence notice and a pointer to the source of the pinned Renovate version, and the same notice appears on the About page in the UI (`/about`).
- **Naming.** Release Ops is "powered by Renovate" and "works with Renovate". "Renovate" and "Mend" are not part of the product name, and the Renovate logo is not used.
- **No Mend images.** The Mend Renovate Community and Enterprise Edition images are not used.

## Self-hosted vs cloud

Release Ops is free to self-host under the [AGPL-3.0](LICENSE). A hosted version may be offered in the future.

## Upgrading

**v2 is a clean break from v1.** It ships a new schema and performs **no automatic data migration**: a v1 `/data` volume is not upgraded. Start v2 on a **fresh data volume** and set up the GitHub App, the integrations and the ticket projects again.

If you ran Renovate before (a GitHub Action or a self-hosted setup), close the old Dependency Dashboard issues of that setup; the migration guide in the customer docs will cover this and what is lost in the move (tracked as [#224](https://github.com/mdg-labs/release-ops/issues/224)).

From v2 on, later releases add migrations on top of the schema and apply them automatically on start, after saving a snapshot under `/data/snapshots`. Upgrade both images to the same tag.

## Documentation

**Published site:** [mdg-labs.github.io/release-ops](https://mdg-labs.github.io/release-ops/)

| Document                                                                              | Description                                                                       |
| ------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| [Introduction](https://mdg-labs.github.io/release-ops/)                               | What Release Ops is, what it is not and who it is for                             |
| [Getting started](https://mdg-labs.github.io/release-ops/getting-started/)            | Operator guide (being rewritten for v2: two services, `/setup`)                   |
| [Docs hub](https://github.com/mdg-labs/release-ops/blob/dev/docs/index.html)          | Entry point to the repository documents below (repo)                              |
| [Product spec](https://github.com/mdg-labs/release-ops/blob/dev/docs/specs.html)      | Update engine, setup and roles, update PRs and tickets, APIs, acceptance criteria |
| [Tech stack](https://github.com/mdg-labs/release-ops/blob/dev/docs/stack.html)        | Renovate worker, Go, Next.js, COSS, SQLite, two-service Compose, CI/CD (repo)     |
| [Threat model](https://github.com/mdg-labs/release-ops/blob/dev/docs/threat-model.md) | Attackers, trust boundaries and invariants for security review (repo)             |
| [Database schema](https://github.com/mdg-labs/release-ops/blob/dev/db/schema.sql)     | SQLite `app.db` tables, canonical DDL (repo)                                      |

The v1 release monitor's [MVP checklist](https://github.com/mdg-labs/release-ops/blob/dev/docs/mvp-checklist.md) and [roadmap](https://github.com/mdg-labs/release-ops/blob/dev/docs/roadmap.html) are kept as v1 history only. GitHub issues are the live plan.

The published site covers installation and the product overview; the spec, stack, threat model and schema live in the repository only.

Operator quick reference (repo, being rewritten for v2): [docs/getting-started.md](docs/getting-started.md).
Build the docs site locally: `npm run docs:sync && npm run dev:docs`.

## Development

Prerequisites: Go 1.26+, Node.js 22+.

```bash
npm install
go test ./...
npm test && npm run lint && npm run typecheck
```

| Command                           | Purpose                                         |
| --------------------------------- | ----------------------------------------------- |
| `npm run dev`                     | Next.js dev server (`apps/web`, port 3000)      |
| `npm run dev:docs`                | Starlight docs site (`apps/docs`, port 4321)    |
| `npm run docs:build`              | Sync + build static docs for GitHub Pages       |
| `go run ./cmd/server`             | Go API (port 8080)                              |
| `make db-migration name=<change>` | Generate migration from `db/schema.sql` changes |
| `make sqlc-generate`              | Regenerate typed SQL from `queries/`            |

Local Go server expects the required variables of spec §9, for example `SESSION_SECRET`, `APP_ENCRYPTION_KEY` and `APP_URL`, and `APP_DB_PATH` (see [spec §9](https://github.com/mdg-labs/release-ops/blob/dev/docs/specs.html)).

## Architecture

```
GitHub (App: webhooks, installation tokens, OAuth, API, git)
   | webhooks                                 ^ branches and PRs, written by Renovate
   v                                          |
+------- app container ----------+      +------ worker container -------+
| Next.js :3000 (public)         |      | release-ops worker (Go)       |
|   /api/go/* proxy -> Go API    |      |   claims a job, posts result  |
|   /hooks/github -> webhook     | HTTP | Renovate (pinned, unmodified, |
| Go API :8080 (loopback)        |<---->|   second Unix user)           |
| Internal listener              |      | no database, no data volume,  |
|   (INTERNAL_ADDR, Compose only)|      | no Docker socket              |
| SQLite app.db (WAL) in /data   |      +-------------------------------+
+---------------+----------------+
                |
                v
       Kaneo / Jira / Linear
```

- **App** — Next.js COSS UI, React Query and next-intl; Go REST API, GitHub App setup and login, webhook handler, job queue, PR-state sync, ticket engine and credential encryption. The only process that writes `app.db`
- **Worker** — claims jobs from the app over the internal listener (Compose network only, authenticated with `WORKER_SECRET`), runs Renovate for the one repository of each claimed job, and posts the result. It never holds the encryption key or the App's private key
- **SQLite** — a single `app.db` file in WAL mode, holding the data and the persistent job queue

See [spec §2 — Architecture](https://github.com/mdg-labs/release-ops/blob/dev/docs/specs.html) for the full contract.

## CI/CD

GitHub Actions on `dev` and `main`: Go lint/test/build, web lint/test/typecheck, `db:check`, Docker image push to GHCR for both the app and the worker image. See [spec §11](https://github.com/mdg-labs/release-ops/blob/dev/docs/specs.html).

## Attribution

Release Ops is built with these open-source projects (among others). Thank you to their authors and maintainers.

### Update engine

| Project                                             | Role                                                                         |
| --------------------------------------------------- | ---------------------------------------------------------------------------- |
| [Renovate](https://github.com/renovatebot/renovate) | Dependency-update engine (AGPL-3.0, unmodified, separate process; see above) |

### Backend (Go)

| Project                                                      | Role                  |
| ------------------------------------------------------------ | --------------------- |
| [chi](https://github.com/go-chi/chi)                         | HTTP router           |
| [alexedwards/scs](https://github.com/alexedwards/scs)        | Session management    |
| [sqlite-migrate](https://github.com/mdg-labs/sqlite-migrate) | Database migrations   |
| [sqlc](https://sqlc.dev/)                                    | Typed SQL queries     |
| [modernc.org/sqlite](https://gitlab.com/cznic/sqlite)        | Pure-Go SQLite driver |
| [google/uuid](https://github.com/google/uuid)                | UUID generation       |

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

Copyright (c) 2026 Michael David Guggenbichler (MDG Labs). Licensed under AGPL-3.0-only — see [LICENSE](LICENSE).
