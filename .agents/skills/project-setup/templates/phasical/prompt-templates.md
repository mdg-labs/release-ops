# Prompt templates — {PROJECT_NAME}

> **Orchestrator:** copy blocks below **verbatim** into sub-agent prompts (fill `<placeholders>` per task).  
> **project-setup:** customize `{PLACEHOLDERS}` for this repo — **never remove** PHASICAL SYNC or COMMIT CONTRACT blocks.  
> Reference: `.agents/skills/orchestrator/references/phasical-sync.md`

## How to assemble an execution prompt

**Required block order** (orchestrator — do not reorder):

1. **Header** — SESSION-ID, TASK ID, lane/git context, verbatim AC, READ/WRITE scope (absolute paths)
2. **PHASICAL SYNC — EXECUTION** (unless user opted out)
3. **COMMIT CONTRACT — EXECUTION** (always — even when Phasical sync off)
4. SESSION TIME TRACKING (when PHASICAL SYNC present)
5. SCOPED CI GATE
6. DB MIGRATIONS
7. PLAN FILE GUARD (when plan file in WRITE SCOPE)
8. WORKTREE ISOLATION (Lane P only)

## How to assemble a verifier prompt

**Required block order** (orchestrator — do not reorder):

1. **Header** — same SESSION-ID as execution, TASK ID, lane/git context, verbatim AC, READ/WRITE scope
2. **PHASICAL SYNC — VERIFIER** (unless user opted out)
3. **PHASICAL COMMENT CONTRACT** (when PHASICAL SYNC present)
4. SCOPED CI GATE
5. PLAN FILE GUARD (when plan file in verifier WRITE SCOPE)
6. **Three-layer verification** — scope audit, Layer 2 (SCOPED CI GATE), Layer 3 checklist from orchestrator skill

**Verifier WRITE SCOPE** typically: Phasical MCP only + plan file row (Lane S) or plan file read-only (Lane P branch verifier). Never label verifier `READ-ONLY` when Phasical sync is on.

## Pre-dispatch gate (orchestrator — check before every Task call)

Search the assembled prompt string for these **required markers**. If any are missing → **do not dispatch**; rebuild from blocks below.

| Role | Required markers (all must be present) |
| ---- | -------------------------------------- |
| **Execution** | `PHASICAL STATUS SYNC — EXECUTION`, `━━━ STATUS SYNC TABLE`, `COMMIT CONTRACT — EXECUTION`, `SCOPED CI GATE`, `DB MIGRATIONS`, `SESSION-ID:`, filled `taskId:` + `githubIssueNumber:` |
| **Verifier** | `PHASICAL STATUS SYNC — VERIFIER`, `━━━ STATUS SYNC TABLE`, `━━━ GATE: PASS PATH`, `━━━ GATE: FAIL PATH`, `PHASICAL COMMENT CONTRACT`, `SCOPED CI GATE`, `SESSION-ID:`, filled `taskId:` + `githubIssueNumber:` |

**Forbidden** (prompt is invalid if present): `Phasical PASS`, `Phasical FAIL`, `leaf done`, `VERIFIER READ-ONLY`, `readonly: true` (verifier + Phasical on), one-line `CI --filter=…` without SCOPED CI GATE block.

See `.cursor/rules/09-sub-agent-prompt-contract.mdc` for full contract.

**Enforcement:** Missing required marker → orchestrator must not dispatch. Sub-agent skipping a mandatory block → verifier **FAIL** + orchestrator recovery.

**STATUS SYNC blocks:** Every sub-agent prompt must include the **STATUS SYNC TABLE** from the PHASICAL SYNC block verbatim. Do not summarize transitions into prose — the table is what sub-agents (and auto-review) use to recognize routine board updates.

---

## PHASICAL SYNC — EXECUTION

