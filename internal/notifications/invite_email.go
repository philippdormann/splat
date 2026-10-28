package notifications

import (
	"encoding/json"
	"fmt"

	"github.com/philippdormann/splat/internal/email"
)

// SendInviteEmail delivers a team invitation through the first enabled
// SMTP integration. Returns nil (no-op) when no SMTP integration is
// configured, so invite creation never fails for missing email infra:
// the token link is always returned for manual copy.
func (s *Store) SendInviteEmail(toEmail, username, link string) error {
	integrations, err := s.Integrations()
	if err != nil {
		return err
	}
	for _, in := range integrations {
		if !in.IsEnabled || in.Provider != "smtp" {
			continue
		}
		host := credsString(in.Credentials, "host")
		from := credsString(in.Credentials, "from")
		if host == "" || from == "" {
			continue
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
		usernameCred, _ := in.Credentials["username"].(string)
		password, _ := in.Credentials["password"].(string)
		useTLS := true
		if v, ok := in.Credentials["use_tls"].(bool); ok {
			useTLS = v
		}
		subject := "[Splat] You are invited to join Splat"
		body := fmt.Sprintf("Hi %s,\n\nYou have been invited to join Splat.\n\nAccept your invitation (expires in 48 hours):\n%s\n\nIf you did not expect this, ignore this email.\n", username, link)
		return email.SendRaw(email.Config{
			Host: host, Port: port, Username: usernameCred,
			Password: password, From: from, UseTLS: useTLS,
		}, toEmail, subject, body)
	}
	return nil
}
