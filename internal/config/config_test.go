package config

import (
	"os"
	"testing"
)

// The SPLAT_* names win; pre-rename ERRGOTRACK_* values keep working so
// existing deployments survive the rename without editing their env.
func TestEnvRenameFallback(t *testing.T) {
	t.Setenv("SPLAT_ADDR", "")
	t.Setenv("ERRGOTRACK_ADDR", "")
	t.Setenv("SPLAT_SESSION_SECRET", "")
	t.Setenv("ERRGOTRACK_SESSION_SECRET", "")
	os.Unsetenv("SPLAT_ADDR")
	os.Unsetenv("ERRGOTRACK_ADDR")
	os.Unsetenv("SPLAT_SESSION_SECRET")
	os.Unsetenv("ERRGOTRACK_SESSION_SECRET")

	t.Setenv("ERRGOTRACK_ADDR", ":9999")
	t.Setenv("ERRGOTRACK_SESSION_SECRET", "old-secret-0123456789abcdef")
	c, err := Load()
	if err != nil {
		t.Fatalf("legacy env should load: %v", err)
	}
	if c.Addr != ":9999" {
		t.Fatalf("Addr = %q, want legacy value", c.Addr)
	}

	t.Setenv("SPLAT_ADDR", ":1111")
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":1111" {
		t.Fatalf("Addr = %q, want SPLAT_ precedence", c.Addr)
	}

	if got := renamed("ERRGOTRACK_FOO"); got != "SPLAT_FOO" {
		t.Fatalf("renamed = %q", got)
	}
	if got := renamed("OTHER"); got != "OTHER" {
		t.Fatalf("renamed = %q", got)
	}
}
