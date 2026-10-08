---
title: "Open-ticket policy"
description: "What Release Ops does when a new release ships while the previous ticket is still open."
---

When a new release appears and the repo's previous ticket is still open, the open-ticket policy decides what happens. You set it per [ticket project](../../guide/ticket-projects/).

The policy applies only when the previous ticket is open according to the [status mapping](../status-mapping/). If there is no previous ticket, or it is done or cancelled, Release Ops simply creates a new ticket.

## Supersede

Supersede is the default. Release Ops creates a ticket for the new release and retires the old one:

1. It creates the new ticket.
2. It sets the old ticket to the **Superseded status** of the ticket project.
3. It adds a comment to the old ticket that names the old and new release and links to the new ticket.

Use it when you want one live ticket per repo and a trail from old tickets to new ones. You can change the comment text with the **Supersede comment template** on the ticket project.

If the old ticket cannot be updated, the new ticket still exists. The poll run records the failure as an error. See [Poll runs](../../guide/poll-runs/).

## Merge

Release Ops does not create a second ticket. It updates the open ticket's title and description to describe the new release. The ticket then tracks the new release.

Use it when you want a single ticket per repo that always shows the current target version.

## Skip if open

Release Ops creates no ticket and changes nothing in your ticket system. It records the new release as seen, so it does not come back to it. When you close the open ticket, the next ticket is created for the next release.

Use it when you only want a new ticket after the previous one has been handled.

## Compare

| | Supersede | Merge | Skip if open |
| - | --------- | ----- | ------------ |
| New ticket | Yes | No | No |
| Old ticket | Moved to the superseded status and commented | Updated in place | Untouched |
| Poll run action | Supersede, then Create ticket | Merge | Skip open ticket |
| Notification | **Supersede** and **Create** events | None | None |

## Related pages

- [Ticket projects](../../guide/ticket-projects/)
- [Status mapping](../status-mapping/)
- [Notifications](../../guide/notifications/)
- [How release detection works](../release-detection/)
