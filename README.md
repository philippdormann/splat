# Errgotrack

**A lightweight, self-hosted, Sentry-compatible error tracker.** One Go
binary, SQLite by default (Postgres optional), a small Docker image. No
Redis, no Node build step at runtime, no CDN at runtime. Point your app's
official Sentry SDK at it and it just works - built to run comfortably on
a small VPS instead of needing a cluster.

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
- **Automatic dark/light theme, keyboard-friendly, no CDN** - the dashboard is a
  Svelte 5 + shadcn-svelte SPA (built with bun, embedded in the binary),
  with every runtime asset served locally. The theme follows the OS color
  scheme live, with no manual toggle.
- **Deploys in one command** - a small Docker image published to
  `ghcr.io/philippdormann/errgotrack` (`:latest`, `:1`, `:1.2`, `:1.2.3`
  on tags), non-root, with its own health check (no shell needed), or a
  single static binary with no dependencies at all.

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
docker run -d --name errgotrack -p 8080:8080 \
  -v errgotrack-data:/data \
  -e ERRGOTRACK_SESSION_SECRET=$(openssl rand -hex 32) \
  -e ERRGOTRACK_ADMIN_USERNAME=admin \
  -e ERRGOTRACK_ADMIN_PASSWORD='change-me' \
  -e ERRGOTRACK_PUBLIC_URL=http://localhost:8080 \
  ghcr.io/philippdormann/errgotrack:latest
```

Images are published by CI to `ghcr.io/philippdormann/errgotrack` on every
push to `main` (`:latest`, `:sha-<commit>`) and on every `v*` tag
(`:1.2.3`, `:1.2`, `:1`, `:latest`, `:sha-<commit>` - so tag `v1.2.3`
publishes all four version tags at once), for both `linux/amd64` and
`linux/arm64` - see [`.github/workflows/docker.yml`](.github/workflows/docker.yml).
To pin a version: `ghcr.io/philippdormann/errgotrack:1.2.3` (or `:1` / `:1.2`
to float on patch/minor releases).

**Or run it directly with Go, no Docker at all:**

Requires Go 1.26+ (`go build` auto-downloads the matching toolchain if
your installed Go is older, per the `go` directive in `go.mod`) and
[bun](https://bun.sh) 1.x for the frontend build.

```bash
make build   # bun install + bun run build (frontend/) + go build, UI embedded

ERRGOTRACK_SESSION_SECRET=$(openssl rand -hex 32) \
ERRGOTRACK_ADMIN_USERNAME=admin \
ERRGOTRACK_ADMIN_PASSWORD='change-me' \
ERRGOTRACK_PUBLIC_URL=http://localhost:8080 \
./bin/errgotrack
```

Or skip the explicit build and just `go run ./cmd/server` with the same
environment variables, if you're iterating rather than deploying (run
`make frontend-build` once first so the embedded UI exists).

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
| `ERRGOTRACK_DB_DRIVER` | `sqlite` | `sqlite` (default, zero-ops) or `postgres` |
| `ERRGOTRACK_DB_PATH` | `data/errgotrack.db` | SQLite file path (sqlite driver only) |
| `ERRGOTRACK_DATABASE_URL` | - | Postgres connection URL, e.g. `postgres://errgotrack:errgotrack@localhost:5432/errgotrack?sslmode=disable` (postgres driver only, **required** there) |
| `ERRGOTRACK_PUBLIC_URL` | `http://localhost:8080` | used to build DSNs and alert links; `https://` here also makes session cookies `Secure` |
| `ERRGOTRACK_ADMIN_USERNAME` / `ERRGOTRACK_ADMIN_PASSWORD` | - | bootstraps the one admin account on first run only |
| `ERRGOTRACK_SESSION_SECRET` | - | **required**; any random 32+ byte string |
| `ERRGOTRACK_RETENTION_SWEEP` | `1h` | how often the retention sweeper runs |
| `ERRGOTRACK_METRICS` | off | `1`/`true` exposes Prometheus metrics at `GET /metrics` |

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

