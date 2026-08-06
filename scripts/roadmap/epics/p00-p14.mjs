import { SPEC, STACK, SCHEMA } from "../spec-links.mjs";

export function registerP00P14({ epic, leaf }) {
  // ─── P00 Scaffold ─────────────────────────────────────────────────────────
  epic("P00", "Repository scaffold & dev tooling", "config", {
    context:
      "Release Ops ships as a single Docker image with a Go API (127.0.0.1:8080) and Next.js UI (:3000). " +
      "Before any domain logic, the repository must match stack.html layout: Go module at repo root, " +
      "apps/web for Next.js 15 App Router, internal/ packages for backend concerns, and shared lint/test tooling. " +
      "This epic establishes the monorepo skeleton autonomous agents will extend through P14.",
    specs: [
      { ...SPEC.architecture, note: "Single-container bind addresses and process responsibilities" },
      { ...SPEC.deployment, note: "Future Dockerfile expects this directory layout" },
    ],
    stack: [
      { ...STACK.layout, note: "Canonical folder tree: cmd/, internal/, apps/web/, migrations/" },
      { ...STACK.quality, note: "golangci-lint, ESLint, Prettier, Vitest entry points" },
    ],
    implementation: [
      "Create go.mod with module path github.com/mdg-labs/release-ops (or org repo path)",
      "cmd/server/main.go — HTTP stub on GO_INTERNAL_PORT default 8080",
      "internal/{api,config,store,crypto,poll,providers}/ package placeholders",
      "apps/web — Next.js 15 App Router, TypeScript, Tailwind v4, COSS components.json",
      "Root package.json scripts: test, lint, typecheck delegating to web + go test",
      "VERSION file semver without v prefix; .gitignore for app.db, node_modules, .next",
      "golangci-lint + ESLint/Prettier configs runnable from CI",
    ],
    acceptance: [
      "go build ./cmd/server succeeds from clean clone",
      "npm run lint passes on apps/web scaffold",
      "npm test runs at least one Vitest smoke test",
      "Directory layout matches stack.html repository layout section",
      "COSS registry configured in apps/web/components.json",
      "golangci-lint run passes with zero issues on scaffold",
      "VERSION file present with valid semver",
      ".gitignore excludes app.db, node_modules, .next, agent-memory",
    ],
  });

  leaf("P00-01", "P00", "Initialize Go module and cmd/server entrypoint", "config", {
    depends_on: [],
    context:
      "The Go binary is the long-running server inside the container: migrations, auth, REST /api/v1, poll scheduler. " +
      "Start with a minimal main that listens on 127.0.0.1:8080 per specs.html#architecture. " +
      "Module path must match the GitHub org/repo for import consistency.",
    specs: [
      { ...SPEC.architecture, note: "Go binds 127.0.0.1:GO_INTERNAL_PORT — not exposed publicly" },
      { ...SPEC.env, note: "GO_INTERNAL_PORT, PORT env vars used later by P02" },
    ],
    stack: [
      { ...STACK.backend, note: "Go 1.22+, chi router added in P02-02" },
      { ...STACK.layout, note: "cmd/server/main.go is the production entrypoint" },
    ],
    implementation: [
      "go mod init with correct module path at repo root",
      "cmd/server/main.go — net/http or chi stub listening on :8080",
      "Read GO_INTERNAL_PORT from env with default 8080 (config wiring in P02-01)",
      "Create empty migrations/ directory for golang-migrate (P01)",
      "Log startup message with slog placeholder",
      "Graceful shutdown on SIGTERM for Docker",
    ],
    acceptance: [
      "go.mod exists at repository root with valid module directive",
      "cmd/server/main.go compiles without errors",
      "go build -o /tmp/release-ops ./cmd/server produces runnable binary",
      "Binary listens on 127.0.0.1:8080 by default",
      "migrations/ directory exists (may be empty)",
      "go test ./... passes (no tests yet is OK)",
      "Module path matches GitHub repository import path",
    ],
    files: ["go.mod", "cmd/server/main.go", "migrations/.gitkeep"],
    tests: ["go build ./cmd/server"],
  });

  leaf("P00-02", "P00", "Create internal package skeleton", "backend", {
    depends_on: ["P00-01"],
    context:
      "Backend code lives under internal/ per Go conventions. Each subpackage maps to a roadmap epic: " +
      "api (handlers), config (env), store (sqlc repos), crypto (AES-GCM), poll (scheduler), providers (source/ticket). " +
      "Placeholders prevent import cycles and give agents clear ownership boundaries.",
    specs: [
      { ...SPEC.architecture, note: "Go owns auth, REST, polling, credential decrypt" },
      { ...SPEC.providers, note: "internal/providers will host SourceProvider and TicketProvider" },
    ],
    stack: [
      { ...STACK.layout, note: "internal/ package tree per stack.html" },
      { ...STACK.backend, note: "Package-per-concern layout for sqlc and chi" },
    ],
    implementation: [
      "internal/api/ — HTTP handlers, router, middleware (doc.go)",
      "internal/config/ — env loader stub",
      "internal/store/ — DB connection + sqlc output target",
      "internal/crypto/ — AES-256-GCM encrypt/decrypt stub",
      "internal/poll/ — scheduler and decision engine stub",
      "internal/providers/source/ and internal/providers/ticket/ directories",
      "Each package exports package doc comment describing responsibility",
    ],
    acceptance: [
      "internal/api, config, store, crypto, poll, providers packages exist",
      "go build ./... succeeds with placeholder packages",
      "No import cycles between internal packages",
      "internal/providers/source and internal/providers/ticket subdirs exist",
      "Each top-level internal package has doc.go or typed placeholder",
      "cmd/server can import internal/api without build errors",
      "Package names match directory names (lowercase)",
    ],
    files: [
      "internal/api/doc.go",
      "internal/config/doc.go",
      "internal/store/doc.go",
      "internal/crypto/doc.go",
      "internal/poll/doc.go",
      "internal/providers/source/doc.go",
      "internal/providers/ticket/doc.go",
    ],
    tests: ["go build ./..."],
  });

  leaf("P00-03", "P00", "Scaffold apps/web Next.js 15 App Router", "web", {
    context:
      "The web UI is Next.js 15 App Router in apps/web. It proxies /api/go/* to the Go server and renders COSS components. " +
      "Scaffold with TypeScript, app/ directory layout, and dev script. Auth pages come in P16/P19; this task is structure only.",
    specs: [
      { ...SPEC.ui, note: "Next.js App Router, COSS UI — pages added in P16+" },
      { ...SPEC.architecture, note: "Next.js public on :3000, proxies to Go" },
    ],
    stack: [
      { ...STACK.frontend, note: "Next.js 15, TypeScript, App Router" },
      { ...STACK.layout, note: "apps/web workspace package" },
    ],
    implementation: [
      "apps/web/package.json with next, react, react-dom, typescript",
      "apps/web/app/layout.tsx — root layout with html/body",
      "apps/web/app/page.tsx — placeholder home",
      "apps/web/tsconfig.json extending shared or local config",
      "next.config.ts — standalone output flag for Docker (P26)",
      "npm run dev starts next dev on port 3000",
    ],
    acceptance: [
      "apps/web/package.json exists with next 15.x dependency",
      "app/layout.tsx and app/page.tsx render without error",
      "npm run dev starts Next.js dev server",
      "TypeScript compiles with strict mode",
      "App Router directory structure (not pages/)",
      "Root page returns 200 in dev",
      "package name scoped or named @release-ops/web",
    ],
    files: ["apps/web/package.json", "apps/web/app/layout.tsx", "apps/web/app/page.tsx", "apps/web/tsconfig.json"],
    tests: ["cd apps/web && npm run build"],
  });

  leaf("P00-04", "P00", "Configure Tailwind CSS v4 and COSS registry", "web", {
    depends_on: ["P00-03"],
    context:
      "Release Ops UI uses COSS (shadcn-compatible) particles per specs.html#ui. Tailwind v4 is required. " +
      "components.json must point at the COSS registry so agents can `npx shadcn@latest add` primitives. " +
      "globals.css imports Tailwind and COSS theme tokens.",
    specs: [
      { ...SPEC.ui, note: "COSS particles: p-card-1, p-field-2, p-table-4, p-drawer-10, etc." },
      { ...SPEC.architecture, note: "UI-only; no auth in Next.js" },
    ],
    stack: [
      { ...STACK.frontend, note: "Tailwind v4 + COSS registry URL" },
      { ...STACK.quality, note: "PostCSS/Tailwind config for lint pipeline" },
    ],
    implementation: [
      "apps/web/components.json — registry URL https://coss.com/ui/r/registry.json",
      "apps/web/app/globals.css — @import tailwindcss and COSS CSS variables",
      "Install tailwindcss@4, @tailwindcss/postcss",
      "postcss.config.mjs for Tailwind v4",
      "Verify Button primitive installable: npx shadcn@latest add button",
      "lib/utils.ts with cn() helper (clsx + tailwind-merge)",
    ],
    acceptance: [
      "Tailwind v4 configured (not v3 config format)",
      "components.json points at COSS registry",
      "globals.css imported in root layout",
      "At least one COSS/shadcn component (Button) installable and renders",
      "cn() utility available in lib/utils.ts",
      "npm run build succeeds with Tailwind processing",
      "Dark theme CSS variables present per COSS defaults",
    ],
    files: ["apps/web/components.json", "apps/web/app/globals.css", "apps/web/postcss.config.mjs", "apps/web/lib/utils.ts"],
    tests: ["cd apps/web && npm run build"],
  });

  leaf("P00-05", "P00", "Add root package.json workspace scripts", "config", {
    depends_on: ["P00-03"],
    context:
      "CI and local dev need a single entry point for test/lint/typecheck. Root package.json orchestrates " +
      "apps/web npm scripts and go test/golangci-lint. Matches specs.html#ci parallel job expectations.",
    specs: [
      { ...SPEC.ci, note: "CI runs go lint/test and web lint/test/typecheck in parallel" },
      { ...SPEC.mvp, note: "All quality gates must pass before MVP sign-off" },
    ],
    stack: [
      { ...STACK.quality, note: "Unified npm scripts at repo root" },
      { ...STACK.cicd, note: "Scripts invoked by .github/workflows/ci.yml in P27" },
    ],
    implementation: [
      "Root package.json with private: true",
      "script test: cd apps/web && npm test && go test ./...",
      "script lint: cd apps/web && npm run lint && golangci-lint run",
      "script typecheck: cd apps/web && npm run typecheck (or tsc --noEmit)",
      "Optional workspaces: [\"apps/web\"] if using npm workspaces",
      "Document scripts in README or docs/stack.html cross-ref",
    ],
    acceptance: [
      "Root package.json exists with test, lint scripts",
      "npm test runs web tests and go test ./...",
      "npm run lint runs ESLint and golangci-lint",
      "npm run typecheck runs TypeScript check on apps/web",
      "Scripts exit non-zero on failure",
      "No package-lock at root required if web has own lockfile",
      "Scripts work from clean clone after npm install in apps/web",
    ],
    files: ["package.json"],
    tests: ["npm test", "npm run lint"],
  });

  leaf("P00-06", "P00", "Add golangci-lint configuration", "config", {
    depends_on: ["P00-01"],
    context:
      "Go code quality is enforced via golangci-lint in CI. Configure linters appropriate for chi, sqlc, " +
      "and standard library usage. Must pass on scaffold before P02 adds real handlers.",
    specs: [
      { ...SPEC.ci, note: "golangci-lint is a required CI step" },
      { ...SPEC.architecture, note: "All Go code under cmd/ and internal/" },
    ],
    stack: [
      { ...STACK.quality, note: "golangci-lint version pinned in CI" },
      { ...STACK.backend, note: "Linters: govet, staticcheck, errcheck, gosimple" },
    ],
    implementation: [
      ".golangci.yml at repo root",
      "Enable: govet, staticcheck, errcheck, ineffassign, unused",
      "run.go: '1.22' or project Go version",
      "timeout: 5m for CI",
      "Exclude testdata and vendor if present",
      "Document golangci-lint install in stack.html or CONTRIBUTING",
    ],
    acceptance: [
      ".golangci.yml exists at repository root",
      "golangci-lint run exits 0 on current scaffold",
      "Config includes govet and staticcheck",
      "Go version in config matches go.mod",
      "No false positives on cmd/server/main.go",
      "Lint integrated into root npm run lint script",
      "CI-ready (non-interactive)",
    ],
    files: [".golangci.yml"],
    tests: ["golangci-lint run"],
  });

  leaf("P00-07", "P00", "Configure ESLint and Prettier for apps/web", "config", {
    depends_on: ["P00-03"],
    context:
      "Web code uses ESLint flat config (eslint.config.mjs) and Prettier for formatting. " +
      "Must integrate with Next.js and TypeScript. npm run lint is a scoped CI gate for apps/web.",
    specs: [
      { ...SPEC.ui, note: "React/Next components will be linted here" },
      { ...SPEC.ci, note: "npm run lint required in CI web job" },
    ],
    stack: [
      { ...STACK.frontend, note: "ESLint 9 flat config, Prettier" },
      { ...STACK.quality, note: "eslint-config-next or equivalent" },
    ],
    implementation: [
      "apps/web/eslint.config.mjs — flat config with typescript-eslint",
      "apps/web/.prettierrc — semi, singleQuote, trailingComma",
      "package.json scripts: lint, lint:fix, format",
      "Ignore .next, node_modules in eslint ignores",
      "Add eslint-plugin-react-hooks",
      "Prettier integrated or separate format script",
    ],
    acceptance: [
      "npm run lint passes on apps/web scaffold",
      "eslint.config.mjs uses flat config format",
      ".prettierrc exists with project style",
      "TypeScript files (.ts, .tsx) are linted",
      "No lint errors on app/layout.tsx and app/page.tsx",
      "lint:fix auto-fixes safe issues",
      "ESLint ignores .next build output",
    ],
    files: ["apps/web/eslint.config.mjs", "apps/web/.prettierrc"],
    tests: ["cd apps/web && npm run lint"],
  });

  leaf("P00-08", "P00", "Set up Vitest and React Testing Library", "web", {
    depends_on: ["P00-03"],
    context:
      "Frontend tests use Vitest + React Testing Library per stack.html. At least one smoke test proves " +
      "the test runner works in CI. Component tests for login, dashboard, etc. come in P19+.",
    specs: [
      { ...SPEC.ui, note: "UI tests validate COSS component integration" },
      { ...SPEC.mvp, note: "MVP includes page-level tests per roadmap" },
    ],
    stack: [
      { ...STACK.frontend, note: "Vitest, @testing-library/react, jsdom" },
      { ...STACK.quality, note: "npm test invoked from root package.json" },
    ],
    implementation: [
      "apps/web/vitest.config.ts — environment jsdom, setupFiles",
      "apps/web/vitest.setup.ts — import @testing-library/jest-dom",
      "package.json script: test → vitest run",
      "Smoke test: app/page renders 'Release Ops' or similar",
      "Path alias @/ mapped in vitest config matching tsconfig",
      "Optional test script in root delegates to apps/web",
    ],
    acceptance: [
      "vitest.config.ts exists and configures jsdom",
      "npm test runs vitest in run mode (non-watch)",
      "At least one .test.tsx file passes",
      "@testing-library/jest-dom matchers available",
      "@/ path alias resolves in tests",
      "Tests run in CI without headed browser",
      "npm test from repo root succeeds",
    ],
    files: ["apps/web/vitest.config.ts", "apps/web/vitest.setup.ts", "apps/web/app/page.test.tsx"],
    tests: ["cd apps/web && npm test"],
  });

  leaf("P00-09", "P00", "Add VERSION file and extend .gitignore", "config", {
    context:
      "Release versioning uses a root VERSION file (no v prefix) read by CI prepare-release workflow (P27). " +
      ".gitignore must exclude SQLite app.db, build artifacts, and agent session memory.",
    specs: [
      { ...SPEC.ci, note: "prepare-release.yml reads VERSION for tag bump" },
      { ...SPEC.deployment, note: "app.db lives on /data volume in container" },
    ],
    stack: [
      { ...STACK.cicd, note: "VERSION file semver for GHCR image tags" },
      { ...STACK.data, note: "SQLite app.db path APP_DB_PATH default /data/app.db" },
    ],
    implementation: [
      "VERSION file containing 0.1.0 (semver, no v prefix)",
      ".gitignore entries: app.db, *.db, node_modules/, apps/web/node_modules/",
      ".gitignore: apps/web/.next/, dist/, .env, .env.local",
      ".gitignore: .agents/project/agent-memory/ (session files)",
      "Optional: coverage/, tmp/",
      "Document VERSION bump process in P27 prepare-release",
    ],
    acceptance: [
      "VERSION file exists with valid semver (e.g. 0.1.0)",
      "VERSION does not include 'v' prefix",
      ".gitignore covers app.db and node_modules",
      ".gitignore covers apps/web/.next",
      ".gitignore covers agent-memory directory",
      ".env and .env.local ignored",
      "git status clean after local dev artifacts generated",
    ],
    files: ["VERSION", ".gitignore"],
    tests: ["test -f VERSION && grep -qE '^[0-9]+\\.[0-9]+\\.[0-9]+$' VERSION"],
  });

  leaf("P00-10", "P00", "Configure next-intl provider and message files", "web", {
    depends_on: ["P00-03"],
    context:
      "All user-facing UI copy must use next-intl per specs.html#i18n and .cursor/rules/10-i18n.mdc. " +
      "MVP ships English only (messages/en.json) but the provider must be in place before any page ships real copy. " +
      "No hardcoded strings in components — enforced by ESLint in P00-11.",
    specs: [
      { ...SPEC.i18n, note: "§8.5 next-intl, messages/en.json, useTranslations" },
      { ...SPEC.ui, note: "All §8.3 page copy via message keys" },
    ],
    stack: [
      { ...STACK.frontend, note: "next-intl App Router setup" },
      { ...STACK.quality, note: "Messages colocated under apps/web/messages/" },
    ],
    implementation: [
      "npm install next-intl in apps/web",
      "apps/web/messages/en.json — namespaces: common, auth, dashboard, repos, integrations, ticketProjects, notifications, settings",
      "apps/web/i18n/request.ts — getRequestConfig default locale en",
      "apps/web/i18n/routing.ts — locales: ['en'], defaultLocale: 'en'",
      "Wrap root layout with NextIntlClientProvider",
      "middleware.ts integrates next-intl middleware (coordinate with P18-04)",
      "Seed en.json with placeholder keys for app shell nav items",
    ],
    acceptance: [
      "next-intl installed and configured for App Router",
      "apps/web/messages/en.json exists with namespaced keys",
      "Root layout wraps children with NextIntlClientProvider",
      "useTranslations('common') works in a test component",
      "Default locale is en",
      "No page component contains hardcoded English UI strings (stub pages use t())",
      "Document pattern in apps/web/README or comment in i18n/request.ts",
      "npm run build succeeds with i18n wiring",
    ],
    files: [
      "apps/web/messages/en.json",
      "apps/web/i18n/request.ts",
      "apps/web/i18n/routing.ts",
      "apps/web/app/layout.tsx",
    ],
    tests: ["cd apps/web && npm run build"],
    relatedTasks: ["P00-11", "P18-01"],
  });

  leaf("P00-11", "P00", "Add ESLint i18n no-literal-string rule", "web", {
    depends_on: ["P00-10", "P00-07"],
    context:
      "CI and local lint must fail on hardcoded JSX text and toast strings per specs.html#i18n. " +
      "Use eslint-plugin-i18next with i18next/no-literal-string. Allowlist: className, testId, route paths, technical enums.",
    specs: [
      { ...SPEC.i18n, note: "§8.5 CI — eslint-plugin-i18next" },
      { ...SPEC.ci, note: "§11.4 web lint job includes i18n rule" },
    ],
    stack: [
      { ...STACK.quality, note: "eslint-plugin-i18next in apps/web" },
      { ...STACK.cicd, note: "npm run lint fails on literal strings" },
    ],
    implementation: [
      "npm install -D eslint-plugin-i18next",
      "Enable i18next/no-literal-string in eslint.config.mjs",
      "options: mode 'jsx-text-only' or 'all' per plugin docs",
      "ignoreAttribute: ['className', 'data-testid', 'href', 'type', 'name']",
      "ignoreCallee: ['console.log', 'Error']",
      "Root package.json lint script runs apps/web lint",
      "Add one intentional test file proving rule catches violations",
    ],
    acceptance: [
      "eslint-plugin-i18next configured",
      "i18next/no-literal-string error on <span>Hardcoded</span> in test",
      "npm run lint passes on scaffold using t() keys only",
      "CI web lint job will run same config (P27-07)",
      "Rule documented in .cursor/rules/10-i18n.mdc cross-ref",
      "aria-label must use t() — not ignored by rule",
      "Toast messages must use t() — verified in mutation hooks P17-05",
      "No disable comments added project-wide",
    ],
    files: ["apps/web/eslint.config.mjs"],
    tests: ["npm run lint"],
    relatedTasks: ["P00-10", "P27-07"],
  });

  // ─── P01 Database ─────────────────────────────────────────────────────────
  epic("P01", "Database migrations & sqlc", "db", {
    depends_on: ["P00-01"],
    context:
      "SQLite schema is canonical in db/schema.sql: users, sessions, integrations, ticket_projects, " +
      "monitored_repos, notification_targets, poll_runs, poll_run_events. golang-migrate applies versioned SQL; " +
      "sqlc generates type-safe Go from queries/. This epic is the data foundation for all store and API work.",
    specs: [
      { ...SPEC.schema, note: "Table definitions, CHECK constraints, JSON columns" },
      { ...SPEC.auth, note: "users + sessions tables for scs session store" },
      { ...SPEC.schemaMigrations, note: "sqldiff-generated migrations only" },
    ],
    stack: [
      { ...STACK.data, note: "golang-migrate, sqlc, modernc.org/sqlite" },
      { ...SCHEMA.sql, note: "Source of truth for migrate-diff" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "All 11 tables + indexes" },
      { ...SCHEMA.html, note: "Human-readable ER reference" },
    ],
    implementation: [
      "scripts/migrate-diff.mjs + make migrate-diff name=init → migrations/000001_init.{up,down}.sql",
      "scripts/ci/check-migrations-sync.mjs — npm run db:check verifies db/schema.sql vs migrations/",
      "sqlc.yaml — engine sqlite, schema migrations/, queries queries/",
      "Makefile targets: migrate-diff, migrate-up, migrate-down",
      "sqlc generate → internal/store/db/*.go",
      "Migration smoke test with temp SQLite file",
    ],
    acceptance: [
      "npm run db:check passes (migrations match db/schema.sql)",
      "Down migration cleanly drops all tables",
      "sqlc generate produces compilable Go code",
      "migrate-up applies to fresh app.db without errors",
      "Foreign keys enforced (PRAGMA foreign_keys=ON)",
      "integrations.kind CHECK includes all 8 kinds",
      "ticket_projects.on_open_ticket_policy CHECK: supersede|merge|skip_if_open",
    ],
  });

  leaf("P01-01", "P01", "Generate initial migration via migrate-diff", "db", {
    depends_on: ["P00-01"],
    context:
      "Baseline migration is generated by scripts/migrate-diff.mjs (SQLite sqldiff) from empty DB → db/schema.sql. " +
      "Must include PRAGMA foreign_keys, all CHECK constraints on integrations.kind, ticket_projects JSON validity, " +
      "and poll_run_events.action enum.",
    specs: [
      { ...SPEC.schema, note: "§4 full table definitions" },
      { ...SPEC.ticketProjects, note: "ticket_projects columns: create_config, status_mapping, on_open_ticket_policy" },
      { ...SPEC.schemaMigrations, note: "Never hand-write migrations/*.sql" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "Canonical DDL — migrate-diff reads this file" },
      { ...SCHEMA.html, note: "Verify column types match" },
    ],
    implementation: [
      "Add scripts/migrate-diff.mjs — sqldiff between migrated DB and db/schema.sql",
      "make migrate-diff name=init → migrations/000001_init.up.sql and .down.sql",
      "Up migration creates users, sessions, app_settings, integrations, ticket_projects",
      "Include monitored_repos, notification_targets, monitored_repo_notifications",
      "Include poll_runs, poll_run_events and all indexes",
      "Down migration generated by sqldiff (reverse transform)",
    ],
    acceptance: [
      "Up migration creates all 11 tables",
      "integrations CHECK enforces base_url rules per kind",
      "ticket_projects UNIQUE(integration_id, external_project_id)",
      "monitored_repos UNIQUE(source_kind, project_path)",
      "poll_run_events.action IN baseline,skip,create,supersede,merge,skip_open,error",
      "Down migration removes all objects without error",
      "npm run db:check passes after generation",
    ],
    files: [
      "db/schema.sql",
      "scripts/migrate-diff.mjs",
      "migrations/000001_init.up.sql",
      "migrations/000001_init.down.sql",
    ],
    tests: ["make migrate-diff name=init", "npm run db:check"],
  });

  leaf("P01-02", "P01", "Add golang-migrate CLI targets", "db", {
    depends_on: ["P01-01"],
    context:
      "Developers and Docker entrypoint need repeatable migrate-up/down commands. Use golang-migrate CLI " +
      "with file://migrations source and sqlite3://$APP_DB_PATH database URL.",
    specs: [
      { ...SPEC.deployment, note: "Entrypoint runs migrate up before Go server starts" },
      { ...SPEC.env, note: "APP_DB_PATH default /data/app.db" },
    ],
    stack: [
      { ...STACK.data, note: "golang-migrate CLI install instructions" },
      { ...STACK.deploy, note: "migrate invoked in docker/entrypoint.sh P26" },
    ],
    implementation: [
      "Makefile targets: migrate-diff, migrate-up, migrate-down",
      "DATABASE_URL=sqlite3://$APP_DB_PATH file://migrations",
      "Document migrate CLI install: go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest",
      "Optional npm script wrapper in root package.json",
      "migrate-up creates parent dir for APP_DB_PATH if missing",
      "Pin migrate version in stack.html or Makefile comment",
    ],
    acceptance: [
      "make migrate-up applies migrations to test db",
      "make migrate-down rolls back one version",
      "Works with sqlite3:// path including directory creation",
      "Documented in Makefile help or README",
      "Second migrate-up is idempotent (no error)",
      "migrate version table exists after up",
      "CLI documented for local dev without Docker",
    ],
    files: ["Makefile"],
    tests: ["make migrate-up DATABASE_URL=sqlite3:///tmp/test.db"],
  });

  leaf("P01-03", "P01", "Configure sqlc.yaml and query directory", "db", {
    depends_on: ["P01-01"],
    context:
      "sqlc generates Go structs and query methods from SQL in queries/. Schema path points at migrations " +
      "so generated types match applied DB. Package output: internal/store/db.",
    specs: [
      { ...SPEC.schema, note: "Column types map to Go: TEXT→string, INTEGER→int64" },
      { ...SPEC.providers, note: "Queries will join integrations for decrypt in store layer" },
    ],
    stack: [
      { ...STACK.data, note: "sqlc v2 config format" },
      { ...STACK.backend, note: "Generated code in internal/store/db" },
    ],
    implementation: [
      "sqlc.yaml version 2 at repo root",
      "sql: schema: migrations/, queries: queries/",
      "gen.go: package db, out internal/store/db, sql_package database/sql",
      "engine: sqlite, emit_json_tags: true",
      "Create queries/ directory with .gitkeep or first stub",
      "Document sqlc install: go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest",
    ],
    acceptance: [
      "sqlc.yaml exists and validates with sqlc compile",
      "queries/ directory present",
      "Schema path includes 000001_init.up.sql tables",
      "Output package is internal/store/db",
      "emit_json_tags enabled for API serialization",
      "sqlc version documented",
      "sqlc generate runs without error (may emit empty package initially)",
    ],
    files: ["sqlc.yaml", "queries/.gitkeep"],
    tests: ["sqlc compile"],
  });

  leaf("P01-04", "P01", "Wire sqlc generate into Go module", "db", {
    depends_on: ["P01-03"],
    context:
      "After first real queries are added (P05), sqlc generate must integrate into go build. " +
      "For this task, add minimal query (e.g. SELECT 1) or users stub so generate produces compilable output.",
    specs: [
      { ...SPEC.schema, note: "users table: id, email, password_hash, created_at, updated_at" },
      { ...SPEC.auth, note: "First queries target users for P03" },
    ],
    stack: [
      { ...STACK.data, note: "sqlc generate in CI and pre-commit optional" },
      { ...STACK.backend, note: "import release-ops/internal/store/db" },
    ],
    implementation: [
      "queries/health.sql or stub query for code gen smoke test",
      "Run sqlc generate → internal/store/db/*.go",
      "go.mod requires no extra deps for sqlc output",
      "Add //go:generate sqlc generate to internal/store/doc.go",
      "Makefile target: sqlc-generate",
      "Verify go build ./internal/store/... after generate",
    ],
    acceptance: [
      "sqlc generate produces files in internal/store/db/",
      "go build ./... succeeds after generate",
      "Generated models include table name comments",
      "go generate ./internal/store works",
      "No hand-edits to generated db/*.go (regenerated only)",
      "sqlc diff clean after generate",
      "CI can run sqlc generate && git diff --exit-code",
    ],
    files: ["internal/store/db/", "queries/health.sql"],
    tests: ["sqlc generate && go build ./..."],
  });

  leaf("P01-05", "P01", "Add migration smoke test", "db", {
    depends_on: ["P01-02"],
    context:
      "Automated test applies up migration to temp SQLite file, verifies schema_migrations version, " +
      "and optionally queries sqlite_master for expected tables. Catches drift from db/schema.sql.",
    specs: [
      { ...SPEC.schema, note: "All tables must exist after migrate up" },
      { ...SPEC.mvp, note: "DB layer tests required for MVP confidence" },
    ],
    stack: [
      { ...STACK.data, note: "Test uses modernc.org/sqlite in-memory or temp file" },
      { ...STACK.quality, note: "go test ./internal/store/..." },
    ],
    implementation: [
      "internal/store/migrate_test.go",
      "Use golang-migrate programmatic API or exec migrate CLI",
      "Temp file :memory: or t.TempDir()/test.db",
      "Assert schema_migrations version = 1",
      "Query sqlite_master for integrations, monitored_repos tables",
      "Test down migration drops tables (optional subtest)",
    ],
    acceptance: [
      "go test ./internal/store/... -run Migrate passes",
      "Test creates fresh DB and applies up migration",
      "Verifies at least integrations and users tables exist",
      "Test is parallel-safe (unique temp paths)",
      "Fails if migration SQL has syntax error",
      "No leftover test.db in repo",
      "Runs in CI without external deps",
    ],
    files: ["internal/store/migrate_test.go"],
    tests: ["go test ./internal/store/... -run Migrate"],
  });

  // ─── P02 Go core ──────────────────────────────────────────────────────────
  epic("P02", "Go server core, config & health", "backend", {
    depends_on: ["P01-04"],
    context:
      "The Go server boots inside the container: load env config, open SQLite, run migrations, mount chi router " +
      "at /api/v1, structured logging, JSON error helper, and unauthenticated GET /healthz. Foundation for auth (P03) " +
      "and all REST handlers P06–P15.",
    specs: [
      { ...SPEC.architecture, note: "Go process responsibilities and bind address" },
      { ...SPEC.env, note: "SESSION_SECRET, APP_ENCRYPTION_KEY, APP_DB_PATH, PORT, GO_INTERNAL_PORT" },
    ],
    stack: [
      { ...STACK.backend, note: "chi router, slog, golang-migrate programmatic" },
      { ...STACK.data, note: "SQLite connection pool via modernc.org/sqlite" },
    ],
    implementation: [
      "internal/config/config.go — Load() from env with validation",
      "internal/api/router.go — chi.NewRouter(), Mount /api/v1",
      "internal/store/db.go — sql.Open sqlite, SetMaxOpenConns",
      "internal/store/migrate.go — migrate.Up on startup",
      "internal/api/errors.go — WriteError(w, code, message, status)",
      "internal/api/logging.go — request ID middleware with slog",
      "internal/api/health.go — GET /healthz → 200 OK",
    ],
    acceptance: [
      "Server starts with valid env and runs migrations",
      "GET /healthz returns 200 without auth",
      "Missing SESSION_SECRET fails fast on startup",
      "Router serves /api/v1 prefix for future handlers",
      "JSON errors match { error: { code, message } }",
      "SQLite pool opens APP_DB_PATH (creates file if needed)",
      "Structured logs include request method and path",
    ],
  });

  leaf("P02-01", "P02", "Implement environment config loader", "backend", {
    depends_on: ["P00-02"],
    context:
      "Config loads from environment per specs.html#env: SESSION_SECRET (32+ bytes), APP_ENCRYPTION_KEY (64 hex chars), " +
      "APP_DB_PATH, GO_INTERNAL_PORT, GO_API_URL, APP_PUBLIC_URL, BOOTSTRAP_ADMIN_EMAIL/PASSWORD. Validate on startup.",
    specs: [
      { ...SPEC.env, note: "Full env var table with defaults and required flags" },
      { ...SPEC.auth, note: "SESSION_SECRET for scs; BOOTSTRAP_ADMIN_* for first user" },
    ],
    stack: [
      { ...STACK.backend, note: "Config struct in internal/config" },
      { ...STACK.deploy, note: "Env vars set in docker-compose.yml P26" },
    ],
    implementation: [
      "type Config struct with all env fields",
      "Load() reads os.Getenv with defaults: GO_INTERNAL_PORT=8080, APP_DB_PATH=/data/app.db",
      "Validate SESSION_SECRET min length 32",
      "Validate APP_ENCRYPTION_KEY is 64 hex characters (32 bytes)",
      "APP_PUBLIC_URL required for cookie Secure flag logic",
      "Export Config singleton or pass to server constructor",
      "Unit tests with t.Setenv for each validation rule",
    ],
    acceptance: [
      "Load() returns error when SESSION_SECRET missing",
      "Load() returns error when APP_ENCRYPTION_KEY not 64 hex",
      "Defaults applied for GO_INTERNAL_PORT and APP_DB_PATH",
      "BOOTSTRAP_ADMIN_EMAIL optional (empty skips bootstrap)",
      "Config struct exported for injection into api/store",
      "go test ./internal/config/... passes",
      "No hardcoded secrets in source",
    ],
    files: ["internal/config/config.go", "internal/config/config_test.go"],
    tests: ["go test ./internal/config/..."],
  });

  leaf("P02-02", "P02", "Set up chi router with /api/v1 prefix", "backend", {
    depends_on: ["P02-01"],
    context:
      "All REST endpoints live under /api/v1 per specs.html#api. Chi subrouter pattern keeps auth, settings, " +
      "integrations routes organized. Mount health outside /api/v1 or at root /healthz.",
    specs: [
      { ...SPEC.api, note: "Base path /api/v1 for all JSON endpoints" },
      { ...SPEC.auth, note: "Auth routes: /api/v1/auth/login, logout, session" },
    ],
    stack: [
      { ...STACK.backend, note: "github.com/go-chi/chi/v5" },
      { ...STACK.layout, note: "internal/api/router.go central mount point" },
    ],
    implementation: [
      "internal/api/router.go — NewRouter(cfg, store) http.Handler",
      "r.Get(/healthz, healthHandler) at root router",
      "r.Route(/api/v1, func(r chi.Router) { ... })",
      "Placeholder routes or 404 for unimplemented paths",
      "chi middleware: RequestID, RealIP, Recoverer",
      "cmd/server/main.go calls api.NewRouter and ListenAndServe",
    ],
    acceptance: [
      "GET /healthz reachable on main router",
      "/api/v1 subrouter exists",
      "Unregistered /api/v1/foo returns 404 JSON",
      "chi RequestID middleware active",
      "Router returned from constructor, not global",
      "main.go uses config.GoInternalPort for listen addr",
      "go build ./cmd/server succeeds",
    ],
    files: ["internal/api/router.go", "cmd/server/main.go"],
    tests: ["go build ./cmd/server"],
  });

  leaf("P02-03", "P02", "Open SQLite connection pool", "backend", {
    depends_on: ["P02-01", "P01-04"],
    context:
      "Single SQLite file app.db per specs. Use modernc.org/sqlite (pure Go) or mattn/go-sqlite3 per stack.html. " +
      "Connection opened once at startup, injected into store and scs session store.",
    specs: [
      { ...SPEC.schema, note: "SQLite single-writer; pool size 1 recommended" },
      { ...SPEC.deployment, note: "/data volume mount for app.db persistence" },
    ],
    stack: [
      { ...STACK.data, note: "modernc.org/sqlite driver registration" },
      { ...SCHEMA.sql, note: "PRAGMA foreign_keys=ON on each connection" },
    ],
    implementation: [
      "internal/store/db.go — Open(cfg Config) (*sql.DB, error)",
      "Import _ modernc.org/sqlite",
      "DSN: file:path?cache=shared&_foreign_keys=on",
      "SetMaxOpenConns(1) for SQLite",
      "Ping on open to verify writable path",
      "Create parent directory for APP_DB_PATH if not exists",
    ],
    acceptance: [
      "Open() creates DB file at APP_DB_PATH",
      "Foreign keys enabled via connection pragma",
      "Ping succeeds on fresh database",
      "Parent directory created for /data/app.db",
      "Second Open reuses or errors clearly (document singleton)",
      "Works with :memory: in tests",
      "go test ./internal/store/... -run Open passes",
    ],
    files: ["internal/store/db.go", "internal/store/db_test.go"],
    tests: ["go test ./internal/store/... -run Open"],
  });

  leaf("P02-04", "P02", "Run golang-migrate on server startup", "backend", {
    depends_on: ["P02-03", "P01-02"],
    context:
      "Before accepting HTTP requests, server applies pending migrations programmatically. " +
      "Same migrations/ source as CLI. Failure to migrate is fatal (log.Fatal).",
    specs: [
      { ...SPEC.deployment, note: "Container entrypoint also migrates; server double-safe" },
      { ...SPEC.schema, note: "Schema must be current before any query" },
    ],
    stack: [
      { ...STACK.data, note: "migrate.NewWithDatabaseInstance with sqlite driver" },
      { ...STACK.deploy, note: "file://migrations embedded or filesystem" },
    ],
    implementation: [
      "internal/store/migrate.go — MigrateUp(db *sql.DB) error",
      "Use github.com/golang-migrate/migrate/v4",
      "source: file://migrations (relative to cwd or embed)",
      "database: sqlite3 instance from *sql.DB",
      "Call from main before router.Listen",
      "Log migration version on success",
    ],
    acceptance: [
      "Fresh DB gets schema_migrations version 1 on startup",
      "Server refuses to start if migration SQL fails",
      "Re-start is idempotent (no re-apply error)",
      "migrate.Up called before HTTP listener",
      "Logs include current migration version",
      "Works with temp DB in integration test",
      "Down migration not called on startup",
    ],
    files: ["internal/store/migrate.go"],
    tests: ["go test ./internal/store/... -run Migrate"],
  });

  leaf("P02-05", "P02", "Add JSON error response helper", "backend", {
    depends_on: ["P02-02"],
    context:
      "All API errors use consistent JSON shape per specs.html#api: { \"error\": { \"code\": \"...\", \"message\": \"...\" } }. " +
      "Handlers call WriteError instead of ad-hoc json.Encode.",
    specs: [
      { ...SPEC.api, note: "Error format and HTTP status code mapping" },
      { ...SPEC.auth, note: "401 unauthorized uses same error envelope" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/api/errors.go shared helper" },
      { ...STACK.quality, note: "Consistent API contract for web client" },
    ],
    implementation: [
      "type APIError struct { Code, Message string }",
      "WriteError(w, status, code, message string)",
      "Content-Type: application/json",
      "Common codes: unauthorized, validation_error, not_found, conflict",
      "WriteJSON(w, status, payload) helper for success responses",
      "Unit test marshals expected JSON shape",
    ],
    acceptance: [
      "WriteError produces {\"error\":{\"code\":\"...\",\"message\":\"...\"}}",
      "Content-Type header is application/json",
      "HTTP status code matches argument",
      "Works with chi ResponseWriter",
      "validation_error code used for 400 responses",
      "not_found for 404, conflict for 409",
      "go test ./internal/api/... -run Error passes",
    ],
    files: ["internal/api/errors.go", "internal/api/errors_test.go"],
    tests: ["go test ./internal/api/... -run Error"],
  });

  leaf("P02-06", "P02", "Add structured logging", "backend", {
    depends_on: ["P02-01"],
    context:
      "Use log/slog (stdlib) for structured JSON or text logs. Request middleware logs method, path, status, duration. " +
      "Poll engine and providers will use same logger with context attributes.",
    specs: [
      { ...SPEC.architecture, note: "Operational visibility for poll runs and errors" },
      { ...SPEC.domain, note: "Poll errors logged and stored in poll_runs.errors_json" },
    ],
    stack: [
      { ...STACK.backend, note: "slog default logger or injected *slog.Logger" },
      { ...STACK.quality, note: "No fmt.Printf in production paths" },
    ],
    implementation: [
      "internal/api/logging.go — RequestLogger middleware",
      "Extract chi middleware.RequestID into log context",
      "Log: method, path, status, duration_ms, remote_addr",
      "Configure slog level from env LOG_LEVEL default info",
      "main.go sets slog.Default on startup",
      "Skip logging for /healthz (optional noise reduction)",
    ],
    acceptance: [
      "Each HTTP request logs one line with status and duration",
      "Request ID present in log when chi RequestID set",
      "slog used instead of log.Printf in api package",
      "LOG_LEVEL=debug increases verbosity",
      "/healthz requests logged or skipped per config",
      "Logger injectable for tests",
      "No sensitive data (passwords, secrets) in logs",
    ],
    files: ["internal/api/logging.go"],
    tests: ["go test ./internal/api/... -run Log"],
  });

  leaf("P02-07", "P02", "Implement GET /healthz", "backend", {
    depends_on: ["P02-02"],
    context:
      "Liveness probe for Docker and load balancers. Returns 200 OK with minimal body, no auth required. " +
      "Does not check DB connectivity (readiness separate if needed post-MVP).",
    specs: [
      { ...SPEC.api, note: "Health endpoint outside auth middleware" },
      { ...SPEC.deployment, note: "Docker HEALTHCHECK can use /healthz via Next proxy or direct" },
    ],
    stack: [
      { ...STACK.deploy, note: "Container orchestration liveness" },
      { ...STACK.backend, note: "internal/api/health.go handler" },
    ],
    implementation: [
      "internal/api/health.go — HealthHandler(w,r)",
      "GET /healthz → 200, body OK or {\"status\":\"ok\"}",
      "Register on root router, not behind auth middleware",
      "No database ping (keep fast)",
      "Test with httptest.NewRecorder",
    ],
    acceptance: [
      "GET /healthz returns HTTP 200",
      "No session cookie required",
      "Response time < 10ms locally",
      "Works before migrations in test (if handler registered early)",
      "Not mounted under /api/v1 prefix",
      "go test ./internal/api/... -run Health passes",
      "Documented as liveness not readiness",
    ],
    files: ["internal/api/health.go", "internal/api/health_test.go"],
    tests: ["go test ./internal/api/... -run Health"],
  });

  leaf("P02-08", "P02", "Add API router registration scaffold", "backend", {
    depends_on: ["P02-02", "P02-05"],
    context:
      "Establish the chi sub-router pattern for /api/v1 before feature handlers land. " +
      "Each epic (P03–P15) registers its routes on this router. Final integration verification in P15-06. " +
      "Prevents route drift and duplicate mounts across handler packages.",
    specs: [
      { ...SPEC.api, note: "§7 base path /api/v1 and JSON error envelope" },
      { ...SPEC.architecture, note: "Go owns all /api/v1/* routes" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/api/router.go — MountAPI(r chi.Router)" },
      { ...STACK.quality, note: "Route list test or compile-time registration" },
    ],
    implementation: [
      "func MountAPI(r chi.Router) — subrouter /api/v1",
      "Apply JSON Content-Type middleware on API subrouter",
      "Apply error recovery middleware",
      "Export RegisterAuthRoutes, RegisterSettingsRoutes, … stubs called from MountAPI",
      "Document registration order: auth (public routes first), then auth middleware, then protected routes",
      "healthz stays on root router outside /api/v1",
      "404 handler returns { error: { code: NOT_FOUND, message } }",
    ],
    acceptance: [
      "MountAPI registers /api/v1 subrouter",
      "Unknown /api/v1 path returns JSON 404",
      "healthz not behind /api/v1 prefix",
      "Each Register*Routes function exists as stub or real mount",
      "No duplicate route registration on double MountAPI call",
      "go build ./... passes",
      "Router test: GET unknown path returns error envelope",
    ],
    files: ["internal/api/router.go", "internal/api/routes.go"],
    tests: ["go test ./internal/api/... -run Router"],
    relatedTasks: ["P15-06"],
  });

  // ─── P03 Auth ─────────────────────────────────────────────────────────────
  epic("P03", "Go authentication", "backend", {
    depends_on: ["P02-04"],
    context:
      "Auth is Go-only: alexedwards/scs sessions in SQLite sessions table, bcrypt password hashes in users, " +
      "release_ops_session cookie. No Next.js auth. Endpoints: POST login, POST logout, GET session. " +
      "Middleware protects all /api/v1/* except login and session. Bootstrap admin via env or seed-admin CLI.",
    specs: [
      { ...SPEC.auth, note: "Full auth flow, cookie flags, bootstrap rules" },
      { ...SPEC.api, note: "§7.1 auth endpoints JSON shapes" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "users and sessions table DDL" },
      { ...SCHEMA.html, note: "Session store BLOB format" },
    ],
    implementation: [
      "queries/users.sql — CreateUser, GetUserByEmail, CountUsers",
      "internal/api/auth/password.go — HashPassword, ComparePassword (bcrypt cost 12)",
      "internal/api/auth/session.go — scs.Manager with SQLite store",
      "Cookie name release_ops_session, HttpOnly, SameSite=Lax, Path=/",
      "POST /api/v1/auth/login, logout, GET session handlers",
      "Auth middleware on /api/v1 subrouter",
      "cmd/release-ops seed-admin CLI + bootstrap from BOOTSTRAP_ADMIN_* env",
    ],
    acceptance: [
      "Login sets release_ops_session cookie on success",
      "Invalid credentials return 401 with error envelope",
      "GET /api/v1/auth/session returns user:null when logged out",
      "Protected routes return 401 without valid session",
      "No POST /api/v1/auth/register endpoint",
      "Bootstrap creates admin when users table empty",
      "seed-admin CLI creates user when none exists",
    ],
  });

  leaf("P03-01", "P03", "Add users table repository", "backend", {
    depends_on: ["P01-04"],
    context:
      "sqlc queries for users table: id (UUID text), email unique, password_hash, timestamps. " +
      "Repository wraps generated queries for auth handlers and bootstrap.",
    specs: [
      { ...SPEC.auth, note: "Single admin user model; email is login identifier" },
      { ...SPEC.schema, note: "users.id TEXT PRIMARY KEY, email UNIQUE" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "users table definition" },
      { ...SCHEMA.html, note: "Column types and constraints" },
    ],
    implementation: [
      "queries/users.sql — name: CreateUser, GetUserByEmail, CountUsers",
      "CreateUser: INSERT id, email, password_hash, created_at, updated_at",
      "GetUserByEmail: SELECT * FROM users WHERE email = ?",
      "CountUsers: SELECT COUNT(*) FROM users",
      "internal/store/users.go — UserRepository struct",
      "Generate UUID v4 for new user id",
      "sqlc generate after adding queries",
    ],
    acceptance: [
      "CreateUser inserts row with valid UUID id",
      "GetUserByEmail returns sql.ErrNoRows equivalent for missing email",
      "CountUsers returns 0 on fresh DB",
      "Email uniqueness enforced by DB constraint",
      "Repository methods accept context.Context",
      "go test ./internal/store/... -run User passes",
      "sqlc models match users table columns",
    ],
    files: ["queries/users.sql", "internal/store/users.go"],
    tests: ["go test ./internal/store/... -run User"],
  });

  leaf("P03-02", "P03", "Add bcrypt password helpers", "backend", {
    context:
      "Passwords hashed with bcrypt before storage. Never log or return password_hash in API. " +
      "ComparePassword constant-time via bcrypt.CompareHashAndPassword.",
    specs: [
      { ...SPEC.auth, note: "bcrypt for password_hash column" },
      { ...SPEC.schema, note: "password_hash TEXT NOT NULL" },
    ],
    stack: [
      { ...STACK.backend, note: "golang.org/x/crypto/bcrypt" },
      { ...STACK.quality, note: "Unit tests with known hash" },
    ],
    implementation: [
      "internal/api/auth/password.go",
      "HashPassword(plain string) (string, error) — bcrypt.GenerateFromPassword cost 12",
      "ComparePassword(hash, plain string) error",
      "Reject empty password on hash",
      "Test round-trip hash and compare",
      "Test wrong password returns error",
    ],
    acceptance: [
      "HashPassword produces bcrypt hash starting with $2a$ or $2b$",
      "ComparePassword succeeds for correct password",
      "ComparePassword fails for wrong password",
      "Empty password rejected before hashing",
      "Cost factor >= 12",
      "go test ./internal/api/auth/... -run Password passes",
      "No password strings in test fixtures committed as plaintext in prod code paths",
    ],
    files: ["internal/api/auth/password.go", "internal/api/auth/password_test.go"],
    tests: ["go test ./internal/api/auth/... -run Password"],
  });

  leaf("P03-03", "P03", "Configure scs session manager with SQLite store", "backend", {
    depends_on: ["P03-01"],
    context:
      "alexedwards/scs v2 with SQLite store persisting to sessions table (token, data BLOB, expiry REAL). " +
      "Session manager injected into handlers and middleware.",
    specs: [
      { ...SPEC.auth, note: "Server-side sessions in SQLite" },
      { ...SPEC.schema, note: "sessions table for scs store" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "sessions(token, data, expiry) + idx_sessions_expiry" },
      { ...SCHEMA.html, note: "Session expiry index for cleanup" },
    ],
    implementation: [
      "internal/api/auth/session.go — NewSessionManager(db, cfg)",
      "github.com/alexedwards/scs/v2",
      "scs storedatabase or custom SQLite store adapter",
      "Lifetime: 7 days default, IdleTimeout configurable",
      "Store user_id in session after login",
      "Cleanup expired sessions on schedule or lazy",
    ],
    acceptance: [
      "Session data persists across server restart",
      "sessions table receives rows after login",
      "Expired sessions not accepted",
      "Session manager returns *scs.SessionManager",
      "Put user_id string in session on login",
      "Get user_id from session in middleware",
      "go test with in-memory sqlite for session round-trip",
    ],
    files: ["internal/api/auth/session.go"],
    tests: ["go test ./internal/api/auth/... -run Session"],
  });

  leaf("P03-04", "P03", "Configure release_ops_session cookie", "backend", {
    depends_on: ["P03-03"],
    context:
      "Cookie name release_ops_session per spec. HttpOnly always; SameSite=Lax; Path=/; Secure when APP_PUBLIC_URL is https.",
    specs: [
      { ...SPEC.auth, note: "Cookie name and security flags" },
      { ...SPEC.env, note: "APP_PUBLIC_URL determines Secure flag" },
    ],
    stack: [
      { ...STACK.backend, note: "scs Cookie struct configuration" },
      { ...STACK.deploy, note: "HTTPS termination at reverse proxy" },
    ],
    implementation: [
      "sessionManager.Cookie.Name = release_ops_session",
      "HttpOnly: true, Path: /, SameSite: Lax",
      "Secure: strings.HasPrefix(cfg.AppPublicURL, https://)",
      "Domain: empty (host-only cookie)",
      "Persist session on login via sessionManager.Put",
      "Renew token on activity if scs supports",
    ],
    acceptance: [
      "Set-Cookie header includes release_ops_session name",
      "HttpOnly flag present on login response",
      "SameSite=Lax in Set-Cookie",
      "Secure flag set when APP_PUBLIC_URL is https",
      "Secure omitted or false for http://localhost dev",
      "Cookie Path is /",
      "Browser devtools shows expected flags after login",
    ],
    files: ["internal/api/auth/session.go"],
    tests: ["go test ./internal/api/auth/... -run Cookie"],
  });

  leaf("P03-05", "P03", "Implement POST /api/v1/auth/login", "backend", {
    depends_on: ["P03-02", "P03-04"],
    context:
      "Request: { email, password }. Success 200: { user: { id, email } } + Set-Cookie. Failure 401: error envelope. " +
      "No user enumeration — same message for bad email vs bad password.",
    specs: [
      { ...SPEC.api, note: "POST /api/v1/auth/login request/response JSON" },
      { ...SPEC.auth, note: "Login flow and session creation" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/api/auth/handlers.go LoginHandler" },
      { ...STACK.quality, note: "httptest integration in P03-11" },
    ],
    implementation: [
      "Decode JSON { email, password }",
      "GetUserByEmail — on miss return 401 invalid_credentials",
      "ComparePassword — on fail return 401",
      "sessionManager.Put(ctx, userIDKey, user.ID)",
      "WriteJSON 200 { user: { id, email } }",
      "Register r.Post(/auth/login, LoginHandler) without auth middleware",
    ],
    acceptance: [
      "Valid credentials return 200 and user object",
      "Response sets release_ops_session cookie",
      "Invalid email returns 401",
      "Invalid password returns 401",
      "Malformed JSON returns 400",
      "password_hash never in response",
      "Same error message for wrong email and wrong password",
    ],
    files: ["internal/api/auth/handlers.go"],
    tests: ["go test ./internal/api/auth/... -run Login"],
  });

  leaf("P03-06", "P03", "Implement POST /api/v1/auth/logout", "backend", {
    depends_on: ["P03-03"],
    context:
      "Destroys server session and clears cookie. Requires valid session (auth middleware). Returns 200 with empty or { ok: true }.",
    specs: [
      { ...SPEC.api, note: "POST /api/v1/auth/logout — clears session" },
      { ...SPEC.auth, note: "Logout invalidates server-side session" },
    ],
    stack: [
      { ...STACK.backend, note: "sessionManager.Destroy or RenewToken" },
      { ...STACK.frontend, note: "Web calls via /api/go/api/v1/auth/logout P16" },
    ],
    implementation: [
      "LogoutHandler calls sessionManager.Destroy(ctx)",
      "Return 200 JSON {}",
      "Mount behind auth middleware OR allow without session (no-op)",
      "Clear cookie via scs Destroy response headers",
      "Register r.Post(/auth/logout, LogoutHandler)",
    ],
    acceptance: [
      "Logout returns 200 when session valid",
      "Subsequent GET /auth/session returns user:null",
      "Set-Cookie clears or expires session cookie",
      "Server-side session row removed or invalidated",
      "Logout without session returns 200 or 401 per spec",
      "go test logout flow passes",
      "Cookie no longer sent grants 401 on protected routes",
    ],
    files: ["internal/api/auth/handlers.go"],
    tests: ["go test ./internal/api/auth/... -run Logout"],
  });

  leaf("P03-07", "P03", "Implement GET /api/v1/auth/session", "backend", {
    depends_on: ["P03-03"],
    context:
      "Always returns 200. Logged out: { user: null }. Logged in: { user: { id, email } }. No auth middleware required — " +
      "used by Next.js middleware to check login state.",
    specs: [
      { ...SPEC.api, note: "GET /api/v1/auth/session response shapes" },
      { ...SPEC.ui, note: "Next middleware calls this via Go proxy" },
    ],
    stack: [
      { ...STACK.frontend, note: "useSession hook consumes this endpoint P17" },
      { ...STACK.backend, note: "SessionHandler reads user_id from scs" },
    ],
    implementation: [
      "SessionHandler: get user_id from session",
      "If missing: WriteJSON 200 { user: null }",
      "If present: load user by id, return { user: { id, email } }",
      "Register without auth middleware",
      "Handle deleted user edge case → user:null",
    ],
    acceptance: [
      "Unauthenticated request returns 200 { user: null }",
      "Authenticated request returns 200 with id and email",
      "Never returns 401 for this endpoint",
      "password_hash never exposed",
      "Works with session cookie from login",
      "Invalid session token returns user:null",
      "go test ./internal/api/auth/... -run SessionHandler passes",
    ],
    files: ["internal/api/auth/handlers.go"],
    tests: ["go test ./internal/api/auth/... -run SessionHandler"],
  });

  leaf("P03-08", "P03", "Add auth middleware for protected routes", "backend", {
    depends_on: ["P03-03"],
    context:
      "Middleware checks scs session for user_id. Missing → 401 unauthorized. Apply to /api/v1 routes except " +
      "POST /auth/login and GET /auth/session. Mount via chi Group or Route with middleware.",
    specs: [
      { ...SPEC.auth, note: "All /api/v1/* require session except login and session" },
      { ...SPEC.api, note: "401 error envelope on missing auth" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/api/auth/middleware.go RequireAuth" },
      { ...STACK.layout, note: "Router wiring in internal/api/router.go" },
    ],
    implementation: [
      "RequireAuth(sessionManager) func(http.Handler) http.Handler",
      "Read user_id from session; if empty WriteError 401",
      "Store user in request context for handlers",
      "Apply to protected route group in router.go",
      "Exclude: /auth/login (POST), /auth/session (GET)",
      "Include: /auth/logout (POST)",
    ],
    acceptance: [
      "GET /api/v1/settings without cookie returns 401",
      "GET /api/v1/auth/session without cookie returns 200",
      "POST /api/v1/auth/login without cookie allowed",
      "Valid session passes through to next handler",
      "User available in context in downstream handlers",
      "401 uses error code unauthorized",
      "All future API handlers inherit protection automatically",
    ],
    files: ["internal/api/auth/middleware.go", "internal/api/router.go"],
    tests: ["go test ./internal/api/auth/... -run RequireAuth"],
  });

  leaf("P03-09", "P03", "Add seed-admin CLI command", "backend", {
    depends_on: ["P03-01", "P03-02"],
    context:
      "CLI binary cmd/release-ops (or subcommand on server) for ops: release-ops seed-admin --email x --password y. " +
      "Creates admin only when users table is empty; exits 0 with message otherwise.",
    specs: [
      { ...SPEC.auth, note: "CLI seed when no users exist" },
      { ...SPEC.deployment, note: "Alternative to BOOTSTRAP_ADMIN env" },
    ],
    stack: [
      { ...STACK.backend, note: "cmd/release-ops/main.go separate from cmd/server" },
      { ...STACK.deploy, note: "Document docker exec seed-admin usage" },
    ],
    implementation: [
      "cmd/release-ops/main.go with cobra or flag package",
      "Subcommand seed-admin --email --password flags",
      "Open DB, run migrations, CountUsers",
      "If count > 0: log and exit 0 without creating",
      "Hash password, CreateUser with new UUID",
      "Print success message to stdout",
    ],
    acceptance: [
      "seed-admin creates user when DB has zero users",
      "Second invocation does not create duplicate",
      "Requires --email and --password flags",
      "Exits non-zero on DB connection failure",
      "Password stored as bcrypt hash",
      "Works against APP_DB_PATH env",
      "go build ./cmd/release-ops succeeds",
    ],
    files: ["cmd/release-ops/main.go"],
    tests: ["go build ./cmd/release-ops"],
  });

  leaf("P03-10", "P03", "Bootstrap admin from env on first start", "backend", {
    depends_on: ["P03-09"],
    context:
      "On server startup after migrations: if users empty AND BOOTSTRAP_ADMIN_EMAIL and BOOTSTRAP_ADMIN_PASSWORD set, " +
      "create admin user automatically. Log info message; never log password.",
    specs: [
      { ...SPEC.auth, note: "BOOTSTRAP_ADMIN_EMAIL/PASSWORD env vars" },
      { ...SPEC.env, note: "Bootstrap vars optional" },
    ],
    stack: [
      { ...STACK.deploy, note: "docker-compose sets bootstrap vars for first run" },
      { ...STACK.backend, note: "internal/api/auth/bootstrap.go" },
    ],
    implementation: [
      "internal/api/auth/bootstrap.go — BootstrapAdmin(ctx, repo, cfg)",
      "Called from main.go after migrate, before Listen",
      "CountUsers == 0 && email && password non-empty → CreateUser",
      "Skip silently if users exist",
      "Skip if env vars empty (no error)",
      "Log: Bootstrap admin created for email@example.com",
    ],
    acceptance: [
      "First start with env vars creates exactly one user",
      "Second start does not create another user",
      "Missing env vars skips bootstrap without error",
      "Password never appears in logs",
      "Bootstrap runs after migrations",
      "Same user usable for POST /auth/login",
      "go test BootstrapAdmin with temp DB passes",
    ],
    files: ["internal/api/auth/bootstrap.go", "cmd/server/main.go"],
    tests: ["go test ./internal/api/auth/... -run Bootstrap"],
  });

  leaf("P03-11", "P03", "Add auth handler integration tests", "backend", {
    depends_on: ["P03-05", "P03-07"],
    context:
      "End-to-end httptest: login → session check → access protected route → logout → session null. " +
      "Uses real router, in-memory SQLite, and session cookies.",
    specs: [
      { ...SPEC.auth, note: "Full login/session/logout flow" },
      { ...SPEC.api, note: "Cookie forwarding behavior matches Next proxy needs" },
    ],
    stack: [
      { ...STACK.quality, note: "httptest + real migrations on :memory:" },
      { ...STACK.backend, note: "internal/api/auth/handlers_test.go" },
    ],
    implementation: [
      "TestMain or helper: setup test DB, migrate, router, seed user",
      "TestLoginSetsCookieAndSession",
      "TestSessionReturnsUserWhenLoggedIn",
      "TestProtectedRouteRequiresAuth",
      "TestLogoutClearsSession",
      "Use httptest.Server and jar for cookies",
    ],
    acceptance: [
      "Login → GET session returns same user id",
      "Logout → GET session returns user:null",
      "Protected route 401 before login, 200 after",
      "Tests use isolated :memory: database",
      "No race conditions (t.Parallel false or unique DB)",
      "go test ./internal/api/auth/... passes all",
      "CI runs without network",
    ],
    files: ["internal/api/auth/handlers_test.go"],
    tests: ["go test ./internal/api/auth/..."],
  });

  // ─── P04 Crypto ───────────────────────────────────────────────────────────
  epic("P04", "Credential encryption", "backend", {
    depends_on: ["P02-01"],
    context:
      "Integration secrets and Shoutrrr URLs stored encrypted in SQLite using AES-256-GCM. Key from APP_ENCRYPTION_KEY " +
      "(64 hex chars = 32 bytes). Payload JSON marshalled per integration kind before encrypt. API never returns decrypted secrets.",
    specs: [
      { ...SPEC.schema, note: "integrations.encrypted_payload, notification_targets.shoutrrr_url_encrypted" },
      { ...SPEC.api, note: "hasSecret boolean; one-time secret on create" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/crypto package" },
      { ...STACK.data, note: "Encryption at rest in SQLite" },
    ],
    implementation: [
      "internal/crypto/aesgcm.go — Encrypt(plaintext) → base64 or JSON wrapper v1",
      "Decrypt(ciphertext) → plaintext bytes",
      "internal/crypto/key.go — ParseKey(hex string) validates 64 hex",
      "internal/crypto/payload.go — MarshalIntegrationSecret(kind, secret)",
      "Nonce prepended or embedded per AES-GCM standard",
      "Validate key on config load startup",
    ],
    acceptance: [
      "Encrypt/Decrypt round-trip preserves plaintext",
      "Wrong key fails decrypt with clear error",
      "APP_ENCRYPTION_KEY invalid length rejected at startup",
      "Ciphertext differs for same plaintext (random nonce)",
      "Integration API never returns decrypted secret",
      "Payload JSON includes kind-specific fields before encrypt",
      "go test ./internal/crypto/... passes",
    ],
  });

  leaf("P04-01", "P04", "Implement AES-256-GCM encrypt and decrypt", "backend", {
    context:
      "Use crypto/aes + cipher.NewGCM. 32-byte key, 12-byte nonce, authenticate ciphertext. " +
      "Store as base64(nonce|ciphertext) or JSON {v:1,nonce,ciphertext} for future versioning.",
    specs: [
      { ...SPEC.schema, note: "encrypted_payload TEXT stores opaque blob" },
      { ...SPEC.providers, note: "Decrypted at provider factory time only" },
    ],
    stack: [
      { ...STACK.backend, note: "stdlib crypto/aes, crypto/cipher" },
      { ...STACK.quality, note: "Property tests for round-trip" },
    ],
    implementation: [
      "type Cipher struct { aead cipher.AEAD }",
      "NewCipher(key []byte) validates len(key)==32",
      "Encrypt: random nonce, Seal, encode base64",
      "Decrypt: decode base64, Open, verify auth tag",
      "Reject empty plaintext on encrypt",
      "Unit tests: round-trip, tampered ciphertext fails",
    ],
    acceptance: [
      "32-byte key required",
      "Encrypt output is base64 string",
      "Decrypt(Encrypt(p)) == p for arbitrary bytes",
      "Tampered ciphertext returns error on decrypt",
      "Different nonces for repeated plaintext",
      "No key material in error messages",
      "go test ./internal/crypto/... -run AES passes",
    ],
    files: ["internal/crypto/aesgcm.go", "internal/crypto/aesgcm_test.go"],
    tests: ["go test ./internal/crypto/..."],
  });

  leaf("P04-02", "P04", "Validate APP_ENCRYPTION_KEY on startup", "backend", {
    depends_on: ["P04-01"],
    context:
      "Config loader calls crypto.ParseKey on APP_ENCRYPTION_KEY. Must be exactly 64 hexadecimal characters. " +
      "Fail server startup if missing or malformed.",
    specs: [
      { ...SPEC.env, note: "APP_ENCRYPTION_KEY required 64 hex chars" },
      { ...SPEC.deployment, note: "Generate with openssl rand -hex 32" },
    ],
    stack: [
      { ...STACK.deploy, note: "Document key generation in getting-started" },
      { ...STACK.backend, note: "internal/crypto/key.go" },
    ],
    implementation: [
      "ParseKey(hex string) ([]byte, error)",
      "encoding/hex.DecodeString after length check == 64",
      "Reject non-hex characters",
      "Wire into config.Load() — required field",
      "Return Config.EncryptionKey []byte",
      "Test: valid key, short key, invalid chars",
    ],
    acceptance: [
      "64-char hex string parses to 32 bytes",
      "63 or 65 char strings rejected",
      "Non-hex characters rejected",
      "Empty string rejected",
      "config.Load fails without APP_ENCRYPTION_KEY",
      "Parsed key usable by NewCipher",
      "go test ./internal/crypto/... -run ParseKey passes",
    ],
    files: ["internal/crypto/key.go", "internal/config/config.go"],
    tests: ["go test ./internal/crypto/... -run ParseKey"],
  });

  leaf("P04-03", "P04", "Add integration payload marshal helpers", "backend", {
    depends_on: ["P04-01"],
    context:
      "Before encrypt, marshal secret to JSON per integration kind: e.g. {token}, {pat}, {apiKey}, {email,apiKey}. " +
      "Decrypt path unmarshals to typed struct for provider factories.",
    specs: [
      { ...SPEC.schema, note: "integrations.kind determines payload shape" },
      { ...SPEC.providers, note: "§6 credential fields per source/ticket kind" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/crypto/payload.go" },
      { ...STACK.data, note: "JSON in encrypted_payload column" },
    ],
    implementation: [
      "type IntegrationPayload struct with kind-specific fields",
      "MarshalSecret(kind string, secret map[string]string) ([]byte, error)",
      "UnmarshalSecret(kind string, data []byte) (map[string]string, error)",
      "Validate required keys per kind: github→token, jira→email+apiToken",
      "EncryptJSON(kind, fields) helper combining marshal+encrypt",
      "DecryptJSON(kind, blob) for handler read path",
    ],
    acceptance: [
      "GitHub payload JSON contains token field",
      "Jira payload contains email and apiToken",
      "Unknown kind returns validation error",
      "Round-trip Marshal→Encrypt→Decrypt→Unmarshal preserves fields",
      "Empty secret rejected for kinds requiring token",
      "Linear payload uses apiKey field",
      "go test ./internal/crypto/... -run Payload passes",
    ],
    files: ["internal/crypto/payload.go", "internal/crypto/payload_test.go"],
    tests: ["go test ./internal/crypto/... -run Payload"],
  });

  // ─── P05 Store layer ──────────────────────────────────────────────────────
  epic("P05", "Go store layer (sqlc repositories)", "db", {
    depends_on: ["P01-04", "P04-01"],
    context:
      "sqlc queries for all domain tables plus repository wrapper exposing typed methods for API handlers and poll engine. " +
      "Handles UUID generation, timestamp UTC ISO8601, and encrypted column pass-through.",
    specs: [
      { ...SPEC.schema, note: "All tables §4" },
      { ...SPEC.domain, note: "Poll state fields on monitored_repos" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "Full DDL for query authoring" },
      { ...SCHEMA.html, note: "FK relationships for JOIN queries" },
    ],
    implementation: [
      "queries/app_settings.sql — GetSettings, UpdatePollInterval",
      "queries/integrations.sql — CRUD + ListByKind",
      "queries/ticket_projects.sql — CRUD + ListByIntegration",
      "queries/monitored_repos.sql — CRUD + ListEnabled + UpdatePollState",
      "queries/notification_targets.sql + repo_notification joins",
      "queries/poll_runs.sql — InsertRun, FinishRun, InsertEvent, ListEvents",
      "internal/store/repository.go — Repository facade",
    ],
    acceptance: [
      "All CRUD operations compile via sqlc generate",
      "Repository used by at least one handler test",
      "ListEnabled repos filters enabled=1",
      "UpdatePollState sets last_known_tag, open_ticket_* fields",
      "Foreign key violations surface as errors",
      "Timestamps stored as UTC ISO8601 strings",
      "go test ./internal/store/... passes",
    ],
  });

  leaf("P05-01", "P05", "Add sqlc queries for app_settings", "db", {
    context:
      "Singleton row id=1: poll_interval_minutes (min 5, default 360), updated_at. Get for scheduler; Patch for settings API.",
    specs: [
      { ...SPEC.schema, note: "app_settings id CHECK (id = 1)" },
      { ...SPEC.api, note: "GET/PATCH /api/v1/settings pollIntervalMinutes" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "app_settings table" },
      { ...SCHEMA.html, note: "poll_interval_minutes CHECK >= 5" },
    ],
    implementation: [
      "queries/app_settings.sql — GetAppSettings :one",
      "UpdatePollInterval :exec SET poll_interval_minutes, updated_at",
      "Seed INSERT in migration or separate seed migration P06-03",
      "sqlc generate models AppSetting",
      "Repository method GetSettings(ctx) (*AppSetting, error)",
      "Validate min 5 in repository or handler layer",
    ],
    acceptance: [
      "GetAppSettings returns row id=1",
      "UpdatePollInterval persists new value",
      "CHECK constraint rejects poll_interval_minutes < 5",
      "updated_at changes on update",
      "sqlc model fields match columns",
      "Repository wraps generated Queries",
      "go test update and get round-trip",
    ],
    files: ["queries/app_settings.sql", "internal/store/settings.go"],
    tests: ["go test ./internal/store/... -run Settings"],
  });

  leaf("P05-02", "P05", "Add sqlc queries for integrations", "db", {
    context:
      "integrations: id, kind (8 values), name, base_url nullable per CHECK, encrypted_payload, timestamps. " +
      "CRUD + list all + filter by kind. Never SELECT decrypted payload to API — handlers add hasSecret.",
    specs: [
      { ...SPEC.schema, note: "integrations.kind CHECK and base_url rules" },
      { ...SPEC.api, note: "Integration list/create JSON without secrets" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "integrations table with kind CHECK" },
      { ...SCHEMA.html, note: "base_url NULL rules per kind" },
    ],
    implementation: [
      "queries/integrations.sql — Create, Get, List, Update, Delete",
      "ListIntegrations, ListIntegrationsByKind",
      "CreateIntegration with all columns",
      "UpdateIntegrationName, UpdateIntegrationSecret (encrypted_payload)",
      "DeleteIntegration by id",
      "CountReferences for delete guard (repos, ticket_projects) — may defer to handler",
    ],
    acceptance: [
      "Create stores encrypted_payload blob",
      "List returns all rows without decrypting",
      "kind CHECK enforced: github requires base_url NULL",
      "gitlab insert fails without base_url",
      "Update name only leaves encrypted_payload unchanged",
      "Delete removes row when no FK references",
      "sqlc generates Integration model",
    ],
    files: ["queries/integrations.sql", "internal/store/integrations.go"],
    tests: ["go test ./internal/store/... -run Integration"],
  });

  leaf("P05-03", "P05", "Add sqlc queries for ticket_projects", "db", {
    context:
      "ticket_projects: integration_id FK, external_project_id, name, create_config JSON, status_mapping JSON, " +
      "on_open_ticket_policy (supersede|merge|skip_if_open). UNIQUE(integration_id, external_project_id).",
    specs: [
      { ...SPEC.ticketProjects, note: "create_config and status_mapping schemas per provider" },
      { ...SPEC.schema, note: "ticket_projects table §4.3" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "ticket_projects DDL" },
      { ...SCHEMA.html, note: "JSON validity CHECK" },
    ],
    implementation: [
      "queries/ticket_projects.sql — CRUD operations",
      "ListTicketProjects, ListByIntegrationID",
      "Create with create_config and status_mapping as JSON strings",
      "Update mapping, policy, create_config, name",
      "Delete by id",
      "GetTicketProject :one",
    ],
    acceptance: [
      "Create stores valid JSON in create_config and status_mapping",
      "UNIQUE violation on duplicate integration_id+external_project_id",
      "ListByIntegrationID filters correctly",
      "on_open_ticket_policy defaults to supersede",
      "FK to integrations enforced",
      "Update preserves id and integration_id",
      "go test CRUD round-trip",
    ],
    files: ["queries/ticket_projects.sql", "internal/store/ticket_projects.go"],
    tests: ["go test ./internal/store/... -run TicketProject"],
  });

  leaf("P05-04", "P05", "Add sqlc queries for monitored_repos", "db", {
    context:
      "monitored_repos: source_kind, project_path, enabled, source_integration_id nullable, ticket_project_id FK, " +
      "open_ticket_external_id, open_ticket_tag, last_known_tag, last_polled_at, last_error. Poll engine updates state fields.",
    specs: [
      { ...SPEC.domain, note: "Per-repo poll state and open ticket tracking" },
      { ...SPEC.schema, note: "monitored_repos §4.4" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "monitored_repos columns" },
      { ...SCHEMA.html, note: "UNIQUE(source_kind, project_path)" },
    ],
    implementation: [
      "queries/monitored_repos.sql — CRUD",
      "ListMonitoredRepos, ListEnabledRepos",
      "UpdatePollState: last_known_tag, last_polled_at, last_error, open_ticket_*",
      "Create with notification target links via separate join queries",
      "SetEnabled toggle",
      "GetMonitoredRepoWithRelations for poll engine",
    ],
    acceptance: [
      "ListEnabledRepos returns only enabled=1",
      "UpdatePollState clears last_error on success path",
      "UNIQUE on source_kind+project_path enforced",
      "source_integration_id nullable for github/codeberg",
      "ticket_project_id FK required on create",
      "open_ticket fields updatable by poll engine",
      "go test poll state update",
    ],
    files: ["queries/monitored_repos.sql", "internal/store/repos.go"],
    tests: ["go test ./internal/store/... -run MonitoredRepo"],
  });

  leaf("P05-05", "P05", "Add sqlc queries for notification targets", "db", {
    context:
      "notification_targets: name, shoutrrr_url_encrypted, events_json default [create,error,supersede], enabled. " +
      "monitored_repo_notifications join for per-repo routing.",
    specs: [
      { ...SPEC.notifications, note: "Shoutrrr URL encrypted; events filter" },
      { ...SPEC.schema, note: "notification_targets + join table §4.5" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "notification_targets, monitored_repo_notifications" },
      { ...SCHEMA.html, note: "events_json CHECK json_valid" },
    ],
    implementation: [
      "queries/notification_targets.sql — CRUD targets",
      "LinkRepoNotification, UnlinkRepoNotification, ListTargetsForRepo",
      "ListEnabledNotificationTargets",
      "Create with encrypted URL blob",
      "Update events_json, enabled, name, optional URL",
      "Delete cascades join rows",
    ],
    acceptance: [
      "Create stores shoutrrr_url_encrypted opaque blob",
      "events_json defaults include create, error, supersede",
      "LinkRepoNotification creates join row",
      "ListTargetsForRepo returns linked targets only",
      "Empty join means global targets apply (poll layer)",
      "enabled=0 excluded from send list",
      "go test target CRUD and linking",
    ],
    files: ["queries/notification_targets.sql", "internal/store/notifications.go"],
    tests: ["go test ./internal/store/... -run Notification"],
  });

  leaf("P05-06", "P05", "Add sqlc queries for poll_runs and events", "db", {
    context:
      "poll_runs: status running|success|partial|failed, counters, errors_json. poll_run_events: action enum " +
      "baseline|skip|create|supersede|merge|skip_open|error per repo.",
    specs: [
      { ...SPEC.domain, note: "Poll run lifecycle and event actions" },
      { ...SPEC.schema, note: "poll_runs, poll_run_events §4.6" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "poll_run_events.action CHECK" },
      { ...SCHEMA.html, note: "poll_runs status enum" },
    ],
    implementation: [
      "queries/poll_runs.sql — CreatePollRun, FinishPollRun",
      "InsertPollRunEvent, ListPollRunEventsByRunID",
      "ListPollRuns paginated ORDER BY started_at DESC",
      "GetPollRunWithEvents :one + :many",
      "Increment counters on FinishPollRun",
      "errors_json as JSON array string",
    ],
    acceptance: [
      "CreatePollRun sets status running",
      "FinishPollRun sets finished_at and final status",
      "InsertPollRunEvent records action per repo",
      "ListPollRuns supports LIMIT/OFFSET",
      "errors_json valid JSON array",
      "FK poll_run_id on events enforced",
      "go test run lifecycle",
    ],
    files: ["queries/poll_runs.sql", "internal/store/poll_runs.go"],
    tests: ["go test ./internal/store/... -run PollRun"],
  });

  leaf("P05-07", "P05", "Add repository wrapper with domain types", "db", {
    depends_on: ["P05-01", "P05-02", "P05-03", "P05-04", "P05-05", "P05-06"],
    context:
      "Single Repository struct embedding *db.Queries with helper methods converting sqlc types to domain structs. " +
      "Injected into api handlers and poll package. Centralizes UUID and time helpers.",
    specs: [
      { ...SPEC.architecture, note: "Store layer between handlers and sqlc" },
      { ...SPEC.providers, note: "Repository loads integration rows for factory" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/store/repository.go" },
      { ...STACK.data, note: "Transaction support optional for multi-table creates" },
    ],
    implementation: [
      "type Repository struct { q *db.Queries; db *sql.DB }",
      "NewRepository(db) with db.New(db)",
      "Methods delegate to generated queries with domain mapping",
      "WithTx(ctx) for repo create + notification links",
      "NewID() UUID v4 helper",
      "NowUTC() ISO8601 timestamp helper",
      "Export all store sub-repositories or flat methods",
    ],
    acceptance: [
      "Repository constructible from *sql.DB",
      "All P05-01..06 operations accessible via Repository",
      "Handlers import store.Repository not raw sqlc",
      "UUIDs generated consistently",
      "Timestamps in UTC Z format",
      "WithTx rolls back on error for repo+links create",
      "go build ./internal/store/... succeeds",
    ],
    files: ["internal/store/repository.go"],
    tests: ["go test ./internal/store/..."],
  });

  // ─── P06 Settings API ─────────────────────────────────────────────────────
  epic("P06", "Settings REST API", "backend", {
    depends_on: ["P05-07", "P03-08"],
    context:
      "Global app settings exposed via GET/PATCH /api/v1/settings. MVP scope: poll_interval_minutes only. " +
      "Requires auth. Validates minimum 5 minutes. Scheduler (P14) reads this value.",
    specs: [
      { ...SPEC.api, note: "§7.2 settings endpoints" },
      { ...SPEC.domain, note: "Poll interval from app_settings" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "app_settings singleton" },
      { ...SCHEMA.html, note: "poll_interval_minutes CHECK >= 5" },
    ],
    implementation: [
      "internal/api/settings.go — GetSettings, PatchSettings handlers",
      "GET /api/v1/settings → { pollIntervalMinutes: 360 }",
      "PATCH body { pollIntervalMinutes } camelCase JSON",
      "Validate >= 5, return 400 validation_error",
      "Seed app_settings id=1 on migrate",
      "Register routes behind auth middleware",
    ],
    acceptance: [
      "GET /api/v1/settings returns pollIntervalMinutes",
      "PATCH updates value in database",
      "PATCH with value < 5 returns 400",
      "Unauthenticated requests return 401",
      "Default 360 when row seeded",
      "Response uses camelCase JSON keys",
      "Settings API tests pass",
    ],
  });

  leaf("P06-01", "P06", "Implement GET /api/v1/settings", "backend", {
    context:
      "Authenticated GET returns current poll_interval_minutes as pollIntervalMinutes. Read from app_settings id=1 via repository.",
    specs: [
      { ...SPEC.api, note: "GET /api/v1/settings response shape" },
      { ...SPEC.schema, note: "app_settings.poll_interval_minutes" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/api/settings.go" },
      { ...STACK.quality, note: "httptest in P06-04" },
    ],
    implementation: [
      "GetSettingsHandler(repo Repository)",
      "repo.GetSettings(ctx) → poll_interval_minutes",
      "JSON: { pollIntervalMinutes: int }",
      "404 or 500 if settings row missing (should not happen after seed)",
      "r.Get(/settings, handler) on protected router",
    ],
    acceptance: [
      "Returns 200 with pollIntervalMinutes integer",
      "Requires auth cookie",
      "Value matches database",
      "Content-Type application/json",
      "No extra fields in response",
      "Handles missing row with 500 internal_error",
      "Camel case JSON key pollIntervalMinutes",
    ],
    files: ["internal/api/settings.go"],
    tests: ["go test ./internal/api/... -run GetSettings"],
  });

  leaf("P06-02", "P06", "Implement PATCH /api/v1/settings", "backend", {
    context:
      "Partial update of poll interval. Body: { pollIntervalMinutes: number }. Validate >= 5. Update updated_at. Return updated object.",
    specs: [
      { ...SPEC.api, note: "PATCH /api/v1/settings request body" },
      { ...SPEC.domain, note: "Scheduler picks up new interval on next tick" },
    ],
    stack: [
      { ...STACK.backend, note: "Decode JSON with json.Decoder" },
      { ...STACK.quality, note: "Validation before DB write" },
    ],
    implementation: [
      "PatchSettingsHandler decodes { pollIntervalMinutes }",
      "Reject if < 5 with validation_error",
      "repo.UpdatePollInterval(ctx, minutes)",
      "Return 200 with same shape as GET",
      "Reject unknown JSON fields or ignore per spec",
      "Log setting change at info level",
    ],
    acceptance: [
      "Valid PATCH persists and returns new value",
      "Value 4 returns 400 validation_error",
      "Value 5 accepted (minimum)",
      "Missing field returns 400",
      "Unauthenticated returns 401",
      "updated_at changes in DB",
      "Subsequent GET returns updated value",
    ],
    files: ["internal/api/settings.go"],
    tests: ["go test ./internal/api/... -run PatchSettings"],
  });

  leaf("P06-03", "P06", "Seed app_settings singleton on migrate", "db", {
    depends_on: ["P05-01"],
    context:
      "Ensure row id=1 exists with poll_interval_minutes=360 after first migration. Either INSERT in 000001_init.up.sql " +
      "or dedicated seed in migrate.go after Up.",
    specs: [
      { ...SPEC.schema, note: "app_settings id CHECK (id = 1) singleton" },
      { ...SPEC.api, note: "Default 360 minutes = 6 hours" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "DEFAULT 360 on poll_interval_minutes" },
      { ...SCHEMA.html, note: "Singleton pattern" },
    ],
    implementation: [
      "Add to migrations/000001_init.up.sql: INSERT OR IGNORE INTO app_settings",
      "Values: id=1, poll_interval_minutes=360, updated_at=now",
      "Or MigrateUp hook: seed if COUNT app_settings = 0",
      "Use UTC ISO8601 for updated_at",
      "Idempotent on re-migrate",
    ],
    acceptance: [
      "Fresh migrate leaves exactly one app_settings row",
      "poll_interval_minutes = 360 by default",
      "id = 1",
      "Re-running migrate does not duplicate row",
      "GET /api/v1/settings works without manual seed",
      "updated_at is valid ISO8601",
      "go test migrate includes settings row check",
    ],
    files: ["migrations/000001_init.up.sql"],
    tests: ["go test ./internal/store/... -run SettingsSeed"],
  });

  leaf("P06-04", "P06", "Add settings API tests", "backend", {
    depends_on: ["P06-01", "P06-02"],
    context:
      "Integration tests: login, GET settings, PATCH valid/invalid, verify DB. Uses httptest and test repository.",
    specs: [
      { ...SPEC.api, note: "Settings endpoint contract" },
      { ...SPEC.mvp, note: "API tests required for MVP" },
    ],
    stack: [
      { ...STACK.quality, note: "go test ./internal/api/... -run Settings" },
      { ...STACK.backend, note: "handlers_test pattern from auth" },
    ],
    implementation: [
      "TestSettingsGetRequiresAuth",
      "TestSettingsGetReturnsDefault360",
      "TestSettingsPatchUpdatesValue",
      "TestSettingsPatchRejectsBelowMin",
      "Helper: authenticatedClient(t)",
      "Use migrated :memory: DB",
    ],
    acceptance: [
      "All settings tests pass",
      "GET without auth → 401",
      "PATCH 10 → GET returns 10",
      "PATCH 3 → 400",
      "Tests isolated per run",
      "No external network",
      "CI green on go test ./internal/api/... -run Settings",
    ],
    files: ["internal/api/settings_test.go"],
    tests: ["go test ./internal/api/... -run Settings"],
  });

  // ─── P07 Integrations API ─────────────────────────────────────────────────
  epic("P07", "Integrations REST API", "backend", {
    depends_on: ["P05-07", "P04-03", "P03-08"],
    context:
      "CRUD for integrations (8 kinds: 5 source + 3 ticket). Secrets encrypted on write; API returns hasSecret never plaintext. " +
      "base_url rules per kind. DELETE 409 when referenced. POST /integrations/{id}/test wired in P11/P12.",
    specs: [
      { ...SPEC.api, note: "§7.3 integrations endpoints" },
      { ...SPEC.schema, note: "integrations table kind CHECK" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "integrations DDL and constraints" },
      { ...SCHEMA.html, note: "base_url per kind matrix" },
    ],
    implementation: [
      "internal/api/integrations/model.go — kind validation",
      "GET /api/v1/integrations — list with hasSecret",
      "POST — create with secret encryption",
      "PATCH /api/v1/integrations/{id} — name, optional secret",
      "DELETE — 409 if referenced",
      "JSON camelCase: baseUrl, hasSecret",
    ],
    acceptance: [
      "List never includes secret or encrypted_payload",
      "Create encrypts secret before store",
      "hasSecret: true when encrypted_payload non-empty",
      "Invalid kind returns 400",
      "github create rejects baseUrl",
      "gitlab create requires baseUrl",
      "DELETE returns 409 when repos reference integration",
    ],
  });

  leaf("P07-01", "P07", "Add integration domain model and kind validation", "backend", {
    context:
      "Validate kind enum and base_url presence per specs: github/codeberg/linear NULL base_url; gitlab/gitea/forgejo/phasical/jira require base_url.",
    specs: [
      { ...SPEC.schema, note: "integrations CHECK constraint" },
      { ...SPEC.providers, note: "8 integration kinds" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "kind IN (...) CHECK" },
      { ...SCHEMA.html, note: "base_url rules table" },
    ],
    implementation: [
      "internal/api/integrations/model.go",
      "const kinds: github, gitlab, gitea, forgejo, codeberg, phasical, jira, linear",
      "ValidateCreate(kind, baseUrl) error",
      "IsSourceKind(kind), IsTicketKind(kind) helpers",
      "Normalize baseUrl: trim trailing slash",
      "Map DB snake_case to JSON camelCase in responses",
    ],
    acceptance: [
      "github with baseUrl fails validation",
      "gitlab without baseUrl fails validation",
      "linear allows null baseUrl",
      "Invalid kind string rejected",
      "baseUrl trimmed of trailing slash",
      "IsSourceKind true for 5 source kinds",
      "IsTicketKind true for phasical, jira, linear",
    ],
    files: ["internal/api/integrations/model.go", "internal/api/integrations/model_test.go"],
    tests: ["go test ./internal/api/integrations/... -run Validate"],
  });

  leaf("P07-02", "P07", "Implement GET /api/v1/integrations", "backend", {
    depends_on: ["P07-01"],
    context:
      "List all integrations: { id, kind, name, baseUrl, hasSecret, createdAt, updatedAt }. Never decrypt or expose secret.",
    specs: [
      { ...SPEC.api, note: "Integration list response fields" },
      { ...SPEC.auth, note: "Requires session" },
    ],
    stack: [
      { ...STACK.backend, note: "handlers.go ListIntegrations" },
      { ...STACK.quality, note: "Test hasSecret true/false" },
    ],
    implementation: [
      "ListIntegrationsHandler(repo)",
      "Map each row to IntegrationResponse DTO",
      "hasSecret: len(encrypted_payload) > 0",
      "Omit encrypted_payload from JSON tags",
      "Sort by name or created_at",
      "r.Get(/integrations, handler)",
    ],
    acceptance: [
      "Returns 200 array of integrations",
      "Each item has id, kind, name, hasSecret",
      "No secret field in JSON",
      "hasSecret false for empty payload edge case",
      "baseUrl null for github in JSON",
      "Requires authentication",
      "Empty list returns []",
    ],
    files: ["internal/api/integrations/handlers.go"],
    tests: ["go test ./internal/api/integrations/... -run List"],
  });

  leaf("P07-03", "P07", "Implement POST /api/v1/integrations", "backend", {
    depends_on: ["P07-01"],
    context:
      "Create: { kind, name, baseUrl?, secret }. Encrypt secret via crypto.EncryptJSON. Return created object without secret.",
    specs: [
      { ...SPEC.api, note: "POST /api/v1/integrations body" },
      { ...SPEC.schema, note: "encrypted_payload NOT NULL" },
    ],
    stack: [
      { ...STACK.backend, note: "Uses crypto + repository CreateIntegration" },
      { ...STACK.data, note: "UUID id generation" },
    ],
    implementation: [
      "Decode CreateIntegrationRequest",
      "ValidateCreate(kind, baseUrl)",
      "Require non-empty secret",
      "encrypted := cipher.EncryptJSON(kind, secret)",
      "repo.CreateIntegration(id, kind, name, baseUrl, encrypted)",
      "Return 201 with IntegrationResponse hasSecret:true",
    ],
    acceptance: [
      "Creates row in integrations table",
      "encrypted_payload not equal plaintext secret",
      "Response 201 with id and hasSecret:true",
      "secret not in response body",
      "Validation errors return 400",
      "Duplicate name allowed (no unique on name)",
      "DB CHECK enforced for kind/base_url combo",
    ],
    files: ["internal/api/integrations/handlers.go"],
    tests: ["go test ./internal/api/integrations/... -run Create"],
  });

  leaf("P07-04", "P07", "Implement PATCH /api/v1/integrations/{id}", "backend", {
    depends_on: ["P07-03"],
    context:
      "Update name and/or replace secret. Partial update: only provided fields. Secret replace re-encrypts entire payload.",
    specs: [
      { ...SPEC.api, note: "PATCH optional secret replace" },
      { ...SPEC.ui, note: "UI never re-displays secret after save" },
    ],
    stack: [
      { ...STACK.backend, note: "chi URL param {id}" },
      { ...STACK.quality, note: "404 when id not found" },
    ],
    implementation: [
      "PatchIntegrationHandler",
      "Load integration by id — 404 not_found",
      "Update name if provided",
      "If secret provided: re-encrypt with existing kind",
      "Cannot change kind via PATCH",
      "Return 200 IntegrationResponse",
    ],
    acceptance: [
      "PATCH name only leaves encrypted_payload unchanged",
      "PATCH secret updates encrypted_payload",
      "Unknown id returns 404",
      "Empty body returns 400 or 200 no-op",
      "kind immutable on PATCH",
      "hasSecret remains true after secret replace",
      "updated_at changes",
    ],
    files: ["internal/api/integrations/handlers.go"],
    tests: ["go test ./internal/api/integrations/... -run Patch"],
  });

  leaf("P07-05", "P07", "Implement DELETE /api/v1/integrations/{id}", "backend", {
    depends_on: ["P07-02"],
    context:
      "Delete integration when not referenced by monitored_repos.source_integration_id or ticket_projects.integration_id. Return 409 conflict otherwise.",
    specs: [
      { ...SPEC.api, note: "DELETE 409 when referenced" },
      { ...SPEC.schema, note: "FK ON DELETE RESTRICT" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "FK references to integrations" },
      { ...SCHEMA.html, note: "RESTRICT on delete" },
    ],
    implementation: [
      "DeleteIntegrationHandler",
      "Count repos with source_integration_id = id",
      "Count ticket_projects with integration_id = id",
      "If any > 0: 409 conflict with message",
      "Else repo.DeleteIntegration(id)",
      "Return 204 No Content",
    ],
    acceptance: [
      "Unreferenced integration deletes with 204",
      "Referenced by repo returns 409",
      "Referenced by ticket_project returns 409",
      "Unknown id returns 404",
      "Requires auth",
      "Row removed from DB on success",
      "Encrypted payload gone after delete",
    ],
    files: ["internal/api/integrations/handlers.go"],
    tests: ["go test ./internal/api/integrations/... -run Delete"],
  });

  leaf("P07-06", "P07", "Add integrations API tests", "backend", {
    depends_on: ["P07-05"],
    context:
      "Full CRUD integration test suite with encrypted secrets verification and 409 delete guard.",
    specs: [
      { ...SPEC.api, note: "Integrations API contract" },
      { ...SPEC.mvp, note: "All 8 kinds creatable per MVP AC" },
    ],
    stack: [
      { ...STACK.quality, note: "go test ./internal/api/integrations/..." },
      { ...STACK.backend, note: "Test all CRUD paths" },
    ],
    implementation: [
      "TestCreateGitHubIntegration",
      "TestCreateGitLabRequiresBaseUrl",
      "TestListDoesNotExposeSecret",
      "TestPatchReplaceSecret",
      "TestDeleteConflictWhenReferenced",
      "Use test cipher with fixed key",
    ],
    acceptance: [
      "Full test package passes",
      "Secret never appears in httptest response bodies",
      "409 delete test with seeded repo FK",
      "All handlers covered",
      "CI compatible",
      "No network calls",
      "Tests run under 30s",
    ],
    files: ["internal/api/integrations/handlers_test.go"],
    tests: ["go test ./internal/api/integrations/..."],
  });

  // ─── P08 Ticket projects API ──────────────────────────────────────────────
  epic("P08", "Ticket projects REST API", "backend", {
    depends_on: ["P07-03"],
    context:
      "ticket_projects link ticket integrations to external project/team IDs with per-project create_config, status_mapping, " +
      "and on_open_ticket_policy (supersede|merge|skip_if_open). Full CRUD at /api/v1/ticket-projects.",
    specs: [
      { ...SPEC.ticketProjects, note: "§5.3 status mapping and policies" },
      { ...SPEC.api, note: "§7.4 ticket-projects endpoints" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "ticket_projects table" },
      { ...SCHEMA.html, note: "create_config JSON per provider" },
    ],
    implementation: [
      "internal/api/ticketprojects/validate.go — provider-specific JSON schemas",
      "GET /api/v1/ticket-projects ?integrationId=",
      "POST create with integrationId, externalProjectId, name, createConfig, statusMapping, onOpenTicketPolicy",
      "PATCH update mapping, policy, configs",
      "DELETE 409 when monitored_repos reference",
    ],
    acceptance: [
      "Create validates create_config per phasical/jira/linear",
      "status_mapping requires open, done, cancelled, superseded keys",
      "onOpenTicketPolicy enum validated",
      "Filter by integrationId query works",
      "DELETE 409 when repo uses ticket_project_id",
      "integrationId must reference ticket kind integration",
      "CamelCase JSON in API",
    ],
  });

  leaf("P08-01", "P08", "Validate ticket project JSON schemas", "backend", {
    context:
      "Validate create_config and status_mapping per ticket provider. Phasical: workspaceId; Jira: projectKey; Linear: teamId. " +
      "status_mapping: { open: string[], done: string[], cancelled: string[], superseded: string[] }.",
    specs: [
      { ...SPEC.ticketProjects, note: "Provider-specific create_config fields" },
      { ...SPEC.providers, note: "Ticket provider credential kinds" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "json_valid CHECK on JSON columns" },
      { ...SCHEMA.html, note: "on_open_ticket_policy enum" },
    ],
    implementation: [
      "internal/api/ticketprojects/validate.go",
      "ValidateCreateConfig(kind, json) error",
      "ValidateStatusMapping(json) — non-empty arrays for each status class",
      "ValidatePolicy(policy) — supersede|merge|skip_if_open",
      "Unmarshal to typed structs per kind",
      "Return field-level validation_error messages",
    ],
    acceptance: [
      "Phasical create_config requires workspaceId",
      "Jira requires projectKey",
      "Linear requires teamId",
      "Missing status_mapping key fails validation",
      "Invalid policy string rejected",
      "Empty status array rejected",
      "Valid payload passes all checks",
    ],
    files: ["internal/api/ticketprojects/validate.go", "internal/api/ticketprojects/validate_test.go"],
    tests: ["go test ./internal/api/ticketprojects/... -run Validate"],
  });

  leaf("P08-02", "P08", "Implement GET /api/v1/ticket-projects", "backend", {
    depends_on: ["P08-01"],
    context:
      "List ticket projects with optional ?integrationId= filter. Return parsed createConfig and statusMapping as JSON objects.",
    specs: [
      { ...SPEC.api, note: "GET ticket-projects query filter" },
      { ...SPEC.ticketProjects, note: "List for UI ticket project table P22" },
    ],
    stack: [
      { ...STACK.backend, note: "handlers.go ListTicketProjects" },
      { ...STACK.frontend, note: "useTicketProjects hook P17" },
    ],
    implementation: [
      "Parse optional integrationId query param",
      "repo.ListTicketProjects or ListByIntegrationID",
      "Map to TicketProjectResponse with camelCase",
      "Parse create_config and status_mapping from DB strings to JSON",
      "Include integrationId, externalProjectId, onOpenTicketPolicy",
      "r.Get(/ticket-projects, handler)",
    ],
    acceptance: [
      "Returns 200 array",
      "integrationId filter returns subset only",
      "createConfig and statusMapping are JSON objects not strings",
      "onOpenTicketPolicy included in each item",
      "Requires auth",
      "Empty filter returns all projects",
      "Sorted by name",
    ],
    files: ["internal/api/ticketprojects/handlers.go"],
    tests: ["go test ./internal/api/ticketprojects/... -run List"],
  });

  leaf("P08-03", "P08", "Implement POST /api/v1/ticket-projects", "backend", {
    depends_on: ["P08-01"],
    context:
      "Create ticket project linked to ticket integration. Body per spec §7.4. Verify integration exists and is ticket kind.",
    specs: [
      { ...SPEC.api, note: "POST /api/v1/ticket-projects JSON example" },
      { ...SPEC.ticketProjects, note: "UNIQUE(integration_id, external_project_id)" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "ticket_projects FK to integrations" },
      { ...SCHEMA.html, note: "external_project_id meaning per provider" },
    ],
    implementation: [
      "Decode CreateTicketProjectRequest",
      "Load integration — 400 if not ticket kind",
      "ValidateCreateConfig and ValidateStatusMapping",
      "Default onOpenTicketPolicy to supersede if omitted",
      "repo.CreateTicketProject with JSON stringified configs",
      "Return 201 TicketProjectResponse",
    ],
    acceptance: [
      "Creates row with valid JSON configs",
      "Duplicate externalProjectId per integration returns 409",
      "Invalid integrationId returns 404",
      "Source kind integration rejected with 400",
      "Default policy supersede when omitted",
      "Returns 201 with new id",
      "Configs round-trip in response",
    ],
    files: ["internal/api/ticketprojects/handlers.go"],
    tests: ["go test ./internal/api/ticketprojects/... -run Create"],
  });

  leaf("P08-04", "P08", "Implement PATCH /api/v1/ticket-projects/{id}", "backend", {
    depends_on: ["P08-03"],
    context:
      "Update name, createConfig, statusMapping, onOpenTicketPolicy. integrationId and externalProjectId immutable.",
    specs: [
      { ...SPEC.api, note: "PATCH ticket-projects partial update" },
      { ...SPEC.ticketProjects, note: "Status mapping editable per project" },
    ],
    stack: [
      { ...STACK.backend, note: "PATCH handler with validation" },
      { ...STACK.ui, note: "Status mapping form P22-04" },
    ],
    implementation: [
      "Load project by id — 404",
      "Load parent integration for kind validation on config updates",
      "Merge partial fields",
      "Re-validate configs if provided",
      "repo.UpdateTicketProject",
      "Return 200 updated object",
    ],
    acceptance: [
      "PATCH name updates name only",
      "PATCH statusMapping re-validates",
      "Cannot change integrationId via PATCH",
      "Invalid policy returns 400",
      "Unknown id 404",
      "updated_at changes",
      "Response reflects all current fields",
    ],
    files: ["internal/api/ticketprojects/handlers.go"],
    tests: ["go test ./internal/api/ticketprojects/... -run Patch"],
  });

  leaf("P08-05", "P08", "Implement DELETE /api/v1/ticket-projects/{id}", "backend", {
    depends_on: ["P08-02"],
    context:
      "Delete when no monitored_repos.ticket_project_id references this row. 409 conflict otherwise.",
    specs: [
      { ...SPEC.api, note: "DELETE 409 when referenced by repos" },
      { ...SPEC.schema, note: "monitored_repos.ticket_project_id FK RESTRICT" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "ON DELETE RESTRICT from monitored_repos" },
      { ...SCHEMA.html, note: "Delete guard" },
    ],
    implementation: [
      "Count monitored_repos where ticket_project_id = id",
      "If count > 0: 409 conflict",
      "repo.DeleteTicketProject(id)",
      "Return 204",
      "404 if not found",
    ],
    acceptance: [
      "Unreferenced project deletes 204",
      "Referenced by repo returns 409",
      "Unknown id 404",
      "Requires auth",
      "Row removed from DB",
      "Integration row unaffected",
      "Subsequent GET list excludes deleted",
    ],
    files: ["internal/api/ticketprojects/handlers.go"],
    tests: ["go test ./internal/api/ticketprojects/... -run Delete"],
  });

  leaf("P08-06", "P08", "Add ticket projects API tests", "backend", {
    depends_on: ["P08-05"],
    context:
      "Integration tests for CRUD, validation failures, integrationId filter, and delete 409.",
    specs: [
      { ...SPEC.api, note: "Ticket projects API" },
      { ...SPEC.mvp, note: "Ticket project CRUD in MVP AC" },
    ],
    stack: [
      { ...STACK.quality, note: "Full package test suite" },
      { ...STACK.backend, note: "handlers_test.go" },
    ],
    implementation: [
      "Seed phasical integration + ticket project",
      "TestCreateValidationFailure",
      "TestListFilterByIntegrationId",
      "TestPatchStatusMapping",
      "TestDelete409WithLinkedRepo",
      "Authenticated httptest client",
    ],
    acceptance: [
      "go test ./internal/api/ticketprojects/... passes",
      "Validation tests for each provider kind",
      "409 delete with FK repo",
      "Filter query param tested",
      "No secret leakage",
      "CI compatible",
      "Coverage of all handlers",
    ],
    files: ["internal/api/ticketprojects/handlers_test.go"],
    tests: ["go test ./internal/api/ticketprojects/..."],
  });

  // ─── P09 Repos API ──────────────────────────────────────────────────────────
  epic("P09", "Monitored repos REST API", "backend", {
    depends_on: ["P08-03"],
    context:
      "CRUD for monitored_repos: links source (kind + path + optional integration) to ticket_project and notification targets. " +
      "Poll engine reads enabled repos. Validates source_integration_id required for self-hosted kinds.",
    specs: [
      { ...SPEC.api, note: "§7.5 repos endpoints" },
      { ...SPEC.domain, note: "Monitored repo is poll unit" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "monitored_repos + join table" },
      { ...SCHEMA.html, note: "source_kind, project_path UNIQUE" },
    ],
    implementation: [
      "internal/api/repos/validate.go",
      "GET /api/v1/repos",
      "POST with notificationTargetIds optional array",
      "PATCH enabled, ticket project, notification links",
      "DELETE monitored repo",
      "Response includes poll state: lastKnownTag, lastError, openTicket*",
    ],
    acceptance: [
      "gitlab repo requires sourceIntegrationId",
      "github repo allows null sourceIntegrationId",
      "POST links notification targets via join table",
      "UNIQUE source_kind+project_path enforced",
      "PATCH enabled toggle works",
      "Response includes ticketProjectId and sourceKind",
      "DELETE removes join rows cascade",
    ],
  });

  leaf("P09-01", "P09", "Validate monitored repo create rules", "backend", {
    context:
      "source_kind enum; project_path format owner/repo; source_integration_id required for gitlab, gitea, forgejo; " +
      "optional for github, codeberg. ticket_project_id required.",
    specs: [
      { ...SPEC.schema, note: "monitored_repos constraints" },
      { ...SPEC.providers, note: "Self-hosted sources need integration for auth" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "source_integration_id nullable FK" },
      { ...SCHEMA.html, note: "project_path format" },
    ],
    implementation: [
      "internal/api/repos/validate.go",
      "ValidateCreate(sourceKind, projectPath, sourceIntegrationId, ticketProjectId)",
      "project_path regex: owner/repo or group/subgroup/repo for gitlab",
      "Verify integration kind matches source_kind when integration provided",
      "Verify ticket_project exists",
      "enabled defaults true",
    ],
    acceptance: [
      "gitlab without sourceIntegrationId fails",
      "github without sourceIntegrationId passes",
      "Mismatched integration kind fails",
      "Invalid source_kind rejected",
      "Empty project_path rejected",
      "Missing ticketProjectId rejected",
      "Valid github/github.com/org/repo passes",
    ],
    files: ["internal/api/repos/validate.go", "internal/api/repos/validate_test.go"],
    tests: ["go test ./internal/api/repos/... -run Validate"],
  });

  leaf("P09-02", "P09", "Implement GET /api/v1/repos", "backend", {
    depends_on: ["P09-01"],
    context:
      "List all monitored repos with related ids and poll state fields for dashboard UI.",
    specs: [
      { ...SPEC.api, note: "Repo list response fields" },
      { ...SPEC.ui, note: "Dashboard repo table P20-03" },
    ],
    stack: [
      { ...STACK.backend, note: "handlers.go ListRepos" },
      { ...STACK.frontend, note: "useRepos hook" },
    ],
    implementation: [
      "repo.ListMonitoredRepos",
      "Include notificationTargetIds from join query",
      "Map: id, sourceKind, projectPath, enabled, sourceIntegrationId, ticketProjectId",
      "Poll state: lastKnownTag, lastPolledAt, lastError, openTicketExternalId, openTicketTag",
      "r.Get(/repos, handler)",
    ],
    acceptance: [
      "Returns 200 array of repos",
      "notificationTargetIds array per repo",
      "lastKnownTag null for never polled",
      "enabled boolean in JSON",
      "Requires auth",
      "Includes all repos enabled and disabled",
      "CamelCase field names",
    ],
    files: ["internal/api/repos/handlers.go"],
    tests: ["go test ./internal/api/repos/... -run List"],
  });

  leaf("P09-03", "P09", "Implement POST /api/v1/repos", "backend", {
    depends_on: ["P09-01"],
    context:
      "Create repo: { sourceKind, projectPath, sourceIntegrationId?, ticketProjectId, notificationTargetIds?, enabled? }. " +
      "Insert join rows for notification targets.",
    specs: [
      { ...SPEC.api, note: "POST /api/v1/repos body" },
      { ...SPEC.notifications, note: "Optional per-repo notification routing" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "monitored_repo_notifications join" },
      { ...SCHEMA.html, note: "Empty join = global targets" },
    ],
    implementation: [
      "ValidateCreate from P09-01",
      "Transaction: insert monitored_repos, insert join rows",
      "Validate notification target ids exist",
      "Generate UUID id",
      "Return 201 RepoResponse",
      "Initial poll fields null",
    ],
    acceptance: [
      "Creates repo row",
      "Links notification targets in join table",
      "Duplicate source_kind+project_path returns 409",
      "Invalid ticketProjectId returns 400",
      "enabled defaults true",
      "Returns 201 with id",
      "notificationTargetIds echoed in response",
    ],
    files: ["internal/api/repos/handlers.go"],
    tests: ["go test ./internal/api/repos/... -run Create"],
  });

  leaf("P09-04", "P09", "Implement PATCH /api/v1/repos/{id}", "backend", {
    depends_on: ["P09-03"],
    context:
      "Update enabled, ticketProjectId, notificationTargetIds, sourceIntegrationId. sourceKind and projectPath immutable.",
    specs: [
      { ...SPEC.api, note: "PATCH repos partial update" },
      { ...SPEC.domain, note: "Disabling repo skips poll" },
    ],
    stack: [
      { ...STACK.backend, note: "Replace notification links on update" },
      { ...STACK.ui, note: "Repo enabled toggle P23-05" },
    ],
    implementation: [
      "Load repo by id — 404",
      "Update enabled, ticket_project_id if provided",
      "Replace notification links: delete joins, insert new",
      "Re-validate ticket project and integration if changed",
      "Cannot change sourceKind or projectPath",
      "Return 200 RepoResponse",
    ],
    acceptance: [
      "PATCH enabled=false persists",
      "PATCH notificationTargetIds replaces all links",
      "Empty notificationTargetIds clears joins (global targets)",
      "ticketProjectId change validated",
      "sourceKind immutable",
      "404 unknown id",
      "updated_at changes",
    ],
    files: ["internal/api/repos/handlers.go"],
    tests: ["go test ./internal/api/repos/... -run Patch"],
  });

  leaf("P09-05", "P09", "Implement DELETE /api/v1/repos/{id}", "backend", {
    depends_on: ["P09-02"],
    context:
      "Delete monitored repo. Join rows cascade via ON DELETE CASCADE. Poll history events may SET NULL monitored_repo_id.",
    specs: [
      { ...SPEC.api, note: "DELETE /api/v1/repos/{id}" },
      { ...SPEC.schema, note: "CASCADE on monitored_repo_notifications" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "ON DELETE CASCADE join table" },
      { ...SCHEMA.html, note: "poll_run_events SET NULL on repo delete" },
    ],
    implementation: [
      "DeleteMonitoredRepoHandler",
      "repo.DeleteMonitoredRepo(id)",
      "404 if not found",
      "Return 204 No Content",
      "Join rows removed automatically",
    ],
    acceptance: [
      "Deletes repo row 204",
      "Join notification links removed",
      "Unknown id 404",
      "Requires auth",
      "Integration and ticket project rows unaffected",
      "Subsequent GET excludes deleted repo",
      "No 409 guard (repos are leaf entities)",
    ],
    files: ["internal/api/repos/handlers.go"],
    tests: ["go test ./internal/api/repos/... -run Delete"],
  });

  leaf("P09-06", "P09", "Add monitored repos API tests", "backend", {
    depends_on: ["P09-05"],
    context:
      "Full CRUD tests including validation, notification linking, and enabled toggle.",
    specs: [
      { ...SPEC.api, note: "Repos API contract" },
      { ...SPEC.mvp, note: "Repo CRUD in MVP" },
    ],
    stack: [
      { ...STACK.quality, note: "go test ./internal/api/repos/..." },
      { ...STACK.backend, note: "handlers_test.go" },
    ],
    implementation: [
      "Seed integration, ticket project, notification target",
      "TestCreateGitLabRequiresIntegration",
      "TestCreateWithNotificationTargets",
      "TestPatchDisableRepo",
      "TestDeleteRepo",
      "TestDuplicateProjectPath409",
    ],
    acceptance: [
      "Full package tests pass",
      "Validation edge cases covered",
      "Notification join verified in DB",
      "409 duplicate path",
      "CI compatible",
      "No network",
      "All handlers tested",
    ],
    files: ["internal/api/repos/handlers_test.go"],
    tests: ["go test ./internal/api/repos/..."],
  });

  // ─── P10 Notifications API ────────────────────────────────────────────────
  epic("P10", "Notification targets REST API", "backend", {
    depends_on: ["P05-05", "P04-01", "P03-08"],
    context:
      "CRUD for Shoutrrr notification targets. URLs encrypted at rest. API never returns shoutrrrUrl after create. " +
      "events filter: create, error, supersede. POST /notification-targets/{id}/test in P13.",
    specs: [
      { ...SPEC.notifications, note: "§5.5 Shoutrrr integration" },
      { ...SPEC.api, note: "§7.6 notification-targets endpoints" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "notification_targets table" },
      { ...SCHEMA.html, note: "events_json default array" },
    ],
    implementation: [
      "internal/api/notifications/crypto.go — encrypt/decrypt URL",
      "GET /api/v1/notification-targets — no URL in response",
      "POST with shoutrrrUrl one-time",
      "PATCH optional URL replace",
      "DELETE target",
      "events array validation",
    ],
    acceptance: [
      "Create encrypts shoutrrr_url_encrypted",
      "List never returns shoutrrrUrl",
      "events defaults [create,error,supersede]",
      "enabled toggle via PATCH",
      "Invalid events value rejected",
      "DELETE returns 204",
      "URL never logged",
    ],
  });

  leaf("P10-01", "P10", "Encrypt Shoutrrr URL on create and update", "backend", {
    depends_on: ["P04-01"],
    context:
      "Shoutrrr URLs (slack://, ntfy://, etc.) encrypted with same AES-GCM cipher as integration secrets. " +
      "Separate helper from integration payload — raw URL string encrypted.",
    specs: [
      { ...SPEC.notifications, note: "Shoutrrr URL stored encrypted" },
      { ...SPEC.schema, note: "shoutrrr_url_encrypted TEXT NOT NULL" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/api/notifications/crypto.go" },
      { ...STACK.data, note: "Decrypt only at send time P13" },
    ],
    implementation: [
      "EncryptShoutrrrURL(cipher, url string) (string, error)",
      "DecryptShoutrrrURL(cipher, blob string) (string, error)",
      "Validate URL non-empty and contains ://",
      "Use cipher.Encrypt([]byte(url))",
      "Unit test round-trip",
      "Never expose decrypt in list handlers",
    ],
    acceptance: [
      "Encrypt produces opaque base64 blob",
      "Decrypt round-trip preserves URL",
      "Empty URL rejected",
      "Different nonces per encrypt",
      "Decrypt wrong key fails",
      "Helper used by create and patch handlers",
      "go test ./internal/api/notifications/... -run Crypto passes",
    ],
    files: ["internal/api/notifications/crypto.go", "internal/api/notifications/crypto_test.go"],
    tests: ["go test ./internal/api/notifications/... -run Crypto"],
  });

  leaf("P10-02", "P10", "Implement GET /api/v1/notification-targets", "backend", {
    context:
      "List targets: { id, name, events, enabled, createdAt, updatedAt }. No shoutrrrUrl field.",
    specs: [
      { ...SPEC.api, note: "Notification target list response" },
      { ...SPEC.notifications, note: "events filter values" },
    ],
    stack: [
      { ...STACK.backend, note: "handlers.go ListNotificationTargets" },
      { ...STACK.frontend, note: "useNotificationTargets P17" },
    ],
    implementation: [
      "repo.ListNotificationTargets",
      "Parse events_json to []string in response",
      "Omit shoutrrr_url_encrypted from DTO",
      "Map enabled int to boolean",
      "r.Get(/notification-targets, handler)",
    ],
    acceptance: [
      "Returns 200 array",
      "No shoutrrrUrl in JSON",
      "events is string array",
      "enabled is boolean",
      "Requires auth",
      "Empty list returns []",
      "Encrypted blob never in response",
    ],
    files: ["internal/api/notifications/handlers.go"],
    tests: ["go test ./internal/api/notifications/... -run List"],
  });

  leaf("P10-03", "P10", "Implement POST /api/v1/notification-targets", "backend", {
    depends_on: ["P10-01"],
    context:
      "Create: { name, shoutrrrUrl, events?, enabled? }. Encrypt URL. Default events [create, error, supersede].",
    specs: [
      { ...SPEC.api, note: "POST notification-targets body" },
      { ...SPEC.notifications, note: "Supported events: create, error, supersede" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "events_json DEFAULT" },
      { ...SCHEMA.html, note: "enabled default 1" },
    ],
    implementation: [
      "Decode CreateNotificationTargetRequest",
      "EncryptShoutrrrURL(shoutrrrUrl)",
      "Validate events subset of allowed",
      "Default events if omitted",
      "repo.CreateNotificationTarget",
      "Return 201 without shoutrrrUrl",
    ],
    acceptance: [
      "Creates encrypted row",
      "Response excludes shoutrrrUrl",
      "Default events applied",
      "Invalid event name returns 400",
      "enabled defaults true",
      "Empty name returns 400",
      "Returns 201 with id",
    ],
    files: ["internal/api/notifications/handlers.go"],
    tests: ["go test ./internal/api/notifications/... -run Create"],
  });

  leaf("P10-04", "P10", "Implement PATCH /api/v1/notification-targets/{id}", "backend", {
    depends_on: ["P10-03"],
    context:
      "Update name, events, enabled, optional shoutrrrUrl replace (re-encrypt).",
    specs: [
      { ...SPEC.api, note: "PATCH optional shoutrrrUrl" },
      { ...SPEC.ui, note: "URL never re-displayed P24" },
    ],
    stack: [
      { ...STACK.backend, note: "Partial update handler" },
      { ...STACK.quality, note: "404 not found" },
    ],
    implementation: [
      "Load target by id — 404",
      "Update fields if provided",
      "If shoutrrrUrl: re-encrypt and replace blob",
      "Validate events if provided",
      "repo.UpdateNotificationTarget",
      "Return 200 without URL",
    ],
    acceptance: [
      "PATCH name only preserves encrypted URL",
      "PATCH shoutrrrUrl re-encrypts",
      "PATCH events updates events_json",
      "PATCH enabled=false works",
      "Unknown id 404",
      "Response has no shoutrrrUrl",
      "updated_at changes",
    ],
    files: ["internal/api/notifications/handlers.go"],
    tests: ["go test ./internal/api/notifications/... -run Patch"],
  });

  leaf("P10-05", "P10", "Implement DELETE /api/v1/notification-targets/{id}", "backend", {
    context:
      "Delete notification target. Join rows in monitored_repo_notifications cascade.",
    specs: [
      { ...SPEC.api, note: "DELETE notification-targets" },
      { ...SPEC.schema, note: "CASCADE on join table" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "ON DELETE CASCADE monitored_repo_notifications" },
      { ...SCHEMA.html, note: "Delete target" },
    ],
    implementation: [
      "DeleteNotificationTargetHandler",
      "repo.DeleteNotificationTarget(id)",
      "404 if not found",
      "Return 204",
    ],
    acceptance: [
      "Deletes row 204",
      "Join links to repos removed",
      "Unknown id 404",
      "Requires auth",
      "Encrypted URL gone from DB",
      "Repos unaffected",
      "List no longer includes target",
    ],
    files: ["internal/api/notifications/handlers.go"],
    tests: ["go test ./internal/api/notifications/... -run Delete"],
  });

  leaf("P10-06", "P10", "Add notification targets API tests", "backend", {
    depends_on: ["P10-05"],
    context:
      "CRUD tests verifying URL never appears in responses and encryption round-trip.",
    specs: [
      { ...SPEC.api, note: "Notification targets API" },
      { ...SPEC.mvp, note: "Notification CRUD in MVP" },
    ],
    stack: [
      { ...STACK.quality, note: "go test ./internal/api/notifications/..." },
      { ...STACK.backend, note: "handlers_test.go" },
    ],
    implementation: [
      "TestCreateEncryptsURL",
      "TestListNoURL",
      "TestPatchReplaceURL",
      "TestInvalidEvents",
      "TestDeleteTarget",
      "Scan response bodies for slack:// leak",
    ],
    acceptance: [
      "Full package passes",
      "No plaintext URL in any response",
      "Events validation tested",
      "Delete removes row",
      "CI compatible",
      "Uses test cipher key",
      "All handlers covered",
    ],
    files: ["internal/api/notifications/handlers_test.go"],
    tests: ["go test ./internal/api/notifications/..."],
  });

  // ─── P11 Source providers ─────────────────────────────────────────────────
  epic("P11", "Source providers", "backend", {
    depends_on: ["P07-03"],
    context:
      "Go implementations of SourceProvider for github, gitlab, gitea, forgejo, codeberg. Fetch latest stable release tag. " +
      "Factory decrypts integration credentials. Connectivity test via GET /user. Wire POST /integrations/{id}/test.",
    specs: [
      { ...SPEC.providers, note: "§6 Source providers" },
      { ...SPEC.domain, note: "releases/latest per platform" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/source/" },
      { ...STACK.quality, note: "httptest mocks per provider" },
    ],
    implementation: [
      "internal/providers/source/types.go — Release, SourceProvider interface",
      "github.go — GET /repos/{owner}/{repo}/releases/latest",
      "gitlab.go — GET /projects/{id}/releases/permalink/latest",
      "gitea.go — GiteaCompatibleSource for gitea/forgejo/codeberg",
      "factory.go — NewSourceProvider(kind, integration)",
      "testconn.go — TestConnection per kind",
      "POST /api/v1/integrations/{id}/test for source kinds",
    ],
    acceptance: [
      "GitHub 404 means no release (not error)",
      "GitLab uses permalink/latest endpoint",
      "Gitea family shares one client",
      "Factory returns error for ticket kinds",
      "Test connection calls GET /user equivalent",
      "Release struct: tag, name, publishedAt, url",
      "httptest mocks for all 5 kinds",
    ],
  });

  leaf("P11-01", "P11", "Define Release struct and SourceProvider interface", "backend", {
    context:
      "interface SourceProvider { GetLatestRelease(ctx, owner, repo) (Release, error); TestConnection(ctx) error }. " +
      "Release: Tag, Name, PublishedAt, URL strings.",
    specs: [
      { ...SPEC.providers, note: "§6.1 SourceProvider interface" },
      { ...SPEC.domain, note: "Tag comparison for poll decision" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/source/types.go" },
      { ...STACK.layout, note: "Provider package structure" },
    ],
    implementation: [
      "type Release struct { Tag, Name, PublishedAt, URL string }",
      "type SourceProvider interface { GetLatestRelease(...); TestConnection(...) }",
      "ErrNoRelease sentinel for 404 latest",
      "context.Context on all methods",
      "Document owner/repo parsing from project_path",
    ],
    acceptance: [
      "Release struct exported",
      "SourceProvider interface defined",
      "ErrNoRelease distinct from network errors",
      "GetLatestRelease accepts owner, repo strings",
      "TestConnection returns nil on success",
      "Package compiles",
      "Godoc on interface methods",
    ],
    files: ["internal/providers/source/types.go"],
    tests: ["go build ./internal/providers/source/..."],
  });

  leaf("P11-02", "P11", "Implement GitHub source provider", "backend", {
    depends_on: ["P11-01"],
    context:
      "GitHub API: GET https://api.github.com/repos/{owner}/{repo}/releases/latest with Authorization token. " +
      "404 → ErrNoRelease. Parse tag_name from response.",
    specs: [
      { ...SPEC.providers, note: "GitHub GET releases/latest" },
      { ...SPEC.schema, note: "github integration: base_url NULL, token in payload" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/source/github.go" },
      { ...STACK.quality, note: "httptest mock GitHub API" },
    ],
    implementation: [
      "type GitHubSource struct { client *http.Client; token string }",
      "GetLatestRelease: GET /repos/{owner}/{repo}/releases/latest",
      "Header: Authorization Bearer {token} or token {pat}",
      "404 → ErrNoRelease",
      "Parse JSON tag_name, name, published_at, html_url",
      "TestConnection: GET /user",
    ],
    acceptance: [
      "Returns Release with tag on 200",
      "404 returns ErrNoRelease not error",
      "401 returns auth error",
      "TestConnection calls GET /user",
      "Uses project_path owner/repo split",
      "Rate limit headers logged on 403",
      "go test -run GitHub passes with mock",
    ],
    files: ["internal/providers/source/github.go", "internal/providers/source/github_test.go"],
    tests: ["go test ./internal/providers/source/... -run GitHub"],
  });

  leaf("P11-03", "P11", "Implement GitLab source provider", "backend", {
    depends_on: ["P11-01"],
    context:
      "GitLab API v4: GET {base_url}/api/v4/projects/{encoded_path}/releases/permalink/latest. " +
      "Private token header PRIVATE-TOKEN. project_path URL-encoded.",
    specs: [
      { ...SPEC.providers, note: "GitLab permalink/latest" },
      { ...SPEC.schema, note: "gitlab requires base_url" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/source/gitlab.go" },
      { ...STACK.quality, note: "Mock GitLab API" },
    ],
    implementation: [
      "GitLabSource { baseURL, token, client }",
      "Encode project path: url.PathEscape(group/subgroup/repo)",
      "GET /api/v4/projects/{path}/releases/permalink/latest",
      "Parse tag_name from release response",
      "TestConnection: GET /api/v4/user",
      "Handle self-hosted base_url",
    ],
    acceptance: [
      "Uses integration base_url",
      "Project path URL-encoded correctly",
      "404 → ErrNoRelease",
      "Returns tag from latest release",
      "TestConnection GET /api/v4/user",
      "Self-hosted GitLab base URL works",
      "go test -run GitLab passes",
    ],
    files: ["internal/providers/source/gitlab.go", "internal/providers/source/gitlab_test.go"],
    tests: ["go test ./internal/providers/source/... -run GitLab"],
  });

  leaf("P11-04", "P11", "Implement GiteaCompatible source client", "backend", {
    depends_on: ["P11-01"],
    context:
      "Shared client for gitea, forgejo, codeberg: GET {base}/api/v1/repos/{owner}/{repo}/releases/latest. " +
      "Codeberg uses fixed base https://codeberg.org. Auth: Authorization token {token}.",
    specs: [
      { ...SPEC.providers, note: "Gitea API v1 shared client" },
      { ...SPEC.schema, note: "codeberg base_url NULL — default https://codeberg.org" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/source/gitea.go" },
      { ...STACK.quality, note: "One test suite for three kinds" },
    ],
    implementation: [
      "GiteaCompatibleSource { kind, baseURL, token }",
      "Default base: codeberg.org when kind=codeberg and base null",
      "GET /api/v1/repos/{owner}/{repo}/releases/latest",
      "TestConnection: GET /api/v1/user",
      "Parse tag_name from Gitea release JSON",
      "kind field for logging only",
    ],
    acceptance: [
      "Works for gitea, forgejo, codeberg kinds",
      "Codeberg default base when integration base_url null",
      "404 → ErrNoRelease",
      "Uses token auth header",
      "project_path owner/repo split",
      "TestConnection GET /api/v1/user",
      "go test -run Gitea passes",
    ],
    files: ["internal/providers/source/gitea.go", "internal/providers/source/gitea_test.go"],
    tests: ["go test ./internal/providers/source/... -run Gitea"],
  });

  leaf("P11-05", "P11", "Add source provider factory", "backend", {
    depends_on: ["P11-02", "P11-03", "P11-04"],
    context:
      "NewSourceProvider(integration, cipher) decrypts payload, returns SourceProvider for source kinds. " +
      "Error for ticket kinds or unknown kind.",
    specs: [
      { ...SPEC.providers, note: "Factory pattern §6" },
      { ...SPEC.schema, note: "integrations.kind drives factory" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/source/factory.go" },
      { ...STACK.data, note: "Decrypt integration encrypted_payload" },
    ],
    implementation: [
      "NewSourceProvider(row Integration, cipher) (SourceProvider, error)",
      "Switch on kind: github, gitlab, gitea, forgejo, codeberg",
      "Decrypt payload → token (+ base_url from row)",
      "Return typed provider with http.Client timeout 30s",
      "Default http.Client with reasonable timeout",
      "Return error for phasical/jira/linear kinds",
    ],
    acceptance: [
      "github kind returns GitHubSource",
      "gitlab returns GitLabSource with base_url",
      "codeberg uses default base when null",
      "phasical kind returns error",
      "Decrypt failure propagates",
      "HTTP client has timeout",
      "go test factory with mock cipher",
    ],
    files: ["internal/providers/source/factory.go", "internal/providers/source/factory_test.go"],
    tests: ["go test ./internal/providers/source/... -run Factory"],
  });

  leaf("P11-06", "P11", "Implement source connectivity tests", "backend", {
    depends_on: ["P11-05"],
    context:
      "TestConnection(ctx) on each provider. Used by POST /integrations/{id}/test. Returns friendly error on auth failure.",
    specs: [
      { ...SPEC.providers, note: "§6.3 Connectivity tests" },
      { ...SPEC.api, note: "POST /integrations/{id}/test response" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/source/testconn.go" },
      { ...STACK.quality, note: "Mock each /user endpoint" },
    ],
    implementation: [
      "RunTestConnection(provider SourceProvider) error",
      "Map HTTP status to messages: 401 invalid credentials",
      "Timeout after 15s",
      "Log provider kind on failure",
      "Wrap errors with kind prefix",
    ],
    acceptance: [
      "Success returns nil",
      "401 returns clear auth error",
      "Network timeout returns wrapped error",
      "Works for all 5 source kinds via factory",
      "Does not leak token in error message",
      "go test connectivity mocks pass",
      "Callable from API test handler",
    ],
    files: ["internal/providers/source/testconn.go"],
    tests: ["go test ./internal/providers/source/... -run TestConn"],
  });

  leaf("P11-07", "P11", "Wire POST /integrations/{id}/test for source kinds", "backend", {
    depends_on: ["P11-06", "P07-02"],
    context:
      "API handler loads integration, builds source provider via factory, runs TestConnection. " +
      "Returns { ok: true } or 400/502 with error message.",
    specs: [
      { ...SPEC.api, note: "POST /integrations/{id}/test" },
      { ...SPEC.providers, note: "Source vs ticket test routing" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/api/integrations/test.go" },
      { ...STACK.quality, note: "httptest with mock provider" },
    ],
    implementation: [
      "TestIntegrationHandler",
      "Load integration by id — 404",
      "If source kind: factory + TestConnection",
      "If ticket kind: delegate to P12-07 (stub or shared handler)",
      "Return 200 { ok: true } on success",
      "Return 400 { error } on connection failure",
    ],
    acceptance: [
      "GitHub integration test returns 200 on mock success",
      "Invalid credentials return 400",
      "Ticket kind not handled here (P12)",
      "Unknown integration 404",
      "Requires auth",
      "Does not modify integration row",
      "go test integration test handler",
    ],
    files: ["internal/api/integrations/test.go"],
    tests: ["go test ./internal/api/integrations/... -run TestIntegration"],
  });

  leaf("P11-08", "P11", "Add source provider httptest mocks", "backend", {
    depends_on: ["P11-05"],
    context:
      "Shared test helpers: mock GitHub/GitLab/Gitea API servers for unit tests without network.",
    specs: [
      { ...SPEC.providers, note: "Provider behavior contract" },
      { ...SPEC.mvp, note: "One test per source kind in CI P28" },
    ],
    stack: [
      { ...STACK.quality, note: "httptest.Server per platform" },
      { ...STACK.backend, note: "internal/providers/source/testing.go" },
    ],
    implementation: [
      "MockGitHubServer(handler) *httptest.Server",
      "MockGitLabServer, MockGiteaServer helpers",
      "Fixture JSON for latest release response",
      "Fixture 404 for no release case",
      "Tests for all providers using mocks",
      "Table-driven tests per kind",
    ],
    acceptance: [
      "All source provider tests pass offline",
      "404 latest release case covered",
      "Auth header verified in mocks",
      "Tag parsed correctly from fixtures",
      "No real network in go test",
      "CI runs without API tokens",
      "go test ./internal/providers/source/... green",
    ],
    files: ["internal/providers/source/testing.go", "internal/providers/source/mock_test.go"],
    tests: ["go test ./internal/providers/source/..."],
  });

  // ─── P12 Ticket providers ─────────────────────────────────────────────────
  epic("P12", "Ticket providers", "backend", {
    depends_on: ["P08-03"],
    context:
      "TicketProvider interface for phasical, jira, linear: CreateTicket, GetStatus, AddComment, UpdateTicket. " +
      "ClassifyStatus maps external status to open/done/cancelled/superseded via ticket_projects.status_mapping. " +
      "Factory + POST /integrations/{id}/test for ticket kinds.",
    specs: [
      { ...SPEC.providers, note: "§6 Ticket providers" },
      { ...SPEC.ticketProjects, note: "status_mapping and create_config usage" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/ticket/" },
      { ...STACK.quality, note: "Unit tests with HTTP mocks" },
    ],
    implementation: [
      "internal/providers/ticket/types.go — TicketProvider, StatusMapping",
      "classify.go — ClassifyStatus(mapping, externalStatus)",
      "phasical.go, jira.go, linear.go implementations",
      "factory.go — NewTicketProvider(integration)",
      "POST /integrations/{id}/test for ticket kinds",
      "Create ticket uses ticket_projects.create_config",
    ],
    acceptance: [
      "Phasical create/status/comment/update per spec §5.4",
      "Jira REST API v3 issue create",
      "Linear GraphQL mutations",
      "ClassifyStatus handles case-insensitive match",
      "Factory errors on source kinds",
      "Test connection per provider",
      "Unit tests with mocks for all 3",
    ],
  });

  leaf("P12-01", "P12", "Define TicketProvider interface and StatusMapping", "backend", {
    context:
      "interface TicketProvider { CreateTicket(ctx, CreateTicketInput) (externalId, url, error); GetStatus(ctx, externalId) (string, error); " +
      "AddComment(ctx, externalId, body) error; UpdateTicket(ctx, externalId, UpdateInput) error }. StatusMapping struct with Open, Done, Cancelled, Superseded []string.",
    specs: [
      { ...SPEC.providers, note: "§6.2 TicketProvider interface" },
      { ...SPEC.ticketProjects, note: "status_mapping JSON shape" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/ticket/types.go" },
      { ...STACK.layout, note: "Input/output structs" },
    ],
    implementation: [
      "type StatusMapping struct { Open, Done, Cancelled, Superseded []string }",
      "type CreateTicketInput struct { Title, Body string; Config map[string]any }",
      "type UpdateInput struct { Title, Body *string; Status *string }",
      "TicketProvider interface with 4 methods",
      "TicketResult { ExternalID, URL string }",
      "ParseStatusMapping(json string) (StatusMapping, error)",
    ],
    acceptance: [
      "TicketProvider interface exported",
      "StatusMapping matches spec JSON keys",
      "CreateTicketInput has Title and Body",
      "All methods accept context.Context",
      "ParseStatusMapping unmarshals JSON",
      "Invalid JSON returns error",
      "Package compiles",
    ],
    files: ["internal/providers/ticket/types.go"],
    tests: ["go build ./internal/providers/ticket/..."],
  });

  leaf("P12-02", "P12", "Implement ClassifyStatus helper", "backend", {
    depends_on: ["P12-01"],
    context:
      "Map external status string to enum: open, done, cancelled, superseded, unknown. Case-insensitive match against status_mapping arrays.",
    specs: [
      { ...SPEC.ticketProjects, note: "Status classification for poll policies" },
      { ...SPEC.domain, note: "Open ticket detection for supersede/merge/skip" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/ticket/classify.go" },
      { ...STACK.quality, note: "Table-driven unit tests" },
    ],
    implementation: [
      "type StatusClass string — open, done, cancelled, superseded, unknown",
      "ClassifyStatus(mapping StatusMapping, external string) StatusClass",
      "strings.EqualFold for comparison",
      "First match wins: open before done, etc.",
      "Empty external → unknown",
      "Export for poll engine P14",
    ],
    acceptance: [
      "Known open status returns open",
      "Done status returns done",
      "Case insensitive matching",
      "Unknown status returns unknown",
      "Empty mapping returns unknown",
      "Superseded statuses classified correctly",
      "go test -run Classify passes",
    ],
    files: ["internal/providers/ticket/classify.go", "internal/providers/ticket/classify_test.go"],
    tests: ["go test ./internal/providers/ticket/... -run Classify"],
  });

  leaf("P12-03", "P12", "Implement Phasical ticket provider", "backend", {
    depends_on: ["P12-01"],
    context:
      "Phasical REST API at integration base_url. Create task in project using external_project_id (Phasical projectId). " +
      "Auth: Bearer api_key from encrypted payload. Status/comment/update per specs.html §5.4 provider table.",
    specs: [
      { ...SPEC.providers, note: "§6.4 Phasical — POST {base_url}/task/{projectId}" },
      { ...SPEC.ticketProjects, note: "§5.3 create_config { status, priority }; external_project_id = projectId" },
      { ...SPEC.domain, note: "§5.4 ticket title/description format" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/ticket/phasical.go" },
      { ...STACK.quality, note: "httptest mock Phasical API" },
    ],
    implementation: [
      "PhasicalProvider { baseURL, apiKey, httpClient }",
      "CreateTicket: POST {base_url}/task/{projectId} per specs.html §5.4 — projectId from TicketProject.ExternalProjectID",
      "Request body uses create_config defaults: { status, priority } from ticket_projects.create_config JSON",
      "GetTicketStatus: GET task by external id — map Phasical status slug for ClassifyStatus",
      "UpdateTicketStatus: transition to superseded slug from status_mapping.superseded",
      "AddTicketComment: POST comment on supersede with old tag → new tag and release URL",
      "UpdateTicket (merge policy): PATCH title/description when tag changes",
      "TestConnection: lightweight authenticated GET against Phasical API base",
      "Auth: Bearer token from decrypted payload { api_key }",
    ],
    acceptance: [
      "Create uses POST /task/{projectId} not a invented path",
      "external_project_id passed as projectId — not workspaceId in create_config",
      "create_config.status and priority applied on create",
      "GetStatus returns raw Phasical status slug",
      "Supersede comment includes release link per §5.2.1",
      "401 on bad api key",
      "Uses integration.base_url from DB",
      "go test -run Phasical with httptest mock",
    ],
    files: ["internal/providers/ticket/phasical.go", "internal/providers/ticket/phasical_test.go"],
    tests: ["go test ./internal/providers/ticket/... -run Phasical"],
  });

  leaf("P12-04", "P12", "Implement Jira ticket provider", "backend", {
    depends_on: ["P12-01"],
    context:
      "Jira Cloud/Server REST API v3. Basic auth email+apiToken. Create issue in projectKey from create_config. " +
      "GET /rest/api/3/issue/{key} for status.",
    specs: [
      { ...SPEC.providers, note: "Jira REST v3" },
      { ...SPEC.ticketProjects, note: "create_config.projectKey" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/ticket/jira.go" },
      { ...STACK.quality, note: "Mock Jira API" },
    ],
    implementation: [
      "JiraProvider { baseURL, email, apiToken }",
      "CreateTicket: POST /rest/api/3/issue with project key",
      "GetStatus: GET issue → fields.status.name",
      "AddComment: POST /rest/api/3/issue/{key}/comment",
      "UpdateTicket: PUT transition or fields update",
      "TestConnection: GET /rest/api/3/myself",
    ],
    acceptance: [
      "Create returns issue key as external id",
      "Issue URL constructed from base and key",
      "GetStatus returns status name",
      "Basic auth header correct",
      "Invalid project key returns error",
      "TestConnection GET /myself",
      "go test -run Jira passes",
    ],
    files: ["internal/providers/ticket/jira.go", "internal/providers/ticket/jira_test.go"],
    tests: ["go test ./internal/providers/ticket/... -run Jira"],
  });

  leaf("P12-05", "P12", "Implement Linear ticket provider", "backend", {
    depends_on: ["P12-01"],
    context:
      "Linear GraphQL API https://api.linear.app/graphql. API key auth. Create issue in team from create_config.teamId.",
    specs: [
      { ...SPEC.providers, note: "Linear GraphQL" },
      { ...SPEC.ticketProjects, note: "create_config.teamId" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/ticket/linear.go" },
      { ...STACK.quality, note: "Mock GraphQL server" },
    ],
    implementation: [
      "LinearProvider { apiKey, client }",
      "CreateTicket: mutation issueCreate with teamId",
      "GetStatus: query issue { state { name } }",
      "AddComment: mutation commentCreate",
      "UpdateTicket: mutation issueUpdate",
      "TestConnection: query { viewer { id } }",
    ],
    acceptance: [
      "Create returns Linear issue id and url",
      "GetStatus returns state name",
      "Authorization header with api key",
      "GraphQL errors surfaced",
      "teamId from create_config required",
      "TestConnection viewer query",
      "go test -run Linear passes",
    ],
    files: ["internal/providers/ticket/linear.go", "internal/providers/ticket/linear_test.go"],
    tests: ["go test ./internal/providers/ticket/... -run Linear"],
  });

  leaf("P12-06", "P12", "Add ticket provider factory", "backend", {
    depends_on: ["P12-03", "P12-04", "P12-05"],
    context:
      "NewTicketProvider(integration, cipher) for phasical, jira, linear. Decrypt credentials. Error on source kinds.",
    specs: [
      { ...SPEC.providers, note: "Factory §6" },
      { ...SPEC.schema, note: "Ticket integration kinds" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/providers/ticket/factory.go" },
      { ...STACK.data, note: "Decrypt encrypted_payload" },
    ],
    implementation: [
      "NewTicketProvider(row, cipher) (TicketProvider, error)",
      "Switch phasical, jira, linear",
      "Decrypt payload fields per kind",
      "HTTP client with 30s timeout",
      "Return error for github etc.",
      "Pass base_url to phasical/jira; linear uses fixed API URL",
    ],
    acceptance: [
      "phasical returns PhasicalProvider",
      "jira returns JiraProvider with credentials",
      "linear returns LinearProvider",
      "github kind returns error",
      "Decrypt failure propagates",
      "HTTP timeout configured",
      "go test factory",
    ],
    files: ["internal/providers/ticket/factory.go", "internal/providers/ticket/factory_test.go"],
    tests: ["go test ./internal/providers/ticket/... -run Factory"],
  });

  leaf("P12-07", "P12", "Wire POST /integrations/{id}/test for ticket kinds", "backend", {
    depends_on: ["P12-06", "P07-02"],
    context:
      "Extend TestIntegrationHandler: ticket kinds use NewTicketProvider + TestConnection (viewer/myself/myself equivalent).",
    specs: [
      { ...SPEC.api, note: "Integration test endpoint" },
      { ...SPEC.providers, note: "§6.3 ticket connectivity tests" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/api/integrations/test.go" },
      { ...STACK.quality, note: "Route by integration kind" },
    ],
    implementation: [
      "In TestIntegrationHandler: branch on IsTicketKind",
      "factory NewTicketProvider + TestConnection",
      "Phasical: GET me; Jira: GET myself; Linear: viewer query",
      "Return 200 { ok: true } or 400 error",
      "Share handler with P11-07",
    ],
    acceptance: [
      "Phasical test returns 200 on mock",
      "Jira test returns 200 on mock",
      "Linear test returns 200 on mock",
      "Source kinds still use source factory",
      "Bad credentials 400",
      "404 unknown integration",
      "go test covers ticket branch",
    ],
    files: ["internal/api/integrations/test.go"],
    tests: ["go test ./internal/api/integrations/... -run TestIntegration"],
  });

  leaf("P12-08", "P12", "Add ticket provider unit tests with mocks", "backend", {
    depends_on: ["P12-06"],
    context:
      "Comprehensive mock tests for all three ticket providers and ClassifyStatus integration.",
    specs: [
      { ...SPEC.providers, note: "Ticket provider contracts" },
      { ...SPEC.mvp, note: "Ticket provider CI tests P28-02" },
    ],
    stack: [
      { ...STACK.quality, note: "go test ./internal/providers/ticket/..." },
      { ...STACK.backend, note: "mock_test.go fixtures" },
    ],
    implementation: [
      "Mock servers for Phasical, Jira, Linear",
      "Test create + get status flow per provider",
      "Test ClassifyStatus with real mapping fixtures",
      "Test factory for all ticket kinds",
      "No network in CI",
      "Table-driven error cases",
    ],
    acceptance: [
      "All ticket provider tests pass",
      "Create and GetStatus covered per kind",
      "ClassifyStatus integration test",
      "Factory rejects source kinds",
      "No API keys in test output",
      "CI offline",
      "go test ./internal/providers/ticket/... green",
    ],
    files: ["internal/providers/ticket/mock_test.go"],
    tests: ["go test ./internal/providers/ticket/..."],
  });

  // ─── P13 Shoutrrr ─────────────────────────────────────────────────────────
  epic("P13", "Shoutrrr notifications", "backend", {
    depends_on: ["P10-03"],
    context:
      "Send notifications via github.com/containrrr/shoutrrr. Router selects global vs per-repo targets. " +
      "Templates for create, error, supersede events. POST /notification-targets/{id}/test sends probe message.",
    specs: [
      { ...SPEC.notifications, note: "§5.5 full notification flow" },
      { ...SPEC.domain, note: "Notify after ticket actions and poll errors" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/notifications/" },
      { ...STACK.data, note: "Decrypt URL at send time only" },
    ],
    implementation: [
      "internal/notifications/shoutrrr.go — Send(url, message)",
      "router.go — ResolveTargets(repo, event)",
      "templates.go — FormatCreate, FormatError, FormatSupersede",
      "POST /api/v1/notification-targets/{id}/test",
      "Filter by events_json and enabled",
      "Called from poll engine after actions",
    ],
    acceptance: [
      "Shoutrrr Send integration works",
      "Global targets when no repo filter",
      "Per-repo filter via join table",
      "events_json filters event type",
      "Test endpoint sends probe message",
      "Templates include repo name and tag",
      "Errors logged not fatal to poll",
    ],
  });

  leaf("P13-01", "P13", "Add Shoutrrr send wrapper", "backend", {
    context:
      "Thin wrapper around shoutrrr.Send(url, message). Decrypt URL from DB blob before send.",
    specs: [
      { ...SPEC.notifications, note: "Shoutrrr library in Go process" },
      { ...SPEC.schema, note: "shoutrrr_url_encrypted decrypt at send" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/notifications/shoutrrr.go" },
      { ...STACK.quality, note: "Mock shoutrrr in tests" },
    ],
    implementation: [
      "import github.com/containrrr/shoutrrr/pkg/router",
      "SendNotification(ctx, decryptedURL, title, message string) error",
      "router.Send(decryptedURL, message) — shoutrrr API",
      "Combine title + body for message string",
      "Timeout context 30s",
      "Log send failures at warn level",
    ],
    acceptance: [
      "Send calls shoutrrr with decrypted URL",
      "Invalid URL format returns error",
      "Context timeout respected",
      "Success returns nil",
      "Error wrapped with target name if available",
      "No URL in logs",
      "go test with injected sender interface",
    ],
    files: ["internal/notifications/shoutrrr.go", "internal/notifications/shoutrrr_test.go"],
    tests: ["go test ./internal/notifications/... -run Shoutrrr"],
  });

  leaf("P13-02", "P13", "Implement notification routing logic", "backend", {
    depends_on: ["P13-01"],
    context:
      "ResolveTargets(repoID, event): if repo has join rows, only those enabled targets matching event; else all global enabled targets matching event.",
    specs: [
      { ...SPEC.notifications, note: "Global vs per-repo routing" },
      { ...SPEC.schema, note: "monitored_repo_notifications join" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "Empty join = all global enabled" },
      { ...SCHEMA.html, note: "events_json filter" },
    ],
    implementation: [
      "internal/notifications/router.go",
      "ResolveTargets(ctx, repo, event string) ([]Target, error)",
      "Query join table — if rows exist, filter to those targets",
      "Else query all enabled notification_targets",
      "Filter targets where events_json contains event",
      "Decrypt URL for each target at send time",
    ],
    acceptance: [
      "Repo with links gets only linked targets",
      "Repo without links gets all global enabled",
      "Disabled targets excluded",
      "Event create only targets with create in events",
      "Empty result when no matching targets",
      "Decrypt called per target",
      "go test routing scenarios",
    ],
    files: ["internal/notifications/router.go", "internal/notifications/router_test.go"],
    tests: ["go test ./internal/notifications/... -run Router"],
  });

  leaf("P13-03", "P13", "Add event message templates", "backend", {
    depends_on: ["P13-01"],
    context:
      "Format messages for create (new ticket), error (poll failure), supersede (old ticket superseded). Include repo path, tag, ticket URL.",
    specs: [
      { ...SPEC.notifications, note: "Event types: create, error, supersede" },
      { ...SPEC.domain, note: "Notification content after poll actions" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/notifications/templates.go" },
      { ...STACK.quality, note: "Snapshot or string assert tests" },
    ],
    implementation: [
      "FormatCreate(repo, tag, ticketURL string) string",
      "FormatError(repo, errMsg string) string",
      "FormatSupersede(repo, oldTag, newTag, ticketURL string) string",
      "Consistent prefix: [Release Ops]",
      "Include project_path and source_kind",
      "Plain text for Shoutrrr compatibility",
    ],
    acceptance: [
      "FormatCreate includes tag and ticket URL",
      "FormatError includes repo and error message",
      "FormatSupersede mentions old and new tag",
      "All templates include [Release Ops] prefix",
      "No HTML in messages",
      "Empty URL handled gracefully",
      "go test template output",
    ],
    files: ["internal/notifications/templates.go", "internal/notifications/templates_test.go"],
    tests: ["go test ./internal/notifications/... -run Template"],
  });

  leaf("P13-04", "P13", "Implement POST /notification-targets/{id}/test", "backend", {
    depends_on: ["P13-01", "P10-02"],
    context:
      "Send probe message via Shoutrrr to verify target configuration. Decrypt URL, send test message, return { ok: true }.",
    specs: [
      { ...SPEC.api, note: "POST /notification-targets/{id}/test" },
      { ...SPEC.ui, note: "Test notification button P24-04" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/api/notifications/test.go" },
      { ...STACK.quality, note: "Mock sender in handler test" },
    ],
    implementation: [
      "TestNotificationTargetHandler",
      "Load target by id — 404",
      "Decrypt shoutrrr_url_encrypted",
      "SendNotification with message Release Ops test notification",
      "Return 200 { ok: true } or 400 on send failure",
      "r.Post(/notification-targets/{id}/test, handler)",
    ],
    acceptance: [
      "Success returns 200 { ok: true }",
      "Send failure returns 400 with error",
      "Unknown id 404",
      "Requires auth",
      "Uses decrypted URL only in memory",
      "Does not modify target row",
      "go test handler with mock sender",
    ],
    files: ["internal/api/notifications/test.go"],
    tests: ["go test ./internal/api/notifications/... -run TestTarget"],
  });

  leaf("P13-05", "P13", "Add Shoutrrr unit tests", "backend", {
    depends_on: ["P13-02"],
    context:
      "Unit tests for router, templates, and send wrapper with injected dependencies.",
    specs: [
      { ...SPEC.notifications, note: "Notification subsystem tests" },
      { ...SPEC.mvp, note: "Notification delivery in MVP AC" },
    ],
    stack: [
      { ...STACK.quality, note: "go test ./internal/notifications/..." },
      { ...STACK.backend, note: "Full package coverage" },
    ],
    implementation: [
      "TestRouterGlobalVsPerRepo",
      "TestRouterEventFilter",
      "TestTemplatesAllEvents",
      "TestSendWrapperWithMock",
      "Test end-to-end resolve + format (no real send)",
      "Table-driven routing cases",
    ],
    acceptance: [
      "go test ./internal/notifications/... passes",
      "Router global and per-repo cases",
      "Event filter edge cases",
      "Templates non-empty",
      "No real Shoutrrr network calls",
      "CI compatible",
      "Mock sender verifies message content",
    ],
    files: ["internal/notifications/notifications_test.go"],
    tests: ["go test ./internal/notifications/..."],
  });

  // ─── P14 Poll engine ──────────────────────────────────────────────────────
  epic("P14", "Poll engine & scheduler", "backend", {
    depends_on: ["P11-05", "P12-06", "P09-03", "P13-02"],
    context:
      "Core poll loop: for each enabled repo fetch latest release, compare tag to last_known_tag, apply baseline/skip/create, " +
      "handle open ticket policies (supersede|merge|skip_if_open), record poll_runs/events, send notifications. " +
      "Cron from poll_interval_minutes; mutex prevents parallel runs.",
    specs: [
      { ...SPEC.domain, note: "§5 Poll engine full logic" },
      { ...SPEC.ticketProjects, note: "on_open_ticket_policy per project" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "monitored_repos poll state columns" },
      { ...SCHEMA.html, note: "poll_run_events.action enum" },
    ],
    implementation: [
      "internal/poll/decision.go — baseline, skip, new tag",
      "ticket_status.go — live open ticket check via TicketProvider",
      "policy_supersede.go, policy_merge.go, policy_skip.go",
      "recorder.go — poll_runs and poll_run_events",
      "mutex.go — single running poll guard",
      "scheduler.go — cron from settings",
      "Integration tests baseline→create→supersede",
    ],
    acceptance: [
      "First sighting: baseline, no ticket, last_known_tag set",
      "Same tag: skip action",
      "New tag: create ticket",
      "supersede policy closes old, creates new",
      "merge policy adds comment to open ticket",
      "skip_if_open skips when open ticket exists",
      "No parallel poll runs",
      "Scheduler respects poll_interval_minutes",
    ],
  });

  leaf("P14-01", "P14", "Implement release decision engine", "backend", {
    context:
      "Pure function Decide(lastKnownTag, fetchedTag string, hadError bool) → action baseline|skip|create|error. " +
      "No ticket provider calls — tag comparison only.",
    specs: [
      { ...SPEC.domain, note: "Baseline vs genuine new release" },
      { ...SPEC.providers, note: "ErrNoRelease handling" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/poll/decision.go" },
      { ...STACK.quality, note: "Pure function unit tests" },
    ],
    implementation: [
      "type Decision string — baseline, skip, create, error",
      "Decide(lastKnown *string, fetchedTag string, fetchErr error) (Decision, string)",
      "lastKnown nil + success → baseline",
      "lastKnown == fetchedTag → skip",
      "lastKnown != fetchedTag → create",
      "fetchErr != nil → error with message",
      "ErrNoRelease from provider → skip or baseline per spec",
    ],
    acceptance: [
      "Nil lastKnown + tag v1.0 → baseline",
      "lastKnown v1.0 + fetched v1.0 → skip",
      "lastKnown v1.0 + fetched v1.1 → create",
      "Fetch error → error decision",
      "ErrNoRelease handled per spec (baseline if never seen)",
      "Tag comparison exact string match",
      "go test -run Decision passes",
    ],
    files: ["internal/poll/decision.go", "internal/poll/decision_test.go"],
    tests: ["go test ./internal/poll/... -run Decision"],
  });

  leaf("P14-02", "P14", "Add live open ticket status check", "backend", {
    depends_on: ["P12-02"],
    context:
      "When repo has open_ticket_external_id, call TicketProvider.GetStatus and ClassifyStatus. " +
      "Determine if ticket still open for policy decisions.",
    specs: [
      { ...SPEC.domain, note: "Open ticket detection before create" },
      { ...SPEC.ticketProjects, note: "status_mapping drives classification" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/poll/ticket_status.go" },
      { ...STACK.quality, note: "Mock TicketProvider" },
    ],
    implementation: [
      "CheckOpenTicket(ctx, provider, mapping, externalId) (isOpen bool, status string, err error)",
      "GetStatus from provider",
      "ClassifyStatus → open means isOpen true",
      "done/cancelled/superseded → isOpen false",
      "Provider error → return error (poll records error event)",
      "Clear open_ticket fields when ticket closed externally",
    ],
    acceptance: [
      "Open status returns isOpen true",
      "Done status returns isOpen false",
      "Provider error propagates",
      "Uses ticket_projects.status_mapping",
      "Unknown status treated as open (safe default) or per spec",
      "Nil external id returns isOpen false",
      "go test with mock provider",
    ],
    files: ["internal/poll/ticket_status.go", "internal/poll/ticket_status_test.go"],
    tests: ["go test ./internal/poll/... -run TicketStatus"],
  });

  leaf("P14-03", "P14", "Implement supersede policy", "backend", {
    depends_on: ["P14-02"],
    context:
      "Default policy: when open ticket exists and new tag detected, mark old ticket superseded status, create new ticket, update open_ticket_* fields.",
    specs: [
      { ...SPEC.domain, note: "supersede policy default" },
      { ...SPEC.ticketProjects, note: "on_open_ticket_policy supersede" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/poll/policy_supersede.go" },
      { ...STACK.quality, note: "Unit test with mock providers" },
    ],
    implementation: [
      "ApplySupersede(ctx, repo, provider, mapping, newTag, release) error",
      "UpdateTicket old id → superseded status from mapping",
      "CreateTicket for new release",
      "Update repo open_ticket_external_id, open_ticket_tag",
      "Record poll_run_event action supersede + create",
      "Send supersede and create notifications",
      "Update last_known_tag",
    ],
    acceptance: [
      "Old ticket gets superseded status",
      "New ticket created with new tag in title",
      "open_ticket_* fields updated",
      "poll_run_events: supersede then create",
      "Notifications sent for supersede and create",
      "last_known_tag updated to newTag",
      "go test supersede flow",
    ],
    files: ["internal/poll/policy_supersede.go", "internal/poll/policy_supersede_test.go"],
    tests: ["go test ./internal/poll/... -run Supersede"],
  });

  leaf("P14-04", "P14", "Implement merge policy", "backend", {
    depends_on: ["P14-02"],
    context:
      "merge policy: when open ticket exists, add comment with new release info, update last_known_tag, no new ticket. action merge.",
    specs: [
      { ...SPEC.domain, note: "merge policy behavior" },
      { ...SPEC.ticketProjects, note: "on_open_ticket_policy merge" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/poll/policy_merge.go" },
      { ...STACK.quality, note: "Mock AddComment" },
    ],
    implementation: [
      "ApplyMerge(ctx, repo, provider, newTag, release) error",
      "AddComment on open_ticket_external_id with release details",
      "Update last_known_tag",
      "Keep open_ticket_external_id unchanged",
      "Record poll_run_event action merge",
      "Optional create notification or skip per spec",
      "No new ticket created",
    ],
    acceptance: [
      "AddComment called with formatted release info",
      "No CreateTicket call",
      "last_known_tag updated",
      "open_ticket fields unchanged",
      "poll_run_event action merge",
      "Works when ticket still open",
      "go test merge policy",
    ],
    files: ["internal/poll/policy_merge.go", "internal/poll/policy_merge_test.go"],
    tests: ["go test ./internal/poll/... -run Merge"],
  });

  leaf("P14-05", "P14", "Implement skip_if_open policy", "backend", {
    depends_on: ["P14-02"],
    context:
      "skip_if_open: when open ticket exists and new tag, skip ticket creation, update last_known_tag, record skip_open event.",
    specs: [
      { ...SPEC.domain, note: "skip_if_open policy" },
      { ...SPEC.ticketProjects, note: "on_open_ticket_policy skip_if_open" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/poll/policy_skip.go" },
      { ...STACK.quality, note: "Unit test skip path" },
    ],
    implementation: [
      "ApplySkipIfOpen(ctx, repo, newTag) error",
      "Update last_known_tag to newTag",
      "Do not create or modify ticket",
      "Record poll_run_event action skip_open",
      "Log info: skipped due to open ticket",
      "No create notification",
    ],
    acceptance: [
      "No CreateTicket when open ticket exists",
      "last_known_tag still updated",
      "poll_run_event action skip_open",
      "open_ticket fields unchanged",
      "No supersede or merge calls",
      "Applies only when isOpen true",
      "go test skip_if_open",
    ],
    files: ["internal/poll/policy_skip.go", "internal/poll/policy_skip_test.go"],
    tests: ["go test ./internal/poll/... -run Skip"],
  });

  leaf("P14-06", "P14", "Record poll_runs and poll_run_events", "backend", {
    depends_on: ["P05-06"],
    context:
      "Recorder wraps repository poll_runs methods. Start run, append events per repo, finish with status and counters.",
    specs: [
      { ...SPEC.domain, note: "Poll run audit trail" },
      { ...SPEC.schema, note: "poll_runs status enum, poll_run_events.action" },
    ],
    schema: [
      { ...SCHEMA.sql, note: "poll_runs counters and errors_json" },
      { ...SCHEMA.html, note: "Event actions list" },
    ],
    implementation: [
      "internal/poll/recorder.go",
      "type Recorder struct { repo, runID string }",
      "StartRun(ctx) (runID, error) — status running",
      "RecordEvent(ctx, repoID, action, detail)",
      "FinishRun(ctx, status, counters, errors)",
      "errors_json accumulates repo-level errors",
    ],
    acceptance: [
      "StartRun inserts poll_runs status running",
      "RecordEvent inserts poll_run_events row",
      "FinishRun sets finished_at and final status",
      "Counters: repos_checked, tickets_created, tickets_superseded",
      "partial status when some repos error",
      "errors_json valid JSON array",
      "go test recorder lifecycle",
    ],
    files: ["internal/poll/recorder.go", "internal/poll/recorder_test.go"],
    tests: ["go test ./internal/poll/... -run Recorder"],
  });

  leaf("P14-07", "P14", "Add poll mutex and running guard", "backend", {
    context:
      "sync.Mutex or atomic flag prevents concurrent poll runs. Second trigger returns early or queues (spec: no parallel).",
    specs: [
      { ...SPEC.domain, note: "No parallel polling same run" },
      { ...SPEC.api, note: "POST /poll/trigger 202 when already running" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/poll/mutex.go" },
      { ...STACK.quality, note: "Concurrent test with goroutines" },
    ],
    implementation: [
      "type RunGuard struct { mu sync.Mutex; running bool }",
      "TryStart() bool — false if already running",
      "Finish() releases lock",
      "defer Finish in poll runner",
      "Expose IsRunning for status API P15",
      "Log when skipping duplicate start",
    ],
    acceptance: [
      "Second TryStart returns false while running",
      "Finish allows next run",
      "defer ensures unlock on panic",
      "Concurrent goroutine test passes",
      "IsRunning reflects state",
      "No deadlock on normal completion",
      "go test -run Guard passes",
    ],
    files: ["internal/poll/mutex.go", "internal/poll/mutex_test.go"],
    tests: ["go test ./internal/poll/... -run Guard"],
  });

  leaf("P14-08", "P14", "Add cron scheduler from poll_interval_minutes", "backend", {
    depends_on: ["P14-06", "P06-01"],
    context:
      "Background scheduler reads poll_interval_minutes from app_settings, runs poll on interval. " +
      "Use robfig/cron or time.Ticker. Reload interval when settings change.",
    specs: [
      { ...SPEC.domain, note: "Interval from app_settings" },
      { ...SPEC.api, note: "Settings change affects scheduler" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/poll/scheduler.go" },
      { ...STACK.deploy, note: "Scheduler runs in cmd/server process" },
    ],
    implementation: [
      "type Scheduler struct { engine *Engine; interval time.Duration }",
      "Start(ctx) — ticker every poll_interval_minutes",
      "On tick: guard.TryStart → engine.RunPoll → guard.Finish",
      "ReloadSettings() reads fresh interval from repo",
      "Start called from main.go after server ready",
      "Graceful stop on SIGTERM",
    ],
    acceptance: [
      "Poll runs automatically on interval",
      "Default 360 minutes from settings",
      "Manual trigger still works (P15)",
      "Scheduler stops on context cancel",
      "Interval reload after PATCH settings",
      "Missed tick does not stack runs (guard)",
      "go test scheduler with short interval mock",
    ],
    files: ["internal/poll/scheduler.go", "internal/poll/engine.go"],
    tests: ["go test ./internal/poll/... -run Scheduler"],
  });

  leaf("P14-10", "P14", "Build ticket title and description per spec §5.4", "backend", {
    depends_on: ["P11-01"],
    context:
      "Every ticket provider receives the same TicketInput built by the poll engine. " +
      "Title must be exactly `Release: {source_kind} {project_path} {tag}`. " +
      "Description is Markdown with release name, URL, and publishedAt per specs.html §5.4. " +
      "Used by create, merge, and supersede flows — single builder prevents provider drift.",
    specs: [
      { ...SPEC.domain, note: "§5.4 Ticket content (all providers)" },
      { ...SPEC.providers, note: "TicketInput passed to TicketProvider.CreateTicket" },
      { ...SPEC.ticketProjects, note: "create_config merged at create time" },
    ],
    stack: [
      { ...STACK.backend, note: "internal/poll/ticket_content.go" },
      { ...STACK.quality, note: "Table-driven unit tests for title/description" },
    ],
    implementation: [
      "func BuildTicketContent(repo MonitoredRepo, release Release) TicketInput",
      "Title: fmt.Sprintf(\"Release: %s %s %s\", sourceKind, projectPath, release.Tag)",
      "Description markdown: release name, html URL link, publishedAt RFC3339",
      "Include source_kind and project_path in body for operator context",
      "Export for policy_supersede, policy_merge, and create path",
      "Supersede comment template references same URL fields",
      "No provider-specific formatting in this package",
    ],
    acceptance: [
      "Title matches `Release: github org/repo v1.0.0` pattern exactly",
      "Description includes release.Name when present",
      "Description includes clickable/markdown URL to release.HTMLURL",
      "Description includes publishedAt in UTC ISO8601",
      "Works for all five source_kind values",
      "Empty release name still produces valid markdown body",
      "Unit tests cover GitHub-shaped Release struct",
      "Supersede comment builder reuses tag and URL helpers",
    ],
    files: ["internal/poll/ticket_content.go", "internal/poll/ticket_content_test.go"],
    tests: ["go test ./internal/poll/... -run TicketContent"],
    relatedTasks: ["P14-03", "P14-04", "P12-03"],
  });

  leaf("P14-09", "P14", "Add poll engine integration tests", "backend", {
    depends_on: ["P14-03", "P14-04", "P14-05", "P14-10"],
    context:
      "End-to-end test: mock source returns tags v1→v2, mock ticket provider, verify baseline→create→supersede scenario.",
    specs: [
      { ...SPEC.domain, note: "Full poll flow MVP scenario" },
      { ...SPEC.mvp, note: "baseline → create → supersede acceptance" },
    ],
    stack: [
      { ...STACK.quality, note: "go test -run Integration" },
      { ...STACK.backend, note: "In-memory DB + mock providers" },
    ],
    implementation: [
      "TestPollBaselineNoTicket",
      "TestPollCreateOnNewTag",
      "TestPollSupersedePolicy",
      "TestPollMergePolicy",
      "TestPollSkipIfOpenPolicy",
      "Seed repo, integration, ticket project in :memory: DB",
      "Mock SourceProvider tag sequence",
    ],
    acceptance: [
      "Baseline sets last_known_tag no ticket",
      "Second poll same tag skip",
      "New tag creates ticket",
      "Supersede flow updates old and creates new",
      "poll_run_events recorded correctly",
      "All integration tests pass",
      "No network calls",
      "CI runs go test ./internal/poll/... -run Integration",
    ],
    files: ["internal/poll/integration_test.go"],
    tests: ["go test ./internal/poll/... -run Integration"],
  });
}
