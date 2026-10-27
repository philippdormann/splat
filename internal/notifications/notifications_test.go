package notifications

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/philippdormann/splat/internal/database"
	"github.com/philippdormann/splat/internal/issues"
	"github.com/philippdormann/splat/internal/projects"
	"github.com/philippdormann/splat/internal/webhooks"
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

func TestRuleCRUD(t *testing.T) {
	store, projectID, _ := newTestStore(t)
	r, err := store.CreateRule(Rule{Name: "prod email", ProjectID: sqlNull(projectID), Environment: "production", NotifyNewIssue: true, CooldownSeconds: 60, Channels: []string{"email"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if r.Name != "prod email" || len(r.Channels) != 1 || r.Channels[0] != "email" {
		t.Fatalf("round trip mismatch: %+v", r)
	}
	if _, err := store.CreateRule(Rule{Name: "bad", Channels: []string{"pager"}}); err == nil {
		t.Fatal("want error on unknown channel")
	}
	r.Name = "renamed"
	r.NotifyReopened = true
	updated, err := store.UpdateRule(r)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "renamed" || !updated.NotifyReopened {
		t.Fatalf("update mismatch: %+v", updated)
	}
	if err := store.DeleteRule(r.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.GetRule(r.ID); err == nil {
		t.Fatal("want not-found after delete")
	}
}

func sqlNull(id int64) sql.NullInt64 {
	return sql.NullInt64{Int64: id, Valid: true}
}

func TestEvaluateRespectsPerRuleChannels(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	// Default rule (all channels) would fan out to telegram; replace it
	// with a slack-only rule.
	rules, _ := store.Rules()
	for _, r := range rules {
		store.DeleteRule(r.ID)
	}
	store.SetWebhookConfig("slack", WebhookConfig{URL: "http://example.invalid", Enabled: true})
	if _, err := store.CreateRule(Rule{Name: "slack only", NotifyNewIssue: true, Channels: []string{"slack"}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()})
	channels := pendingChannels(t, store)
	if len(channels) != 1 || channels[0] != "slack" {
		t.Fatalf("want only slack queued, got %v", channels)
	}
}

func TestSMTPConfigRoundTrip(t *testing.T) {
	store, _, _ := newTestStore(t)
	cfg, err := store.GetSMTPConfig()
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("SMTP must default to disabled")
	}
	cfg.Host, cfg.Port, cfg.To, cfg.Enabled = "smtp.example.com", 587, "oncall@example.com", true
	if err := store.SetSMTPConfig(cfg); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, _ := store.GetSMTPConfig()
	if got.Host != "smtp.example.com" || got.To != "oncall@example.com" || !got.Enabled {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if !ValidChannel("email") || ValidChannel("pager") {
		t.Fatal("ValidChannel wrong")
	}
}

func TestGenericWebhookCRUD(t *testing.T) {
	store, _, _ := newTestStore(t)
	h, err := store.CreateGenericWebhook(GenericWebhook{Name: "ops", URL: "http://example.invalid/hook", Secret: "s3cr3t", Headers: map[string]string{"X-Team": "ops"}, Enabled: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if h.ID == 0 || h.Headers["X-Team"] != "ops" {
		t.Fatalf("round trip mismatch: %+v", h)
	}
	if _, err := store.CreateGenericWebhook(GenericWebhook{Name: "no-url"}); err == nil {
		t.Fatal("want error on empty URL")
	}
	hooks, err := store.GenericWebhooks()
	if err != nil || len(hooks) != 1 {
		t.Fatalf("list = %v, %v", hooks, err)
	}
	h.Name = "ops-renamed"
	updated, err := store.UpdateGenericWebhook(h)
	if err != nil || updated.Name != "ops-renamed" {
		t.Fatalf("update = %+v, %v", updated, err)
	}
	if err := store.DeleteGenericWebhook(h.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	hooks, _ = store.GenericWebhooks()
	if len(hooks) != 0 {
		t.Fatalf("want empty list after delete, got %v", hooks)
	}
}

func TestGenericWebhookDeliverySignsAndMergesHeaders(t *testing.T) {
	store, _, _ := newTestStore(t)
	var gotSig, gotTeam, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig, gotTeam, gotCT = r.Header.Get(SignatureHeader), r.Header.Get("X-Team"), r.Header.Get("Content-Type")
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Read the body in a second server round-trip is awkward; instead verify
	// the signature inline below against genericPayload directly.
	h, err := store.CreateGenericWebhook(GenericWebhook{Name: "ops", URL: srv.URL, Secret: "s3cr3t", Headers: map[string]string{"X-Team": "ops"}, Enabled: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	a := webhooks.Alert{ProjectName: "P", Environment: "prod", ExceptionType: "E", Message: "boom", IsNew: true, Timestamp: time.Now()}
	if err := store.sendGeneric(context.Background(), h, a); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotTeam != "ops" {
		t.Fatalf("custom header not merged, got %q", gotTeam)
	}
	if gotCT != "application/json" {
		t.Fatalf("content-type = %q", gotCT)
	}
	wantSig := signBody("s3cr3t", genericPayload(a))
	if gotSig != wantSig || !strings.HasPrefix(gotSig, "sha256=") {
		t.Fatalf("signature = %q, want %q", gotSig, wantSig)
	}
	if signBody("", genericPayload(a)) != "" {
		t.Fatal("empty secret must mean unsigned")
	}
}

func TestGenericChannelFanout(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	if _, err := store.CreateGenericWebhook(GenericWebhook{Name: "ops", URL: srv.URL, Enabled: true}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Default rule (no explicit channels) must include the generic channel
	// once at least one generic webhook is enabled.
	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()})
	channels := pendingChannels(t, store)
	found := false
	for _, c := range channels {
		if c == "generic" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want generic in %v", channels)
	}
}

func TestEvaluateSkipsMutedAndIgnored(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	if _, err := store.CreateRule(Rule{Name: "threshold", NotifyThreshold: 5, CooldownSeconds: 0}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	for _, status := range []string{"muted", "ignored"} {
		// Event count hits the threshold - would fire on any other status.
		store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: false, Reopened: false, EventCount: 5, Status: status, Timestamp: time.Now()})
		if n := pendingCount(t, store); n != 0 {
			t.Fatalf("status %q must never notify, got %d queued", status, n)
		}
	}
	// Sanity: the same threshold fires for unresolved issues.
	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: false, Reopened: false, EventCount: 5, Status: "unresolved", Timestamp: time.Now()})
	if n := pendingCount(t, store); n == 0 {
		t.Fatal("threshold should fire for unresolved issues")
	}
}
