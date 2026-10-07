---
title: "Settings"
description: "Set how often Release Ops polls and how long invitation and reset links stay valid."
---

Set how often Release Ops checks your repos and how long invitation and password-reset links work. These settings apply to the whole instance.

## Prerequisites

- You are signed in to Release Ops.

<!-- SCREENSHOT: Settings page with polling and token expiry sections -->

The **Settings** page has two sections, linked at the top: **General**, described here, and **Users**. See [Users](../users/).

## Change the poll interval

1. Open **Settings**. The **General** section is selected.
2. Under **Polling**, enter the **Poll interval (minutes)**. The minimum is 5 minutes.
3. Click **Save**.

**Result:** **Settings saved** appears. The new interval applies to the next scheduled run. By default Release Ops polls every 360 minutes (6 hours).

Release Ops does not poll when it starts. The first scheduled poll comes one full interval after the start. To check right away, use **Run poll now** on the [Dashboard](../dashboard/).

## Change how long links stay valid

1. Under **Token expiry**, enter the **Invitation link expiry (hours)**. Use a value from 1 to 720. The default is 168 hours (7 days).
2. Enter the **Password reset link expiry (minutes)**. Use a value from 5 to 1440. The default is 60 minutes.
3. Click **Save**.

**Result:** **Settings saved** appears. New links use the new lifetime. Links that already exist keep theirs.

The password reset lifetime also applies to the confirmation link Release Ops sends when someone changes their email address on the [Profile](../profile/) page.

## Field reference

| Field | Description | Required |
| ----- | ----------- | -------- |
| Poll interval (minutes) | Time between automatic polls. Minimum 5 minutes. | Yes |
| Invitation link expiry (hours) | How long an invitation link works. Between 1 and 720 hours. | Yes |
| Password reset link expiry (minutes) | How long a password reset or email change link works. Between 5 and 1440 minutes. | Yes |

## Troubleshooting

### Save shows a validation message

The value is outside its range. **Poll interval must be at least 5 minutes.** The invitation expiry must be between 1 and 720 hours. The password reset expiry must be between 5 and 1440 minutes.

### The new interval does not seem to apply

The setting takes effect on the next scheduled run. Check **Poll interval** on the [Dashboard](../dashboard/) to confirm the saved value.

## Related pages

- [Dashboard](../dashboard/)
- [Users](../users/)
- [Profile](../profile/)
