# Phasical sync reference (universal)

Orchestrator and sub-agents use this when a prompt includes a **PHASICAL SYNC** block. Project IDs live in `.agents/project/orchestrator/project.config.md` (supporting file).

## GitHub external linkage

Phasical auto-creates a GitHub issue per task. After sync (typically a few seconds), resolve the GitHub number from `externalLinks`:

| MCP call     | `externalLinks` location                               |
| ------------ | ------------------------------------------------------ |
| `list_tasks` | On each task inside `data.columns[].tasks[]`           |
| `get_task`   | On the task object (may lag — retry or use `list_tasks`) |

```json
{
  "resourceType": "issue",
  "externalId": "36",
  "url": "https://github.com/mdg-labs/example/issues/36",
  "metadata": { "state": "open", "createdFrom": "phasical" }
}
```

| Field            | Use                                                          |
| ---------------- | ------------------------------------------------------------ |
| `externalId`     | GitHub issue number → commit subject `[#N]`                  |
| `url`            | Human link in prompts and handoff                            |
| Phasical `taskId`   | `update_task_status`, `create_task_comment`, `update_task`   |
| Phasical `number`   | Project-scoped task number (not used in commits)             |

Resolve after `create_task`: wait ~3–5s, then `get_task` or `list_tasks` until `externalLinks` is populated.

## Column / status slugs

| Slug          | Column name | Workflow role                                 |
| ------------- | ----------- | --------------------------------------------- |
| `backlog`     | Backlog     | Unrefined / deferred                          |
| `to-do`       | Ready       | Fully specified; orchestrator picks from here |
| `in-progress` | In Progress | Execution agent (first action)                |
| `in-review`   | In Review   | Execution agent (pre-verifier handoff)        |
| `done`        | Done        | Verifier after all layers PASS                |
| `closed`      | Closed      | Final archive (operator; not verifier default) |

## Status ownership

| Status          | Who sets it                                               | When                                |
| --------------- | --------------------------------------------------------- | ----------------------------------- |
| Backlog         | Anyone (creation default)                                 | Unrefined                           |
| Ready (`to-do`) | **phasical-intake** / **phasical-triage** / orchestrator / user | After intake/triage                 |
| In Progress     | **Execution agent**                                       | First action, before session memory |
| In Review       | **Execution agent**                                       | Last action before verifier handoff |
| Done            | **Verifier**                                              | After all layers PASS               |

**Planning skills → Ready:** phasical-intake moves tasks to `to-do`. phasical-triage moves **backlog → to-do** after triage.

### Failure path

Verifier FAIL → `create_task_comment` (FAIL template) → `update_task_status` → `in-progress` + append VERIFICATION FAILED to local session memory. Work returns to execution for rework — do **not** move to `to-do` (Ready).

## Status sync — sub-agent duties (mandatory)

Skip only when user said **"don't update Phasical"** or prompt has no PHASICAL SYNC block.

Sub-agent prompts include a **STATUS SYNC TABLE** (in `prompt-templates.md`). Copy it verbatim — do not paraphrase. These transitions are routine, pre-authorized workflow steps; sub-agents execute them immediately without asking for approval.

### Execution agent — status transitions

| When | From → To | `status` slug | `create_task_comment`? |
| ---- | --------- | ------------- | ---------------------- |
| **First action** — before session memory or repo work | Ready (`to-do`) → In Progress | `in-progress` | No |
| During implementation | stay In Progress | — | No |
| **Last action** — before verifier handoff, before commit | In Progress → In Review | `in-review` | No |

### Execution agent — first action (before session memory)

```text
CallMcpTool user-phasical / update_task_status
  taskId: <each leaf phasicalTaskId>
  status: in-progress
```

- Combined batch: set **In Progress** on **every** listed leaf task.
- **Parent epic task:** when prompt lists parent taskId, set parent **in-progress** in the **same first-action batch** as the leaf.
- If already in-progress or done, continue (idempotent).
- If transition fails → `blocked`; do not start implementation.

### Execution agent — last actions (before verifier handoff)

When the prompt includes **PHASICAL SYNC**, perform these in order:

```text
1. Session memory (local): set ended + duration in header (wall-clock from started → now)
2. update_task_status → in-review for each leaf taskId
3. Single implementation commit — task files only; subject uses githubIssueNumber [#N]
```

### Verifier — after all layers PASS

Task must already be **In Review** (execution agent set this). Do not transition to `in-review`.

| When | From → To | `status` slug | `create_task_comment`? |
| ---- | --------- | ------------- | ---------------------- |
| All layers PASS (incl. 3c3) | In Review → Done | `done` | **Yes** — PASS comment first |

```text
1. Session memory: set verification ended + duration
2. create_task_comment — mandatory structured Done summary (see § Verifier Done comment)
3. update_task_status → done for each leaf taskId
4. If parent epic taskId listed and this completes the epic → done on parent too
5. Optionally delete local active/<SESSION-ID>.md or move to local archive/ (never commit)
```

### Verifier — on FAIL

| When | From → To | `status` slug | `create_task_comment`? |
| ---- | --------- | ------------- | ---------------------- |
| Any layer FAIL | In Review → In Progress | `in-progress` | **Yes** — FAIL comment first |

```text
1. create_task_comment with Layer failures + fix hints (see § Verifier FAIL comment)
2. update_task_status → in-progress for each leaf taskId
3. Append VERIFICATION FAILED to local active/<SESSION-ID>.md if file exists (never commit)
4. Do NOT set done
```

## Verifier Done comment (mandatory on PASS)

Post via `create_task_comment` on each **leaf** taskId before transitioning to done.

