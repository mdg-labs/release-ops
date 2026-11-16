---
title: "Update PRs and tickets"
description: "Which pull requests Release Ops tracks, which of them get a ticket, and how the ticket follows its PR."
---

Renovate opens and maintains the update PRs. Release Ops tracks each one and keeps one ticket per PR in your ticket system. A ticket exists because an update PR exists, never because a project published a release.

## What counts as an update PR

A PR is an update PR when both of these are true:

- Its author is the Release Ops App's bot user.
- Its description carries Renovate's own markers: the hidden comment Renovate adds to the descriptions it generates, or one of Renovate's checkboxes.

The Dependency Dashboard is recognised the same way: an issue by the App's bot user with Renovate's markers. Release Ops never recognises these objects by branch prefix or by title, because those come from per-repository and preset configuration that Release Ops does not evaluate. A PR that another bot or a person opened is not an update PR, even when it looks like one.

GitHub is the source of truth for a PR's state. Release Ops learns it from webhooks and from a reconcile that reads the bot's open PRs after every run and once an hour, so a missed webhook is repaired.

| State | Meaning |
| ----- | ------- |
| Open | The PR is open. |
| Merged | The PR was merged. |
| Closed | The PR was closed without a merge and no other PR replaced it. |
| Superseded | The PR was closed without a merge because a newer update PR replaced it. Release Ops finds the replacement as an open update PR of the same repository that updates the same dependency. |

## Which PRs get a ticket

A PR gets a ticket when all of these hold:

- It is an update PR and it is open. A PR that Release Ops first sees already merged or closed gets no ticket.
- It updates at least one dependency. No ticket is created for Renovate's config-migration PR, for onboarding PRs, or for any other PR without dependencies.
- Its repository has a ticket project. That is the repository's own ticket project when one is set, otherwise the default ticket project that an admin chooses on the **Settings** page. With neither, the repository's PRs get no tickets.
- It passes the ticket project's filter.

A grouped PR is one ticket that lists all of its dependencies.

### The filter

Each ticket project has a filter, edited on the **Filter** tab of the **Ticket projects** page. By default every PR that is not automerged gets a ticket.

| Setting | Effect |
| ------- | ------ |
| **Update types** | A PR needs a dependency of one of the ticked types: major, minor, patch, digest or lockfile. Tick none to allow every type, including others. |
| **Labels** | A PR needs one of the listed labels, matched by exact name. Leave empty for no restriction. |
| **Skip automerged pull requests** | On by default: a PR that Renovate merges by itself gets no ticket. |

The filter is checked on every PR change, so a PR that gets a matching label later gets its ticket then. A ticket that already exists is never removed because its PR stops matching.

## The ticket lifecycle

The ticket follows its PR. The statuses it moves to come from the ticket project's [status mapping](../status-mapping/).

| What happens to the PR | What happens to the ticket |
| ---------------------- | -------------------------- |
| An eligible PR appears. | A ticket is created, then the PR comment is added. |
| The PR gets a newer version, for example a new push or a new target version. | The ticket's title and description are updated and a note is added. |
| A newer update PR replaces it. | The old ticket moves to the mapped superseded status and gets a comment that links the new PR and, when it has one, the new PR's ticket. |
| The PR is merged. | The ticket moves to the first **Done statuses** entry. |
| The PR is closed without a merge and nothing has replaced it. | The ticket moves to the first **Cancelled statuses** entry. If a replacement appears later, the ticket is superseded instead. |

Before it changes a ticket, Release Ops reads the ticket's current status and sorts it into open, done or cancelled with the project's [status mapping](../status-mapping/). It compares that with the group it last put the ticket in. If the groups differ, for example someone closed or reopened the ticket by hand, Release Ops leaves it alone from then on: it does not reopen, edit or supersede it, and it does not create a second ticket for the PR. A move between two statuses of the same group, for example from To Do to In Progress when both are open statuses, is not a hand change, and the ticket keeps following its PR. A status that is in none of the mapping's lists cannot be sorted. Release Ops then changes nothing, shows **Ticket error** for the PR and retries later, so add the status to the right list. A ticket in the done or superseded state gets no further changes.

The ticket's title, description, update note and supersede comment come from the content templates of the ticket project. The **Ticket projects** page lists the variables a template can use.

Nothing flows from the ticket back to the PR: closing, moving or commenting on a ticket does not act on the PR. Release Ops sends no email, chat or webhook messages of its own.

## The PR comment

After it creates a ticket, Release Ops adds a comment to the PR from the App's bot user. The comment links the ticket and shows its state: open, done, won't do or superseded. Release Ops edits that one comment when the state changes and never posts a second one. The text is fixed and is not a template.

Release Ops never writes into the PR's description. Renovate owns the description and rewrites it on every run.

On the **Dashboard**, each update PR shows its ticket state: Pending, Open, Done, Cancelled or Superseded. **No ticket** appears when a PR has none, and **Ticket error** appears while an operation on its ticket is failing.

## Related pages

- [Status mapping](../status-mapping/)
- [How runs are triggered](../how-runs-are-triggered/)
- [Migrating from a Renovate GitHub Action or self-hosted Renovate](../../guide/migrating-from-renovate/)
