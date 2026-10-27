// Package notifications decides which issue events deserve an alert and
// durably queues them per-channel in SQLite so a flaky Telegram/Slack/
// Discord/Teams API never blocks or fails event ingestion, and one
// channel's outage never blocks or duplicates another's delivery.
package notifications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"errgotrack/internal/database"
	"errgotrack/internal/telegram"
	"errgotrack/internal/webhooks"
)

type TelegramConfig struct {
	BotToken string
	ChatID   string
	Enabled  bool
}

// WebhookConfig is the shape shared by Slack, Discord, and Teams: a single
// incoming-webhook URL plus an enabled flag.
type WebhookConfig struct {
	URL     string
	Enabled bool
}

var webhookChannelNames = []string{"slack", "discord", "teams", "sheets"}

type Store struct {
	db       *database.DB
	telegram *telegram.Client
	webhook  *webhooks.Client
	logger   *slog.Logger
}

func NewStore(db *database.DB, logger *slog.Logger) *Store {
	return &Store{db: db, telegram: telegram.NewClient(), webhook: webhooks.NewClient(), logger: logger}
}

func (s *Store) GetTelegramConfig() (TelegramConfig, error) {
	var c TelegramConfig
	var enabled int
	err := s.db.QueryRow(`SELECT bot_token, chat_id, enabled FROM telegram_config WHERE id = 1`).Scan(&c.BotToken, &c.ChatID, &enabled)
	c.Enabled = enabled != 0
	return c, err
}

func (s *Store) SetTelegramConfig(c TelegramConfig) error {
	_, err := s.db.Exec(`UPDATE telegram_config SET bot_token = ?, chat_id = ?, enabled = ? WHERE id = 1`,
		c.BotToken, c.ChatID, c.Enabled)
	return err
}

func (s *Store) SendTest(ctx context.Context) error {
	cfg, err := s.GetTelegramConfig()
	if err != nil {
		return err
	}
	return s.telegram.Send(ctx, cfg.BotToken, cfg.ChatID, "<b>Errgotrack</b>\nTest notification - if you can read this, Telegram alerts are working.")
}

// GetWebhookConfig and SetWebhookConfig manage Slack/Discord/Teams, which
// is why channel must be one of webhookChannelNames - anything else is a
// caller bug, not bad user input (the HTTP layer validates the path
// segment before it gets here).
func (s *Store) GetWebhookConfig(channel string) (WebhookConfig, error) {
	var c WebhookConfig
	var enabled int
	err := s.db.QueryRow(`SELECT webhook_url, enabled FROM webhook_channels WHERE channel = ?`, channel).Scan(&c.URL, &enabled)
	c.Enabled = enabled != 0
	return c, err
}

func (s *Store) SetWebhookConfig(channel string, c WebhookConfig) error {
	_, err := s.db.Exec(`UPDATE webhook_channels SET webhook_url = ?, enabled = ? WHERE channel = ?`, c.URL, c.Enabled, channel)
	return err
}

func (s *Store) SendTestWebhook(ctx context.Context, channel string) error {
	cfg, err := s.GetWebhookConfig(channel)
	if err != nil {
		return err
	}
	body := formatForChannel(channel, webhooks.Alert{
		ProjectName: "Errgotrack", ExceptionType: "Test",
		Message: "Test notification - if you can read this, it's working.", IsNew: true, Timestamp: time.Now(),
	})
	return s.webhook.Send(ctx, cfg.URL, body)
}

func formatForChannel(channel string, a webhooks.Alert) []byte {
	switch channel {
	case "slack":
		return webhooks.FormatSlack(a)
	case "discord":
		return webhooks.FormatDiscord(a)
	case "teams":
		return webhooks.FormatTeams(a)
	case "sheets":
		return webhooks.FormatSheets(a)
	default:
		return nil
	}
}

type Rule struct {
	ID              int64
	ProjectID       sql.NullInt64
	Environment     string
	NotifyNewIssue  bool
	NotifyThreshold int64
	NotifyReopened  bool
	CooldownSeconds int
}

