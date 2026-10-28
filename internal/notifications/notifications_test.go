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
	store := NewStore(db, slog.Default(), "test-session-secret-32-bytes-minimum!!")
	if err := store.UpsertDefaultRule(p.ID); err != nil {
		t.Fatalf("default rule: %v", err)
	}
	return store, p.ID, res.Issue.ID
}

func mustTelegram(t *testing.T, store *Store) int64 {
	t.Helper()
	in, err := store.CreateIntegration(Integration{
		Name: "tg", Provider: "telegram",
		Credentials: map[string]any{"bot_token": "t", "default_chat_id": "c"},
		IsEnabled:   true,
	})
	if err != nil {
		t.Fatalf("create telegram: %v", err)
	}
	return in.ID
}

func attachIntegration(t *testing.T, store *Store, projectID, integrationID int64, override RoutingOverride) {
	t.Helper()
	rules, err := store.Rules()
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	for _, r := range rules {
		if r.ProjectID.Valid && r.ProjectID.Int64 == projectID {
			r.Integrations = append(r.Integrations, RuleIntegration{IntegrationID: integrationID, Override: override})
			if _, err := store.UpdateRule(r); err != nil {
				t.Fatalf("attach: %v", err)
			}
			return
		}
	}
	t.Fatalf("no rule for project %d", projectID)
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
	tg := mustTelegram(t, store)
	attachIntegration(t, store, projectID, tg, RoutingOverride{})
	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()})
	if got := pendingCount(t, store); got != 1 {
		t.Fatalf("expected 1 queued notification, got %d", got)
	}
}

func TestEvaluateSkipsWhenIntegrationDisabled(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	tg := mustTelegram(t, store)
	attachIntegration(t, store, projectID, tg, RoutingOverride{})
	in, _ := store.GetIntegration(tg)
	in.IsEnabled = false
	if _, err := store.UpdateIntegration(in); err != nil {
		t.Fatalf("disable: %v", err)
	}
	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()})
	if got := pendingCount(t, store); got != 0 {
		t.Fatalf("expected no notification queued while disabled, got %d", got)
	}
}

func TestEvaluateRespectsCooldown(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	tg := mustTelegram(t, store)
	attachIntegration(t, store, projectID, tg, RoutingOverride{})
	ev := AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()}
	store.Evaluate(context.Background(), ev)
	ev2 := ev
	ev2.IsNew, ev2.Reopened = false, true
	store.Evaluate(context.Background(), ev2)
	if got := pendingCount(t, store); got != 1 {
		t.Fatalf("expected cooldown to suppress the second alert, got %d queued", got)
	}
}

func TestEvaluateSkipsNonNotifyWorthyEvent(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	mustTelegram(t, store)
	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: false, Reopened: false, EventCount: 5, Timestamp: time.Now()})
	if got := pendingCount(t, store); got != 0 {
		t.Fatalf("expected no alert for a plain recurrence with no threshold rule, got %d", got)
	}
}

func pendingProviders(t *testing.T, store *Store) []string {
	t.Helper()
	rows, err := store.db.Query(`SELECT COALESCE(provider, channel) FROM notification_queue WHERE status = 'pending' ORDER BY 1`)
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

func mustWebhook(t *testing.T, store *Store, provider, url string) int64 {
	t.Helper()
	in, err := store.CreateIntegration(Integration{
		Name: provider + "-test", Provider: provider,
		Credentials: map[string]any{"url": url}, IsEnabled: true,
	})
	if err != nil {
		t.Fatalf("create %s: %v", provider, err)
	}
	return in.ID
}

func TestEvaluateFansOutToEveryLinkedIntegration(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	tg := mustTelegram(t, store)
	slack := mustWebhook(t, store, "slack", "http://example.invalid/slack")
	discord := mustWebhook(t, store, "discord", "http://example.invalid/discord")
	attachIntegration(t, store, projectID, tg, RoutingOverride{})
	attachIntegration(t, store, projectID, slack, RoutingOverride{})
	attachIntegration(t, store, projectID, discord, RoutingOverride{})

	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()})

	got := pendingProviders(t, store)
	if len(got) != 3 {
		t.Fatalf("expected telegram+slack+discord queued, got %v", got)
	}
	want := map[string]bool{"telegram": true, "slack": true, "discord": true}
	for _, c := range got {
		if !want[c] {
			t.Fatalf("unexpected provider %q queued: %v", c, got)
		}
	}
}

