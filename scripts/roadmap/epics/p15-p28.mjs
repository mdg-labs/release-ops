import { SPEC, STACK, SCHEMA } from "../spec-links.mjs";

/** @typedef {import("../registry.mjs").createRegistry extends (...args: any) => infer R ? R : never} Registry */

const STACK_KEYS = /** @type {const} */ ([
  "layout",
  "backend",
  "frontend",
  "data",
  "quality",
  "cicd",
  "deploy",
]);

/** @param {Partial<Record<(typeof STACK_KEYS)[number], string>>} notes */
function stackRefs(notes) {
  return STACK_KEYS.map((key) => ({
    ...STACK[key],
    note: notes[key] ?? STACK[key].label,
  }));
}

/** @param {Registry} reg */
function registerP15({ epic, leaf }) {
  epic("P15", "Status & poll REST API", "backend", {
    depends_on: ["P14-08"],
    context:
      "Expose operational visibility and manual poll control through the Go REST API defined in specs.html §7.1. " +
      "These endpoints power the dashboard summary card, manual poll button, and run history views in the web UI. " +
      "Implementation must aggregate poll state, monitored repos, and in-flight poll status from the store layer built in prior phases. " +
      "All responses use the standard JSON error envelope and require an authenticated session except where auth middleware already gates the router.",
    specs: [
      { ...SPEC.api, note: "§7.1 Status — GET /status, POST /poll/trigger, GET /poll/runs" },
      { ...SPEC.domain, note: "Poll run lifecycle and repo status fields in aggregated JSON" },
      { ...SPEC.architecture, note: "Go API on :8080; Next proxies via /api/go" },
      { ...SPEC.mvp, note: "Dashboard and manual poll depend on these endpoints" },
    ],
    stack: stackRefs({
      layout: "Handlers in internal/api; routes registered from cmd/server",
      backend: "Go chi router, sqlc store queries for runs and repos",
      frontend: "Consumed later by useStatus and dashboard (P17, P20)",
      data: "Reads poll_runs, poll_run_events, monitored_repos, settings",
      quality: "Table-driven httptest coverage in P15-05",
      cicd: "go test ./internal/api/... in ci.yml",
      deploy: "Same binary as rest of API; no separate service",
    }),
    schema: [
      { ...SCHEMA.sql, note: "poll_runs, poll_run_events, monitored_repos tables" },
      { ...SCHEMA.html, note: "Browser reference for poll and repo columns" },
    ],
    implementation: [
      "Register status and poll routes on the authenticated API sub-router",
      "Implement GET /api/v1/status aggregating settings.poll_interval, last run, repos[], isPolling",
      "POST /api/v1/poll/trigger starts async poll worker; return 202 with run id when accepted",
      "GET /api/v1/poll/runs supports limit/offset pagination (default limit 20)",
      "GET /api/v1/poll/runs/{id} returns run row plus events[] ordered by time",
      "Map store errors to VALIDATION_ERROR / NOT_FOUND per §7 error format",
      "Set isPolling true while a run is in progress (mutex or DB flag)",
      "Reuse poll service from internal/poll; handlers stay thin",
    ],
    acceptance: [
      "GET /api/v1/status returns JSON matching §7.1 example shape",
      "Status includes pollIntervalMinutes, lastRun, repos[], isPolling",
      "POST /api/v1/poll/trigger returns 202 when poll starts",
      "POST /api/v1/poll/trigger returns 409 or 429 when poll already running (per domain rules)",
      "GET /api/v1/poll/runs returns paginated list defaulting to 20 items",
      "GET /api/v1/poll/runs/{id} includes events array for the run",
      "Unauthenticated requests receive 401 on all status/poll routes",
      "Invalid run id returns 404 with standard error envelope",
    ],
  });

  leaf("P15-01", "P15", "Implement GET /api/v1/status", "backend", {
    context:
      "The status endpoint is the primary read model for the dashboard. It must join settings, the most recent completed or in-progress poll run, and every monitored repo with ticket and tag metadata. " +
      "Field names and nesting must match specs.html §7.1 exactly so the React client can deserialize without adapters. " +
      "This handler is read-only and should be efficient enough for frequent polling from the UI.",
    specs: [
      { ...SPEC.api, note: "§7.1 GET /api/v1/status JSON schema" },
      { ...SPEC.domain, note: "Repo lastKnownTag, openTicketExternalId, lastError fields" },
      { ...SPEC.schema, note: "monitored_repos and poll_runs columns" },
    ],
    stack: stackRefs({
      layout: "internal/api/status.go",
      backend: "Single handler calling store.ListReposWithStatus and store.LastPollRun",
      frontend: "Response consumed by useStatus hook (P17-03)",
      data: "SQL joins across repos, ticket_projects, poll state",
      quality: "Golden JSON fixture test",
      cicd: "Covered by go test in P15-05",
      deploy: "No env-specific behavior beyond DB path",
    }),
    schema: [
      { ...SCHEMA.sql, note: "monitored_repos, poll_runs, ticket_projects" },
    ],
    implementation: [
      "Add StatusHandler in internal/api/status.go",
      "Query poll_interval_minutes from settings singleton",
      "Load last run summary (id, startedAt, finishedAt, status, counters, errors)",
      "Map each monitored repo to status DTO with sourceKind, projectPath, ticket project name",
      "Include openTicketExternalId, openTicketTag, lastKnownTag, lastPolledAt, lastError",
      "Set isPolling from poll service or active run row",
      "Return 200 application/json; no secrets in response",
    ],
    acceptance: [
      "GET /api/v1/status returns 200 for authenticated session",
      "Response contains pollIntervalMinutes integer",
      "lastRun object present when at least one run exists (null otherwise)",
      "repos array includes all monitored repos with enabled flag",
      "Each repo item includes sourceKind, projectPath, ticketProjectId, ticketProjectName",
      "isPolling reflects true while worker is active",
      "JSON field names use camelCase per §7.1",
      "No integration secrets or tokens in response body",
    ],
    files: ["internal/api/status.go"],
    relatedTasks: ["P17-03", "P20-01"],
  });

  leaf("P15-02", "P15", "Implement POST /api/v1/poll/trigger", "backend", {
    context:
      "Manual poll lets admins force an immediate check outside the scheduled interval. The endpoint must start the poll worker asynchronously and return 202 Accepted with a run identifier the UI can track. " +
      "Concurrent manual triggers while a poll is running should be rejected cleanly per domain rules. " +
      "This reuses the same poll pipeline as the background scheduler from P14.",
    specs: [
      { ...SPEC.api, note: "POST /api/v1/poll/trigger — async 202" },
      { ...SPEC.domain, note: "Poll worker entry and run record creation" },
      { ...SPEC.architecture, note: "Single Go process runs scheduler and API" },
    ],
    stack: stackRefs({
      layout: "internal/api/poll.go",
      backend: "Delegate to internal/poll.Service.TriggerManual",
      frontend: "Called from dashboard Run poll now button (P20-02)",
      data: "Inserts poll_runs row with status running",
      quality: "Test 202 and conflict when already polling",
      cicd: "go test -run Trigger",
      deploy: "No extra configuration beyond poll interval in DB",
    }),
    schema: [{ ...SCHEMA.sql, note: "poll_runs status enum and timestamps" }],
    implementation: [
      "Add TriggerPollHandler in internal/api/poll.go",
      "Check poll service IsRunning; return 409 if busy",
      "Create poll_runs row and spawn goroutine via poll.Service",
      "Return 202 with { runId } or Location header per project convention",
      "Log trigger with user/session context for audit",
      "Ensure scheduler does not double-start same run",
    ],
    acceptance: [
      "POST /api/v1/poll/trigger returns 202 when poll starts",
      "Response body includes new run id",
      "Second concurrent trigger returns 409 or 429 (documented choice)",
      "Unauthenticated request returns 401",
      "Started run appears in GET /poll/runs within one request cycle",
      "isPolling becomes true in GET /status after trigger",
      "Worker completes and updates run status to success or error",
      "Invalid method GET on trigger path returns 405",
    ],
    files: ["internal/api/poll.go"],
    relatedTasks: ["P14-08", "P20-02"],
  });

  leaf("P15-03", "P15", "Implement GET /api/v1/poll/runs", "backend", {
    context:
      "Run history supports debugging and future UI expansion beyond the dashboard last-run card. The list endpoint returns recent poll runs with pagination defaulting to 20 items per specs.html §7.1. " +
      "Each list item should include summary counters without the full events array to keep payloads small. " +
      "Ordering is newest-first by startedAt.",
    specs: [
      { ...SPEC.api, note: "GET /api/v1/poll/runs paginated default limit 20" },
      { ...SPEC.schema, note: "poll_runs table columns" },
      { ...SPEC.domain, note: "Run status values: running, success, error" },
    ],
    stack: stackRefs({
      layout: "internal/api/poll.go",
      backend: "ListPollRuns store query with LIMIT/OFFSET",
      frontend: "Optional future run history UI",
      data: "Index on poll_runs.started_at for sort",
      quality: "Pagination edge-case tests",
      cicd: "Included in api test suite",
      deploy: "N/A",
    }),
    schema: [{ ...SCHEMA.sql, note: "poll_runs" }],
    implementation: [
      "Parse limit (default 20, max 100) and offset query params",
      "Return array of run summaries: id, startedAt, finishedAt, status, counters",
      "Omit events[] from list response",
      "Include total count or hasMore if spec requires (optional X-Total-Count)",
      "Validate limit is positive integer",
      "Return empty array when no runs exist",
    ],
    acceptance: [
      "GET /api/v1/poll/runs returns 200 with JSON array",
      "Default page size is 20 runs",
      "limit query param overrides default when valid",
      "Runs ordered by startedAt descending",
      "Each item includes id, status, reposChecked, ticketsCreated, ticketsSuperseded",
      "Invalid limit returns 400 VALIDATION_ERROR",
      "Unauthenticated request returns 401",
      "events array not included in list items",
    ],
    files: ["internal/api/poll.go"],
  });

  leaf("P15-04", "P15", "Implement GET /api/v1/poll/runs/{id}", "backend", {
    context:
      "Detail view for a single poll run includes the events timeline (per-repo actions, errors, ticket operations). This powers deep inspection when a run fails or creates unexpected tickets. " +
      "The handler validates UUID format and returns 404 when the run does not exist. " +
      "Events must be ordered chronologically and include enough context for support without leaking secrets.",
    specs: [
      { ...SPEC.api, note: "GET /api/v1/poll/runs/{id} with events array" },
      { ...SPEC.domain, note: "Poll run event types: baseline, skip, create, supersede, error" },
      { ...SPEC.schema, note: "poll_run_events table" },
    ],
    stack: stackRefs({
      layout: "internal/api/poll.go",
      backend: "GetPollRunByID with preloaded events",
      frontend: "Future drill-down from dashboard",
      data: "FK poll_run_events.run_id",
      quality: "404 and malformed id tests",
      cicd: "go test -run PollRunDetail",
      deploy: "N/A",
    }),
    schema: [
      { ...SCHEMA.sql, note: "poll_runs and poll_run_events" },
      { ...SCHEMA.html, note: "Event payload columns" },
    ],
    implementation: [
      "Parse {id} path param as UUID",
      "Load run row; 404 if missing",
      "Load events for run_id ordered by created_at ASC",
      "Map event type, repoId, message, metadata JSON to response DTO",
      "Include full run summary fields on parent object",
      "Strip any secret-bearing metadata from event payloads",
    ],
    acceptance: [
      "GET /api/v1/poll/runs/{valid-id} returns 200 with run and events",
      "events is non-empty array when run produced events",
      "Unknown id returns 404 NOT_FOUND",
      "Malformed id returns 400 VALIDATION_ERROR",
      "Each event includes type, timestamp, and human-readable message",
      "Run counters match sum of event outcomes where applicable",
      "Unauthenticated request returns 401",
      "Response JSON matches §7.1 detail shape",
    ],
    files: ["internal/api/poll.go"],
    relatedTasks: ["P15-05"],
  });

  leaf("P15-05", "P15", "Add status and poll API tests", "backend", {
    depends_on: ["P15-04"],
    context:
      "Automated tests lock the status and poll API contract before the web UI depends on it. Use httptest with an in-memory or migrated SQLite fixture and authenticated session cookie. " +
      "Cover happy paths, auth failures, pagination, and concurrent trigger behavior. " +
      "Tests run in CI via go test ./internal/api/... -run Poll.",
    specs: [
      { ...SPEC.api, note: "§7.1 all status/poll endpoints" },
      { ...SPEC.mvp, note: "API correctness underpins dashboard AC" },
      { ...SPEC.ci, note: "go test in reusable ci.yml" },
    ],
    stack: stackRefs({
      layout: "internal/api/status_test.go, poll_test.go",
      backend: "httptest.Server with test store and session middleware",
      frontend: "N/A — backend contract tests",
      data: "Test fixtures seed repos and runs",
      quality: "Table-driven subtests per endpoint",
      cicd: "go test ./internal/api/... -run Poll",
      deploy: "N/A",
    }),
    schema: [{ ...SCHEMA.sql, note: "Migrate test DB from migrations/" }],
    implementation: [
      "Shared test helper: newTestServer with logged-in session",
      "Golden file or struct comparison for GET /status JSON",
      "Test POST /poll/trigger 202 and verify run row created",
      "Test list pagination with 25 seeded runs",
      "Test GET run detail includes events",
      "Test 401 without session cookie",
      "Test concurrent trigger rejection",
      "Run golangci-lint on test files",
    ],
    acceptance: [
      "go test ./internal/api/... -run Poll passes locally",
      "Status test asserts all §7.1 top-level keys",
      "Trigger test verifies 202 and run id in response",
      "List test asserts default limit 20",
      "Detail test loads events for seeded run",
      "Auth test proves 401 without cookie",
      "Concurrent trigger test documents expected status code",
      "Tests use t.Parallel where safe",
      "No flaky timing — poll worker stubbed or synchronized",
    ],
    files: ["internal/api/status_test.go", "internal/api/poll_test.go"],
    tests: ["go test ./internal/api/... -run Poll"],
    relatedTasks: ["P15-01", "P15-02", "P15-03", "P15-04"],
  });

  leaf("P15-06", "P15", "Register all API routes on chi router", "backend", {
    depends_on: ["P15-04", "P02-08", "P03-08"],
    context:
      "Final backend integration: mount every §7 handler on the /api/v1 router started in P02-08. " +
      "Verifies no orphan handlers and correct middleware order (public auth routes → session middleware → protected CRUD). " +
      "Prevents shipping UI that calls endpoints with no server route.",
    specs: [
      { ...SPEC.api, note: "§7 complete endpoint inventory" },
      { ...SPEC.auth, note: "Public vs protected route list" },
      { ...SPEC.architecture, note: "Single Go process owns all routes" },
    ],
    stack: stackRefs({
      layout: "internal/api/routes.go — RegisterAll(mux, deps)",
      backend: "chi Mount for auth, settings, integrations, ticket-projects, repos, notifications, status, poll",
      frontend: "Every §8.4 hook maps to a registered route",
      data: "Handlers receive *store.Repository",
      quality: "Route inventory test or OpenAPI-style checklist",
      cicd: "go test ./internal/api/... -run Routes",
      deploy: "cmd/server/main.go calls RegisterAll",
    }),
    implementation: [
      "RegisterAuthRoutes — login, logout, session (login/session public)",
      "RegisterSettingsRoutes, RegisterIntegrationsRoutes, RegisterTicketProjectRoutes",
      "RegisterReposRoutes, RegisterNotificationRoutes, RegisterStatusRoutes, RegisterPollRoutes",
      "Apply auth middleware after public auth routes mounted",
      "cmd/server/main.go: migrate → RegisterAll → StartScheduler",
      "Document route table in internal/api/routes.go comment matching specs §7",
      "404 and method-not-allowed return JSON error envelope",
    ],
    acceptance: [
      "Every specs.html §7 path has a registered handler",
      "GET /healthz on root router only",
      "POST /auth/login and GET /auth/session work without prior session",
      "Protected routes return 401 without cookie",
      "No duplicate route patterns registered",
      "RegisterAll callable from integration tests",
      "go test ./internal/api/... -run Routes passes",
      "Route list comment matches §7 tables exactly",
    ],
    files: ["internal/api/routes.go", "cmd/server/main.go"],
    tests: ["go test ./internal/api/... -run Routes"],
    relatedTasks: ["P02-08", "P15-05"],
  });
}

