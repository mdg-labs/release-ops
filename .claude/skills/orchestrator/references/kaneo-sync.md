# Kaneo sync reference — Release Ops

Orchestrator and sub-agents use this when a prompt includes a **KANEO STATUS SYNC** block. IDs and lookup rules also live in `.agents/project/orchestrator/project.config.md`; the copy-paste blocks live in `.agents/project/orchestrator/prompt-templates.md`.

## Board

| Field | Value |
| ----- | ----- |
| MCP server | `Kaneo` — tools `mcp__Kaneo__<tool>` |
| Workspace | MDG-Labs `ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj` |
| Project | Release Ops `z4janvyjsbbb0esishvd9gb8` — ticket key `RO` (refs `RO-<n>`) |
| Task URL | `https://cloud.kaneo.app/dashboard/workspace/ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj/project/z4janvyjsbbb0esishvd9gb8/task/<taskCuid>` |

Kaneo is the source of truth for task status, AC and verifier comments. GitHub issues in `mdg-labs/release-ops` mirror the tasks so commits can carry `[#N]` in the subject and the `fixes #N` trailer in the body; the trailer landing on `main` closes the issue and the Kaneo ↔ GitHub sync moves the task to `done`. **No agent ever writes to a GitHub issue**.

## Identifiers

| Identifier | Example | Used for |
| ---------- | ------- | -------- |
| Ticket ref | `RO-108` | Humans; lookup via `get_task_by_ticket_id` |
| Task CUID (`taskId`) | `k3x9…` | Every `mcp__Kaneo__*` write: `update_task_status`, `create_task_comment`, `get_task`, `get_task_relations` |
| GitHub issue `#N` | `#111` | Commit subject `[#N]` + body trailer `fixes #N` — resolved read-only |
| Roadmap key | `E03-02` | Commit subject `[E03-02]` when no GitHub issue exists; plan-file row |

Kaneo CUIDs and `RO-<n>` refs **never** appear in commit messages.

## Lookup rules

| Input | Call |
| ----- | ---- |
| `RO-<n>` | `mcp__Kaneo__get_task_by_ticket_id` with `ticketId: "RO-<n>"` and **no** `projectId` (passing it currently 404s) |
| Task CUID | `mcp__Kaneo__get_task` with the CUID. `get_task("RO-108")` fails with "Workspace ID could not be determined" |
| GitHub `#N` | `mcp__github__issue_read` (owner `mdg-labs`, repo `release-ops`) for the title → `mcp__Kaneo__search` with `q` = title, `workspaceId` + `projectId` |
| Title / keyword | `mcp__Kaneo__search` — param is `q`; pass **both** `workspaceId` and `projectId` |
| Ready queue | `mcp__Kaneo__list_tasks` for the project, filter column `ready` |
| Epic subtasks / prerequisites | `mcp__Kaneo__get_task_relations` with the parent CUID → subtask edges / blocking edges |
| Roadmap key (`E03-02`) | `search` for the key or title; task descriptions carry `Roadmap ID: E03-02` |

### Resolving `githubIssueNumber` (read-only)

Kaneo task payloads have so far carried **no** `externalLinks` (`get_task` / `get_task_by_ticket_id` for RO-108 return none). Resolve `#N` like this:

1. The user gave `#N` → use it (confirm the title with `mcp__github__issue_read`).
2. Optional: the payload has `externalLinks` → use `externalLinks[].externalId` of the issue link. Not observed today; fall through to step 3 when absent.
3. Otherwise `mcp__github__search_issues` with owner `mdg-labs`, repo `release-ops`, query = the exact Kaneo task title → take the issue whose title matches exactly. This is the primary method.
4. No exact match → use the roadmap key (`[E*-*]`) if the task has one; else **ask the user**. Never create an issue to get a number.

## Columns

| Slug | Meaning | Set by |
| ---- | ------- | ------ |
| `backlog` | Unrefined / deferred | intake / triage / user |
| `ready` | Fully specified; orchestrator picks from here | **kaneo-intake** / **kaneo-triage** / user |
| `in-progress` | Being implemented (also rework after FAIL) | **execution agent** (first action); **verifier** on FAIL |
| `in-review` | Handed to verifier | **execution agent** (after AC + gate, before commit) |
| `implemented` | Verified; commit on `dev`, not yet on `main` | **verifier** after all layers PASS + PASS comment |
| `done` | Final — fix is on `main` | **Kaneo ↔ GitHub sync** when the `fixes #N` commit lands on `main` and GitHub closes the issue; the user may also set it manually. No agent ever sets `done` |

