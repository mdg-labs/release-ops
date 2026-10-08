---
title: "Repos"
description: "Choose the repositories Release Ops watches and where their tickets go."
---

Add the repositories you want Release Ops to watch for new releases, and choose the ticket project that receives their tickets.

## Prerequisites

- You are signed in to Release Ops.
- You have added at least one ticket project on the [Ticket projects](../ticket-projects/) page. Without one, the repo form cannot be saved.
- For a GitLab, Gitea or Forgejo repo, you have added a matching source integration on the [Integrations](../integrations/) page.

<!-- SCREENSHOT: Repos page with several repos -->

## Add a repo

1. Open **Repos** and click **Add repo**.
2. Choose the **Source**: GitHub, GitLab, Gitea, Forgejo or Codeberg.
3. Enter the **Project path**. GitHub, Gitea, Forgejo and Codeberg use `owner/repo`. GitLab uses `namespace/project`.
4. Choose the **Ticket project** that receives the tickets.
5. Choose the **Source integration**. The list shows the integrations of the source you chose. GitLab, Gitea and Forgejo need one. For GitHub and Codeberg you can leave it on **None**, but a token avoids rate limits and gives access to private repos. If you set a default integration for the source, it is preselected. When no integration exists for the source you chose, the field is not shown for GitHub and Codeberg, and for GitLab, Gitea and Forgejo the form tells you to add one first.
6. Optional: under **Notification targets**, select the targets that should hear about this repo. Leave all unselected to use every enabled target. The field only appears when at least one enabled target exists. See [Notifications](../notifications/).
7. Keep **Enabled** on. Switch it off to pause the repo.
8. Optional: switch on **Include pre-releases** if you want pre-releases to count as new releases.
9. Click **Save**.

**Result:** The repo appears in the list. It is checked on the next poll.

### What happens on the first poll

The first release Release Ops sees for a repo is recorded as a baseline and creates no ticket. This keeps a new repo from opening a ticket for a release that is already out. The same holds for a repo whose project has no releases yet: the first release that appears later becomes the baseline, and only the release after that creates a ticket. See [How release detection works](../../concepts/release-detection/).

## Edit a repo

1. Click the **Edit** icon in the **Actions** column.
2. Change the fields you need.
3. Click **Save**.

Changing the **Source**, **Project path** or **Source integration** resets the repo's known release. The next poll records a new baseline and creates no ticket. Changing the **Ticket project** or any of these three fields also clears the repo's link to its open ticket. The old ticket stays in your ticket system.

## Pause or resume a repo

1. Edit the repo.
2. Switch **Enabled** off or on.
3. Click **Save**.

Release Ops skips disabled repos during polling. Their **Enabled** column shows **Disabled**.

## Delete a repo

1. Click the **Delete** icon in the **Actions** column.
2. Click **Delete** to confirm.

Release Ops stops watching the repo. Tickets it already created stay in your ticket system. Poll run details no longer show the path of a deleted repo.

## Field reference

| Field | Description | Required |
| ----- | ----------- | -------- |
| Source | GitHub, GitLab, Gitea, Forgejo or Codeberg. | Yes |
| Project path | `owner/repo`, or `namespace/project` for GitLab. | Yes |
| Ticket project | The ticket project that receives tickets for this repo. | Yes |
| Source integration | The credentials used to read releases. Required for GitLab, Gitea and Forgejo. Optional for GitHub and Codeberg. | Depends on the source |
| Notification targets | Targets that receive messages for this repo. Empty means all enabled targets. | No |
| Enabled | Disabled repos are skipped during polling. | No |
| Include pre-releases | Treat pre-releases as new releases. The change applies from the next poll. | No |

The list shows **Project path**, **Source**, **Source integration**, **Ticket project**, **Enabled**, **Last polled**, **Last error** and **Actions**.

## Troubleshooting

### Last error shows a message

The last poll could not finish for this repo. Read the message in the **Last error** column, or open the run on [Poll runs](../poll-runs/). Typical causes are credentials that were rejected, a ticket system that could not be reached, and a ticket whose status is not in the ticket project's status mapping. The error clears after the next successful poll of the repo.

### The Dashboard shows no last tag for the repo

Release Ops treats a "not found" answer from the source as "no release yet", not as an error. A mistyped project path, or a private repo that your token cannot read, can look the same as a project without releases. Check the **Project path** and the integration's token.

### No ticket after adding a repo

That is expected for the first poll. See [What happens on the first poll](#what-happens-on-the-first-poll).

### Save is disabled

No ticket project exists yet. Add one on [Ticket projects](../ticket-projects/).

## Related pages

- [Integrations](../integrations/)
- [Ticket projects](../ticket-projects/)
- [Poll runs](../poll-runs/)
- [How release detection works](../../concepts/release-detection/)
