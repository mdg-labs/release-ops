---
name: customer-docs
description: >-
  Write and maintain Release Ops end-user documentation on the Starlight docs site
  (apps/docs): one page per web UI page in apps/web, a "First steps" guide, concept
  pages and an FAQ. English only. Proposal first, writes only after explicit approval.
  Use when the user asks to write customer docs, document page X (repos, integrations,
  ticket projects, …), create a getting started guide, update user documentation,
  check docs coverage, or run a full docs pass on the app.
---

# Customer docs

Write and maintain **end-user documentation** for Release Ops: one doc page per page of the web UI (`apps/web`), plus a "First steps" guide, concept pages and an FAQ, published on the Starlight site in `apps/docs` (`https://mdg-labs.github.io/release-ops/`). English only.

All release-ops specifics (docs root, synced vs hand-written files, sidebar file, route → doc map, frontmatter, glossary, commit format) are in [docs-config.md](docs-config.md). Voice and structure rules are in [style-guide.md](style-guide.md).

## Files

| What | Path (repo root relative) |
| ---- | ---- |
| This skill | `.claude/skills/customer-docs/SKILL.md` |
| Release-ops config + glossary | `.claude/skills/customer-docs/docs-config.md` |
| Style rules | `.claude/skills/customer-docs/style-guide.md` |
| Templates | `.claude/skills/customer-docs/page-doc-template.md`, `getting-started-template.md` |
| Coverage inventory (created on first approved write) | `.agents/project/customer-docs/coverage.md` |
| Docs content root | `apps/docs/src/content/docs/` (`guide/`, `concepts/`, `faq.md`) |
| Sidebar | `apps/docs/astro.config.mjs` |
| App routes / nav / labels (read-only) | `apps/web/app/`, `apps/web/components/app-sidebar.tsx`, `apps/web/messages/en.json` |
| Behaviour spec (read-only) | `docs/specs.html`, `db/schema.sql`, `README.md` |

Never hand-edit the synced files listed in `docs-config.md` § Synced files (`getting-started.md`, `mvp-checklist.md`, `spec.mdx`, `stack.mdx`, `schema.mdx`, `index.mdx` under `apps/docs/src/content/docs/`). Edit their sources in `docs/` and run `npm run docs:sync`.

---

## Mode detection

| Mode | User says | Action |
| ---- | --------- | ------ |
| **1 — Full pass** | "write all docs", "document the app", "full docs pass" | Inventory → proposal → write batch-by-batch |
| **2 — Single page** | "document the repos page", "write docs for /ticket-projects" | Scoped inventory → outline → write one page |
| **3 — Update / drift** | "update docs", "check docs coverage", "sync docs" | Diff routes vs coverage → propose → execute |

Default when ambiguous: Mode 3 if `coverage.md` exists, else Mode 1.

---

## Approval gate (mandatory — no exceptions)

**Phase 1 — investigate + written proposal only.** No file writes (no doc pages, no `astro.config.mjs` sidebar edits, no `docs/` source edits, no `coverage.md`, no glossary changes in `docs-config.md`).

**Phase 2 — writes** only after the user explicitly approves the proposal in chat (**approve**, **yes write them**, **go ahead**, **LGTM**).

| Rule | Detail |
| ---- | ------ |
| Always propose first | Full pass, single page, drift check — every run that writes docs |
| Never skip Phase 1 | Even if the user said "write all docs" upfront |
| Wait for reply | End Phase 1 with: *"Approve to write these docs (or tell me what to change)."* |
| Re-propose after edits | User requests changes → updated proposal; no writes until approved again |
| Sub-agents | Parent agents must not write docs while customer-docs waits for approval |

---

## Mode 1 — Full docs pass

### Phase 1 — Inventory + proposal (no writes)

1. Read [docs-config.md](docs-config.md), [style-guide.md](style-guide.md), and `.agents/project/customer-docs/coverage.md` if it exists.
2. Inventory pages **read-only**: every `page.tsx` under `apps/web/app/`, the sidebar in `apps/web/components/app-sidebar.tsx`, the settings sub-nav, and the feature components under `apps/web/components/<feature>/` (buttons, forms, dialogs, table columns, empty states). Take every label from `apps/web/messages/en.json`.
3. Read the matching sections of `docs/specs.html` (integrations and kinds, ticket projects with status mapping and open-ticket policy, polling and release detection, notifications, auth and invitations, settings). Do not invent behaviour.
4. Check the existing site: `apps/docs/src/content/docs/` and the `sidebar` in `apps/docs/astro.config.mjs`. `docs/getting-started.md` already covers installation; link to it instead of repeating it.
5. Post the written proposal (format below) and **stop**.

