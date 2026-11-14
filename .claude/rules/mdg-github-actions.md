---
description: Writing GitHub Actions workflows for MDG Labs repos — which runners and labels (runners01, runners01-light), and what the self-hosted runners01 runners can and cannot do
paths:
  - ".github/**"
---

# GitHub Actions workflows

The runner a repo uses is `githubRunner` in `.claude/workflow.json`. **Public repos** run on GitHub-hosted runners (`ubuntu-latest`). **Private repos** run on the self-hosted runners on `runners01`, which have two labels:

- **`runs-on: [self-hosted, runners01]`** for heavy jobs: builds (Go, TypeScript, …), tests, integration tests, image builds, e2e. This is `githubRunner`.
- **`runs-on: [self-hosted, runners01-light]`** for short jobs that need little memory: lint, schema, contracts, docs, workflow checks (actionlint, …), status and tag checks, and deploy jobs that build no image. Setup renders the issue-status workflow here.

When unsure, a job stays on `runners01`. A job asking only for `self-hosted` is never picked up, and `.github/actionlint.yaml` declares both labels for `actionlint`.

## What the runners01 runners are

Five plain GitHub Actions runners installed directly on the server `runners01` (8 vCores, 16 GB, used only for CI), defined in `mdg-labs/infra` (`inventory/runners/runners01.yaml`, `runbooks/ci-runners.md`):

| Runner | Label | For |
|--------|-------|-----|
| `runners01-1`, `runners01-2` | `runners01` | Heavy jobs |
| `runners01-3`, `runners01-4`, `runners01-5` | `runners01-light` | Short jobs |

- **The labels do not size the runners.** All five have the same resources. The labels limit how many heavy jobs run at once (two) and keep short jobs from waiting behind a long build.
- **Plain GitHub self-hosted runners, for host jobs** (mdg-labs/infra#388, 2026-11-08): each runner is the official `actions/runner` release under its own Unix user, registered with `config.sh` and installed as a service with `svc.sh install`, as GitHub documents it, in the private organization runner group `runners01`, with its label plus `self-hosted`, `linux` and `x64`. It updates itself. Nothing hooks into a job: no hook, no watchdog, nothing that stops or restarts a runner.
- **Jobs run on the host, as on GitHub-hosted runners.** Write workflows the way you would for `ubuntu-latest`: setup actions for toolchains, `sudo apt-get install` for system packages, service containers with mapped ports. Never use `container:` on these runners: a container job writes root-owned files into the persistent `_work`, and the next job's checkout fails on them (actions/runner#434).
- **Passwordless sudo,** as on GitHub-hosted Linux runners. Each runner user is also in the `docker` group and uses the host's Docker daemon (`/var/run/docker.sock`).
- **Persistent, not ephemeral:** the runner itself empties `_work/_temp` at the start and end of every job, removes the job's service containers and their network when the job ends, and kills the job's leftover processes. It keeps workspaces, `_work/_tool` (setup actions cache their downloads there) and the user's home caches between jobs; `actions/checkout` cleans the workspace per job. Never rely on anything surviving between jobs, and never leave anything behind.
- **Five runners share one host.** This is the difference from GitHub-hosted runners, where every job gets its own VM:
  - **One `/tmp`:** never write fixed paths under `/tmp`; use `$RUNNER_TEMP` (emptied per job) or the runner user's `$HOME`.
  - **One Docker daemon:** every runner sees, and can clash with, the containers, networks, volumes and images of the other four, and the host's monitoring containers run on the same daemon. Never touch what a job didn't start (no `docker rm`, `docker system prune` or `docker network prune` in a job).
  - **One apt:** `sudo apt-get install` from two runners at once collides on the dpkg lock. A package that a workflow installs on every run belongs in the host baseline; ask for it in `mdg-labs/infra`.
  - **One memory:** there is no limit per job. At memory exhaustion the kernel ends the biggest process, which is a job: keep a job's memory modest, and put heavy work on `runners01`, never on `runners01-light`. Tools that size themselves by the host's core count (vitest, turbo, `make -j$(nproc)`) see 8 cores shared by five runners: cap their workers in CI.
- **Docker cleanup:** an hourly host timer, outside the runner software, removes Docker images and build cache that nothing used for 7 days. An image cache can survive between jobs; never depend on it.
- **Time:** the runner takes a job's time limit from `timeout-minutes` (GitHub's default is 360 minutes); nothing else ends a long job. Give a job that can hang a `timeout-minutes`.
- **Host baseline:** the tools preinstalled on the host are listed in `mdg-labs/infra` `runbooks/ci-runners.md`, section "Host baseline" (infra#74). The list is modelled on the apt packages of GitHub's hosted Ubuntu image (`make`, `gcc`, `curl`, `jq`, `git`, `gh`, Docker CLI with `buildx` and `compose`, …) but is **not** the hosted image: no language toolchains, browsers or databases. Read the list there; this rule does not copy it.
- **Network devices in a container, measured 2026-10-18** under the earlier rootless Docker (mdg-labs/commerce probe run 39080133515 on `runner2`, Docker 29.9.1 rootless, kernel 7.0), not yet measured again on the host daemon:
  - `docker build` works.
  - A container started with `--cap-add NET_ADMIN --device /dev/net/tun` has `NET_ADMIN` in its effective set and sees `/dev/net/tun`. Inside it `ip tuntap add mode tun`, `ip addr add`, `ip link set … up` and `ip route add` succeed, and it can rewrite its own `/etc/resolv.conf`.
  - A second container on `--network container:<client>` starts and shares that network.

Pangolin tunnel from a container, measured 2026-10-08 on the previous runners and confirmed on runners01 on 2026-10-18 by mdg-labs/commerce's staging deploy (run 39081243760: the machine client reached `Connected`, and `migrate` ran through the tunnel with `--network container:<client>`):

- **A Pangolin tunnel works from a container:**
  - **Client:** the Pangolin CLI as a machine client (`pangolin up --attach --id … --secret … --endpoint …`) in a `debian:bookworm-slim` container started with `--cap-add NET_ADMIN --device /dev/net/tun` is `Connected` after about 15 s.
  - **DNS:** the client sets its container's resolver to Pangolin DNS (`100.96.128.1`). A second container on `--network container:<client>` therefore resolves Pangolin resource aliases with the normal system resolver; no `dig @100.96.128.1` workaround is needed.
  - **No direct path:** without the tunnel, the job doesn't reach services on other MDG Labs servers.
  - **Site side:** Newt must reach the target. Newt installed **on the host** cannot resolve container names, which only exist in Docker's DNS. Newt running **as a container on the service's Docker network** reaches `<container name>:<port>` with no host port mapped.

## Rules

- **No `container:` jobs.** Every job runs on the host. Toolchains come from setup actions with pinned versions (`actions/setup-node`, `actions/setup-go`, `actions/setup-python`, `pnpm/action-setup`, …); system packages from `sudo apt-get install -y --no-install-recommends …`. A tool that only exists as an image runs as a step with `docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/w" -w /w <image> …`.
- **Service containers are reached on `localhost`.** Map only the container port (`ports: ['5432']`), never a fixed host port: Docker picks a free one, read it as `${{ job.services.postgres.ports['5432'] }}`. A fixed `5432:5432` clashes as soon as two runners run the job at once.
- **Nothing fixed per run on the shared daemon:** container names and compose project names are unique per run, e.g. built from `${{ github.run_id }}-${{ github.run_attempt }}` and the job name, and a host port of a `docker run -p` is `0` (read it with `docker port`). Remove what the job started in an `if: always()` step.
- **A container that writes into the checkout runs as `--user "$(id -u):$(id -g)"`,** so what it writes belongs to the runner user. A container that writes as root leaves files the next checkout cannot clean.
- **Temp files and caches:** `$RUNNER_TEMP` for files of this job, the runner user's `$HOME` (or the setup action's cache) for caches. Never a fixed path under `/tmp`.
- **Never rely on the runner for network devices or host changes.** A VPN or WireGuard tunnel (e.g. Pangolin) runs in a container started with `--cap-add NET_ADMIN --device /dev/net/tun`, and the commands that need the tunnel run in that container's network (`--network container:<id>`). Don't use `--privileged`.
- **Reaching a private service (e.g. a database for migrations)** goes through that tunnel shape. The secrets reach the containers only as `docker run -e NAME` from the job's env, never on a command line. Wait for `pangolin status` to show `Connected` before using the tunnel, and remove the client container in an `if: always()` step. Pangolin resources target `<container name>:<port>` through a containerised Newt on the server; never map a host port for this.
- **Install with pinned versions.**
- **On a GitHub-hosted public repo** the same workflow works unchanged; that is the point of host jobs.

