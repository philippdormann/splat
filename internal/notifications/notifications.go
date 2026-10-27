// Package notifications decides which issue events deserve an alert and
// durably queues them per-channel in SQLite so a flaky Telegram/Slack/
// Discord/Teams API never blocks or fails event ingestion, and one
// channel's outage never blocks or duplicates another's delivery.
package notifications

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"errgotrack/internal/database"
	"errgotrack/internal/email"
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

// allChannelNames is every deliverable destination, used to validate
// per-rule channel selections.
var allChannelNames = []string{"telegram", "slack", "discord", "teams", "sheets", "email", "generic"}

// ValidChannel reports whether c names a deliverable destination.
func ValidChannel(c string) bool {
	for _, n := range allChannelNames {
		if n == c {
			return true
		}
	}
	return false
}

// SMTPConfig is the instance-level mail setup. The To address is the
// on-call recipient - alert rules pick channels, not people.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       string
	UseTLS   bool
	Enabled  bool
}

func (s *Store) GetSMTPConfig() (SMTPConfig, error) {
	var c SMTPConfig
	var useTLS, enabled int
	err := s.db.QueryRow(`SELECT host, port, username, password, from_address, to_address, use_tls, enabled FROM smtp_config WHERE id = 1`).
		Scan(&c.Host, &c.Port, &c.Username, &c.Password, &c.From, &c.To, &useTLS, &enabled)
	c.UseTLS, c.Enabled = useTLS != 0, enabled != 0
	return c, err
}

func (s *Store) SetSMTPConfig(c SMTPConfig) error {
	_, err := s.db.Exec(`UPDATE smtp_config SET host = ?, port = ?, username = ?, password = ?, from_address = ?, to_address = ?, use_tls = ?, enabled = ? WHERE id = 1`,
		c.Host, c.Port, c.Username, c.Password, c.From, c.To, c.UseTLS, c.Enabled)
	return err
}

func (s *Store) SendTestEmail(ctx context.Context) error {
	cfg, err := s.GetSMTPConfig()
	if err != nil {
		return err
	}
	return email.Send(email.Config{
		Host: cfg.Host, Port: cfg.Port, Username: cfg.Username,
		Password: cfg.Password, From: cfg.From, UseTLS: cfg.UseTLS,
	}, cfg.To, email.Alert{
		ProjectName: "Errgotrack", ExceptionType: "Test",
		Message: "Test notification - if you can read this, email alerts are working.",
		IsNew:   true, Timestamp: time.Now(),
	})
}

// GenericWebhook is a user-defined HTTP endpoint receiving the standard
// JSON alert payload. Secret signs the body (HMAC-SHA256, sent as
// X-Errgotrack-Signature); Headers are extra request headers merged in,
// one per line as "Name: value" in storage, parsed on send.
type GenericWebhook struct {
	ID      int64
	Name    string
	URL     string
	Secret  string
	Headers map[string]string
	Enabled bool
}

// SignatureHeader is the HMAC header name receivers should verify.
const SignatureHeader = "X-Errgotrack-Signature"

