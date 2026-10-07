# Kaneo sync reference (universal)

Orchestrator and sub-agents use this when a prompt includes a **KANEO SYNC** block. Project IDs live in `.agents/project/orchestrator/project.config.md` (supporting file).

MCP server: `Kaneo` — Claude Code tools `mcp__Kaneo__<tool>`.

## GitHub external linkage

The Kaneo GitHub integration creates a GitHub issue per task (typically 1–5s after `create_task`). Resolve the GitHub number from `externalLinks`:

| MCP call | `externalLinks` location |
| -------- | ------------------------ |
| `list_tasks` | On each task inside `data.columns[].tasks[]` — **only source** |
| `get_task` / `get_task_by_ticket_id` | **Not returned** — use `list_tasks` |

```json
{
  "resourceType": "issue",
  "externalId": "108",
  "url": "https://github.com/mdg-labs/release-ops/issues/108",
  "metadata": { "state": "open", "createdFrom": "kaneo" }
}
```

| Field | Use |
| ----- | --- |
| `externalId` | GitHub issue number → commit subject `[#N]` + `fixes #N` trailer |
| `url` | Human link in prompts and handoff |
| Kaneo task `id` | `update_task_status`, `create_task_comment`, `update_task`, `create_label` |
| Kaneo `number` | Ticket ID `RO-<number>` — **not** the GitHub number (e.g. `RO-106` = `#108`); never in commits |

Resolve after `create_task`: wait ~3–5s, then `list_tasks` (`sortBy: "number", sortOrder: "desc"`) until `externalLinks` is populated.

The GitHub issue body ends with `<sub>Task: <kaneo-task-id></sub>` and Kaneo posts a `[RO-<n>](…)` backlink comment — use either to go from a GitHub issue to the Kaneo task when `list_tasks` paging is impractical.

## Task lookup

| User says | MCP call |
| --------- | -------- |
| `RO-106` | `get_task_by_ticket_id({ ticketId: "RO-106" })` (`get_task` rejects ticket IDs) |
| Kaneo task ID | `get_task({ taskId })` |
| GitHub `#108` or issue URL | `list_tasks` (paged, max 100) → match `externalLinks[].externalId === "108"`; fallback `github` `issue_read` → body footer `Task: <id>` → `get_task` |
| Task title | `search({ q, type: "tasks", workspaceId, projectId })` — `workspaceId` required; does not match GitHub numbers |
| Roadmap ID in description | `list_tasks` + search description for `Roadmap ID: P*` |
| Ready queue | `list_tasks({ projectId, status: "ready" })` |
| Subtasks | `get_task_relations({ taskId })` → `relationType: subtask` (parent = source) |
| Prerequisites | `get_task_relations` → `blocks` edges |

## Column / status slugs

| Slug | Column | Workflow role |
| ---- | ------ | ------------- |
| `backlog` | Backlog | Raw / untriaged — creation default |
| `ready` | Ready | Triaged, fully specified; orchestrator picks from here |
| `in-progress` | In Progress | Execution agent (first action); rework after verifier FAIL |
| `in-review` | In Review | Execution agent (pre-verifier handoff) |
| `implemented` | Implemented | Verifier PASS — commit is on `dev`, not yet on `main` |
| `done` | Done (final) | **GitHub sync only** — set when the `fixes #N` commit lands on `main` and GitHub closes the issue |

There is **no `closed` column**. `done` is the only final state.

## Status ownership

| Status | Who sets it | When |
| ------ | ----------- | ---- |
| Backlog | Anyone (creation default) | New / raw issue |
| Ready | **kaneo-triage** / **kaneo-intake** / user | After triage / intake approval |
| In Progress | **Execution agent** (start) · **Verifier** (FAIL → rework) | First action, before session memory |
| In Review | **Execution agent** | Last action before verifier handoff |
| Implemented | **Verifier** | After all layers PASS |
| Done | **Nobody in the harness** — GitHub ↔ Kaneo sync | `fixes #N` / `closes #N` commit reaches the default branch (`main`) |

