# Documentation sources

Human-readable documentation is published to **GitHub Pages** via the Starlight site in `apps/docs/`.

| Role | Location |
|------|----------|
| **Published site** | https://mdg-labs.github.io/release-ops/ |
| **Site source** | `apps/docs/` (Astro + Starlight) |
| **Sync script** | `scripts/sync-docs-content.mjs` |
| **Roadmap machine source** | `docs/roadmap.json` |
| **Roadmap plan file** (orchestrator) | `docs/roadmap.html` (generated) |

## Authoring

| Content | Edit here | Notes |
|---------|-----------|-------|
| Getting started, MVP checklist | `docs/*.md` | Synced to Starlight on build |
| Product spec, stack, schema | `docs/*.html` | Legacy HTML sources; synced to `.mdx` |
| Introduction hub | `docs/index.html` + `apps/docs` splash | Card grid lives in sync script |
| Roadmap | `scripts/roadmap/` → `node scripts/generate-roadmap.mjs` | JSON + HTML + docs site copy |

```bash
npm run docs:sync    # regenerate Starlight content from docs/
npm run dev:docs     # local preview at http://localhost:4321/release-ops/
npm run docs:build   # production build → apps/docs/dist/
```

The `docs-pages` GitHub Actions workflow deploys on push to `dev` when docs paths change.
