---
title: "Status mapping"
description: "How Release Ops reads ticket statuses, and how to set up the mapping for a ticket project."
---

Every ticket system names its statuses differently. The status mapping tells Release Ops which of your statuses mean a ticket is open, done or cancelled, and which status to use when it supersedes a ticket.

## Why you need it

Before it creates a ticket for a new release, Release Ops checks the status of the repo's previous ticket. It reads the status live from your ticket system and sorts it with the mapping of the repo's ticket project:

| List | Meaning | Effect on a new release |
| ---- | ------- | ----------------------- |
| Open statuses | The ticket is still being worked on. | The [open-ticket policy](../open-ticket-policy/) decides. |
| Done statuses | The ticket is finished. | A new ticket is created. |
| Cancelled statuses | The ticket was dropped. | Treated like done: a new ticket is created. |

The **Superseded status** is the one status Release Ops sets on an old ticket when it supersedes it. A ticket in that status counts as cancelled, so a ticket left superseded never blocks a repo.

## One mapping per ticket project

The mapping belongs to the ticket project, not to the integration. A Kaneo board, a Jira workflow and a Linear team each have their own statuses, so each [ticket project](../../guide/ticket-projects/) has its own mapping. You pick the statuses from the list your ticket system provides:

- **Kaneo:** the board columns of the project.
- **Jira:** the statuses in the workflow of the project.
- **Linear:** the workflow states of the team.

## Set it up

1. Open the ticket project and its **Status mapping** tab. Choose the project first, because changing the project resets the tab.
2. Select every status that counts as open under **Open statuses**.
3. Select every status that counts as done under **Done statuses**.
4. Select every status that counts as cancelled under **Cancelled statuses**.
5. Choose the **Superseded status**.

Each list needs at least one status, and the superseded status is required.

## Cover every status

Select every status your tickets can have. If Release Ops finds a status that is in none of the three lists and is not the **Superseded status**, it cannot tell whether the ticket is still open. It then reports an error for the repo and creates no new ticket, so a release is not lost by accident. Add the status to the right list to fix it.

Update the mapping when you add or rename statuses in your ticket system.

## Related pages

- [Ticket projects](../../guide/ticket-projects/)
- [Open-ticket policy](../open-ticket-policy/)
- [How release detection works](../release-detection/)