Issues are **only ever closed by a commit trailer** landing on `main`. No agent sets `done`, closes a GitHub issue, or calls `update_task_status → done`.

### Failure path

Verifier FAIL → `create_task_comment` (FAIL template) → `update_task_status` → `in-progress` + append VERIFICATION FAILED to local session memory. Work returns to execution for rework — do **not** move to `ready`.

## Status sync — sub-agent duties (mandatory)

Skip only when user said **"don't update Kaneo"** or prompt has no KANEO SYNC block.

Sub-agent prompts include a **STATUS SYNC TABLE** (in `prompt-templates.md`). Copy it verbatim — do not paraphrase. These transitions are routine, pre-authorized workflow steps; sub-agents execute them immediately without asking for approval.

### Execution agent — status transitions

| When | From → To | `status` slug | `create_task_comment`? |
| ---- | --------- | ------------- | ---------------------- |
| **First action** — before session memory or repo work | Ready → In Progress | `in-progress` | No |
| During implementation | stay In Progress | — | No |
| **Last action** — before verifier handoff, before commit | In Progress → In Review | `in-review` | No |

### Execution agent — first action (before session memory)

```text
mcp__Kaneo__update_task_status
  taskId: <each leaf kaneoTaskId>
  status: in-progress
```

- Combined batch: set **In Progress** on **every** listed leaf task.
- **Parent epic task:** when prompt lists parent taskId, set parent **in-progress** in the **same first-action batch** as the leaf.
- If already in-progress, continue (idempotent).
- If transition fails → `blocked`; do not start implementation.

### Execution agent — last actions (before verifier handoff)

When the prompt includes **KANEO SYNC**, perform these in order:

```text
1. Session memory (local): set ended + duration in header (wall-clock from started → now)
2. update_task_status → in-review for each leaf taskId
3. Single implementation commit — task files only; subject uses githubIssueNumber [#N];
   body ends with `fixes #N` (+ `fixes #<parent-N>` when closesParent: yes)
```

### Verifier — after all layers PASS

Task must already be **In Review** (execution agent set this). Do not transition to `in-review`.

| When | From → To | `status` slug | `create_task_comment`? |
| ---- | --------- | ------------- | ---------------------- |
| All layers PASS (incl. 3c3 + trailer) | In Review → Implemented | `implemented` | **Yes** — PASS comment first |

```text
1. Session memory: set verification ended + duration
2. create_task_comment — mandatory structured PASS summary (see § Verifier PASS comment)
3. update_task_status → implemented for each leaf taskId
4. If parent epic listed with closesParent: yes → PASS comment + implemented on parent too
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
4. Do NOT set implemented or done
```

## Verifier PASS comment (mandatory on PASS)

Post via `create_task_comment` on each **leaf** taskId before transitioning to implemented.

```markdown
## Verified — <SESSION-ID>

**Commit:** `<sha>` — <subject one line>
**Closes on main via:** `fixes #<N>` (Done follows when merged to main)

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

Comments mirror to the GitHub issue as `**<user>** commented:` — write for operators reading the issue.

### Orchestrator role

- Resolve Kaneo task IDs + GitHub issue numbers (via `list_tasks` `externalLinks`); include in every execution + verifier prompt.
- Run **pre-dispatch gate** (prompt-templates.md + `09-sub-agent-prompt-contract.md`) before every Agent call.
- Confirm sub-agents report sync in REQUIRED OUTPUT.
- **Recovery only** if sub-agent skipped sync.
- After verifier PASS: optionally **re-query** `get_task` to confirm `implemented` (orchestrator is source of truth).

### Comment rules (precise)

| Role | `create_task_comment` | When | Then `update_task_status` |
| ---- | --------------------- | ---- | ------------------------- |
| Execution | **Never** | — | — |
| Verifier PASS | **Mandatory** — PASS template on each leaf | After all layers PASS, before implemented | `implemented` (parent if closesParent: yes) |
| Verifier FAIL | **Mandatory** — FAIL template on each leaf | Before rework transition | `in-progress` (not `ready`) |

