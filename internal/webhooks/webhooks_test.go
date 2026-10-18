package webhooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sampleAlert() Alert {
	return Alert{
		ProjectName: "Demo", Environment: "production", ExceptionType: "ValueError",
		Message: "boom", IsNew: true, EventCount: 1, Timestamp: time.Now(), IssueURL: "http://localhost/issues/1",
	}
}

func TestFormatSlackIsValidJSONWithText(t *testing.T) {
	var out map[string]any
	if err := json.Unmarshal(FormatSlack(sampleAlert()), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	text, _ := out["text"].(string)
	if !strings.Contains(text, "ValueError") || !strings.Contains(text, "Demo") {
		t.Fatalf("text missing expected content: %q", text)
	}
}

func TestFormatDiscordIsValidJSONWithEmbed(t *testing.T) {
	var out map[string]any
	if err := json.Unmarshal(FormatDiscord(sampleAlert()), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	embeds, ok := out["embeds"].([]any)
	if !ok || len(embeds) != 1 {
		t.Fatalf("expected exactly one embed, got %+v", out)
	}
}

func TestFormatTeamsIsValidMessageCard(t *testing.T) {
	var out map[string]any
	if err := json.Unmarshal(FormatTeams(sampleAlert()), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if out["@type"] != "MessageCard" {
		t.Fatalf("expected MessageCard, got %+v", out["@type"])
	}
}

func TestFormatSheetsIsValidJSONWithFields(t *testing.T) {
	var out map[string]any
	if err := json.Unmarshal(FormatSheets(sampleAlert()), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if out["project"] != "Demo" || out["exception"] != "ValueError" {
		t.Fatalf("missing expected fields: %+v", out)
	}
}

func TestValidateURLRejectsPrivateAndLoopback(t *testing.T) {
	cases := []string{
		"http://127.0.0.1:9000/hook",
		"http://localhost/hook",
		"http://169.254.169.254/latest/meta-data/", // cloud metadata - link-local range
		"http://10.0.0.5/hook",
		"http://192.168.1.1/hook",
		"ftp://example.com/hook", // wrong scheme
	}
	for _, u := range cases {
		if err := ValidateURL(u); err == nil {
			t.Errorf("expected ValidateURL(%q) to reject it, got nil error", u)
		}
	}
}

func TestValidateURLAcceptsPublicAddress(t *testing.T) {
	// A literal public IP, not a hostname, so this doesn't depend on any
	// particular DNS name resolving in the test environment.
	if err := ValidateURL("https://8.8.8.8/webhook"); err != nil {
		t.Fatalf("expected a public IP to validate, got: %v", err)
	}
}

func TestSendSuccessAndFailure(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer ok.Close()
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer fail.Close()

	c := NewClient()
	if err := c.Send(context.Background(), ok.URL, []byte(`{}`)); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if err := c.Send(context.Background(), fail.URL, []byte(`{}`)); err == nil {
		t.Fatal("expected error for 500 response")
	}
	if err := c.Send(context.Background(), "", []byte(`{}`)); err == nil {
		t.Fatal("expected error for empty url")
	}
}