/** @param {Registry} reg */
function registerP16({ epic, leaf }) {
  epic("P16", "Next.js scaffold & Go API proxy", "web", {
    depends_on: ["P00-04", "P02-07"],
    context:
      "The Next.js app is UI-only; all business logic lives in Go. A same-origin catch-all proxy at /api/go forwards requests to GO_API_URL (default 127.0.0.1:8080) so the browser sends session cookies without CORS. " +
      "This epic establishes the app directory layout, proxy route, and cookie forwarding required before React Query hooks can call the API. " +
      "Architecture matches specs.html §2: one container, Go on loopback, Next on public port.",
    specs: [
      { ...SPEC.architecture, note: "Next UI-only; Go owns auth and API" },
      { ...SPEC.ui, note: "§8 — API calls via /api/go/api/v1/..." },
      { ...SPEC.auth, note: "Session cookie must forward through proxy" },
      { ...SPEC.env, note: "GO_API_URL env for proxy target" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/ with (authenticated) and login route groups",
      backend: "Go API unchanged; proxy is Next route handler",
      frontend: "Next.js 15 App Router, route.ts catch-all",
      data: "N/A at proxy layer",
      quality: "Vitest tests for proxy in P16-04",
      cicd: "npm test -- proxy in ci.yml web job",
      deploy: "GO_API_URL=127.0.0.1:8080 in Docker entrypoint",
    }),
    implementation: [
      "Create apps/web/app/(authenticated)/ and apps/web/app/login/ layouts",
      "Add apps/web/app/api/go/[...path]/route.ts catch-all",
      "Proxy GET/POST/PATCH/DELETE to GO_API_URL preserving path and query",
      "Forward Cookie request header and Set-Cookie response headers",
      "Preserve status codes and JSON bodies unchanged",
      "Add health check proxy path for deployment probe",
      "Document GO_API_URL in .env.example",
    ],
    acceptance: [
      "Next dev server starts with app directory layout",
      "/api/go/api/v1/auth/session proxies to Go and returns JSON",
      "Login Set-Cookie from Go reaches browser via proxy",
      "Authenticated API calls include session cookie automatically",
      "Proxy returns Go error responses without modification",
      "Unknown paths return Go 404, not Next 404",
      "GO_API_URL defaults to http://127.0.0.1:8080",
      "Vitest proxy tests pass (P16-04)",
    ],
  });

  leaf("P16-01", "P16", "Create Next.js app directory layout", "web", {
    depends_on: ["P00-03"],
    context:
      "Establish the App Router folder structure separating public login routes from authenticated app pages. The (authenticated) route group will later wrap sidebar and frame layouts. " +
      "Root layout.tsx loads global styles, fonts, and providers. " +
      "This task does not implement auth — only directory scaffolding per stack.html.",
    specs: [
      { ...SPEC.ui, note: "§8 route structure: /login vs authenticated pages" },
      { ...SPEC.architecture, note: "apps/web in monorepo layout" },
      { ...SPEC.ui, note: "apps/web/app/ directory per stack.html" },
    ],
    stack: stackRefs({
      layout: "app/(authenticated)/, app/login/, app/layout.tsx",
      backend: "N/A",
      frontend: "Next.js 15 App Router, TypeScript",
      data: "N/A",
      quality: "Lint passes on scaffold files",
      cicd: "npm run build includes new routes",
      deploy: "Standalone output configured later in P26",
    }),
    implementation: [
      "Create app/(authenticated)/layout.tsx placeholder",
      "Create app/login/page.tsx placeholder",
      "Create app/(authenticated)/page.tsx dashboard stub",
      "Ensure root layout.tsx wraps children with html/body",
      "Add globals.css import and metadata export",
      "Configure next.config for apps/web workspace",
    ],
    acceptance: [
      "apps/web/app/(authenticated)/ directory exists",
      "apps/web/app/login/ directory exists",
      "npm run dev serves /login without error",
      "npm run dev serves / (authenticated group) without error",
      "Root layout applies global CSS",
      "TypeScript compiles with strict mode",
      "No business logic in layout files yet",
      "Route groups do not affect URL paths",
    ],
    files: ["apps/web/app/(authenticated)/", "apps/web/app/login/"],
    relatedTasks: ["P18-05", "P19-01"],
  });

  leaf("P16-02", "P16", "Add /api/go catch-all proxy route", "web", {
    depends_on: ["P16-01"],
    context:
      "The catch-all API route is the bridge between browser and Go. It must support all HTTP methods used by the REST API and stream request bodies for POST/PATCH. " +
      "Target URL is process.env.GO_API_URL with fallback http://127.0.0.1:8080 per acceptance criteria. " +
      "Path after /api/go/ is appended to the Go server root (e.g. api/v1/status).",
    specs: [
      { ...SPEC.ui, note: "§8.4 — calls to /api/go/api/v1/..." },
      { ...SPEC.api, note: "All §7 endpoints reachable via proxy" },
      { ...SPEC.env, note: "GO_API_URL variable" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/api/go/[...path]/route.ts",
      backend: "fetch() to Go upstream",
      frontend: "Route handlers export GET, POST, PATCH, DELETE",
      data: "N/A",
      quality: "Manual smoke: curl /api/go/healthz",
      cicd: "Tested in P16-04",
      deploy: "GO_API_URL in container env",
    }),
    implementation: [
      "Create [...path]/route.ts with method exports",
      "Build upstream URL: GO_API_URL + '/' + path.join('/')",
      "Forward request headers except host",
      "Pipe request body for mutating methods",
      "Return new Response with upstream status and body",
      "Handle connection errors with 502 JSON error",
    ],
    acceptance: [
      "GET /api/go/api/v1/auth/session returns Go response",
      "Proxies to GO_API_URL default 127.0.0.1:8080",
      "POST with JSON body reaches Go handler",
      "PATCH and DELETE methods supported",
      "Query string preserved on upstream URL",
      "Go 401/404 status codes pass through",
      "GO_API_URL env overrides default target",
      "Malformed upstream connection returns 502",
    ],
    files: ["apps/web/app/api/go/[...path]/route.ts"],
    relatedTasks: ["P16-03", "P17-01"],
  });

  leaf("P16-03", "P16", "Forward Cookie and Set-Cookie in proxy", "web", {
    depends_on: ["P16-02"],
    context:
      "Go session auth depends on HTTP-only cookies set at login. The proxy must forward the incoming Cookie header to Go and relay Set-Cookie headers from Go responses back to the browser. " +
      "Without this, login appears to succeed in Go but the UI remains unauthenticated. " +
      "This is the critical piece enabling P17 useSession and P18 middleware.",
    specs: [
      { ...SPEC.auth, note: "Session cookie set by POST /auth/login" },
      { ...SPEC.ui, note: "§8.4 same-origin cookie forwarding" },
      { ...SPEC.architecture, note: "No JWT in localStorage — cookie only" },
    ],
    stack: stackRefs({
      layout: "Same route.ts — header forwarding logic",
      backend: "Go sets Set-Cookie on login/logout",
      frontend: "credentials: include on client fetch (P17)",
      data: "N/A",
      quality: "Integration test in P16-04",
      cicd: "npm test -- proxy",
      deploy: "Secure cookie flags from Go in production",
    }),
    implementation: [
      "Copy Cookie header from incoming request to upstream fetch",
      "Collect Set-Cookie from upstream response headers",
      "Append Set-Cookie to Next response (may be multiple)",
      "Do not strip HttpOnly or Secure attributes",
      "Forward Set-Cookie on logout to clear session",
      "Avoid duplicating cookies on chained proxies",
    ],
    acceptance: [
      "Login via /api/go sets session cookie in browser",
      "Subsequent /api/go requests include session cookie",
      "Logout clears cookie via Set-Cookie from Go",
      "Multiple Set-Cookie headers handled correctly",
      "Cookie not logged or exposed in error messages",
      "Session persists across page reload",
      "Unauthenticated session returns { user: null }",
      "Works in next dev and production standalone",
    ],
    files: ["apps/web/app/api/go/[...path]/route.ts"],
    relatedTasks: ["P17-02", "P18-04"],
  });

  leaf("P16-04", "P16", "Add proxy route tests", "web", {
    depends_on: ["P16-03"],
    context:
      "Vitest tests mock the upstream Go server to verify proxy path construction, method forwarding, and cookie relay without running Go in CI for every web test. " +
      "Tests should fail if cookie forwarding regresses — a common breakage when refactoring route handlers. " +
      "Run via npm test -- proxy.",
    specs: [
      { ...SPEC.ui, note: "§8.4 proxy contract" },
      { ...SPEC.ci, note: "Web test job in ci.yml" },
      { ...SPEC.auth, note: "Cookie round-trip behavior" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/api/go/[...path]/route.test.ts",
      backend: "Mock upstream with undici MockAgent or MSW",
      frontend: "Vitest + import route handlers directly",
      data: "N/A",
      quality: "RTL not needed — handler unit tests",
      cicd: "npm test -- proxy",
      deploy: "N/A",
    }),
    implementation: [
      "Mock GO_API_URL to local test server",
      "Test GET forwards path and returns body",
      "Test POST forwards JSON body",
      "Test Cookie header forwarded upstream",
      "Test Set-Cookie relayed to client response",
      "Test 502 on upstream connection failure",
      "Use vi.stubEnv for GO_API_URL",
    ],
    acceptance: [
      "npm test -- proxy passes",
      "Test asserts upstream receives Cookie header",
      "Test asserts client receives Set-Cookie",
      "Test covers GET and POST at minimum",
      "Test verifies path join for nested routes",
      "Tests run in CI web job",
      "No network calls to real Go in unit tests",
      "Coverage includes error path for ECONNREFUSED",
    ],
    files: ["apps/web/app/api/go/[...path]/route.test.ts"],
    tests: ["npm test -- proxy"],
    relatedTasks: ["P16-02", "P16-03"],
  });
}

