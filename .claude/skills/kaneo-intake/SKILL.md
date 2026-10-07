---
name: kaneo-intake
description: >-
  Create or enrich Release Ops (RO) tasks on the Kaneo board from a feature description,
  codebase change, roadmap item (docs/roadmap.json E*-*), or user-drafted issue. Single task
  for small work; parent + subtasks (create_task_relation subtask / blocks) for multi-task
  features. Dual mode: create net-new or enrich an existing RO-<n> (never when implemented or
  done). Always presents a written proposal and waits for explicit user approval before any
  Kaneo write. Stops at ready (or backlog on request). Never writes to GitHub. Use when the
  user asks to plan or ticket work, flesh out a draft task, import roadmap epics, or break a
  change into board tasks before implementation.
---

# Kaneo intake

Turn a feature request, codebase change, roadmap epic or rough draft into **`ready`** tasks on the Release Ops Kaneo board.

**Every run:** investigate → **written proposal in chat** → wait for approval → then write to Kaneo.

## Board constants

| Field | Value |
| ----- | ----- |
| MCP tools | `mcp__Kaneo__<tool>` (or `mcp__claude_ai_Kaneo__<tool>` via the claude.ai connector) |
| Workspace | MDG-Labs `ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj` |
| Project | Release Ops `z4janvyjsbbb0esishvd9gb8`, ticket key `RO` |
| Task URL | `https://cloud.kaneo.app/dashboard/workspace/ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj/project/z4janvyjsbbb0esishvd9gb8/task/<taskCuid>` |
| Columns | `backlog` → `ready` → `in-progress` → `in-review` → `implemented` → `done` |
| Intake stops at | `ready` (or `backlog` when the user asks) |
| GitHub repo (read-only) | `mdg-labs/release-ops` |

## Path layout

| What | Path (repo root) |
| ---- | ---- |
| This skill | `.claude/skills/kaneo-intake/SKILL.md` |
| Description templates | `.claude/skills/kaneo-intake/templates.md` |
| Title patterns | `.claude/skills/kaneo-triage/summary-patterns.md` |
| Project constants | `.agents/project/orchestrator/project.config.md` |
| Doc index | `.agents/project/orchestrator/doc-index.md` |
| Implementation (consumes `ready` tasks) | `.claude/skills/orchestrate/SKILL.md` — its readiness gate checks the shape in `templates.md` |
| Intake plan drafts (gitignored) | `.agents/project/kaneo-intake/plans/` |
| Roadmap source / view | `docs/roadmap.json` → `docs/roadmap.html` |

## Kaneo lookup rules

| Input | Call |
| ----- | ---- |
| `RO-<n>` | `get_task_by_ticket_id` `{ ticketId: "RO-<n>" }` — **no** `projectId` (passing it 404s) |
| Task CUID | `get_task` `{ taskId: "<cuid>" }` — never pass `RO-<n>` to `get_task` |
| Free text / title | `search` `{ q: "<text>", workspaceId: "ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj", projectId: "z4janvyjsbbb0esishvd9gb8", type: "tasks" }` — the parameter is `q`, not `query` |
| GitHub `#N` from the user | `mcp__github__issue_read` (read-only, `mdg-labs/release-ops`) for the title → `search` Kaneo with `q` = that title |
| Browse / dedupe | `list_tasks` `{ projectId: "z4janvyjsbbb0esishvd9gb8", status?, page?, limit? }` — paginate via `pagination.totalPages` |
| Relations | `get_task_relations` `{ taskId: "<cuid>" }` |

Kaneo task payloads have so far carried **no** GitHub link. The mirrored GitHub issue number (used for the `[#N]` commit subject and the `fixes #N` body trailer) is resolved **read-only**: `externalLinks[].externalId` if the payload ever carries it; otherwise `mcp__github__search_issues` or `gh issue list --repo mdg-labs/release-ops --state all --search "<title> in:title"` with the exact task title → take the exact-title match. If none matches, report "no GitHub mirror" — never create one.

## GitHub: read-only

This skill **never** creates, comments on, labels, assigns, edits or closes GitHub issues. GitHub is only read to resolve a `#N` the user gives, or to report the mirrored issue number in the handoff.

## Parent agents: do not take over intake

If you dispatched **kaneo-intake** as a sub-agent, **do not** create or enrich Kaneo tasks yourself while it may still be running — even if git is quiet. Kaneo writes produce no commits.