func TestRoutingOverrideWinsOverDefault(t *testing.T) {
	store, _, _ := newTestStore(t)
	in, err := store.CreateIntegration(Integration{Name: "tg", Provider: "telegram",
		Credentials: map[string]any{"bot_token": "t", "default_chat_id": "default"}, IsEnabled: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Empty override with default present validates.
	if err := ValidateRoutingOverride("telegram", in.Credentials, RoutingOverride{}); err != nil {
		t.Fatalf("default chat should validate: %v", err)
	}
	// Missing default + empty override fails.
	in2, err := store.CreateIntegration(Integration{Name: "tg2", Provider: "telegram",
		Credentials: map[string]any{"bot_token": "t"}, IsEnabled: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := ValidateRoutingOverride("telegram", in2.Credentials, RoutingOverride{}); err == nil {
		t.Fatal("want error for missing chat_id with no default")
	}
	// Explicit override fixes it.
	if err := ValidateRoutingOverride("telegram", in2.Credentials, RoutingOverride{ChatID: "other"}); err != nil {
		t.Fatalf("override should validate: %v", err)
	}
	// SMTP requires recipients.
	if err := ValidateRoutingOverride("smtp", map[string]any{"host": "h", "from": "a@b.c"}, RoutingOverride{}); err == nil {
		t.Fatal("want error for smtp without recipients")
	}
	if err := ValidateRoutingOverride("smtp", map[string]any{"host": "h", "from": "a@b.c"}, RoutingOverride{To: []string{"ops@example.com"}}); err != nil {
		t.Fatalf("smtp override should validate: %v", err)
	}
	if !ValidProvider("telegram") || ValidProvider("pager") {
		t.Fatal("ValidProvider wrong")
	}
}

func TestDrainDeliversIntegrationsIndependently(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	slackOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer slackOK.Close()
	discordDown := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer discordDown.Close()

	slack := mustWebhook(t, store, "slack", slackOK.URL)
	discord := mustWebhook(t, store, "discord", discordDown.URL)
	attachIntegration(t, store, projectID, slack, RoutingOverride{})
	attachIntegration(t, store, projectID, discord, RoutingOverride{})

	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()})
	store.DrainOnce(context.Background(), 10)

	var slackStatus, discordStatus string
	store.db.QueryRow(`SELECT status FROM notification_queue WHERE integration_id = ?`, slack).Scan(&slackStatus)
	store.db.QueryRow(`SELECT status FROM notification_queue WHERE integration_id = ?`, discord).Scan(&discordStatus)

	if slackStatus != "sent" {
		t.Fatalf("slack should have been delivered despite discord's outage, status=%q", slackStatus)
	}
	if discordStatus != "pending" {
		t.Fatalf("discord should still be pending (scheduled for retry), status=%q", discordStatus)
	}
}

func TestDrainSkipsIntegrationDisabledBeforeDelivery(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	slack := mustWebhook(t, store, "slack", "http://example.invalid")
	attachIntegration(t, store, projectID, slack, RoutingOverride{})
	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()})
	in, _ := store.GetIntegration(slack)
	in.IsEnabled = false
	store.UpdateIntegration(in)
	store.DrainOnce(context.Background(), 10)

	var status string
	store.db.QueryRow(`SELECT status FROM notification_queue WHERE integration_id = ?`, slack).Scan(&status)
	if status != "skipped" {
		t.Fatalf("expected skipped for an integration disabled before delivery, got %q", status)
	}
}

