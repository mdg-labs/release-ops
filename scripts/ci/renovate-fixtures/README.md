# Renovate fixtures

Small repositories the worker image's Renovate runs against in CI, so a bump of the pinned Renovate version that changes what it extracts, looks up or reports fails the pull request instead of the worker in production. Nothing here needs a token, a secret or a live host.

## How a Renovate bump is checked

Dependabot (`.github/dependabot.yml`, `docker` ecosystem) proposes a pull request that changes the `FROM ghcr.io/renovatebot/renovate:<tag>@sha256:<digest> AS worker` line of the root `Dockerfile`. Every pull request runs these jobs of `.github/workflows/ci.yml`; none is path-filtered, so each can be a required check (that is a repository setting the maintainer makes):

| Job | What it does |
| --- | --- |
| `Renovate pin` | `scripts/ci/check-renovate-pin.mjs` fails unless the worker stage is a literal `<x.y.z>@sha256:<digest>` and `ARG WORKER_RENOVATE_TAG` names the same version. It also runs the `node --test` suites of these scripts. |
| `Renovate fixtures` | `scripts/ci/renovate-fixtures.sh` builds the worker image and runs Renovate in it, as the `renovate` user, against each fixture. |
| `Worker run smoke test` | `docker/worker-run-smoke.sh` runs a `renovate_run` job through `release-ops worker` in the image (see below). |

A Dependabot pull request changes only the `FROM` line. It stays red until the same version is also set in `ARG WORKER_RENOVATE_TAG` of the `Dockerfile` and in `RenovateVersion` of `internal/buildinfo/buildinfo.go`; the pin check and `go test ./...` say which one is missing. Review the fixture job's result and Renovate's release notes before merging.

## The fixture run

For every directory under `repos/` the script runs `renovate --platform=local` with `--dry-run=extract` and, for fixtures with `lookup` expectations, `--dry-run=lookup`. Renovate runs as the `renovate` user with all capabilities dropped, `reportType=file` and a JSON log. The container's resolver knows no host name but `host.docker.internal`, where `fake-registries.mjs` serves a Docker registry, an npm registry and a Go module proxy with the fixed releases listed in that file, so a lookup cannot reach a live registry or GitHub and cannot change when upstream publishes. The GitHub Actions fixture is checked on extract only: looking up `github-tags` would call GitHub.

| Fixture | Manager (report key) | Lookup |
| --- | --- | --- |
| `dockerfile` | `dockerfile` | local registry |
| `docker-compose` | `docker-compose` | local registry |
| `github-actions` | `github-actions` | none |
| `gomod` | `gomod` | local Go proxy |
| `no-config` | none (no Renovate config; see below) | none |
| `npm` | `npm` | local npm registry |
| `regex` | `regex` (a custom regex manager in the fixture's `renovate.json`) | local npm registry |

The `no-config` fixture has a dependency file and no Renovate config, so under the run's `requireConfig=required` (with `onboarding=false`, as the worker's defaults set) Renovate skips the repository before it extracts anything. The worker records such a run as skipped for a missing config (`skippedForNoConfig` in `internal/renovate/handler.go`) when Renovate exits 0 and the report entry for the repository has `status: "disabled"`; the fixture proves that Renovate still does that, so a bump that changes the status fails here instead of silently stopping the `no_config` signal. Its expectation is `{ "noConfig": true }`: exit 0, the pinned version in the log, and that status, nothing else. Every other fixture has a config and must not report `disabled`.

`assert.mjs` then checks each run against `expectations.json`:

- Renovate exited 0, the log is JSON lines, and its `Repository started` record names the pinned Renovate version (so the image under test is the pinned one) and a `Repository finished` record follows.
- The report is valid JSON with one repository whose `branches` is an array and whose `packageFiles` is an object, the shape the app reads (`internal/updateprs/report.go`).
- The expected manager and package file hold exactly the expected dependencies (`depName`, `currentValue`, `datasource`; entries with a `skipReason` and those in `ignoreDatasources` are not counted). A fixture's `allowSkipReasons` names skip reasons it does count: such a dependency is checked like any other, including its expected `skipReason`. The GitHub Actions fixture runs without a GitHub token, so Renovate extracts its actions and skips them with `github-token-required`. On lookup each also has the expected `currentVersion` and exactly the expected `updates` (`newVersion`, `newValue`, `updateType`).
- Every fake registry received the requests that serve the lookups.
- With `--self-check`, each expectation is deliberately made wrong in several ways (another dependency, current version, datasource, manager, new version, update type, a missing entry) and the check must fail for each, so the assertions are not vacuous. `assert.test.mjs` does the same against synthetic reports.

Under `--dry-run=extract` and `lookup` Renovate does not write branches, so the report's `branches` stays empty and the branch name, PR number and the upgrade entries the app reads from a full run are not produced here. The dependency name, current and new version, datasource, manager and update type that those entries carry are checked where Renovate reports them in a dry run, on each dependency of `packageFiles`.

## Changing an expectation

When a Renovate bump legitimately changes what it reports:

1. Read the failing job's output: it names the fixture, the dependency and the field, with the value found and the value expected. The reports and logs of a failed run are the `renovate-fixtures` artifact of the workflow run.
2. Edit `expectations.json` to the new output, in the same pull request as the bump, and say in the description what changed in Renovate and why that is fine for `internal/updateprs`.
3. A new dependency or datasource that the fixtures should cover goes into the fixture repository under `repos/` (committed test data: no secrets, no token-shaped strings), its releases into `RELEASES` in `fake-registries.mjs` and its expectations into `expectations.json`.
4. `node --test "scripts/ci/**/*.test.mjs"` checks the assertion logic without Docker. The fixture run itself needs a Docker daemon: let CI run it.

If a change to the report breaks what the app reads, fix the app (`internal/updateprs`) or hold the bump back; do not weaken an expectation to get it through.

## The worker run smoke test

`docker/worker-run-smoke.sh` starts `release-ops worker` from the built image as the Compose service does (worker user, `cap_drop ALL`, setuid and setgid added, a cache volume) with a stand-in for the app's internal listener (`docker/worker-run-smoke/fake-app.mjs`), which also answers as the GitHub API. Two jobs run, each in its own container:

- `real`: the real `renovate` against an API that answers 401. It must run to an exit as the second Unix user, send its requests to the endpoint the worker derived with the job's token, leave no `job-*` scratch directory in `/cache/slot-0`, and the token must not appear in the posted result.
- `deadline`: a stand-in `renovate` (`docker/worker-run-smoke/shim/renovate`) that ignores SIGTERM and has a child process, run under a token that expires in about three minutes. The worker must end it at the deadline with the `timeout` error code, SIGKILL included, for the whole process group (no process of the `renovate` user may remain), without a "signalling the process group failed" warning: the worker holds no `CAP_KILL`, so the signals can only arrive through the helper it starts as the `renovate` user. The job scratch directory must exist while the job runs and be gone afterwards. This scenario takes about three minutes.

These paths run only in the image, where both Unix users exist and the worker is not root, so no Go test covers them.
