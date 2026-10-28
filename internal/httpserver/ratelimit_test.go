package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIPRateLimiterPerIPBuckets(t *testing.T) {
	l := newIPRateLimiter(0.5, 2) // burst 2, slow refill - plenty for this test's timing
	if !l.allow("1.1.1.1") || !l.allow("1.1.1.1") {
		t.Fatal("expected first two requests within burst to be allowed")
	}
	if l.allow("1.1.1.1") {
		t.Fatal("expected third request to exceed burst and be denied")
	}
	// A different IP has its own bucket, unaffected by 1.1.1.1 exhausting its.
	if !l.allow("2.2.2.2") {
		t.Fatal("expected a different IP to have an independent, unexhausted bucket")
	}
}

func TestRateLimitMiddlewareReturns429WhenExhausted(t *testing.T) {
	l := newIPRateLimiter(0.5, 1)
	handler := rateLimit(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "9.9.9.9:12345"

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec1.Code)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request past burst=1: expected 429, got %d", rec2.Code)
	}
}

func TestSecurityHeadersMiddlewareSetsHeaders(t *testing.T) {
	handler := securityHeadersMiddleware(false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	for header, want := range map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "same-origin",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestClientIPStripsPort(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:54321"
	if got := clientIP(req); got != "203.0.113.5" {
		t.Fatalf("got %q", got)
	}
}
