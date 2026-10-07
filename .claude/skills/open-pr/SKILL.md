---
name: open-pr
description: Opens (or updates) the dev→main promotion pull request, titled for everything the PR implements (never "release"/"promote" framing), with a description generated from the commits, the GitHub issues their `fixes #N` trailers close and the Kaneo tasks behind them. Use when the maintainer says "open a PR to main", "promote dev", "release dev to main", or similar. Never touches main directly and never merges — it only prepares and files the dev→main PR.
argument-hint: (no arguments — always operates on origin/dev → origin/main)
allowed-tools:
  - Read
  - Write
  - Bash(git fetch *)
  - Bash(git status)
  - Bash(git branch *)
  - Bash(git log *)
  - Bash(git diff *)
  - Bash(git rev-list *)
  - Bash(git rev-parse *)
  - Bash(git merge-tree *)
  - Bash(git merge --no-ff origin/main *)
  - Bash(git merge --abort)
  - Bash(git push origin dev)
  - Bash(gh pr list *)
  - Bash(gh pr view *)
  - Bash(gh pr create *)
  - Bash(gh pr edit *)
  - Bash(gh issue view *)
  - Bash(gh run list *)
  - Bash(gh run view *)
  - Bash(timeout * gh run watch *)
  - Bash(.claude/skills/dev-diff/dev-diff.sh*)
  - AskUserQuestion
---

# open-pr

Opens the **dev → main** promotion pull request — the *only* way `main` ever
moves (`.claude/rules/01-git-workflow.md`). This skill prepares and files it (or
updates one already open); it never merges anything and never touches `main`
itself. Merging is gated by the required CI checks and CodeRabbit's review
(`/cr-review`), and is the maintainer's call. When it merges, every `fixes #N`
trailer in it closes its GitHub issue and the Kaneo ↔ GitHub sync moves the task
from `implemented` to `done`; a `VERSION` bump on `main` also drafts a release
(`.github/workflows/main.yml`).

## Hard constraints

- **Base is always `main`, head is always `dev`.** Never the reverse.
- **Never merge.** No `gh pr merge`, no approving or requesting review on the
  maintainer's behalf.
- **Never push on the maintainer's behalf**, with one exception. If local `dev`
  is ahead of `origin/dev`, stop and say so — don't push to make the PR
  "complete". (Under `orchestrate`'s push policy every landed commit already
  reaches `origin/dev` immediately, so this should be rare.) The one push this
  skill makes is step 4's back-merge of `main` into `dev`: a merge commit that
  brings no content, needed because every promotion leaves `main` one merge
  commit ahead of `dev`.
- **GitHub issues are read-only** (`.claude/rules/07-commit-linking.md`): read
  titles, never comment, label or close. The PR body may list `Fixes #N` lines;
  the commit trailers already do the closing.
- **No attribution lines** in the PR title or body — no "Generated with Claude
  Code", no session link (`~/.claude/CLAUDE.md`).
- **Invoke-only.** Run this only when asked directly — opening a PR is a
  visible, shared-state action; being asked to run this skill *is* that request.

## Steps

1. `git fetch origin` for current refs.
2. `git rev-parse dev` vs `git rev-parse origin/dev` — if they differ, local
   `dev` has unpushed commits. Stop and tell the maintainer to push first rather
   than pushing it yourself.
3. `git rev-list --left-right --count origin/main...origin/dev` — if `dev` is 0
   commits ahead, there is nothing to promote. Report that and stop.
4. **Bring `dev` up to date with `main` — only when it is behind.**
   - `git rev-list --count origin/dev..origin/main` — if `0`, skip this whole
     step.
   - **Only a merge that brings no content is made here.** Every change on
     `main` came from `dev`, so merging `main` back must leave `dev`'s tree
     exactly as it is: `git merge-tree --write-tree origin/dev origin/main` must
     succeed and print the same tree as `git rev-parse origin/dev^{tree}`. If it
     fails (a conflict) or prints a different tree, `main` holds something `dev`
     lacks — a hotfix or a hand edit. Stop, show
     `git log --oneline origin/dev..origin/main` and
     `git diff origin/dev origin/main --stat`, and leave it to the maintainer.
     Never resolve a conflict or merge real content here.
   - The current branch must be `dev` with a clean working tree (`git status`).
     Otherwise stop and say so — never switch branches or stash the
     maintainer's work.
   - `git merge --no-ff origin/main -m "Merge branch 'main' into dev"`. If it
     fails anyway, `git merge --abort` and stop.
   - `git push origin dev`. Never force. If the push is rejected (`dev` moved
     meanwhile, e.g. an `orchestrate` landing), stop and report it.
   - **Wait for CI on the merge commit before going on.** Find the `Dev`
     workflow run for the exact pushed SHA:
     `gh run list --repo mdg-labs/release-ops --branch dev --workflow Dev --commit $(git rev-parse dev) --limit 1 --json databaseId,status,conclusion`
     — it can take a few seconds to appear; retry a bounded number of times (at
     most ~10 tries, a few seconds apart), never an open-ended loop. Then wait
     on it with
     `timeout 3600 gh run watch <id> --repo mdg-labs/release-ops --exit-status`
     run as a **background** Bash command, and end the turn; its exit re-invokes
     you. Never poll with `sleep` in the foreground.
   - **CI must pass.** If the run fails, is cancelled or times out, stop — don't
     create or update the PR. Report the run URL and the failing jobs
     (`gh run view <id> --repo mdg-labs/release-ops --log-failed`, bounded and
     filtered). Since the merge brought no content, a red run means `dev` itself
     is red, which is the thing to fix first.
