---
title: "How release detection works"
description: "How Release Ops decides that a release is new, and why the first poll creates no ticket."
---

On every poll, Release Ops looks at the latest release of each enabled repo and compares it with the release it remembers. This page explains the outcomes and what decides between them.

## What counts as the latest release

By default, Release Ops asks the source for the latest stable release. Pre-releases and drafts do not count.

If you switch on **Include pre-releases** for a repo, Release Ops looks at the repo's releases and picks the newest one by publish date, pre-releases included. Drafts never count. The setting applies from the next poll.

## The decision

For each repo, Release Ops compares the latest release tag with the last known tag:

| Situation | What Release Ops does | Event |
| --------- | --------------------- | ----- |
| No last known tag yet | Remembers the tag. Creates no ticket. | Baseline |
| Same tag as last time | Nothing. | Skip |
| A different tag | Checks the previous ticket, then creates, supersedes, merges or skips. See [Open-ticket policy](../open-ticket-policy/). | Create ticket, Supersede, Merge or Skip open ticket |
| The source reports an error | Records the error on the repo and carries on with the next repo. | Error |

Release Ops compares tags as exact text. It does not rank versions. Any tag that differs from the last known one counts as new, even if it looks older.

## The baseline

The first release Release Ops sees for a repo is recorded as a baseline and creates no ticket. Without this rule, adding a repo would open a ticket for a release that has been out for months.

- If a project has no release yet, Release Ops treats that as "no release", not as an error. The first release that appears later becomes the baseline. The one after it creates a ticket.
- If you change a repo's **Source**, **Project path** or **Source integration**, the known release is reset. The next poll is a new baseline and creates no ticket.

## What happens with the previous ticket

Before it acts on a new tag, Release Ops asks your ticket system for the current status of the repo's previous ticket. It does not rely on its own records, because someone may have closed the ticket by hand.

- The ticket is **done** or **cancelled** according to the [status mapping](../status-mapping/): Release Ops creates a new ticket.
- The ticket is **open**: the ticket project's [open-ticket policy](../open-ticket-policy/) decides.
- The ticket is in the **Superseded status**: it counts as cancelled, so Release Ops creates a new ticket.
- The status is in none of the lists and is not the **Superseded status**, or the ticket system cannot be reached: Release Ops reports an error for the repo and creates no ticket.

## One ticket per release

Release Ops writes down which release it is about to ticket before it asks your ticket system to create the ticket. After the ticket exists, it saves the ticket's ID and clears that note. If saving the ID fails, the note stays. On the next poll Release Ops sees it, creates no second ticket and reports an error that a ticket for that release may already exist. See [Poll runs](../../guide/poll-runs/) for what to do.

What happens to the note when the ticket system reports a failure while creating a ticket depends on whether the ticket may exist:

- The ticket system refused the request with a 4xx answer, for example because the credentials or the project are wrong, or because it asked Release Ops to try later (408, 429). Or the request never reached it: the host name does not resolve, the connection fails straight away (it is refused, or the network or host is unreachable), or the failure happened before Release Ops sent the request. No ticket exists. Release Ops clears the note and tries again on the next poll.
- Any other failure leaves it unclear whether the ticket exists: a server error (5xx), a timeout, including one while Release Ops is still connecting, an answer Release Ops cannot read or that has no ticket ID, a connection that breaks after it was opened, or a Release Ops shutdown while the request was in flight. A TLS or certificate failure, or a failure to connect through a proxy, also counts here, although the request most likely never reached the ticket system. Release Ops keeps the note. The next poll creates no ticket and reports that a ticket may already exist, so you can check the ticket system.

## When polls run

Release Ops polls on the interval you set under [Settings](../../guide/settings/), and whenever you click **Run poll now**. Only one poll runs at a time. Release Ops does not poll when it starts, so the first scheduled poll comes one full interval later.

## Related pages

- [Repos](../../guide/repos/)
- [Poll runs](../../guide/poll-runs/)
- [Status mapping](../status-mapping/)
