package apikeys

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"errgotrack/internal/database"
	"errgotrack/internal/projects"
)

func setup(t *testing.T) (*Store, int64) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	p, err := projects.NewStore(db).Create("P", "p", "production", "", 90)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return NewStore(db), p.ID
}

func TestCreateValidateRoundTrip(t *testing.T) {
	s, pid := setup(t)
	k, err := s.Create("ci", sql.NullInt64{Int64: pid, Valid: true}, []string{"ingest", "read"}, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(k.Secret, "egt_") {
		t.Fatalf("secret missing prefix: %q", k.Secret)
	}
	if k.Prefix != k.Secret[:12] {
		t.Fatalf("prefix mismatch: %q vs %q", k.Prefix, k.Secret)
	}
	got, err := s.Validate(k.Secret)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got.Secret != "" {
		t.Fatal("reads must never return the secret")
	}
	if !got.HasScope(ScopeIngest) || !got.HasScope(ScopeRead) || got.HasScope(ScopeWrite) {
		t.Fatalf("scopes wrong: %v", got.Scopes)
	}
	if !got.ProjectID.Valid || got.ProjectID.Int64 != pid {
		t.Fatalf("project binding wrong: %+v", got.ProjectID)
	}
	if _, err := s.Validate("egt_" + strings.Repeat("0", 48)); err == nil {
		t.Fatal("want error on wrong secret")
	}
	if _, err := s.Validate(""); err == nil {
		t.Fatal("want error on empty secret")
	}
}

func TestDefaultScopeAndBadInput(t *testing.T) {
	s, _ := setup(t)
	k, err := s.Create("x", sql.NullInt64{}, nil, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(k.Scopes) != 1 || k.Scopes[0] != ScopeRead {
		t.Fatalf("default scopes = %v", k.Scopes)
	}
	if _, err := s.Create("x", sql.NullInt64{}, []string{"root"}, ""); err == nil {
		t.Fatal("want error on unknown scope")
	}
	if _, err := s.Create("x", sql.NullInt64{}, nil, "tomorrow-ish"); err == nil {
		t.Fatal("want error on bad expiry")
	}
}

func TestExpiryAndRevoke(t *testing.T) {
	s, _ := setup(t)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	k, err := s.Create("old", sql.NullInt64{}, []string{"read"}, past)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Validate(k.Secret); err == nil {
		t.Fatal("expired key must not validate")
	}
	live, err := s.Create("live", sql.NullInt64{}, []string{"read"}, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Revoke(live.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := s.Validate(live.Secret); err == nil {
		t.Fatal("revoked key must not validate")
	}
}

func TestRotate(t *testing.T) {
	s, pid := setup(t)
	k, err := s.Create("rot", sql.NullInt64{Int64: pid, Valid: true}, []string{"read", "write"}, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	next, err := s.Rotate(k.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if next.Secret == "" || next.Secret == k.Secret {
		t.Fatal("rotation must mint a fresh secret")
	}
	if _, err := s.Validate(k.Secret); err == nil {
		t.Fatal("old secret must die on rotation")
	}
	got, err := s.Validate(next.Secret)
	if err != nil {
		t.Fatalf("new secret must validate: %v", err)
	}
	if got.Name != "rot" || !got.ProjectID.Valid {
		t.Fatalf("rotation must preserve attributes: %+v", got)
	}
}

func TestListHidesSecrets(t *testing.T) {
	s, _ := setup(t)
	if _, err := s.Create("a", sql.NullInt64{}, nil, ""); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
	if list[0].Secret != "" {
		t.Fatal("list must never return secrets")
	}
}
