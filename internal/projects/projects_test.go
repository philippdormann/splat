package projects

import (
	"path/filepath"
	"strconv"
	"testing"

	"errgotrack/internal/database"
)

// TestDSNPathIsBareProjectID guards against a real bug caught by running the
// actual sentry-sdk client against this server: the SDK's transport treats
// everything in the DSN path as a prefix before the "api/<id>/envelope/" it
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
