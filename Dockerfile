# syntax=docker/dockerfile:1

# The Renovate version the worker image is built on: the tag of the FROM line of the worker
# stage below, which stays equal to it (scripts/ci/check-renovate-pin.mjs, run in CI), as does
# internal/buildinfo. It feeds the NOTICE file; it does not pick the base image.
ARG WORKER_RENOVATE_TAG=44.239.1

FROM node:22-bookworm-slim AS web-deps
WORKDIR /app
COPY package.json package-lock.json ./
COPY apps/web/package.json ./apps/web/
RUN npm ci

FROM node:22-bookworm-slim AS web-build
WORKDIR /app
COPY --from=web-deps /app/node_modules ./node_modules
COPY package.json package-lock.json ./
COPY apps/web ./apps/web
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build -w apps/web

FROM golang:1.26-bookworm AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
ENV CGO_ENABLED=0
# Release tag (v1.2.3) or nightly-<short sha>; empty or unset builds as "dev".
ARG APP_VERSION=dev
RUN VERSION="${APP_VERSION:-dev}" && \
    go build -trimpath -ldflags="-s -w -X github.com/mdg-labs/release-ops/internal/buildinfo.Version=${VERSION}" -o /out/server ./cmd/server

# The worker image: the pinned Renovate image plus the release-ops binary. It has no data
# volume and no Docker socket. Two Unix users: "worker" runs `release-ops worker` and holds the
# worker secret; "renovate" is the user the worker starts Renovate as and cannot read that
# secret. Both run on /cache, which belongs to renovate and root's group (the group
# containerbase gives its tool directories), and the worker is a member of that group so it can
# create the per-slot cache directories inside it.
# The official Renovate OSS image (never a Mend Renovate CE or EE image), pinned by tag and
# digest; one version per release. Dependabot bumps both together. A different Renovate is a
# change to this line, at your own risk: the report and log handling is tested against the
# pinned version only.
FROM ghcr.io/renovatebot/renovate:44.239.1@sha256:7dba1a3b916a2cf302ba0df458ddcc8fcbac3c92d0fe9882ac13ba0363de656b AS worker
ARG WORKER_RENOVATE_TAG
USER root
# The binary carries the setuid and setgid file capabilities so the unprivileged worker can
# start Renovate as the renovate user under cap_drop ALL; it is not executable by renovate.
RUN apt-get update && \
    apt-get install -y --no-install-recommends libcap2-bin && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd --gid 12032 worker && \
    useradd --uid 12032 --gid worker --groups 0 --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin worker && \
    useradd --uid 12031 --gid 0 --create-home --home-dir /home/renovate --shell /bin/bash renovate && \
    install -d -o renovate -g 0 -m 2775 /cache
COPY --from=go-build --chown=worker:worker --chmod=0750 /out/server /usr/local/bin/release-ops
RUN setcap cap_setuid,cap_setgid+ep /usr/local/bin/release-ops
COPY --chmod=0644 NOTICE /NOTICE
RUN sed -i "s|@RENOVATE_VERSION@|${WORKER_RENOVATE_TAG}|g" /NOTICE
USER worker
ENTRYPOINT ["/usr/local/bin/release-ops", "worker"]
CMD []

FROM node:22-bookworm-slim AS runtime
ARG WORKER_RENOVATE_TAG
RUN apt-get update && \
    apt-get install -y --no-install-recommends bash ca-certificates curl wget && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=go-build /out/server /app/server
COPY docker/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh /app/server
# `release-ops admin login-link` (specs.html § 3.6) runs from a shell in this container.
RUN ln -s /app/server /usr/local/bin/release-ops

COPY --from=web-build /app/apps/web/.next/standalone ./
COPY --from=web-build /app/apps/web/.next/static ./apps/web/.next/static
COPY apps/web/preload/client-ip.cjs apps/web/preload/preload.cjs /app/preload/

COPY NOTICE /NOTICE
RUN sed -i "s|@RENOVATE_VERSION@|${WORKER_RENOVATE_TAG}|g" /NOTICE && \
    chmod 0644 /NOTICE

RUN mkdir -p /data && chown -R node:node /app /data

USER node

ENV NODE_ENV=production \
    PORT=3000 \
    HOSTNAME=0.0.0.0 \
    APP_DB_PATH=/data/app.db \
    GO_INTERNAL_PORT=8080 \
    GO_API_URL=http://127.0.0.1:8080

EXPOSE 3000
VOLUME ["/data"]

# Probes Next.js and, through its proxy, the Go API.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD curl -fsS -o /dev/null "http://127.0.0.1:${PORT}/api/go/healthz" || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
