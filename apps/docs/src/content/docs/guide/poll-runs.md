---
title: "Poll runs"
description: "Review every poll run and see what happened to each repo."
---

Review the history of poll runs, from the schedule or started by hand, and open a run to see what Release Ops did for each repo.

## Prerequisites

- You are signed in to Release Ops.
- At least one poll has run. Until then the page shows **No poll runs yet**.

<!-- SCREENSHOT: Poll run history table -->

## Browse the history

Open **Poll runs**. The newest run is first. Each row shows:

| Column | Description |
| ------ | ----------- |
| Started | When the run began. |
| Finished | When the run ended. Shows **In progress** while it is running. |
| Status | **Success**, **Partial**, **Failed** or **Running**. |
| Trigger | **Automatic** for a scheduled run, **Manual** for **Run poll now**. |
| Repos checked | How many enabled repos the run looked at. |
| Tickets created | How many tickets the run created. |

Use **Rows per page**, **Previous** and **Next** to move through the list. Times use the 24-hour clock with milliseconds, in the time zone your administrator configured (UTC by default).

The status tells you how the run went:

- **Success**: no repo had an error.
- **Partial**: some repos had an error and others did not.
- **Failed**: every repo that was checked had an error.
- **Running**: the run has not finished.

## Open a run

1. Click a row.
2. Read the **Poll run details** dialog.
3. Click **Cancel** to close it.

The dialog shows the status, trigger, **Started**, **Finished**, **Repos checked**, **Tickets created** and **Tickets superseded**. If repos had errors, an **Errors** list names each repo with its message. Under **Events**, a table lists what Release Ops did:

| Column | Description |
| ------ | ----------- |
| Time | When the event happened. |
| Repo | The repo path and its source. A dash means the repo was deleted. |
| Action | What Release Ops did. See the table below. |
| Tag | The release the event refers to. |
| Ticket | The ticket ID, linked to the ticket when Release Ops knows its address. |
| Detail | Extra information, such as an error message. |

| Action | Meaning |
| ------ | ------- |
| Baseline | First release seen for the repo. Recorded, no ticket. |
| Skip | The release has not changed since the last poll. Nothing to do. |
| Create ticket | A new ticket was created. |
| Supersede | The previous open ticket was closed out in favour of a new one. The **Ticket** column shows the old ticket. A **Create ticket** event follows for the new one. |
| Merge | The open ticket was updated with the new release. No second ticket. |
| Skip open ticket | A new release appeared but the previous ticket is still open and the policy says to skip. |
| Error | Something failed for this repo. **Detail** has the message. |

See [Open-ticket policy](../../concepts/open-ticket-policy/) for when each of these happens.

## Troubleshooting

### A run has no events

The dialog shows **No events recorded for this run.** There are two reasons:

- No repo was enabled during the run, so nothing was checked.
- Release Ops could not save the result for a repo, for example because the database was busy. The repo counts as checked and appears in the **Errors** list, but it has no event.

A repo that could not be checked for another reason, for example because its ticket project or ticket integration could not be loaded, still gets an **Error** event with the reason in **Detail**.

### A run is Partial or Failed

Open the run and read the **Errors** list and the **Detail** of each **Error** event. Common causes are rejected credentials, a ticket system that could not be reached, and a ticket status missing from the status mapping. See [Ticket projects](../ticket-projects/).

### A Create ticket event is followed by an Error

With the supersede policy, Release Ops creates the new ticket first and then updates the old one. If the old ticket cannot be updated, for example its status or comment, the run lists a **Create ticket** event and then an **Error** event for the repo, with no **Supersede** event, and **Tickets superseded** does not count it. The new ticket exists. Check the old ticket in your ticket system and the **Superseded status** in the ticket project.

## Related pages

- [Dashboard](../dashboard/)
- [How release detection works](../../concepts/release-detection/)
- [Open-ticket policy](../../concepts/open-ticket-policy/)
