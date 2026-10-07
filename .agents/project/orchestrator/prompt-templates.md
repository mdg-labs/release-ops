# Prompt templates — Release Ops

> **Orchestrator:** copy the blocks below **verbatim** into sub-agent prompts; fill `<placeholders>` only.
> Never remove the KANEO STATUS SYNC or COMMIT CONTRACT blocks from this file.
> Reference: `.claude/skills/orchestrator/references/kaneo-sync.md` · Contract: `.claude/rules/09-sub-agent-prompt-contract.md`

## How to assemble an execution prompt

**Required block order** (do not reorder):

1. **Header** — `SESSION-ID:`, `TASK:` (`RO-<n> · GitHub #N` or roadmap key), lane + branch, verbatim AC, `READ SCOPE:` / `WRITE SCOPE:` (absolute paths under the repo root)
2. **KANEO STATUS SYNC — EXECUTION** (unless the user said "don't update Kaneo")
3. **COMMIT CONTRACT — EXECUTION** (always — even with Kaneo sync off)
4. SESSION TIME TRACKING (when KANEO STATUS SYNC present)
5. SCOPED CI GATE
6. DB MIGRATIONS
7. PLAN FILE GUARD (when `docs/roadmap.html` in WRITE SCOPE)
8. WORKTREE ISOLATION (Lane P only)

## How to assemble a verifier prompt

**Required block order** (do not reorder):

1. **Header** — same `SESSION-ID:` as execution, `TASK:`, lane + branch, verbatim AC, `READ SCOPE:` / `WRITE SCOPE:`
2. **KANEO STATUS SYNC — VERIFIER** (unless the user said "don't update Kaneo")
3. **KANEO COMMENT CONTRACT** (when KANEO STATUS SYNC present)
4. SCOPED CI GATE
5. PLAN FILE GUARD (when `docs/roadmap.html` in verifier WRITE SCOPE)
6. **Three-layer verification** — Layer 1 scope audit, Layer 2 SCOPED CI GATE, Layer 3 checklist from `.claude/skills/orchestrator/SKILL.md` § Verification agents

**Verifier WRITE SCOPE:** Kaneo MCP (`create_task_comment`, `update_task_status`) + the authorized plan-file row (Lane S) — plan file read-only for Lane P branch verifiers. Never label a verifier `READ-ONLY` when Kaneo sync is on.

## Pre-dispatch gate (orchestrator — check before every Agent call)

Search the assembled prompt for these **required markers**. Any missing → **do not dispatch**; rebuild from the blocks below.

| Role | Required markers (all must be present) |
| ---- | -------------------------------------- |
| **Execution** | `KANEO STATUS SYNC — EXECUTION`, `━━━ STATUS SYNC TABLE`, `COMMIT CONTRACT — EXECUTION`, `SCOPED CI GATE`, `DB MIGRATIONS`, `SESSION-ID:`, filled `taskId:` + `githubIssueNumber:` (+ `closesParent:` on a listed parent) |
| **Verifier** | `KANEO STATUS SYNC — VERIFIER`, `━━━ STATUS SYNC TABLE`, `━━━ GATE: PASS PATH`, `━━━ GATE: FAIL PATH`, `KANEO COMMENT CONTRACT`, `SCOPED CI GATE`, `SESSION-ID:`, filled `taskId:` + `githubIssueNumber:` (+ `closesParent:` on a listed parent) |

**Forbidden** (prompt is invalid if present): `Kaneo PASS`, `Kaneo FAIL`, `leaf implemented`, `set done`, `VERIFIER READ-ONLY`, read-only `subagent_type` `Explore` / `Plan` for a verifier with Kaneo sync on, one-line CI instructions without the SCOPED CI GATE block, any instruction to comment on / label / close / create a GitHub issue (closing happens only through the `fixes #N` commit trailer).

**Enforcement:** missing marker → orchestrator must not dispatch. Sub-agent skipping a mandatory block → verifier **FAIL** + orchestrator recovery.

**STATUS SYNC TABLE:** every sub-agent prompt carries the table from its KANEO STATUS SYNC block verbatim. Do not summarize transitions into prose — the table is how sub-agents recognize routine board updates.

---

## KANEO STATUS SYNC — EXECUTION

