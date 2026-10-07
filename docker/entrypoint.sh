#!/usr/bin/env bash
set -euo pipefail

go_pid=""
next_pid=""

stop_children() {
  for pid in "$next_pid" "$go_pid"; do
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill -TERM "$pid" 2>/dev/null || true
    fi
  done
  for pid in "$next_pid" "$go_pid"; do
    if [[ -n "$pid" ]]; then
      wait "$pid" 2>/dev/null || true
    fi
  done
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM
  stop_children
  exit "$status"
}
trap cleanup EXIT INT TERM

/app/server &
go_pid=$!

go_ready=false
for _ in $(seq 1 150); do
  if ! kill -0 "$go_pid" 2>/dev/null; then
    break
  fi
  if node -e "fetch('http://127.0.0.1:${GO_INTERNAL_PORT:-8080}/healthz').then((r) => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))" 2>/dev/null; then
    go_ready=true
    break
  fi
  sleep 0.2
done

if [[ "$go_ready" != true ]]; then
  echo "entrypoint: Go server did not become healthy; exiting" >&2
  exit 1
fi

export HOSTNAME="${HOSTNAME:-0.0.0.0}"
export PORT="${PORT:-3000}"
export GO_API_URL="${GO_API_URL:-http://127.0.0.1:8080}"

cd /app
node apps/web/server.js &
next_pid=$!

# Exit as soon as either process dies so the container stops and the restart policy
# (or the orchestrator) can bring it back; cleanup stops the survivor.
status=0
wait -n "$go_pid" "$next_pid" || status=$?
if ! kill -0 "$go_pid" 2>/dev/null; then
  echo "entrypoint: Go server exited (status ${status}); stopping container" >&2
else
  echo "entrypoint: Next.js server exited (status ${status}); stopping container" >&2
fi
if [[ "$status" -eq 0 ]]; then
  status=1
fi
exit "$status"
