// Package database owns the DB connection and embedded schema migrations.
//
// SQLite is the default backend (single file, zero ops). Postgres is an
// opt-in backend selected via ERRGOTRACK_DB_DRIVER=postgres +
// ERRGOTRACK_DATABASE_URL. Both backends share the same store interfaces;
// the only SQLite-specific bits (FTS5 search, BEGIN IMMEDIATE locking,
// strftime bucketing) are branched behind DB.IsPostgres at the call sites.
package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var embeddedSQLiteMigrations embed.FS

//go:embed migrations/postgres/*.sql
var embeddedPostgresMigrations embed.FS

// Driver identifies the backing database.
type Driver string

const (
	DriverSQLite   Driver = "sqlite"
	DriverPostgres Driver = "postgres"
)

// DB wraps *sql.DB with driver-aware placeholder rebinding: all existing
// store code keeps writing "?" placeholders and this wrapper rewrites them
// to "$n" on Postgres. On SQLite queries pass through untouched.
type DB struct {
	*sql.DB
	driver Driver
}

// Driver returns the backing driver.
func (d *DB) Driver() Driver { return d.driver }

// IsPostgres reports whether the backing database is Postgres.
func (d *DB) IsPostgres() bool { return d.driver == DriverPostgres }

// Rebind converts "?" placeholders to "$1","$2",... for Postgres.
// On SQLite the query is returned unchanged.
func Rebind(driver Driver, query string) string {
	if driver != DriverPostgres {
		return query
	}
	var b strings.Builder
	n := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteByte(query[i])
		}
	}
	return b.String()
}

func (d *DB) rebind(q string) string { return Rebind(d.driver, q) }

// Exec with automatic placeholder rebinding.
func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	return d.DB.Exec(d.rebind(query), args...)
}

// ExecContext with automatic placeholder rebinding.
func (d *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.DB.ExecContext(ctx, d.rebind(query), args...)
}

// Query with automatic placeholder rebinding.
func (d *DB) Query(query string, args ...any) (*sql.Rows, error) {
	return d.DB.Query(d.rebind(query), args...)
}

// QueryContext with automatic placeholder rebinding.
func (d *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.DB.QueryContext(ctx, d.rebind(query), args...)
}

// QueryRow with automatic placeholder rebinding.
func (d *DB) QueryRow(query string, args ...any) *sql.Row {
	return d.DB.QueryRow(d.rebind(query), args...)
}

// QueryRowContext with automatic placeholder rebinding.
func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.DB.QueryRowContext(ctx, d.rebind(query), args...)
}

// Conn returns a driver-aware dedicated connection.
func (d *DB) Conn(ctx context.Context) (*Conn, error) {
	c, err := d.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	return &Conn{Conn: c, driver: d.driver}, nil
}

// Begin returns a driver-aware transaction.
func (d *DB) Begin() (*Tx, error) {
	tx, err := d.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, driver: d.driver}, nil
}

// BeginTx returns a driver-aware transaction.
func (d *DB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := d.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, driver: d.driver}, nil
}

// Conn is a driver-aware *sql.Conn with placeholder rebinding.
type Conn struct {
	*sql.Conn
	driver Driver
}

func (c *Conn) rebind(q string) string { return Rebind(c.driver, q) }

// ExecContext with rebinding.
func (c *Conn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.Conn.ExecContext(ctx, c.rebind(query), args...)
}

// QueryContext with rebinding.
func (c *Conn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.Conn.QueryContext(ctx, c.rebind(query), args...)
}

// QueryRowContext with rebinding.
func (c *Conn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.Conn.QueryRowContext(ctx, c.rebind(query), args...)
}

// Tx is a driver-aware *sql.Tx with placeholder rebinding.
type Tx struct {
	*sql.Tx
	driver Driver
}

func (t *Tx) rebind(q string) string { return Rebind(t.driver, q) }

// Exec with rebinding.
func (t *Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.Tx.Exec(t.rebind(query), args...)
}

// ExecContext with rebinding.
func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, t.rebind(query), args...)
}

// Query with rebinding.
func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.Tx.Query(t.rebind(query), args...)
}

// QueryContext with rebinding.
func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(ctx, t.rebind(query), args...)
}

// QueryRow with rebinding.
func (t *Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.Tx.QueryRow(t.rebind(query), args...)
}

// QueryRowContext with rebinding.
func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.Tx.QueryRowContext(ctx, t.rebind(query), args...)
}

// Prepare with rebinding.
func (t *Tx) Prepare(query string) (*Stmt, error) {
	s, err := t.Tx.Prepare(t.rebind(query))
	if err != nil {
		return nil, err
	}
	return &Stmt{Stmt: s}, nil
}

// Stmt wraps *sql.Stmt (already rebound at Prepare time).
type Stmt struct {
	*sql.Stmt
}

// Open opens (creating if needed) the SQLite database at path, applies
// pragmas suited to a single-instance low-concurrency deployment, and runs
// any pending migrations. Kept for backwards compatibility (tests, sqlite
// default); prefer OpenWithDriver for driver selection.
func Open(path string) (*DB, error) {
	return OpenSQLite(path)
}

// OpenWithDriver selects the backend: "sqlite" (pathOrURL = file path,
// default) or "postgres" (pathOrURL = connection URL).
func OpenWithDriver(driver, pathOrURL string) (*DB, error) {
	switch Driver(strings.ToLower(driver)) {
	case "", DriverSQLite:
		return OpenSQLite(pathOrURL)
	case DriverPostgres:
		return OpenPostgres(pathOrURL)
	default:
		return nil, fmt.Errorf("unknown database driver %q (want sqlite or postgres)", driver)
	}
}

// OpenSQLite opens the SQLite backend.
func OpenSQLite(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)", path)
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite allows exactly one writer; keep the pool small so readers queue
	// instead of hammering the busy_timeout retry path.
	raw.SetMaxOpenConns(8)
	db := &DB{DB: raw, driver: DriverSQLite}
	if err := migrateSQLite(db); err != nil {
		raw.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

// OpenPostgres opens the Postgres backend and runs pending migrations.
func OpenPostgres(url string) (*DB, error) {
	raw, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	raw.SetMaxOpenConns(16)
	db := &DB{DB: raw, driver: DriverPostgres}
	if err := raw.Ping(); err != nil {
		raw.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	if err := migratePostgres(db); err != nil {
		raw.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func migrateSQLite(db *DB) error {
	if _, err := db.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')))`); err != nil {
		return err
	}

	entries, err := fs.Glob(embeddedSQLiteMigrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)

	for _, name := range entries {
		// Skip the postgres subdirectory (matched only if glob changes).
		if strings.Contains(name, "/postgres/") {
			continue
		}
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
		sqlBytes, err := embeddedSQLiteMigrations.ReadFile(name)
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

func migratePostgres(db *DB) error {
	if _, err := db.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (now()::text))`); err != nil {
		return err
	}

	entries, err := fs.Glob(embeddedPostgresMigrations, "migrations/postgres/*.sql")
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
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		sqlBytes, err := embeddedPostgresMigrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := db.DB.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
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
