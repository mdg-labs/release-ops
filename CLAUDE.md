# Release Ops

Self-hosted dependency-update bot: a GitHub App runs Renovate against the repositories it is installed on, and Release Ops creates a ticket in the configured integration for each update PR. Two images from one `Dockerfile` — the app (Go API, webhook receiver, job queue, ticket engine in `cmd/server`, `internal/`, plus the Next.js + COSS UI in `apps/web`) and the worker (`release-ops worker`, which runs Renovate as an unprivileged Unix user). Sibling repo: `../release-ops-cloud` (control plane, landing page, billing, provisioning).

## Where things live

| What | Path |
| ---- | ---- |
| Project rules (auto-loaded; `paths:` frontmatter = file-scoped) | `.claude/rules/*.md` |
| Skills (`/orchestrate`, `/dev-diff`, `/open-pr`, `/pr-review`, `/security-audit`, `/triage`, `/customer-docs`, `/coss`) | `.claude/skills/` — vendored from `mdg-labs/skills` by `/mdg-setup:setup` (locked in `.claude/mdg-workflow.lock.json`; update through setup, not by hand). Only `coss` and `coss-particles` are third-party (`cosscom/coss`, pinned in `skills-lock.json`); don't edit those |
| Project sub-agents | `.claude/agents/*.md` |
| Orchestrate prompt templates + known escapes | `.claude/skills/orchestrate/templates/` |
| Project constants (tracker ids, branches, gate, checks) / area → paths, hazards, security units | `.claude/workflow.json` / `## Agent workflow` below |
| Threat model (security yardstick) | `docs/threat-model.md` |
| Defect patterns that escaped review / session memory (gitignored) | `.claude/known-escapes.md` / `.agents/project/agent-memory/` |
| Historical roadmap (generated from `docs/roadmap.json`; GitHub issues are the live plan) | `docs/roadmap.html` — `docs/index.html` is the doc hub |
| Third-party skill lock (coss only) | `skills-lock.json` |

Start with `.claude/rules/00-project.md` (identity, doc precedence, hard rules) and `.claude/rules/01-git-workflow.md`.

## Commands

```bash
npm test && npm run lint && npm run typecheck  # web / root JS — before every commit that touches them
npm run db:check                               # db/schema.sql, migrations/ (sqlite-migrate check; needs Go only)
go test ./... && golangci-lint run             # Go
.claude/skills/dev-diff/dev-diff.sh            # dev→main size as CodeRabbit counts it (cap 300)
make db-migration name=<change>                # schema change — edit db/schema.sql first; never hand-write migrations/
```

## Tracker

GitHub issues in `mdg-labs/release-ops` are the plan and the status (`status:*` labels), used through `gh` exactly as `.claude/trackers/github.md` says: every `gh` call names the repo (`-R mdg-labs/release-ops`), status changes only through `.claude/scripts/issue-status.sh` / `epic-status.sh`, and `.github/workflows/issue-status.yml` sets `status:new`, `closed` and `cancelled`. No agent closes, reopens or deletes an issue; a `Fixes #N` trailer reaching `main` is the only way an issue closes.

## Workflow

Issue → `/triage` (`#N`, a problem statement, or a Dependabot alert; stops at `status:ready`) → `/orchestrate #N` (scratch-clone executor, independent verifier, land on `dev` after PASS, push `dev`) → `/open-pr` (dev→main PR) → `/pr-review <PR>` → the maintainer merges; the `Fixes #N` trailers close the issues. `/security-audit` reviews the code against `docs/threat-model.md`.

## Agent workflow

<!-- Read by the vendored skills (orchestrate, triage, pr-review, security-audit).
     Machine-read settings live in .claude/workflow.json. -->

### Project

Release Ops is a self-hosted dependency-update bot. One GitHub App is the bot (it opens the update PRs through Renovate), the webhook sender and the sign-in provider; Release Ops creates a ticket per update PR in the configured ticket integration (Jira, Linear, Kaneo) and keeps it in step with the PR. Two processes ship from one `Dockerfile`:

- **App** (`runtime` target): the Go API with SQLite via sqlc (`cmd/server`, `internal/`) — setup flow, GitHub sign-in and roles, `POST /hooks/github`, update-PR tracking, ticket engine, the job queue, and an internal listener for the worker — behind a Next.js + COSS web UI (`apps/web`) with next-intl. Auth lives in Go. The App private key and `APP_ENCRYPTION_KEY` exist only here, and installation tokens are minted only here.
- **Worker** (`worker` target, `release-ops worker`): claims jobs from the app's internal listener with `WORKER_SECRET`, has no database access, and runs the pinned Renovate as a second Unix user with the job's installation token.

