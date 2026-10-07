---
description: Link every task commit to its GitHub issue with [#N] (resolved read-only) and close it with a mandatory `fixes #N` body trailer; GitHub issues are never written by agents
---

# Commit linking

Kaneo (project Release Ops, ticket key `RO`) is the board and the source of truth for task status. Each Kaneo task is mirrored by a GitHub issue in `mdg-labs/release-ops`; agents use that issue's number **only** in commit messages. Every **task-related** commit carries `[#N]` in the subject so GitHub links commits and PRs to the issue, and a `fixes #N` trailer in the body so GitHub closes the issue when the commit lands on `main` (the Kaneo ↔ GitHub sync then moves the task to `done`).

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

Kaneo task payloads have so far carried no GitHub link. Resolve the number by **reading** only:

1. The user or the orchestrator prompt gives `#N` → use it.
2. Optional: if the Kaneo task payload has `externalLinks`, use `externalLinks[].externalId` (issue link) → `#N`. Not observed on current payloads (`get_task` / `get_task_by_ticket_id` for RO-108 return none), so expect to fall through to step 3.
3. Otherwise `mcp__github__search_issues` (owner `mdg-labs`, repo `release-ops`, query = the exact Kaneo task title) → take the exact-title match.
4. No match → use the roadmap key if the task has one; otherwise ask the user. Never create an issue to obtain a number.

### Examples

```bash
feat(api)[#111]: add Kaneo ticket integration client   # body ends with: fixes #111
fix(api)[#103]: apply custom ticket templates at poll time   # body ends with: fixes #103
chore(docs)[E03-02]: mark E03-02 verified   # roadmap-only, no trailer
```

## Body and `fixes` trailer

Plain prose context, then the closing trailer. **Mandatory** on every task commit with a GitHub issue: the body ends with `fixes #N`, where `#N` is the same issue as the subject's `[#N]`.

```text
feat(api)[#111]: add Kaneo ticket integration client

Adds the Kaneo REST client and wires it into the ticket dispatcher.

fixes #111
```

**Final leaf of an epic** (the orchestrator passes `closesParent: yes` for that parent, i.e. it is in `CLOSE_PARENTS`): add the parent's issue too — `fixes #<N>` and `fixes #<parent-N>` on separate lines.

```text
fixes #111
fixes #108
```

**Roadmap-only work** (`[E*-*]`, no GitHub issue): no trailer.

The trailer is the **only** way an issue gets closed, and the only path by which agents' work reaches `done`: when the commit lands on `main`, GitHub closes the issue and the Kaneo ↔ GitHub sync moves the task to `done`. Until then the task sits in `implemented` (verified, commit on `dev`, not yet on `main`). The user may also set `done` manually; no agent ever does.

PR bodies may contain closing keywords too, but don't need to — the commit trailer already does the job.

No GitHub smart commands (`#time`, `#comment`, `#resolve`).

## GitHub issues are never written

No agent, skill or sub-agent ever:

- comments on, labels, assigns, edits, closes, reopens or creates a GitHub issue
- links or unlinks issues to PRs by API

The `fixes #N` commit trailer is not an issue write: GitHub acts on it when the commit reaches `main`. It is the only closing mechanism.

Allowed: `mcp__github__issue_read`, `mcp__github__search_issues`, `mcp__github__list_issues` (read-only).

## Forbidden

- Kaneo CUIDs or `RO-<n>` in any commit message
- Task commits without `[#N]` or `[E*-*]`
- Task commits with `[#N]` but without the `fixes #N` trailer (and `fixes #<parent-N>` on the epic's final leaf)
- Closing GitHub issues directly or setting Kaneo `done` — `done` comes only from the trailer landing on `main` (or the user)
- Any GitHub issue write
