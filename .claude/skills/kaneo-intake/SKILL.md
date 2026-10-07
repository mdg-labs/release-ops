---
name: kaneo-intake
description: >-
  Create or enrich Kaneo tasks from a feature description, codebase change, roadmap
  item, or user-drafted issue. Single task for small work; parent + subtasks for
  multi-task features. Dual mode: create net-new or enrich existing (never when
  implemented/done). Always presents a written issue proposal and waits for explicit
  user approval before any Kaneo MCP writes. Stops at Ready (ready). Use when the user
  asks to plan or ticket work, flesh out a draft issue, import roadmap epics, or
  break a change into board tasks before implementation.
---

# Kaneo intake

Turn a feature request, codebase change, roadmap epic, or rough draft into **Ready** (`ready`) tasks in the project's Kaneo board. Kaneo auto-syncs each task to a GitHub issue.

> Local Kaneo port of upstream `phasical-intake` (`mdg-labs/skills`). Not in `skills-lock.json` — edit here until upstreamed.

**Every run:** investigate → **written proposal in chat** → wait for your approval → then create in Kaneo.

## Path layout

| What | Path |
| ---- | ---- |
| This skill (installed) | `.claude/skills/kaneo-intake/SKILL.md` |
| Templates (installed) | `.claude/skills/kaneo-intake/templates.md` |
| Summary patterns (installed) | `.claude/skills/kaneo-triage/summary-patterns.md` |
| Project constants (supporting) | `.agents/project/orchestrator/project.config.md` |
| Kaneo sync (installed) | `.claude/skills/orchestrator/references/kaneo-sync.md` |
| Doc index (supporting) | `.agents/project/orchestrator/doc-index.md` |
| Intake plan drafts (supporting, gitignored) | `.agents/project/kaneo-intake/plans/` |
| Sub-agent monitoring (installed) | `.claude/skills/orchestrator/references/sub-agent-monitoring.md` |

## Parent agents: do not take over intake

If you dispatched **kaneo-intake** as a sub-agent (Agent tool), **do not** create or enrich Kaneo tasks yourself while it may still be running — even if git is quiet. MCP writes produce no commits.

Follow `.claude/skills/orchestrator/references/sub-agent-monitoring.md`:

1. Suspect stall only when there is real reason to — not because git is silent.
2. Read sub-agent **transcript** → wait **10–20s** → read again.
3. Still no progress → **terminate** the intake sub-agent → `list_tasks` for partial creates → **dedupe** → then re-dispatch once.

Taking over intake while the sub-agent is alive causes **duplicate tasks** on Kaneo/GitHub.

## When to use

