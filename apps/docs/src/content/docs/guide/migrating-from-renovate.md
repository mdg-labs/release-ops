---
title: "Migrating from a Renovate GitHub Action or self-hosted Renovate"
description: "Move from a scheduled Renovate GitHub Action or any self-hosted Renovate to Release Ops while keeping your presets and repository configs."
---

Release Ops runs Renovate unmodified, so most of what you already have keeps working. This guide moves a Renovate GitHub Action, or any other self-hosted Renovate that you start yourself, over to Release Ops. It does not depend on how the old setup is scheduled.

You are done when the old schedule is off, the new bot has opened the update PRs in your repositories, and each update PR has a ticket.

## What stays and what changes

| You keep | You replace |
| -------- | ----------- |
| Shared presets, such as `local>your-org/renovate-presets` | The GitHub App or token the old setup used. Release Ops has its own App. |
| Each repository's `renovate.json` | The schedule that started Renovate: a workflow, a cron job or a container. Release Ops starts runs itself. |
| Your global (self-hosted) configuration, minus a few settings | The old bot's open PRs and its Dependency Dashboard issues. The new bot opens its own. |

## Prerequisites

- Release Ops is running and you have signed in as an admin. See [Getting started](../../getting-started/).
- You know where the old setup's global configuration file, its tools directory and its shared presets live.
- You can close PRs and issues, and delete branches, in the repositories you migrate.

## 1. Keep your presets and `renovate.json` files

Leave the repositories' `renovate.json` files and your shared presets as they are. Renovate reads them the same way under Release Ops. Presets that use `local>…` keep working when the preset repository is reachable by the job, which step 5 sets up.

## 2. Point the instance at your global configuration

Release Ops uses the global (self-hosted) configuration that you already have. Prepare the file first:

1. Remove `repositories`, `platform` and `token` from it. Release Ops sets them for every job, and it ignores them if they are still there. The same goes for `allowScripts`, which Release Ops always sets to `false`.
2. Keep `onboarding: false` and `requireConfig: "required"`, so Renovate opens no onboarding PR and skips repositories without their own Renovate configuration.
3. Keep your other global settings, such as `platformCommit`, `prConcurrentLimit`, schedules and presets.
4. Make sure the file contains no secrets. The **Settings** page shows its contents to admins.

Then give the instance the file in one of two ways. See [The global Renovate config](../../concepts/global-renovate-config/).

- **From a repository.** Commit the file to a repository, and set the instance's global config repository and ref. The worker fetches it at the start of every job, with the job's GitHub token.
- **From the host.** Mount the file read-only into the worker container.

The release you run lists the exact file location in the repository and the mount paths in its `.env.example` and `docker-compose.yml`. Restart the services after you change the instance's environment.

## 3. Allow your commands and provide the tools directory

If your old setup runs commands through `postUpgradeTasks`, keep `allowedCommands` in the global configuration file. It is only read from that file. A repository's own config cannot add to it, and the web interface cannot change it.

If the old setup used a tools directory for those commands, bring it along with the global configuration: it is fetched together with the file, or you mount it read-only into the worker next to the file. Renovate cannot modify it.

## 4. Create a new App

Create the App through the setup page, as in [Getting started](../../getting-started/#5-open-the-setup-page-and-create-the-github-app). You cannot adopt an existing GitHub App or a hand-made one: Release Ops creates its own with the permissions it needs, and one instance has exactly one App and one installation.

## 5. Install it on the same repositories

1. Install the App on the repositories the old setup covered.
2. Install it on your preset repository and on the global configuration repository too.
3. List the preset repository in the instance configuration, so each job's token can read it. A shared presets repository that the token cannot read breaks `extends` for every repository that uses it.

Release Ops takes part in a repository when the App is installed on it and it has a Renovate configuration file. See [How runs are triggered](../../concepts/how-runs-are-triggered/) for what starts a run.

## 6. Stop the old schedule

Disable the old workflow's schedule, or stop the old Renovate process, before the new bot starts opening PRs. Two bots on one repository open duplicate PRs.

## 7. Close the old bot's open PRs and delete their branches

The new bot is a different author. It does not adopt the old bot's PRs, and a branch left behind can keep the new bot from using the same branch name.

In each repository:

1. Close every open Renovate PR from the old setup.
2. Delete each of their `renovate/*` branches.

With the GitHub CLI, one repository takes this:

```bash
gh pr list --repo <owner>/<repo> --author "<old bot login>" --state open
gh pr close <number> --repo <owner>/<repo> --delete-branch
```

Use the old bot's login as the author. For a GitHub App it has the form `app/<slug>`.

## 8. Close the old Dashboards

Close the old bot's Dependency Dashboard issue in each repository. Release Ops does not adopt it: it recognises a dashboard only when the App's bot user is its author. On its first run the new bot opens a dashboard of its own, when your configuration enables one.

## 9. Let the first runs recreate the PRs

A run starts when you add the repository to the installation, when you push to its default branch, and every hour. The new bot then opens the update PRs, and a new Dependency Dashboard when your configuration enables one. Release Ops creates a ticket for each update PR. See [Update PRs and tickets](../../concepts/update-prs-and-tickets/).

The first PRs can take several minutes while Renovate processes each repository for the first time.

## 10. Retire the old App and workflow

When the new bot works in every repository:

1. Delete the old workflow file, or remove the old Renovate service.
2. Uninstall the old App from the repositories, or delete it in its owner's settings on GitHub, and revoke any token or key the old setup used.

## Skip decisions are lost

Under the old bot, closing an update PR without merging it told Renovate to skip that version. **Those decisions are lost in the move.** The new bot starts without that history, so it opens those updates again.

Before you retire the old setup, look through each repository's PRs that were closed without a merge and note the ones that matter. Then re-apply those decisions in a way that does not depend on the old bot:

- Add the dependency to `ignoreDeps` in the repository's Renovate config.
- Add a `packageRules` entry that disables the update, for example with `matchPackageNames` and `enabled: false`, or limit the versions with `allowedVersions`.
- For updates that you want to approve one by one, use `dependencyDashboardApproval` in a `packageRules` entry, and tick the checkbox on the new Dependency Dashboard when you want the PR.

Put shared decisions in your preset, and decisions for one repository in its `renovate.json`.

## Troubleshooting

| Symptom | What to check |
| ------- | ------------- |
| A repository gets no update PRs | The App must be installed on it, and it must have a Renovate configuration file. Without one, it is skipped and recorded as having no config. |
| `extends` with a `local>` preset fails | The preset repository must have the App installed and be listed in the instance's preset repositories. |
| Renovate opens an onboarding PR | The global configuration lacks `onboarding: false`. Release Ops creates no ticket for onboarding PRs. |
| A command from `postUpgradeTasks` does not run | The command must match an entry in `allowedCommands` in the global configuration file. |
| A job fails before Renovate starts | The global configuration could not be fetched. Check the repository, the ref and that the App is installed on that repository. |
| The old PRs are still there | Release Ops never touches them. Close them and delete their branches as in step 7. |

## Related pages

- [The global Renovate config](../../concepts/global-renovate-config/)
- [How runs are triggered](../../concepts/how-runs-are-triggered/)
- [Update PRs and tickets](../../concepts/update-prs-and-tickets/)
