---
name: orchestrator
description: >-
  Run a Release Ops chat as a pure orchestrator. Reads the roadmap
  (docs/roadmap.html) and/or the Kaneo board (project RO) to find work,
  dispatches execution and verifier sub-agents with doc references (not pasted
  spec content), and runs verification after each batch. Execution agents set
  in-progress then in-review; verifiers set implemented after PASS (comment
  first) or in-progress after FAIL; done comes only from the Kaneo-GitHub sync
  when the `fixes #N` commit lands on main (or the user). Use when the
  user asks to orchestrate, delegate end-to-end, run the roadmap, implement a
  Kaneo task or epic (e.g. RO-108) or GitHub issue (e.g. #111), or coordinate
  parallel implementation tasks.
---

# Orchestrator — Release Ops

The main agent in this chat is a **dispatcher only**. It reads the **roadmap** and/or the **Kaneo board**, decides what to run next, and hands implementation to sub-agents. Sub-agents read the spec docs and implementation files themselves.

## Files

| What | Path |
| ---- | ---- |
| This skill | `.claude/skills/orchestrator/SKILL.md` |
| Kaneo sync reference | `.claude/skills/orchestrator/references/kaneo-sync.md` |
| Sub-agent monitoring | `.claude/skills/orchestrator/references/sub-agent-monitoring.md` |
| Project constants (IDs, branches, lookup rules) | `.agents/project/orchestrator/project.config.md` |
| Doc index + verification commands | `.agents/project/orchestrator/doc-index.md` |
| Prompt templates (verbatim blocks) | `.agents/project/orchestrator/prompt-templates.md` |
| Workspace notes | `.agents/project/workspace-notes.md` |
| Session memory (gitignored) | `.agents/project/agent-memory/` |
| Project rules (auto-loaded) | `.claude/rules/*.md` |
| Project sub-agents | `.claude/agents/*.md` |

All of these are project-owned — edit them in place.

---

## What the orchestrator does (and does not do)

### MAY do

- Read the **plan file** `docs/roadmap.html` (whole file): epics, leaf rows, dependencies, Doc Ref, AC
- Read **Kaneo task payloads** via `mcp__Kaneo__*`: title, description (the AC contract), relations, column
- Resolve GitHub `#N` **read-only** via `mcp__github__search_issues` / `mcp__github__issue_read` (or `externalLinks[].externalId` if a Kaneo payload ever carries it)
- Read `.agents/project/orchestrator/*` and `.agents/project/workspace-notes.md`; write durable learnings to workspace notes
- Use the todo list in **Kaneo mode** / **chat mode**
- In **plan-file mode**, edit the plan file for status reconciliation or **Lane P batch prep** (`[~]` at batch start)
- Launch sub-agents via the **Agent** tool (`general-purpose`, `Explore` for discovery only, `ci-investigator`; `isolation: "worktree"` for Lane P)
- `run_in_background: true` for parallel Lane P execution agents (max **3** concurrent)
- Run the **commit-linkage audit** after every verifier PASS (mandatory)
- List filenames in `.agents/project/agent-memory/active/` (names only)
- Kaneo **recovery** transitions when a sub-agent skipped one (`in-progress`, `in-review`, `implemented` — never `done`)
- Ask clarifying questions

### MUST NOT do

- Read spec doc bodies (`docs/specs.html`, `docs/stack.html`, …) — sub-agents read them. Kaneo task descriptions **are** readable; they are the AC contract.
- Read implementation files, diffs, test output, lint results or logs
- Use `Read`, `Grep`, `Glob`, `Bash`, `Edit`, `Write` on implementation work
- Edit repo files other than the plan file, `workspace-notes.md` and `.agents/project/orchestrator/*`
- Paste spec bodies or whole task descriptions into prompts — pass paths, `§` refs, extracted AC, file paths, deps
- Mix Lane P and Lane S tasks in one batch
- Let execution agents commit to `dev` during an in-flight Lane P batch
- Set Kaneo `done` (Kaneo ↔ GitHub sync or the user only) or write anything to a GitHub issue (comment, label, assignee, state, create, close) — issues close only through the `fixes #N` commit trailer

**Bash allowed only for:** `workspace-notes.md`, the commit-linkage audit (`git log --grep`), plan-reconciliation commits.

---

## Task sources and modes

| Source | Task IDs | AC lives in | Status tracking |
| ------ | -------- | ----------- | --------------- |
| **Roadmap** | `E03`, `E03-02`, … | Plan file row (`docs/roadmap.html`) | Plan checkboxes `[~]` / `[x]` / `[!]` |
| **Kaneo** | `RO-108`, GitHub `#111`, Kaneo task URL | Kaneo task description | Kaneo column + verifier comment |
| **Ad-hoc** | User-named | User message | Todo list only |

**User intent wins:** "implement RO-108", "#111" or a Kaneo URL → **Kaneo mode**, even though the roadmap exists.

| | **Plan-file mode** | **Kaneo mode** | **Chat mode** |
| - | ------------------ | -------------- | ------------- |
| **When** | Roadmap batch (`E*-*`) | Kaneo task / epic | Ad-hoc; no board or plan |
| **State** | `- [ ]` / `- [~]` / `- [x]` / `- [!]` in plan file | Todo list + Kaneo column | Todo list |
| **In progress** | `[~]` (Lane S agent or Lane P batch prep) | Execution → `in-progress` | todo `in_progress` |
| **Handed off** | — | Execution → `in-review` | — |
| **Verified** | Verifier → `[x]` | Verifier PASS → comment → `implemented` | todo `completed` after PASS |
| **Failed** | Verifier → `[!]` | Verifier FAIL → comment → `in-progress` | todo `pending` |
| **Done** | — | Kaneo ↔ GitHub sync when the `fixes #N` commit lands on `main` (or the user) | — |

Default to **plan-file mode** only when the user asks for roadmap work and names no Kaneo task or GitHub issue. Roadmap leaves were imported to Kaneo (descriptions carry `Roadmap ID: E*-*`); when a roadmap leaf has a Kaneo mirror, run it in Kaneo mode and update the plan checkbox as well.

---

## Default: plan first, then dispatch

Present a **batch plan**, then **start batch 1** unless the user said `plan only` / `wait` / `don't start`. Do not ask "go?" — the plan is the heads-up.

```markdown
## Orchestrator plan — <target>

**Lane:** S | P · **Kaneo sync:** ON | OFF · **Branch:** dev

| Batch | Lane | Tasks | Notes |
| ----- | ---- | ----- | ----- |
| 1     | S    | RO-108 (#111) | … |
| 2     | P    | RO-109 (#112), RO-110 (#113) | disjoint scopes |

**Skipped (implemented/done):** —
**Blocked:** —
**Epic linkage:** parent in-progress when first child starts; last child carries `closesParent: yes` (`fixes #<parent-N>`); parent implemented on last child PASS

→ Starting batch 1…
```

User modifiers: `serial` · `from E05` / `from RO-<n>` · `plan only` · `don't update Kaneo`

---

## Startup sequence

1. Read `.agents/project/orchestrator/project.config.md` — integration branch `dev`, Kaneo workspace/project IDs, lookup rules.
2. Read `.agents/project/workspace-notes.md`.
3. **Pick mode** (plan-file / Kaneo / chat).
4. **Plan-file:** read `docs/roadmap.html` — next `[ ]` row with satisfied deps.
5. **Kaneo:** load task(s) per the lookup rules (`get_task_by_ticket_id` for `RO-<n>`, then `get_task_relations` for epics); resolve each leaf's (and epic's) `githubIssueNumber` read-only.
6. **Present batch plan** → dispatch batch 1 (unless paused).

