package sessions

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/philippdormann/splat/internal/database"
	"github.com/philippdormann/splat/internal/events"
	"github.com/philippdormann/splat/internal/issues"
	"github.com/philippdormann/splat/internal/projects"
)

func setup(t *testing.T) (*Store, int64) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	p, err := projects.NewStore(db).Create("P", "p", "production", "", 0)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return NewStore(db), p.ID
}

func TestUpsertAndCrashFree(t *testing.T) {
	s, pid := setup(t)
	// 3 healthy (2 users), 1 crashed (1 user).
	updates := []string{
		`{"sid":"a","did":"u1","release":"1.0","status":"ok"}`,
		`{"sid":"b","did":"u1","release":"1.0","status":"exited"}`,
		`{"sid":"c","did":"u2","release":"1.0","status":"ok"}`,
		`{"sid":"d","did":"u3","release":"1.0","status":"crashed"}`,
	}
	for _, u := range updates {
		if err := s.UpsertSession(pid, []byte(u)); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	// Status update on the same sid replaces, not duplicates.
	if err := s.UpsertSession(pid, []byte(`{"sid":"a","did":"u1","release":"1.0","status":"errored"}`)); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if err := s.UpsertSession(pid, []byte(`{"nope":true}`)); err == nil {
		t.Fatal("want error on missing sid")
	}
	if err := s.UpsertSession(pid, []byte(`not json`)); err == nil {
		t.Fatal("want error on bad json")
	}

	sum, err := s.CrashFree(pid, "", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("crashfree: %v", err)
	}
	if sum.TotalSessions != 4 {
		t.Fatalf("sessions = %d, want 4", sum.TotalSessions)
	}
	if sum.CrashFreeSessions != 75 {
		t.Fatalf("crash-free sessions = %v, want 75", sum.CrashFreeSessions)
	}
	if sum.TotalUsers != 3 {
		t.Fatalf("users = %d, want 3", sum.TotalUsers)
	}
	if diff := sum.CrashFreeUsers - 100*2.0/3.0; diff < -0.001 || diff > 0.001 {
		t.Fatalf("crash-free users = %v, want 66.67", sum.CrashFreeUsers)
	}
}

func TestAggregatesFeedSessions(t *testing.T) {
	s, pid := setup(t)
	body := `{"attrs":{"release":"2.0","environment":"prod"},"aggregates":[
		{"started":"2026-10-27T10:00:00Z","exited":8,"errored":1,"crashed":1,"abnormal":0},
		{"started":"2026-10-27T10:01:00Z","exited":5,"crashed":0,"abnormal":0}
	]}`
	if err := s.AddAggregates(pid, []byte(body)); err != nil {
		t.Fatalf("aggregates: %v", err)
	}
	sum, err := s.CrashFree(pid, "2.0", time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("crashfree: %v", err)
	}
	// 15 sessions, 1 crashed -> 93.33%.
	if sum.TotalSessions != 15 {
		t.Fatalf("sessions = %d, want 15", sum.TotalSessions)
	}
	if diff := sum.CrashFreeSessions - 100*14.0/15.0; diff < -0.001 || diff > 0.001 {
		t.Fatalf("crash-free = %v", sum.CrashFreeSessions)
	}
	// Idempotent day buckets must not double count on re-ingest of the
	// same day... (SDKs resend on retry; each receipt sums, which matches
	// Sentry's at-least-once aggregate semantics.)
}

func TestHealthTimeseriesAndReleases(t *testing.T) {
	s, pid := setup(t)
	if err := s.UpsertSession(pid, []byte(`{"sid":"a","did":"u1","release":"1.0","started":"2026-10-27T10:00:00Z","status":"ok"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSession(pid, []byte(`{"sid":"b","did":"u2","release":"1.0","started":"2026-10-27T11:00:00Z","status":"crashed"}`)); err != nil {
		t.Fatal(err)
	}
	pts, err := s.HealthTimeseries(pid, time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("timeseries: %v", err)
	}
	if len(pts) != 1 || pts[0].Healthy != 1 || pts[0].Crashed != 1 {
		t.Fatalf("points = %+v", pts)
	}

	rel, err := s.Releases(pid)
	if err != nil {
		t.Fatalf("releases: %v", err)
	}
	if len(rel) != 1 || rel[0].Release != "1.0" {
		t.Fatalf("releases = %+v", rel)
	}
	if rel[0].Sessions != 2 || rel[0].Users != 2 {
		t.Fatalf("release row = %+v", rel[0])
	}
	if rel[0].CrashFreeSessions != 50 {
		t.Fatalf("crash-free = %v", rel[0].CrashFreeSessions)
	}
}

func TestReleaseEnvironmentsGroupsEventsAndSessions(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "env.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	p, err := projects.NewStore(db).Create("P", "p", "production", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	pid := p.ID
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

	iss, err := issues.NewStore(db).Upsert(t.Context(), pid, "fp", "Bug", "E", "v", "", "error", "production", "2.0", now)
	if err != nil {
		t.Fatal(err)
	}
	evStore := events.NewStore(db)
	for i, env := range []string{"production", "production", "staging"} {
		if err := evStore.Insert(&events.Event{
			EventID:     "e" + string(rune('0'+i)) + "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			IssueID:     iss.Issue.ID,
			ProjectID:   pid,
			Environment: env,
			Release:     "2.0",
			Timestamp:   now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	s := NewStore(db)
	if err := s.UpsertSession(pid, []byte(`{"sid":"a","did":"u1","release":"2.0","environment":"production","started":"2026-10-27T10:00:00Z","status":"ok"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSession(pid, []byte(`{"sid":"b","did":"u2","release":"2.0","environment":"staging","started":"2026-10-27T10:00:00Z","status":"ok"}`)); err != nil {
		t.Fatal(err)
	}

	rows, err := s.ReleaseEnvironments(pid, "2.0")
	if err != nil {
		t.Fatalf("envs: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Environment != "production" || rows[0].Events != 2 || rows[0].Sessions != 1 {
		t.Fatalf("production row = %+v", rows[0])
	}
	if rows[1].Environment != "staging" || rows[1].Events != 1 || rows[1].Sessions != 1 {
		t.Fatalf("staging row = %+v", rows[1])
	}

	empty, err := s.ReleaseEnvironments(pid, "9.9")
	if err != nil || len(empty) != 0 {
		t.Fatalf("unknown release = %v, %v", empty, err)
	}
}