Svelte 5 + shadcn-svelte SPA in [`frontend/`](frontend/), built with bun
(`bun install && bun run build`) and embedded into the Go binary as
`web/dist` - the server only serves the built `index.html` + fingerprinted
assets and a JSON API under `/api/v1/` (same-origin, session-cookie auth).
The theme follows the OS color scheme live.
Chart.js for charts, lucide-svelte icons. For local UI iteration,
`cd frontend && bun run dev` proxies `/api` to a Go server on `:8080`.

## Notifications

Six channels, any combination:

- **Telegram** - bot token + chat id.
- **Slack**, **Discord**, **Microsoft Teams** - a single incoming-webhook
  URL each, simpler than Telegram's bot setup.
- **Google Sheets** - paste the URL of a Google Apps Script Web App you
  deploy yourself (snippet provided on the Settings page); it appends one
  row per alert. No Google credentials are ever stored on the server.
- **Email** - plain-text alerts over SMTP (STARTTLS, optional auth) to the
  on-call address in Settings.

Configure under Settings and use "Send test" to confirm delivery. Alert
**rules** decide which events notify and where: each rule matches a project
(blank = all), environment, and trigger (new issue, reopened issue,
event-count threshold), with its own cooldown; a rule with no channels uses
every enabled channel, while picking channels routes e.g. production to
email and staging to chat. Rules are managed in Settings and via
`/api/v1/rules`.

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

Each project has a `retention_days` setting (default `0` = keep events
forever). A background sweeper
(`internal/retention`) deletes events older than that in bounded batches of
500 rows, so it never holds a long-running transaction that would stall
ingestion. **Issue summaries (title, counts, first/last seen) are a
separate table and are never deleted by retention** - only the individual
event rows backing them age out.

## Storage

SQLite by default via `modernc.org/sqlite` (pure Go, no CGO - a single
static binary, no libsqlite3 to manage). WAL mode, `busy_timeout=5000`,
foreign keys on, and a capped connection pool (8), since SQLite has exactly
one writer regardless of pool size. Issue-counter updates run under
`BEGIN IMMEDIATE` to serialize concurrent ingestion correctly instead of
racing a read-then-write (see `internal/issues/store.go` and the concurrency
test in `internal/issues/store_test.go`, which fires 40 concurrent events at
the same fingerprint and checks the final count is exactly 40, with
`-race` clean).

Event search uses an FTS5 virtual table over message/exception
type/value on SQLite, kept in sync via triggers, so free-text search doesn't
scan every row.

**Postgres (optional):** set `ERRGOTRACK_DB_DRIVER=postgres` +
`ERRGOTRACK_DATABASE_URL` (see [`.env.example`](.env.example) and
[`compose.postgres.yaml`](compose.postgres.yaml), which runs a `postgres`
service alongside the app). Schema and migrations are ported 1:1
(`internal/database/migrations/postgres/`); timestamps stay RFC3339 TEXT on
both backends so every store query works unchanged. Backend differences are
isolated to small call sites: `?` placeholders are rebound to `$n`
(`internal/database.DB`), inserts use `RETURNING id` instead of
`LastInsertId`, issue upserts use a transaction + `SELECT ... FOR UPDATE`
instead of `BEGIN IMMEDIATE`, free-text search uses `ILIKE` (+ a GIN
tsvector index) instead of FTS5, and time bucketing uses `to_char` instead
of `strftime`. The `?`-placeholder queries in every store work on both
backends untouched thanks to the `database.DB` wrapper.

Settings shows live disk usage (database size + per-table row counts) with
one-click **retention sweep now** and **vacuum** (`GET /api/v1/storage`,
`POST /api/v1/storage/sweep`, `POST /api/v1/storage/vacuum`).

## Source maps

Upload a `.map` file per project + release + bundle URL (project page, or
`POST /api/v1/projects/{id}/sourcemaps` as JSON or multipart) and minified
JS frames in issue detail resolve to the original file/line, function name,
and surrounding source (from `sourcesContent`). Matching is exact-then-
suffix on the bundle URL, so `/static/app.min.js` matches a frame from
`https://cdn.example.com/static/app.min.js?v=2`; maps uploaded without a
release act as fallback. Re-uploading replaces; deleting returns frames to
minified. The mappings consumer is hand-rolled (base64-VLQ, no dependency).

## API

