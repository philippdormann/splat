package httpserver

import "testing"

// Regression test for a real gap found by comparing against a GlitchTip
// issue page: Sentry's "contexts" object (browser/os/device/runtime/trace/
// custom contexts) and a breadcrumb's "data" field were both being parsed
// off the wire but never reaching the template.
func TestParseContextsFlattensAndSortsFields(t *testing.T) {
	raw := `{
		"browser": {"type": "browser", "name": "Chrome", "version": "151.0.0"},
		"trace": {"trace_id": "abc123", "span_id": "def456"}
	}`
	contexts := parseContexts(raw)
	if len(contexts) != 2 {
		t.Fatalf("expected 2 contexts, got %d: %+v", len(contexts), contexts)
	}
	if contexts[0].Name != "browser" || contexts[1].Name != "trace" {
		t.Fatalf("expected contexts sorted by name, got %+v", contexts)
	}
	for _, f := range contexts[0].Fields {
		if f.Key == "type" {
			t.Fatal("the context's own 'type' key should be dropped as redundant with its name")
		}
	}
	if len(contexts[0].Fields) != 2 {
		t.Fatalf("expected name+version, got %+v", contexts[0].Fields)
	}
}

func TestParseContextsHandlesEmptyOrInvalid(t *testing.T) {
	if got := parseContexts(""); got != nil {
		t.Fatalf("empty input should yield nil, got %+v", got)
	}
	if got := parseContexts("not json"); got != nil {
		t.Fatalf("invalid json should yield nil, got %+v", got)
	}
}

func TestTraceIDFromContexts(t *testing.T) {
	contexts := parseContexts(`{"trace": {"trace_id": "c2154553a5c6479cb16f6194750f7ad3", "span_id": "a10d5228"}}`)
	if got := traceIDFromContexts(contexts); got != "c2154553a5c6479cb16f6194750f7ad3" {
		t.Fatalf("got %q", got)
	}
	if got := traceIDFromContexts(parseContexts(`{"browser": {"name": "Chrome"}}`)); got != "" {
		t.Fatalf("expected empty trace id when no trace context present, got %q", got)
	}
}

func TestFlattenToStringsForBreadcrumbData(t *testing.T) {
	got := flattenToStrings([]byte(`{"to": "/a", "from": "/b", "status_code": 200}`))
	if got["to"] != "/a" || got["from"] != "/b" || got["status_code"] != "200" {
		t.Fatalf("flatten mismatch: %+v", got)
	}
}
