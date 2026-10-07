/** Canonical doc links for roadmap task descriptions (relative to docs/). */

export const SPEC = {
  purpose: { href: "specs.html#purpose", label: "§1 Purpose & Non-goals" },
  architecture: { href: "specs.html#architecture", label: "§2 Architecture" },
  auth: { href: "specs.html#auth", label: "§3 Authentication" },
  schema: { href: "specs.html#schema", label: "§4 Data model" },
  domain: { href: "specs.html#domain", label: "§5 Domain logic & Idempotency" },
  ticketProjects: { href: "specs.html#ticket-projects", label: "§5.3 Ticket projects & status mapping" },
  notifications: { href: "specs.html#notifications", label: "§5.5 Notifications (Shoutrrr)" },
  providers: { href: "specs.html#providers", label: "§6 Provider interfaces (Go)" },
  api: { href: "specs.html#api", label: "§7 REST API (Go)" },
  ui: { href: "specs.html#ui", label: "§8 Web UI & COSS Particles" },
  i18n: { href: "specs.html#i18n", label: "§8.5 Internationalization (i18n)" },
  schemaMigrations: { href: "specs.html#schema-migrations", label: "§4.0 Schema migrations (sqldiff)" },
  env: { href: "specs.html#env", label: "§9 Environment variables" },
  deployment: { href: "specs.html#deployment", label: "§10 Deployment" },
  ci: { href: "specs.html#ci", label: "§11 CI/CD (GitHub Actions)" },
  mvp: { href: "specs.html#mvp", label: "§12 MVP Acceptance Criteria" },
};

export const STACK = {
  layout: { href: "stack.html", label: "Tech stack — Repository layout" },
  backend: { href: "stack.html", label: "Tech stack — Backend (Go)" },
  frontend: { href: "stack.html", label: "Tech stack — Frontend (Next.js + COSS)" },
  data: { href: "stack.html", label: "Tech stack — Data" },
  quality: { href: "stack.html", label: "Tech stack — Quality" },
  cicd: { href: "stack.html", label: "Tech stack — CI/CD" },
  deploy: { href: "stack.html", label: "Tech stack — Deployment" },
};

export const SCHEMA = {
  sql: { href: "../db/schema.sql", label: "db/schema.sql (canonical)" },
  html: { href: "schema.html", label: "schema.html (browser view)" },
};

/** @param {Array<{href:string,label:string,note?:string}>} refs */
export function specRefs(refs) {
  return refs
    .map((r) => `- [${r.label}](${r.href})${r.note ? ` — ${r.note}` : ""}`)
    .join("\n");
}

/** Build markdown description for Phasical / orchestrator. */
export function describe({
  context,
  specs = [],
  stack = [],
  schema = [],
  implementation = [],
  acceptance = [],
  files = [],
  tests = [],
  outOfScope = [],
  relatedTasks = [],
}) {
  const parts = [];

  if (context) parts.push(`## Context\n\n${context}`);

  const allRefs = [
    ...specs.length ? [`## Spec references\n\n${specRefs(specs)}`] : [],
    ...stack.length ? [`## Stack references\n\n${specRefs(stack)}`] : [],
    ...schema.length ? [`## Schema references\n\n${specRefs(schema)}`] : [],
  ];
  parts.push(...allRefs);

  if (implementation.length)
    parts.push(`## Implementation notes\n\n${implementation.map((l) => `- ${l}`).join("\n")}`);

  if (files.length)
    parts.push(`## Files\n\n${files.map((f) => `- \`${f}\``).join("\n")}`);

  if (acceptance.length)
    parts.push(
      `## Acceptance criteria\n\n${acceptance.map((a) => `- [ ] ${a}`).join("\n")}`
    );

  if (tests.length)
    parts.push(`## Tests\n\n${tests.map((t) => `- \`${t}\``).join("\n")}`);

  if (outOfScope.length)
    parts.push(`## Out of scope\n\n${outOfScope.map((o) => `- ${o}`).join("\n")}`);

  if (relatedTasks.length)
    parts.push(`## Related roadmap tasks\n\n${relatedTasks.map((t) => `- \`${t}\``).join("\n")}`);

  return parts.join("\n\n");
}
