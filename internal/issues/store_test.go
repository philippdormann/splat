package issues

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"errgotrack/internal/database"
	"errgotrack/internal/projects"
)

func newTestDB(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	p, err := projects.NewStore(db).Create("Test Project", "test-project", "production", "", 90)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return db, p.ID
}

func TestUpsertCreatesThenIncrements(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	r1, err := store.Upsert(ctx, projectID, "fp-1", "ValueError: x", "ValueError", "x", "checkout", "error", "production", "1.0.0", time.Now())
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if !r1.IsNew || r1.Issue.EventCount != 1 {
		t.Fatalf("expected new issue with count 1, got %+v", r1)
	}

	r2, err := store.Upsert(ctx, projectID, "fp-1", "ValueError: x", "ValueError", "x", "checkout", "error", "production", "1.0.0", time.Now())
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if r2.IsNew || r2.Issue.ID != r1.Issue.ID || r2.Issue.EventCount != 2 {
		t.Fatalf("expected same issue incremented to 2, got %+v", r2)
	}
}

func TestUpsertReopensResolvedIssue(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	r1, _ := store.Upsert(ctx, projectID, "fp-2", "t", "T", "v", "c", "error", "production", "", time.Now())
	if err := store.SetStatus(r1.Issue.ID, StatusResolved); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	r2, err := store.Upsert(ctx, projectID, "fp-2", "t", "T", "v", "c", "error", "production", "", time.Now())
	if err != nil {
		t.Fatalf("reopen upsert: %v", err)
	}
	if !r2.Reopened || r2.Issue.Status != StatusUnresolved {
		t.Fatalf("expected reopened+unresolved, got %+v", r2)
	}
}

func TestUpsertIgnoredIssueStaysIgnored(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	r1, _ := store.Upsert(ctx, projectID, "fp-3", "t", "T", "v", "c", "error", "production", "", time.Now())
	store.SetStatus(r1.Issue.ID, StatusIgnored)

	r2, err := store.Upsert(ctx, projectID, "fp-3", "t", "T", "v", "c", "error", "production", "", time.Now())
	if err != nil {
		t.Fatalf("upsert on ignored issue: %v", err)
	}
	if r2.Issue.Status != StatusIgnored {
		t.Fatalf("ignored issues should not auto-reopen, got status %q", r2.Issue.Status)
	}
	if r2.Issue.EventCount != 2 {
		t.Fatalf("event count should still increment while ignored, got %d", r2.Issue.EventCount)
	}
}

// TestUpsertConcurrentSameFingerprint fires many concurrent ingestions for
// the same fingerprint and checks the final count matches exactly - the
// thing BEGIN IMMEDIATE in Upsert exists to guarantee under SQLite's
// single-writer model.
func TestUpsertConcurrentSameFingerprint(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	const n = 40
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Upsert(ctx, projectID, "fp-concurrent", "t", "T", "v", "c", "error", "production", "", time.Now())
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent upsert error: %v", err)
		}
	}

	list, _, err := store.List(ListFilter{ProjectID: projectID, Limit: 200})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *Issue
	for _, iss := range list {
		if iss.Fingerprint == "fp-concurrent" {
			found = iss
		}
	}
	if found == nil {
		t.Fatal("issue with fingerprint fp-concurrent not found")
	}
	if found.EventCount != n {
		t.Fatalf("expected event_count = %d, got %d (lost update under concurrency)", n, found.EventCount)
	}
}

func TestSparkline24hBucketsByHourAndZeroFillsGaps(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	r, err := store.Upsert(ctx, projectID, "fp-spark", "ValueError: x", "ValueError", "x", "checkout", "error", "production", "1.0.0", time.Now())
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	issueID := r.Issue.ID

	now := time.Now().UTC()
	insert := func(id string, ts time.Time) {
		_, err := db.Exec(`INSERT INTO events (event_id, issue_id, project_id, timestamp) VALUES (?, ?, ?, ?)`,
			id, issueID, projectID, ts.Format(time.RFC3339Nano))
		if err != nil {
			t.Fatalf("insert event: %v", err)
		}
	}
	insert("e1", now)                     // current hour
	insert("e2", now)                     // same hour, should add to the same bucket
	insert("e3", now.Add(-5*time.Hour))   // an older bucket
	insert("e4", now.Add(-30*time.Hour))  // outside the 24h window entirely

	out, err := store.Sparkline24h([]int64{issueID})
	if err != nil {
		t.Fatalf("sparkline: %v", err)
	}
	counts := out[issueID]
	if len(counts) != 24 {
		t.Fatalf("expected 24 buckets, got %d", len(counts))
	}
	var total int64
	for _, c := range counts {
		total += c
	}
	if total != 3 {
		t.Fatalf("expected 3 events within the 24h window (e4 excluded), got %d across buckets %v", total, counts)
	}
	if counts[23] != 2 {
		t.Fatalf("expected the most recent bucket to hold the 2 same-hour events, got %d (%v)", counts[23], counts)
	}
}
