package logs

import (
	"path/filepath"
	"testing"

	"errgotrack/internal/database"
	"errgotrack/internal/sentryprotocol"
)

func TestInsertAndList(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO projects (name, slug, public_key) VALUES ('t', 't', 'k'), ('t2', 't2', 'k2')`); err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)

	batch, err := sentryprotocol.ParseLogBatch([]byte(`{"items": [
		{"timestamp": 1719398400, "severity_number": 9, "body": "hello", "trace_id": "t1"},
		{"timestamp": 1719398401, "severity_number": 18, "body": "boom", "trace_id": "t1"},
		{"timestamp": 1719398402, "severity_number": 9, "body": "other"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertBatch(1, "production", batch); err != nil {
		t.Fatal(err)
	}

	all, err := s.List(1, Filter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("list = %v, %v", all, err)
	}
	if all[0].Message != "other" || all[0].Environment != "production" {
		t.Fatalf("newest first with default env: %+v", all[0])
	}

	errors, err := s.List(1, Filter{Level: "error"})
	if err != nil || len(errors) != 1 || errors[0].Message != "boom" {
		t.Fatalf("level filter = %v, %v", errors, err)
	}

	byTrace, err := s.List(1, Filter{TraceID: "t1"})
	if err != nil || len(byTrace) != 2 {
		t.Fatalf("trace filter = %v, %v", byTrace, err)
	}

	byQuery, err := s.List(1, Filter{Query: "hell"})
	if err != nil || len(byQuery) != 1 {
		t.Fatalf("query filter = %v, %v", byQuery, err)
	}

	// Other projects are isolated; empty batch is a no-op.
	if err := s.InsertBatch(2, "staging", &sentryprotocol.LogBatch{}); err != nil {
		t.Fatal(err)
	}
	mine, _ := s.List(2, Filter{})
	if len(mine) != 0 {
		t.Fatalf("project isolation broken: %v", mine)
	}
}
