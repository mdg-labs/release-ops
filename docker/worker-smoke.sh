#!/usr/bin/env bash
# Smoke test of the worker image, run by CI after it is built: no token and no live GitHub.
# Renovate runs as the renovate user with binarySource=install, the tools mount and one
# allowedCommands command against a fixture repository (--platform=local), and the worker user
# hands it a cache directory. Needs a Docker daemon; CI is the only place it runs.
#
# Usage: docker/worker-smoke.sh <worker image>
set -euo pipefail

image=${1:?usage: $0 <worker image>}
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
smoke="$here/worker-smoke"

volume="worker-smoke-cache-$$"
cleanup() { docker volume rm -f "$volume" >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker volume create "$volume" >/dev/null

fail() { echo "worker-smoke: $*" >&2; exit 1; }
as_user() { local user=$1; shift; docker run --rm --user "$user" --entrypoint /bin/bash "$@"; }

echo "== image configuration"
[[ "$(docker image inspect --format '{{.Config.User}}' "$image")" == worker ]] || fail "the image does not run as the worker user"
docker image inspect --format '{{json .Config.Volumes}}' "$image" | grep -Eq '^(null|\{\})$' || fail "the image declares a volume"
as_user worker "$image" -c 'test -s /NOTICE && grep -q AGPL /NOTICE && ! grep -q "@RENOVATE_VERSION@" /NOTICE' || fail "NOTICE is missing, has no licence notice or an unfilled version"

echo "== users"
[[ "$(as_user worker "$image" -c 'id -un')" == worker ]] || fail "the worker user is missing"
[[ "$(as_user renovate "$image" -c 'id -un')" == renovate ]] || fail "the renovate user is missing"
[[ "$(as_user worker "$image" -c 'id -u')" != "$(as_user renovate "$image" -c 'id -u')" ]] || fail "worker and renovate share a uid"
as_user renovate "$image" -c '! test -r /usr/local/bin/release-ops && ! test -x /usr/local/bin/release-ops' || fail "renovate can read or run the release-ops binary"

echo "== the worker creates a slot cache directory the renovate user writes to"
as_user worker -v "$volume:/cache" "$image" -c 'mkdir -p /cache/slot-0 && chmod 2775 /cache/slot-0' || fail "the worker cannot create a slot cache directory"
as_user renovate -v "$volume:/cache" "$image" -c 'touch /cache/slot-0/probe && mkdir -p /cache/slot-0/renovate' || fail "renovate cannot write to the slot cache directory"

echo "== renovate as the renovate user: binarySource=install, tools mount, allowedCommands"
out=$(mktemp)
trap 'rm -f "$out"; cleanup' EXIT
# The script for the container is single-quoted on purpose: it expands there, not here.
# shellcheck disable=SC2016
as_user renovate \
  -v "$volume:/cache" \
  -v "$smoke/tools:/opt/renovate-tools:ro" \
  -v "$smoke/config.json:/config/config.json:ro" \
  -v "$smoke/fixture:/fixture:ro" \
  -w /fixture \
  -e RENOVATE_CONFIG_FILE=/config/config.json \
  -e RENOVATE_BINARY_SOURCE=install \
  -e RENOVATE_CACHE_DIR=/cache/slot-0/renovate \
  -e RENOVATE_DRY_RUN=lookup \
  -e RENOVATE_PLATFORM=local \
  -e LOG_LEVEL=debug \
  "$image" -c '
    set -euo pipefail
    [[ "$(id -un)" == renovate ]]
    install-tool node 22.12.0
    node --version
    out=$(/opt/renovate-tools/hello.sh)
    [[ "$out" == "tools-mount-ok renovate" ]]
    ! touch /opt/renovate-tools/probe 2>/dev/null
    renovate --platform=local
  ' 2>&1 | tee "$out" || fail "renovate did not complete as the renovate user"

grep -q 'left-pad' "$out" || fail "renovate did not extract the fixture dependency"
echo "worker-smoke: ok"
