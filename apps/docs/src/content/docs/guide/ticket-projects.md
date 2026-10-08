---
title: "Ticket projects"
description: "Define where tickets are created, how statuses are read and what happens when a ticket is still open."
---

Define the Kaneo project, Jira project or Linear team that receives tickets, the status mapping Release Ops uses to read ticket states, and the policy for releases that arrive while a ticket is still open.

## Prerequisites

- You are signed in to Release Ops.
- You have added a ticket integration (Kaneo, Jira or Linear) on the [Integrations](../integrations/) page. The integration needs working credentials, because the form loads its options from your ticket system.

<!-- SCREENSHOT: Ticket projects list -->

## Add a ticket project

One ticket integration can serve several ticket projects, for example one per team. Each ticket project has its own defaults, status mapping and policy.

1. Open **Ticket projects** and click **Add ticket project**.
2. On the **General** tab, choose the **Integration**.
3. For Kaneo, choose the **Workspace**.
4. Choose the **External project ID**. The list shows the projects or teams of your ticket system. Choose the project before you fill in the other tabs. If you change it later, Release Ops resets the **Create config** and **Status mapping** tabs.
5. Enter a **Display name**. Repos show this name when you pick a ticket project.
6. Choose the **Open ticket policy**. **Supersede** is the default. See [Open-ticket policy](../../concepts/open-ticket-policy/).
7. Open the **Create config** tab and fill in the defaults for new tickets (see the field reference below).
8. Open the **Status mapping** tab and select the statuses for **Open statuses**, **Done statuses** and **Cancelled statuses**, then choose the **Superseded status**. See [Status mapping](../../concepts/status-mapping/).
9. Optional: open the **Content templates** tab to change how tickets read.
10. Click **Save**.

**Result:** The ticket project appears in the list, which shows **Integration**, **Name**, **External project ID** and **Policy**.

If a tab is incomplete, Release Ops opens it, marks it in red and shows what is missing.

## Edit a ticket project

1. Click the **Edit** icon in the **Actions** column.
2. Change the display name, the policy, the create config, the status mapping or the content templates.
3. Click **Save**.

The integration and the external project ID are fixed. To point at a different project, add a new ticket project.

## Change the ticket text

Open the **Content templates** tab. It has three fields:

- **Title template**: the title of a new ticket. It is also used when a merge updates an existing ticket.
- **Description template**: the body of a ticket.
- **Supersede comment template**: the comment Release Ops adds to the old ticket when it supersedes it.

Leave a field empty to use the default for your ticket system. The default text shows in grey inside the field. Fill a field to replace the default.

Write placeholders in double braces, for example `{{ .Release.Tag }}`. The table under the fields lists every variable. Click the **Copy** icon in a row to copy the placeholder, then paste it into a field.

| Variable | Description | Works in |
| -------- | ----------- | -------- |
| `{{ .Repo.SourceKind }}` | Source of the repo, such as `github` | Title, Description, Supersede comment |
| `{{ .Repo.ProjectPath }}` | Project path of the repo | Title, Description, Supersede comment |
| `{{ .Repo.URL }}` | Repository web address | Title, Description, Supersede comment |
| `{{ .Release.Tag }}` | Tag of the new release | Title, Description, Supersede comment |
| `{{ .Release.Name }}` | Release name from the source | Title, Description, Supersede comment |
| `{{ .Release.URL }}` | Release page address | Title, Description, Supersede comment |
| `{{ .Release.Notes }}` | Release notes or changelog text | Title, Description, Supersede comment |
| `{{ .Release.PublishedAt }}` | Release time (UTC) | Title, Description, Supersede comment |
| `{{ .Release.IsPrerelease }}` | Whether the release is a pre-release (true or false) | Title, Description, Supersede comment |
| `{{ .Previous.Tag }}` | The last known tag before this poll | Title, Description, Supersede comment |
| `{{ .Supersede.OldTag }}` | Tag of the ticket being superseded | Supersede comment |
| `{{ .Supersede.NewTag }}` | Tag of the new release | Supersede comment |
| `{{ .Supersede.NewTicketURL }}` | Address of the ticket created during the supersede | Supersede comment |

Use `{{ .Previous.Tag }}` in the title and description of a new ticket. The **Supersede** variables only work in the supersede comment.

## Delete a ticket project

1. Click the **Delete** icon in the **Actions** column.
2. Click **Delete** to confirm.

Release Ops refuses to delete a ticket project that repos still use, and the dialog tells you so. Edit each of those repos to use another ticket project, or delete the repos, then delete the ticket project.

## Field reference

| Field | Description | Required |
| ----- | ----------- | -------- |
| Integration | The ticket integration. Fixed after you create the ticket project. | Yes |
| Workspace | The Kaneo workspace. Kaneo only, when you add a ticket project. | Yes, for Kaneo |
| External project ID | The Kaneo project, Jira project or Linear team. Fixed after you create the ticket project. | Yes |
| Display name | The name shown on this page and in the repo form. | Yes |
| Open ticket policy | **Supersede**, **Merge** or **Skip if open**. | Yes |
| Default status | Kaneo: the board column new tickets start in. | Yes, for Kaneo |
| Default priority | Kaneo: the priority of new tickets. | Yes, for Kaneo |
| Issue type | Jira: the type of new issues. | Yes, for Jira |
| Priority | Jira and Linear: the priority of new tickets. | No |
| Initial status | Jira: the status new issues start in. | No |
| Initial state | Linear: the workflow state of new issues. | Yes, for Linear |
| Open statuses | Statuses that mean the ticket is still open. | Yes, at least one |
| Done statuses | Statuses that mean the ticket is done. | Yes, at least one |
| Cancelled statuses | Statuses that mean the ticket was cancelled. | Yes, at least one |
| Superseded status | The single status Release Ops sets on an old ticket when it supersedes it. | Yes |
| Title template, Description template, Supersede comment template | Text templates for tickets. Empty means the default. | No |

## Troubleshooting

### The options lists are empty or show an error

Release Ops loads projects, statuses and priorities from your ticket system with the integration's saved credentials. The form shows **Could not load options from the integration** or the message your ticket system returned. Open [Integrations](../integrations/), use **Test connection**, and fix the credentials. If the list is empty without an error, the ticket system returned no options for that project.

### A poll reports an error for a repo that uses this ticket project

Read the error on the [Dashboard](../dashboard/) or in the poll run details. Common causes are a ticket status that is not in any of your three status lists, and a template with a syntax error. Release Ops does not fall back to the default text when a template is invalid. It reports an error for that repo until you fix the template.

### The ticket project cannot be deleted

Repos still use it. See [Delete a ticket project](#delete-a-ticket-project).

## Related pages

- [Status mapping](../../concepts/status-mapping/)
- [Open-ticket policy](../../concepts/open-ticket-policy/)
- [Integrations](../integrations/)
- [Repos](../repos/)
