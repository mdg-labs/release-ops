---
description: Release Ops project identity, doc precedence, and current-phase awareness
---

# Release Ops

Self-hosted release monitor — polls releases on GitHub, GitLab, Gitea, Forgejo, and Codeberg; creates tickets when new releases ship. Single Docker container (Next.js + Go); config via Web UI (COSS).

## Tech stack

- Go API + polling (`cmd/server`), `sqlc`, `golang-migrate`, SQLite (`app.db`)
- Next.js App Router + COSS UI (`apps/web`) — same container image; auth in Go
- **next-intl** — all UI strings via message files; ESLint enforces no literals (`10-i18n.md`)
- Vitest (web), `go test` (server), ESLint, Prettier, golangci-lint
- Docker: one image `ghcr.io/{owner}/release-ops`; GitHub Actions → GHCR

## Repository state

Implementation in progress: Go server (`cmd/server`, `internal/`), web app (`apps/web`), customer docs site (`apps/docs`), schema + generated migrations (`db/schema.sql`, `migrations/`). Work is planned and tracked on the Kaneo board (project Release Ops, `RO-<n>`); `docs/roadmap.html` is the historical plan the board was seeded from.

## Spec doc precedence (when docs conflict)

1. `docs/specs.html` — MVP contract, APIs, UI, domain logic
2. `db/schema.sql` — database schema (canonical DDL; `docs/schema.html` is browser view only)
3. `docs/stack.html` — tooling choices
4. `docs/index.html` — doc hub (not implementation roadmap)

Deprecated (do not use): old learning-path HTML under <code>docs/</code> — removed.

## Hard rules

- If behaviour is not defined in a spec doc, **ask before guessing**.
- Never invent fields, endpoints, or IDs not in the spec.
- Schema changes only via **sqldiff-generated** migrations — edit `db/schema.sql`, run `make migrate-diff`; never hand-write `migrations/*.sql` (`11-db-migrations.md`)
- Web UI: **zero hardcoded user-facing strings** — next-intl message keys only (`10-i18n.md`)

## Agent config

- Orchestration: `.claude/skills/orchestrate/SKILL.md` + `templates/` (project-owned; edit in place); agents in `.claude/agents/` (`task-executor`, `task-verifier`, `task-refiner`, `security-reviewer`, `security-verifier`, `ci-investigator`)
- Promotion: `/dev-diff`, `/open-pr`, `/cr-review`; security: `/security-audit` against `docs/threat-model.md`
- Project constants (Kaneo IDs, lookup rules, area → paths): `.agents/project/orchestrator/project.config.md`
- Board: Kaneo (MCP `Kaneo`, or the claude.ai connector `claude_ai_Kaneo`); GitHub issues are read-only for agents (`07-commit-linking.md`)
- Threat model: `docs/threat-model.md` — the yardstick for security review and severity
