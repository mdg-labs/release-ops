---
name: orchestrator
description: >-
  Run a chat as a pure orchestrator. Reads the development roadmap and/or Phasical
  board to find work, dispatches sub-agents with doc references (not pasted spec
  content), and runs verification after each batch. Optional session-end Slack DM.
  Execution agents set in-progress (leaf + parent when subtask); only verification
  agents set done after PASS. Use when the user asks to orchestrate, delegate
  end-to-end, execute the roadmap, implement a GitHub issue/epic (e.g. #21), or
  coordinate parallel implementation tasks.
---

# Orchestrator

The main agent in this chat is a **dispatcher only**. It reads the **roadmap** and/or **Phasical board**, decides what to run next, and hands implementation to sub-agents. Sub-agents read spec docs and implementation files themselves.

## Path layout (Cursor + `npx skills`)

`npx skills add` installs skills under `.agents/skills/`. **`npx skills update` replaces each skill directory wholesale** — anything not in the upstream skill repo is deleted.

Project-specific supporting files live under **`.agents/project/`** (safe from updates). Cursor rules live under `.cursor/rules/` only.

| What | Path |
| ---- | ---- |
| This skill (installed) | `.agents/skills/orchestrator/SKILL.md` |
| Phasical sync reference (installed) | `.agents/skills/orchestrator/references/phasical-sync.md` |
| Sub-agent monitoring (installed) | `.agents/skills/orchestrator/references/sub-agent-monitoring.md` |
| Project constants (supporting) | `.agents/project/orchestrator/project.config.md` |
| Doc index (supporting) | `.agents/project/orchestrator/doc-index.md` |
| Prompt templates (supporting) | `.agents/project/orchestrator/prompt-templates.md` |
| Slack session-end (supporting, optional) | `.agents/project/orchestrator/slack-session-end.md` |
| Workspace notes (supporting) | `.agents/project/workspace-notes.md` |
| Session memory (supporting, gitignored) | `.agents/project/agent-memory/` |

Created/updated by **project-setup** (layer **phasical**).

---

## What the orchestrator does (and does not do)

### MAY do

- Read the **plan file** (full file): phases, task rows, dependencies, Doc Ref column, exit criteria
- Read **Phasical task payloads** via MCP (`user-phasical`): title, description, subtasks, dependencies, status
- Read `.agents/project/orchestrator/doc-index.md`, `prompt-templates.md`, `project.config.md`, and optional `slack-session-end.md`
- Call Slack MCP for **session-end DM** only (if `slack-session-end.md` exists)
- Read `.agents/project/workspace-notes.md`; write durable learnings there
- Use `TodoWrite` in **chat mode** / **Phasical mode**
- In **plan-file mode**, edit the plan file for status reconciliation or **Lane P batch prep** (`[~]` at batch start)
- Launch sub-agents via **Task** (`generalPurpose`, `best-of-n-runner`, `shell`, `explore`, `ci-investigator`)
- Set `run_in_background: true` on Task when dispatching parallel Lane P execution agents (max **3** concurrent)
- **Commit-linkage audit** after verifier PASS (EnvHub/Pipewatch pattern — mandatory)
- List filenames in `.agents/project/agent-memory/active/` (names only, not contents)
- Ask clarifying questions

### MUST NOT do

- Read spec doc bodies — sub-agents read these (Phasical task descriptions **are** readable — they are the AC contract)
- Read implementation files, diffs, test output, lint results, or logs
- Use `Read`, `Grep`, `Glob`, `ReadLints`, `Shell`, `ApplyPatch`, etc. on implementation work
- Summarize file contents from memory
- Edit repo files other than plan file, `workspace-notes.md`, `project.config.md`, or orchestrator supporting files under `.agents/project/`
- Paste spec doc bodies into sub-agent prompts — pass paths and `§` section refs
- Paste entire task descriptions — extract AC, file paths, doc refs, and deps
- Dispatch Lane P and Lane S tasks in the same batch
- Allow execution agents to commit to the integration branch during an in-flight Lane P batch

**Shell allowed only for:** `workspace-notes.md`, commit-linkage audit (`git log --grep`), plan reconciliation commits, optional Slack DM.

---

## Three task sources

| Source      | Task IDs                         | AC lives in             | Status tracking                 |
| ----------- | -------------------------------- | ----------------------- | ------------------------------- |
| **Roadmap** | `P0-03`, `P2-01`, `P6`, …        | Plan file row           | Plan checkboxes `[x]`/`[!]`     |
| **Phasical**   | `#47`, GitHub URL, Phasical taskId  | Task description (MCP)  | Phasical status + comment          |
| **Ad-hoc**  | User-named                       | User message            | `TodoWrite` only                |

**User intent wins:** if they say "implement #21" or give a GitHub/Phasical URL → **Phasical mode**, even though the roadmap exists.

## Three modes

|                 | **Plan-file mode**                                 | **Phasical mode**                          | **Chat mode**                      |
| --------------- | -------------------------------------------------- | --------------------------------------- | ---------------------------------- |
| **When**        | Roadmap batch (`P*-*`)                             | Phasical task/epic (`#N`, …)               | Ad-hoc; no board or plan           |
| **State**       | `- [ ]` / `- [~]` / `- [x]` / `- [!]` in plan file | `TodoWrite` + Phasical status              | `TodoWrite` in chat                |
| **In progress** | `[~]` (Lane S agent or Lane P batch prep)          | Execution → **in-progress**             | todo `in_progress`                 |
| **Done**        | Verifier → `[x]` on plan file                      | Verifier PASS → **done**                | todo `completed` after verify PASS |
| **Failed**      | Verifier → `[!]` on plan file                      | Verifier → FAIL comment; **in-progress** (rework) | todo `pending`                     |

Pick mode on first turn from user message. Default to **plan-file mode** only when user asks for roadmap work and did not name a Phasical/GitHub task.

---

## Default: plan first, then dispatch

Present a **batch plan**, then **start batch 1** unless user said `plan only` / `wait` / `don't start`.

Do **not** ask "go?" — the plan is the heads-up; execution follows unless paused.

### Batch plan format

```markdown
## Orchestrator plan — <target>

**Lane:** S | P · **Phasical sync:** ON | OFF · **Branch:** <integration branch>

| Batch | Lane | Tasks | Notes |
| ----- | ---- | ----- | ----- |
| 1     | S    | #47   | …     |
| 2     | P    | #48, #49 | disjoint scopes |

**Skipped (Done):** —
**Blocked:** —
**Epic linkage:** parent in-progress when child starts; parent done on final child PASS

→ Starting batch 1…
```

User modifiers: `serial` · `from P6` / `from #N` · `plan only` · `no slack` · `don't update Phasical`

---

## Startup sequence

1. Read `.agents/project/orchestrator/project.config.md` — confirm repo path, integration branch, Phasical projectId.
2. Read `.agents/project/workspace-notes.md` (create on first durable note).
3. **Pick mode** (plan-file / Phasical / chat).
4. **Plan-file:** read plan file — next TODO with satisfied deps.
5. **Phasical:** load task(s) via `get_task` / `list_tasks` / `get_task_relations`.
6. **Present batch plan** → dispatch batch 1 (unless paused).
7. Note Slack override if user said `no slack` or `slack to <email>`.

**Commits:** Local commits per task by default. **Never push** unless user explicitly asks. Never push to production branch if `project.config.md` marks it protected.

**Phasical sync (default ON):** Resolve taskIds + `githubIssueNumber`s; pass role-specific PHASICAL SYNC blocks from `.agents/project/orchestrator/prompt-templates.md`. Sub-agents perform updates — orchestrator recovers only on failure. Skip if user says **"don't update Phasical"**.

### Phasical status ownership (non-negotiable)

| Column      | Who may set it | When                                              |
| ----------- | -------------- | ------------------------------------------------- |
| in-progress | **Execution**  | First action, before session memory               |
| in-review   | **Execution**  | Pre-verifier handoff                              |
| done        | **Verifier**   | After all verification layers PASS only           |

See `.agents/skills/orchestrator/references/phasical-sync.md`.

---

## Commit linkage audit (mandatory)

**Rule:** Phasical **done** or plan `[x]` is insufficient without a matching commit on the integration branch.

After **every verifier PASS**, before advancing queue or marking Done:

```bash
# Phasical task #N marked Done in this run:
git log <base>..HEAD --grep='\[#N\]'
git log <base>..HEAD --grep='fixes #N'

# Roadmap task P6-T03:
git log <base>..HEAD --grep='\[P6-T03\]'
```

`<base>` = commit before orchestrator run started, or merge-base with `origin/<integration-branch>`.

| Result | Action |
| ------ | ------ |
| Task key / `fixes #N` found | OK — update progress |
| No matching commit | **FAIL** — re-dispatch execution |
| Uncommitted WRITE SCOPE changes | **FAIL** — execution did not finish handoff |

**Verifier Layer 3c3:** Confirm task commit in `git log`. Missing → **FAIL** even if AC and CI pass.

---

## Dispatching sub-agents

**Before every Task call:** run the **pre-dispatch gate** in `.agents/project/orchestrator/prompt-templates.md` and `.cursor/rules/09-sub-agent-prompt-contract.mdc`. Search the prompt for required markers. **Do not dispatch** if any marker is missing or forbidden shorthand is present.

When building a prompt:

1. **Header** — `SESSION-ID:`, task ID (roadmap `P*-*` or GitHub `#N`), lane/git context
2. **Acceptance criteria** — verbatim bullets from plan row or Phasical description
3. **Doc references** — plan Doc Ref or `§` citations (`.agents/project/orchestrator/doc-index.md`)
4. **READ SCOPE** / **WRITE SCOPE** — absolute paths; verifier scope is Phasical MCP + authorized plan edits only
5. Session ID: `<TASK-ID>-<YYYYMMDD>-<4hex>` — **same** for execution + verifier
6. **Lane** (`S` or `P`) and git context (branch, worktree, `STAGING_BASE_SHA` for Lane P)
7. **Epic context** — parent key, sibling deps, `CLOSE_PARENTS` when final child
8. **PHASICAL SYNC block** — execution or verifier variant — **copy verbatim** from prompt-templates, including **STATUS SYNC TABLE**; fill taskIds + `#N`
9. **PHASICAL COMMENT CONTRACT** — **mandatory on every verifier prompt** when Phasical sync on
10. **COMMIT CONTRACT block** — **mandatory on every execution prompt** (even when Phasical sync off)
11. **SESSION TIME TRACKING** — when PHASICAL SYNC present (from prompt-templates.md)
12. **SCOPED CI GATE** — mandatory in every execution and verifier prompt (full block — not a one-line filter)
13. **DB MIGRATIONS** — mandatory in every execution prompt (from prompt-templates.md)
14. **PLAN FILE GUARD** — mandatory when plan file in WRITE SCOPE (SlugBase pattern)
15. **WORKTREE ISOLATION** — Lane P execution only

Use `.agents/project/orchestrator/prompt-templates.md`. Copy template blocks **verbatim** — do not summarize PHASICAL SYNC, COMMENT CONTRACT, or COMMIT CONTRACT into prose. One prompt = one **leaf** task unless user requested batching or shared-file serialization.

**Do not dispatch** if pre-dispatch gate fails. Execution: missing PHASICAL SYNC (when enabled) or COMMIT CONTRACT. Verifier: missing PHASICAL SYNC, COMMENT CONTRACT, or SCOPED CI GATE block.

**Never** pre-decide verification outcome in the prompt (`Phasical PASS`, `leaf done`, etc.). **Never** mark verifier `READ-ONLY` or `readonly: true` when Phasical sync requires MCP writes.

### Phasical parent epic batches

1. `get_task` epic + `get_task_relations` → subtask list.
2. Read epic **Suggested implementation order** in description.
3. Build batch plan; satisfy `blocks` relations and prose deps.
4. Execution marks epic **in-progress** when any subtask starts.
5. Last subtask verifier (or orchestrator recovery) marks epic **done**.

### Sub-agent types

| Type               | Use when                                              |
| ------------------ | ----------------------------------------------------- |
| `best-of-n-runner` | **Lane P execution** — isolated worktree per task       |
| `generalPurpose`   | Lane S; branch verify; integration conflict analysis    |
| `shell`            | Worktree cleanup; integration merges                  |
| `explore`          | Read-only discovery                                   |
| `ci-investigator`  | Single failing CI check on a PR                       |

**Model:** Do not hardcode unless user specifies.

**Parallel Lane P:** `run_in_background: true`, max **3** concurrent.

### Monitoring background sub-agents

Follow `.agents/skills/orchestrator/references/sub-agent-monitoring.md` — **mandatory**.

- **Git alone is not liveness.** Intake/triage/execution may show no commits while still working.
- Check transcript only when the agent **appears** stalled — not on every poll.
- **Two-sample rule:** read transcript → wait **10–20s** → read again. No progress on both → likely stalled.
- **Terminate the old agent before spawning a replacement.** Never run two agents on the same task.
- After confirmed stall: audit partial work (Phasical tasks, commits, files) and **dedupe** before re-dispatch.

While sub-agents run: orchestrator does **not** take over their WRITE scope (no implementation, no intake `create_task`, no verifier actions).

### Cross-cutting scope exceptions

Shared root files (`package.json`, lockfiles, CI workflows) may be touched only when required by AC, minimal, and justified in session memory. **Lane P:** shared-file contention → serialize or Lane S.

---

## Parallelism — Lane S / P / B

| Lane | When | Who touches integration branch |
| ---- | ---- | -------------------------------- |
| **S** | Single task; migrations; shared contracts; uncertain overlap | Execution + verifier |
| **P** | 2–3 tasks; disjoint WRITE scopes; no migration conflict | Integration agent + batch verifier only |
| **B** | Same file in multiple tasks | Serialize or split batch |

**Default when uncertain:** Lane S.

**Serialize (force Lane S) when:** DB migrations, shared types/contracts, root tooling, dependency chains incomplete, merge conflicts on integration branch.

**Lane P hard rules:**

- Execution agents **never** commit to integration branch.
- Pin `STAGING_BASE_SHA` at batch start.
- Branch verifiers **never** edit the plan file.
- Batch verifier is the **only** agent that sets `[x]`/`[!]` for Lane P tasks.
- Never mix Lane P and Lane S in the same batch.
- **Worktree isolation mandatory** — use `best-of-n-runner`; fresh worktrees need `pnpm install` first (SlugBase pattern).

Always: **execute batch → verify (per task) → integrate (Lane P) → batch verify → next batch**.

---

## Lane P batch lifecycle

### Batch metadata

| Field              | Example                          |
| ------------------ | -------------------------------- |
| `BATCH_ID`         | `20260722-a3f1`                  |
| `STAGING_BASE_SHA` | integration branch HEAD at start |
| Per task           | TASK-ID, SESSION ID, branch, worktree path |

### Flow

```text
1. Record BATCH_ID + STAGING_BASE_SHA
2. Set [~] on plan file for batch tasks (Lane P prep)
3. Spawn execution agents (best-of-n-runner, run_in_background: true)
4. Each execution: session memory + one implementation commit on task branch
5. Spawn branch verifier per completed task (in worktree)
6. Branch verifier PASS → report; no plan file write
7. Integration agent: merge PASS branches onto integration branch
8. Batch verifier: scoped smoke + plan [x]/[!]
9. Commit-linkage audit for each integrated task
10. Cleanup shell agent: remove worktrees; delete merged branches
```

---

## Execution agents

### Lane S (serial on integration branch)

**Handoff gates (non-negotiable):**

```text
1. Phasical in-progress (FIRST — before code)
2. Implement + session memory
3. Phasical in-review
4. Single implementation commit with [#N] (COMMIT CONTRACT)
```

1. **Phasical (if PHASICAL SYNC)** — first action: `in-progress` (leaf + parent when subtask). **Blocked if MCP fails.**
2. **Session memory** — `.agents/project/agent-memory/active/<SESSION-ID>.md`; set `started` after in-progress succeeds
3. **Implementation** — task files only
4. **Scoped CI gate** — before commit (from prompt-templates.md)
5. **Pre-handoff** — `ended` + `duration` → `in-review` → **one implementation commit** per COMMIT CONTRACT (verifier checks `git log --grep='\[#N\]'`)

Never push unless user asks. Stage explicit paths only. Never commit `agent-memory/**`.

**Forbidden for execution:** `done` status; verification comments; Phasical task IDs in commits; **commit before in-review**; **implementation before in-progress**.

### Lane P (isolated task branch)

Same flow on `orchestrator/<TASK-ID>` only. Never checkout integration branch. Plan file read-only.

### Commit messages

Every task commit must include a work-item key per project commit rules in `project.config.md`:

```
feat(<area>)[#123]: <imperative summary>
fix(<area>)[P6-T03]: <imperative summary>   # roadmap-only when no Phasical mirror
```

Subject ≤72 chars. Use **`[#N]`** for Phasical/GitHub tasks. Do not use GitHub smart commands (`#time`, `#comment`) — MCP owns sync.

---

## Verification agents

Never reuse a verifier thread across batches.

### Three layers (all must pass)

**Layer 1 — Scope audit:** committed paths vs WRITE SCOPE.

**Layer 2 — Scoped automated checks** from `.agents/project/orchestrator/prompt-templates.md` and `doc-index.md`. Mark `n/a` for undefined commands.

**Layer 3 — Logic review:**

- 3a. Each acceptance criterion implemented?
- 3b. Doc contract deviations with file:line + fix hint
- 3c. Security baseline (if project defines one)
- 3c2. Env vars registered (if applicable)
- 3c3. **Commit linkage** — `git log` contains `[#N]` or `[P*-*]` per audit rules
- 3d. **DB migrations** — hand-written migration SQL → **FAIL**
- 3c5. **Plan file integrity** — PLAN FILE GUARD; unauthorized row changes → **FAIL**

| Result | Plan-file mode | Phasical | Session memory |
| ------ | -------------- | ----- | -------------- |
| PASS   | `[x]` that row only | Done + mandatory comment | Delete or archive locally |
| FAIL   | `[!]` that row only | in-progress + FAIL comment | Append VERIFICATION FAILED |

**Verifier Phasical duties:** see [references/phasical-sync.md](references/phasical-sync.md).

### Lane P — branch verifier

Same three layers in task worktree. No plan file write on PASS/FAIL.

### Lane P — batch verifier

Post-merge scoped smoke on integration branch. Sets `[x]`/`[!]` on plan file. Confirms commit-linkage for integrated tasks.

---

## Integration agent

Only agent that commits to integration branch during Lane P batch. Merge `--no-ff`, one task at a time. On conflict → stop and report.

---

## Worktree cleanup

After batch closes, shell agent removes worktrees and deletes merged task branches.

---

## Session memory lifecycle

Path: `.agents/project/agent-memory/` — **never committed**. See `agent-memory/README.md`.

| Step | Who | Action |
| ---- | --- | ------ |
| Before dispatch | Orchestrator | Generate SESSION ID |
| Phase 1 | Execution | Create `active/<SESSION-ID>.md`; set `started` when PHASICAL SYNC |
| Pre-handoff | Execution | `ended` + `duration`; in-review; one commit |
| Verifier end | Verifier | Phasical comment + done/in-progress; verification timing |
| PASS | Verifier | Mandatory Done comment → done |
| FAIL | Verifier | FAIL comment → in-progress; append VERIFICATION FAILED locally |

**Retry after FAIL:** same SESSION ID.

---

## Workspace memory

Path: `.agents/project/workspace-notes.md` — durable conventions, CI quirks, merge conflict patterns.

---

## Session-end Slack DM (optional)

When `.agents/project/orchestrator/slack-session-end.md` exists, send **once** when the run loop exits. Never DM the Slack service account — DM the operator per local config.

---

## Minimal run loop

1. Read workspace-notes + project.config; pick mode.
2. Load next task(s); check deps.
3. Present batch plan → dispatch batch 1.
4. Collect execution outputs; verify each task.
   - Background agents: monitor per `sub-agent-monitoring.md` — transcript two-sample before any stall verdict.
5. Lane P: integrate → batch verify → commit-linkage audit.
6. Reconcile plan / Phasical status; update workspace-notes.
7. Repeat or send session-end Slack DM.
8. Report batch result + next steps.

---

## Anti-patterns

- **Declaring sub-agent stalled from git silence or one transcript read**
- **Spawning a replacement sub-agent without terminating the old one**
- **Parent taking over sub-agent WRITE scope** (intake `create_task`, implementation, verification) while sub-agent may still run
- Orchestrator reading spec doc bodies or implementation files
- Orchestrator reading session memory **contents** (filenames only)
- Pasting spec bodies or full task descriptions into sub-agent prompts
- Editing roadmap checkboxes for Phasical-only tasks
- Marking epic done before all in-scope subtasks PASS
- **Execution agent setting Phasical done**
- **Skipping commit-linkage audit** after verifier PASS
- **Marking Done without `fixes #N` / `[#N]` in git history**
- **Lane P without `best-of-n-runner`**
- **Dispatching before batch plan** (unless user gave explicit single-task command)
- **Verifier `readonly: true`** when Phasical sync requires MCP writes (see `09-sub-agent-prompt-contract.mdc`)
- **Pre-deciding verification outcome** in sub-agent prompt (`Phasical PASS`, `leaf done`)
- **Dispatching with shorthand prompts** — one-line Phasical/CI instead of verbatim template blocks
- **Omitting PHASICAL COMMENT CONTRACT** from verifier prompts when Phasical sync is on
- Committing session memory
- Blanket `git add .` / `-A`
- Pushing without user request
- **Omitting SCOPED CI GATE, DB MIGRATIONS, or PLAN FILE GUARD** from prompts when required
- **Omitting PHASICAL SYNC or COMMIT CONTRACT** from execution prompts (or summarizing STATUS SYNC TABLE instead of verbatim copy)
- **Dispatching execution without filled `githubIssueNumber` / `[#N]`** in COMMIT CONTRACT
