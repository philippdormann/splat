package httpserver

import (
	"net/http/httptest"
	"strings"
	"testing"

	"errgotrack/internal/issues"
)

// Event payloads are untrusted: an exception type, message, or tag value can
// contain anything the instrumented app's user input produced. The Svelte
// frontend renders all API values as text (never as raw HTML), and the API
// itself serializes with encoding/json, which HTML-escapes '<', '>' and '&'
// by default - so a "</script>" inside exception data cannot break out of
// the JSON document the way it could break out of a server-rendered
// <script> block.
func TestWriteJSONEscapesScriptClose(t *testing.T) {
	payload := `</script><script>alert(1)</script>`
	rec := httptest.NewRecorder()
	writeJSON(rec, 200, map[string]any{"label": payload})
	body := rec.Body.String()
	if strings.Contains(body, "</script>") {
		t.Fatalf("JSON response contains a raw script-closing sequence: %s", body)
	}
	if !strings.Contains(body, "<script>") == false {
		t.Fatalf("expected escaped payload, got: %s", body)
	}
}

func TestIssueJSONKeepsUntrustedDataIntactForTextRendering(t *testing.T) {
	payload := `<script>alert('xss')</script>`
	iss := &issues.Issue{ID: 1, Title: payload, ExceptionType: payload, ExceptionValue: payload, Status: issues.StatusUnresolved, Level: "error"}
	rec := httptest.NewRecorder()
	writeJSON(rec, 200, issueJSON(iss))
	body := rec.Body.String()
	if strings.Contains(body, "<script>alert") {
		t.Fatalf("raw script tag leaked into JSON response:\n%s", body)
	}
	// The frontend receives the escaped form and renders it as text.
	if !strings.Contains(body, "script") {
		t.Fatalf("expected payload to survive (escaped) in JSON, got:\n%s", body)
	}
}

func TestAPIErrorIsJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeAPIError(rec, 401, "unauthorized")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected JSON content type, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "unauthorized") {
		t.Fatalf("expected error body, got %q", rec.Body.String())
	}
}
