// Package apikeys manages Bearer tokens for automation: SDK ingestion
// without embedding a DSN key, and management-API access without a session
// cookie. Only the SHA256 hash ever touches the database - the secret is
// shown once at creation (and again at rotation) and then unrecoverable.
//
// Scopes are deliberately coarse: ingest (event ingestion), read (GET API),
// write (mutating API). A key is optionally bound to one project; a bound
// key is refused anywhere outside that project's ingestion path and
// project-scoped reads.
package apikeys

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"errgotrack/internal/database"
)

const (
	ScopeIngest = "ingest"
	ScopeRead   = "read"
	ScopeWrite  = "write"
)

// Prefix identifies the secret to its owner in listings ("egt_a1b2c3d4").
const secretPrefix = "egt_"

var validScopes = map[string]bool{ScopeIngest: true, ScopeRead: true, ScopeWrite: true}

type Key struct {
	ID        int64         `json:"id"`
	Name      string        `json:"name"`
	Prefix    string        `json:"prefix"`
	ProjectID sql.NullInt64 `json:"-"`
	Scopes    []string      `json:"scopes"`
	ExpiresAt string        `json:"expiresAt"`
	Revoked   bool          `json:"revoked"`
	LastUsed  string        `json:"lastUsedAt"`
	CreatedAt time.Time     `json:"createdAt"`
	// Secret is populated only at creation/rotation time, never on read.
	Secret string `json:"secret,omitempty"`
}

// ProjectIDJSON renders project_id as null-or-int for the API.
func (k Key) ProjectIDJSON() any {
	if k.ProjectID.Valid {
		return k.ProjectID.Int64
	}
	return nil
}

func (k Key) HasScope(scope string) bool {
	for _, s := range k.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Expired reports whether the key is past its expiry ("" = never).
func (k Key) Expired() bool {
	if k.ExpiresAt == "" {
		return false
	}
	exp, err := time.Parse(time.RFC3339, k.ExpiresAt)
	if err != nil {
		return true
	}
	return time.Now().After(exp)
}

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func newSecret() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return secretPrefix + hex.EncodeToString(buf), nil
}

func cleanScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return []string{ScopeRead}, nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if !validScopes[s] {
			return nil, fmt.Errorf("unknown scope %q (want ingest, read, write)", s)
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out, nil
}

func encodeScopes(scopes []string) string {
	b, _ := json.Marshal(scopes)
	return string(b)
}

// Create mints a key. expiresAt "" means never expires. The returned Key
// carries Secret exactly once - callers must show it now or lose it.
func (s *Store) Create(name string, projectID sql.NullInt64, scopes []string, expiresAt string) (Key, error) {
	clean, err := cleanScopes(scopes)
	if err != nil {
		return Key{}, err
	}
	if expiresAt != "" {
		if _, err := time.Parse(time.RFC3339, expiresAt); err != nil {
			return Key{}, fmt.Errorf("expiresAt must be RFC3339 (or empty for never): %w", err)
		}
	}
	secret, err := newSecret()
	if err != nil {
		return Key{}, err
	}
	var project any
	if projectID.Valid {
		project = projectID.Int64
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if s.db.IsPostgres() {
		var id int64
		err := s.db.QueryRow(`
			INSERT INTO api_keys (name, key_hash, key_prefix, project_id, scopes, expires_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			name, hashSecret(secret), secret[:12], project, encodeScopes(clean), expiresAt, now).Scan(&id)
		if err != nil {
			return Key{}, err
		}
		k, err := s.get(id)
		k.Secret = secret
		return k, err
	}
	res, err := s.db.Exec(`
		INSERT INTO api_keys (name, key_hash, key_prefix, project_id, scopes, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		name, hashSecret(secret), secret[:12], project, encodeScopes(clean), expiresAt, now)
	if err != nil {
		return Key{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Key{}, err
	}
	k, err := s.get(id)
	k.Secret = secret
	return k, err
}

func scanKey(row interface{ Scan(...any) error }) (Key, error) {
	var k Key
	var scopes, created string
	var revoked int
	err := row.Scan(&k.ID, &k.Name, &k.Prefix, &k.ProjectID, &scopes, &k.ExpiresAt, &revoked, &k.LastUsed, &created)
	if err != nil {
		return k, err
	}
	k.Revoked = revoked != 0
	if scopes != "" {
		_ = json.Unmarshal([]byte(scopes), &k.Scopes)
	}
	k.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return k, nil
}

const keyCols = `id, name, key_prefix, project_id, scopes, expires_at, revoked, last_used_at, created_at`

func (s *Store) get(id int64) (Key, error) {
	k, err := scanKey(s.db.QueryRow(`SELECT `+keyCols+` FROM api_keys WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return k, sql.ErrNoRows
	}
	return k, err
}

func (s *Store) List() ([]Key, error) {
	rows, err := s.db.Query(`SELECT ` + keyCols + ` FROM api_keys ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Key
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// Validate checks a presented secret and returns its key if live
// (exists, not revoked, not expired). The hash comparison is constant-time
// so a wrong secret costs the same as a right one.
func (s *Store) Validate(secret string) (Key, error) {
	if secret == "" {
		return Key{}, errors.New("empty secret")
	}
	want := hashSecret(secret)
	rows, err := s.db.Query(`SELECT ` + keyCols + `, key_hash FROM api_keys WHERE revoked = 0`)
	if err != nil {
		return Key{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var k Key
		var scopes, created, gotHash string
		var revoked int
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.ProjectID, &scopes, &k.ExpiresAt, &revoked, &k.LastUsed, &created, &gotHash); err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(gotHash), []byte(want)) != 1 {
			continue
		}
		k.Revoked = revoked != 0
		if scopes != "" {
			_ = json.Unmarshal([]byte(scopes), &k.Scopes)
		}
		k.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		if k.Expired() {
			return Key{}, errors.New("key expired")
		}
		return k, rows.Err()
	}
	return Key{}, errors.New("invalid key")
}

func (s *Store) Revoke(id int64) error {
	_, err := s.db.Exec(`UPDATE api_keys SET revoked = 1 WHERE id = ?`, id)
	return err
}

func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM api_keys WHERE id = ?`, id)
	return err
}

// Rotate revokes the old key and mints a replacement with identical
// attributes, returning the new secret (shown once, as at creation).
func (s *Store) Rotate(id int64) (Key, error) {
	old, err := s.get(id)
	if err != nil {
		return Key{}, err
	}
	var expires string
	if old.ExpiresAt != "" {
		expires = old.ExpiresAt
	}
	k, err := s.Create(old.Name, old.ProjectID, old.Scopes, expires)
	if err != nil {
		return Key{}, err
	}
	if err := s.Revoke(id); err != nil {
		return Key{}, err
	}
	return k, nil
}

// BearerToken extracts a "Bearer <secret>" API key from the request, or ""
// when absent.
func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	rest, ok := strings.CutPrefix(h, "Bearer ")
	if !ok || strings.TrimSpace(rest) == "" {
		return ""
	}
	return strings.TrimSpace(rest)
}

// TouchLastUsed records use in RFC3339 (second precision is plenty for a
// "last seen" column and keeps the value fixed-width).
func (s *Store) TouchLastUsed(id int64) {
	_, _ = s.db.Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id)
}
