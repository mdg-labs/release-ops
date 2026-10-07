# syntax=docker/dockerfile:1

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
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM node:22-bookworm-slim AS runtime
RUN apt-get update && \
    apt-get install -y --no-install-recommends bash ca-certificates curl wget && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=go-build /out/server /app/server
COPY docker/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh /app/server

COPY --from=web-build /app/apps/web/.next/standalone ./
COPY --from=web-build /app/apps/web/.next/static ./apps/web/.next/static

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
