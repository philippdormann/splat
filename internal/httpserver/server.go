// Package httpserver wires HTTP routes and middleware for the JSON API and
// the embedded Svelte SPA. Event ingestion itself lives in
// internal/ingestion; this package mounts it alongside the API.
package httpserver

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"time"

	"github.com/philippdormann/splat/internal/apikeys"
	"github.com/philippdormann/splat/internal/audit"
	"github.com/philippdormann/splat/internal/auth"
	"github.com/philippdormann/splat/internal/dashboard"
	"github.com/philippdormann/splat/internal/database"
	"github.com/philippdormann/splat/internal/events"
	"github.com/philippdormann/splat/internal/ingestion"
	"github.com/philippdormann/splat/internal/invites"
	"github.com/philippdormann/splat/internal/issues"
	"github.com/philippdormann/splat/internal/logs"
	"github.com/philippdormann/splat/internal/metrics"
	"github.com/philippdormann/splat/internal/notifications"
	"github.com/philippdormann/splat/internal/projects"
	"github.com/philippdormann/splat/internal/quotas"
	"github.com/philippdormann/splat/internal/repolink"
	"github.com/philippdormann/splat/internal/retention"
	"github.com/philippdormann/splat/internal/sessions"
	"github.com/philippdormann/splat/internal/sourcemaps"
	"github.com/philippdormann/splat/internal/sso"
	"github.com/philippdormann/splat/internal/storage"
	"github.com/philippdormann/splat/internal/teams"
	"github.com/philippdormann/splat/internal/traces"
	"github.com/philippdormann/splat/internal/tracing"
	"github.com/philippdormann/splat/web"
)

type Server struct {
	Auth          *auth.Store
	Projects      *projects.Store
	Issues        *issues.Store
	Events        *events.Store
	Notifications *notifications.Store
	Dashboard     *dashboard.Store
	Tracing       *tracing.Store
	Retention     *retention.Sweeper
	Storage       *storage.Store
	SourceMaps    *sourcemaps.Store
	APIKeys       *apikeys.Store
	Sessions      *sessions.Store
	Teams         *teams.Store
	Audit         *audit.Store
	Quotas        *quotas.Store
	Invites       *invites.Store
	SSO           *sso.Provider // nil disables single sign-on
	Logs          *logs.Store
	Traces        *traces.Store
	RepoLinks     *repolink.Store
	Metrics       *metrics.Registry // nil disables /metrics
	DB            *database.DB
	Logger        *slog.Logger
	PublicURL     string
	CookieSecure  bool
	Version       string
	StartedAt     time.Time

	mux    *http.ServeMux
	ingest *ingestion.Handler
}

func New(s *Server) (*Server, error) {
	s.mux = http.NewServeMux()
	if s.StartedAt.IsZero() {
		s.StartedAt = time.Now()
	}
	s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	// Logging sits inside security headers so its `next` is the mux
	// itself, letting request metrics resolve route patterns.
	return recoverMiddleware(s.Logger, securityHeadersMiddleware(loggingMiddleware(s.Logger, s.Metrics, s.mux)))
}

