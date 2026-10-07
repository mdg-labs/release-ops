#!/usr/bin/env node
/**
 * Sync docs/getting-started.md into the apps/docs Starlight content.
 * Every other page under apps/docs/src/content/docs is hand-written.
 * Run: node scripts/sync-docs-content.mjs
 */
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const docsDir = join(root, "docs");
const outDir = join(root, "apps/docs/src/content/docs");

mkdirSync(outDir, { recursive: true });

// The site is served under a base path; internal links must carry it.
const astroConfigPath = join(root, "apps/docs/astro.config.mjs");
const { default: astroConfig } = await import(pathToFileURL(astroConfigPath).href);
const base = (astroConfig.base ?? "").replace(/\/+$/, "");
const withBase = (path) => `${base}${path}`;

const repoBlob = "https://github.com/mdg-labs/release-ops/blob/dev";

// Pages that exist on the site.
const linkMap = {
  "getting-started.md": withBase("/getting-started/"),
};

// Internal docs that stay in the repository only; links go to GitHub.
const repoOnlyDocs = [
  "specs.html",
  "stack.html",
  "schema.html",
  "mvp-checklist.md",
  "roadmap.html",
];

function rewriteLinks(content) {
  let out = content;
  for (const [from, to] of Object.entries(linkMap)) {
    out = out.replaceAll(`](${from}`, `](${to}`);
  }
  for (const file of repoOnlyDocs) {
    const escaped = file.replaceAll(".", "\\.");
    out = out.replace(
      new RegExp(`\\]\\(${escaped}(#[^)]*)?\\)`, "g"),
      `](${repoBlob}/docs/${file})`,
    );
  }
  return out;
}

console.log("Syncing docs content…");

function mdToDoc({ slug, title, description, sourceFile }) {
  const content = rewriteLinks(readFileSync(join(docsDir, sourceFile), "utf8"));
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

console.log("Done.");
