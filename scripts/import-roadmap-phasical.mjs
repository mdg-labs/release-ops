#!/usr/bin/env node
/**
 * Idempotent Phasical import from docs/roadmap.json.
 * API: Kaneo-compatible (https://kaneo.app/docs/api-reference/introduction)
 *
 * Dry-run (default): node scripts/import-roadmap-phasical.mjs
 * Apply:             node scripts/import-roadmap-phasical.mjs --apply
 *
 * Requires (.env):
 *   PHASICAL_API_KEY
 *   PHASICAL_API_BASE   — e.g. https://phasical.mdg-labs.dev (/api appended if missing)
 *   PHASICAL_USER_ID    — assignee on create (API keys do not return session user)
 *
 * Optional delays (self-hosted — light pacing, not rate-limit driven):
 *   PHASICAL_DELAY_MS            — between write calls (default 100)
 *   PHASICAL_GITHUB_POLL_MS      — poll interval for GitHub issue link (default 300)
 *   PHASICAL_GITHUB_POLL_MAX_MS  — stop polling after (default 2000)
 *
 * Idempotency: skip tasks whose description contains `**Roadmap ID:** {id}`.
 */
import { readFileSync, existsSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const __dirname = dirname(fileURLToPath(import.meta.url));
const root = join(__dirname, "..");

const PROJECT_ID = "tv679ggt5ier9r5dx70w8ks6";
const WORKSPACE_ID = "X3VbytvC7pKgazK2dAsOQIFtdGYRzdGH";
const GITHUB_REPO = "mdg-labs/release-ops";
const READY_STATUS = "ready";
const CREATE_STATUS = "backlog";

const apply = process.argv.includes("--apply");
const ROADMAP_ID_RE = /\*\*Roadmap ID:\*\* (E\d{2}(?:-\d{2})?)/;

function loadEnv() {
  const envPath = join(root, ".env");
  if (!existsSync(envPath)) return;
  for (const line of readFileSync(envPath, "utf8").split("\n")) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const m = /^([A-Z_]+)=(.*)$/.exec(trimmed);
    if (m && !process.env[m[1]]) process.env[m[1]] = m[2];
  }
}

function envMs(name, fallback) {
  const v = Number(process.env[name]);
  return Number.isFinite(v) && v >= 0 ? v : fallback;
}

function normalizeApiBase(url) {
  let base = url.replace(/\/$/, "");
  if (!base.endsWith("/api")) base += "/api";
  return base;
}

loadEnv();

const DELAY_MS = envMs("PHASICAL_DELAY_MS", 100);
const GITHUB_POLL_MS = envMs("PHASICAL_GITHUB_POLL_MS", 300);
const GITHUB_POLL_MAX_MS = envMs("PHASICAL_GITHUB_POLL_MAX_MS", 2000);

const audit = spawnSync("node", [join(root, "scripts/audit-roadmap-intake.mjs")], {
  encoding: "utf8",
});
if (audit.status !== 0) {
  console.error(audit.stdout);
  console.error("Intake audit failed — aborting import");
  process.exit(1);
}

const roadmap = JSON.parse(readFileSync(join(root, "docs/roadmap.json"), "utf8"));
const tasks = roadmap.tasks;
const epics = tasks.filter((t) => t.type === "epic");
const leaves = tasks.filter((t) => t.type === "leaf");

const apiKey = process.env.PHASICAL_API_KEY;
const apiBase = normalizeApiBase(process.env.PHASICAL_API_BASE ?? "https://app.phasical.io");
const operatorUserId = process.env.PHASICAL_USER_ID;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function delayAfterWrite() {
  if (DELAY_MS > 0) await sleep(DELAY_MS);
}

