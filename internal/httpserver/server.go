// Package httpserver wires HTTP routes and middleware for the JSON API and
// the embedded Svelte SPA. Event ingestion itself lives in
// internal/ingestion; this package mounts it alongside the API.
package httpserver

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"errgotrack/internal/apikeys"
	"errgotrack/internal/auth"
	"errgotrack/internal/dashboard"
	"errgotrack/internal/events"
	"errgotrack/internal/ingestion"
	"errgotrack/internal/issues"
	"errgotrack/internal/notifications"
	"errgotrack/internal/projects"
	"errgotrack/internal/retention"
	"errgotrack/internal/sourcemaps"
	"errgotrack/internal/storage"
	"errgotrack/internal/tracing"
	"errgotrack/web"
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
	Logger        *slog.Logger
	PublicURL     string
	CookieSecure  bool

	mux *http.ServeMux
}

func New(s *Server) (*Server, error) {
	s.mux = http.NewServeMux()
	s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return recoverMiddleware(s.Logger, loggingMiddleware(s.Logger, securityHeadersMiddleware(s.mux)))
}

func (s *Server) routes() {
	ingest := &ingestion.Handler{
		Projects:      s.Projects,
		APIKeys:       s.APIKeys,
		Issues:        s.Issues,
		Events:        s.Events,
		Notifications: s.Notifications,
		Logger:        s.Logger,
		PublicURL:     s.PublicURL,
	}
	limiter := newIPRateLimiter(50, 100)     // 50 req/s sustained, burst 100 - generous for a handful of app processes
	loginLimiter := newIPRateLimiter(0.5, 5) // 1 attempt/2s sustained, burst 5 - throttles brute force against the single admin account

	// Sentry-compatible ingestion (unchanged wire protocol).
	s.mux.Handle("POST /api/{project}/envelope/", rateLimit(limiter, http.HandlerFunc(ingest.Envelope)))
	s.mux.Handle("POST /api/{project}/store/", rateLimit(limiter, http.HandlerFunc(ingest.Store)))
	s.mux.HandleFunc("GET /healthz", s.healthz)

	// JSON API for the Svelte SPA. Session cookie or Bearer API key;
	// read vs write API-key scopes enforced per endpoint.
	s.mux.Handle("POST /api/v1/login", rateLimit(loginLimiter, http.HandlerFunc(s.apiLogin)))
	s.mux.HandleFunc("GET /api/v1/me", s.apiMe)
	s.mux.HandleFunc("POST /api/v1/logout", s.apiLogout)

	s.mux.HandleFunc("GET /api/v1/dashboard", s.requireScope(s.apiDashboard))

	s.mux.HandleFunc("GET /api/v1/issues", s.requireScope(s.apiIssuesList))
	s.mux.HandleFunc("POST /api/v1/issues/bulk", s.requireScope(s.apiIssuesBulk))
	s.mux.HandleFunc("GET /api/v1/issues/{id}", s.requireScope(s.apiIssueDetail))
	s.mux.HandleFunc("POST /api/v1/issues/{id}/status", s.requireScope(s.apiIssueSetStatus))

	s.mux.HandleFunc("GET /api/v1/projects", s.requireScope(s.apiProjectsList))
	s.mux.HandleFunc("POST /api/v1/projects", s.requireInstance(s.apiProjectCreate))
	s.mux.HandleFunc("GET /api/v1/projects/{id}", s.requireScope(s.apiProjectDetail))
	s.mux.HandleFunc("PATCH /api/v1/projects/{id}", s.requireScope(s.apiProjectUpdate))
	s.mux.HandleFunc("POST /api/v1/projects/{id}/rotate", s.requireInstance(s.apiProjectRotate))
	s.mux.HandleFunc("DELETE /api/v1/projects/{id}", s.requireInstance(s.apiProjectDelete))
	s.mux.HandleFunc("GET /api/v1/platforms", s.requireScope(s.apiPlatforms))

	s.mux.HandleFunc("GET /api/v1/settings", s.requireInstance(s.apiSettings))
	s.mux.HandleFunc("PUT /api/v1/settings/telegram", s.requireInstance(s.apiSettingsSaveTelegram))
	s.mux.HandleFunc("POST /api/v1/settings/telegram/test", s.requireInstance(s.apiSettingsTestTelegram))
	s.mux.HandleFunc("PUT /api/v1/settings/webhooks/{channel}", s.requireInstance(s.apiSettingsSaveWebhook))
	s.mux.HandleFunc("POST /api/v1/settings/webhooks/{channel}/test", s.requireInstance(s.apiSettingsTestWebhook))
	s.mux.HandleFunc("GET /api/v1/settings/generic-webhooks", s.requireInstance(s.apiGenericWebhooksList))
	s.mux.HandleFunc("POST /api/v1/settings/generic-webhooks", s.requireInstance(s.apiGenericWebhooksCreate))
	s.mux.HandleFunc("PUT /api/v1/settings/generic-webhooks/{id}", s.requireInstance(s.apiGenericWebhooksUpdate))
	s.mux.HandleFunc("DELETE /api/v1/settings/generic-webhooks/{id}", s.requireInstance(s.apiGenericWebhooksDelete))
	s.mux.HandleFunc("POST /api/v1/settings/generic-webhooks/{id}/test", s.requireInstance(s.apiGenericWebhooksTest))
	s.mux.HandleFunc("PUT /api/v1/settings/tracing", s.requireInstance(s.apiSettingsSaveTracing))
	s.mux.HandleFunc("PUT /api/v1/settings/smtp", s.requireInstance(s.apiSettingsSaveSMTP))
	s.mux.HandleFunc("POST /api/v1/settings/smtp/test", s.requireInstance(s.apiSettingsTestSMTP))

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
		"GET /settings",
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
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
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
