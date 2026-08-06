#!/usr/bin/env node
/**
 * Phasical intake readiness audit for docs/roadmap.json.
 * Run: node scripts/audit-roadmap-intake.mjs
 */
import { readFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const roadmap = JSON.parse(readFileSync(join(root, "docs/roadmap.json"), "utf8"));
const tasks = roadmap.tasks;
const epics = tasks.filter((t) => t.type === "epic");
const leaves = tasks.filter((t) => t.type === "leaf");
const ids = new Set(tasks.map((t) => t.id));

const DOMAIN_LABELS = ["backend", "web", "db", "config", "ci", "docs"];
const ROADMAP_ID_RE = /\*\*Roadmap ID:\*\* (E\d{2}(?:-\d{2})?)/;

const blockers = [];
const warnings = [];

if (roadmap.format !== "phasical-intake-v2") {
  blockers.push(`format must be phasical-intake-v2 (got ${roadmap.format})`);
}

if (!roadmap.meta?.phasical_mapping) {
  blockers.push("meta.phasical_mapping missing");
}

for (const l of leaves) {
  if (!l.parent || !ids.has(l.parent)) {
    blockers.push(`${l.id}: missing or invalid parent (${l.parent})`);
  }
}

for (const t of tasks) {
  for (const d of t.depends_on ?? []) {
    if (!ids.has(d)) blockers.push(`${t.id}: invalid depends_on ${d}`);
  }
}

const missingRoadmapId = tasks.filter((t) => !ROADMAP_ID_RE.test(t.description ?? ""));
if (missingRoadmapId.length) {
  blockers.push(`${missingRoadmapId.length} tasks missing **Roadmap ID:** marker (idempotency)`);
}

const wrongRoadmapId = tasks.filter((t) => {
  const m = (t.description ?? "").match(ROADMAP_ID_RE);
  return m && m[1] !== t.id;
});
if (wrongRoadmapId.length) {
  blockers.push(`${wrongRoadmapId.length} tasks have Roadmap ID mismatch vs task.id`);
}

const shortDesc = tasks.filter((t) => !t.description || t.description.length < 200);
if (shortDesc.length) blockers.push(`${shortDesc.length} tasks with description < 200 chars`);

const fewAc = leaves.filter((t) => (t.acceptance_criteria ?? []).length < 5);
if (fewAc.length) warnings.push(`${fewAc.length} leaves with < 5 acceptance_criteria`);

const longTitles = tasks.filter((t) => t.title.length > 80);
if (longTitles.length) warnings.push(`${longTitles.length} titles > 80 chars`);

const domains = new Set(tasks.map((t) => t.domain));
for (const d of domains) {
  if (!DOMAIN_LABELS.includes(d)) blockers.push(`unknown domain: ${d}`);
}

// depends_on uses roadmap IDs — intake must patch to #N after create
const depCount = tasks.reduce((n, t) => n + (t.depends_on?.length ?? 0), 0);
warnings.push(
  `${depCount} depends_on edges use roadmap IDs — import script must patch to GitHub #N after sync`
);

// spec audit gate
const specAudit = spawnSync("node", [join(root, "scripts/audit-roadmap-spec.mjs")], {
  encoding: "utf8",
});
if (specAudit.status !== 0) {
  blockers.push("audit-roadmap-spec.mjs failed — fix spec gaps first");
}

console.log("=== Phasical intake readiness ===\n");
console.log(`Format: ${roadmap.format}`);
console.log(`Tasks: ${epics.length} epics + ${leaves.length} leaves = ${tasks.length}`);
console.log(`Domains: ${[...domains].sort().join(", ")}`);
console.log(`Roadmap ID markers: ${tasks.length - missingRoadmapId.length}/${tasks.length}`);
console.log(`\nBlockers: ${blockers.length}`);
for (const b of blockers) console.log(`  ✗ ${b}`);
console.log(`\nWarnings: ${warnings.length}`);
for (const w of warnings) console.log(`  ⚠ ${w}`);

if (blockers.length === 0) {
  console.log("\n✓ Ready for phasical-intake (after user approval per skill)");
  console.log("  Dry-run: node scripts/import-roadmap-phasical.mjs --dry-run");
  console.log("  Apply:   node scripts/import-roadmap-phasical.mjs --apply  (requires PHASICAL_API_KEY)");
} else {
  console.log("\n✗ Not ready — fix blockers before import");
}

process.exit(blockers.length ? 1 : 0);
