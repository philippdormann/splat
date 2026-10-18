// Package httpserver wires HTTP routes, middleware, and template rendering
// for the dashboard and admin UI. Event ingestion itself lives in
// internal/ingestion; this package mounts it alongside the dashboard.
package httpserver

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"errgotrack/internal/auth"
	"errgotrack/internal/dashboard"
	"errgotrack/internal/events"
	"errgotrack/internal/ingestion"
	"errgotrack/internal/issues"
	"errgotrack/internal/notifications"
	"errgotrack/internal/projects"
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
	Logger        *slog.Logger
	PublicURL     string
	CookieSecure  bool

	render *renderer
	mux    *http.ServeMux
}

func New(s *Server) (*Server, error) {
	r, err := newRenderer(s.Logger)
	if err != nil {
		return nil, err
	}
	s.render = r
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
		Issues:        s.Issues,
		Events:        s.Events,
		Notifications: s.Notifications,
		Logger:        s.Logger,
		PublicURL:     s.PublicURL,
	}
	limiter := newIPRateLimiter(50, 100)     // 50 req/s sustained, burst 100 - generous for a handful of Django processes
	loginLimiter := newIPRateLimiter(0.5, 5) // 1 attempt/2s sustained, burst 5 - throttles brute force against the single admin account

	s.mux.Handle("POST /api/{project}/envelope/", rateLimit(limiter, http.HandlerFunc(ingest.Envelope)))
	s.mux.Handle("POST /api/{project}/store/", rateLimit(limiter, http.HandlerFunc(ingest.Store)))
	s.mux.HandleFunc("GET /healthz", s.healthz)

	staticFS, _ := fs.Sub(web.Static, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	s.mux.HandleFunc("GET /login", s.loginPage)
	s.mux.Handle("POST /login", rateLimit(loginLimiter, http.HandlerFunc(s.loginSubmit)))
	s.mux.HandleFunc("POST /logout", s.requireAuth(s.logout))

	s.mux.HandleFunc("GET /{$}", s.requireAuth(s.dashboard))
	s.mux.HandleFunc("GET /issues", s.requireAuth(s.issuesList))
	s.mux.HandleFunc("POST /issues/bulk", s.requireAuth(s.issuesBulk))
	s.mux.HandleFunc("GET /issues/{id}", s.requireAuth(s.issueDetail))
	s.mux.HandleFunc("POST /issues/{id}/status", s.requireAuth(s.issueSetStatus))

	s.mux.HandleFunc("GET /projects", s.requireAuth(s.projectsList))
	s.mux.HandleFunc("GET /projects/new", s.requireAuth(s.projectNewPage))
	s.mux.HandleFunc("POST /projects/new", s.requireAuth(s.projectCreate))
	s.mux.HandleFunc("GET /projects/{id}", s.requireAuth(s.projectDetail))
	s.mux.HandleFunc("POST /projects/{id}/settings", s.requireAuth(s.projectUpdateSettings))
	s.mux.HandleFunc("POST /projects/{id}/rotate", s.requireAuth(s.projectRotate))
	s.mux.HandleFunc("POST /projects/{id}/delete", s.requireAuth(s.projectDelete))

	s.mux.HandleFunc("GET /settings", s.requireAuth(s.settingsPage))
	s.mux.HandleFunc("POST /settings/telegram", s.requireAuth(s.settingsSaveTelegram))
	s.mux.HandleFunc("POST /settings/telegram/test", s.requireAuth(s.settingsTestTelegram))
	s.mux.HandleFunc("POST /settings/webhook/{channel}", s.requireAuth(s.settingsSaveWebhook))
	s.mux.HandleFunc("POST /settings/webhook/{channel}/test", s.requireAuth(s.settingsTestWebhook))
	s.mux.HandleFunc("POST /settings/tracing", s.requireAuth(s.settingsSaveTracing))
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// PageBase carries the fields every authenticated page template needs:
// which nav item is active, and the CSRF token to echo back in forms.
type PageBase struct {
	Authenticated bool
	Nav           string
	CSRFToken     string
}

func (s *Server) pageBase(w http.ResponseWriter, r *http.Request, nav string) PageBase {
	return PageBase{
		Authenticated: true,
		Nav:           nav,
		CSRFToken:     auth.EnsureCSRFCookie(w, r, s.CookieSecure),
	}
}

type userKey struct{}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := auth.SessionTokenFromRequest(r)
		userID, ok := s.Auth.ValidateSession(token)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), userKey{}, userID)
		next(w, r.WithContext(ctx))
	}
}

func checkCSRF(w http.ResponseWriter, r *http.Request) bool {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return false
	}
	if !auth.ValidCSRF(r, r.FormValue("csrf_token")) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return false
	}
	return true
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
