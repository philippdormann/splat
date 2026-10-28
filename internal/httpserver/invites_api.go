package httpserver

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/philippdormann/splat/internal/audit"
	"github.com/philippdormann/splat/internal/auth"
	"github.com/philippdormann/splat/internal/invites"
)

// --- invitations (admin session only, except accept) ---

func inviteJSON(inv invites.Invitation) map[string]any {
	return map[string]any{
		"token": inv.Token, "email": inv.Email, "username": inv.Username,
		"role": inv.Role, "status": inv.Status, "expiresAt": inv.ExpiresAt,
		"createdAt": inv.CreatedAt, "acceptedAt": inv.AcceptedAt, "invitedBy": inv.InvitedBy,
	}
}

func (s *Server) apiInvitationsCreate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	me, _ := s.Auth.ValidateSession(auth.SessionTokenFromRequest(r))
	inv, err := s.Invites.Create(input.Email, input.Username, input.Role, me)
	if err != nil {
		msg := err.Error()
		status := http.StatusBadRequest
		if strings.Contains(msg, "already exists") || strings.Contains(msg, "already taken") {
			status = http.StatusConflict
		}
		writeAPIError(w, status, msg)
		return
	}
	s.audit(r, audit.InviteCreated, "invitation", inv.Token, `{"email":"`+inv.Email+`"}`)
	// Optional SMTP email: best-effort, never fails creation. The token is
	// always returned so the admin can copy the link manually.
	if err := s.sendInviteEmail(inv); err != nil {
		s.Logger.Error("send invite email", "error", err, "email", inv.Email)
	}
	writeJSON(w, http.StatusCreated, inviteJSON(inv))
}

func (s *Server) apiInvitationsList(w http.ResponseWriter, r *http.Request) {
	list, err := s.Invites.List()
	if err != nil {
		s.Logger.Error("list invitations", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, inv := range list {
		out = append(out, inviteJSON(inv))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) apiInvitationRevoke(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Invites.Revoke(token); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "pending invitation not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.InviteRevoked, "invitation", token, "{}")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Public: invitation details for the accept page (no auth).
func (s *Server) apiInvitationGet(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	inv, err := s.Invites.Get(token)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "invitation not found")
		return
	}
	if !invites.IsAcceptable(inv.Status, inv.ExpiresAt, time.Now()) {
		writeAPIError(w, http.StatusGone, "invitation is expired or already used")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"email": inv.Email, "username": inv.Username, "role": inv.Role, "expiresAt": inv.ExpiresAt,
	})
}

// Public: accept an invitation by choosing a password (+ username override).
func (s *Server) apiInvitationAccept(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token    string `json:"token"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(r, &input) || input.Token == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	inv, err := s.Invites.Get(input.Token)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid invitation token")
		return
	}
	if !invites.IsAcceptable(inv.Status, inv.ExpiresAt, time.Now()) {
		writeAPIError(w, http.StatusBadRequest, "invitation is expired or already used")
		return
	}
	username := strings.TrimSpace(input.Username)
	if username == "" {
		username = inv.Username
	}
	if len(input.Password) < 8 {
		writeAPIError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	role := inv.Role
	if role != auth.RoleAdmin && role != auth.RoleMember {
		role = auth.RoleMember
	}
	u, err := s.Auth.CreateUser(username, input.Password, role)
	if err != nil {
		writeAPIError(w, http.StatusConflict, err.Error())
		return
	}
	if inv.Email != "" {
		// Invited email wins over caller input (anti-spoof): link it,
		// ignore conflicts only if the user row already carries it.
		_ = s.Auth.SetEmail(u.ID, inv.Email)
	}
	// Consume atomically: only a still-pending invite flips; otherwise roll back.
	res, err := s.DB.Exec(`UPDATE invitations SET status = 'accepted', accepted_at = ? WHERE token = ? AND status = 'pending'`,
		time.Now().UTC().Format(time.RFC3339Nano), input.Token)
	if err != nil {
		_ = s.Auth.DeleteUser(u.ID)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = s.Auth.DeleteUser(u.ID)
		writeAPIError(w, http.StatusBadRequest, "invitation is expired or already used")
		return
	}
	s.auditAs(u.ID, u.Username, clientIP(r), audit.InviteAccepted, "invitation", input.Token, "{}")
	token, expires, err := s.Auth.CreateSession(u.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = s.Auth.TouchLastLogin(u.ID)
	auth.SetSessionCookie(w, token, expires, s.CookieSecure)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": u.Username})
}

// sendInviteEmail is best-effort: uses the first enabled SMTP integration
// for a plain-text invite. Returns nil when no SMTP is configured.
func (s *Server) sendInviteEmail(inv invites.Invitation) error {
	if s.Notifications == nil {
		return nil
	}
	link := strings.TrimSuffix(s.PublicURL, "/") + "/invite/" + inv.Token
	if s.PublicURL == "" {
		link = "/invite/" + inv.Token
	}
	return s.Notifications.SendInviteEmail(inv.Email, inv.Username, link)
}
