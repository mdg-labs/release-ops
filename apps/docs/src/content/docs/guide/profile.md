---
title: "Profile"
description: "Change your sign-in email address and your password."
---

Check which email address you sign in with, change it, and change your password.

## Prerequisites

- You are signed in to Release Ops.
- To change your email address, your administrator has set up outgoing email for Release Ops. Release Ops sends a confirmation link to your new address.

<!-- SCREENSHOT: Profile page with account, change email and change password panels -->

Open **Profile** from the bottom of the sidebar. The link shows your email address.

## Check your account

The **Account** panel shows your sign-in **Email**. You cannot edit it here. Use **Change email** below.

## Change your email address

1. In the **Change email** panel, enter your **New email**.
2. Enter your current password in **Password confirmation**.
3. Click **Request change**.
4. Open the message sent to your new address and click the confirmation link.

**Result:** Release Ops confirms the change and takes you to the Dashboard. Your sign-in email is now the new address. Your other sessions on other browsers or devices are signed out. The session you are using stays signed in.

Until you click the link, your old address stays active. After you click **Request change**, the panel shows **Verification email sent**. The link expires after the time set for password reset links on [Settings](../settings/).

## Change your password

1. In the **Change password** panel, enter your **Current password**.
2. Enter a **New password** of at least 8 characters.
3. Enter it again in **Confirm new password**.
4. Click **Update password**.

**Result:** **Password updated** appears. You stay signed in.

## Sign out

Click **Log out** at the bottom of the sidebar.

## Field reference

| Field | Description | Required |
| ----- | ----------- | -------- |
| Email | Your current sign-in address. Read-only. | — |
| New email | The address you want to sign in with from now on. | Yes |
| Password confirmation | Your current password, to confirm the email change. | Yes |
| Current password | Your current password, to confirm the password change. | Yes |
| New password | The new password. At least 8 characters. | Yes |
| Confirm new password | The new password again. Must match. | Yes |

## Troubleshooting

### "Current password is incorrect."

Re-enter your current password. If you have forgotten it, sign out and use **Forgot password?** on the sign-in page. See [Signing in](../signing-in/).

### "SMTP is not configured. Ask an administrator to set SMTP environment variables."

Release Ops cannot send email yet, so it cannot send the confirmation link. Ask the person who runs your Release Ops instance to set up outgoing email.

### "Too many requests. Try again later."

You tried the password change too many times in a short time. Wait and try again.

### "Passwords do not match." or "Password must be at least 8 characters."

Enter the same new password in both fields and use at least 8 characters.

### The confirmation link says it is invalid or expired

Request the change again from this page. See [Signing in](../signing-in/).

## Related pages

- [Signing in](../signing-in/)
- [Users](../users/)
- [Settings](../settings/)
