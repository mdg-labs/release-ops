# Project config — Release Ops

> Supporting file — created by project-setup (layer **phasical**), migrated to Kaneo. Lives under `.agents/project/` — **not** inside `.claude/skills/` (`npx skills update` wipes skill directories).

## Repository

| Field | Value |
| ----- | ----- |
| Project name | Release Ops |
| Repo path | `/home/mdguggenbichler/projects/release-ops` |
| GitHub repo | `mdg-labs/release-ops` |
| Integration branch | `dev` |
| Production branch | `main` — agents must not push here |
| Task branch (Lane P) | `orchestrator/<TASK-ID>` |
| Worktree (Lane P) | Managed by Claude Code (Agent `isolation: "worktree"` → `.claude/worktrees/…`); agent switches to the task branch |
| Plan file | `docs/roadmap.html` |
| Spec doc glob | `docs/specs.html`, `db/schema.sql`, `docs/stack.html` |

## Kaneo

| Field | Value |
| ----- | ----- |
| MCP server | `Kaneo` — Claude Code tools `mcp__Kaneo__<tool>` (server name must match) |
| Workspace | MDG-Labs (`ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj`) |
| Project | Release Ops (`z4janvyjsbbb0esishvd9gb8`) |
| Project slug | `RO` — ticket IDs are `RO-<number>` (e.g. `RO-1`, `RO-106`) |
| GitHub sync | Kaneo GitHub integration — every task mirrors to a `mdg-labs/release-ops` issue (~1–5s after create); comments, title, description, labels mirror too |
| GitHub MCP (read) | `github` — tools `mcp__github__<tool>` |

**Ticket number ≠ GitHub number.** `RO-106` is GitHub `#108`. Commits use GitHub `[#N]` from `externalLinks.externalId` — never derive `#N` from the `RO-` number, never put Kaneo task IDs in git.

### Columns / status slugs

| Slug | Column | Set by | When |
| ---- | ------ | ------ | ---- |
| `backlog` | Backlog | intake / new issue | Raw, untriaged |
| `ready` | Ready | kaneo-triage / kaneo-intake | Triaged, fully specified — orchestrator picks from here |
| `in-progress` | In Progress | execution agent; verifier on FAIL | Work started / rework |
| `in-review` | In Review | execution agent | Work done, before verifier handoff |
| `implemented` | Implemented | verifier on PASS | Commit is on `dev`, not yet on `main` |
| `done` | Done (final) | **GitHub ↔ Kaneo sync only — never an agent** | Commit with `fixes #N` / `closes #N` lands on the default branch (`main`) → GitHub closes the issue → Kaneo moves the task to Done |

There is **no `closed` column** — `done` is the only final state. Agents never set `done`.

### Task lookup (orchestrator + agents)

| User says | MCP call |
| --------- | -------- |
| `RO-106` (ticket ID) | `get_task_by_ticket_id({ ticketId: "RO-106" })` — **not** `get_task` (rejects ticket IDs with `Workspace ID could not be determined`) |
| Kaneo task ID (e.g. `lo4emygcwezpk1r8ey5lk4os`) | `get_task({ taskId })` |
| GitHub `#108` or issue URL | `list_tasks` (see below) → match `externalLinks[].externalId === "108"` (`resourceType: "issue"`). `search` does **not** match GitHub numbers. Fallback: `github` `issue_read` → the issue body ends with `<sub>Task: <kaneo-task-id></sub>` → `get_task` |
| Task → GitHub `#N` | `list_tasks` only — `get_task` / `get_task_by_ticket_id` do **not** return `externalLinks` |
| Epic subtasks | `get_task_relations({ taskId })` → `relationType: subtask` (parent = source) |
| Epic prerequisites | `get_task_relations` → `relationType: blocks` |
| Roadmap ID in description (`E01`, …) | `list_tasks` — filter description for `Roadmap ID: E01` |
| Ready queue | `list_tasks({ projectId, status: "ready" })` |
| Task title (fuzzy) | `search` (below) or `list_tasks` filter by title |

**`list_tasks` paging:** max `limit: 100`; follow `pagination.totalPages`. Tasks are nested under `data.columns[].tasks[]`. For a GitHub number, start with `sortBy: "number", sortOrder: "desc"` (recent issues are on page 1) and page on until matched.

```text
list_tasks({ projectId: "z4janvyjsbbb0esishvd9gb8", limit: 100, sortBy: "number", sortOrder: "desc", page: 1 })
```

**Epic workflow (`/orchestrator RO-1`):**

```text
1. get_task_by_ticket_id("RO-1")             # parent epic AC + Kaneo task ID
2. get_task_relations(parentTaskId)          # subtasks + blocks edges
3. get_task each leaf task ID                # leaf AC
4. list_tasks → externalLinks per leaf + parent → githubIssueNumber
5. Dispatch leaf tasks; parent in-progress on first leaf; parent implemented on last leaf PASS
   (last leaf commit carries `fixes #<parent>` too — see COMMIT CONTRACT)
```

**`search` — title/keyword discovery only:**

- Parameter is `q` (not `query`).
- Always pass `workspaceId` — `projectId` alone returns `Workspace ID could not be determined`.
- Matches ticket IDs (`RO-106`) and text; does **not** match GitHub numbers.

```text
search({ q: "scaffold", type: "tasks", workspaceId: "ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj", projectId: "z4janvyjsbbb0esishvd9gb8" })
```

### Labels

Kaneo labels are **per task** — each label row belongs to one task (`list_workspace_labels` returns one `web` row per tagged task). To tag a task, create the label on it:

```text
create_label({ name: "web", color: "#64748b", workspaceId: "ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj", taskId: "<kaneo-task-id>" })
```

Do **not** `attach_label_to_task` with a label ID taken from another task — it would move that label off its current task.

### Assignee

`whoami` → `user.id`; pass as `userId` on `create_task`. (Kaneo assignee does not mirror to the GitHub issue.)

## Domain labels

Create on each task via `create_label` with `taskId` (see § Labels) — use these exact names + colors.

| Label | Color | Scope |
| ----- | ----- | ----- |
| `backend` | `#3b82f6` | Go API, polling, providers (same container as Next.js) |
| `web` | `#64748b` | Next.js UI, COSS, Go API proxy |
| `db` | `#64748b` | SQLite schema, migrations (`app.db` — app + auth) |
| `config` | `#64748b` | App settings in DB, env bootstrap only |
| `ci` | `#64748b` | GitHub Actions, Docker, GHCR |
| `docs` | `#6b7280` | Spec and roadmap HTML docs |
| `bug` | `#d73a4a` | Defects (triage / dependabot) |
| `regression` | `#dc2626` | Recurrence of a previously fixed (done) bug |

## Area prefixes (titles)

- `E01`–`E11` — consolidated roadmap epics (one orchestrator session per epic)
- `G1` / `G2` — Grundlagen modules (legacy)
- `EL-` — epic-level orchestrator batches (when used)

## Commit conventions

- Kaneo/GitHub tasks: `[#N]` in subject
- Roadmap-only (no Kaneo mirror): `[E*-*]` or `[G*]` in subject
- Body: `fixes #N` trailer — **mandatory** on every task commit (only way an issue reaches Done); last leaf of an epic also adds `fixes #<parent>` (see `.claude/rules/07-kaneo-commit-linking.md`)

## Optional

| Field | Value |
| ----- | ----- |
| Multi-repo workspace | `../release-ops-cloud` (sibling repo; `release-ops.code-workspace` + `.claude/settings.json` `additionalDirectories`) |
| Slack session-end | not configured |
| Phase gates | see `doc-index.md` § Phase gates |
