---
title: "First steps"
description: "From your first sign-in to the first ticket Release Ops creates for a new release."
---

This guide takes you from your first sign-in to your first automatically created ticket. It assumes Release Ops is already running. If it is not, follow [Getting started](../../getting-started/) first.

## What you'll need

- A Release Ops account: the admin account created during setup, or an invitation link from another user.
- The repository you want to watch on GitHub, GitLab, Gitea, Forgejo or Codeberg.
- An API token for your ticket system (Kaneo, Jira or Linear). Jira also needs the email address of the token's owner.
- An API token for the release source. GitLab, Gitea and Forgejo need one. GitHub and Codeberg work without it, but a token avoids rate limits and gives access to private repos.

## Quick start

1. **Sign in.** Open Release Ops in your browser, enter your email and password, and click **Sign in**. See [Signing in](../signing-in/).
2. **Add a source integration.** Open **Integrations**, click **Add integration**, pick the source (for example GitHub), enter a name and the token, and click **Save**. You can skip this step for a public GitHub or Codeberg repo. See [Integrations](../integrations/).
3. **Add a ticket integration.** Click **Add integration** again, pick **Kaneo**, **Jira** or **Linear**, and enter the base URL (where the kind asks for one) and your token or API key. For Jira, enter the email as well. Click **Save**, then use the **Test connection** button on the new row to check the credentials.
4. **Add a ticket project.** Open **Ticket projects** and click **Add ticket project**. Choose the integration and the project or team, then fill in the **Create config** and **Status mapping** tabs. Leave **Open ticket policy** on **Supersede** for now. See [Ticket projects](../ticket-projects/) and [Status mapping](../../concepts/status-mapping/).
5. **Add a repo.** Open **Repos** and click **Add repo**. Choose the **Source**, enter the **Project path** (for example `owner/repo`), select your **Ticket project**, and click **Save**. See [Repos](../repos/).
6. **Run a poll.** Open the **Dashboard** and click **Run poll now**. Release Ops does not poll when it starts, so the first scheduled poll comes only after one full poll interval.
7. **Check the result.** Open **Poll runs** and click the newest run. Your repo shows a **Baseline** event with the latest release tag. See [Poll runs](../poll-runs/).
8. **Wait for the next release.** The baseline records the release that already exists and creates no ticket. When the project publishes a newer release, the next poll creates a ticket in your ticket project. The **Dashboard** then shows the ticket under **Open ticket**. See [How release detection works](../../concepts/release-detection/).

**You're ready when:** the poll run shows a **Baseline** event for your repo. Release Ops now creates a ticket as soon as a poll finds a new release.

## Next steps

| Goal | Go to |
| ---- | ----- |
| Get a message when a ticket is created or a poll fails | [Notifications](../notifications/) |
| Change how often Release Ops checks for releases | [Settings](../settings/) |
| Choose what happens when the previous ticket is still open | [Open-ticket policy](../../concepts/open-ticket-policy/) |
| Invite a teammate | [Users](../users/) |

## Get help

- [FAQ](../../faq/)
- [Report an issue on GitHub](https://github.com/mdg-labs/release-ops/issues)
