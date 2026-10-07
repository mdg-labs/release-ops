---
title: "Signing in"
description: "Sign in, accept an invitation, reset a forgotten password and confirm an email change."
---

Sign in to Release Ops, join through an invitation, and recover or update your account with the links Release Ops emails you.

## Prerequisites

- You have an account. The first account is created during setup (see [Getting started](../../getting-started/)). Everyone else joins through an invitation. There is no public sign-up.

<!-- SCREENSHOT: Log in page -->

## Sign in

1. Open Release Ops in your browser. If you are not signed in, you land on the **Log in** page.
2. Enter your **Email** and **Password**.
3. Click **Sign in**.

**Result:** You land on the [Dashboard](../dashboard/), or on the page you tried to open.

Wrong credentials show **Invalid email or password.** After too many attempts in a short time you see **Too many requests. Try again later.** Wait a moment and try again.

## Accept an invitation

An existing user sends you an invitation link, by email or by a copied link. See [Users](../users/).

1. Open the link. The **Accept invitation** page opens.
2. Enter a **Password** of at least 8 characters.
3. Enter it again in **Confirm password**.
4. Click **Set password**.

**Result:** Your account is created and you are signed in.

If the page says **This link is invalid or has expired.**, ask the person who invited you for a new link.

## Reset a forgotten password

1. On the **Log in** page, click **Forgot password?**.
2. Enter your **Email** and click **Send reset link**.
3. Open the email and click the link. The **Reset password** page opens.
4. Enter a new **Password** of at least 8 characters, enter it again in **Confirm password** and click **Set password**.

**Result:** Your password is changed and you are signed in.

The page after step 2 always reads **If an account exists for that email, a reset link has been sent.** It does not tell you whether the address has an account. Release Ops sends the email only when an account exists and your administrator has set up outgoing email. The link works for the time set on [Settings](../settings/), 60 minutes by default. A new request replaces earlier unused reset links for the same address.

## Confirm an email change

When you change your email address on the [Profile](../profile/) page, Release Ops sends a link to the new address. Open it. Release Ops completes the change, signs you in in that browser, even if you were signed out, and takes you to the Dashboard.

If the page says **This link is invalid or has expired.**, request the change again on the Profile page.

## Sign out

Click **Log out** at the bottom of the sidebar.

## Field reference

| Field | Description |
| ----- | ----------- |
| Email | The address you sign in with. |
| Password | Your password. New passwords need at least 8 characters. |
| Confirm password | The new password again. Must match. |

## Troubleshooting

### I did not receive a reset email

Check your spam folder and the address you entered. If your instance has no outgoing email set up, Release Ops cannot send the email. Ask the person who runs your instance.

### "Passwords do not match." or "Password must be at least 8 characters."

Enter the same password in both fields and use at least 8 characters.

### The link opens "Link invalid" or "Request failed"

**Link invalid** appears when the address has no token, for example when the link was cut off when copied. **Request failed** with **This link is invalid or has expired.** appears when the link has expired or was already used. Invitation, reset and email change links work once and expire. Request a new one, or copy the full link from the email.

## Related pages

- [Profile](../profile/)
- [Users](../users/)
- [First steps](../first-steps/)
