#!/usr/bin/env node
/**
 * Audits docs/roadmap.json against docs/specs.html contract.
 * Run: node scripts/audit-roadmap-spec.mjs
 */
import { readFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const roadmap = JSON.parse(readFileSync(join(__dirname, "../docs/roadmap.json"), "utf8"));
const tasks = roadmap.tasks;
const leaves = tasks.filter((t) => t.type === "leaf");
const text = (t) => `${t.title}\n${t.description}`.toLowerCase();

/** @type {Array<{id:string, area:string, requirement:string, match:(t:object)=>boolean}>} */
const REQUIREMENTS = [
  // ── REST API (§7) ──
  { id: "API-AUTH-LOGIN", area: "api", requirement: "POST /api/v1/auth/login", match: (t) => text(t).includes("auth/login") },
  { id: "API-AUTH-LOGOUT", area: "api", requirement: "POST /api/v1/auth/logout", match: (t) => text(t).includes("auth/logout") },
  { id: "API-AUTH-SESSION", area: "api", requirement: "GET /api/v1/auth/session", match: (t) => text(t).includes("auth/session") },
  { id: "API-HEALTHZ", area: "api", requirement: "GET /healthz", match: (t) => text(t).includes("/healthz") },
  { id: "API-STATUS", area: "api", requirement: "GET /api/v1/status", match: (t) => text(t).includes("/api/v1/status") },
  { id: "API-POLL-TRIGGER", area: "api", requirement: "POST /api/v1/poll/trigger", match: (t) => text(t).includes("poll/trigger") },
  { id: "API-POLL-RUNS", area: "api", requirement: "GET /api/v1/poll/runs", match: (t) => {
    const s = text(t);
    return s.includes("get /api/v1/poll/runs paginated")
      || s.includes("get /api/v1/poll/runs returns")
      || (s.includes("poll/runs") && !s.includes("poll/runs/{") && !s.includes("poll/runs/:id"));
  }},
  { id: "API-POLL-RUN-ID", area: "api", requirement: "GET /api/v1/poll/runs/{id}", match: (t) => text(t).includes("poll/runs/{id}") || text(t).includes("poll/runs/:id") },
  { id: "API-SETTINGS-GET", area: "api", requirement: "GET /api/v1/settings", match: (t) => text(t).includes("get /api/v1/settings") || (text(t).includes("/api/v1/settings") && text(t).includes("get")) },
  { id: "API-SETTINGS-PATCH", area: "api", requirement: "PATCH /api/v1/settings", match: (t) => text(t).includes("patch /api/v1/settings") || (text(t).includes("/api/v1/settings") && text(t).includes("patch")) },
  { id: "API-INTEGRATIONS-CRUD", area: "api", requirement: "Integrations CRUD + test", match: (t) => text(t).includes("/api/v1/integrations") },
  { id: "API-TICKET-PROJECTS", area: "api", requirement: "Ticket projects CRUD", match: (t) => text(t).includes("ticket-projects") },
  { id: "API-REPOS", area: "api", requirement: "Monitored repos CRUD", match: (t) => text(t).includes("/api/v1/repos") },
  { id: "API-NOTIFICATIONS", area: "api", requirement: "Notification targets CRUD + test", match: (t) => text(t).includes("notification-targets") },

  // ── UI routes (§8.2) ──
  { id: "UI-LOGIN", area: "web", requirement: "/login page", match: (t) => t.domain === "web" && (text(t).includes("/login") || text(t).includes("login page")) },
  { id: "UI-DASHBOARD", area: "web", requirement: "Dashboard /", match: (t) => t.domain === "web" && text(t).includes("dashboard") },
  { id: "UI-REPOS", area: "web", requirement: "/repos page", match: (t) => t.domain === "web" && text(t).includes("/repos") },
  { id: "UI-INTEGRATIONS", area: "web", requirement: "/integrations page", match: (t) => t.domain === "web" && text(t).includes("/integrations") },
  { id: "UI-TICKET-PROJECTS", area: "web", requirement: "/ticket-projects page", match: (t) => t.domain === "web" && text(t).includes("ticket-projects") || text(t).includes("ticket projects") },
  { id: "UI-NOTIFICATIONS", area: "web", requirement: "/notifications page", match: (t) => t.domain === "web" && text(t).includes("/notifications") },
  { id: "UI-SETTINGS", area: "web", requirement: "/settings page", match: (t) => t.domain === "web" && text(t).includes("/settings") },

  // ── React Query hooks (§8.4) ──
  { id: "HOOK-SESSION", area: "web", requirement: "useSession", match: (t) => text(t).includes("usesession") },
  { id: "HOOK-STATUS", area: "web", requirement: "useStatus", match: (t) => text(t).includes("usestatus") },
  { id: "HOOK-REPOS", area: "web", requirement: "useRepos", match: (t) => text(t).includes("userepos") },
  { id: "HOOK-INTEGRATIONS", area: "web", requirement: "useIntegrations", match: (t) => text(t).includes("useintegrations") },
  { id: "HOOK-TICKET-PROJECTS", area: "web", requirement: "useTicketProjects", match: (t) => text(t).includes("useticketprojects") },
  { id: "HOOK-NOTIFICATIONS", area: "web", requirement: "useNotificationTargets", match: (t) => text(t).includes("usenotificationtargets") },
  { id: "HOOK-SETTINGS", area: "web", requirement: "useSettings", match: (t) => text(t).includes("usesettings") },
  { id: "HOOK-POLL-RUNS", area: "web", requirement: "usePollRuns", match: (t) => text(t).includes("usepollruns") },
  { id: "HOOK-POLL-RUN", area: "web", requirement: "usePollRun", match: (t) => text(t).includes("usepollrun") },
  { id: "HOOK-POLL-TRIGGER", area: "web", requirement: "useTriggerPoll", match: (t) => text(t).includes("usetriggerpoll") },

  // ── Policy (spec §4.0, §8.5, cursor rules) ──
  { id: "I18N-NEXT-INTL", area: "web", requirement: "next-intl provider + messages/en.json", match: (t) => text(t).includes("next-intl") && text(t).includes("messages/en.json") },
  { id: "I18N-ESLINT", area: "web", requirement: "eslint-plugin-i18next no-literal-string", match: (t) => text(t).includes("eslint-plugin-i18next") || text(t).includes("i18next/no-literal-string") },
  { id: "DB-MIGRATE-DIFF", area: "db", requirement: "scripts/migrate-diff.mjs sqldiff workflow", match: (t) => text(t).includes("migrate-diff") && text(t).includes("sqldiff") },
  { id: "DB-CHECK", area: "db", requirement: "npm run db:check migration drift gate", match: (t) => text(t).includes("db:check") || text(t).includes("check-migrations-sync") },
  { id: "DB-SCHEMA-PATH", area: "db", requirement: "db/schema.sql canonical path", match: (t) => text(t).includes("db/schema.sql") },
  { id: "CI-DB-CHECK", area: "ci", requirement: "db:check in CI workflow", match: (t) => t.domain === "ci" && text(t).includes("db:check") },
  { id: "SRC-GITHUB", area: "backend", requirement: "GitHub source provider", match: (t) => text(t).includes("github") && text(t).includes("source") },
  { id: "SRC-GITLAB", area: "backend", requirement: "GitLab source provider", match: (t) => text(t).includes("gitlab") && text(t).includes("source") },
  { id: "SRC-GITEA", area: "backend", requirement: "Gitea/Forgejo/Codeberg source", match: (t) => text(t).includes("gitea") || text(t).includes("forgejo") || text(t).includes("codeberg") },
  { id: "TKT-PHASICAL", area: "backend", requirement: "Phasical ticket provider", match: (t) => text(t).includes("phasical") && text(t).includes("ticket") },
  { id: "TKT-JIRA", area: "backend", requirement: "Jira ticket provider", match: (t) => text(t).includes("jira") && text(t).includes("ticket") || text(t).includes("jira ticket provider") },
  { id: "TKT-LINEAR", area: "backend", requirement: "Linear ticket provider", match: (t) => text(t).includes("linear") && text(t).includes("ticket") || text(t).includes("linear ticket provider") },
  { id: "POLL-BASELINE", area: "backend", requirement: "baseline poll action", match: (t) => text(t).includes("baseline") },
  { id: "POLL-SKIP", area: "backend", requirement: "skip poll action", match: (t) => text(t).includes("skip") && text(t).includes("poll") },
  { id: "POLL-SUPERSEDE", area: "backend", requirement: "supersede policy", match: (t) => text(t).includes("supersede") },
  { id: "POLL-MERGE", area: "backend", requirement: "merge policy", match: (t) => text(t).includes("merge policy") || (text(t).includes("merge") && text(t).includes("open ticket")) },
  { id: "POLL-SKIP-OPEN", area: "backend", requirement: "skip_if_open policy", match: (t) => text(t).includes("skip_if_open") || text(t).includes("skip open") },
  { id: "TICKET-CONTENT", area: "backend", requirement: "Ticket title Release: format §5.4", match: (t) => text(t).includes("release:") || text(t).includes("ticket content") || text(t).includes("§5.4") },

  // ── Infra ──
  { id: "DOCKER-COMPOSE", area: "ci", requirement: "docker-compose single service", match: (t) => text(t).includes("docker-compose") },
  { id: "DOCKERFILE", area: "ci", requirement: "Multi-stage Dockerfile", match: (t) => text(t).includes("dockerfile") },
  { id: "CI-WORKFLOWS", area: "ci", requirement: "GitHub Actions workflows §11", match: (t) => text(t).includes(".github/workflows") },
  { id: "PROXY-GO", area: "web", requirement: "/api/go proxy", match: (t) => text(t).includes("/api/go") },
  { id: "LOGOUT-UI", area: "web", requirement: "Logout UI control", match: (t) => text(t).includes("logout") && t.domain === "web" },
  { id: "POLL-RUNS-UI", area: "web", requirement: "Poll run history UI consumes GET poll/runs", match: (t) => t.domain === "web" && text(t).includes("poll run") && (text(t).includes("history") || text(t).includes("runs")) },

  // ── Consolidation regression guards (spec detail) ──
  { id: "CLASSIFY-STATUS", area: "backend", requirement: "ClassifyStatus helper §5.2", match: (t) => text(t).includes("classifystatus") },
  { id: "LIVE-TICKET-STATUS", area: "backend", requirement: "Live GetTicketStatus before policy §5.2", match: (t) => text(t).includes("getticketstatus") || (text(t).includes("live") && text(t).includes("ticket status")) },
  { id: "SUPERSEDE-COMMENT", area: "backend", requirement: "Supersede comment on old ticket §5.2.1", match: (t) => text(t).includes("supersede") && (text(t).includes("comment") || text(t).includes("addticketcomment")) },
  { id: "TICKETS-SUPERSEDED", area: "backend", requirement: "tickets_superseded counter §5.6", match: (t) => text(t).includes("tickets_superseded") || text(t).includes("ticketssuperseded") },
  { id: "API-HAS-SECRET", area: "api", requirement: "hasSecret in integration list §7.3", match: (t) => text(t).includes("hassecret") },
  { id: "API-DELETE-409", area: "api", requirement: "DELETE 409 when referenced §7.3–7.4", match: (t) => text(t).includes("409") && text(t).includes("referenced") },
  { id: "API-POLL-202", area: "api", requirement: "POST poll/trigger 202 async §7.1", match: (t) => text(t).includes("poll/trigger") && text(t).includes("202") },
  { id: "API-STATUS-FULL", area: "api", requirement: "GET /status full JSON repos[] §7.1", match: (t) => text(t).includes("/api/v1/status") && (text(t).includes("ticketprojectname") || text(t).includes("repos[]")) },
  { id: "API-NOTIF-TARGET-IDS", area: "api", requirement: "notificationTargetIds on repos §7.5", match: (t) => text(t).includes("notificationtargetids") },
  { id: "API-SOURCE-INTEGRATION", area: "api", requirement: "sourceIntegrationId on repos §7.5", match: (t) => text(t).includes("sourceintegrationid") },
  { id: "SHELL-FRAME", area: "web", requirement: "COSS Frame p-frame-3 §8.1", match: (t) => text(t).includes("p-frame-3") || text(t).includes("appframe") },
  { id: "SHELL-BREADCRUMB", area: "web", requirement: "COSS Breadcrumb p-breadcrumb-3 §8.1", match: (t) => text(t).includes("p-breadcrumb-3") || text(t).includes("breadcrumb") },
  { id: "SHELL-TOASTER", area: "web", requirement: "COSS Toaster p-toast-2 §8.1", match: (t) => text(t).includes("p-toast-2") || text(t).includes("toaster") },
  { id: "SHELL-SPINNER", area: "web", requirement: "COSS Spinner p-spinner-1 §8.1", match: (t) => text(t).includes("p-spinner-1") || text(t).includes("spinner") },
  { id: "DASH-REPO-TABLE", area: "web", requirement: "Dashboard repo status table p-table-4 §8.3", match: (t) => text(t).includes("p-table-4") || text(t).includes("repo status table") },
  { id: "REPOS-NOTIF-CHECKBOX", area: "web", requirement: "Repos notification targets checkbox §8.3", match: (t) => text(t).includes("p-checkbox-group-1") || text(t).includes("notification targets checkbox") },
  { id: "MODERNC-SQLITE", area: "db", requirement: "modernc.org/sqlite driver stack.html", match: (t) => text(t).includes("modernc") },
  { id: "GITEA-COMPAT", area: "backend", requirement: "GiteaCompatibleSource shared client §6", match: (t) => text(t).includes("giteacompatiblesource") || text(t).includes("gitea_compatible") },
  { id: "CI-BUILDCACHE", area: "ci", requirement: "GHCR buildcache tag §11", match: (t) => text(t).includes("buildcache") },
  { id: "CI-CONCURRENCY", area: "ci", requirement: "cancel-in-progress concurrency §11", match: (t) => text(t).includes("cancel-in-progress") || text(t).includes("concurrency") },
  { id: "PUBLISHED-AT", area: "backend", requirement: "publishedAt in ticket content §5.4", match: (t) => text(t).includes("publishedat") },
  { id: "TAG-STRING-EQ", area: "backend", requirement: "Poll tag skip uses string equality §5.1", match: (t) => text(t).includes("string equal") || (text(t).includes("exact") && text(t).includes("tag")) || text(t).includes("tag string equals") },
  { id: "ENCRYPT-WIRE", area: "backend", requirement: "base64 nonce+ciphertext wire format §4.8", match: (t) => text(t).includes("base64(nonce") || (text(t).includes("base64") && text(t).includes("nonce")) },
  { id: "CRON-SCHEDULER", area: "backend", requirement: "robfig/cron poll scheduler", match: (t) => text(t).includes("robfig/cron") },
  { id: "BOOTSTRAP-NO-UI", area: "backend", requirement: "Admin bootstrap env+CLI only no register", match: (t) => text(t).includes("seed-admin") || text(t).includes("bootstrap_admin") },
  { id: "LINEAR-STATEID", area: "backend", requirement: "Linear stateId mapping MVP", match: (t) => text(t).includes("stateid") },
];

const gaps = [];
const coverage = [];

for (const req of REQUIREMENTS) {
  const hits = leaves.filter(req.match);
  const backendHits = hits.filter((t) => t.domain === "backend" || t.domain === "db");
  const webHits = hits.filter((t) => t.domain === "web");
  coverage.push({ ...req, hits: hits.map((t) => t.id), backend: backendHits.map((t) => t.id), web: webHits.map((t) => t.id) });
  if (hits.length === 0) gaps.push(req);
}

// Backend API → frontend consumer pairs
const API_UI_PAIRS = [
  { api: "API-POLL-RUNS", ui: "POLL-RUNS-UI", note: "GET poll/runs needs dashboard/history UI" },
  { api: "API-POLL-RUN-ID", ui: "POLL-RUNS-UI", note: "GET poll/runs/{id} needs run detail UI" },
  { api: "API-AUTH-LOGOUT", ui: "LOGOUT-UI", note: "logout endpoint needs shell logout control" },
];

const drift = [];
for (const pair of API_UI_PAIRS) {
  const apiCov = coverage.find((c) => c.id === pair.api);
  const uiCov = coverage.find((c) => c.id === pair.ui);
  if (apiCov?.hits.length && !uiCov?.hits.length) {
    drift.push({ ...pair, apiTasks: apiCov.hits });
  }
}

console.log("=== Roadmap spec audit ===\n");
console.log(`Leaves: ${leaves.length}`);
console.log(`Requirements checked: ${REQUIREMENTS.length}`);
console.log(`Gaps (no matching leaf): ${gaps.length}`);
if (gaps.length) {
  console.log("\n--- GAPS ---");
  for (const g of gaps) console.log(`  [${g.area}] ${g.id}: ${g.requirement}`);
}
console.log(`\nBackend/frontend drift: ${drift.length}`);
if (drift.length) {
  console.log("\n--- DRIFT ---");
  for (const d of drift) console.log(`  ${d.note} (API: ${d.apiTasks.join(", ")})`);
}

// MVP AC mapping
const MVP_AC = [
  { n: 1, kw: ["login", "email", "password"] },
  { n: 2, kw: ["eight", "8 kind", "integration", "hassecret"] },
  { n: 3, kw: ["ticket project", "status_mapping", "on_open_ticket"] },
  { n: 4, kw: ["monitored repo", "source", "ticket"] },
  { n: 5, kw: ["test connection", "integration test"] },
  { n: 6, kw: ["baseline"] },
  { n: 7, kw: ["skip", "identical tag"] },
  { n: 8, kw: ["create ticket", "new release"] },
  { n: 9, kw: ["supersede", "tickets_superseded", "comment"] },
  { n: 10, kw: ["merge", "skip_if_open"] },
  { n: 11, kw: ["status mapping", "per ticket project"] },
  { n: 12, kw: ["shoutrrr", "notification"] },
  { n: 13, kw: ["mock", "ci", "provider"] },
  { n: 14, kw: ["docker compose", "dashboard", "manual poll"] },
  { n: 15, kw: ["ci.yml", "workflow", "github actions"] },
];

console.log("\n--- MVP AC coverage (keyword match) ---");
for (const ac of MVP_AC) {
  const hits = leaves.filter((t) => ac.kw.some((k) => text(t).includes(k)));
  console.log(`  AC #${ac.n}: ${hits.length} leaves ${hits.length < 2 ? "⚠️  thin" : "✓"}`);
}

// ── Consistency: stale patterns, broken task refs ──
const allText = tasks.map((t) => `${t.id}\n${t.title}\n${t.description}`).join("\n").toLowerCase();
const STALE = [
  { id: "STALE-DOCS-SCHEMA", pattern: "docs/schema.sql", note: "Schema moved to db/schema.sql" },
  { id: "STALE-ATLAS", pattern: "atlas", note: "Atlas removed — use sqldiff migrate-diff" },
  { id: "STALE-HAND-MIGRATION", pattern: "copied from docs/schema", note: "Hand-copy migrations forbidden" },
  { id: "STALE-MIGRATE-CREATE", pattern: "migrate create", note: "Use migrate-diff, not migrate create" },
];
const staleHits = STALE.filter((s) => {
  if (s.id === "STALE-ATLAS") return false;
  return allText.includes(s.pattern);
});

// atlas false positive: atlassian.net in jira rows — only flag atlas.hcl, atlas migrate, atlas oss
const atlasStale = /\batlas\.hcl\b|\batlas migrate\b|\batlas oss\b|\batlas\.sum\b/.test(allText);

const ids = new Set(tasks.map((t) => t.id));
const brokenRefs = [];
for (const t of tasks) {
  for (const dep of [...(t.depends_on ?? []), ...(t.related_tasks ?? [])]) {
    if (!ids.has(dep)) brokenRefs.push({ from: t.id, ref: dep, where: "depends_on/related_tasks" });
  }
  const descRefs = (t.description ?? "").match(/P\d{2}-\d{2}/g) ?? [];
  for (const ref of [...new Set(descRefs)]) {
    if (ref !== t.id && !ids.has(ref)) brokenRefs.push({ from: t.id, ref, where: "description" });
  }
}

console.log("\n--- Consistency ---");
console.log(`  Stale pattern hits: ${staleHits.length + (atlasStale ? 1 : 0)}`);
if (staleHits.length) {
  for (const s of staleHits) console.log(`  ⚠️  ${s.id}: ${s.note} (pattern: ${s.pattern})`);
}
if (atlasStale) console.log("  ⚠️  STALE-ATLAS: Atlas references remain in roadmap text");
console.log(`  Broken task refs: ${brokenRefs.length}`);
if (brokenRefs.length) {
  for (const b of brokenRefs) console.log(`  ⚠️  ${b.from} → ${b.ref} (${b.where})`);
}

// Spec doc gaps (roadmap ahead of spec — informational)
const SPEC_DOC_GAPS = [
  {
    id: "SPEC-84-POLL-HOOKS",
    note: "§8.4 should list usePollRuns, usePollRun, useTriggerPoll",
    ok: /usepollruns.*usepollrun.*usetriggerpoll/i.test(
      readFileSync(join(__dirname, "../docs/specs.html"), "utf8")
    ),
  },
  {
    id: "SPEC-114-DB-CHECK",
    note: "§11.4 CI table should include npm run db:check job",
    ok: readFileSync(join(__dirname, "../docs/specs.html"), "utf8").includes("db:check"),
  },
];
const specGaps = SPEC_DOC_GAPS.filter((g) => !g.ok);
console.log(`  Spec doc gaps: ${specGaps.length}`);
for (const g of specGaps) console.log(`  ⚠️  ${g.id}: ${g.note}`);

const consistencyFail =
  staleHits.length > 0 || atlasStale || brokenRefs.length > 0 || specGaps.length > 0;

process.exit(gaps.length || drift.length || consistencyFail ? 1 : 0);
