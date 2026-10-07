---
description: Which checks run before a commit and before a push
---

# Local CI before commit

npm workspaces repo (`apps/web`, `apps/docs`) plus a Go module. Run the checks for the paths you touched:

| Paths | Checks |
|-------|--------|
| `apps/web/**`, root `package.json` / `package-lock.json` | `npm test && npm run lint && npm run typecheck` — lint includes **i18n** (`eslint-plugin-i18next`, no literal UI strings) |
| `db/schema.sql`, `migrations/**` | **`npm run db:check`** (`sqlite-migrate check`: checksums + drift vs `db/schema.sql`; needs only Go) |
| `cmd/**`, `internal/**`, `queries/**`, `tools/**`, `sqlc.yaml`, `go.mod` / `go.sum` | `gofmt -l .`, `go vet ./...`, `go test ./... && golangci-lint run` |
| `apps/docs/**` | `npm run docs:build` |
| `docs/**` only | no test/lint gate; every `§`/anchor added or touched resolves |
| Config / CI files (`Makefile`, `Dockerfile`, `docker/**`, `.github/workflows/**`) | all of the above that the change can affect; `actionlint` if installed |

Mark a check `n/a` only when it does not apply to the touched paths. On failure → `blocked`; no commit.

## Before a push

Every commit that reaches `origin/dev` has passed the checks above for everything it touches, including `npm run typecheck`. Under `/orchestrate` the `task-verifier` runs them in the scratch clone before the landing; `/cr-review` runs the full set below on the real repo before its push; a manual push the user asks for runs:

```bash
npm test && npm run lint && npm run typecheck && npm run db:check
go test ./... && golangci-lint run
```

## Rules

- Scratch clones run the checks in their own directory with their own `node_modules` (`npm ci`); never point a check at another agent's clone.
- Kill by PID only — never `pkill`/`killall`, which can take down sibling agents' processes.
