package retention

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"errgotrack/internal/database"
	"errgotrack/internal/events"
	"errgotrack/internal/issues"
	"errgotrack/internal/projects"
)

func TestSweepDeletesOldEventsButKeepsIssueSummary(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	p, err := projects.NewStore(db).Create("P", "p", "production", "", 1) // 1-day retention
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	issStore := issues.NewStore(db)
	oldTime := time.Now().AddDate(0, 0, -5)
	recentTime := time.Now()
	res, err := issStore.Upsert(context.Background(), p.ID, "fp", "old issue", "T", "v", "c", "error", "production", "", oldTime)
	if err != nil {
		t.Fatalf("upsert old: %v", err)
	}
	// A second occurrence, same as ingestion would do per event: Upsert
	// bumps the issue's lifetime event_count, then the raw event row is
	// stored separately.
	if _, err := issStore.Upsert(context.Background(), p.ID, "fp", "old issue", "T", "v", "c", "error", "production", "", recentTime); err != nil {
		t.Fatalf("upsert recent: %v", err)
	}

	evStore := events.NewStore(db)
	if err := evStore.Insert(&events.Event{
		EventID: "old-event", IssueID: res.Issue.ID, ProjectID: p.ID, Timestamp: oldTime,
		StacktraceJSON: "{}", TagsJSON: "{}", ExtraJSON: "{}", BreadcrumbsJSON: "[]", UserJSON: "{}", SDKJSON: "{}", RequestJSON: "{}",
	}); err != nil {
		t.Fatalf("insert old event: %v", err)
	}
	if err := evStore.Insert(&events.Event{
		EventID: "recent-event", IssueID: res.Issue.ID, ProjectID: p.ID, Timestamp: recentTime,
		StacktraceJSON: "{}", TagsJSON: "{}", ExtraJSON: "{}", BreadcrumbsJSON: "[]", UserJSON: "{}", SDKJSON: "{}", RequestJSON: "{}",
	}); err != nil {
		t.Fatalf("insert recent event: %v", err)
	}

	sweeper := NewSweeper(db, slog.Default())
	sweeper.sweepOnce(context.Background())

	if _, err := evStore.Get("old-event"); err == nil {
		t.Fatal("old event should have been deleted by retention sweep")
	}
	if _, err := evStore.Get("recent-event"); err != nil {
		t.Fatalf("recent event should survive retention sweep: %v", err)
	}

	issue, err := issStore.Get(res.Issue.ID)
	if err != nil {
		t.Fatalf("issue summary should remain after its events age out: %v", err)
	}
	if issue.EventCount != 2 {
		t.Fatalf("retention must not rewrite the issue's event_count (it's a lifetime counter, not derived from live rows), got %d", issue.EventCount)
	}
}