/** @param {Registry} reg */
function registerP17({ epic, leaf }) {
  epic("P17", "React Query API client & hooks", "web", {
    depends_on: ["P16-03"],
    context:
      "Centralize all frontend API access through a typed fetch wrapper and TanStack React Query hooks per specs.html §8.4. " +
      "Hooks mirror REST resources: session, status, settings, repos, integrations, ticket projects, notification targets. " +
      "Mutations invalidate relevant query keys and surface errors via toasts (wired in P17-05). " +
      "Base URL is always /api/go/api/v1 — same origin, cookies included.",
    specs: [
      { ...SPEC.ui, note: "§8.4 API client — hooks list and proxy path" },
      { ...SPEC.api, note: "§7 REST shapes for hook return types" },
      { ...SPEC.auth, note: "useSession wraps GET /auth/session" },
      { ...SPEC.architecture, note: "React Query for server state" },
    ],
    stack: stackRefs({
      layout: "apps/web/lib/api/, apps/web/lib/hooks/",
      backend: "Types generated or hand-written from §7 JSON",
      frontend: "TanStack Query v5, fetch wrapper",
      data: "N/A — client cache only",
      quality: "Hook unit tests with MSW",
      cicd: "npm test in ci.yml",
      deploy: "N/A",
    }),
    implementation: [
      "Create apiClient in lib/api/client.ts with base path /api/go/api/v1",
      "Parse { error: { code, message } } on non-2xx",
      "Configure QueryClientProvider in root or authenticated layout",
      "Implement query hooks for each GET resource",
      "Implement mutation hooks with onSuccess invalidation",
      "Export query keys factory for consistent cache keys",
      "credentials: 'include' on all fetch calls",
      "TypeScript interfaces for Status, Repo, Integration, etc.",
    ],
    acceptance: [
      "apiClient throws typed ApiError on 4xx/5xx",
      "useSession returns { user } or { user: null }",
      "useStatus fetches GET /status with staleTime appropriate for dashboard",
      "useRepos, useIntegrations, useTicketProjects, useNotificationTargets list hooks work",
      "useSettings returns pollIntervalMinutes",
      "Mutation hooks invalidate affected queries on success",
      "All hooks use credentials include",
      "No direct fetch() in page components — hooks only",
    ],
  });

  leaf("P17-01", "P17", "Add API client fetch wrapper", "web", {
    context:
      "The fetch wrapper is the single choke point for HTTP to Go. It prefixes /api/go/api/v1, sets Content-Type for JSON bodies, parses errors, and always sends cookies. " +
      "Downstream hooks depend on consistent behavior here — especially error shape handling for form validation messages. " +
      "Keep the module free of React imports for easy testing.",
    specs: [
      { ...SPEC.ui, note: "§8.4 — /api/go/api/v1/... base path" },
      { ...SPEC.api, note: "§7 error envelope { error: { code, message } }" },
      { ...SPEC.auth, note: "credentials include for session cookie" },
    ],
    stack: stackRefs({
      layout: "apps/web/lib/api/client.ts",
      backend: "Mirrors Go JSON contracts",
      frontend: "Native fetch, no axios",
      data: "N/A",
      quality: "Unit tests for error parsing",
      cicd: "npm test -- client",
      deploy: "N/A",
    }),
    implementation: [
      "Export apiClient.get/post/patch/delete helpers",
      "BASE = '/api/go/api/v1'",
      "Default headers: Accept and Content-Type application/json",
      "credentials: 'include' on every request",
      "Parse JSON body; throw ApiError with code and message on !ok",
      "Support optional signal for query cancellation",
      "Export ApiError class extending Error",
    ],
    acceptance: [
      "GET helper returns parsed JSON on 200",
      "POST helper serializes body as JSON",
      "401 response throws ApiError with code UNAUTHORIZED or similar",
      "VALIDATION_ERROR includes message from Go",
      "credentials include set on all methods",
      "Network failure throws distinct error type",
      "client.ts has zero React imports",
      "TypeScript strict mode passes",
    ],
    files: ["apps/web/lib/api/client.ts"],
    relatedTasks: ["P17-02", "P17-03", "P17-04", "P17-05"],
  });

  leaf("P17-02", "P17", "Add useSession hook", "web", {
    depends_on: ["P17-01"],
    context:
      "useSession wraps GET /api/v1/auth/session and drives auth gating in middleware and layouts. It always returns 200 from Go — check user === null for logged-out state. " +
      "Query should refetch on window focus and after login/logout mutations. " +
      "Used by P18 middleware and P19 login redirect logic.",
    specs: [
      { ...SPEC.auth, note: "GET /auth/session — user null or { id, email }" },
      { ...SPEC.ui, note: "§8.4 lists useSession" },
      { ...SPEC.api, note: "§7.0 Auth session response" },
    ],
    stack: stackRefs({
      layout: "apps/web/lib/hooks/use-session.ts",
      backend: "Session endpoint always 200",
      frontend: "useQuery with key ['session']",
      data: "N/A",
      quality: "MSW mock session tests",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "useSession() calls apiClient.get('/auth/session')",
      "Return { data, isLoading, isAuthenticated: !!data?.user }",
      "staleTime: 0 or short — auth state must be fresh",
      "Export useLogout mutation calling POST /auth/logout",
      "Invalidate session query on logout success",
      "Type SessionResponse { user: User | null }",
    ],
    acceptance: [
      "useSession returns isLoading true while fetching",
      "Logged-in response exposes user.email",
      "Logged-out response has user null and isAuthenticated false",
      "Hook refetches after invalidateQueries(['session'])",
      "useLogout clears session and redirects to /login",
      "No infinite refetch loop on /login page",
      "Works with proxy cookie forwarding (P16-03)",
      "TypeScript types match §7.0 JSON",
    ],
    files: ["apps/web/lib/hooks/use-session.ts"],
    relatedTasks: ["P18-04", "P19-03"],
  });

  leaf("P17-03", "P17", "Add useStatus, useSettings, useRepos hooks", "web", {
    depends_on: ["P17-01"],
    context:
      "Dashboard and repos pages need read hooks for operational status, app settings, and monitored repo list. useStatus maps to §7.1 aggregated JSON; useSettings to §7.2; useRepos to §7.5. " +
      "Appropriate staleTime reduces load on dashboard while keeping manual poll feedback snappy via invalidation. " +
      "These hooks are consumed in P20 and P23.",
    specs: [
      { ...SPEC.api, note: "§7.1 status, §7.2 settings, §7.5 repos" },
      { ...SPEC.ui, note: "§8.4 hook names" },
      { ...SPEC.domain, note: "Repo fields: sourceKind, ticketProjectId, enabled" },
    ],
    stack: stackRefs({
      layout: "apps/web/lib/hooks/use-status.ts, use-settings.ts, use-repos.ts",
      backend: "Typed responses from §7",
      frontend: "Separate files or barrel export",
      data: "N/A",
      quality: "MSW fixtures from §7 examples",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "useStatus — GET /status, key ['status']",
      "useSettings — GET /settings, key ['settings']",
      "useRepos — GET /repos, key ['repos']",
      "Export TypeScript types Status, Settings, MonitoredRepo",
      "useStatus refetchInterval optional when isPolling true",
      "Document query keys in lib/api/query-keys.ts",
    ],
    acceptance: [
      "useStatus returns pollIntervalMinutes and repos array",
      "useStatus includes isPolling flag",
      "useSettings returns pollIntervalMinutes number",
      "useRepos returns array of repo objects with id",
      "Hooks handle 401 by relying on global error boundary or redirect",
      "Loading and error states exposed via React Query",
      "Types include ticketProjectName on status repos",
      "Invalidation after repo mutation refreshes useRepos",
    ],
    files: ["apps/web/lib/hooks/"],
    relatedTasks: ["P20-01", "P23-01", "P25-01"],
  });

  leaf("P17-04", "P17", "Add useIntegrations, useTicketProjects, useNotificationTargets", "web", {
    depends_on: ["P17-01"],
    context:
      "Configuration pages need list hooks for integrations (§7.3), ticket projects (§7.4), and notification targets (§7.6). Responses never include secrets — UI shows hasSecret flags instead. " +
      "Ticket projects may filter by integrationId query param for repo form selects. " +
      "Consumed by P21, P22, P23, and P24.",
    specs: [
      { ...SPEC.api, note: "§7.3 integrations, §7.4 ticket-projects, §7.6 notification-targets" },
      { ...SPEC.ticketProjects, note: "§5.3 status mapping fields in list items" },
      { ...SPEC.notifications, note: "Shoutrrr targets without URL in GET" },
    ],
    stack: stackRefs({
      layout: "apps/web/lib/hooks/",
      backend: "List DTOs without secret fields",
      frontend: "Optional integrationId filter on ticket projects",
      data: "N/A",
      quality: "Assert no secret in mocked responses",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "useIntegrations — GET /integrations",
      "useTicketProjects(integrationId?) — GET /ticket-projects",
      "useNotificationTargets — GET /notification-targets",
      "Types: Integration (hasSecret), TicketProject, NotificationTarget",
      "Query keys include filter params when present",
      "Export hooks from lib/hooks/index.ts",
    ],
    acceptance: [
      "useIntegrations returns kind, name, baseUrl, hasSecret",
      "No secret field in Integration type",
      "useTicketProjects accepts optional integrationId filter",
      "Ticket project includes statusMapping and onOpenTicketPolicy",
      "useNotificationTargets returns events array and enabled",
      "Shoutrrr URL never in list response type",
      "Hooks share consistent error handling with useRepos",
      "Empty lists return [] not null",
    ],
    files: ["apps/web/lib/hooks/"],
    relatedTasks: ["P21-01", "P22-01", "P24-01"],
  });

  leaf("P17-05", "P17", "Add mutation hooks with error toasts", "web", {
    depends_on: ["P17-01"],
    context:
      "Create, update, delete, and test actions across all resources need mutation hooks with consistent cache invalidation and user feedback. On error, surface message via toast (Toaster from P18-03). " +
      "Success paths invalidate the relevant query keys — e.g. useCreateIntegration invalidates ['integrations']. " +
      "Test connection and test notification mutations use promise toasts per COSS p-toast-5.",
    specs: [
      { ...SPEC.ui, note: "§8.4 mutations; p-toast-2 / p-toast-5 feedback" },
      { ...SPEC.api, note: "§7 POST/PATCH/DELETE and /test endpoints" },
      { ...SPEC.notifications, note: "Test notification async feedback" },
    ],
    stack: stackRefs({
      layout: "apps/web/lib/hooks/mutations.ts",
      backend: "All write endpoints via apiClient",
      frontend: "useMutation + toast from sonner or COSS",
      data: "N/A",
      quality: "Test onError calls toast",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "useCreateIntegration, useUpdateIntegration, useDeleteIntegration",
      "useCreateTicketProject, useUpdateTicketProject, useDeleteTicketProject",
      "useCreateRepo, useUpdateRepo, useDeleteRepo",
      "useCreateNotificationTarget, useUpdateNotificationTarget, useDeleteNotificationTarget",
      "useUpdateSettings — PATCH /settings",
      "useTriggerPoll — POST /poll/trigger",
      "useTestIntegration, useTestNotificationTarget",
      "onError: toast.error(message); onSuccess: invalidate + toast.success where appropriate",
    ],
    acceptance: [
      "Create integration invalidates ['integrations']",
      "Delete repo invalidates ['repos'] and ['status']",
      "useUpdateSettings invalidates ['settings'] and ['status']",
      "useTriggerPoll invalidates ['status'] on success",
      "API error message shown in error toast",
      "Test connection shows loading toast then success/failure",
      "409 conflict shows user-friendly message",
      "Mutations use credentials include via apiClient",
    ],
    files: ["apps/web/lib/hooks/mutations.ts"],
    relatedTasks: ["P18-03", "P21-04", "P24-04"],
  });

  leaf("P17-06", "P17", "Add usePollRuns, usePollRun, and useTriggerPoll hooks", "web", {
    depends_on: ["P17-01", "P15-03"],
    context:
      "Backend exposes GET /api/v1/poll/runs and GET /api/v1/poll/runs/{id} (P15-03, P15-04) plus POST /api/v1/poll/trigger (P15-02). " +
      "The dashboard poll history UI (P20-06) and manual poll button (P20-02) require dedicated React Query hooks — no ad-hoc fetch in components. " +
      "Parity rule: every poll API endpoint has a named hook exported from lib/hooks.",
    specs: [
      { ...SPEC.api, note: "§7.1 poll/runs list, detail, and trigger" },
      { ...SPEC.ui, note: "§8.4 hook pattern — extends useStatus/useSettings set" },
      { ...SPEC.mvp, note: "AC #14 manual poll + run visibility" },
    ],
    stack: stackRefs({
      layout: "apps/web/lib/hooks/use-poll.ts",
      backend: "Proxied via /api/go/api/v1/poll/*",
      frontend: "useQuery + useMutation with query key ['poll-runs']",
      data: "Types mirror §7.1 poll run and event shapes",
      quality: "MSW tests for list, detail, trigger",
      cicd: "npm test -- use-poll",
      deploy: "N/A",
    }),
    implementation: [
      "usePollRuns({ limit }) — GET /poll/runs with pagination params",
      "usePollRun(id) — GET /poll/runs/{id} enabled when id set",
      "useTriggerPoll — POST /poll/trigger mutation; invalidates ['status','poll-runs']",
      "Export PollRun, PollRunEvent TypeScript types from API responses",
      "Handle 202 response from trigger — return run id if present",
      "useTriggerPoll onError surfaces toast via mutations helper",
      "Re-export from lib/hooks/index.ts",
    ],
    acceptance: [
      "usePollRuns fetches paginated run list",
      "usePollRun fetches single run with events[]",
      "useTriggerPoll POSTs to /poll/trigger",
      "Trigger success invalidates status and poll-runs queries",
      "Types include startedAt, finishedAt, status, reposChecked, ticketsCreated, ticketsSuperseded",
      "Event type includes action baseline|skip|create|supersede|merge|skip_open|error",
      "Hooks use apiClient with credentials include",
      "npm test -- use-poll passes",
    ],
    files: ["apps/web/lib/hooks/use-poll.ts", "apps/web/lib/hooks/index.ts"],
    tests: ["npm test -- use-poll"],
    relatedTasks: ["P20-02", "P20-06", "P15-02", "P15-03", "P15-04"],
  });
}

/** @param {Registry} reg */
function registerP18({ epic, leaf }) {
  epic("P18", "App shell & routing", "web", {
    depends_on: ["P17-02", "P00-04"],
    context:
      "Authenticated pages share a COSS app shell: sidebar navigation, frame layout with breadcrumbs, global toaster, and loading spinner per specs.html §8.1. " +
      "Next.js middleware redirects unauthenticated users to /login; protected layout wraps all (authenticated) routes. " +
      "Particle references: p-frame-3, p-breadcrumb-3, p-toast-2, p-spinner-1 from coss.com/ui/r/*.json.",
    specs: [
      { ...SPEC.ui, note: "§8.1 shell particles — Frame, Breadcrumb, Toaster, Spinner" },
      { ...SPEC.auth, note: "Middleware auth redirect to /login" },
      { ...SPEC.architecture, note: "Route groups and protected layout" },
      { ...SPEC.mvp, note: "Nav links to all MVP pages" },
    ],
    stack: stackRefs({
      layout: "components/app-sidebar.tsx, app-frame.tsx, middleware.ts",
      backend: "N/A",
      frontend: "COSS Sidebar, Frame, Breadcrumb, Toaster",
      data: "N/A",
      quality: "Middleware unit tests",
      cicd: "npm run build + lint",
      deploy: "middleware runs on Edge or Node per Next config",
    }),
    implementation: [
      "Install COSS particles: p-frame-3, p-breadcrumb-3, p-toast-2, p-spinner-1",
      "Sidebar links: Dashboard, Repos, Integrations, Ticket Projects, Notifications, Settings",
      "Frame wraps page content with breadcrumb slot",
      "Toaster and Spinner in root layout.tsx",
      "middleware.ts checks session cookie or calls session endpoint",
      "(authenticated)/layout.tsx composes Sidebar + Frame",
    ],
    acceptance: [
      "Sidebar shows all six nav links with active state",
      "Frame uses p-frame-3 layout pattern",
      "Breadcrumb uses p-breadcrumb-3 with home icon",
      "Toaster displays success/error toasts globally",
      "Unauthenticated / access redirects to /login",
      "/login accessible without auth",
      "Protected layout wraps dashboard and settings",
      "Spinner shown during route-level loading.tsx where used",
    ],
  });

  leaf("P18-01", "P18", "Add SidebarProvider and navigation", "web", {
    context:
      "Primary navigation lives in a persistent sidebar using COSS Sidebar primitives. Links must match MVP routes: /, /repos, /integrations, /ticket-projects, /notifications, /settings. " +
      "SidebarProvider manages collapse state on desktop and sheet on mobile. " +
      "Active link styling uses pathname from next/navigation.",
    specs: [
      { ...SPEC.ui, note: "§8.1 navigation link list" },
      { ...SPEC.mvp, note: "All config pages reachable from nav" },
      { ...SPEC.architecture, note: "apps/web/components/app-sidebar.tsx" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/app-sidebar.tsx",
      backend: "N/A",
      frontend: "COSS Sidebar, next/link",
      data: "N/A",
      quality: "Snapshot or RTL test for link hrefs",
      cicd: "npm run lint",
      deploy: "N/A",
    }),
    implementation: [
      "Create AppSidebar with SidebarProvider",
      "Nav items: Dashboard (/), Repos, Integrations, Ticket Projects, Notifications, Settings",
      "Highlight active route via usePathname()",
      "App title/logo in sidebar header",
      "Logout control in sidebar footer — see P18-06 (required, not optional)",
      "Responsive: collapsible or sheet on narrow viewports",
    ],
    acceptance: [
      "Links: Dashboard, Repos, Integrations, Ticket Projects, Notifications, Settings",
      "Each link navigates to correct path",
      "Active link visually distinct",
      "Sidebar renders inside authenticated layout only",
      "Keyboard navigation works on nav items",
      "Logout clears session and redirects to /login",
      "No broken links on empty app state",
      "Matches specs §8.1 route table",
    ],
    files: ["apps/web/components/app-sidebar.tsx"],
    relatedTasks: ["P18-05"],
  });

  leaf("P18-02", "P18", "Add Frame layout and breadcrumb", "web", {
    context:
      "Page content sits inside a COSS Frame with optional breadcrumb trail per specs.html §8.1. Use particle p-frame-3 (https://coss.com/ui/r/p-frame-3.json) for separated header/content panels and p-breadcrumb-3 (https://coss.com/ui/r/p-breadcrumb-3.json) for sub-page context. " +
      "Each page passes title and breadcrumb segments as props or slots. " +
      "Frame provides consistent padding and max-width across all authenticated views.",
    specs: [
      { ...SPEC.ui, note: "§8.1 Frame + Breadcrumb particles" },
      { ...SPEC.mvp, note: "Consistent page chrome for all admin pages" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/app-frame.tsx",
      backend: "N/A",
      frontend: "p-frame-3, p-breadcrumb-3 from COSS registry",
      data: "N/A",
      quality: "Visual match to particle JSON",
      cicd: "npm run build",
      deploy: "N/A",
    }),
    implementation: [
      "Add shadcn/COSS Frame from https://coss.com/ui/r/p-frame-3.json",
      "Add Breadcrumb from https://coss.com/ui/r/p-breadcrumb-3.json",
      "AppFrame accepts title, breadcrumbs[], children",
      "Home breadcrumb links to dashboard /",
      "Export for use in (authenticated)/layout or per-page",
      "Page header slot for actions (e.g. Add button)",
    ],
    acceptance: [
      "Frame layout matches p-frame-3 multiple panels pattern",
      "Breadcrumb shows home icon per p-breadcrumb-3",
      "Sub-pages show parent → current trail",
      "Frame wraps children with consistent spacing",
      "Title displayed in frame header area",
      "Breadcrumb links are clickable except current page",
      "Works on mobile without horizontal overflow",
      "Particle JSON URLs referenced in component comments",
    ],
    files: ["apps/web/components/app-frame.tsx"],
    relatedTasks: ["P18-05"],
  });

  leaf("P18-03", "P18", "Add global Toaster and Spinner", "web", {
    context:
      "Global feedback components mount once in root layout.tsx. Toaster uses p-toast-2 (https://coss.com/ui/r/p-toast-2.json) for semantic success/error variants; Spinner uses p-spinner-1 (https://coss.com/ui/r/p-spinner-1.json) for loading states. " +
      "Mutation hooks from P17-05 call toast() for errors and async operations. " +
      "Spinner may wrap Suspense boundaries or inline button loading states.",
    specs: [
      { ...SPEC.ui, note: "§8.1 Toaster p-toast-2, Spinner p-spinner-1" },
      { ...SPEC.notifications, note: "Toast for test notification feedback" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/layout.tsx",
      backend: "N/A",
      frontend: "COSS Toaster, Spinner primitives",
      data: "N/A",
      quality: "Smoke: toast renders on trigger",
      cicd: "npm run build",
      deploy: "N/A",
    }),
    implementation: [
      "Install Toaster from https://coss.com/ui/r/p-toast-2.json",
      "Install Spinner from https://coss.com/ui/r/p-spinner-1.json",
      "Mount <Toaster /> in root layout after children",
      "Export toast helper wrapping COSS/sonner API",
      "Spinner component for consistent sizing",
      "Document toast usage in mutations.ts",
    ],
    acceptance: [
      "Toaster mounted in root layout renders toasts",
      "Success toast uses semantic success styling",
      "Error toast uses semantic error styling",
      "Spinner displays when passed loading prop",
      "Toasts stack without overlapping nav",
      "toast() callable from mutation onError",
      "Promise toast pattern available for test buttons",
      "No duplicate Toaster instances",
    ],
    files: ["apps/web/app/layout.tsx"],
    relatedTasks: ["P17-05", "P19-03"],
  });

  leaf("P18-04", "P18", "Add Next.js middleware auth redirect", "web", {
    depends_on: ["P17-02"],
    context:
      "Edge middleware protects all (authenticated) routes by checking for a valid session before rendering. Unauthenticated requests to /, /repos, etc. redirect to /login with optional return URL. " +
      "/login and /api/go paths must be excluded from auth check. " +
      "Session check can inspect cookie presence or lightweight fetch — prefer cookie name match for edge performance.",
    specs: [
      { ...SPEC.auth, note: "Unauthenticated → /login redirect" },
      { ...SPEC.ui, note: "§8 route protection" },
      { ...SPEC.api, note: "Proxy routes must bypass middleware auth" },
    ],
    stack: stackRefs({
      layout: "apps/web/middleware.ts",
      backend: "Session cookie set by Go login",
      frontend: "Next.js middleware matcher config",
      data: "N/A",
      quality: "middleware.test.ts with mock request",
      cicd: "npm test",
      deploy: "Runs on Vercel/Node deployment",
    }),
    implementation: [
      "Create middleware.ts at apps/web root",
      "matcher: all except /login, /api/go, _next, static files",
      "Check session cookie exists (name from Go auth config)",
      "Redirect to /login?next=pathname when missing",
      "Allow /login when already authenticated → redirect to /",
      "Export config.matcher array",
    ],
    acceptance: [
      "Unauthenticated GET / redirects to /login",
      "Unauthenticated GET /repos redirects to /login",
      "/login loads without redirect when logged out",
      "/api/go/* requests not blocked by middleware",
      "Authenticated user accessing /login redirects to /",
      "next query param preserved for post-login redirect",
      "Static assets and _next not matched",
      "middleware.test.ts passes",
    ],
    files: ["apps/web/middleware.ts"],
    relatedTasks: ["P17-02", "P18-05"],
  });

  leaf("P18-05", "P18", "Add protected layout wrapper", "web", {
    depends_on: ["P18-01", "P18-04"],
    context:
      "The (authenticated)/layout.tsx composes SidebarProvider, AppSidebar, AppFrame, and QueryClientProvider for all protected pages. This is the single wrapper every dashboard and settings page inherits. " +
      "Depends on sidebar (P18-01) and middleware (P18-04) being in place. " +
      "Children render inside Frame content area.",
    specs: [
      { ...SPEC.ui, note: "§8.1 authenticated shell composition" },
      { ...SPEC.architecture, note: "(authenticated) route group layout" },
      { ...SPEC.auth, note: "Layout only reachable after middleware pass" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/layout.tsx",
      backend: "N/A",
      frontend: "Sidebar + Frame + providers",
      data: "N/A",
      quality: "Integration: protected page renders shell",
      cicd: "npm run build",
      deploy: "N/A",
    }),
    implementation: [
      "Import SidebarProvider, AppSidebar, AppFrame",
      "Wrap children with QueryClientProvider if not in root",
      "Pass default breadcrumb from segment or let pages override",
      "Ensure layout is client or server component per COSS needs",
      "Add loading.tsx optional at group level",
      "Logout in sidebar footer",
    ],
    acceptance: [
      "Dashboard at / shows sidebar and frame",
      "All authenticated routes share same shell",
      "/login does not render sidebar",
      "Children render inside frame content panel",
      "Query client available to all child hooks",
      "Layout does not flash unauthenticated content",
      "Mobile sidebar toggle works",
      "No duplicate providers on navigation",
    ],
    files: ["apps/web/app/(authenticated)/layout.tsx"],
    relatedTasks: ["P20-01", "P21-01", "P25-01"],
  });

  leaf("P18-06", "P18", "Add sidebar logout control", "web", {
    depends_on: ["P18-01", "P17-02"],
    context:
      "Backend POST /api/v1/auth/logout (P03-06) must have a visible UI control — not optional. " +
      "Sidebar footer hosts Sign out button calling useLogout from P17-02. " +
      "Clears session cookie via proxy, invalidates useSession, redirects to /login. Required for operator session hygiene.",
    specs: [
      { ...SPEC.auth, note: "§3 POST /auth/logout clears session" },
      { ...SPEC.api, note: "§7.0 auth/logout endpoint" },
      { ...SPEC.ui, note: "§8.1 app shell — operator controls in sidebar" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/app-sidebar.tsx footer",
      backend: "POST /api/go/api/v1/auth/logout",
      frontend: "p-button-1 ghost variant in sidebar footer",
      data: "N/A",
      quality: "RTL test: click logout → redirect /login",
      cicd: "npm test -- sidebar",
      deploy: "N/A",
    }),
    implementation: [
      "SidebarFooter with Sign out button (p-button-1)",
      "onClick → useLogout.mutate()",
      "On success: router.push('/login')",
      "Show user email from useSession in footer when available",
      "Disable button while logout pending",
      "Toast on logout error",
    ],
    acceptance: [
      "Sign out button visible on all authenticated pages",
      "Click calls POST /auth/logout through proxy",
      "Session cookie cleared after logout",
      "User redirected to /login",
      "useSession returns user:null after logout",
      "Protected routes inaccessible after logout without re-login",
      "Button accessible (keyboard + screen reader label)",
      "npm test covers logout click flow",
    ],
    files: ["apps/web/components/app-sidebar.tsx"],
    tests: ["npm test -- sidebar"],
    relatedTasks: ["P03-06", "P17-02", "P18-01"],
  });
}

/** @param {Registry} reg */
function registerP19({ epic, leaf }) {
  epic("P19", "Login page", "web", {
    depends_on: ["P18-03", "P17-02"],
    context:
      "The login page is the only unauthenticated UI surface besides static assets. It uses COSS particles from specs.html §8.3 /login: p-card-1 centered container, p-field-2 required fields, p-button-1 submit, p-alert-7 on 401. " +
      "Successful login sets session via Go proxy and redirects to dashboard or next param. " +
      "No public sign-up — MVP is single admin account seeded at deploy.",
    specs: [
      { ...SPEC.ui, note: "§8.3 /login — p-card-1, p-field-2, p-alert-7" },
      { ...SPEC.auth, note: "POST /auth/login email+password only" },
      { ...SPEC.mvp, note: "AC #1 — admin can log in" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/login/",
      backend: "Login via /api/go/api/v1/auth/login",
      frontend: "COSS Card, Field, Button, Alert particles",
      data: "N/A",
      quality: "Vitest + RTL login tests P19-04",
      cicd: "npm test -- login",
      deploy: "APP_PUBLIC_URL for redirect if needed",
    }),
    implementation: [
      "Centered login page at /login",
      "Card from https://coss.com/ui/r/p-card-1.json",
      "Email and password fields from p-field-2",
      "Submit calls POST /auth/login via apiClient",
      "401 shows p-alert-7 error alert",
      "Success invalidates session and redirects to / or next",
    ],
    acceptance: [
      "Login page renders centered card",
      "Email and password fields are required",
      "Valid credentials redirect to dashboard",
      "Invalid credentials show error alert without navigation",
      "Session cookie set after successful login",
      "next query param honored after login",
      "Page accessible without authentication",
      "Matches §8.3 particle mapping",
    ],
  });

  leaf("P19-01", "P19", "Build centered login card layout", "web", {
    context:
      "Visual shell for login using p-card-1 (https://coss.com/ui/r/p-card-1.json) as a centered container on a minimal full-viewport layout. " +
      "No form logic yet — structure and typography only. " +
      "Page should not include sidebar or authenticated frame.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-card-1 Basic card — centered login container" },
      { ...SPEC.auth, note: "Login route outside authenticated group" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/login/page.tsx",
      backend: "N/A",
      frontend: "p-card-1 particle",
      data: "N/A",
      quality: "RTL renders card title",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "login/page.tsx with flex center min-h-screen",
      "Card component from COSS p-card-1",
      "Title: Sign in to Release Ops",
      "Slot for LoginForm child component",
      "No authenticated layout wrapper",
      "Metadata title for login page",
    ],
    acceptance: [
      "p-card-1 centered on viewport",
      "Card visible without sidebar",
      "Responsive on mobile widths",
      "Title text present",
      "Login route at /login",
      "No auth required to view page",
      "Card has consistent padding per particle",
      "Placeholder for form inside card body",
    ],
    files: ["apps/web/app/login/page.tsx"],
    relatedTasks: ["P19-02"],
  });

  leaf("P19-02", "P19", "Add email and password form fields", "web", {
    context:
      "Login form fields use p-field-2 (https://coss.com/ui/r/p-field-2.json) required pattern for email and password. Password may use p-input-group-1 for visibility toggle if included in particle. " +
      "Client-side HTML5 validation for required and email format. " +
      "Submit button uses p-button-1 but wiring is P19-03.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-field-2 required — Email, Password" },
      { ...SPEC.auth, note: "POST body { email, password }" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/login/login-form.tsx",
      backend: "N/A",
      frontend: "p-field-2, p-button-1",
      data: "N/A",
      quality: "RTL field labels and required attrs",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "LoginForm component with controlled or uncontrolled inputs",
      "Email input type=email required",
      "Password input type=password required",
      "Labels: Email, Password",
      "Submit button Sign in (disabled state in P19-03)",
      "Form prevents default on submit handler stub",
    ],
    acceptance: [
      "p-field-2 required fields for email and password",
      "Empty submit blocked by HTML5 validation",
      "Invalid email format blocked",
      "Password field masks input",
      "Submit button labeled Sign in",
      "Fields accessible with associated labels",
      "Tab order: email → password → submit",
      "Form inside login card from P19-01",
    ],
    files: ["apps/web/app/login/login-form.tsx"],
    relatedTasks: ["P19-03"],
  });

  leaf("P19-03", "P19", "Wire login submit and error alert", "web", {
    depends_on: ["P19-02"],
    context:
      "Submit handler POSTs to /api/go/api/v1/auth/login via apiClient. On 200, invalidate useSession and redirect. On 401, show p-alert-7 (https://coss.com/ui/r/p-alert-7.json) error alert with generic invalid credentials message. " +
      "Disable submit while pending; optional spinner on button. " +
      "Never log password values.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-alert-7 error on login failure" },
      { ...SPEC.auth, note: "§7.0 POST /auth/login 401 on failure" },
      { ...SPEC.api, note: "Login response { user: { id, email } }" },
    ],
    stack: stackRefs({
      layout: "login-form.tsx submit handler",
      backend: "Go sets session cookie via proxy",
      frontend: "useSession invalidate + router.push",
      data: "N/A",
      quality: "Mock 401 and 200 responses",
      cicd: "npm test -- login",
      deploy: "N/A",
    }),
    implementation: [
      "onSubmit: apiClient.post('/auth/login', { email, password })",
      "onSuccess: invalidateQueries session, redirect to next or /",
      "on 401: set error state, render Alert p-alert-7",
      "Clear error on field change",
      "isSubmitting disables button",
      "useRouter from next/navigation for redirect",
    ],
    acceptance: [
      "Valid login redirects to dashboard",
      "401 shows p-alert-7 error alert",
      "Error message does not leak whether email exists",
      "Session cookie present after success",
      "next= query param redirect works",
      "Submit disabled while request in flight",
      "Password not in URL or console logs",
      "Double submit prevented",
    ],
    files: ["apps/web/app/login/login-form.tsx"],
    relatedTasks: ["P17-02", "P19-04"],
  });

  leaf("P19-04", "P19", "Add login page tests", "web", {
    depends_on: ["P19-03"],
    context:
      "Vitest and React Testing Library tests cover render, validation, successful login redirect, and 401 error display. MSW mocks /auth/login endpoint. " +
      "Run npm test -- login in CI. " +
      "Tests prove MVP AC #1 at UI layer.",
    specs: [
      { ...SPEC.ui, note: "§8.3 login page behavior" },
      { ...SPEC.mvp, note: "AC #1 admin login" },
      { ...SPEC.ci, note: "Web test job" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/login/*.test.tsx",
      backend: "MSW mock login API",
      frontend: "RTL userEvent",
      data: "N/A",
      quality: "Coverage for happy and error paths",
      cicd: "npm test -- login",
      deploy: "N/A",
    }),
    implementation: [
      "Render login page with providers",
      "Test required field validation",
      "MSW 200 → assert redirect or session invalidate",
      "MSW 401 → assert alert visible",
      "Test next param redirect mock",
      "Mock useRouter push",
    ],
    acceptance: [
      "npm test -- login passes",
      "Test renders email and password fields",
      "Test shows error on 401 response",
      "Test calls login API with credentials",
      "Test does not navigate on 401",
      "No flaky async — await findByRole",
      "Tests isolated with MSW reset",
      "Runs in CI without Go server",
    ],
    files: ["apps/web/app/login/login-form.test.tsx"],
    tests: ["npm test -- login"],
    relatedTasks: ["P19-01", "P19-02", "P19-03"],
  });
}

/** @param {Registry} reg */
function registerP20({ epic, leaf }) {
  epic("P20", "Dashboard page", "web", {
    depends_on: ["P18-05", "P17-03"],
    context:
      "Dashboard is the home page at / showing last poll summary, manual poll trigger, and repo status table per specs.html §8.3 dashboard particles. " +
      "Uses useStatus for data and mutation hooks for manual poll. " +
      "Particles: p-card-10, p-badge-5, p-button-1, p-table-4, p-alert-6, p-empty-1.",
    specs: [
      { ...SPEC.ui, note: "§8.3 dashboard particles table" },
      { ...SPEC.api, note: "§7.1 GET /status response" },
      { ...SPEC.mvp, note: "AC #14 dashboard and manual poll" },
      { ...SPEC.domain, note: "Repo status columns in table" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/page.tsx",
      backend: "Status from proxied Go API",
      frontend: "COSS Card, Badge, Table, Alert, Empty",
      data: "N/A — React Query cache",
      quality: "Dashboard Vitest suite P20-05",
      cicd: "npm test -- dashboard",
      deploy: "N/A",
    }),
    implementation: [
      "Dashboard page at (authenticated)/page.tsx",
      "Last poll card with p-card-10 stats",
      "Status badge p-badge-5 and Run poll now button",
      "Sortable repo table p-table-4",
      "Warning alert p-alert-6 for last run errors",
      "Empty state p-empty-1 with CTA to /repos",
    ],
    acceptance: [
      "Dashboard loads at / inside app shell",
      "Last poll card shows run timestamps and counters",
      "Manual poll button triggers POST /poll/trigger",
      "Repo table lists all repos from status",
      "Errors from lastRun.errors shown in alert",
      "Empty repos shows empty state with link to /repos",
      "isPolling disables or shows loading on poll button",
      "Table sortable per p-table-4 TanStack pattern",
    ],
  });

  leaf("P20-01", "P20", "Build last poll summary card", "web", {
    context:
      "Summary card uses p-card-10 (https://coss.com/ui/r/p-card-10.json) frame header+footer showing last run startedAt, finishedAt, reposChecked, ticketsCreated, ticketsSuperseded. " +
      "Data from useStatus().lastRun. Handle null lastRun with friendly first-run message. " +
      "Poll interval from status.pollIntervalMinutes in card footer.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-card-10 Last poll summary card" },
      { ...SPEC.api, note: "§7.1 lastRun object fields" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/page.tsx",
      backend: "useStatus hook",
      frontend: "p-card-10",
      data: "N/A",
      quality: "RTL with mock status fixture",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "PollSummaryCard component",
      "Display lastRun.startedAt, finishedAt formatted",
      "Show reposChecked, ticketsCreated, ticketsSuperseded",
      "Footer: Poll every N minutes",
      "Skeleton while useStatus loading",
      "Null lastRun: No polls yet message",
    ],
    acceptance: [
      "p-card-10 with run stats when lastRun present",
      "Timestamps human-readable (locale format)",
      "Counters match API integers",
      "Poll interval shown from pollIntervalMinutes",
      "Loading skeleton during fetch",
      "Graceful empty when lastRun null",
      "Card header title Last poll",
      "No errors when repos array empty",
    ],
    files: ["apps/web/app/(authenticated)/page.tsx"],
    relatedTasks: ["P20-02"],
  });

  leaf("P20-02", "P20", "Add run status badge and manual poll button", "web", {
    depends_on: ["P20-01"],
    context:
      "Status badge uses p-badge-5 (https://coss.com/ui/r/p-badge-5.json) semantic colors for success/error/running. Run poll now button uses p-button-1 and calls useTriggerPoll mutation. " +
      "Disable button when status.isPolling true. " +
      "Invalidate status query on successful trigger.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-badge-5, p-button-1 Run poll now" },
      { ...SPEC.api, note: "POST /poll/trigger 202" },
      { ...SPEC.mvp, note: "Manual poll on dashboard" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/dashboard/poll-actions.tsx",
      backend: "Trigger poll mutation",
      frontend: "Badge + Button particles",
      data: "N/A",
      quality: "Test button disabled when polling",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "PollActions component with badge + button",
      "Badge maps lastRun.status to variant",
      "Button onClick → useTriggerPoll.mutate()",
      "Disabled when isPolling or mutation pending",
      "Toast on trigger success/error",
      "Refetch status on interval while isPolling",
    ],
    acceptance: [
      "Badge shows success/error/running semantic color",
      "Run poll now triggers POST /poll/trigger",
      "Button disabled while isPolling true",
      "Toast on successful 202 response",
      "Error toast on 409 concurrent poll",
      "Status refetches after trigger",
      "Button label Run poll now",
      "Badge updates when lastRun.status changes",
    ],
    files: ["apps/web/components/dashboard/poll-actions.tsx"],
    relatedTasks: ["P15-02", "P17-05"],
  });

  leaf("P20-03", "P20", "Add repo status table", "web", {
    context:
      "Repo status subset in dashboard table using p-table-4 (https://coss.com/ui/r/p-table-4.json) TanStack sort and pagination. Columns: source, project path, ticket project, last tag, open ticket, last error. " +
      "Data from useStatus().repos — not separate useRepos call. " +
      "Sort by projectPath default.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-table-4 repo status subset" },
      { ...SPEC.api, note: "§7.1 repos[] in status response" },
      { ...SPEC.domain, note: "openTicketTag, lastKnownTag columns" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/dashboard/repo-table.tsx",
      backend: "Status repos array",
      frontend: "TanStack Table + p-table-4",
      data: "N/A",
      quality: "Sort column unit test",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "RepoStatusTable with TanStack Table",
      "Columns: sourceKind, projectPath, ticketProjectName, lastKnownTag, openTicketTag, lastError",
      "Client-side sort on columns",
      "Truncate long errors with tooltip",
      "Badge for source_kind p-badge-1 style",
      "Pagination if >10 repos optional",
    ],
    acceptance: [
      "p-table-4 sortable columns",
      "All status.repos rows rendered",
      "sourceKind displayed as badge or text",
      "openTicketTag shown when ticket open",
      "lastError highlighted when non-null",
      "Empty table hidden when repos empty (empty state elsewhere)",
      "Sort toggles asc/desc on header click",
      "Accessible table headers",
    ],
    files: ["apps/web/components/dashboard/repo-table.tsx"],
    relatedTasks: ["P20-04"],
  });

  leaf("P20-04", "P20", "Add error alert and empty state", "web", {
    context:
      "When lastRun.errors is non-empty, show p-alert-6 (https://coss.com/ui/r/p-alert-6.json) warning with error messages. When repos array empty, show p-empty-1 (https://coss.com/ui/r/p-empty-1.json) with CTA link to /repos. " +
      "These states are mutually exclusive with full dashboard content. " +
      "Improves first-run onboarding.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-alert-6 warning, p-empty-1 no repos" },
      { ...SPEC.mvp, note: "Dashboard usable on fresh install" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/page.tsx",
      backend: "lastRun.errors from status",
      frontend: "Alert + Empty particles",
      data: "N/A",
      quality: "RTL both states",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "Conditional render: errors alert above table",
      "Map lastRun.errors to alert description list",
      "Empty state when repos.length === 0",
      "CTA Button links to /repos Add your first repo",
      "Do not show empty when loading",
      "Alert dismissible optional",
    ],
    acceptance: [
      "Warning alert when lastRun.errors non-empty",
      "Each error message listed in alert",
      "Empty state when no repos configured",
      "Empty CTA navigates to /repos",
      "No empty state while isLoading",
      "Alert uses p-alert-6 warning variant",
      "Empty uses p-empty-1 pattern",
      "Both states accessible to screen readers",
    ],
    files: ["apps/web/app/(authenticated)/page.tsx"],
    relatedTasks: ["P20-05"],
  });

  leaf("P20-05", "P20", "Add dashboard page tests", "web", {
    depends_on: ["P20-04"],
    context:
      "Integration tests for dashboard with MSW mocking GET /status and POST /poll/trigger. Verify card stats, table rows, empty state, error alert, and poll button behavior. " +
      "npm test -- dashboard in CI.",
    specs: [
      { ...SPEC.ui, note: "§8.3 dashboard behavior" },
      { ...SPEC.mvp, note: "Dashboard AC coverage" },
      { ...SPEC.ci, note: "Vitest in ci.yml" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/page.test.tsx",
      backend: "MSW status fixtures",
      frontend: "RTL within QueryClientProvider",
      data: "N/A",
      quality: "Multiple fixture scenarios",
      cicd: "npm test -- dashboard",
      deploy: "N/A",
    }),
    implementation: [
      "Mock status with lastRun and repos",
      "Assert poll card counters",
      "Assert table row count",
      "Mock empty repos → empty state",
      "Mock errors → alert visible",
      "Click poll button → assert POST called",
    ],
    acceptance: [
      "npm test -- dashboard passes",
      "Test renders summary card with fixture data",
      "Test shows empty state without repos",
      "Test shows alert with errors fixture",
      "Test poll button calls trigger endpoint",
      "Test table renders correct column values",
      "Tests use MSW not live API",
      "No console errors in test output",
    ],
    files: ["apps/web/app/(authenticated)/page.test.tsx"],
    tests: ["npm test -- dashboard"],
    relatedTasks: ["P20-01", "P20-02", "P20-03", "P20-04"],
  });

  leaf("P20-06", "P20", "Add poll run history table and run detail drawer", "web", {
    depends_on: ["P20-01", "P17-06"],
    context:
      "Backend exposes GET /api/v1/poll/runs and GET /api/v1/poll/runs/{id} with events[] (P15-03, P15-04). " +
      "Dashboard last-run card (P20-01) is insufficient alone — operators need run history and per-repo event detail. " +
      "This task closes backend/frontend drift: every poll API endpoint has a UI consumer. " +
      "Use usePollRuns and usePollRun hooks from P17-06.",
    specs: [
      { ...SPEC.api, note: "§7.1 GET /poll/runs and /poll/runs/{id} with events" },
      { ...SPEC.ui, note: "§8.3 dashboard — extend with run history; p-table-4, p-drawer-10" },
      { ...SPEC.domain, note: "§5.6 poll_run_events.action values in event list" },
      { ...SPEC.mvp, note: "AC #14 operational visibility" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/dashboard/poll-run-history.tsx",
      backend: "usePollRuns, usePollRun hooks",
      frontend: "p-table-4 for runs; p-drawer-10 for event timeline",
      data: "Display action: baseline|skip|create|supersede|merge|skip_open|error",
      quality: "MSW tests for history + drawer",
      cicd: "npm test -- poll-run-history",
      deploy: "N/A",
    }),
    implementation: [
      "PollRunHistory section below last-run card on dashboard",
      "Table columns: startedAt, status, reposChecked, ticketsCreated, ticketsSuperseded, errors count",
      "Row click opens drawer with usePollRun(id)",
      "Drawer lists poll_run_events: action, detail, createdAt, repo reference",
      "Badge colors per action type (p-badge-5 semantic)",
      "Empty state when no runs yet",
      "Pagination or load-more using poll/runs limit param",
      "Link from last-run card View all runs scrolls to history section",
    ],
    acceptance: [
      "Dashboard renders poll run history table",
      "Table data from GET /poll/runs via usePollRuns",
      "Clicking run opens drawer with events from GET /poll/runs/{id}",
      "Each event shows action matching §5.6 enum",
      "Error events display repoId and message from detail",
      "Drawer uses p-drawer-10 particle",
      "Table uses p-table-4 sortable columns",
      "No direct fetch in component — hooks only",
      "npm test -- poll-run-history passes",
    ],
    files: [
      "apps/web/components/dashboard/poll-run-history.tsx",
      "apps/web/components/dashboard/poll-run-detail-drawer.tsx",
    ],
    tests: ["npm test -- poll-run-history"],
    relatedTasks: ["P15-03", "P15-04", "P17-06", "P20-01"],
  });
}

/** @param {Registry} reg */
function registerP21({ epic, leaf }) {
  epic("P21", "Integrations UI", "web", {
    depends_on: ["P18-05", "P17-04"],
    context:
      "Integrations page at /integrations manages source and ticket provider credentials per specs.html §8.3 /integrations. " +
      "Eight kind values supported; secrets never re-displayed after save. Test connection uses POST /integrations/{id}/test with toast feedback. " +
      "Particles: p-table-2, p-drawer-10, p-field-1/2, p-input-1 password, p-toast-5.",
    specs: [
      { ...SPEC.ui, note: "§8.3 /integrations particle table" },
      { ...SPEC.api, note: "§7.3 Integrations CRUD + test" },
      { ...SPEC.providers, note: "§6.3 connectivity test per kind" },
      { ...SPEC.mvp, note: "AC #2 all eight kinds" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/integrations/",
      backend: "Integration API without secrets in GET",
      frontend: "Table, Drawer, password input",
      data: "N/A",
      quality: "integrations test suite P21-06",
      cicd: "npm test -- integrations",
      deploy: "N/A",
    }),
    implementation: [
      "List table grouped or flat by kind",
      "Create/edit drawer with kind-specific baseUrl fields",
      "Secret field password type; blank on edit means unchanged",
      "Test connection button with promise toast",
      "Delete with 409 handling when referenced",
      "Mutation hooks from P17-05",
    ],
    acceptance: [
      "Table lists all integrations with kind and name",
      "Create drawer supports all eight kind values",
      "Secret never shown after save (hasSecret only)",
      "Test connection shows success/failure toast",
      "Delete blocked with message when repos reference integration",
      "baseUrl shown for gitlab, gitea, forgejo, phasical, jira kinds",
      "Jira shows email field per §8.3",
      "Page matches §8.3 particle mapping",
    ],
  });

  leaf("P21-01", "P21", "Build integrations list table", "web", {
    context:
      "Integrations list uses p-table-2 (https://coss.com/ui/r/p-table-2.json) card-style table showing name, kind, baseUrl, hasSecret, updatedAt. " +
      "Row actions open edit drawer or delete. Add Integration button in page header. " +
      "Data from useIntegrations hook.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-table-2 integration list" },
      { ...SPEC.api, note: "GET /integrations list shape" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/integrations/page.tsx",
      backend: "useIntegrations",
      frontend: "p-table-2",
      data: "N/A",
      quality: "RTL table headers",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "IntegrationsPage with AppFrame title Integrations",
      "Table columns: name, kind, baseUrl, hasSecret badge, actions",
      "Add Integration opens create drawer",
      "Loading skeleton rows",
      "Empty state when no integrations",
      "Kind displayed as readable label",
    ],
    acceptance: [
      "Table renders integration rows from API",
      "kind column shows github, gitlab, etc.",
      "hasSecret shown as Yes/No or icon",
      "Add button visible in header",
      "Edit action per row",
      "Empty state when list empty",
      "No secret column in table",
      "Page at /integrations",
    ],
    files: ["apps/web/app/(authenticated)/integrations/page.tsx"],
    relatedTasks: ["P21-02"],
  });

  leaf("P21-02", "P21", "Add create and edit integration drawer", "web", {
    context:
      "Create and edit use p-drawer-10 (https://coss.com/ui/r/p-drawer-10.json) mobile-friendly form. Kind select on create only; kind-specific baseUrl fields for gitlab, gitea, forgejo, phasical, jira per §8.3. " +
      "Name field always required. Edit mode pre-fills name and baseUrl but not secret.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-drawer-10, p-field-1 base URL fields" },
      { ...SPEC.api, note: "POST/PATCH integration body" },
      { ...SPEC.providers, note: "Kind-specific baseUrl requirements" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/integrations/integration-drawer.tsx",
      backend: "create/update mutations",
      frontend: "Drawer + Select + Field",
      data: "N/A",
      quality: "Test kind switches visible fields",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "IntegrationDrawer open state from page",
      "Kind select on create (all 8 kinds)",
      "Conditional baseUrl field by kind",
      "Jira email field p-field-2",
      "Save calls create or patch mutation",
      "Close on success",
    ],
    acceptance: [
      "Kind-specific base URL fields per spec",
      "Create sends kind, name, baseUrl?, secret",
      "Edit disables kind change",
      "Name required validation",
      "Drawer closes on successful save",
      "GitHub kind hides baseUrl",
      "GitLab shows baseUrl required",
      "Form resets on close",
    ],
    files: ["apps/web/components/integrations/integration-drawer.tsx"],
    relatedTasks: ["P21-03", "P21-04"],
  });

  leaf("P21-03", "P21", "Add secret password input", "web", {
    context:
      "Secret/token field uses p-input-1 (https://coss.com/ui/r/p-input-1.json) type password. On edit, placeholder indicates leave blank to keep existing; never fetch or display saved secret. " +
      "Create requires non-empty secret. Aligns with API hasSecret flag in list.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-input-1 password — never re-display" },
      { ...SPEC.api, note: "secret one-time on POST; optional replace on PATCH" },
      { ...SPEC.auth, note: "Secrets encrypted at rest in Go" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/integrations/secret-field.tsx",
      backend: "N/A",
      frontend: "Password input component",
      data: "N/A",
      quality: "Assert value cleared after save",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "SecretField reusable component",
      "type=password, autoComplete=off",
      "Create: required",
      "Edit: optional with helper text",
      "Clear local state after successful save",
      "Never log value",
    ],
    acceptance: [
      "Never re-display secret after save",
      "Create requires secret non-empty",
      "Edit allows empty secret (unchanged)",
      "Helper text on edit: leave blank to keep",
      "Input type password masks characters",
      "hasSecret true in list after create without showing value",
      "autocomplete off on field",
      "No secret in React devtools persisted state",
    ],
    files: ["apps/web/components/integrations/secret-field.tsx"],
    relatedTasks: ["P21-02"],
  });

  leaf("P21-04", "P21", "Add test connection button with toast", "web", {
    depends_on: ["P21-02"],
    context:
      "Test connection button in drawer calls POST /integrations/{id}/test via useTestIntegration. Uses p-toast-5 (https://coss.com/ui/r/p-toast-5.json) promise pattern for async feedback. " +
      "Only available after integration saved (has id). Maps Go test result to success/error toast.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-button-1 Test connection, p-toast-5 promise" },
      { ...SPEC.api, note: "POST /integrations/{id}/test" },
      { ...SPEC.providers, note: "§6.3 per-kind connectivity" },
      { ...SPEC.mvp, note: "AC #5 integration test in UI" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/integrations/test-button.tsx",
      backend: "Test endpoint",
      frontend: "Promise toast",
      data: "N/A",
      quality: "MSW test success/fail",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "TestConnectionButton in drawer footer",
      "Disabled until integration has id",
      "Calls useTestIntegration mutation",
      "toast.promise with loading/success/error",
      "Display message from API on failure",
      "Loading state on button",
    ],
    acceptance: [
      "Test connection shows loading toast",
      "Success toast on 200 test result",
      "Error toast with API message on failure",
      "Button disabled during test",
      "Not shown on create before first save",
      "Works for all integration kinds",
      "Uses p-toast-5 promise pattern",
      "Does not expose secret in toast",
    ],
    files: ["apps/web/components/integrations/test-button.tsx"],
    relatedTasks: ["P17-05"],
  });

  leaf("P21-05", "P21", "Add delete integration flow", "web", {
    context:
      "Delete integration with confirmation alert-dialog. API returns 409 when repos or ticket projects still reference the integration — show error toast with message. " +
      "Successful delete invalidates integrations list and closes drawer if open.",
    specs: [
      { ...SPEC.api, note: "DELETE /integrations/{id} 409 when referenced" },
      { ...SPEC.ui, note: "Delete confirmation pattern" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/integrations/delete-integration.tsx",
      backend: "useDeleteIntegration",
      frontend: "Alert dialog confirm",
      data: "N/A",
      quality: "Test 409 error display",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "Delete button in table row and drawer",
      "Confirm dialog before delete",
      "useDeleteIntegration mutation",
      "Handle 409 with toast.error",
      "Invalidate ['integrations'] on success",
      "Close drawer if deleting current item",
    ],
    acceptance: [
      "Confirm dialog before delete",
      "Successful delete removes row from table",
      "409 shows user-friendly error toast",
      "Delete invalidates integrations query",
      "Cancel closes dialog without API call",
      "Delete works from table row actions",
      "Referenced integration cannot be deleted",
      "No silent failure on error",
    ],
    files: ["apps/web/components/integrations/delete-integration.tsx"],
    relatedTasks: ["P21-06"],
  });

  leaf("P21-06", "P21", "Add integrations UI tests", "web", {
    depends_on: ["P21-05"],
    context:
      "Vitest suite covers list render, create flow, secret behavior, test button, delete, and 409 error. MSW mocks all §7.3 endpoints. npm test -- integrations.",
    specs: [
      { ...SPEC.ui, note: "§8.3 /integrations flows" },
      { ...SPEC.mvp, note: "AC #2 integrations UI" },
      { ...SPEC.ci, note: "CI web tests" },
    ],
    stack: stackRefs({
      layout: "integrations/*.test.tsx",
      backend: "MSW integrations API",
      frontend: "RTL userEvent",
      data: "N/A",
      quality: "End-to-end UI flows mocked",
      cicd: "npm test -- integrations",
      deploy: "N/A",
    }),
    implementation: [
      "List render test with fixtures",
      "Open drawer and submit create",
      "Assert secret not in DOM after save",
      "Test connection mock",
      "Delete confirm flow",
      "409 delete error toast",
    ],
    acceptance: [
      "npm test -- integrations passes",
      "Create integration API called with secret",
      "Secret field cleared after submit",
      "Test button triggers test endpoint",
      "Delete confirm removes item on 200",
      "409 delete shows error",
      "All tests use MSW",
      "No live network in CI",
    ],
    files: ["apps/web/app/(authenticated)/integrations/page.test.tsx"],
    tests: ["npm test -- integrations"],
    relatedTasks: ["P21-01", "P21-02", "P21-03", "P21-04", "P21-05"],
  });
}

