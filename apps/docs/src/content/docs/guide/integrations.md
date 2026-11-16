---
title: "Integrations"
description: "Connect Release Ops to Kaneo, Jira or Linear, test the connection and replace or remove credentials."
---

The **Integrations** page holds the connections to your ticket systems. Release Ops creates the tickets of your update PRs through them. Each integration is one Kaneo, Jira or Linear account with its credentials.

## Prerequisites

- You are signed in as an admin.
- You have the credentials for your ticket system: an API key for Kaneo or Linear, or an email and an API token for Jira, and the URL of the system for Kaneo and Jira.

## What viewers and admins see

| | Viewer | Admin |
| --- | ------ | ----- |
| **Integrations** in the sidebar | No | Yes |
| The page | **You do not have access to this page**: "This page is for admins. Your role is read-only." | Add, edit, test and delete integrations |

Integrations are admin only because they hold credentials. Release Ops stores them encrypted and never shows them again: the page shows only whether a secret is configured.

## Add an integration

1. Open **Integrations** in the sidebar.
2. Select **Add integration**.
3. Choose the **Kind**: **Kaneo**, **Jira** or **Linear**.
4. Enter a **Name** that tells your integrations apart.
5. Fill in the fields of the kind. See the table below.
6. Select **Save**.

**Result:** the integration appears in the list with **Configured** in the **Secret** column. Next, add a [ticket project](../ticket-projects/) that uses it.

| Kind | Fields |
| ---- | ------ |
| **Kaneo** | **Base URL** (`https://cloud.kaneo.app` for Kaneo cloud, or your own instance's URL, to which `/api` is added automatically) and **API key**. |
| **Jira** | **Base URL**, **Email** and **API token**. |
| **Linear** | **API key**. Linear has no base URL. |

## Test the connection

1. Select the test connection button in the integration's row, or open the integration and select **Test connection**.

**Result:** a message shows **Testing connection**, then either **Connection successful** (the provider accepted the stored credentials) or **Connection failed**, with the reason the provider gave, when it gives one.

## Edit an integration

1. Select the edit button in the integration's row.
2. Change the **Name** or the **Base URL**.
3. To replace the credentials, enter the new value in the secret field. Leave it empty to keep the stored secret.
4. Select **Save**.

Rules for an edit:

- The **Kind** cannot be changed.
- For Jira, enter the **Email** again when you replace the **API token**.
- When you change the **Base URL**, enter the token or API key again. This stops the stored secret from being sent to a new host.

## Delete an integration

1. Select the delete button in the integration's row.
2. Read **Delete integration?** and confirm.

**Result:** the integration is removed. It cannot be undone. An integration that ticket projects still use cannot be deleted: the dialog says "This integration is still used by ticket projects. Remove it from those ticket projects first, then delete it."

## Field and option reference

| Field / option | Description | Required |
| -------------- | ----------- | -------- |
| **Kind** | **Kaneo**, **Jira** or **Linear**. Fixed after you save. | Yes |
| **Name** | The name shown in the list and on the **Ticket projects** page. | Yes |
| **Base URL** | The URL of your Kaneo or Jira. Shown as `—` for Linear. | Yes for Kaneo and Jira |
| **Email** | The Jira account's email. | Yes for Jira, when you set a token |
| **API key** / **API token** / **Token** | The credential. Never shown after you save. | Yes when you add; no when you edit |
| **Secret** (column) | **Configured** or **Missing**. | — |

## What the page looks like when nothing is there

| State | What you see |
| ----- | ------------ |
| Loading | Grey placeholder rows. |
| No integrations | **No integrations yet**: "Connect a ticket provider to get started." with an **Add integration** button. |
| An error | **Failed to load integrations.** |

## Troubleshooting

### A form field is refused

The form tells you what is missing: **Name is required.**, **Base URL is required for this kind.**, **Email is required for Jira.**, **Token or API key is required.**, **Enter the Jira email when you replace the API token.** or **Re-enter the token or API key when you change the base URL.**

### Connection failed

The provider rejected the stored credentials, or could not be reached. Edit the integration, enter a new key or token and test again.

### The Secret column says Missing

The integration has no stored credential. Edit it and enter one.

### Ticket creation fails

Open **Jobs**, then **Problems**. A **Ticket error** problem shows the error text to admins. See [Jobs and Problems](../jobs/).

## Related pages

- [Ticket projects](../ticket-projects/)
- [Update PRs and tickets](../../concepts/update-prs-and-tickets/)
- [Settings](../settings/)
