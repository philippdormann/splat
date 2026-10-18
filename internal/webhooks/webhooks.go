// Package webhooks sends issue alerts to Slack, Discord, and Microsoft
// Teams via their incoming-webhook URLs - simpler than Telegram's bot API,
// since each is just one JSON POST to a URL the operator pastes in, no
// token/chat-id lookup involved.
package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{httpClient: &http.Client{Timeout: 10 * time.Second}}
}

type Alert struct {
	ProjectName   string
	Environment   string
	ExceptionType string
	Message       string
	IsNew         bool
	Reopened      bool
	EventCount    int64
	Timestamp     time.Time
	IssueURL      string
}

func kind(a Alert) string {
	switch {
	case a.IsNew:
		return "New issue"
	case a.Reopened:
		return "Issue reopened"
	default:
		return "Recurring issue"
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// FormatSlack builds a Slack Incoming Webhook payload using Block Kit.
func FormatSlack(a Alert) []byte {
	text := fmt.Sprintf("*%s* · %s\n*%s*\n%s\nOccurrences: %d · %s",
		kind(a), a.ProjectName, a.ExceptionType, truncate(a.Message, 300), a.EventCount, a.Timestamp.Format("2006-01-02 15:04 MST"))

	blocks := []map[string]any{
		{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": text}},
	}
	if a.IssueURL != "" {
		blocks = append(blocks, map[string]any{
			"type": "actions",
			"elements": []map[string]any{{
				"type": "button", "text": map[string]any{"type": "plain_text", "text": "View issue"}, "url": a.IssueURL,
			}},
		})
	}
	b, _ := json.Marshal(map[string]any{"text": text, "blocks": blocks})
	return b
}

// FormatDiscord builds a Discord webhook payload using an embed.
func FormatDiscord(a Alert) []byte {
	const colorNew = 0xdc2626 // red - matches the error-severity color used in the dashboard
	const colorRecurring = 0x6b7280
	color := colorRecurring
	if a.IsNew || a.Reopened {
		color = colorNew
	}

	embed := map[string]any{
		"title":       fmt.Sprintf("%s · %s", kind(a), a.ProjectName),
		"description": fmt.Sprintf("**%s**\n%s", a.ExceptionType, truncate(a.Message, 300)),
		"color":       color,
		"fields": []map[string]any{
			{"name": "Environment", "value": orDash(a.Environment), "inline": true},
			{"name": "Occurrences", "value": fmt.Sprintf("%d", a.EventCount), "inline": true},
		},
		"timestamp": a.Timestamp.Format(time.RFC3339),
	}
	if a.IssueURL != "" {
		embed["url"] = a.IssueURL
	}
	b, _ := json.Marshal(map[string]any{"embeds": []map[string]any{embed}})
	return b
}

// FormatTeams builds a legacy MessageCard payload for a Teams Incoming
// Webhook connector - still the most broadly supported format for that
// integration type, despite Teams pushing "Workflows" for new setups.
func FormatTeams(a Alert) []byte {
	payload := map[string]any{
		"@type":      "MessageCard",
		"@context":   "http://schema.org/extensions",
		"summary":    fmt.Sprintf("%s: %s", kind(a), a.ExceptionType),
		"themeColor": "DC2626",
		"title":      fmt.Sprintf("%s · %s", kind(a), a.ProjectName),
		"text": fmt.Sprintf("**%s**\n\n%s\n\nEnvironment: %s · Occurrences: %d · %s",
			a.ExceptionType, truncate(a.Message, 300), orDash(a.Environment), a.EventCount, a.Timestamp.Format("2006-01-02 15:04 MST")),
	}
	if a.IssueURL != "" {
		payload["potentialAction"] = []map[string]any{{
			"@type": "OpenUri", "name": "View issue",
			"targets": []map[string]any{{"os": "default", "uri": a.IssueURL}},
		}}
	}
	b, _ := json.Marshal(payload)
	return b
}

// FormatSheets builds a flat JSON object for a Google Apps Script Web App
// deployed as the webhook target - there's no fixed schema to match like
// Slack/Discord/Teams have, this is just the row data; the paired Apps
// Script snippet (see settings page) decides column order.
func FormatSheets(a Alert) []byte {
	b, _ := json.Marshal(map[string]any{
		"timestamp":   a.Timestamp.Format(time.RFC3339),
		"kind":        kind(a),
		"project":     a.ProjectName,
		"environment": orDash(a.Environment),
		"exception":   a.ExceptionType,
		"message":     truncate(a.Message, 500),
		"occurrences": a.EventCount,
		"issue_url":   a.IssueURL,
	})
	return b
}

// ValidateURL rejects webhook URLs that would make the server issue a
// request to a private, loopback, or link-local address - including cloud
// metadata endpoints like 169.254.169.254, which live in the link-local
// range. This is SSRF defense for a URL an admin pastes into Settings: it
// protects against a compromised or malicious admin session making this
// server probe internal infrastructure, not against an unauthenticated
// attacker, since setting the URL already requires being logged in.
//
// It checks the hostname's resolved IPs once, here, not at the moment the
// request is actually sent - so it does not defend against DNS rebinding
// (a hostname resolving to a public IP now and a private one on the actual
// request). Accepted gap for a self-hosted single-admin tool; a stricter
// deployment would resolve once and connect to the pinned IP directly.
func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL must use http or https")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no host")
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("could not resolve host %q: %w", host, err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("webhook URL resolves to a private/internal address (%s) - refusing to send requests to internal infrastructure", ip)
		}
	}
	return nil
}

// Send posts a pre-built JSON payload to a webhook URL. Callers handle
// retry/backoff (see internal/notifications) - this is a single best-effort
// attempt bounded by the client's timeout.
func (c *Client) Send(ctx context.Context, url string, body []byte) error {
	if url == "" {
		return fmt.Errorf("webhook not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
