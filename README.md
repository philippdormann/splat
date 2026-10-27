# Splat

> Forked from [raiden70/errgotrack](https://github.com/raiden70/errgotrack) and renamed as it diverged.

**A lightweight, self-hosted, Sentry-compatible error tracker.** One Go
binary, SQLite by default (Postgres optional), a small Docker image. No
Redis, no runtime dependencies, no CDN. The dashboard is a Svelte 5 +
shadcn-svelte SPA built with bun and embedded in the binary. Point your
app's official Sentry SDK at it and it just works - built to run
comfortably on a small VPS instead of needing a cluster.

![Splat dashboard](docs/screenshot-dashboard.png)

## Why

Sentry (self-hosted) and even lighter forks like GlitchTip or Bugsink are
real, mature products - but they're still a multi-service stack with real
memory and operational overhead. Splat is the "I just want to see my
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
  reopen automatically if they recur (or pin a resolve to a release so it
  only reopens on a *different* release); ignored issues never reopen, and
  muted issues stay silent - with optional expiry - while still counting
  events.
- **A real dashboard** - events over time, top exceptions, error
  distribution by project/environment, new-vs-recurring issues, all backed
  by live SQL aggregates, not placeholder charts. Per-project overviews add
  severity-stacked volume, release health, and top issues.
- **Rich issue detail** - full stack traces with surrounding source lines
  and captured local variables, tags, breadcrumbs (with their structured
  data), user/request context, and Sentry's `contexts` object (browser, OS,
  device, custom frameworks) all rendered, not just the exception message.
  Step through individual event occurrences, inspect the reconstructed raw
  event JSON, and leave operator notes on the issue.
- **Source map resolution** - upload `.map` files per release + bundle URL
  and minified JS frames resolve to the original file/line/function with
  source context.
- **Release health** - SDK session envelopes are stored and aggregated into
  crash-free sessions/users per release, with a releases view and session
  health trends. No APM/tracing backend required.
- **Logs and traces from the same SDKs** - structured log records (`log`
  items) are stored per project with level/search/trace filters, and
  transaction items become browsable traces with span waterfalls, so the
  `trace_id` on an issue opens the real local trace (plus logs in that
  trace) instead of only linking out. Releases roll up events, sessions,
  and transactions alike.
- **Repository integration** - link a project to GitHub, GitLab, Gitea, or
  Forgejo and file Splat issues there with one click (title/body
  prefilled, backlink included), then track them as linked external issues.
- **Notifications on Telegram, Slack, Discord, Microsoft Teams, Google
  Sheets, email, and generic webhooks** - configure any combination; each delivers independently through a durable
  retry queue, so one channel's outage never blocks or duplicates another's
  alert, and none of them can block event ingestion itself. Generic
  webhooks post signed JSON (`X-Splat-Signature`) with custom headers
  to any HTTPS endpoint.
- **Trace correlation** - if an event carries a `trace_id` (from an SDK
  wired for distributed tracing), link straight out to whatever tracing
  backend you already run (Jaeger, Tempo, Honeycomb, ...), and opens the matching local trace when the SDK's spans were ingested. Splat
  stores spans for its own trace view - it stays an error tracker, not an APM.
- **Multi-project, with per-platform setup snippets** - create a project
  from a visual platform picker, get the right install command + init
  code with your real DSN already filled in. A one-click test event
  verifies the whole pipeline and jumps straight to its issue.
- **Search, filters, bulk actions** - full-text search over messages and
  exception types (SQLite FTS5), filter by project/environment/status
  (including muted), bulk resolve/ignore/mute.
- **API keys with scopes** - `egt_…` Bearer tokens for automation
  (`ingest`/`read`/`write`), optionally project-bound and expiring, hashes
  only on disk, secret shown once.
- **Multi-user with teams and project grants** - instance admins manage
  users, teams, and per-project access; members only ever see granted
  projects across the dashboard, issues, and API (ungranted projects read
  as 404, never 403).
- **Configurable retention** - old events age out in the background without
  ever touching issue history (counts, first/last seen stay intact).
- **Automatic dark/light theme, keyboard-friendly, no CDN** - the dashboard is a
  Svelte 5 + shadcn-svelte SPA (built with bun, embedded in the binary),
  with every runtime asset served locally. The theme follows the OS color
  scheme live, with no manual toggle.