func (s *Server) routes() {
	ingest := &ingestion.Handler{
		Projects:      s.Projects,
		Sessions:      s.Sessions,
		APIKeys:       s.APIKeys,
		Issues:        s.Issues,
		Events:        s.Events,
		Notifications: s.Notifications,
		Logs:          s.Logs,
		Traces:        s.Traces,
		Quotas:        s.Quotas,
		Metrics:       s.Metrics,
		Logger:        s.Logger,
		PublicURL:     s.PublicURL,
	}
	s.ingest = ingest
	limiter := newIPRateLimiter(50, 100)     // 50 req/s sustained, burst 100 - generous for a handful of app processes
	loginLimiter := newIPRateLimiter(0.5, 5) // 1 attempt/2s sustained, burst 5 - throttles brute force against the single admin account
	twofaLimiter := newIPRateLimiter(2, 10)  // separate bucket so enrollment flows don't starve login, still brute-force resistant

	// Sentry-compatible ingestion (unchanged wire protocol).
	s.mux.Handle("POST /api/{project}/envelope/", rateLimit(limiter, http.HandlerFunc(ingest.Envelope)))
	s.mux.Handle("POST /api/{project}/store/", rateLimit(limiter, http.HandlerFunc(ingest.Store)))
	s.mux.HandleFunc("GET /healthz", s.healthz)
	s.mux.HandleFunc("GET /readyz", s.readyz)
	s.mux.HandleFunc("GET /robots.txt", s.robots)
	s.mux.HandleFunc("GET /api/v1/meta", s.apiMeta)
	s.mux.HandleFunc("GET /favicon.svg", s.serveFavicon)

	if s.Metrics != nil {
		m := s.Metrics
		m.CounterVec("splat_http_requests_total", "HTTP requests by route and status.", []string{"route", "code"})
		m.Counter("splat_events_ingested_total", "Events stored via ingestion.")
		m.CounterVec("splat_events_rejected_total", "Events rejected before storage, by reason.", []string{"reason"})
		m.Counter("splat_logs_ingested_total", "Log batches stored via ingestion.")
		m.Counter("splat_transactions_ingested_total", "Transactions stored via ingestion.")
		m.CounterVec("splat_envelope_items_skipped_total", "Envelope items accepted but not stored, by item type.", []string{"type"})
		m.GaugeFunc("splat_db_events_total", "Rows in events.", func() float64 {
			return countOr(s.DB, `SELECT COUNT(*) FROM events`)
		})
		m.GaugeFunc("splat_db_issues_unresolved_total", "Unresolved issues.", func() float64 {
			return countOr(s.DB, `SELECT COUNT(*) FROM issues WHERE status = 'unresolved'`)
		})
		m.GaugeFunc("splat_notifications_pending_total", "Queued pending notifications.", func() float64 {
			return countOr(s.DB, `SELECT COUNT(*) FROM notification_queue WHERE status = 'pending'`)
		})
		if s.Storage != nil {
			m.GaugeFunc("splat_db_size_bytes", "Database size in bytes.", func() float64 {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				u, err := s.Storage.Usage(ctx)
				if err != nil {
					return 0
				}
				return float64(u.SizeBytes)
			})
		}
		s.mux.Handle("GET /metrics", m.Handler())
	}

	// JSON API for the Svelte SPA. Session cookie or Bearer API key;
	// read vs write API-key scopes enforced per endpoint.
	s.mux.Handle("POST /api/v1/login", rateLimit(loginLimiter, http.HandlerFunc(s.apiLogin)))
	s.mux.Handle("POST /api/v1/login/2fa", rateLimit(loginLimiter, http.HandlerFunc(s.apiLogin2FA)))
	s.mux.HandleFunc("GET /api/v1/me", s.apiMe)
	s.mux.HandleFunc("POST /api/v1/logout", s.apiLogout)
	s.mux.HandleFunc("GET /api/v1/sso/config", s.apiSSOConfig)
	s.mux.HandleFunc("GET /api/v1/sso/login", s.apiSSOLogin)
	s.mux.HandleFunc("GET /api/v1/sso/callback", s.apiSSOCallback)
	s.mux.HandleFunc("GET /api/v1/settings/sso", s.requireInstance(s.apiSSOSettingsGet))
	s.mux.HandleFunc("PUT /api/v1/settings/sso", s.requireInstance(s.apiSSOSettingsSave))

	// Two-factor self-service: any session (the challenge authorizes
	// enrollment inline during login when enforcement requires it).
	// Setup/disable/code-regeneration touch TOTP secrets and redeem
	// 6-digit codes, so they share the login brute-force limiter.
	s.mux.HandleFunc("GET /api/v1/2fa/status", s.requireAPIAuth(s.api2FAStatus))
	s.mux.Handle("POST /api/v1/2fa/setup/begin", rateLimit(twofaLimiter, http.HandlerFunc(s.api2FASetupBegin)))
	s.mux.Handle("POST /api/v1/2fa/setup/confirm", rateLimit(twofaLimiter, http.HandlerFunc(s.api2FASetupConfirm)))
	s.mux.Handle("POST /api/v1/2fa/disable", rateLimit(twofaLimiter, http.HandlerFunc(s.api2FADisable)))
	s.mux.Handle("POST /api/v1/2fa/recovery-codes/regenerate", rateLimit(twofaLimiter, http.HandlerFunc(s.api2FARegenerateCodes)))

	// Instance security policy (2FA enforcement): same visibility as the
	// other instance settings.
	s.mux.HandleFunc("GET /api/v1/settings/security", s.requireInstance(s.apiSecurityGet))
	s.mux.HandleFunc("PUT /api/v1/settings/security", s.requireInstance(s.apiSecuritySave))
	s.mux.HandleFunc("GET /api/v1/settings/quotas", s.requireInstance(s.apiQuotasDefaultsGet))
	s.mux.HandleFunc("PUT /api/v1/settings/quotas", s.requireInstance(s.apiQuotasDefaultsSave))

	s.mux.HandleFunc("GET /api/v1/dashboard", s.requireScope(s.apiDashboard))

	s.mux.HandleFunc("GET /api/v1/issues", s.requireScope(s.apiIssuesList))
	s.mux.HandleFunc("POST /api/v1/issues/bulk", s.requireScope(s.apiIssuesBulk))
	s.mux.HandleFunc("GET /api/v1/issues/{id}", s.requireScope(s.apiIssueDetail))
	s.mux.HandleFunc("POST /api/v1/issues/{id}/status", s.requireScope(s.apiIssueSetStatus))
	s.mux.HandleFunc("GET /api/v1/issues/{id}/notes", s.requireScope(s.apiIssueNotesList))
	s.mux.HandleFunc("POST /api/v1/issues/{id}/notes", s.requireScope(s.apiIssueNotesCreate))
	s.mux.HandleFunc("DELETE /api/v1/issues/{id}/notes/{noteId}", s.requireScope(s.apiIssueNotesDelete))

	s.mux.HandleFunc("GET /api/v1/projects", s.requireScope(s.apiProjectsList))
	s.mux.HandleFunc("POST /api/v1/projects", s.requireInstance(s.apiProjectCreate))
	s.mux.HandleFunc("GET /api/v1/projects/{id}", s.requireScope(s.apiProjectDetail))
	s.mux.HandleFunc("PATCH /api/v1/projects/{id}", s.requireScope(s.apiProjectUpdate))
	s.mux.HandleFunc("POST /api/v1/projects/{id}/rotate", s.requireInstance(s.apiProjectRotate))
	s.mux.HandleFunc("DELETE /api/v1/projects/{id}", s.requireInstance(s.apiProjectDelete))
	s.mux.HandleFunc("GET /api/v1/platforms", s.requireScope(s.apiPlatforms))
	s.mux.HandleFunc("GET /api/v1/projects/{id}/overview", s.requireScope(s.apiProjectOverview))
	s.mux.HandleFunc("GET /api/v1/projects/{id}/releases", s.requireScope(s.apiProjectReleases))
	s.mux.HandleFunc("GET /api/v1/projects/{id}/releases/{release}", s.requireScope(s.apiProjectReleaseDetail))
	s.mux.HandleFunc("GET /api/v1/projects/{id}/logs", s.requireScope(s.apiProjectLogs))
	s.mux.HandleFunc("GET /api/v1/projects/{id}/traces", s.requireScope(s.apiProjectTraces))
	s.mux.HandleFunc("GET /api/v1/projects/{id}/traces/{traceId}", s.requireScope(s.apiProjectTraceDetail))
	s.mux.HandleFunc("GET /api/v1/projects/{id}/sessions/summary", s.requireScope(s.apiProjectSessionSummary))
	s.mux.HandleFunc("POST /api/v1/projects/{id}/test-event", s.requireScope(s.apiProjectTestEvent))

	s.mux.HandleFunc("GET /api/v1/settings", s.requireInstance(s.apiSettings))
	s.mux.HandleFunc("PUT /api/v1/settings/tracing", s.requireInstance(s.apiSettingsSaveTracing))

	s.mux.HandleFunc("GET /api/v1/integrations", s.requireInstance(s.apiIntegrationsList))
	s.mux.HandleFunc("POST /api/v1/integrations", s.requireInstance(s.apiIntegrationsCreate))
	s.mux.HandleFunc("PUT /api/v1/integrations/{id}", s.requireInstance(s.apiIntegrationsUpdate))
	s.mux.HandleFunc("DELETE /api/v1/integrations/{id}", s.requireInstance(s.apiIntegrationsDelete))
	s.mux.HandleFunc("POST /api/v1/integrations/{id}/test", s.requireInstance(s.apiIntegrationsTest))

	s.mux.HandleFunc("GET /api/v1/rules", s.requireInstance(s.apiRulesList))
	s.mux.HandleFunc("POST /api/v1/rules", s.requireInstance(s.apiRulesCreate))
	s.mux.HandleFunc("PUT /api/v1/rules/{id}", s.requireInstance(s.apiRulesUpdate))
	s.mux.HandleFunc("DELETE /api/v1/rules/{id}", s.requireInstance(s.apiRulesDelete))

	s.mux.HandleFunc("GET /api/v1/storage", s.requireInstance(s.apiStorageUsage))
	s.mux.HandleFunc("POST /api/v1/storage/sweep", s.requireInstance(s.apiStorageSweep))
	s.mux.HandleFunc("POST /api/v1/storage/vacuum", s.requireInstance(s.apiStorageVacuum))

	s.mux.HandleFunc("GET /api/v1/projects/{id}/sourcemaps", s.requireScope(s.apiSourceMapsList))
	s.mux.HandleFunc("POST /api/v1/projects/{id}/sourcemaps", s.requireScope(s.apiSourceMapsUpload))
	s.mux.HandleFunc("DELETE /api/v1/projects/{id}/sourcemaps/{mapId}", s.requireScope(s.apiSourceMapsDelete))

	// API key management is admin-session-only: keys must never mint,
	// rotate or revoke keys (privilege escalation by construction).
	s.mux.HandleFunc("GET /api/v1/api-keys", s.requireAdmin(s.apiKeysList))
	s.mux.HandleFunc("POST /api/v1/api-keys", s.requireAdmin(s.apiKeysCreate))
	s.mux.HandleFunc("POST /api/v1/api-keys/{id}/rotate", s.requireAdmin(s.apiKeysRotate))
	s.mux.HandleFunc("POST /api/v1/api-keys/{id}/revoke", s.requireAdmin(s.apiKeysRevoke))
	s.mux.HandleFunc("DELETE /api/v1/api-keys/{id}", s.requireAdmin(s.apiKeysDelete))

	// Users, teams, and project grants are admin-session-only as well:
	// whoever can edit membership can read everything.
	s.mux.HandleFunc("GET /api/v1/users", s.requireAdmin(s.apiUsersList))
	s.mux.HandleFunc("POST /api/v1/users", s.requireAdmin(s.apiUsersCreate))
	s.mux.HandleFunc("DELETE /api/v1/users/{id}", s.requireAdmin(s.apiUsersDelete))
	s.mux.HandleFunc("PUT /api/v1/users/{id}/role", s.requireAdmin(s.apiUsersSetRole))
	s.mux.HandleFunc("PUT /api/v1/users/{id}/password", s.requireAdmin(s.apiUsersSetPassword))
	s.mux.HandleFunc("POST /api/v1/users/{id}/2fa/reset", s.requireAdmin(s.apiUsersReset2FA))
	s.mux.HandleFunc("GET /api/v1/audit-log", s.requireAdmin(s.apiAuditList))
	s.mux.HandleFunc("GET /api/v1/teams", s.requireAdmin(s.apiTeamsList))
	s.mux.HandleFunc("POST /api/v1/teams", s.requireAdmin(s.apiTeamsCreate))
	s.mux.HandleFunc("DELETE /api/v1/teams/{id}", s.requireAdmin(s.apiTeamsDelete))
	s.mux.HandleFunc("POST /api/v1/teams/{id}/members", s.requireAdmin(s.apiTeamAddMember))
	s.mux.HandleFunc("DELETE /api/v1/teams/{id}/members/{userId}", s.requireAdmin(s.apiTeamRemoveMember))
	s.mux.HandleFunc("POST /api/v1/teams/{id}/projects", s.requireAdmin(s.apiTeamGrantProject))
	s.mux.HandleFunc("DELETE /api/v1/teams/{id}/projects/{projectId}", s.requireAdmin(s.apiTeamRevokeProject))
	s.mux.HandleFunc("GET /api/v1/projects/{id}/access", s.requireAdmin(s.apiProjectAccess))
	s.mux.HandleFunc("POST /api/v1/projects/{id}/access/members", s.requireAdmin(s.apiProjectAddMember))
	s.mux.HandleFunc("GET /api/v1/projects/{id}/quota", s.requireScope(s.apiProjectQuotaGet))
	s.mux.HandleFunc("PUT /api/v1/projects/{id}/quota", s.requireAdmin(s.apiProjectQuotaSave))
	s.mux.HandleFunc("DELETE /api/v1/projects/{id}/access/members/{userId}", s.requireAdmin(s.apiProjectRemoveMember))
	s.mux.HandleFunc("GET /api/v1/invitations", s.requireAdmin(s.apiInvitationsList))
	s.mux.HandleFunc("POST /api/v1/invitations", s.requireAdmin(s.apiInvitationsCreate))
	s.mux.HandleFunc("DELETE /api/v1/invitations/{token}", s.requireAdmin(s.apiInvitationRevoke))
	s.mux.HandleFunc("GET /api/v1/invite/{token}", s.apiInvitationGet)
	s.mux.HandleFunc("POST /api/v1/invite/accept", s.apiInvitationAccept)

	// Repository integration: config read needs project access, config
	// write/test are admin-only (a forge token posts as its owner).
	// Creating/unlinking external issues needs project access.
	s.mux.HandleFunc("GET /api/v1/projects/{id}/repo", s.requireScope(s.apiProjectRepoGet))
	s.mux.HandleFunc("PUT /api/v1/projects/{id}/repo", s.requireAdmin(s.apiProjectRepoSave))
	s.mux.HandleFunc("DELETE /api/v1/projects/{id}/repo", s.requireAdmin(s.apiProjectRepoDelete))
	s.mux.HandleFunc("POST /api/v1/projects/{id}/repo/test", s.requireAdmin(s.apiProjectRepoTest))
	s.mux.HandleFunc("GET /api/v1/issues/{id}/links", s.requireScope(s.apiIssueLinksList))
	s.mux.HandleFunc("POST /api/v1/issues/{id}/links", s.requireScope(s.apiIssueLinksCreate))
	s.mux.HandleFunc("DELETE /api/v1/issues/{id}/links/{linkId}", s.requireScope(s.apiIssueLinksDelete))

	s.mux.HandleFunc("GET /api/v1/openapi.json", s.apiOpenAPI)

	// Built Svelte assets (fingerprinted files under /assets/).
	dist := web.Sub()
	assets, _ := fs.Sub(dist, "assets")
	if assets != nil {
		s.mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets)))
	}

	// SPA entry points: every UI route serves the built index.html and lets
	// the Svelte router take over client-side.
	for _, route := range []string{
		"GET /{$}", "GET /login",
		"GET /issues", "GET /issues/{id}",
		"GET /projects", "GET /projects/new", "GET /projects/{id}",
		"GET /projects/{id}/releases", "GET /projects/{id}/releases/{release}", "GET /projects/{id}/traces/{traceId}",
		"GET /settings", "GET /security", "GET /invite/{token}",
	} {
		s.mux.HandleFunc(route, s.serveSPA)
	}
}

