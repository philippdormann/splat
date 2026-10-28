// Package email delivers issue alerts over SMTP using only the standard
// library. The server never verifies certificates against a custom pool
// and never sends HTML - plain-text alerts with the project, environment,
// exception and a link back to the issue.
package email

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	UseTLS   bool
	To       string // per-send recipient; instance config carries no default recipient
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

// Subject prefixes new vs recurring issues so rules/filtering work.
func Subject(a Alert) string {
	kind := "Recurring issue"
	switch {
	case a.IsNew:
		kind = "New issue"
	case a.Reopened:
		kind = "Reopened issue"
	}
	title := a.ExceptionType
	if title == "" {
		title = "message"
	}
	return fmt.Sprintf("[Splat] %s in %s: %s", kind, a.ProjectName, title)
}

// Body is plain text - no HTML, no tracking pixels, nothing to render.
func Body(a Alert) string {
	var b strings.Builder
	kind := "recurring"
	switch {
	case a.IsNew:
		kind = "new"
	case a.Reopened:
		kind = "reopened"
	}
	fmt.Fprintf(&b, "Project: %s\nEnvironment: %s\nStatus: %s\n", a.ProjectName, a.Environment, kind)
	if a.ExceptionType != "" {
		fmt.Fprintf(&b, "Exception: %s\n", a.ExceptionType)
	}
	if a.Message != "" {
		fmt.Fprintf(&b, "Message: %s\n", a.Message)
	}
	fmt.Fprintf(&b, "Events: %d\nTime: %s\n", a.EventCount, a.Timestamp.UTC().Format(time.RFC3339))
	if a.IssueURL != "" {
		fmt.Fprintf(&b, "Issue: %s\n", a.IssueURL)
	}
	return b.String()
}

// Send delivers one alert email. To is the recipient (alert rules don't
// carry recipients - the operator puts the on-call address in the SMTP
// settings or per-rule config instead; see notifications).
func Send(cfg Config, to string, a Alert) error {	if cfg.Host == "" {
		return fmt.Errorf("SMTP host not configured")
	}
	if to == "" {
		return fmt.Errorf("no recipient address")
	}
	from := cfg.From
	if from == "" {
		from = cfg.Username
	}
	if from == "" {
		return fmt.Errorf("no sender address (set From or Username)")
	}
	port := cfg.Port
	if port <= 0 {
		port = 587
	}
	msg := "From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + Subject(a) + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" + Body(a)

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("dial SMTP: %w", err)
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("SMTP handshake: %w", err)
	}
	defer c.Close()
	if cfg.UseTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
				return fmt.Errorf("SMTP STARTTLS: %w", err)
			}
		}
	}
	if cfg.Username != "" {
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("SMTP RCPT: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("SMTP write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("SMTP send: %w", err)
	}
	return c.Quit()
}

// SendRaw delivers a plain-text message with a custom subject (invites,
// not issue alerts). Reuses the same SMTP dial/TLS/auth path as Send.
func SendRaw(cfg Config, to, subject, body string) error {
	if cfg.Host == "" {
		return fmt.Errorf("SMTP host not configured")
	}
	if to == "" {
		return fmt.Errorf("no recipient address")
	}
	from := cfg.From
	if from == "" {
		from = cfg.Username
	}
	if from == "" {
		return fmt.Errorf("no sender address (set From or Username)")
	}
	msg := "From: " + from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" + body
	return sendRaw(cfg, to, from, msg)
}

func sendRaw(cfg Config, to, from, msg string) error {
	port := cfg.Port
	if port <= 0 {
		port = 587
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("dial SMTP: %w", err)
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("SMTP handshake: %w", err)
	}
	defer c.Close()
	if cfg.UseTLS {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
				return fmt.Errorf("SMTP STARTTLS: %w", err)
			}
		}
	}
	if cfg.Username != "" {
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("SMTP RCPT: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("SMTP write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("SMTP send: %w", err)
	}
	return c.Quit()
}
