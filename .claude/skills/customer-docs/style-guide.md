# Customer docs style guide — Release Ops

Voice, structure and terminology rules for the Release Ops docs site (`apps/docs`, Starlight). Referenced by [SKILL.md](SKILL.md) on every write pass; release-ops paths and the glossary are in [docs-config.md](docs-config.md).

## Voice and tone

| Rule | Detail |
| ---- | ------ |
| Audience | People who self-host Release Ops and configure it in the web UI: technical, but not Release Ops developers. Assume Docker and API tokens are familiar; do not explain Go, Next.js or the database. |
| Person | Second person ("you") |
| Tense | Present tense |
| Steps | Imperative ("Click **Save**", not "The user should click") |
| Sentences | Short; one idea per sentence |
| Fluff | No marketing hype or empty superlatives |
| Language | English only (baseline) |

## Task orientation

Every **page doc** answers: **What can I do here, and how?**

| Do | Don't |
| -- | ----- |
| Describe user goals and actions | Describe React components, Go packages, or SQLite tables |
| Lead with outcomes | Lead with architecture |
| Link to related tasks | Duplicate full procedures from other pages |

## Page doc structure

Use [page-doc-template.md](page-doc-template.md). Starlight renders the frontmatter `title` as the H1, so the body starts with the purpose sentence. Required sections:

1. **One-sentence purpose** — first line of the body
2. **Prerequisites** — what must be true before starting (or "None")
3. **Step-by-step sections** — one `##` heading per key action on the page
4. **Field reference** — table of labels users see in the UI (from `apps/web/messages/en.json`)
5. **Troubleshooting** — common problems for this page only
6. **Related pages** — links to adjacent docs

## Getting started structure

Use [getting-started-template.md](getting-started-template.md).

- Saved as `guide/first-steps.md`; installation stays in the synced Getting started page (`docs/getting-started.md`) — link, do not repeat
- Happy path from first sign-in to the **first ticket created from a release** (there is no public sign-up)
- **≤ 10 numbered steps** — link to page docs instead of duplicating detail
- End with **Next steps** table and FAQ link

## Terminology

| Rule | Detail |
| ---- | ------ |
| Glossary | [docs-config.md](docs-config.md) § Glossary — internal term → customer-facing term |
| Consistency | One term per concept across all pages |
| Unknown mapping | **Ask the user** — never guess (`00-project.md`: ask before guessing) |
| Providers | Release sources: GitHub, GitLab, Gitea, Forgejo, Codeberg. Ticket integrations: Kaneo, Jira, Linear. Use these exact product names. |
| Forbidden in customer docs | Table/column names (`monitored_repos`, `on_open_ticket_policy`), code identifiers, `/api/...` routes, env var names (except in the install guide `docs/getting-started.md`) |

## Accuracy

| Rule | Detail |
| ---- | ------ |
| Source of truth | `docs/specs.html`, then app code in `apps/web` (read-only) and labels in `apps/web/messages/en.json` |
| Unverified flows | List under **Open questions** in the proposal — do not publish |
| Fabrication | Never invent features, buttons, fields, or behaviour |
| UI text | Quote visible labels from the app when known; do not invent label copy |
| Screenshots | Never fabricate images; use `<!-- SCREENSHOT: description -->` placeholders |

## Security and privacy

Never include in customer docs:

- Secrets, API keys, tokens, or credentials
- Internal-only URLs (staging, admin panels, VPN endpoints)
- Admin-only or operator-only endpoints
- Real API tokens or token-shaped strings (write `<your API token>`)
- Personal data examples (use placeholders like `you@example.com`, `https://kaneo.example.com`)

## Screenshots

Insert `<!-- SCREENSHOT: short description -->` where a visual helps (HTML comment, so nothing renders until a real image replaces it). This skill **never** captures screenshots.

## Navigation and naming

- File names: **kebab-case**
- Doc titles: Title Case for H1; sentence case for descriptions
- Cross-links: relative paths within the docs site (`../integrations/`) so they work under the `/release-ops` base
- Sidebar (`apps/docs/astro.config.mjs`): "User guide" order — First steps, Dashboard, Integrations, Ticket projects, Repos, Poll runs, Notifications, Settings, Users, Profile, Signing in

## FAQ and concept pages

**Concept pages** (`concepts/`) explain one product idea without walking through a single screen — e.g. how release detection works, status mapping, open-ticket policies (supersede, merge, skip if open).

**FAQ** (`faq.md`) answers recurring questions in Q&A format — link to page docs for procedural detail; do not duplicate full step lists.

## Review checklist (before marking coverage `current`)

- [ ] Purpose sentence matches what the page actually does
- [ ] Every documented action exists in code or spec
- [ ] Field table matches visible UI labels
- [ ] No internal jargon (glossary applied)
- [ ] Related links resolve (`npm run docs:build` passes)
- [ ] English only; short sentences
- [ ] No secrets or internal URLs
