.PHONY: help migrate-diff migrate-up migrate-down sqlc-generate

# golang-migrate (https://github.com/golang-migrate/migrate) via tools/migrate (modernc sqlite).
# Optional global CLI: go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest
MIGRATE ?= go run ./tools/migrate
# sqlc CLI (https://sqlc.dev):
#   go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

APP_DB_PATH ?= /tmp/release-ops-app.db
DATABASE_URL ?= sqlite://$(APP_DB_PATH)

help:
	@echo "Database targets:"
	@echo "  make migrate-diff name=<snake_case>  Generate migrations from db/schema.sql drift (sqldiff)"
	@echo "  make migrate-up                       Apply pending migrations (golang-migrate up)"
	@echo "  make migrate-down                     Roll back one migration (golang-migrate down 1)"
	@echo "  make sqlc-generate                    Regenerate internal/store/db from queries/"
	@echo ""
	@echo "Environment:"
	@echo "  APP_DB_PATH=$(APP_DB_PATH)"
	@echo "  DATABASE_URL=$(DATABASE_URL)"

migrate-diff:
ifndef name
	$(error Usage: make migrate-diff name=<snake_case_description>)
endif
	node scripts/migrate-diff.mjs $(name)

migrate-up:
	@mkdir -p $(dir $(APP_DB_PATH))
	$(MIGRATE) -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	$(MIGRATE) -path migrations -database "$(DATABASE_URL)" down 1

sqlc-generate:
	sqlc generate
