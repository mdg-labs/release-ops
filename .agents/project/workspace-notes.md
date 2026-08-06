# Workspace notes — Release Ops

Durable project learnings for the orchestrator. Not session-specific.

## Conventions

- Phasical is the board source of truth; GitHub issues mirror via sync.
- Commits use `[#N]` from Phasical `externalLinks`.
- Roadmap lives in `docs/roadmap.html` (generated from `docs/roadmap.json`).
- Early project: follow `docs/stack.html` — sqldiff migrations (`db/schema.sql`), Vitest required from phase 00.

## Phasical task lookup

See `project.config.md` § **Task lookup**. Summary:

- **`RO-<N>` refs → `get_task`** — never `search` for a known ref like `RO-1`.
- Epic batches: `get_task` parent → `get_task_relations` → `get_task` each leaf.
- `search` needs `q` (not `query`) and **both** `workspaceId` + `projectId` when scoped.
- Ready column slug is `ready` (not `to-do`).

---

<!-- Add sections as needed:

## <topic>

<2-4 lines>
_added: YYYY-MM-DD_

-->
