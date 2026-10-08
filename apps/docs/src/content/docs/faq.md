---
title: "FAQ"
description: "Short answers to the questions people ask most often."
---

## Why did adding a repo not create a ticket?

The first release Release Ops sees for a repo is recorded as a baseline and creates no ticket. The next release creates one. See [How release detection works](../concepts/release-detection/).

## How often does Release Ops check for releases?

Every 360 minutes (6 hours) by default. Change the interval, minimum 5 minutes, under [Settings](../guide/settings/). To check right away, click **Run poll now** on the [Dashboard](../guide/dashboard/). Release Ops does not poll when it starts.

## Does Release Ops notice pre-releases?

Not by default. It follows the latest stable release. Switch on **Include pre-releases** when you edit a [repo](../guide/repos/) to treat pre-releases as new releases too.

## Which sources and ticket systems are supported?

Sources: GitHub, GitLab, Gitea, Forgejo and Codeberg. Ticket systems: Kaneo, Jira and Linear. Any source can be combined with any ticket system, per repo. Other sources, such as Bitbucket and Azure DevOps, are not supported.

## Can tickets go to different projects or teams?

Yes. Add one [ticket project](../guide/ticket-projects/) for each Kaneo project, Jira project or Linear team. A repo picks the ticket project that receives its tickets.

## What happens when a new release ships and the last ticket is still open?

That depends on the ticket project's policy: **Supersede** (the default), **Merge** or **Skip if open**. See [Open-ticket policy](../concepts/open-ticket-policy/).

## A repo shows an error about an unknown ticket status. What now?

The ticket's status is not in any of the status lists of its ticket project, so Release Ops cannot tell whether the ticket is open. Add the status to the right list. See [Status mapping](../concepts/status-mapping/).

## Are my tokens and URLs safe?

Release Ops stores integration tokens and notification URLs encrypted and never shows them again after you save them. The integrations list only shows whether a token is set: **Configured** or **Missing**. **Missing** means no token is set, which is fine for public repos on GitHub, Gitea, Forgejo and Codeberg.

## Can I give users different permissions?

No. Everyone who is signed in has the same access, and there are no roles. Any user can invite and remove others. See [Users](../guide/users/).

## Can people sign up on their own?

No. The first account is created during setup. Everyone else joins through an invitation from an existing user. See [Users](../guide/users/).

## I forgot my password

Click **Forgot password?** on the sign-in page. Release Ops emails you a reset link if outgoing email is set up on your instance. See [Signing in](../guide/signing-in/).

## Why do the times in the UI differ from my local time?

Release Ops shows times in the time zone your administrator configured. The default is UTC. Times use the 24-hour clock.

## How do I upgrade Release Ops?

Pull the newer image and recreate the container. Your data stays in the volume, and pending database migrations run on start. Read the upgrade notes before you upgrade: [Upgrading in Getting started](../getting-started/#upgrading) and [Upgrading in the README](https://github.com/mdg-labs/release-ops/blob/dev/README.md#upgrading).

## Where do I report a problem or ask for a feature?

Open an issue on [GitHub](https://github.com/mdg-labs/release-ops/issues).
