---
title: "Integrations"
description: "Store the credentials Release Ops uses to read releases and create tickets."
---

Connect Release Ops to the services it reads releases from (GitHub, GitLab, Gitea, Forgejo, Codeberg) and the services it creates tickets in (Kaneo, Jira, Linear).

## Prerequisites

- You are signed in to Release Ops.
- You have an API token for each service you want to connect. The token is optional for GitHub, Gitea, Forgejo and Codeberg. For Jira you also need the email address of the token's owner.

<!-- SCREENSHOT: Integrations page with a source and a ticket integration -->

## Source and ticket integrations

An integration stores the credentials for one service. There are two kinds:

- **Source integrations** (GitHub, GitLab, Gitea, Forgejo, Codeberg) let Release Ops read releases. A [repo](../repos/) uses one source integration.
- **Ticket integrations** (Kaneo, Jira, Linear) let Release Ops create tickets. You pick the project or team on the [Ticket projects](../ticket-projects/) page, and one ticket integration can serve several ticket projects.

## Add an integration

1. Open **Integrations** and click **Add integration**.
2. Choose the **Kind**. You cannot change the kind later.
3. Enter a **Name** that helps you tell integrations apart.
4. Enter the **Base URL** if the form shows the field. Use the root address of the service, for example `https://gitea.example.com` or `https://kaneo.example.com`. For Kaneo, Release Ops adds `/api` automatically.
5. For Jira, enter the **Email** of the token's owner.
6. Enter the **Token**, **API key** or **API token**. The label depends on the kind. For GitHub, Gitea, Forgejo and Codeberg you can leave it blank to read public repos without a token. A token is required for private repos and recommended for higher rate limits.
7. For a source kind, switch on **Default for this source type** if you want this integration preselected when you add a repo of that source.
8. Click **Save**.

**Result:** The integration appears in the list. Release Ops stores a token you entered encrypted and never shows it again.

## Test a connection

1. Find the integration in the list.
2. Click the **Test connection** icon in the **Actions** column. You can also click **Test connection** at the bottom of the edit panel.

**Result:** A message reports **Connection successful** or **Connection failed**. The test uses the credentials already saved, so save your changes first.

With a token, the test checks that the provider accepts it. For a GitHub, Gitea, Forgejo or Codeberg integration without a token, the test only checks that the provider is reachable. It cannot tell you whether a private repo is readable.

## Edit an integration

1. Click the **Edit** icon in the **Actions** column.
2. Change the name, the base URL or the default switch.
3. To replace the token, enter the new value. Leave the field blank to keep the existing one.
4. Click **Save**.

Two rules apply when you save:

- If you change the **Base URL**, enter the token or API key again.
- For Jira, enter both **Email** and the new **API token** when you replace the credentials. Leave both blank to keep them.

## Delete an integration

1. Click the **Delete** icon in the **Actions** column.
2. Click **Delete** to confirm.

Release Ops refuses to delete an integration that repos or ticket projects still use, and the dialog tells you so. Remove the references first:

- A source integration: edit each repo that uses it and pick another integration, or delete the repo.
- A ticket integration: delete its ticket projects. Before you can delete a ticket project, no repo may use it. See [Ticket projects](../ticket-projects/).

Deleting the default integration of a source leaves that source without a default. Release Ops does not pick a new one.

## Field reference

| Field | Description | Required |
| ----- | ----------- | -------- |
| Kind | The service: GitHub, GitLab, Gitea, Forgejo, Codeberg, Kaneo, Jira or Linear. Fixed after you create the integration. | Yes |
| Name | A display name for the list and for the pickers on other pages. | Yes |
| Base URL | The root address of the service. Shown for GitLab, Gitea, Forgejo, Kaneo and Jira. GitHub, Codeberg and Linear use fixed addresses. | Yes, where shown |
| Email | The email address of the Jira account that owns the API token. Jira only. | Yes, for Jira |
| Token / API key / API token | The credential. It is called **Token** for the source kinds, **API key** for Kaneo and Linear, and **API token** for Jira. | Yes, except for GitHub, Gitea, Forgejo and Codeberg, where a token is required for private repos and recommended for rate limits |
| Default for this source type | Preselects this integration when you add a repo of the same source. Only one integration per source can be the default. Source kinds only. | No |

The list shows **Name**, **Kind**, **Base URL**, **Secret** and **Actions**. The **Secret** column shows **Configured** when a token is stored and **Missing** when none is set. **Missing** is fine for GitHub, Gitea, Forgejo and Codeberg when you only read public repos. A **Default** badge marks the default integration of a source.

## Troubleshooting

### Test connection fails

The provider rejected the saved credentials, or Release Ops could not reach it. Check that the token has not expired and that it can read what you need: releases for a source, tickets for a ticket system. Check the **Base URL** for typos, then enter the token again and save.

### "Enter the Jira email when you replace the API token"

For Jira, the email and the token belong together. Enter both when you replace the credentials.

### "Re-enter the token or API key when you change the base URL"

The form asks for the token again whenever you change the base URL. Enter it and save.

### The integration cannot be deleted

Repos or ticket projects still use it. See [Delete an integration](#delete-an-integration).

## Related pages

- [Ticket projects](../ticket-projects/)
- [Repos](../repos/)
- [First steps](../first-steps/)
