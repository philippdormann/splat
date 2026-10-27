package sentryprotocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseLogBatchToleratesFieldVariants(t *testing.T) {
	raw := `{"items": [
		{"timestamp": 1719398400.5, "trace_id": "aaa", "span_id": "bbb", "severity_number": 9, "body": "hello", "attributes": {"user": "u1"}},
		{"timestamp": "2024-06-26T12:00:01Z", "level": "WARN", "message": "careful"},
		{"timestamp": 1719398402, "severity_number": 18, "body": "boom"},
		{"timestamp": 1719398403, "severity_number": 99, "body": "weird"},
		{}
	]}`
	batch, err := ParseLogBatch([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(batch.Items) != 5 {
		t.Fatalf("items = %d", len(batch.Items))
	}
	first := batch.Items[0]
	if !first.ParsedTimestamp().Equal(time.Unix(1719398400, 500*1e6).UTC()) {
		t.Fatalf("float timestamp = %v", first.ParsedTimestamp())
	}
	if first.NormalizedLevel() != "info" || first.Text() != "hello" {
		t.Fatalf("first = %+v", first)
	}
	if batch.Items[1].NormalizedLevel() != "warn" || batch.Items[1].Text() != "careful" {
		t.Fatalf("second = %+v", batch.Items[1])
	}
	if batch.Items[2].NormalizedLevel() != "error" {
		t.Fatalf("severity 18 should map to error, got %q", batch.Items[2].NormalizedLevel())
	}
	if batch.Items[3].NormalizedLevel() != "fatal" {
		t.Fatalf("out-of-range severity should map to fatal, got %q", batch.Items[3].NormalizedLevel())
	}
	// Empty record: defaults, never empty-message panics downstream.
	if batch.Items[4].NormalizedLevel() != "info" {
		t.Fatalf("empty record level = %q", batch.Items[4].NormalizedLevel())
	}
	var attrs map[string]string
	if err := json.Unmarshal(first.Attributes, &attrs); err != nil || attrs["user"] != "u1" {
		t.Fatalf("attributes = %s, %v", first.Attributes, err)
	}
}

func TestParseTransactionWithSpans(t *testing.T) {
	raw := `{
		"event_id": "abc123", "transaction": "GET /api/users",
		"release": "1.2.3", "environment": "production",
		"start_timestamp": 1719398400.0, "timestamp": 1719398400.25,
		"contexts": {"trace": {"trace_id": "t1", "span_id": "s1", "op": "http.server", "status": "ok"}},
		"spans": [
			{"trace_id": "t1", "span_id": "s2", "parent_span_id": "s1", "op": "db", "description": "SELECT users",
				"status": "ok", "start_timestamp": 1719398400.01, "timestamp": 1719398400.05},
			{"trace_id": "t1", "span_id": "s3", "parent_span_id": "s1", "op": "http.client",
				"start_timestamp": 1719398400.1, "timestamp": 1719398400.2}
		]
	}`
	txn, err := ParseTransaction([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if txn.Name != "GET /api/users" || txn.Release != "1.2.3" {
		t.Fatalf("txn = %+v", txn)
	}
	tc := txn.Trace()
	if tc.TraceID != "t1" || tc.SpanID != "s1" || tc.Op != "http.server" {
		t.Fatalf("trace ctx = %+v", tc)
	}
	if len(txn.Spans) != 2 {
		t.Fatalf("spans = %d", len(txn.Spans))
	}
	if d := txn.Spans[0].DurationMS(); d < 39.9 || d > 40.1 {
		t.Fatalf("db span duration = %v", d)
	}
	// Span without an end timestamp: zero duration, not negative.
	bare := Span{SpanID: "x"}
	if bare.DurationMS() != 0 {
		t.Fatalf("bare span duration = %v", bare.DurationMS())
	}
	// Transaction without contexts: empty trace, not an error.
	empty, err := ParseTransaction([]byte(`{"transaction": "job"}`))
	if err != nil {
		t.Fatalf("parse minimal: %v", err)
	}
	if tc := empty.Trace(); tc.TraceID != "" {
		t.Fatalf("trace ctx = %+v", tc)
	}
}