5. **Check the CodeRabbit budget.** Run `.claude/skills/dev-diff/dev-diff.sh`.
   If `FILES REVIEWABLE BY CODERABBIT` is above 100, say so plainly before going
   on: CodeRabbit will not review the whole PR, and the maintainer decides
   whether to file it anyway.
6. Check for an existing open promotion PR:
   `gh pr list --repo mdg-labs/release-ops --base main --head dev --state open --json number,title,url`.
   If one exists, go on through steps 7 and 8 as usual, then in step 9
   **update** it instead of creating a duplicate.
7. Gather the promotion's contents:
   - `git log --oneline origin/main..origin/dev` for the commit list.
   - `git log origin/main..origin/dev --format=%B` to pull every `fixes #N`
     trailer (and `Refs: GHSA-…` trailers), then
     `gh issue view <N> --repo mdg-labs/release-ops --json title,labels` for
     each to get its title. A `Refs: GHSA-…` trailer is listed by id only,
     never with a description — the advisory is not published until the
     maintainer does so after the merge.
   - `git diff --stat origin/main...origin/dev` for files and areas touched.
   - Cross-check the touched paths against the domain labels in
     `.agents/project/orchestrator/project.config.md` (§ Area → paths). Call out
     security-sensitive content explicitly (auth, sessions, tokens, credential
     storage, outbound requests, schema and migrations) — never bury it.
   - Does `VERSION` change in this promotion? Say so: merging drafts a release.
   - `gh run list --repo mdg-labs/release-ops --branch dev --workflow Dev --commit $(git rev-parse origin/dev) --limit 1 --json status,conclusion,workflowName,headSha`
     for the CI result on the exact commit being promoted. If none exists for
     that SHA, or it isn't green, say so plainly — don't fall back to an older
     or unrelated run to make the PR look ready.
8. Draft:
   - **Title** describes what changed, never that a promotion is happening —
     GitHub already shows this is a dev→main PR. Never
     `chore(release): promote dev to main`, "Release x.y.z: dev → main", or any
     "release"/"promote" framing.
     - The title names **everything the PR implements**, as one Conventional
       Commits-style line: the shared type and scope, then each piece of work in
       a few words, e.g.
       `fix(deps): bump next, astro, vitest and 14 transitive packages`. Never
       title it after one commit or one issue. Group closely related commits
       under one phrase so the line stays readable. When the work spans several
       scopes, use the type that fits most of it and leave the scope out.
     - Re-check the title against the full commit list before creating or
       updating the PR: every issue in `## Issues closed` and every uncounted
       commit must fall under some phrase in it.
   - **Body** (write to a scratchpad temp file for `--body-file`):
     - `## Summary` — one or two sentences on what this promotion contains.
     - `## Issues closed` — bulleted `Fixes #N — <title>` list.
     - `## Changes by area` — bulleted, grouped by domain (`backend`, `web`,
       `db`, `config`, `ci`, `docs`, `deps`).
     - `## Security-sensitive` — only if applicable; name the issues and what
       makes them so.
     - `## Release` — only if `VERSION` changes: the new version, and that
       merging drafts the release.
     - `## CI status` — the latest `Dev` run's result from step 7, and the
       CodeRabbit reviewable-file count from step 5.
9. `gh pr create --repo mdg-labs/release-ops --base main --head dev --title "…" --body-file <tmp>`
   (or `gh pr edit <n> --repo mdg-labs/release-ops --title "…" --body-file <tmp>`
   if updating an existing one).
10. Report the PR URL, the closed-issue list, and the CI status. Stop there — no
    merge, no review request, no further action. CodeRabbit's findings are the
    next step: `/cr-review <PR number>`.
