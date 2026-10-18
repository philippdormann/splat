package events

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"errgotrack/internal/database"
	"errgotrack/internal/issues"
	"errgotrack/internal/projects"
)

func setup(t *testing.T) (*sql.DB, int64, int64) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	p, err := projects.NewStore(db).Create("P", "p", "production", "", 90)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	res, err := issues.NewStore(db).Upsert(context.Background(), p.ID, "fp", "t", "T", "v", "c", "error", "production", "", time.Now())
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return db, p.ID, res.Issue.ID
}

func insertEvent(t *testing.T, store *Store, projectID, issueID int64, eventID string, ts time.Time, message string) {
	t.Helper()
	if err := store.Insert(&Event{
		EventID: eventID, IssueID: issueID, ProjectID: projectID, Environment: "production",
		Timestamp: ts, Message: message, ExceptionType: "ValueError", ExceptionValue: message,
		StacktraceJSON: "{}", TagsJSON: "{}", ExtraJSON: "{}", BreadcrumbsJSON: "[]", UserJSON: "{}", SDKJSON: "{}", RequestJSON: "{}",
	}); err != nil {
		t.Fatalf("insert event %s: %v", eventID, err)
	}
}

func TestSearchFullText(t *testing.T) {
	db, projectID, issueID := setup(t)
	store := NewStore(db)
	now := time.Now()
	insertEvent(t, store, projectID, issueID, "e1", now, "database connection refused")
	insertEvent(t, store, projectID, issueID, "e2", now.Add(time.Second), "disk quota exceeded")

	results, err := store.Search(SearchFilter{ProjectID: projectID, Text: "connection"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].EventID != "e1" {
		t.Fatalf("expected only e1, got %+v", results)
	}
}

func TestSearchFiltersByEnvironmentAndTime(t *testing.T) {
	db, projectID, issueID := setup(t)
	store := NewStore(db)
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now()
	insertEvent(t, store, projectID, issueID, "old-event", old, "stale")
	insertEvent(t, store, projectID, issueID, "new-event", recent, "fresh")

	results, err := store.Search(SearchFilter{ProjectID: projectID, Since: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].EventID != "new-event" {
		t.Fatalf("time filter failed, got %+v", results)
	}
}

func TestNeighborNavigation(t *testing.T) {
	db, projectID, issueID := setup(t)
	store := NewStore(db)
	base := time.Now()
	insertEvent(t, store, projectID, issueID, "e1", base, "first")
	insertEvent(t, store, projectID, issueID, "e2", base.Add(time.Minute), "second")
	insertEvent(t, store, projectID, issueID, "e3", base.Add(2*time.Minute), "third")

	mid, err := store.Get("e2")
	if err != nil {
		t.Fatalf("get e2: %v", err)
	}
	prev, err := store.Neighbor(issueID, mid.Timestamp, "prev")
	if err != nil || prev.EventID != "e1" {
		t.Fatalf("prev = %+v, err=%v", prev, err)
	}
	next, err := store.Neighbor(issueID, mid.Timestamp, "next")
	if err != nil || next.EventID != "e3" {
		t.Fatalf("next = %+v, err=%v", next, err)
	}
}

func TestPaginationLimitClamping(t *testing.T) {
	db, projectID, issueID := setup(t)
	store := NewStore(db)
	for i := 0; i < 5; i++ {
		insertEvent(t, store, projectID, issueID, "e"+string(rune('a'+i)), time.Now().Add(time.Duration(i)*time.Second), "m")
	}
	results, err := store.ListByIssue(issueID, 2, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected limit=2 to return 2 rows, got %d", len(results))
	}
}