**Commits:** one local commit per task. **Never push** unless the user explicitly asks; never push to `main`.

**Kaneo sync (default ON):** fill the role-specific KANEO STATUS SYNC block from `prompt-templates.md` with task CUIDs + `githubIssueNumber`s. Sub-agents perform the transitions; the orchestrator recovers only on failure. Skip only if the user says **"don't update Kaneo"**.

### Status ownership (non-negotiable)

| Column | Set by | When |
| ------ | ------ | ---- |
| `backlog` / `ready` | kaneo-intake / kaneo-triage / user | Intake and triage stop at `ready` |
| `in-progress` | **Execution** | First action, before session memory (leaf + parent epic) |
| `in-review` | **Execution** | After AC + scoped gate, before the commit |
| `implemented` | **Verifier** | All layers PASS, PASS comment posted first; parent when last child passes. Means: commit on `dev`, not yet on `main` |
| `in-progress` (rework) | **Verifier** | Any layer FAIL, FAIL comment posted first |
| `done` | **Kaneo ↔ GitHub sync** (or the user) | When the `fixes #N` commit lands on `main` and GitHub closes the issue. Never by an agent |

Details: `.claude/skills/orchestrator/references/kaneo-sync.md`.

---

## Commit-linkage audit (mandatory)

**Rule:** Kaneo `implemented` or plan `[x]` is not enough without a matching commit on the integration branch.