Intake and triage stop at `ready`.

## Status sync — sub-agent duties

Skip only when the user said **"don't update Kaneo"** or the prompt has no KANEO STATUS SYNC block. Sub-agent prompts carry the **STATUS SYNC TABLE** verbatim. The transitions are routine, pre-authorized workflow steps — sub-agents execute them without asking.

### Execution agent

| # | When | From → To | `status` | Comment? |
| - | ---- | --------- | -------- | -------- |
| 1 | First action — before session memory or repo work | `ready` → `in-progress` | `in-progress` | No |
| — | During implementation | stay `in-progress` | — | No |
| 2 | After AC done + scoped gate green, **before** the commit | `in-progress` → `in-review` | `in-review` | No |

```text
mcp__Kaneo__update_task_status
  taskId: <each listed task CUID>
  status: in-progress
```

- Step 1 covers every listed task: each leaf **and** the parent epic (same batch). Idempotent if already `in-progress`.
- Transition fails → report `blocked`; do not touch repo files.
- Step 2 order: session memory `ended` + `duration` → `in-review` on each leaf → one implementation commit with `[#N]` in the subject and `fixes #N` in the body (plus `fixes #<parent-N>` when the parent is listed with `closesParent: yes`).

### Verifier — PASS (all layers, including 3c3 commit linkage)

Task must already be `in-review`.

```text
1. Session memory: verification ended + duration
2. mcp__Kaneo__create_task_comment — PASS template on each leaf (mandatory, FIRST)
3. mcp__Kaneo__update_task_status → implemented on each leaf
4. Parent epic listed with closesParent: yes (its last open child) → PASS comment + implemented on the parent too
5. Optionally archive/delete local active/<SESSION-ID>.md (never commit)
```

### Verifier — FAIL (any layer)

```text
1. mcp__Kaneo__create_task_comment — FAIL template on each leaf (mandatory, FIRST)
2. mcp__Kaneo__update_task_status → in-progress on each leaf (rework — never back to ready)
3. Append VERIFICATION FAILED to local active/<SESSION-ID>.md (never commit)
```

The verifier never sets `done`, never sets `in-review`, and never writes to GitHub. `done` follows from the `fixes #N` trailer reaching `main`.

## Verifier PASS comment

Posted via `mcp__Kaneo__create_task_comment` on each leaf before `implemented`.

```markdown
## Verified — <SESSION-ID>

**Commit:** `<sha>` — <subject one line>
**Closes on main via:** `fixes #<N>` (done follows when merged to main)

### Summary

- <1–3 bullets: what shipped>

### Scope

- <key paths or areas touched>

### Automated checks

- npm test: PASS | FAIL | n/a
- npm run lint: PASS | FAIL | n/a
- npm run db:check: PASS | FAIL | n/a
- go test ./... / golangci-lint run: PASS | FAIL | n/a

### Operator follow-ups

- <items or "None">

### Deviations / open questions

- <items or "None">
```

## Verifier FAIL comment

```markdown
## Verification failed — <SESSION-ID>

### Layers failed

- Layer 1: PASS | FAIL — <detail>
- Layer 2: PASS | FAIL — <detail>
- Layer 3: PASS | FAIL — <detail>

### Fix hints

