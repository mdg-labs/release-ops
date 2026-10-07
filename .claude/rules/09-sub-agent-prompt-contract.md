---
description: Mandatory verbatim blocks for orchestrator sub-agent prompts — no shorthand, no pre-decided outcomes
---

# Sub-agent prompt contract

When you dispatch a sub-agent via the **Agent** tool for orchestrated work (execution or verification), the prompt is a **contract**. Shorthand prompts are **forbidden**.

Source of truth: `.agents/project/orchestrator/prompt-templates.md` — copy blocks **verbatim** (fill placeholders only).

## Pre-dispatch gate (orchestrator — mandatory)

**Do not call Agent** until the assembled prompt passes **every** check below.

### Execution prompt — required content

| # | Must appear verbatim in prompt | Block in prompt-templates.md |
|---|-------------------------------|------------------------------|
| 1 | `KANEO STATUS SYNC — EXECUTION` | § KANEO STATUS SYNC — EXECUTION |
| 2 | `━━━ STATUS SYNC TABLE` | (inside execution block) |
| 3 | `COMMIT CONTRACT — EXECUTION` | § COMMIT CONTRACT — EXECUTION |
| 4 | `SCOPED CI GATE` | § SCOPED CI GATE |
| 5 | `DB MIGRATIONS` | § DB MIGRATIONS |
| 6 | Filled `taskId:` (Kaneo CUID) + `githubIssueNumber:` per leaf task | inside KANEO STATUS SYNC block |
| 7 | `SESSION-ID:` with value `<TASK-ID>-<YYYYMMDD>-<4hex>` | orchestrator header |
| 8 | Verbatim acceptance criteria bullets | Kaneo task description / plan row |
| 9 | `READ SCOPE:` and `WRITE SCOPE:` with absolute paths | orchestrator header |

Also include when applicable: `SESSION TIME TRACKING`, `PLAN FILE GUARD`, `WORKTREE ISOLATION` (Lane P).

Skip KANEO STATUS SYNC only when the user said **"don't update Kaneo"**. COMMIT CONTRACT is **always** required.

### Verifier prompt — required content

| # | Must appear verbatim in prompt | Block in prompt-templates.md |
|---|-------------------------------|------------------------------|
| 1 | `KANEO STATUS SYNC — VERIFIER` | § KANEO STATUS SYNC — VERIFIER |
| 2 | `━━━ STATUS SYNC TABLE` | (inside verifier block) |
| 3 | `━━━ GATE: PASS PATH` and `━━━ GATE: FAIL PATH` | (inside verifier block) |
| 4 | `KANEO COMMENT CONTRACT` | § KANEO COMMENT CONTRACT |
| 5 | `SCOPED CI GATE` | § SCOPED CI GATE |
| 6 | Filled `taskId:` (Kaneo CUID) + `githubIssueNumber:` per leaf task | inside KANEO STATUS SYNC block |
| 7 | Same `SESSION-ID:` as the execution agent | orchestrator header |
| 8 | Verbatim acceptance criteria bullets | Kaneo task description / plan row |
| 9 | `READ SCOPE:` / `WRITE SCOPE:` (verifier writes Kaneo + plan-file row only) | orchestrator header |

Also include when applicable: `PLAN FILE GUARD`, the three-layer verification checklist from the orchestrator skill.

Skip KANEO STATUS SYNC only when the user said **"don't update Kaneo"**.

### Verifier is NOT read-only

With Kaneo sync on, the verifier **must** call `mcp__Kaneo__create_task_comment` and `mcp__Kaneo__update_task_status` (`implemented` on PASS, `in-progress` on FAIL). Never label a verifier prompt `READ-ONLY` or dispatch it as a read-only agent type (`Explore`, `Plan`) — that blocks the required MCP writes.

## Forbidden shorthand (never dispatch with these)

These patterns make the prompt **invalid** — rebuild from prompt-templates before calling Agent:

- `Kaneo PASS` / `Kaneo FAIL` / `leaf implemented` / `set done` — the orchestrator **never** pre-decides the verification outcome
- Any instruction for an agent to set `done` (user only)
- Any instruction to comment on, label, assign, close or create a GitHub issue, or to use `fixes` / `closes` / `resolves #N`
- `VERIFIER READ-ONLY` or a read-only `subagent_type` (`Explore`, `Plan`) on a verifier with Kaneo sync on
- One-line Kaneo instructions (`Kaneo sync on`, `update board`, `git log [#N]`)
- CI as a lone flag without the full `SCOPED CI GATE` block
- TaskId / `#N` in prose without the filled `tasks:` list inside the KANEO STATUS SYNC block
- Summarized status transitions instead of the `━━━ STATUS SYNC TABLE`
- Missing `COMMIT CONTRACT — EXECUTION` on execution prompts
- Missing `━━━ REQUIRED OUTPUT` section from the KANEO STATUS SYNC block

### Invalid example (do not dispatch)

```text
Verifier RO-108 #111 taskId abc. Epic not implemented. git log [#111].
AC: …
WRITE: …
Kaneo PASS leaf implemented. VERIFIER READ-ONLY. CI web only.
```

### Valid shape (minimum — blocks must be full verbatim copies)

```text
SESSION-ID: RO-108-20261007-a3f1
TASK: RO-108 · GitHub #111 · Lane S · branch dev

AC:
- …

READ SCOPE: …
WRITE SCOPE: …

<verbatim KANEO STATUS SYNC — VERIFIER block from prompt-templates.md>

<verbatim KANEO COMMENT CONTRACT block from prompt-templates.md>

<verbatim SCOPED CI GATE block from prompt-templates.md>
```

## Kaneo status + comment rules (summary)

| Role | Status (`mcp__Kaneo__update_task_status`) | Comment (`mcp__Kaneo__create_task_comment`) |
|------|------------|-------------|
| **Execution** | `in-progress` first; `in-review` before the commit | **Never** |
| **Verifier PASS** | `implemented` on leaf (+ parent if listed in `CLOSE_PARENTS`) | **Mandatory** PASS template — **before** `implemented` |
| **Verifier FAIL** | `in-progress` on leaf (rework) | **Mandatory** FAIL template — **before** `in-progress` |
| **User** | `done` | — |

Full templates: `.claude/skills/orchestrator/references/kaneo-sync.md` and prompt-templates § KANEO COMMENT CONTRACT.

## Enforcement

- The orchestrator holds the Agent call → **blocked** until the prompt passes the pre-dispatch gate.
- Sub-agent skips a mandatory block → verifier **FAIL** + orchestrator recovery.
- See `.claude/skills/orchestrator/SKILL.md` § Dispatching sub-agents.
