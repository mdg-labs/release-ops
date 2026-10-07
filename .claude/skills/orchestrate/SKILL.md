---
name: orchestrate
description: Given Kaneo tasks (RO-<n>, a Kaneo epic with subtasks, or the GitHub issue numbers #N that mirror them) or a private security advisory id (`--advisory GHSA-…`), autonomously implement and verify the work — sonnet task-executor agents in isolated scratch clones (one per task, or one per bundle of small, correlated tasks), one independent task-verifier per attempt, landing on local dev only after a PASS and pushing dev immediately. Parallelizes tasks with disjoint file scope, serializes overlapping ones. Never touches main — that's the separate dev→main promotion (open-pr). Use when asked to "work on RO-<n>", "implement #N", "implement epic RO-<n>", "run the orchestrator", or "orchestrate …".
argument-hint: <RO-n | #N>... [--advisory <GHSA-id>]...
allowed-tools:
  - Read
  - Grep
  - Glob
  - Agent
  - AskUserQuestion
  - Bash
---

# orchestrate

Turns Kaneo tasks on the Release Ops board — single tasks, or an epic with
subtasks — into landed, verified commits on local `dev`, with no human in the
loop except at a genuine blocker (a repeated verification failure, an external
unmet dependency, a decision only the maintainer can make). **Every landed
commit is pushed to `origin/dev` as soon as it lands** — `dev` is a working
branch, not a release branch, so the gate that matters is the independent
verifier's PASS before landing, not a manual pre-push read — one push per task,
unless the task itself carries an open blocking dependency added during this
run, which waits for the maintainer to read and push instead. `main` is out of
scope for this skill entirely: it only moves via the `dev → main` pull request
that `open-pr` opens and the maintainer merges.

A finding the threat model's rubric rates Critical or High lives in a private
repository security advisory, not a Kaneo task or GitHub issue
(`docs/threat-model.md` §7). `--advisory GHSA-…` fixes one through this same
loop — executor, Opus verifier, landing on `dev` — without writing anything
about it to Kaneo or a public GitHub surface (step 0a). It may be given alone or
beside task numbers.

**You (the current session) are the orchestrator.** You spawn `task-refiner`,
`task-executor` and `task-verifier` subagents and drive the loop yourself — this
skill is not itself a subagent. Follow the steps in order. A run takes a while;
give the user a short progress update at the start of each wave and after each
landed commit, rather than going quiet.

## Board and GitHub — the two systems

- **Kaneo is the plan and the status.** Project Release Ops, ticket key `RO`.
  Constants, column slugs and lookup rules: `.agents/project/orchestrator/project.config.md`.
  Kaneo tools are `mcp__Kaneo__<tool>` (the project's `Kaneo` MCP server) or
  `mcp__claude_ai_Kaneo__<tool>` (the claude.ai connector) — use whichever this
  session lists; every Kaneo call below is written as `Kaneo <tool>`.
- **GitHub issues mirror Kaneo tasks and are read-only for every agent**
  (`.claude/rules/07-commit-linking.md`). Their only use is the number `#N` in
  the commit subject `[#N]` and the body trailer `fixes #N`, which closes the
  issue when the commit reaches `main`; the Kaneo ↔ GitHub sync then moves the
  task to `done`. No agent ever writes to a GitHub issue, and no agent ever sets
  Kaneo `done`.

## Hazards this project has

Release Ops has no storage lab and no destructive host tooling, but the
development host is the maintainer's machine. Every dispatch carries these
rules — they are baked into the templates; **never soften them when filling one
in**:

- **No `sudo`, no package-manager install, no write under `/etc`**, no system
  service touched. Work that needs root is reported `blocked`.
- **Docker is root-equivalent.** Agents never run, stop, remove or prune a
  container, image or volume; the gates don't need Docker. A task whose
  acceptance needs `docker build` is verified by CI after the push (step 7).
- **Kill by PID only** — never `pkill`/`killall`/pattern kills. Capture the PID
  of anything started (`cmd & PID=$!`) and kill exactly that.
- **Every command is bounded** — an explicit timeout on anything not obviously
  fast, no recursive scan rooted at `/` or `$HOME`, and no background command,
  dev server or daemon left running when a dispatch ends.
- **No agent connects to a live provider or ticket system** (GitHub, GitLab,
  Gitea, Jira, Linear, Kaneo's own API as a *product* integration, SMTP).
  Provider code is tested against fakes and recorded fixtures only.
- **Secrets never leave `.env` files** — no agent prints, copies or commits a
  token, key or password, and none reads `~/.claude/.env` or the maintainer's
  credential stores.

**Verify the machine at the start of a run, then write what you read into the
dispatch — never copy a claim from this file.** One Bash call:

```
cd <real repo> && git status -sb | head -1 && git status --porcelain | head -5
git -C <real repo> rev-parse dev origin/dev 2>&1
command -v go node npm golangci-lint gh python3 2>&1
go version 2>&1; node --version 2>&1
gh auth status 2>&1 | head -3
```

That tells you whether the real repo is on `dev` and clean (landing needs both —
if it is dirty or on another branch, stop and tell the maintainer; never stash
or switch their work), whether local `dev` is in sync with `origin/dev` (local
commits ahead of `origin/dev` that this run did not land are reported, never
pushed by you), and which checkers are installed (`npm run db:check` needs only
Go).

## 0. Resolve the target

The arguments name Kaneo tasks as `RO-<n>`, GitHub issue numbers `#N` (ranges
such as `#114 - #130` mean every number in between), or Kaneo task URLs.

- `RO-<n>` → `Kaneo get_task_by_ticket_id { ticketId: "RO-<n>" }` (no
  `projectId` — passing it 404s).
- `#N` → read the issue title read-only (`gh issue view <N> --repo mdg-labs/release-ops --json number,title,state`,
  or GitHub MCP `issue_read`), then `Kaneo search { q: "<title>", type: "tasks", workspaceId, projectId }`
  and take the exact-title match.
- Then the task's relations: `Kaneo get_task_relations { taskId: <cuid> }`.

- **Not an epic** (no subtask edges): the target set is just this task.
- **An epic** (it has subtasks): the target set is its subtasks. Drop any in
  `implemented` or `done`.
- A number that doesn't resolve, or resolves to two tasks, is a stop — say so,
  never guess.

For every task in the set, resolve its GitHub mirror number **read-only**
(project.config.md § Resolving the GitHub `#N`) — the exact-title match from
`gh issue list --repo mdg-labs/release-ops --state all --search "<title> in:title" --json number,title`
or GitHub MCP `search_issues`. An epic's own `#N` is resolved too (step 8 adds
its trailer). A task without a mirror and without a roadmap key is reported and
left out — never create an issue to get a number.

Call the resulting set of tasks **T**. Each member carries: CUID, `RO-<n>`,
`#N`, title, status, labels, parent epic (CUID, `RO-<n>`, `#N`) if any.

## 0a. Advisory targets

For each `--advisory <GHSA-id>`:

```
gh api repos/mdg-labs/release-ops/security-advisories/<GHSA-id> --jq '{ghsa_id,summary,description,severity,state}'
```

If the call fails — a malformed id, an advisory that does not exist or that
this account cannot read — stop and say so; never guess an id. The advisory's
`description` is its body, and it is the only thing about it that reaches an
executor or verifier. The advisory joins **T** as an **advisory unit**,
identified by its GHSA id; its unit id (steps 5–7) is `adv-` plus the id's three
four-character groups joined with `-` (`adv-abcd-efgh-ijkl`).

An advisory unit differs from a task in exactly these ways, and in no others:

- **Nothing is written to Kaneo or a public GitHub surface** — no task,
  comment, status, epic rollup, branch, pull request or any text naming it,
  beyond the `Refs:` trailer of its landing commit (step 8). Verdicts, findings
  and the run log stay in this session and the report (step 12).
- **No relations, no epic.**
- **The readiness gate (step 1b) is read by hand** from the description: it
  needs acceptance criteria and an out-of-scope section like a task's. A
  description without them is reported and left out of T — a `task-refiner`
  never touches an advisory; the maintainer fixes the description on GitHub.
- **File scope (step 3)** comes from the paths the description names; if it
  names none, its scope is the whole repo.
- **Never bundled** (step 4). The promotion-diff budget applies unchanged.
- **The commit message is neutral** and ends in `Refs: <GHSA-id>` instead of
  `fixes #N`, with no `[#N]` in the subject (steps 6 and 8). Neutral means it
  says what the code now does and nothing about how it used to fail: no
  reproduction, trace, payload, attacker narrative, severity or quotation or
  paraphrase of the advisory. The same holds for every code comment, test name
  and fixture the diff adds. The executor and verifier dispatches state the rule
  (the `ADVISORY` blocks in both templates).
- **The verifier is always Opus**, whatever the diff's size (step 7), and
  posts nothing.

## 1. Pull each task's full description, comments and relations

For every task in T (an advisory unit has none of these — step 0a):

```
Kaneo get_task { taskId: <cuid> }
Kaneo list_task_comments { taskId: <cuid> }
Kaneo get_task_relations { taskId: <cuid> }
```

**Comments are authoritative over the description where they disagree** —
scope corrections, replaced acceptance criteria and prior verification findings
all land as comments. Fold what you learn into your own decisions (scope,
ordering, whether it still belongs in T) and into the dispatch; never assume the
agent will rediscover it.

For each task `d` the task is **blocked by** (a `blocks` edge pointing at it, or
a `Depends on: RO-<n>` line in its description):

- `d` is `implemented` or `done` → satisfied: its commit is on `dev`, which is
  enough for a scratch clone made from the real repo's current state.
- Otherwise, and `d` is in T → an intra-run ordering edge.
- Otherwise → **external blocker.** Remove the task from T and report:
  `RO-<n> is blocked by RO-<d>, which is outside this run — orchestrate RO-<d> first, or include it explicitly`.

Also read the spec sections each task cites (`docs/specs.html §…`,
`db/schema.sql`, `docs/stack.html`) — you are about to judge its scope, and the
specs are where scope lives.

## 1b. Readiness gate — refine thin or stale tasks before anything else

A task that reaches an executor thin fails verification far more often and lets
more defects through to CodeRabbit. Check every task in T against this list,
mechanically, from the description you already read (`.claude/skills/kaneo-intake/templates.md`
is the shape it should have):

- an **`Acceptance criteria`** section with at least one `- [ ]` item;
- a **`## Out of scope`** section on every leaf task (`feat`, `bug`, `chore`,
  dependency bump), naming adjacent work and the `RO-<n>` it lives in, or
  "none";
- **backticked paths** under `### Files` or `## Scope hint` — the top-level
  paths it will touch, entry-point files included;
- on a task that adds or changes **runtime behaviour**, an acceptance criterion
  `Reachable via: <entry point> → <capability>` (a route in
  `internal/api/routes.go` served by `cmd/server`, a web page under
  `apps/web/app/`, a poll-cycle step, a `make` target or CI job for a script);
- **no stale reference**: Lane S / Lane P, `orchestrator/<TASK-ID>` branches,
  `/orchestrator`, `prompt-templates.md`, `closesParent`, worktrees,
  "never push", or a path that no longer exists on `dev`.

A task missing any of these is **NOT-READY**, with the reasons. Path warnings
alone never block.

**You do not investigate or rewrite a NOT-READY task yourself** — that is code
reading your context must not carry through the rest of the run. Dispatch one
`task-refiner` per epic (or per task without one), all in one message, from
`.claude/skills/orchestrate/templates/refiner-prompt.md` with `MODE = apply`:

```
Agent({
  subagent_type: "task-refiner",
  model: "sonnet",
  description: "Refine <RO numbers>",
  prompt: <the filled template>
})
```

Fill `WORKSPACE_PATH` with a fresh clone for that refiner
(`git clone --quiet --branch dev <real repo> <scratchpad>/orchestrate/refine-<unit>`),
`OUT_DIR` with `<scratchpad>/orchestrate/refine-<unit>-out`, and
`VERDICT_TEMPLATE_PATH` with the real repo's
`.claude/skills/orchestrate/templates/refiner-verdict.md`. The refiner
inventories what already exists on `dev` and returns one short verdict per task.
Read **only the verdicts**:

- `refined` — the refiner already applied the new description. Wire the
  relations it lists (`Kaneo create_task_relation`), re-read the task (step 1)
  and continue with it.
- `split-proposed` — `AskUserQuestion` with the one-line-per-part proposal. On
  approval, apply `OUT_DIR/<RO-n>.md` to the original task with
  `Kaneo update_task` (it keeps its number as part A), create each
  `OUT_DIR/<RO-n>-NEW-*.md` as a new task in `ready` with `Kaneo create_task`
  (same parent epic via a `subtask` relation, the labels the verdict lists),
  replace every `<NEW-…>` placeholder in the descriptions with the real `RO-<n>`,
  wire the relations, resolve each new part's GitHub mirror (the Kaneo ↔ GitHub
  sync creates it; if it has not appeared yet, wait for the next step and
  re-check once), and put all parts in T. The refiner already wrote the bodies —
  don't rewrite them.
- `already-done` / `obsolete` — `AskUserQuestion` with the evidence; drop the
  task from T. Never cancel or close it yourself.
- `needs-decision` — `AskUserQuestion` with the refiner's question and
  recommended default; record the answer as a Kaneo comment on the task, then
  treat it as `refined` after a second refiner pass.

Delete each refiner clone when its verdicts are in. A task that is still
NOT-READY after one refine pass is reported and left out of T — never
dispatched thin.

## 2. Pull out tasks that need the maintainer

No agent runs root commands, touches live provider accounts or production data.
A task whose acceptance genuinely needs one of those (installing a system
package, rotating a real token, a live migration of a deployed instance) is
read by you, prepared as far as files go, and reported as "prepared, awaiting
maintainer" with the exact commands — never dispatched, never committed. Its
dependents stay blocked.

## 3. Determine each remaining task's file scope

For each task in T, derive the set of top-level paths it will touch:

- Backtick-quoted paths in the description (`internal/poll/`, `apps/web/app/(authenticated)/repos/`, `db/schema.sql`, …).
- Fallback: its domain label, via `.agents/project/orchestrator/project.config.md` § Area → paths.
- **Always-shared files** are their own scope entries whenever a task plausibly
  touches them: `CLAUDE.md`, `Makefile`, `go.mod`, `go.sum`, `package.json`,
  `package-lock.json`, `apps/web/package.json`, `apps/docs/package.json`,
  `db/schema.sql`, `migrations/`, `internal/store/db/` (sqlc output),
  `apps/web/messages/en.json`, `docs/specs.html`, `Dockerfile`,
  `.github/workflows/`, `.gitignore`. Any schema change touches `db/schema.sql`,
  `migrations/` and usually `queries/` + `internal/store/db/`; any npm
  dependency change touches `package-lock.json`.
- **Entry points are in scope.** A task's `Reachable via:` criterion names
  where its capability must be reachable from — `cmd/server/main.go`,
  `internal/api/routes.go` / `router.go`, `apps/web/app/…` pages, the
  `apps/web/app/api/go/` proxy, `Makefile`, `.github/workflows/`. Add every such
  file to the task's scope as its own entry, so lanes serialize on it.
- Can't confidently bound it → its scope is **the whole repo**, which
  serializes it against everything.

## 4. Batch into waves, then bundles, then lanes

**Waves** (dependency order): wave 1 = tasks with no unresolved same-run
dependency; wave 2 = tasks whose same-run dependencies are all in wave 1; and so
on.

### Bundles

One `task-executor` per **bundle**; a bundle is usually one task. Two shapes
qualify, nothing else:

- **Correlated** — scopes intersect, so lanes would serialize them anyway.
- **Small and adjacent** — each is a one-sitting change sharing a domain label,
  with nothing open in its thread that needs a decision.

A dependency edge between two members is a reason to bundle: order parent
first, delete that edge, re-layer the waves.

**Never bundle:**

- an advisory unit (step 0a) with anything;
- a task scoped "the whole repo";
- a task whose thread carries a verification FAIL, or that is entering a fix round;
- a security-sensitive task (step 7's Opus list) — it gets its own agent, its own verifier, and its own line in the report;
- a schema-change task (`db/schema.sql`) — one writer per schema change.

Cap a bundle at **3 tasks**. Its scope is the union of its members'. **Every
commit stays one task**: each member gets its own commit with its own `[#N]` and
`fixes #N`. When an earlier member's change already resolves a later member
(a dependency bump that also lifts a transitive one), the later member's commit
is the explicit pin, override or test that guarantees it — never an empty commit
and never a shared one; if nothing at all is left to change, the executor
reports that member `already-resolved` with the evidence, and you ask the
maintainer whether to close it by hand (step 9's escalation list).

### Lanes within a wave

Process the wave's units (a bundle is one unit, ordered by its lowest member)
in `RO` number order; place each in the first lane whose accumulated scope
doesn't intersect its own, else start a new lane. Lanes run in parallel; units
within a lane run serially. At most **3 lanes** run at once.

### Promotion-diff budget — a hard rule

**The `dev → main` reviewable diff never grows past 100 files. No run, no
option, no maintainer prompt may take it there.** CodeRabbit reviews at most 100
files per pull request, counted after `.coderabbit.yaml`'s `path_filters`
exclusions, and `dev` only reaches `main` through one `dev → main` PR (`open-pr`,
then `cr-review`). A diff past 100 forces a split promotion. There is no
"proceed anyway".

**Check before every action, not once per run.** Re-run the budget check below,
from the real repo, immediately before **each** of these, every time:

- dispatching any unit — first dispatch, next unit in a lane, a fix round, a
  rebase re-run, a task pulled in by step 11;
- landing any commit (step 8), before the cherry-pick.

**The check:**

1. `R` = the output of `.claude/skills/dev-diff/dev-diff.sh --list` (the
   reviewable paths of the current `main...dev` diff). First confirm local `dev`
   exists (`git rev-parse --verify dev`): without it the script exits zero with
   no paths, which is not an empty diff. If `dev` is missing or the script exits
   non-zero, stop and report it — never treat a failed comparison as an empty
   `R`, and never act without a successful check.
2. **In flight** — each unit dispatched but not yet landed. Every count is a
   CodeRabbit-reviewable count, never a raw file count. If the unit has
   committed, its files are
   `git -C <workspace> diff --name-only origin/dev..HEAD -- . <excludes>`, where
   `<excludes>` is one `':!<pattern>'` per `!`-prefixed entry in
   `.coderabbit.yaml`'s `reviews.path_filters` (today `package-lock.json`,
   `internal/store/db/*.sql.go`, `.claude/**`, `.agents/**`, `CLAUDE.md`, …).
   If it has not committed yet, use its estimate.
3. **Estimate high, never low.** For a task not yet committed, `E` is the
   **larger** of its `Expected files:` line and a count derived from its step-3
   scope at **one file per backticked file path and four per backticked
   directory**, plus every entry point and always-shared file in its scope that
   is reviewable, then **×1.5 rounded up**.
4. `P` = |R| + files of in-flight units not in `R` + `E` of every
   not-yet-dispatched task still in T. Print it:
   `dev→main reviewable: |R| now, P projected (cap 100, drop at 90)`.

**Acting on it:**

- **P ≥ 90** — drop tasks from T until `P` < 90, starting with the last in wave
  order. Only tasks **not in flight** are dropped — never a dispatched unit's
  work, and never by shrinking or excluding code, tests or fixtures. A dropped
  task goes back to `ready` if you touched its status, and is listed in the
  report as "dropped for the budget".
- **A dispatch would push P to 90 or above** — don't dispatch it; drop it.
- **Landing a commit would take |R ∪ the commit's reviewable files| past 100** —
  do not land. Stop the run, leave the commit in its clone, and tell the
  maintainer a promotion is needed first (`/open-pr`).
- **|R| is already ≥ 90 at the start of a run** — dispatch nothing; tell the
  maintainer to promote first.

Print the plan before dispatching — waves, bundles and why, lanes and why, and
the budget lines above. If T has more than ~12 tasks, state the count and
confirm via `AskUserQuestion` first.

## 5. Per dispatch unit: isolated scratch clone

Never work in the real repo; never share a clone between concurrent units.

```
mkdir -p <scratchpad dir>/orchestrate
git clone --quiet --branch dev <real repo> <scratchpad dir>/orchestrate/<unit-id>-a1
```

`<unit-id>` is the task number (`RO-110-a1`) or bundle members joined with `+`
(`RO-110+RO-115-a1`). The clone's `origin` is the real repo's local path, so
`origin/dev` inside it is the `dev` the unit started from. The clone has no
`node_modules`: the executor runs `npm ci` in it first (the template says so).
Clones are made **when the unit is dispatched**, so the next unit in a lane
starts from the `dev` its predecessor landed on.

A **verification-FAIL retry** (step 9) reuses the same clone so the executor can
amend. Only a **rebase retry** (step 8) or a fix round after a bundled attempt
re-clones fresh.

## Kaneo status — exactly one column, always

Every task sits in exactly one column. (An advisory unit has none — step 0a.)

| Transition | Set by | When |
|---|---|---|
| → `ready` | `kaneo-intake` / `kaneo-triage` / `dependabot-triage` / user | specified |
| → `in-progress` | `task-executor` (you claim the first task right after dispatch as a backstop) | before it starts a task |
| → `in-review` | `task-executor` | after that task's commit in the scratch clone |
| → `implemented` | `task-verifier` | PASS, after its PASS comment |
| → `in-progress` | `task-verifier` | FAIL, after its FAIL comment |
| → `done` | Kaneo ↔ GitHub sync, or the user | the `fixes #N` commit reaches `main` |

- **You never set `done`.** The trailer only closes the issue once its commit
  reaches `main` — that's the later `dev → main` promotion, not this run's push
  to `dev`.
- **You own the abandonment transitions:** a task leaving your hands still open
  (executor `blocked`, escalated after three FAILs, dropped for the budget) goes
  back to `ready`.
- **You own the epic rollup**, after every claim and every verdict: an epic
  whose subtasks include anything `in-progress` / `in-review` / `implemented` is
  `in-progress`; once every subtask is `implemented` or `done`, the epic is
  `implemented` (step 8 adds its trailer to the landing commit). Never `done`.

## 5a. Security history of each unit's paths

For each unit, run this in the real repo, once per unit, with every path of the
unit's step-3 scope as an argument:

```
scripts/security-history.sh <each path of the unit's scope>
```

It lists the earlier commits on those paths that fixed a security defect (a
`CVE-`/`GHSA-` id, `security` or `harden` in the subject, or a `Refs: GHSA-…`
trailer), each with its subject. Keep its output verbatim as `SECURITY_HISTORY`
for steps 6 and 7. Empty output means there is none: the dispatches omit the
block. A non-zero exit means the history could not be read: say so in both
dispatches in place of the block ("Security history unavailable: <the script's
message>"), never omit it as if there were none.

## 6. Dispatch `task-executor`

**Run the promotion-diff budget check (step 4) first — before every dispatch,
including fix rounds, rebase re-runs and pulled-in tasks.** If it says drop or
stop, do that instead of dispatching.

Read `.claude/skills/orchestrate/templates/executor-prompt.md` and fill every
`{{…}}` token: the shared preamble once (workspace, what step 0 found about the
machine, the Kaneo tool prefix this session lists), then the per-task block once
per task in bundle order — `RO-<n>`, CUID, `#N`, title, description, **comment
thread**, scope, `FIXES_TRAILER`, whether it is security-sensitive or a schema
change, and `SECURITY_HISTORY` (step 5a). On a fix round, the rejected SHA and
the verifier's blocking findings verbatim.

`FIXES_TRAILER` is `fixes #<N>`. For an advisory unit it is `Refs: <GHSA-id>`:
fill the template's `ADVISORY` blocks (the neutral-commit-message rule, no Kaneo
calls) and put the advisory's `description` where the task description goes,
with "No comments on this task." where the thread goes.

```
Agent({
  subagent_type: "task-executor",
  model: "sonnet",
  description: "Implement <unit-id>",
  prompt: <the filled template>
})
```

**The filled template is the prompt, inline and in full** — never write it to a
file and send a short prompt that points the agent at that file. This holds for
every dispatch: refiner, executor and verifier, and fix rounds too.

**All lane-head dispatches for a wave go in one assistant message**, so they run
concurrently.

**Claim the unit's first task yourself, right after dispatching it** — the
executor's own claim step can lag. Immediately after the `Agent()` call:
`Kaneo update_task_status { taskId: <first task's CUID>, status: "in-progress" }`,
then the epic rollup. An advisory unit has nothing to claim. A repeat claim by
the executor later is harmless.

A bundle produces **one commit per task**, each with only that task's files and
its own `[#N]` / `fixes #N`. Blocked is per task: a bundle that committed task 1
and blocked on task 2 hands you a real commit for task 1.

If a task comes back `blocked`: report the reason, put it back to `ready`, leave
it open, and continue with the rest of T that doesn't depend on it.

### Waiting: end the turn, don't schedule anything

Subagents re-invoke you when they finish. Once everything dispatchable is out
the door, say in one line what you're waiting on and **end your turn.** Never
`ScheduleWakeup`, `Monitor`, or `sleep`; never re-dispatch because you haven't
heard back, and never take over a running agent's work. If the maintainer asks
you to replace an agent, stop it (`TaskStop`) and confirm it stopped before
dispatching the replacement, then check what it already wrote (Kaneo column,
commits in its clone) before re-dispatching. When a notification arrives, route
its findings (step 11), verify that unit (step 7), and dispatch the next unit in
its lane.

## 7. Dispatch `task-verifier`

Read `.claude/skills/orchestrate/templates/verifier-prompt.md` and fill it: per
task, its details, the same comment thread, its scope, its flags, and **its own
commit SHA**; once, the workspace, attempt number, epic, the Kaneo tool prefix
and the same `SECURITY_HISTORY` (step 5a). For an advisory unit, fill the
template's `ADVISORY` blocks: the verifier posts no comment and moves no column,
and its returned verdict is read only here. On a fix round, fill the `FIX_ROUND`
block too: the rejected SHA, where it can be read, and the previous round's
**blocking** findings verbatim — the verifier checks those are closed and
reviews what changed, rather than restarting the review.

```
Agent({
  subagent_type: "task-verifier",
  model: "opus",      // the unit is security-sensitive (below), OR an advisory unit, OR its diff is large (below)
  model: "sonnet",    // otherwise
  description: "Verify <unit-id> attempt <n>",
  prompt: <the filled template>
})
```

**Security-sensitive** means the diff touches authentication, sessions or
tokens (`internal/api/auth/`, `internal/api/middleware/`), credential storage or
encryption (`internal/crypto/`, encrypted columns in `db/schema.sql`), outbound
requests to user-configured URLs (`internal/providers/`, `internal/mail/`),
`db/schema.sql` / `migrations/` (data safety), the Go API proxy
(`apps/web/app/api/go/`), or the task is titled `Security:` or labelled
`security`.

**Large diffs get the Opus verifier too.** Before dispatching, measure the
unit's changed lines excluding generated code:

```
git -C <workspace> diff --numstat origin/dev..HEAD -- . ':!internal/store/db' ':!**/package-lock.json' ':!package-lock.json' | awk '{s+=$1+$2} END {print s}'
```

Above ~1000, dispatch the verifier on Opus and say so in the report.

**Acceptance that only a real GitHub Actions run can show** — a changed workflow
under `.github/workflows/`, the Docker image build, the docs Pages job — cannot
be verified from the scratch clone. Before dispatching the verifier, run it for
real, without touching `dev`:

```
git -C <workspace> push <real repo's origin URL> HEAD:refs/heads/ci/<unit-id>
gh workflow run <workflow file> --repo mdg-labs/release-ops --ref ci/<unit-id>
gh run list --repo mdg-labs/release-ops --branch ci/<unit-id> --limit 1 --json databaseId,url
```

Put the run's URL and id in the verifier dispatch, so the verifier reads its
result and logs (`gh run view <id> --log-failed`) as layer-1 evidence. Wait for
it with `timeout 3600 gh run watch <id> --repo mdg-labs/release-ops --exit-status`
as a **background** Bash command, then end your turn — its exit re-invokes you.
Delete the branch once the unit is resolved:
`git push <origin URL> --delete ci/<unit-id>`. Never for an advisory unit (it
would publish the fix before it is verified) — such a unit is left out of T and
reported. A workflow that has no `workflow_dispatch` trigger cannot be run this
way: say so, and let the verifier judge the change by reading it plus
`actionlint` if installed.

**One verifier per unit per attempt**, verdicts **per task**: all seven layers
run against each commit separately, one Kaneo comment and one column move per
task (none for an advisory unit). **Only blocking findings fail a task**; notes
are recorded in the comment and go nowhere else — which is why a note may never
describe a defect. If a PASS comment's notes describe a concrete defect anyway,
treat that note as a finding outside the task and route it in step 11. A mixed
PASS/FAIL result is normal. Read the verifier's returned verdicts rather than
re-deriving them from Kaneo.

## 8. On PASS — land, sequentially, never in parallel

**Run the promotion-diff budget check (step 4) before every landing.**

Land one commit at a time, in bundle order, skipping members that FAILed. Every
git command in this step runs in the real repo, on `dev`, with a clean working
tree (step 0). **Invoking this skill is the maintainer's authorization to commit
to and push `dev`.** If a permission check denies one of those exact steps,
report the denial, name the command, and retry it once the maintainer says it is
granted — never hand the commit or the push back to them. The project allow
rules `Bash(git commit:*)` and `Bash(git push origin dev)` match the plain forms
below.

```
git fetch <scratch workspace path> <sha>
git cherry-pick -n FETCH_HEAD
```

- **Cherry-pick succeeds.** Decide whether this is its epic's last open subtask —
  by column, not by count: `Kaneo get_task_relations` on the epic, then the
  column of every subtask. If every other subtask is `implemented` or `done`
  (the verifier already moved this one), it is the last.

  Commit with the executor's message, adding `fixes #<epic-N>` on its own line
  after the task's `fixes #<N>` only if it is really the last:

  ```
  git commit -m "$(cat <<'EOF'
  <the executor's own commit message>
  EOF
  )"
  ```

  You may change only the message (to add the epic trailer), never the diff.
  For an advisory unit the trailer is the executor's `Refs: <GHSA-id>` and
  nothing is added. No `Co-Authored-By`, no session link, no "Generated with"
  line — ever (`~/.claude/CLAUDE.md`).

  The gate already ran in the clone (executor and verifier); the real repo's
  `node_modules` is the maintainer's and is not refreshed by you — when a landed
  commit changed `package-lock.json`, say in the report that the maintainer's
  checkout needs `npm ci`.

  **Push, unless this task now carries an open blocking dependency** (a
  follow-up step 11 filed against it during this run, limiting trust in the
  fix):

  ```
  git push origin dev
  ```

  **Before pushing, confirm no unpushed ancestor is itself held back:**
  `git log origin/dev..dev --oneline`. If that list contains anything besides
  the commit you just landed, stop and look at what it is — a commit this run
  deliberately held back, or commits the maintainer had locally before the run
  (step 0) — and **don't push**; report it in step 12 instead. If the push is
  rejected (`dev` moved meanwhile), don't force it — report it and stop touching
  that remote for the rest of this run.

  Then the epic rollup (Kaneo status section). Once **every** task in the unit
  is resolved, delete the clone.

- **Cherry-pick conflicts** (a wrong scope prediction, or a lockfile both
  touched): `git cherry-pick --abort`. A mechanical rebase problem, not a
  rejected implementation — doesn't spend a fix attempt. Delete the clone,
  re-clone fresh from current `dev`, redispatch the same task with a one-line
  "rebase re-run" note. Cap at 2 rebase retries, then escalate as in step 9.

## 9. On FAIL — fix, then re-verify, capped at 3 attempts

**A fix round is always single-task** — a FAIL dissolves its unit.

- **After a single-task attempt:** a fresh `task-executor` in the **same
  clone**, template branch `FIX_ROUND_SAME_WORKSPACE`, with the rejected SHA and
  the **blocking** findings verbatim (never the notes). It amends; the workspace
  stays one commit ahead of `origin/dev`.
- **After a bundled attempt:** re-clone fresh from current `dev` (its passing
  siblings have landed), template branch `FIX_ROUND_FRESH_CLONE`, with
  `PRIOR_ATTEMPT_PATH`/`PRIOR_COMMIT_PATH` pointing at the old bundle workspace,
  read-only. A normal new commit.

The attempt counter carries over. Dispatch a fresh verifier against the new SHA
with the `FIX_ROUND` block filled. If a fix round nonetheless raises new
blocking findings in unchanged code, read them yourself before dispatching
another round: a real data-loss or security defect stays; anything else is a
note, and you say so in the report.

If attempt 3 also fails, or a task comes back `already-resolved`: stop. Put the
task back to `ready`, then `AskUserQuestion` with the latest findings — keep
trying / hand it to the maintainer / skip for now. Delete its clone.

## 10. Repeat until T is empty

Move to the next wave once every task in the current one has landed, been
skipped as blocked, or been escalated — including tasks step 11 pulled into this
run.

## 11. Route what the run surfaces — as it arrives, not at the end

Every executor report and every verdict can carry **Findings outside these
tasks**. Route each one **as soon as that report arrives**, before you dispatch
the next unit. Notes never qualify — they stay in the verification comment.

1. **Is it real?** Check it yourself against the file, line or command it
   names: a defect with a concrete scenario, an untrue spec statement, or work a
   planned feature cannot do without. If it isn't, drop it and list it under
   "dropped" in the report with a one-line reason.
2. **Is it already tracked?** `Kaneo search` for it. If an open task's scope
   already covers it, file nothing — note the `RO-<n>`, and comment on that task
   only if the finding adds a concrete detail it lacks. If it is really a
   missing acceptance criterion of an open task nobody has started (`backlog` /
   `ready`), add it there with `Kaneo update_task` instead of filing a new task.
3. **Otherwise file it now** with `Kaneo create_task` in `ready`, in
   `.claude/skills/kaneo-intake/templates.md`'s leaf or bug shape — including
   `## Out of scope`, `Reachable via:` where it applies and the scope hint —
   with a domain label. Invoking this skill authorizes these writes; kaneo-intake's
   approval gate is for maintainer-driven intake. Then decide where it belongs:
   - **This run** — it belongs to this run's scope (same area or paths as a task
     in T, or a task in T is not really right without it) **and** it can be done
     now (no maintainer-only step, no open dependency outside the run, no
     pending decision). Attach it to the same epic (`subtask` relation), add any
     `blocks` relations ordering needs, resolve its GitHub mirror once the sync
     has created it, add it to T, place it in the waves — usually right after
     the task that surfaced it — and dispatch it like any other task, with its
     own commit and trailer.
   - **Deferred** — it belongs to other work: attach it to the open epic whose
     scope it falls under, with `blocks` relations to the open tasks it needs.
     If no open epic fits, leave it without a parent and say so in the report.
4. **A spec statement that looks wrong** (`docs/specs.html`, `db/schema.sql`,
   `docs/stack.html`) is always a task, routed the same way — never a silent doc
   edit, and never decided by you: the hard rule "if behaviour is not defined in
   a spec doc, ask before guessing" holds.
5. **A decision only the maintainer can make** — ask with `AskUserQuestion`
   when it comes up, and record the answer as a Kaneo comment on the task.

**Growth guard.** If pulled-in tasks would grow T by more than three beyond its
original size, or a pulled-in task surfaces yet another pull-in, ask the
maintainer before pulling in more; defer whatever they don't want in this run.

Never fold a finding into an unrelated landing commit: a pulled-in finding gets
its own task and its own commit.

**A finding an advisory unit surfaces is never filed anywhere if it could be
exploited.** Rate it with `docs/threat-model.md` §6; one that is Critical or
High goes into the report for the maintainer to record as an advisory, and
nothing else is written anywhere. A Medium or lower one is routed as above.

## 12. Compose the report — your final message

- What landed (`RO-<n>` / `#N` → commit SHA → one line), and whether it reached
  `origin/dev`
- **Security-sensitive commits landed this run** — their own list, even if empty
  ("none this run"), so the maintainer knows which deserve a closer read even
  though they're already on `origin/dev`
- **Commits held back** — landed locally, not pushed, and why (a fresh blocking
  dependency, or pre-existing local commits found under them); "none this run"
  when empty
- **Advisory targets** — each by its GHSA id: landed commit and whether it
  reached `origin/dev`, or why it did not. For every one that landed: once it
  reaches `main`, the maintainer publishes the advisory on GitHub — nothing else
  records it. "none this run" when there were none.
- What's blocked and why (needs the maintainer, external dependency, escalated
  after 3 FAILs, already-resolved)
