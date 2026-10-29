package traces

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

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

func TestLatencyPercentiles(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "latency.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO projects (name, slug, public_key) VALUES ('t', 't', 'k')`); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)

	// Empty project reads zero without error.
	empty, err := s.Latency(1, time.Now().Add(-24*time.Hour))
	if err != nil || empty.Count != 0 {
		t.Fatalf("empty latency = %+v, %v", empty, err)
	}

	// 100 transactions, durations 10ms..1000ms in 10ms steps.
	since := time.Now().Add(-time.Hour)
	for i := 1; i <= 100; i++ {
		ts := since.Add(time.Duration(i) * time.Second).UTC().Format(time.RFC3339Nano)
		name := "GET /api/users"
		if i > 70 {
			name = "POST /api/orders"
		}
		if _, err := db.Exec(`INSERT INTO transactions
			(event_id, project_id, name, trace_id, span_id, release, environment,
			 start_timestamp, timestamp, duration_ms, span_count)
			VALUES (?, 1, ?, 't', 's', 'r', 'prod', ?, ?, ?, 0)`,
			fmt.Sprintf("e%d", i), name, ts, ts, float64(i*10)); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.Latency(1, since.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != 100 {
		t.Fatalf("count = %d", got.Count)
	}
	// Nearest-rank on 10..1000: p50=500, p75=750, p95=950, p99=990.
	if got.P50 != 500 || got.P75 != 750 || got.P95 != 950 || got.P99 != 990 {
		t.Fatalf("overall = p50:%v p75:%v p95:%v p99:%v", got.P50, got.P75, got.P95, got.P99)
	}
	if got.Sampled {
		t.Fatalf("sampled = true for 100 rows")
	}
	if len(got.ByName) != 2 {
		t.Fatalf("byName = %+v", got.ByName)
	}
	if got.ByName[0].Name != "GET /api/users" || got.ByName[0].Count != 70 {
		t.Fatalf("byName[0] = %+v", got.ByName[0])
	}
	if got.ByName[0].P50 != 350 || got.ByName[0].P95 != 670 {
		t.Fatalf("users = %+v", got.ByName[0])
	}
	if got.ByName[1].Name != "POST /api/orders" || got.ByName[1].Count != 30 {
		t.Fatalf("byName[1] = %+v", got.ByName[1])
	}

	// Narrow window excludes everything.
	narrow, err := s.Latency(1, time.Now().Add(time.Hour))
	if err != nil || narrow.Count != 0 {
		t.Fatalf("narrow = %+v, %v", narrow, err)
	}

	// Other project reads empty.
	other, err := s.Latency(2, since.Add(-time.Minute))
	if err != nil || other.Count != 0 {
		t.Fatalf("other project = %+v, %v", other, err)
	}
}

func TestPercentileNearestRank(t *testing.T) {
	if got := percentileNearestRank(nil, 0.5); got != 0 {
		t.Fatalf("empty = %v", got)
	}
	vals := []float64{10, 20, 30, 40}
	if got := percentileNearestRank(vals, 0); got != 10 {
		t.Fatalf("q0 = %v", got)
	}
	if got := percentileNearestRank(vals, 1); got != 40 {
		t.Fatalf("q1 = %v", got)
	}
	if got := percentileNearestRank(vals, 0.5); got != 20 {
		t.Fatalf("p50 = %v", got)
	}
}
