#!/usr/bin/env node
/**
 * Generates docs/roadmap.json and docs/roadmap.html from detailed epic modules.
 * Run: node scripts/generate-roadmap.mjs
 */
import { writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { describe } from "./roadmap/spec-links.mjs";
import { createRegistry } from "./roadmap/registry.mjs";
import { registerE01E06 } from "./roadmap/epics/e01-e06.mjs";
import { registerE07E11 } from "./roadmap/epics/e07-e11.mjs";

const __dirname = dirname(fileURLToPath(import.meta.url));
const OUT_JSON = join(__dirname, "../docs/roadmap.json");
const OUT_HTML = join(__dirname, "../docs/roadmap.html");

const { tasks, epic, leaf } = createRegistry(describe);
registerE01E06({ epic, leaf });
registerE07E11({ epic, leaf });

const roadmap = {
  version: 2,
  format: "phasical-intake-v2",
  generated: new Date().toISOString().slice(0, 10),
  spec_ref: "docs/specs.html v2.7",
  project: "release-ops",
  integration_branch: "dev",
  tasks,
  meta: {
    epic_count: tasks.filter((t) => t.type === "epic").length,
    leaf_count: tasks.filter((t) => t.type === "leaf").length,
    domains: ["backend", "web", "db", "config", "ci", "docs"],
    phasical_mapping: {
      epic: { relation: null, status_on_import: "to-do" },
      leaf: {
        relation: "subtask",
        parent_field: "parent",
        blocks_field: "depends_on",
        description_field: "description",
      },
    },
  },
};

console.log(`Built roadmap: ${roadmap.meta.epic_count} epics, ${roadmap.meta.leaf_count} leaves`);

// ─── HTML plan file ─────────────────────────────────────────────────────────
const epics = tasks.filter((t) => t.type === "epic");
const leavesByParent = new Map();
for (const t of tasks.filter((t) => t.type === "leaf")) {
  if (!leavesByParent.has(t.parent)) leavesByParent.set(t.parent, []);
  leavesByParent.get(t.parent).push(t);
}

function esc(s) {
  return String(s ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function deps(t) {
  return (t.depends_on ?? []).length ? (t.depends_on ?? []).join(", ") : "—";
}

function docRef(t) {
  return (t.doc_ref ?? []).length ? (t.doc_ref ?? []).join(", ") : "—";
}

function mdToHtml(md) {
  return esc(md)
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2">$1</a>')
    .replace(/^## (.+)$/gm, "<h4>$1</h4>")
    .replace(/^- \[ \] (.+)$/gm, "<li class='ac'>$1</li>")
    .replace(/^- `([^`]+)`$/gm, "<li><code>$1</code></li>")
    .replace(/^- (.+)$/gm, "<li>$1</li>")
    .replace(/\n\n/g, "</p><p>")
    .replace(/\n/g, "<br>");
}

const epicSections = epics
  .map((epicTask) => {
    const children = leavesByParent.get(epicTask.id) ?? [];
    const rows = children
      .map((leafTask) => {
        const acCount = (leafTask.acceptance_criteria ?? []).length;
        const descPreview =
          leafTask.description.length > 200
            ? leafTask.description.slice(0, 200).replace(/\n/g, " ") + "…"
            : leafTask.description.replace(/\n/g, " ");
        return `        <tr data-task-id="${esc(leafTask.id)}">
          <td><code>${esc(leafTask.id)}</code></td>
          <td class="cb">- [ ]</td>
          <td>
            <strong>${esc(leafTask.title)}</strong>
            <details class="task-detail">
              <summary>${esc(descPreview)} <span class="meta">(${acCount} AC · ${esc(docRef(leafTask))})</span></summary>
              <div class="desc-body">${mdToHtml(leafTask.description)}</div>
            </details>
          </td>
          <td><span class="tag">${esc(leafTask.domain)}</span></td>
          <td>${esc(deps(leafTask))}</td>
          <td>${esc(docRef(leafTask))}</td>
        </tr>`;
      })
      .join("\n");

    return `  <section class="epic" id="${esc(epicTask.id)}">
    <h2><code>${esc(epicTask.id)}</code> ${esc(epicTask.title)} <span class="tag">${esc(epicTask.domain)}</span></h2>
    <div class="epic-body">${mdToHtml(epicTask.description)}</div>
    <table>
      <thead>
        <tr><th>ID</th><th>Status</th><th>Task</th><th>Domain</th><th>Depends</th><th>Doc Ref</th></tr>
      </thead>
      <tbody>
${rows || '        <tr><td colspan="6">No leaves</td></tr>'}
      </tbody>
    </table>
  </section>`;
  })
  .join("\n\n");

const html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Roadmap — Release Ops</title>
<style>
@import url('https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;700&family=Inter:wght@400;500;600;700&display=swap');
:root {
  --ink: #10141a; --panel: #171d26; --panel-2: #1c2330; --border: #262e3a;
  --text: #dce3ea; --text-dim: #8590a3; --text-faint: #5b6577;
  --amber: #e8a33d; --teal: #4fd1c5; --mono: 'JetBrains Mono', ui-monospace, monospace; --sans: 'Inter', sans-serif;
  --radius: 6px; --maxw: 1100px;
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--ink); color: var(--text); font-family: var(--sans); line-height: 1.6; font-size: 15px; }
a { color: var(--teal); text-decoration: none; }
a:hover { text-decoration: underline; }
.wrap { max-width: var(--maxw); margin: 0 auto; padding: 48px 24px 96px; }
.topnav { display: flex; gap: 20px; font-family: var(--mono); font-size: 13px; color: var(--text-dim); padding: 18px 24px; border-bottom: 1px solid var(--border); flex-wrap: wrap; align-items: center; }
.topnav a { color: var(--text-dim); }
.topnav a.brand { color: var(--amber); font-weight: 700; }
.topnav .current { color: var(--text); }
.topnav .sep { color: var(--text-faint); }
.eyebrow { font-family: var(--mono); font-size: 12px; letter-spacing: 0.08em; text-transform: uppercase; color: var(--amber); margin: 0 0 10px; }
h1 { font-size: clamp(28px, 4vw, 40px); font-weight: 700; margin: 0 0 14px; }
h2 { font-size: 20px; margin: 36px 0 10px; padding-bottom: 8px; border-bottom: 1px solid var(--border); }
h4 { font-size: 14px; color: var(--amber); margin: 14px 0 6px; font-family: var(--mono); }
p.lede { color: var(--text-dim); font-size: 17px; max-width: 760px; }
.card { background: var(--panel); border: 1px solid var(--border); border-radius: var(--radius); padding: 16px 18px; margin: 20px 0; font-size: 14px; color: var(--text-dim); }
.epic-body { background: var(--panel); border: 1px solid var(--border); border-radius: var(--radius); padding: 14px 16px; margin: 10px 0 16px; font-size: 14px; color: var(--text-dim); }
.epic-body li, .desc-body li { margin: 4px 0; }
.epic-body li.ac, .desc-body li.ac { list-style: none; padding-left: 0; }
.epic-body li.ac::before, .desc-body li.ac::before { content: "☐ "; color: var(--amber); font-family: var(--mono); }
table { width: 100%; border-collapse: collapse; margin: 12px 0 24px; font-size: 13px; }
th, td { text-align: left; padding: 9px 10px; border-bottom: 1px solid var(--border); vertical-align: top; }
th { font-family: var(--mono); font-size: 10px; text-transform: uppercase; color: var(--text-faint); }
td.cb { font-family: var(--mono); white-space: nowrap; color: var(--amber); width: 56px; }
code { font-family: var(--mono); font-size: 0.88em; background: var(--panel-2); border: 1px solid var(--border); border-radius: 4px; padding: 1px 5px; color: var(--teal); }
.tag { display: inline-block; font-family: var(--mono); font-size: 10px; padding: 1px 7px; border-radius: 3px; border: 1px solid var(--border); color: var(--text-dim); margin-left: 6px; }
.toc { columns: 2; font-size: 14px; }
.toc a { color: var(--text-dim); }
.docnav { margin-top: 32px; font-family: var(--mono); font-size: 13px; color: var(--text-dim); }
details.task-detail { margin-top: 6px; }
details.task-detail summary { cursor: pointer; color: var(--text-dim); font-size: 13px; list-style: disclosure-closed; }
details.task-detail[open] summary { margin-bottom: 8px; color: var(--text); }
details.task-detail .meta { color: var(--text-faint); font-size: 12px; }
.desc-body { background: var(--panel-2); border: 1px solid var(--border); border-radius: var(--radius); padding: 12px 14px; font-size: 13px; color: var(--text-dim); max-height: 480px; overflow-y: auto; }
@media (max-width: 800px) { table { font-size: 12px; } .toc { columns: 1; } }
</style>
</head>
<body>
<nav class="topnav">
  <a class="brand" href="index.html">release-ops</a>
  <span class="sep">/</span>
  <a href="index.html">docs</a>
  <span class="sep">·</span>
  <span class="current">roadmap</span>
  <span class="sep">·</span>
  <a href="roadmap.json">roadmap.json</a>
</nav>
<div class="wrap">
  <p class="eyebrow">Implementation plan · MVP</p>
  <h1>Release Ops roadmap</h1>
  <p class="lede">
    ${roadmap.meta.epic_count} epics · ${roadmap.meta.leaf_count} leaf tasks · generated ${roadmap.generated}.
    Each task has a full markdown description with spec backlinks, implementation notes, and acceptance criteria.
    Canonical source: <a href="roadmap.json"><code>roadmap.json</code></a> (<code>${roadmap.format}</code>).
    Regenerate: <code>node scripts/generate-roadmap.mjs</code>.
  </p>

  <div class="card">
    <strong>Status checkboxes</strong> (orchestrator): <code>- [ ]</code> not started ·
    <code>- [~]</code> awaiting verification · <code>- [x]</code> verified · <code>- [!]</code> failed.<br>
  Expand any task row for the full description. <strong>Phasical import</strong> uses <code>description</code> field from JSON (markdown with spec links).
  </div>

  <h2>Epic index</h2>
  <nav class="toc">
${epics.map((e) => `    <div><a href="#${esc(e.id)}"><code>${esc(e.id)}</code> ${esc(e.title)}</a></div>`).join("\n")}
  </nav>

${epicSections}

  <p class="docnav"><a href="index.html">Docs</a> · <a href="specs.html">Spec</a> · <a href="roadmap.json">JSON</a></p>
</div>
</body>
</html>
`;

writeFileSync(OUT_HTML, html);
console.log(`Wrote ${OUT_HTML}`);

// Quality gate
const emptyDesc = tasks.filter((t) => !t.description || t.description.length < 200);
const fewAc = tasks.filter((t) => t.type === "leaf" && (t.acceptance_criteria ?? []).length < 5);
if (emptyDesc.length) console.warn(`WARN: ${emptyDesc.length} tasks with short/missing descriptions`);
if (fewAc.length) console.warn(`WARN: ${fewAc.length} leaves with fewer than 5 AC`);

writeFileSync(OUT_JSON, JSON.stringify(roadmap, null, 2) + "\n");

// Spec coverage audit (reads docs/roadmap.json; result embedded in meta)
const audit = spawnSync("node", [join(__dirname, "audit-roadmap-spec.mjs")], {
  encoding: "utf8",
});
roadmap.meta.spec_audit = {
  passed: audit.status === 0,
  exit_code: audit.status,
  stdout: audit.stdout?.trim() ?? "",
};
writeFileSync(OUT_JSON, JSON.stringify(roadmap, null, 2) + "\n");
console.log(`Wrote ${OUT_JSON}`);
if (audit.status !== 0) {
  console.warn("Spec audit reported gaps — see meta.spec_audit in roadmap.json");
  console.warn(audit.stdout);
} else {
  console.log("Spec audit: all requirements covered, no backend/frontend drift");
}