/** @param {Registry} reg */
function registerP22({ epic, leaf }) {
  epic("P22", "Ticket projects UI", "web", {
    depends_on: ["P18-05", "P17-04"],
    context:
      "Ticket projects page at /ticket-projects configures per-project status mapping and on_open_ticket_policy per specs.html §5.3 and §8.3. " +
      "Multiple projects per ticket integration; create_config tabs vary by provider (Phasical, Jira, Linear). " +
      "Particles: p-table-3, p-drawer-10, p-tabs-1, p-field-2 status mapping, p-alert-5 note.",
    specs: [
      { ...SPEC.ui, note: "§8.3 /ticket-projects particles" },
      { ...SPEC.ticketProjects, note: "§5.3 status mapping and policies" },
      { ...SPEC.api, note: "§7.4 Ticket Projects CRUD" },
      { ...SPEC.domain, note: "§5.2.1 on_open_ticket_policy values" },
      { ...SPEC.mvp, note: "AC #3, #10, #11 ticket projects" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/ticket-projects/",
      backend: "Ticket project API",
      frontend: "Drawer, tabs, mapping form",
      data: "N/A",
      quality: "ticket-projects tests P22-06",
      cicd: "npm test -- ticket-projects",
      deploy: "N/A",
    }),
    implementation: [
      "Table grouped by integration",
      "Drawer create/edit with integration select (ticket kinds only)",
      "create_config tabs per provider kind",
      "Status mapping arrays for open/done/cancelled + superseded",
      "on_open_ticket_policy select: supersede, merge, skip_if_open",
      "Alert note: mapping differs per project",
    ],
    acceptance: [
      "Table lists ticket projects with integration name",
      "Create requires ticket integration (phasical, jira, linear)",
      "statusMapping saved per §7.4 JSON shape",
      "onOpenTicketPolicy select with three values",
      "create_config tabs switch by integration kind",
      "externalProjectId and display name fields",
      "Delete 409 when repos reference project",
      "Alert explains per-project mapping",
    ],
  });

  leaf("P22-01", "P22", "Build ticket projects table", "web", {
    context:
      "Project list uses p-table-3 (https://coss.com/ui/r/p-table-3.json) grouped by integration. Columns: name, externalProjectId, integration, policy, actions. " +
      "useTicketProjects hook loads data. Filter ticket integrations for create button context.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-table-3 project list grouped by integration" },
      { ...SPEC.api, note: "GET /ticket-projects" },
      { ...SPEC.ticketProjects, note: "§5.3 multiple projects per integration" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/ticket-projects/page.tsx",
      backend: "useTicketProjects",
      frontend: "p-table-3",
      data: "N/A",
      quality: "RTL grouped rows",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "TicketProjectsPage with frame title",
      "Table with integration grouping header rows",
      "Columns: name, external id, policy badge",
      "Add Ticket Project button",
      "Edit/delete row actions",
      "Empty state when none",
    ],
    acceptance: [
      "Table shows all ticket projects",
      "Grouped or sorted by integration name",
      "Policy displayed as readable label",
      "externalProjectId column present",
      "Add button opens drawer",
      "Empty state when no projects",
      "Page at /ticket-projects",
      "No create_config secrets in table",
    ],
    files: ["apps/web/app/(authenticated)/ticket-projects/page.tsx"],
    relatedTasks: ["P22-02"],
  });

  leaf("P22-02", "P22", "Add create and edit drawer", "web", {
    context:
      "Create/edit drawer uses p-drawer-10 (https://coss.com/ui/r/p-drawer-10.json). Integration select filtered to ticket kinds only (phasical, jira, linear). " +
      "Fields: externalProjectId, display name via p-field-1. Foundation for tabs and mapping in later leaves.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-drawer-10, p-select-1 integration, p-field-1 ids" },
      { ...SPEC.api, note: "POST /ticket-projects body" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/ticket-projects/project-drawer.tsx",
      backend: "create/update mutations",
      frontend: "p-drawer-10",
      data: "N/A",
      quality: "Test integration filter",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "ProjectDrawer component",
      "Integration select from useIntegrations filtered",
      "externalProjectId and name fields",
      "Create vs edit mode",
      "Save POST or PATCH",
      "Slots for tabs and mapping subforms",
    ],
    acceptance: [
      "p-drawer-10 used for create/edit",
      "Integration select shows ticket kinds only",
      "externalProjectId required",
      "Display name required",
      "Edit pre-fills fields",
      "Create POST sends integrationId",
      "Drawer closes on success",
      "Validation errors from API shown",
    ],
    files: ["apps/web/components/ticket-projects/project-drawer.tsx"],
    relatedTasks: ["P22-03", "P22-04", "P22-05"],
  });

  leaf("P22-03", "P22", "Add provider-specific create_config tabs", "web", {
    context:
      "create_config subform uses p-tabs-1 (https://coss.com/ui/r/p-tabs-1.json) — one tab panel per ticket provider kind with fields for issueType, priority, teamId, etc. per §7.4 examples. " +
      "Tab visible set driven by selected integration kind. JSON stored in createConfig field.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-tabs-1 create_config per provider" },
      { ...SPEC.api, note: "createConfig object in POST body" },
      { ...SPEC.providers, note: "Ticket provider create payloads" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/ticket-projects/create-config-tabs.tsx",
      backend: "Provider-specific JSON schema",
      frontend: "Tabs per kind",
      data: "N/A",
      quality: "Test Phasical vs Jira fields",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "CreateConfigTabs reads integration kind",
      "Phasical tab: default fields per spec",
      "Jira tab: issueType, priority",
      "Linear tab: teamId or equivalent",
      "Serialize to createConfig on save",
      "Parse existing createConfig on edit",
    ],
    acceptance: [
      "Tabs switch fields by integration kind",
      "Phasical createConfig fields match spec example",
      "Jira issueType and priority inputs",
      "Linear team fields present",
      "createConfig included in POST/PATCH body",
      "Invalid JSON prevented by form validation",
      "Tabs hidden until integration selected",
      "Edit loads existing createConfig values",
    ],
    files: ["apps/web/components/ticket-projects/create-config-tabs.tsx"],
    relatedTasks: ["P22-02"],
  });

  leaf("P22-04", "P22", "Add status mapping form", "web", {
    context:
      "Status mapping form uses p-field-2 (https://coss.com/ui/r/p-field-2.json) for open/done/cancelled status arrays (comma-separated or multi-input) and superseded single value. " +
      "Critical for per-project ticket state detection per §5.3. p-alert-5 note that mapping differs per project.",
    specs: [
      { ...SPEC.ticketProjects, note: "§5.3 status_mapping open/done/cancelled/superseded" },
      { ...SPEC.ui, note: "§8.3 p-field-2 mapping, p-alert-5 note" },
      { ...SPEC.mvp, note: "AC #11 per-project mapping" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/ticket-projects/status-mapping-form.tsx",
      backend: "statusMapping in API",
      frontend: "Field arrays + alert",
      data: "N/A",
      quality: "Serialize arrays correctly",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "StatusMappingForm subcomponent",
      "Inputs for open[], done[], cancelled[] arrays",
      "superseded single string field",
      "p-alert-5 info: mapping is different per project",
      "Validate non-empty open and done arrays",
      "Map to statusMapping camelCase JSON",
    ],
    acceptance: [
      "open, done, cancelled arrays editable",
      "superseded field for supersede target status",
      "Alert note visible per p-alert-5",
      "Saved mapping matches §7.4 JSON shape",
      "Empty open array blocked on submit",
      "Comma-separated or tag input UX documented",
      "Edit loads existing mapping arrays",
      "Different projects can have different mappings",
    ],
    files: ["apps/web/components/ticket-projects/status-mapping-form.tsx"],
    relatedTasks: ["P22-05"],
  });

  leaf("P22-05", "P22", "Add on_open_ticket_policy select", "web", {
    context:
      "Policy select uses p-select-1 (https://coss.com/ui/r/p-select-1.json) with values supersede, merge, skip_if_open per §5.2.1. " +
      "Default supersede for new projects. Help text explains behavior on new release when open ticket exists.",
    specs: [
      { ...SPEC.domain, note: "§5.2.1 on_open_ticket_policy behaviors" },
      { ...SPEC.ui, note: "§8.3 p-select-1 on_open_ticket_policy" },
      { ...SPEC.mvp, note: "AC #9, #10 policy behaviors" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/ticket-projects/policy-select.tsx",
      backend: "onOpenTicketPolicy in API",
      frontend: "Select with labels",
      data: "N/A",
      quality: "All three values selectable",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "PolicySelect component",
      "Options: supersede, merge, skip_if_open",
      "Human labels and descriptions",
      "Default supersede on create",
      "Included in drawer save payload",
      "Show in table as badge",
    ],
    acceptance: [
      "Select offers supersede, merge, skip_if_open",
      "Default value supersede on new project",
      "Value saved as onOpenTicketPolicy",
      "Help text describes each policy",
      "Edit shows current policy",
      "Required field validation",
      "Policy visible in table list",
      "Matches §5.2.1 domain rules",
    ],
    files: ["apps/web/components/ticket-projects/policy-select.tsx"],
    relatedTasks: ["P22-06"],
  });

  leaf("P22-06", "P22", "Add ticket projects UI tests", "web", {
    depends_on: ["P22-05"],
    context:
      "Test suite for ticket projects CRUD, tabs, mapping form, and policy select. MSW mocks §7.4. npm test -- ticket-projects.",
    specs: [
      { ...SPEC.ui, note: "§8.3 /ticket-projects" },
      { ...SPEC.ticketProjects, note: "§5.3 mapping validation" },
      { ...SPEC.ci, note: "CI web job" },
    ],
    stack: stackRefs({
      layout: "ticket-projects/*.test.tsx",
      backend: "MSW ticket-projects",
      frontend: "RTL",
      data: "N/A",
      quality: "Full drawer flow test",
      cicd: "npm test -- ticket-projects",
      deploy: "N/A",
    }),
    implementation: [
      "List render test",
      "Create project with mapping",
      "Tab fields per kind",
      "Policy select value in POST body",
      "Edit pre-fill test",
      "Delete flow",
    ],
    acceptance: [
      "npm test -- ticket-projects passes",
      "Create sends statusMapping object",
      "Policy included in payload",
      "createConfig varies by kind test",
      "Mapping alert rendered",
      "Edit loads existing data",
      "MSW isolation between tests",
      "No regression on integrations filter",
    ],
    files: ["apps/web/app/(authenticated)/ticket-projects/page.test.tsx"],
    tests: ["npm test -- ticket-projects"],
    relatedTasks: ["P22-01", "P22-02", "P22-03", "P22-04", "P22-05"],
  });
}

/** @param {Registry} reg */
function registerP23({ epic, leaf }) {
  epic("P23", "Monitored repos UI", "web", {
    depends_on: ["P18-05", "P17-03"],
    context:
      "Repos page at /repos manages monitored repositories per specs.html §8.3 /repos. " +
      "Table with checkboxes, dialog form for add/edit, source and ticket project selects, notification target checkboxes, enabled toggle, delete confirm. " +
      "Combines source_kind, integrations, ticket projects, and notification targets.",
    specs: [
      { ...SPEC.ui, note: "§8.3 /repos particles" },
      { ...SPEC.api, note: "§7.5 Monitored Repos CRUD" },
      { ...SPEC.domain, note: "sourceKind + ticketProjectId pairing" },
      { ...SPEC.mvp, note: "AC #4 any source/ticket combination" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/repos/",
      backend: "Repos API + related selects data",
      frontend: "Table, Dialog, Select, Checkbox, Switch",
      data: "N/A",
      quality: "repos tests P23-06",
      cicd: "npm test -- repos",
      deploy: "N/A",
    }),
    implementation: [
      "Table p-table-3 with row checkboxes",
      "Dialog p-dialog-1 add/edit form",
      "sourceKind and integration selects",
      "ticketProjectId select filtered by ticket integration",
      "notificationTargetIds checkbox group",
      "Enabled switch and delete alert-dialog",
    ],
    acceptance: [
      "Table lists repos with source and ticket project",
      "Add dialog creates repo per §7.5 POST body",
      "sourceKind select drives integration visibility",
      "ticketProject select required",
      "Notification targets optional multi-select",
      "Enabled toggle updates via PATCH",
      "Delete confirms via p-alert-dialog-1",
      "Badges for source_kind in table",
    ],
  });

  leaf("P23-01", "P23", "Build repos table with checkboxes", "web", {
    context:
      "Repos table uses p-table-3 (https://coss.com/ui/r/p-table-3.json) with checkboxes for optional bulk enable/disable. Columns: source, path, ticket project, enabled, last tag, actions. " +
      "useRepos hook for data. Row click or edit opens dialog.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-table-3 checkboxes repo list" },
      { ...SPEC.api, note: "GET /repos" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/repos/page.tsx",
      backend: "useRepos",
      frontend: "p-table-3 checkboxes",
      data: "N/A",
      quality: "Checkbox selection test",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "ReposPage with Add Repo button",
      "Table with checkbox column",
      "Display sourceKind badge p-badge-1",
      "ticket project name column",
      "Enabled indicator",
      "Row edit/delete actions",
    ],
    acceptance: [
      "Table renders all monitored repos",
      "Checkbox column per p-table-3",
      "source_kind badge visible",
      "ticket project name shown",
      "Enabled state visible per row",
      "Add Repo opens dialog",
      "Empty state when no repos",
      "Page at /repos",
    ],
    files: ["apps/web/app/(authenticated)/repos/page.tsx"],
    relatedTasks: ["P23-02"],
  });

  leaf("P23-02", "P23", "Add add and edit repo dialog", "web", {
    context:
      "Add/edit repo uses p-dialog-1 (https://coss.com/ui/r/p-dialog-1.json) form layout. projectPath text field, enabled switch stub. " +
      "Full selects added in P23-03. Dialog mode create vs edit from row action.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-dialog-1 Add / Edit Repo" },
      { ...SPEC.api, note: "POST/PATCH /repos" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/repos/repo-dialog.tsx",
      backend: "create/update repo mutations",
      frontend: "Dialog form",
      data: "N/A",
      quality: "Open/close dialog test",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "RepoDialog controlled open state",
      "projectPath required field",
      "Title Add Repo vs Edit Repo",
      "Save button triggers mutation",
      "Cancel closes without save",
      "Form reset on close",
    ],
    acceptance: [
      "Dialog opens for add and edit",
      "projectPath required",
      "Create POST on save for new repo",
      "Edit PATCH on save for existing",
      "Dialog closes on successful save",
      "Cancel discards changes",
      "Form clears when reopened for add",
      "Uses p-dialog-1 form pattern",
    ],
    files: ["apps/web/components/repos/repo-dialog.tsx"],
    relatedTasks: ["P23-03", "P23-04", "P23-05"],
  });

  leaf("P23-03", "P23", "Add source and ticket project selects", "web", {
    context:
      "Source configuration uses p-select-1 (https://coss.com/ui/r/p-select-1.json) for sourceKind (github, gitlab, gitea, forgejo, codeberg) and optional sourceIntegrationId when kind requires PAT. " +
      "ticketProjectId select lists all ticket projects. Validates combinations per MVP AC #4.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-select-1 source kind, integration, ticket project" },
      { ...SPEC.api, note: "§7.5 POST body fields" },
      { ...SPEC.mvp, note: "AC #4 any source/ticket combo" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/repos/repo-form.tsx",
      backend: "useIntegrations + useTicketProjects",
      frontend: "Select fields",
      data: "N/A",
      quality: "Test kind changes integration list",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "sourceKind select all five source kinds",
      "sourceIntegrationId select filtered by kind",
      "GitHub may allow null integration if env token",
      "ticketProjectId select required",
      "Disable save until ticket project chosen",
      "Edit mode pre-selects values",
    ],
    acceptance: [
      "sourceKind select lists all MVP source kinds",
      "sourceIntegrationId filtered by selected kind",
      "ticketProjectId required before save",
      "Any valid source+ticket combo submittable",
      "Edit shows current selections",
      "Integration select hidden when not needed",
      "Validation message when ticket project missing",
      "POST body matches §7.5 shape",
    ],
    files: ["apps/web/components/repos/repo-form.tsx"],
    relatedTasks: ["P23-02"],
  });

  leaf("P23-04", "P23", "Add notification targets checkbox group", "web", {
    context:
      "Optional notification targets per repo via p-checkbox-group-1 (https://coss.com/ui/r/p-checkbox-group-1.json). Lists enabled targets from useNotificationTargets. " +
      "Maps to notificationTargetIds array in POST/PATCH. Empty means no per-repo overrides (global targets still apply per domain rules).",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-checkbox-group-1 notification targets per repo" },
      { ...SPEC.notifications, note: "§5.5 per-repo target assignment" },
      { ...SPEC.api, note: "notificationTargetIds in repo body" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/repos/notification-targets-field.tsx",
      backend: "useNotificationTargets",
      frontend: "Checkbox group",
      data: "N/A",
      quality: "Selected ids in payload",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "NotificationTargetsField component",
      "Checkbox per enabled target",
      "Label shows target name",
      "Controlled selectedIds state",
      "Include in repo form save",
      "Optional — none selected sends [] or omit",
    ],
    acceptance: [
      "All enabled targets listed as checkboxes",
      "Selected ids sent as notificationTargetIds",
      "Edit pre-checks assigned targets",
      "Uncheck removes id from array",
      "Empty selection allowed",
      "Disabled targets not shown",
      "Accessible group label",
      "Works inside repo dialog form",
    ],
    files: ["apps/web/components/repos/notification-targets-field.tsx"],
    relatedTasks: ["P23-02"],
  });

  leaf("P23-05", "P23", "Add enabled toggle and delete confirm", "web", {
    context:
      "Enabled toggle uses p-switch-1 (https://coss.com/ui/r/p-switch-1.json) in form and optionally inline in table. Delete uses p-alert-dialog-1 (https://coss.com/ui/r/p-alert-dialog-1.json) confirmation before DELETE /repos/{id}.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-switch-1 enabled, p-alert-dialog-1 delete" },
      { ...SPEC.api, note: "PATCH enabled, DELETE repo" },
      { ...SPEC.domain, note: "Disabled repos skipped in poll" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/repos/repo-actions.tsx",
      backend: "update/delete mutations",
      frontend: "Switch + alert dialog",
      data: "N/A",
      quality: "Delete confirm test",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "Enabled switch in repo form",
      "Quick toggle in table row optional",
      "Delete button opens alert-dialog",
      "Confirm calls useDeleteRepo",
      "Invalidate repos and status on change",
      "Toast on success/error",
    ],
    acceptance: [
      "Enabled toggle updates repo via PATCH",
      "Disabled repo shows in table as off",
      "Delete requires confirmation dialog",
      "Confirm delete calls DELETE endpoint",
      "Cancel delete closes dialog",
      "Status query invalidated after toggle",
      "Delete removes row on success",
      "Switch uses p-switch-1 pattern",
    ],
    files: ["apps/web/components/repos/repo-actions.tsx"],
    relatedTasks: ["P23-06"],
  });

  leaf("P23-06", "P23", "Add repos UI tests", "web", {
    depends_on: ["P23-05"],
    context:
      "Vitest suite for repos table, dialog CRUD, selects, notifications checkboxes, toggle, delete. MSW §7.5. npm test -- repos.",
    specs: [
      { ...SPEC.ui, note: "§8.3 /repos flows" },
      { ...SPEC.mvp, note: "AC #4 repo combinations" },
      { ...SPEC.ci, note: "CI web tests" },
    ],
    stack: stackRefs({
      layout: "repos/*.test.tsx",
      backend: "MSW repos API",
      frontend: "RTL",
      data: "N/A",
      quality: "Full CRUD flow",
      cicd: "npm test -- repos",
      deploy: "N/A",
    }),
    implementation: [
      "Table render test",
      "Create repo dialog flow",
      "Select source and ticket project",
      "Notification checkboxes in payload",
      "Toggle enabled",
      "Delete confirm",
    ],
    acceptance: [
      "npm test -- repos passes",
      "Create sends correct POST body",
      "ticketProjectId required validation",
      "notificationTargetIds in body when checked",
      "Delete confirm triggers DELETE",
      "Enabled toggle PATCH called",
      "MSW used throughout",
      "Codeberg+Phasical combo test case",
    ],
    files: ["apps/web/app/(authenticated)/repos/page.test.tsx"],
    tests: ["npm test -- repos"],
    relatedTasks: ["P23-01", "P23-02", "P23-03", "P23-04", "P23-05"],
  });
}

