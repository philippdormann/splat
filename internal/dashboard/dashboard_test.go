package dashboard

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"errgotrack/internal/database"
	"errgotrack/internal/issues"
	"errgotrack/internal/projects"
)

func pointValue(points []Point, label string) int64 {
	for _, p := range points {
		if p.Label == label {
			return p.Value
		}
	}
	return -1
}

// TestIssuesByStatusReflectsCurrentStatusNotEventHistory is the core
// semantic this query depends on: it's a snapshot of each issue's current
// status, not a count of events grouped by whatever status the issue had
// when each event arrived. A resolved issue's older events must still
// count toward "resolved", not get stuck showing as "unresolved" forever.
func TestIssuesByStatusReflectsCurrentStatusNotEventHistory(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	p, err := projects.NewStore(db).Create("Test", "test", "production", "", 90)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issueStore := issues.NewStore(db)
	ctx := context.Background()

	r1, err := issueStore.Upsert(ctx, p.ID, "fp-a", "A", "TypeA", "", "", "error", "production", "", time.Now())
	if err != nil {
		t.Fatalf("upsert a: %v", err)
	}
	if err := issueStore.SetStatus(r1.Issue.ID, issues.StatusResolved); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	r2, err := issueStore.Upsert(ctx, p.ID, "fp-b", "B", "TypeB", "", "", "error", "production", "", time.Now())
	if err != nil {
		t.Fatalf("upsert b: %v", err)
	}

	// IssuesByStatus joins against the events table (it counts issues with
	// activity in range, not just any issue that ever existed), so each
	// issue needs an actual event row, not just the issues-table row that
	// Upsert maintains on its own.
	for i, issueID := range []int64{r1.Issue.ID, r2.Issue.ID} {
		_, err := db.Exec(`INSERT INTO events (event_id, issue_id, project_id, timestamp) VALUES (?, ?, ?, ?)`,
			"evt-"+string(rune('a'+i)), issueID, p.ID, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			t.Fatalf("insert event: %v", err)
		}
	}

	since := time.Now().Add(-time.Hour)
	points, err := NewStore(db).IssuesByStatus(since)
	if err != nil {
		t.Fatalf("issues by status: %v", err)
	}
	if got := pointValue(points, "resolved"); got != 1 {
		t.Fatalf("expected 1 resolved issue (status set after its event was recorded), got %d (%+v)", got, points)
	}
	if got := pointValue(points, "unresolved"); got != 1 {
		t.Fatalf("expected 1 unresolved issue, got %d (%+v)", got, points)
	}
}

func TestEventsBySeverityBucketsLevels(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	p, err := projects.NewStore(db).Create("Test", "test", "production", "", 0)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issueStore := issues.NewStore(db)
	ctx := context.Background()
	now := time.Now()

	r1, err := issueStore.Upsert(ctx, p.ID, "fp-err", "E", "TypeE", "", "", "error", "production", "", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	r2, err := issueStore.Upsert(ctx, p.ID, "fp-warn", "W", "TypeW", "", "", "warning", "production", "", now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	for i, e := range []struct {
		id      string
		issueID int64
		ts      time.Time
	}{
		{"sev-e1", r1.Issue.ID, now.Add(-time.Hour)},
		{"sev-w1", r2.Issue.ID, now.Add(-time.Hour)},
		{"sev-w2", r2.Issue.ID, now.Add(-30 * time.Minute)},
	} {
		_, err := db.Exec(`INSERT INTO events (event_id, issue_id, project_id, timestamp) VALUES (?, ?, ?, ?)`,
			e.id, e.issueID, p.ID, e.ts.UTC().Format(time.RFC3339Nano))
		if err != nil {
			t.Fatalf("insert event %d: %v", i, err)
		}
	}

	pts, err := NewStore(db).EventsBySeverity(p.ID, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("severity: %v", err)
	}
	got := map[string]int64{}
	for _, sp := range pts {
		got[sp.Level] += sp.Value
	}
	if got["error"] != 1 || got["warning"] != 2 {
		t.Fatalf("severity counts = %v", got)
	}

	n, err := NewStore(db).NewIssues(p.ID, now.Add(-24*time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("new issues = %d, %v", n, err)
	}
	e, err := NewStore(db).ProjectEvents(p.ID, now.Add(-24*time.Hour))
	if err != nil || e != 3 {
		t.Fatalf("events = %d, %v", e, err)
	}
}
