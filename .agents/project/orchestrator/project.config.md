# Project config — Release Ops

Project constants for the orchestrator, execution/verifier sub-agents, `/kaneo-intake`, `/kaneo-triage` and `/dependabot-triage`. Project-owned — edit in place.

## Repository

| Field | Value |
| ----- | ----- |
| Project name | Release Ops |
| Repo path | the repo root (wherever this checkout lives — do not hardcode an absolute path) |
| GitHub repo | `mdg-labs/release-ops` |
| Integration branch | `dev` |
| Production branch | `main` — agents never push here |
| Task branch (Lane P) | `orchestrator/<TASK-ID>` |
| Worktree (Lane P) | Managed by Claude Code (Agent `isolation: "worktree"` → `.claude/worktrees/…`); agent switches to the task branch |
| Plan file | `docs/roadmap.html` (generated from `docs/roadmap.json`; 11 epics `E01`–`E11`, leaves `E01-01`, …) |
| Doc hub | `docs/index.html` (not the plan file) |
| Spec docs | `docs/specs.html`, `db/schema.sql`, `docs/stack.html` |
| Sibling repo | `../release-ops-cloud` (control plane, landing page, billing, provisioning) |

## Kaneo board

| Field | Value |
| ----- | ----- |
| MCP server | `Kaneo` — Claude Code tools `mcp__Kaneo__<tool>` |
| Workspace | MDG-Labs `ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj` |
| Project | Release Ops `z4janvyjsbbb0esishvd9gb8` |
| Ticket key | `RO` — refs are `RO-<n>` (e.g. `RO-108`) |
| Task URL | `https://cloud.kaneo.app/dashboard/workspace/ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj/project/z4janvyjsbbb0esishvd9gb8/task/<taskCuid>` |
| Columns (slugs) | `backlog`, `ready`, `in-progress`, `in-review`, `implemented`, `done` (final) |
| GitHub MCP | `github` — tools `mcp__github__<tool>`; **read-only** for issues |

## Status ownership

| Column | Set by | When |
| ------ | ------ | ---- |
| `backlog` / `ready` | kaneo-intake / kaneo-triage / dependabot-triage / user | Intake and triage stop at `ready` |
| `in-progress` | Execution agent | First action (leaf + parent epic) |
| `in-review` | Execution agent | After AC + scoped gate, before the commit |
| `implemented` | Verifier | All layers PASS, PASS comment first; parent epic when its last child passes. Commit on `dev`, not yet on `main` |
| `in-progress` (rework) | Verifier | Any layer FAIL, FAIL comment first |
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
3. Otherwise (primary method) `mcp__github__search_issues` (owner `mdg-labs`, repo `release-ops`, query = exact Kaneo task title) → pick the exact-title match.
4. No match → roadmap key (`[E*-*]`) if the task has one (no trailer); otherwise ask the user.

The same `#N` goes into the subject `[#N]` and the body trailer `fixes #N`. Resolve the epic's `#N` too — the final leaf carries `fixes #<parent-N>`.

Never create, comment on, label, assign, close or otherwise write to a GitHub issue. Issues close only through the `fixes #N` trailer landing on `main`.

### Epic workflow (`/orchestrator RO-<n>`)

```text
1. get_task_by_ticket_id("RO-<n>")        # epic AC + CUID
2. get_task_relations(<epic CUID>)        # subtasks + blocking edges
3. get_task(<leaf CUID>) per leaf         # leaf AC
4. search_issues per leaf + epic title    # githubIssueNumber (read-only)
5. Dispatch leaves; epic in-progress on first leaf; last leaf gets closesParent: yes (fixes #<parent-N>);
   epic implemented on last leaf PASS; done via GitHub sync when the commits land on main
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

## Task ID prefixes

- `E01`–`E11` — roadmap epics; leaves `E01-01`, … (one orchestrator session per epic)
- `RO-<n>` — Kaneo tickets
- `EL-` — epic-level orchestrator batch IDs (when used)

## Commit conventions

- Subject: `<type>(<scope>)[#N]: <summary>` — `#N` = GitHub issue mirroring the Kaneo task, resolved read-only
- Body trailer (mandatory on task commits with a GitHub issue): `fixes #N`; the final leaf of an epic (`closesParent: yes`) adds `fixes #<parent-N>`
- Roadmap-only (no GitHub issue): `[E*-*]`, no trailer
- Scopes: `release-ops`, `api`, `db`, `config`, `ci`, `docs`, `deps`
- PR bodies may carry closing keywords too (optional); no Kaneo CUIDs or `RO-<n>` in commits
- Full rule: `.claude/rules/07-commit-linking.md`

## Gates

| Gate | Command |
| ---- | ------- |
| Scoped (every task commit) | `npm test && npm run lint && npm run db:check` |
| Go | `go test ./... && golangci-lint run` |
| Full (pre-push, only when asked to push) | `npm test && npm run lint && npm run typecheck && npm run db:check` |
| Migrations | edit `db/schema.sql` → `make migrate-diff name=<change>` |

## Optional

| Field | Value |
| ----- | ----- |
| Multi-repo workspace | `release-ops.code-workspace` + `.claude/settings.json` `additionalDirectories` → `../release-ops-cloud` |
| Phase gates | `doc-index.md` § Phase gates |
