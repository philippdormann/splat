// Package notifications decides which issue events deserve an alert and
// durably queues them per-integration in SQLite so a flaky Telegram/Slack/
// Discord/Teams API never blocks or fails event ingestion.
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
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/philippdormann/splat/internal/database"
	"github.com/philippdormann/splat/internal/email"
	"github.com/philippdormann/splat/internal/secretbox"
	"github.com/philippdormann/splat/internal/telegram"
	"github.com/philippdormann/splat/internal/webhooks"
)

// Providers.
var allProviders = []string{"telegram", "slack", "discord", "teams", "sheets", "smtp", "generic"}

// ValidProvider reports whether p names a supported integration provider.
func ValidProvider(p string) bool {
	for _, n := range allProviders {
		if n == p {
			return true
		}
	}
	return false
}

// ValidChannel is kept for backwards compatibility; providers replaced channels.
func ValidChannel(c string) bool { return ValidProvider(c) }

// Integration is a named set of credentials for one provider.
// Credentials hold defaults only; per-rule routing lives in RoutingOverride.
type Integration struct {
	ID           int64
	Name         string
	Provider     string
	Credentials  map[string]any
	IsEnabled    bool
	FailureCount int
	LastFailure  string
}

type TelegramCreds struct {
	BotToken      string `json:"bot_token"`
	DefaultChatID string `json:"default_chat_id"`
}

type WebhookCreds struct {
	URL string `json:"url"`
}

type SMTPCreds struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	UseTLS   bool   `json:"use_tls"`
}

type GenericCreds struct {
	URL     string            `json:"url"`
	Secret  string            `json:"secret"`
	Headers map[string]string `json:"headers"`
}

// RoutingOverride is stored per rule->integration link. Only the fields
// relevant to the integration's provider are used.
type RoutingOverride struct {
	ChatID       string            `json:"chat_id,omitempty"`
	URL          string            `json:"url,omitempty"`
	To           []string          `json:"to,omitempty"`
	ExtraHeaders map[string]string `json:"extra_headers,omitempty"`
}

func (s *Store) encodeOverride(o RoutingOverride) (string, error) {
	b, _ := json.Marshal(o)
	if string(b) == "null" {
		b = []byte("{}")
	}
	// Empty overrides stay plaintext "{}" so rule/queue rows without
	// secrets don't churn; anything else is sealed at rest.
	if string(b) == "{}" {
		return "{}", nil
	}
	return secretbox.Encrypt(s.secret, string(b))
}

func (s *Store) decodeOverride(raw string) (RoutingOverride, error) {
	var o RoutingOverride
	if raw == "" || raw == "{}" {
		o.ExtraHeaders = map[string]string{}
		return o, nil
	}
	plain, err := secretbox.Decrypt(s.secret, raw)
	if err != nil {
		return o, err
	}
	_ = json.Unmarshal([]byte(plain), &o)
	if o.ExtraHeaders == nil {
		o.ExtraHeaders = map[string]string{}
	}
	return o, nil
}

func (s *Store) encodeCreds(m map[string]any) (string, error) {
	if m == nil {
		m = map[string]any{}
	}
	b, _ := json.Marshal(m)
	if string(b) == "null" || string(b) == "{}" {
		return "{}", nil
	}
	return secretbox.Encrypt(s.secret, string(b))
}

func (s *Store) decodeCreds(raw string) (map[string]any, error) {
	out := map[string]any{}
	if raw == "" || raw == "{}" {
		return out, nil
	}
	plain, err := secretbox.Decrypt(s.secret, raw)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(plain), &out)
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func credsString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func isEmail(s string) bool {
	s = strings.TrimSpace(s)
	at := strings.Index(s, "@")
	return at > 0 && strings.Contains(s[at:], ".")
}

// ValidateCredentials checks provider-specific credential shapes.
// URL checks are scheme-only here (no DNS); the HTTP layer applies SSRF
// validation on save.
func ValidateCredentials(provider string, creds map[string]any) error {
	if !ValidProvider(provider) {
		return fmt.Errorf("unknown provider %q", provider)
	}
	switch provider {
	case "telegram":
		if credsString(creds, "bot_token") == "" {
			return fmt.Errorf("bot_token is required")
		}
	case "slack", "discord", "teams", "sheets":
		u := credsString(creds, "url")
		if u == "" {
			return fmt.Errorf("url is required")
		}
		if !isHTTPURL(u) {
			return fmt.Errorf("url must use http or https")
		}
	case "smtp":
		if credsString(creds, "host") == "" {
			return fmt.Errorf("host is required")
		}
		if credsString(creds, "from") == "" {
			return fmt.Errorf("from address is required")
		}
	case "generic":
		u := credsString(creds, "url")
		if u == "" {
			return fmt.Errorf("url is required")
		}
		if !isHTTPURL(u) {
			return fmt.Errorf("url must use http or https")
		}
	}
	return nil
}

