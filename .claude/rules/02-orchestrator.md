---
description: How work moves from the Kaneo board to verified commits on dev — the orchestrate skill, its agents, and who sets each Kaneo column
---

# Orchestration & the Kaneo board

Kaneo (project Release Ops, ticket key `RO`) is the plan and the status. Tasks are specified by `/kaneo-intake`, `/kaneo-triage` or `/dependabot-triage` and stop at `ready`; `/orchestrate` turns them into verified commits on `dev`. `docs/roadmap.html` is a historical plan — Kaneo is the only live one.

## Use `/orchestrate` when

- The user asks to "orchestrate", "delegate", "implement RO-<n>" / "implement #N", "work on epic RO-<n>", or to execute tasks autonomously.
- Any work spans more than one Kaneo task.

Follow `.claude/skills/orchestrate/SKILL.md`. Do not improvise.

## How it works

- `task-executor` agents (sonnet) implement each task in an **isolated scratch clone** (`git clone` into the scratchpad), one commit per task, never in the real repo.
- One independent `task-verifier` per attempt (sonnet; Opus for security-sensitive work, advisories and large diffs) runs seven layers and the full gate, posts a verdict comment on the Kaneo task and moves its column.
- The orchestrator lands a commit on local `dev` (cherry-pick) **only after a PASS**, then pushes `origin/dev` immediately.
- A thin or stale task goes through a `task-refiner` (readiness gate) before any executor sees it.
- Prompts are filled from `.claude/skills/orchestrate/templates/` and passed **inline, in full**. Never a pointer to a file, never shorthand.
- `known-escapes.md` in the same folder lists defect patterns CodeRabbit caught after a PASS; executors and verifiers read it, `/cr-review` appends to it.
- `main` moves only through the `dev → main` PR: `/open-pr`, then `/cr-review <PR>`. `/dev-diff` reports the CodeRabbit-reviewable size; the orchestrator keeps it under 100 files.

## Sub-agents (any Agent dispatch)

- Subagents re-invoke you when they finish: end your turn instead of polling, sleeping or scheduling a wakeup.
- Never take over a running sub-agent's work (e.g. kaneo-intake `create_task`). To replace one, stop it first (`TaskStop`), confirm it stopped, and check what it already wrote before dispatching again.

## Kaneo board status

| Column | Who sets it |
|--------|-------------|
| `backlog` / `ready` | kaneo-intake / kaneo-triage / dependabot-triage / user (they stop at `ready`) |
| `in-progress` | **task-executor** — before it starts a task (the orchestrator claims the first task right after dispatch as a backstop); the **orchestrator** for an epic with started subtasks |
| `in-review` | **task-executor** — after the task's commit in its scratch clone |
| `implemented` | **task-verifier** — PASS, after its PASS comment; the **orchestrator** for an epic whose subtasks are all implemented |
| `in-progress` (rework) | **task-verifier** — FAIL, after its FAIL comment |
| `ready` (abandoned) | **orchestrator** — a task leaving the run unfinished (blocked, escalated, dropped for the budget) |
| `done` | **Kaneo ↔ GitHub sync** when the `fixes #N` commit reaches `main` and GitHub closes the issue (the user may also set it manually) — no agent ever sets `done` |

`implemented` means the commit is verified and on `dev`, not yet on `main`. Verifier comments go on the Kaneo task, never to GitHub. GitHub issues are read-only for every agent (`07-commit-linking.md`).

## Project config

Kaneo IDs, lookup rules, area → paths: `.agents/project/orchestrator/project.config.md`.
