---
description: Git branch policy, commit format, staging rules, never-push default
---

# Git workflow

## Branches

| Branch | Role |
|--------|------|
| `dev` | Integration — nightly GHCR images on push (after CI) |
| `main` | Release track — CI + draft release when `VERSION` bumps; production images on GitHub Release publish |

- Feature work on `dev` (Lane S) or `orchestrator/<TASK-ID>` branches (Lane P); merge to `main` for release.
- **Never push** unless the user explicitly asks.
- **Never push to `main`** from agents.

## Commit messages (Conventional Commits)

Subject ≤72 chars, imperative mood, **scoped**. Task commits carry the GitHub issue number per `07-commit-linking.md`.

```
feat(<scope>)[#123]: <summary>     # body ends with: fixes #123 (mandatory)
fix(<scope>)[#123]: <summary>      # body ends with: fixes #123 (mandatory)
chore(<scope>)[E03-02]: <summary>   # roadmap-only, no GitHub issue, no trailer
```

**Allowed scopes**: release-ops, api, db, config, ci, docs, deps

Plan-file verifier commits: `chore(docs)[E03-02]: mark E03-02 verified`

Task commits **must** end their body with `fixes #N` (the final leaf of an epic also `fixes #<parent-N>`). When the commit lands on `main`, GitHub closes the issue and the Kaneo ↔ GitHub sync moves the task to `done`. That trailer is the only closing mechanism — agents never change GitHub issue state via API. PR bodies may also carry closing keywords; they don't need to.

Session memory (`.agents/project/agent-memory/`) is **gitignored** — never commit session files.

## Staging

Stage explicit paths only. Never `git add .` or `git add -A`.

## Push policy

Agents default to **local commits only**. Full CI gate before push when the user explicitly requests a push (see `06-local-ci-before-commit.md`). Never amend pushed commits (`13-no-amend-pushed.md`).
