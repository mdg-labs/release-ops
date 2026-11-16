# Customer docs config — Release Ops

Configuration for the `customer-docs` skill in this repo, written by its bootstrap mode and owned by the repo — the skill itself is vendored and never edited here. Edit this file when the docs site or the app layout changes. Paths are relative to the repo root.

## Product

| Field | Value |
| ----- | ----- |
| Product name | Release Ops |
| Core use case | Self-hosted dependency-update bot: one GitHub App runs Renovate for the repositories it is installed on and opens the update pull requests, and Release Ops creates a ticket in Kaneo, Jira or Linear for each update PR and keeps it in step with the PR. |
| Audience | Small self-hosting dev and ops teams and individuals. Technical: they run Docker Compose, can set environment variables and a reverse proxy, know GitHub Apps and organizations, and already use or know Renovate. |
| Deployment model | Self-hosting only: two services (the app and the worker) from one Docker Compose project, one instance per GitHub App. Never mention, hint at or compare with a hosted, cloud or managed version of Release Ops, on any page. |
| Accounts and roles | No local accounts, no passwords, no mail, no invitations. People sign in with GitHub through the instance's own GitHub App. Two roles, admin and viewer, derived from GitHub (organization owners and the optional admin team are admins; other organization members are viewers; on a personal account only the owner is an admin). Roles are never edited in the UI. A bootstrap admin can be pinned with a GitHub login. |
| Behaviour source of truth | `docs/specs.html` (v2 contract, APIs, UI, domain logic), then `db/schema.sql`, then `apps/web/messages/en.json` for UI labels. Never document behaviour that is not in the spec and built; a page or option that exists only in the spec is left out until it ships. |

## Docs site

| Field | Value |
| ----- | ----- |
| Framework | Astro + Starlight (`apps/docs`, npm workspace) |
| Published URL | `https://mdg-labs.github.io/release-ops/` (Astro `base: "/release-ops"`) |
| Deploy | `.github/workflows/docs-pages.yml` — builds on push to `main` (paths `apps/docs/**`, `docs/**`, `scripts/sync-docs-content.mjs`) and publishes to GitHub Pages |
| Local preview | `npm run docs:sync && npm run dev:docs` → `http://localhost:4321/release-ops/` |
| Build check | `npm run docs:build` (runs sync, then `astro build`) |
| Content root | `apps/docs/src/content/docs/` |
| Sidebar / nav file | `apps/docs/astro.config.mjs` → `starlight({ sidebar: [...] })` |
| Language | English only |

### Synced files — never hand-edit

`scripts/sync-docs-content.mjs` (`npm run docs:sync`) regenerates exactly one file from `docs/` and overwrites any manual change:

| Generated file in `apps/docs/src/content/docs/` | Edit this source instead |
| ----------------------------------------------- | ------------------------ |
| `getting-started.md` | `docs/getting-started.md` (operator install guide: Compose, URLs, setup, first run) |

The sync script rewrites links in that file that point at the internal docs (`specs.html`, `stack.html`, `schema.html`, `roadmap.html`) to their GitHub URLs. The internal docs stay in `docs/` and are never published as site pages or listed in the sidebar. Hand-written customer pages never link them; only the synced `getting-started.md` links them, and only as GitHub URLs.

`index.mdx` (the landing page at the site root) is hand-written, not synced. Edit it through this skill's proposal gate, following [landing-page-template.md](../skills/customer-docs/landing-page-template.md).

The sync script writes only `getting-started.md` and deletes nothing, so every other file is safe.

### Customer doc layout

| Doc type | Path | Sidebar group |
| -------- | ---- | ------------- |
| Landing page (what it is / is not / who it is for / how it works) | `apps/docs/src/content/docs/index.mdx` (hand-written, splash template) | none (reached through the site title link) |
| Install, first run and operating the instance (network access, backup, restore, upgrade) | `docs/getting-started.md` (synced to `getting-started.md`; the operating part is its second half) | Start here → "Getting started" |
| Concept pages | `apps/docs/src/content/docs/concepts/<concept>.md` | Concepts |
| Migration guide | `apps/docs/src/content/docs/guide/migrating-from-renovate.md` | User guide |
| Page docs (one per in-app page) | `apps/docs/src/content/docs/guide/<page>.md` | User guide |
| Metrics reference | `apps/docs/src/content/docs/metrics.md` | Reference |
| FAQ | `apps/docs/src/content/docs/faq.md` | Start here |

File names: kebab-case. Add each new page to the `sidebar` array in `apps/docs/astro.config.mjs` (`{ label: "<Title>", slug: "<path without extension>" }`), creating a group on first use. The sidebar lists only pages that exist on the site, and a page links only to pages that exist.

