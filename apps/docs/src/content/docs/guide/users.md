---
title: "Users"
description: "Invite teammates, see who has an account and remove users."
---

Invite teammates to Release Ops, track pending invitations and remove accounts that no longer need access.

## Prerequisites

- You are signed in to Release Ops.

<!-- SCREENSHOT: Users page with active users and pending invitations -->

There is no public sign-up. Every account beyond the first one is created through an invitation. All signed-in users have the same permissions, so any user can invite and remove others.

Open **Settings** and select **Users** to reach this page.

## Invite a user

1. Click **Invite user**.
2. Enter the person's **Email**.
3. Click **Create invitation**.
4. Click the **Copy invite link** icon and send the link to the person.
5. Close the dialog.

**Result:** The invitation appears under **Pending invitations** until the person accepts it or it expires.

Copy the link before you close the dialog. Release Ops shows the link only once. If you lose it, revoke the invitation and create a new one.

The person opens the link, chooses a password and is signed in. See [Signing in](../signing-in/).

## Send the invitation by email

If your administrator has set up outgoing email for Release Ops, you can send the invitation from the list:

1. Find the invitation under **Pending invitations**.
2. Click the **Send invitation email** icon.

**Result:** **Invitation email sent** appears. If outgoing email is not set up, the icon is disabled and its tooltip reads **SMTP is not configured. Copy the invite link instead.** See [Getting started](../../getting-started/).

## Revoke an invitation

1. Click the **Revoke invitation** icon in the **Actions** column.
2. Click **Revoke invitation** to confirm.

**Result:** The link stops working.

## Remove a user

1. Under **Active users**, click the **Remove** icon in the **Actions** column.
2. Click **Remove user** to confirm.

**Result:** The user disappears from the list and is signed out everywhere. This cannot be undone.

You cannot remove your own account, and you cannot remove the last remaining user. In both cases the icon is disabled and a tooltip explains why.

## Field reference

| Field | Description |
| ----- | ----------- |
| Email | The sign-in address of a user, or the address an invitation was created for. |
| Joined | When the account was created. Active users only. |
| Expires | When the invitation link stops working. Pending invitations only. The lifetime is set on [Settings](../settings/). |
| You | A badge on your own row. |

## Troubleshooting

### The invitation link does not work

The link may have expired or been revoked, or it may already have been used. Create a new invitation.

### The send email icon is disabled

Outgoing email is not set up. Copy the invite link and send it yourself.

### The person did not receive the invitation email

Ask them to check their spam folder. You can also revoke the invitation and send them a copied link instead.

## Related pages

- [Signing in](../signing-in/)
- [Settings](../settings/)
- [Profile](../profile/)