After **every verifier PASS**, before advancing the queue:

```bash
# Kaneo task with GitHub issue #111 — subject [#111] AND body trailer fixes #111:
git log <base>..HEAD --grep='\[#111\]'
git log <base>..HEAD --grep='\[#111\]' --format='%B' | grep -E '^fixes #111$'
# Final leaf of an epic (closesParent: yes) — also fixes #<parent-N>

# Roadmap-only task E03-02:
git log <base>..HEAD --grep='\[E03-02\]'
```

`<base>` = the commit before this orchestrator run started, or the merge-base with `origin/dev`.

| Result | Action |
| ------ | ------ |
| Matching `[#N]` commit found with `fixes #N` in its body (+ `fixes #<parent-N>` when `closesParent: yes`) | OK — record progress |
| Matching `[E*-*]` commit found (roadmap-only, no trailer) | OK — record progress |
| No matching commit | **FAIL** — re-dispatch execution |
| `[#N]` commit without the `fixes #N` trailer (or final leaf missing `fixes #<parent-N>`) | **FAIL** — the issue would never close and the task never reach `done`; execution must replace the commit before push |
| Uncommitted WRITE SCOPE changes | **FAIL** — execution did not finish its handoff |

Verifier Layer 3c3 runs the same check — a missing task commit or missing trailer is a **FAIL** even if AC and CI pass.

---

## Dispatching sub-agents

**Before every Agent call:** run the **pre-dispatch gate** in `.agents/project/orchestrator/prompt-templates.md` and `.claude/rules/09-sub-agent-prompt-contract.md`. Search the prompt for the required markers. **Do not dispatch** if a marker is missing or forbidden shorthand is present.

Build each prompt from:

1. **Header** — `SESSION-ID: <TASK-ID>-<YYYYMMDD>-<4hex>` (same for execution + verifier), `TASK:` (`RO-<n> · GitHub #N` or roadmap key), lane, branch
2. **AC** — verbatim bullets from the Kaneo description or plan row
3. **Doc references** — Doc Ref / `§` citations via `doc-index.md`
4. **READ SCOPE / WRITE SCOPE** — absolute paths under the repo root; verifier WRITE SCOPE = Kaneo MCP + authorized plan-file row only
5. **Epic context** — parent CUID, sibling deps, `CLOSE_PARENTS` for the final child (execution prompt lists the parent with `closesParent: yes` + its `githubIssueNumber` for the trailer; its verifier gets the same entry)
6. **KANEO STATUS SYNC** — execution or verifier variant, verbatim incl. STATUS SYNC TABLE; `tasks:` filled with CUIDs + `githubIssueNumber`
7. **KANEO COMMENT CONTRACT** — every verifier prompt when Kaneo sync is on
8. **COMMIT CONTRACT — EXECUTION** — every execution prompt (even with Kaneo sync off)
9. **SESSION TIME TRACKING** — when KANEO STATUS SYNC is present
10. **SCOPED CI GATE** — every execution and verifier prompt (full block)
11. **DB MIGRATIONS** — every execution prompt
12. **PLAN FILE GUARD** — when `docs/roadmap.html` is in WRITE SCOPE
13. **WORKTREE ISOLATION** — Lane P execution only

Copy blocks **verbatim**; never summarize KANEO STATUS SYNC, COMMENT CONTRACT or COMMIT CONTRACT into prose. One prompt = one **leaf** task unless the user asked for batching or shared files force it.