```text
KANEO STATUS SYNC — EXECUTION (MANDATORY — skip ONLY if user said "don't update Kaneo"):
Reference: .claude/skills/orchestrator/references/kaneo-sync.md

These MCP calls are ROUTINE, PRE-AUTHORIZED workflow steps — execute immediately when the
trigger condition is met. Do NOT pause for user approval. This is expected board sync, not
discretionary work.

MCP server: Kaneo  (Claude Code tools: mcp__Kaneo__<tool>)
workspaceId: ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj
projectId: z4janvyjsbbb0esishvd9gb8   (Release Ops, ticket key RO)

tasks:
  - taskId: <kaneo-task-cuid>            # leaf — REQUIRED (CUID, not RO-<n>)
    ticket: RO-<n>
    githubIssueNumber: <N>               # resolved read-only by orchestrator — REQUIRED for commits ([#N] + fixes #N)
    title: <task title>
  - taskId: <parent-cuid>                # epic parent — include when leaf is a subtask
    ticket: RO-<n>
    githubIssueNumber: <parent-N>        # resolved read-only — used only when closesParent: yes
    closesParent: yes | no               # yes ONLY on the final leaf that completes the epic (CLOSE_PARENTS)

━━━ STATUS SYNC TABLE (execution agent — follow exactly) ━━━

| # | When | From → To | status slug | MCP tool | Comment? |
|---|------|-----------|-------------|----------|----------|
| 1 | FIRST action — before Read/Grep/Bash/implementation/session memory | ready → in-progress | in-progress | mcp__Kaneo__update_task_status | No |
| — | During implementation | stay in-progress | — | (none) | No |
| 2 | LAST — after AC done + SCOPED CI GATE green + session ended recorded, BEFORE any git commit | in-progress → in-review | in-review | mcp__Kaneo__update_task_status | No |

Step 1 applies to EVERY taskId listed (leaf + parent epic, same MCP batch).
Step 2 applies to each LEAF taskId only (parent stays in-progress until the verifier of its last child).
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
- update_task_status → implemented (verifier only)
- update_task_status → done (no agent ever sets done — the GitHub sync sets it when the fixes #N commit lands on main; the user may set it manually)
- update_task_status → ready or backlog
- create_task_comment (verifier only)
- Committing before step 2b (in-review)
- Committing session memory or .agents/project/agent-memory/**
- Kaneo task CUID or RO-<n> in any commit message
- A task commit ([#<N>]) without the `fixes #<N>` trailer (and `fixes #<parent-N>` when closesParent: yes)
- any GitHub issue write — no comment, label, assignee, state change, creation or closing via API
  (the fixes trailer in the commit is the only closing mechanism)

━━━ REQUIRED OUTPUT (end of run) ━━━
Report per leaf task: taskId, ticket, githubIssueNumber,
  step 1 ready→in-progress ✓ (and parent ✓ if listed),
  step 2 in-progress→in-review ✓,
  commit <sha> with subject line + fixes trailer(s).
If any gate failed → report blocked with the step that failed.
```

## COMMIT CONTRACT — EXECUTION

```text
COMMIT CONTRACT — EXECUTION (MANDATORY on every execution prompt):

