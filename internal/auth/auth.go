// Package auth implements admin login (single or few operators, not a
// multi-tenant user system), session cookies, and CSRF protection - enough
// for a private dashboard on a VPS, not an identity provider.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

const sessionCookieName = "errgotrack_session"
const sessionDuration = 30 * 24 * time.Hour

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// EnsureAdmin creates the admin user from env-provided credentials if no
// admin exists yet. Safe to call on every startup.
func (s *Store) EnsureAdmin(username, password string) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM admin_users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if username == "" || password == "" {
		return errors.New("no admin user exists and ERRGOTRACK_ADMIN_USERNAME/ERRGOTRACK_ADMIN_PASSWORD are not set")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO admin_users (username, password_hash) VALUES (?, ?)`, username, string(hash))
	return err
}

func (s *Store) Authenticate(username, password string) (int64, error) {
	var id int64
	var hash string
	err := s.db.QueryRow(`SELECT id, password_hash FROM admin_users WHERE username = ?`, username).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		// Still run bcrypt to keep timing similar whether or not the username exists.
		bcrypt.CompareHashAndPassword([]byte("$2a$10$........................................................"), []byte(password))
		return 0, ErrInvalidCredentials
	}
	if err != nil {
		return 0, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return 0, ErrInvalidCredentials
	}
	return id, nil
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (s *Store) CreateSession(userID int64) (token string, expires time.Time, err error) {
	token, err = randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires = time.Now().Add(sessionDuration)
	_, err = s.db.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`,
		token, userID, expires.UTC().Format(time.RFC3339))
	return token, expires, err
}

func (s *Store) ValidateSession(token string) (userID int64, ok bool) {
	if token == "" {
		return 0, false
	}
	var expiresAt string
	err := s.db.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE token = ?`, token).Scan(&userID, &expiresAt)
	if err != nil {
		return 0, false
	}
	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || time.Now().After(exp) {
		return 0, false
	}
	return userID, true
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func SetSessionCookie(w http.ResponseWriter, token string, expires time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func SessionTokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// CSRF: a double-submit token stored in a non-HttpOnly cookie and echoed
// back in a hidden form field / HTMX header on state-changing requests.
const csrfCookieName = "errgotrack_csrf"

func EnsureCSRFCookie(w http.ResponseWriter, r *http.Request, secure bool) string {
	if c, err := r.Cookie(csrfCookieName); err == nil && c.Value != "" {
		return c.Value
	}
	token, err := randomToken()
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:  csrfCookieName,
		Value: token,
		Path:  "/",
		// The token reaches forms via server-rendered hidden inputs, never
		// via JS reading document.cookie, so HttpOnly costs nothing here
		// and denies an XSS payload one more way to read application state.
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
	return token
}

func ValidCSRF(r *http.Request, submitted string) bool {
	c, err := r.Cookie(csrfCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(submitted)) == 1
}
