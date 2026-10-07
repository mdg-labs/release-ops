#!/usr/bin/env node
/**
 * Sync legacy docs/ HTML + markdown into apps/docs Starlight content.
 * Run: node scripts/sync-docs-content.mjs
 */
import { readFileSync, writeFileSync, mkdirSync, copyFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const docsDir = join(root, "docs");
const outDir = join(root, "apps/docs/src/content/docs");
const publicDir = join(root, "apps/docs/public");
const dataDir = join(root, "apps/docs/src/data");

mkdirSync(outDir, { recursive: true });
mkdirSync(publicDir, { recursive: true });
mkdirSync(dataDir, { recursive: true });

const linkMap = {
  "specs.html": "/spec/",
  "stack.html": "/stack/",
  "schema.html": "/schema/",
  "index.html": "/",
  "getting-started.md": "/getting-started/",
  "mvp-checklist.md": "/mvp-checklist/",
  "roadmap.html": "/roadmap/",
};

function rewriteLinks(html) {
  let out = html;
  for (const [from, to] of Object.entries(linkMap)) {
    const hash = from.replace(".html", "").replace(".md", "");
    out = out.replaceAll(`href="${from}#`, `href="${to}#`);
    out = out.replaceAll(`href='${from}#`, `href='${to}#`);
    out = out.replaceAll(`href="${from}"`, `href="${to}"`);
    out = out.replaceAll(`href='${from}'`, `href='${to}'`);
    if (from.endsWith(".html")) {
      out = out.replaceAll(`href="${hash}#`, `href="${to}#`);
    }
  }
  out = out.replaceAll("../db/schema.sql", "https://github.com/mdg-labs/release-ops/blob/dev/db/schema.sql");
  return out;
}

function extractWrap(html) {
  const match = html.match(/<div class="wrap">([\s\S]*?)<\/div>\s*<\/body>/i);
  if (!match) {
    throw new Error("Could not find .wrap content");
  }
  return rewriteLinks(match[1].trim());
}

function stripTopNav(html) {
  return html.replace(/<nav class="topnav">[\s\S]*?<\/nav>\s*/i, "");
}

function removeBalancedDiv(html, className) {
  const open = `<div class="${className}">`;
  const start = html.indexOf(open);
  if (start === -1) return html;
  let depth = 0;
  let i = start;
  while (i < html.length) {
    if (html.startsWith("<div", i)) depth++;
    if (html.startsWith("</div>", i)) {
      depth--;
      if (depth === 0) {
        return html.slice(0, start) + html.slice(i + "</div>".length);
      }
    }
    i++;
  }
  return html;
}

function stripLinkgrid(html) {
  let out = html.replace(
    /<h2[^>]*>\s*Documents\s*<\/h2>\s*/i,
    "",
  );
  return removeBalancedDiv(out, "linkgrid");
}

function escapeCurlyBracesForMdx(html) {
  return html.replace(/\{/g, "&#123;").replace(/\}/g, "&#125;");
}

function preCodeBlocksToMarkdown(html) {
  return html.replace(/<pre><code>([\s\S]*?)<\/code><\/pre>/gi, (_, code) => {
    const decoded = code
      .replace(/&lt;/g, "<")
      .replace(/&gt;/g, ">")
      .replace(/&amp;/g, "&")
      .replace(/&#123;/g, "{")
      .replace(/&#125;/g, "}");
    return `\n\`\`\`\n${decoded.trim()}\n\`\`\`\n`;
  });
}

function prepareHtmlForMdx(html) {
  return escapeCurlyBracesForMdx(preCodeBlocksToMarkdown(html));
}

function htmlToDoc({ slug, title, description, sourceFile, eyebrow }) {
  const raw = readFileSync(join(docsDir, sourceFile), "utf8");
  let body = stripTopNav(extractWrap(raw));
  body = body.replace(/<p class="docnav">[\s\S]*?<\/p>\s*$/i, "");
  if (eyebrow) {
    body = body.replace(/<p class="eyebrow">[\s\S]*?<\/p>\s*/i, "");
  }
  body = body.replace(/<h1>[\s\S]*?<\/h1>\s*/i, "");
  body = prepareHtmlForMdx(body);

  const frontmatter = `---
title: ${JSON.stringify(title)}
description: ${JSON.stringify(description)}
---

`;
  writeFileSync(join(outDir, `${slug}.mdx`), `${frontmatter}${body}\n`);
  console.log(`  wrote ${slug}.mdx`);
}

console.log("Syncing docs content…");

// Markdown sources — copy with frontmatter
function mdToDoc({ slug, title, description, sourceFile }) {
  const body = readFileSync(join(docsDir, sourceFile), "utf8");
  let content = body;
  for (const [from, to] of Object.entries(linkMap)) {
    content = content.replaceAll(`](${from}`, `](${to}`);
  }
  const frontmatter = `---
title: ${JSON.stringify(title)}
description: ${JSON.stringify(description)}
---

`;
  writeFileSync(join(outDir, `${slug}.md`), `${frontmatter}${content}`);
  console.log(`  wrote ${slug}.md`);
}

mdToDoc({
  slug: "getting-started",
  title: "Getting started",
  description: "Docker Compose setup, environment variables, admin bootstrap, and troubleshooting.",
  sourceFile: "getting-started.md",
});

mdToDoc({
  slug: "mvp-checklist",
  title: "MVP checklist",
  description: "Sign-off checklist for all 15 MVP acceptance criteria.",
  sourceFile: "mvp-checklist.md",
});

htmlToDoc({
  slug: "spec",
  title: "Product specification",
  description: "MVP contract — architecture, APIs, providers, UI, and acceptance criteria.",
  sourceFile: "specs.html",
  eyebrow: true,
});

htmlToDoc({
  slug: "stack",
  title: "Tech stack",
  description: "Go, Next.js, COSS, SQLite, Docker, and CI/CD tooling choices.",
  sourceFile: "stack.html",
  eyebrow: true,
});

htmlToDoc({
  slug: "schema",
  title: "Database schema",
  description: "SQLite app.db tables and relationships.",
  sourceFile: "schema.html",
  eyebrow: true,
});

// Introduction from index.html
const indexRaw = readFileSync(join(docsDir, "index.html"), "utf8");
let indexBody = stripTopNav(extractWrap(indexRaw));
indexBody = indexBody.replace(/<p class="eyebrow">[\s\S]*?<\/p>\s*/i, "");
indexBody = indexBody.replace(/<h1>[\s\S]*?<\/h1>\s*/i, "");
indexBody = stripLinkgrid(indexBody);
indexBody = indexBody.replace(/<p class="docnav">[\s\S]*?<\/p>\s*$/i, "");
indexBody = escapeCurlyBracesForMdx(indexBody);

const indexFrontmatter = `---
title: Introduction
description: Self-hosted release monitor with a web UI — poll releases and create tickets when they ship.
template: splash
hero:
  tagline: Self-hosted release monitor — one Docker container, full control.
  actions:
    - text: Get started
      link: /getting-started/
      icon: right-arrow
      variant: primary
    - text: View on GitHub
      link: https://github.com/mdg-labs/release-ops
      icon: external
      variant: minimal
---

import { CardGrid, LinkCard } from '@astrojs/starlight/components';

`;

const indexCards = `<CardGrid>
  <LinkCard title="Getting started" href="/getting-started/">
    Docker Compose setup, env vars, admin bootstrap, troubleshooting.
  </LinkCard>
  <LinkCard title="Product specification" href="/spec/">
    Architecture, schema, APIs, providers, UI, MVP acceptance criteria.
  </LinkCard>
  <LinkCard title="Tech stack" href="/stack/">
    Go, Next.js, COSS, session auth, SQLite, Docker.
  </LinkCard>
  <LinkCard title="Database schema" href="/schema/">
    SQLite app.db — HTML view; canonical DDL in db/schema.sql.
  </LinkCard>
  <LinkCard title="MVP checklist" href="/mvp-checklist/">
    Sign-off against all 15 acceptance criteria.
  </LinkCard>
  <LinkCard title="Roadmap" href="/roadmap/">
    Implementation phases — epics, leaf tasks, spec backlinks.
  </LinkCard>
</CardGrid>

`;

writeFileSync(
  join(outDir, "index.mdx"),
  `${indexFrontmatter}${indexBody}\n\n${indexCards}`,
);
console.log("  wrote index.mdx");

copyFileSync(join(docsDir, "roadmap.json"), join(publicDir, "roadmap.json"));
copyFileSync(join(docsDir, "roadmap.json"), join(dataDir, "roadmap.json"));
console.log("  copied roadmap.json → apps/docs/public/ and src/data/");

console.log("Done.");