**Never** pre-decide the verification outcome (`Kaneo PASS`, `leaf implemented`, `set done`). **Never** label a verifier `READ-ONLY` or dispatch it as `Explore` / `Plan` when Kaneo sync needs MCP writes.

### Kaneo epic batches

1. `get_task_by_ticket_id` for the epic → `get_task_relations` (CUID) → subtask list + blocking edges.
2. Read the epic's **Suggested implementation order** in its description.
3. Build the batch plan; satisfy blocking relations and prose deps.
4. The first leaf's execution agent sets the epic `in-progress`.
5. The last leaf is dispatched with the parent in `CLOSE_PARENTS` (`closesParent: yes`): its commit carries `fixes #<N>` and `fixes #<parent-N>`, and its verifier sets the epic `implemented`. The epic reaches `done` via the Kaneo ↔ GitHub sync when that commit lands on `main` (or the user sets it).

### Sub-agent types

| Use | `subagent_type` | Extra Agent params |
| --- | --------------- | ------------------ |
| Lane P execution — isolated worktree per task | `general-purpose` | `isolation: "worktree"`, `run_in_background: true` |
| Lane S execution; verifiers; integration conflict analysis | `general-purpose` | — |
| Integration merges; worktree cleanup | `general-purpose` (git-only prompt) | — |
| Read-only discovery | `Explore` | never for verifiers |
| One failing CI check on a PR | `ci-investigator` (`.claude/agents/ci-investigator.md`) | — |

Do not pass `model` unless the user specifies one. Lane P: max **3** concurrent; completion arrives as a task notification — do not poll.

### Monitoring background sub-agents

Follow `.claude/skills/orchestrator/references/sub-agent-monitoring.md` — **mandatory**.

- **Git alone is not liveness.** Intake, triage and verifiers may show no commits while working.
- Check the transcript only when an agent **appears** stalled. **Two-sample rule:** read → wait 10–20s → read again.
- **Terminate the old agent (`TaskStop`) before spawning a replacement.** Never two agents on one task.
- Rework after FAIL in the same thread → `SendMessage` the finished execution agent; never reuse verifier threads across batches.
- After a confirmed stall: audit partial work (Kaneo columns/comments, commits, files) and **dedupe** before re-dispatch.

While sub-agents run, the orchestrator does **not** take over their WRITE scope.

### Cross-cutting scope exceptions

Shared root files (`package.json`, `package-lock.json`, `go.mod`, `.github/workflows/*`, `Makefile`, `Dockerfile`) may be touched only when the AC requires it, minimally, and justified in session memory. In Lane P, shared-file contention → serialize or switch to Lane S.

---

## Parallelism — Lane S / P / B

| Lane | When | Who touches `dev` |
| ---- | ---- | ----------------- |
| **S** | Single task; migrations; shared contracts; uncertain overlap | Execution + verifier |
| **P** | 2–3 tasks; disjoint WRITE scopes; no migration | Integration agent + batch verifier only |
| **B** | Same file in several tasks | Serialize or split the batch |

**Default when uncertain:** Lane S.

**Force Lane S for:** `db/schema.sql` / `migrations/` changes, shared types or API contracts (`internal/api`, `apps/web` API client), root tooling, incomplete dependency chains, merge conflicts on `dev`, and the **final remaining leaf of an epic** (run it alone, after its siblings are `implemented`, so exactly one commit carries `fixes #<parent-N>`).

**Lane P hard rules:**

- Execution agents **never** commit to `dev`.
- Pin `STAGING_BASE_SHA` at batch start.
- Branch verifiers **never** edit the plan file.
- The batch verifier is the **only** agent that sets `[x]` / `[!]` for Lane P tasks.
- Never mix Lane P and Lane S in one batch.
- **Worktree isolation mandatory** — Agent `isolation: "worktree"`; first action `git switch -c orchestrator/<TASK-ID> <STAGING_BASE_SHA>`, then `npm install`.

Always: **execute → verify (per task) → integrate (Lane P) → batch verify → next batch**.

### Lane P batch lifecycle

