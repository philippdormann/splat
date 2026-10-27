package httpserver

import (
	"net/http"
	"strconv"

	"github.com/philippdormann/splat/internal/audit"
	"github.com/philippdormann/splat/internal/auth"
)

// --- audit log ---
//
// Management and authentication actions append one line each (see
// internal/audit). Recording never blocks the action: failures are
// logged server-side and the request continues.

// audit records an action by the session owner (or anonymous when no
// session resolves, e.g. a rejected request).
func (s *Server) audit(r *http.Request, action, entityType, entityID, detail string) {
	var userID int64
	var username string
	if token := auth.SessionTokenFromRequest(r); token != "" {
		if id, ok := s.Auth.ValidateSession(token); ok {
			userID = id
			username = s.Auth.UsernameOf(id)
		}
	}
	s.auditAs(userID, username, clientIP(r), action, entityType, entityID, detail)
}

// auditAs records an action with an explicit actor (failed logins name
// the claimed username with userID 0).
func (s *Server) auditAs(userID int64, username, ip, action, entityType, entityID, detail string) {
	if s.Audit == nil {
		return
	}
	if detail == "" {
		detail = "{}"
	}
	if err := s.Audit.Record(userID, username, action, entityType, entityID, detail, ip); err != nil {
		s.Logger.Error("audit record", "error", err, "action", action)
	}
}

func auditEntryJSON(e audit.Entry) map[string]any {
	return map[string]any{
		"id": e.ID, "createdAt": e.CreatedAt,
		"actorUserId": e.ActorUserID, "actorUsername": e.ActorUsername,
		"action": e.Action, "entityType": e.EntityType, "entityId": e.EntityID,
		"detail": e.Detail, "ip": e.IP,
	}
}

func (s *Server) apiAuditList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 50
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		limit = n
	}
	offset := 0
	if n, err := strconv.Atoi(q.Get("offset")); err == nil && n > 0 {
		offset = n
	}
	entries, total, err := s.Audit.List(limit, offset, q.Get("action"))
	if err != nil {
		s.Logger.Error("list audit log", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, auditEntryJSON(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "total": total})
}
