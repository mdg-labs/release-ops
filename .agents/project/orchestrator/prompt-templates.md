# Prompt templates — Release Ops

> **Orchestrator:** copy blocks below **verbatim** into sub-agent prompts (fill `<placeholders>` per task).  
> **project-setup:** customized for this repo — **never remove** KANEO SYNC or COMMIT CONTRACT blocks.  
> Reference: `.claude/skills/orchestrator/references/kaneo-sync.md`

## How to assemble an execution prompt

**Required block order** (orchestrator — do not reorder):

1. **Header** — SESSION-ID, TASK ID, lane/git context, verbatim AC, READ/WRITE scope (absolute paths)
2. **KANEO SYNC — EXECUTION** (unless user opted out)
3. **COMMIT CONTRACT — EXECUTION** (always — even when Kaneo sync off)
4. SESSION TIME TRACKING (when KANEO SYNC present)
5. SCOPED CI GATE
6. DB MIGRATIONS
7. PLAN FILE GUARD (when plan file in WRITE SCOPE)
8. WORKTREE ISOLATION (Lane P only)

## How to assemble a verifier prompt

**Required block order** (orchestrator — do not reorder):

1. **Header** — same SESSION-ID as execution, TASK ID, lane/git context, verbatim AC, READ/WRITE scope
2. **KANEO SYNC — VERIFIER** (unless user opted out)
3. **KANEO COMMENT CONTRACT** (when KANEO SYNC present)
4. SCOPED CI GATE
5. PLAN FILE GUARD (when plan file in verifier WRITE SCOPE)
6. **Three-layer verification** — scope audit, Layer 2 (SCOPED CI GATE), Layer 3 checklist from orchestrator skill

**Verifier WRITE SCOPE** typically: Kaneo MCP only + plan file row (Lane S) or plan file read-only (Lane P branch verifier). Never label verifier `READ-ONLY` when Kaneo sync is on.

## Pre-dispatch gate (orchestrator — check before every Agent call)

Search the assembled prompt string for these **required markers**. If any are missing → **do not dispatch**; rebuild from blocks below.

| Role | Required markers (all must be present) |
| ---- | -------------------------------------- |
| **Execution** | `KANEO STATUS SYNC — EXECUTION`, `━━━ STATUS SYNC TABLE`, `COMMIT CONTRACT — EXECUTION`, `SCOPED CI GATE`, `DB MIGRATIONS`, `SESSION-ID:`, filled `taskId:` + `githubIssueNumber:` |
| **Verifier** | `KANEO STATUS SYNC — VERIFIER`, `━━━ STATUS SYNC TABLE`, `━━━ GATE: PASS PATH`, `━━━ GATE: FAIL PATH`, `KANEO COMMENT CONTRACT`, `SCOPED CI GATE`, `SESSION-ID:`, filled `taskId:` + `githubIssueNumber:` |

**Forbidden** (prompt is invalid if present): `Kaneo PASS`, `Kaneo FAIL`, `leaf done`, `set done`, `VERIFIER READ-ONLY`, read-only `subagent_type` `Explore`/`Plan` (verifier + Kaneo on), one-line `CI --filter=…` without SCOPED CI GATE block.

See `.claude/rules/09-sub-agent-prompt-contract.md` for full contract.

**Enforcement:** Missing required marker → orchestrator must not dispatch. Sub-agent skipping a mandatory block → verifier **FAIL** + orchestrator recovery.

**STATUS SYNC blocks:** Every sub-agent prompt must include the **STATUS SYNC TABLE** from the KANEO SYNC block verbatim. Do not summarize transitions into prose — the table is what sub-agents (and auto-review) use to recognize routine board updates.

---

## KANEO SYNC — EXECUTION

