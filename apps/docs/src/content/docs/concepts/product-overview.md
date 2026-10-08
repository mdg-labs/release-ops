---
title: "Product overview"
description: "What Release Ops is, what it is not, who it is for and how it works."
---

Release Ops is a self-hosted release monitor. You list the repositories you care about. Release Ops checks them on a schedule and creates a ticket when a new release ships.

## What Release Ops is

- **Sources:** GitHub, GitLab, Gitea, Forgejo and Codeberg. Release Ops follows the latest stable release of each repo you add.
- **Tickets:** Kaneo, Jira or Linear. You can combine any source with any ticket system. One ticket integration can serve several projects or teams, and each one has its own status mapping.
- **Notifications:** Send a message to any Shoutrrr target, such as Slack or ntfy, when a ticket is created, when a check fails, or when a ticket is superseded.
- **Status at a glance:** The web UI shows the last check, errors and an overview of your repos.

You configure everything in the web UI. Release Ops runs as a single Docker container that you host yourself.

## How it works

```text
Source repository  →  Poll  →  Ticket + notification
GitHub, GitLab,       Latest     Kaneo, Jira or Linear,
Gitea, Forgejo,       stable     plus Shoutrrr targets
Codeberg              release
```

1. **Source:** You add a repository from one of the supported sources on the [Repos](../../guide/repos/) page.
2. **Poll:** Release Ops checks the latest stable release at the interval you set. The first check of a new repo only records the current release, so adding a repo does not create a ticket for a release that already exists. See [How release detection works](../release-detection/).
3. **Ticket and notification:** When the release changes, Release Ops creates a ticket in the ticket project you chose and notifies your targets. If an earlier ticket for the same repo is still open, the default is to supersede it. You can merge instead, or skip the new ticket. See [Open-ticket policy](../open-ticket-policy/).

## Who it is for

Release Ops is for small self-hosting dev and ops teams, and for individuals, who want new releases of the projects they depend on turned into tickets automatically. If you track upstream versions by hand or by watching release feeds, Release Ops puts those releases where you already plan your work.

## What it is not

Release Ops keeps a deliberately small scope:

- **No other sources.** Only the five sources above are supported. Bitbucket and Azure DevOps, for example, are not.
- **No roles or permissions.** Everyone who is signed in has the same access.
- **No public sign-up.** The first account is created during setup, and everyone else joins through an invitation.
- **Not a multi-service deployment.** There is one container and one public port, with no separate web and worker services to run.

## Next steps

- [First steps](../../guide/first-steps/): from sign-in to your first ticket
- [Getting started](../../getting-started/): run Release Ops with Docker Compose
- [FAQ](../../faq/)
