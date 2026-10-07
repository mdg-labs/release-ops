---
name: dependabot-triage
description: >-
  Read a Dependabot security alert for mdg-labs/release-ops (GitHub MCP when available,
  otherwise gh CLI), search the Kaneo Release Ops board for an open duplicate by package,
  GHSA or CVE, and create a Kaneo task in `ready` (label `deps`, else `ci`, else none)
  when no open duplicate exists. Read-only on GitHub: never dismisses alerts and never
  touches GitHub issues. Use when the user references a Dependabot alert number, a
  CVE/GHSA ID, a security alert URL, or asks to triage a dependency vulnerability.
---

# Dependabot triage

Read one Dependabot alert from `mdg-labs/release-ops`, check the Kaneo board for an open duplicate, and create a Kaneo task in `ready` if there is none. Triage only: no code changes, no dependency bumps, no commits. The fix is implemented later as a normal task (`/orchestrate RO-<n>`).

| What | Path (repo root relative) |
| ---- | ---- |
| This skill | `.claude/skills/dependabot-triage/SKILL.md` |
| Dependabot policy (binding) | `.claude/rules/08-dependabot-alerts.md` |
| Commit linking | rule 07 in `.claude/rules/` (GitHub `[#N]` commit linking) |
| Task description conventions | `.claude/skills/kaneo-intake/SKILL.md` |
| Implementation (consumes `ready` tasks) | `.claude/skills/orchestrate/SKILL.md` |

If this skill runs as a sub-agent, the parent waits for its completion notification and must not call `create_task` for the same alert while this skill may still be running.

## Constants

```text
GitHub repo:   mdg-labs/release-ops   (owner mdg-labs, repo release-ops)
Kaneo MCP:     Kaneo   (tools mcp__Kaneo__<tool>)
workspaceId:   ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj   (MDG-Labs)
projectId:     z4janvyjsbbb0esishvd9gb8           (Release Ops, ticket key RO)
Task URL:      https://cloud.kaneo.app/dashboard/workspace/ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj/project/z4janvyjsbbb0esishvd9gb8/task/<taskCuid>
Create status: ready
```

## When to use

| User input | Action |
| ---------- | ------ |
| Alert number (`alert 2`, `dependabot #2`) | Fetch → duplicate search → create |
| CVE or GHSA ID | List open alerts, filter by ID → duplicate search → create |
| Alert URL (`github.com/mdg-labs/release-ops/security/dependabot/<N>`) | Extract `<N>` → fetch → duplicate search → create |
| "Is there a ticket for the `ws` vulnerability?" | Duplicate search only, no create |

## Hard rules

1. **GitHub is read-only.** Read Dependabot alerts and, for `#N` resolution, issues. Nothing else.
2. **Never dismiss an alert** (UI, `gh api -X PATCH … state=dismissed`, or any MCP tool) — rule `08-dependabot-alerts.md`. The alert closes on its own once the bump reaches the default branch.
3. **Never touch GitHub issues**: no create, comment, label, assign, edit, close or reopen. Kaneo is the only place this skill writes.
4. **No code changes** during triage: no `package.json`, lockfile, `go.mod`, `Dockerfile` or workflow edits, no commits.
5. **Stop at `ready`.** Never set `in-progress`, `in-review`, `implemented` or `done` (`done` comes from the Kaneo ↔ GitHub sync when the `fixes #N` commit lands on `main`, or from the user).
6. Never put the Kaneo task CUID or `RO-<n>` in a commit message.

## Alert access — MCP vs gh CLI

Check what the session offers before fetching, and prefer MCP.

| Option | Tools / command | Use when |
| ------ | --------------- | -------- |
| **1. GitHub MCP (preferred)** | `mcp__github__get_dependabot_alert` (`owner`, `repo`, `alertNumber`), `mcp__github__list_dependabot_alerts` (`owner`, `repo`, `state`, `severity`) | These tools are listed in the session (search deferred tools for `dependabot`). They belong to the GitHub MCP server's `dependabot` toolset, which is not always enabled. |
| **2. gh CLI (fallback)** | `gh api repos/mdg-labs/release-ops/dependabot/alerts/<N>` · `gh api 'repos/mdg-labs/release-ops/dependabot/alerts?state=open&per_page=100'` | MCP Dependabot tools are absent. Check `gh auth status` first; the token needs Dependabot alerts read access (`security_events` / "Dependabot alerts: read"). |
| **3. Ask the user** | — | Neither works (no MCP tool, `gh` missing or unauthenticated). Ask for the alert details (package, ecosystem, manifest, GHSA/CVE, severity, patched version, URL). Do not guess. |

Only GET requests. Never use `gh api -X PATCH`, `-X POST` or `-f state=…` against alert or issue endpoints.

## Release-ops ecosystems

Record which manifest the alert points at (`dependency.manifest_path`) and map it:

| Ecosystem | Manifest(s) | Resolution path (for the task description) | Gate after the bump |
| --------- | ----------- | ------------------------------------------ | ------------------- |
| npm | `package.json` (root, npm workspaces `apps/web`, `apps/docs`); single lockfile `package-lock.json` at repo root (no per-workspace lockfiles) | Bump in the owning `package.json` (`npm install <pkg>@<ver> -w apps/web` / `-w apps/docs`, or root); transitive deps via `overrides` in root `package.json`; regenerate root `package-lock.json` | `npm test && npm run lint && npm run db:check` |
| Go modules | `go.mod` / `go.sum` (module `github.com/mdg-labs/release-ops`) | `go get <module>@<ver> && go mod tidy` | `go test ./... && golangci-lint run` |
| Docker | `Dockerfile` base images (`node:22-bookworm-slim`, `golang:1.25-bookworm`) | Bump the image tag in every affected `FROM` stage | `docker build .` locally if possible, else note for CI |
| GitHub Actions | `.github/workflows/*.yml` (`actions/*`, `docker/*`, `golangci/golangci-lint-action`) | Bump the `uses:` ref in every workflow that references it | CI on `dev` |

`apps/docs` is the Starlight docs site; a vulnerability there does not ship in the product image but still gets fixed. Mention that in the description.

## Workflow

```text
- [ ] Phase 1: Fetch alert (MCP, else gh CLI, else ask)
- [ ] Phase 2: Kaneo duplicate search (package, GHSA, CVE)
- [ ] Phase 3: Create Kaneo task in ready + label (only if no open duplicate)
- [ ] Phase 4: Read-only GitHub #N lookup, then summary in chat
```

### Phase 1 — Fetch the alert

By number: `mcp__github__get_dependabot_alert` (owner `mdg-labs`, repo `release-ops`, `alertNumber`) or `gh api repos/mdg-labs/release-ops/dependabot/alerts/<N>`.

By CVE/GHSA: list open alerts and filter on `security_advisory.cve_id` / `security_advisory.ghsa_id`. Several alerts for one advisory (for example the same npm package in two workspaces) become **one** task listing every alert number and manifest.

Record:

| Field | Source |
| ----- | ------ |
| Alert number, URL, state | `number`, `html_url`, `state` |
| Package, ecosystem | `dependency.package.name`, `dependency.package.ecosystem` |
| Manifest, scope | `dependency.manifest_path`, `dependency.scope` (runtime / development) |
| GHSA, CVE, summary | `security_advisory.ghsa_id`, `.cve_id`, `.summary` |
| Severity | `security_vulnerability.severity` (or `security_advisory.severity`) |
| Vulnerable range, patched version | `security_vulnerability.vulnerable_version_range`, `.first_patched_version.identifier` |

If the alert is already `fixed` or `dismissed`, report that and stop (no task).

### Phase 2 — Kaneo duplicate search

Search the Release Ops project for each identifier. A match in the title **or** description counts:

```text
mcp__Kaneo__search  q: "<package_name>"  workspaceId: ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj  projectId: z4janvyjsbbb0esishvd9gb8  type: tasks
mcp__Kaneo__search  q: "<GHSA-xxxx-xxxx-xxxx>"  (same scope)
mcp__Kaneo__search  q: "<CVE-YYYY-NNNNN>"       (same scope, when a CVE exists)
```

For each hit, `mcp__Kaneo__get_task` (CUID) to read the status and confirm the package / GHSA / CVE really appears in the title or description (search is fuzzy). If search returns nothing but you suspect it missed matches, page through `mcp__Kaneo__list_tasks` (projectId, `limit: 100`) and scan titles and descriptions.

| Result | Action |
| ------ | ------ |
| Open duplicate (status `backlog`, `ready`, `in-progress`, `in-review` or `implemented`) | Report `RO-<n>` + task URL; do **not** create. If the existing task lacks this alert number, say so in chat; do not edit the task unless the user asks. |
| Only `done` duplicates | Create a new task (regression or incomplete fix); reference the old `RO-<n>` in the description. |
| No duplicate | Phase 3 |

### Phase 3 — Create the Kaneo task

Kaneo has no "Bug" task type: the title prefix and the label carry the meaning.

```text
mcp__Kaneo__create_task
  projectId:   z4janvyjsbbb0esishvd9gb8
  title:       "Security: bump <package> to <patched_version> (<CVE or GHSA>)"
  description: <template below>
  priority:    <severity map below>
  status:      ready
```

| Dependabot severity | Kaneo priority |
| ------------------- | -------------- |
| critical | `urgent` |
| high | `high` |
| medium | `medium` |
| low | `low` |

**Label.** Call `mcp__Kaneo__list_workspace_labels` (workspaceId `ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj`) and pick the first label name that exists: `deps`, else `ci`, else no label. Do not create a new label name. Kaneo stores labels as rows that carry a `taskId`:

- Row with `taskId: null` (workspace-level definition) → `mcp__Kaneo__attach_label_to_task` (`labelId`, `taskId` = new task CUID).
- Only rows already attached to other tasks → `mcp__Kaneo__create_label` with the **same name and color**, `workspaceId`, and `taskId` = new task CUID. Do not re-attach another task's label row (that would move it off that task).

**Description template:**

```markdown
## Report

Dependabot alert #<alert_number> (<severity>): <advisory summary>

## Alert details

- **Package:** `<package_name>` (<ecosystem>)
- **Manifest:** `<manifest_path>` · scope: <runtime | development>
- **Vulnerable range:** `<vulnerable_version_range>`
- **Patched version:** `>= <patched_version>`
- **GHSA:** <ghsa_id> · **CVE:** <cve_id or "none">
- **Alert URL:** <html_url>
- **Other alerts for the same advisory:** <#numbers + manifests, or "none">
- **Previous task:** <RO-<n> if re-opened as regression, else omit>

## Resolution path

<ecosystem row from "Release-ops ecosystems": bump command, files to change>

Land the fix on `dev`. The Dependabot alert closes automatically once the vulnerable version is gone from the dependency graph. Never dismiss the alert (rule 08).

## Acceptance criteria

- [ ] `<package_name>` resolved to `>= <patched_version>` in `<manifest_path>` (and lockfile where applicable)
- [ ] Gate passes: <ecosystem gate command>
- [ ] Commit: `fix(deps)[#N]: bump <package_name> to <patched_version> (<CVE or GHSA>)`, body ending with `fixes #N`

### Files

- `<owning package.json, root package.json for overrides, package-lock.json — or go.mod/go.sum, Dockerfile, the workflow file>`

## Out of scope

- <other vulnerable packages pulled in by the same parent, with their RO-<n>; a major-version migration beyond what the bump needs> | none

## Scope hint

~<N> changed lines (excluding the lockfile) · Expected files: <N> reviewable (`<paths>`)
```

A dependency bump has no `Reachable via` criterion; `/orchestrate`'s readiness gate needs the `### Files` paths, `## Out of scope` and `## Scope hint`. Note in "Out of scope" when another task's bump (a parent package) probably resolves this one, so the orchestrator can bundle them.

### Phase 4 — Read-only `#N` lookup and summary

The fix commit needs the GitHub issue number that mirrors the Kaneo task. Kaneo payloads have so far carried no `externalLinks`; resolve it read-only:

0. Optional: the payload has `externalLinks` → use `externalLinks[].externalId` of the issue link (not observed today).
1. `mcp__github__search_issues` (owner `mdg-labs`, repo `release-ops`, query = the exact task title) → take the exact-title match as `#N`.
2. No match (the mirror may not exist yet) → report "no GitHub mirror found"; the implementer resolves `#N` the same way before committing, or asks the user. Never create the issue yourself.

Report in chat:

- Alert: number, package, ecosystem, manifest, severity, GHSA/CVE, patched version, URL
- Duplicate result: open duplicate `RO-<n>` + URL (no create), or "none"
- Created task: `RO-<n>`, task URL, status `ready`, priority, label used (`deps` / `ci` / none)
- GitHub `#N` if found (read-only lookup), else "not found yet"
- Next step: `/orchestrate RO-<n>`; fix commit `fix(deps)[#N]: bump <pkg> to <ver> (CVE-…)` with body trailer `fixes #N`

## Fix commit convention (for the implementer)

```text
fix(deps)[#N]: bump <pkg> to <ver> (CVE-YYYY-NNNNN)

fixes #N
```

- `#N` = GitHub issue mirroring the Kaneo task (read-only lookup above). Use the GHSA ID in parentheses when there is no CVE.
- The body **must** end with the `fixes #N` trailer (rule 07): when the commit lands on `main`, GitHub closes the issue and the Kaneo ↔ GitHub sync moves the task to `done`. No trailer only when there is no GitHub issue (roadmap key instead).
- Never the Kaneo CUID or `RO-<n>`. Stage explicit paths only. Pushing follows `.claude/rules/01-git-workflow.md` (`/orchestrate` pushes `dev` after each verified landing).

## Tools

| Tool | Purpose |
| ---- | ------- |
| `mcp__github__get_dependabot_alert` / `list_dependabot_alerts` | Read alert (preferred, when listed) |
| `gh api repos/mdg-labs/release-ops/dependabot/alerts…` (GET) | Read alert (fallback) |
| `mcp__github__search_issues` | Read-only `#N` lookup |
| `mcp__Kaneo__search`, `get_task`, `list_tasks` | Duplicate check |
| `mcp__Kaneo__create_task` | Create task in `ready` |
| `mcp__Kaneo__list_workspace_labels`, `attach_label_to_task`, `create_label` | `deps` / `ci` label |

**Forbidden:** dismissing or otherwise modifying alerts; any GitHub issue write (create, comment, label, edit, close); setting any status other than `ready`; creating a duplicate when an open task exists; code or dependency changes during triage; closing issues any way other than the `fixes #N` commit trailer.