// ValidateRoutingOverride checks the per-link override against the provider.
func ValidateRoutingOverride(provider string, creds map[string]any, o RoutingOverride) error {
	if !ValidProvider(provider) {
		return fmt.Errorf("unknown provider %q", provider)
	}
	switch provider {
	case "telegram":
		chat := strings.TrimSpace(o.ChatID)
		if chat == "" {
			chat = credsString(creds, "default_chat_id")
		}
		if chat == "" {
			return fmt.Errorf("chat_id is required (no default configured)")
		}
	case "slack", "discord", "teams", "sheets":
		u := strings.TrimSpace(o.URL)
		if u == "" {
			u = credsString(creds, "url")
		}
		if u == "" {
			return fmt.Errorf("url is required (no default configured)")
		}
		if !isHTTPURL(u) {
			return fmt.Errorf("url must use http or https")
		}
	case "smtp":
		if len(o.To) == 0 {
			return fmt.Errorf("at least one recipient (to) is required")
		}
		for _, t := range o.To {
			if !isEmail(t) {
				return fmt.Errorf("invalid recipient email %q", t)
			}
		}
	case "generic":
		u := strings.TrimSpace(o.URL)
		if u == "" {
			u = credsString(creds, "url")
		}
		if u == "" {
			return fmt.Errorf("url is required (no default configured)")
		}
		if !isHTTPURL(u) {
			return fmt.Errorf("url must use http or https")
		}
	}
	return nil
}

// SignatureHeader is the HMAC header name generic receivers should verify.
const SignatureHeader = "X-Splat-Signature"

// parseHeaders turns stored "Name: value" lines into a map.
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

