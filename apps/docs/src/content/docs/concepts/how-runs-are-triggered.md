---
title: "How runs are triggered"
description: "What starts a Renovate run for a repository, how runs are queued and what happens when several triggers arrive at once."
---

A run is one Renovate job for one repository. Release Ops starts a run when something changes in the repository, when you tick a checkbox that Renovate put in a PR or on the Dependency Dashboard, and on an hourly schedule as a safety net. Admins can also start a run by hand.

## Triggers

| Trigger | What starts it | Priority |
| ------- | -------------- | -------- |
| Ticking a checkbox on the Dependency Dashboard | You tick a box in the Dependency Dashboard issue that the App's bot user opened. | High |
| Ticking a checkbox in an update PR | You tick a box in the description of a PR that the App's bot user opened, such as the rebase checkbox. | High |
| A closed update PR | An update PR of the bot is merged or closed. A merge or a close frees a slot for the next update. | High |
| A push to the default branch | Configuration or manifests may have changed. | High |
| A repository added to the installation | You add a repository to the App's installation on GitHub. One run per added repository. | High |
| Run now | An admin starts a run for a repository. Viewers cannot. | High |
| Hourly run | Every hour, each enabled repository that is not paused and has a Renovate configuration. | Normal |

High-priority runs are claimed before normal ones. A webhook-driven run never waits behind the hourly runs.

## Ticking a checkbox

Renovate marks its checkboxes with a hidden comment on the same line. A run starts when an edit changes a box from unchecked to checked.

- It works for any checkbox Renovate writes, in a PR or on the Dependency Dashboard. Release Ops keeps no list of checkbox names, so a checkbox that a later Renovate version adds works without a Release Ops change.
- An edit that ticks nothing is ignored: a title change, a text edit, or Renovate unticking a box after it acted.
- Edits made by the App's own bot user are ignored. Renovate rewrites the dashboard and the PR descriptions on every run, and those rewrites must not start new runs.

Release Ops recognises the bot's PRs and dashboard by their author, the App's bot user, together with Renovate's own markers in the text. It never goes by a branch prefix or by the dashboard's title. See [Update PRs and tickets](../update-prs-and-tickets/).

## How fast a run starts

With a webhook, Release Ops aims to start the run within about 10 seconds of the event. That covers the time until Renovate begins, not Renovate's own run time. The PR or the dashboard is updated when Renovate finishes, which takes minutes for most repositories.

Without a public webhook URL, Release Ops runs in polling mode: it checks GitHub every few minutes instead, so ticks, closed PRs and pushes are noticed with a delay, and a tick made during a run can be missed. See [Without a public URL: polling mode](../../getting-started/#without-a-public-url-polling-mode). The hourly run and run now work the same in both modes.

## Queueing and coalescing

- A repository has at most one running job and one waiting job.
- A trigger that arrives while a job for the repository is waiting joins that job. It does not create a second one. The job keeps the highest priority among its triggers and records the checkboxes that were ticked.
- A high-priority trigger that arrives while the repository's job is running is not lost. Renovate may already have read the old PR description, so Release Ops queues one more run when the current one ends. Several ticks during one run collapse into that one rerun.
- A normal-priority trigger during a run is dropped, because the run in progress covers it.
- Across all repositories, the concurrency cap limits how many runs go at once. The default is 2, and you change it on the **Settings** page. With a cap of 2 or more, one slot stays reserved for high-priority runs.

## Repositories that do not run

- A repository without a Renovate configuration file ends its run early and is recorded as having no config. The hourly run skips it. A push to its default branch, adding it to the installation, or a run now still starts a run, so adding the file is noticed.
- A paused repository gets no run from any trigger. Events for it are accepted and dropped.

## When a run fails

A failed run is retried, up to three attempts, with a growing delay. A run that ends because GitHub's rate limit is used up gets more attempts and waits for the limit to reset. All of the installation's work shares one GitHub rate-limit budget.

## Related pages

- [Update PRs and tickets](../update-prs-and-tickets/)
- [The global Renovate config](../global-renovate-config/)
- [Roles](../roles/)
