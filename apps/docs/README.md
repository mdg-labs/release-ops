# Release Ops docs site

[Astro](https://astro.build/) + [Starlight](https://starlight.astro.build/) documentation for GitHub Pages.

## Commands

```bash
# from repo root
npm run docs:sync     # copy docs/ → src/content/docs/
npm run dev:docs      # http://localhost:4321/release-ops/
npm run docs:build    # static output in dist/
```

## Layout

- `src/content/docs/` — synced from `docs/` (do not hand-edit; changes will be overwritten)
- `src/pages/roadmap/` — interactive roadmap from `src/data/roadmap.json`
- `src/styles/custom.css` — teal/amber theme + roadmap styles
- `astro.config.mjs` — `base: '/release-ops'` for GitHub Pages project site

## Deploy

`.github/workflows/docs-pages.yml` builds on push to `main` and publishes to GitHub Pages.
