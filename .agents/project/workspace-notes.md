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

## Board MCP in cloud sessions

In claude.ai cloud sessions the board MCP is exposed as `Kaneo` (`mcp__Kaneo__*`), not `phasical`. Same tool names/args (`get_task`, `update_task_status`, `create_task_comment`). Workspace `ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj`, project `z4janvyjsbbb0esishvd9gb8` (slug `RO`) — differs from IDs in `project.config.md`. `get_task("RO-N")` fails (no active workspace) → `search` with `workspaceId`+`projectId` to get the CUID, then `get_task(cuid)`. Tasks have no `externalLinks` here → resolve GitHub `#N` via `github` `search_issues` by title (e.g. RO-106 → #108). Column slugs: backlog, ready, in-progress, in-review, implemented, done.
_added: 2026-10-07_
