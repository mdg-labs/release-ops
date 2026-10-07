# Doc index — Release Ops

Shorthand → path map for prompt Doc Refs, plus the verification commands sub-agents run. The orchestrator passes paths and `§` anchors; sub-agents read the bodies.

## Spec documents

Precedence when docs conflict: `spec` → `schema` → `stack` → `hub` (see `.claude/rules/00-project.md`).

| Shorthand | Path | Topics |
| --------- | ---- | ------ |
| `spec` | `docs/specs.html` | MVP contract, APIs, UI, providers, ticket integrations, domain logic; anchors e.g. `#architecture`, `#ui`, `#i18n`, `#env`, `#ci`, `#schema-migrations` |
| `schema` | `db/schema.sql` | SQLite `app.db` schema (canonical DDL) |
| `schema-html` | `docs/schema.html` | Browser view of the schema only — never the source of truth |
| `stack` | `docs/stack.html` | Go, Next.js, COSS, Go session auth, Docker, repository layout |
| `roadmap` | `docs/roadmap.html` | **Plan file** — 11 epics, 51 leaves, status checkboxes; generated from `docs/roadmap.json` |
| `hub` | `docs/index.html` | Doc hub (not the plan file) |
| `docs-site` | `apps/docs/` | Published customer docs (Starlight) → https://mdg-labs.github.io/release-ops/ |

## Verification commands

| Paths touched | Command |
| ------------- | ------- |
| `apps/web/**`, root `package.json` / lockfile | `npm test && npm run lint` (lint enforces `i18next/no-literal-string`) |
| `db/schema.sql`, `migrations/**` | `npm run db:check` (+ Go gate when sqlc output changes) |
| `cmd/**`, `internal/**`, `queries/**`, `sqlc.yaml`, `go.mod` | `go test ./... && golangci-lint run` |
| `docs/**` only | no test/lint gate — sanity-check the edited HTML/Markdown |
| Pre-push (only when the user asks to push) | `npm test && npm run lint && npm run typecheck && npm run db:check` (+ Go gate) |
| CI layout | `docs/specs.html#ci` — `pr` / `dev` / `main` / `release` entrypoints |

Full mapping: `.claude/rules/06-local-ci-before-commit.md`.

## Phase gates

| Gate | Blocks |
| ---- | ------ |
| `db-migrations` | Schema changes: edit `db/schema.sql` → `make migrate-diff name=<change>` only; `npm run db:check` (`.claude/rules/11-db-migrations.md`) |
| `i18n` | No hardcoded UI strings in `apps/web/` — next-intl keys only (`.claude/rules/10-i18n.md`) |

## Hot files (never parallelize)

- `db/schema.sql`, `migrations/` — single writer per schema change
- `docs/roadmap.html` / `docs/roadmap.json` — plan file
- `package.json`, `package-lock.json`, `go.mod`, `go.sum` — root dependency manifests
- `apps/web/messages/en.json` — shared i18n message file
