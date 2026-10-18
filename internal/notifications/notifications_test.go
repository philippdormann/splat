package notifications

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"errgotrack/internal/database"
	"errgotrack/internal/issues"
	"errgotrack/internal/projects"
)

// newTestStore sets up a project with Telegram enabled and the default
// notification rule, plus one real issue row - notification_queue.issue_id
// has a foreign key into issues(id), so tests must alert about an issue
// that actually exists rather than an arbitrary int.
func newTestStore(t *testing.T) (*Store, int64, int64) {
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
	store := NewStore(db, slog.Default())
	if err := store.SetTelegramConfig(TelegramConfig{BotToken: "t", ChatID: "c", Enabled: true}); err != nil {
		t.Fatalf("enable telegram: %v", err)
	}
	if err := store.UpsertDefaultRule(p.ID); err != nil {
		t.Fatalf("default rule: %v", err)
	}
	return store, p.ID, res.Issue.ID
}

func pendingCount(t *testing.T, store *Store) int {
	t.Helper()
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM notification_queue WHERE status = 'pending'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestEvaluateEnqueuesNewIssue(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()})
	if got := pendingCount(t, store); got != 1 {
		t.Fatalf("expected 1 queued notification, got %d", got)
	}
}

func TestEvaluateSkipsWhenTelegramDisabled(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	store.SetTelegramConfig(TelegramConfig{Enabled: false})
	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()})
	if got := pendingCount(t, store); got != 0 {
		t.Fatalf("expected no notification queued while disabled, got %d", got)
	}
}

func TestEvaluateRespectsCooldown(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	ev := AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()}
	store.Evaluate(context.Background(), ev)
	ev2 := ev
	ev2.IsNew, ev2.Reopened = false, true // still a notify-worthy transition, but same issue within cooldown
	store.Evaluate(context.Background(), ev2)
	if got := pendingCount(t, store); got != 1 {
		t.Fatalf("expected cooldown to suppress the second alert, got %d queued", got)
	}
}

func TestEvaluateSkipsNonNotifyWorthyEvent(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: false, Reopened: false, EventCount: 5, Timestamp: time.Now()})
	if got := pendingCount(t, store); got != 0 {
		t.Fatalf("expected no alert for a plain recurrence with no threshold rule, got %d", got)
	}
}

func pendingChannels(t *testing.T, store *Store) []string {
	t.Helper()
	rows, err := store.db.Query(`SELECT channel FROM notification_queue WHERE status = 'pending' ORDER BY channel`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		rows.Scan(&c)
		out = append(out, c)
	}
	return out
}

func TestEvaluateFansOutToEveryEnabledChannel(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	store.SetWebhookConfig("slack", WebhookConfig{URL: "http://example.invalid", Enabled: true})
	store.SetWebhookConfig("discord", WebhookConfig{URL: "http://example.invalid", Enabled: true})
	// teams left disabled on purpose

	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()})

	channels := pendingChannels(t, store)
	if len(channels) != 3 {
		t.Fatalf("expected telegram+slack+discord queued (teams disabled), got %v", channels)
	}
	want := map[string]bool{"telegram": true, "slack": true, "discord": true}
	for _, c := range channels {
		if !want[c] {
			t.Fatalf("unexpected channel %q queued: %v", c, channels)
		}
	}
}

// TestDrainDeliversChannelsIndependently is the actual point of splitting
// the queue per-channel: one channel failing must not block or duplicate
// another channel's successful delivery for the same alert.
func TestDrainDeliversChannelsIndependently(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	store.SetTelegramConfig(TelegramConfig{Enabled: false}) // isolate to just the two webhooks under test

	slackOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer slackOK.Close()
	discordDown := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer discordDown.Close()

	store.SetWebhookConfig("slack", WebhookConfig{URL: slackOK.URL, Enabled: true})
	store.SetWebhookConfig("discord", WebhookConfig{URL: discordDown.URL, Enabled: true})

	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()})
	store.DrainOnce(context.Background(), 10)

	var slackStatus, discordStatus string
	store.db.QueryRow(`SELECT status FROM notification_queue WHERE channel = 'slack'`).Scan(&slackStatus)
	store.db.QueryRow(`SELECT status FROM notification_queue WHERE channel = 'discord'`).Scan(&discordStatus)

	if slackStatus != "sent" {
		t.Fatalf("slack should have been delivered despite discord's outage, status=%q", slackStatus)
	}
	if discordStatus != "pending" {
		t.Fatalf("discord should still be pending (scheduled for retry), status=%q", discordStatus)
	}
}

func TestDrainSkipsChannelDisabledBeforeDelivery(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	store.SetTelegramConfig(TelegramConfig{Enabled: false})
	store.SetWebhookConfig("slack", WebhookConfig{URL: "http://example.invalid", Enabled: true})

	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()})
	// Disabled after enqueue, before the worker ever drains it.
	store.SetWebhookConfig("slack", WebhookConfig{URL: "http://example.invalid", Enabled: false})
	store.DrainOnce(context.Background(), 10)

	var status string
	store.db.QueryRow(`SELECT status FROM notification_queue WHERE channel = 'slack'`).Scan(&status)
	if status != "skipped" {
		t.Fatalf("expected skipped (not failed/retried) for a channel disabled before delivery, got %q", status)
	}
}
