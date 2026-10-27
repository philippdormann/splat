package traces

import (
	"path/filepath"
	"testing"

	"github.com/philippdormann/splat/internal/database"
	"github.com/philippdormann/splat/internal/sentryprotocol"
)

const txnPayload = `{
	"event_id": "abc123", "transaction": "GET /api/users",
	"release": "1.2.3", "environment": "production",
	"start_timestamp": 1719398400.0, "timestamp": 1719398400.25,
	"contexts": {"trace": {"trace_id": "t1", "span_id": "s1", "op": "http.server", "status": "ok"}},
	"spans": [
		{"trace_id": "t1", "span_id": "s2", "parent_span_id": "s1", "op": "db", "description": "SELECT users",
			"status": "ok", "start_timestamp": 1719398400.01, "timestamp": 1719398400.05},
		{"span_id": "s3", "parent_span_id": "s1", "op": "cache",
			"start_timestamp": 1719398400.1, "timestamp": 1719398400.2}
	]
}`

func TestInsertRecentAndTrace(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "traces.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO projects (name, slug, public_key) VALUES ('t', 't', 'k')`); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)

	txn, err := sentryprotocol.ParseTransaction([]byte(txnPayload))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Insert(1, "production", txn); err != nil {
		t.Fatal(err)
	}
	// Duplicate delivery (same event_id) is a no-op, not an error.
	if err := s.Insert(1, "production", txn); err != nil {
		t.Fatalf("duplicate insert: %v", err)
	}

	recent, err := s.Recent(1, 10)
	if err != nil || len(recent) != 1 {
		t.Fatalf("recent = %v, %v", recent, err)
	}
	if recent[0].Name != "GET /api/users" || recent[0].TraceID != "t1" || recent[0].SpanCount != 2 {
		t.Fatalf("recent = %+v", recent[0])
	}
	if d := recent[0].DurationMS; d < 249.9 || d > 250.1 {
		t.Fatalf("duration = %v", d)
	}

	txns, spans, err := s.Trace(1, "t1")
	if err != nil || len(txns) != 1 || len(spans) != 2 {
		t.Fatalf("trace = %d/%d, %v", len(txns), len(spans), err)
	}
	// Span without its own trace_id inherits the transaction's.
	if spans[1].TraceID != "t1" || spans[1].Op != "cache" {
		t.Fatalf("span = %+v", spans[1])
	}

	// Other projects and unknown traces read empty.
	if txns, spans, err := s.Trace(2, "t1"); err != nil || len(txns) != 0 || len(spans) != 0 {
		t.Fatalf("cross-project trace = %d/%d, %v", len(txns), len(spans), err)
	}
	if txns, spans, err := s.Trace(1, "nope"); err != nil || len(txns) != 0 || len(spans) != 0 {
		t.Fatalf("unknown trace = %d/%d, %v", len(txns), len(spans), err)
	}
}
