// Package projects manages ingestion projects: creation, lookup by key, and
// DSN/credential rotation.
package projects

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/philippdormann/splat/internal/database"
	"github.com/philippdormann/splat/internal/platforms"
)

type Project struct {
	ID                 int64
	Name               string
	Slug               string
	PublicKey          string
	Enabled            bool
	DefaultEnvironment string
	RetentionDays      int
	Platform           string
	CreatedAt          time.Time
}

var ErrNotFound = errors.New("project not found")

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

func generateKey() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (s *Store) Create(name, slug, defaultEnv, platform string, retentionDays int) (*Project, error) {
	key, err := generateKey()
	if err != nil {
		return nil, err
	}
	if defaultEnv == "" {
		defaultEnv = "production"
	}
	// retentionDays <= 0 means infinite: the sweeper only touches projects
	// with retention_days > 0, so clamp negatives to 0 (keep forever).
	if retentionDays < 0 {
		retentionDays = 0
	}
	if !platforms.Valid(platform) {
		platform = platforms.DefaultID
	}
	if s.db.IsPostgres() {
		var id int64
		err := s.db.QueryRow(`INSERT INTO projects (name, slug, public_key, default_environment, retention_days, platform, created_at) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			name, slug, key, defaultEnv, retentionDays, platform, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&id)
		if err != nil {
			return nil, err
		}
		return s.Get(id)
	}
	res, err := s.db.Exec(`INSERT INTO projects (name, slug, public_key, default_environment, retention_days, platform, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		name, slug, key, defaultEnv, retentionDays, platform, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

func scanProject(row interface{ Scan(...any) error }) (*Project, error) {
	var p Project
	var enabled int
	var createdAt string
	if err := row.Scan(&p.ID, &p.Name, &p.Slug, &p.PublicKey, &enabled, &p.DefaultEnvironment, &p.RetentionDays, &p.Platform, &createdAt); err != nil {
		return nil, err
	}
	p.Enabled = enabled != 0
	p.CreatedAt = parseCreatedAt(createdAt)
	return &p, nil
}

// parseCreatedAt tolerates every timestamp shape this column can hold:
// RFC3339Nano written by Go, and Postgres now()::text ("2006-01-02
// 15:04:05.999999+00") left behind by the column default on rows created
// before inserts set created_at explicitly. A strict RFC3339 parse
// silently zeroed those rows, showing up as "739917d ago" in the UI.
func parseCreatedAt(s string) time.Time {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07",
		"2006-01-02T15:04:05.999999999-07",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

const selectCols = `id, name, slug, public_key, enabled, default_environment, retention_days, platform, created_at`

func (s *Store) Get(id int64) (*Project, error) {
	row := s.db.QueryRow(`SELECT `+selectCols+` FROM projects WHERE id = ?`, id)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Store) GetBySlug(slug string) (*Project, error) {
	row := s.db.QueryRow(`SELECT `+selectCols+` FROM projects WHERE slug = ?`, slug)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// GetByPublicKey resolves the project a Sentry client is authenticating as.
func (s *Store) GetByPublicKey(key string) (*Project, error) {
	row := s.db.QueryRow(`SELECT `+selectCols+` FROM projects WHERE public_key = ?`, key)
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Store) List() ([]*Project, error) {
	rows, err := s.db.Query(`SELECT ` + selectCols + ` FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SetEnabled(id int64, enabled bool) error {
	// INTEGER column: pass 0/1, a raw bool trips pgx with
	// "unable to encode ... into binary format for int4".
	flag := 0
	if enabled {
		flag = 1
	}
	_, err := s.db.Exec(`UPDATE projects SET enabled = ? WHERE id = ?`, flag, id)
	return err
}

func (s *Store) UpdateSettings(id int64, name, defaultEnvironment, platform string, retentionDays int) error {
	if defaultEnvironment == "" {
		defaultEnvironment = "production"
	}
	if retentionDays < 0 {
		retentionDays = 0
	}
	if !platforms.Valid(platform) {
		platform = platforms.DefaultID
	}
	if name == "" {
		// Blank submissions (shouldn't normally happen, the field is
		// required client-side) keep the existing name rather than
		// clearing it - a project with no name is worse than a no-op.
		_, err := s.db.Exec(`UPDATE projects SET default_environment = ?, retention_days = ?, platform = ? WHERE id = ?`,
			defaultEnvironment, retentionDays, platform, id)
		return err
	}
	_, err := s.db.Exec(`UPDATE projects SET name = ?, default_environment = ?, retention_days = ?, platform = ? WHERE id = ?`,
		name, defaultEnvironment, retentionDays, platform, id)
	return err
}

func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	return err
}

// RotateKey replaces the project's public key, immediately invalidating any
// client still configured with the old DSN.
func (s *Store) RotateKey(id int64) (string, error) {
	key, err := generateKey()
	if err != nil {
		return "", err
	}
	if _, err := s.db.Exec(`UPDATE projects SET public_key = ? WHERE id = ?`, key, id); err != nil {
		return "", err
	}
	return key, nil
}

// DSN builds the Sentry-compatible DSN clients configure via SENTRY_DSN.
// baseURL must be the externally reachable scheme+host, e.g. https://errors.example.com.
//
// The DSN's path is just the project id - sentry-sdk's transport treats
// everything in the DSN path as a prefix before "api/<project_id>/envelope/"
// that it builds itself, so a DSN path of "/api/1" would make the SDK
// request "/api/api/1/envelope/" (confirmed against the real Python SDK).
func DSN(baseURL string, p *Project) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base url: %w", err)
	}
	u.User = url.User(p.PublicKey)
	u.Path = fmt.Sprintf("/%d", p.ID)
	return u.String(), nil
}
