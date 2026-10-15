# Release Ops

Self-hosted release monitor: polls GitHub, GitLab, Gitea, Forgejo and Codeberg for new releases and creates tickets in configured integrations. Single Docker container — Go API + poller (`cmd/server`, `internal/`) and Next.js + COSS UI (`apps/web`). Sibling repo: `../release-ops-cloud` (control plane, landing page, billing, provisioning).

## Where things live

| What | Path |
| ---- | ---- |
| Project rules (auto-loaded; `paths:` frontmatter = file-scoped) | `.claude/rules/*.md` |
| Skills (`/orchestrate`, `/dev-diff`, `/open-pr`, `/cr-review`, `/security-audit`, `/triage`, `/customer-docs`, `/coss`) | `.claude/skills/` — vendored from `mdg-labs/skills` by `/mdg-setup:setup` (locked in `.claude/mdg-workflow.lock.json`; update through setup, not by hand). Only `coss` and `coss-particles` are third-party (`cosscom/coss`, pinned in `skills-lock.json`); don't edit those |
| Project sub-agents | `.claude/agents/*.md` |
| Orchestrate prompt templates + known escapes | `.claude/skills/orchestrate/templates/` |
| Project constants (tracker ids, branches, gate, checks) / area → paths, hazards, security units | `.claude/workflow.json` / `## Agent workflow` below |
| Threat model (security yardstick) | `docs/threat-model.md` |
| Defect patterns that escaped review / session memory (gitignored) | `.claude/known-escapes.md` / `.agents/project/agent-memory/` |
| Historical roadmap (generated from `docs/roadmap.json`; Kaneo is the live plan) | `docs/roadmap.html` — `docs/index.html` is the doc hub |
| Third-party skill lock (coss only) | `skills-lock.json` |

Start with `.claude/rules/00-project.md` (identity, doc precedence, hard rules) and `.claude/rules/01-git-workflow.md`.

## Commands

```bash
npm test && npm run lint && npm run typecheck  # web / root JS — before every commit that touches them
npm run db:check                               # db/schema.sql, migrations/ (sqlite-migrate check; needs Go only)
go test ./... && golangci-lint run             # Go
.claude/skills/dev-diff/dev-diff.sh            # dev→main size as CodeRabbit counts it (cap 100)
make db-migration name=<change>                # schema change — edit db/schema.sql first; never hand-write migrations/
```

## MCP servers

The harness expects these Claude Code MCP server names (tool prefix `mcp__<name>__`):

- `Kaneo` — Kaneo board, project Release Ops (`RO-<n>`); source of truth for tasks and status. Through the claude.ai connector the same tools appear as `mcp__claude_ai_Kaneo__<tool>`; skills and agents use whichever the session lists. Agents never set `done`: it comes from the Kaneo ↔ GitHub sync when a `fixes #N` commit lands on `main` (or the user sets it).
- `github` — read-only for issues (resolve the `#N` for commit subjects); without it, `.claude/scripts/gh-rest.sh issue-view` / `issue-search` do the same reads. Agents never comment on, label, close or create GitHub issues via API. Task commits carry `[#N]` in the subject and a mandatory `fixes #N` trailer in the body — the only way an issue closes (`07-commit-linking.md`).

## Workflow

Task → `/triage` (RO-<n>, a problem statement, or a Dependabot alert; stops at `ready`) → `/orchestrate RO-<n>` (scratch-clone executor, independent verifier, land on `dev` after PASS, push `dev`) → `/open-pr` (dev→main PR) → `/cr-review <PR>` → the maintainer merges; the `fixes #N` trailers close the issues and the Kaneo ↔ GitHub sync moves tasks to `done`. `/security-audit` reviews the code against `docs/threat-model.md`.

## Agent workflow

<!-- Read by the vendored skills (orchestrate, triage, cr-review, security-audit).
     Machine-read settings live in .claude/workflow.json. -->

### Project

Release Ops is a self-hosted release monitor: it polls GitHub, GitLab, Gitea, Forgejo and Codeberg for new releases and creates tickets in configured integrations (Jira, Linear, Kaneo, GitHub, …). One Docker container: Go API + poller (`cmd/server`, `internal/`, SQLite via sqlc) and a Next.js + COSS web UI (`apps/web`) with next-intl; auth lives in Go. Customer docs are a Starlight site (`apps/docs`).

