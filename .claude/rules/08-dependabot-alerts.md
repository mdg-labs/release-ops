---
description: Never dismiss Dependabot security alerts — bump the dependency, tracked as a GitHub issue through /triage
---

# Dependabot alert resolution policy

## Rule

**Never dismiss a Dependabot alert via the GitHub UI or API** (`PATCH state=dismissed`).

The correct resolution is to **upgrade the vulnerable dependency** and land the fix on `dev`. GitHub auto-closes the alert once the vulnerable version is gone from the dependency graph.

## Correct workflow

1. Read the alert (read-only): package, ecosystem (`npm` or Go modules), vulnerable range, patched version.
2. Track it: `/triage <alert>` searches the open GitHub issues for a duplicate and, if none exists, creates a **GitHub issue** in `status:ready`.
3. Upgrade in the relevant manifest — `package.json` (root, `apps/web`, `apps/docs`) + `package-lock.json`, or `go.mod` + `go.sum`.
4. Run the scoped gate (`06-local-ci-before-commit.md`); Go bumps also run `go test ./... && golangci-lint run`.
5. Commit: `fix(deps): bump <pkg> to <version> (CVE-XXXX-XXXX)`; the body ends with `Fixes #N` for the tracking issue.
6. The alert auto-closes when the fix reaches the default branch.

## Dismissal is only acceptable when

- The vulnerability does not affect this project's usage **AND**
- The user explicitly reviewed the CVE **AND**
- The user dismisses it themselves, with the reason documented in the alert.

Even then, prefer a version bump if one is available.

## This rule applies to ALL agents

No agent or sub-agent may dismiss an alert or write to a GitHub issue (no comments, labels, state changes or new issues). Report `blocked` if the bump cannot land — never dismiss.
