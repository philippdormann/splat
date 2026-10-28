package projects

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/philippdormann/splat/internal/database"
)

// TestDSNPathIsBareProjectID guards against a real bug caught by running the
// actual sentry-sdk client against this server: the SDK's transport treats
// everything in the DSN path as a prefix before the "api/<project_id>/envelope/" it
// builds itself, so a DSN path like "/api/1" makes the SDK request
// "/api/api/1/envelope/" - a 404 against this server's routes. The DSN path
// must be exactly "/<project_id>".
func TestDSNPathIsBareProjectID(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	p, err := store.Create("Demo", "demo", "production", "", 90)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	dsn, err := DSN("https://errors.example.com", p)
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}
	want := "https://" + p.PublicKey + "@errors.example.com/" + strconv.FormatInt(p.ID, 10)
	if dsn != want {
		t.Fatalf("DSN = %q, want %q (sentry-sdk would request /api/api/%d/envelope/ instead of /api/%d/envelope/)", dsn, want, p.ID, p.ID)
	}
}

func TestUpdateSettingsRenamesProject(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	p, err := store.Create("Old Name", "old-name", "production", "", 90)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := store.UpdateSettings(p.ID, "New Name", "staging", "", 30); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	got, err := store.Get(p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "New Name" {
		t.Fatalf("expected name to update to %q, got %q", "New Name", got.Name)
	}

	// A blank name on the form must not wipe out the existing one - the
	// field is required client-side, but a direct/malformed POST shouldn't
	// be able to leave the project unnamed.
	if err := store.UpdateSettings(p.ID, "", "staging", "", 30); err != nil {
		t.Fatalf("update settings with blank name: %v", err)
	}
	got, err = store.Get(p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "New Name" {
		t.Fatalf("expected a blank name submission to keep the existing name %q, got %q", "New Name", got.Name)
	}
}

// TestCreatedAtIsRecent guards against the "/projects shows 739917d ago"
// bug: created_at fell back to the DB default and the strict RFC3339 parse
// silently zeroed it (Postgres now()::text is "2006-01-02 15:04:05.999+00").
func TestCreatedAtIsRecent(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	before := time.Now().Add(-time.Minute)
	p, err := store.Create("Fresh", "fresh", "production", "", 90)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.CreatedAt.Before(before) || p.CreatedAt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("CreatedAt = %v, want ~now (zero time renders as ~739917d ago)", p.CreatedAt)
	}

	// Every timestamp shape the column can hold must parse to a real time.
	for _, raw := range []string{
		"2026-10-28T12:34:56.789Z",   // Go-written / SQLite fractional
		"2026-10-28T12:34:56Z",       // Go-written, whole seconds
		"2026-10-28 12:34:56.789+00", // Postgres now()::text default
		"2026-10-28 12:34:56+00",
	} {
		got := parseCreatedAt(raw)
		if got.IsZero() {
			t.Fatalf("parseCreatedAt(%q) = zero time", raw)
		}
		if got.Year() != 2026 || got.Month() != time.October || got.Day() != 28 {
			t.Fatalf("parseCreatedAt(%q) = %v, want 2026-10-28", raw, got)
		}
	}
}
