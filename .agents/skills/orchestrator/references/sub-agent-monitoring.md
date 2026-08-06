# Sub-agent monitoring

**Applies to any agent that dispatches sub-agents** via Task (`run_in_background: true` or not) — orchestrator, main chat delegating phasical-intake, triage, explore, etc.

## The failure mode

Parent infers **stalled** from git silence (or one quiet minute), takes over the sub-agent's WRITE scope, and duplicates work — e.g. phasical-intake still creating tasks via MCP while the parent also `create_task`s the same issues.

**Git history alone is not liveness.** MCP-heavy sub-agents (intake, triage, explore) may produce no commits for long stretches while still working.

---

## Liveness signals (use together)

| Signal | What to check |
| ------ | ------------- |
| **Sub-agent transcript** | New tool calls, MCP writes, assistant turns in the agent transcript JSONL |
| **Task / Await** | Background task notification or `Await` output still changing |
| **Git** | New commits on task branch or integration branch (when sub-agent has WRITE scope that commits) |
| **Session memory** | `active/<SESSION-ID>.md` mtime or new lines (execution agents only) |

Transcript is the **primary** signal for intake/triage/explore. Git is supplementary.

---

## Stall detection (two-sample — mandatory)

**Never** declare stalled from a single check.

### Step 1 — Appears stalled only

Investigate only when you have a stall **suspicion**, e.g.:

- No background notification for an unusually long time **and** no obvious progress
- User asks "what's happening?"
- You're about to re-dispatch or take over work yourself

Routine polling: prefer Task/Await notifications. Do not spam transcript reads on a healthy run.

### Step 2 — First transcript read

Read the sub-agent transcript (not just git). Note:

- Last timestamp / last tool call / last assistant turn
- Whether MCP activity is in flight (e.g. `create_task`, `list_tasks`, `update_task`)

### Step 3 — Wait 10–20 seconds

Do not spawn, terminate, or take over during this wait.

### Step 4 — Second transcript read

Compare to step 2. **Progress** = new transcript lines, new tool calls, or completed MCP results.

| Second read | Verdict |
| ----------- | ------- |
| Clear progress | **Not stalled** — keep waiting |
| No change | **Likely stalled** — proceed to recovery |

### Step 5 — Recovery (confirmed stall only)

1. **Terminate the existing sub-agent first** — `Task` with `interrupt: true` on the running agent id, or the documented terminate path. Wait until it is stopped.
2. **Audit partial work** before re-dispatch:
   - Phasical: `list_tasks` / search for titles created in this run
   - Git: `git log`, `git status` on relevant branches
   - Files: plan drafts, session memory
3. **Dedupe** — continue or enrich existing artifacts; do **not** blindly recreate tasks, commits, or files.
4. **Then** spawn one replacement sub-agent (or resume with explicit new instructions).

**Forbidden:** spawning a second agent on the same work item while the first is still running.

---

## Parent agent: do not take over

While a sub-agent is in-flight for a scoped skill (phasical-intake, phasical-triage, execution, verifier):

- **Do not** perform that skill's WRITE actions yourself (no `create_task`, no implementation edits, no verifier PASS/FAIL).
- **Do not** assume stalled because git is quiet.
- **Do** follow the two-sample transcript check above.
- **Do** terminate → audit → dedupe → re-dispatch on confirmed stall only.

---

## Quick reference

```text
appears stalled?
  → read transcript (sample 1)
  → wait 10–20s
  → read transcript (sample 2)
  → progress? → keep waiting
  → no progress? → terminate old agent → audit partial work → dedupe → spawn replacement
```
