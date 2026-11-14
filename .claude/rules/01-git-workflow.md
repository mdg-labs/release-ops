---
description: Git branch policy, commit format, staging rules, push policy
---

# Git workflow

## Branches

| Branch | Role |
|--------|------|
| `dev` | Integration — nightly GHCR images on push (after CI) |
| `main` | Release track — CI + draft release when `VERSION` bumps; production images on GitHub Release publish |

- Work lands on `dev`; `main` moves only through the `dev → main` pull request (`/open-pr`), merged by the maintainer.
- **Never push to `main`** from agents, and never force-push anything.

## Commit messages (Conventional Commits)

Subject ≤72 chars, imperative mood, **scoped**, no issue number in the subject (`mdg-git.md`).

```
feat(<scope>): <summary>     # body ends with: Fixes #123
fix(<scope>): <summary>      # body ends with: Fixes #123
```

**Allowed scopes**: release-ops, api, db, config, ci, docs, deps

A commit that completes a tracked issue ends its body with `Fixes #N` (the commit that completes an epic also `Fixes #<epic-N>`, added by the orchestrator at landing). When the commit lands on `main`, GitHub closes the issue. That trailer is the only closing mechanism; agents never close issues by hand (`.claude/trackers/github.md`). One issue per commit.

Session memory (`.agents/project/agent-memory/`) is **gitignored** — never commit session files.

## Staging

Stage explicit paths only. Never `git add .` or `git add -A`.

## Push policy

- **`/orchestrate`** pushes `origin/dev` right after each landed commit — the independent verifier's PASS, which includes the full gate, is the gate. It holds a commit back only when a fresh blocking dependency was added to its task during the run, or when unpushed commits it did not land sit underneath.
- **`/pr-review`** pushes `origin/dev` after its round's fixes pass the full gate; **`/open-pr`** pushes only the content-free back-merge of `main` into `dev`.
- Invoking one of those skills is the authorization for its pushes. Everywhere else, agents commit locally and push only when the user explicitly asks — after the full gate (`06-local-ci-before-commit.md`).
- Before any push: `git log origin/dev..dev --oneline` must list only commits you mean to push. Never amend pushed commits (`13-no-amend-pushed.md`).
