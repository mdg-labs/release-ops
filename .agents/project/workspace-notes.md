# Workspace notes — Release Ops

Durable project learnings for the orchestrator. Not session-specific.

## Conventions

- Kaneo is the board source of truth; GitHub issues mirror via sync.
- Commits use `[#N]` from Kaneo `externalLinks`.
- Roadmap lives in `docs/roadmap.html` (generated from `docs/roadmap.json`).
- Early project: follow `docs/stack.html` — sqldiff migrations (`db/schema.sql`), Vitest required from phase 00.

## Kaneo task lookup

See `project.config.md` § **Task lookup**. Summary:

- **`RO-<N>` refs → `get_task_by_ticket_id`** — `get_task` only accepts Kaneo task IDs.
- **GitHub `#N` ≠ `RO-<N>`** (`RO-106` = `#108`) — always read `externalLinks` from `list_tasks` (`get_task` omits them).
- Epic batches: `get_task_by_ticket_id` parent → `get_task_relations` → `get_task` each leaf → `list_tasks` for `#N`.
- `search` needs `q` (not `query`) and `workspaceId` whenever scoped; it does not match GitHub numbers.
- Labels are per task — `create_label` with `taskId`.
- Agents stop at `implemented`; `done` comes from GitHub when the `fixes #N` commit lands on `main`.
_migrated from Phasical: 2026-10-07_

---

<!-- Add sections as needed:

## <topic>

<2-4 lines>
_added: YYYY-MM-DD_

-->
