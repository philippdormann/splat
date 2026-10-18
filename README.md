# Errgotrack

**A lightweight, self-hosted, Sentry-compatible error tracker.** One Go
binary, one SQLite file, a 15MB Docker image. No Redis, no Postgres, no
Node build step, no CDN at runtime. Point your app's official Sentry SDK at
it and it just works - built to run comfortably on a small VPS instead of
needing a cluster.

![Errgotrack dashboard](docs/screenshot.png)

## Why

Sentry (self-hosted) and even lighter forks like GlitchTip or Bugsink are
real, mature products - but they're still a multi-service stack with real
memory and operational overhead. Errgotrack is the "I just want to see my
app's exceptions, with alerts, on a $5 VPS" version: a single static binary
that embeds its own frontend, talks the real Sentry wire protocol, and
keeps its entire database in one SQLite file you can back up with one
command.

## Features

- **Sentry-compatible ingestion** - the real envelope + legacy store
  protocol, not a custom API. Any official Sentry SDK works unmodified;
  verified against the real Python, Node, and Java SDKs, not just
  hand-crafted test payloads.
- **Automatic issue grouping** - repeated occurrences of the same bug
  collapse into one issue instead of flooding your list; resolved issues
  reopen automatically if they recur.
- **A real dashboard** - events over time, top exceptions, error
  distribution by project/environment, new-vs-recurring issues, all backed
  by live SQL aggregates, not placeholder charts.
- **Rich issue detail** - full stack traces with surrounding source lines
  and captured local variables, tags, breadcrumbs (with their structured
  data), user/request context, and Sentry's `contexts` object (browser, OS,
  device, custom frameworks) all rendered, not just the exception message.
- **Notifications on Telegram, Slack, Discord, Microsoft Teams, and Google
  Sheets** - configure any combination; each delivers independently through a durable
  retry queue, so one channel's outage never blocks or duplicates another's
  alert, and none of them can block event ingestion itself.
- **Trace correlation** - if an event carries a `trace_id` (from an SDK
  wired for distributed tracing), link straight out to whatever tracing
  backend you already run (Jaeger, Tempo, Honeycomb, ...). Errgotrack
  doesn't store spans itself - it stays an error tracker, not an APM.
- **Multi-project, with per-platform setup snippets** - create a project,
  pick its framework from a dropdown, get the right install command + init
  code with your real DSN already filled in.
- **Search, filters, bulk actions** - full-text search over messages and
  exception types (SQLite FTS5), filter by project/environment/status, bulk
  resolve/ignore.
- **Configurable retention** - old events age out in the background without
  ever touching issue history (counts, first/last seen stay intact).
- **Dark/light theme, keyboard-friendly, no CDN** - every asset (HTMX,
  Chart.js, icons) is embedded in the binary.
- **Deploys in one command** - a 15MB Docker image, non-root, with its own
  health check (no shell needed), or a single static binary with no
  dependencies at all.

## Supported platforms

Any official Sentry SDK speaks the same protocol, so this list is really
"what has a ready-made setup snippet in the UI today," not a hard
compatibility wall:

| Platform | Status |
|---|---|
| Python - Django | ✅ verified with the real SDK, real request-handling exception |
| Python - Flask | ✅ verified with the real SDK, real request-handling exception |
| Python - FastAPI | ✅ verified with the real SDK, real request-handling exception |
| Node.js | ✅ verified with `@sentry/node` |
| Java (plain JVM, Spring Boot, Quarkus) | ✅ core SDK verified directly; Spring Boot/Quarkus wrap the same core |
| Next.js | ⚠️ protocol/payload shape verified, not a full Next.js build |
| JavaScript (browser) | ⚠️ same core protocol as Node, not separately run |
| Other / any Sentry SDK | Should work - it's the same wire protocol everywhere |

## Quickstart

**Docker:**

```bash
docker build -t errgotrack:latest .

docker run -d --name errgotrack -p 8080:8080 \
  -v errgotrack-data:/data \
  -e ERRGOTRACK_SESSION_SECRET=$(openssl rand -hex 32) \
  -e ERRGOTRACK_ADMIN_USERNAME=admin \
  -e ERRGOTRACK_ADMIN_PASSWORD='change-me' \
  -e ERRGOTRACK_PUBLIC_URL=http://localhost:8080 \
  errgotrack:latest
```

