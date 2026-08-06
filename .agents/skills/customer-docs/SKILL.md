---
name: customer-docs
description: >-
  Autonomously write and maintain end-user customer documentation for an app:
  one doc page per in-app page, plus Getting Started, concept pages, and FAQ.
  English only. Bootstraps per-project config on first run. Use when the user asks
  to write customer docs, document page X, create a getting started guide, update
  user documentation, check docs coverage, or run a full docs pass on the app.
---

# Customer docs

Write and maintain **end-user / customer documentation** for an application: one doc page per in-app page, plus Getting Started, concept pages, and FAQ. Language: **English only** (baseline).

This skill is **fully generic** — all project-specific facts live in a local config bootstrapped on first run (same pattern as project-setup).

**Style rules:** [style-guide.md](style-guide.md) — single source of truth for voice, structure, and terminology.

## Path layout

| What | Path |
| ---- | ---- |
| This skill (installed) | `.agents/skills/customer-docs/SKILL.md` |
| Templates (installed) | `.agents/skills/customer-docs/*-template.md`, `style-guide.md` |
| Project config (supporting — **NEVER** under `.agents/skills/`) | `.agents/project/customer-docs/docs.config.md` |
| Coverage inventory (supporting) | `.agents/project/customer-docs/coverage.md` |
| Project glossary (supporting) | `.agents/project/customer-docs/glossary.md` |
| Orchestrator constants (optional) | `.agents/project/orchestrator/project.config.md` |
| Spec doc index (optional) | `.agents/project/orchestrator/doc-index.md` |
| Sub-agent monitoring (installed) | `.agents/skills/orchestrator/references/sub-agent-monitoring.md` |

`npx skills` installs to `.agents/skills/`. **`npx skills update` replaces each skill directory wholesale** — local files there are deleted. Supporting files (config, coverage, glossary) **must** live under **`.agents/project/customer-docs/`**. **Never** write supporting files under `.agents/skills/` or `.cursor/`.

---

## Mode detection

Parse user intent on first turn:

| Mode | User says | Action |
| ---- | --------- | ------ |
| **0 — Bootstrap** | First run, or `docs.config.md` missing | Interactive setup — no doc writes until config exists |
| **1 — Full pass** | "write all docs", "document the app", "full docs pass" | Inventory → proposal → write batch-by-batch |
| **2 — Single page** | "document the settings page", "write docs for /billing" | Scoped inventory → outline → write one page |
| **3 — Update / drift** | "update docs", "check docs coverage", "sync docs" | Diff routes vs coverage → propose → execute |

Default: **Mode 0** if `.agents/project/customer-docs/docs.config.md` is missing; otherwise infer from user intent (Mode 1 if ambiguous).

---

## Mode 0 — Bootstrap (first run, config missing)

If `.agents/project/customer-docs/docs.config.md` does **not** exist: **do NOT write any customer docs.** Run interactive setup analogous to project-setup.

### Gather inputs

| Step | Topic | Questions |
| ---- | ----- | --------- |
| 1 | **Repo detection** | Are docs in the same repo (`docs/`, `apps/docs/`, `website/`) or a second repo in the workspace? Scan workspace directories, propose findings, user confirms. |
| 2 | **Publishing setup** | Framework (Docusaurus, Mintlify, Nextra, VitePress, Starlight, plain Markdown, other), deploy mechanism (CI, manual), published base URL. |
| 3 | **App inventory** | Where does app code live (routes/pages dir, e.g. `app/`, `src/pages/`, `src/routes/`)? Which apps in the monorepo are end-user facing? |
| 4 | **Audience + product** | Technical vs non-technical users, product name, core use case, existing spec docs (read `.agents/project/orchestrator/doc-index.md` if present). |
| 5 | **Docs structure** | Folder layout, sidebar/nav file, naming (kebab-case), framework frontmatter schema. |
| 6 | **Screenshot policy** | Insert placeholders like `{SCREENSHOT: description}` vs no images — this skill **never** takes screenshots itself. |
| 7 | **Phasical integration** (optional) | If `.agents/project/orchestrator/project.config.md` exists, adopt Phasical constants; docs commits follow `07-phasical-commit-linking` (`[#N]` when a task exists; `docs(scope)` without a task key only if user opts out of Phasical sync). |

### Bootstrap workflow