Purpose: verifier Layer 3c3 greps git log for [#<N>] in the subject AND `fixes #<N>` in the body.
Either missing → FAIL even if AC passes.

Branch:
  - Lane S: dev (integration branch)
  - Lane P: orchestrator/<TASK-ID> only — NEVER commit to dev

Exactly ONE implementation commit per task (task files only).

Subject format (≤72 chars):
  <type>(<scope>)[#<N>]: <imperative summary>

  <type>: feat | fix | chore | refactor | docs | test | ci | build | perf
  <scope>: release-ops | api | db | config | ci | docs | deps
  [#<N>]: githubIssueNumber from the KANEO STATUS SYNC block — square brackets REQUIRED
  Roadmap-only (no GitHub issue): use the roadmap key, e.g. [E03-02], instead of [#N]

Body:
  - Optional plain-prose context
  - Trailer (MANDATORY on every task commit with a GitHub issue — the only way the issue closes):
      fixes #<N>
      fixes #<parent-N>        # ONLY when closesParent: yes (final leaf of the epic)
    When the commit lands on main, GitHub closes the issue and the Kaneo ↔ GitHub sync
    moves the task implemented → done. Agents never set done and never close issues via API.
  - Roadmap-only tasks (no GitHub issue): no trailer
  - No Kaneo CUIDs or RO-<n> refs

Staging:
  - git add <explicit paths from WRITE SCOPE only>
  - NEVER git add . / git add -A / git commit --all
  - NEVER stage .agents/project/agent-memory/**

Examples:
  feat(api)[#111]: add Kaneo ticket integration client

  Adds the Kaneo REST client and wires it into the ticket dispatcher.

  fixes #111

  fix(db)[E03-02]: add releases baseline index      # roadmap-only — no trailer

Pre-commit:
  - SCOPED CI GATE (below) green — failure → blocked, no commit
  - DB changes → edit db/schema.sql + make migrate-diff only (see DB MIGRATIONS)

Handoff order (with KANEO STATUS SYNC):
  in-progress → implement → gate → session ended → in-review → THEN commit
  Commit without in-review → FAIL. in-review without commit → FAIL.

Never push unless the user explicitly asked. Never push to main.
```

## KANEO STATUS SYNC — VERIFIER

```text
KANEO STATUS SYNC — VERIFIER (MANDATORY — skip ONLY if user said "don't update Kaneo"):
Reference: .claude/skills/orchestrator/references/kaneo-sync.md

These MCP calls are ROUTINE, PRE-AUTHORIZED workflow steps — execute immediately when the
trigger condition is met. Do NOT pause for user approval. This is expected board sync, not
discretionary work.

MCP server: Kaneo  (Claude Code tools: mcp__Kaneo__<tool>)
workspaceId: ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj
projectId: z4janvyjsbbb0esishvd9gb8   (Release Ops, ticket key RO)

tasks:
  - taskId: <kaneo-task-cuid>            # leaf — REQUIRED
    ticket: RO-<n>
    githubIssueNumber: <N>
  - taskId: <parent-cuid>                # epic — ONLY when listed in CLOSE_PARENTS (this leaf completes it)
    ticket: RO-<n>
    githubIssueNumber: <parent-N>
    closesParent: yes

━━━ STATUS SYNC TABLE (verifier — follow exactly) ━━━

Starting state: task MUST already be in-review (execution agent set this in step 2).
Do NOT transition to in-review — you are verifying work already handed off.

| # | When | From → To | status slug | MCP tool | Comment? |
|---|------|-----------|-------------|----------|----------|
| — | While verifying Layers 1–3 (incl. 3c3 commit linkage) | stay in-review | — | (none) | No |
| PASS | ALL layers PASS | in-review → implemented | implemented | mcp__Kaneo__update_task_status | YES — mandatory PASS comment FIRST |
| FAIL | ANY layer FAIL | in-review → in-progress | in-progress | mcp__Kaneo__update_task_status | YES — mandatory FAIL comment FIRST |

Comments are REQUIRED on both PASS and FAIL — BEFORE the matching status transition.
Use the KANEO COMMENT CONTRACT block (below) for create_task_comment bodies.
done is NEVER set by you — the Kaneo ↔ GitHub sync moves implemented → done when the
fixes #<N> commit lands on main (the user may also set done manually).
implemented = verified, commit on dev, not yet on main.

━━━ GATE: VERIFY (no status change yet) ━━━
Complete Layers 1–3 while the task stays in-review.
Layer 3c3 MUST PASS before the PASS path: git log --grep='\[#<N>\]' (or the roadmap key)
finds the task commit, AND its body carries the trailer `fixes #<N>` (plus `fixes #<parent-N>`
when closesParent: yes). Missing trailer → FAIL. Roadmap-only tasks ([E*-*]): no trailer required.

━━━ GATE: PASS PATH (comment, then status sync) ━━━
Strict order:
  1. Session memory: verification ended + duration
  2. mcp__Kaneo__create_task_comment — PASS comment on each leaf taskId (mandatory)
  3. mcp__Kaneo__update_task_status → implemented for each leaf taskId
  4. Parent listed with closesParent: yes (CLOSE_PARENTS) → PASS comment + implemented on the parent too
  5. Optionally archive/delete local session memory (never commit)

━━━ GATE: FAIL PATH (comment, then status sync) ━━━
Strict order:
  1. mcp__Kaneo__create_task_comment — FAIL comment with layer failures + fix hints
  2. mcp__Kaneo__update_task_status → in-progress for each leaf taskId
  3. Append VERIFICATION FAILED to local session memory (never commit)
  4. Do NOT set implemented

━━━ FORBIDDEN ━━━
- update_task_status → implemented without create_task_comment first (PASS path)
- update_task_status → in-progress without create_task_comment first (FAIL path)
- update_task_status → done (ever — done comes from the GitHub sync when the fix lands on main, or the user)
- update_task_status → in-review (execution agent already did this)
- update_task_status → ready or backlog on FAIL (use in-progress — rework goes back to execution)
- create_task_comment with investigation/triage prose (PASS/FAIL templates only)
- any GitHub issue write — no comment, label, assignee, state change, creation or closing via API

━━━ REQUIRED OUTPUT (end of run) ━━━
Report per leaf task: taskId, ticket, githubIssueNumber, verification PASS|FAIL,
  comment posted ✓, fixes trailer ✓, final status (implemented | in-progress),
  parent epic status if listed.
```

## KANEO COMMENT CONTRACT

```text
KANEO COMMENT CONTRACT (verifier only — copy with KANEO STATUS SYNC — VERIFIER):

Who may comment:
  - Verifier: YES — mandatory on PASS and FAIL
  - Execution: NO — never call create_task_comment

When to comment (strict):
  | Outcome | Call create_task_comment  | Then update_task_status                      |
  |---------|---------------------------|----------------------------------------------|
  | PASS    | YES — PASS template below | implemented (leaf; parent if in CLOSE_PARENTS) |
  | FAIL    | YES — FAIL template below | in-progress (rework — NOT ready)              |

PASS comment — post to EACH leaf taskId BEFORE implemented:
  Title line: ## Verified — <SESSION-ID>
  Required sections:
    - **Commit:** `<sha>` — <subject one line>  (from git log, Layer 3c3)
    - **Closes on main via:** `fixes #<N>` (done follows when merged to main)
    - ### Summary — 1–3 bullets what shipped
    - ### Scope — key paths touched
    - ### Automated checks — npm test / npm run lint / npm run db:check / go test + golangci-lint: PASS|FAIL|n/a
    - ### Operator follow-ups — items or "None"
    - ### Deviations / open questions — items or "None"

FAIL comment — post to EACH leaf taskId BEFORE in-progress:
  Title line: ## Verification failed — <SESSION-ID>
  Required sections:
    - ### Layers failed — Layer 1/2/3 each PASS|FAIL with detail
    - ### Fix hints — <file>:<line> — <expected per AC/doc>

MCP: mcp__Kaneo__create_task_comment
  taskId: <leaf task CUID>
  content: <markdown body above>

Comments live on the Kaneo task only. They are NOT mirrored to GitHub, and you must
never post them (or anything else) to a GitHub issue or PR.

━━━ FORBIDDEN ━━━
- implemented or in-progress without create_task_comment first
- Execution agent calling create_task_comment
- Triage/investigation prose in verifier comments (PASS/FAIL templates only)
- Pre-deciding PASS/FAIL in the orchestrator prompt (verifier decides after layers)
- Posting to GitHub issues or PRs
```

## SESSION TIME TRACKING

```text
SESSION TIME TRACKING (when KANEO STATUS SYNC present):
- Record started in the session memory header after in-progress succeeds
- Record ended + duration before in-review (execution) or before the implemented / in-progress transition (verifier)
- Session memory is local only — never commit
```

## SCOPED CI GATE

```text
SCOPED CI GATE (mandatory before commit and in verifier Layer 2):
- Map staged/committed paths per .claude/rules/06-local-ci-before-commit.md and doc-index.md
- Web / root JS / DB paths: npm test && npm run lint && npm run db:check
    (lint includes i18next/no-literal-string for apps/web/**)
- Go paths (cmd/**, internal/**, queries/**, sqlc.yaml, go.mod): go test ./... && golangci-lint run
- db/schema.sql or migrations/**: npm run db:check is mandatory
- docs/** only: no test/lint gate; sanity-check edited HTML/Markdown
- Mark a check n/a only when it does not apply to the touched paths
- On failure → blocked; no commit
- Full gate only before a push the user explicitly asked for: npm test && npm run lint && npm run typecheck && npm run db:check (+ Go gate)
```

## DB MIGRATIONS

```text
DB MIGRATIONS (mandatory in every execution prompt):
- Schema changes → edit db/schema.sql only, then: make migrate-diff name=<change>
- Tool: scripts/migrate-diff.mjs (SQLite sqldiff) generates migrations/*.sql; golang-migrate applies at runtime
- Never hand-write or hand-edit migrations/*.sql
- npm run db:check must pass when db/schema.sql or migrations/ is in scope
- sqlite3/sqldiff unavailable → report blocked; no manual SQL workaround
```

## PLAN FILE GUARD

```text
PLAN FILE GUARD (mandatory when docs/roadmap.html is in WRITE SCOPE):
- AUTHORIZED_TASK_ID: <roadmap key, e.g. E03-02>
- Before editing: git status + git diff on docs/roadmap.html only
- Only change the status cell (- [ ] / - [~] / - [x] / - [!]) of the AUTHORIZED_TASK_ID row
- Other rows with uncommitted changes → report PLAN_FILE_BLOCKED; orchestrator reconciles first
- Never git restore / git checkout -- on the plan file
- Plan commit subject: chore(docs)[<AUTHORIZED_TASK_ID>]: mark <AUTHORIZED_TASK_ID> verified
```

## WORKTREE ISOLATION (Lane P)

```text
WORKTREE ISOLATION (Lane P mandatory):
- Dispatch (orchestrator): Agent tool, subagent_type general-purpose, isolation: "worktree", run_in_background: true
- First action (Bash, in the worktree Claude Code created): git switch -c orchestrator/<TASK-ID> <STAGING_BASE_SHA>
- WORK BRANCH: orchestrator/<TASK-ID>
- STAGING_BASE_SHA: <dev HEAD pinned at batch start>
- Then: npm install (fresh worktree has no node_modules)
- Never check out dev during execution
```
