---
title: "The global Renovate config"
description: "Where Renovate's self-hosted configuration comes from, which settings Release Ops owns and what the job's GitHub token can reach."
---

Renovate has two levels of configuration. A repository's own `renovate.json` and the shared presets it extends decide what Renovate updates. The global (self-hosted) configuration decides how Renovate runs on your host: concurrency limits, schedules, which commands it may run and similar. Release Ops runs Renovate unmodified, so it uses the same global configuration file you may already have.

The file is yours. It lives in git or on the host, it keeps its own review and validation, and Release Ops never edits your copy of it. The file can be JavaScript, which Release Ops never runs. For a JSON file, Release Ops drops the settings it owns (see below) from the copy it hands to Renovate. A JavaScript file goes to Renovate as it is.

## Where the file comes from

Pick one of two ways. Both are set by the operator.

| Way | How it works |
| --- | ------------ |
| **A repository** | The worker fetches the file at the start of every job from a repository and a ref (a branch, tag or commit) you configure for the instance. It reads the repository with the job's GitHub token. The same fetch brings the tools directory. |
| **A mounted file** | You mount the file and the tools directory into the worker container from the host, read-only. Nothing is fetched. A mounted file wins over a configured repository. |

The paths are fixed:

| | In the repository | Mounted into the worker |
| --- | ----------------- | ----------------------- |
| Global configuration | `renovate-global/config.js`, or `renovate-global/config.json` when there is no `config.js` | `/config/config.js`, or `/config/config.json` when there is no `config.js` |
| Tools directory (optional) | `renovate-global/tools` | `/opt/renovate-tools` |

If the fetch fails, the job fails before Renovate starts. The App must be installed on the configuration repository, so the job's token can read it.

The **Global Renovate config** section on the **Settings** page shows the file exactly as the worker last used it, as plain text, with its source and the time it was last used. Token-shaped strings are hidden. Until a job has finished, it says that no job has returned the config yet. The page cannot edit the file: change it where it lives. The file must not contain secrets.

## What Release Ops owns

Release Ops sets four settings itself, for every job:

| Setting | Value |
| ------- | ----- |
| `platform` | `github` |
| `token` | The job's GitHub token |
| `repositories` | The one repository of the job |
| `allowScripts` | Always `false`: package-manager install scripts never run |

If a JSON file contains one of these, at the top level or inside `force`, Release Ops removes it from the copy it hands to Renovate and logs a warning that names the setting, never a value. A JavaScript file is not changed, and neither is a JSON file Release Ops cannot parse. For those, the settings Release Ops sets for the job win over your file, except inside `force`, which Renovate applies over everything else. When you reuse a JavaScript file, delete `platform`, `token`, `repositories` and `allowScripts` from it yourself, including any inside `force`.

Everything else in the file is Renovate's: presets, `prConcurrentLimit`, schedules, `binarySource` and so on. Release Ops does not set these for you, so your file carries them.

Two settings matter for how Release Ops chooses repositories. Keep `onboarding: false` and `requireConfig: "required"` in the file. Then Renovate opens no onboarding PR, and it skips a repository that has no Renovate configuration of its own. Release Ops records such a repository as having no config.

## Allowing commands

`allowedCommands` is set only in the global configuration file. A repository's own config cannot add to it, and you cannot set it through the web interface or the API. Renovate runs no `postUpgradeTasks` command that the file does not list. To allow one, list it as a regular expression in the file.

Commands an admin allows can live in a tools directory instead of in the worker image. The directory is read-only, so Renovate cannot modify it. It is fetched together with the configuration file or mounted from the host.

## What the job's GitHub token can reach

Each job gets its own installation token, minted by the app and valid for about one hour. A job never outlives its token. The token covers:

- the repository the job is running for,
- the preset repositories listed in the instance configuration, so Renovate can resolve shared presets such as `local>your-org/renovate-presets`,
- the configuration repository, when you use one.

The App must be installed on the preset repositories and on the configuration repository. The token is held in memory only, and is never stored or logged. Renovate runs as a separate Unix user inside the worker container and cannot read the worker's own secret.

## Related pages

- [Migrating from a Renovate GitHub Action or self-hosted Renovate](../../guide/migrating-from-renovate/)
- [How runs are triggered](../how-runs-are-triggered/)
