---
description: When and how to use the orchestrator skill and development roadmap
---

# Orchestrator & roadmap

This project is built **task-by-task** from the plan file (see `project.config.md` § Plan file). Sub-agents implement tasks; a verifier checks them; progress is tracked on Kaneo and/or plan checkboxes.

## Use the orchestrator skill when

- The user asks to "orchestrate", "delegate", "run the roadmap", "implement #N", or "execute tasks autonomously"
- Multi-step work spans several tasks

Follow `.claude/skills/orchestrator/SKILL.md` (installed from `mdg-labs/skills`). Do not improvise.

## Sub-agent prompts (orchestrator dispatch)

Before every **Agent** call: `.claude/rules/09-sub-agent-prompt-contract.md` — copy verbatim blocks from `.agents/project/orchestrator/prompt-templates.md`. No shorthand. Verifier is not read-only when Kaneo sync is on.

## Sub-agents (any Agent dispatch)

When waiting on a sub-agent: follow `.claude/skills/orchestrator/references/sub-agent-monitoring.md`.

- Git silence ≠ stalled. Check **sub-agent transcript** (two samples, 10–20s apart) before any stall verdict.
- **Terminate** the old agent before spawning a replacement.
- Never take over the sub-agent's work (e.g. intake `create_task`) while it may still be running.

## Use the roadmap directly when

- You're implementing a single task and the user already pointed at a Task ID (`P*-*` or `#N`)
- You're checking dependencies or what's next

Open the plan file from `project.config.md`, search the Task ID, read the row (Doc Ref + acceptance criteria + Tests).

## Task lifecycle (plan-file mode)

```
- [ ] not started
- [~] awaiting verification    ← Lane S: execution agent (if plan file in WRITE SCOPE)
                               ← Lane P: orchestrator at batch start
- [x] verified                 ← Lane S: task verifier
                               ← Lane P: batch verifier after integration
- [!] failed — <reason>        ← verifier only
```

If you're the orchestrator chat: do not edit checkboxes during normal flow except **Lane P batch prep** (`[~]` at batch start). Verifiers update `[x]` / `[!]`.

**Parallel batches (Lane P):** execution on isolated worktrees (Agent `isolation: "worktree"`); integration merges to the integration branch. See `.claude/skills/orchestrator/SKILL.md` § Parallelism.

## Kaneo board status (when orchestrating)

| Column | Who sets it |
|--------|-------------|
| Backlog (`backlog`) | new / raw issue |
| Ready (`ready`) | intake / triage / user |
| In Progress (`in-progress`) | **execution agent** (first action) |
| In Review (`in-review`) | **execution agent** (pre-verifier) |
| Implemented (`implemented`) | **verifier** after PASS (work on `dev`, not yet on `main`) |
| In Progress (rework) | **verifier** after FAIL |
| Done (`done`) | **never an agent** — GitHub closes the issue when the `fixes #N` commit lands on `main`; Kaneo syncs Implemented → Done |

Ticket IDs (`RO-106`) resolve with `get_task_by_ticket_id`; the GitHub number comes from `list_tasks` `externalLinks` and differs from the `RO-` number (`RO-106` = `#108`). See `project.config.md` § Task lookup.

See `.claude/skills/orchestrator/references/kaneo-sync.md` and `07-kaneo-commit-linking.md`.

## Project config

Project constants: `.agents/project/orchestrator/project.config.md`
