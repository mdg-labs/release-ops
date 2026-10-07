# Sub-agent monitoring

**Applies to any chat that dispatches sub-agents** with the Claude Code **Agent** tool (background or not): the orchestrator, or a main chat delegating `/kaneo-intake`, `/kaneo-triage`, exploration, CI investigation, etc.

## The failure mode

The parent infers **stalled** from git silence (or one quiet minute), takes over the sub-agent's WRITE scope and duplicates work. Example: kaneo-intake is still creating tasks via `mcp__Kaneo__create_task` while the parent also creates the same tasks.

**Git history alone is not liveness.** MCP-heavy sub-agents (intake, triage, verifiers posting Kaneo comments) can go a long time without a commit while still working.

---

## Liveness signals (use together)

| Signal | What to check |
| ------ | ------------- |
| **Sub-agent transcript** | New tool calls, MCP writes, assistant turns in the agent's transcript / output file (path in the Agent launch result) |
| **Task notification** | Background agent completion notification; `/tasks` still shows it running |
| **Git** | New commits on `dev` (Lane S) or `orchestrator/<TASK-ID>` (Lane P), when the sub-agent's WRITE scope commits |
| **Session memory** | `.agents/project/agent-memory/active/<SESSION-ID>.md` mtime or new lines (execution agents only) |
| **Kaneo activity** | `mcp__Kaneo__list_task_activity` / `list_task_comments` on the task — status moves or new comments |

The transcript is the **primary** signal for intake / triage / explore. Git is supplementary.

---

## Stall detection (two-sample — mandatory)

**Never** declare a sub-agent stalled from a single check.

### Step 1 — Only when it appears stalled

Investigate only on a real stall **suspicion**, e.g.:

- No completion notification for an unusually long time **and** no visible progress
- The user asks what is happening
- You are about to re-dispatch or take over work yourself

Routine waiting: rely on the completion notification — do not poll, and do not read transcripts of a healthy run.

### Step 2 — First transcript read

Read the sub-agent transcript (not just git). Note the last timestamp, last tool call, last assistant turn, and whether an MCP call (`create_task`, `update_task_status`, `create_task_comment`, …) is in flight.

### Step 3 — Wait 10–20 seconds

Do not spawn, terminate or take over during this wait.

### Step 4 — Second transcript read

Compare with step 2. **Progress** = new transcript lines, new tool calls, or completed MCP results.

| Second read | Verdict |
| ----------- | ------- |
| Clear progress | **Not stalled** — keep waiting |
| No change | **Likely stalled** — go to recovery |

### Step 5 — Recovery (confirmed stall only)

1. **Terminate the existing sub-agent first** — `TaskStop` on its task id; confirm it stopped.
2. **Audit partial work** before re-dispatch:
   - Kaneo: `mcp__Kaneo__search` (`q`, `workspaceId`, `projectId`) for titles created in this run; `get_task` for the current column of the task in flight
   - Git: `git log`, `git status` on `dev` or the task branch
   - Files: intake plan drafts under `.agents/project/kaneo-intake/plans/`, session memory
3. **Dedupe** — continue or enrich what exists; do **not** blindly recreate tasks, commits or files.
4. **Then** spawn one replacement sub-agent (or `SendMessage` the stopped agent with explicit new instructions).

**Forbidden:** a second agent on the same work item while the first is still running.

---

## Parent agent: do not take over

While a sub-agent is in flight for a scoped job (kaneo-intake, kaneo-triage, execution, verifier):

- **Do not** perform that job's WRITE actions yourself — no `create_task`, no implementation edits, no verifier comment or `implemented` / `in-progress` transition.
- **Do not** assume a stall because git is quiet.
- **Do** run the two-sample transcript check above.
- **Do** terminate → audit → dedupe → re-dispatch, on a confirmed stall only.

---

## Quick reference

```text
appears stalled?
  → read transcript (sample 1)
  → wait 10–20s
  → read transcript (sample 2)
  → progress? → keep waiting
  → no progress? → TaskStop old agent → audit partial work → dedupe → spawn replacement
```
