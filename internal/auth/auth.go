// Package auth implements multi-user login with instance roles, session
// cookies, and CSRF protection - enough for a private dashboard on a VPS,
// not an identity provider. Project-level access (direct membership or via
// teams) lives in internal/teams; this package owns identity.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/philippdormann/splat/internal/database"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCredentials = errors.New("invalid username or password")

// Instance roles: admins bypass every access check; members only see
// projects granted to them directly or through a team.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

func validRole(r string) bool { return r == RoleAdmin || r == RoleMember }

const sessionCookieName = "splat_session"
const sessionDuration = 30 * 24 * time.Hour

type Store struct {
	db *database.DB
	// sessionSecret signs login challenges and encrypts TOTP secrets
	// at rest (see totp.go). Empty fails 2FA operations closed.
	sessionSecret string
}

func NewStore(db *database.DB, sessionSecret string) *Store {
	return &Store{db: db, sessionSecret: sessionSecret}
}

// EnsureAdmin creates the admin user from env-provided credentials if no
// users exist yet. Safe to call on every startup.
func (s *Store) EnsureAdmin(username, password string) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if username == "" || password == "" {
		return errors.New("no users exist and SPLAT_ADMIN_USERNAME/SPLAT_ADMIN_PASSWORD are not set")
	}
	_, err := s.CreateUser(username, password, RoleAdmin)
	return err
}

func (s *Store) Authenticate(username, password string) (int64, error) {
	var id int64
	var hash string
	err := s.db.QueryRow(`SELECT id, password_hash FROM users WHERE username = ?`, username).Scan(&id, &hash)
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

// --- user management (admin callers only; enforced in httpserver) ---

type User struct {
	ID          int64
	Username    string
	Role        string
	CreatedAt   string
	TOTPEnabled bool
	Email       string
	SSO         bool
}

// RoleOf returns the instance role for a user id ("admin"/"member").
// Unknown users report member so callers fail closed.
func (s *Store) RoleOf(userID int64) string {
	var role string
	if err := s.db.QueryRow(`SELECT role FROM users WHERE id = ?`, userID).Scan(&role); err != nil {
		return RoleMember
	}
	if !validRole(role) {
		return RoleMember
	}
	return role
}

func (s *Store) UsernameOf(userID int64) string {
	var username string
	if err := s.db.QueryRow(`SELECT username FROM users WHERE id = ?`, userID).Scan(&username); err != nil {
		return ""
	}
	return username
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, role, created_at, email, (sso_subject IS NOT NULL),
		EXISTS(SELECT 1 FROM user_totp WHERE user_id = users.id AND enabled) AS totp_enabled
		FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		var email sql.NullString
		if s.db.IsPostgres() {
			var sso, totp bool
			if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &email, &sso, &totp); err != nil {
				return nil, err
			}
			u.SSO, u.TOTPEnabled = sso, totp
		} else {
			var sso, enabled int
			if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &email, &sso, &enabled); err != nil {
				return nil, err
			}
			u.SSO, u.TOTPEnabled = sso != 0, enabled != 0
		}
		u.Email = email.String
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CreateUser(username, password, role string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return User{}, errors.New("username is required")
	}
	if len(password) < 8 {
		return User{}, errors.New("password must be at least 8 characters")
	}
	if !validRole(role) {
		return User{}, errors.New("role must be admin or member")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	if s.db.IsPostgres() {
		var u User
		err := s.db.QueryRow(
			`INSERT INTO users (username, password_hash, role) VALUES (?, ?, ?) RETURNING id, username, role, created_at`,
			username, string(hash), role).Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt)
		if err != nil {
			return User{}, userCreateErr(err)
		}
		return u, nil
	}
	res, err := s.db.Exec(`INSERT INTO users (username, password_hash, role) VALUES (?, ?, ?)`,
		username, string(hash), role)
	if err != nil {
		return User{}, userCreateErr(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return User{ID: id, Username: username, Role: role}, nil
}

func userCreateErr(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return errors.New("username already taken")
	}
	return err
}

func (s *Store) DeleteUser(id int64) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return errors.New("cannot delete the last user")
	}
	_, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

func (s *Store) SetRole(id int64, role string) error {
	if !validRole(role) {
		return errors.New("role must be admin or member")
	}
	// Admins are never demoted: with no per-project roles, demotion only
	// ever removes instance administration, which is destructive and
	// irreversible for the affected account. Remove and recreate the user
	// instead. (This also implies the last admin can never be demoted.)
	if role != RoleAdmin {
		var current string
		if err := s.db.QueryRow(`SELECT role FROM users WHERE id = ?`, id).Scan(&current); err != nil {
			return err
		}
		if current == RoleAdmin {
			return errors.New("admins cannot be demoted")
		}
	}
	_, err := s.db.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id)
	return err
}

func (s *Store) SetPassword(id int64, password string) error {
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), id)
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
const csrfCookieName = "splat_csrf"

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