(If you've pushed this repo to GitHub, CI builds and publishes the image to
`ghcr.io/<you>/errgotrack:latest` on every push to `main` - see
[`.github/workflows/docker.yml`](.github/workflows/docker.yml) - so you can
`docker pull` that instead of building locally.)

**Or run it directly with Go, no Docker at all:**

Requires Go 1.26+ (`go build` auto-downloads the matching toolchain if
your installed Go is older, per the `go` directive in `go.mod`).

```bash
go build -o bin/errgotrack ./cmd/server

ERRGOTRACK_SESSION_SECRET=$(openssl rand -hex 32) \
ERRGOTRACK_ADMIN_USERNAME=admin \
ERRGOTRACK_ADMIN_PASSWORD='change-me' \
ERRGOTRACK_PUBLIC_URL=http://localhost:8080 \
./bin/errgotrack
```

Or skip the explicit build and just `go run ./cmd/server` with the same
environment variables, if you're iterating rather than deploying.

Then:

1. Open http://localhost:8080 and log in.
2. Create a project, picking your framework from the dropdown.
3. Copy the DSN and install snippet shown - it already has your real DSN
   filled in.
4. Trigger a real exception in your app. It should show up on the
   dashboard within a second.

No app handy to test with? Run
[`tests/integration_sentry_sdk.sh`](tests/integration_sentry_sdk.sh): it
spins up a throwaway server, creates a project through the real HTTP API,
and sends an exception through the official `sentry-sdk` Python package -
useful for confirming the server itself works before wiring up anything.

## Configuration

All configuration is environment variables (see `internal/config/config.go`):

| Variable | Default | Purpose |
|---|---|---|
| `ERRGOTRACK_ADDR` | `:8080` | listen address |
| `ERRGOTRACK_DB_PATH` | `data/errgotrack.db` | SQLite file path |
| `ERRGOTRACK_PUBLIC_URL` | `http://localhost:8080` | used to build DSNs and alert links; `https://` here also makes session/CSRF cookies `Secure` |
| `ERRGOTRACK_ADMIN_USERNAME` / `ERRGOTRACK_ADMIN_PASSWORD` | - | bootstraps the one admin account on first run only |
| `ERRGOTRACK_SESSION_SECRET` | - | **required**; any random 32+ byte string |
| `ERRGOTRACK_RETENTION_SWEEP` | `1h` | how often the retention sweeper runs |

## Sentry protocol compatibility

