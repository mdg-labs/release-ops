---
title: "Ticket projects"
description: "Define where tickets are created and how they look: the project, a filter, the create defaults, the status mapping and the content templates."
---

A ticket project is a Kaneo project, a Jira project or a Linear team that receives the tickets of your update PRs, together with the rules for them: which PRs get a ticket, how a ticket is created, which statuses mean what, and what its text says. The **Ticket projects** page manages them.

## Prerequisites

- You are signed in as an admin.
- You have added an integration. See [Integrations](../integrations/).

## What viewers and admins see

| | Viewer | Admin |
| --- | ------ | ----- |
| **Ticket projects** in the sidebar | No | Yes |
| The page | **You do not have access to this page**: "This page is for admins. Your role is read-only." | Add, edit and delete ticket projects |

A viewer still sees the state of a ticket on the [Dashboard](../dashboard/), but not the project that holds it.

## Add a ticket project

1. Open **Ticket projects** in the sidebar.
2. Select **Add ticket project**. A panel opens with five tabs.
3. On **General**, choose the **Integration**. For Kaneo, also choose the **Workspace**.
4. Choose the **External project ID**: the project or team of your ticket system. The list is read from the integration.
5. Enter a **Display name**.
6. Open **Create config** and **Status mapping** and complete them. They are required.
7. Optionally set the **Filter** and the **Content templates**.
8. Select **Save**.

**Result:** the project appears in the list. If a tab is incomplete, Release Ops opens that tab and says what is missing. Edit a project later with the edit button in its row. The integration and the external project ID cannot be changed after you save.

## Choose which PRs get a ticket

The **Filter** tab decides which update PRs get a ticket. By default **No restriction is set: every pull request that is not automerged gets a ticket.** With a restriction, only the PRs that pass all of the settings get a ticket. A ticket that already exists is never removed when its PR stops matching.

| Setting | Effect |
| ------- | ------ |
| **Update types** | Tick **Major**, **Minor**, **Patch**, **Digest** or **Lockfile**. A PR needs a dependency of one ticked type. Tick none to allow every type. |
| **Labels** | A comma-separated list, for example `dependencies, security`. A PR needs one of these labels. Leave it empty for no restriction. |
| **Skip automerged pull requests** | On by default. A PR that Renovate merges by itself gets no ticket. |

