package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"errgotrack/internal/database"
)

func testHealthServer(t *testing.T) *Server {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := New(&Server{
		DB:       db,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, nil)),
		Version:  "test-1.0",
		Projects: nil,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return s
}

func TestHealthzIsJSONOk(t *testing.T) {
	s := testHealthServer(t)
	rec := httptest.NewRecorder()
	s.healthz(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("healthz is not JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

func TestReadyzChecksDB(t *testing.T) {
	s := testHealthServer(t)
	rec := httptest.NewRecorder()
	s.readyz(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("readyz is not JSON: %v", err)
	}
	if body["status"] != "ok" || body["db"] != "ok" || body["version"] != "test-1.0" {
		t.Fatalf("body = %v", body)
	}

	// Closed DB must flip readiness to 503 without taking down liveness.
	s.DB.Close()
	rec = httptest.NewRecorder()
	s.readyz(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != 503 {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.healthz(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Fatalf("liveness must stay 200 when DB is down, got %d", rec.Code)
	}
}

func TestMetaExposesBuildInfo(t *testing.T) {
	s := testHealthServer(t)
	s.StartedAt = time.Now().Add(-time.Minute)
	rec := httptest.NewRecorder()
	s.apiMeta(rec, httptest.NewRequest("GET", "/api/v1/meta", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("meta is not JSON: %v", err)
	}
	if body["version"] != "test-1.0" || body["dbDriver"] != "sqlite" {
		t.Fatalf("body = %v", body)
	}
	if up, _ := body["uptimeSeconds"].(float64); up < 59 {
		t.Fatalf("uptimeSeconds = %v, want >= 59", up)
	}
}

func TestRobotsDisallowsAll(t *testing.T) {
	s := testHealthServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/robots.txt", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Disallow: /") {
		t.Fatalf("robots.txt = %q", body)
	}
}

func TestSecurityHeadersIncludeNoindex(t *testing.T) {
	s := testHealthServer(t)
	for _, path := range []string{"/healthz", "/readyz", "/robots.txt", "/api/v1/openapi.json"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if got := rec.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
			t.Fatalf("%s: X-Robots-Tag = %q", path, got)
		}
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Fatalf("%s: X-Frame-Options = %q", path, got)
		}
	}
}
