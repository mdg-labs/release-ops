---
name: kaneo-triage
description: >-
  Investigate a Release Ops bug or task read-only (Go cmd/server + internal/, web apps/web,
  db/schema.sql, docs/specs.html), then update the Kaneo task description with code findings —
  or create a new RO task when none exists. Preserves the original reporter text under
  ## Report at the top. Regressions of implemented/done work get a new task, never a
  reopened one. Stops at ready. Never writes to GitHub. Use when the user asks to triage,
  investigate or diagnose an RO-<n> task, a GitHub #N (resolved read-only to its Kaneo
  task), or reports a bug without an existing task.
---

# Kaneo triage

Read-only code investigation, then **update the Kaneo task description (and title when warranted)** — or **create a task** when the user reports a defect without one.

## Board constants

| Field | Value |
| ----- | ----- |
| MCP tools | `mcp__Kaneo__<tool>` |
| Workspace | MDG-Labs `ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj` |
| Project | Release Ops `z4janvyjsbbb0esishvd9gb8`, ticket key `RO` |
| Task URL | `https://cloud.kaneo.app/dashboard/workspace/ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj/project/z4janvyjsbbb0esishvd9gb8/task/<taskCuid>` |
| Columns | `backlog` → `ready` → `in-progress` → `in-review` → `implemented` → `done` |
| Triage stops at | `ready` (or leaves `backlog` when the user asks) |
| GitHub repo (read-only) | `mdg-labs/release-ops` |

## Path layout

| What | Path (repo root) |
| ---- | ---- |
| This skill | `.claude/skills/kaneo-triage/SKILL.md` |
| Description template | `.claude/skills/kaneo-triage/description-template.md` |
| Title patterns | `.claude/skills/kaneo-triage/summary-patterns.md` |
| Project constants | `.agents/project/orchestrator/project.config.md` |
| Doc index | `.agents/project/orchestrator/doc-index.md` |
| Implementation (consumes `ready` tasks) | `.claude/skills/orchestrate/SKILL.md` |

## Parent agents: do not take over triage

If **kaneo-triage** runs as a sub-agent, the parent must not `update_task` / `create_task` while it may still be running. Git silence is normal during investigation. Wait for its completion notification; to replace it, stop it first (`TaskStop`), dedupe what it wrote, then re-dispatch.

## When to use

| User intent | Action |
| ----------- | ------ |
| "Triage RO-108", "investigate #50", Kaneo task URL | **Update mode** |
| Bug report, no task yet | **Create mode** — `create_task` with the triage description |
| "Don't change code" / "investigate only" | Default — triage never changes code |
| "Fix it" after triage | Separate implementation pass (`/orchestrate RO-<n>`) |
| "Don't update Kaneo" | Findings in chat only — no Kaneo writes |
| Multi-task feature breakdown | Use `/kaneo-intake` instead |

## Hard rules

1. **Read-only investigation.** No code edits, no commits, no branch changes in a triage run.
2. **Findings go in the description** via `update_task` — never as `create_task_comment` (comments are reserved for verifier PASS/FAIL).
3. **Preserve the reporter text verbatim** under `## Report` at the top. If the task already has a `## Report` section, keep it unchanged; otherwise move the existing description there.
4. **Title** follows [summary-patterns.md](summary-patterns.md) — rewrite when vague, typo'd or mis-scoped; keep when accurate.
5. **Status:** after triage, `backlog` → `ready` unless the user opted out. Never set `in-progress`, `in-review`, `implemented` or `done`.
6. **Regression:** if the defect was fixed on a task that is `implemented` or `done`, create a **new** task with `bug` + `regression` labels and a `## Regression` section linking the old RO key — never reopen, move or edit the old task.
7. **GitHub is read-only.** Never create, comment on, label, assign, edit or close GitHub issues.
8. **No secrets** (tokens, `.env` values, API keys) in descriptions.
9. **Assign new tasks** to the operator: `whoami` → `userId` on `create_task`.
10. **Ask before guessing** when behaviour is not defined in the spec docs — record it under `## Open questions` rather than inventing it.

## Lookup (Step 1)

| Input | Call |
| ----- | ---- |
| `RO-<n>` | `get_task_by_ticket_id` `{ ticketId: "RO-<n>" }` — **no** `projectId` (passing it 404s) |
| Kaneo task URL | take the trailing CUID → `get_task` `{ taskId: "<cuid>" }` |
| GitHub `#N` | `mcp__github__issue_read` (read-only, `mdg-labs/release-ops`) → title → `search` `{ q: "<title>", workspaceId: "ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj", projectId: "z4janvyjsbbb0esishvd9gb8", type: "tasks" }` |
| Title / keywords | `search` with `q` (not `query`), same workspace/project IDs |

`get_task` takes the CUID only — `get_task("RO-108")` fails. Record: CUID, `RO-<number>`, title, description, status.

Kaneo payloads have so far carried no GitHub link. To report the mirrored GitHub issue (used for the `[#N]` commit subject and the `fixes #N` body trailer), use `externalLinks[].externalId` if the payload ever carries it; otherwise `mcp__github__search_issues` on `mdg-labs/release-ops` with the exact task title and take the exact-title match — read only, and report "no GitHub mirror" if none.

## Workflow

