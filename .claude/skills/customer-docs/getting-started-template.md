---
title: "First steps"
description: "From your first sign-in to the first ticket Release Ops creates for a new release."
---

<!-- Starlight renders `title` as the H1: do not add a `# Heading` here. -->
<!-- Save as apps/docs/src/content/docs/guide/first-steps.md. Installation lives in the synced
     Getting started page (source: docs/getting-started.md) — link to it, do not repeat it. -->

This guide takes you from your first sign-in to your first automatically created ticket. It assumes Release Ops is already running; if not, follow [Getting started](../../getting-started/) first.

## What you'll need

- A Release Ops account (the bootstrap admin, or an invitation link from another user).
- An API token for the release source you want to watch (GitHub, GitLab, Gitea, Forgejo or Codeberg).
- An API token for your ticket system (Kaneo, Jira or Linear).

## Quick start

1. **Sign in** — <Brief instruction.> → See [Signing in](../signing-in/).
2. **Add a source integration** — <Brief instruction.> → See [Integrations](../integrations/).
3. **Add a ticket integration** — <Brief instruction, e.g. Kaneo with its base URL and API token.>
4. **Add a ticket project** — <Brief instruction: project/team, status mapping, open-ticket policy.> → See [Ticket projects](../ticket-projects/).
5. **Add a repo** — <Brief instruction: source integration, repository, ticket project.> → See [Repos](../repos/).
6. **Run a poll** — <Brief instruction, e.g. "Click **Run poll now** on the Dashboard.">
7. **Check the result** — <Where the poll run and created ticket show up.> → See [Poll runs](../poll-runs/).

<!-- Keep it to 10 steps or fewer. Take every label from apps/web/messages/en.json and every behaviour from docs/specs.html. -->

**You're ready when:** <first success moment, e.g. "the poll run shows a created ticket and you can open it in your ticket system.">

## Next steps

| Goal | Go to |
| ---- | ----- |
| Get notified when polls create tickets or fail | [Notifications](../notifications/) |
| Invite a teammate | [Users](../users/) |
| Change the poll schedule | [Settings](../settings/) |

## Get help

- [FAQ](../../faq/)
- [Report an issue on GitHub](https://github.com/mdg-labs/release-ops/issues)
