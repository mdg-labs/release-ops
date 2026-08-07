# Doc index — Release Ops

> Local spec shorthand map.

## Spec documents

| Shorthand | Path | Topics |
| --------- | ---- | ------ |
| `docs-site` | `apps/docs/` | Published docs (Starlight) → https://mdg-labs.github.io/release-ops/ |
| `docs` | `docs/index.html` | Legacy hub source (synced to site intro) |
| `spec` | `docs/specs.html` | MVP contract, APIs, UI, providers, CI/CD (§11), domain logic |
| `schema` | `db/schema.sql` | SQLite app.db schema (canonical DDL) |
| `schema-html` | `docs/schema.html` | Schema browser view (synced to site) |
| `stack` | `docs/stack.html` | Go, Next.js, COSS, Go session auth, Docker |
| `roadmap` | `docs/roadmap.html` | Plan file — 11 epics, 51 leaves; machine source: `docs/roadmap.json`; site: `/roadmap/` |

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
| `db-migrations` | Schema changes: edit `db/schema.sql` → `make migrate-diff` only; CI `npm run db:check` |
| `i18n` | No hardcoded UI strings in `apps/web/` — next-intl keys only; ESLint `i18next/no-literal-string` |

## Hot files (never parallelize)

- `migrations/` — single writer per schema change batch
- `docs/roadmap.html` — plan file checkboxes (when created)