- **Deploys in one command** - a small Docker image published to
  `ghcr.io/philippdormann/splat` (`:latest`, `:1`, `:1.2`, `:1.2.3`
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
| Bun | ✅ setup snippet + protocol verified (`@sentry/bun` speaks the same envelope) |
| Java (plain JVM, Spring Boot, Quarkus) | ✅ core SDK verified directly; Spring Boot/Quarkus wrap the same core |
| Next.js | ⚠️ protocol/payload shape verified, not a full Next.js build |
| JavaScript (browser) | ⚠️ same core protocol as Node, not separately run |
| Other / any Sentry SDK | Should work - it's the same wire protocol everywhere |

## Quickstart

**Docker:**

```bash
docker run -d --name splat -p 8080:8080 \
  -v splat-data:/data \
  -e SPLAT_SESSION_SECRET=$(openssl rand -hex 32) \
  -e SPLAT_ADMIN_USERNAME=admin \
  -e SPLAT_ADMIN_PASSWORD=$(openssl rand -base64 24) \
  -e SPLAT_PUBLIC_URL=http://localhost:8080 \
  ghcr.io/philippdormann/splat:latest
```

Images are published by CI to `ghcr.io/philippdormann/splat` on every
push to `main` (`:latest`, `:sha-<commit>`) and on every version tag
(`:1.2.3`, `:1.2`, `:1`, `:latest`, `:sha-<commit>` - so tag `1.2.3`
publishes all four version tags at once, never with a `v` prefix), for both `linux/amd64` and
`linux/arm64` - see [`.github/workflows/docker.yml`](.github/workflows/docker.yml).
To pin a version: `ghcr.io/philippdormann/splat:1.2.3` (or `:1` / `:1.2`
to float on patch/minor releases).

**Or run it directly with Go, no Docker at all:**

Requires Go 1.27+ (`go build` auto-downloads the matching toolchain if
your installed Go is older, per the `go` directive in `go.mod`) and
[bun](https://bun.sh) 1.x for the frontend build.

```bash
make build   # bun install + bun run build (frontend/) + go build, UI embedded

SPLAT_SESSION_SECRET=$(openssl rand -hex 32) \
SPLAT_ADMIN_USERNAME=admin \
SPLAT_ADMIN_PASSWORD=$(openssl rand -base64 24) \
SPLAT_PUBLIC_URL=http://localhost:8080 \
./bin/splat
```

Or skip the explicit build and just `go run ./cmd/server` with the same
environment variables, if you're iterating rather than deploying (run
`make frontend-build` once first so the embedded UI exists).

Then:

1. Open http://localhost:8080 and log in.
2. Create a project, picking your platform from the grid.
3. Copy the DSN and install snippet shown - it already has your real DSN
   filled in. No app handy? Hit "Send test event" on the project page to
   verify the whole pipeline and jump straight to its issue.
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
| `SPLAT_ADDR` | `:8080` | listen address |
| `SPLAT_DB_DRIVER` | `sqlite` | `sqlite` (default, zero-ops) or `postgres` |
| `SPLAT_DB_PATH` | `data/splat.db` | SQLite file path (sqlite driver only) |
| `SPLAT_DATABASE_URL` | - | Postgres connection URL, e.g. `postgres://splat:splat@localhost:5432/splat?sslmode=disable` (postgres driver only, **required** there) |
| `SPLAT_PUBLIC_URL` | `http://localhost:8080` | used to build DSNs and alert links; `https://` here also makes session cookies `Secure` |
| `SPLAT_ADMIN_USERNAME` / `SPLAT_ADMIN_PASSWORD` | - | bootstraps the first admin user when no users exist yet; ignored afterwards (manage users in the UI) |
| `SPLAT_SESSION_SECRET` | - | **required**; any random 32+ byte string |
| `SPLAT_RETENTION_SWEEP` | `1h` | how often the retention sweeper runs |
| `SPLAT_METRICS` | off | `1`/`true` exposes Prometheus metrics at `GET /metrics` |
| `SPLAT_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |

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
- Performance tracing / transactions, profiling, session replay, client
  reports, metrics. This is an error tracker, not an APM platform - see
  Trace correlation above for the lightweight alternative. (Session
  envelopes *are* stored, but only for release-health aggregation.)
- Attachments (binary item payloads are parsed per envelope framing but not
  persisted).
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
ignoring it. A muted issue stays silent while counting events unless its
mute expiry passes. Resolving "in the next release" pins the resolve to
the current release: same-release events keep it resolved, a different
release reopens it.

## Dashboard

Svelte 5 + shadcn-svelte SPA in [`frontend/`](frontend/), built with bun
(`bun install && bun run build`) and embedded into the Go binary as
`web/dist` - the server only serves the built `index.html` + fingerprinted
assets and a JSON API under `/api/v1/` (same-origin, session-cookie auth).
The theme follows the OS color scheme live.
Charts use shadcn-svelte chart components over layerchart, lucide-svelte icons. For local UI iteration,
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

**Postgres (optional):** set `SPLAT_DB_DRIVER=postgres` +
`SPLAT_DATABASE_URL` (see [`.env.example`](.env.example) and
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

## Users, teams and access control

Two instance roles: **admins** bypass every check; **members** only see
projects granted to them, directly (project page → Access card) or through
a **team** (Settings → Users & Teams → grant the team to projects). Grants
are binary - there are no per-project roles. Everything else follows:

- Dashboard, issue lists, and project lists scope to a member's grants; an
  ungranted project reads as `404`, never `403`, so membership can't be
  probed. Members never reach Settings, user/team/key management, or
  project create/delete.
- `SPLAT_ADMIN_USERNAME`/`SPLAT_ADMIN_PASSWORD` bootstrap the
  first admin only when no users exist. Create the rest in the UI.
- Safety guards: the last user can't be deleted, admins can't be demoted
  (or change their own role), and admins can't delete their own account
  out from under their session.

**Two-factor authentication** (TOTP): each user enables it for
themselves on the Security page — scan the QR code with any
authenticator app, verify once, and store the ten single-use recovery
codes (each works once, shown only at setup). Sign-in becomes two
steps: password first, then the authenticator or a recovery code; the
password step mints a 5-minute signed challenge instead of a session,
and both steps share the login brute-force budget. Admins can require
2FA for every login (Settings → General): users without a factor
enroll one inline before their session is issued, and can never
disable it while required. Locked out (lost device, no codes)? An
admin resets 2FA per user in Settings → Users & Teams. TOTP secrets
are AES-256-GCM encrypted in the database (keyed by
`SPLAT_SESSION_SECRET`); recovery codes are stored as hashes. API keys
are separate automation credentials and are unaffected by 2FA.

**Single sign-on** (generic OIDC: Keycloak, Authentik, Google, …):
set `SPLAT_OIDC_ISSUER`, `SPLAT_OIDC_CLIENT_ID`, and
`SPLAT_OIDC_CLIENT_SECRET` (optionally `SPLAT_OIDC_NAME` and
`SPLAT_OIDC_SCOPES`), register
`{SPLAT_PUBLIC_URL}/api/v1/sso/callback` at the provider, and restart.
First login auto-provisions a member; a matching verified email links
a pre-created account (admins can set the email at user creation).
Local TOTP is bypassed for SSO sessions — MFA lives at the provider.
Admins can disable password login in Settings → General for SSO-only
mode (refused until a provider is configured, so nobody locks the
instance out). Only RS256 ID tokens are accepted; the flow uses
HMAC-bound state, nonce, and PKCE, and every SSO sign-in is
audit-logged.

**Audit log** (Settings → Audit log, admins only): every sign-in
(success and failure), 2FA change, user/team/role change, settings
save, API key change, project change, and quota/policy change is
appended with actor, IP, and timestamp. Entries are never edited or
swept — retention is yours to decide. Filter by action prefix
(`auth.`, `users.`, …) in the UI or via
`GET /api/v1/audit-log?action=&limit=&offset=`.

**Ingestion quotas** (spike protection): instance defaults (5000
events/hour, 50000/day — Settings → General) with per-project
overrides on each project's Settings tab (`null` inherits, `0`
unlimited). Only stored error events count, over trailing hour/day
windows. Over-quota ingestion is rejected with `429` + `Retry-After`
(test events through the UI bypass quotas), and the operator is
alerted once per hour per project through the normal alert channels.
Usage and breach state are visible on the project page; every change
is audit-logged.

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

Covers: envelope/store/session parsing (including length-prefixed items with
embedded newlines), auth/project resolution, fingerprinting and grouping
edge cases, concurrent ingestion, migrations (idempotent re-open, FK
enforcement, on both SQLite and Postgres), FTS5/ILIKE search and pagination,
notification formatting/retry/
backoff/cooldowns across all channels and their independence from
each other, retention deletion vs. issue summary preservation, and
multi-user/team access scoping (grants, team grants, admin-only instance
routes, 404-not-403 for ungranted projects), log/transaction ingestion and
per-trace assembly, forge issue creation against stub APIs for all four
providers, and
JSON-escaping of untrusted event data (a regression test serializes an issue
with a `<script>` payload in the exception type/tags/project
name through the JSON API and asserts it never appears unescaped - stored
XSS is the main injection risk here, since exception messages and tags come
straight from the instrumented app; the Svelte frontend additionally renders
all API values as text, never as raw HTML).

## Deployment

**Docker:**

```bash
cp .env.example .env   # set SPLAT_SESSION_SECRET, SPLAT_ADMIN_USERNAME/PASSWORD
docker compose -f compose.sqlite.yaml up -d --build
# ...or with Postgres instead of SQLite:
docker compose -f compose.postgres.yaml up -d --build
```

Runs as a non-root user (uid 65532) on a `scratch` runtime image built
from `golang:1.27.3-alpine3.24` - no OS layer, so no OS CVEs to track,
with the CA bundle copied in for HTTPS notifications. SQLite data on
a named volume (or set `SPLAT_DB_DRIVER=postgres` +
`SPLAT_DATABASE_URL` in `.env` to use the bundled `postgres` service
instead - SQLite stays the default), health-checked via the binary's own
`-healthcheck` flag (the runtime image has no shell/curl for a traditional
`HEALTHCHECK`).

**Standalone binary**, no Docker:

```bash
make build
SPLAT_SESSION_SECRET=... SPLAT_ADMIN_USERNAME=... SPLAT_ADMIN_PASSWORD=... \
SPLAT_PUBLIC_URL=https://errors.example.com ./bin/splat
```

Put it behind a reverse proxy for TLS - see
[`deploy/Caddyfile`](deploy/Caddyfile) or [`deploy/nginx.conf`](deploy/nginx.conf).
`SPLAT_PUBLIC_URL` must use `https://` for session/CSRF cookies to be
marked `Secure`.

**Upgrading:** stop the container/process, replace the binary/image, start
it again - migrations in `internal/database/migrations/` run automatically
and are idempotent.

**Backup (SQLite):** the entire database is one file (plus its `-wal`/`-shm`
sidecars while running). Simplest safe backup:

```bash
sqlite3 /data/splat.db ".backup /backup/splat-$(date +%F).db"
```

(`.backup` is safe against a concurrently-running writer; `cp`-ing the file
directly while the server is live is not.) **Restore:** stop the server,
replace the file, start it again. **Backup (Postgres):** use `pg_dump` /
your provider's snapshots as usual.

## Health, metrics & security

- `GET /healthz` - liveness (`{"status":"ok"}`), no database touch.
- `GET /readyz` - readiness: pings the database, returns version + uptime,
  `503` when the DB is unreachable. `GET /api/v1/meta` exposes the same
  build metadata publicly.
- `GET /metrics` - Prometheus exposition (request counts by route, ingested
  / rejected events, DB gauges). Off unless `SPLAT_METRICS=1`.
- Every response carries a strict Content-Security-Policy (the SPA has no
  inline scripts or third-party fetches), `X-Frame-Options: DENY`, and
  `X-Robots-Tag: noindex, nofollow`, plus a deny-all `robots.txt`.
- Webhook URLs are SSRF-guarded at save time (no private/link-local
  targets); generic webhooks sign bodies with HMAC-SHA256. API keys store
  hashes only.

## What's deliberately not here

- No APM (metrics, profiling, session replay, span search across projects)
  - transactions and logs are ingested and viewable per project, but there
  is no sampling, alerting, or aggregation on them. See Trace correlation
  above for linking out instead.
- No per-project roles or SSO - membership is binary (access or not) and
  auth is username + password with bcrypt hashes, plus optional TOTP
  two-factor authentication.

## Layout

```
cmd/server/            entrypoint, wiring, graceful shutdown
frontend/              Svelte 5 + shadcn-svelte SPA (bun, vite, tailwind,
                       layerchart charts); `bun run build` emits web/dist,
                       embedded in the binary
internal/sentryprotocol/  envelope + store + session parsing, auth/DSN parsing
internal/ingestion/    HTTP handlers for the Sentry-compatible endpoints
internal/issues/       fingerprinting, grouping, status transitions, notes
internal/events/       event storage, FTS5 (sqlite) / ILIKE (postgres) search
internal/dashboard/    aggregate queries behind the dashboard/stat panels
internal/sessions/     session ingestion, crash-free math, release rollups
internal/projects/     project CRUD, DSN/key rotation
internal/platforms/    per-framework setup snippets shown on a project's page
internal/notifications/   alert rules, durable per-channel queue
internal/email/        stdlib SMTP delivery
internal/telegram/ + internal/webhooks/   channel-specific formatting/delivery clients
internal/tracing/      trace_id -> external tracing backend URL correlation
internal/auth/         users, instance roles, login sessions, CSRF, TOTP 2FA
internal/teams/        teams, memberships, project grants, access checks
internal/logs/         structured SDK log records, level/search/trace queries
internal/traces/       transactions and spans, per-trace assembly
internal/forge/        GitHub/GitLab/Gitea/Forgejo issue APIs
internal/repolink/     linked repos and external issue links (storage)
internal/apikeys/      Bearer keys, scopes, expiry, rotation
internal/sourcemaps/   upload + dependency-free v3 resolution
internal/storage/      usage reporting, manual sweep + vacuum
internal/metrics/      dependency-free Prometheus exposition
internal/retention/    bounded-batch event cleanup
internal/database/     sqlite (default) + postgres backends, embedded migrations
internal/httpserver/   routes, middleware, JSON API (/api/v1/), SPA serving
web/dist               built Svelte UI, embedded into the binary (gitignored)
examples/django/       settings snippet + a view that deliberately throws
tests/                 the real-SDK integration script
```

## License

[MIT](LICENSE)
