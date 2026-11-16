---
title: "Settings"
description: "Set the concurrency cap and the default ticket project, read the global Renovate config, check the App and its installation, see how roles are mapped and replace the App's private key."
---

The **Settings** page shows how the instance is set up and lets an admin change what the web interface owns: the concurrency cap and the default ticket project. It also shows the global Renovate config as a file, the GitHub App and its installation, the role mapping, and holds the form that replaces the App's private key.

## Prerequisites

- You are signed in as an admin.
- The App has been created. Until then, the App sections say **The GitHub App is not set up yet.** See [Getting started](../../getting-started/).

## What viewers and admins see

| | Viewer | Admin |
| --- | ------ | ----- |
| **Settings** in the sidebar | No | Yes |
| The page | **You do not have access to this page**: "This page is for admins. Your role is read-only." | All sections |
| Change a role | No | No. Roles come from GitHub and the page shows them read-only. |

The page never shows a secret. It shows only whether each stored secret is there.

## Set the concurrency cap

The **Updates** section controls how many Renovate jobs run at the same time.

1. Open **Settings** in the sidebar.
2. Enter a whole number of at least 1 in **Concurrency cap (jobs)**.
3. Select **Save**.

**Result:** **Settings saved** appears. The new cap applies to the next job that starts. A running job is never interrupted. A value below 1 is refused with **The concurrency cap must be a whole number of at least 1.** If the save fails, **Failed to save settings.** shows with the reason.

When the instance has no public webhook URL, a **Polling mode** notice sits above this section. It says that GitHub sends no events and that changes reach Release Ops with a delay of minutes. See [Without a public URL: polling mode](../../getting-started/#without-a-public-url-polling-mode).

## Choose the default ticket project

The **Default ticket project** section chooses where the tickets go for repositories that have no project of their own.

1. Open the **Default ticket project** select and choose a project, or **No default**.
2. Select **Save**.

**Result:** **Default ticket project saved** appears. A change applies to tickets created afterwards. To give one repository its own project, use [Repositories](../repositories/#choose-the-ticket-project-of-a-repository).

| Situation | What you see |
| --------- | ------------ |
| The App has no installation yet | "The App has no installation yet, so there is nothing to set a default for." |
| No ticket project exists | "There are no ticket projects yet. Create one to choose it as the default." with a **Go to ticket projects** link |
| The data cannot be read | **Failed to load the installation or the ticket projects.** |

## Read the global Renovate config

The **Global Renovate config** section shows the file as the worker last used it, as plain text. Release Ops never parses or evaluates it, and token-shaped strings are hidden. The page cannot edit it: change the file where it lives. See [The global Renovate config](../../concepts/global-renovate-config/).

| Row | Meaning |
| --- | ------- |
| **Source** | **Repository** followed by the repository and the ref, **Repository** at its default branch, **A file mounted into the worker**, or **Not known yet**. |
| **Last used** | When a job last used the file. |
| The text box | **Config file contents**, scrollable and read-only. |

Before the first job has returned the config, the section says "No job has returned the config yet. It appears here after the first run." If the section cannot load, it says **Failed to load the global Renovate config.**

## Check the GitHub App and its installation

The **GitHub App** section shows the App this instance runs as.

| Row | Meaning |
| --- | ------- |
| **App ID**, **Slug** | The App's identifiers on GitHub. |
| **Owner** | The account that owns the App, with **personal account** or **organization**. |
| **Webhook** | **Active**, or **Not active (polling mode)**. |
| **Private key**, **Client secret**, **Webhook secret** | **Stored** or **Missing**. |

The **Installation** section shows the account the App is installed on.

| Row | Meaning |
| --- | ------- |
| **Account** | The account and its type. |
| **Status** | **Active**, **Suspended** or **Removed**. |
| **Repositories** | How many repositories, and whether the App covers **all repositories** or **selected repositories**. |
| **Permissions** | **Every permission the App requests is granted.**, or **Missing or too low** with the names. **None granted** when there are none. |
| **Rate limit budget** | How much of the GitHub rate limit is left and when it resets. **Not read yet** until Release Ops has read it. |

Without an installation, the section says "The App has no installation yet. Install it on your account or organization to start." A missing permission or a status other than **Active** also appears as **Permissions lost** on the [Jobs and Problems](../jobs/) page.

## See how roles are mapped

The **Role mapping** section explains who gets which role. It is read-only: "Roles come from GitHub and are not edited here." The rows **ADMIN_TEAM** and **VIEWER_TEAM** show the team that the operator set, or **Not set**. Under **Resulting rules** the page lists who is an admin, who is a viewer and who has no access. A **Configuration problem** notice appears when a team names another organization's team or is ignored on a personal account. See [Roles](../../concepts/roles/).

Before the App is installed, the section says "Roles apply once the App is installed. Until then only the bootstrap admin can sign in."

## Replace the App's private key

Replace the key when you suspect it has leaked, or as part of regular rotation. Release Ops checks the new key against GitHub before it replaces the old one, so a wrong key does no harm.

1. On GitHub, open the settings of your App and generate a new private key. GitHub downloads a `.pem` file.
2. On the **Settings** page, find **GitHub App private key**.
3. Choose the file in **Key file (.pem)**, or paste the key into **Private key (PEM)**.
4. Select **Verify and replace key**. The button says **Verifying with GitHub** while it works.
5. Wait for **Private key replaced**: "GitHub accepted the new key and it is now in use. The previous key is discarded."
6. On GitHub, delete the old key in the App's settings. Release Ops does not do this for you.

**Result:** the new key is in use. The key is never shown again, and it is never logged.

If the key is not replaced, the current key stays in use. The page says why:

| Message | What to do |
| ------- | ---------- |
| **This key could not be verified for this App.** The current key is unchanged. | Use a PEM-encoded key generated in this App's settings. |
| **GitHub could not be reached to check the key.** | Try again. |
| **The result could not be confirmed**: the request did not finish. | It is not known whether the key was replaced. Submitting the same key again is safe. |
| **The file could not be read.** | Choose the file again, or paste the key. |

## Field and option reference

| Field / option | Description | Required |
| -------------- | ----------- | -------- |
| **Concurrency cap (jobs)** | How many Renovate jobs run at the same time. At least 1. | Yes |
| **Default ticket project** | The project for repositories without their own. | No |
| **Key file (.pem)** | A private key file, read into the field below. | One of the two |
| **Private key (PEM)** | The new private key as text. | One of the two |

## What the page looks like when nothing is there

| State | What you see |
| ----- | ------------ |
| Loading | A grey placeholder for the page, and placeholders in the sections that load on their own. |
| An error | **Failed to load settings.** for the page. Each section reports its own failure, for example **Failed to load the GitHub App.** |
| Before the first job | The global config section says that no job has returned the config yet. |

## Troubleshooting

### The cap does not seem to apply

It applies to the next job that starts. Running jobs finish first.

### The global config section stays empty

The worker returns the file with each job result. Start a run, for example with **Run now** on [Repositories](../repositories/), and open the section again.

### The webhook is not active

The instance runs in polling mode. See [Without a public URL: polling mode](../../getting-started/#without-a-public-url-polling-mode).

## Related pages

- [Repositories](../repositories/)
- [Ticket projects](../ticket-projects/)
- [Roles](../../concepts/roles/)
- [The global Renovate config](../../concepts/global-renovate-config/)
