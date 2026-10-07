.PHONY: help db-migration db-check sqlc-generate

# sqlite-migrate (https://github.com/mdg-labs/sqlite-migrate) runs as a `tool` pinned in go.mod,
# so `make`, `npm run db:check` and CI all use the same version.
SQLITE_MIGRATE ?= go tool sqlite-migrate
# sqlc CLI (https://sqlc.dev):
#   go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

help:
	@echo "Database targets:"
	@echo "  make db-migration name=<snake_case>  Generate a migration from db/schema.sql changes (sqlite-migrate generate)"
	@echo "  make db-check                         Verify migrations/ checksums and that they reproduce db/schema.sql"
	@echo "  make sqlc-generate                    Regenerate internal/store/db from db/schema.sql and queries/"
	@echo ""
	@echo "Migrations are applied by the Go server on startup."

db-migration:
ifndef name
	$(error Usage: make db-migration name=<snake_case_description>)
endif
	$(SQLITE_MIGRATE) generate -schema db/schema.sql -dir migrations -m $(name)

db-check:
	$(SQLITE_MIGRATE) check -schema db/schema.sql -dir migrations

sqlc-generate:
	sqlc generate
