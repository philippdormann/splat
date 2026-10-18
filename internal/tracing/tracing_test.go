package tracing

import (
	"path/filepath"
	"testing"

	"errgotrack/internal/database"
)

func TestBuildURL(t *testing.T) {
	cases := []struct {
		tmpl, traceID, want string
	}{
		{"https://tempo.example.com/explore?traceID={trace_id}", "abc123", "https://tempo.example.com/explore?traceID=abc123"},
		{"", "abc123", ""},
		{"https://x/{trace_id}", "", ""},
	}
	for _, c := range cases {
		if got := BuildURL(c.tmpl, c.traceID); got != c.want {
			t.Errorf("BuildURL(%q, %q) = %q, want %q", c.tmpl, c.traceID, got, c.want)
		}
	}
}

func TestStoreRoundTrip(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	s := NewStore(db)
	got, err := s.GetURLTemplate()
	if err != nil || got != "" {
		t.Fatalf("expected empty default, got %q err=%v", got, err)
	}

	if err := s.SetURLTemplate("https://jaeger.example.com/trace/{trace_id}"); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err = s.GetURLTemplate()
	if err != nil || got != "https://jaeger.example.com/trace/{trace_id}" {
		t.Fatalf("got %q err=%v", got, err)
	}
}
