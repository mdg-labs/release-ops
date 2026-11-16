#!/usr/bin/env bash
# Smoke test of a renovate_run job through `release-ops worker` in the built worker image, run by
# CI after it is built: no token and no live GitHub. A local stand-in for the app hands the worker
# one job and a job token, and doubles as the GitHub API. The container runs as the Compose
# service does: the worker user, every capability dropped but setuid and setgid, a per-slot cache
# volume. Two jobs run, each in its own worker container:
#   real      the real renovate binary against an API that answers 401; the job must run as the
#             second Unix user, reach the API endpoint with the job's token and leave no scratch
#             directory behind;
#   deadline  a stand-in for renovate that ignores SIGTERM, started under a token that expires
#             in minutes; the deadline must stop it, SIGKILL included, for its whole process
#             group, which the worker, without CAP_KILL, can only do through the helper it
#             starts as the second user (internal/renovate/process.go).
# Needs a Docker daemon and node; CI is the only place it runs.
#
# Usage: docker/worker-run-smoke.sh <worker image>
set -euo pipefail

image=${1:?usage: $0 <worker image>}
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
smoke="$here/worker-run-smoke"
host=host.docker.internal

fail() { echo "worker-run-smoke: $*" >&2; exit 1; }

work=$(mktemp -d)
volume="worker-run-smoke-cache-$$"
container="worker-run-smoke-$$"
fake_pid=""
cleanup() {
  local rc=$?
  if ((rc != 0)); then
    echo "worker-run-smoke: the worker's log:" >&2
    docker logs "$container" >&2 2>&1 || true
    echo "worker-run-smoke: what the stand-in for the app saw:" >&2
    cat "$work"/*/meta.json >&2 2>/dev/null || true
  fi
  [[ -z $fake_pid ]] || kill "$fake_pid" 2>/dev/null || true
  docker rm -f "$container" >/dev/null 2>&1 || true
  docker volume rm -f "$volume" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT
docker volume create "$volume" >/dev/null

renovate_uid=$(docker run --rm --entrypoint id "$image" -u renovate)
[[ $renovate_uid =~ ^[0-9]+$ ]] || fail "the image has no renovate user"
WORKER_SECRET=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')
export WORKER_SECRET

in_container() { docker exec "$container" /bin/bash -c "$@"; }

# The scripts run in the container are single-quoted on purpose: they expand there, not here.
#
# The pids of the processes in the container owned by a uid. A zombie does not count: the worker
# is the container's init and does not reap the orphans of a killed process group.
# shellcheck disable=SC2016
pids_of_uid() {
  in_container '
    for status in /proc/[0-9]*/status; do
      uid="" state=""
      while read -r key value _; do
        [[ $key == State: ]] && state=$value
        [[ $key == Uid: ]] && uid=$value
      done <"$status" 2>/dev/null || true
      [[ $uid == "$1" && $state != Z ]] && basename "$(dirname "$status")"
    done
    true' bash "$1"
}

# shellcheck disable=SC2016
job_dirs() { in_container 'ls -d "/cache/slot-$1"/job-* 2>/dev/null || true' bash "$1"; }

# run_scenario <name> <slot> <token life in seconds> <wait in seconds> <fake-app flags> -- <docker flags>
run_scenario() {
  local name=$1 slot=$2 life=$3 wait=$4
  shift 4
  local fake_flags=() docker_flags=()
  while [[ $# -gt 0 && $1 != -- ]]; do fake_flags+=("$1"); shift; done
  shift
  docker_flags=("$@")

  echo "== $name"
  local state="$work/$name"
  mkdir -p "$state"
  node "$smoke/fake-app.mjs" --secret "$WORKER_SECRET" --state-dir "$state" --slot "$slot" --token-life "$life" "${fake_flags[@]}" &
  fake_pid=$!
  local deadline=$((SECONDS + 20))
  until [[ -s $state/port ]] || ((SECONDS > deadline)); do sleep 0.2; done
  [[ -s $state/port ]] || fail "$name: the stand-in for the app did not start"
  local port url
  port=$(cat "$state/port")
  url="http://$host:$port"

  docker run -d --name "$container" \
    --cap-drop ALL --cap-add SETUID --cap-add SETGID \
    --dns 127.0.0.1 \
    --add-host "$host:host-gateway" \
    -v "$volume:/cache" \
    -v "$smoke/config.json:/config/config.json:ro" \
    -e WORKER_SECRET \
    -e "INTERNAL_URL=$url" \
    -e "GITHUB_BASE_URL=$url" \
    "${docker_flags[@]}" \
    "$image" >/dev/null

  # A failed docker exec stops the script: only an assignment passes the status of its command
  # substitution on.
  local saw_job_dir=no saw_process=no dirs procs
  deadline=$((SECONDS + wait))
  until [[ -s $state/result.json ]] || ((SECONDS > deadline)); do
    [[ $(docker inspect -f '{{.State.Running}}' "$container") == true ]] || fail "$name: the worker container stopped"
    dirs=$(job_dirs "$slot")
    [[ -z $dirs ]] || saw_job_dir=yes
    procs=$(pids_of_uid "$renovate_uid")
    [[ -z $procs ]] || saw_process=yes
    sleep 1
  done
  [[ -s $state/result.json ]] || { docker logs "$container" >&2 || true; fail "$name: the worker posted no result within ${wait}s"; }

  # The scratch directory is removed before the result is posted, and what Renovate left running
  # is killed before that.
  [[ $(docker inspect -f '{{.State.Running}}' "$container") == true ]] || fail "$name: the worker container stopped"
  dirs=$(job_dirs "$slot")
  [[ -z $dirs ]] || fail "$name: a job scratch directory is left in /cache/slot-$slot"
  procs=$(pids_of_uid "$renovate_uid")
  [[ -z $procs ]] || fail "$name: a process of the renovate user is still running after the job"
  # shellcheck disable=SC2016
  in_container 'test -d "/cache/slot-$1/renovate" && test -d "/cache/slot-$1/base"' bash "$slot" || fail "$name: the slot's cache directories are missing"
  local logs
  logs=$(docker logs "$container" 2>&1)
  grep -q 'renovate: starting' <<<"$logs" || fail "$name: the worker never started renovate"
  if grep -q 'signalling the process group failed' <<<"$logs"; then
    fail "$name: the worker could not signal the renovate process group"
  fi

  if [[ $name == deadline ]]; then
    [[ $saw_job_dir == yes ]] || fail "$name: no job scratch directory was seen while the job ran"
    [[ $saw_process == yes ]] || fail "$name: no process of the renovate user was seen while the job ran"
  fi
  node "$smoke/check-result.mjs" "$name" "$state" "$renovate_uid" || fail "$name: the result is not what was expected"

  docker rm -f "$container" >/dev/null
  kill "$fake_pid" 2>/dev/null || true
  wait "$fake_pid" 2>/dev/null || true
  fake_pid=""
}

run_scenario real 0 600 240 --

shim_flags=(-v "$smoke/shim:/shim:ro" -e "PATH=/shim:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
run_scenario deadline 1 195 300 --github silent -- "${shim_flags[@]}"

echo "worker-run-smoke: ok"