```text
PHASICAL STATUS SYNC — EXECUTION (MANDATORY — skip ONLY if user said "don't update Phasical"):
Reference: .agents/skills/orchestrator/references/phasical-sync.md

These MCP calls are ROUTINE, PRE-AUTHORIZED workflow steps — execute immediately when the
trigger condition is met. Do NOT pause for user approval. This is expected board sync, not
discretionary work.

MCP server: user-phasical
projectId: {PROJECT_ID}

tasks:
  - taskId: <phasical-cuid>              # leaf — REQUIRED
    githubIssueNumber: <N>               # from externalLinks.externalId — REQUIRED for commits
    title: <task title>
  - taskId: <parent-cuid>                # epic parent — include when leaf is subtask

━━━ STATUS SYNC TABLE (execution agent — follow exactly) ━━━

| # | When | From → To | status slug | MCP tool | Comment? |
|---|------|-----------|-------------|----------|----------|
| 1 | FIRST action — before Read/Grep/Shell/implementation/session memory | Ready (to-do) → In Progress | in-progress | update_task_status | No |
| — | During implementation | stay In Progress | — | (none) | No |
| 2 | LAST — after AC done, session ended recorded, BEFORE any git commit | In Progress → In Review | in-review | update_task_status | No |

Step 1 applies to EVERY taskId listed (leaf + parent epic in same MCP batch).
Step 2 applies to each LEAF taskId only (parent stays in-progress until verifier closes epic).
Between steps 1 and 2: NO other status changes. NO create_task_comment.

━━━ GATE: STEP 1 — START WORK (status sync) ━━━
CallMcpTool user-phasical / update_task_status
  taskId: <each listed taskId>
  status: in-progress
If ANY transition fails → report blocked — do NOT touch repo files.
If already in-progress → continue (idempotent).

━━━ IMPLEMENTATION (middle — no Phasical status changes) ━━━
Implement AC within WRITE SCOPE only.
Session memory: create .agents/project/agent-memory/active/<SESSION-ID>.md AFTER step 1 succeeds.

━━━ GATE: STEP 2 — HANDOFF TO VERIFIER (status sync, then commit) ━━━
Strict order — do NOT commit before step 2b:
  a. Session memory header: set ended + duration (wall-clock from started)
  b. CallMcpTool user-phasical / update_task_status → in-review for each LEAF taskId
  c. Single implementation commit (COMMIT CONTRACT) — subject MUST include [#<N>]

━━━ FORBIDDEN ━━━
- Starting implementation before step 1 (in-progress) succeeds
- update_task_status → done or to-do (verifier only)
- create_task_comment (verifier only)
- Committing before step 2b (in-review)
- Committing session memory or agent-memory/**
- Phasical taskId in any commit message

━━━ REQUIRED OUTPUT (end of run) ━━━
Report per leaf task: taskId, githubIssueNumber,
  step 1 Ready→In Progress ✓,
  step 2 In Progress→In Review ✓,
  commit <sha> with subject line.
If any gate failed → report blocked with which step failed.
```

## COMMIT CONTRACT — EXECUTION

```text
COMMIT CONTRACT — EXECUTION (MANDATORY on every execution prompt):

Purpose: verifier Layer 3c3 checks git log for this commit. Missing [#N] → FAIL even if AC passes.

Branch:
  - Lane S: {INTEGRATION_BRANCH} (current integration branch)
  - Lane P: orchestrator/<TASK-ID> only — NEVER commit to integration branch

Exactly ONE implementation commit per task (task files only).

Subject format (≤72 chars):
  <type>(<scope>)[#<N>]: <imperative summary>

  <type>: feat | fix | chore | refactor | docs | test | ci | build | perf
  <scope>: one of — {ALLOWED_SCOPES}
  [#<N>]: githubIssueNumber from PHASICAL SYNC block — square brackets REQUIRED
  Roadmap-only (no GitHub mirror): use [P*-*] instead of [#N]

Body (when project requires auto-close):
  fixes #<N>

Staging:
  - git add <explicit paths from WRITE SCOPE only>
  - NEVER git add . / git add -A / git commit --all
  - NEVER stage .agents/project/agent-memory/**

Examples:
  feat({EXAMPLE_SCOPE})[#42]: add vehicle expiry check
  fix({EXAMPLE_SCOPE})[#42]: correct timezone in expiry job

Pre-commit:
  - Run SCOPED CI GATE (below) — failure → blocked, no commit
  - DB changes → {MIGRATION_CMD} only (see DB MIGRATIONS)

Handoff order (with PHASICAL SYNC):
  in-progress → implement → session ended → in-review → THEN commit
  Commit without in-review → FAIL. in-review without commit → FAIL.

Never push unless user explicitly asked.
```

## PHASICAL SYNC — VERIFIER