```text
Bootstrap progress:
- [ ] Scan workspace for docs repo/path and app routes dirs
- [ ] Gather answers (steps 1–7)
- [ ] Show change plan — what will be created under .agents/project/customer-docs/
- [ ] User confirms plan (project-setup update semantics)
- [ ] Render docs.config.md from docs-config-template.md
- [ ] Create coverage.md stub (table headers only)
- [ ] Create glossary.md stub
- [ ] Report handoff — offer Mode 1/2/3 or end run
```

**Merge policy:** On re-bootstrap (config exists but user asks to refresh), show change plan and patch named fields only — never blind-overwrite glossary terms or coverage rows without confirmation.

---

## Approval gate (mandatory — no exceptions)

**Phase 1 — investigate + written proposal only.** No doc file writes (no page markdown, no nav/sidebar edits, no coverage updates).

**Phase 2 — file writes** only after the user explicitly approves the proposal in chat (e.g. **approve**, **yes write them**, **go ahead**, **LGTM**).

| Rule | Detail |
| ---- | ------ |
| **Always propose first** | Full pass, single page, drift check — every run that writes docs |
| **Never skip Phase 1** | Even if the user said "write all docs" or "document the settings page" upfront |
| **Wait for reply** | End Phase 1 with: *"Approve to write these docs (or tell me what to change)."* |
| **Re-propose after edits** | User requests changes → updated proposal; no writes until they approve again |
| **Sub-agent same rule** | Parent agents must not write docs while customer-docs waits for approval |

**Forbidden before approval:** any customer doc file, nav/sidebar file, `coverage.md`, or `glossary.md` writes (bootstrap config stubs are Mode 0 only, after user confirms the change plan).

Mode 0 bootstrap: no doc writes at all until config exists. Config file creation follows project-setup change-plan confirmation.

---

## Mode 1 — Full docs pass

### Phase 1 — Inventory + proposal (no writes)

1. Read `docs.config.md`, `coverage.md`, `glossary.md`, [style-guide.md](style-guide.md).
2. Inventory app routes/pages from code (**read-only**): route files, navigation, page titles, visible UI actions (buttons, forms, dialogs), roles/permissions where detectable.
3. Read spec docs per `doc-index.md` when present — do not invent behaviour.
4. **Present written proposal** (format below).
5. **Stop and wait** for explicit user approval.

### Phase 2 — Write (after approval only)

1. Write docs batch-by-batch using [page-doc-template.md](page-doc-template.md) and [getting-started-template.md](getting-started-template.md).
2. Update nav/sidebar file per `docs.config.md`.
3. Update `coverage.md` — route → doc path → last-synced commit SHA → status.
4. Maintain `glossary.md` for product terms (internal → customer-facing).
5. Report handoff.

### Written proposal format (Mode 1)

```markdown
## Proposed customer docs — {product name}

**Mode:** full pass
**Config:** `.agents/project/customer-docs/docs.config.md`
**Routes scanned:** {dirs}

### Planned pages

| Doc path | Page title | Source (route/component) | Priority | Type |
| -------- | ---------- | ------------------------ | -------- | ---- |
| docs/getting-started.md | Getting started | — | P0 | getting started |
| docs/settings/profile.md | Profile settings | `/settings/profile` · `ProfilePage.tsx` | P1 | page doc |
| docs/concepts/workspaces.md | Workspaces | concept | P2 | concept |

**Nav changes:** {sidebar file} — add {N} entries under {section}
**Glossary additions:** {term → customer term, or "none"}
**Open questions:** {flows that could not be verified read-only — or "none"}

---
**Approve to write these docs** (or tell me what to change).
```

---

## Mode 2 — Single page

Same flow as Mode 1, scoped to one route/page or named feature.

- Phase 1 proposal may be a **short outline** (purpose, key actions, related pages) instead of a full table.
- Still end with the approval ask — **no file writes before approval**.

---

## Mode 3 — Update / drift check

### Phase 1 — Diff + proposal (no writes)

1. Read `docs.config.md` and `coverage.md`.
2. Diff current routes against coverage:
   - **New pages** — routes with no doc row (missing)
   - **Removed pages** — doc rows whose route no longer exists (stale)
   - **Changed pages** — compare `last-synced SHA` vs current; read the diff **read-only**
3. **Present proposal:** create / update / archive per page.
4. **Stop and wait** for approval.

### Phase 2 — Execute (after approval)

Update or create docs, adjust nav, refresh `coverage.md` SHAs, archive or remove stale pages per user preference in config.

### Drift proposal format (Mode 3)

