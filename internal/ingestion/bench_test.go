package ingestion

import (
	"context"
	"fmt"
	"testing"

	"errgotrack/internal/sentryprotocol"
)

// realisticEvent mirrors what sentry-sdk actually sends for a Django
// exception: a handful of in-app + framework frames, tags, breadcrumbs, user
// and request context - not an empty payload, per the benchmarking
// requirement that performance be measured against realistic events.
func realisticEvent(eventID string) *sentryprotocol.Event {
	payload := []byte(fmt.Sprintf(`{
		"event_id": %q, "timestamp": 1700000000.123, "level": "error", "environment": "production",
		"release": "demo-app@1.4.2", "transaction": "myapp.views.checkout",
		"exception": {"values": [{"type": "ValueError", "value": "Invalid quantity: -1", "module": "myapp.views",
			"stacktrace": {"frames": [
				{"filename": "django/core/handlers/exception.py", "function": "response_for_exception", "module": "django.core.handlers.exception", "lineno": 55, "in_app": false},
				{"filename": "django/core/handlers/base.py", "function": "_get_response", "module": "django.core.handlers.base", "lineno": 197, "in_app": false},
				{"filename": "myapp/middleware.py", "function": "__call__", "module": "myapp.middleware", "lineno": 12, "in_app": true},
				{"filename": "myapp/views.py", "function": "checkout", "module": "myapp.views", "lineno": 42, "in_app": true}
			]}}]},
		"breadcrumbs": {"values": [
			{"timestamp": 1700000000.0, "type": "default", "category": "http", "level": "info", "message": "GET /cart"},
			{"timestamp": 1700000000.1, "type": "default", "category": "http", "level": "info", "message": "POST /checkout"}
		]},
		"user": {"id": "42", "email": "user@example.com", "ip_address": "203.0.113.4"},
		"tags": {"server_name": "web-1", "browser": "Chrome 128"},
		"request": {"url": "https://example.com/checkout", "method": "POST", "query_string": "promo=SAVE10"},
		"sdk": {"name": "sentry.python.django", "version": "2.18.0"}
	}`, eventID))
	ev, err := sentryprotocol.ParseEventPayload(payload)
	if err != nil {
		panic(err)
	}
	return ev
}

// BenchmarkIngestEventNewIssue measures the cost of the first occurrence of
// each exception (worst case: a fresh issue row, no existing fingerprint to
// match).
func BenchmarkIngestEventNewIssue(b *testing.B) {
	h, p := newTestHandler(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev := realisticEvent(fmt.Sprintf("bench-new-%d", i))
		ev.Exception.Values[0].Value = fmt.Sprintf("Invalid quantity: -%d", i) // unique fingerprint-irrelevant value, unique event
		ev.Fingerprint = []string{fmt.Sprintf("bench-fp-%d", i)}
		h.ingestEvent(ctx, p, ev)
	}
}

// BenchmarkIngestEventSameIssue measures the common case: repeated
// occurrences of the same bug, which take the grouping/increment path
// instead of creating a new issue row.
func BenchmarkIngestEventSameIssue(b *testing.B) {
	h, p := newTestHandler(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ev := realisticEvent(fmt.Sprintf("bench-same-%d", i))
		h.ingestEvent(ctx, p, ev)
	}
}
