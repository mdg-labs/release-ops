---
title: "Repositories"
description: "See which repositories the App is installed on, pause one, start a run by hand, open a renovate.json pull request and choose a ticket project for each repository."
---

The **Repositories** page lists the repositories that the GitHub App is installed on and shows what Release Ops does with each. Admins also pause repositories, start runs, enable Renovate and map a repository to a ticket project here.

## Prerequisites

- You are signed in.
- The App is installed on the repositories you want to see. As an admin, you can install it from this page. See [Getting started](../../getting-started/#6-install-the-app-once).

## What viewers and admins see

| | Viewer | Admin |
| --- | ------ | ----- |
| Repositories listed | Only the repositories you can access on GitHub | All repositories the App is installed on |
| Columns **Repository**, **Renovate config**, **Open update PRs**, **Last job** and **Dependency Dashboard** | Yes | Yes |
| A **Paused** badge next to a repository | Yes | No, admins see the **Enabled** switch instead |
| Columns **Enabled**, **Ticket project** and **Actions** | No | Yes |
| Search | Yes | Yes |

A viewer sees the state of the repositories and none of the actions. The server refuses an action from a viewer even if one is attempted another way. See [Roles](../../concepts/roles/).

## Find a repository

1. Open **Repositories** in the sidebar.
2. Type part of a name in **Search repositories**.

**Result:** the list shows the repositories whose name matches. Use **Rows per page**, **Previous** and **Next** to move through a long list.

## Pause or resume a repository

An admin pauses a repository with the switch in the **Enabled** column. The switch is named **Run Renovate for** followed by the repository.

1. Find the repository.
2. Turn its **Enabled** switch off to pause it, or on to resume it.

**Result:** a paused repository gets no run from any trigger, and its **Run now** button is disabled. Resume it and runs start again. See [How runs are triggered](../../concepts/how-runs-are-triggered/#repositories-that-do-not-run).

If the change fails, a message says **Could not change** the repository and why, and the switch keeps its old value.

## Start a run now

An admin can start a Renovate run for one repository without waiting for a trigger.

1. Find the repository. It must be enabled.
2. Select **Run now** in the **Actions** column.

**Result:** a message says **Run queued for** the repository, with a **View jobs** link to the [Jobs](../jobs/) page. A run started by hand has high priority. If the request fails, a message says **Could not start a run for** the repository and why.

## Enable Renovate for a repository without a config

A repository whose **Renovate config** column says **Missing** has no Renovate configuration file, so Renovate skips it. **Enable repo** opens a pull request that adds one.

1. Find the repository. In **Actions**, select **Enable repo**.
2. Read the dialog **Open a pull request that adds renovate.json?** It says which preset the new `renovate.json` extends: the first preset repository of your instance, written as `local>owner/repo`, or `config:recommended` when none is set.
3. Select **Open pull request**.

**Result:** a message says **Pull request #** and its number is open for the repository, and the button turns into a link to that pull request. Release Ops never merges it: review and merge it yourself. The merge is a push to the default branch, which starts a run. After that run, the repository's **Renovate config** shows **Present**.

If the dialog says the preset could not be loaded, the pull request cannot be opened right now. Close the dialog and try again later. If it says the branch `release-ops/enable-renovate` already exists with commits that Release Ops did not make, Release Ops left that branch as it is. Delete or merge the branch, then try again.

## Choose the ticket project of a repository

Release Ops creates the tickets of a repository's update PRs in its ticket project. An admin chooses it in the **Ticket project** column.

1. Find the repository.
2. Open the select in **Ticket project**.
3. Choose **Installation default** to use the default that is set on [Settings](../settings/#choose-the-default-ticket-project), or choose one of your ticket projects.

**Result:** the choice is saved at once, and applies to tickets created afterwards. The **Installation default** entry names the current default, or says **none** when no default is set.

A warning, **No ticket project: its update pull requests get no tickets.**, shows under the select when the repository has neither its own project nor a default. If the projects cannot be read, the cell says **The ticket projects could not be loaded.** Create projects on the [Ticket projects](../ticket-projects/) page.

## Field and option reference

| Field / option | Description | Required |
| -------------- | ----------- | -------- |
| **Repository** | The repository's full name, linked to GitHub. | — |
| **Renovate config** | **Present**, **Missing** or **Unknown**. Renovate decides whether the repository has a configuration file. | — |
| **Open update PRs** | The number of update PRs that are open now. | — |
| **Last job** | The state of the latest job (**Queued**, **Running**, **Succeeded** or **Failed**) and when it finished. **No job yet** when none has run. | — |
| **Dependency Dashboard** | An **Open** link to the repository's Dependency Dashboard issue, or **Unknown** when no Dependency Dashboard is known for the repository. | — |
| **Enabled** | Admins only. Off means paused. | — |
| **Ticket project** | Admins only. The repository's ticket project, or the installation default. | No |
| **Actions** | Admins only. **Run now**, and **Enable repo** when the config is missing. | — |

## What the page looks like when nothing is there

| State | What you see |
| ----- | ------------ |
| Loading | Grey placeholder rows. |
| No repository installed | **No repositories yet**. An admin reads "The GitHub App has no repository installed. Install it on the repositories Renovate should update." and gets the button **Install the GitHub App on repositories**. A viewer reads "There are no repositories you can see yet." The search box is hidden. |
| No match for a search | **No repositories match this search.** |
| An error | **Could not load the repositories.** with a **Try again** button. |

## Troubleshooting

### Run now is disabled

The repository is paused. Turn its **Enabled** switch on.

### A repository shows Missing

It has no Renovate configuration file. Add one yourself, or use **Enable repo**. The hourly run skips such a repository. A push to its default branch, adding it to the installation, or **Run now** still starts a run, so adding the file is noticed.

### The Renovate config column says Unknown

The column shows whether Renovate found a configuration file in the repository's last job. Until a job has run, it says **Unknown**. Wait for the first run, or select **Run now**.

### A repository is missing from the list

The App must be installed on it. As a viewer, you also need access to it on GitHub, and your list is read when you sign in.

## Related pages

- [Dashboard](../dashboard/)
- [Jobs and Problems](../jobs/)
- [Ticket projects](../ticket-projects/)
- [How runs are triggered](../../concepts/how-runs-are-triggered/)