func signBody(secret string, body []byte) string {
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

type Store struct {
	db       *database.DB
	telegram *telegram.Client
	webhook  *webhooks.Client
	logger   *slog.Logger
	// secret seals credentials and routing overrides at rest
	// (SPLAT_SESSION_SECRET). Empty fails writes closed; reads of
	// legacy plaintext rows still work so existing DBs migrate cleanly.
	secret string
}

func NewStore(db *database.DB, logger *slog.Logger, sessionSecret string) *Store {
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{db: db, telegram: telegram.NewClient(), webhook: webhooks.NewClient(), logger: logger, secret: sessionSecret}
}

// --- integrations ---

func (s *Store) scanIntegration(row interface {
	Scan(...any) error
}) (Integration, error) {
	var in Integration
	var creds string
	var enabled int
	if err := row.Scan(&in.ID, &in.Name, &in.Provider, &creds, &enabled, &in.FailureCount, &in.LastFailure); err != nil {
		return in, err
	}
	in.IsEnabled = enabled != 0
	credsMap, err := s.decodeCreds(creds)
	if err != nil {
		return in, err
	}
	in.Credentials = credsMap
	return in, nil
}

func (s *Store) Integrations() ([]Integration, error) {
	rows, err := s.db.Query(`SELECT id, name, provider, credentials, is_enabled, failure_count, last_failure FROM integrations ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Integration{}
	for rows.Next() {
		in, err := s.scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func (s *Store) GetIntegration(id int64) (Integration, error) {
	row := s.db.QueryRow(`SELECT id, name, provider, credentials, is_enabled, failure_count, last_failure FROM integrations WHERE id = ?`, id)
	in, err := s.scanIntegration(row)
	if errors.Is(err, sql.ErrNoRows) {
		return in, sql.ErrNoRows
	}
	return in, err
}

func (s *Store) CreateIntegration(in Integration) (Integration, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return Integration{}, fmt.Errorf("name is required")
	}
	if !ValidProvider(in.Provider) {
		return Integration{}, fmt.Errorf("unknown provider %q", in.Provider)
	}
	if in.Credentials == nil {
		in.Credentials = map[string]any{}
	}
	if err := ValidateCredentials(in.Provider, in.Credentials); err != nil {
		return Integration{}, err
	}
	creds, err := s.encodeCreds(in.Credentials)
	if err != nil {
		return Integration{}, fmt.Errorf("encrypt credentials: %w", err)
	}
	enabled := 0
	if in.IsEnabled {
		enabled = 1
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if s.db.IsPostgres() {
		var id int64
		err := s.db.QueryRow(`INSERT INTO integrations (name, provider, credentials, is_enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
			in.Name, in.Provider, creds, enabled, now, now).Scan(&id)
		if err != nil {
			return Integration{}, err
		}
		return s.GetIntegration(id)
	}
	res, err := s.db.Exec(`INSERT INTO integrations (name, provider, credentials, is_enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		in.Name, in.Provider, creds, enabled, now, now)
	if err != nil {
		return Integration{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Integration{}, err
	}
	return s.GetIntegration(id)
}

func (s *Store) UpdateIntegration(in Integration) (Integration, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return Integration{}, fmt.Errorf("name is required")
	}
	if !ValidProvider(in.Provider) {
		return Integration{}, fmt.Errorf("unknown provider %q", in.Provider)
	}
	if in.Credentials == nil {
		in.Credentials = map[string]any{}
	}
	if err := ValidateCredentials(in.Provider, in.Credentials); err != nil {
		return Integration{}, err
	}
	creds, err := s.encodeCreds(in.Credentials)
	if err != nil {
		return Integration{}, fmt.Errorf("encrypt credentials: %w", err)
	}
	enabled := 0
	if in.IsEnabled {
		enabled = 1
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(`UPDATE integrations SET name = ?, provider = ?, credentials = ?, is_enabled = ?, updated_at = ? WHERE id = ?`,
		in.Name, in.Provider, creds, enabled, now, in.ID)
	if err != nil {
		return Integration{}, err
	}
	return s.GetIntegration(in.ID)
}

func (s *Store) DeleteIntegration(id int64) error {
	_, err := s.db.Exec(`DELETE FROM integrations WHERE id = ?`, id)
	return err
}

func (s *Store) bumpFailure(id int64, msg string) {
	_, _ = s.db.Exec(`UPDATE integrations SET failure_count = failure_count + 1, last_failure = ? WHERE id = ?`, msg, id)
}

func (s *Store) clearFailure(id int64) {
	_, _ = s.db.Exec(`UPDATE integrations SET failure_count = 0, last_failure = '' WHERE id = ?`, id)
}

// MigratePlaintextSecrets re-encrypts legacy plaintext rows (written
// before at-rest encryption) with the current server secret. Encrypted,
// empty, and "{}" rows are left alone. Safe to run on every startup;
// returns the number of rows sealed.
func (s *Store) MigratePlaintextSecrets() (int, error) {
	if s.secret == "" {
		return 0, fmt.Errorf("cannot migrate secrets: server secret is not configured")
	}
	sealed := 0
	// 1) integrations.credentials
	rows, err := s.db.Query(`SELECT id, credentials FROM integrations`)
	if err != nil {
		return sealed, err
	}
	type pending struct{ id int64; creds string }
	var work []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.creds); err != nil {
			continue
		}
		work = append(work, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return sealed, err
	}
	for _, p := range work {
		if p.creds == "" || p.creds == "{}" || secretbox.IsEncrypted(p.creds) {
			continue
		}
		enc, err := secretbox.Encrypt(s.secret, p.creds)
		if err != nil {
			return sealed, err
		}
		if _, err := s.db.Exec(`UPDATE integrations SET credentials = ? WHERE id = ?`, enc, p.id); err != nil {
			return sealed, err
		}
		sealed++
	}
	// 2) routing overrides in rule links and the durable queue.
	// Only non-empty, non-"{}" plaintext rows are sealed; "{}" carries
	// no secret and stays plaintext by design.
	rules, err := s.db.Query(`SELECT rule_id, integration_id, routing_override FROM notification_rule_integrations`)
	if err != nil {
		return sealed, err
	}
	type ruleRow struct {
		ruleID, integrationID int64
		raw                  string
	}
	var ruleRows []ruleRow
	for rules.Next() {
		var rr ruleRow
		if err := rules.Scan(&rr.ruleID, &rr.integrationID, &rr.raw); err != nil {
			continue
		}
		ruleRows = append(ruleRows, rr)
	}
	rules.Close()
	if err := rules.Err(); err != nil {
		return sealed, err
	}
	for _, rr := range ruleRows {
		if rr.raw == "" || rr.raw == "{}" || secretbox.IsEncrypted(rr.raw) {
			continue
		}
		enc, err := secretbox.Encrypt(s.secret, rr.raw)
		if err != nil {
			return sealed, err
		}
		if _, err := s.db.Exec(`UPDATE notification_rule_integrations SET routing_override = ? WHERE rule_id = ? AND integration_id = ?`,
			enc, rr.ruleID, rr.integrationID); err != nil {
			return sealed, err
		}
		sealed++
	}
	queue, err := s.db.Query(`SELECT id, routing_override FROM notification_queue`)
	if err != nil {
		return sealed, err
	}
	type queueRow struct {
		id  int64
		raw string
	}
	var queueRows []queueRow
	for queue.Next() {
		var qr queueRow
		if err := queue.Scan(&qr.id, &qr.raw); err != nil {
			continue
		}
		queueRows = append(queueRows, qr)
	}
	queue.Close()
	if err := queue.Err(); err != nil {
		return sealed, err
	}
	for _, qr := range queueRows {
		if qr.raw == "" || qr.raw == "{}" || secretbox.IsEncrypted(qr.raw) {
			continue
		}
		enc, err := secretbox.Encrypt(s.secret, qr.raw)
		if err != nil {
			return sealed, err
		}
		if _, err := s.db.Exec(`UPDATE notification_queue SET routing_override = ? WHERE id = ?`, enc, qr.id); err != nil {
			return sealed, err
		}
		sealed++
	}
	return sealed, nil
}

// SendTestIntegration delivers a canned alert via one integration.
func (s *Store) SendTestIntegration(ctx context.Context, id int64, override RoutingOverride) error {
	in, err := s.GetIntegration(id)
	if err != nil {
		return err
	}
	if err := ValidateRoutingOverride(in.Provider, in.Credentials, override); err != nil {
		return err
	}
	e := AlertEvent{
		ProjectName: "Splat", ExceptionType: "Test",
		Message: "Test notification - if you can read this, it's working.",
		IsNew: true, Timestamp: time.Now(),
	}
	_, err = s.deliver(ctx, in, override, e)
	return err
}

// --- rules ---

type RuleIntegration struct {
	IntegrationID   int64           `json:"integrationId"`
	Provider        string          `json:"provider"`
	IntegrationName string          `json:"integrationName"`
	IsEnabled       bool            `json:"isEnabled"`
	Override        RoutingOverride `json:"routingOverride"`
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
	Integrations    []RuleIntegration
}

func (s *Store) loadRuleLinks(ruleID int64) ([]RuleIntegration, error) {
	rows, err := s.db.Query(`SELECT ri.integration_id, i.provider, i.name, i.is_enabled, ri.routing_override
		FROM notification_rule_integrations ri JOIN integrations i ON i.id = ri.integration_id WHERE ri.rule_id = ? ORDER BY i.id`, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RuleIntegration{}
	for rows.Next() {
		var ri RuleIntegration
		var override string
		var enabled int
		if err := rows.Scan(&ri.IntegrationID, &ri.Provider, &ri.IntegrationName, &enabled, &override); err != nil {
			return nil, err
		}
		ri.IsEnabled = enabled != 0
		ov, err := s.decodeOverride(override)
		if err != nil {
			return nil, err
		}
		ri.Override = ov
		out = append(out, ri)
	}
	return out, rows.Err()
}

func (s *Store) Rules() ([]Rule, error) {
	rows, err := s.db.Query(`SELECT id, name, project_id, environment, notify_new_issue, notify_threshold, notify_reopened, cooldown_seconds FROM notification_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		var r Rule
		var newIssue, reopened int
		if err := rows.Scan(&r.ID, &r.Name, &r.ProjectID, &r.Environment, &newIssue, &r.NotifyThreshold, &reopened, &r.CooldownSeconds); err != nil {
			return nil, err
		}
		r.NotifyNewIssue, r.NotifyReopened = newIssue != 0, reopened != 0
		links, err := s.loadRuleLinks(r.ID)
		if err != nil {
			return nil, err
		}
		r.Integrations = links
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetRule(id int64) (Rule, error) {
	var r Rule
	var newIssue, reopened int
	err := s.db.QueryRow(`SELECT id, name, project_id, environment, notify_new_issue, notify_threshold, notify_reopened, cooldown_seconds FROM notification_rules WHERE id = ?`, id).
		Scan(&r.ID, &r.Name, &r.ProjectID, &r.Environment, &newIssue, &r.NotifyThreshold, &reopened, &r.CooldownSeconds)
	if err != nil {
		return r, err
	}
	r.NotifyNewIssue, r.NotifyReopened = newIssue != 0, reopened != 0
	links, err := s.loadRuleLinks(r.ID)
	if err != nil {
		return r, err
	}
	r.Integrations = links
	return r, nil
}

func (s *Store) validateRuleLinks(links []RuleIntegration) error {
	seen := map[int64]bool{}
	for _, l := range links {
		if seen[l.IntegrationID] {
			return fmt.Errorf("duplicate integration %d", l.IntegrationID)
		}
		seen[l.IntegrationID] = true
		in, err := s.GetIntegration(l.IntegrationID)
		if err != nil {
			return fmt.Errorf("integration %d not found", l.IntegrationID)
		}
		if err := ValidateRoutingOverride(in.Provider, in.Credentials, l.Override); err != nil {
			return fmt.Errorf("integration %q: %w", in.Name, err)
		}
	}
	return nil
}

func (s *Store) saveRuleLinks(ruleID int64, links []RuleIntegration) error {
	if _, err := s.db.Exec(`DELETE FROM notification_rule_integrations WHERE rule_id = ?`, ruleID); err != nil {
		return err
	}
	for _, l := range links {
		enc, err := s.encodeOverride(l.Override)
		if err != nil {
			return fmt.Errorf("encrypt routing override: %w", err)
		}
		if _, err := s.db.Exec(`INSERT INTO notification_rule_integrations (rule_id, integration_id, routing_override) VALUES (?, ?, ?)`,
			ruleID, l.IntegrationID, enc); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateRule(r Rule) (Rule, error) {
	if r.CooldownSeconds < 0 {
		r.CooldownSeconds = 0
	}
	if err := s.validateRuleLinks(r.Integrations); err != nil {
		return Rule{}, err
	}
	var id int64
	if s.db.IsPostgres() {
		err := s.db.QueryRow(`INSERT INTO notification_rules (name, project_id, environment, notify_new_issue, notify_threshold, notify_reopened, cooldown_seconds)
			VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			r.Name, nullableProjectID(r.ProjectID), r.Environment, r.NotifyNewIssue, r.NotifyThreshold, r.NotifyReopened, r.CooldownSeconds).Scan(&id)
		if err != nil {
			return Rule{}, err
		}
	} else {
		res, err := s.db.Exec(`INSERT INTO notification_rules (name, project_id, environment, notify_new_issue, notify_threshold, notify_reopened, cooldown_seconds)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			r.Name, nullableProjectID(r.ProjectID), r.Environment, r.NotifyNewIssue, r.NotifyThreshold, r.NotifyReopened, r.CooldownSeconds)
		if err != nil {
			return Rule{}, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return Rule{}, err
		}
	}
	if err := s.saveRuleLinks(id, r.Integrations); err != nil {
		return Rule{}, err
	}
	return s.GetRule(id)
}

func (s *Store) UpdateRule(r Rule) (Rule, error) {
	if r.CooldownSeconds < 0 {
		r.CooldownSeconds = 0
	}
	if err := s.validateRuleLinks(r.Integrations); err != nil {
		return Rule{}, err
	}
	_, err := s.db.Exec(`UPDATE notification_rules SET name = ?, project_id = ?, environment = ?, notify_new_issue = ?,
		notify_threshold = ?, notify_reopened = ?, cooldown_seconds = ? WHERE id = ?`,
		r.Name, nullableProjectID(r.ProjectID), r.Environment, r.NotifyNewIssue, r.NotifyThreshold, r.NotifyReopened, r.CooldownSeconds, r.ID)
	if err != nil {
		return Rule{}, err
	}
	if err := s.saveRuleLinks(r.ID, r.Integrations); err != nil {
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

// --- events ---

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
	Status        string
	Timestamp     time.Time
	IssueURL      string
}

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
	type want struct {
		integration Integration
		override    RoutingOverride
	}
	wants := map[int64]want{}
	integrations, err := s.Integrations()
	if err != nil {
		s.logger.Error("load integrations", "error", err)
		return
	}
	byID := map[int64]Integration{}
	for _, in := range integrations {
		byID[in.ID] = in
	}
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
		for _, l := range r.Integrations {
			in, ok := byID[l.IntegrationID]
			if !ok || !in.IsEnabled {
				continue
			}
			wants[in.ID] = want{integration: in, override: l.Override}
		}
	}
	if !shouldNotify {
		return
	}
	if s.withinCooldown(e.IssueID, cooldown) {
		return
	}
	for _, w := range wants {
		if err := s.enqueue(e, w.integration.ID, w.integration.Provider, w.override); err != nil {
			s.logger.Error("enqueue notification", "error", err, "issue_id", e.IssueID, "integration", w.integration.ID)
		}
	}
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

func (s *Store) enqueue(e AlertEvent, integrationID int64, provider string, override RoutingOverride) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	enc, err := s.encodeOverride(override)
	if err != nil {
		return fmt.Errorf("encrypt routing override: %w", err)
	}
	_, err = s.db.Exec(`INSERT INTO notification_queue (issue_id, kind, channel, provider, integration_id, routing_override, payload_json, next_attempt_at) VALUES (?, 'issue_alert', ?, ?, ?, ?, ?, ?)`,
		e.IssueID, provider, provider, integrationID, enc, string(payload), sortableNow())
	return err
}

func sortableNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// DrainOnce sends up to batchSize pending notifications.
func (s *Store) DrainOnce(ctx context.Context, batchSize int) {
	rows, err := s.db.Query(`SELECT id, channel, provider, integration_id, routing_override, payload_json, attempts FROM notification_queue WHERE status = 'pending' AND next_attempt_at <= ? ORDER BY id LIMIT ?`,
		sortableNow(), batchSize)
	if err != nil {
		s.logger.Error("query notification queue", "error", err)
		return
	}
	type job struct {
		id            int64
		channel       string
		provider      string
		integrationID int64
		override      string
		payload       string
		attempts      int
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.channel, &j.provider, &j.integrationID, &j.override, &j.payload, &j.attempts); err != nil {
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
		provider := j.provider
		if provider == "" {
			provider = j.channel
		}
		override, err := s.decodeOverride(j.override)
		if err != nil {
			s.markFailed(j.id, "cannot decrypt routing override: "+err.Error())
			continue
		}
		skip, err := s.sendToIntegration(ctx, j.integrationID, provider, override, e)
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

func (s *Store) sendToIntegration(ctx context.Context, integrationID int64, provider string, override RoutingOverride, e AlertEvent) (bool, error) {
	in, err := s.GetIntegration(integrationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil // deleted integration: skip, don't retry forever
		}
		// Decrypt failure (e.g. rotated server secret): retry with
		// backoff so the operator sees failure_count/last_failure
		// instead of silently dropping the alert.
		return false, err
	}
	if !in.IsEnabled {
		return true, nil
	}
	if provider == "" {
		provider = in.Provider
	}
	skip, err := s.deliver(ctx, in, override, e)
	if err != nil {
		s.bumpFailure(in.ID, err.Error())
		return skip, err
	}
	s.clearFailure(in.ID)
	return skip, nil
}

func (s *Store) deliver(ctx context.Context, in Integration, override RoutingOverride, e AlertEvent) (bool, error) {
	alert := webhooks.Alert{
		ProjectName: e.ProjectName, Environment: e.Environment, ExceptionType: e.ExceptionType, Message: e.Message,
		IsNew: e.IsNew, Reopened: e.Reopened, EventCount: e.EventCount, Timestamp: e.Timestamp, IssueURL: e.IssueURL,
	}
	switch in.Provider {
	case "telegram":
		botToken := credsString(in.Credentials, "bot_token")
		chatID := strings.TrimSpace(override.ChatID)
		if chatID == "" {
			chatID = credsString(in.Credentials, "default_chat_id")
		}
		if botToken == "" || chatID == "" {
			return true, nil
		}
		text := telegram.Format(telegram.Alert{
			ProjectName: e.ProjectName, Environment: e.Environment, ExceptionType: e.ExceptionType, Message: e.Message,
			IsNew: e.IsNew, Reopened: e.Reopened, EventCount: e.EventCount, Timestamp: e.Timestamp, IssueURL: e.IssueURL,
		})
		return false, s.telegram.Send(ctx, botToken, chatID, text)
	case "slack", "discord", "teams", "sheets":
		u := strings.TrimSpace(override.URL)
		if u == "" {
			u = credsString(in.Credentials, "url")
		}
		if u == "" {
			return true, nil
		}
		var body []byte
		switch in.Provider {
		case "slack":
			body = webhooks.FormatSlack(alert)
		case "discord":
			body = webhooks.FormatDiscord(alert)
		case "teams":
			body = webhooks.FormatTeams(alert)
		default:
			body = webhooks.FormatSheets(alert)
		}
		return false, s.webhook.Send(ctx, u, body)
	case "smtp":
		host := credsString(in.Credentials, "host")
		from := credsString(in.Credentials, "from")
		if host == "" || from == "" {
			return true, nil
		}
		if len(override.To) == 0 {
			return true, nil
		}
		var port int
		switch v := in.Credentials["port"].(type) {
		case float64:
			port = int(v)
		case int:
			port = v
		case int64:
			port = int(v)
		case json.Number:
			if n, err := v.Int64(); err == nil {
				port = int(n)
			}
		default:
			port = 587
		}
		if port == 0 {
			port = 587
		}
		username, _ := in.Credentials["username"].(string)
		password, _ := in.Credentials["password"].(string)
		useTLS := true
		if v, ok := in.Credentials["use_tls"].(bool); ok {
			useTLS = v
		}
		var lastErr error
		for _, to := range override.To {
			err := email.Send(email.Config{
				Host: host, Port: port, Username: username,
				Password: password, From: from, UseTLS: useTLS,
			}, to, email.Alert{
				ProjectName: e.ProjectName, Environment: e.Environment, ExceptionType: e.ExceptionType, Message: e.Message,
				IsNew: e.IsNew, Reopened: e.Reopened, EventCount: e.EventCount, Timestamp: e.Timestamp, IssueURL: e.IssueURL,
			})
			if err != nil {
				lastErr = err
			}
		}
		return false, lastErr
	case "generic":
		u := strings.TrimSpace(override.URL)
		if u == "" {
			u = credsString(in.Credentials, "url")
		}
		if u == "" {
			return true, nil
		}
		secret, _ := in.Credentials["secret"].(string)
		headers := map[string]string{}
		if raw, ok := in.Credentials["headers"]; ok {
			switch h := raw.(type) {
			case map[string]any:
				for k, v := range h {
					if vs, ok := v.(string); ok {
						headers[k] = vs
					}
				}
			case map[string]string:
				for k, v := range h {
					headers[k] = v
				}
			case string:
				for k, v := range parseHeaders(h) {
					headers[k] = v
				}
			}
		}
		for k, v := range override.ExtraHeaders {
			headers[k] = v
		}
		body := genericPayload(alert)
		hdrs := map[string]string{}
		for k, v := range headers {
			hdrs[k] = v
		}
		if sig := signBody(secret, body); sig != "" {
			hdrs[SignatureHeader] = sig
		}
		return false, s.webhook.SendWithHeaders(ctx, u, hdrs, body)
	default:
		return false, fmt.Errorf("unknown provider %q", in.Provider)
	}
}

// NotifyQuotaBreach alerts every enabled integration synchronously.
func (s *Store) NotifyQuotaBreach(ctx context.Context, projectName, projectURL, window string, used, limit int) {
	e := AlertEvent{
		ProjectName:   projectName,
		ExceptionType: "Quota exceeded",
		Message:       fmt.Sprintf("%s ingested %d error events in the last %s (limit %d) - ingestion is rejected with 429 until usage drops.", projectName, used, window, limit),
		EventCount:    int64(used),
		Timestamp:     time.Now(),
		IssueURL:      projectURL,
	}
	integrations, err := s.Integrations()
	if err != nil {
		return
	}
	for _, in := range integrations {
		if !in.IsEnabled {
			continue
		}
		// Quota alerts use integration defaults (empty override where valid).
		if _, err := s.deliver(ctx, in, RoutingOverride{}, e); err != nil {
			s.logger.Error("quota breach notify", "error", err, "integration", in.ID, "project", projectName)
		}
	}
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
