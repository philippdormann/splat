package database

import (
	"path/filepath"
	"testing"
)

func TestOpenRunsMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	for _, table := range []string{"projects", "issues", "events", "admin_users", "sessions", "telegram_config", "notification_queue"} {
		var name string
		if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("expected table %q to exist: %v", table, err)
		}
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil || count == 0 {
		t.Fatalf("expected at least one recorded migration, count=%d err=%v", count, err)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	db1.Close()

	// Reopening an already-migrated database must not re-run migrations
	// (which would fail on CREATE TABLE of an existing table) or error.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer db2.Close()
}

func TestForeignKeysEnforced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`INSERT INTO issues (project_id, fingerprint, title, status, first_seen, last_seen) VALUES (999, 'x', 'x', 'unresolved', '2024-01-01', '2024-01-01')`)
	if err == nil {
		t.Fatal("expected foreign key violation when inserting an issue for a non-existent project")
	}
}
