---
title: "Dashboard"
description: "See the open and recently merged update PRs of your repositories, with the state of each PR's ticket."
---

The **Dashboard** is the first page you see after you sign in. It lists the update PRs that Renovate has opened and the ones that were merged recently, and shows the state of the ticket that belongs to each.

## Prerequisites

- You are signed in. See [Getting started](../../getting-started/#8-sign-in).
- The App is installed on at least one repository that is enabled. Update PRs appear after Renovate has opened them. See [Repositories](../repositories/).

## What viewers and admins see

| | Viewer | Admin |
| --- | ------ | ----- |
| Update PRs | Only the repositories you can access on GitHub | All repositories the App is installed on |
| Filters, ticket links and pagination | Yes | Yes |

The page has no actions, so viewers and admins use it the same way. A repository outside your list does not appear, and Release Ops does not reveal that it exists. See [Roles](../../concepts/roles/#which-repositories-a-viewer-sees).

## Read the open update PRs

1. Open **Dashboard** in the sidebar.
2. Find the **Open update pull requests** section. The newest PR comes first.
3. Click the title of a PR to open it on GitHub.

**Result:** each row shows the repository, the PR, its update types, how long the PR has been open and its ticket.

The **Age** column shows how long ago the PR was opened. A PR with no known date shows **Unknown**.

## Read the ticket state

The **Ticket** column shows the ticket that Release Ops created for the PR. Click the ticket's ID to open it in your ticket system, when the ticket has a link.

| Shown | Meaning |
| ----- | ------- |
| **Pending** | Release Ops is creating the ticket. |
| **Open** | The ticket exists and is open. |
| **Done** | The PR was merged and the ticket moved to a done status. |
| **Cancelled** | The PR was closed without a merge. |
| **Superseded** | A newer update PR replaced this one. |
| **No ticket** | The PR has no ticket, for example because the repository has no ticket project or the PR does not pass the ticket project's filter. |
| **Ticket error** | An operation on the ticket is failing. Release Ops retries it. Open **Jobs**, then the **Problems** tab, to see it. |

See [Update PRs and tickets](../../concepts/update-prs-and-tickets/) for when a PR gets a ticket and how the ticket follows the PR.

## Filter the lists

1. Choose a repository in **Repository**. The default is **All repositories**.
2. Choose a type in **Update type**: **Major**, **Minor**, **Patch**, **Digest**, **Lockfile** or **Other**. The default is **All update types**.

**Result:** both lists show only the PRs that match. A grouped PR carries one badge for each update type it contains.

## See what was merged

The **Recently merged** section lists the ten most recent merges. The **Merged** column shows when each PR was merged. This list has no pages.

## Page through a long list

The **Open update pull requests** section shows its page range, for example `1–10 of 24`. Use **Rows per page**, **Previous** and **Next** to move. Your choice of page size is remembered in your browser.

## Field and option reference

| Field / option | Description | Required |
| -------------- | ----------- | -------- |
| **Repository** (filter) | Shows the PRs of one repository. | No |
| **Update type** (filter) | Shows the PRs with a dependency of this update type. | No |
| **Repository** (column) | The full name of the repository, `owner/repo`. | — |
| **Pull request** (column) | The PR title, linked to GitHub. | — |
| **Update type** (column) | The update types of the PR's dependencies. **Unknown** when none is known. | — |
| **Age** / **Merged** (column) | When the PR was opened, or when it was merged. | — |
| **Ticket** (column) | The ticket's ID, its state and, when an operation is failing, **Ticket error**. | — |

## What the page looks like when nothing is there

| State | What you see |
| ----- | ------------ |
| Loading | Grey placeholder rows in each section. |
| Nothing at all | **No update pull requests yet**, with the text "Update pull requests appear here once Renovate opens them in a repository that has Release Ops enabled." and a **View repositories** button. |
| No match for the filters | **No open update pull requests match these filters.** or **No merged update pull requests match these filters.** |
| An empty list | **No open update pull requests.** or **Nothing has been merged yet.** |
| An error | **Could not load the update pull requests.** with a **Try again** button, in the section that failed. |

## Troubleshooting

### A PR does not show a ticket

The PR may not be eligible. A repository needs a ticket project, and the PR must pass the project's filter. See [Which PRs get a ticket](../../concepts/update-prs-and-tickets/#which-prs-get-a-ticket) and [Ticket projects](../ticket-projects/).

### The Dashboard is empty

Check the **Repositories** page: a repository without a Renovate configuration gets no update PRs, and a paused one gets no runs. As a viewer, you see only the repositories you can access on GitHub.

### A repository is missing

You see a repository only when the App is installed on it. As a viewer, you also need access to it on GitHub. Your list is read when you sign in, so sign out and in again after you were given access.

## Related pages

- [Repositories](../repositories/)
- [Jobs and Problems](../jobs/)
- [Update PRs and tickets](../../concepts/update-prs-and-tickets/)
- [Roles](../../concepts/roles/)