func TestRuleCRUD(t *testing.T) {
	store, projectID, _ := newTestStore(t)
	smtpIn, err := store.CreateIntegration(Integration{Name: "smtp", Provider: "smtp",
		Credentials: map[string]any{"host": "smtp.example.com", "port": 587, "from": "splat@example.com", "use_tls": true}, IsEnabled: true})
	if err != nil {
		t.Fatalf("create smtp: %v", err)
	}
	r, err := store.CreateRule(Rule{Name: "prod email", ProjectID: sqlNull(projectID), Environment: "production", NotifyNewIssue: true, CooldownSeconds: 60,
		Integrations: []RuleIntegration{{IntegrationID: smtpIn.ID, Override: RoutingOverride{To: []string{"ops@example.com"}}}}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if r.Name != "prod email" || len(r.Integrations) != 1 {
		t.Fatalf("round trip mismatch: %+v", r)
	}
	if _, err := store.CreateRule(Rule{Name: "bad", Integrations: []RuleIntegration{{IntegrationID: 9999}}}); err == nil {
		t.Fatal("want error on unknown integration")
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

func TestEvaluateRespectsPerRuleIntegrations(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	rules, _ := store.Rules()
	for _, r := range rules {
		store.DeleteRule(r.ID)
	}
	slack := mustWebhook(t, store, "slack", "http://example.invalid")
	if _, err := store.CreateRule(Rule{Name: "slack only", NotifyNewIssue: true,
		Integrations: []RuleIntegration{{IntegrationID: slack}}}); err != nil {
		t.Fatalf("create: %v", err)
	}
	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: true, EventCount: 1, Timestamp: time.Now()})
	got := pendingProviders(t, store)
	if len(got) != 1 || got[0] != "slack" {
		t.Fatalf("want only slack queued, got %v", got)
	}
}

func TestIntegrationCRUDValidation(t *testing.T) {
	store, _, _ := newTestStore(t)
	if _, err := store.CreateIntegration(Integration{Name: "x", Provider: "pager", Credentials: map[string]any{}}); err == nil {
		t.Fatal("want error on unknown provider")
	}
	if _, err := store.CreateIntegration(Integration{Name: "no-url", Provider: "slack", Credentials: map[string]any{}}); err == nil {
		t.Fatal("want error on empty URL")
	}
	in, err := store.CreateIntegration(Integration{Name: "ops", Provider: "generic",
		Credentials: map[string]any{"url": "http://example.invalid/hook", "secret": "s3cr3t", "headers": map[string]any{"X-Team": "ops"}}, IsEnabled: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if in.ID == 0 {
		t.Fatalf("round trip mismatch: %+v", in)
	}
	list, err := store.Integrations()
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
	in.Name = "ops-renamed"
	updated, err := store.UpdateIntegration(in)
	if err != nil || updated.Name != "ops-renamed" {
		t.Fatalf("update = %+v, %v", updated, err)
	}
	if err := store.DeleteIntegration(in.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = store.Integrations()
	if len(list) != 0 {
		t.Fatalf("want empty list after delete, got %v", list)
	}
}

func TestGenericDeliverySignsAndMergesHeaders(t *testing.T) {
	store, _, _ := newTestStore(t)
	var gotSig, gotTeam, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig, gotTeam, gotCT = r.Header.Get(SignatureHeader), r.Header.Get("X-Team"), r.Header.Get("Content-Type")
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	in, err := store.CreateIntegration(Integration{Name: "ops", Provider: "generic",
		Credentials: map[string]any{"url": srv.URL, "secret": "s3cr3t", "headers": map[string]any{"X-Team": "ops"}}, IsEnabled: true})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	a := AlertEvent{ProjectName: "P", Environment: "prod", ExceptionType: "E", Message: "boom", IsNew: true, Timestamp: time.Now()}
	if _, err := store.deliver(context.Background(), in, RoutingOverride{}, a); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotTeam != "ops" {
		t.Fatalf("custom header not merged, got %q", gotTeam)
	}
	if gotCT != "application/json" {
		t.Fatalf("content-type = %q", gotCT)
	}
	wantSig := signBody("s3cr3t", genericPayload(webhooks.Alert{ProjectName: "P", Environment: "prod", ExceptionType: "E", Message: "boom", IsNew: true, Timestamp: a.Timestamp}))
	if gotSig != wantSig || !strings.HasPrefix(gotSig, "sha256=") {
		t.Fatalf("signature = %q, want %q", gotSig, wantSig)
	}
	if signBody("", genericPayload(webhooks.Alert{Timestamp: a.Timestamp})) != "" {
		t.Fatal("empty secret must mean unsigned")
	}
}

func TestEvaluateSkipsMutedAndIgnored(t *testing.T) {
	store, projectID, issueID := newTestStore(t)
	tg := mustTelegram(t, store)
	attachIntegration(t, store, projectID, tg, RoutingOverride{})
	if _, err := store.CreateRule(Rule{Name: "threshold", NotifyThreshold: 5, CooldownSeconds: 0, Integrations: []RuleIntegration{{IntegrationID: tg}}}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	for _, status := range []string{"muted", "ignored"} {
		store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: false, Reopened: false, EventCount: 5, Status: status, Timestamp: time.Now()})
		if n := pendingCount(t, store); n != 0 {
			t.Fatalf("status %q must never notify, got %d queued", status, n)
		}
	}
	store.Evaluate(context.Background(), AlertEvent{IssueID: issueID, ProjectID: projectID, IsNew: false, Reopened: false, EventCount: 5, Status: "unresolved", Timestamp: time.Now()})
	if n := pendingCount(t, store); n == 0 {
		t.Fatal("threshold should fire for unresolved issues")
	}
}

func TestCredentialsEncryptedAtRest(t *testing.T) {
	store, _, _ := newTestStore(t)
	in, err := store.CreateIntegration(Integration{
		Name: "smtp-enc", Provider: "smtp",
		Credentials: map[string]any{"host": "mail.example.com", "from": "a@b.c", "password": "super-secret-pw"},
		IsEnabled:   true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var raw string
	if err := store.db.QueryRow(`SELECT credentials FROM integrations WHERE id = ?`, in.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "super-secret-pw") {
		t.Fatalf("password stored in plaintext: %q", raw)
	}
	if !strings.HasPrefix(raw, "enc:v1:") {
		t.Fatalf("credentials not sealed: %q", raw)
	}
	got, err := store.GetIntegration(in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Credentials["password"] != "super-secret-pw" {
		t.Fatalf("decrypt = %v", got.Credentials)
	}
}

func TestLegacyPlaintextCredentialsMigrate(t *testing.T) {
	store, _, _ := newTestStore(t)
	res, err := store.db.Exec(`INSERT INTO integrations (name, provider, credentials, is_enabled, created_at, updated_at)
		VALUES ('legacy', 'telegram', '{"bot_token":"tok123","default_chat_id":"c"}', 1, '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	// Legacy rows still read through the new decrypt path.
	got, err := store.GetIntegration(id)
	if err != nil {
		t.Fatalf("legacy read: %v", err)
	}
	if got.Credentials["bot_token"] != "tok123" {
		t.Fatalf("legacy decrypt = %v", got.Credentials)
	}
	if n, err := store.MigratePlaintextSecrets(); err != nil || n < 1 {
		t.Fatalf("migrate = %d, %v", n, err)
	}
	var raw string
	if err := store.db.QueryRow(`SELECT credentials FROM integrations WHERE id = ?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "enc:v1:") || strings.Contains(raw, "tok123") {
		t.Fatalf("not sealed after migrate: %q", raw)
	}
}

func TestRoutingOverrideEncryptedAtRest(t *testing.T) {
	store, projectID, _ := newTestStore(t)
	tg := mustTelegram(t, store)
	secretURL := "https://hooks.example.com/secret-token-abc"
	attachIntegration(t, store, projectID, tg, RoutingOverride{URL: secretURL})
	var raw string
	if err := store.db.QueryRow(`SELECT routing_override FROM notification_rule_integrations`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "secret-token-abc") {
		t.Fatalf("override URL stored in plaintext: %q", raw)
	}
	rules, err := store.Rules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) == 0 || len(rules[0].Integrations) == 0 {
		t.Fatal("no rules")
	}
	if got := rules[0].Integrations[0].Override.URL; got != secretURL {
		t.Fatalf("override decrypt = %q", got)
	}
}

func TestWriteWithoutSecretFailsClosed(t *testing.T) {
	db := storeNoSecretDB(t)
	bare := NewStore(db, slog.Default(), "")
	if _, err := bare.CreateIntegration(Integration{
		Name: "x", Provider: "telegram",
		Credentials: map[string]any{"bot_token": "t"},
	}); err == nil {
		t.Fatal("create without server secret must fail")
	}
}

func storeNoSecretDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "nosecret.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
