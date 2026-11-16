#!/usr/bin/env bash
# Runs the worker image's Renovate against the fixture repositories and checks what it reports,
# with no token and no live host: --platform=local, extract for every fixture and lookup for the
# fixtures served by the local registry fakes. Renovate runs as the renovate user, as the worker
# starts it, with every capability dropped and a resolver that knows no host name except the
# one the fakes are reached at. Needs a Docker daemon and node; CI is the only place it runs.
#
# Usage: scripts/ci/renovate-fixtures.sh <worker image>
# RENOVATE_FIXTURES_RESULTS=<dir> keeps the reports and logs there instead of in a temporary
# directory that is removed at the end.
# Expectations and how to change them: scripts/ci/renovate-fixtures/README.md
set -euo pipefail

image=${1:?usage: $0 <worker image>}
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
fx="$here/renovate-fixtures"

fail() { echo "renovate-fixtures: $*" >&2; exit 1; }

read -r version _ < <(node "$here/check-renovate-pin.mjs") || fail "the Dockerfile does not pin Renovate"

work=$(mktemp -d)
results=${RENOVATE_FIXTURES_RESULTS:-$work/results}
mkdir -p "$results"
volume="renovate-fixtures-cache-$$"
registries_pid=""
cleanup() {
  [[ -z $registries_pid ]] || kill "$registries_pid" 2>/dev/null || true
  docker volume rm -f "$volume" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT
docker volume create "$volume" >/dev/null

echo "== local registries"
node "$fx/fake-registries.mjs" --ports-file "$work/ports.json" --requests-file "$work/requests.log" &
registries_pid=$!
deadline=$((SECONDS + 20))
until [[ -s $work/ports.json ]] || ((SECONDS > deadline)); do sleep 0.2; done
[[ -s $work/ports.json ]] || fail "the local registries did not start"
port() { node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))[process.argv[2]])' "$work/ports.json" "$1"; }
host=host.docker.internal
docker_url="http://$host:$(port docker)"
npm_url="http://$host:$(port npm)"
go_url="http://$host:$(port go)"

cat >"$work/config.json" <<EOF
{
  "onboarding": false,
  "requireConfig": "required",
  "packageRules": [
    { "matchDatasources": ["docker"], "registryUrls": ["$docker_url"] },
    { "matchDatasources": ["npm"], "registryUrls": ["$npm_url"] }
  ]
}
EOF

run_renovate() {
  local fixture=$1 mode=$2
  local out="$results/$fixture/$mode"
  mkdir -p "$out"
  chmod 0777 "$out"
  local rc=0
  docker run --rm \
    --user renovate \
    --cap-drop ALL \
    --dns 127.0.0.1 \
    --add-host "$host:host-gateway" \
    --entrypoint renovate \
    -v "$volume:/cache" \
    -v "$fx/repos/$fixture:/fixture:ro" \
    -v "$work/config.json:/config/config.json:ro" \
    -v "$out:/out" \
    -w /fixture \
    -e RENOVATE_CONFIG_FILE=/config/config.json \
    -e RENOVATE_BASE_DIR="/cache/$fixture-$mode/base" \
    -e RENOVATE_CACHE_DIR="/cache/$fixture-$mode/renovate" \
    -e RENOVATE_REPORT_TYPE=file \
    -e RENOVATE_REPORT_PATH=/out/report.json \
    -e GOPROXY="$go_url" \
    -e LOG_FORMAT=json \
    -e LOG_LEVEL=debug \
    "$image" --platform=local "--dry-run=$mode" >"$out/log.ndjson" 2>&1 || rc=$?
  echo "$rc" >"$out/exit_code"
  if [[ $rc != 0 ]]; then
    echo "renovate exited with $rc for $fixture ($mode); the end of its log:" >&2
    tail -n 20 "$out/log.ndjson" >&2
  fi
}

for repo in "$fx"/repos/*/; do
  fixture=$(basename "$repo")
  echo "== $fixture"
  run_renovate "$fixture" extract
  if node -e 'process.exit(JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))[process.argv[2]].lookup ? 0 : 1)' "$fx/expectations.json" "$fixture"; then
    run_renovate "$fixture" lookup
  fi
done

echo "== the report against the expectations"
node "$fx/assert.mjs" --results "$results" --renovate-version "$version" || fail "a report differs from its expectations"

echo "== deliberately wrong expectations fail"
node "$fx/assert.mjs" --results "$results" --renovate-version "$version" --self-check || fail "the assertions pass for an expectation that is wrong"

echo "== lookups were answered by the local registries"
for want in \
  "docker /v2/fixture/base/tags/list" "docker /v2/fixture/web/tags/list" \
  "npm /fixture-lib" "npm /fixture-dev-lib" "npm /fixture-tool" \
  "go /github.com/fixture-org/golib/@v/list"; do
  grep -qF -- "$want" "$work/requests.log" || fail "no request for: $want"
done
echo "renovate-fixtures: ok (Renovate $version)"
