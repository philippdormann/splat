package httpserver

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// The OpenAPI doc is hand-maintained; this pins the paths the router
// actually serves so the two can't drift apart silently.
func TestOpenAPICoversRoutes(t *testing.T) {
	var doc struct {
		OpenAPI string                    `json:"openapi"`
		Paths   map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(openAPIDoc, &doc); err != nil {
		t.Fatalf("openapi.json does not parse: %v", err)
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.") {
		t.Fatalf("openapi version = %q", doc.OpenAPI)
	}
	for _, tc := range []struct{ method, path string }{
		{"post", "/api/v1/login"},
		{"post", "/api/v1/login/2fa"},
		{"get", "/api/v1/2fa/status"},
		{"post", "/api/v1/2fa/setup/begin"},
		{"post", "/api/v1/2fa/setup/confirm"},
		{"post", "/api/v1/2fa/disable"},
		{"post", "/api/v1/2fa/recovery-codes/regenerate"},
		{"post", "/api/v1/users/{id}/2fa/reset"},
		{"get", "/api/v1/settings/security"},
		{"put", "/api/v1/settings/security"},
		{"get", "/api/v1/settings/quotas"},
		{"put", "/api/v1/settings/quotas"},
		{"get", "/api/v1/projects/{id}/quota"},
		{"put", "/api/v1/projects/{id}/quota"},
		{"get", "/api/v1/sso/config"},
		{"get", "/api/v1/sso/login"},
		{"get", "/api/v1/sso/callback"},
		{"get", "/api/v1/settings/sso"},
		{"put", "/api/v1/settings/sso"},
		{"get", "/api/v1/audit-log"},
		{"get", "/api/v1/me"},
		{"get", "/api/v1/dashboard"},
		{"get", "/api/v1/issues"},
		{"post", "/api/v1/issues/bulk"},
		{"get", "/api/v1/issues/{id}"},
		{"post", "/api/v1/issues/{id}/status"},
		{"get", "/api/v1/issues/{id}/notes"},
		{"post", "/api/v1/issues/{id}/notes"},
		{"delete", "/api/v1/issues/{id}/notes/{noteId}"},
		{"get", "/api/v1/projects"},
		{"post", "/api/v1/projects"},
		{"get", "/api/v1/projects/{id}"},
		{"patch", "/api/v1/projects/{id}"},
		{"delete", "/api/v1/projects/{id}"},
		{"post", "/api/v1/projects/{id}/rotate"},
		{"get", "/api/v1/platforms"},
		{"get", "/api/v1/projects/{id}/overview"},
		{"get", "/api/v1/projects/{id}/releases"},
		{"get", "/api/v1/projects/{id}/releases/{release}"},
		{"get", "/api/v1/projects/{id}/logs"},
		{"get", "/api/v1/projects/{id}/traces"},
		{"get", "/api/v1/projects/{id}/traces/{traceId}"},
		{"get", "/api/v1/projects/{id}/sessions/summary"},
		{"post", "/api/v1/projects/{id}/test-event"},
		{"get", "/api/v1/projects/{id}/sourcemaps"},
		{"post", "/api/v1/projects/{id}/sourcemaps"},
		{"delete", "/api/v1/projects/{id}/sourcemaps/{mapId}"},
		{"get", "/api/v1/settings"},
		{"put", "/api/v1/settings/tracing"},
		{"get", "/api/v1/integrations"},
		{"post", "/api/v1/integrations"},
		{"put", "/api/v1/integrations/{id}"},
		{"delete", "/api/v1/integrations/{id}"},
		{"post", "/api/v1/integrations/{id}/test"},
		{"get", "/api/v1/rules"},
		{"post", "/api/v1/rules"},
		{"put", "/api/v1/rules/{id}"},
		{"delete", "/api/v1/rules/{id}"},
		{"get", "/api/v1/storage"},
		{"post", "/api/v1/storage/sweep"},
		{"post", "/api/v1/storage/vacuum"},
		{"get", "/api/v1/api-keys"},
		{"post", "/api/v1/api-keys"},
		{"post", "/api/v1/api-keys/{id}/rotate"},
		{"post", "/api/v1/api-keys/{id}/revoke"},
		{"delete", "/api/v1/api-keys/{id}"},
		{"get", "/api/v1/users"},
		{"post", "/api/v1/users"},
		{"delete", "/api/v1/users/{id}"},
		{"put", "/api/v1/users/{id}/role"},
		{"put", "/api/v1/users/{id}/password"},
		{"get", "/api/v1/teams"},
		{"post", "/api/v1/teams"},
		{"delete", "/api/v1/teams/{id}"},
		{"post", "/api/v1/teams/{id}/members"},
		{"delete", "/api/v1/teams/{id}/members/{userId}"},
		{"post", "/api/v1/teams/{id}/projects"},
		{"delete", "/api/v1/teams/{id}/projects/{projectId}"},
		{"get", "/api/v1/projects/{id}/access"},
		{"post", "/api/v1/projects/{id}/access/members"},
		{"delete", "/api/v1/projects/{id}/access/members/{userId}"},
		{"get", "/api/v1/projects/{id}/repo"},
		{"put", "/api/v1/projects/{id}/repo"},
		{"delete", "/api/v1/projects/{id}/repo"},
		{"post", "/api/v1/projects/{id}/repo/test"},
		{"get", "/api/v1/issues/{id}/links"},
		{"post", "/api/v1/issues/{id}/links"},
		{"delete", "/api/v1/issues/{id}/links/{linkId}"},
		{"get", "/api/v1/openapi.json"},
		{"get", "/healthz"},
		{"get", "/robots.txt"},
		{"get", "/favicon.png"},
		{"get", "/favicon.svg"},
		{"get", "/readyz"},
		{"get", "/api/v1/meta"},
		{"get", "/metrics"},
	} {
		ops, ok := doc.Paths[tc.path]
		if !ok {
			t.Errorf("openapi.json missing path %s", tc.path)
			continue
		}
		if _, ok := ops[tc.method]; !ok {
			t.Errorf("openapi.json path %s missing method %s", tc.path, tc.method)
		}
	}
}

func TestOpenAPIEndpointServesDoc(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/openapi.json", nil)
	s.apiOpenAPI(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("response does not parse: %v", err)
	}
}
