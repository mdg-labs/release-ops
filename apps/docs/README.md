# Release Ops docs site

[Astro](https://astro.build/) + [Starlight](https://starlight.astro.build/) documentation for GitHub Pages.

## Commands

```bash
# from repo root
npm run docs:sync     # docs/getting-started.md → src/content/docs/getting-started.md
npm run dev:docs      # http://localhost:4321/release-ops/
npm run docs:build    # static output in dist/
```

## Layout

- `src/content/docs/` — hand-written pages, including the landing page (`index.mdx`); only `getting-started.md` is synced from `docs/getting-started.md` (do not hand-edit it; changes will be overwritten)
- `src/styles/custom.css` — teal/amber theme
- `astro.config.mjs` — `base: '/release-ops'` for GitHub Pages project site

## Deploy

`.github/workflows/docs-pages.yml` builds on push to `main` and publishes to GitHub Pages.