| User intent                               | Action                                                    |
| ----------------------------------------- | --------------------------------------------------------- |
| Feature or change needing **2+ tasks**    | **Parent task** + child subtasks (`create_task_relation`) |
| Single task sufficient                    | **One** task — no parent                                  |
| Bug fix (one task)                        | Bug-style title; `bug` label if useful                      |
| User provides `RO-<n>`, Kaneo task ID or GitHub `#N` | **Enrich mode** — only if status is not implemented/done |
| **Full roadmap import**                   | See [Roadmap import](#roadmap-import) below               |
| User says "don't create issues"           | Skip MCP; written proposal only — no Kaneo writes        |
| Greenfield roadmap (no ticketing)         | **Do not use intake** — orchestrator reads plan file directly |

## Approval gate (mandatory — no exceptions)

**Phase 1 — investigate + written proposal only.** No Kaneo MCP writes (`create_task`, `update_task`, `update_task_status`, `create_task_relation`, `create_label`, `attach_label_to_task`).

**Phase 2 — MCP writes** only after the user explicitly approves the proposal in chat (e.g. **approve**, **yes create them**, **go ahead**, **LGTM**).

| Rule | Detail |
| ---- | ------ |
| **Always propose first** | Single task, multi-task epic, enrich, roadmap import — every run |
| **Never skip Phase 1** | Even if the user said "create issues", "ticket this", or "import roadmap" upfront |
| **Wait for reply** | End Phase 1 with a clear ask: *"Approve to create in Kaneo, or tell me what to change."* |
| **Re-propose after edits** | If the user requests changes, show an updated proposal; do not write until they approve again |
| **Sub-agent same rule** | Parent agents must not create tasks while intake is waiting for approval |

**Forbidden before approval:** any `create_task`, `update_task`, `update_task_status`, `create_task_relation`, `create_label`, `attach_label_to_task`.

Ask clarifying questions during Phase 1 if priority, owning domain, or product rules are unclear — still no MCP writes until approved.

## Kaneo status vs enrich guard

| Status | Action |
| ------ | ------ |
| `backlog`, `ready` | Enrich allowed |
| `in-progress`, `in-review` | Enrich only if user explicitly asks; do not change status |
| `implemented`, `done` | **Stop** — propose **new** tasks; cite finished work as `Related: #N — Implemented/Done` (Linear pattern) |

## Dual mode

### Mode A — Create net-new

User describes work with no existing Kaneo task.

### Mode B — Enrich existing

User names `RO-<n>`, Kaneo task ID, GitHub `#N`, or URL. Resolve per `project.config.md` § Task lookup (`get_task_by_ticket_id` for `RO-<n>`, `get_task` for task IDs, `list_tasks` `externalLinks` for `#N`). If **implemented** or **done** → stop; propose new issues instead.

## Domain routing (labels)

Read domain labels (name + color) from `project.config.md` § Domain labels. Kaneo labels are per task — tag each task with `create_label({ name, color, workspaceId, taskId })`. Do **not** `attach_label_to_task` with a label ID from another task (it moves the label).

Parent task gets the **owning** domain. Each child gets its own label.

## Assignee (mandatory on create)

Every **new** task must be assigned to the **current operator** (the Kaneo user running this session).

1. Call `whoami` once before the first `create_task` in this run (reuse `user.id` for all creates).
2. Pass `userId: <user.id>` on every `create_task` (parent, children, single-task, roadmap import).

Do not leave new tasks unassigned. Enrich-only runs (`update_task` on existing tasks) do not change assignee unless the user asks.

## Workflow

Two phases — **never skip Phase 1**.

### Phase 1 — Understand + written proposal (no MCP writes)

1. Read relevant spec docs — cite `§` sections (`.agents/project/orchestrator/doc-index.md`).
2. Search the codebase for patterns / file paths.
3. `list_tasks` for duplicates (read-only).
4. Split into **leaf tasks** — each independently implementable and verifiable.
5. **Present the written proposal** (format below) — full titles, domains, dependencies, and description outlines.
6. **Stop and wait** for explicit user approval. Do not call `whoami` or `create_task` yet.

### Phase 2 — Create in Kaneo (after approval only)

Proceed only when the user explicitly approved the proposal in their latest message.

1. `whoami` → `OPERATOR_USER_ID`
2. Execute MCP creates/updates per mode (steps below)
3. Report handoff with GitHub URLs + Kaneo taskIds

---

### Written proposal format (required every run)

Post this in chat before any Kaneo writes. For **single-task** work, omit the children table but still include title, domain, priority, and full description draft.

```markdown
## Proposed Kaneo issues — {feature or change name}

**Mode:** create net-new | enrich #N | roadmap import
**Duplicates checked:** {none | link to existing #N — why not duplicating}

### Parent (omit if single-task)

| Field | Value |
| ----- | ----- |
| Title | {title per summary-patterns.md} |
| Priority | medium |
| Domain label | {label} |
| Description | {summary or full draft — see templates.md} |

### Children / leaves

| # | Title | Domain | Priority | Depends on | Description (outline) |
| - | ----- | ------ | -------- | ---------- | --------------------- |
| 1 | {title} | backend | medium | — | {AC bullets, files, tests — key sections} |
| 2 | {title} | frontend | medium | #1 | {…} |

**Implementation order:** 1 → 2 → …
**Relations:** subtask parent→child; blocks where listed
**Enrich changes** (Mode B only): {what will change on existing #N}
**Open questions:** {any — or "none"}

---
**Approve to create these in Kaneo** (or tell me what to change).
```

**Large plans (5+ issues):** also save the same content to `.agents/project/kaneo-intake/plans/YYYY-MM-DD-<slug>.md` (gitignored) for reference — **approval in chat is still required** before Phase 2.

### 3. Create parent task (Mode A only — Phase 2)

```text
whoami
  → record user.id as OPERATOR_USER_ID

create_task
  projectId: <from project.config.md>
  title: "{Feature name}"
  description: <parent template — templates.md>
  priority: medium
  status: backlog
  userId: <OPERATOR_USER_ID>
```

Record `taskId` (Kaneo `id`) and ticket ID `RO-<number>`. After sync (~3–5s), record `githubIssueNumber` from `externalLinks` via `list_tasks` (`sortBy: number`, `sortOrder: desc`) — `get_task` does not return `externalLinks`. The `RO-` number and the GitHub number differ.

### 4. Create children (Mode A) or enrich + add children (Mode B)

```text
create_task
  projectId: <from project.config.md>
  title: "<per summary-patterns.md>"
  description: <subtask template>
  priority: medium
  status: backlog
  userId: <OPERATOR_USER_ID>

create_task_relation
  sourceTaskId: <parent-taskId>
  targetTaskId: <child-taskId>
  relationType: subtask
```

For blocking dependencies:

```text
create_task_relation
  sourceTaskId: <blocker-taskId>
  targetTaskId: <blocked-taskId>
  relationType: blocks
```

Document **Depends on** in each leaf description with GitHub URLs (`#N` only — never bare roadmap IDs).

### 5. Finalize parent description

`update_task` on parent — **Subtasks table** with every child `#N`, domain, one-line scope, URLs, and **Suggested implementation order**.

### 6. Patch dependency lines

Before setting Ready, replace any bare `P*.*` placeholders with `#N (P*.*)` (Testwatch pattern).

### 7. Transition to Ready

Intake stops at **Ready** (`ready`) — not in-progress, implemented or done.

```text
update_task_status
  taskId: <each created/enriched leaf + parent>
  status: ready
```

## Roadmap import

Use when the user asks to import a **whole roadmap** in one pass.

**Phase 1:** Present the full written proposal — every epic/parent and leaf with roadmap IDs, titles, domains, deps, and description outlines. **Wait for approval.**

**Phase 2 (after approval only):**

1. Create all **epic/parent** tasks → record each `#N` and roadmap ID.
2. Create all **leaf** tasks → record each `#N` and roadmap ID.
3. Wire `create_task_relation` (subtask + blocks).
4. **Patch dependency lines** — every `Depends on:` must use `#N (P*.*)`.
5. Set all tasks to **ready**. Execution proceeds epic-by-epic via orchestrator.

### Verification

Re-read every created description. Assert:

- No unresolved bare `P*.*` in `Depends on:` lines.
- Every leaf has parent relation.
- All tasks are `ready`.

## Description rules

- **Parent:** cross-cutting product rules, subtask table, suggested order. Children hold implementable AC.
- **Leaf:** parent link, Depends on, technical sections, AC checklist, Files, Tests.
- Include `Roadmap ID: P*-*` when mirroring plan-file tasks.

## Sizing

| Good leaf | Too big |
| --------- | ------- |
| One roadmap sub-issue | Entire phase in one task |
| One migration + entity | "All auth" |

## After creation — handoff

```markdown
Created in Kaneo ({project name from project.config.md}):

- Parent: https://github.com/{owner}/{repo}/issues/<N>
- #<N2> … / #<N3> … (all leaf GitHub URLs)

Kaneo: RO-<n> (<task-id>) … (for orchestrator MCP)

Suggested order: #N2 → #N3 → #N4
Ready for orchestrator: "implement #N3" or "orchestrate epic #N"
```

## MCP checklist

```
Phase 1 (no writes):
- [ ] Read project.config.md for workspaceId, projectId, GitHub repo
- [ ] Read MCP tool schemas under Kaneo
- [ ] list_tasks (avoid duplicates)
- [ ] Written proposal posted (titles + description outlines)
- [ ] User explicitly approved proposal in chat

Phase 2 (after approval):
- [ ] whoami → OPERATOR_USER_ID (`user.id`)
- [ ] Summaries follow summary-patterns.md
- [ ] create_task (parent) OR get_task + update_task (enrich) — **userId on every create**
- [ ] create_task × N (children)
- [ ] create_task_relation (subtask + blocks)
- [ ] create_label (domain label, with taskId) on every task
- [ ] update_task parent (subtask table + order)
- [ ] Patch Depends on lines with real #N
- [ ] list_tasks → externalLinks → githubIssueNumber for each task
- [ ] update_task_status → ready on parent + leaves
- [ ] Report GitHub URLs + Kaneo taskIds + suggested order
```

## Forbidden

- **Any Kaneo MCP write before explicit user approval** of the written proposal (no "create now" bypass)
- **Skipping the written proposal** — including single-task and roadmap import
- Proceeding to Phase 2 in the same turn as Phase 1 without user reply
- Enriching **implemented** / **done** tasks
- Setting in-progress, in-review, implemented or done during intake
- Inventing product behaviour not in spec docs — ask first
- Pasting secrets into task descriptions
- Creating a multi-task feature without a parent task
- Using Kaneo task IDs or `RO-<n>` in commit messages (use `[#N]` only)
- Leaving bare `P*.*` in `Depends on:` lines after all tasks exist
- Creating tasks without `userId` (unassigned)
