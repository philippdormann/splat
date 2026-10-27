package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
)

// SSO identity: users carry an email (matching/linking) and an
// issuer+subject pair (login lookup). Password-only users keep NULLs,
// which never conflict in the unique indexes.

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// FindBySSO returns the user linked to an issuer+subject pair.
func (s *Store) FindBySSO(issuer, subject string) (User, error) {
	var u User
	var email sql.NullString
	var sso int
	if s.db.IsPostgres() {
		var linked bool
		err := s.db.QueryRow(`SELECT id, username, role, created_at, email, (sso_subject IS NOT NULL) FROM users WHERE sso_issuer = ? AND sso_subject = ?`,
			issuer, subject).Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &email, &linked)
		if err != nil {
			return User{}, err
		}
		u.TOTPEnabled = s.TOTPEnabled(u.ID)
		u.SSO = linked
	} else {
		err := s.db.QueryRow(`SELECT id, username, role, created_at, email, (sso_subject IS NOT NULL) FROM users WHERE sso_issuer = ? AND sso_subject = ?`,
			issuer, subject).Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &email, &sso)
		if err != nil {
			return User{}, err
		}
		u.TOTPEnabled = s.TOTPEnabled(u.ID)
		u.SSO = sso != 0
	}
	u.Email = email.String
	return u, nil
}

// FindByEmail returns the user with an email address (case-insensitive).
func (s *Store) FindByEmail(email string) (User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return User{}, sql.ErrNoRows
	}
	var u User
	var em sql.NullString
	var sso int
	if s.db.IsPostgres() {
		var linked bool
		err := s.db.QueryRow(`SELECT id, username, role, created_at, email, (sso_subject IS NOT NULL) FROM users WHERE email = ?`,
			email).Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &em, &linked)
		if err != nil {
			return User{}, err
		}
		u.SSO = linked
	} else {
		err := s.db.QueryRow(`SELECT id, username, role, created_at, email, (sso_subject IS NOT NULL) FROM users WHERE email = ?`,
			email).Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &em, &sso)
		if err != nil {
			return User{}, err
		}
		u.SSO = sso != 0
	}
	u.Email = em.String
	u.TOTPEnabled = s.TOTPEnabled(u.ID)
	return u, nil
}

// SetEmail stores (or clears) a user's email address.
func (s *Store) SetEmail(userID int64, email string) error {
	email = normalizeEmail(email)
	_, err := s.db.Exec(`UPDATE users SET email = ? WHERE id = ?`, nullIfEmpty(email), userID)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return errors.New("email already in use")
	}
	return err
}

// LinkSSO attaches an SSO identity (and email) to a user. Unique
// violations mean the subject or email belongs to someone else.
func (s *Store) LinkSSO(userID int64, email, issuer, subject string) error {
	email = normalizeEmail(email)
	_, err := s.db.Exec(`UPDATE users SET email = ?, sso_issuer = ?, sso_subject = ? WHERE id = ?`,
		nullIfEmpty(email), issuer, subject, userID)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return errors.New("this login is already linked to another user")
	}
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// CreateSSOUser provisions a member for first-time SSO login. The
// password hash is random and unusable: the account only works via SSO
// (an admin can still set a password for it later).
func (s *Store) CreateSSOUser(username, email, issuer, subject string) (User, error) {
	pw := make([]byte, 24)
	if _, err := rand.Read(pw); err != nil {
		return User{}, err
	}
	u, err := s.CreateUser(username, hex.EncodeToString(pw), RoleMember)
	if err != nil {
		return User{}, err
	}
	if err := s.LinkSSO(u.ID, email, issuer, subject); err != nil {
		return User{}, err
	}
	u.Email = normalizeEmail(email)
	u.SSO = true
	return u, nil
}

// UsernameTaken reports whether a username exists (for SSO deduping).
func (s *Store) UsernameTaken(username string) bool {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = ?`, username).Scan(&n)
	return n > 0
}

// PasswordLoginDisabled reports whether /login (password) is turned off
// instance-wide (SSO-only mode).
func (s *Store) PasswordLoginDisabled() bool {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'password_login_disabled'`).Scan(&v); err != nil {
		return false
	}
	return v == "1" || strings.EqualFold(v, "true")
}

// SetPasswordLoginDisabled toggles SSO-only mode.
func (s *Store) SetPasswordLoginDisabled(disabled bool) error {
	v := "0"
	if disabled {
		v = "1"
	}
	if s.db.IsPostgres() {
		_, err := s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('password_login_disabled', ?)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, v)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('password_login_disabled', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, v)
	return err
}
