---
name: project-setup
description: >-
  Bootstrap or update MDG Labs Cursor project agent config: Phasical orchestrator
  supporting files (project.config, doc-index, prompt-templates, skills-lock)
  under .agents/project/, Cursor rules (numbered .mdc set from central templates),
  and central skill installs. Supports create and update modes per layer. Use when
  onboarding a repo, changing Phasical IDs/branches, refreshing shared rules from
  mdg-labs/skills, or migrating from per-project orchestrator/Linear/GitLab board skills.
---

# Project setup

Bootstrap **or update** consumer-project agent configuration. Three independent **layers** — run one or all:

| Layer | What it manages | Manifest |
| ----- | --------------- | -------- |
| **phasical** | `.agents/project/orchestrator/*`, `workspace-notes.md`, `agent-memory/`, `skills-lock.json` | `skills-lock.json` |
| **rules** | `.cursor/rules/*.mdc` (numbered MDG Labs convention) | `rules-manifest.json` |
| **skills** | Installed skills under `.agents/skills/` from `mdg-labs/skills` | `skills-lock.json` |

**Templates:** [templates/](templates/) (phasical + rules).  
**Repo guide:** [../README.md](../README.md).

This skill is **not** the orchestrator, intake, or triage runtime — it only materializes and updates local config.

## Path layout (always)

| What | Path |
| ---- | ---- |
| Installed skills (`SKILL.md`, references) | `.agents/skills/<skill-name>/` — **managed by `npx skills`; wiped on update** |
| Supporting files (project.config, doc-index, prompt-templates) | `.agents/project/orchestrator/` |
| Workspace notes, agent-memory, intake plans | `.agents/project/` |
| Cursor rules | `.cursor/rules/*.mdc` |

`npx skills` installs to `.agents/skills/`. **Never** put supporting files there — `npx skills update` deletes them. **Never** write under `.cursor/` for skills/config.

---

## Modes

Parse user intent on first turn:

| Mode | User says | Action |
| ---- | --------- | ------ |
| **create** | "set up", "bootstrap", "initialize" | Write missing files; install skills |
| **update** | "update", "refresh", "change Phasical project", "sync rules" | Merge/sync per layer rules below |
| **layer:phasical** | "update Phasical config", "change projectId" | Phasical layer only |
| **layer:kaneo** | *(legacy alias)* same as `layer:phasical` | Phasical layer only |
| **layer:rules** | "update rules", "refresh cursor rules" | Rules layer only |
| **layer:skills** | "update skills", "reinstall skills" | Run `npx skills update -p` |
| **full** | "full project setup" | All layers, create or update as needed |

Default: **update** if manifests or config already exist; **create** if greenfield.

---

## Update semantics (critical)

Setup skills are **idempotent operators**, not one-shot installers.

### Before any write

1. Detect existing files (manifests + targets).
2. Show a **change plan** — what will be created, updated, merged, or skipped.
3. For **update**, never blind-overwrite without user confirmation on touched files.

### Merge policy by file type

