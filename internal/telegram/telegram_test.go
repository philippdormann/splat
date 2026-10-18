package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFormatEscapesHTML(t *testing.T) {
	out := Format(Alert{
		ProjectName:   "Demo",
		Environment:   "prod<ok>",
		ExceptionType: "<script>alert(1)</script>",
		Message:       "msg & stuff",
		IsNew:         true,
		EventCount:    1,
		Timestamp:     time.Now(),
	})
	if strings.Contains(out, "<script>") {
		t.Fatalf("exception type was not escaped: %s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Fatalf("expected escaped script tag, got: %s", out)
	}
}

func TestFormatTruncatesLongMessage(t *testing.T) {
	long := strings.Repeat("x", 1000)
	out := Format(Alert{ExceptionType: "E", Message: long, Timestamp: time.Now()})
	if strings.Contains(out, strings.Repeat("x", 1000)) {
		t.Fatal("message should have been truncated")
	}
}

func TestSendRespectsTimeoutAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client()}
	err := c.sendTo(context.Background(), srv.URL, "token", "chat", "hi")
	if err == nil {
		t.Fatal("expected error for non-2xx response")
	}
}

func TestSendRequiresConfig(t *testing.T) {
	c := NewClient()
	if err := c.Send(context.Background(), "", "", "hi"); err == nil {
		t.Fatal("expected error when bot token/chat id are empty")
	}
}