// serveSPA serves the embedded Svelte build's index.html. If the frontend
// has not been built (fresh checkout without `bun run build`), it serves a
// plain-text hint instead of a 500 so the failure mode is obvious.
func (s *Server) serveSPA(w http.ResponseWriter, r *http.Request) {
	index, err := fs.ReadFile(web.Sub(), "index.html")
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("UI not built: run `bun install && bun run build` in frontend/ and rebuild.\n"))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(index)
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	// Liveness only: the process is up. Use /readyz to check the database.
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// countOr runs a COUNT(*) query for a gauge, returning 0 on any failure so
// a scrape never fails because one gauge query did.
func countOr(db *database.DB, query string) float64 {
	if db == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var n int64
	if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
		return 0
	}
	return float64(n)
}

func (s *Server) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

func (s *Server) serveFavicon(w http.ResponseWriter, r *http.Request) {
	icon, err := fs.ReadFile(web.Sub(), "favicon.svg")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(icon)
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	// Readiness: the process is up AND can serve traffic (database reachable).
	dbStatus := "ok"
	code := http.StatusOK
	if s.DB == nil {
		dbStatus = "not configured"
		code = http.StatusServiceUnavailable
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := s.DB.PingContext(ctx); err != nil {
			s.Logger.Error("readiness db ping failed", "error", err)
			dbStatus = "unreachable"
			code = http.StatusServiceUnavailable
		}
	}
	status := "ok"
	if code != http.StatusOK {
		status = "degraded"
	}
	writeJSON(w, code, map[string]any{
		"status":        status,
		"db":            dbStatus,
		"version":       s.Version,
		"uptimeSeconds": int64(time.Since(s.StartedAt).Seconds()),
	})
}

func (s *Server) apiMeta(w http.ResponseWriter, r *http.Request) {
	// Public build metadata: no secrets, useful for dashboards and for
	// confirming which build a running instance actually is.
	driver := ""
	if s.DB != nil {
		driver = string(s.DB.Driver())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":       s.Version,
		"goVersion":     runtime.Version(),
		"dbDriver":      driver,
		"startedAt":     s.StartedAt.UTC().Format(time.RFC3339),
		"uptimeSeconds": int64(time.Since(s.StartedAt).Seconds()),
	})
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return pluralize(int(d.Minutes()), "minute") + " ago"
	case d < 24*time.Hour:
		return pluralize(int(d.Hours()), "hour") + " ago"
	default:
		return pluralize(int(d.Hours()/24), "day") + " ago"
	}
}

func pluralize(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}
