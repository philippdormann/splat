package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/philippdormann/splat/internal/database"
	"github.com/philippdormann/splat/internal/projects"
)

func TestUsageOnFreshDB(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	p, err := projects.NewStore(db).Create("P", "p", "production", "", 90)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	_ = p

	s := NewStore(db, filepath.Join(t.TempDir(), "test.db"))
	u, err := s.Usage(context.Background())
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if u.Driver != "sqlite" {
		t.Fatalf("driver = %q, want sqlite", u.Driver)
	}
	got := map[string]int64{}
	for _, tb := range u.Tables {
		got[tb.Name] = tb.Rows
	}
	if got["projects"] != 1 {
		t.Fatalf("projects rows = %d, want 1", got["projects"])
	}
	if _, ok := got["events"]; !ok {
		t.Fatal("expected events table in usage report")
	}
	// source_maps exists since migration 0007; a genuinely unknown table
	// must be skipped, not fatal (covered implicitly - no error above).
	if _, ok := got["source_maps"]; !ok {
		t.Fatal("expected source_maps table in usage report")
	}
}

func TestVacuum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	s := NewStore(db, path)
	if err := s.Vacuum(context.Background()); err != nil {
		t.Fatalf("vacuum: %v", err)
	}
}