```text
KANEO STATUS SYNC — EXECUTION (MANDATORY — skip ONLY if user said "don't update Kaneo"):
Reference: .claude/skills/orchestrator/references/kaneo-sync.md

These MCP calls are ROUTINE, PRE-AUTHORIZED workflow steps — execute immediately when the
trigger condition is met. Do NOT pause for user approval. This is expected board sync, not
discretionary work.

MCP server: Kaneo  (Claude Code tools: mcp__Kaneo__<tool>)
workspaceId: ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj
projectId: z4janvyjsbbb0esishvd9gb8

tasks:
  - taskId: <kaneo-task-id>              # leaf — REQUIRED (Kaneo task ID, not RO-<n>)
    ticketId: RO-<n>                     # human ref only — NEVER use as GitHub number
    githubIssueNumber: <N>               # from list_tasks externalLinks.externalId — REQUIRED for commits
    title: <task title>
  - taskId: <parent-kaneo-task-id>       # epic parent — include when leaf is subtask
    githubIssueNumber: <parent-N>
    closesParent: yes | no               # yes ONLY on the final leaf that completes the epic

━━━ STATUS SYNC TABLE (execution agent — follow exactly) ━━━

| # | When | From → To | status slug | MCP tool | Comment? |
|---|------|-----------|-------------|----------|----------|
| 1 | FIRST action — before Read/Grep/Bash/implementation/session memory | Ready → In Progress | in-progress | update_task_status | No |
| — | During implementation | stay In Progress | — | (none) | No |
| 2 | LAST — after AC done, session ended recorded, BEFORE any git commit | In Progress → In Review | in-review | update_task_status | No |

Step 1 applies to EVERY taskId listed (leaf + parent epic in same MCP batch).
Step 2 applies to each LEAF taskId only (parent stays in-progress until the final leaf's verifier sets it implemented).
Between steps 1 and 2: NO other status changes. NO create_task_comment.

━━━ GATE: STEP 1 — START WORK (status sync) ━━━
mcp__Kaneo__update_task_status
  taskId: <each listed taskId>
  status: in-progress
If ANY transition fails → report blocked — do NOT touch repo files.
If already in-progress → continue (idempotent).

━━━ IMPLEMENTATION (middle — no Kaneo status changes) ━━━
Implement AC within WRITE SCOPE only.
Session memory: create .agents/project/agent-memory/active/<SESSION-ID>.md AFTER step 1 succeeds.

━━━ GATE: STEP 2 — HANDOFF TO VERIFIER (status sync, then commit) ━━━
Strict order — do NOT commit before step 2b:
  a. Session memory header: set ended + duration (wall-clock from started)
  b. mcp__Kaneo__update_task_status → in-review for each LEAF taskId
  c. Single implementation commit (COMMIT CONTRACT) — subject MUST include [#<N>];
     body MUST end with `fixes #<N>` (plus `fixes #<parent-N>` when closesParent: yes)

