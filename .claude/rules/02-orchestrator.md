---
description: When and how to use the orchestrator skill, the roadmap plan file and the Kaneo board
---

# Orchestrator & roadmap

Release Ops is built **task-by-task** from the roadmap plan file `docs/roadmap.html` (generated from `docs/roadmap.json`) and the Kaneo board (project Release Ops, ticket key `RO`). Sub-agents implement tasks; a verifier checks them; progress is tracked in Kaneo columns and/or plan checkboxes.

## Use the orchestrator skill when

- The user asks to "orchestrate", "delegate", "run the roadmap", "implement RO-<n>" / "implement #N", or "execute tasks autonomously"
- Multi-step work spans several tasks

Follow `.claude/skills/orchestrator/SKILL.md`. Do not improvise.

## Sub-agent prompts (orchestrator dispatch)

Before every **Agent** call: `.claude/rules/09-sub-agent-prompt-contract.md` — copy verbatim blocks from `.agents/project/orchestrator/prompt-templates.md`. No shorthand. The verifier is not read-only when Kaneo sync is on.

## Sub-agents (any Agent dispatch)

When waiting on a sub-agent: follow `.claude/skills/orchestrator/references/sub-agent-monitoring.md`.

- Git silence ≠ stalled. Check the **sub-agent transcript** (two samples, 10–20s apart) before any stall verdict.
- **Terminate** the old agent before spawning a replacement.
- Never take over the sub-agent's work (e.g. kaneo-intake `create_task`) while it may still be running.

## Use the roadmap directly when

- You're implementing a single task and the user already pointed at a task (`E*-*`, `RO-<n>` or `#N`)
- You're checking dependencies or what's next

Open `docs/roadmap.html`, search the task key, read the row (Doc Ref + acceptance criteria + Tests). For a Kaneo task, read its description (the AC contract) via the lookup rules in `.agents/project/orchestrator/project.config.md`.

## Task lifecycle (plan-file mode)

```
- [ ] not started
- [~] awaiting verification    ← Lane S: execution agent (if plan file in WRITE SCOPE)
                               ← Lane P: orchestrator at batch start
- [x] verified                 ← Lane S: task verifier
                               ← Lane P: batch verifier after integration
- [!] failed — <reason>        ← verifier only
```

The orchestrator chat does not edit checkboxes during normal flow except **Lane P batch prep** (`[~]` at batch start). Verifiers set `[x]` / `[!]`.

**Parallel batches (Lane P):** execution in isolated worktrees (Agent `isolation: "worktree"`); an integration agent merges to `dev`. See `.claude/skills/orchestrator/SKILL.md` § Parallelism.

## Kaneo board status

| Column | Who sets it |
|--------|-------------|
| `backlog` / `ready` | kaneo-intake / kaneo-triage / user (intake and triage stop at `ready`) |
| `in-progress` | **execution agent** — first action (leaf + parent epic) |
| `in-review` | **execution agent** — after AC + gate, before the commit |
| `implemented` | **verifier** — after all layers PASS, PASS comment first (parent epic when its last child passes) |
| `in-progress` (rework) | **verifier** — after FAIL, FAIL comment first |
| `done` | **Kaneo ↔ GitHub sync** when the `fixes #N` commit lands on `main` and GitHub closes the issue (the user may also set it manually) — no agent ever sets `done` |

`implemented` means the commit is on `dev` but not yet on `main`. Verifier comments go on the Kaneo task, never to GitHub. GitHub issues are read-only for every agent; the `fixes #N` commit trailer is the only closing mechanism. See `.claude/skills/orchestrator/references/kaneo-sync.md` and `07-commit-linking.md`.

## Project config

Project constants: `.agents/project/orchestrator/project.config.md`
