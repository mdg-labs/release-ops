# Doc index — Release Ops

Shorthand → path map for spec references in task descriptions and dispatches, plus the verification commands agents run. Dispatches pass paths and `§` anchors; agents read the bodies.

## Spec documents

Precedence when docs conflict: `spec` → `schema` → `stack` → `hub` (see `.claude/rules/00-project.md`).

| Shorthand | Path | Topics |
| --------- | ---- | ------ |
| `spec` | `docs/specs.html` | MVP contract, APIs, UI, providers, ticket integrations, domain logic; anchors e.g. `#architecture`, `#ui`, `#i18n`, `#env`, `#ci`, `#schema-migrations` |
| `schema` | `db/schema.sql` | SQLite `app.db` schema (canonical DDL) |
| `schema-html` | `docs/schema.html` | Browser view of the schema only — never the source of truth |
| `stack` | `docs/stack.html` | Go, Next.js, COSS, Go session auth, Docker, repository layout |
| `roadmap` | `docs/roadmap.html` | Historical plan (11 epics) the Kaneo board was seeded from; generated from `docs/roadmap.json` |
| `threats` | `docs/threat-model.md` | Assets, attackers, entry points, invariants `T1`…, severity rubric, disclosure split |
| `hub` | `docs/index.html` | Doc hub (not the plan file) |
| `docs-site` | `apps/docs/` | Published customer docs (Starlight) → https://mdg-labs.github.io/release-ops/ |

## Verification commands

| Paths touched | Command |
| ------------- | ------- |
| `apps/web/**`, root `package.json` / lockfile | `npm test && npm run lint && npm run typecheck` (lint enforces `i18next/no-literal-string`) |
| `db/schema.sql`, `migrations/**` | `npm run db:check` (+ Go gate when sqlc output changes) |
| `cmd/**`, `internal/**`, `queries/**`, `sqlc.yaml`, `go.mod` | `go test ./... && golangci-lint run` |
| `docs/**` only | no test/lint gate — sanity-check the edited HTML/Markdown |
| `apps/docs/**` | `npm run docs:build` |
| Before a push | everything above that applies — run by `task-verifier` under `/orchestrate` |
| CI layout | `docs/specs.html#ci` — `pr` / `dev` / `main` / `release` entrypoints |

Full mapping: `.claude/rules/06-local-ci-before-commit.md`.

## Phase gates

| Gate | Blocks |
| ---- | ------ |
| `db-migrations` | Schema changes: edit `db/schema.sql` → `make db-migration name=<change>` only; `npm run db:check` (`.claude/rules/11-db-migrations.md`) |
| `i18n` | No hardcoded UI strings in `apps/web/` — next-intl keys only (`.claude/rules/10-i18n.md`) |

## Hot files (never parallelize)

- `db/schema.sql`, `migrations/` — single writer per schema change
- `package.json`, `package-lock.json`, `go.mod`, `go.sum` — root dependency manifests
- `apps/web/messages/en.json` — shared i18n message file
