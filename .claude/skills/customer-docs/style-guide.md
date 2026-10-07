# Customer docs style guide

Single source of truth for voice, structure, and terminology. Referenced by [SKILL.md](SKILL.md) on every write pass.

## Voice and tone

| Rule | Detail |
| ---- | ------ |
| Audience | End users — not developers unless `docs.config.md` says technical audience |
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
| Describe user goals and actions | Describe React components, services, or DB tables |
| Lead with outcomes | Lead with architecture |
| Link to related tasks | Duplicate full procedures from other pages |

## Page doc structure

Use [page-doc-template.md](page-doc-template.md). Required sections:

1. **One-sentence purpose** — immediately under the H1
2. **Prerequisites** — what must be true before starting (or "None")
3. **Step-by-step sections** — one `##` heading per key action on the page
4. **Field and option reference** — table of labels users see in the UI
5. **Troubleshooting** — common problems for this page only
6. **Related pages** — links to adjacent docs

## Getting started structure

Use [getting-started-template.md](getting-started-template.md).

- Happy path from signup/login (or equivalent) to **first success moment**
- **≤ 10 numbered steps** — link to page docs instead of duplicating detail
- End with **Next steps** table and FAQ link

## Terminology

| Rule | Detail |
| ---- | ------ |
| Glossary | Maintain `.agents/project/customer-docs/glossary.md` — internal term → customer-facing term |
| Consistency | One term per concept across all pages |
| Unknown mapping | **Ask the user** — never guess (same spirit as `00-project` "ask before guessing") |
| Forbidden in customer docs | Internal entity names, DB column names, code identifiers, env var names, API route paths meant for integrators only |

## Accuracy

| Rule | Detail |
| ---- | ------ |
| Source of truth | App code (read-only) + spec docs per `doc-index.md` |
| Unverified flows | List under **Open questions** in the proposal — do not publish |
| Fabrication | Never invent features, buttons, fields, or behaviour |
| UI text | Quote visible labels from the app when known; do not invent label copy |
| Screenshots | Never fabricate images; use `{SCREENSHOT: description}` only when config policy allows |

## Security and privacy

Never include in customer docs:

- Secrets, API keys, tokens, or credentials
- Internal-only URLs (staging, admin panels, VPN endpoints)
- Admin-only or operator-only endpoints
- Personal data examples (use placeholders like `you@example.com`)

## Screenshots

| Policy (from config) | Action |
| -------------------- | ------ |
| PLACEHOLDERS | Insert `{SCREENSHOT: short description}` where a visual helps |
| NONE | No image placeholders |

This skill **never** captures screenshots.

## Navigation and naming

- File names: **kebab-case**
- Doc titles: Title Case for H1; sentence case for descriptions
- Cross-links: relative paths within the docs site
- Sidebar order: Getting started first, then high-traffic flows, then settings/admin (if user-facing)

## FAQ and concept pages

**Concept pages** explain one product idea (e.g. "Workspaces", "Roles") without walking through a single screen.

**FAQ** answers recurring questions in Q&A format — link to page docs for procedural detail; do not duplicate full step lists.

## Review checklist (before marking coverage `current`)

- [ ] Purpose sentence matches what the page actually does
- [ ] Every documented action exists in code or spec
- [ ] Field table matches visible UI labels
- [ ] No internal jargon (glossary applied)
- [ ] Related links resolve
- [ ] English only; short sentences
- [ ] No secrets or internal URLs