async function api(method, path, body) {
  const res = await fetch(`${apiBase}${path}`, {
    method,
    headers: {
      Authorization: `Bearer ${apiKey}`,
      "Content-Type": "application/json",
      Accept: "application/json",
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let data;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = { raw: text };
  }
  if (!res.ok) {
    throw new Error(`${method} ${path} → ${res.status}: ${text.slice(0, 300)}`);
  }
  return data;
}

/** GET /task/tasks/{projectId} — Kaneo listTasks */
async function listAllTasks() {
  const data = await api("GET", `/task/tasks/${PROJECT_ID}`);
  const flat = [];
  for (const col of data.data?.columns ?? []) {
    for (const t of col.tasks ?? []) flat.push(t);
  }
  for (const t of data.data?.archivedTasks ?? []) flat.push(t);
  return flat;
}

/** GET /search — find task by Roadmap ID marker */
async function searchByRoadmapId(roadmapId) {
  const q = encodeURIComponent(`Roadmap ID: ${roadmapId}`);
  const data = await api(
    "GET",
    `/search?q=${q}&type=tasks&workspaceId=${WORKSPACE_ID}&projectId=${PROJECT_ID}&limit=5`
  );
  const results = data.results ?? [];
  return (
    results.find((r) => r.description?.includes(`**Roadmap ID:** ${roadmapId}`)) ??
    results[0] ??
    null
  );
}

function githubIssueFromTask(task) {
  const links = task.externalLinks ?? [];
  return links.find((l) => l.resourceType === "issue") ?? null;
}

async function waitForGithubIssue(taskId) {
  const deadline = Date.now() + GITHUB_POLL_MAX_MS;
  while (Date.now() < deadline) {
    const task = await api("GET", `/task/${taskId}`);
    const gh = githubIssueFromTask(task);
    if (gh) return Number(gh.externalId);
    await sleep(GITHUB_POLL_MS);
  }
  return null;
}

/** GET /label/workspace/{workspaceId} */
async function loadLabelMap() {
  const labels = await api("GET", `/label/workspace/${WORKSPACE_ID}`);
  const byName = new Map();
  for (const l of labels ?? []) {
    if (!l.taskId && l.name) byName.set(l.name, l.id);
  }
  return byName;
}

/** POST /label — create missing domain label */
async function ensureLabel(name, labelByName) {
  if (labelByName.has(name)) return labelByName.get(name);
  const created = await api("POST", "/label", {
    name,
    color: "#64748b",
    workspaceId: WORKSPACE_ID,
  });
  labelByName.set(name, created.id);
  await delayAfterWrite();
  return created.id;
}

function buildDescription(task, parentGithubUrl) {
  let desc = task.description ?? "";
  if (task.type === "leaf" && parentGithubUrl) {
    desc = `**Parent:** ${parentGithubUrl}\n\n${desc}`;
  }
  return desc;
}

async function main() {
  console.log(`=== Phasical roadmap import (${apply ? "APPLY" : "DRY-RUN"}) ===\n`);
  console.log(`API: ${apiBase} (Kaneo-compatible)`);
  console.log(
    `Delays: write=${DELAY_MS}ms, github-poll=${GITHUB_POLL_MS}ms (max ${GITHUB_POLL_MAX_MS}ms)`
  );
  console.log(`Project: Release Ops (${PROJECT_ID})`);
  console.log(`Tasks: ${epics.length} epics + ${leaves.length} leaves\n`);

  if (apply && !apiKey) {
    console.error("PHASICAL_API_KEY required for --apply");
    process.exit(1);
  }
  if (apply && !operatorUserId) {
    console.error("PHASICAL_USER_ID required for --apply (API keys have no session user)");
    console.error("Set in .env — use your Phasical user CUID from whoami / account settings.");
    process.exit(1);
  }

  const existingByRoadmapId = new Map();
  let labelByName = new Map();

  if (apply) {
    const status = await api("GET", "/instance/status");
    console.log(`Instance: hasUsers=${status.hasUsers} hasAdmin=${status.hasAdmin}`);

    const existing = await listAllTasks();
    for (const t of existing) {
      const m = (t.description ?? "").match(ROADMAP_ID_RE);
      if (m) existingByRoadmapId.set(m[1], t);
    }
    console.log(`Existing tasks with Roadmap ID: ${existingByRoadmapId.size}`);
    labelByName = await loadLabelMap();
    console.log(`Workspace labels loaded: ${labelByName.size}\n`);
  }

  const phasicalByRoadmapId = new Map();
  const githubByRoadmapId = new Map();
  const plan = { create: [], skip: [], relations: [] };

  const createOrder = [...epics, ...leaves];
  let created = 0;

  for (const task of createOrder) {
    let existing = existingByRoadmapId.get(task.id);
    if (!existing && apply) {
      const hit = await searchByRoadmapId(task.id);
      if (hit) existing = hit;
    }

    if (existing) {
      plan.skip.push(task.id);
      phasicalByRoadmapId.set(task.id, existing.id);
      const gh = githubIssueFromTask(existing);
      if (gh) githubByRoadmapId.set(task.id, Number(gh.externalId));
      continue;
    }

    plan.create.push({ id: task.id, type: task.type, title: task.title, domain: task.domain });
    if (!apply) continue;

    const parentGh =
      task.type === "leaf" && githubByRoadmapId.get(task.parent)
        ? `https://github.com/${GITHUB_REPO}/issues/${githubByRoadmapId.get(task.parent)}`
        : null;

    const result = await api("POST", `/task/${PROJECT_ID}`, {
      title: task.title,
      description: buildDescription(task, parentGh),
      priority: task.priority ?? "medium",
      status: CREATE_STATUS,
      userId: operatorUserId,
    });

    phasicalByRoadmapId.set(task.id, result.id);
    created += 1;

    if (created % 10 === 0 || created === 1) {
      console.log(`  [create ${created}] ${task.id} → ${result.id}`);
    }

    const ghNum = await waitForGithubIssue(result.id);
    if (ghNum) githubByRoadmapId.set(task.id, ghNum);

    const labelId = await ensureLabel(task.domain, labelByName);
    await api("PUT", `/label/${labelId}/task`, { taskId: result.id });

    await delayAfterWrite();
  }

  if (apply && created > 0) console.log(`  [create] ${created} new tasks\n`);

  for (const leaf of leaves) {
    if (leaf.parent) plan.relations.push({ type: "subtask", parent: leaf.parent, child: leaf.id });
    for (const blocker of leaf.depends_on ?? []) {
      plan.relations.push({ type: "blocks", blocker, blocked: leaf.id });
    }
  }

  if (apply) {
    let rel = 0;
    for (const leaf of leaves) {
      const childId = phasicalByRoadmapId.get(leaf.id);
      const parentId = phasicalByRoadmapId.get(leaf.parent);
      if (childId && parentId) {
        await api("POST", "/task-relation", {
          sourceTaskId: parentId,
          targetTaskId: childId,
          relationType: "subtask",
        });
        rel += 1;
        await delayAfterWrite();
      }
      for (const blocker of leaf.depends_on ?? []) {
        const src = phasicalByRoadmapId.get(blocker);
        const tgt = phasicalByRoadmapId.get(leaf.id);
        if (src && tgt) {
          await api("POST", "/task-relation", {
            sourceTaskId: src,
            targetTaskId: tgt,
            relationType: "blocks",
          });
          rel += 1;
          await delayAfterWrite();
        }
      }
    }
    if (rel > 0) console.log(`  [relations] ${rel} wired`);
  }

  console.log(`\nWould create: ${plan.create.length}`);
  console.log(`Would skip (existing): ${plan.skip.length}`);
  console.log(`Relations: ${plan.relations.length}`);

  if (!apply) {
    const estSec = Math.round(
      (plan.create.length * (DELAY_MS + GITHUB_POLL_MAX_MS / 2) +
        plan.relations.length * DELAY_MS) /
        1000
    );
    console.log(`\nDry-run complete (~${estSec}s estimated at current delays).`);
    console.log("Use --apply after explicit approval (phasical-intake skill).");
    return;
  }

  let ready = 0;
  for (const task of tasks) {
    const taskId = phasicalByRoadmapId.get(task.id);
    if (!taskId) continue;
    await api("PUT", `/task/status/${taskId}`, { status: READY_STATUS });
    ready += 1;
    await delayAfterWrite();
  }
  console.log(`  [status] ${ready} → ${READY_STATUS}`);

  console.log(`\n✓ Import complete: ${plan.create.length} created, ${plan.skip.length} skipped`);
  console.log("Re-run is idempotent — existing Roadmap ID markers are skipped.");
}

main().catch((err) => {
  console.error(err.message);
  process.exit(1);
});
