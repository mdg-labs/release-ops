---
name: kaneo-triage
description: >-
  Investigate a bug or task read-only, then update the Kaneo task description with
  code findings — or create a new task when no issue exists. Preserves the original
  reporter text under ## Report at the top. Regression defects get a new task, not
  a reopened done task. Use when the user asks to triage, investigate, or diagnose
  a GitHub issue (e.g. #50), Kaneo task, or reports a bug without an existing key.
---

# Kaneo triage

> Local Kaneo port of upstream `phasical-triage` (`mdg-labs/skills`). Not in `skills-lock.json` — edit here until upstreamed.

Read-only codebase investigation, then **update the Kaneo task description and title** — or **create a task** when the user reports a defect without an existing key. Comments on Kaneo mirror to GitHub.

## Path layout

| What | Path |
| ---- | ---- |
| This skill (installed) | `.claude/skills/kaneo-triage/SKILL.md` |
| Description template (installed) | `.claude/skills/kaneo-triage/description-template.md` |
| Summary patterns (installed) | `.claude/skills/kaneo-triage/summary-patterns.md` |
| Project constants (supporting) | `.agents/project/orchestrator/project.config.md` |
| Kaneo sync (installed) | `.claude/skills/orchestrator/references/kaneo-sync.md` |
| Doc index (supporting) | `.agents/project/orchestrator/doc-index.md` |
| Sub-agent monitoring (installed) | `.claude/skills/orchestrator/references/sub-agent-monitoring.md` |

## Parent agents: do not take over triage

If **kaneo-triage** runs as a sub-agent, the parent must not `update_task` / `create_task` while it may still be running. Git silence is normal during investigation. Follow `sub-agent-monitoring.md` (transcript two-sample → terminate → dedupe → re-dispatch).

## When to use

| User intent                                     | Action                                               |
| ----------------------------------------------- | ---------------------------------------------------- |
| "Triage #50", "investigate this bug", issue URL | **Update mode** — full workflow below                |
| Bug report, no issue yet                        | **Create mode** — `create_task` + triage description   |
| "Don't change code" / "investigate only"        | Read-only — no commits, no fixes                     |
| "Fix it" after triage                           | Separate implementation pass (orchestrator)          |
| "Don't update Kaneo"                            | Skip all Kaneo writes                                |

## Hard rules

1. **No implementation in a triage run** — read-only codebase investigation only.
2. **Update `description` via `update_task`** — never post investigation findings as `create_task_comment` (comments are for verifier PASS/FAIL summaries only).
3. **Preserve the original report** under `## Report` at the top (verbatim reporter wording).
4. **Title follows [summary-patterns.md](summary-patterns.md)** — rewrite when vague/typo/mis-scoped; keep when already correct.
5. **After successful triage:** transition **backlog → ready** unless user opted out or task is already ready / in-progress / in-review / implemented / done.
6. **Never** transition to in-progress, in-review, implemented, or done — execution / verifier / GitHub sync own those statuses.
7. **Regression:** if the same bug was fixed on a task with status **implemented** or **done**, create a **new** Bug task with `regression` label — do **not** re-open or enrich the old task (Linear/GitLab pattern).
8. **Never put secret plaintext** in task descriptions (EnvHub pattern).
9. **Assign every new task to the current operator** — `whoami` → `userId` on `create_task` (see [Assignee](#assignee-mandatory-on-create)).

## Assignee (mandatory on create)

Every **new** task must be assigned to the **current operator** (the Kaneo user running this session).

1. Call `whoami` once before `create_task` in create mode (or regression create).
2. Pass `userId: <user.id>` on `create_task`.

Do not leave new bug/regression tasks unassigned. Update mode on existing tasks does not change assignee unless the user asks.

## Board constants

Read from `.agents/project/orchestrator/project.config.md`:

```text
MCP server: Kaneo  (Claude Code tools: mcp__Kaneo__<tool>)
workspaceId: <from project.config.md>
projectId: <from project.config.md>
Ready status slug: ready
```

## Dual mode

### Update mode (existing issue)

User provides GitHub `#N`, issue URL, ticket ID `RO-<n>`, or Kaneo task ID.

**Enrich guard:** If task status is **implemented** or **done** and the user reports a regression → **stop** update mode; switch to **create mode** with regression label and link to original `#N`.

### Create mode (new Bug)

```text
whoami
  → record user.id as OPERATOR_USER_ID

create_task
  projectId: <from project.config.md>
  title: "<Area>: <observed defect>"
  description: <template with ## Report = user message>
  priority: medium
  status: backlog
  userId: <OPERATOR_USER_ID>
```

After create: wait for sync, resolve `githubIssueNumber` via `list_tasks` `externalLinks`, add `bug` (+ `regression` if applicable) via `create_label` with `taskId` (colors in `project.config.md`), transition to **ready**.

## Workflow (update mode)

```text
Triage progress:
- [ ] Step 0: whoami → OPERATOR_USER_ID (create mode only)
- [ ] Step 1: Resolve task from Kaneo or GitHub
- [ ] Step 2: Extract and lock original ## Report text
- [ ] Step 3: Investigate codebase (read-only)
- [ ] Step 4: Regression check — duplicate search via list_tasks
- [ ] Step 5: Compose description from template; draft title per summary-patterns.md
- [ ] Step 6: update_task → description (+ title when changed)
- [ ] Step 7: update_task_status → ready if status is backlog
- [ ] Step 8: Summarize findings in chat
```

### Step 1 — Fetch task

| Input          | MCP call                                                                                                                                    |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| GitHub `#50`   | `list_tasks` (paged, max 100, `sortBy: number desc`) + match `externalLinks[].externalId === "50"`; fallback `github` `issue_read` → body footer `<sub>Task: <id></sub>` → `get_task`                                |
| `RO-<n>`       | `get_task_by_ticket_id({ ticketId: "RO-<n>" })` — **not** `get_task`                                                                          |
| Kaneo task ID  | `get_task`                                                                                                                                  |
| Task title     | `list_tasks` filter by title                                                                                                                |

Record: taskId, ticket ID, title, current description, status, githubIssueNumber (from `list_tasks` `externalLinks` — `get_task` omits it; `RO-<n>` ≠ `#N`).

### Step 4 — Regression check

```text
list_tasks with projectId — search title/description for similar defects
```

| Match status | Action |
| ------------ | ------ |
| Open duplicate (not implemented/done) | Report existing `#N`; do not create duplicate |
| implemented/done duplicate + user reports recurrence | **Create mode** with `regression` label |
| No duplicate | Continue update or create |

### Step 6 — Update Kaneo

```text
mcp__Kaneo__update_task
  taskId: <kaneo-task-id>
  description: "<composed markdown>"
  title: "<revised title when warranted>"
```

### Step 7 — Transition to Ready

| Current status                 | Action             |
| ------------------------------ | ------------------ |
| backlog                        | Transition → ready |
| ready                          | No transition      |
| in-progress / in-review / implemented / done | Skip transition |

## Investigation sources

1. Spec docs per `.agents/project/orchestrator/doc-index.md`
2. Codebase search (read-only)
3. Related tasks via `list_tasks` / `get_task_relations`

## What triage does not do

- Set **in-progress**, **in-review**, **implemented**, or **done**
- Create epic breakdown (use `.claude/skills/kaneo-intake/SKILL.md`)
- Commit code or session memory
- Modify repo files during triage (unless user explicitly requests implementation)

## MCP tools used

| Tool                 | Purpose                                    |
| -------------------- | ------------------------------------------ |
| `whoami`             | Current operator `user.id` for assignee    |
| `get_task_by_ticket_id` | Resolve `RO-<n>`                        |
| `get_task`           | Resolve task by Kaneo task ID              |
| `list_tasks`         | Find by GitHub number (`externalLinks`), title, duplicates |
| `update_task`        | Write investigation to description + title |
| `create_task`        | Create mode — new bug task                 |
| `update_task_status` | backlog → ready after successful triage    |
| `create_label` (with `taskId`) | `bug` / `regression` label when applicable |

**Forbidden during triage:** `create_task_comment` for findings; transitions to in-progress / in-review / implemented / done; `create_task` without `userId`.
