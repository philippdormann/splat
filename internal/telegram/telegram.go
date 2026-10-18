// Package telegram sends issue alerts via the Telegram Bot HTTP API. No SDK
// needed - it's one JSON POST.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	httpClient *http.Client
	baseURL    string // overridden in tests to point at an httptest server
}

func NewClient() *Client {
	return &Client{httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: "https://api.telegram.org"}
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

// Format renders an alert using Telegram's HTML parse mode. All dynamic
// values are escaped since they come from untrusted exception data.
func Format(a Alert) string {
	kind := "Recurring issue"
	if a.IsNew {
		kind = "New issue"
	} else if a.Reopened {
		kind = "Issue reopened"
	}
	msg := a.Message
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s</b> · %s\n", esc(kind), esc(a.ProjectName))
	fmt.Fprintf(&b, "Environment: <code>%s</code>\n", esc(orDash(a.Environment)))
	fmt.Fprintf(&b, "<b>%s</b>\n", esc(a.ExceptionType))
	if msg != "" {
		fmt.Fprintf(&b, "%s\n", esc(msg))
	}
	fmt.Fprintf(&b, "Occurrences: %d · %s\n", a.EventCount, a.Timestamp.Format("2006-01-02 15:04 MST"))
	if a.IssueURL != "" {
		fmt.Fprintf(&b, "%s", esc(a.IssueURL))
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func esc(s string) string {
	return html.EscapeString(s)
}

type sendMessageRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

// Send posts one message. Callers are responsible for retry/backoff (see
// internal/notifications) - this is a single best-effort attempt bounded by
// the client's timeout.
func (c *Client) Send(ctx context.Context, botToken, chatID, text string) error {
	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram not configured")
	}
	return c.sendTo(ctx, c.baseURL, botToken, chatID, text)
}

func (c *Client) sendTo(ctx context.Context, baseURL, botToken, chatID, text string) error {
	body, err := json.Marshal(sendMessageRequest{ChatID: chatID, Text: text, ParseMode: "HTML"})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", baseURL, botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}
	return nil
}