Comment body templates: prompt-templates § **KANEO COMMENT CONTRACT** (copy into verifier prompts).

## KANEO SYNC blocks (orchestrator copies from prompt-templates.md)

**Canonical copy-paste blocks live in `.agents/project/orchestrator/prompt-templates.md`** — copy verbatim, do not paraphrase. This section summarizes; templates enforce gates and REQUIRED OUTPUT.

Use **two variants** — never pass `implemented` or `done` to execution agents. Fill `workspaceId`, `projectId` and task list from `.agents/project/orchestrator/project.config.md`.

### Execution variant (summary)

See prompt-templates § **KANEO SYNC — EXECUTION**. Key gates:

1. **STEP 1:** Ready → `in-progress` on every listed taskId before any implementation
2. **STEP 2:** `in-progress` → `in-review` on each leaf, then commit with `[#N]` + `fixes #N`
3. **REQUIRED OUTPUT:** report each transition + commit sha

### Verifier variant (summary)

See prompt-templates § **KANEO SYNC — VERIFIER**. Task starts In Review. Commit linkage (Layer 3c3, incl. `fixes #N` trailer) must PASS before PASS path:

- **PASS:** comment → `implemented`
- **FAIL:** comment → `in-progress` (rework — not `ready`)

## Epic pattern

Feature work with **2+ tasks** uses a parent task + `create_task_relation` (`relationType: subtask`, `sourceTaskId` = parent).

- Implement **leaf** tasks; pass each leaf taskId + `githubIssueNumber` to execution + verifier prompts.
- Parent **in-progress**: execution sets parent when any subtask starts.
- Parent **implemented**: final leaf's verifier (or orchestrator recovery).
- Parent **done**: GitHub sync — the **final leaf's commit** carries `fixes #<parent-N>` in addition to `fixes #<leaf-N>`.
- **closesParent:** before dispatch, the orchestrator computes whether this leaf is the last open leaf of its epic (all siblings `implemented` or `done`). Pass `closesParent: yes` on the parent entry to that leaf's execution **and** verifier prompts only.

## MCP tools by role

| Tool | Orchestrator | Execution | Verifier |
| ---- | ------------ | --------- | -------- |
| `get_task_by_ticket_id` | Resolve `RO-<n>` | — | — |
| `list_tasks` | Find work, read externalLinks | — | — |
| `get_task` | Load AC / description | — | — |
| `get_task_relations` | Epic children, deps | — | — |
| `update_task_status` | Recovery only | → in-progress; → in-review | → implemented / in-progress (FAIL) |
| `create_task_comment` | — | — | On PASS and FAIL |
| `create_task_relation` | Intake skill | — | — |
| `create_label` (with `taskId`) | Intake / triage skills | — | — |

## Roadmap vs Kaneo

| | Roadmap (`P*-*`) | Kaneo + GitHub (`#N`) |
| - | ---------------- | --------------------- |
| Plan file checkboxes | Verifier | No (unless linked via Roadmap ID in description) |
| Board status | No | Execution → in-progress → in-review; Verifier → implemented; GitHub sync → done |
| Commit subject | `[P*-*]` when no Kaneo mirror | `[#N]` — **required** when Kaneo sync in scope |
| Commit body | — | `fixes #N` — **required** |

## Commit → GitHub linking

GitHub links commits when the message contains `#N` (e.g. `[#108]` in subject) and closes the issue when a commit with `fixes #N` / `closes #N` reaches the default branch (`main`). Kaneo task IDs and `RO-<n>` ticket IDs must **never** appear in commit messages.

## Time tracking

When KANEO SYNC is present, execution and verifier agents record `started`, `ended`, and `duration` in local session memory headers. Session memory is never committed. (Kaneo time-entry tools exist but are not used by the harness.)
