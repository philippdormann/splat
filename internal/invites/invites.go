// Package invites implements token invitations for team onboarding.
//
// Flow (mirrors rustrak v1): an admin creates a pending invite with an
// email + username + role; the frontend builds /invite/<token> and copies
// it to the clipboard (manual share, plus optional SMTP email). The
// invitee opens the link and sets a password, which creates the user with
// the invite's email/role and marks the invite accepted. 48h TTL,
// single-use, fail-closed on unknown status.
package invites

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/philippdormann/splat/internal/database"
)

const TTL = 48 * time.Hour

type Invitation struct {
	Token      string `json:"token"`
	Email      string `json:"email"`
	Username   string `json:"username"`
	Role       string `json:"role"`
	Status     string `json:"status"`
	ExpiresAt  string `json:"expiresAt"`
	CreatedAt  string `json:"createdAt"`
	AcceptedAt string `json:"acceptedAt,omitempty"`
	InvitedBy  int64  `json:"invitedBy,omitempty"`
}

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func normalizeUsername(username string) string {
	return strings.TrimSpace(username)
}

func generateToken() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func validRole(role string) bool { return role == "admin" || role == "member" }

func isValidEmail(email string) bool {
	email = strings.TrimSpace(email)
	at := strings.Index(email, "@")
	return at > 0 && strings.Contains(email[at:], ".")
}

// IsAcceptable reports whether an invite can still be redeemed.
func IsAcceptable(status, expiresAt string, now time.Time) bool {
	if status != "pending" {
		return false
	}
	exp, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		if exp, err = time.Parse(time.RFC3339, expiresAt); err != nil {
			return false
		}
	}
	return exp.After(now)
}

func (s *Store) Create(email, username, role string, invitedBy int64) (Invitation, error) {
	email = normalizeEmail(email)
	username = normalizeUsername(username)
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		role = "member"
	}
	if !validRole(role) {
		return Invitation{}, errors.New("role must be admin or member")
	}
	if !isValidEmail(email) {
		return Invitation{}, errors.New("invalid email format")
	}
	if username == "" {
		return Invitation{}, errors.New("username is required")
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = ?`, username).Scan(&n); err == nil && n > 0 {
		return Invitation{}, errors.New("username already taken")
	}
	var em string
	if err := s.db.QueryRow(`SELECT email FROM users WHERE email = ?`, email).Scan(&em); err == nil && em != "" {
		return Invitation{}, errors.New("a user with that email already exists")
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Invitation{}, err
	}
	var existing string
	if err := s.db.QueryRow(`SELECT token FROM invitations WHERE email = ? AND status = 'pending'`, email).Scan(&existing); err == nil {
		return Invitation{}, errors.New("a pending invitation for that email already exists")
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Invitation{}, err
	}
	if err := s.db.QueryRow(`SELECT token FROM invitations WHERE username = ? AND status = 'pending'`, username).Scan(&existing); err == nil {
		return Invitation{}, errors.New("a pending invitation for that username already exists")
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Invitation{}, err
	}
	token, err := generateToken()
	if err != nil {
		return Invitation{}, err
	}
	expiresAt := time.Now().Add(TTL).UTC().Format(time.RFC3339Nano)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var createdAt string
	if s.db.IsPostgres() {
		err = s.db.QueryRow(`INSERT INTO invitations (token, email, username, role, status, expires_at, invited_by, created_at)
			VALUES (?, ?, ?, ?, 'pending', ?, ?, ?) RETURNING created_at`,
			token, email, username, role, expiresAt, invitedBy, now).Scan(&createdAt)
	} else {
		_, err = s.db.Exec(`INSERT INTO invitations (token, email, username, role, status, expires_at, invited_by, created_at)
			VALUES (?, ?, ?, ?, 'pending', ?, ?, ?)`,
			token, email, username, role, expiresAt, invitedBy, now)
		createdAt = now
	}
	if err != nil {
		return Invitation{}, err
	}
	return Invitation{Token: token, Email: email, Username: username, Role: role, Status: "pending", ExpiresAt: expiresAt, CreatedAt: createdAt, InvitedBy: invitedBy}, nil
}

func scanInvitation(row interface {
	Scan(dest ...any) error
}) (Invitation, error) {
	var inv Invitation
	var invitedBy sql.NullInt64
	var acceptedAt sql.NullString
	if err := row.Scan(&inv.Token, &inv.Email, &inv.Username, &inv.Role, &inv.Status, &inv.ExpiresAt, &invitedBy, &inv.CreatedAt, &acceptedAt); err != nil {
		return Invitation{}, err
	}
	if invitedBy.Valid {
		inv.InvitedBy = invitedBy.Int64
	}
	if acceptedAt.Valid {
		inv.AcceptedAt = acceptedAt.String
	}
	return inv, nil
}

func (s *Store) Get(token string) (Invitation, error) {
	return scanInvitation(s.db.QueryRow(`SELECT token, email, username, role, status, expires_at, invited_by, created_at, accepted_at FROM invitations WHERE token = ?`, token))
}

func (s *Store) List() ([]Invitation, error) {
	rows, err := s.db.Query(`SELECT token, email, username, role, status, expires_at, invited_by, created_at, accepted_at FROM invitations ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invitation{}
	for rows.Next() {
		var inv Invitation
		var invitedBy sql.NullInt64
		var acceptedAt sql.NullString
		if err := rows.Scan(&inv.Token, &inv.Email, &inv.Username, &inv.Role, &inv.Status, &inv.ExpiresAt, &invitedBy, &inv.CreatedAt, &acceptedAt); err != nil {
			return nil, err
		}
		if invitedBy.Valid {
			inv.InvitedBy = invitedBy.Int64
		}
		if acceptedAt.Valid {
			inv.AcceptedAt = acceptedAt.String
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *Store) Revoke(token string) error {
	res, err := s.db.Exec(`UPDATE invitations SET status = 'revoked' WHERE token = ? AND status = 'pending'`, token)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