Customer docs are a Starlight site (`apps/docs`).

### Design docs

Precedence on conflict: `docs/specs.html` (MVP contract, APIs, UI, domain logic) → `db/schema.sql` (canonical DDL; `docs/schema.html` is a view only) → `docs/stack.html` → `docs/index.html` (hub). `docs/threat-model.md` is the security yardstick. `docs/roadmap.html` is historical; the tracker is the live plan. Behaviour not defined in a spec doc: ask before guessing; never invent fields, endpoints or IDs.

### Area → paths

| Label | Paths |
|-------|-------|
| `backend` | `cmd/`, `internal/api/`, `internal/buildinfo/`, `internal/crypto/`, `internal/github/`, `internal/logging/`, `internal/metrics/`, `internal/pollwatch/`, `internal/providers/`, `internal/queue/`, `internal/ratelimit/`, `internal/renovate/`, `internal/scheduler/`, `internal/tickettemplate/`, `internal/updateprs/`, `internal/updatetickets/` |
| `web` | `apps/web/` |
| `db` | `db/schema.sql`, `migrations/`, `queries/`, `internal/store/`, `sqlc.yaml` |
| `config` | `internal/config/`, `.env.example`, `docker-compose.yml` |
| `ci` | `.github/`, `Dockerfile`, `docker/`, `Makefile`, `scripts/ci/`, `.golangci.yml`, `.coderabbit.yaml` |
| `docs` | `docs/`, `apps/docs/`, `scripts/` except `scripts/ci/` (roadmap and docs-sync tooling), `.agents/`, `.claude/` (repo-owned workflow config only; vendored files are never edited) |

### Always-shared files

`CLAUDE.md`, `Makefile`, `go.mod`, `go.sum`, `package.json`, `package-lock.json`, `apps/web/package.json`, `apps/docs/package.json`, `db/schema.sql`, `migrations/`, `internal/store/db/` (sqlc output), `apps/web/messages/en.json`, `docs/specs.html`, `Dockerfile`, `.github/workflows/`, `.gitignore`. A schema change touches `db/schema.sql`, `migrations/` and usually `queries/` + `internal/store/db/`; an npm dependency change touches `package-lock.json`.

### Entry points

- App: `cmd/server/main.go` (the server; `admin login-link` in `cmd/server/admin.go`). Worker: `cmd/server/worker.go` (`release-ops worker`, wired to `internal/queue/worker/` and `internal/renovate/`).
- Public API: `internal/api/router.go` / `routes.go` — sign-in and session routes, the setup routes (`/api/v1/setup/*`, `internal/api/setup/`) and the protected `/api/v1/*` handlers.
- Webhook route: `POST /hooks/github`, registered by `internal/api/router.go` (`registerWebhook`) over `internal/api/hooks/`; it exists only when `WEBHOOK_URL` is set.
- Internal-listener worker routes (`/internal/v1/jobs/…`: claim, token, heartbeat, result, release): `internal/queue/internalapi/handler.go` (`NewHandler`), served from `cmd/server/main.go` on `INTERNAL_ADDR`.
- Web: `apps/web/app/…` pages (including `/setup` and `/login`), the `apps/web/app/api/go/` proxy and the `/hooks/github` rewrite in `apps/web/next.config.ts`.
- Build and CI: `Makefile`, `Dockerfile` (targets `runtime` and `worker`), `.github/workflows/`.

A task's `Reachable via:` criterion names one of these; add it to the task's scope.

### Hazards

