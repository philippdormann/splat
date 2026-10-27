package httpserver

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/philippdormann/splat/internal/metrics"
)

// recoverMiddleware turns a panicking handler into a generic 500 instead of
// taking down the process - event ingestion processes untrusted payloads,
// so a parsing bug in one request must not affect any other.
func recoverMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("panic recovered", "error", rec, "path", r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// securityHeadersMiddleware sets headers that cost nothing and only ever
// restrict behavior. The Content-Security-Policy below is possible because
// the dashboard is a Vite SPA with no inline scripts/styles and no
// third-party fetches - everything comes from 'self'. If the frontend ever
// gains a CDN dependency, this policy must grow with it (the build will
// keep working, browsers will just refuse to load the new origin).
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self'; "+
				"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; "+
				"frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		// An error tracker is authenticated internal tooling, never public
		// content: keep it out of search indexes even if accidentally
		// exposed. Belt and suspenders with /robots.txt below.
		h.Set("X-Robots-Tag", "noindex, nofollow")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(logger *slog.Logger, m *metrics.Registry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start))
		if m != nil {
			// r.Pattern is only populated for handlers inside the mux;
			// this middleware wraps it from the outside, so resolve the
			// pattern explicitly ("" when nothing matched). Patterns
			// already include the method ("GET /metrics").
			route := "unknown"
			if mux, ok := next.(*http.ServeMux); ok {
				if _, pattern := mux.Handler(r); pattern != "" {
					route = pattern
				}
			}
			m.Inc("errgotrack_http_requests_total", route, strconv.Itoa(rec.status))
		}
	})
}