### Design docs

Precedence on conflict: `docs/specs.html` (MVP contract, APIs, UI, domain logic) → `db/schema.sql` (canonical DDL; `docs/schema.html` is a view only) → `docs/stack.html` → `docs/index.html` (hub). `docs/threat-model.md` is the security yardstick. `docs/roadmap.html` is historical; the tracker is the live plan. Behaviour not defined in a spec doc: ask before guessing; never invent fields, endpoints or IDs.

### Area → paths

| Label | Paths |
|-------|-------|
| `backend` | `cmd/`, `internal/api/`, `internal/poll/`, `internal/providers/`, `internal/mail/`, `internal/tickettemplate/`, `internal/crypto/`, `tools/` |
| `web` | `apps/web/` |
| `db` | `db/schema.sql`, `migrations/`, `queries/`, `internal/store/`, `sqlc.yaml` |
| `config` | `internal/config/`, `.env.example`, `docker-compose.yml` |
| `ci` | `.github/workflows/`, `Dockerfile`, `docker/`, `Makefile`, `scripts/ci/` |
| `docs` | `docs/`, `apps/docs/` |

### Always-shared files

`CLAUDE.md`, `Makefile`, `go.mod`, `go.sum`, `package.json`, `package-lock.json`, `apps/web/package.json`, `apps/docs/package.json`, `db/schema.sql`, `migrations/`, `internal/store/db/` (sqlc output), `apps/web/messages/en.json`, `docs/specs.html`, `Dockerfile`, `.github/workflows/`, `.gitignore`. A schema change touches `db/schema.sql`, `migrations/` and usually `queries/` + `internal/store/db/`; an npm dependency change touches `package-lock.json`.

### Entry points

`cmd/server/main.go`, `internal/api/routes.go` / `router.go`, `apps/web/app/…` pages, the `apps/web/app/api/go/` proxy, `Makefile`, `.github/workflows/`. A task's `Reachable via:` criterion names one of these; add it to the task's scope.

### Hazards

- No `sudo`, package-manager install or write under `/etc`; no system service touched. Work that needs root is reported `blocked`.
- Docker is root-equivalent: agents never run, stop, remove or prune a container, image or volume. A task whose acceptance needs `docker build` is verified by CI after the push.
- Kill by PID only (`cmd & PID=$!`) — never `pkill`/`killall`.
- Every command is bounded: explicit timeout on anything not obviously fast, no scan rooted at `/` or `$HOME`, nothing left running when a dispatch ends.
- No agent connects to a live provider or ticket system (GitHub, GitLab, Gitea, Jira, Linear, Kaneo as a product integration, SMTP). Provider code is tested against fakes and recorded fixtures.
- Secrets never leave `.env` files; no agent prints, copies or commits a token, key or password, and none reads `~/.claude/.env` or credential stores.

### Implementation rules

- Schema changes only via sqlite-migrate: edit `db/schema.sql` (every table `STRICT`), run `make db-migration name=<change>`; never hand-write `migrations/*.sql` (`.claude/rules/11-db-migrations.md`).
- sqlc output (`internal/store/db/*.sql.go`) is generated: edit `queries/*.sql`, run `make sqlc-generate`.
- Web UI: zero hardcoded user-facing strings — next-intl keys in `apps/web/messages/en.json` only; ESLint enforces it (`.claude/rules/10-i18n.md`).
- Credentials and integration tokens are never logged or returned by the API.
- Dependabot alerts are resolved by upgrading the dependency, never by dismissing (`.claude/rules/08-dependabot-alerts.md`).
- Task commits: `<type>(<scope>)[#N]: …` with a mandatory `fixes #N` trailer (`.claude/rules/07-commit-linking.md`); allowed scopes `release-ops`, `api`, `db`, `config`, `ci`, `docs`, `deps`. Never amend pushed commits (`.claude/rules/13-no-amend-pushed.md`).
- Kaneo quirks: `RO-<n>` → `get_task_by_ticket_id` without `projectId`; `get_task` needs the CUID; `search` takes `q` and needs both `workspaceId` and `projectId`. Through the claude.ai connector the tools are `mcp__claude_ai_Kaneo__*`.

