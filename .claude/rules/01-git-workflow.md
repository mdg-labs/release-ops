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
feat(<scope>)[#123]: <summary>
fix(<scope>)[#123]: <summary>
chore(<scope>)[E03-02]: <summary>   # roadmap-only, no GitHub issue
```

**Allowed scopes**: release-ops, api, db, config, ci, docs, deps

Plan-file verifier commits: `chore(docs)[E03-02]: mark E03-02 verified`

No closing keywords (`fixes` / `closes` / `resolves #N`) in commit or PR bodies — agents never change GitHub issue state.

Session memory (`.agents/project/agent-memory/`) is **gitignored** — never commit session files.

## Staging

Stage explicit paths only. Never `git add .` or `git add -A`.

## Push policy

Agents default to **local commits only**. Full CI gate before push when the user explicitly requests a push (see `06-local-ci-before-commit.md`). Never amend pushed commits (`13-no-amend-pushed.md`).