/** @param {Registry} reg */
function registerP24({ epic, leaf }) {
  epic("P24", "Notifications UI", "web", {
    depends_on: ["P18-05", "P17-04"],
    context:
      "Notifications page at /notifications configures Shoutrrr targets per specs.html §8.3 /notifications and §5.5. " +
      "Shoutrrr URL entered once as password-style input; events checkbox group for create, error, supersede; test notification button. " +
      "Particles: p-table-2, p-drawer-10, p-input-1, p-checkbox-group-1, p-toast-5.",
    specs: [
      { ...SPEC.ui, note: "§8.3 /notifications particles" },
      { ...SPEC.notifications, note: "§5.5 Shoutrrr targets and events" },
      { ...SPEC.api, note: "§7.6 Notification Targets CRUD + test" },
      { ...SPEC.mvp, note: "AC #12 notification targets" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/notifications/",
      backend: "Notification targets API",
      frontend: "Drawer, password URL, checkboxes",
      data: "N/A",
      quality: "notifications tests P24-05",
      cicd: "npm test -- notifications",
      deploy: "N/A",
    }),
    implementation: [
      "Table of targets: name, events, enabled",
      "Drawer create/edit with Shoutrrr URL field",
      "Events checkboxes: create, error, supersede",
      "Test notification button with toast",
      "Never re-display Shoutrrr URL after save",
      "Delete target flow",
    ],
    acceptance: [
      "Table lists notification targets without URL",
      "Create saves shoutrrrUrl once",
      "Events array persisted per target",
      "Test sends probe via POST .../test",
      "Enabled flag toggleable",
      "Shoutrrr URL never in GET response",
      "Toast feedback on test success/failure",
      "Matches §8.3 particle table",
    ],
  });

  leaf("P24-01", "P24", "Build notification targets table", "web", {
    context:
      "Target list uses p-table-2 (https://coss.com/ui/r/p-table-2.json) with columns name, events summary, enabled, actions. useNotificationTargets hook. No Shoutrrr URL column.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-table-2 target list" },
      { ...SPEC.api, note: "GET /notification-targets" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/notifications/page.tsx",
      backend: "useNotificationTargets",
      frontend: "p-table-2",
      data: "N/A",
      quality: "RTL no URL column",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "NotificationsPage with Add Target",
      "Table columns: name, events badges, enabled, actions",
      "Format events array as comma list or badges",
      "Edit/delete row actions",
      "Empty state",
    ],
    acceptance: [
      "Table shows name and events per target",
      "No Shoutrrr URL in table",
      "Enabled column shows on/off",
      "Add Target opens drawer",
      "Empty state when none",
      "Page at /notifications",
      "Events create/error/supersede displayed",
      "Loading state while fetching",
    ],
    files: ["apps/web/app/(authenticated)/notifications/page.tsx"],
    relatedTasks: ["P24-02"],
  });

  leaf("P24-02", "P24", "Add create and edit drawer with Shoutrrr URL", "web", {
    context:
      "Drawer p-drawer-10 with name field and Shoutrrr URL p-input-1 password style. URL never shown on edit — leave blank to keep. Same pattern as integration secrets.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-drawer-10, p-input-1 Shoutrrr URL password-style" },
      { ...SPEC.notifications, note: "Shoutrrr URL storage" },
      { ...SPEC.api, note: "POST/PATCH notification-targets" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/notifications/target-drawer.tsx",
      backend: "create/update mutations",
      frontend: "Drawer + password input",
      data: "N/A",
      quality: "URL not in DOM after save",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "TargetDrawer with name input",
      "shoutrrrUrl password input required on create",
      "Optional on edit with helper text",
      "enabled switch default true",
      "Save POST or PATCH",
      "Close on success",
    ],
    acceptance: [
      "Name required on create",
      "shoutrrrUrl required on create",
      "URL never re-displayed after save",
      "Edit allows blank URL unchanged",
      "Drawer uses p-drawer-10",
      "Password-style input masks URL",
      "Successful save closes drawer",
      "API error shown in form",
    ],
    files: ["apps/web/components/notifications/target-drawer.tsx"],
    relatedTasks: ["P24-03", "P24-04"],
  });

  leaf("P24-03", "P24", "Add events checkbox group", "web", {
    context:
      "Events field p-checkbox-group-1 with create, error, supersede options per §5.5. At least one event should be selected. Stored as events string array in API.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-checkbox-group-1 Events create, error, supersede" },
      { ...SPEC.notifications, note: "§5.5 event types" },
      { ...SPEC.api, note: "events field in target body" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/notifications/events-field.tsx",
      backend: "events[] in API",
      frontend: "Checkbox group",
      data: "N/A",
      quality: "All events combinable",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "EventsField with three checkboxes",
      "Values: create, error, supersede",
      "Default all checked or create+error",
      "Validate at least one selected",
      "Include in drawer save payload",
      "Pre-check on edit",
    ],
    acceptance: [
      "Three event checkboxes: create, error, supersede",
      "At least one event required",
      "events array in POST body",
      "Edit pre-selects saved events",
      "Uncheck removes from array",
      "Group accessible label",
      "Matches §5.5 event names exactly",
      "Displayed in table after save",
    ],
    files: ["apps/web/components/notifications/events-field.tsx"],
    relatedTasks: ["P24-02"],
  });

  leaf("P24-04", "P24", "Add test notification button", "web", {
    context:
      "Test notification button calls POST /notification-targets/{id}/test with p-toast-5 async feedback. Only after target saved. Probe message sent via Shoutrrr in Go.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-button-1 Test notification, p-toast-5" },
      { ...SPEC.api, note: "POST .../test probe" },
      { ...SPEC.notifications, note: "Test endpoint sends probe" },
      { ...SPEC.mvp, note: "AC #12 test targets" },
    ],
    stack: stackRefs({
      layout: "apps/web/components/notifications/test-button.tsx",
      backend: "useTestNotificationTarget",
      frontend: "Promise toast",
      data: "N/A",
      quality: "MSW test endpoint",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "TestNotificationButton in drawer",
      "Requires target id",
      "toast.promise pattern",
      "Show API error message on failure",
      "Disabled while pending",
    ],
    acceptance: [
      "Test button calls POST .../test",
      "Loading toast during request",
      "Success toast on 200",
      "Error toast on failure",
      "Disabled before target saved",
      "Does not expose Shoutrrr URL",
      "Uses p-toast-5 pattern",
      "Button label Test notification",
    ],
    files: ["apps/web/components/notifications/test-button.tsx"],
    relatedTasks: ["P24-05"],
  });

  leaf("P24-05", "P24", "Add notifications UI tests", "web", {
    depends_on: ["P24-04"],
    context:
      "Vitest suite for notifications CRUD, events, Shoutrrr URL handling, test button. npm test -- notifications.",
    specs: [
      { ...SPEC.ui, note: "§8.3 /notifications" },
      { ...SPEC.mvp, note: "AC #12" },
      { ...SPEC.ci, note: "CI web job" },
    ],
    stack: stackRefs({
      layout: "notifications/*.test.tsx",
      backend: "MSW notification-targets",
      frontend: "RTL",
      data: "N/A",
      quality: "URL secrecy test",
      cicd: "npm test -- notifications",
      deploy: "N/A",
    }),
    implementation: [
      "List render",
      "Create with URL and events",
      "URL not in document after save",
      "Test button mock",
      "Edit events change",
      "Delete target",
    ],
    acceptance: [
      "npm test -- notifications passes",
      "Create includes shoutrrrUrl in POST",
      "events array in payload",
      "Test endpoint called on button click",
      "No URL in list render HTML",
      "At least one event validation",
      "MSW isolation",
      "Delete removes target",
    ],
    files: ["apps/web/app/(authenticated)/notifications/page.test.tsx"],
    tests: ["npm test -- notifications"],
    relatedTasks: ["P24-01", "P24-02", "P24-03", "P24-04"],
  });
}