- <file>:<line> — <expected per AC/doc>
```

## Comment rules

| Role | `create_task_comment` | Then `update_task_status` |
| ---- | --------------------- | ------------------------- |
| Execution | **Never** | `in-progress`, later `in-review` |
| Verifier PASS | **Mandatory** PASS template on each leaf, first | `implemented` (parent too when `closesParent: yes`) |
| Verifier FAIL | **Mandatory** FAIL template on each leaf, first | `in-progress` |

Comments go on the Kaneo task only — never to GitHub issues or PRs.

## Orchestrator role

- Resolve each leaf's task CUID and `githubIssueNumber` (read-only) before dispatch; fill them into the `tasks:` list of the KANEO STATUS SYNC block. For a listed parent, also fill its `githubIssueNumber` and `closesParent: yes | no`.
- Run the pre-dispatch gate (`prompt-templates.md` + `.claude/rules/09-sub-agent-prompt-contract.md`) before every Agent call.
- Check each sub-agent's REQUIRED OUTPUT for the transitions.
- After a verifier PASS, re-query `get_task` to confirm `implemented`.
- **Recovery only** when a sub-agent skipped a transition. The orchestrator may set `in-progress` / `in-review` / `implemented` in recovery, never `done`.

## Epic pattern

Feature work with 2+ tasks is a parent task with subtask relations (created by **kaneo-intake**).

- Implement **leaf** tasks; each execution + verifier prompt lists the leaf CUID + `githubIssueNumber`, plus the parent CUID.
- Parent → `in-progress`: the execution agent of the first leaf that starts.
- `closesParent` / `CLOSE_PARENTS`: before dispatching a leaf's execution agent, the orchestrator computes `CLOSE_PARENTS` (parents whose other children are all `implemented` or `done`). For those parents the leaf is the final one: its execution prompt lists the parent with `closesParent: yes` and its `githubIssueNumber`, so the commit body carries `fixes #<N>` **and** `fixes #<parent-N>`. Every other leaf gets `closesParent: no` (trailer `fixes #<N>` only). Never put an epic's last remaining leaves in the same Lane P batch: the final leaf always runs alone in Lane S once its siblings are `implemented`, so `CLOSE_PARENTS` is unambiguous.
- Parent → `implemented`: the verifier of that final leaf, which gets the same parent entry (`closesParent: yes`) and only those parents.
- Parent → `done`: the Kaneo ↔ GitHub sync when the `fixes #<parent-N>` commit lands on `main` (or the user, manually).

## MCP tools by role

| Tool | Orchestrator | Execution | Verifier |
| ---- | ------------ | --------- | -------- |
| `get_task_by_ticket_id`, `get_task`, `list_tasks`, `search` | Find work, load AC | — | Read AC if needed |
| `get_task_relations` | Epic children, prerequisites | — | — |
| `update_task_status` | Recovery only (never `done`) | → `in-progress`; → `in-review` | → `implemented` / `in-progress` |
| `create_task_comment` | — | — | PASS and FAIL |
| `create_task`, `create_task_relation` | — (kaneo-intake / kaneo-triage) | — | — |
| `mcp__github__issue_read`, `search_issues` | Resolve `#N` | — | — |
| `fixes #N` commit trailer | — | **Mandatory** in the task commit | Checks it (Layer 3c3) |
| Any GitHub issue write | **Forbidden** | **Forbidden** | **Forbidden** |

## Roadmap vs Kaneo

| | Roadmap (`E*-*`) | Kaneo task (`RO-<n>` / `#N`) |
| - | ---------------- | ---------------------------- |
| Plan file checkboxes (`docs/roadmap.html`) | Verifier `[x]` / `[!]` | Only when the task carries a `Roadmap ID` and the plan file is in WRITE SCOPE |
| Board status | — | Execution `in-progress` → `in-review`; verifier `implemented` |
| Commit subject | `[E*-*]` when no GitHub issue exists | `[#N]` |
| Commit body trailer | none | `fixes #N` (+ `fixes #<parent-N>` on the epic's final leaf) |

## Commit linkage

GitHub links a commit to an issue through `#N` in the subject (`[#111]`). Every task commit with a GitHub issue **must** also end its body with the closing trailer `fixes #N`; the final leaf of an epic (`closesParent: yes`) adds `fixes #<parent-N>` on its own line:

```text
feat(api)[#111]: add Kaneo ticket integration client

Adds the Kaneo REST client and wires it into the ticket dispatcher.

fixes #111
fixes #108        # only when closesParent: yes
```

The trailer is the **only** closing mechanism: when the commit lands on `main`, GitHub closes the issue and the Kaneo ↔ GitHub sync moves the task `implemented` → `done`. No agent sets `done` or closes an issue via API. Roadmap-only commits (`[E*-*]`) carry no trailer. PR bodies may also contain closing keywords, but don't need to.

The orchestrator's commit-linkage audit and verifier Layer 3c3 check **both** `[#N]` in the subject and `fixes #N` in the body; a missing trailer is a FAIL.

## Time tracking

With a KANEO STATUS SYNC block, execution and verifier agents record `started`, `ended` and `duration` in the local session memory header. Session memory is never committed.
