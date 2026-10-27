package quotas

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/philippdormann/splat/internal/database"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "quota.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db)
}

func intp(n int) *int { return &n }

func TestDefaultsAndOverrides(t *testing.T) {
	s := testStore(t)
	seedProject(t, s, 1)
	h, d := s.Defaults()
	if h != 5000 || d != 50000 {
		t.Fatalf("defaults = %d/%d", h, d)
	}
	oh, od, err := s.Overrides(1)
	if err != nil || oh != nil || od != nil {
		t.Fatalf("overrides = %v/%v %v", oh, od, err)
	}
	eh, ed, err := s.Effective(1)
	if err != nil || eh != 5000 || ed != 50000 {
		t.Fatalf("effective = %d/%d %v", eh, ed, err)
	}
	if err := s.SetOverrides(1, intp(10), nil); err != nil {
		t.Fatal(err)
	}
	eh, ed, err = s.Effective(1)
	if err != nil || eh != 10 || ed != 50000 {
		t.Fatalf("effective after override = %d/%d %v", eh, ed, err)
	}
	if err := s.SetOverrides(1, intp(0), intp(0)); err != nil {
		t.Fatal(err)
	}
	eh, ed, _ = s.Effective(1)
	if eh != 0 || ed != 0 {
		t.Fatalf("zero must mean unlimited, got %d/%d", eh, ed)
	}
	if err := s.SetOverrides(1, intp(-1), nil); err == nil {
		t.Fatal("negative override must fail")
	}
	if err := s.SetDefaults(100, 200); err != nil {
		t.Fatal(err)
	}
	if h, d := s.Defaults(); h != 100 || d != 200 {
		t.Fatalf("defaults after set = %d/%d", h, d)
	}
	if err := s.SetDefaults(-1, 0); err == nil {
		t.Fatal("negative defaults must fail")
	}
}

func seedProject(t *testing.T, s *Store, id int64) int64 {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO projects (id, name, slug, public_key) VALUES (?, ?, ?, ?)`,
		id, "p", "p", "key")
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(`INSERT INTO issues (project_id, fingerprint, title, first_seen, last_seen) VALUES (?, ?, ?, ?, ?)`,
		id, "fp", "t", ts, ts)
	if err != nil {
		t.Fatal(err)
	}
	issueID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return issueID
}

func insertEvents(t *testing.T, s *Store, projectID, issueID int64, n int, at time.Time, tag string) {
	t.Helper()
	// Minimal rows: quota counts events, grouping is irrelevant.
	for i := range n {
		_, err := s.db.Exec(`INSERT INTO events (event_id, issue_id, project_id, timestamp)
			VALUES (?, ?, ?, ?)`, "ev-"+tag+"-"+string(rune('a'+i)), issueID, projectID, at.UTC().Format(time.RFC3339Nano))
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestUsageAndCheck(t *testing.T) {
	s := testStore(t)
	issueID := seedProject(t, s, 1)
	now := time.Now().UTC()
	insertEvents(t, s, 1, issueID, 3, now.Add(-30*time.Minute), "a")
	insertEvents(t, s, 1, issueID, 2, now.Add(-2*time.Hour), "b")
	h, d, err := s.Usage(1)
	if err != nil {
		t.Fatal(err)
	}
	if h != 3 || d != 5 {
		t.Fatalf("usage = %d/%d", h, d)
	}
	if dec := s.Check(context.Background(), 1, "p", "u"); !dec.Allowed {
		t.Fatal("under default quota must pass")
	}
	if err := s.SetOverrides(1, intp(3), nil); err != nil {
		t.Fatal(err)
	}
	dec := s.Check(context.Background(), 1, "p", "u")
	if dec.Allowed || dec.Window != "hourly" || dec.RetryAfter != 300 {
		t.Fatalf("decision = %+v", dec)
	}
}

func TestBreachNotifyThrottling(t *testing.T) {
	s := testStore(t)
	var calls int
	s.Notify = func(ctx context.Context, projectID int64, projectName, projectURL, window string, used, limit int) {
		calls++
	}
	issueID := seedProject(t, s, 2)
	now := time.Now().UTC()
	insertEvents(t, s, 2, issueID, 5, now.Add(-time.Minute), "a")
	if err := s.SetOverrides(2, intp(2), nil); err != nil {
		t.Fatal(err)
	}
	s.Check(context.Background(), 2, "p", "u")
	s.Check(context.Background(), 2, "p", "u")
	if calls != 1 {
		t.Fatalf("notify calls = %d, want 1 (cooldown)", calls)
	}
	// Recovery clears the stamp: next breach alerts again.
	if _, err := s.db.Exec(`DELETE FROM events WHERE project_id = 2`); err != nil {
		t.Fatal(err)
	}
	if dec := s.Check(context.Background(), 2, "p", "u"); !dec.Allowed {
		t.Fatal("recovered project must pass")
	}
	insertEvents(t, s, 2, issueID, 5, now.Add(-time.Minute), "c")
	s.Check(context.Background(), 2, "p", "u")
	if calls != 2 {
		t.Fatalf("notify calls after re-breach = %d, want 2", calls)
	}
}
