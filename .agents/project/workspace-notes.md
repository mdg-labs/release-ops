# Workspace notes — Release Ops

Durable project learnings for the orchestrator. Not session-specific.

## Conventions

- Kaneo is the board and source of truth (MCP server `Kaneo`, tools `mcp__Kaneo__*`; workspace `ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj`, project `z4janvyjsbbb0esishvd9gb8`, refs `RO-<n>`). IDs and lookup rules: `orchestrator/project.config.md`.
- Columns: `backlog`, `ready`, `in-progress`, `in-review`, `implemented`, `done`. Verifier PASS → `implemented` (commit on `dev`); `done` comes from the Kaneo ↔ GitHub sync when the `fixes #N` commit lands on `main` (or the user sets it). No agent sets `done`.
- Commits use GitHub `[#N]`. Kaneo payloads have no `externalLinks` (use `externalLinks[].externalId` if one ever appears) — resolve `#N` read-only with `github` `search_issues` on the exact task title. Task commit bodies end with `fixes #N` (final epic leaf adds `fixes #<parent-N>`); GitHub issues are never written.
- Roadmap lives in `docs/roadmap.html` (generated from `docs/roadmap.json`); `docs/index.html` is the doc hub.
- Follow `docs/stack.html` — sqldiff migrations from `db/schema.sql`, Vitest for web, `go test` for the server.

## Kaneo MCP quirks

- `RO-<n>` → `get_task_by_ticket_id` **without** `projectId` (with it: 404). `get_task` needs the CUID — `get_task("RO-108")` fails with "Workspace ID could not be determined".
- `search` takes `q` (not `query`) and needs **both** `workspaceId` + `projectId` when scoped.
_added: 2026-10-07_

---

<!-- Add sections as needed:

## <topic>

<2-4 lines>
_added: YYYY-MM-DD_

-->
