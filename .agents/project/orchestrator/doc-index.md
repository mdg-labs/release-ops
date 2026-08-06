# Doc index — Release Ops

> Local spec shorthand map.

## Spec documents

| Shorthand | Path | Topics |
| --------- | ---- | ------ |
| `docs` | `docs/index.html` | Doc hub, MVP summary |
| `spec` | `docs/specs.html` | MVP contract, APIs, UI, providers, CI/CD (§11), domain logic |
| `schema` | `docs/schema.sql` | SQLite app.db schema (source file) |
| `schema-html` | `docs/schema.html` | Schema browser view (local preview) |
| `stack` | `docs/stack.html` | Go, Next.js, COSS, Go session auth, Docker |
| `roadmap` | `docs/roadmap.html` | Implementation phases (not created yet) |

## Verification commands (when scaffold exists)

| Scope | Command |
| ----- | ------- |
| Go test | `go test ./...` |
| Go lint | `golangci-lint run` |
| Web test | `npm test` (in `apps/web`) |
| Web typecheck | `npm run typecheck` (in `apps/web`) |
| CI layout | `docs/specs.html#ci` — `pr` / `dev` / `main` / `release` entrypoints |

Map committed paths per `.cursor/rules/06-local-ci-before-commit.mdc`.

## Phase gates

| Gate | Blocks |
| ---- | ------ |
| `db-migrations` | Schema changes only via `migrations/` (golang-migrate) |

## Hot files (never parallelize)

- `migrations/` — single writer per schema change batch
- `docs/roadmap.html` — plan file checkboxes (when created)