```markdown
## Docs drift report — {date}

| Route | Doc path | Status | Action |
| ----- | -------- | ------ | ------ |
| `/settings/billing` | docs/settings/billing.md | changed (abc123 → def456) | update |
| `/reports` | — | missing | create |
| `/legacy/foo` | docs/legacy/foo.md | route removed | archive |

**Open questions:** {any — or "none"}

---
**Approve to apply these doc changes** (or tell me what to change).
```

---

## Coverage inventory format

File: `.agents/project/customer-docs/coverage.md`

```markdown
# Docs coverage

| Route | Doc path | Status | Last-synced SHA | Notes |
| ----- | -------- | ------ | --------------- | ----- |
| `/settings/profile` | docs/settings/profile.md | current | abc1234 | |
```

**Status values:** `current` · `stale` · `missing`

Update `last-synced SHA` to the commit at which the doc was last verified against the route source.

---

## Content quality (summary)

Full rules: [style-guide.md](style-guide.md). Enforce on every write.

- Task-oriented: every page doc answers **what can I do here and how** — not what components exist.
- Second person ("you"), present tense, imperative steps.
- Page structure: purpose → prerequisites → steps per key action → field/option table → troubleshooting → related links.
- Getting started: happy path signup/login → first success in ≤10 steps; link to page docs for detail.
- No internal jargon — map terms via `glossary.md`; **ask the user** when mapping is unclear (never guess).
- Never invent features or behaviour not found in code or spec docs — list unverified flows under **Open questions**.
- Never include secrets, internal URLs, or admin-only endpoints.
- One term per concept across all pages (glossary-enforced).
- Short sentences; no marketing fluff; English only.

---

## Commits

Follow project git rules (`.cursor/rules/01-git-workflow.mdc`, `07-phasical-commit-linking.mdc` when Phasical is enabled):

- Explicit path staging only — **no** `git add .`
- Subject: `[#N] docs(scope): …` when a Phasical task exists; `docs(scope): …` only if user opted out of Phasical sync in config
- Never push unless the user asks

---

## Parent agents: do not take over customer-docs

If **customer-docs** runs as a sub-agent (Task), the parent must not write doc files while it may still be running. Follow `.agents/skills/orchestrator/references/sub-agent-monitoring.md` (transcript two-sample → terminate → dedupe → re-dispatch).

---

## Workflow checklist

```text
Phase 1 (no doc writes):
- [ ] docs.config.md exists (else Mode 0 bootstrap)
- [ ] Read config, coverage, glossary, style-guide.md
- [ ] Read-only route/UI inventory from app code
- [ ] Read spec docs via doc-index.md when present
- [ ] Written proposal posted (table or outline)
- [ ] User explicitly approved proposal in chat

Phase 2 (after approval):
- [ ] Write pages from templates — style-guide.md
- [ ] Update nav/sidebar per config
- [ ] Update coverage.md (route, path, SHA, status)
- [ ] Update glossary.md for new terms
- [ ] Report handoff
```

---

## Handoff template

```markdown
## Customer docs — {MODE}

**Config:** `.agents/project/customer-docs/docs.config.md`
**Coverage:** `.agents/project/customer-docs/coverage.md`

### Written
- {list of doc paths}
- Nav: {sidebar file} — {summary of changes}

### Coverage
- {N} current · {N} updated · {N} new

### Open questions
- {items needing product input — or "none"}

### Suggested review order
1. Getting started
2. {highest-traffic or P0 pages}
3. {remaining pages}

### Next
- "update docs" — drift check (Mode 3)
- "document {page}" — single page (Mode 2)
```

---

## Forbidden

- **Any customer doc or nav file write before explicit user approval** of the written proposal (no exceptions — same gate as phasical-intake)
- **Skipping the written proposal** — including single-page and drift-check runs
- Proceeding to Phase 2 in the same turn as Phase 1 without user reply
- **Modifying application source code** — read-only on app code; only docs files, nav files, and supporting files under `.agents/project/customer-docs/`
- Writing supporting files under `.agents/skills/` or `.cursor/`
- **Fabricating** behaviour, screenshots, or UI text
- Taking screenshots — use `{SCREENSHOT: description}` placeholders only when policy allows
- Exposing internal entity names, DB terms, code identifiers, secrets, or admin-only URLs in customer docs
- `git add .` or push without explicit user request
- Parent agent takeover while customer-docs sub-agent is in flight
