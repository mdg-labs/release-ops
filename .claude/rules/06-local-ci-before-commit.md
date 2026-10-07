---
description: Run scoped CI before every task commit; full gate only before push
---

# Local CI before commit

## Scoped gate (every task commit and verifier Layer 2)

npm workspaces repo (`apps/web`, `apps/docs`) plus a Go module. Run the checks for the paths you touched:

```bash
npm test && npm run lint && npm run db:check   # web, root JS, DB
go test ./... && golangci-lint run             # Go
```

| Paths | Checks |
|-------|--------|
| `apps/web/**`, root `package.json` / `package-lock.json` | `npm test && npm run lint` — lint includes **i18n** (`eslint-plugin-i18next`, no literal UI strings) |
| `db/schema.sql`, `migrations/**` | **`npm run db:check`** (sqldiff drift vs `db/schema.sql`) |
| `cmd/**`, `internal/**`, `queries/**`, `sqlc.yaml`, `go.mod` / `go.sum` | `go test ./... && golangci-lint run` |
| `docs/**`, `apps/docs/**` only | no test/lint gate; sanity-check the edited content |
| Config / CI files (`Makefile`, `Dockerfile`, `.github/workflows/**`) | full scoped gate above |

Mark a check `n/a` only when it does not apply to the touched paths.

On failure → `blocked`; no commit.

## Full gate (pre-push only)

When the user **explicitly asks to push**:

```bash
npm test && npm run lint && npm run typecheck && npm run db:check
go test ./... && golangci-lint run
```

Sub-agents must **not** run the full gate during routine task execution unless push is requested.

## Rules

- Derive scope from WRITE ∪ READ paths (see `.agents/project/orchestrator/doc-index.md`).
- Lane P parallel agents: use per-worktree `WORK_ROOT`; never global `pkill` that kills sibling agents.