- Bundles and why
- What step 11 routed: pulled into this run (`RO-<n>` → commit), deferred
  (`RO-<n>` → epic), added to an existing task, and dropped (with why)
- **Promotion-diff budget** — `dev→main reviewable: before → after` (actual:
  re-run `.claude/skills/dev-diff/dev-diff.sh --list` after the last landing),
  next to step 4's projection; name every task whose actual count exceeds its
  estimate by more than about 50%, and list every task dropped for the budget
- Reminder: `implemented` tasks reach `done` when their `fixes #N` commits land
  on `main` via `/open-pr` (or the maintainer sets `done`).

## Non-negotiables

- **The `dev → main` reviewable diff never passes 100 files.** The budget check runs before every dispatch and every landing; at a projected 90 or more, not-yet-dispatched tasks are dropped from T, last in wave order first. Estimates err high. There is no "proceed anyway".
- **Commits land on local `dev` only after a verifier PASS and are pushed to `origin/dev` immediately after landing.** Only a commit held back by a fresh blocking dependency added during this run, or one sitting on unpushed commits this run did not land, is never pushed by you. `main` is never touched by this skill.
- **No agent ever sets Kaneo `done` or writes to a GitHub issue.** Closing happens only via a commit's `fixes #N` trailer reaching `main`.
- **No agent runs `sudo`, a package install, a Docker container, or anything against a live provider, ticket system or SMTP server.**
- **Never poll or self-schedule while agents run.** The one sanctioned wait on something outside the harness is a bounded background `gh run watch` for a CI-dependent unit (step 7).
- **Exactly one Kaneo column per task**; abandonment goes back to `ready`; the epic rollup is yours.
- **Never invent an issue number** in a trailer; never put a Kaneo CUID or `RO-<n>` in a commit message.
- **Parallel lanes never share a scratch clone**, and only you touch the real repo, only at landing, one commit at a time.
- **One commit per task, always.**
- **A fix round is never bundled; a security-sensitive or schema-change task is never bundled.**
- **Only blocking findings fail a task or reach a fix round**; a fix round's verifier checks closure and the change, not the whole task afresh.
- **Surfaced findings are filed and routed as they arrive** — pulled into this run when they belong to its scope, otherwise attached to the open epic they belong to.
- **Every written artifact uses its template** — dispatch prompts, the executor's report, the verifier's comment.
- **Every dispatch prompt is passed inline in full** — never as a pointer to a file holding it.
- **An advisory unit writes nothing to Kaneo or a public GitHub surface.** Its commit message is neutral and carries `Refs: <GHSA-id>` and no `[#N]`/`fixes`; its verifier is Opus and posts nothing.