func (s *Store) Rules() ([]Rule, error) {
	rows, err := s.db.Query(`SELECT id, project_id, environment, notify_new_issue, notify_threshold, notify_reopened, cooldown_seconds FROM notification_rules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var r Rule
		var newIssue, reopened int
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Environment, &newIssue, &r.NotifyThreshold, &reopened, &r.CooldownSeconds); err != nil {
			return nil, err
		}
		r.NotifyNewIssue, r.NotifyReopened = newIssue != 0, reopened != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) UpsertDefaultRule(projectID int64) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM notification_rules WHERE project_id = ?`, projectID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO notification_rules (project_id, notify_new_issue, notify_reopened, cooldown_seconds) VALUES (?, 1, 1, 300)`, projectID)
	return err
}

// AlertEvent is the evaluation input: what just happened to an issue.
type AlertEvent struct {
	IssueID       int64
	ProjectID     int64
	ProjectName   string
	Environment   string
	ExceptionType string
	Message       string
	EventCount    int64
	IsNew         bool
	Reopened      bool
	Timestamp     time.Time
	IssueURL      string
}

// Evaluate checks configured rules against what just happened to an issue
// and, if warranted, enqueues one alert per enabled channel (Telegram,
// Slack, Discord, Teams - any combination). Never returns an error that
// should interrupt ingestion - failures are logged and swallowed, since a
// missed alert is recoverable but a dropped event is not.
func (s *Store) Evaluate(ctx context.Context, e AlertEvent) {
	rules, err := s.Rules()
	if err != nil {
		s.logger.Error("load notification rules", "error", err)
		return
	}

	shouldNotify := false
	cooldown := 300
	for _, r := range rules {
		if r.ProjectID.Valid && r.ProjectID.Int64 != e.ProjectID {
			continue
		}
		if r.Environment != "" && r.Environment != e.Environment {
			continue
		}
		if e.IsNew && r.NotifyNewIssue {
			shouldNotify = true
		}
		if e.Reopened && r.NotifyReopened {
			shouldNotify = true
		}
		if r.NotifyThreshold > 0 && e.EventCount == r.NotifyThreshold {
			shouldNotify = true
		}
		if shouldNotify {
			cooldown = r.CooldownSeconds
			break
		}
	}
	if !shouldNotify {
		return
	}

	// Cooldown applies once across every channel: if any channel already
	// alerted about this issue recently, don't let a second trigger spam
	// through whichever channels are enabled.
	if s.withinCooldown(e.IssueID, cooldown) {
		return
	}

	for _, channel := range s.enabledChannels() {
		if err := s.enqueue(e, channel); err != nil {
			s.logger.Error("enqueue notification", "error", err, "issue_id", e.IssueID, "channel", channel)
		}
	}
}

func (s *Store) enabledChannels() []string {
	var out []string
	if cfg, err := s.GetTelegramConfig(); err == nil && cfg.Enabled {
		out = append(out, "telegram")
	}
	for _, channel := range webhookChannelNames {
		if cfg, err := s.GetWebhookConfig(channel); err == nil && cfg.Enabled {
			out = append(out, channel)
		}
	}
	return out
}

func (s *Store) withinCooldown(issueID int64, cooldownSeconds int) bool {
	if cooldownSeconds <= 0 {
		return false
	}
	var lastCreated string
	err := s.db.QueryRow(`SELECT created_at FROM notification_queue WHERE issue_id = ? ORDER BY created_at DESC LIMIT 1`, issueID).Scan(&lastCreated)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		return false
	}
	last, err := time.Parse(time.RFC3339Nano, lastCreated)
	if err != nil {
		return false
	}
	return time.Since(last) < time.Duration(cooldownSeconds)*time.Second
}

func (s *Store) enqueue(e AlertEvent, channel string) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO notification_queue (issue_id, kind, channel, payload_json, next_attempt_at) VALUES (?, 'issue_alert', ?, ?, ?)`,
		e.IssueID, channel, string(payload), sortableNow())
	return err
}