```text
Triage progress:
- [ ] 1 Resolve the task (or confirm none exists → create mode)
- [ ] 2 Lock the original ## Report text
- [ ] 3 Investigate spec + code (read-only)
- [ ] 4 Duplicate / regression check
- [ ] 5 Compose description (description-template.md) + title
- [ ] 6 update_task (update mode) or whoami + create_task (create mode)
- [ ] 7 Labels
- [ ] 8 Status backlog → ready
- [ ] 9 Summarize in chat
```

### Step 3 — Investigation sources

1. Spec, in precedence order: `docs/specs.html` (contract, APIs, UI, domain logic) > `db/schema.sql` (canonical DDL) > `docs/stack.html` (tooling). Cite `§` sections. Use `.agents/project/orchestrator/doc-index.md` to find the right section.
2. Code (read-only):
   - Go: `cmd/server`, `internal/api/` (handlers, auth, middleware), `internal/poll/` (scheduler), `internal/providers/` (release sources: GitHub, GitLab, Gitea, Forgejo, Codeberg; ticket integrations), `internal/store/` (sqlc), `internal/config/`, `internal/crypto/`, `internal/mail/`, `internal/tickettemplate/`
   - Web: `apps/web/` (`app/`, `components/`, `lib/`, `messages/en.json`)
   - Schema: `db/schema.sql`, `migrations/` (generated — any fix goes through `db/schema.sql` + `make migrate-diff`)
   - Tests next to the code (`*_test.go`, `*.test.ts`)
3. Related tasks: `search` (`q`), `list_tasks` `{ projectId: "z4janvyjsbbb0esishvd9gb8" }`, `get_task_relations`.
4. Git history (`git log`, `git blame`) — read-only.

You may run read-only checks (`go test ./...`, `npm test`) to confirm a hypothesis; never edit files to do so.

### Step 4 — Duplicate / regression check

`search` `{ q: "<key terms>", workspaceId, projectId, type: "tasks" }`, plus `list_tasks` filtered by status when needed.

| Match | Action |
| ----- | ------ |
| Open duplicate (`backlog` … `in-review`) | Report the existing RO key; do not create a duplicate |
| `implemented` / `done` task fixed this, and it recurs | **Create mode** with `bug` + `regression` labels and a `## Regression` section; optionally `create_task_relation` `related` to the old task |
| No match | Continue |

### Step 5 — Compose

Follow [description-template.md](description-template.md) (shape of RO-108). The proposed acceptance criteria must list the gates for the touched area (`go test ./...`, `golangci-lint run`, `npm test`, `npm run lint`, `npm run db:check`) and, for schema changes, "schema change via `make migrate-diff` only".

### Step 6 — Write

Update mode:

```text
mcp__Kaneo__update_task
  taskId: <cuid>
  description: <composed markdown>
  title: <revised title, only when warranted>
```

`update_task` merges fields — pass only what changes.

Create mode:

```text
mcp__Kaneo__whoami
  → OPERATOR_USER_ID = user.id

mcp__Kaneo__create_task
  projectId: z4janvyjsbbb0esishvd9gb8
  title: <Area>: <observed defect>
  description: <composed markdown, ## Report = user's message verbatim>
  priority: medium
  status: backlog
  userId: <OPERATOR_USER_ID>
```

### Step 7 — Labels

Domain labels: `backend`, `web`, `db`, `config`, `ci`, `docs`; plus `bug` for defects and `regression` for escaped defects.

1. `list_workspace_labels` `{ workspaceId: "ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj" }`.
2. Name exists → attach: `attach_label_to_task` with a row whose `taskId` is `null`; if all rows of that name are bound to other tasks, `create_label` `{ name, color: <existing color>, workspaceId, taskId }` (same name and color — not a new label). Never re-attach a row bound to another task.
3. Name does not exist → ask the user before creating it.

Skip labels the task already has.

### Step 8 — Status

| Current status | Action |
| -------------- | ------ |
| `backlog` | `update_task_status` → `ready` (unless the user wants it kept in `backlog`) |
| `ready` | none |
| `in-progress` / `in-review` / `implemented` / `done` | none |

### Step 9 — Chat summary

Report: RO key + task URL, classification, top suspects, open questions, label/status changes, and the read-only GitHub mirror number (or "none").

## Kaneo tools used

| Tool | Purpose |
| ---- | ------- |
| `get_task_by_ticket_id` | Resolve `RO-<n>` |
| `get_task` | Resolve CUID (from URL) |
| `search` (`q`) | Find by title / keywords; duplicates |
| `list_tasks`, `get_task_relations` | Related work, regressions |
| `update_task` | Write description (+ title) |
| `whoami`, `create_task` | Create mode (assigned to operator) |
| `list_workspace_labels`, `attach_label_to_task`, `create_label` | Labels |
| `create_task_relation` (`related`) | Optional link from a regression to the original task |
| `update_task_status` | `backlog` → `ready` |

GitHub (read-only): `issue_read`, `search_issues`.

## Forbidden

- Code edits, commits or staging during triage
- Findings as `create_task_comment`
- Paraphrasing or dropping the `## Report` text
- Setting `in-progress`, `in-review`, `implemented` or `done`
- Reopening, moving or editing an `implemented` / `done` task for a regression
- Any GitHub write
- `create_task` without `userId`; creating a new label name without approval
- Secrets in descriptions
