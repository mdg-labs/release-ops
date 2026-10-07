---
title: "Dashboard"
description: "See the state of your last poll and every watched repo, and start a poll on demand."
---

See at a glance when Release Ops last checked your repos, whether anything failed, and which release and ticket each repo is at. Start a poll whenever you do not want to wait for the schedule.

## Prerequisites

- You are signed in to Release Ops.
- To see repos in the table, you have added at least one on the [Repos](../repos/) page.

<!-- SCREENSHOT: Dashboard with status card and repo table -->

## Read the status card

The **System status** card shows the **Last poll**:

- A badge with the state of the last run. It reads **Polling** while a poll is in progress, otherwise **Success** or **Failed**. **No runs** shows only when no poll run exists at all. A run where only some repos had errors shows as partial. **Running** appears only for a run still marked as running while no poll is active.
- **Poll interval**: how often Release Ops polls on its own, in minutes.
- **Started** and **Finished**, **Repos checked** and **Tickets created** for the last run. **Finished** shows **In progress** while the run is active.

Before the first poll, the card shows **No poll has completed yet.**

## Run a poll now

1. Click **Run poll now**.
2. Wait while the card shows **Poll in progress…** and the badge reads **Polling**.

**Result:** The card updates with the new run. The run appears on [Poll runs](../poll-runs/) with the trigger **Manual**. While a poll is running, the button is disabled.

## Check for errors

When the last run had errors, a warning titled **Errors in last poll run** lists each affected repo with its message. Fix the cause, for example credentials or a status mapping, and run the poll again. See [Poll runs](../poll-runs/) for the full history.

## Read the repo table

Under **Monitored repos**, the table has one row per repo:

- **Source** and **Path**. The path links to the repository.
- **Last tag**: the latest release Release Ops has seen. It links to the release page. A dash means no release has been seen yet.
- **Release date**: when that release was published.
- **Open ticket**: the ticket Release Ops created for the repo and is still tracking, with the release tag it belongs to underneath. The ticket ID links to the ticket in your ticket system. A dash means there is none.
- **Last polled**: when the repo was last checked.

Click a column heading to sort. Use **Rows per page**, **Previous** and **Next** to page through long lists. Times use the 24-hour clock in the time zone your administrator configured, which is UTC by default.

With no repos, the table shows **No monitored repos** and an **Add repo** button.

## Field reference

| Field | Description |
| ----- | ----------- |
| Last poll badge | **Polling** while a poll is in progress. Otherwise the state of the last run: **Success**, **Failed**, partial, **Running** (a run still marked as running while no poll is active) or **No runs** (no run exists yet). |
| Poll interval | Minutes between automatic polls. Change it on [Settings](../settings/). |
| Repos checked | How many enabled repos the last run looked at. |
| Tickets created | How many tickets the last run created. |
| Last tag | The latest release seen for the repo. |
| Open ticket | The ticket Release Ops tracks for the repo. |

## Troubleshooting

### Run poll now does nothing

A poll is already running. Wait for it to finish. The button is disabled until then.

### The table is empty

You have no repos yet. Click **Add repo**.

### A repo shows a dash under Last tag

Release Ops has not seen a release for it. The first poll has not run, or the project has no release. See [Repos](../repos/).

## Related pages

- [Poll runs](../poll-runs/)
- [Repos](../repos/)
- [Settings](../settings/)
