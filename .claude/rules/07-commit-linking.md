---
description: Link every task commit to its GitHub issue with [#N] (resolved read-only); no closing keywords; GitHub issues are never written by agents
---

# Commit linking

Kaneo (project Release Ops, ticket key `RO`) is the board and the source of truth for task status. Each Kaneo task is mirrored by a GitHub issue in `mdg-labs/release-ops`; agents use that issue's number **only** to link commits. Every **task-related** commit carries `[#N]` so GitHub links commits and PRs to the issue.

## Required subject format

```
<type>(<scope>)[#<N>]: <imperative summary>
```

| Part | Rule |
|------|------|
| `<type>` | `feat`, `fix`, `chore`, `refactor`, `docs`, `test`, `ci`, `build`, `perf` |
| `<scope>` | `release-ops`, `api`, `db`, `config`, `ci`, `docs`, `deps` (`01-git-workflow.md`) |
| `[#<N>]` | **Mandatory** on task commits — square brackets, `#` + GitHub issue number |
| Summary | Imperative mood; whole subject ≤72 chars |

### Which key goes in the brackets

1. **Kaneo task with a GitHub issue** — `[#N]`, the issue that mirrors the task.
2. **Roadmap-only work without a GitHub issue** — the roadmap key, e.g. `[E03-02]`.
3. **Combined batch** — the primary leaf's `[#N]`; list sibling tasks in session memory.

Never put a Kaneo task CUID or an `RO-<n>` ref in a commit message.

### Resolving `#N` (read-only)

Kaneo task payloads carry no GitHub link. Resolve the number by **reading** GitHub only:

1. The user or the orchestrator prompt gives `#N` → use it.
2. Otherwise `mcp__github__search_issues` (owner `mdg-labs`, repo `release-ops`, query = the exact Kaneo task title) → take the exact-title match.
3. No match → use the roadmap key if the task has one; otherwise ask the user. Never create an issue to obtain a number.

### Examples

```bash
feat(api)[#111]: add Kaneo ticket integration client
fix(api)[#103]: apply custom ticket templates at poll time
chore(docs)[E03-02]: mark E03-02 verified   # roadmap-only
```

## Body

Plain prose context only. **Closing keywords are forbidden** in commit and PR bodies — `fixes #N`, `closes #N`, `resolves #N` and their variants (`fix`, `fixed`, `close`, `closed`, `resolve`, `resolved`), in any casing. GitHub would close the issue on merge, and issue state is not the agents' to change. Status lives on Kaneo: the verifier sets `implemented`, the user sets `done`.

No GitHub smart commands (`#time`, `#comment`, `#resolve`) either.

## GitHub issues are never written

No agent, skill or sub-agent ever:

- comments on, labels, assigns, edits, closes, reopens or creates a GitHub issue
- links or unlinks issues to PRs by API

Allowed: `mcp__github__issue_read`, `mcp__github__search_issues`, `mcp__github__list_issues` (read-only).

## Forbidden

- Kaneo CUIDs or `RO-<n>` in any commit message
- Task commits without `[#N]` or `[E*-*]`
- Closing keywords in commit or PR bodies
- Any GitHub issue write