## Running a workflow locally with act

A changed job is proven in the scratch clone with
`.claude/scripts/act-run.sh <checkout> <workflow file> <job id> [event]`, configured by `act` in `.claude/workflow.json`. Never call `act` directly, and never commit a `.actrc`: `act` applies `./.actrc` from the directory it starts in on top of its flags, so one could hand a job secrets. The script launches `act` outside the checkout with a minimal environment and an empty `HOME`, gives it no secrets, env, vars or inputs, refuses a job when it or anything it `needs` matches `act.neverRun`, reports PASS only when act says the job itself succeeded, and runs one `act` at a time per user.

What a local run is, measured with act 0.2.89 on 2026-10-17:

- **Not runners01.** The job runs in a container of the image `act.platforms` maps its `runs-on` label to, on the local Docker daemon: no daemon shared with four other runners, and an image that carries far more than the host baseline. A pass shows the steps work; it doesn't show the job fits on runners01.
- **`act` copies `GITHUB_TOKEN` or `GH_TOKEN` from its own environment** into `secrets.GITHUB_TOKEN` and `github.token`. Hence the minimal environment: under the script both are empty.
- **`${{ secrets.X }}` is empty,** so a step that needs a secret, OIDC or the GitHub API fails, or does nothing, for that reason alone. What only a real run can show (a deploy, a release, a `schedule`, a production-branch-only trigger) is listed as *proves after landing*, never forced into a local run.
- **`-j <job>` also runs the jobs it `needs`.** Hence the check against `neverRun` covers act's whole plan, including a needed job the dry run skips: its `if:` may read outputs only a real run sets.
- **A job its `if:` skips exits 0.** The script reports it as unproven (exit 5), never as a pass.

## Testing runner behaviour

When you need to know how the runners behave — anything this rule doesn't answer, or something you suspect has changed — test it in a throwaway workflow in a private test repo on the label the job would use (`[self-hosted, runners01]` or `[self-hosted, runners01-light]`), writing its findings to the step summary, never by trial and error in a product repo's workflows. Then record what you learned in this rule.