- No `sudo`, package-manager install or write under `/etc`; no system service touched. Work that needs root is reported `blocked`.
- Docker is root-equivalent: agents never run, stop, remove or prune a container, image or volume themselves. The one exception is `.claude/scripts/act-run.sh`, which runs changed GitHub Actions jobs locally under `workflow.json` `act` (jobs in `act.neverRun` never run). A task whose acceptance needs `docker build` is verified by CI after the push.
- Kill by PID only (`cmd & PID=$!`) — never `pkill`/`killall`.
- Every command is bounded: explicit timeout on anything not obviously fast, no scan rooted at `/` or `$HOME`, nothing left running when a dispatch ends.
- No agent connects to a live GitHub (github.com, api.github.com, a GitHub Enterprise Server) or ticket system (Jira, Linear, Kaneo as a product integration). GitHub and ticket code is tested against fakes (`internal/github/githubtest/`) and recorded fixtures.
- No agent runs the Renovate worker, `release-ops worker` against a real app, or any Renovate command (`renovate`, `renovate-config-validator`, a dry run) against live GitHub or with any live token. Renovate runs only inside the fixture CI jobs, which need Docker and are never run locally (`act.neverRun`).
- No agent creates, modifies, installs or deletes a GitHub App, a webhook or an OAuth app, and none follows the setup manifest flow against github.com.
- Secrets never leave `.env` files; no agent prints, copies or commits a token, key or password, and none reads `~/.claude/.env` or credential stores. That includes the GitHub App private key (pem), client secret and webhook secret, installation tokens, user OAuth tokens, `WORKER_SECRET`, `SETUP_TOKEN`, `SESSION_SECRET`, `APP_ENCRYPTION_KEY` and break-glass login links. Test fixtures use generated throwaway keys and secrets, never real ones.

### Implementation rules

- Schema changes only via sqlite-migrate: edit `db/schema.sql` (every table `STRICT`), run `make db-migration name=<change>`; never hand-write `migrations/*.sql` (`.claude/rules/11-db-migrations.md`).
- sqlc output (`internal/store/db/*.sql.go`) is generated: edit `queries/*.sql`, run `make sqlc-generate`.
- Web UI: zero hardcoded user-facing strings — next-intl keys in `apps/web/messages/en.json` only; ESLint enforces it (`.claude/rules/10-i18n.md`).
- Credentials and integration tokens are never logged or returned by the API.
- Dependabot alerts are resolved by upgrading the dependency, never by dismissing (`.claude/rules/08-dependabot-alerts.md`).
- Task commits: `<type>(<scope>): …` with a `Fixes #N` trailer (`.claude/rules/mdg-git.md`, `01-git-workflow.md`); allowed scopes `release-ops`, `api`, `db`, `config`, `ci`, `docs`, `deps`. Never amend pushed commits (`.claude/rules/13-no-amend-pushed.md`).

### Risk review

For risk-flagged items (schema, migrations, credential crypto, the GitHub App key and setup flow, token minting, the internal listener, store): the migration is sqlite-migrate-generated and `npm run db:check` passes; existing migration files are untouched; the change upgrades an existing `app.db` without data loss (new columns nullable or defaulted); stored secrets (integration credentials, the App private key, client secret and webhook secret) stay encrypted at rest and never reach logs, API responses or the worker; installation tokens and `WORKER_SECRET` are never stored or logged; `go test ./...` covers the upgrade path.

### Security units

Source: `docs/threat-model.md` (attackers §2, invariants §4).

