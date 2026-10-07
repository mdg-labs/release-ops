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

Subject ≤72 chars, imperative mood, **scoped**. Task commits carry the GitHub issue number per `07-commit-linking.md`.

```
feat(<scope>)[#123]: <summary>     # body ends with: fixes #123 (mandatory)
fix(<scope>)[#123]: <summary>      # body ends with: fixes #123 (mandatory)
chore(<scope>)[E03-02]: <summary>   # roadmap-only, no GitHub issue, no trailer
```

**Allowed scopes**: release-ops, api, db, config, ci, docs, deps

Task commits **must** end their body with `fixes #N` (the commit that completes an epic also `fixes #<epic-N>`, added by the orchestrator at landing). When the commit lands on `main`, GitHub closes the issue and the Kaneo ↔ GitHub sync moves the task to `done`. That trailer is the only closing mechanism — agents never change GitHub issue state via API. PR bodies may also carry closing keywords; they don't need to.

Session memory (`.agents/project/agent-memory/`) is **gitignored** — never commit session files.

## Staging

Stage explicit paths only. Never `git add .` or `git add -A`.

## Push policy

- **`/orchestrate`** pushes `origin/dev` right after each landed commit — the independent verifier's PASS, which includes the full gate, is the gate. It holds a commit back only when a fresh blocking dependency was added to its task during the run, or when unpushed commits it did not land sit underneath.
- **`/cr-review`** pushes `origin/dev` after its round's fixes pass the full gate; **`/open-pr`** pushes only the content-free back-merge of `main` into `dev`.
- Invoking one of those skills is the authorization for its pushes. Everywhere else, agents commit locally and push only when the user explicitly asks — after the full gate (`06-local-ci-before-commit.md`).
- Before any push: `git log origin/dev..dev --oneline` must list only commits you mean to push. Never amend pushed commits (`13-no-amend-pushed.md`).