```markdown
## Verified — <SESSION-ID>

**Commit:** `<sha>` — <subject one line>

### Summary

- <1–3 bullets: what shipped>

### Scope

- <key paths or areas touched>

### Automated checks

- lint: PASS | FAIL | n/a
- typecheck: PASS | FAIL | n/a
- <task-specific>: PASS | FAIL | n/a

### Operator follow-ups

- <items or "None">

### Deviations / open questions

- <items or "None">
```

## Verifier FAIL comment (mandatory on FAIL)

```markdown
## Verification failed — <SESSION-ID>

### Layers failed

- Layer 1: PASS | FAIL — <detail>
- Layer 2: PASS | FAIL — <detail>
- Layer 3: PASS | FAIL — <detail>

### Fix hints

- <file>:<line> — <expected per AC/doc>
```

### Orchestrator role

- Resolve Phasical taskIds + GitHub issue numbers; include in every execution + verifier prompt.
- Run **pre-dispatch gate** (prompt-templates.md + `09-sub-agent-prompt-contract.mdc`) before every Task call.
- Confirm sub-agents report sync in REQUIRED OUTPUT.
- **Recovery only** if sub-agent skipped sync.
- After verifier PASS: optionally **re-query** `get_task` to confirm `done` (Pipewatch pattern — orchestrator is source of truth).

### Comment rules (precise)

| Role | `create_task_comment` | When | Then `update_task_status` |
| ---- | --------------------- | ---- | ------------------------- |
| Execution | **Never** | — | — |
| Verifier PASS | **Mandatory** — PASS template on each leaf | After all layers PASS, before done | `done` (parent if epic complete) |
| Verifier FAIL | **Mandatory** — FAIL template on each leaf | Before rework transition | `in-progress` (not `to-do`) |

Comment body templates: prompt-templates § **PHASICAL COMMENT CONTRACT** (copy into verifier prompts).

## PHASICAL SYNC blocks (orchestrator copies from prompt-templates.md)

**Canonical copy-paste blocks live in `.agents/project/orchestrator/prompt-templates.md`** — copy verbatim, do not paraphrase. This section summarizes; templates enforce gates and REQUIRED OUTPUT.

Use **two variants** — never pass `done` status to execution agents. Fill `projectId` and task list from `.agents/project/orchestrator/project.config.md`.

### Execution variant (summary)

See prompt-templates § **PHASICAL SYNC — EXECUTION**. Sub-agent prompt must include the **STATUS SYNC TABLE** verbatim. Key gates:

1. **STEP 1:** Ready (`to-do`) → `in-progress` on every listed taskId before any implementation
2. **STEP 2:** `in-progress` → `in-review` on each leaf, then commit with `[#N]`
3. **REQUIRED OUTPUT:** report each transition + commit sha

### Verifier variant (summary)

See prompt-templates § **PHASICAL SYNC — VERIFIER**. Sub-agent prompt must include the **STATUS SYNC TABLE** verbatim. Task starts In Review. Commit linkage (Layer 3c3) must PASS before PASS path:

- **PASS:** comment → `done`
- **FAIL:** comment → `in-progress` (rework — not `to-do`)

## Task lookup (orchestrator)

| User says                 | MCP call                                                                            |
| ------------------------- | ----------------------------------------------------------------------------------- |
| GitHub `#36` or issue URL | `list_tasks` + match `externalLinks.externalId`, or `user-github` `issue_read`      |
| Phasical task title          | `list_tasks` with `projectId`, filter by title                                      |
| Roadmap ID in description | `list_tasks` + search description for `Roadmap ID: P*`                              |
| Ready queue               | `list_tasks` `status: to-do`                                                        |
| Subtasks                  | `get_task_relations` `taskId` → `relationType: subtask`                             |
| Prerequisites             | `get_task_relations` → `blocks` edges                                               |

## Epic pattern

Feature work with **2+ tasks** uses a parent task + `create_task_relation` (`relationType: subtask`).

- Implement **leaf** tasks; pass each leaf taskId + `githubIssueNumber` to execution + verifier prompts.
- Parent **in-progress**: execution sets parent when any subtask starts.
- Parent **done**: last subtask verifier (or orchestrator recovery).
- **CLOSE_PARENTS:** before dispatch, compute which parent epics may be closed when this leaf completes (SlugBase pattern — pass parent taskId to final child verifier only).

## MCP tools by role

| Tool                   | Orchestrator                  | Execution                  | Verifier         |
| ---------------------- | ----------------------------- | -------------------------- | ---------------- |
| `list_tasks`           | Find work, read externalLinks | —                          | —                |
| `get_task`             | Load AC / description         | —                          | —                |
| `get_task_relations`   | Epic children, deps           | —                          | —                |
| `update_task_status`   | Recovery only                 | → in-progress; → in-review | → done / in-progress (FAIL) |
| `create_task_comment`  | —                             | —                          | On PASS and FAIL |
| `create_task_relation` | Intake skill                  | —                          | —                |

## Roadmap vs Phasical

|                      | Roadmap (`P*-*`)               | Phasical + GitHub (`#N`)                                |
| -------------------- | ------------------------------ | ---------------------------------------------------- |
| Plan file checkboxes | Verifier                       | No (unless linked via Roadmap ID in description)     |
| Board status         | No                             | Execution → in-progress → in-review; Verifier → done |
| Commit subject       | `[P*-*]` when no Phasical mirror  | `[#N]` — **required** when Phasical sync in scope      |

## Commit → GitHub linking

GitHub links commits when the message contains `#N` (e.g. `[#36]` in subject). Phasical task IDs must **never** appear in commit messages.

## Time tracking

When PHASICAL SYNC is present, execution and verifier agents record `started`, `ended`, and `duration` in local session memory headers. Session memory is never committed.