See [The filter](../../concepts/update-prs-and-tickets/#the-filter) for the exact rules.

## Set the create defaults

The **Create config** tab, headed **Create defaults**, holds what a new ticket starts with. It depends on the kind. The options are read from the integration.

| Kind | Fields |
| ---- | ------ |
| Kaneo | **Default status** and **Default priority**, both required |
| Jira | **Issue type** (required), **Priority** and **Initial status** |
| Linear | **Initial state** (required) and **Priority** |

Select the project on the **General** tab first. Until then the tab says **Select an integration project before configuring defaults and status mapping.**

## Map the statuses

The **Status mapping** tab tells Release Ops which statuses of your ticket system mean **Open statuses**, **Done statuses** and **Cancelled statuses**, and which single **Superseded status** an old ticket moves to. Each of the first three needs at least one status, and the superseded status is required. Status values differ per project, so choose from the options the integration offers. See [Status mapping](../../concepts/status-mapping/) for what each field does.

## Write the ticket text

The **Content templates** tab has four templates:

| Template | Used |
| -------- | ---- |
| **Title template** | When the ticket is created and updated. |
| **Description template** | When the ticket is created and updated. Markdown for Kaneo and Linear, plain text for Jira. |
| **Update comment template** | Added to the ticket when its PR gets a new version. |
| **Supersede comment template** | Added to the old ticket when a newer PR replaces it. |

Leave a field empty to use the integration's default, which is shown in grey inside the field. Under each field, **Preview with sample data** renders your template. When a template uses syntax the preview cannot show, or a variable its field does not have, the preview says "No preview". The server checks every template when you save, and refuses one that is not valid with a message.

Templates use Go `text/template` syntax. Select **Copy** next to a variable to copy it, then paste it into a template. The **Template variables** table lists them:

| Variable | Description | Works in |
| -------- | ----------- | -------- |
| `{{ .PullRequest.URL }}` | The PR's web URL | All four |
| `{{ .PullRequest.Number }}` | The PR number | All four |
| `{{ .PullRequest.Title }}` | The PR title as GitHub reports it | All four |
| `{{ .Repository }}` | The repository's full name, `owner/repo` | All four |
| `{{ .Dependencies }}` | The PR's dependencies, a list to loop over with `range` | All four |
| `{{ .Name }}` | Inside the loop: the dependency's name | All four |
| `{{ .From }}` | Inside the loop: the current version, empty when unknown | All four |
| `{{ .To }}` | Inside the loop: the new version, empty when unknown | All four |
| `{{ .UpdateType }}` | Inside the loop: major, minor, patch, digest, lockfile or other, empty when unknown | All four |
| `{{ .ReleaseNotes }}` | The release notes from the PR body, truncated | Title, Description and Update comment |
| `{{ .Supersede.NewPullRequest.URL }}` | The web URL of the PR that replaced this one | Supersede comment |
| `{{ .Supersede.NewPullRequest.Number }}` | The number of the PR that replaced this one | Supersede comment |
| `{{ .Supersede.NewPullRequest.Title }}` | The title of the PR that replaced this one | Supersede comment |
| `{{ .Supersede.NewTicketURL }}` | The web URL of the replacing PR's ticket, empty when it has none | Supersede comment |

A list of dependencies looks like this:

```text
{{ range .Dependencies }}- {{ .Name }}: {{ .From }} → {{ .To }}
{{ end }}
```

## Choose which repositories use a project

A repository's tickets go to its own ticket project when it has one, otherwise to the default ticket project. Set them in two places:

- The repository mapping: an admin picks a project for one repository in the **Ticket project** column of [Repositories](../repositories/#choose-the-ticket-project-of-a-repository).
- The default for the installation: an admin picks it under **Default ticket project** on [Settings](../settings/#choose-the-default-ticket-project).

A repository with neither gets no tickets.

## Delete a ticket project

1. Select the delete button in the project's row.
2. Read **Delete ticket project?** and confirm.

**Result:** the project is removed. This cannot be undone. A project that is still in use cannot be deleted: the dialog says "This ticket project is still in use. Remove what references it first, then delete it."

## Field and option reference

| Field / option | Description | Required |
| -------------- | ----------- | -------- |
| **Integration** | The integration the project belongs to. | Yes |
| **Workspace** | Kaneo only. The workspace that holds the project. | Yes for Kaneo |
| **External project ID** | The project, project key or team in your ticket system. | Yes |
| **Display name** | The name shown in Release Ops. | Yes |
| **Filter** | Which PRs get a ticket. | No |
| **Create config** | The defaults of a new ticket. | Yes |
| **Status mapping** | Which statuses mean open, done, cancelled and superseded. | Yes |
| **Content templates** | The text of the ticket and its comments. | No |

## What the page looks like when nothing is there

| State | What you see |
| ----- | ------------ |
| Loading | Grey placeholder rows. |
| No ticket projects | **No ticket projects yet**: "Define a project target with status mapping and ticket content." with an **Add ticket project** button. |
| No integration | In the panel: **Add a Kaneo, Jira, or Linear integration first.** |
| Options loading or failing | **Loading options from the integration…**, **Could not load options from the integration.** or **No options returned for this integration.** |
| An error | **Failed to load ticket projects.** |

## Troubleshooting

### The form says what is missing

Messages such as **Display name is required.**, **External project ID is required.**, **Select a workspace.**, **Complete the create defaults for this provider.** or **At least one open status is required.** name the tab to fix. A tab with a problem is marked in red.

### A ticket was not created for a PR

Check the PR against the project's filter, and that the repository has this project or a default. See [Which PRs get a ticket](../../concepts/update-prs-and-tickets/#which-prs-get-a-ticket).

### Ticket error on the Dashboard

A status that is in no list of the status mapping cannot be sorted. Add it to the right list. See [Status mapping](../../concepts/status-mapping/#why-the-lists-hold-more-than-one-status).

## Related pages

- [Integrations](../integrations/)
- [Repositories](../repositories/)
- [Settings](../settings/)
- [Status mapping](../../concepts/status-mapping/)
- [Update PRs and tickets](../../concepts/update-prs-and-tickets/)
