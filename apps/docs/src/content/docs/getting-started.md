---
title: "Getting started"
description: "Docker Compose setup, environment variables, admin bootstrap, and troubleshooting."
---

# Getting started

Operator guide for self-hosting Release Ops with Docker Compose. For the full product contract, see [specs.html](/spec/).

## Prerequisites

- Docker and Docker Compose v2
- A host with port **3000** available (or change the published port in `docker-compose.yml`)

## Quick start

### 1. Configure environment variables

Copy the example env file and set the **required** secrets:

```bash
cp .env.example .env
```

Edit `.env` and replace the placeholder values. See [Environment variables](#environment-variables) below for the full list.

Generate secure values:

```bash
# SESSION_SECRET — 32+ random bytes (base64)
openssl rand -base64 32

# APP_ENCRYPTION_KEY — 64 hex characters (32 bytes)
openssl rand -hex 32
```

### 2. Start the container

```bash
docker compose up -d
```

This starts a **single** `release-ops` service (see [specs.html §10 Deployment](/spec/#deployment)):

- Image: `ghcr.io/mdg-labs/release-ops:latest` (or a local build from the repo `Dockerfile`)
- Port: `3000:3000`
- Volume: `release-ops-data` mounted at `/data` (SQLite `app.db`)
- Health check: `GET /api/go/healthz`

Wait until the container is healthy:

```bash
docker compose ps
```

### 3. Create the admin user

Choose **one** of the following methods.

#### Option A — Bootstrap env vars (recommended for first boot)

Set these in `.env` before `docker compose up`:

```bash
BOOTSTRAP_ADMIN_EMAIL=admin@example.com
BOOTSTRAP_ADMIN_PASSWORD=change-me-to-a-strong-password
```

On the **first** start, when the `users` table is empty, the server creates the admin automatically. **Remove these variables from `.env` after the first successful login** — they are one-time bootstrap only (see [specs.html §3 Authentication](/spec/#auth)).

#### Option B — `seed-admin` CLI via `docker compose exec`

If you started without bootstrap env vars, create the first admin inside the running container:

```bash
docker compose exec release-ops /app/seed-admin \
  --email admin@example.com \
  --password 'your-secure-password'
```

Interactive mode (prompts for email and password):

```bash
docker compose exec -it release-ops /app/seed-admin
```

Notes:

- The container must be running and healthy.
- `seed-admin` only works when **no users exist**; it exits with an error if an admin is already present.
- The CLI reads `APP_DB_PATH` from the container environment (default `/data/app.db`).
- For local development without Docker, run: `APP_DB_PATH=./data/app.db go run ./cmd/seed-admin --email … --password …`

### 4. Log in

Open [http://localhost:3000](http://localhost:3000), sign in with your admin credentials, and configure the app via the sidebar:

| Page | Purpose |
|------|---------|
| **Dashboard** | Operational status, last poll run, manual poll trigger |
| **Repos** | Monitored repositories |
| **Integrations** | Source and ticket provider credentials |
| **Ticket Projects** | Per-project status mapping and open-ticket policy |
| **Notifications** | Shoutrrr targets |
| **Settings** | Global poll interval (minutes, minimum 5) |

## Environment variables

All variables apply to the **single** container. Full spec reference: [specs.html §9](/spec/#env).

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SESSION_SECRET` | **yes** | — | Session cookie signing key (32+ random bytes) |
| `APP_ENCRYPTION_KEY` | **yes** | — | AES-256 credential encryption key (64 hex chars = 32 bytes) |
| `APP_PUBLIC_URL` | no | — | Public URL (e.g. `https://release-ops.example.com`) — enables `Secure` cookies over HTTPS |
| `GO_API_URL` | no | `http://127.0.0.1:8080` | Next.js proxy target for the Go API (same container) |
| `GO_INTERNAL_PORT` | no | `8080` | Go API bind port on `127.0.0.1` |
| `APP_DB_PATH` | no | `/data/app.db` | SQLite database path |
| `BOOTSTRAP_ADMIN_EMAIL` | no | — | One-time first-start admin email (only when no users exist) |
| `BOOTSTRAP_ADMIN_PASSWORD` | no | — | Pair with `BOOTSTRAP_ADMIN_EMAIL`; remove after first boot |
| `PORT` | no | `3000` | Public HTTP port (Next.js) |

`docker-compose.yml` passes `SESSION_SECRET`, `APP_ENCRYPTION_KEY`, `APP_PUBLIC_URL`, and optional bootstrap vars from `.env`. Other variables use container defaults unless you add them under `environment:`.

## Dashboard and manual poll

After configuring at least one integration, ticket project, and monitored repo:

1. Open **Dashboard** (`/`) to see the last poll run, per-repo status, and any errors.
2. Click **Run poll now** to trigger `POST /api/v1/poll/trigger` (async; returns 202).
3. Refresh or wait for the status card to update with the new run.
4. Adjust the global poll interval under **Settings** (`/settings`).

First poll on a new repo performs a **baseline** — the latest release tag is stored but **no ticket is created**. Subsequent polls create tickets only when the tag changes (see [specs.html §5.1](/spec/#domain)).

## Upgrading

Pull a newer image and recreate the container (data persists in the `release-ops-data` volume):

```bash
docker compose pull
docker compose up -d
```

Migrations run automatically on container start via `golang-migrate`.

## Troubleshooting

| Symptom | Check |
|---------|-------|
| Container unhealthy | `docker compose logs release-ops` — verify `SESSION_SECRET` and `APP_ENCRYPTION_KEY` are set |
| Cannot log in | Confirm admin was created (bootstrap env or `seed-admin`); no public sign-up in MVP |
| Poll errors on dashboard | **Integrations** → test connection; verify repo path and ticket project mapping |
| `seed-admin: users already exist` | Admin already created — use login or reset DB volume (destructive) |

## Further reading

- [mvp-checklist.md](/mvp-checklist/) — MVP acceptance criteria sign-off checklist
- [specs.html](/spec/) — architecture, APIs, providers, CI/CD
- [schema.html](/schema/) — database tables
