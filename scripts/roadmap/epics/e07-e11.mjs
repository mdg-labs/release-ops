import { SPEC, STACK, SCHEMA } from "../spec-links.mjs";

/** @param {{ epic: Function, leaf: Function }} reg */
export function registerE07E11({ epic, leaf }) {
  // ─── E07 Poll engine & notifications ──────────────────────────────────────
  epic("E07", "Poll engine, scheduler & Shoutrrr notifications", "backend", {
    depends_on: ["E05", "E06", "E04"],
    context:
      "Poll decision engine: baseline, skip, create, supersede, merge, skip_if_open. " +
      "Cron scheduler; poll_runs/events persistence; Shoutrrr on create/error/supersede. Merges P13+P14.",
    specs: [SPEC.domain, SPEC.notifications, SPEC.api],
    stack: [STACK.backend],
    acceptance: [
      "Scheduled poll respects poll_interval_minutes",
      "Manual POST /api/v1/poll/trigger works",
      "Notifications on configured events",
    ],
  });

  leaf("E07-01", "E07", "Poll decision engine and open-ticket policies", "backend", {
    depends_on: ["E06-01", "E05-01"],
    context:
      "Compare last_known_tag vs remote using exact string equality (§5.1); baseline on first sight; " +
      "skip when tags match; on new tag: live GetTicketStatus + ClassifyStatus per §5.2; " +
      "create/supersede/merge/skip_if_open per on_open_ticket_policy; supersede adds comment.",
    specs: [SPEC.domain, SPEC.ticketProjects],
    implementation: [
      "internal/poll/engine.go — EvaluateRepo",
      "Actions: baseline, skip, create, supersede, merge, skip_open, error",
      "Live ticket status via TicketProvider.GetTicketStatus + ClassifyStatus",
      "Update monitored_repos poll state",
    ],
    acceptance: [
      "baseline poll action on first poll",
      "skip poll when tag string equals last_known_tag",
      "create ticket on new release when no open ticket",
      "Live GetTicketStatus before applying open-ticket policy",
      "supersede: old ticket status + comment, new ticket created",
      "merge policy updates existing ticket via UpdateTicket",
      "skip_if_open policy skips duplicate",
    ],
    files: ["internal/poll/engine.go", "internal/poll/engine_test.go"],
    tests: ["go test ./internal/poll/..."],
  });

  leaf("E07-02", "E07", "Poll scheduler and run orchestration", "backend", {
    depends_on: ["E07-01", "E04-06"],
    context:
      "robfig/cron scheduler reading poll_interval_minutes from app_settings; " +
      "POST /api/v1/poll/trigger manual run.",
    specs: [SPEC.domain, SPEC.api, SPEC.deployment],
    implementation: [
      "internal/poll/scheduler.go",
      "RunAll: iterate enabled repos, call engine",
      "Wire trigger handler to scheduler",
    ],
    acceptance: [
      "Scheduler uses robfig/cron from poll_interval_minutes",
      "Manual poll via POST /api/v1/poll/trigger",
      "Single poll run at a time (mutex)",
      "poll_runs row per execution",
      "repos_checked counter incremented",
      "Cron respects min 5 minute interval",
      "Graceful stop on shutdown",
    ],
    files: ["internal/poll/scheduler.go"],
    tests: ["go test ./internal/poll/..."],
  });

  leaf("E07-03", "E07", "Poll run events and persistence", "backend", {
    depends_on: ["E07-02", "E03-02"],
    context: "poll_runs and poll_run_events tables; GET /api/v1/poll/runs and /poll/runs/{id} data source.",
    specs: [SPEC.schema, SPEC.api],
    schema: [SCHEMA.sql],
    implementation: [
      "Insert poll_run on start, finish on complete",
      "poll_run_events per repo action",
      "errors_json array on failed runs",
    ],
    acceptance: [
      "poll_run_events.action enum values persisted",
      "GET /api/v1/poll/runs returns list",
      "GET /api/v1/poll/runs/{id} includes events",
      "status running|success|partial|failed",
      "tickets_created counter accurate",
      "tickets_superseded counter incremented on supersede",
      "errors_json array on partial/failed runs",
    ],
    files: ["internal/poll/run.go"],
    tests: ["go test ./internal/poll/..."],
  });

  leaf("E07-04", "E07", "Shoutrrr notification dispatcher", "backend", {
    depends_on: ["E07-01", "E03-02"],
    context: "github.com/containrrr/shoutrrr; events create, error, supersede; per-repo routing.",
    specs: [SPEC.notifications, SPEC.domain],
    implementation: [
      "internal/poll/notify.go",
      "Resolve targets: global enabled + repo overrides",
      "Decrypt shoutrrr_url at send time",
    ],
    acceptance: [
      "Shoutrrr URL schemes supported",
      "Notification on create event",
      "Notification on poll error",
      "Notification on supersede",
      "events_json filters delivery",
      "Failed notify does not fail poll",
      "No URL in logs",
    ],
    files: ["internal/poll/notify.go"],
    tests: ["go test ./internal/poll/..."],
  });

  leaf("E07-05", "E07", "End-to-end poll integration test", "backend", {
    depends_on: ["E07-03", "E07-04", "E06-03"],
    context: "Integration test: baseline → new tag → create ticket → supersede; mock providers.",
    specs: [SPEC.mvp, SPEC.domain],
    implementation: ["internal/poll/integration_test.go"],
    acceptance: [
      "Baseline then create flow in test",
      "Mock source returns sequential tags",
      "Mock ticket provider records creates",
      "poll_run_events assert create action",
      "supersede policy test case",
      "CI runs go test ./internal/poll/...",
      "No flaky timing in test",
    ],
    files: ["internal/poll/integration_test.go"],
    tests: ["go test ./internal/poll/..."],
  });

  // ─── E08 Web platform ─────────────────────────────────────────────────────
  epic("E08", "Web platform — proxy, hooks, shell & i18n", "web", {
    depends_on: ["E01", "E04"],
    context:
      "Next.js /api/go proxy, React Query hooks for all §8.4 endpoints, app shell with sidebar/logout, " +
      "next-intl + ESLint i18n. Merges P16–P18, P17, P00 i18n.",
    specs: [SPEC.ui, SPEC.i18n, SPEC.api],
    stack: [STACK.frontend, STACK.layout],
    acceptance: [
      "All API calls via /api/go/api/v1",
      "Zero hardcoded UI strings",
      "Sidebar nav matches §8.1",
    ],
  });

  leaf("E08-01", "E08", "next-intl setup and ESLint i18n enforcement", "web", {
    depends_on: ["E01-01"],
    context: "apps/web/messages/en.json; useTranslations; eslint-plugin-i18next no-literal-string.",
    specs: [SPEC.i18n, SPEC.ci],
    stack: [STACK.frontend, STACK.quality],
    implementation: [
      "next-intl App Router provider",
      "messages/en.json namespaces: common, auth, dashboard, repos, integrations, " +
      "ticket-projects, notifications, settings",
      "eslint-plugin-i18next in eslint.config.mjs",
    ],
    acceptance: [
      "next-intl installed and configured",
      "messages/en.json with namespaces: common, auth, dashboard, repos",
      "messages/en.json includes integrations, ticket-projects, notifications, settings",
      "eslint-plugin-i18next configured",
      "i18next/no-literal-string error on literals",
      "npm run lint passes on scaffold",
      "Document in .cursor/rules/10-i18n.mdc",
    ],
    files: ["apps/web/messages/en.json", "apps/web/i18n/request.ts", "apps/web/eslint.config.mjs"],
    tests: ["npm run lint"],
  });

  leaf("E08-02", "E08", "Go API proxy and fetch client", "web", {
    depends_on: ["E01-01"],
    context: "apps/web/app/api/go/[...path]/route.ts forwards to Go; credentials include; cookie relay.",
    specs: [SPEC.ui, SPEC.auth, SPEC.architecture],
    implementation: [
      "Catch-all proxy /api/go/* → GO_API_URL",
      "lib/api/client.ts — prefix /api/go/api/v1",
      "route.test.ts with mock upstream",
    ],
    acceptance: [
      "/api/go proxy forwards session cookie",
      "POST /api/go/api/v1/auth/login works",
      "Error envelope parsed from Go",
      "Proxy test with undici MockAgent",
      "Methods GET POST PATCH DELETE forwarded",
      "Content-Type application/json set",
      "Same-origin requests from browser",
    ],
    files: ["apps/web/app/api/go/[...path]/route.ts", "apps/web/lib/api/client.ts"],
    tests: ["npm test -- proxy"],
  });

  leaf("E08-03", "E08", "React Query hooks for all API endpoints", "web", {
    depends_on: ["E08-02", "E04-07"],
    context:
      "useSession, useStatus, useRepos, useIntegrations, useTicketProjects, useNotificationTargets, " +
      "useSettings, usePollRuns, usePollRun, useTriggerPoll per §8.4.",
    specs: [SPEC.ui, SPEC.api],
    implementation: [
      "apps/web/lib/hooks/use-session.ts, use-status.ts, use-repos.ts",
      "use-integrations.ts, use-ticket-projects.ts, use-notifications.ts",
      "use-settings.ts, use-poll.ts",
    ],
    acceptance: [
      "useSession fetches GET /api/v1/auth/session",
      "useStatus fetches GET /api/v1/status",
      "useRepos CRUD mutations",
      "useIntegrations includes test mutation",
      "usePollRuns fetches paginated run list",
      "usePollRun fetches single run with events[]",
      "useTriggerPoll POST /api/v1/poll/trigger",
    ],
    files: ["apps/web/lib/hooks/"],
    tests: ["npm test -- hooks"],
  });

  leaf("E08-04", "E08", "App shell, sidebar navigation, and logout", "web", {
    depends_on: ["E08-03", "E08-01"],
    context:
      "COSS app shell per §8.1: SidebarProvider, Frame (p-frame-3), Breadcrumb (p-breadcrumb-3), " +
      "global Toaster (p-toast-2) and Spinner (p-spinner-1) in root layout; sidebar links " +
      "/, /repos, /integrations, /ticket-projects, /notifications, /settings; " +
      "logout POST /api/v1/auth/logout; required sidebar logout control.",
    specs: [SPEC.ui, SPEC.auth],
    implementation: [
      "app/(authenticated)/layout.tsx with SidebarProvider + AppFrame",
      "components/app-frame.tsx — p-frame-3, p-breadcrumb-3",
      "root layout mounts <Toaster /> (p-toast-2) and Spinner (p-spinner-1)",
      "components/app-sidebar.tsx — next/link nav",
      "Logout button calls auth logout",
    ],
    acceptance: [
      "Sidebar links match §8.1 routes",
      "Frame wraps page content with breadcrumb slot (p-frame-3)",
      "Breadcrumb home icon per p-breadcrumb-3",
      "Toaster mounted once in root layout for mutation errors",
      "Spinner for async loading states",
      "logout UI control in sidebar",
      "All nav labels use t() keys",
    ],
    files: ["apps/web/components/app-sidebar.tsx", "apps/web/app/(authenticated)/layout.tsx"],
    tests: ["npm test -- sidebar"],
  });

  leaf("E08-05", "E08", "Authenticated routing and Next.js middleware", "web", {
    depends_on: ["E08-04"],
    context: "middleware.ts redirects unauthenticated users to /login; public /login route.",
    specs: [SPEC.auth, SPEC.ui],
    implementation: [
      "apps/web/middleware.ts",
      "app/login/page.tsx placeholder shell",
      "app/(authenticated)/ route group",
    ],
    acceptance: [
      "/login accessible without session",
      "Protected pages redirect to /login",
      "GET /api/go/api/v1/auth/session check in middleware",
      "Static assets excluded from middleware",
      "Authenticated layout wraps dashboard pages",
      "matcher config correct",
      "No flash of protected content",
    ],
    files: ["apps/web/middleware.ts", "apps/web/app/login/page.tsx"],
    tests: ["npm test"],
  });

  // ─── E09 Web config UI ────────────────────────────────────────────────────
  epic("E09", "Web config UI — login and settings pages", "web", {
    depends_on: ["E08", "E04"],
    context: "COSS UI for /login, /integrations, /ticket-projects, /repos, /notifications, /settings. Merges P19,P21–P25.",
    specs: [SPEC.ui],
    stack: [STACK.frontend],
    acceptance: ["All §8.3 pages implemented", "i18n keys only", "CRUD via hooks"],
  });

  leaf("E09-01", "E09", "Login page with email/password form", "web", {
    depends_on: ["E08-05", "E08-03"],
    context:
      "/login page per §8.3: p-card-1 centered container, p-field-2 email/password, p-button-1 submit; " +
      "p-alert-7 on login error; POST /api/go/api/v1/auth/login; redirect to / on success.",
    specs: [SPEC.ui, SPEC.auth],
    implementation: [
      "app/login/page.tsx — p-card-1, p-field-2, p-button-1, p-alert-7",
    ],
    acceptance: [
      "/login page renders centered p-card-1 form",
      "Email and password fields i18n via p-field-2",
      "Submit calls login mutation",
      "Invalid credentials show p-alert-7 error (not toast only)",
      "Redirect to / on success",
      "Already logged in redirects away",
      "Accessible labels and focus management",
    ],
    files: ["apps/web/app/login/page.tsx"],
    tests: ["npm test -- login"],
  });

  leaf("E09-02", "E09", "Integrations page — list, drawer CRUD, test connection", "web", {
    depends_on: ["E09-01", "E08-03"],
    context:
      "/integrations — p-table-2, p-drawer-10; all 8 kinds; Jira email field (p-field-2); " +
      "hasSecret indicator; test connection p-toast-5 async feedback.",
    specs: [SPEC.ui, SPEC.providers],
    implementation: ["app/(authenticated)/integrations/page.tsx"],
    acceptance: [
      "/integrations page lists integrations",
      "Create/edit drawer per kind with base_url when required",
      "Jira kind shows email field (p-field-2)",
      "hasSecret shown; token password field never re-displayed",
      "Test connection button with p-toast-5 async feedback",
      "Delete confirmation i18n",
      "useIntegrations hook wired",
    ],
    files: ["apps/web/app/(authenticated)/integrations/page.tsx"],
    tests: ["npm test -- integrations"],
  });

  leaf("E09-03", "E09", "Ticket projects page — status mapping and policies", "web", {
    depends_on: ["E09-02"],
    context: "/ticket-projects — create_config tabs, status_mapping arrays, on_open_ticket_policy select.",
    specs: [SPEC.ui, SPEC.ticketProjects],
    implementation: ["app/(authenticated)/ticket-projects/page.tsx"],
    acceptance: [
      "/ticket-projects page CRUD",
      "Integration select ticket kinds only",
      "on_open_ticket_policy supersede|merge|skip_if_open",
      "status_mapping per open/done/cancelled",
      "create_config subform per provider",
      "useTicketProjects hook",
      "JSON validation errors shown",
    ],
    files: ["apps/web/app/(authenticated)/ticket-projects/page.tsx"],
    tests: ["npm test"],
  });

  leaf("E09-04", "E09", "Monitored repos page", "web", {
    depends_on: ["E09-03"],
    context:
      "/repos per §8.3 — p-table-3 list, p-dialog-1 add/edit, p-select-1 for source/ticket project; " +
      "p-checkbox-group-1 optional notification targets per monitored_repo_notifications; p-switch-1 enabled.",
    specs: [SPEC.ui, SPEC.domain],
    implementation: ["app/(authenticated)/repos/page.tsx"],
    acceptance: [
      "/repos page lists monitored repos (p-table-3, no bulk checkboxes in MVP)",
      "Add/edit dialog with source_kind and ticket_project selects",
      "sourceIntegrationId select for gitlab/gitea/forgejo",
      "Optional notification targets checkbox group per repo",
      "enabled toggle (p-switch-1) and p-alert-dialog-1 delete confirm",
      "Shows last_polled_at and last_error",
      "useRepos hook; empty state i18n",
    ],
    files: ["apps/web/app/(authenticated)/repos/page.tsx"],
    tests: ["npm test"],
  });

  leaf("E09-05", "E09", "Notifications page", "web", {
    depends_on: ["E09-01", "E08-03"],
    context: "/notifications — Shoutrrr URL, events checkboxes, test notification.",
    specs: [SPEC.ui, SPEC.notifications],
    implementation: ["app/(authenticated)/notifications/page.tsx"],
    acceptance: [
      "/notifications page CRUD",
      "Shoutrrr URL password input",
      "events_json checkbox group create,error,supersede",
      "Test notification button",
      "useNotificationTargets hook",
      "enabled toggle",
      "Never show stored URL after save",
    ],
    files: ["apps/web/app/(authenticated)/notifications/page.tsx"],
    tests: ["npm test"],
  });

  leaf("E09-06", "E09", "Settings page — poll interval", "web", {
    depends_on: ["E09-01", "E08-03"],
    context: "/settings — p-number-field poll_interval_minutes min 5; PATCH /api/v1/settings.",
    specs: [SPEC.ui, SPEC.api],
    implementation: ["app/(authenticated)/settings/page.tsx"],
    acceptance: [
      "/settings page shows poll interval",
      "Min 5 minutes validation",
      "Save calls PATCH /api/v1/settings",
      "Success alert i18n",
      "useSettings hook",
      "Loading and error states",
      "p-card-8 layout per spec particles",
    ],
    files: ["apps/web/app/(authenticated)/settings/page.tsx"],
    tests: ["npm test"],
  });

  // ─── E10 Dashboard ────────────────────────────────────────────────────────
  epic("E10", "Dashboard — status, manual poll & run history", "web", {
    depends_on: ["E08", "E07"],
    context: "Dashboard / with status card, manual poll button, poll run history UI. Merges P20.",
    specs: [SPEC.ui, SPEC.mvp],
    acceptance: ["AC #14 operational visibility", "Manual poll from UI"],
  });

  leaf("E10-01", "E10", "Dashboard status card, repo table, and manual poll", "web", {
    depends_on: ["E09-01", "E08-03"],
    context:
      "GET /api/v1/status display; repo status subset table (p-table-4); error alert (p-alert-6) " +
      "from last run; POST /api/v1/poll/trigger button; useStatus, useRepos, useTriggerPoll.",
    specs: [SPEC.ui, SPEC.api, SPEC.mvp],
    implementation: [
      "app/(authenticated)/page.tsx",
      "components/dashboard/status-card.tsx — p-card-10, p-badge-5",
      "components/dashboard/repo-status-table.tsx — p-table-4 TanStack sort+pagination",
      "components/dashboard/last-run-errors.tsx — p-alert-6 when errors_json present",
    ],
    acceptance: [
      "Dashboard / shows system status card",
      "Repo status table (subset) with source, path, last tag, open ticket",
      "Error alert when last poll run has errors",
      "Manual poll button triggers POST /api/v1/poll/trigger",
      "Empty state p-empty-1 CTA links to /repos",
      "Loading state during poll with Spinner",
      "All dashboard strings use t() keys",
    ],
    files: ["apps/web/app/(authenticated)/page.tsx"],
    tests: ["npm test -- dashboard"],
  });

  leaf("E10-02", "E10", "Poll run history table and run detail drawer", "web", {
    depends_on: ["E10-01", "E08-03"],
    context:
      "GET /api/v1/poll/runs table; row click GET /api/v1/poll/runs/{id} drawer with events; " +
      "usePollRuns, usePollRun hooks.",
    specs: [SPEC.ui, SPEC.api, SPEC.domain],
    implementation: ["components/dashboard/poll-run-history.tsx"],
    acceptance: [
      "Poll run history UI consumes GET poll/runs",
      "Table data from GET /api/v1/poll/runs via usePollRuns",
      "Row opens drawer with usePollRun(id)",
      "Events show action enum labels i18n",
      "Status badges running|success|partial|failed",
      "Pagination or limit",
      "Empty state when no runs",
    ],
    files: ["apps/web/components/dashboard/poll-run-history.tsx"],
    tests: ["npm test"],
  });

  leaf("E10-03", "E10", "Dashboard page integration and smoke tests", "web", {
    depends_on: ["E10-02"],
    context: "Vitest/RTL smoke for dashboard; docker compose manual poll AC #14.",
    specs: [SPEC.mvp, SPEC.ci],
    implementation: ["dashboard page tests"],
    acceptance: [
      "Dashboard renders status + history",
      "Manual poll flow tested mock API",
      "All dashboard copy uses t()",
      "npm test passes",
      "No hardcoded English in dashboard",
      "Links to config pages from empty states",
      "Responsive layout mobile",
    ],
    files: ["apps/web/app/(authenticated)/page.test.tsx"],
    tests: ["npm test"],
  });

  // ─── E11 Ship ─────────────────────────────────────────────────────────────
  epic("E11", "Ship — Docker, CI/CD & MVP verification", "ci", {
    depends_on: ["E07", "E10"],
    context: "Dockerfile, compose, GitHub Actions §11, GHCR, MVP checklist. Merges P26+P27+P28.",
    specs: [SPEC.deployment, SPEC.ci, SPEC.mvp],
    stack: [STACK.cicd, STACK.deploy],
    acceptance: [
      "docker compose up works",
      "CI workflows per §11.2",
      "All 15 MVP AC documented",
    ],
  });

  leaf("E11-01", "E11", "Multi-stage Dockerfile and entrypoint", "ci", {
    depends_on: ["E01", "E07"],
    context:
      "Next standalone + Go binary; docker/entrypoint.sh: migrate up → Go background → Next foreground; " +
      "single image. Non-root user when feasible.",
    specs: [SPEC.deployment, SPEC.schemaMigrations],
    implementation: [
      "Dockerfile multi-stage",
      "docker/entrypoint.sh — golang-migrate up, start Go, start Next",
      "next.config standalone output",
    ],
    acceptance: [
      "Dockerfile builds successfully",
      "Single image release-ops",
      "Entrypoint runs migrate up then Go then Next",
      "Go on 127.0.0.1:8080 inside container",
      "Next on :3000 public",
      "VERSION not baked as secret",
      "Non-root user if feasible",
    ],
    files: ["Dockerfile", "docker/entrypoint.sh"],
    tests: ["docker build -t release-ops ."],
  });

  leaf("E11-02", "E11", "docker-compose.yml single service", "ci", {
    depends_on: ["E11-01"],
    context: "One service release-ops; volume /data; port 3000; env from example.",
    specs: [SPEC.deployment, SPEC.mvp],
    implementation: ["docker-compose.yml", ".env.example"],
    acceptance: [
      "docker-compose single service release-ops",
      "Volume release-ops-data:/data",
      "Port 3000:3000",
      "SESSION_SECRET and APP_ENCRYPTION_KEY documented",
      "Health via GET /api/go/healthz or proxy",
      "docker compose up --build works",
      "AC #14 docker compose dashboard",
    ],
    files: ["docker-compose.yml", ".env.example"],
    tests: ["docker compose config"],
  });

  leaf("E11-03", "E11", "GitHub Actions CI workflows", "ci", {
    depends_on: ["E01"],
    context: "pr.yml, dev.yml, main.yml, release.yml, reusable ci.yml per §11.2.",
    specs: [SPEC.ci, SPEC.mvp],
    implementation: [
      ".github/workflows/ci.yml — go lint/test/build, web lint/test/typecheck/build",
      ".github/workflows/pr.yml, dev.yml, main.yml, release.yml",
    ],
    acceptance: [
      ".github/workflows per §11.2 present",
      "Reusable workflows workflow_call only",
      "concurrency cancel-in-progress on PR/dev/main",
      "Parallel go and web jobs",
      "PR blocked on CI failure",
      "go test ./... in CI",
      "npm test and npm run typecheck in CI",
    ],
    files: [".github/workflows/ci.yml", ".github/workflows/pr.yml"],
    tests: ["actionlint if available"],
  });

  leaf("E11-04", "E11", "GHCR image push and prepare-release", "ci", {
    depends_on: ["E11-03"],
    context: "dev push nightly+sha; release published latest+version; prepare-release on VERSION bump.",
    specs: [SPEC.ci, SPEC.deployment],
    implementation: [
      "build-and-push-image.yml reusable",
      "prepare-release.yml + scripts/ci/create-draft-release.mjs",
      "dev.yml chains ci → push-image",
    ],
    acceptance: [
      "dev push produces nightly image after CI",
      "Published release pushes latest + version tag",
      "main push runs prepare-release on VERSION bump",
      "create-draft-release script semver compare",
      "GHCR auth via GITHUB_TOKEN",
      "Docker buildcache ghcr.io/{owner}/release-ops:buildcache",
      "No image build on main without release",
    ],
    files: [".github/workflows/build-and-push-image.yml", "scripts/ci/create-draft-release.mjs"],
    tests: ["node scripts/ci/create-draft-release.mjs --dry-run"],
  });

  leaf("E11-05", "E11", "CI policy gates — db:check and i18n lint", "ci", {
    depends_on: ["E11-03", "E01-02", "E08-01"],
    context: "ci.yml db-migrations job npm run db:check; web-lint enforces i18next rule.",
    specs: [SPEC.ci, SPEC.i18n, SPEC.schemaMigrations],
    implementation: [
      "db-migrations job in ci.yml",
      "sqlite3 in CI for sqldiff",
      "Root package.json db:check script",
    ],
    acceptance: [
      "db:check in CI workflow",
      "CI fails when db/schema.sql drifts",
      "web-lint fails on hardcoded JSX string",
      "Policy gates on PR and dev push",
      "sqlite3 available in runner",
      "npm run db:check documented",
      "No Atlas in pipeline",
    ],
    files: [".github/workflows/ci.yml", "package.json"],
    tests: ["npm run db:check"],
  });

  leaf("E11-06", "E11", "MVP checklist, getting-started docs, and sign-off", "docs", {
    depends_on: ["E11-02", "E11-04", "E07-05"],
    context:
      "docs/mvp-checklist.md maps AC #1–15; docs/getting-started.md operator guide including " +
      "docker compose up and docker exec seed-admin for manual admin creation.",
    specs: [SPEC.mvp, SPEC.deployment, SPEC.env],
    implementation: [
      "docs/mvp-checklist.md",
      "docs/getting-started.md",
      "Provider mock tests verified in CI",
    ],
    acceptance: [
      "mvp-checklist.md covers all 15 AC",
      "getting-started.md docker compose up",
      "Documents required env vars §9",
      "Documents docker exec seed-admin CLI usage",
      "Mock provider tests in CI AC #13",
      "Manual poll + dashboard AC #14",
      "GitHub Actions workflows AC #15",
    ],
    files: ["docs/mvp-checklist.md", "docs/getting-started.md"],
    tests: ["npm test", "go test ./..."],
  });
}