### Phase 2 — Write (after approval only)

1. Write pages batch-by-batch from [page-doc-template.md](page-doc-template.md) and [getting-started-template.md](getting-started-template.md) at the paths in `docs-config.md` § Route → doc map.
2. Add sidebar entries in `apps/docs/astro.config.mjs` ("User guide" / "Concepts" groups).
3. Run `npm run docs:build` to check the site builds and links resolve. On failure, fix the docs; never touch app code.
4. Update `.agents/project/customer-docs/coverage.md` (route → doc path → last-synced SHA → status).
5. Add new terms to `docs-config.md` § Glossary.
6. Report the handoff.

### Written proposal format (Mode 1)

```markdown
## Proposed customer docs — Release Ops

**Mode:** full pass
**Routes scanned:** apps/web/app/ ({N} pages)

### Planned pages

| Doc path | Title | Source (route · component) | Priority | Type |
| -------- | ----- | -------------------------- | -------- | ---- |
| guide/first-steps.md | First steps | — | P0 | getting started |
| guide/integrations.md | Integrations | `/integrations` · components/integrations/ | P0 | page doc |
| guide/ticket-projects.md | Ticket projects | `/ticket-projects` · components/ticket-projects/ | P0 | page doc |
| concepts/release-detection.md | How release detection works | docs/specs.html | P1 | concept |
| faq.md | FAQ | — | P2 | FAQ |

**Sidebar changes:** apps/docs/astro.config.mjs — add {N} entries under "User guide" / "Concepts"
**Glossary additions:** {internal term → customer term, or "none"}
**Open questions:** {flows that could not be verified read-only — or "none"}

---
**Approve to write these docs** (or tell me what to change).
```

---

## Mode 2 — Single page

Same flow as Mode 1, scoped to one route or feature (use the route → doc map in `docs-config.md`).

- Phase 1 may be a **short outline**: purpose, key actions, fields, related pages, open questions.
- Still end with the approval ask — no writes before approval.

---

## Mode 3 — Update / drift check

### Phase 1 — Diff + proposal (no writes)

1. Read `docs-config.md` and `coverage.md`.
2. Diff `apps/web/app/**/page.tsx` against coverage:
   - **New pages** — routes with no doc row (missing)
   - **Removed pages** — doc rows whose route no longer exists (stale)
   - **Changed pages** — `git log --oneline <last-synced SHA>..HEAD -- apps/web/app/<route> apps/web/components/<feature> apps/web/messages/en.json`; read the diff read-only
3. Also flag doc text that contradicts the current spec (for example a ticket integration kind or setting that was renamed).
4. Present the drift proposal and **stop**.

### Phase 2 — Execute (after approval)

Update or create pages, adjust the sidebar, run `npm run docs:build`, refresh `coverage.md` SHAs. Removed pages: delete the doc and its sidebar entry unless the user asks to keep a redirect note.

### Drift proposal format (Mode 3)

```markdown
## Docs drift report — {date}

| Route | Doc path | Status | Action |
| ----- | -------- | ------ | ------ |
| `/ticket-projects` | guide/ticket-projects.md | changed (abc1234 → def5678) | update |
| `/poll-runs` | — | missing | create |

**Open questions:** {any — or "none"}

---
**Approve to apply these doc changes** (or tell me what to change).
```

---

## Coverage inventory format

File: `.agents/project/customer-docs/coverage.md`

```markdown
# Docs coverage — Release Ops

| Route | Doc path | Status | Last-synced SHA | Notes |
| ----- | -------- | ------ | --------------- | ----- |
| `/repos` | apps/docs/src/content/docs/guide/repos.md | current | abc1234 | |
```

**Status values:** `current` · `stale` · `missing`. Set the SHA to the commit at which the doc was last checked against the route source.

---

## Content quality (summary)