### Frontmatter (Starlight `docsSchema`)

```yaml
---
title: "Repositories"          # required; Starlight renders it as the page H1
description: "One sentence."   # recommended; sentence case
---
```

Do not add a `# H1` in the body (Starlight already renders `title`). In the synced page the source `docs/getting-started.md` has no frontmatter and no H1: the sync adds the frontmatter. Links between hand-written pages are relative (`../concepts/roles/`); the sync rewrites links to other site pages through its link map, which carries the `/release-ops` base.

## App page inventory

| Field | Value |
| ----- | ----- |
| App | `apps/web` (Next.js App Router, served from the same container as the Go API) |
| Routes dir | `apps/web/app/` — `(authenticated)/` group = signed-in pages; top-level folders (`login`, `setup`) = public pages |
| Navigation source | `apps/web/components/app-sidebar.tsx` |
| UI labels | `apps/web/messages/en.json` (next-intl; every visible string lives here — quote labels from this file, never invent them) |
| Feature components | `apps/web/components/<feature>/` |

### Route → doc map

| Route | Page (UI title) | Doc path |
| ----- | --------------- | -------- |
| `/setup`, `/login`, `/login/link` | First-start setup and sign-in | `getting-started.md` (first run); the break-glass link in `faq.md` |
| `/` | Dashboard | `guide/dashboard.md` |
| `/repositories` | Repositories | `guide/repositories.md` |
| `/jobs` | Jobs and Problems | `guide/jobs.md` |
| `/integrations` | Integrations | `guide/integrations.md` |
| `/ticket-projects` | Ticket projects | `guide/ticket-projects.md` |
| `/settings` | Settings | `guide/settings.md` |
| `/about` | About | no page planned yet |

Re-scan `apps/web/app/` on every run: a `page.tsx` without a row here is a new page (missing doc). The v1 routes `/poll-runs`, `/repos`, `/notifications`, `/profile` and `/settings/users` no longer exist.

## Integrations (product feature)

| Role | Kinds (from `docs/specs.html`) |
| ---- | ------------------------------ |
| Platform | GitHub (github.com) only |
| Ticket integrations | Kaneo, Jira, Linear |
| Update engine | Renovate, unmodified and pinned per release, run by the worker |

Do not document GitLab, GitHub Enterprise Server, private-registry `hostRules` or any hosted offering; the spec lists them as later. Do not describe the v1 release monitoring, Shoutrrr notifications, local accounts or the other four release sources as features.

## Screenshots

| Field | Value |
| ----- | ----- |
| Policy | NONE |
| Format | Text and tables only. The skill never captures images. |

## Glossary (internal term → customer term)

One customer term per concept across all pages. Extend this table (after approval) when a new term appears; ask the user when a mapping is unclear.

| Internal / code term | Customer-facing term |
| -------------------- | -------------------- |
| `github_app` | the GitHub App (the instance's App; "the App" after first use) |
| `update_prs` | update PRs (the pull requests Renovate opens) |
| `update_tickets` | tickets (one per update PR) |
| `ticket_projects` | ticket project (a Kaneo project, Jira project key or Linear team) |
| `status_mapping` | status mapping |
| `integrations` | integrations (the ticket systems) |
| `jobs`, `renovate_run` | jobs (a Renovate run for one repository) |
| worker | worker (the second service that runs Renovate) |
| internal listener (`INTERNAL_ADDR`) | the app's internal listener |
| `auth_tokens` kind `setup`, `SETUP_TOKEN` | setup token |
| setup session | setup session (15 minutes) |
| polling fallback, polling mode | polling mode (degraded, no webhook) |
| safety net | hourly run (the safety net that starts every enabled repository) |
| `GLOBAL_CONFIG_REPO` file | global Renovate config |
| `PRESET_REPOSITORIES` | preset repositories |
| bootstrap admin, `BOOTSTRAP_ADMIN_GITHUB_LOGIN` | bootstrap admin |
| `release-ops admin login-link` | break-glass login link |
| installation | installation (exactly one per instance) |
| `app.db` | the database (`app.db` on the data volume) |

## Coverage tracking

File: `.claude/customer-docs/coverage.md` (table format in the skill).

## Commits

| Field | Value |
| ----- | ----- |
| Subject | `docs(docs): <summary>` |
| Scope | `docs` (allowed scopes: release-ops, api, db, config, ci, docs, deps) |
| Trailer | `Fixes #<n>` when the work belongs to a tracked item (its GitHub issue number), otherwise none |
