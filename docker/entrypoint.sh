#!/usr/bin/env bash
set -euo pipefail

APP_DB_PATH="${APP_DB_PATH:-/data/app.db}"
mkdir -p "$(dirname "$APP_DB_PATH")"

/app/migrate -path /app/migrations -database "sqlite://${APP_DB_PATH}" up

/app/server &
go_pid=$!

cleanup() {
  local status=$?
  if kill -0 "$go_pid" 2>/dev/null; then
    kill -TERM "$go_pid" 2>/dev/null || true
    wait "$go_pid" 2>/dev/null || true
  fi
  exit "$status"
}
trap cleanup EXIT INT TERM

for _ in $(seq 1 30); do
  if node -e "fetch('http://127.0.0.1:${GO_INTERNAL_PORT:-8080}/healthz').then((r) => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))" 2>/dev/null; then
    break
  fi
  sleep 0.2
done

export HOSTNAME="${HOSTNAME:-0.0.0.0}"
export PORT="${PORT:-3000}"
export GO_API_URL="${GO_API_URL:-http://127.0.0.1:8080}"

cd /app
node apps/web/server.js &
next_pid=$!
wait "$next_pid"