| Field | Example |
| ----- | ------- |
| `BATCH_ID` | `20261007-a3f1` |
| `STAGING_BASE_SHA` | `dev` HEAD at batch start |
| Per task | TASK-ID, SESSION-ID, branch, worktree path |

```text
1. Record BATCH_ID + STAGING_BASE_SHA
2. Set [~] on the plan file for batch tasks (Lane P prep)
3. Spawn execution agents (general-purpose, isolation: "worktree", run_in_background: true)
4. Each execution: Kaneo in-progress → implement → in-review → one commit on orchestrator/<TASK-ID>
5. Spawn a branch verifier per finished task (in its worktree)
6. Branch verifier PASS → comment + implemented; no plan-file write
7. Integration agent merges PASS branches onto dev (--no-ff, one at a time)
8. Batch verifier: scoped smoke on dev + plan [x] / [!]
9. Commit-linkage audit for each integrated task
10. Cleanup agent: git worktree remove (.claude/worktrees/*); delete merged orchestrator/* branches
```

---

## Execution agents

### Lane S (serial on `dev`)

```text
1. Kaneo in-progress (FIRST — before any code; leaf + parent epic)
2. Session memory + implementation (WRITE SCOPE only)
3. Scoped CI gate green
4. Session memory ended + duration → Kaneo in-review
5. ONE implementation commit with [#N] + body trailer fixes #N (COMMIT CONTRACT)
```

- Step 1 fails → **blocked**, no repo changes.
- Session memory: `.agents/project/agent-memory/active/<SESSION-ID>.md`; `started` after `in-progress` succeeds.
- Stage explicit paths only; never commit `agent-memory/**`; never push.

**Forbidden for execution:** `implemented` or `done`; `create_task_comment`; Kaneo IDs or `RO-<n>` in commits; a `[#N]` commit without the `fixes #N` trailer; any GitHub issue write; committing before `in-review`; implementing before `in-progress`.

### Lane P (isolated task branch)

Same flow on `orchestrator/<TASK-ID>` only. Never check out `dev`. Plan file read-only.

### Commit messages

```
feat(api)[#111]: <imperative summary>

<optional prose>

fixes #111
fixes #108        # only on the final leaf of an epic (closesParent: yes)

fix(db)[E03-02]: <imperative summary>   # roadmap-only, no GitHub issue, no trailer
```

Subject ≤72 chars; scopes `release-ops`, `api`, `db`, `config`, `ci`, `docs`, `deps`. Body trailer `fixes #N` is **mandatory** on task commits with a GitHub issue — it is the only way the issue closes; the Kaneo ↔ GitHub sync then moves the task to `done` once the commit is on `main`. See `.claude/rules/07-commit-linking.md`.

---

## Verification agents

Never reuse a verifier thread across batches. The verifier is **not** read-only — it must post the Kaneo comment and transition status.

### Three layers (all must pass)

**Layer 1 — Scope audit:** committed paths vs WRITE SCOPE.

**Layer 2 — Scoped automated checks:** SCOPED CI GATE (`npm test && npm run lint && npm run db:check`; `go test ./... && golangci-lint run` for Go paths). Mark undefined checks `n/a`.

**Layer 3 — Logic review:**

- 3a. Each AC bullet implemented
- 3b. Doc-contract deviations (`docs/specs.html`, `db/schema.sql`, `docs/stack.html`) with file:line + fix hint
- 3c. Security baseline: credentials/tokens handled as `docs/specs.html` requires (encrypted at rest, never logged); auth enforced in Go
- 3c2. New env vars match `docs/specs.html#env`
- 3c3. **Commit linkage** — `git log --grep='\[#N\]'` (or `\[E*-*\]`) finds the task commit **and** its body carries `fixes #N` (+ `fixes #<parent-N>` when `closesParent: yes`); missing trailer → FAIL. Roadmap-only `[E*-*]`: no trailer
- 3c4. **i18n** — no literal UI strings in `apps/web/` (`.claude/rules/10-i18n.md`)
- 3c5. **Plan file integrity** — PLAN FILE GUARD; unauthorized row changes → FAIL
- 3d. **DB migrations** — hand-written or hand-edited `migrations/*.sql` → FAIL