| Unit | Paths | Attackers | Invariants | Priority |
|------|-------|-----------|------------|----------|
| `auth` — sessions, GitHub sign-in (OAuth state and PKCE), role and repository-scope checks, break-glass login link, rate limiting | `internal/api/auth/` `internal/api/middleware/` `internal/store/signin.go` `internal/github/oauth.go` `internal/github/membership.go` `queries/sessions.sql` `queries/auth_tokens.sql` `queries/users.sql` `queries/login_link_tokens.sql` `cmd/server/admin.go` | 2.1 2.2 2.5 2.6 2.7 2.11 | T4 T5 T7 T11 T17 T19 T20 T21 | yes |
| `proxy` — the Next.js listener, the `/api/go` proxy and the `/hooks/github` pass-through, middleware, forwarded-address preload and login redirect | `apps/web/app/api/` `apps/web/middleware.ts` `apps/web/next.config.ts` `apps/web/preload/` `apps/web/lib/auth/` `apps/web/lib/api/` `apps/web/app/login/` | 2.1 2.6 2.7 2.9 | T4 T9 T11 T18 | yes |
| `webhooks` — `POST /hooks/github`: signature check, delivery dedupe, event handlers, redelivery recovery | `internal/api/hooks/` `internal/store/webhook_deliveries.go` `queries/webhook_deliveries.sql` `internal/github/deliveries.go` `internal/github/recovery.go` | 2.1 2.3 2.6 2.9 | T11 T12 T18 T23 | yes |
| `setup` — App manifest flow, setup token, App private key and its rotation, installation binding, installation-token minting in the app | `internal/api/setup/` `internal/github/manifest.go` `internal/github/app.go` `internal/github/appclient.go` `internal/github/jwt.go` `internal/api/handlers/github_app.go` `internal/api/handlers/installations.go` `internal/store/github_app.go` `internal/store/access.go` `internal/store/installations.go` `internal/store/installation_sync.go` `internal/store/installation_settings.go` `queries/github_app.sql` `queries/installations.sql` `apps/web/app/setup/` `apps/web/lib/setup/` | 2.1 2.2 2.5 2.6 | T1 T2 T7 T9 T10 T17 T22 T25 T27 | yes |
| `worker` — the worker command, Renovate runner as an unprivileged Unix user, the job queue, and the internal listener (claim, token, heartbeat, result, release) with `WORKER_SECRET` auth; the worker has no database access | `cmd/server/worker.go` `internal/queue/` `internal/renovate/` `internal/config/worker.go` `internal/store/jobs.go` `queries/jobs.sql` `internal/ratelimit/` | 2.3 2.7 2.8 2.10 | T3 T4 T13 T15 T22 T24 | yes |
| `credentials` — secrets at rest, log redaction, config and startup checks | `internal/crypto/` `internal/config/` `internal/logging/` `internal/store/integrations.go` `queries/integrations.sql` `.env.example` | 2.2 2.5 | T1 T2 T3 T14 | yes |
| `api-handlers` — the protected REST handlers, router and operational endpoints | `internal/api/handlers/` `internal/api/router.go` `internal/api/routes.go` `internal/api/health.go` `internal/api/ready.go` `internal/api/doc.go` `internal/metrics/` | 2.1 2.2 2.11 | T2 T3 T4 T10 T20 T26 | yes |
| `engine` — update-PR tracking, reconcile, scheduler and polling-mode watcher | `internal/updateprs/` `internal/scheduler/` `internal/pollwatch/` `internal/github/sync.go` `internal/github/watch.go` `internal/github/listing.go` `internal/github/pulls.go` `internal/github/bot.go` `queries/repositories.sql` `queries/update_prs.sql` `queries/update_pr_dependencies.sql` | 2.3 2.9 | T10 T12 T15 T23 T27 | |
| `tickets` — ticket creation and lifecycle, metadata, templates, PR comments, connection tests | `internal/updatetickets/` `internal/providers/ticket/` `internal/providers/integrationtester/` `internal/providers/doc.go` `internal/tickettemplate/` `internal/github/comments.go` `queries/update_tickets.sql` `queries/ticket_projects.sql` | 2.2 2.3 2.4 | T10 T15 | |
| `store` — schema, queries, migrations tooling | `internal/store/` `queries/` `db/schema.sql` `migrations/` `sqlc.yaml` | 2.5 | T12 T16 | |
| `runtime` — server and worker wiring, images, entrypoint | `cmd/server/` `Dockerfile` `docker/` `docker-compose.yml` | 2.7 2.8 2.10 | T4 T13 T14 T16 | |
| `web` — the rest of the web UI | `apps/web/` | 2.2 2.3 2.11 | T9 T15 T20 | |
| `supply-chain` — dependencies, the pinned Renovate image, workflows, build | `go.mod` `go.sum` `package.json` `package-lock.json` `apps/docs/package.json` `.github/` `Dockerfile` `Makefile` `.golangci.yml` `.coderabbit.yaml` `.gitignore` `scripts/` | 2.8 | T13 | |

Sweeps — cross-cutting units whose files are found at run time (tests excluded):

- `sweep-outbound` — files: `git grep -l -E 'http\.(NewRequest|Get|Post|Client|Transport)|\.Do\(|\.do\(|net\.(Dial|Dialer)|DialContext' -- '*.go'` (the GitHub client and OAuth calls in `internal/github/`, the ticket providers and metadata, the worker's client of the internal listener, the global-config fetch, the update-PR reconcile) — looks for: requests to user-configured hosts (ticket base URLs, `GITHUB_BASE_URL`) without the SSRF / redirect guards, credentials or installation tokens sent to a host they were not issued for (T10), tokens wider than the job's scope (T22), TLS verification turned off, a redirect that carries a credential to another host, missing timeouts, response bodies read unbounded.
- `sweep-sql` — files: `git grep -l -E '\.(Exec|Query|QueryRow)(Context)?\(|fmt\.Sprintf' -- 'internal/*.go' 'cmd/*.go' 'queries/*.sql' ':!internal/store/db/'` — looks for: SQL built from input rather than bound parameters (T12); queries missing an owner, installation or repository-scope filter.

### Machine check

```
git status -sb | head -1 && git status --porcelain | head -5
git rev-parse dev origin/dev
command -v go node npm golangci-lint gh python3
go version; node --version; gh auth status 2>&1 | head -3
```
