package email

import (
	"strings"
	"testing"
	"time"
)

func testAlert() Alert {
	return Alert{
		ProjectName: "Shop", Environment: "production", ExceptionType: "ValueError",
		Message: "boom", IsNew: true, EventCount: 3,
		Timestamp: time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC),
		IssueURL:  "http://localhost:8080/issues/7",
	}
}

func TestSubject(t *testing.T) {
	if s := Subject(testAlert()); s != "[Errgotrack] New issue in Shop: ValueError" {
		t.Fatalf("subject = %q", s)
	}
	a := testAlert()
	a.IsNew, a.Reopened = false, true
	if s := Subject(a); !strings.Contains(s, "Reopened issue") {
		t.Fatalf("subject = %q", s)
	}
	a.IsNew, a.Reopened = false, false
	if s := Subject(a); !strings.Contains(s, "Recurring issue") {
		t.Fatalf("subject = %q", s)
	}
}

func TestBodyPlainText(t *testing.T) {
	b := Body(testAlert())
	for _, want := range []string{"Project: Shop", "Environment: production", "Exception: ValueError", "Message: boom", "Events: 3", "http://localhost:8080/issues/7"} {
		if !strings.Contains(b, want) {
			t.Fatalf("body missing %q:\n%s", want, b)
		}
	}
	if strings.Contains(strings.ToLower(b), "<html") {
		t.Fatal("body must stay plain text")
	}
}

func TestSendRequiresConfig(t *testing.T) {
	if err := Send(Config{}, "oncall@example.com", testAlert()); err == nil {
		t.Fatal("want error with no host")
	}
	if err := Send(Config{Host: "smtp.example.com"}, "", testAlert()); err == nil {
		t.Fatal("want error with no recipient")
	}
	// Unreachable host must fail fast-ish, not hang.
	if err := Send(Config{Host: "127.0.0.1", Port: 1}, "oncall@example.com", testAlert()); err == nil {
		t.Fatal("want dial error for closed port")
	}
}
