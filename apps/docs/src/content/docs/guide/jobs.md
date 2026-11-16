---
title: "Jobs and Problems"
description: "Follow what Release Ops ran for your repositories, read a job's attempts and log excerpt, and see what needs attention."
---

The **Jobs** page has two tabs. **Jobs** lists the Renovate runs, one job per run of one repository. **Problems** lists what needs attention: failed jobs, ticket errors, rate limits and lost App permissions.

## Prerequisites

- You are signed in.
- A run has happened for at least one repository. Without one, the **Jobs** tab is empty.

## What viewers and admins see

| | Viewer | Admin |
| --- | ------ | ----- |
| Jobs and their details | Only the repositories you can access on GitHub | All repositories |
| **Job failed** problems | Your repositories only | All repositories |
| **Ticket error** problems | Your repositories only, without the error text | All repositories, with the error text |
| **Rate limited** and **Permissions lost** problems | Not shown | Shown |
| **Clear pending marker** | Not shown | Yes |

A job that is not one of your repositories' jobs is not shown to you, and its detail answers **This job does not exist or is not one of your repositories.**

## Read the job list

1. Open **Jobs** in the sidebar. The **Jobs** tab is open.
2. Choose a **Repository** or a **State** to narrow the list. The defaults are **All repositories** and **All states**.

**Result:** each row is one job.

## Open a job

1. Select **Details** at the end of a row.
2. Read the dialog **Job** followed by the repository. It shows the job's state and what triggered it.

The dialog has two parts:

- **Attempts.** A job is retried when it fails, so it can have several attempts. Each shows its **Outcome**, when it started and finished, the **Exit code**, the **Error code** and **Error** when there are any, and the **Report** state. The **Log excerpt** is a redacted part of Renovate's log. **No log was stored for this attempt.** appears when there is none. **No attempt has finished yet.** appears for a job that has not run.
- **Events.** What happened to the job, with the time. **No events were recorded.** appears when there are none.

Select **Close** to leave the dialog. The address of the page changes while a job is open, so you can send the link to a colleague who can see the same repository.

## Read the Problems tab

1. Open the **Problems** tab.
2. Read each row: the kind of **Problem**, the **Repository**, **Since** when it exists and its **Details**.
3. Use **View job** or **View pull request** to open the cause.

A problem is not stored: it shows while its cause exists and disappears with it.

| Problem | Shown while |
| ------- | ----------- |
| **Job failed** | The latest finished job of the repository failed. |
| **Ticket error** | An operation on a ticket is failing, or a ticket's creation is unconfirmed and no admin has resolved it. |
| **Rate limited** | The installation's GitHub rate-limit budget is used up, or the latest attempt ended rate limited. The row says **All repositories**. |
| **Permissions lost** | The App lacks a permission it needs, or its installation is suspended or removed. The row says **All repositories**. |

A viewer sees **Only admins see the error text** in place of the details of a **Ticket error**. **No details recorded** appears when a problem has no text.

## Clear a pending marker

When Release Ops cannot tell whether a ticket was created, for example after a timeout, it records a pending marker and creates nothing more for that PR until an admin decides. Only an admin can clear it.

1. In your ticket system, search the ticket project for a ticket whose description has the line `Release Ops key:` followed by the key shown in the dialog.
2. If a ticket exists, do not clear the marker.
3. If none exists, select **Clear pending marker** on the problem's row, and read **Clear the pending marker?**
4. Select **Clear marker**.

**Result:** the message **Pending marker cleared for** the repository appears. The next event of the PR creates its ticket. Clearing the marker of a ticket that does exist creates a duplicate, which is why the dialog asks you to check first. If the request is refused, the dialog says **The marker could not be cleared** and why, for example because the ticket is already stored.

## Field and option reference

| Field / option | Description | Required |
| -------------- | ----------- | -------- |
| **Repository** (filter) | Shows the jobs of one repository. | No |
| **State** (filter) | **Queued**, **Running**, **Succeeded** or **Failed**. | No |
| **Repository**, **State** | The job's repository and state. | — |
| **Priority** | **High** or **Normal**. Runs started by an event and by **Run now** are high. | — |
| **Trigger** | What started the job: **Dependency Dashboard checkbox**, **Pull request checkbox**, **Pull request closed**, **Push**, **Repository added**, **Schedule**, **Polling** or **Run now**. A **Markers** line lists the checkboxes that were ticked. | — |
| **Attempts** | How many attempts the job has had. | — |
| **Created**, **Started**, **Finished** | Times. **Not yet** when the job has not started or finished. | — |
| **Duration** | How long the job ran. | — |
| **Start latency** | How long it took from the event to the start of the job. | — |
| **Outcome** (attempt) | **Running**, **Succeeded**, **No config**, **Failed**, **Aborted** or **Lease expired**. | — |
| **Report** (attempt) | **Read**, **Missing** or **Unreadable**: whether Release Ops could read Renovate's report. | — |

## What the page looks like when nothing is there

| State | What you see |
| ----- | ------------ |
| Loading | Grey placeholder rows, in the list and in the job dialog. |
| No jobs yet | **No jobs yet**: "Jobs appear here once Renovate has run for a repository." with a **Go to repositories** button. |
| No match for the filters | **No jobs match these filters.** |
| No problems | **No problems**: "Nothing needs attention right now." |
| An error | **Could not load the jobs.**, **Could not load the problems.** or **Could not load this job.**, with a **Try again** button. |

## Troubleshooting

### A job has the outcome No config

The repository has no Renovate configuration file, so the run ended early. See [Repositories](../repositories/#enable-renovate-for-a-repository-without-a-config).

### A job failed

Open the job and read the **Error** and the **Log excerpt** of its last attempt. A failed run is retried up to three attempts. A run that fails because the global configuration could not be fetched is explained under [The global Renovate config](../../concepts/global-renovate-config/).

### A job stays queued

The concurrency cap limits how many jobs run at once. An admin changes it on [Settings](../settings/#set-the-concurrency-cap). See [How runs are triggered](../../concepts/how-runs-are-triggered/#queueing-and-coalescing).

### I see no Rate limited or Permissions lost problems

Only admins see them. They are not about one repository.

## Related pages

- [Repositories](../repositories/)
- [Dashboard](../dashboard/)
- [How runs are triggered](../../concepts/how-runs-are-triggered/)
- [Update PRs and tickets](../../concepts/update-prs-and-tickets/)
