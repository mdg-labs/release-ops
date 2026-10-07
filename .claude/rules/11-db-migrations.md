---
description: SQLite migrations — sqlite-migrate-generated only; never hand-write SQL migrations
paths:
  - "migrations/**/*"
  - "db/schema.sql"
  - "internal/store/migrate.go"
  - "Makefile"
---

# Database migrations (generated only)

## Rule

**Never hand-write or hand-edit** files under `migrations/*.sql`. A migration that is already
generated is immutable: its checksum header is recorded in every database that applied it, and
editing or deleting it makes startup fail.

Schema changes flow:

1. Edit **`db/schema.sql`** (canonical DDL — source of truth; not under `docs/`). Every table must be
   declared `STRICT`; use `TEXT` / `INTEGER` / `REAL` / `BLOB` column types only.
2. Run **`make db-migration name=<snake_case_description>`** — runs `sqlite-migrate generate`, which diffs
   `db/schema.sql` against the existing migrations and writes `migrations/<timestamp>_<name>.sql`.
   It refuses a change that drops a table or column; pass `-allow-destructive` only for a drop the task
   asks for, and say so in the commit. Renames prompt; use `-assume-renames` or `-assume-no-renames`
   when there is no terminal.
3. Review the generated file; commit it unedited together with `db/schema.sql`.
4. Run `make sqlc-generate` if the schema or `queries/` changed (sqlc reads `db/schema.sql`).

The Go server applies pending migrations on startup (`internal/store/migrate.go`), before it opens its
connection pool and listens. There is no separate migrate binary and no down migration.

## Tooling (all free, no paid services)

| Tool | Role |
|------|------|
| **`sqlite-migrate`** (`github.com/mdg-labs/sqlite-migrate`) | Generates migrations from `db/schema.sql`; library applies them at startup. Pinned as a `tool` in `go.mod` — run it as `go tool sqlite-migrate`, never a globally installed copy |
| **`migrations/embed.go`** | Embeds the generated `*.sql` files into the server binary |
| **sqlc** | Reads `db/schema.sql` (not `migrations/`) for Go types |

Apply behaviour: one transaction for all pending migrations, a `VACUUM INTO` snapshot first
(`<dir of APP_DB_PATH>/snapshots`, newest 3 kept), `foreign_key_check` and `integrity_check` before
commit. A failure rolls the whole batch back — there is no dirty state.

**Do not use Atlas, golang-migrate or sqldiff** — not in this project.

## Forbidden

- Hand-writing a file under `migrations/`, or copy-pasting `db/schema.sql` into it
- Editing or deleting a generated migration that has been committed (add a new one instead)
- A table without `STRICT` in `db/schema.sql`
- `-allow-destructive` to get past a refusal without the task asking for the drop
- Atlas, Drizzle, Prisma, or other paid/SaaS schema-diff services

## CI

`npm run db:check` runs `sqlite-migrate check`: fails if a migration file no longer matches its checksum
header, or if replaying `migrations/` does not reproduce `db/schema.sql`. It needs Go only, no database.

## Spec

See `docs/specs.html#schema-migrations`.