Everything the SPA does goes through `GET/POST /api/v1/...` (session
cookie auth, `401` JSON when missing), documented by the embedded OpenAPI
3.1 spec at `GET /api/v1/openapi.json` - the TypeScript client in
`frontend/src/lib/api.ts` mirrors it. Press `⌘K`/`Ctrl+K` anywhere for the
command palette (navigation, actions, project search); `Shift+I/P/S` jump
to Issues/Projects/Settings.

**API keys** (`Authorization: Bearer egt_…`, managed in Settings, admin
session only) cover automation without a browser session: `ingest` posts
events to the Sentry endpoints without a DSN key, `read`/`write` use the
management API. Keys are optionally bound to one project (confined to that
project's ingestion and reads; instance routes stay admin/unbound) and can
expire. Only SHA256 hashes are stored - the secret is shown once at
creation/rotation; rotate, revoke, or delete any time.

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
enforcement, on both SQLite and Postgres), FTS5/ILIKE search and pagination,
notification formatting/retry/
backoff/cooldowns across all four channels and their independence from
each other, retention deletion vs. issue summary preservation, and
JSON-escaping of untrusted event data (a regression test serializes an issue
with a `<script>` payload in the exception type/tags/project
name through the JSON API and asserts it never appears unescaped - stored
XSS is the main injection risk here, since exception messages and tags come
straight from the instrumented app; the Svelte frontend additionally renders
all API values as text, never as raw HTML).

## Deployment

**Docker:**

```bash
cp .env.example .env   # set ERRGOTRACK_SESSION_SECRET, ERRGOTRACK_ADMIN_USERNAME/PASSWORD
docker compose -f compose.sqlite.yaml up -d --build
# ...or with Postgres instead of SQLite:
docker compose -f compose.postgres.yaml up -d --build
```

Runs as a non-root user on a `distroless/static` base image, SQLite data on
a named volume (or set `ERRGOTRACK_DB_DRIVER=postgres` +
`ERRGOTRACK_DATABASE_URL` in `.env` to use the bundled `postgres` service
instead - SQLite stays the default), health-checked via the binary's own
`-healthcheck` flag (the runtime image has no shell/curl for a traditional
`HEALTHCHECK`).

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

**Backup (SQLite):** the entire database is one file (plus its `-wal`/`-shm`
sidecars while running). Simplest safe backup:

```bash
sqlite3 /data/errgotrack.db ".backup /backup/errgotrack-$(date +%F).db"
```

(`.backup` is safe against a concurrently-running writer; `cp`-ing the file
directly while the server is live is not.) **Restore:** stop the server,
replace the file, start it again. **Backup (Postgres):** use `pg_dump` /
your provider's snapshots as usual.

## What's deliberately not here

- No performance tracing / APM, profiling, or session replay - this is an
  error tracker. See Trace correlation above for linking out instead.
- No multi-user/team/RBAC model - one admin account, matching "a private
  dashboard on a VPS."
- No source map resolution for minified JS stacks.

## Layout

```
cmd/server/            entrypoint, wiring, graceful shutdown
frontend/              Svelte 5 + shadcn-svelte SPA (bun, vite, tailwind);
                       `bun run build` emits web/dist, embedded in the binary
internal/sentryprotocol/  envelope + store parsing, auth/DSN parsing
internal/ingestion/    HTTP handlers for the Sentry-compatible endpoints
internal/issues/       fingerprinting, grouping, status transitions
internal/events/       event storage, FTS5 (sqlite) / ILIKE (postgres) search
internal/dashboard/    aggregate queries behind the dashboard/stat panels
internal/projects/     project CRUD, DSN/key rotation
internal/platforms/    per-framework setup snippets shown on a project's page
internal/notifications/   alert rules, durable per-channel queue
internal/telegram/ + internal/webhooks/   channel-specific formatting/delivery clients
internal/tracing/      trace_id -> external tracing backend URL correlation
internal/auth/         admin login, sessions, CSRF
internal/retention/    bounded-batch event cleanup
internal/database/     sqlite (default) + postgres backends, embedded migrations
internal/httpserver/   routes, middleware, JSON API (/api/v1/), SPA serving
web/dist               built Svelte UI, embedded into the binary (gitignored)
examples/django/       settings snippet + a view that deliberately throws
tests/                 the real-SDK integration script
```

## License

[MIT](LICENSE)
