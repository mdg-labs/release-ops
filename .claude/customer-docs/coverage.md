# Docs coverage

Status values: `current` (checked against the route's source at the SHA), `missing` (planned, not written yet). The SHA is the commit the doc was last checked against. Pages marked with an item number are written by that item; each adds its sidebar entry when it lands.

| Route | Doc path | Status | Last-synced SHA | Notes |
| ----- | -------- | ------ | --------------- | ----- |
| site root | apps/docs/src/content/docs/index.mdx | current | 8289c1e | Landing page (#223) |
| `/setup`, `/login`, `/login/link` | docs/getting-started.md → getting-started.md | current | 8289c1e | Install, first run, polling mode (#223) |
| (operating the instance) | docs/getting-started.md → getting-started.md § Operating your instance | current | 8289c1e | Network access, backup, restore, upgrade (#223) |
| (concept) how runs are triggered | concepts/how-runs-are-triggered.md | current | 7bb8356 | #224 |
| (concept) update PRs and tickets, status mapping | concepts/update-prs-and-tickets.md, concepts/status-mapping.md | current | 7bb8356 | #224 |
| (concept) roles | concepts/roles.md | current | 7bb8356 | #224 |
| (concept) the global Renovate config and the job token scope | concepts/global-renovate-config.md | current | 7bb8356 | #224 |
| (guide) migrating from a Renovate GitHub Action or self-hosted Renovate | guide/migrating-from-renovate.md | current | 7bb8356 | #224 |
| `/` | guide/dashboard.md | missing | — | #225 |
| `/repositories` | guide/repositories.md | missing | — | #225 |
| `/jobs` | guide/jobs.md | missing | — | #225, includes the Problems view |
| `/integrations` | guide/integrations.md | missing | — | #225 |
| `/ticket-projects` | guide/ticket-projects.md | missing | — | #225 |
| `/settings` | guide/settings.md | missing | — | #225, includes key rotation |
| `/about` | — | missing | — | Not in #225's list; decide there whether it gets a page |
| (reference) `/metrics` and alert rules | metrics.md | current | 7bb8356 | #226 |
| (FAQ) | faq.md | current | 7bb8356 | #226 |
