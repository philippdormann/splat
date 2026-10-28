package issues

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/philippdormann/splat/internal/database"
	"github.com/philippdormann/splat/internal/projects"
)

func newTestDB(t *testing.T) (*database.DB, int64) {
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

func TestReopenCountIncrements(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	r1, _ := store.Upsert(ctx, projectID, "fp-rc", "t", "T", "v", "c", "error", "production", "", time.Now())
	if r1.Issue.ReopenedCount != 0 {
		t.Fatalf("new issue should start at 0, got %d", r1.Issue.ReopenedCount)
	}
	// Plain reoccurrence without a resolve in between must not count.
	r2, _ := store.Upsert(ctx, projectID, "fp-rc", "t", "T", "v", "c", "error", "production", "", time.Now())
	if r2.Issue.ReopenedCount != 0 {
		t.Fatalf("non-reopening upsert should not count, got %d", r2.Issue.ReopenedCount)
	}
	for i := 0; i < 2; i++ {
		if err := store.SetStatus(r1.Issue.ID, StatusResolved); err != nil {
			t.Fatalf("resolve: %v", err)
		}
		r, err := store.Upsert(ctx, projectID, "fp-rc", "t", "T", "v", "c", "error", "production", "", time.Now())
		if err != nil {
			t.Fatalf("reopen upsert: %v", err)
		}
		if !r.Reopened {
			t.Fatal("expected reopened")
		}
	}
	got, err := store.Get(r1.Issue.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ReopenedCount != 2 {
		t.Fatalf("expected reopened_count 2, got %d", got.ReopenedCount)
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
	insert("e1", now)                    // current hour
	insert("e2", now)                    // same hour, should add to the same bucket
	insert("e3", now.Add(-5*time.Hour))  // an older bucket
	insert("e4", now.Add(-30*time.Hour)) // outside the 24h window entirely

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

func TestUpsertMutedIssueStaysMuted(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	r1, _ := store.Upsert(ctx, projectID, "fp-mute", "t", "T", "v", "c", "error", "production", "", time.Now())
	if err := store.SetMuted(r1.Issue.ID, time.Time{}, 0); err != nil {
		t.Fatalf("mute: %v", err)
	}

	r2, err := store.Upsert(ctx, projectID, "fp-mute", "t", "T", "v", "c", "error", "production", "", time.Now())
	if err != nil {
		t.Fatalf("upsert on muted issue: %v", err)
	}
	if r2.Issue.Status != StatusMuted {
		t.Fatalf("muted issues should not auto-reopen, got status %q", r2.Issue.Status)
	}
	if r2.Reopened {
		t.Fatal("muted upsert must not report reopened")
	}
	if r2.Issue.EventCount != 2 {
		t.Fatalf("event count should still increment while muted, got %d", r2.Issue.EventCount)
	}
}

func TestUpsertExpiredMuteUnmutes(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	r1, _ := store.Upsert(ctx, projectID, "fp-mute-exp", "t", "T", "v", "c", "error", "production", "", time.Now())
	past := time.Now().Add(-time.Hour)
	if err := store.SetMuted(r1.Issue.ID, past, 0); err != nil {
		t.Fatalf("mute: %v", err)
	}

	r2, err := store.Upsert(ctx, projectID, "fp-mute-exp", "t", "T", "v", "c", "error", "production", "", time.Now())
	if err != nil {
		t.Fatalf("upsert after mute expiry: %v", err)
	}
	if r2.Issue.Status != StatusUnresolved {
		t.Fatalf("expired mute should unmute, got status %q", r2.Issue.Status)
	}
	if !r2.Reopened {
		t.Fatal("lapsed mute should report reopened so rules can alert")
	}
	if !r2.Issue.MutedUntil.IsZero() {
		t.Fatalf("muted_until should be cleared, got %v", r2.Issue.MutedUntil)
	}
}

func TestUpsertCountMuteDecrementsAndLifts(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	r1, _ := store.Upsert(ctx, projectID, "fp-mute-n", "t", "T", "v", "c", "error", "production", "", time.Now())
	if err := store.SetMuted(r1.Issue.ID, time.Time{}, 3); err != nil {
		t.Fatalf("mute: %v", err)
	}

	// First two appearances stay muted while the countdown ticks down.
	for want := int64(2); want >= 1; want-- {
		r, err := store.Upsert(ctx, projectID, "fp-mute-n", "t", "T", "v", "c", "error", "production", "", time.Now())
		if err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if r.Issue.Status != StatusMuted || r.Reopened {
			t.Fatalf("countdown should stay muted, got %+v", r.Issue)
		}
		if r.Issue.MutedCount != want {
			t.Fatalf("expected %d remaining, got %d", want, r.Issue.MutedCount)
		}
	}

	// The crossing event resurfaces the issue and reports reopened.
	r, err := store.Upsert(ctx, projectID, "fp-mute-n", "t", "T", "v", "c", "error", "production", "", time.Now())
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if r.Issue.Status != StatusUnresolved || !r.Reopened {
		t.Fatalf("countdown exhaustion should unmute+reopen, got %+v", r.Issue)
	}
	if r.Issue.MutedCount != 0 {
		t.Fatalf("countdown should be spent, got %d", r.Issue.MutedCount)
	}
}

func TestMergeMovesEventsAndDeletesLosers(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	w, _ := store.Upsert(ctx, projectID, "fp-w", "winner", "T", "v", "c", "error", "production", "", time.Now())
	l, _ := store.Upsert(ctx, projectID, "fp-l", "loser", "T", "v", "c", "error", "production", "", time.Now())
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, e := range []struct {
		eventID string
		issueID int64
	}{
		{"e1", w.Issue.ID},
		{"e2", l.Issue.ID},
		{"e3", l.Issue.ID},
	} {
		if _, err := db.Exec(`INSERT INTO events (event_id, issue_id, project_id, timestamp) VALUES (?, ?, ?, ?)`, e.eventID, e.issueID, projectID, now); err != nil {
			t.Fatalf("insert event: %v", err)
		}
	}
	if _, err := store.AddNote(l.Issue.ID, "loser note"); err != nil {
		t.Fatalf("add note: %v", err)
	}
	if err := store.SetStatus(w.Issue.ID, StatusResolved); err != nil {
		t.Fatalf("resolve winner: %v", err)
	}

	merged, err := store.Merge(w.Issue.ID, []int64{l.Issue.ID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if merged.EventCount != 3 {
		t.Fatalf("expected recomputed count 3, got %d", merged.EventCount)
	}
	if merged.Status != StatusResolved {
		t.Fatalf("winner status should be kept, got %q", merged.Status)
	}
	notes, err := store.Notes(w.Issue.ID)
	if err != nil || len(notes) != 1 || notes[0].Body != "loser note" {
		t.Fatalf("loser note should move to winner, got %+v, err %v", notes, err)
	}
	if _, err := store.Get(l.Issue.ID); err == nil {
		t.Fatal("loser should be deleted")
	}
}

func TestMergeRejectsCrossProject(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	p2, err := projects.NewStore(db).Create("Other", "other", "production", "", 90)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	w, _ := store.Upsert(ctx, projectID, "fp-w2", "winner", "T", "v", "c", "error", "production", "", time.Now())
	l, _ := store.Upsert(ctx, p2.ID, "fp-l2", "loser", "T", "v", "c", "error", "production", "", time.Now())
	if _, err := store.Merge(w.Issue.ID, []int64{l.Issue.ID}); err == nil {
		t.Fatal("cross-project merge must fail")
	}
}

func TestSetStatusClearsMute(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	r1, _ := store.Upsert(ctx, projectID, "fp-mute-clr", "t", "T", "v", "c", "error", "production", "", time.Now())
	if err := store.SetMuted(r1.Issue.ID, time.Now().Add(time.Hour), 0); err != nil {
		t.Fatalf("mute: %v", err)
	}
	if err := store.SetStatus(r1.Issue.ID, StatusResolved); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	got, err := store.Get(r1.Issue.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusResolved || !got.MutedUntil.IsZero() {
		t.Fatalf("resolve should clear mute, got %+v", got)
	}
}

func TestResolveInNextReleaseKeepsSameReleaseResolved(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	r1, _ := store.Upsert(ctx, projectID, "fp-rnr", "t", "T", "v", "c", "error", "production", "1.2.3", time.Now())
	if err := store.ResolveInNextRelease(r1.Issue.ID); err != nil {
		t.Fatalf("resolve in next release: %v", err)
	}
	got, _ := store.Get(r1.Issue.ID)
	if got.Status != StatusResolved || got.ResolvedInRelease != "1.2.3" {
		t.Fatalf("pin mismatch: %+v", got)
	}

	// Same release: stays resolved, no reopen.
	r2, err := store.Upsert(ctx, projectID, "fp-rnr", "t", "T", "v", "c", "error", "production", "1.2.3", time.Now())
	if err != nil {
		t.Fatalf("same-release upsert: %v", err)
	}
	if r2.Issue.Status != StatusResolved || r2.Reopened {
		t.Fatalf("same release must stay resolved, got %+v", r2)
	}
}

func TestResolveInNextReleaseReopensOnNewRelease(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	r1, _ := store.Upsert(ctx, projectID, "fp-rnr2", "t", "T", "v", "c", "error", "production", "1.2.3", time.Now())
	if err := store.ResolveInNextRelease(r1.Issue.ID); err != nil {
		t.Fatalf("resolve in next release: %v", err)
	}

	r2, err := store.Upsert(ctx, projectID, "fp-rnr2", "t", "T", "v", "c", "error", "production", "1.2.4", time.Now())
	if err != nil {
		t.Fatalf("new-release upsert: %v", err)
	}
	if r2.Issue.Status != StatusUnresolved || !r2.Reopened {
		t.Fatalf("new release must reopen, got %+v", r2)
	}
	if r2.Issue.ResolvedInRelease != "" {
		t.Fatalf("pin should be cleared on reopen, got %q", r2.Issue.ResolvedInRelease)
	}
}

func TestPlainResolveStillReopensOnAnyRelease(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	r1, _ := store.Upsert(ctx, projectID, "fp-plain", "t", "T", "v", "c", "error", "production", "1.2.3", time.Now())
	if err := store.SetStatus(r1.Issue.ID, StatusResolved); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	r2, err := store.Upsert(ctx, projectID, "fp-plain", "t", "T", "v", "c", "error", "production", "1.2.3", time.Now())
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if r2.Issue.Status != StatusUnresolved || !r2.Reopened {
		t.Fatalf("plain resolve must reopen on any event, got %+v", r2)
	}
}

func TestIssueNotesCRUD(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	r1, _ := store.Upsert(ctx, projectID, "fp-notes", "t", "T", "v", "c", "error", "production", "", time.Now())

	if _, err := store.AddNote(r1.Issue.ID, "   "); err == nil {
		t.Fatal("want error on empty note")
	}
	if _, err := store.AddNote(r1.Issue.ID, strings.Repeat("x", MaxNoteBytes+1)); err == nil {
		t.Fatal("want error on oversized note")
	}
	n1, err := store.AddNote(r1.Issue.ID, "fixed in staging, watching")
	if err != nil {
		t.Fatalf("add note: %v", err)
	}
	if n1.ID == 0 || n1.Body != "fixed in staging, watching" {
		t.Fatalf("note mismatch: %+v", n1)
	}
	if _, err := store.AddNote(999999, "orphan"); err == nil {
		t.Fatal("want FK error on unknown issue")
	}

	notes, err := store.Notes(r1.Issue.ID)
	if err != nil || len(notes) != 1 {
		t.Fatalf("notes = %v, %v", notes, err)
	}
	if err := store.DeleteNote(r1.Issue.ID, n1.ID); err != nil {
		t.Fatalf("delete note: %v", err)
	}
	notes, _ = store.Notes(r1.Issue.ID)
	if len(notes) != 0 {
		t.Fatalf("want empty notes after delete, got %v", notes)
	}
	// Deleting another issue's note id must not touch this issue's notes.
	n2, _ := store.AddNote(r1.Issue.ID, "keep me")
	if err := store.DeleteNote(r1.Issue.ID+999, n2.ID); err != nil {
		t.Fatalf("scoped delete: %v", err)
	}
	notes, _ = store.Notes(r1.Issue.ID)
	if len(notes) != 1 {
		t.Fatalf("cross-issue delete must not remove notes, got %v", notes)
	}
}

func TestNewSinceWindowsByFirstSeen(t *testing.T) {
	db, projectID := newTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	mk := func(fp string, at time.Time) {
		t.Helper()
		if _, err := store.Upsert(ctx, projectID, fp, "t-"+fp, "E", "v", "", "error", "production", "1.0", at); err != nil {
			t.Fatal(err)
		}
	}
	mk("old", base)
	mk("mid1", base.Add(48*time.Hour))
	mk("mid2", base.Add(72*time.Hour))
	mk("new", base.Add(120*time.Hour))

	// Window from day 2 on: mid1, mid2, new — biggest event_count first
	// is trivially all 1s here, so assert the set.
	got, err := store.NewSince(projectID, base.Add(24*time.Hour), nil, 10)
	if err != nil || len(got) != 3 {
		t.Fatalf("open window = %v, %v", got, err)
	}
	// Bounded window excludes "new".
	end := base.Add(96 * time.Hour)
	got, err = store.NewSince(projectID, base.Add(24*time.Hour), &end, 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("bounded window = %v, %v", got, err)
	}
	for _, iss := range got {
		if iss.FirstSeen.Before(base.Add(24*time.Hour)) || !iss.FirstSeen.Before(end) {
			t.Fatalf("out-of-window issue: %+v", iss)
		}
	}
	// Limit applies.
	got, err = store.NewSince(projectID, base, nil, 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("limited = %v, %v", got, err)
	}
	// Other projects are invisible.
	got, err = store.NewSince(projectID+999, base, nil, 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("other project = %v, %v", got, err)
	}
}
