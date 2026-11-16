---
title: "FAQ"
description: "Answers to common questions about running Release Ops: URLs and webhooks, duplicate PRs, permissions, sign-in and what is not supported yet."
---

## Setup and network

### Why does Release Ops need a public `WEBHOOK_URL` and an `APP_URL`?

They are two different addresses with two different jobs.

- `APP_URL` is the address your browser uses to open Release Ops. GitHub sends your browser back to it during setup and sign-in. GitHub itself never calls it, so it only has to be reachable from the browsers of the people who sign in. It is required.
- `WEBHOOK_URL` is the address GitHub calls to deliver events: a ticked checkbox, a closed PR, a push. It must be reachable from GitHub over HTTPS and it ends in `/hooks/github`. It is optional.

With a webhook, a run starts within seconds of the event. Release Ops reads both addresses when you create the GitHub App, and changing them later does not change the App: after a new `WEBHOOK_URL`, edit the App's webhook URL on GitHub, and after a new `APP_URL`, edit its callback URL, or sign-in fails. See [Getting started](../getting-started/#1-choose-your-urls).

### What happens if I have no public webhook URL?

Release Ops still works, in a degraded polling mode. Leave `WEBHOOK_URL` unset, for example at home or behind NAT. GitHub then sends no events, and Release Ops checks GitHub every few minutes for ticked checkboxes, closed PRs and pushes. Reactions take minutes, not seconds, and a tick made while a run is already going can be missed. The hourly run and an admin's run now work as usual. The **Settings** page shows **Polling mode**. See [Without a public URL: polling mode](../getting-started/#without-a-public-url-polling-mode).

### Why do I get two update PRs for the same dependency?

Renovate's hosted app from Mend is probably installed on the same repositories as Release Ops. Both bots read the same `renovate.json`, so each opens its own update PR for the same update. Release Ops tracks and creates tickets only for the PRs of its own App. The other bot's PRs get no ticket.

Pick one of these:

- Remove those repositories from the Mend app in your GitHub organization's or account's installed apps. Release Ops then keeps the update PRs.
- Or turn Release Ops off for them: remove the repositories from the Release Ops App's installation on GitHub.

Close the duplicate PRs of the bot you stop using and delete their branches. The same happens with a Renovate GitHub Action or another Renovate that is still running: see [Migrating from a Renovate GitHub Action or self-hosted Renovate](../guide/migrating-from-renovate/).

## The GitHub App

### Which permissions does the App request?

The App requests the permissions Renovate documents for a GitHub App, plus one for sign-in. You see all of them on GitHub's confirmation page before the App exists.

| Permission | Access |
| ---------- | ------ |
| Administration | Read |
| Checks | Read and write |
| Commit statuses | Read and write |
| Contents | Read and write |
| Dependabot alerts | Read |
| Issues | Read and write |
| Metadata | Read |
| Pull requests | Read and write |
| Workflows | Read and write |
| Merge queues | Read and write |
| Organization members | Read |

**Organization members** is the one that Renovate does not need: Release Ops uses it to work out who may sign in and with which role. **Workflows** lets Renovate update files under `.github/workflows`. The App subscribes to push, pull request, issues and installation events. The App is private, so only its owner can install it, and an instance has one installation.

### Why are the update PRs authored by the bot?

The App is the bot. Renovate runs with the App's installation token, so the branches, PRs and PR comments come from the App's bot user, shown on GitHub as `<app name>[bot]`. Release Ops identifies its update PRs and the Dependency Dashboard by that author. It does not use the branch prefix or the PR title, which your Renovate configuration can change. Commits that Renovate creates through the GitHub API are signed by GitHub as the App.

### Why is there no local login?

GitHub is the only identity provider. You sign in with GitHub through the instance's own App, and your role is derived from your GitHub membership: admin or viewer. There are no passwords, no registration, no invitations and no email, so there is nothing to reset and no second user list to keep in step. See [Roles](../concepts/roles/).

### I cannot sign in because GitHub sign-in is broken. How do I get back in?

Use the break-glass login link. You need shell access to the app container, which already implies admin rights over the instance. Run:

```bash
docker compose exec app release-ops admin login-link
```

The command prints a one-time link to your terminal. Open it in your browser. The link:

- signs you in as the bootstrap admin, with the admin role,
- works once and expires after 15 minutes,
- opens a session that ends after 8 hours, like any other.

The command refuses while no bootstrap admin has signed in yet, so pin one with `BOOTSTRAP_ADMIN_GITHUB_LOGIN` and sign in once with it before you need the link. See [Getting started](../getting-started/#8-sign-in).

## What Release Ops does and does not do

### Why were release polling and notifications removed?

Release Ops 2 does not watch upstream releases. Renovate discovers updates and opens the PRs, and Release Ops creates a ticket for each update PR. A ticket exists because an update PR exists.

It also sends no notifications of its own: no email, chat or webhook messages. Your ticket system already notifies people when a ticket is created or assigned, and GitHub notifies them about PRs. To be alerted when Release Ops itself fails, scrape its metrics and use the [example alert rules](../metrics/#example-alert-rules).

### Does Release Ops support private registries?

Not yet. Renovate needs credentials to read a private registry, the global Renovate config must not hold secrets, and Release Ops has no other place for registry credentials yet. Support is planned for a later version.

### Does Release Ops support GitLab?

Not yet. Release Ops works with github.com and one GitHub App. Other platforms are planned for later versions.

## Related pages

- [Getting started](../getting-started/)
- [How runs are triggered](../concepts/how-runs-are-triggered/)
- [Metrics and alert rules](../metrics/)
