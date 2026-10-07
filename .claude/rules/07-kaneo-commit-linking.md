---
description: Link every task commit to a GitHub issue number (synced from Kaneo) for commit/PR traceability
---

# Kaneo / GitHub commit linking

Every **task-related** commit must include the **GitHub issue number** so commits and PRs link in GitHub when pushed. Kaneo is the project-management source of truth; Kaneo auto-syncs tasks to GitHub issues. **Commits use the GitHub number — not the Kaneo task ID.**

## Required subject format

```
<type>(<scope>)[#<N>]: <imperative summary>
```

| Part | Rule |
|------|------|
| `<type>` | Conventional Commits: `feat`, `fix`, `chore`, `refactor`, `docs`, `test`, `ci`, `build`, `perf` |
| `<scope>` | Allowed scopes from `01-git-workflow.md` |
| `[#<N>]` | **Mandatory** on task commits — square brackets, hash + GitHub issue number |
| Summary | Imperative mood; whole subject ≤72 chars |

### Issue number precedence

1. **Kaneo task with GitHub sync** — `[#N]` where `N` is `externalLinks[].externalId` for `resourceType: "issue"` (read via `list_tasks`). **Not** the `RO-<n>` ticket number — `RO-106` is `#108`.
2. **Roadmap only** — `[P2-01]` when no Kaneo/GitHub issue exists yet
3. **Combined batch** — primary leaf GitHub number in brackets; list siblings in session memory

### Examples

```bash
feat(web)[#36]: add Playwright auth fixture
fix(worker)[#42]: skip expiry while vehicles en route
chore(plan)[P8-07]: mark P8-07 verified   # roadmap-only
```

## Body

**Mandatory:** every task commit ends with a `fixes #N` trailer. Issues are closed **only** by this trailer: when the commit lands on the default branch (`main`), GitHub closes the issue and Kaneo moves the task Implemented → Done. No agent closes issues or sets `done`.

```
feat(api)[#108]: fix Jira get-ticket request

<optional body>

fixes #108
```

**Final leaf of an epic** (orchestrator passes `closesParent: yes`): add the parent too — `fixes #<leaf>` and `fixes #<parent>` on separate lines.

Roadmap-only commits (`[P*-*]`, no GitHub issue) have no trailer.

Kaneo MCP owns all other status transitions — do not use GitHub smart commands (`#time`, `#comment`, `#resolve`) in commit messages.

## Forbidden

- Kaneo task ID or `RO-<n>` ticket ID in any commit message
- Task commits without `[#N]` or `[P*-*]` when Kaneo sync was in scope
- Task commits (`[#N]`) without the `fixes #N` trailer
- Closing GitHub issues directly or setting Kaneo `done` — Done comes only from the trailer landing on `main`