/** @param {Registry} reg */
function registerP25({ epic, leaf }) {
  epic("P25", "Settings UI", "web", {
    depends_on: ["P18-05", "P17-03"],
    context:
      "Settings page at /settings configures global poll interval per specs.html §8.3 /settings. " +
      "Single field pollIntervalMinutes with minimum 5 via p-number-field-1; save PATCHes /api/v1/settings and shows p-alert-5 success. " +
      "Uses useSettings and useUpdateSettings from P17.",
    specs: [
      { ...SPEC.ui, note: "§8.3 /settings — p-card-8, p-number-field-1, p-alert-5" },
      { ...SPEC.api, note: "§7.2 GET/PATCH /settings" },
      { ...SPEC.mvp, note: "AC #14 settings interval" },
      { ...SPEC.domain, note: "Poll scheduler reads interval from DB" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/settings/page.tsx",
      backend: "Settings API",
      frontend: "Number field, save button, alert",
      data: "N/A",
      quality: "settings tests P25-03",
      cicd: "npm test -- settings",
      deploy: "N/A",
    }),
    implementation: [
      "Settings card p-card-8 within frame",
      "pollIntervalMinutes number field min 5",
      "Save button PATCH settings",
      "Success alert p-alert-5 on save",
      "Load current value from useSettings",
      "Invalidate status on save (interval in dashboard)",
    ],
    acceptance: [
      "Settings page at /settings",
      "Poll interval field min 5 minutes",
      "Current value loaded from API",
      "Save PATCHes pollIntervalMinutes",
      "Success alert shown after save",
      "Validation blocks values below 5",
      "Error toast on API failure",
      "Matches §8.3 particle mapping",
    ],
  });

  leaf("P25-01", "P25", "Add poll interval number field", "web", {
    context:
      "Poll interval uses p-number-field-1 (https://coss.com/ui/r/p-number-field-1.json) with min 5 per spec. Label explains scheduler frequency. " +
      "Controlled input bound to useSettings data. Unit displayed as minutes.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-number-field-1 Poll interval min 5" },
      { ...SPEC.api, note: "§7.2 pollIntervalMinutes field" },
    ],
    stack: stackRefs({
      layout: "apps/web/app/(authenticated)/settings/page.tsx",
      backend: "useSettings",
      frontend: "p-number-field-1",
      data: "N/A",
      quality: "Min validation test",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "SettingsPage with p-card-8 section",
      "NumberField for pollIntervalMinutes",
      "min=5, step=1",
      "Label: Poll interval (minutes)",
      "Default from API on load",
      "Local state until save",
    ],
    acceptance: [
      "p-number-field-1 for poll interval",
      "Minimum value 5 enforced",
      "Value loaded from GET /settings",
      "Integer minutes only",
      "Field labeled clearly",
      "Shows current DB value on mount",
      "Cannot submit 4 or below",
      "Within p-card-8 settings section",
    ],
    files: ["apps/web/app/(authenticated)/settings/page.tsx"],
    relatedTasks: ["P25-02"],
  });

  leaf("P25-02", "P25", "Wire save settings with success alert", "web", {
    depends_on: ["P25-01"],
    context:
      "Save button p-button-1 calls useUpdateSettings PATCH. On success show p-alert-5 (https://coss.com/ui/r/p-alert-5.json) success alert Settings saved. " +
      "Invalidate settings and status queries. Disable save when unchanged or invalid.",
    specs: [
      { ...SPEC.ui, note: "§8.3 p-button-1 Save, p-alert-5 success" },
      { ...SPEC.api, note: "PATCH /settings body" },
    ],
    stack: stackRefs({
      layout: "settings/page.tsx save handler",
      backend: "useUpdateSettings mutation",
      frontend: "Button + Alert",
      data: "N/A",
      quality: "Save success test",
      cicd: "npm test",
      deploy: "N/A",
    }),
    implementation: [
      "Save button calls PATCH with pollIntervalMinutes",
      "Show p-alert-5 on success",
      "invalidateQueries settings and status",
      "Disable when value unchanged",
      "Loading state on button",
      "Error toast on failure",
    ],
    acceptance: [
      "Save sends PATCH /settings",
      "Success alert p-alert-5 displayed",
      "Alert message Settings saved",
      "Save disabled when no changes",
      "Status refetched after save",
      "Error toast on validation error",
      "Button shows loading while saving",
      "Value persists after page reload",
    ],
    files: ["apps/web/app/(authenticated)/settings/page.tsx"],
    relatedTasks: ["P25-03"],
  });

  leaf("P25-03", "P25", "Add settings page tests", "web", {
    depends_on: ["P25-02"],
    context:
      "Vitest tests for settings load, min validation, save PATCH, success alert. MSW §7.2. npm test -- settings.",
    specs: [
      { ...SPEC.ui, note: "§8.3 /settings behavior" },
      { ...SPEC.ci, note: "CI web tests" },
      { ...SPEC.mvp, note: "Settings interval AC" },
    ],
    stack: stackRefs({
      layout: "settings/page.test.tsx",
      backend: "MSW settings API",
      frontend: "RTL",
      data: "N/A",
      quality: "Save flow coverage",
      cicd: "npm test -- settings",
      deploy: "N/A",
    }),
    implementation: [
      "Load fixture pollIntervalMinutes",
      "Assert field shows value",
      "Change value and save",
      "Assert PATCH body",
      "Assert success alert",
      "Test min 5 validation",
    ],
    acceptance: [
      "npm test -- settings passes",
      "Field renders with API value",
      "Save calls PATCH with new value",
      "Success alert appears after save",
      "Value below 5 blocked",
      "Save disabled when unchanged",
      "MSW used",
      "Error path tested",
    ],
    files: ["apps/web/app/(authenticated)/settings/page.test.tsx"],
    tests: ["npm test -- settings"],
    relatedTasks: ["P25-01", "P25-02"],
  });
}