```text
PHASICAL STATUS SYNC — VERIFIER (MANDATORY — skip ONLY if user said "don't update Phasical"):
Reference: .agents/skills/orchestrator/references/phasical-sync.md

These MCP calls are ROUTINE, PRE-AUTHORIZED workflow steps — execute immediately when the
trigger condition is met. Do NOT pause for user approval. This is expected board sync, not
discretionary work.

MCP server: user-phasical
projectId: {PROJECT_ID}

tasks:
  - taskId: <phasical-cuid>              # leaf — REQUIRED
    githubIssueNumber: <N>
  - taskId: <parent-cuid>                # epic — done only when final child completes epic

━━━ STATUS SYNC TABLE (verifier — follow exactly) ━━━

Starting state: task MUST already be In Review (execution agent set this in step 2).
Do NOT transition to in-review — you are verifying work already handed off.

| # | When | From → To | status slug | MCP tool | Comment? |
|---|------|-----------|-------------|----------|----------|
| — | While verifying Layers 1–3 (incl. 3c3 commit linkage) | stay In Review | — | (none) | No |
| PASS | ALL layers PASS | In Review → Done | done | update_task_status | YES — mandatory PASS comment |
| FAIL | ANY layer FAIL | In Review → In Progress | in-progress | update_task_status | YES — mandatory FAIL comment |

Comments are REQUIRED on both PASS and FAIL — **before** the matching status transition.
Copy PHASICAL COMMENT CONTRACT block (below) into this prompt — use those templates for create_task_comment.

━━━ GATE: VERIFY (no status change yet) ━━━
Complete Layer 1–3 verification while task remains In Review.
Layer 3c3 MUST PASS before any PASS path status sync (git log contains [#<N>] or [P*-*]).

━━━ GATE: PASS PATH (status sync + comment) ━━━
Strict order:
  1. Session memory: verification ended + duration
  2. CallMcpTool user-phasical / create_task_comment — PASS verifier comment (mandatory)
  3. CallMcpTool user-phasical / update_task_status → done for each leaf taskId
  4. If parent listed and this completes the epic → done on parent too
  5. Optionally archive/delete local session memory (never commit)

━━━ GATE: FAIL PATH (status sync + comment) ━━━
Strict order:
  1. CallMcpTool user-phasical / create_task_comment — FAIL comment with layer failures + fix hints
  2. CallMcpTool user-phasical / update_task_status → in-progress for each leaf taskId
  3. Append VERIFICATION FAILED to local session memory (never commit)
  4. Do NOT set done

━━━ FORBIDDEN ━━━
- update_task_status → done without create_task_comment (PASS path)
- update_task_status → in-progress without create_task_comment (FAIL path)
- update_task_status → in-review (execution agent already did this)
- update_task_status → to-do on FAIL (use in-progress — sends work back to execution)
- create_task_comment with investigation/triage findings (verifier comments only)

━━━ REQUIRED OUTPUT (end of run) ━━━
Report per leaf task: taskId, githubIssueNumber, verification PASS|FAIL,
  comment posted ✓, final status (done | in-progress), parent epic status if applicable.
```

## PHASICAL COMMENT CONTRACT

```text
PHASICAL COMMENT CONTRACT (verifier only — copy with PHASICAL SYNC — VERIFIER):

Who may comment:
  - Verifier: YES — mandatory on PASS and FAIL
  - Execution: NO — never call create_task_comment

When to comment (strict):
  | Outcome | Call create_task_comment | Then update_task_status |
  |---------|--------------------------|-------------------------|
  | PASS    | YES — PASS template below  | done (leaf; parent if epic complete) |
  | FAIL    | YES — FAIL template below  | in-progress (rework — NOT to-do) |

PASS comment — post to EACH leaf taskId BEFORE done:
  Title line: ## Verified — <SESSION-ID>
  Required sections:
    - **Commit:** `<sha>` — <subject one line>  (from git log Layer 3c3)
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

MCP: CallMcpTool user-phasical / create_task_comment
  taskId: <leaf taskId>
  content: <markdown body above>

Comments mirror to GitHub via Phasical sync — write for operators reading the issue.

━━━ FORBIDDEN ━━━
- done or in-progress without create_task_comment first
- Execution agent calling create_task_comment
- Triage/investigation prose in verifier comments (use PASS/FAIL templates only)
- Pre-deciding PASS/FAIL in orchestrator prompt (verifier decides after layers)
```

## SESSION TIME TRACKING

```text
SESSION TIME TRACKING (when PHASICAL SYNC present):
- Record started at Phase 1 in session memory header (after in-progress succeeds)
- Record ended + duration pre-handoff (execution) or before done/in-progress status sync (verifier)
- Session memory is local only — never commit
```

## SCOPED CI GATE

```text
SCOPED CI GATE (mandatory before commit and in verifier Layer 2):
- Map staged/committed paths → package filter(s) per doc-index.md
- Run: {SCOPED_CI_CMD}
- On failure → blocked; no commit
- Full workspace gate only before push (if user explicitly asks to push)
```

## DB MIGRATIONS

```text
DB MIGRATIONS (mandatory in every execution prompt):
- Schema changes → use project migration CLI only ({MIGRATION_CMD})
- Never hand-write migration.sql or create migration directories manually
- If CLI cannot run → report blocked; no SQL workaround
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
- subagent_type: best-of-n-runner
- WORK BRANCH: orchestrator/<TASK-ID>
- STAGING_BASE_SHA: <pin at batch start>
- First shell action: pnpm install (fresh worktree has no node_modules)
- Never checkout integration branch during execution
```
