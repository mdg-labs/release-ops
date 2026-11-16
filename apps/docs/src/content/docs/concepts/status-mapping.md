---
title: "Status mapping"
description: "How a ticket project tells Release Ops which ticket statuses mean open, done, cancelled and superseded."
---

A status mapping translates the life of an update PR into the statuses of your ticket system. Each ticket project has its own mapping, because Kaneo boards, Jira workflows and Linear teams each define their own statuses. You edit it on the **Status mapping** tab of the **Ticket projects** page.

## The four parts

| Field | What it is | What Release Ops does with it |
| ----- | ---------- | ----------------------------- |
| **Open statuses** | Every status that counts as still open. | A ticket in one of these may be updated when its PR gets a newer version. |
| **Done statuses** | Every status that counts as done. | The first entry is the status a ticket moves to when its PR is merged. |
| **Cancelled statuses** | Every status that counts as cancelled or won't do. | The first entry is the status a ticket moves to when its PR is closed without a merge. |
| **Superseded status** | One status. | The status the old ticket moves to when a newer update PR replaces its PR. |

Each of the first three needs at least one status, and the superseded status is required.

The values are the ones your ticket system uses: board column slugs in Kaneo, status names from the project's workflow in Jira, and workflow state IDs in Linear. The page offers the options it reads from the integration.

## Example

A Jira project that uses the statuses To Do, In Progress, Done and Cancelled:

| Field | Value |
| ----- | ----- |
| **Open statuses** | To Do, In Progress |
| **Done statuses** | Done |
| **Cancelled statuses** | Cancelled |
| **Superseded status** | Cancelled |

When a PR is merged, its ticket moves to Done. When a PR is closed without a merge, the ticket moves to Cancelled. When a newer PR replaces it, the old ticket moves to Cancelled and a comment points to the new PR.

## Why the lists hold more than one status

Release Ops reads a ticket's current status before it changes the ticket and sorts it into open, done or cancelled using these lists. A status that is in no list cannot be sorted, so Release Ops retries and does not guess.

If the sorted result differs from what Release Ops last set, someone changed the ticket by hand. Release Ops then leaves the ticket alone. See [the ticket lifecycle](../update-prs-and-tickets/#the-ticket-lifecycle). List every status your team uses for a ticket in the right group, so a ticket that a person moves between two open statuses is still handled as open.

## Related pages

- [Update PRs and tickets](../update-prs-and-tickets/)