| File | Create | Update |
| ---- | ------ | ------ |
| **Shared rules** (`managed: shared` in manifest) | Copy from central template | Overwrite from central **only if** user confirms refresh OR file matches prior manifest hash |
| **Generated rules** (`managed: generated`) | Render template with placeholders | Re-render **only fields user asked to change**; preserve other sections |
| **project.config.md** | Render template | Patch named fields (Phasical IDs, branches, paths); keep comments |
| **doc-index.md** | Render template | **Merge** — never delete user-added rows |
| **prompt-templates.md** | Render template | **Merge** — preserve project CI commands unless user asks full refresh |
| **workspace-notes.md** | Create stub if missing | **Never overwrite** body |
| **slack-session-end.md** | Optional create | Patch operator fields only |
| **skills-lock.json** / **rules-manifest.json** | Write from examples | Bump versions/hashes; preserve custom entries |
| **learned/*.mdc** | — | **Never touch** (envhub self-learning harvests) |
| **Stack glob rules** (prisma.mdc, backend.mdc) | Add if profile selected | Only add missing; never delete user rules |

### After update

Report: files created · updated · skipped · manual follow-ups.

---

## Layer: phasical

### Targets

```
repo-root/skills-lock.json
.agents/project/orchestrator/project.config.md
.agents/project/orchestrator/doc-index.md
.agents/project/orchestrator/prompt-templates.md
.agents/project/orchestrator/slack-session-end.md   # optional
.agents/project/workspace-notes.md
.agents/project/agent-memory/active/.gitkeep
.agents/project/agent-memory/archive/.gitkeep
```

Templates: [templates/phasical/](templates/phasical/).

### Gather inputs

| Field | Used in |
| ----- | ------- |
| Project name | project.config, 00-project rule |
| Repo path | project.config |
| GitHub owner/repo | project.config |
| Phasical workspaceId, projectId, display name | project.config |
| Integration / production branch | project.config, 01-git-workflow |
| Plan file, spec glob, primary spec | project.config, doc-index |
| Worktree pattern | project.config |
| Domain labels, area prefixes | project.config, intake |
| Scoped CI / full CI / migration CLI | prompt-templates, 06-local-ci rule |
| Slack session-end? | slack-session-end.md |
| Allowed commit scopes | project.config, 01-git-workflow |

If Phasical project missing, offer `user-phasical` `create_project` (user confirms).

### Migration from legacy layout

If old files exist:

| Legacy | Action |
| ------ | ------ |
| `orchestrator/phasical-board.md` | Extract constants → `project.config.md` |
| `.agents/skills/orchestrator/project.config.md` (and siblings) | **Move** → `.agents/project/orchestrator/` — old path is wiped by `npx skills update` |
| `.cursor/skills/**` supporting files | Move → `.agents/project/` |
| `orchestrator/SKILL.md` (local copy under `.cursor/` or repo root) | Remove after `skills` layer install |
| `linear-*`, `gitlab-*`, `github-intake` skills | List for removal after Phasical migration |
| `12-linear-board.mdc`, `12-github-project-board.mdc` | Deprecate — remove on rules refresh |

### .gitignore snippet

Append [templates/phasical/gitignore-snippet.txt](templates/phasical/gitignore-snippet.txt) if missing.

### Customizing `prompt-templates.md`

Rendered to `.agents/project/orchestrator/prompt-templates.md`. Orchestrator copies blocks **verbatim** into sub-agent prompts.

| Placeholder | Fill from |
| ----------- | --------- |
| `{PROJECT_ID}` | Phasical projectId |
| `{INTEGRATION_BRANCH}` | project.config integration branch |
| `{ALLOWED_SCOPES}` | Gathered commit scopes (comma-separated) |
| `{EXAMPLE_SCOPE}` | Primary scope for examples |
| `{SCOPED_CI_CMD}` | Project scoped CI one-liner |
| `{MIGRATION_CMD}` | Prisma/drizzle/etc. migration CLI |

**Never remove or shorten:**

- **PHASICAL SYNC — EXECUTION** / **VERIFIER** — STATUS SYNC TABLE + gate sections
- **PHASICAL COMMENT CONTRACT** — verifier comment timing + PASS/FAIL templates
- **COMMIT CONTRACT — EXECUTION** — subject format, staging rules, handoff order
- **Pre-dispatch gate** — required marker table

**On update:** merge user CI command tweaks; preserve custom examples. Full refresh only when user confirms — PHASICAL/COMMIT blocks must stay intact.

**Validate after render:** execution template has PHASICAL SYNC + COMMIT CONTRACT; verifier template has PHASICAL SYNC + PHASICAL COMMENT CONTRACT + SCOPED CI GATE. Rule `09-sub-agent-prompt-contract.mdc` installed.

---

## Layer: rules

### Numbering convention (MDG Labs)

| Slot | Rule | Source |
| ---- | ---- | ------ |
| `00-project` | Identity, stack, doc precedence | generated |
| `01-git-workflow` | Branches, scopes, never-push | generated |
| `02-orchestrator` | Pointer to orchestrator skill | shared |
| `06-local-ci-before-commit` | Scoped vs full CI gate | generated |
| `07-phasical-commit-linking` | `[#N]` commit subjects | shared |
| `08-dependabot-alerts` | Never dismiss | shared |
| `09-sub-agent-prompt-contract` | Verbatim sub-agent prompt blocks | shared |
| `13-no-amend-pushed` | No amend after push | shared |

Optional **stack profile** adds glob rules (not in default manifest — user/project adds):

| Profile | Extra rules |
| ------- | ----------- |
| `nestjs-prisma` | `prisma.mdc`, `backend.mdc` |
| `nextjs` | `web.mdc`, `i18n.mdc` |
| `drizzle-monorepo` | `db.mdc` (project-authored) |

Templates: [templates/rules/shared/](templates/rules/shared/) · [templates/rules/generated/](templates/rules/generated/).

### rules-manifest.json

Write or update at repo root from [rules-manifest.example.json](rules-manifest.example.json).

- **shared** rules: byte-copy from central repo templates (path in manifest)
- **generated** rules: render placeholders from phasical gather inputs (same session)

### Update rules workflow

```text
Rules update progress:
- [ ] Read rules-manifest.json (or create from example)
- [ ] For each shared rule: diff installPath vs central template
- [ ] Show diff summary; confirm overwrite for changed shared rules
- [ ] For each generated rule: ask which fields changed; re-render those only
- [ ] Skip learned/, user-authored glob rules, deprecated board rules unless user asks removal
- [ ] Update computedHash per rule in manifest
- [ ] Report handoff
```

### Deprecated rules to remove on request

- `07-issue-commit-linking.mdc` (Linear) → replaced by `07-phasical-commit-linking.mdc`
- `12-linear-board.mdc`, `12-github-project-board.mdc` → board logic in skills

---

## Layer: skills

Install or update central skills from `skills-lock.json`.

### Create

```bash
npx skills experimental_install -a cursor -y
```

### Update

```bash
npx skills update -p -y
```

Default skills (in [templates/phasical/skills-lock.json](templates/phasical/skills-lock.json)):

- orchestrator
- phasical-intake
- phasical-triage
- dependabot-triage
- customer-docs

**Do not** install `project-setup` into consumer projects — use globally (`-g`) or invoke from the skills repo checkout.

Verify: `npx skills list -p`

---

## Full bootstrap workflow

```text
1. Pick mode (create | update) and layers (phasical | rules | skills | full)
2. Gather inputs (minimal set for selected layers)
3. Detect existing files → change plan → user confirms if overwrites
4. phasical layer → write/merge supporting files under `.agents/skills/`
5. rules layer → copy shared + render generated → write rules-manifest.json
6. skills layer → experimental_install or update
7. .gitignore snippet
8. Handoff summary
```

---

## Handoff template

```markdown
## Project setup — {MODE} ({LAYERS})

**Phasical config:** `.agents/project/orchestrator/project.config.md`
**Rules:** {N} managed rules · manifest `rules-manifest.json`
**Skills:** orchestrator, phasical-intake, phasical-triage, dependabot-triage, customer-docs

### Changed
- created: …
- updated: …
- skipped: …

### Next
- Fill `doc-index.md` if stub
- Customize `prompt-templates.md` CI filters
- Commit manifests + `.agents/skills/` supporting files (not `agent-memory/active/*`)
- Remove legacy local SKILL.md copies if migrating

### Try
- "project-setup update rules" — refresh shared rules
- "project-setup layer:phasical — change integration branch to dev"
- "orchestrate #N"
```

---

## Forbidden

- Running orchestrator, intake, triage, or creating Phasical tasks during setup
- Blind-overwriting `workspace-notes.md`, `learned/`, or `doc-index.md` user content
- Embedding project-specific IDs in the central `mdg-labs/skills` repo
- Editing installed skill copies in `.agents/skills/orchestrator/SKILL.md` — change upstream + `npx skills update`
- Writing supporting files under `.agents/skills/` — always `.agents/project/` (skills update wipes skill dirs)
- Writing supporting files under `.cursor/`
- Removing user-authored `.mdc` files without explicit confirmation
