# Project config — Release Ops

Project constants for `/orchestrate` and its agents, `/kaneo-intake`, `/kaneo-triage`, `/dependabot-triage`, `/cr-review`, `/open-pr` and `/security-audit`. Project-owned — edit in place.

## Repository

| Field | Value |
| ----- | ----- |
| Project name | Release Ops |
| Repo path | the repo root (wherever this checkout lives — do not hardcode an absolute path) |
| GitHub repo | `mdg-labs/release-ops` |
| Integration branch | `dev` |
| Production branch | `main` — moves only through the `dev → main` PR (`/open-pr`); agents never push here |
| Task isolation | `/orchestrate` scratch clones: `git clone --branch dev <repo> <scratchpad>/orchestrate/<unit-id>-a<n>` |
| Historical plan | `docs/roadmap.html` (generated from `docs/roadmap.json`; epics `E01`–`E11`) — Kaneo is the live plan |
| Doc hub | `docs/index.html` (not the plan file) |
| Spec docs | `docs/specs.html`, `db/schema.sql`, `docs/stack.html` |
| Sibling repo | `../release-ops-cloud` (control plane, landing page, billing, provisioning) |

## Kaneo board

| Field | Value |
| ----- | ----- |
| MCP server | `Kaneo` — Claude Code tools `mcp__Kaneo__<tool>`; through the claude.ai connector the same tools are `mcp__claude_ai_Kaneo__<tool>` — use whichever the session lists |
| Workspace | MDG-Labs `ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj` |
| Project | Release Ops `z4janvyjsbbb0esishvd9gb8` |
| Ticket key | `RO` — refs are `RO-<n>` (e.g. `RO-108`) |
| Task URL | `https://cloud.kaneo.app/dashboard/workspace/ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj/project/z4janvyjsbbb0esishvd9gb8/task/<taskCuid>` |
| Columns (slugs) | `backlog`, `ready`, `in-progress`, `in-review`, `implemented`, `done` (final) |
| GitHub (read-only for issues) | GitHub MCP `github` (`mcp__github__<tool>`) when configured, else `gh issue view` / `gh issue list` |

## Status ownership

| Column | Set by | When |
| ------ | ------ | ---- |
| `backlog` / `ready` | kaneo-intake / kaneo-triage / dependabot-triage / user | Intake and triage stop at `ready` |
| `in-progress` | `task-executor` (orchestrator backstop right after dispatch; orchestrator for epics) | Before it starts a task |
| `in-review` | `task-executor` | After the task's commit in its scratch clone |
| `implemented` | `task-verifier` (orchestrator for epics) | PASS, PASS comment first. Commit verified, landed and pushed to `dev`, not yet on `main` |
| `in-progress` (rework) | `task-verifier` | FAIL, FAIL comment first |
| `ready` (abandoned) | orchestrator | Blocked, escalated or dropped for the budget |
| `done` | **Kaneo ↔ GitHub sync** (or the user manually) | When the `fixes #N` commit lands on `main` and GitHub closes the issue. Never by an agent |

Verifier comments go on the Kaneo task (`mcp__Kaneo__create_task_comment`), never to GitHub.

## Task lookup

| Input | Call |
| ----- | ---- |
| `RO-<n>` | `mcp__Kaneo__get_task_by_ticket_id` `{ ticketId: "RO-<n>" }` — **no** `projectId` (passing it currently 404s) |
| Task CUID | `mcp__Kaneo__get_task` `{ taskId: "<cuid>" }` — `get_task` does **not** accept `RO-<n>` ("Workspace ID could not be determined") |
| GitHub `#N` / issue URL | `mcp__github__issue_read` (owner `mdg-labs`, repo `release-ops`) → title → `mcp__Kaneo__search` `{ q: "<title>", workspaceId, projectId }` |
| Title / keyword | `mcp__Kaneo__search` `{ q, type: "tasks", workspaceId: "ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj", projectId: "z4janvyjsbbb0esishvd9gb8" }` — param is `q`, not `query` |
| Roadmap key (`E03-02`) | `search` for the key or title — descriptions carry `Roadmap ID: E03-02` |
| Ready queue | `mcp__Kaneo__list_tasks` for the project, column `ready` |
| Epic subtasks / prerequisites | `mcp__Kaneo__get_task_relations` with the parent CUID → subtask / blocking edges |

### Resolving the GitHub `#N` for commits (read-only)

Kaneo payloads have so far carried **no** `externalLinks` (`get_task` / `get_task_by_ticket_id` for RO-108 return none).

1. User gave `#N` → use it.
2. Optional: the payload has `externalLinks` → use `externalLinks[].externalId` of the issue link. Not observed today; skip when absent.
3. Otherwise (primary method) search by the exact Kaneo task title — `mcp__github__search_issues` (owner `mdg-labs`, repo `release-ops`) or `gh issue list --repo mdg-labs/release-ops --state all --search "<title> in:title" --json number,title` — and pick the exact-title match.
4. No match → roadmap key (`[E*-*]`) if the task has one (no trailer); otherwise ask the user.

