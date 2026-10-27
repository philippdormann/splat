// Package storage reports how much disk the instance is using and offers
// manual cleanup. On a self-hosted box nobody else notices the disk filling
// up, so the settings page shows the database size plus per-table row
// counts, with one-click retention sweep and vacuum.
package storage

import (
	"context"
	"os"

	"errgotrack/internal/database"
)

// Tables we report row counts for. Queried best-effort: a table missing on
// an older database is skipped, not an error.
var reportedTables = []string{
	"projects",
	"issues",
	"events",
	"sessions",
	"notification_queue",
	"notification_rules",
	"source_maps",
	"api_keys",
}

type TableUsage struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

type Usage struct {
	Driver    string       `json:"driver"`
	SizeBytes int64        `json:"sizeBytes"`
	Tables    []TableUsage `json:"tables"`
}

type Store struct {
	db     *database.DB
	dbPath string // sqlite file path; empty on postgres
}

func NewStore(db *database.DB, dbPath string) *Store {
	if db.IsPostgres() {
		dbPath = ""
	}
	return &Store{db: db, dbPath: dbPath}
}

// Usage returns the database size plus one row count per known table.
func (s *Store) Usage(ctx context.Context) (Usage, error) {
	u := Usage{Driver: string(s.db.Driver()), Tables: []TableUsage{}}
	size, err := s.dbSize(ctx)
	if err != nil {
		return u, err
	}
	u.SizeBytes = size
	for _, t := range reportedTables {
		var n int64
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+t).Scan(&n); err != nil {
			// Table may not exist on databases migrated before it was
			// introduced - skip it rather than failing the whole report.
			continue
		}
		u.Tables = append(u.Tables, TableUsage{Name: t, Rows: n})
	}
	return u, nil
}

func (s *Store) dbSize(ctx context.Context) (int64, error) {
	if s.db.IsPostgres() {
		var n int64
		if err := s.db.QueryRowContext(ctx, `SELECT pg_database_size(current_database())`).Scan(&n); err != nil {
			return 0, err
		}
		return n, nil
	}
	// SQLite: main file plus WAL/sidecar files while running.
	var total int64
	for _, p := range []string{s.dbPath, s.dbPath + "-wal", s.dbPath + "-shm"} {
		if fi, err := os.Stat(p); err == nil {
			total += fi.Size()
		}
	}
	return total, nil
}

// Vacuum reclaims free pages. On SQLite this is VACUUM (rewrites the file,
// can take a while on large databases - the API runs it synchronously and
// the UI warns accordingly). On Postgres it is VACUUM (ANALYZE), which
// never rewrites the whole database.
func (s *Store) Vacuum(ctx context.Context) error {
	if s.db.IsPostgres() {
		_, err := s.db.ExecContext(ctx, `VACUUM (ANALYZE)`)
		return err
	}
	_, err := s.db.ExecContext(ctx, `VACUUM`)
	return err
}