func (s *Store) GenericWebhooks() ([]GenericWebhook, error) {
	rows, err := s.db.Query(`SELECT id, name, url, secret, headers, enabled FROM generic_webhooks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GenericWebhook
	for rows.Next() {
		var w GenericWebhook
		var headersJSON string
		var enabled int
		if err := rows.Scan(&w.ID, &w.Name, &w.URL, &w.Secret, &headersJSON, &enabled); err != nil {
			return nil, err
		}
		w.Enabled = enabled != 0
		w.Headers = parseHeaders(headersJSON)
		out = append(out, w)
	}
	return out, rows.Err()
}

// parseHeaders turns stored "Name: value" lines into a map, skipping
// blank lines and malformed rows (header values may contain colons; only
// the first colon splits).
func parseHeaders(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if name == "" || value == "" {
			continue
		}
		out[name] = value
	}
	return out
}

func encodeHeaders(headers map[string]string) string {
	names := make([]string, 0, len(headers))
	for k, v := range headers {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, k := range names {
		b.WriteString(strings.TrimSpace(k))
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(headers[k]))
		b.WriteString("\n")
	}
	return b.String()
}

func (s *Store) CreateGenericWebhook(w GenericWebhook) (GenericWebhook, error) {
	if strings.TrimSpace(w.URL) == "" {
		return GenericWebhook{}, fmt.Errorf("url is required")
	}
	headers := encodeHeaders(w.Headers)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if s.db.IsPostgres() {
		var id int64
		err := s.db.QueryRow(`INSERT INTO generic_webhooks (name, url, secret, headers, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
			w.Name, w.URL, w.Secret, headers, w.Enabled, now).Scan(&id)
		if err != nil {
			return GenericWebhook{}, err
		}
		w.ID = id
		return w, nil
	}
	res, err := s.db.Exec(`INSERT INTO generic_webhooks (name, url, secret, headers, enabled, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		w.Name, w.URL, w.Secret, headers, w.Enabled, now)
	if err != nil {
		return GenericWebhook{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return GenericWebhook{}, err
	}
	w.ID = id
	w.Headers = parseHeaders(headers)
	return w, nil
}

func (s *Store) UpdateGenericWebhook(w GenericWebhook) (GenericWebhook, error) {
	if strings.TrimSpace(w.URL) == "" {
		return GenericWebhook{}, fmt.Errorf("url is required")
	}
	headers := encodeHeaders(w.Headers)
	_, err := s.db.Exec(`UPDATE generic_webhooks SET name = ?, url = ?, secret = ?, headers = ?, enabled = ? WHERE id = ?`,
		w.Name, w.URL, w.Secret, headers, w.Enabled, w.ID)
	if err != nil {
		return GenericWebhook{}, err
	}
	w.Headers = parseHeaders(headers)
	return w, nil
}

func (s *Store) DeleteGenericWebhook(id int64) error {
	_, err := s.db.Exec(`DELETE FROM generic_webhooks WHERE id = ?`, id)
	return err
}

func (s *Store) SendTestGeneric(ctx context.Context, id int64) error {
	hooks, err := s.GenericWebhooks()
	if err != nil {
		return err
	}
	for _, h := range hooks {
		if h.ID != id {
			continue
		}
		return s.sendGeneric(ctx, h, webhooks.Alert{
			ProjectName: "Errgotrack", ExceptionType: "Test",
			Message: "Test notification - if you can read this, it's working.",
			IsNew:   true, Timestamp: time.Now(),
		})
	}
	return fmt.Errorf("generic webhook %d not found", id)
}

// genericPayload is the JSON body every generic webhook receives.
func genericPayload(a webhooks.Alert) []byte {
	b, _ := json.Marshal(map[string]any{
		"project":        a.ProjectName,
		"environment":    a.Environment,
		"kind":           kindOf(a),
		"exception_type": a.ExceptionType,
		"message":        a.Message,
		"event_count":    a.EventCount,
		"issue_url":      a.IssueURL,
		"timestamp":      a.Timestamp.UTC().Format(time.RFC3339),
	})
	return b
}

func kindOf(a webhooks.Alert) string {
	switch {
	case a.IsNew:
		return "new_issue"
	case a.Reopened:
		return "reopened"
	default:
		return "recurring"
	}
}

// signBody returns "sha256=<hex HMAC-SHA256(body, secret)>" - empty secret
// means unsigned.
func signBody(secret string, body []byte) string {
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (s *Store) sendGeneric(ctx context.Context, h GenericWebhook, a webhooks.Alert) error {
	if h.URL == "" {
		return fmt.Errorf("generic webhook %q has no URL", h.Name)
	}
	// No ValidateURL here: the URL was validated on save (same as the
	// fixed channels, which also only validate at save time).
	body := genericPayload(a)
	headers := map[string]string{}
	for k, v := range h.Headers {
		headers[k] = v
	}
	if sig := signBody(h.Secret, body); sig != "" {
		headers[SignatureHeader] = sig
	}
	return s.webhook.SendWithHeaders(ctx, h.URL, headers, body)
}

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
	Name            string
	ProjectID       sql.NullInt64
	Environment     string
	NotifyNewIssue  bool
	NotifyThreshold int64
	NotifyReopened  bool
	CooldownSeconds int
	// Channels selects this rule's destinations. Empty means "every enabled
	// channel" (the pre-rules-upgrade behavior), so old rules keep working
	// unchanged while new rules can route e.g. production to email and
	// staging to a chat channel.
	Channels []string
}

func scanRule(row interface {
	Scan(...any) error
}) (Rule, error) {
	var r Rule
	var newIssue, reopened int
	var channels string
	if err := row.Scan(&r.ID, &r.Name, &r.ProjectID, &r.Environment, &newIssue, &r.NotifyThreshold, &reopened, &r.CooldownSeconds, &channels); err != nil {
		return r, err
	}
	r.NotifyNewIssue, r.NotifyReopened = newIssue != 0, reopened != 0
	if channels != "" {
		_ = json.Unmarshal([]byte(channels), &r.Channels)
	}
	return r, nil
}

const ruleCols = `id, name, project_id, environment, notify_new_issue, notify_threshold, notify_reopened, cooldown_seconds, channels`

func (s *Store) Rules() ([]Rule, error) {
	rows, err := s.db.Query(`SELECT ` + ruleCols + ` FROM notification_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetRule(id int64) (Rule, error) {
	row := s.db.QueryRow(`SELECT `+ruleCols+` FROM notification_rules WHERE id = ?`, id)
	r, err := scanRule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return r, sql.ErrNoRows
	}
	return r, err
}

func encodeChannels(channels []string) (string, error) {
	if len(channels) == 0 {
		return "[]", nil
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(channels))
	for _, c := range channels {
		if !ValidChannel(c) {
			return "", fmt.Errorf("unknown channel %q", c)
		}
		if !seen[c] {
			seen[c] = true
			clean = append(clean, c)
		}
	}
	b, err := json.Marshal(clean)
	return string(b), err
}

// CreateRule validates and stores a new alert rule.
func (s *Store) CreateRule(r Rule) (Rule, error) {
	ch, err := encodeChannels(r.Channels)
	if err != nil {
		return Rule{}, err
	}
	if r.CooldownSeconds < 0 {
		r.CooldownSeconds = 0
	}
	if s.db.IsPostgres() {
		var id int64
		err := s.db.QueryRow(`
			INSERT INTO notification_rules (name, project_id, environment, notify_new_issue, notify_threshold, notify_reopened, cooldown_seconds, channels)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			r.Name, nullableProjectID(r.ProjectID), r.Environment, r.NotifyNewIssue, r.NotifyThreshold, r.NotifyReopened, r.CooldownSeconds, ch).Scan(&id)
		if err != nil {
			return Rule{}, err
		}
		return s.GetRule(id)
	}
	res, err := s.db.Exec(`
		INSERT INTO notification_rules (name, project_id, environment, notify_new_issue, notify_threshold, notify_reopened, cooldown_seconds, channels)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Name, nullableProjectID(r.ProjectID), r.Environment, r.NotifyNewIssue, r.NotifyThreshold, r.NotifyReopened, r.CooldownSeconds, ch)
	if err != nil {
		return Rule{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Rule{}, err
	}
	return s.GetRule(id)
}

// UpdateRule replaces a rule's editable fields.
func (s *Store) UpdateRule(r Rule) (Rule, error) {
	ch, err := encodeChannels(r.Channels)
	if err != nil {
		return Rule{}, err
	}
	if r.CooldownSeconds < 0 {
		r.CooldownSeconds = 0
	}
	_, err = s.db.Exec(`
		UPDATE notification_rules SET name = ?, project_id = ?, environment = ?, notify_new_issue = ?,
			notify_threshold = ?, notify_reopened = ?, cooldown_seconds = ?, channels = ? WHERE id = ?`,
		r.Name, nullableProjectID(r.ProjectID), r.Environment, r.NotifyNewIssue, r.NotifyThreshold, r.NotifyReopened, r.CooldownSeconds, ch, r.ID)
	if err != nil {
		return Rule{}, err
	}
	return s.GetRule(r.ID)
}

func (s *Store) DeleteRule(id int64) error {
	_, err := s.db.Exec(`DELETE FROM notification_rules WHERE id = ?`, id)
	return err
}

func nullableProjectID(n sql.NullInt64) any {
	if n.Valid {
		return n.Int64
	}
	return nil
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
	// Status is the issue's status after this event was grouped. Muted and
	// ignored issues never notify - not on new occurrences, not on
	// thresholds: silenced means silenced.
	Status    string
	Timestamp time.Time
	IssueURL  string
}

// Evaluate checks configured rules against what just happened to an issue
// and, if warranted, enqueues one alert per enabled channel (Telegram,
// Slack, Discord, Teams - any combination). Never returns an error that
// should interrupt ingestion - failures are logged and swallowed, since a
// missed alert is recoverable but a dropped event is not.
func (s *Store) Evaluate(ctx context.Context, e AlertEvent) {
	if e.Status == "muted" || e.Status == "ignored" {
		return
	}
	rules, err := s.Rules()
	if err != nil {
		s.logger.Error("load notification rules", "error", err)
		return
	}

	shouldNotify := false
	cooldown := 300
	cooldownSet := false
	wantChannels := map[string]bool{}
	for _, r := range rules {
		if r.ProjectID.Valid && r.ProjectID.Int64 != e.ProjectID {
			continue
		}
		if r.Environment != "" && r.Environment != e.Environment {
			continue
		}
		matched := false
		if e.IsNew && r.NotifyNewIssue {
			matched = true
		}
		if e.Reopened && r.NotifyReopened {
			matched = true
		}
		if r.NotifyThreshold > 0 && e.EventCount == r.NotifyThreshold {
			matched = true
		}
		if !matched {
			continue
		}
		shouldNotify = true
		if !cooldownSet {
			cooldown = r.CooldownSeconds
			cooldownSet = true
		}
		if len(r.Channels) == 0 {
			// No explicit selection: every enabled channel.
			for _, c := range s.enabledChannels() {
				wantChannels[c] = true
			}
		} else {
			for _, c := range r.Channels {
				wantChannels[c] = true
			}
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

	for channel := range wantChannels {
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
	if cfg, err := s.GetSMTPConfig(); err == nil && cfg.Enabled {
		out = append(out, "email")
	}
	if hooks, err := s.GenericWebhooks(); err == nil {
		for _, h := range hooks {
			if h.Enabled {
				out = append(out, "generic")
				break
			}
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

	case "email":
		cfg, err := s.GetSMTPConfig()
		if err != nil {
			return false, err
		}
		if !cfg.Enabled {
			return true, nil
		}
		return false, email.Send(email.Config{
			Host: cfg.Host, Port: cfg.Port, Username: cfg.Username,
			Password: cfg.Password, From: cfg.From, UseTLS: cfg.UseTLS,
		}, cfg.To, email.Alert{
			ProjectName: e.ProjectName, Environment: e.Environment, ExceptionType: e.ExceptionType, Message: e.Message,
			IsNew: e.IsNew, Reopened: e.Reopened, EventCount: e.EventCount, Timestamp: e.Timestamp, IssueURL: e.IssueURL,
		})

	case "generic":
		hooks, err := s.GenericWebhooks()
		if err != nil {
			return false, err
		}
		var anyEnabled bool
		for _, h := range hooks {
			if !h.Enabled {
				continue
			}
			anyEnabled = true
			if err := s.sendGeneric(ctx, h, webhooks.Alert{
				ProjectName: e.ProjectName, Environment: e.Environment, ExceptionType: e.ExceptionType, Message: e.Message,
				IsNew: e.IsNew, Reopened: e.Reopened, EventCount: e.EventCount, Timestamp: e.Timestamp, IssueURL: e.IssueURL,
			}); err != nil {
				return false, err
			}
		}
		if !anyEnabled {
			return true, nil
		}
		return false, nil

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