Implements enough of the [envelope](https://develop.sentry.dev/sdk/envelopes/)
and legacy [store](https://develop.sentry.dev/sdk/store/) endpoints for
official Sentry SDKs to work unmodified. Verified against real SDK
installs (Python, Node, Java), not just hand-crafted payloads - see
`tests/integration_sentry_sdk.sh`.

**Supported:**
- Envelope (`POST /api/<project>/envelope/`) and legacy store
  (`POST /api/<project>/store/`) endpoints, both gzip or plain.
- `X-Sentry-Auth` header and DSN-embedded key authentication.
- Error events: exception type/value/module, full stacktrace (file, line,
  function, module, context line + surrounding source, captured local
  variables, in-app flag), chained exceptions.
- Timestamps (unix float or ISO8601), level, logger, platform, release,
  environment, transaction/culprit.
- Tags (object or legacy array form), extra context, breadcrumbs (with
  structured data), user context, request context, SDK metadata, and the
  free-form `contexts` object (browser, OS, device, runtime, trace,
  framework-specific contexts).
- Client-supplied `fingerprint` overrides automatic grouping.
- Bare message events (no exception) with their own grouping.
- Captured local variables of any shape - some SDKs repr() a local down to
  a plain string, others (e.g. Flask with a dict-valued local) send actual
  nested JSON; both are handled.

**Explicitly not implemented** (accepted and silently dropped where the
protocol allows it, so a real SDK never errors, but nothing is stored or
shown for these):
- Performance tracing / transactions, profiling, session replay, release
  health "sessions", client reports, metrics. This is an error tracker, not
  an APM platform - see Trace correlation above for the lightweight
  alternative.
- Attachments (binary item payloads are parsed per envelope framing but not
  persisted).
- Source map resolution / server-side symbolication of minified stacks.
- `X-Sentry-Rate-Limits` backoff signaling to the SDK (the server rate-limits
  by IP; it doesn't yet tell a misbehaving client to slow down).

## How issues are grouped

Events are grouped by a fingerprint: a client-supplied `fingerprint` wins if
present, otherwise it's derived from the exception type plus the
module+function of each **in-app** stack frame (`internal/issues/fingerprint.go`).
The exception *message* is deliberately excluded, since messages often embed
request-specific values (`"User 42 not found"` vs `"User 99 not found"`)
that would otherwise fragment one bug into many issues.

A resolved issue that sees a new occurrence flips back to `unresolved`
("reopened"). An ignored issue does not auto-reopen - that's the point of
ignoring it.

## Dashboard

Server-rendered Go templates + HTMX for interactivity + a small hand-written
CSS theme (light/dark via `prefers-color-scheme` and a manual toggle) +
Chart.js for charts. No Tailwind build step: deliberately a plain CSS file
with a small utility-class layer instead, to avoid needing a Node/Tailwind
toolchain in the build. HTMX, Chart.js and all icons (inlined Lucide SVGs)
are vendored under `web/static/`, so the running app needs no CDN or
internet access to render.

## Notifications

Five channels, any combination:

- **Telegram** - bot token + chat id.
- **Slack**, **Discord**, **Microsoft Teams** - a single incoming-webhook
  URL each, simpler than Telegram's bot setup.
- **Google Sheets** - paste the URL of a Google Apps Script Web App you
  deploy yourself (snippet provided on the Settings page); it appends one
  row per alert. No Google credentials are ever stored on the server.

Configure under Settings and use "Send test" to confirm delivery. Alerts
fire for new issues and reopened issues by default (5 minute cooldown per
issue, shared across every enabled channel so one trigger doesn't spam
through all four); per-project/environment rules and occurrence thresholds
live in the `notification_rules` table.

Delivery goes through a durable SQLite queue, one row per channel per
alert, drained by a background worker with exponential backoff. This means:
a channel being down or misconfigured never blocks or fails event
ingestion, and it never blocks or duplicates delivery to the *other*
enabled channels either.

## Trace correlation

Not full APM - see "Explicitly not implemented" above - but when an event
carries a `trace_id` (from any SDK configured with distributed tracing),
the issue page shows it with a copy button, and - if you've configured a
URL template under Settings (e.g. `https://tempo.example.com/explore?traceID={trace_id}`) -
a direct link into whatever tracing backend you already run.

## Retention

Each project has a `retention_days` setting. A background sweeper
(`internal/retention`) deletes events older than that in bounded batches of
500 rows, so it never holds a long-running transaction that would stall
ingestion. **Issue summaries (title, counts, first/last seen) are a
separate table and are never deleted by retention** - only the individual
event rows backing them age out.

## Storage

SQLite via `modernc.org/sqlite` (pure Go, no CGO - a single static binary,
no libsqlite3 to manage). WAL mode, `busy_timeout=5000`, foreign keys on,
and a capped connection pool (8), since SQLite has exactly one writer
regardless of pool size. Issue-counter updates run under `BEGIN IMMEDIATE`
to serialize concurrent ingestion correctly instead of racing a
read-then-write (see `internal/issues/store.go` and the concurrency test in
`internal/issues/store_test.go`, which fires 40 concurrent events at the
same fingerprint and checks the final count is exactly 40, with
`-race` clean).

Event search uses an FTS5 virtual table over message/exception
type/value, kept in sync via triggers, so free-text search doesn't scan
every row.

PostgreSQL isn't implemented. If you outgrow single-writer SQLite, the
natural path is swapping `internal/database` for a `pgx`-backed
implementation behind the same store interfaces - nothing else depends on
SQLite specifics except the FTS5 search query and the `BEGIN IMMEDIATE`
locking trick, both isolated to small, easy to find call sites.

## Benchmark

```
$ go test -bench=. -benchmem -run '^$' ./internal/ingestion/...
BenchmarkIngestEventNewIssue-8     2340    469734 ns/op   15602 B/op   268 allocs/op
BenchmarkIngestEventSameIssue-8    2824    418826 ns/op   15453 B/op   264 allocs/op
```

(Apple M2, realistic payload: 4-frame stacktrace, 2 breadcrumbs, user/tags/
request context - see `internal/ingestion/bench_test.go`.) That's roughly
2,000-2,400 single-threaded events/sec against on-disk SQLite, which is far
beyond what a small app's worker processes will produce.

## Testing

```bash
go test ./...                 # unit + integration tests against temp SQLite DBs
go test -race ./...           # same, with the race detector
./tests/integration_sentry_sdk.sh   # real sentry-sdk client, real HTTP server
```

Covers: envelope/store parsing (including length-prefixed items with
embedded newlines), auth/project resolution, fingerprinting and grouping
edge cases, concurrent ingestion, migrations (idempotent re-open, FK
enforcement), FTS5 search and pagination, notification formatting/retry/
backoff/cooldowns across all four channels and their independence from
each other, retention deletion vs. issue summary preservation, and
HTML-escaping of untrusted event data (a regression test renders an issue
detail page with a `<script>` payload in the exception type/tags/project
name and asserts it never appears unescaped - stored XSS is the main
injection risk here, since exception messages and tags come straight from
the instrumented app).

## Deployment

**Docker:**

```bash
cp .env.example .env   # set ERRGOTRACK_SESSION_SECRET, ERRGOTRACK_ADMIN_USERNAME/PASSWORD
docker compose up -d
```

Runs as a non-root user on a `distroless/static` base image (15MB), SQLite
data on a named volume, health-checked via the binary's own `-healthcheck`
flag (the runtime image has no shell/curl for a traditional `HEALTHCHECK`).

**Standalone binary**, no Docker:

```bash
make build
ERRGOTRACK_SESSION_SECRET=... ERRGOTRACK_ADMIN_USERNAME=... ERRGOTRACK_ADMIN_PASSWORD=... \
ERRGOTRACK_PUBLIC_URL=https://errors.example.com ./bin/errgotrack
```

Put it behind a reverse proxy for TLS - see
[`deploy/Caddyfile`](deploy/Caddyfile) or [`deploy/nginx.conf`](deploy/nginx.conf).
`ERRGOTRACK_PUBLIC_URL` must use `https://` for session/CSRF cookies to be
marked `Secure`.

**Upgrading:** stop the container/process, replace the binary/image, start
it again - migrations in `internal/database/migrations/` run automatically
and are idempotent.

**Backup:** the entire database is one file (plus its `-wal`/`-shm`
sidecars while running). Simplest safe backup:

```bash
sqlite3 /data/errgotrack.db ".backup /backup/errgotrack-$(date +%F).db"
```

(`.backup` is safe against a concurrently-running writer; `cp`-ing the file
directly while the server is live is not.) **Restore:** stop the server,
replace the file, start it again.

## What's deliberately not here

- No performance tracing / APM, profiling, or session replay - this is an
  error tracker. See Trace correlation above for linking out instead.
- No multi-user/team/RBAC model - one admin account, matching "a private
  dashboard on a VPS."
- No PostgreSQL backend yet (see Storage above).
- No source map resolution for minified JS stacks.

## Layout

```
cmd/server/            entrypoint, wiring, graceful shutdown
internal/sentryprotocol/  envelope + store parsing, auth/DSN parsing
internal/ingestion/    HTTP handlers for the Sentry-compatible endpoints
internal/issues/       fingerprinting, grouping, status transitions
internal/events/       event storage, FTS5 search
internal/dashboard/    aggregate queries behind the dashboard/stat panels
internal/projects/     project CRUD, DSN/key rotation
internal/platforms/    per-framework setup snippets shown on a project's page
internal/notifications/   alert rules, durable per-channel queue
internal/telegram/ + internal/webhooks/   channel-specific formatting/delivery clients
internal/tracing/      trace_id -> external tracing backend URL correlation
internal/auth/         admin login, sessions, CSRF
internal/retention/    bounded-batch event cleanup
internal/database/     SQLite open + embedded migrations
internal/httpserver/   routes, middleware, template rendering
web/                   templates + vendored static assets (embedded into the binary)
examples/django/       settings snippet + a view that deliberately throws
tests/                 the real-SDK integration script
```

## License

[MIT](LICENSE)