// sortableNow formats the current time for any column compared via plain
// SQL string ordering (next_attempt_at). time.RFC3339 (second precision,
// always UTC "Z" suffix here) is fixed-width, so lexicographic and
// chronological ordering always agree. RFC3339Nano is NOT safe for this:
// its fractional part is variable-width (trailing zeros get trimmed), so
// e.g. "...123Z" can sort *after* "...123456789Z" despite being earlier,
// since 'Z' > any digit - which could leave a just-written row invisible
// to a query run moments later. (Columns that get time.Parse'd instead of
// string-compared, like created_at, aren't affected by this and can stay
// on RFC3339Nano.)
func sortableNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// DrainOnce sends up to batchSize pending notifications. Called on a timer
// by the worker (see worker.go), never from the request path.
func (s *Store) DrainOnce(ctx context.Context, batchSize int) {
	rows, err := s.db.Query(`SELECT id, channel, payload_json, attempts FROM notification_queue WHERE status = 'pending' AND next_attempt_at <= ? ORDER BY id LIMIT ?`,
		sortableNow(), batchSize)
	if err != nil {
		s.logger.Error("query notification queue", "error", err)
		return
	}
	type job struct {
		id       int64
		channel  string
		payload  string
		attempts int
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.channel, &j.payload, &j.attempts); err != nil {
			continue
		}
		jobs = append(jobs, j)
	}
	rows.Close()

	for _, j := range jobs {
		var e AlertEvent
		if err := json.Unmarshal([]byte(j.payload), &e); err != nil {
			s.markFailed(j.id, "invalid payload: "+err.Error())
			continue
		}
		skip, err := s.send(ctx, j.channel, e)
		switch {
		case skip:
			s.markSkipped(j.id)
		case err != nil:
			s.retry(j.id, j.attempts+1, err)
		default:
			s.markSent(j.id)
		}
	}
}

// send delivers one alert to one channel. skip=true means the channel was
// disabled between enqueue and drain - not a failure worth retrying, just
// nothing to do.
func (s *Store) send(ctx context.Context, channel string, e AlertEvent) (skip bool, err error) {
	switch channel {
	case "telegram":
		cfg, err := s.GetTelegramConfig()
		if err != nil {
			return false, err
		}
		if !cfg.Enabled {
			return true, nil
		}
		text := telegram.Format(telegram.Alert{
			ProjectName: e.ProjectName, Environment: e.Environment, ExceptionType: e.ExceptionType, Message: e.Message,
			IsNew: e.IsNew, Reopened: e.Reopened, EventCount: e.EventCount, Timestamp: e.Timestamp, IssueURL: e.IssueURL,
		})
		return false, s.telegram.Send(ctx, cfg.BotToken, cfg.ChatID, text)

	case "slack", "discord", "teams", "sheets":
		cfg, err := s.GetWebhookConfig(channel)
		if err != nil {
			return false, err
		}
		if !cfg.Enabled {
			return true, nil
		}
		body := formatForChannel(channel, webhooks.Alert{
			ProjectName: e.ProjectName, Environment: e.Environment, ExceptionType: e.ExceptionType, Message: e.Message,
			IsNew: e.IsNew, Reopened: e.Reopened, EventCount: e.EventCount, Timestamp: e.Timestamp, IssueURL: e.IssueURL,
		})
		return false, s.webhook.Send(ctx, cfg.URL, body)

	default:
		return false, fmt.Errorf("unknown notification channel %q", channel)
	}
}

const maxAttempts = 8

func (s *Store) retry(id int64, attempts int, sendErr error) {
	if attempts >= maxAttempts {
		s.markFailed(id, sendErr.Error())
		return
	}
	backoff := time.Duration(math.Min(float64(attempts*attempts)*5, 600)) * time.Second
	next := time.Now().Add(backoff).UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`UPDATE notification_queue SET attempts = ?, next_attempt_at = ?, last_error = ? WHERE id = ?`,
		attempts, next, sendErr.Error(), id)
	if err != nil {
		s.logger.Error("update retry", "error", err)
	}
}

func (s *Store) markSent(id int64) {
	s.db.Exec(`UPDATE notification_queue SET status = 'sent' WHERE id = ?`, id)
}

func (s *Store) markSkipped(id int64) {
	s.db.Exec(`UPDATE notification_queue SET status = 'skipped' WHERE id = ?`, id)
}

func (s *Store) markFailed(id int64, reason string) {
	s.db.Exec(`UPDATE notification_queue SET status = 'failed', last_error = ? WHERE id = ?`, reason, id)
}