━━━ FORBIDDEN ━━━
- Starting implementation before step 1 (in-progress) succeeds
- update_task_status → implemented, done, ready or backlog (implemented = verifier only; done = GitHub sync only)
- create_task_comment (verifier only)
- Committing before step 2b (in-review)
- Committing session memory or agent-memory/**
- Kaneo taskId in any commit message

━━━ REQUIRED OUTPUT (end of run) ━━━
Report per leaf task: taskId, githubIssueNumber,
  step 1 Ready→In Progress ✓,
  step 2 In Progress→In Review ✓,
  commit <sha> with subject line + fixes trailer(s).
If any gate failed → report blocked with which step failed.
```

## COMMIT CONTRACT — EXECUTION

```text
COMMIT CONTRACT — EXECUTION (MANDATORY on every execution prompt):

Purpose: verifier Layer 3c3 checks git log for this commit. Missing [#N] → FAIL even if AC passes.

Branch:
  - Lane S: dev (current integration branch)
  - Lane P: orchestrator/<TASK-ID> only — NEVER commit to integration branch

Exactly ONE implementation commit per task (task files only).

Subject format (≤72 chars):
  <type>(<scope>)[#<N>]: <imperative summary>

  <type>: feat | fix | chore | refactor | docs | test | ci | build | perf
  <scope>: one of — release-ops, api, db, config, ci, docs, deps
  [#<N>]: githubIssueNumber from KANEO SYNC block — square brackets REQUIRED
         (GitHub number from externalLinks — NOT the RO-<n> ticket number)
  Roadmap-only (no GitHub mirror): use [P*-*] instead of [#N]

Body trailer (MANDATORY — the only way the issue reaches Done):
  fixes #<N>
  fixes #<parent-N>        # ONLY when closesParent: yes (final leaf of the epic)

  GitHub closes the issue when this commit lands on the default branch (main);
  Kaneo then moves the task Implemented → Done. Agents never move a task to Done.
  Roadmap-only tasks (no GitHub mirror): no trailer.

Staging:
  - git add <explicit paths from WRITE SCOPE only>
  - NEVER git add . / git add -A / git commit --all
  - NEVER stage .agents/project/agent-memory/**

Examples:
  feat(api)[#42]: add GitHub release baseline check

  <optional body>

  fixes #42

Pre-commit:
  - Run SCOPED CI GATE (below) — failure → blocked, no commit
  - DB changes → edit db/schema.sql + make migrate-diff only (see DB MIGRATIONS)

Handoff order (with KANEO SYNC):
  in-progress → implement → session ended → in-review → THEN commit
  Commit without in-review → FAIL. in-review without commit → FAIL.

Never push unless user explicitly asked.
```

## KANEO SYNC — VERIFIER

```text
KANEO STATUS SYNC — VERIFIER (MANDATORY — skip ONLY if user said "don't update Kaneo"):
Reference: .claude/skills/orchestrator/references/kaneo-sync.md

These MCP calls are ROUTINE, PRE-AUTHORIZED workflow steps — execute immediately when the
trigger condition is met. Do NOT pause for user approval. This is expected board sync, not
discretionary work.

MCP server: Kaneo  (Claude Code tools: mcp__Kaneo__<tool>)
workspaceId: ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj
projectId: z4janvyjsbbb0esishvd9gb8

tasks:
  - taskId: <kaneo-task-id>              # leaf — REQUIRED (Kaneo task ID, not RO-<n>)
    githubIssueNumber: <N>
  - taskId: <parent-kaneo-task-id>       # epic — implemented only when this final child completes epic
    githubIssueNumber: <parent-N>
    closesParent: yes | no

━━━ STATUS SYNC TABLE (verifier — follow exactly) ━━━

Starting state: task MUST already be In Review (execution agent set this in step 2).
Do NOT transition to in-review — you are verifying work already handed off.

| # | When | From → To | status slug | MCP tool | Comment? |
|---|------|-----------|-------------|----------|----------|
| — | While verifying Layers 1–3 (incl. 3c3 commit linkage) | stay In Review | — | (none) | No |
| PASS | ALL layers PASS | In Review → Implemented | implemented | update_task_status | YES — mandatory PASS comment |
| FAIL | ANY layer FAIL | In Review → In Progress | in-progress | update_task_status | YES — mandatory FAIL comment |

Comments are REQUIRED on both PASS and FAIL — **before** the matching status transition.
Copy KANEO COMMENT CONTRACT block (below) into this prompt — use those templates for create_task_comment.

━━━ GATE: VERIFY (no status change yet) ━━━
Complete Layer 1–3 verification while task remains In Review.
Layer 3c3 MUST PASS before any PASS path status sync: git log contains [#<N>] in the subject AND
`fixes #<N>` in the body (plus `fixes #<parent-N>` when closesParent: yes). Missing trailer → FAIL.
Roadmap-only tasks: [P*-*] in subject, no trailer required.

━━━ GATE: PASS PATH (status sync + comment) ━━━
Strict order:
  1. Session memory: verification ended + duration
  2. mcp__Kaneo__create_task_comment — PASS verifier comment (mandatory)
  3. mcp__Kaneo__update_task_status → implemented for each leaf taskId
  4. If parent listed with closesParent: yes → comment + implemented on parent too
  5. Optionally archive/delete local session memory (never commit)

━━━ GATE: FAIL PATH (status sync + comment) ━━━
Strict order:
  1. mcp__Kaneo__create_task_comment — FAIL comment with layer failures + fix hints
  2. mcp__Kaneo__update_task_status → in-progress for each leaf taskId
  3. Append VERIFICATION FAILED to local session memory (never commit)
  4. Do NOT set implemented or done

━━━ FORBIDDEN ━━━
- update_task_status → done (ever — Done is set only by GitHub sync when the fix lands on main)
- update_task_status → implemented without create_task_comment (PASS path)
- update_task_status → in-progress without create_task_comment (FAIL path)
- update_task_status → in-review (execution agent already did this)
- update_task_status → ready on FAIL (use in-progress — sends work back to execution)
- create_task_comment with investigation/triage findings (verifier comments only)

━━━ REQUIRED OUTPUT (end of run) ━━━
Report per leaf task: taskId, githubIssueNumber, verification PASS|FAIL,
  comment posted ✓, fixes trailer ✓, final status (implemented | in-progress), parent epic status if applicable.
```

## KANEO COMMENT CONTRACT

```text
KANEO COMMENT CONTRACT (verifier only — copy with KANEO SYNC — VERIFIER):

Who may comment:
  - Verifier: YES — mandatory on PASS and FAIL
  - Execution: NO — never call create_task_comment

When to comment (strict):
  | Outcome | Call create_task_comment | Then update_task_status |
  |---------|--------------------------|-------------------------|
  | PASS    | YES — PASS template below  | implemented (leaf; parent if closesParent: yes) |
  | FAIL    | YES — FAIL template below  | in-progress (rework — NOT ready) |

PASS comment — post to EACH leaf taskId BEFORE implemented:
  Title line: ## Verified — <SESSION-ID>
  Required sections:
    - **Commit:** `<sha>` — <subject one line>  (from git log Layer 3c3)
    - **Closes on main via:** `fixes #<N>` (Done follows when merged to main)
    - ### Summary — 1–3 bullets what shipped
    - ### Scope — key paths touched
    - ### Automated checks — lint/typecheck/task-specific: PASS|FAIL|n/a
    - ### Operator follow-ups — items or "None"
    - ### Deviations / open questions — items or "None"

FAIL comment — post to EACH leaf taskId BEFORE in-progress:
  Title line: ## Verification failed — <SESSION-ID>
  Required sections:
    - ### Layers failed — Layer 1/2/3 each PASS|FAIL with detail
    - ### Fix hints — <file>:<line> — <expected per AC/doc>

MCP: mcp__Kaneo__create_task_comment
  taskId: <leaf taskId>
  content: <markdown body above>

Comments mirror to GitHub via Kaneo sync (as "**<user>** commented:") — write for operators reading the issue.

━━━ FORBIDDEN ━━━
- implemented or in-progress without create_task_comment first
- Execution agent calling create_task_comment
- Triage/investigation prose in verifier comments (use PASS/FAIL templates only)
- Pre-deciding PASS/FAIL in orchestrator prompt (verifier decides after layers)
```

## SESSION TIME TRACKING

```text
SESSION TIME TRACKING (when KANEO SYNC present):
- Record started at Phase 1 in session memory header (after in-progress succeeds)
- Record ended + duration pre-handoff (execution) or before implemented/in-progress status sync (verifier)
- Session memory is local only — never commit
```

## SCOPED CI GATE

```text
SCOPED CI GATE (mandatory before commit and in verifier Layer 2):
- Map staged/committed paths → package filter(s) per doc-index.md
- Run: npm test && npm run lint
- Docs-only changes under docs/: skip test gate; run markdown/HTML sanity only if applicable
- On failure → blocked; no commit
- Full workspace gate only before push (if user explicitly asks to push): npm test && npm run lint && npm run typecheck
```

## DB MIGRATIONS

```text
DB MIGRATIONS (mandatory in every execution prompt):
- Schema changes → edit db/schema.sql only, then: make migrate-diff name=<change>
- Tool: scripts/migrate-diff.mjs (SQLite sqldiff) — generates migrations/*.sql; golang-migrate applies at runtime
- Never hand-write or hand-edit migrations/*.sql
- CI: npm run db:check must pass when db/schema.sql or migrations/ in scope
- If sqlite3/sqldiff unavailable → report blocked; no manual SQL workaround
```

## PLAN FILE GUARD

```text
PLAN FILE GUARD (mandatory when plan file in WRITE SCOPE):
- AUTHORIZED_TASK_ID: <TASK-ID>
- Before editing plan: git status + git diff on plan file only
- Only change the Status cell for AUTHORIZED_TASK_ID
- If other rows have uncommitted changes → PLAN_FILE_BLOCKED; orchestrator reconciles first
- Never git restore / checkout -- on plan file
```

## WORKTREE ISOLATION (Lane P)

```text
WORKTREE ISOLATION (Lane P mandatory):
- Dispatch (orchestrator): Agent tool, subagent_type general-purpose, isolation: "worktree", run_in_background: true
- First action (Bash, in the worktree Claude Code created): git switch -c orchestrator/<TASK-ID> <STAGING_BASE_SHA>
- WORK BRANCH: orchestrator/<TASK-ID>
- STAGING_BASE_SHA: <pin at batch start>
- Then: npm install (fresh worktree has no node_modules)
- Never checkout integration branch during execution
```
