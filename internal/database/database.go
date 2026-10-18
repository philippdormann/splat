// Package database owns the SQLite connection and embedded schema migrations.
package database

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// Open opens (creating if needed) the SQLite database at path, applies
// pragmas suited to a single-instance low-concurrency deployment, and runs
// any pending migrations.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite allows exactly one writer; keep the pool small so readers queue
	// instead of hammering the busy_timeout retry path.
	db.SetMaxOpenConns(8)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')))`); err != nil {
		return err
	}

	entries, err := fs.Glob(embeddedMigrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)

	for _, name := range entries {
		version, err := versionOf(name)
		if err != nil {
			return err
		}
		var applied int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		sqlBytes, err := embeddedMigrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func versionOf(filename string) (int, error) {
	base := filename
	if idx := strings.LastIndex(filename, "/"); idx >= 0 {
		base = filename[idx+1:]
	}
	prefix, _, ok := strings.Cut(base, "_")
	if !ok {
		return 0, fmt.Errorf("migration filename %q missing version prefix", filename)
	}
	return strconv.Atoi(prefix)
}
