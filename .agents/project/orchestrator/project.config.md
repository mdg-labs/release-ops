# Project config — Release Ops

> Supporting file — created by project-setup (layer **phasical**). Lives under `.agents/project/` — **not** inside `.agents/skills/` (`npx skills update` wipes skill directories).

## Repository

| Field | Value |
| ----- | ----- |
| Project name | Release Ops |
| Repo path | `/home/mdguggenbichler/projects/release-ops` |
| GitHub repo | `mdg-labs/release-ops` |
| Integration branch | `dev` |
| Production branch | `main` — agents must not push here |
| Task branch (Lane P) | `orchestrator/<TASK-ID>` |
| Worktree (Lane P) | `../release-ops-wt/<TASK-ID>` |
| Plan file | `docs/roadmap.html` |
| Spec doc glob | `docs/specs.html`, `db/schema.sql`, `docs/stack.html` |

## Phasical

| Field | Value |
| ----- | ----- |
| MCP server | `user-phasical` |
| Workspace | MDG-Labs (`X3VbytvC7pKgazK2dAsOQIFtdGYRzdGH`) |
| Project | Release Ops (`tv679ggt5ier9r5dx70w8ks6`) |
| Project slug | `RO` — human-readable refs are `RO-<number>` (e.g. `RO-1`, `RO-12`) |
| Ready status slug | `ready` |
| GitHub MCP (read) | `user-github` |

**Commits:** use GitHub `[#N]` from `externalLinks.externalId`. Never Phasical task IDs in git.

### Task lookup (orchestrator + agents)

When the user names a Phasical ref (`RO-1`), GitHub `#N`, or a CUID — **resolve with `get_task` first**. Do not use `search` for known refs.

| User says | MCP call |
| --------- | -------- |
| `RO-1`, `RO-12`, … | `get_task` `taskId: "RO-1"` (slug + number) |
| Phasical CUID | `get_task` `taskId: "<cuid>"` |
| GitHub `#36` or issue URL | `list_tasks` `projectId` + match `externalLinks.externalId`, or `user-github` `issue_read` |
| Epic subtasks | `get_task` parent → `get_task_relations` `taskId` → filter `relationType: subtask` |
| Epic prerequisites | `get_task_relations` → filter `relationType: blocks` |
| Roadmap ID in description (`E01`, …) | `list_tasks` `projectId` — filter description for `Roadmap ID: E01` |
| Ready queue | `list_tasks` `projectId` + `status: ready` |
| Task title (fuzzy) | `list_tasks` `projectId` — filter by title |

**`get_task` example (preferred for `RO-*`):**

```text
get_task({ taskId: "RO-1" })
get_task({ taskId: "RO-1", scopeProjectId: "tv679ggt5ier9r5dx70w8ks6", scopeWorkspaceId: "X3VbytvC7pKgazK2dAsOQIFtdGYRzdGH" })  # disambiguation only
```

**Epic workflow (`/orchestrator RO-1`):**

```text
1. get_task("RO-1")                          # parent epic AC + CUID
2. get_task_relations(parentCuid)            # subtasks + blocks edges
3. get_task each leaf RO-N                   # leaf AC + externalLinks → githubIssueNumber
4. Dispatch leaf tasks; parent in-progress on first leaf; parent done on last leaf PASS
```

**`search` — use only when `get_task` / `list_tasks` cannot resolve the ref:**

- Parameter is `q` (not `query`).
- When scoping to this project, pass **both** `workspaceId` and `projectId` — `projectId` alone returns `Workspace ID could not be determined`.
- Unscoped `search` for task refs often returns HTTP 400.

```text
# OK — title/keyword discovery
search({ q: "scaffold", type: "tasks", workspaceId: "X3VbytvC7pKgazK2dAsOQIFtdGYRzdGH", projectId: "tv679ggt5ier9r5dx70w8ks6" })

# WRONG — known ref; use get_task instead
search({ q: "RO-1", projectId: "tv679ggt5ier9r5dx70w8ks6" })   # missing workspaceId → error
search({ query: "RO-1" })                                       # wrong param name
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

## Area prefixes (titles)

- `E01`–`E11` — consolidated roadmap epics (one orchestrator session per epic)
- `G1` / `G2` — Grundlagen modules (legacy)
- `EL-` — epic-level orchestrator batches (when used)

## Commit conventions

- Phasical/GitHub tasks: `[#N]` in subject
- Roadmap-only (no Phasical mirror): `[E*-*]` or `[G*]` in subject
- Body: `fixes #N` when project rules require it (see `.cursor/rules/`)

## Optional

| Field | Value |
| ----- | ----- |
| Multi-repo workspace | none |
| Slack session-end | not configured |
| Phase gates | see `doc-index.md` § Phase gates |