/** @param {Registry} reg */
function registerP26({ epic, leaf }) {
  epic("P26", "Docker image & compose", "ci", {
    depends_on: ["P15-05", "P25-03"],
    context:
      "Package Release Ops as a single Docker image per specs.html §10 Deployment: Next standalone + Go binary, one service compose file, entrypoint runs migrations then Go then Next. " +
      "Image ghcr.io/{owner}/release-ops published via P27 workflows. " +
      "Volume /data for app.db and persistence.",
    specs: [
      { ...SPEC.deployment, note: "§10 one image, compose, entrypoint order" },
      { ...SPEC.architecture, note: "Go :8080 loopback, Next :3000 public" },
      { ...SPEC.env, note: "SESSION_SECRET, APP_ENCRYPTION_KEY, APP_PUBLIC_URL" },
      { ...SPEC.ci, note: "§11.6 GHCR image build" },
    ],
    stack: stackRefs({
      layout: "Dockerfile, docker/, docker-compose.yml root",
      backend: "Go binary in image",
      frontend: "Next standalone output",
      data: "SQLite at /data/app.db volume",
      quality: "Local compose smoke doc P26-04",
      cicd: "build-and-push-image.yml",
      deploy: "docker compose up single service",
    }),
    schema: [
      { ...SCHEMA.sql, note: "Migrations applied at container start" },
    ],
    implementation: [
      "Multi-stage Dockerfile: node build, go build, runtime",
      "Next output: standalone in final image",
      "Copy migrations/ and golang-migrate binary",
      "entrypoint.sh: migrate up → go server → next start",
      "docker-compose.yml one release-ops service",
      "Health probe GET /api/go/healthz",
      "Document env vars in getting-started",
    ],
    acceptance: [
      "docker build produces single image with Go + Next",
      "entrypoint runs migrate before servers",
      "Go listens 127.0.0.1:8080 inside container",
      "Next listens :3000 published",
      "compose one service with volume /data",
      "Health check passes after start",
      "No runtime secrets baked into image",
      "Matches §10 reference compose",
    ],
  });

  leaf("P26-01", "P26", "Create multi-stage Dockerfile", "ci", {
    context:
      "Dockerfile builds Next.js standalone and Go cmd/server into one runtime image. Stages: deps, web build, go build, final slim image with node runtime for Next standalone and Go binary. " +
      "No separate web/worker images per §10.",
    specs: [
      { ...SPEC.deployment, note: "§10 Dockerfile multi-stage Next + Go" },
      { ...SPEC.architecture, note: "Single container process supervision" },
      { ...SPEC.ci, note: "Image built by build-and-push-image.yml" },
    ],
    stack: stackRefs({
      layout: "Dockerfile at repo root",
      backend: "go build -o /app/server ./cmd/server",
      frontend: "next build output standalone",
      data: "N/A in build stage",
      quality: "docker build succeeds in CI",
      cicd: "build-and-push-image.yml",
      deploy: "ghcr.io/{owner}/release-ops",
    }),
    implementation: [
      "Stage 1: node — npm ci && npm run build in apps/web",
      "Stage 2: golang — CGO_ENABLED=1 for sqlite",
      "Stage 3: runtime — copy standalone, server binary, migrations",
      "EXPOSE 3000",
      "USER non-root if feasible",
      "COPY docker/entrypoint.sh",
    ],
    acceptance: [
      "Next standalone + Go binary in one image",
      "docker build -t release-ops . succeeds",
      "Image size reasonable (no dev deps in final)",
      "Migrations directory copied to image",
      "Go binary statically linked or with sqlite deps",
      "Next standalone includes node server",
      "ENTRYPOINT entrypoint.sh",
      "No .env files in image layers",
    ],
    files: ["Dockerfile"],
    relatedTasks: ["P26-02", "P27-03"],
  });

  leaf("P26-02", "P26", "Add docker entrypoint script", "ci", {
    context:
      "entrypoint.sh orchestrates startup: golang-migrate up against /data/app.db, start Go API on 127.0.0.1:8080 in background, exec Next on :3000 foreground. " +
      "Fail fast if migrate fails. Trap signals for graceful shutdown.",
    specs: [
      { ...SPEC.deployment, note: "§10 entrypoint migrate → Go → Next" },
      { ...SPEC.schema, note: "Migrations from migrations/" },
      { ...SPEC.env, note: "DATABASE_PATH /data/app.db" },
    ],
    stack: stackRefs({
      layout: "docker/entrypoint.sh",
      backend: "migrate up then ./server",
      frontend: "node server.js standalone",
      data: "SQLite file on volume",
      quality: "Script shellcheck clean",
      cicd: "Tested via compose smoke",
      deploy: "ENTRYPOINT in Dockerfile",
    }),
    schema: [{ ...SCHEMA.sql, note: "Applied via golang-migrate" }],
    implementation: [
      "set -euo pipefail",
      "migrate -path /app/migrations -database sqlite:///data/app.db up",
      "Start Go server background with GO_API_URL",
      "Wait for Go health or short sleep",
      "exec node apps/web/server.js or standalone path",
      "Forward SIGTERM to child processes",
    ],
    acceptance: [
      "migrate up runs before Go starts",
      "Go starts on 127.0.0.1:8080",
      "Next starts on PORT 3000",
      "Container exits non-zero if migrate fails",
      "SIGTERM stops both processes",
      "Uses /data volume for app.db",
      "GO_API_URL set for Next proxy",
      "Script executable in image",
    ],
    files: ["docker/entrypoint.sh"],
    relatedTasks: ["P26-03"],
  });

  leaf("P26-03", "P26", "Add docker-compose.yml single service", "ci", {
    context:
      "Reference docker-compose.yml with one release-ops service, port 3000:3000, volume release-ops-data:/data, required env vars from §9. " +
      "Matches specs.html §10 reference compose block.",
    specs: [
      { ...SPEC.deployment, note: "§10 docker-compose.yml one service" },
      { ...SPEC.env, note: "SESSION_SECRET, APP_PUBLIC_URL, APP_ENCRYPTION_KEY" },
    ],
    stack: stackRefs({
      layout: "docker-compose.yml repo root",
      backend: "N/A",
      frontend: "N/A",
      data: "named volume release-ops-data",
      quality: "compose config validates",
      cicd: "Optional CI compose smoke",
      deploy: "Self-host single URL",
    }),
    implementation: [
      "service release-ops with build or image",
      "ports 3000:3000",
      "volumes release-ops-data:/data",
      "environment from .env.example",
      "restart unless-stopped",
      "healthcheck curl /api/go/healthz",
    ],
    acceptance: [
      "Exactly one service in compose file",
      "Port 3000 published",
      "Volume mounted at /data",
      "Required env vars documented inline or in example",
      "docker compose config valid",
      "docker compose up starts app",
      "Matches §10 reference YAML structure",
      "No second web/worker service",
    ],
    files: ["docker-compose.yml"],
    relatedTasks: ["P26-04", "P28-05"],
  });

  leaf("P26-04", "P26", "Document local docker compose smoke test", "docs", {
    depends_on: ["P26-03"],
    context:
      "Add docker compose smoke test section to docs/getting-started.md: build, up, login, verify health. " +
      "Steps for local validation before CI image push. Referenced by P28-05 getting started doc.",
    specs: [
      { ...SPEC.deployment, note: "§10 Coolify / self-host smoke" },
      { ...SPEC.mvp, note: "AC #14 docker compose up one service" },
      { ...SPEC.auth, note: "Login after compose up" },
    ],
    stack: stackRefs({
      layout: "docs/getting-started.md",
      backend: "N/A",
      frontend: "N/A",
      data: "N/A",
      quality: "Doc steps reproducible",
      cicd: "N/A",
      deploy: "Operator runbook",
    }),
    implementation: [
      "Section: Run with Docker Compose",
      "Steps: cp .env.example, docker compose up --build",
      "Health check URL",
      "Default admin login note",
      "Volume persistence mention",
      "Troubleshooting migrate failures",
    ],
    acceptance: [
      "getting-started.md includes compose smoke steps",
      "Documents required env vars",
      "curl health check example",
      "Login URL http://localhost:3000/login",
      "Single service emphasized",
      "Volume path /data explained",
      "Build and up commands copy-pasteable",
      "Links to specs.html#deployment",
    ],
    files: ["docs/getting-started.md"],
    relatedTasks: ["P28-05"],
    outOfScope: ["Production TLS termination", "Kubernetes manifests"],
  });
}

