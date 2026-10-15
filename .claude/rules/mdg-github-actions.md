---
description: Writing GitHub Actions workflows for MDG Labs repos — which runners, and what the self-hosted Coolify runners can and cannot do
paths:
  - ".github/**"
---

# GitHub Actions workflows

The runner a repo uses is `githubRunner` in `.claude/workflow.json`. **Public repos** run on GitHub-hosted runners (`ubuntu-latest`). **Private repos** run every job on the self-hosted Coolify runners: `runs-on: [self-hosted, coolify]`. A job asking only for `self-hosted` is never picked up, and `.github/actionlint.yaml` declares the `coolify` label for `actionlint`.

## What the Coolify runners are

Measured on 2026-10-08, Coolify "Docker (privileged)" mode, by the probe workflow in the private repo `mdg-labs/coolify-runner-probe` (re-run it there after a Coolify update — the feature is in beta — and update this rule):

- **Ephemeral:** every job gets a fresh runner. Nothing on disk survives to the next job, and the Docker daemon starts with no images, so every job pulls its images again. Use `actions/cache` (it works) and artifacts (they work between jobs).
- **Runner image:** Ubuntu 24.04, user `runner` with passwordless sudo but **no capabilities** and **no `/dev/net/tun`**.
  - **It has:** `bash`, `curl`, `git`, `jq`, `python3`, `docker`, `unzip`, `openssl`, `ssh`, `gpg`.
  - **It does not have:** `node`, `npm`, `pnpm`, `make`, `gcc`, `xz`, `zstd`, `zip`, `rsync`, `wget`, `yq`, `pip`, `psql`, `dig`, `nc`, `shellcheck`, `actionlint`, `rg`, `gh`, or `docker compose`.
- **Docker is a separate daemon** (Docker-in-Docker in a privileged sidecar), reached through the socket. `docker build`/`push`, `docker run`, bind mounts of the workspace, `--privileged`, and `--cap-add NET_ADMIN --device /dev/net/tun` all work.
- About four jobs run in parallel; `pull_request` jobs run.
- **A Pangolin tunnel works from a container** (`pangolin-postgres` job, 2026-10-08):
  - **Client:** the Pangolin CLI as a machine client (`pangolin up --attach --id … --secret … --endpoint …`) in a `debian:bookworm-slim` container started with `--cap-add NET_ADMIN --device /dev/net/tun` is `Connected` after about 15 s.
  - **DNS:** the client sets its container's resolver to Pangolin DNS (`100.96.128.1`). A second container on `--network container:<client>` therefore resolves Pangolin resource aliases with the normal system resolver; no `dig @100.96.128.1` workaround is needed.
  - **No direct path:** without the tunnel, the job doesn't reach services on other MDG Labs servers.
  - **Site side:** Newt must reach the target. Newt installed **on the host** cannot resolve container names, which only exist in Docker's DNS. Newt running **as a container on the service's Docker network** reaches `<container name>:<port>` with no host port mapped.

## Rules

- **Never assume a tool on the runner.** Get it from an action (`actions/setup-node`, `pnpm/action-setup`, …) or run the job in a container that has it.
- **A job that needs a toolbox runs in a container:** `container: node:22-bookworm` (or another image), then `apt-get install -y --no-install-recommends …` as root, with no sudo needed. Add `git config --global --add safe.directory "$GITHUB_WORKSPACE"` when the job runs git. `run:` steps in a container default to `sh`: set `defaults: run: shell: bash` when a script needs bash.
- **Service containers only work from a container job.** With `container:` set, reach the service by its name (`postgres:5432`). From a job running directly on the runner, neither the service nor any `docker run -p` port is reachable on `localhost`.
- **Never use `docker compose` on the runner** (the plugin isn't installed); use plain `docker` or run compose in a container.
- **Never rely on the runner for network devices or host changes.** A VPN or WireGuard tunnel (e.g. Pangolin) runs in a container started with `--cap-add NET_ADMIN --device /dev/net/tun`, and the commands that need the tunnel run in that container's network (`--network container:<id>`).
- **Reaching a private service (e.g. a database for migrations)** goes through that tunnel shape. The secrets reach the containers only as `docker run -e NAME` from the job's env, never on a command line. Wait for `pangolin status` to show `Connected` before using the tunnel, and remove the client container in an `if: always()` step. Pangolin resources target `<container name>:<port>` through a containerised Newt on the server; never map a host port for this.
- **Install with pinned versions,** and prefer the distro's packages in a container to downloading archives on the runner (no `xz` to unpack `.tar.xz`).
- **On a GitHub-hosted public repo** none of the above applies, but a workflow that may later move to the Coolify runners should still follow it.

## Testing runner behaviour

When you need to know how the Coolify runners behave — anything this rule doesn't answer, or something you suspect has changed — test it in `mdg-labs/coolify-runner-probe`, never by trial and error in a product repo's workflows. Add a job or step to its `.github/workflows/probe.yml` (on `[self-hosted, coolify]`, writing its findings to the step summary), push to `main` or run the workflow by hand, read the job logs, then record what you learned in this rule and refresh the copy in `mdg-labs/skills` `docs/probes/coolify-runner-probe.yml`.
