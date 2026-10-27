# syntax=docker/dockerfile:1

# --- frontend: Svelte 5 + shadcn-svelte UI built with bun ---
# (vite outDir is ../web/dist, so the build lands in /app/web/dist)
FROM ghcr.io/philippdormann/bun:1.4.2 AS frontend
WORKDIR /app
COPY frontend/package.json frontend/bun.lock frontend/bunfig.toml ./frontend/
RUN cd frontend && bun install --frozen-lockfile
COPY frontend/ ./frontend/
RUN cd frontend && bun run build

# --- backend: Go binary with the Svelte build embedded ---
FROM golang:1.27.3-alpine3.24 AS build
ARG VERSION=dev
# ca-certificates is copied into the scratch final image below: without it
# every HTTPS call (Telegram/Slack/Discord/Teams/Sheets webhooks, SMTP with
# STARTTLS, forge APIs) would fail with x509 errors.
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/splat ./cmd/server
# Scratch has no shell to mkdir/chown at runtime, so the data directory is
# created here (as root, in a stage that has a shell) and copied over with
# the numeric ownership the final image's USER needs to write to it.
# Scratch also has no /etc/passwd, so USER must be numeric (65532 is the
# distroless "nonroot" uid, kept for continuity).
RUN mkdir -p /out/data && chown 65532:65532 /out/data

FROM scratch
WORKDIR /app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/splat /app/splat
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
EXPOSE 8080
VOLUME ["/data"]
ENV SPLAT_DB_PATH=/data/splat.db
HEALTHCHECK --interval=30s --timeout=3s --retries=3 CMD ["/app/splat", "-healthcheck"]
ENTRYPOINT ["/app/splat"]