The same `#N` goes into the subject `[#N]` and the body trailer `fixes #N`. Resolve the epic's `#N` too — `/orchestrate` adds `fixes #<epic-N>` to the commit that completes the epic.

Never create, comment on, label, assign, close or otherwise write to a GitHub issue. Issues close only through the `fixes #N` trailer landing on `main`.

### Epic workflow (`/orchestrate RO-<n>`)

```text
1. get_task_by_ticket_id("RO-<n>")        # epic + CUID
2. get_task_relations(<epic CUID>)        # subtasks + blocking edges
3. get_task(<subtask CUID>) per subtask   # AC contract
4. gh / GitHub MCP search per title       # #N for each subtask and the epic (read-only)
5. Readiness gate → waves → bundles → lanes; the orchestrator rolls the epic up
   (in-progress when a subtask starts, implemented when all are) and adds
   fixes #<epic-N> to the commit that completes it; done via the GitHub sync
```

## Domain labels

| Label | Scope |
| ----- | ----- |
| `backend` | Go API, polling, providers (same container as Next.js) |
| `web` | Next.js UI, COSS, Go API proxy |
| `db` | SQLite schema, migrations (`app.db` — app + auth) |
| `config` | App settings in DB, env bootstrap only |
| `ci` | GitHub Actions, Docker, GHCR |
| `docs` | Spec and roadmap HTML docs |

## Area → paths

Used by `/orchestrate` (file scope fallback) and `/security-audit` (scope by label).

| Label | Paths |
| ----- | ----- |
| `backend` | `cmd/`, `internal/api/`, `internal/poll/`, `internal/providers/`, `internal/mail/`, `internal/tickettemplate/`, `internal/crypto/`, `tools/` |
| `web` | `apps/web/` |
| `db` | `db/schema.sql`, `migrations/`, `queries/`, `internal/store/`, `sqlc.yaml`, `scripts/migrate-diff.mjs`, `scripts/ci/check-migrations-sync.mjs` |
| `config` | `internal/config/`, `.env.example`, `docker-compose.yml` |
| `ci` | `.github/workflows/`, `Dockerfile`, `docker/`, `Makefile`, `scripts/ci/` |
| `docs` | `docs/`, `apps/docs/` |

Always-shared files (any change touching them serializes against every other): `CLAUDE.md`, `Makefile`, `go.mod`, `go.sum`, `package.json`, `package-lock.json`, `apps/web/package.json`, `apps/docs/package.json`, `db/schema.sql`, `migrations/`, `internal/store/db/`, `apps/web/messages/en.json`, `docs/specs.html`, `Dockerfile`, `.github/workflows/`, `.gitignore`.

## Task ID prefixes

- `RO-<n>` — Kaneo tickets
- `E01`–`E11`, `E01-01`, … — historical roadmap keys (task descriptions carry `Roadmap ID:`)
- `<unit-id>-a<n>` — orchestrate scratch clone names (`RO-110-a1`, `RO-110+RO-115-a1`)

## Commit conventions

- Subject: `<type>(<scope>)[#N]: <summary>` — `#N` = GitHub issue mirroring the Kaneo task, resolved read-only
- Body trailer (mandatory on task commits with a GitHub issue): `fixes #N`; the commit that completes an epic also carries `fixes #<epic-N>` (added by `/orchestrate` at landing)
- Advisory fixes: no `[#N]`, neutral message, trailer `Refs: GHSA-…`
- Roadmap-only (no GitHub issue): `[E*-*]`, no trailer
- Scopes: `release-ops`, `api`, `db`, `config`, `ci`, `docs`, `deps`
- PR bodies may carry closing keywords too (optional); no Kaneo CUIDs or `RO-<n>` in commits
- Full rule: `.claude/rules/07-commit-linking.md`

## Gates

| Gate | Command |
| ---- | ------- |
| Web / root JS | `npm test && npm run lint && npm run typecheck` |
| DB | `npm run db:check` |
| Go | `go test ./... && golangci-lint run` |
| Customer docs | `npm run docs:build` |
| Before a push | all of the above that apply (`.claude/rules/06-local-ci-before-commit.md`) |
| Migrations | edit `db/schema.sql` → `make migrate-diff name=<change>` |

## Optional

| Field | Value |
| ----- | ----- |
| Multi-repo workspace | `release-ops.code-workspace` + `.claude/settings.json` `additionalDirectories` → `../release-ops-cloud` |
| Phase gates | `doc-index.md` § Phase gates |
| Promotion budget | `.claude/skills/dev-diff/dev-diff.sh --list` — CodeRabbit-reviewable `main...dev` files; cap 100 |
