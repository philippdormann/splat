# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/errgotrack ./cmd/server
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
ENTRYPOINT ["/app/errgotrack"]
