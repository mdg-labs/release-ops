import { SPEC, STACK, SCHEMA } from "../spec-links.mjs";

/** @param {{ epic: Function, leaf: Function }} reg */
export function registerE01E06({ epic, leaf }) {
  // ─── E01 Foundation ───────────────────────────────────────────────────────
  epic("E01", "Foundation — scaffold, database & Go server", "config", {
    depends_on: [],
    context:
      "Monorepo skeleton (Go + Next.js), db/schema.sql with sqldiff migrations, sqlc scaffold, " +
      "and runnable Go server with config, chi router, GET /healthz, SQLite + golang-migrate. " +
      "Merges former P00, P01, P02.",
    specs: [
      SPEC.architecture,
      SPEC.schema,
      SPEC.schemaMigrations,
      SPEC.deployment,
      SPEC.env,
    ],
    stack: [STACK.layout, STACK.backend, STACK.data, STACK.quality],
    implementation: [
      "go.mod, cmd/server, internal/* placeholders, apps/web Next 15 + COSS",
      "db/schema.sql → make migrate-diff name=init, npm run db:check",
      "sqlc.yaml, Makefile migrate-up/down/diff",
      "internal/config, chi router, GET /healthz, modernc sqlite",
    ],
    acceptance: [
      "go build ./cmd/server and npm run lint pass",
      "npm run db:check passes after initial migration",
      "migrate-up applies to fresh app.db",
      "GET /healthz returns 200",
      "Layout matches stack.html",
    ],
  });

  leaf("E01-01", "E01", "Scaffold Go module, internal packages, and Next.js COSS web", "config", {
    context:
      "Repository layout per stack.html: cmd/server, internal/{api,config,store,crypto,poll,providers}, " +
      "apps/web with Next.js 15 App Router, Tailwind v4, COSS components.json, root package.json scripts.",
    specs: [SPEC.architecture, SPEC.ui],
    stack: [STACK.layout, STACK.frontend, STACK.quality],
    implementation: [
      "go mod init, cmd/server stub on 127.0.0.1:8080",
      "internal package skeleton with doc.go per package",
      "apps/web: layout, page, globals.css, components.json COSS registry",
      "Root package.json: test, lint, typecheck",
      "VERSION file, .gitignore",
    ],
    acceptance: [
      "go build ./cmd/server succeeds",
      "go build ./... with internal packages",
      "npm run dev starts Next on :3000",
      "COSS Button installable",
      "npm test runs Vitest smoke",
      "golangci-lint run passes",
      "Directory layout matches stack.html",
    ],
    files: ["go.mod", "cmd/server/main.go", "apps/web/", "package.json", "VERSION"],
    tests: ["go build ./...", "npm test", "npm run lint"],
  });

  leaf("E01-02", "E01", "Database schema, migrate-diff, and initial migration", "db", {
    depends_on: ["E01-01"],
    context:
      "Canonical DDL in db/schema.sql. scripts/migrate-diff.mjs uses SQLite sqldiff to generate " +
      "migrations/000001_init.{up,down}.sql. Never hand-write migrations/*.sql.",
    specs: [SPEC.schema, SPEC.schemaMigrations],
    schema: [SCHEMA.sql, SCHEMA.html],
    stack: [STACK.data],
    implementation: [
      "db/schema.sql — all 11 tables from spec §4",
      "scripts/migrate-diff.mjs + scripts/ci/check-migrations-sync.mjs",
      "make migrate-diff name=init",
      "npm run db:check in package.json",
    ],
    acceptance: [
      "db/schema.sql has all 10 tables: users, sessions, app_settings, integrations",
      "ticket_projects, monitored_repos, notification_targets, monitored_repo_notifications",
      "poll_runs, poll_run_events",
      "migrate-diff generates 000001_init up/down",
      "npm run db:check passes",
      "PRAGMA foreign_keys in migration",
      "integrations.kind CHECK all 8 kinds",
    ],
    files: ["db/schema.sql", "scripts/migrate-diff.mjs", "migrations/000001_init.up.sql"],
    tests: ["make migrate-diff name=init", "npm run db:check"],
  });

  leaf("E01-03", "E01", "sqlc, golang-migrate CLI, and store connection", "db", {
    depends_on: ["E01-02"],
    context:
      "sqlc.yaml points at migrations/; queries/ directory; Open SQLite with foreign_keys ON; " +
      "driver modernc.org/sqlite (CGO-free per stack.html); migrate-up target.",
    specs: [SPEC.schema, SPEC.deployment],
    stack: [STACK.data, STACK.backend],
    implementation: [
      "sqlc.yaml — engine sqlite, schema migrations/, queries queries/",
      "Makefile: migrate-diff, migrate-up, migrate-down",
      "internal/store/db.go — Open, MaxOpenConns(1), Ping",
      "migrate_test.go — apply up, verify tables",
    ],
    acceptance: [
      "sqlc generate produces compilable Go",
      "make migrate-up applies to temp db",
      "store opens app.db with foreign keys via modernc.org/sqlite",
      "migrate_test passes",
      "queries/ directory exists",
      "golang-migrate documented in Makefile",
      "DSN uses APP_DB_PATH env",
    ],
    files: ["sqlc.yaml", "Makefile", "internal/store/db.go", "internal/store/migrate_test.go"],
    tests: ["go test ./internal/store/...", "sqlc generate"],
  });

  leaf("E01-04", "E01", "Go config loader and environment variables", "backend", {
    depends_on: ["E01-01"],
    context: "Load SESSION_SECRET, APP_ENCRYPTION_KEY, APP_DB_PATH, GO_INTERNAL_PORT, PORT per specs §9.",
    specs: [SPEC.env, SPEC.architecture],
    stack: [STACK.backend],
    implementation: [
      "internal/config/config.go — Load from env",
      "Validate required: SESSION_SECRET, APP_ENCRYPTION_KEY",
      "Defaults: APP_DB_PATH=/data/app.db, GO_INTERNAL_PORT=8080, PORT=3000",
      "config_test.go",
    ],
    acceptance: [
      "Missing SESSION_SECRET fails fast",
      "APP_DB_PATH default /data/app.db",
      "GO_INTERNAL_PORT default 8080",
      "Config struct passed to server main",
      "Unit tests for defaults",
      "APP_PUBLIC_URL optional for Secure cookies",
      "BOOTSTRAP_ADMIN_* optional pair",
    ],
    files: ["internal/config/config.go", "internal/config/config_test.go"],
    tests: ["go test ./internal/config/..."],
  });

  leaf("E01-05", "E01", "Chi HTTP router, middleware, and GET /healthz", "backend", {
    depends_on: ["E01-04"],
    context:
      "chi router on 127.0.0.1:GO_INTERNAL_PORT. GET /healthz no auth (container probe). " +
      "API subrouter /api/v1 scaffold for later epics.",
    specs: [SPEC.architecture, SPEC.api],
    stack: [STACK.backend],
    implementation: [
      "internal/api/router.go — MountAPI, /api/v1 subrouter",
      "GET /healthz handler",
      "Recovery + request logging middleware",
      "Graceful shutdown on SIGTERM",
      "cmd/server wires router",
    ],
    acceptance: [
      "GET /healthz returns 200 JSON",
      "Server binds 127.0.0.1 only",
      "chi MountAPI exports /api/v1 subrouter",
      "Graceful shutdown completes in-flight requests",
      "go test ./internal/api/...",
      "Router test lists registered paths",
      "JSON Content-Type on API subrouter",
    ],
    files: ["internal/api/router.go", "internal/api/health.go", "cmd/server/main.go"],
    tests: ["go test ./internal/api/...", "curl http://127.0.0.1:8080/healthz"],
  });

  // ─── E02 Auth & crypto ────────────────────────────────────────────────────
  epic("E02", "Authentication & credential encryption", "backend", {
    depends_on: ["E01"],
    context:
      "Go-only auth: scs sessions, bcrypt users, release_ops_session cookie, AES-GCM for integration secrets. " +
      "POST /api/v1/auth/login, logout, GET /api/v1/auth/session. Middleware protects /api/v1/*. Merges P03+P04.",
    specs: [SPEC.auth, SPEC.api, SPEC.schema],
    stack: [STACK.backend, STACK.data],
    acceptance: [
      "Login sets HttpOnly session cookie",
      "Protected routes return 401 without session",
      "Encrypted payloads never in API responses",
      "Bootstrap admin on first start",
    ],
  });

  leaf("E02-01", "E02", "AES-GCM credential encryption service", "backend", {
    depends_on: ["E01-04"],
    context:
      "APP_ENCRYPTION_KEY 64 hex chars → AES-256-GCM. Wire format per §4.8: " +
      "base64(nonce + ciphertext + tag) with 12-byte random nonce.",
    specs: [SPEC.schema, SPEC.env],
    stack: [STACK.backend],
    implementation: [
      "internal/crypto/aesgcm.go — Encrypt, Decrypt",
      "12-byte random nonce prepended to ciphertext+tag; base64 wire encoding",
      "crypto_test.go with fixed test vector",
    ],
    acceptance: [
      "Encrypt/decrypt roundtrip",
      "Wrong key fails decrypt",
      "64 hex char key validation",
      "Wire format base64(nonce+ciphertext+tag) per §4.8",
      "Used by store layer for encrypted_payload columns",
      "No plaintext secrets logged",
      "Tests cover empty and large payloads",
    ],
    files: ["internal/crypto/aesgcm.go", "internal/crypto/crypto_test.go"],
    tests: ["go test ./internal/crypto/..."],
  });

  leaf("E02-02", "E02", "Users store, bcrypt, and scs session manager", "backend", {
    depends_on: ["E01-03", "E02-01"],
    context: "sqlc users queries; alexedwards/scs v2 with SQLite sessions table; bcrypt cost 12.",
    specs: [SPEC.auth, SPEC.schema],
    schema: [SCHEMA.sql],
    implementation: [
      "queries/users.sql — CreateUser, GetUserByEmail, CountUsers",
      "internal/api/auth/password.go — HashPassword, ComparePassword",
      "internal/api/auth/session.go — NewSessionManager",
      "Session lifetime 7 days, idx_sessions_expiry",
    ],
    acceptance: [
      "GetUserByEmail returns user row",
      "bcrypt hash verifies password",
      "Session persists across restart",
      "sessions table used by scs store",
      "CountUsers for bootstrap guard",
      "sqlc generate users models",
      "HttpOnly cookie name release_ops_session",
    ],
    files: ["queries/users.sql", "internal/api/auth/session.go", "internal/api/auth/password.go"],
    tests: ["go test ./internal/api/auth/..."],
  });

  leaf("E02-03", "E02", "Auth HTTP handlers and session middleware", "backend", {
    depends_on: ["E02-02"],
    context:
      "POST /api/v1/auth/login, POST /api/v1/auth/logout, GET /api/v1/auth/session. " +
      "Middleware on /api/v1/* except login and session.",
    specs: [SPEC.auth, SPEC.api],
    implementation: [
      "internal/api/auth/handlers.go — Login, Logout, Session",
      "internal/api/middleware/auth.go — RequireSession",
      "JSON error envelope per §7",
      "Register on /api/v1 router",
    ],
    acceptance: [
      "POST /api/v1/auth/login sets cookie on valid credentials",
      "POST /api/v1/auth/logout clears session",
      "GET /api/v1/auth/session returns user or null",
      "Protected route returns 401 without cookie",
      "Invalid password returns 401 not 500",
      "SameSite=Lax, Path=/, HttpOnly",
      "Login rate limit not required MVP",
    ],
    files: ["internal/api/auth/handlers.go", "internal/api/middleware/auth.go"],
    tests: ["go test ./internal/api/auth/..."],
  });

  leaf("E02-04", "E02", "Bootstrap admin and seed-admin CLI", "backend", {
    depends_on: ["E02-03"],
    context:
      "BOOTSTRAP_ADMIN_EMAIL/PASSWORD env on first start when CountUsers=0; cmd/seed-admin CLI. " +
      "No POST /api/v1/auth/register and no in-app setup wizard (§3).",
    specs: [SPEC.auth, SPEC.env, SPEC.deployment],
    implementation: [
      "Bootstrap in server startup before Listen",
      "cmd/seed-admin for manual admin creation",
      "Skip bootstrap when users exist",
    ],
    acceptance: [
      "First boot with env vars creates admin user",
      "Second boot does not duplicate user",
      "seed-admin CLI creates user interactively",
      "Bootstrap skipped when users > 0",
      "No auth register endpoint in MVP",
      "Invalid email rejected",
      "Password hashed with bcrypt",
      "Document in getting-started (E11)",
    ],
    files: ["internal/api/auth/bootstrap.go", "cmd/seed-admin/main.go"],
    tests: ["go test ./internal/api/auth/... -run Bootstrap"],
  });

  // ─── E03 Store layer ──────────────────────────────────────────────────────
  epic("E03", "Store layer — sqlc repositories", "db", {
    depends_on: ["E01", "E02"],
    context: "Typed repositories for all domain tables. Merges P05.",
    specs: [SPEC.schema, SPEC.domain],
    stack: [STACK.data],
    acceptance: [
      "All §4 tables queryable via sqlc",
      "Repositories used by API handlers",
      "Foreign key relationships respected",
    ],
  });

  leaf("E03-01", "E03", "sqlc queries for all domain tables", "db", {
    depends_on: ["E01-03"],
    context:
      "queries/*.sql for app_settings, integrations, ticket_projects, monitored_repos, " +
      "notification_targets, monitored_repo_notifications, poll_runs, poll_run_events.",
    specs: [SPEC.schema, SPEC.ticketProjects],
    schema: [SCHEMA.sql],
    implementation: [
      "queries/app_settings.sql, integrations.sql, ticket_projects.sql",
      "queries/monitored_repos.sql, notification_targets.sql, poll_runs.sql",
      "queries/monitored_repo_notifications.sql — replace join rows per repo",
      "CRUD + list enabled + update poll state on repos",
    ],
    acceptance: [
      "sqlc generate compiles all query files",
      "Integrations ListByKind query",
      "MonitoredRepos ListEnabled with notification target IDs",
      "monitored_repo_notifications replace on repo update",
      "PollRuns InsertRun, FinishRun, InsertEvent",
      "TicketProjects UNIQUE constraint in queries",
      "Poll run tickets_superseded counter update",
    ],
    files: ["queries/*.sql"],
    tests: ["sqlc generate", "go build ./..."],
  });

  leaf("E03-02", "E03", "Domain repository wrappers with crypto pass-through", "db", {
    depends_on: ["E03-01", "E02-01"],
    context: "internal/store/*.go wraps sqlc; encrypt on write, decrypt on read for credential columns.",
    specs: [SPEC.schema, SPEC.providers],
    implementation: [
      "internal/store/settings.go, integrations.go, ticket_projects.go",
      "internal/store/repos.go, notifications.go, poll.go",
      "UUID v4 generation, UTC ISO8601 timestamps",
    ],
    acceptance: [
      "Integration Create encrypts encrypted_payload",
      "Get never returns decrypted secrets to API layer raw",
      "UpdatePollState on monitored_repos",
      "Repo Create/Update persists notificationTargetIds join rows",
      "Repository interfaces mockable for tests",
      "app_settings Get returns id=1",
      "Notification targets events_json valid JSON",
    ],
    files: ["internal/store/integrations.go", "internal/store/repos.go"],
    tests: ["go test ./internal/store/..."],
  });

  leaf("E03-03", "E03", "Seed app_settings and store integration tests", "db", {
    depends_on: ["E03-02"],
    context: "app_settings row id=1 with poll_interval_minutes default 360; integration tests against temp SQLite.",
    specs: [SPEC.schema, SPEC.api],
    implementation: [
      "Seed in migration or store test fixture",
      "store_integration_test.go — full CRUD smoke",
    ],
    acceptance: [
      "GetAppSettings returns poll_interval_minutes >= 5",
      "Integration CRUD roundtrip in test db",
      "Ticket project create with json_valid configs",
      "Monitored repo UNIQUE(source_kind, project_path) enforced",
      "migrate_test still passes",
      "Tests use t.TempDir for app.db",
      "No test data committed to repo",
    ],
    files: ["internal/store/store_integration_test.go"],
    tests: ["go test ./internal/store/..."],
  });

  // ─── E04 REST API ─────────────────────────────────────────────────────────
  epic("E04", "REST API — all /api/v1 handlers", "backend", {
    depends_on: ["E02", "E03"],
    context:
      "CRUD handlers for settings, integrations, ticket-projects, repos, notification-targets; " +
      "status and poll endpoints. Register all routes on chi router. Merges P06–P10, P15.",
    specs: [SPEC.api, SPEC.auth],
    stack: [STACK.backend],
    acceptance: [
      "All §7 endpoints implemented",
      "Session middleware on protected routes",
      "JSON shapes match spec",
    ],
  });

  leaf("E04-01", "E04", "Settings API — GET/PATCH /api/v1/settings", "backend", {
    depends_on: ["E03-02", "E02-03"],
    context: "poll_interval_minutes min 5; GET returns current; PATCH validates and persists.",
    specs: [SPEC.api, SPEC.schema],
    implementation: ["internal/api/handlers/settings.go", "Register on /api/v1/settings"],
    acceptance: [
      "GET /api/v1/settings returns pollIntervalMinutes",
      "PATCH /api/v1/settings updates value",
      "PATCH rejects value < 5",
      "Requires valid session",
      "Response JSON matches §7 shape",
      "updated_at set on patch",
      "Handler unit tests with mock store",
    ],
    files: ["internal/api/handlers/settings.go"],
    tests: ["go test ./internal/api/handlers/..."],
  });

  leaf("E04-02", "E04", "Integrations API — CRUD and test connection", "backend", {
    depends_on: ["E03-02", "E02-03"],
    context: "GET/POST/PATCH/DELETE /api/v1/integrations; POST .../test validates credentials without persisting.",
    specs: [SPEC.api, SPEC.providers],
    implementation: [
      "handlers/integrations.go — full CRUD",
      "TestConnection calls provider ping",
      "Never return encrypted_payload in JSON",
    ],
    acceptance: [
      "GET /api/v1/integrations lists without secrets",
      "List items include hasSecret boolean per §7.3",
      "POST creates integration with encrypted token",
      "PATCH updates name/base_url only if token omitted",
      "DELETE returns 409 when referenced by repos or ticket projects",
      "POST test connection returns success/error",
      "All 8 integration kinds accepted",
    ],
    files: ["internal/api/handlers/integrations.go"],
    tests: ["go test ./internal/api/handlers/..."],
  });

  leaf("E04-03", "E04", "Ticket projects API — full CRUD", "backend", {
    depends_on: ["E04-02"],
    context: "GET/POST/PATCH/DELETE /api/v1/ticket-projects; create_config and status_mapping JSON.",
    specs: [SPEC.api, SPEC.ticketProjects],
    implementation: ["handlers/ticket_projects.go"],
    acceptance: [
      "CRUD /api/v1/ticket-projects",
      "create_config and status_mapping json_valid",
      "on_open_ticket_policy supersede|merge|skip_if_open",
      "UNIQUE(integration_id, external_project_id)",
      "DELETE returns 409 when referenced by monitored repos",
      "List filtered by integration_id",
      "Handler tests with fixtures",
    ],
    files: ["internal/api/handlers/ticket_projects.go"],
    tests: ["go test ./internal/api/handlers/..."],
  });

  leaf("E04-04", "E04", "Monitored repos API — full CRUD", "backend", {
    depends_on: ["E04-03"],
    context:
      "GET/POST/PATCH/DELETE /api/v1/repos; source_kind, project_path, ticket_project_id; " +
      "sourceIntegrationId required for gitlab/gitea/forgejo; notificationTargetIds optional array " +
      "persisted via monitored_repo_notifications join.",
    specs: [SPEC.api, SPEC.domain],
    implementation: ["handlers/repos.go"],
    acceptance: [
      "CRUD /api/v1/repos",
      "POST/PATCH accept notificationTargetIds array",
      "sourceIntegrationId required for self-hosted source kinds",
      "UNIQUE(source_kind, project_path)",
      "enabled flag 0/1",
      "ticket_project_id FK required",
      "open_ticket fields read-only from API",
    ],
    files: ["internal/api/handlers/repos.go"],
    tests: ["go test ./internal/api/handlers/..."],
  });

  leaf("E04-05", "E04", "Notification targets API — CRUD and test", "backend", {
    depends_on: ["E03-02", "E02-03"],
    context: "GET/POST/PATCH/DELETE /api/v1/notification-targets; POST test sends Shoutrrr ping.",
    specs: [SPEC.api, SPEC.notifications],
    implementation: ["handlers/notifications.go"],
    acceptance: [
      "CRUD /api/v1/notification-targets",
      "shoutrrr_url never returned after create",
      "events_json default create,error,supersede",
      "POST test notification async-safe",
      "enabled toggle",
      "monitored_repo_notifications routing optional",
      "JSON events array validated",
    ],
    files: ["internal/api/handlers/notifications.go"],
    tests: ["go test ./internal/api/handlers/..."],
  });

  leaf("E04-06", "E04", "Status and poll API endpoints", "backend", {
    depends_on: ["E03-02", "E02-03"],
    context:
      "GET /api/v1/status returns full §7.1 JSON (repos[], lastRun, isPolling, ticketsSuperseded); " +
      "POST /api/v1/poll/trigger async 202; GET /api/v1/poll/runs paginated; GET /api/v1/poll/runs/{id} with events[].",
    specs: [SPEC.api, SPEC.domain],
    implementation: ["handlers/status.go", "handlers/poll.go"],
    acceptance: [
      "GET /api/v1/status returns repos[] with ticketProjectName",
      "GET /api/v1/status includes lastRun and isPolling fields",
      "POST /api/v1/poll/trigger returns 202 Accepted async",
      "GET /api/v1/poll/runs paginated list default limit 20",
      "GET /api/v1/poll/runs/{id} includes events[]",
      "Trigger returns 409 if poll already running",
      "poll_run_events.action enum values in response",
    ],
    files: ["internal/api/handlers/poll.go", "internal/api/handlers/status.go"],
    tests: ["go test ./internal/api/handlers/..."],
  });

  leaf("E04-07", "E04", "Register all API routes and integration test", "backend", {
    depends_on: ["E04-01", "E04-02", "E04-03", "E04-04", "E04-05", "E04-06", "E02-03"],
    context: "Wire every §7 handler on /api/v1; public auth routes before session middleware; route list test.",
    specs: [SPEC.api, SPEC.auth, SPEC.architecture],
    implementation: [
      "internal/api/routes.go — RegisterAll(deps)",
      "routes_test.go — compile-time or httptest route inventory",
      "Document route order in comment",
    ],
    acceptance: [
      "All §7 endpoints registered",
      "POST /api/v1/auth/login public",
      "GET /api/v1/auth/session public",
      "All other /api/v1/* require session",
      "No duplicate route mounts",
      "routes_test lists expected paths",
      "Integration test: login → GET settings 200",
    ],
    files: ["internal/api/routes.go", "internal/api/routes_test.go"],
    tests: ["go test ./internal/api/..."],
  });

  // ─── E05 Source providers ───────────────────────────────────────────────────
  epic("E05", "Source providers — GitHub, GitLab, Gitea, Forgejo, Codeberg", "backend", {
    depends_on: ["E04"],
    context: "SourceProvider interface; fetch latest release tag per monitored repo. Merges P11.",
    specs: [SPEC.providers, SPEC.domain],
    acceptance: ["All five source kinds implemented", "Mock tests in CI"],
  });

  leaf("E05-01", "E05", "SourceProvider interface and registry", "backend", {
    depends_on: ["E04-02"],
    context: "SourceProvider.GetLatestRelease(ctx, repo) → tag, url, publishedAt; registry by source_kind.",
    specs: [SPEC.providers],
    implementation: ["internal/providers/source/provider.go", "registry.go"],
    acceptance: [
      "Interface documented per §6",
      "Registry returns provider by kind",
      "Unknown kind returns error",
      "Context cancellation respected",
      "No HTTP in interface consumers",
      "Mock provider for tests",
      "Package compiles standalone",
    ],
    files: ["internal/providers/source/provider.go"],
    tests: ["go test ./internal/providers/source/..."],
  });

  leaf("E05-02", "E05", "GitHub and Codeberg source providers", "backend", {
    depends_on: ["E05-01"],
    context: "github source — api.github.com, no base_url. codeberg — codeberg.org API, no base_url.",
    specs: [SPEC.providers],
    implementation: ["internal/providers/source/github.go", "codeberg.go"],
    acceptance: [
      "GitHub source provider fetches latest release tag",
      "Codeberg source provider works",
      "Uses integration token from decrypt",
      "Handles 404 no releases",
      "Rate limit errors surfaced",
      "Unit tests with httptest mock",
      "project_path owner/repo format",
    ],
    files: ["internal/providers/source/github.go", "internal/providers/source/codeberg.go"],
    tests: ["go test ./internal/providers/source/..."],
  });

  leaf("E05-03", "E05", "GitLab, Gitea, and Forgejo source providers", "backend", {
    depends_on: ["E05-01"],
    context:
      "Self-hosted base_url required; GitLab API v4; GiteaCompatibleSource shared client for " +
      "gitea, forgejo, codeberg (§6) — same API, different default base_url.",
    specs: [SPEC.providers],
    implementation: [
      "internal/providers/source/gitlab.go",
      "internal/providers/source/gitea_compatible.go — GiteaCompatibleSource",
      "gitea.go, forgejo.go thin wrappers",
    ],
    acceptance: [
      "GitLab source provider with base_url",
      "GiteaCompatibleSource used for gitea, forgejo, codeberg",
      "base_url trailing slash normalized",
      "Invalid base_url errors clearly",
      "Mock HTTP tests per provider",
      "404 no release not treated as error",
      "publishedAt mapped from provider response",
    ],
    files: ["internal/providers/source/gitlab.go", "internal/providers/source/gitea.go"],
    tests: ["go test ./internal/providers/source/..."],
  });

  leaf("E05-04", "E05", "Source provider mock tests for CI", "backend", {
    depends_on: ["E05-02", "E05-03"],
    context:
      "go test with mock HTTP for each source kind; required for MVP AC #13. " +
      "Poll skip uses exact tag string equality (§5.1) — provider tests assert tag extraction only.",
    specs: [SPEC.mvp, SPEC.ci],
    implementation: ["internal/providers/source/*_test.go with httptest"],
    acceptance: [
      "Mock test per source provider kind",
      "CI go test ./internal/providers/source/...",
      "No live network in unit tests",
      "Coverage for error paths",
      "Tag fields mapped correctly from provider JSON",
      "Empty release list handled",
      "Tests run in parallel safe",
    ],
    files: ["internal/providers/source/github_test.go"],
    tests: ["go test ./internal/providers/source/..."],
  });

  // ─── E06 Ticket providers ─────────────────────────────────────────────────
  epic("E06", "Ticket providers — Phasical, Jira, Linear", "backend", {
    depends_on: ["E04", "E05"],
    context: "TicketProvider create/update/close; ticket content §5.4 Release: format. Merges P12.",
    specs: [SPEC.providers, SPEC.ticketProjects, SPEC.domain],
    acceptance: ["Three ticket providers", "Status mapping per project"],
  });

  leaf("E06-01", "E06", "TicketProvider interface, ClassifyStatus, and content builder", "backend", {
    depends_on: ["E04-03"],
    context:
      "TicketProvider per §6: CreateTicket, GetTicketStatus, UpdateTicketStatus, AddTicketComment, " +
      "UpdateTicket (merge). ClassifyStatus(mapping, rawStatus) → open|done|cancelled|unknown. " +
      "Ticket content §5.4: title Release: {source_kind} {project_path} {tag}; body with URL and publishedAt.",
    specs: [SPEC.providers, SPEC.domain, SPEC.ticketProjects],
    implementation: [
      "internal/providers/ticket/provider.go",
      "internal/providers/ticket/classify.go — ClassifyStatus",
      "content.go — includes publishedAt in markdown body",
    ],
    acceptance: [
      "ClassifyStatus maps external status via status_mapping",
      "Ticket content §5.4 title format Release:",
      "Description includes release URL and publishedAt",
      "Interface: CreateTicket, GetTicketStatus, UpdateTicketStatus",
      "Interface: AddTicketComment, UpdateTicket for merge policy",
      "ClassifyStatus unit tests with mapping fixtures",
      "No provider-specific logic in poll engine",
    ],
    files: ["internal/providers/ticket/content.go", "internal/providers/ticket/provider.go"],
    tests: ["go test ./internal/providers/ticket/..."],
  });

  leaf("E06-02", "E06", "Phasical and Jira ticket providers", "backend", {
    depends_on: ["E06-01"],
    context:
      "Phasical POST {base_url}/task/{projectId}; Jira REST with email+token; " +
      "GetTicketStatus for live open-ticket check; AddTicketComment for supersede (old tag → new tag, release link).",
    specs: [SPEC.providers],
    implementation: ["internal/providers/ticket/phasical.go", "jira.go"],
    acceptance: [
      "Phasical ticket provider creates task",
      "Jira ticket provider creates issue with email auth",
      "GetTicketStatus returns provider-native status slug",
      "AddTicketComment on supersede with release link",
      "Status transitions via status_mapping",
      "Mock tests httptest",
      "base_url required for both",
    ],
    files: ["internal/providers/ticket/phasical.go", "internal/providers/ticket/jira.go"],
    tests: ["go test ./internal/providers/ticket/..."],
  });

  leaf("E06-03", "E06", "Linear ticket provider and provider mock tests", "backend", {
    depends_on: ["E06-01"],
    context:
      "Linear GraphQL API; status mapping MVP uses stateId strings only (openTypes post-MVP). " +
      "Mock CI tests for all three ticket providers.",
    specs: [SPEC.providers, SPEC.mvp],
    implementation: ["internal/providers/ticket/linear.go", "*_test.go"],
    acceptance: [
      "Linear ticket provider creates issue",
      "team id from external_project_id",
      "ClassifyStatus with Linear stateId mapping",
      "Mock test per ticket provider",
      "CI runs provider tests",
      "supersede closes prior ticket with comment",
      "merge policy updates existing via UpdateTicket",
    ],
    files: ["internal/providers/ticket/linear.go"],
    tests: ["go test ./internal/providers/ticket/..."],
  });
}
