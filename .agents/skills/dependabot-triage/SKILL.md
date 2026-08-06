---
name: dependabot-triage
description: >-
  Fetch a Dependabot security alert from a GitHub repo via gh CLI, search Phasical for
  a duplicate Bug, and create a Phasical Bug if no open duplicate exists. Records
  githubIssueNumber after sync. Use when the user references a Dependabot alert
  number, CVE/GHSA ID, or asks to triage a dependency vulnerability.
---

# Dependabot triage

Fetch alert details from GitHub, search Phasical for duplicates, create a Phasical Bug if none found. Phasical sync creates the GitHub mirror issue.

| What | Path |
| ---- | ---- |
| This skill (installed) | `.agents/skills/dependabot-triage/SKILL.md` |
| Project constants (supporting) | `.agents/project/orchestrator/project.config.md` |
| Intake patterns (installed) | `.agents/skills/phasical-intake/SKILL.md` |
| Sub-agent monitoring (installed) | `.agents/skills/orchestrator/references/sub-agent-monitoring.md` |

If dispatched as a sub-agent: parent must follow `sub-agent-monitoring.md` — no duplicate `create_task` while this skill is in-flight.

## When to use

| User input | Action |
| ---------- | ------ |
| Alert number `#2` | Fetch → search → create |
| CVE/GHSA ID | Filter open alerts → search → create |
| Security alert URL | Extract number → fetch → search → create |
| "Ticket for ws vulnerability?" | Search only |

## Hard rules

1. **Never** dismiss alerts via GitHub API — follow project Dependabot rule (e.g. `08-dependabot-alerts.mdc`).
2. **No code changes** during triage.
3. **Do not** set in-progress or done — leave at backlog or to-do after create.
4. **Phasical-first** create via `create_task`; record `githubIssueNumber` after sync.

## Constants

Read from `.agents/project/orchestrator/project.config.md`:

```text
MCP server: user-phasical
projectId: <from project.config.md>
GitHub: <owner/repo from project.config.md>
```

## Workflow

```
- [ ] Phase 1: Fetch alert (gh api REST)
- [ ] Phase 2: Search Phasical duplicates (list_tasks)
- [ ] Phase 3: Create Phasical Bug (if no open duplicate)
- [ ] Phase 4: Summarise #N + taskId in chat
```

### Phase 1 — Fetch alert

```bash
gh api repos/<owner>/<repo>/dependabot/alerts/<N>
```

Record: package, CVE, GHSA, severity, patched version, alert URL.

### Phase 2 — Duplicate search

```text
list_tasks: projectId + search title/description for <package_name>
list_tasks: projectId + search for <cve_id>
```

| Result | Action |
| ------ | ------ |
| Open duplicate (not done/closed) | Report `#N` + taskId; do not create |
| done/closed duplicate | Create anyway (regression or incomplete fix) |
| No duplicate | Phase 3 |

### Phase 3 — Create Bug

```text
create_task
  projectId: <from project.config.md>
  title: "dep(<package>): vulnerable to <cve> — bump to <patched>"
  description: <template below>
  priority: high
  status: backlog
```

Attach labels: `bug`, `security`, `dependabot` (if project uses them).

Wait for sync; resolve `githubIssueNumber` from `externalLinks`.

**Description template:**

```markdown
## Report

Dependabot alert #<alert_number>: <summary>

## Alert details

- **Package:** `<package_name>`
- **Patched version:** `>= <patched_version>`
- **CVE:** <cve_id> · **GHSA:** <ghsa_id>
- **Alert URL:** <alert_url>

## Resolution path

Bump `<package_name>` per project Dependabot policy. Land fix on integration branch.
```

```text
update_task_status → to-do
```

### Phase 4 — Summarise

Report alert details, duplicate status, new `#N` + Phasical taskId, suggested next step (`implement #N` via orchestrator).

## Tools

| Tool | Purpose |
| ---- | ------- |
| `gh api` REST | Dependabot alerts |
| `list_tasks` | Duplicate check |
| `create_task` | Create Bug |
| `get_task` | Resolve synced GitHub # |
| `update_task_status` | Move to to-do |

**Forbidden:** setting done; posting findings as comments only; dismissing alerts.