Full rules: [style-guide.md](style-guide.md).

- Task-oriented: every page doc answers **what can I do here and how**.
- Second person, present tense, imperative steps; short sentences; English only.
- Page structure: purpose → prerequisites → steps per key action → field table → troubleshooting → related pages.
- First steps: sign in → first ticket created from a release in ≤10 steps; link to page docs for detail.
- Ticket integrations are **Kaneo, Jira and Linear**; release sources are **GitHub, GitLab, Gitea, Forgejo and Codeberg**. Do not mention removed integration kinds except in the FAQ upgrade entry pointing to `README.md`.
- No internal jargon (glossary in `docs-config.md`); ask the user when a mapping is unclear.
- Never invent features, labels or behaviour — unverified flows go under **Open questions**.
- Never include secrets, real tokens, internal URLs or API routes meant only for integrators.

---

## Commits

Follow `.claude/rules/01-git-workflow.md` and the commit-linking rule (rule 07):

- Subject: `docs(docs)[#N]: <summary>` when the work belongs to a Kaneo task. `#N` is the GitHub issue that mirrors the task, resolved **read-only**: the user gives `#N`; or `externalLinks[].externalId` if the Kaneo payload ever carries it; otherwise `mcp__github__search_issues` (owner `mdg-labs`, repo `release-ops`, query = exact task title) → exact-title match.
- Body: task commits with `[#N]` **must** end with the trailer `fixes #N` — the only way the issue closes (GitHub closes it when the commit lands on `main`; the Kaneo ↔ GitHub sync then sets `done`).
- Roadmap-only work with no GitHub issue: `docs(docs)[E<x>-<y>]: <summary>`, no trailer. Neither available: ask the user.
- Never the Kaneo CUID or `RO-<n>` in a commit. Never write to GitHub issues (the trailer is not an issue write).
- Stage explicit paths only (no `git add .` / `-A`). Never push unless the user asks.
- Gate: docs-only changes skip `npm test`/`npm run lint`; run `npm run docs:build` instead.

---

## Parent agents: do not take over customer-docs

If customer-docs runs as a sub-agent, the parent must not write doc files while it may still be running. Wait for its completion notification; to replace it, stop it first (`TaskStop`), check what it wrote, then re-dispatch.

---

## Workflow checklist

```text
Phase 1 (no writes):
- [ ] Read docs-config.md, style-guide.md, coverage.md (if present)
- [ ] Read-only inventory: apps/web/app/, app-sidebar.tsx, components/<feature>/, messages/en.json
- [ ] Read matching docs/specs.html sections
- [ ] Written proposal posted (table or outline)
- [ ] User explicitly approved in chat

Phase 2 (after approval):
- [ ] Write pages from templates (Starlight frontmatter, no body H1)
- [ ] Sidebar entries in apps/docs/astro.config.mjs
- [ ] npm run docs:build passes
- [ ] coverage.md updated (route, path, SHA, status)
- [ ] Glossary in docs-config.md updated
- [ ] Handoff report
```

---

## Handoff template

```markdown
## Customer docs — {MODE}

### Written
- {doc paths}
- Sidebar: apps/docs/astro.config.mjs — {summary}
- Build: npm run docs:build — {pass/fail}

### Coverage
- {N} current · {N} updated · {N} new

### Open questions
- {items needing product input — or "none"}

### Suggested review order
1. guide/first-steps.md
2. Integrations, Ticket projects, Repos
3. Remaining pages

### Next
- "update docs" — drift check (Mode 3)
- "document {page}" — single page (Mode 2)
```

---

## Forbidden

- Any doc, sidebar, `docs/` source, coverage or glossary write before explicit approval of the written proposal
- Skipping the written proposal, or moving to Phase 2 in the same turn as Phase 1
- Hand-editing synced files in `apps/docs/src/content/docs/` (edit `docs/` sources instead)
- Modifying application code (`apps/web`, `cmd/`, `internal/`, `db/`, `migrations/`) — read-only
- Fabricating behaviour, UI labels or screenshots; capturing screenshots
- Exposing internal table/column names, code identifiers, env var names (outside the install guide), secrets or internal URLs
- `git add .`, push without explicit request, a `[#N]` commit without the `fixes #N` trailer, any GitHub issue write
