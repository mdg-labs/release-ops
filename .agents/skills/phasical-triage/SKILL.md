---
name: phasical-triage
description: >-
  Investigate a bug or task read-only, then update the Phasical task description with
  code findings — or create a new task when no issue exists. Preserves the original
  reporter text under ## Report at the top. Regression defects get a new task, not
  a reopened done task. Use when the user asks to triage, investigate, or diagnose
  a GitHub issue (e.g. #50), Phasical task, or reports a bug without an existing key.
---

# Phasical triage

Read-only codebase investigation, then **update the Phasical task description and title** — or **create a task** when the user reports a defect without an existing key. Comments on Phasical mirror to GitHub.

## Path layout

| What | Path |
| ---- | ---- |
| This skill (installed) | `.agents/skills/phasical-triage/SKILL.md` |
| Description template (installed) | `.agents/skills/phasical-triage/description-template.md` |
| Summary patterns (installed) | `.agents/skills/phasical-triage/summary-patterns.md` |
| Project constants (supporting) | `.agents/project/orchestrator/project.config.md` |
| Phasical sync (installed) | `.agents/skills/orchestrator/references/phasical-sync.md` |
| Doc index (supporting) | `.agents/project/orchestrator/doc-index.md` |
| Sub-agent monitoring (installed) | `.agents/skills/orchestrator/references/sub-agent-monitoring.md` |

## Parent agents: do not take over triage

If **phasical-triage** runs as a sub-agent, the parent must not `update_task` / `create_task` while it may still be running. Git silence is normal during investigation. Follow `sub-agent-monitoring.md` (transcript two-sample → terminate → dedupe → re-dispatch).

## When to use

| User intent                                     | Action                                               |
| ----------------------------------------------- | ---------------------------------------------------- |
| "Triage #50", "investigate this bug", issue URL | **Update mode** — full workflow below                |
| Bug report, no issue yet                        | **Create mode** — `create_task` + triage description   |
| "Don't change code" / "investigate only"        | Read-only — no commits, no fixes                     |
| "Fix it" after triage                           | Separate implementation pass (orchestrator)          |
| "Don't update Phasical"                            | Skip all Phasical writes                                |

## Hard rules

1. **No implementation in a triage run** — read-only codebase investigation only.
2. **Update `description` via `update_task`** — never post investigation findings as `create_task_comment` (comments are for verifier PASS/FAIL summaries only).
3. **Preserve the original report** under `## Report` at the top (verbatim reporter wording).
4. **Title follows [summary-patterns.md](summary-patterns.md)** — rewrite when vague/typo/mis-scoped; keep when already correct.
5. **After successful triage:** transition **backlog → to-do** unless user opted out or task is already in-progress / in-review / done / closed.
6. **Never** transition to in-progress, in-review, or done — orchestrator / execution / verifier own those statuses.
7. **Regression:** if the same bug was fixed on a task with status **done** or **closed**, create a **new** Bug task with `regression` label — do **not** re-open or enrich the old task (Linear/GitLab pattern).
8. **Never put secret plaintext** in task descriptions (EnvHub pattern).
9. **Assign every new task to the current operator** — `whoami` → `userId` on `create_task` (see [Assignee](#assignee-mandatory-on-create)).

## Assignee (mandatory on create)

Every **new** task must be assigned to the **current operator** (the Phasical user running this session).

1. Call `whoami` once before `create_task` in create mode (or regression create).
2. Pass `userId: <user.id>` on `create_task`.

Do not leave new bug/regression tasks unassigned. Update mode on existing tasks does not change assignee unless the user asks.

## Board constants

Read from `.agents/project/orchestrator/project.config.md`:

```text
MCP server: user-phasical
workspaceId: <from project.config.md>
projectId: <from project.config.md>
Ready status slug: to-do
```

## Dual mode

### Update mode (existing issue)

User provides GitHub `#N`, issue URL, or Phasical `taskId`.

**Enrich guard:** If task status is **done** or **closed** and the user reports a regression → **stop** update mode; switch to **create mode** with regression label and link to original `#N`.

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

After create: wait for sync, resolve `githubIssueNumber`, attach `regression` label if applicable, transition to **to-do** if status is backlog.

## Workflow (update mode)

```text
Triage progress:
- [ ] Step 0: whoami → OPERATOR_USER_ID (create mode only)
- [ ] Step 1: Resolve task from Phasical or GitHub
- [ ] Step 2: Extract and lock original ## Report text
- [ ] Step 3: Investigate codebase (read-only)
- [ ] Step 4: Regression check — duplicate search via list_tasks
- [ ] Step 5: Compose description from template; draft title per summary-patterns.md
- [ ] Step 6: update_task → description (+ title when changed)
- [ ] Step 7: update_task_status → to-do if status is backlog
- [ ] Step 8: Summarize findings in chat
```

### Step 1 — Fetch task

| Input          | MCP call                                                                                                                                    |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| GitHub `#50`   | `list_tasks` + match `externalLinks.externalId === "50"`, or `user-github` `issue_read` then find Phasical task                                |
| Phasical `taskId` | `get_task`                                                                                                                                  |
| Task title     | `list_tasks` filter by title                                                                                                                |

Record: taskId, title, current description, status, githubIssueNumber.

### Step 4 — Regression check

```text
list_tasks with projectId — search title/description for similar defects
```

| Match status | Action |
| ------------ | ------ |
| Open duplicate (not done/closed) | Report existing `#N`; do not create duplicate |
| done/closed duplicate + user reports recurrence | **Create mode** with `regression` label |
| No duplicate | Continue update or create |

### Step 6 — Update Phasical

```text
CallMcpTool user-phasical / update_task
  taskId: <cuid>
  description: "<composed markdown>"
  title: "<revised title when warranted>"
```

### Step 7 — Transition to Ready

| Current status                 | Action             |
| ------------------------------ | ------------------ |
| backlog                        | Transition → to-do |
| to-do                          | No transition      |
| in-progress / in-review / done / closed | Skip transition |

## Investigation sources

1. Spec docs per `.agents/project/orchestrator/doc-index.md`
2. Codebase search (read-only)
3. Related tasks via `list_tasks` / `get_task_relations`

## What triage does not do

- Set **in-progress**, **in-review**, or **done**
- Create epic breakdown (use `.agents/skills/phasical-intake/SKILL.md`)
- Commit code or session memory
- Modify repo files during triage (unless user explicitly requests implementation)

## MCP tools used

| Tool                 | Purpose                                    |
| -------------------- | ------------------------------------------ |
| `whoami`             | Current operator `user.id` for assignee    |
| `get_task`           | Resolve task by id                         |
| `list_tasks`         | Find by GitHub number, title, duplicates   |
| `update_task`        | Write investigation to description + title |
| `create_task`        | Create mode — new bug task                 |
| `update_task_status` | backlog → to-do after successful triage    |
| `attach_label_to_task` | `regression` label when applicable       |

**Forbidden during triage:** `create_task_comment` for findings; transitions to in-progress / in-review / done; `create_task` without `userId`.