Wait for its completion notification instead of polling. To replace it: stop it first (`TaskStop`) and confirm it stopped → `list_tasks` / `search` for partial creates → **dedupe** → re-dispatch once.

Taking over while the sub-agent is alive causes **duplicate RO tasks**.

## When to use

| User intent | Action |
| ----------- | ------ |
| Feature or change needing **2+ tasks** | **Parent task** + child subtasks (`create_task_relation` `subtask`) |
| Single task is enough | **One** task — no parent |
| Bug fix (one task) | Bug-style title, `bug` label + domain label (prefer `/kaneo-triage` when it needs code investigation) |
| User gives `RO-<n>`, task URL or GitHub `#N` | **Enrich mode** — only if status allows (see guard) |
| Roadmap import (`E*` epics from `docs/roadmap.json`) | See [Roadmap import](#roadmap-import) |
| User says "don't create tasks" / "proposal only" | Written proposal only — no Kaneo writes |

## Approval gate (mandatory — no exceptions)

**Phase 1 — investigate + written proposal.** No Kaneo writes.

**Phase 2 — Kaneo writes** only after the user explicitly approves the proposal in chat (e.g. **approve**, **yes create them**, **go ahead**, **LGTM**).

| Rule | Detail |
| ---- | ------ |
| Always propose first | Single task, epic, enrich, roadmap import — every run |
| Never skip Phase 1 | Even if the user said "ticket this" or "import the roadmap" upfront |
| Wait for reply | End Phase 1 with: *"Approve to create in Kaneo, or tell me what to change."* |
| Re-propose after edits | Show the updated proposal; do not write until approved again |
| Sub-agent same rule | Parent agents must not create tasks while intake waits for approval |

**Kaneo writes (forbidden before approval):** `create_task`, `update_task`, `update_task_status`, `move_task`, `create_task_relation`, `create_label`, `attach_label_to_task`, `update_task_assignee`.

Ask clarifying questions in Phase 1 when priority, owning domain or product behaviour is unclear — still no writes until approved.

## Status guard (enrich)

| Status | Action |
| ------ | ------ |
| `backlog`, `ready` | Enrich allowed |
| `in-progress`, `in-review` | Enrich only if the user explicitly asks; never change status |
| `implemented`, `done` | **Stop** — propose a **new** task; cite the finished one as `Related: RO-<n> (implemented/done)` |

## Spec sources (precedence when docs conflict)

1. `docs/specs.html` — MVP contract, APIs, UI, domain logic
2. `db/schema.sql` — canonical DDL (`docs/schema.html` is a browser view only)
3. `docs/stack.html` — tooling choices

`docs/index.html` is the doc hub; `docs/roadmap.html` (generated from `docs/roadmap.json`) is the historical plan the board was seeded from. If behaviour is not defined in a spec doc, **ask** — never invent fields, endpoints or IDs. Cite `§` sections in descriptions (see `.agents/project/orchestrator/doc-index.md`).

## Code layout (for Files / Key files sections)

| Area | Path |
| ---- | ---- |
| Go entrypoints | `cmd/server`, `cmd/seed-admin` |
| Go API, auth, middleware | `internal/api/` (`handlers/`, `auth/`, `middleware/`) |
| Polling scheduler | `internal/poll/` |
| Release + ticket providers | `internal/providers/` |
| Persistence (sqlc) | `internal/store/` |
| Config, crypto, mail, ticket templates | `internal/config/`, `internal/crypto/`, `internal/mail/`, `internal/tickettemplate/` |
| Schema / migrations | `db/schema.sql` (edit) → `migrations/` (generated by `make migrate-diff`) |
| Web UI (Next.js + COSS, next-intl) | `apps/web/` (`app/`, `components/`, `lib/`, `messages/en.json`) |

Sibling repo `../release-ops-cloud` (control plane, billing) is out of scope unless the user says otherwise.

## Labels

Domain labels: **`backend`**, **`web`**, **`db`**, **`config`**, **`ci`**, **`docs`**. Plus `bug` for defects and `regression` for escaped defects.

| Label | Scope |
| ----- | ----- |
| `backend` | Go API, polling, providers (`cmd/server`, `internal/`) |
| `web` | Next.js UI, COSS, i18n (`apps/web`) |
| `db` | `db/schema.sql`, generated migrations, sqlc queries |
| `config` | App settings in DB, env bootstrap |
| `ci` | GitHub Actions, Docker, GHCR |
| `docs` | `docs/*.html`, spec and roadmap |

Parent gets the **owning** domain; each child gets its own.

Kaneo labels are workspace rows bound to a task. Before labelling:

1. `list_workspace_labels` `{ workspaceId: "ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj" }` — find rows with the wanted name and note their color.
2. Name already exists → attach it: `attach_label_to_task` with a row whose `taskId` is `null`; if every row of that name is already bound to another task, call `create_label` `{ name, color: <existing color>, workspaceId, taskId: <new task> }` (same name and color — not a new label). Never re-attach a row that is bound to another task.
3. Name does **not** exist yet (e.g. `config` or `ci` on first use) → list it under **Open questions** in the proposal; create it only after the user approves that label.

Never invent label names outside the list above without approval.

## Assignee (mandatory on create)

Every **new** task is assigned to the operator running the session.

1. Phase 2 only: `whoami` once → `OPERATOR_USER_ID` (`user.id`).
2. Pass `userId: OPERATOR_USER_ID` on every `create_task`.

Enrich runs do not change the assignee unless the user asks (`update_task_assignee`).

## Workflow

### Phase 1 — understand + written proposal (no writes)

1. Read the relevant spec sections (precedence above) and the roadmap row when mirroring `docs/roadmap.json`.
2. Search the code (`cmd/server`, `internal/`, `apps/web`, `db/schema.sql`) for patterns and file paths.
3. Dedupe: `search` (`q` = key terms, `type: "tasks"`, workspace + project IDs) and/or `list_tasks`.
4. Enrich mode: fetch the task (`get_task_by_ticket_id`) and apply the status guard.
5. `list_workspace_labels` — note which labels exist.
6. Split into **leaf tasks**, each independently implementable and verifiable.
7. Post the written proposal (format below). For 5+ tasks also save it to `.agents/project/kaneo-intake/plans/YYYY-MM-DD-<slug>.md`.
8. **Stop and wait** for approval.

### Phase 2 — write to Kaneo (after approval only)

1. `whoami` → `OPERATOR_USER_ID`.
2. Create / enrich per mode (below).
3. Wire relations, labels, finalize parent, set status.
4. Re-read every created/edited task and verify (checklist below).
5. Handoff report.

### Written proposal format (required every run)

For single-task work omit the children table but keep title, labels, priority and the full description draft.

```markdown
## Proposed Kaneo tasks — <feature or change name>

**Mode:** create net-new | enrich RO-<n> | roadmap import
**Duplicates checked:** none found | RO-<n> — why not a duplicate
**Target status:** ready | backlog

### Parent (omit if single task)

| Field | Value |
| ----- | ----- |
| Title | <per summary-patterns.md> |
| Priority | medium |
| Labels | backend |
| Description | <draft per templates.md> |

### Children / leaves

| # | Title | Labels | Priority | Depends on | Description outline |
| - | ----- | ------ | -------- | ---------- | ------------------- |
| 1 | Add Kaneo column discovery endpoint | backend | medium | — | AC, files, gates |
| 2 | Show Kaneo columns in project drawer | web | medium | 1 | AC, files, gates |

**Implementation order:** 1 → 2
**Relations:** subtask parent → each child; blocks 1 → 2
**Enrich changes** (enrich mode only): what changes on RO-<n>
**New labels needing approval:** none | config, ci
**Open questions:** none | …

---
**Approve to create in Kaneo, or tell me what to change.**
```

### Create parent (create mode, Phase 2)

```text
mcp__Kaneo__create_task
  projectId: z4janvyjsbbb0esishvd9gb8
  title: <Feature name>
  description: <parent template — templates.md>
  priority: medium
  status: backlog
  userId: <OPERATOR_USER_ID>
```

Record the returned `id` (CUID) and `number` (→ `RO-<number>`).

### Create children / enrich + add children

```text
mcp__Kaneo__create_task
  projectId: z4janvyjsbbb0esishvd9gb8
  title: <per summary-patterns.md>
  description: <leaf template — templates.md>
  priority: medium
  status: backlog
  userId: <OPERATOR_USER_ID>

mcp__Kaneo__create_task_relation
  sourceTaskId: <parent CUID>
  targetTaskId: <child CUID>
  relationType: subtask

mcp__Kaneo__create_task_relation      # blocking dependency
  sourceTaskId: <blocker CUID>
  targetTaskId: <blocked CUID>
  relationType: blocks
```

Enrich mode: `update_task` `{ taskId: <cuid>, description, title? }` — `update_task` merges fields, so pass only what changes.

Write `Depends on:` / `Blocks:` lines with **RO keys** (`RO-<n>`), never bare roadmap IDs or CUIDs.

### Finalize parent

`update_task` on the parent: Subtasks table with every child `RO-<n>`, label, one-line scope, and the suggested implementation order.

### Patch dependency lines

Replace any placeholder (`#1`, `E03-02`) in `Depends on:` lines with the real key: `RO-<n> (E03-02)`.

### Set status

```text
mcp__Kaneo__update_task_status
  taskId: <each created/enriched CUID — parent + leaves>
  status: ready            # or backlog if the user asked
```

Never set `in-progress`, `in-review`, `implemented` or `done`.

## Roadmap import

Use when the user asks to import roadmap epics (`E01`–`E11`, leaves `E*-*`) from `docs/roadmap.json`.

**Phase 1:** full proposal — every epic and leaf with roadmap ID, title, labels, deps and description outline. Save to `.agents/project/kaneo-intake/plans/`. **Wait for approval.**

**Phase 2:**

1. Create every epic/parent task → record `RO-<n>` ↔ roadmap ID.
2. Create every leaf → record `RO-<n>` ↔ roadmap ID.
3. Wire `create_task_relation` (`subtask` + `blocks`).
4. Patch every `Depends on:` to `RO-<n> (E*-*)`.
5. Set all to `ready` (or `backlog`).

## Description rules

- **Parent:** background, subtask table, epic-level product rules, suggested order. Children hold the implementable AC.
- **Leaf:** parent link, Depends on / Blocks, AC checklist (including the gates), Files, Tests, implementation notes.
- Include `Roadmap ID: E*-*` when mirroring a roadmap row.
- AC must include the relevant gates and, for schema work, the migration rule — see templates.md.
- Never paste secrets, tokens or `.env` values.

## Sizing

| Good leaf | Too big |
| --------- | ------- |
| One roadmap leaf (`E03-02`) | A whole epic in one task |
| One schema change + its sqlc queries + handler | "All integrations" |
| One provider (e.g. Gitea polling) | "All release sources" |

## Verification (Phase 2)

Re-read each task (`get_task`) and its relations (`get_task_relations`). Assert:

- Every leaf has a `subtask` relation from its parent; `blocks` relations match `Depends on:`.
- No placeholder or bare roadmap ID left in `Depends on:` lines.
- Every new task has `userId` = operator and the agreed labels.
- Status is `ready` (or `backlog` when requested) on all created/enriched tasks.

## Handoff

```markdown
Created in Kaneo (Release Ops):

- Parent: RO-<n> — <title> — https://cloud.kaneo.app/dashboard/workspace/ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj/project/z4janvyjsbbb0esishvd9gb8/task/<cuid>
- RO-<n2> — <title> — <url>
- RO-<n3> — <title> — <url>

GitHub mirror (read-only lookup): RO-<n2> → #<N> | not found
Status: ready
Suggested order: RO-<n2> → RO-<n3>
Next: "implement RO-<n2>" or "orchestrate RO-<n>"
```

## Checklist

```text
Phase 1 (no writes):
- [ ] Spec read (specs.html > schema.sql > stack.html) and code searched
- [ ] Duplicates checked via search (q) / list_tasks
- [ ] Enrich: status guard applied (no implemented/done)
- [ ] list_workspace_labels; new labels flagged for approval
- [ ] Written proposal posted; plan saved if 5+ tasks
- [ ] User explicitly approved

Phase 2:
- [ ] whoami → OPERATOR_USER_ID
- [ ] create_task (userId on every create) / update_task (enrich)
- [ ] create_task_relation subtask + blocks
- [ ] Labels attached (approved new labels only)
- [ ] Parent subtask table + order; Depends on lines use RO-<n>
- [ ] update_task_status → ready (or backlog)
- [ ] Verification re-read
- [ ] Handoff with RO keys, URLs, read-only GitHub mirror numbers
```

## Forbidden

- Any Kaneo write before explicit approval; skipping the written proposal; Phase 2 in the same turn as Phase 1
- Enriching `implemented` / `done` tasks
- Setting `in-progress`, `in-review`, `implemented` or `done`
- Any GitHub write (issues, comments, labels, assignees, state)
- Inventing behaviour, fields or endpoints not in the spec docs — ask first
- A multi-task feature without a parent task
- Creating tasks without `userId`
- Creating a new label name without user approval
- Secrets in descriptions
- Kaneo CUIDs or RO keys in commit messages (commits use `[#N]` or `[E*-*]`)