### Risk review

For risk-flagged items (schema, migrations, credential crypto, store): the migration is sqlite-migrate-generated and `npm run db:check` passes; existing migration files are untouched; the change upgrades an existing `app.db` without data loss (new columns nullable or defaulted); stored credentials stay encrypted at rest and never reach logs or API responses; `go test ./...` covers the upgrade path.

### Security units

Source: `docs/threat-model.md` (attackers §2, invariants §4).

| Unit | Paths | Attackers | Invariants | Priority |
|------|-------|-----------|------------|----------|
| `auth` — sessions, login, one-time tokens, bootstrap, rate limiting | `internal/api/auth/` `internal/api/middleware/` `queries/sessions.sql` `queries/auth_tokens.sql` `queries/users.sql` `cmd/seed-admin/` | 2.1 2.2 2.5 2.7 | T4 T5 T6 T7 T8 T11 T17 | yes |
| `proxy` — the Next.js listener, the `/api/go` proxy, middleware and login redirect | `apps/web/app/api/` `apps/web/middleware.ts` `apps/web/next.config.ts` `apps/web/lib/auth/` `apps/web/lib/api/` `apps/web/app/login/` | 2.1 2.6 2.7 | T4 T9 T11 | yes |
| `credentials` — secrets at rest, config and startup checks | `internal/crypto/` `internal/config/` `internal/store/integrations.go` `internal/store/notifications.go` `queries/integrations.sql` `queries/notification_targets.sql` `.env.example` | 2.2 2.5 | T1 T2 T3 T14 | yes |
| `api-handlers` — the protected REST handlers and router | `internal/api/handlers/` `internal/api/router.go` `internal/api/routes.go` `internal/api/health.go` `internal/api/doc.go` | 2.1 2.2 | T2 T3 T4 T10 | yes |
| `sources` — release fetching from forges | `internal/providers/source/` `internal/providers/doc.go` | 2.3 | T10 T15 | |
| `tickets` — ticket creation, metadata, templates, connection tests | `internal/providers/ticket/` `internal/providers/integrationtester/` `internal/tickettemplate/` | 2.2 2.4 | T10 T15 | |
| `poll` — the scheduler, run engine and notifications | `internal/poll/` `internal/store/poll.go` `queries/poll_runs.sql` `queries/monitored_repos.sql` `queries/monitored_repo_notifications.sql` | 2.3 2.4 | T3 T10 T15 | |
| `mail` — outbound email and its templates | `internal/mail/` | 2.1 2.6 | T3 T8 T14 T15 | |
| `store` — schema, queries, migrations tooling | `internal/store/` `queries/` `db/schema.sql` `migrations/` `sqlc.yaml` | 2.5 | T12 T16 | |
| `runtime` — server wiring, image, entrypoint | `cmd/server/` `Dockerfile` `docker/` `docker-compose.yml` | 2.7 2.8 | T4 T13 T14 T16 | |
| `web` — the rest of the web UI | `apps/web/` | 2.2 2.3 | T9 T15 | |
| `supply-chain` — dependencies, workflows, build | `go.mod` `go.sum` `package.json` `package-lock.json` `apps/docs/package.json` `.github/` `Makefile` `.golangci.yml` `.coderabbit.yaml` `.gitignore` `scripts/` | 2.8 | T13 | |

Sweeps — cross-cutting units whose files are found at run time (tests excluded):

- `sweep-outbound` — files: `git grep -l -E 'http\.(NewRequest|Get|Post)|\.Do\(|net\.Dial|smtp\.' -- '*.go'` — looks for: requests to user-configured hosts without the SSRF / redirect guards, credentials sent to a host they were not saved for, TLS verification turned off, response bodies read unbounded.
- `sweep-sql` — files: `git grep -l -E '\.(Exec|Query|QueryRow)(Context)?\(|fmt\.Sprintf' -- 'internal/*.go' 'cmd/*.go' 'tools/*.go' 'queries/*.sql'` — looks for: SQL built from input rather than bound parameters; queries missing an owner or role filter.

### Machine check

```
git status -sb | head -1 && git status --porcelain | head -5
git rev-parse dev origin/dev
command -v go node npm golangci-lint gh python3
go version; node --version; gh auth status 2>&1 | head -3
```
