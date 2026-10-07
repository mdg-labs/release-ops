---
name: dependabot-triage
description: >-
  Fetch a Dependabot security alert from a GitHub repo via gh CLI, search Kaneo for
  a duplicate Bug, and create a Kaneo Bug if no open duplicate exists. Records
  githubIssueNumber after sync. Use when the user references a Dependabot alert
  number, CVE/GHSA ID, or asks to triage a dependency vulnerability.
---

# Dependabot triage

Fetch alert details from GitHub, search Kaneo for duplicates, create a Kaneo Bug if none found. Kaneo sync creates the GitHub mirror issue.

> Locally migrated to Kaneo — upstream `mdg-labs/skills` still ships the Phasical version; `npx skills update` would revert this.

| What | Path |
| ---- | ---- |
| This skill (installed) | `.claude/skills/dependabot-triage/SKILL.md` |
| Project constants (supporting) | `.agents/project/orchestrator/project.config.md` |
| Intake patterns (installed) | `.claude/skills/kaneo-intake/SKILL.md` |
| Sub-agent monitoring (installed) | `.claude/skills/orchestrator/references/sub-agent-monitoring.md` |

If dispatched as a sub-agent: parent must follow `sub-agent-monitoring.md` — no duplicate `create_task` while this skill is in-flight.

## When to use

| User input | Action |
| ---------- | ------ |
| Alert number `#2` | Fetch → search → create |
| CVE/GHSA ID | Filter open alerts → search → create |
| Security alert URL | Extract number → fetch → search → create |
| "Ticket for ws vulnerability?" | Search only |

## Hard rules

1. **Never** dismiss alerts via GitHub API — follow project Dependabot rule (e.g. `08-dependabot-alerts.md`).
2. **No code changes** during triage.
3. **Do not** set in-progress, implemented or done — leave at backlog or ready after create.
4. **Kaneo-first** create via `create_task` (with `userId` from `whoami`); record `githubIssueNumber` from `list_tasks` `externalLinks` after sync.

## Constants

Read from `.agents/project/orchestrator/project.config.md`:

```text
MCP server: Kaneo  (Claude Code tools: mcp__Kaneo__<tool>)
workspaceId: <from project.config.md>
projectId: <from project.config.md>
GitHub: <owner/repo from project.config.md>
```

## Workflow

```
- [ ] Phase 1: Fetch alert (gh api REST)
- [ ] Phase 2: Search Kaneo duplicates (list_tasks)
- [ ] Phase 3: Create Kaneo Bug (if no open duplicate)
- [ ] Phase 4: Summarise #N + taskId in chat
```

### Phase 1 — Fetch alert

```bash
gh api repos/<owner>/<repo>/dependabot/alerts/<N>
```

Record: package, CVE, GHSA, severity, patched version, alert URL.

### Phase 2 — Duplicate search

```text
search: { q: <package_name>, type: "tasks", workspaceId, projectId }
search: { q: <cve_id>, type: "tasks", workspaceId, projectId }
(or list_tasks: projectId — filter title/description)
```

| Result | Action |
| ------ | ------ |
| Open duplicate (not implemented/done) | Report `#N` + taskId; do not create |
| implemented/done duplicate | Create anyway (regression or incomplete fix) |
| No duplicate | Phase 3 |

### Phase 3 — Create Bug

```text
create_task
  projectId: <from project.config.md>
  title: "dep(<package>): vulnerable to <cve> — bump to <patched>"
  description: <template below>
  priority: high
  status: backlog
  userId: <whoami user.id>
```

Labels: `create_label` with `taskId` for `bug` (+ `security`, `dependabot` if the project uses them) — Kaneo labels are per task.

Wait for sync; resolve `githubIssueNumber` from `list_tasks` `externalLinks` (`get_task` does not return it).

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
update_task_status → ready
```

### Phase 4 — Summarise

Report alert details, duplicate status, new `#N` + Kaneo taskId, suggested next step (`implement #N` via orchestrator).

## Tools

| Tool | Purpose |
| ---- | ------- |
| `gh api` REST | Dependabot alerts |
| `search` / `list_tasks` | Duplicate check |
| `whoami` | Operator `user.id` for `userId` |
| `create_label` | Per-task labels |
| `create_task` | Create Bug |
| `list_tasks` | Resolve synced GitHub # (`externalLinks`) |
| `update_task_status` | Move to ready |

**Forbidden:** setting done; posting findings as comments only; dismissing alerts.
