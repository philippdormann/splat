// Package audit records authentication and management actions in an
// append-only log. Entries are written by the HTTP layer (one line per
// action) and read back by admins; nothing is ever updated or swept.
package audit

import (
	"github.com/philippdormann/splat/internal/database"
)

// Actions recorded in the log. Dotted names group by area for filtering.
const (
	Login             = "auth.login"
	LoginFailed       = "auth.login_failed"
	Logout            = "auth.logout"
	TOTPEnabled       = "auth.2fa_enabled"
	TOTPDisabled      = "auth.2fa_disabled"
	TOTPCodesRolled   = "auth.2fa_codes_regenerated"
	TOTPReset         = "users.2fa_reset"
	UserCreated       = "users.created"
	UserDeleted       = "users.deleted"
	UserRoleChanged   = "users.role_changed"
	UserPassReset     = "users.password_reset"
	InviteCreated     = "invites.created"
	InviteAccepted    = "invites.accepted"
	InviteRevoked     = "invites.revoked"
	TeamCreated       = "teams.created"
	TeamDeleted       = "teams.deleted"
	TeamMemberAdded   = "teams.member_added"
	TeamMemberShed    = "teams.member_removed"
	TeamProjGranted   = "teams.project_granted"
	TeamProjRevoked   = "teams.project_revoked"
	ProjMemberAdded   = "projects.member_added"
	ProjMemberShed    = "projects.member_removed"
	ProjectCreated    = "projects.created"
	ProjectUpdated    = "projects.updated"
	ProjectKeyRotated = "projects.key_rotated"
	ProjectDeleted    = "projects.deleted"
	SettingsSaved     = "settings.saved"
	RuleChanged       = "rules.changed"
	APIKeyChanged     = "api_keys.changed"
	SecurityChanged   = "security.policy_changed"
	QuotaChanged      = "projects.quota_changed"
	SSOLogin          = "auth.sso_login"
	SSOLinked         = "users.sso_linked"
)

type Store struct{ db *database.DB }

func NewStore(db *database.DB) *Store { return &Store{db: db} }

type Entry struct {
	ID            int64
	CreatedAt     string
	ActorUserID   int64
	ActorUsername string
	Action        string
	EntityType    string
	EntityID      string
	Detail        string
	IP            string
}

// Record appends one entry. actorUserID 0 means no authenticated actor
// (failed login, system); actorUsername then carries the claimed name.
// The id is denormalized history, not a relation: recording never fails
// because of user rows.
func (s *Store) Record(actorUserID int64, actorUsername, action, entityType, entityID, detail, ip string) error {
	_, err := s.db.Exec(`INSERT INTO audit_log (actor_user_id, actor_username, action, entity_type, entity_id, detail, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, actorUserID, actorUsername, action, entityType, entityID, detail, ip)
	return err
}

// List returns entries newest-first with an optional action-prefix
// filter ("auth." matches the whole auth group).
func (s *Store) List(limit, offset int, actionPrefix string) ([]Entry, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	where := ""
	var args []any
	if actionPrefix != "" {
		where = `WHERE action LIKE ? || '%'`
		args = append(args, actionPrefix)
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_log `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT id, created_at, actor_user_id, actor_username, action, entity_type, entity_id, detail, ip
		FROM audit_log `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.ActorUserID, &e.ActorUsername, &e.Action, &e.EntityType, &e.EntityID, &e.Detail, &e.IP); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
