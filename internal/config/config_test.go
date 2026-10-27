package config

import (
	"testing"
)

func TestLoadReadsSplatEnv(t *testing.T) {
	t.Setenv("SPLAT_ADDR", ":9999")
	t.Setenv("SPLAT_SESSION_SECRET", "test-secret-0123456789abcdef01234567")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Addr != ":9999" {
		t.Fatalf("Addr = %q", c.Addr)
	}
	if c.DatabasePath != "data/splat.db" {
		t.Fatalf("DatabasePath = %q", c.DatabasePath)
	}
}

func TestLoadRequiresSessionSecret(t *testing.T) {
	t.Setenv("SPLAT_SESSION_SECRET", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing secret must fail")
	}
}