/** @param {Registry} reg */
function registerP27({ epic, leaf }) {
  epic("P27", "CI/CD workflows", "ci", {
    depends_on: ["P00-05", "P26-01"],
    context:
      "GitHub Actions workflows per specs.html §11 CI/CD and §11.2 layout: entrypoints pr.yml, dev.yml, main.yml, release.yml call reusable ci.yml, prepare-release.yml, build-and-push-image.yml. " +
      "Parallel Go and web jobs in ci.yml; dev pushes nightly image; main prepares draft release on VERSION bump; release publishes latest image. " +
      "No monolithic per-branch workflows.",
    specs: [
      { ...SPEC.ci, note: "§11 full CI/CD model" },
      { ...SPEC.deployment, note: "§11.6 GHCR tags nightly, latest, v*" },
      { ...SPEC.mvp, note: "AC #15 workflows per §11" },
      { ...SPEC.env, note: "CI secrets inherit; no runtime secrets in image" },
    ],
    stack: stackRefs({
      layout: ".github/workflows/*.yml",
      backend: "go lint/test/build jobs",
      frontend: "npm lint/test/typecheck/build jobs",
      data: "N/A",
      quality: "All jobs green required",
      cicd: "Self-hosting CI in GitHub Actions",
      deploy: "GHCR push on dev and release",
    }),
    implementation: [
      "ci.yml workflow_call with parallel jobs",
      "pr.yml on pull_request → ci.yml",
      "dev.yml push dev → ci → build nightly+sha",
      "main.yml push main → ci → prepare-release",
      "release.yml published → build latest+version",
      "prepare-release uses create-draft-release script",
      "Reusable workflows no on: push",
    ],
    acceptance: [
      "All §11.2 workflow files present",
      "Reusable workflows only workflow_call",
      "PR blocked on CI failure",
      "dev push produces nightly image after CI",
      "main push runs prepare-release on VERSION bump",
      "Published release pushes latest + version tag",
      "Parallel go and web jobs in ci.yml",
      "No --no-verify in CI scripts",
    ],
  });

  leaf("P27-01", "P27", "Add reusable ci.yml workflow", "ci", {
    context:
      "Reusable ci.yml with workflow_call only. Parallel jobs: go-lint, go-test, go-build, web-lint, web-test, web-typecheck, web-build per §11.4 table. " +
      "All must pass; secrets: inherit from caller.",
    specs: [
      { ...SPEC.ci, note: "§11.4 reusable ci.yml parallel jobs" },
      { ...SPEC.mvp, note: "AC #15 Go + web tests green in CI" },
    ],
    stack: stackRefs({
      layout: ".github/workflows/ci.yml",
      backend: "golangci-lint run, go test, go build",
      frontend: "npm run lint, test, typecheck, build",
      data: "N/A",
      quality: "All gates enforced",
      cicd: "workflow_call entry",
      deploy: "N/A",
    }),
    implementation: [
      "on: workflow_call only",
      "Job go-lint: golangci-lint run",
      "Job go-test: go test ./...",
      "Job go-build: go build ./cmd/server",
      "Job web-lint: npm run lint",
      "Job web-test: npm test",
      "Job web-typecheck: npm run typecheck",
      "Job web-build: npm run build in apps/web",
    ],
    acceptance: [
      "Parallel go lint/test/build and web lint/test/typecheck/build",
      "workflow_call only — no on: push",
      "go test ./... runs",
      "npm test runs Vitest",
      "npm run typecheck passes",
      "Production next build in web-build job",
      "Failed job fails workflow",
      "Uses actions/setup-go and setup-node",
    ],
    files: [".github/workflows/ci.yml"],
    relatedTasks: ["P27-02", "P27-03", "P27-04"],
  });

  leaf("P27-02", "P27", "Add pr.yml entrypoint", "ci", {
    depends_on: ["P27-01"],
    context:
      "PR entrypoint on pull_request calls ci.yml with secrets: inherit. No image build or release. Concurrency cancel-in-progress per PR.",
    specs: [
      { ...SPEC.ci, note: "§11.3 pr.yml entrypoint" },
      { ...SPEC.mvp, note: "AC #15 PR CI green" },
    ],
    stack: stackRefs({
      layout: ".github/workflows/pr.yml",
      backend: "N/A",
      frontend: "N/A",
      data: "N/A",
      quality: "CI gate on PR",
      cicd: "on: pull_request",
      deploy: "No images on PR",
    }),
    implementation: [
      "on: pull_request",
      "jobs.ci uses: ./.github/workflows/ci.yml",
      "secrets: inherit",
      "concurrency group per PR",
      "cancel-in-progress: true",
    ],
    acceptance: [
      "pr.yml triggers on pull_request",
      "Calls reusable ci.yml",
      "secrets: inherit",
      "No image push job",
      "No release job",
      "Concurrency cancels stale runs",
      "CI failure blocks merge (branch protection)",
      "Works for PRs to dev and main",
    ],
    files: [".github/workflows/pr.yml"],
    relatedTasks: ["P27-01"],
  });

  leaf("P27-03", "P27", "Add dev.yml with nightly image push", "ci", {
    depends_on: ["P27-01"],
    context:
      "dev.yml on push to dev: ci → meta job for short_sha → build-and-push-image with tags nightly and 7-char SHA per §11.3 and §11.6.",
    specs: [
      { ...SPEC.ci, note: "§11.3 dev.yml, §11.6 nightly tags" },
      { ...SPEC.deployment, note: "GHCR ghcr.io/{owner}/release-ops" },
    ],
    stack: stackRefs({
      layout: ".github/workflows/dev.yml, build-and-push-image.yml",
      backend: "N/A",
      frontend: "N/A",
      data: "N/A",
      quality: "Image push only after CI success",
      cicd: "push branches: [dev]",
      deploy: "nightly + short_sha tags",
    }),
    implementation: [
      "on: push branches dev, workflow_dispatch",
      "Job ci uses ci.yml",
      "Job meta outputs short_sha from GITHUB_SHA",
      "Job push-image needs ci, if success",
      "uses build-and-push-image.yml with tags",
      "permissions packages: write",
    ],
    acceptance: [
      "Push dev runs CI then image push",
      "Tags include nightly and 7-char SHA",
      "push-image skipped on CI failure",
      "uses build-and-push-image.yml reusable",
      "docker/login-action ghcr.io",
      "permissions packages: write on push job",
      "workflow_dispatch supported",
      "No release on dev push",
    ],
    files: [".github/workflows/dev.yml", ".github/workflows/build-and-push-image.yml"],
    relatedTasks: ["P27-05", "P26-01"],
  });

  leaf("P27-04", "P27", "Add main.yml and prepare-release.yml", "ci", {
    depends_on: ["P27-01"],
    context:
      "main.yml on push main: ci → prepare-release.yml. No image on main push per §11.3. prepare-release creates draft release and git tag only when VERSION semver-increases.",
    specs: [
      { ...SPEC.ci, note: "§11.3 main.yml, §11.5 VERSION and prepare-release" },
      { ...SPEC.deployment, note: "Production images only on release published" },
    ],
    stack: stackRefs({
      layout: ".github/workflows/main.yml, prepare-release.yml",
      backend: "N/A",
      frontend: "N/A",
      data: "N/A",
      quality: "Skip release when VERSION unchanged",
      cicd: "contents: write for tag",
      deploy: "Draft release not production image",
    }),
    implementation: [
      "main.yml on push main → ci → prepare-release",
      "prepare-release workflow_call",
      "permissions contents: write",
      "Invoke scripts/ci/create-draft-release.mjs",
      "No image build in main.yml",
      "concurrency per main branch",
    ],
    acceptance: [
      "main.yml calls ci then prepare-release",
      "No image build on main push",
      "prepare-release is workflow_call only",
      "contents: write permission set",
      "VERSION bump creates draft release + tag",
      "No bump skips release",
      "Tag format v{X.Y.Z}",
      "Matches §11.3 main.yml example",
    ],
    files: [".github/workflows/main.yml", ".github/workflows/prepare-release.yml"],
    relatedTasks: ["P27-06"],
  });

  leaf("P27-05", "P27", "Add release.yml entrypoint", "ci", {
    depends_on: ["P27-03"],
    context:
      "release.yml on release published calls build-and-push-image with ref target_commitish and tags latest + release tag_name per §11.3 release.yml example.",
    specs: [
      { ...SPEC.ci, note: "§11.3 release.yml entrypoint" },
      { ...SPEC.deployment, note: "§11.6 latest + v{X.Y.Z} on publish" },
      { ...SPEC.mvp, note: "AC #15 published release image" },
    ],
    stack: stackRefs({
      layout: ".github/workflows/release.yml",
      backend: "N/A",
      frontend: "N/A",
      data: "N/A",
      quality: "Single image per release",
      cicd: "on: release published",
      deploy: "latest + version tags",
    }),
    implementation: [
      "on: release types published",
      "Job push-image uses build-and-push-image.yml",
      "with ref: github.event.release.target_commitish",
      "tags: latest and release.tag_name",
      "No cancel-in-progress on release",
      "packages: write permission",
    ],
    acceptance: [
      "release.yml triggers on release published",
      "Pushes one image with latest tag",
      "Pushes version tag matching release tag_name",
      "Uses target_commitish ref",
      "uses build-and-push-image.yml",
      "No CI re-run required if image from prior dev",
      "permissions packages: write",
      "Matches §11.3 release.yml snippet",
    ],
    files: [".github/workflows/release.yml"],
    relatedTasks: ["P27-03", "P28"],
  });

  leaf("P27-06", "P27", "Add create-draft-release script", "ci", {
    depends_on: ["P00-09"],
    context:
      "scripts/ci/create-draft-release.mjs implements §11.5: fetch-depth 0, compare VERSION at HEAD vs last v* tag, skip if not semver-greater, else tag v{VERSION} and create draft release with git log notes.",
    specs: [
      { ...SPEC.ci, note: "§11.5 create-draft-release script steps" },
      { ...SPEC.deployment, note: "Tag v{X.Y.Z} matches VERSION file" },
    ],
    stack: stackRefs({
      layout: "scripts/ci/create-draft-release.mjs",
      backend: "N/A",
      frontend: "N/A",
      data: "N/A",
      quality: "Unit test version compare logic",
      cicd: "Called from prepare-release.yml",
      deploy: "GitHub Releases API via gh",
    }),
    implementation: [
      "Read VERSION file at repo root",
      "git tag --list v* --sort=-v:refname for last tag",
      "Semver compare HEAD VERSION vs tag VERSION",
      "Skip exit 0 if not greater",
      "Create and push tag v{VERSION}",
      "gh release create --draft with changelog",
      "Title: Release Ops v{VERSION}",
    ],
    acceptance: [
      "Script reads VERSION at HEAD",
      "Skips when VERSION not greater than last tag",
      "Creates git tag v{X.Y.Z} on bump",
      "Creates draft GitHub release",
      "Release notes from git log since last tag",
      "No date-based release tags",
      "Uses gh CLI or GitHub API",
      "Invoked from prepare-release workflow",
    ],
    files: ["scripts/ci/create-draft-release.mjs"],
    relatedTasks: ["P27-04", "P00-09"],
  });

  leaf("P27-07", "P27", "Add CI policy gates (db:check + i18n lint)", "ci", {
    depends_on: ["P27-01", "P01-01", "P00-11"],
    context:
      "Specs §4.0 requires npm run db:check when db/schema.sql or migrations/ change; §8.5 requires eslint-plugin-i18next in CI. " +
      "Extends reusable ci.yml: add db-migrations job running scripts/ci/check-migrations-sync.mjs; document web-lint job enforces i18next/no-literal-string from P00-11.",
    specs: [
      { ...SPEC.schemaMigrations, note: "§4.0 CI db:check drift gate" },
      { ...SPEC.i18n, note: "§8.5 i18n ESLint enforced in CI web-lint" },
      { ...SPEC.ci, note: "§11.4 db job + web lint includes i18n rule" },
    ],
    stack: stackRefs({
      layout: ".github/workflows/ci.yml",
      backend: "db-migrations job",
      frontend: "web-lint with eslint-plugin-i18next",
      data: "check-migrations-sync.mjs",
      quality: "Policy gates block merge",
      cicd: "workflow_call ci.yml extension",
      deploy: "N/A",
    }),
    implementation: [
      "Add job db-migrations to ci.yml: setup-node, npm run db:check",
      "Install sqlite3 in CI runner (apt) for sqldiff",
      "Document in ci.yml comment: web-lint uses apps/web/eslint.config.mjs (i18n rule)",
      "Root package.json script db:check → node scripts/ci/check-migrations-sync.mjs",
      "Fail workflow if db/schema.sql drifts from migrations/",
      "depends_on P01-01 so migrations/ exists before gate runs",
    ],
    acceptance: [
      "ci.yml includes db-migrations job running npm run db:check",
      "CI fails when db/schema.sql and migrations/ diverge",
      "web-lint job fails on hardcoded JSX string (i18n rule)",
      "db:check script documented in doc-index.md phase gates",
      "sqlite3 available in CI job environment",
      "No Atlas or hand-written migration bypass",
      "Policy gates run on PR and dev/main push via entrypoints",
      "db:check skips gracefully only before P01 scaffold (documented)",
    ],
    files: [".github/workflows/ci.yml", "package.json"],
    tests: ["npm run db:check"],
    relatedTasks: ["P00-11", "P01-01", "P27-01"],
  });
}

/** @param {Registry} reg */
function registerP28({ epic, leaf }) {
  epic("P28", "MVP verification & polish", "docs", {
    depends_on: ["P27-05", "P26-04"],
    context:
      "Final MVP gate: provider mock tests in CI, poll integration test, MVP runbook mapping all 15 §12 acceptance criteria, and getting-started documentation. " +
      "Proves the product meets specs.html §12 before release. " +
      "Combines backend verification (P28-01–03) with operator docs (P28-04–05).",
    specs: [
      { ...SPEC.mvp, note: "§12 all 15 MVP acceptance criteria" },
      { ...SPEC.ci, note: "§11.7 MVP CI requirements" },
      { ...SPEC.providers, note: "§6 per-source and per-ticket tests" },
      { ...SPEC.domain, note: "Poll baseline/create/supersede scenarios" },
    ],
    stack: stackRefs({
      layout: "docs/mvp-checklist.md, docs/getting-started.md, go test suites",
      backend: "Provider and poll integration tests",
      frontend: "N/A",
      data: "Test fixtures and mocks",
      quality: "Full MVP checklist sign-off",
      cicd: "All tests green in ci.yml",
      deploy: "docker compose smoke in docs",
    }),
    implementation: [
      "CI mock test per source provider kind",
      "CI mock test per ticket provider kind",
      "Poll integration: baseline → create → supersede",
      "mvp-checklist.md maps AC #1–15",
      "getting-started.md operator guide",
      "Cross-link specs.html#mvp",
    ],
    acceptance: [
      "All §12 MVP AC items have verification step in runbook",
      "Source provider tests run in CI",
      "Ticket provider tests run in CI",
      "Poll integration test passes",
      "getting-started.md complete",
      "docker compose one-service documented",
      "Go + web CI green",
      "Workflow layout matches §11.2",
    ],
  });

  leaf("P28-01", "P28", "Add source provider CI mock tests", "backend", {
    depends_on: ["P11-08"],
    context:
      "One CI test per MVP source kind (GitHub, GitLab, Gitea, Forgejo, Codeberg) using HTTP mocks per §12 AC #13. " +
      "Tests live in internal/providers/source/ and run via go test ./internal/providers/source/.... " +
      "Validates release tag fetching without live API keys.",
    specs: [
      { ...SPEC.providers, note: "§6 source provider interface" },
      { ...SPEC.mvp, note: "AC #13 one test per MVP source in CI" },
      { ...SPEC.ci, note: "go test in ci.yml" },
    ],
    stack: stackRefs({
      layout: "internal/providers/source/*_test.go",
      backend: "httptest mock servers per host API",
      frontend: "N/A",
      data: "N/A",
      quality: "Table-driven per kind",
      cicd: "go test ./internal/providers/source/...",
      deploy: "N/A",
    }),
    implementation: [
      "Mock GitHub releases API",
      "Mock GitLab, Gitea, Forgejo, Codeberg equivalents",
      "Test GetLatestRelease per kind",
      "Test auth header forwarding with integration token",
      "Test error handling 404/no releases",
      "Run in CI without secrets",
    ],
    acceptance: [
      "One test per source kind in CI",
      "GitHub mock test passes",
      "GitLab mock test passes",
      "Gitea mock test passes",
      "Forgejo mock test passes",
      "Codeberg mock test passes",
      "Tests use httptest not live APIs",
      "go test ./internal/providers/source/... green",
    ],
    files: ["internal/providers/source/"],
    tests: ["go test ./internal/providers/source/..."],
    relatedTasks: ["P11-08"],
  });

  leaf("P28-02", "P28", "Add ticket provider CI mock tests", "backend", {
    depends_on: ["P12-08"],
    context:
      "CI mock tests for Phasical, Jira, and Linear ticket providers per §12 AC #13. Tests in internal/providers/ticket/ validate create, status update, and comment operations against mocked APIs.",
    specs: [
      { ...SPEC.providers, note: "§6 ticket provider interface" },
      { ...SPEC.ticketProjects, note: "create_config per provider" },
      { ...SPEC.mvp, note: "AC #13 ticket provider CI coverage" },
    ],
    stack: stackRefs({
      layout: "internal/providers/ticket/*_test.go",
      backend: "Mock Phasical, Jira, Linear APIs",
      frontend: "N/A",
      data: "N/A",
      quality: "Per-provider subtests",
      cicd: "go test ./internal/providers/ticket/...",
      deploy: "N/A",
    }),
    implementation: [
      "Mock Phasical task API",
      "Mock Jira issue REST",
      "Mock Linear GraphQL or REST",
      "Test CreateTicket with createConfig",
      "Test UpdateStatus with statusMapping",
      "Test AddComment for supersede flow",
    ],
    acceptance: [
      "Phasical provider mock test passes",
      "Jira provider mock test passes",
      "Linear provider mock test passes",
      "Create ticket payload matches provider API",
      "Status mapping applied on update",
      "No live API keys in CI",
      "go test ./internal/providers/ticket/... green",
      "Tests run in parallel where safe",
    ],
    files: ["internal/providers/ticket/"],
    tests: ["go test ./internal/providers/ticket/..."],
    relatedTasks: ["P12-08"],
  });

  leaf("P28-03", "P28", "Add poll flow integration test", "backend", {
    depends_on: ["P14-09"],
    context:
      "End-to-end poll test in internal/poll exercising baseline (first poll sets last_known_tag, no ticket), skip (no release change), create (new release creates ticket), and supersede (policy supersede closes old ticket). " +
      "Uses test DB and mocked providers. go test ./internal/poll/... -run Integration.",
    specs: [
      { ...SPEC.domain, note: "§5 poll lifecycle baseline/skip/create/supersede" },
      { ...SPEC.mvp, note: "AC #6–#10 poll behaviors" },
      { ...SPEC.ticketProjects, note: "on_open_ticket_policy supersede" },
    ],
    stack: stackRefs({
      layout: "internal/poll/integration_test.go",
      backend: "Full poll worker with mocks",
      frontend: "N/A",
      data: "SQLite test DB with migrations",
      quality: "Integration tag -run Integration",
      cicd: "go test ./internal/poll/... -run Integration",
      deploy: "N/A",
    }),
    schema: [{ ...SCHEMA.sql, note: "repos, tickets, poll_runs tables" }],
    implementation: [
      "Seed repo + ticket project + integration",
      "First poll: assert baseline, last_known_tag set, no ticket",
      "Second poll same tag: skip",
      "Third poll new tag: create ticket mock called",
      "Fourth poll with open ticket + supersede: supersede + create",
      "Assert poll_run events and counters",
    ],
    acceptance: [
      "baseline → create → supersede scenario passes",
      "First poll does not create ticket",
      "last_known_tag updated on baseline",
      "Skip when tag unchanged",
      "Create calls ticket provider on new release",
      "Supersede increments tickets_superseded",
      "Integration test uses migrated test DB",
      "go test -run Integration passes",
    ],
    files: ["internal/poll/integration_test.go"],
    tests: ["go test ./internal/poll/... -run Integration"],
    relatedTasks: ["P14-09"],
  });

  leaf("P28-04", "P28", "Write MVP acceptance runbook", "docs", {
    context:
      "docs/mvp-checklist.md maps each of the 15 specs.html §12 MVP acceptance criteria to a concrete verification step (manual or automated). " +
      "Checklist used for release sign-off and QA. References test commands and UI paths.",
    specs: [
      { ...SPEC.mvp, note: "§12 all 15 AC items mapped" },
      { ...SPEC.ui, note: "UI paths for manual verification" },
      { ...SPEC.ci, note: "Automated steps reference CI jobs" },
    ],
    stack: stackRefs({
      layout: "docs/mvp-checklist.md",
      backend: "go test commands per AC",
      frontend: "npm test / manual UI steps",
      data: "N/A",
      quality: "Checklist complete 15/15",
      cicd: "Links to workflow files",
      deploy: "compose smoke step",
    }),
    implementation: [
      "Section per §12 numbered AC",
      "AC #1: login steps at /login",
      "AC #2: create all 8 integration kinds",
      "AC #3–#4: ticket projects and repos",
      "AC #5: test connection UI",
      "AC #6–#10: poll behaviors — link integration test",
      "AC #11: status mapping per project",
      "AC #12: notifications test",
      "AC #13: provider CI test commands",
      "AC #14: dashboard, manual poll, compose",
      "AC #15: CI workflow checklist",
    ],
    acceptance: [
      "Checklist maps to specs.html §12 MVP AC item #1 (admin login)",
      "Checklist maps AC #2 (eight integration kinds, no secrets in API)",
      "Checklist maps AC #3 (multiple ticket projects with mapping and policy)",
      "Checklist maps AC #4 (any source/ticket-project combination)",
      "Checklist maps AC #5 (integration test success/failure in UI)",
      "Checklist maps AC #6–#10 (baseline, skip, create, supersede, merge, skip_if_open)",
      "Checklist maps AC #11 (status mapping per ticket project)",
      "Checklist maps AC #12 (Shoutrrr targets and events)",
      "Checklist maps AC #13 (CI tests per source and ticket provider)",
      "Checklist maps AC #14 (dashboard, manual poll, settings, docker compose one service)",
      "Checklist maps AC #15 (Go + web tests green, §11 workflow layout)",
    ],
    files: ["docs/mvp-checklist.md"],
    relatedTasks: ["P28-01", "P28-02", "P28-03", "P28-05"],
  });

  leaf("P28-05", "P28", "Add getting started documentation", "docs", {
    depends_on: ["P26-04"],
    context:
      "Complete docs/getting-started.md for operators: prerequisites, env vars, docker compose, first login, initial setup flow (integration → ticket project → repo), and links to specs. " +
      "Builds on P26-04 compose smoke section. Primary onboarding doc for self-hosters.",
    specs: [
      { ...SPEC.deployment, note: "§10 self-host quick start" },
      { ...SPEC.env, note: "§9 required variables" },
      { ...SPEC.mvp, note: "AC #14 docker compose up" },
      { ...SPEC.auth, note: "Default admin bootstrap" },
    ],
    stack: stackRefs({
      layout: "docs/getting-started.md",
      backend: "N/A",
      frontend: "N/A",
      data: "Volume /data persistence",
      quality: "Doc review against compose",
      cicd: "N/A",
      deploy: "Operator-facing",
    }),
    implementation: [
      "Prerequisites: Docker, compose v2",
      "Clone and configure .env from example",
      "docker compose up --build",
      "First login and password change if applicable",
      "Setup wizard order: integration, ticket project, repo",
      "Link to mvp-checklist.md",
      "Troubleshooting section",
    ],
    acceptance: [
      "getting-started.md exists and is complete",
      "Documents all §9 required env vars",
      "docker compose up single service documented",
      "Login URL and first-time setup steps",
      "Links to specs.html and mvp-checklist",
      "GHCR image pull alternative to local build",
      "Volume persistence explained",
      "Health check verification step",
    ],
    files: ["docs/getting-started.md"],
    relatedTasks: ["P26-04"],
    outOfScope: ["Kubernetes helm chart", "Multi-tenant setup"],
  });
}

/**
 * Register roadmap epics P15–P28 and all leaf tasks.
 * @param {Pick<Registry, "epic" | "leaf">} reg
 */
export function registerP15P28({ epic, leaf }) {
  registerP15({ epic, leaf });
  registerP16({ epic, leaf });
  registerP17({ epic, leaf });
  registerP18({ epic, leaf });
  registerP19({ epic, leaf });
  registerP20({ epic, leaf });
  registerP21({ epic, leaf });
  registerP22({ epic, leaf });
  registerP23({ epic, leaf });
  registerP24({ epic, leaf });
  registerP25({ epic, leaf });
  registerP26({ epic, leaf });
  registerP27({ epic, leaf });
  registerP28({ epic, leaf });
}