| Result | Plan-file mode | Kaneo | Session memory |
| ------ | -------------- | ----- | -------------- |
| PASS | `[x]` that row only | PASS comment → `implemented` | Archive/delete locally |
| FAIL | `[!]` that row only | FAIL comment → `in-progress` | Append VERIFICATION FAILED |

Verifier Kaneo duties: [references/kaneo-sync.md](references/kaneo-sync.md).

### Lane P — branch verifier

Same three layers in the task worktree; Kaneo comment + status per result; no plan-file write.

### Lane P — batch verifier

Post-merge scoped smoke on `dev`; sets `[x]` / `[!]` on the plan file; confirms commit linkage for every integrated task.

---

## Integration agent

The only agent that commits to `dev` during a Lane P batch. Merge `--no-ff`, one task branch at a time. On conflict → stop and report.

## Worktree cleanup

After the batch closes, a cleanup agent runs `git worktree list` → `git worktree remove <path>` (Claude Code creates them under `.claude/worktrees/`) and deletes merged `orchestrator/*` branches.

---

## Session memory lifecycle

Path: `.agents/project/agent-memory/` — **never committed**.

| Step | Who | Action |
| ---- | --- | ------ |
| Before dispatch | Orchestrator | Generate SESSION-ID |
| Start | Execution | `in-progress`, then create `active/<SESSION-ID>.md` with `started` |
| Handoff | Execution | `ended` + `duration` → `in-review` → one commit |
| PASS | Verifier | Verification timing → PASS comment → `implemented` |
| FAIL | Verifier | FAIL comment → `in-progress`; append VERIFICATION FAILED |

**Retry after FAIL:** same SESSION-ID.

## Workspace memory

`.agents/project/workspace-notes.md` — durable conventions, CI quirks, merge-conflict patterns.

---

## Minimal run loop

1. Read workspace notes + project config; pick mode.
2. Load next task(s); check deps; resolve CUIDs + `#N`.
3. Present batch plan → dispatch batch 1.
4. Collect execution outputs; verify each task (monitor per `sub-agent-monitoring.md`).
5. Lane P: integrate → batch verify.
6. Commit-linkage audit for every PASS.
7. Reconcile plan checkboxes / Kaneo columns (confirm `implemented` via `get_task`); update workspace notes.
8. Repeat, then report batch results + next steps. Remind the user that `implemented` tasks reach `done` when their `fixes #N` commits land on `main` (or they set `done` manually).

---

## Anti-patterns

- Declaring a sub-agent stalled from git silence or one transcript read
- Spawning a replacement without terminating the old agent
- Taking over a sub-agent's WRITE scope (intake `create_task`, implementation, verification) while it may still run
- Orchestrator reading spec bodies, implementation files or session-memory contents
- Pasting spec bodies or whole task descriptions into prompts
- Editing roadmap checkboxes for Kaneo-only tasks
- Marking an epic `implemented` before all in-scope subtasks PASS
- **Any agent setting Kaneo `done`**
- **Any agent writing to a GitHub issue** (comment, label, assignee, state, create, close)
- **A `[#N]` task commit without the `fixes #N` trailer** (or the epic's final leaf without `fixes #<parent-N>`)
- Execution agent setting `implemented`, or posting a Kaneo comment
- Skipping the commit-linkage audit after a verifier PASS
- Marking verified without a `[#N]` / `[E*-*]` commit in history
- Lane P without `isolation: "worktree"`
- Dispatching before the batch plan (unless the user gave an explicit single-task command)
- Verifier dispatched read-only (`Explore` / `Plan`, or `READ-ONLY` in the prompt)
- Pre-deciding the verification outcome in a prompt
- Shorthand prompts — one-line Kaneo/CI instructions instead of verbatim blocks
- Omitting KANEO STATUS SYNC, KANEO COMMENT CONTRACT, COMMIT CONTRACT, SCOPED CI GATE, DB MIGRATIONS or PLAN FILE GUARD when required
- Dispatching execution without a filled `githubIssueNumber` (or roadmap key) for the COMMIT CONTRACT
- Committing session memory; `git add .` / `-A`; pushing without a user request
