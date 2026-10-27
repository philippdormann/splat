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
FROM golang:1.26-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/errgotrack ./cmd/server
# distroless has no shell to mkdir/chown at runtime, so the data directory is
# created here (as root, in a stage that has a shell) and copied over with
# the nonroot ownership the final image's USER needs to write to it.
RUN mkdir -p /out/data && chown 65532:65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/errgotrack /app/errgotrack
COPY --from=build --chown=65532:65532 /out/data /data
USER nonroot:nonroot
EXPOSE 8080
VOLUME ["/data"]
ENV ERRGOTRACK_DB_PATH=/data/errgotrack.db
HEALTHCHECK --interval=30s --timeout=3s --retries=3 CMD ["/app/errgotrack", "-healthcheck"]
ENTRYPOINT ["/app/errgotrack"]
