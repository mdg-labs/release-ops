# Documentation sources (v1 history)

> **v1 history.** This page describes how the v1 docs were laid out and published. The v2 documentation hub is [index.html](index.html); the customer docs for v2 are being rewritten (#223). Kept for reference only; the rows for the MVP checklist and the roadmap below refer to v1 history documents.

Customer-facing documentation is published to **GitHub Pages** via the Starlight site in `apps/docs/`. The spec, stack, schema, threat model, MVP checklist (v1 history) and roadmap (v1 history) stay in this folder and are not published on the site.

| Role | Location |
|------|----------|
| **Published site** | https://mdg-labs.github.io/release-ops/ |
| **Site source** | `apps/docs/` (Astro + Starlight) |
| **Sync script** | `scripts/sync-docs-content.mjs` (writes only `getting-started.md`) |
| **Roadmap machine source** | `docs/roadmap.json` |
| **Roadmap plan file** (orchestrator) | `docs/roadmap.html` (generated) |

## Authoring

| Content | Edit here | Notes |
|---------|-----------|-------|
| Getting started | `docs/getting-started.md` | Synced to the site on build; links to the internal docs below are rewritten to GitHub |
| Landing page, customer docs | `apps/docs/src/content/docs/` | Hand-written (`index.mdx` is the site front page); not synced |
| Product spec, stack, schema | `docs/*.html` | Repo only, not on the site |
| Threat model | `docs/threat-model.md` | Repo only, not on the site |
| MVP checklist (v1 history) | `docs/mvp-checklist.md` | Repo only, not on the site |
| Introduction hub | `docs/index.html` | Repo only, not on the site |
| Roadmap (v1 history) | `scripts/roadmap/` → `node scripts/generate-roadmap.mjs` | Writes `docs/roadmap.json` and `docs/roadmap.html`; repo only, not on the site |

```bash
npm run docs:sync    # regenerate the Getting started page from docs/getting-started.md
npm run dev:docs     # local preview at http://localhost:4321/release-ops/
npm run docs:build   # production build → apps/docs/dist/
```

The `docs-pages` GitHub Actions workflow deploys on push to `main` when docs paths change.
