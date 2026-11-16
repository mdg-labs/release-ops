Run Release Ops on your own host with Docker Compose, create the GitHub App it runs as, install the App on your repositories and sign in. You are done when the Settings page shows your App and its installation, and the first update PR that Renovate opens is tracked as a ticket. The second half of this page covers [operating the instance](#operating-your-instance): network access, backups, restores and upgrades.

## What you'll need

- A host with Docker and Docker Compose v2. The app publishes port **3000**.
- A GitHub account or organization that will own the GitHub App. You need to be able to create an App there.
- At least one repository with a Renovate configuration file, such as `renovate.json`. A repository without one is skipped.
- An account in the ticket system you use: Kaneo, Jira or Linear.
- For fast reactions, a public HTTPS URL that GitHub can reach. Without one, Release Ops runs in [polling mode](#without-a-public-url-polling-mode).

## How the pieces fit

Release Ops runs as two services in one Compose project.

| Service | What it does | Network |
| ------- | ------------ | ------- |
| `app` | The web UI, the API, the webhook receiver, the job queue and the ticket logic. It holds every secret and is the only process that opens the database (`app.db`, on the `release-ops-data` volume). | Publishes port 3000. Also listens on an internal port for the worker. |
| `worker` | Runs Renovate, one job per repository, as an unprivileged user. It has no database, no data volume and no Docker socket. | Publishes nothing. It reaches the app only on the internal listener, with a shared secret. |

One GitHub App is the bot that opens the update PRs, the sender of webhooks and the sign-in provider. Release Ops supports exactly one installation of that App per instance.

## Quick start

### 1. Choose your URLs

Two URLs matter:

| Setting | What it is | Required |
| ------- | ---------- | -------- |
| `APP_URL` | The address your browser uses to open Release Ops, without a trailing slash, for example `https://release-ops.example.com`. GitHub sends your browser back to it during setup and sign-in. | Yes |
| `WEBHOOK_URL` | The public URL GitHub delivers webhooks to. It ends in `/hooks/github`, for example `https://release-ops.example.com/hooks/github`. | No |

Release Ops reads both at every start, and uses them to build the GitHub App's manifest when you create the App. Changing them later does not change the App that already exists. Edit the App's settings on GitHub to match:

- After you change `WEBHOOK_URL`, edit the App's webhook URL and its activation.
- After you change `APP_URL`, edit the App's callback URL (the user authorization callback) to `<APP_URL>/api/go/api/v1/auth/github/callback`. GitHub accepts a sign-in redirect only when it matches a registered callback URL, so sign-in fails until you do.

GitHub itself calls only the webhook, and only the path `/hooks/github` of it. Your browser follows the setup and sign-in redirects, so GitHub never calls those addresses:

| What | Who reaches it | How to expose it |
| ---- | -------------- | ---------------- |
| `/hooks/github` on port 3000 | GitHub | Public HTTPS, for example through a reverse proxy that forwards only this path. |
| The web UI, including the setup and sign-in callbacks | The browsers of the people who sign in | As you choose. It only has to be reachable from those browsers. |
| The internal listener (`INTERNAL_ADDR`, port 9090 in the example) | The worker | Compose network only. Never publish it, never proxy it. |

If a reverse proxy terminates TLS in front of the app, set `TRUSTED_PROXIES` to the proxy's address or range. Otherwise the rate limits treat every visitor as one client.

### 2. Write the environment file

Create a `.env` file next to your Compose file. Generate each secret yourself; never reuse an example value.

```bash
# SESSION_SECRET: 32 or more random bytes
openssl rand -base64 32

# APP_ENCRYPTION_KEY: 64 hex characters (32 bytes)
openssl rand -hex 32

# WORKER_SECRET: 32 or more random bytes
openssl rand -base64 32
```

```bash
SESSION_SECRET=<output of the first command>
APP_ENCRYPTION_KEY=<output of the second command>
WORKER_SECRET=<output of the third command>
APP_URL=https://release-ops.example.com
WEBHOOK_URL=https://release-ops.example.com/hooks/github
BOOTSTRAP_ADMIN_GITHUB_LOGIN=<your GitHub login>
```

Leave `WEBHOOK_URL` out when you have no public URL.

The settings this guide uses:

| Variable | Required | Description |
| -------- | -------- | ----------- |
| `SESSION_SECRET` | Yes | Signs the session cookie. |
| `APP_ENCRYPTION_KEY` | Yes | Encrypts the GitHub App's private key, client secret and webhook secret, and the credentials of your ticket integrations. Keep it with every backup: without it the stored credentials cannot be read. |
| `APP_URL` | Yes | See step 1. The app refuses to start without it. |
| `WEBHOOK_URL` | No | See step 1. Unset means [polling mode](#without-a-public-url-polling-mode). |
| `WORKER_SECRET` | Yes | The shared secret the worker presents to the app's internal listener. The same value goes on both services. |
| `INTERNAL_ADDR` | Yes | The bind address of the internal listener, such as `:9090`. The Compose file below sets it. |
| `SETUP_TOKEN` | No | A setup token of your own, at least 64 characters (`openssl rand -hex 32`). Unset: the app generates one and prints it to its log. |
| `BOOTSTRAP_ADMIN_GITHUB_LOGIN` | No | The GitHub login of your first admin. See [step 8](#8-sign-in). |
| `ADMIN_TEAM`, `VIEWER_TEAM` | No | Organization teams in the form `org/team-slug`. Members of `ADMIN_TEAM` are admins. When `VIEWER_TEAM` is set, only its members and the admins can sign in as viewers. |
| `GLOBAL_CONFIG_REPO`, `GLOBAL_CONFIG_REF` | One of two ways | The repository, and the branch, tag or commit in it, that holds the global Renovate configuration. The worker fetches it at the start of every job. Set both or neither. Every instance needs a global configuration, either this or a mounted file: see [Provide the global Renovate config](#provide-the-global-renovate-config). |
| `PRESET_REPOSITORIES` | No | A comma-separated list of `owner/repo` repositories, such as a shared presets repository. Each job's GitHub token covers them besides the job's own repository, so Renovate can resolve presets from them. The App must be installed on them. |
| `TRUSTED_PROXIES` | No | See step 1. |
| `RELEASE_OPS_TAG` | No | The image tag. Both images use the same tag. Default `latest`. |

The [full list of settings](specs.html) has the rest.

### 3. Create the Compose file

Save this as `docker-compose.yml`:

```yaml
services:
  app:
    image: ghcr.io/mdg-labs/release-ops:${RELEASE_OPS_TAG:-latest}
    ports:
      - "3000:3000"
    volumes:
      - release-ops-data:/data
    environment:
      SESSION_SECRET: ${SESSION_SECRET:?set SESSION_SECRET in .env}
      APP_ENCRYPTION_KEY: ${APP_ENCRYPTION_KEY:?set APP_ENCRYPTION_KEY in .env}
      APP_URL: ${APP_URL:?set APP_URL in .env}
      WEBHOOK_URL: ${WEBHOOK_URL:-}
      SETUP_TOKEN: ${SETUP_TOKEN:-}
      TRUSTED_PROXIES: ${TRUSTED_PROXIES:-}
      BOOTSTRAP_ADMIN_GITHUB_LOGIN: ${BOOTSTRAP_ADMIN_GITHUB_LOGIN:-}
      ADMIN_TEAM: ${ADMIN_TEAM:-}
      VIEWER_TEAM: ${VIEWER_TEAM:-}
      PRESET_REPOSITORIES: ${PRESET_REPOSITORIES:-}
      GLOBAL_CONFIG_REPO: ${GLOBAL_CONFIG_REPO:-}
      GLOBAL_CONFIG_REF: ${GLOBAL_CONFIG_REF:-}
      # The internal listener: reachable on the Compose network only, so it has no ports entry.
      INTERNAL_ADDR: ":9090"
      WORKER_SECRET: ${WORKER_SECRET:?set WORKER_SECRET in .env}
    restart: unless-stopped

  worker:
    image: ghcr.io/mdg-labs/release-ops-worker:${RELEASE_OPS_TAG:-latest}
    depends_on:
      app:
        condition: service_healthy
    environment:
      INTERNAL_URL: http://app:9090
      WORKER_SECRET: ${WORKER_SECRET:?set WORKER_SECRET in .env}
    volumes:
      # Renovate's per-slot caches. They are disposable.
      - worker-cache:/cache
      # The global Renovate config, read-only. See "Provide the global Renovate config" below.
      - ./worker/config:/config:ro
    # The worker runs as an unprivileged user and starts Renovate as another one.
    # SETUID and SETGID are the only capabilities it keeps for that.
    cap_drop:
      - ALL
    cap_add:
      - SETUID
      - SETGID
    # Example limits, sized for the default concurrency cap of 2.
    cpus: 2
    mem_limit: 4g
    restart: unless-stopped

volumes:
  release-ops-data:
  worker-cache:
```

Keep these in place:

- Do not publish port 9090, and do not mount a Docker socket anywhere. The worker needs none.
- Do not add `no-new-privileges` to the worker. It disables the file capabilities the worker uses to switch users.
- Mount no data volume on the worker. It has no database access.

#### Provide the global Renovate config

Every job needs a global (self-hosted) Renovate configuration. A repository's own `renovate.json` does not replace it. Without one, every job fails before Renovate starts, because no global config is mounted and none is configured to fetch. Choose one way.

**A mounted file.** Create `worker/config/config.json` next to your Compose file, readable by every user, with this content:

```json
{
  "onboarding": false,
  "requireConfig": "required"
}
```

The Compose file above mounts `./worker/config` read-only at `/config`. The worker uses `config.js` there when it exists and `config.json` otherwise.

**A repository.** Commit the same file as `renovate-global/config.json` (or `config.js`) to a repository. Set `GLOBAL_CONFIG_REPO` to `owner/repo` and `GLOBAL_CONFIG_REF` to a branch, tag or commit in `.env`, and install the App on that repository in [step 6](#6-install-the-app-once). A mounted file wins over the repository, so leave `./worker/config` empty, or remove the mount, when you use a repository.

The file must contain no secrets, and none of the settings Release Ops sets for every job: `platform`, `token`, `repositories` and `allowScripts`. `onboarding: false` and `requireConfig: "required"` stop Renovate from opening onboarding PRs and make it skip repositories without a Renovate configuration of their own. Add your other global settings, such as `prConcurrentLimit`, as you need them. [The global Renovate config](https://mdg-labs.github.io/release-ops/concepts/global-renovate-config/) explains where the file can live and what the worker does with it.

### 4. Start the services and find the setup token

```bash
docker compose up -d
docker compose ps
```

Wait until `app` is healthy. The worker starts after it.

While no GitHub App exists, the instance is in setup mode: sign-in is unavailable and only the setup page works. The setup token opens it. If you did not set `SETUP_TOKEN`, the app printed a token to its log:

```bash
docker compose logs app
```

Each start that finds no App configured replaces the stored token. If you missed the log line, restart the app to get a new one. The token works once. A token that is wrong or already used is refused; restart the app for a new one.

### 5. Open the setup page and create the GitHub App

1. Open `APP_URL` in your browser. It sends you to the setup page.
2. Enter the **Setup token**. The setup session lasts 15 minutes.
3. Choose the **App owner**: **My personal account** or **An organization**. For an organization, enter its **Organization name**.
4. Optionally enter an **App name**. GitHub lets you change it before it creates the App.
5. Select **Create GitHub App**.

GitHub opens its confirmation page for the new App. It lists every permission the App requests:

| Permission | Access |
| ---------- | ------ |
| Administration | Read |
| Checks | Read and write |
| Commit statuses | Read and write |
| Contents | Read and write |
| Dependabot alerts | Read |
| Issues | Read and write |
| Metadata | Read |
| Pull requests | Read and write |
| Workflows | Read and write |
| Merge queues | Read and write |
| Organization members | Read |

These are the permissions Renovate documents, plus **Organization members**, which Release Ops uses to work out who may sign in. The App is private, so only its owner can install it. Confirm on GitHub.

The setup token stays used even when a later step fails. If the conversion fails, you retry within the setup session. After the session ends, restart the app for a new token while no App exists.

### 6. Install the App once

GitHub sends you to the App's installation page. Install the App on your account or organization and choose the repositories: all of them, or a selection. Release Ops accepts one installation per instance; a second one is ignored.

Install the App on the repositories you list in `PRESET_REPOSITORIES` and `GLOBAL_CONFIG_REPO` too.

GitHub then sends you back, and Release Ops records the installation and opens the sign-in page.

### 7. Add a Renovate config where you want updates

A repository takes part when the App is installed on it and it has a Renovate configuration file. Add a `renovate.json` to each repository you want updated. A repository without one is skipped and recorded as having no config. Pushing the file to the default branch is noticed.

### 8. Sign in

Select **Sign in with GitHub**. The first admin is one of these:

| Installed on | Admins |
| ------------ | ------ |
| An organization | The organization's owners, and the members of `ADMIN_TEAM` when it is set. Every other active member is a viewer, or only the members of `VIEWER_TEAM` when it is set. |
| A personal account | The account owner. There are no viewers. |

`BOOTSTRAP_ADMIN_GITHUB_LOGIN` pins a first admin regardless of membership. At the first sign-in with a matching GitHub login (case-insensitive), Release Ops stores that account's numeric GitHub id as the bootstrap admin. From then on the stored id is the bootstrap admin and the variable is ignored: removing or changing it has no effect, and renaming the account gains nothing. Before the installation is recorded, only the bootstrap admin can sign in.

Roles come from GitHub and are re-checked every 15 minutes. A session ends 8 hours after you sign in. A viewer sees only the repositories they can access on GitHub.

### 9. Connect your ticket system

Open the **Integrations** page, select **Add integration** and enter the credentials of your Kaneo, Jira or Linear account. Use **Test connection** to check them. Then open **Ticket projects** and add the project or team that should receive tickets, with its status mapping. Finally open **Settings** and pick the **Default ticket project**. Tickets for update PRs go there unless a repository sets its own.

### 10. Check the first run

Open **Settings**. You should see:

- **GitHub App** shows your App, its owner and the stored secrets.
- **Installation** shows your account, the status **Active** and the number of repositories.
- With a webhook, the App's webhook is **Active**. Without one, **Polling mode** appears.

Renovate runs for a repository when a trigger starts a job:

- A repository is added to the installation.
- A push reaches the default branch of a repository.
- You tick a checkbox in a Renovate PR or in the repository's Dependency Dashboard.
- A Renovate PR is merged or closed.
- Every hour, a safety-net run covers every enabled repository.

Without a webhook, [polling](#without-a-public-url-polling-mode) stands in for the events, and the hourly run still applies. After the first job finishes, **Settings** shows the global Renovate config as the worker last used it.

**You're ready when:** Renovate opens an update PR in one of your repositories and a ticket for it appears in your ticket project.

## Without a public URL: polling mode

Leave `WEBHOOK_URL` unset when your host has no public address, for example at home or behind NAT. The App is created with its webhook inactive, GitHub sends no events, and Release Ops falls back to polling.

Polling mode is degraded:

- Reactions take minutes, not seconds.
- A checkbox tick made while a run is already in progress can be missed.
- Installations and repositories sync on a timer, not when they change.
- A change made while the app was down waits for the hourly run.

The **Settings** page shows **Polling mode** while the webhook is not active. To leave polling mode, publish `/hooks/github`, set `WEBHOOK_URL` in `.env` and restart the app, then set the App's webhook URL and activate it in the App's settings on GitHub. The app registers `/hooks/github` only when `WEBHOOK_URL` is set at start.

## Operating your instance

### Network access

The app and the worker need outbound access. The lists below name the categories; the hosts depend on your repositories.

| Service | Outbound destinations | Why |
| ------- | --------------------- | --- |
| `app` | The GitHub API and GitHub's web address (`api.github.com`, `github.com`) | Installation tokens, the App's own calls, sign-in and role checks |
| `app` | The address of each ticket integration | Creating and updating tickets |
| `worker` | The GitHub API and git over HTTPS (`api.github.com`, `github.com`) | Your repositories, presets and the global config repository |
| `worker` | The package registries and datasources of the ecosystems your repositories use, such as npm, PyPI, Maven, Docker registries and GitHub releases of other projects | Whatever Renovate's managers for those ecosystems contact |
| `worker` | The hosts the tool installer fetches language runtimes and package managers from | Renovate installs tools at run time (`binarySource=install`) |
| `worker` | The app's internal listener on the Compose network | Claiming jobs, tokens, heartbeats and results |

The worker needs no inbound connection, and no access to the database, a Docker socket or any ticket system. The app needs an inbound path from GitHub to `/hooks/github` when you use a webhook, and from your browser to the web UI.

To write a firewall rule, start from these categories and add the hosts your repositories' ecosystems and tool installs contact.

### Backing up the database

All durable state is in `app.db` on the `release-ops-data` volume. The worker's cache volume is disposable. Keep `APP_ENCRYPTION_KEY` and `SESSION_SECRET` with every backup, because the App's private key and your integration credentials are unreadable without the encryption key.

The database runs in WAL mode. Next to `app.db` there are `app.db-wal` and `app.db-shm`. Copying `app.db` alone from a running app can miss committed data that still sits in the WAL file. Use one of these:

- **`VACUUM INTO`.** It writes a consistent copy while the app runs.

  ```bash
  sqlite3 <path to app.db on the volume> "VACUUM INTO '<path of the backup file>'"
  ```

  The app image does not include the `sqlite3` command-line tool. Run the command on the Docker host, against the volume's location there (`docker volume inspect` prints it).

- **A volume snapshot** that is atomic across `app.db`, `app.db-wal` and `app.db-shm`, or one taken with the app stopped.

Before every upgrade, take a backup the same way. The app also saves its own `VACUUM INTO` snapshot to `/data/snapshots` before it applies pending migrations, and keeps the newest three. That snapshot is on the same volume, so it does not replace your backup.

### Restoring the database

1. Stop both services.
2. Put the backup in place:
   - A **`VACUUM INTO` copy** is a single file. Delete the existing `app.db-wal` and `app.db-shm` on the volume, then put the copy in place as `app.db`.
   - A **volume snapshot** is restored whole: put its `app.db`, `app.db-wal` and `app.db-shm` back together. Never delete the snapshot's WAL file; it can hold committed data that is not in `app.db`.
3. Start the app, then the worker.

Jobs that were running when the backup was taken have expired leases, and the app reclaims them. Webhook deliveries that failed while the app was down are redelivered at startup.

### Upgrading

1. Take a backup, as described above.
2. Set `RELEASE_OPS_TAG` to the new tag, and pull both images at that same tag.
3. Restart both services:

   ```bash
   docker compose pull
   docker compose up -d
   ```

The app applies pending migrations when it starts, before it listens. All pending migrations run in one transaction: a failure rolls the whole batch back and the next start reports the same error. There are no down migrations, so going back means restoring the backup you took before the upgrade, and running the previous tag.

Release Ops 2 does not read a Release Ops 1 data volume. Start it with a new, empty data volume.

After the upgrade, check:

- `docker compose ps` shows `app` healthy and `worker` running.
- `docker compose logs app` has no migration error.
- If migrations ran, a new snapshot appears in `/data/snapshots`.
- You can sign in, and **About** shows the new Release Ops version and the pinned Renovate version.
- **Settings** still shows your App and its installation.
- A Renovate run finishes after the first trigger.

## Troubleshooting

| Symptom | Check |
| ------- | ----- |
| `docker compose up` stops with "set ... in .env" | Set the named variable in `.env`. `SESSION_SECRET`, `APP_ENCRYPTION_KEY`, `APP_URL` and `WORKER_SECRET` have no defaults. |
| The app does not start | Run `docker compose logs app`. A malformed `TRUSTED_PROXIES` or a `SETUP_TOKEN` shorter than 64 characters stops it at startup. |
| The setup page refuses the token | The token is wrong or already used. Restart the app and read the new token from its log. |
| Sign-in says the account is not allowed | The GitHub account has no role. Check your organization membership, `ADMIN_TEAM` and `VIEWER_TEAM`. |
| GitHub sign-in is broken | A shell on the app container can run `release-ops admin login-link`, which prints a one-time link for the bootstrap admin. It refuses until a bootstrap admin has signed in once. |
| Nothing happens after a tick or a push | Without a webhook the instance is in polling mode and reacts in minutes. With a webhook, check that GitHub can reach `/hooks/github`. |
| A repository gets no update PRs | Check that the App is installed on it and that it has a Renovate configuration file. |

## Further reading

The [specification](specs.html) describes the deployment, the Compose reference and the settings in full.
