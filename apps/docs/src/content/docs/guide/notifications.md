---
title: "Notifications"
description: "Get a message in Slack, ntfy or another service when tickets are created, superseded or a poll fails."
---

Send a message to a chat or push service when Release Ops creates a ticket, supersedes one, or hits an error while polling.

## Prerequisites

- You are signed in to Release Ops.
- You have a Shoutrrr URL for the service you want to notify. Shoutrrr is the library Release Ops uses to send messages. It supports services such as Slack and ntfy, for example `slack://token@channel` or `ntfy://topic`.

<!-- SCREENSHOT: Notifications page with targets -->

## Add a notification target

1. Open **Notifications** and click **Add notification target**.
2. Enter a **Name**, for example `Slack - releases`.
3. Enter the **Shoutrrr URL**. The field hides what you type, because the URL usually contains a secret.
4. Under **Events**, select the poll events you want to hear about: **Create**, **Error** and **Supersede**. All three are selected by default.
5. Keep **Enabled** on.
6. Click **Save**.
7. Back in the list, click the **Test notification** icon in the **Actions** column to check the target.

**Result:** The target appears in the list with a **Configured** badge in the **URL** column. Release Ops stores the URL encrypted and never shows it again.

## Test a target

1. Click the **Test notification** icon in the **Actions** column. You can also click **Test notification** at the bottom of the edit panel.
2. Wait for the message in the bottom-right corner of the page.

**Result:** **Test notification sent** means the service accepted a test message. **Test notification failed** means Shoutrrr could not deliver it. The test uses the saved URL, so save your changes first.

## Edit a target

1. Click the **Edit** icon in the **Actions** column.
2. Change the name, the events or the **Enabled** switch.
3. To replace the URL, enter the new one. Leave the field blank to keep the existing URL.
4. Click **Save**.

## Pause a target

Switch **Enabled** off in the edit panel and click **Save**. Release Ops skips disabled targets. The list shows **Disabled** in the **Enabled** column.

## Delete a target

1. Click the **Delete** icon in the **Actions** column.
2. Click **Delete** to confirm.

## What gets sent

| Event | Sent when |
| ----- | --------- |
| Create | A ticket is created for a new release. |
| Supersede | An older open ticket is superseded by a new one. |
| Error | A repo could not be polled, or its ticket could not be created or updated. |

Release Ops does not send a message for a baseline, a skipped poll, a merge or a skipped release because a ticket is still open.

Every enabled target that subscribes to the event gets the message. If you select notification targets on a [repo](../repos/), only those targets get that repo's messages. A message that cannot be delivered does not stop the poll.

## Field reference

| Field | Description | Required |
| ----- | ----------- | -------- |
| Name | A display name for the list and the repo form. | Yes |
| Shoutrrr URL | Where messages go. Stored encrypted and not shown again. | Yes |
| Events | **Create**, **Error** and **Supersede**. Select at least one. | Yes |
| Enabled | Disabled targets receive no messages. | No |

The list shows **Name**, **Events**, **Enabled**, **URL** and **Actions**.

## Troubleshooting

### Test notification failed

The service did not accept the message. Check the Shoutrrr URL against the service's format and make sure the token and channel or topic are valid. Enter the corrected URL and save before you test again.

### A repo sends no messages

Check that the target is enabled and subscribes to the event. If the repo has specific notification targets selected, only those receive its messages. Edit the [repo](../repos/) to check the selection.

### I get no message for the first poll of a repo

That is expected. The first poll records a baseline and creates no ticket, so there is nothing to announce. See [How release detection works](../../concepts/release-detection/).

## Related pages

- [Repos](../repos/)
- [Poll runs](../poll-runs/)
