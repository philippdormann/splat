package httpserver

import (
	"net/http"
	"strconv"

	"github.com/philippdormann/splat/internal/audit"
	"github.com/philippdormann/splat/internal/auth"
)

// --- users & teams (admin session only) ---
//
// Instance admins manage users, teams, and project grants here. Members
// never reach these endpoints (requireAdmin); they only ever see projects
// granted to them directly or through a team.

func rbacUserJSON(u auth.User) map[string]any {
	return map[string]any{"id": u.ID, "username": u.Username, "role": u.Role, "createdAt": u.CreatedAt, "totpEnabled": u.TOTPEnabled}
}

func (s *Server) apiUsersList(w http.ResponseWriter, r *http.Request) {
	users, err := s.Auth.ListUsers()
	if err != nil {
		s.Logger.Error("list users", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(users))
	for _, u := range users {
		out = append(out, rbacUserJSON(u))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) apiUsersCreate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if input.Role == "" {
		input.Role = auth.RoleMember
	}
	u, err := s.Auth.CreateUser(input.Username, input.Password, input.Role)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.UserCreated, "user", strconv.FormatInt(u.ID, 10), `{"role":"`+u.Role+`"}`)
	writeJSON(w, http.StatusCreated, rbacUserJSON(u))
}

func (s *Server) apiUsersDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	// Admins cannot delete themselves out from under their own session.
	if me, ok := s.Auth.ValidateSession(auth.SessionTokenFromRequest(r)); ok && me == id {
		writeAPIError(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}
	if err := s.Auth.DeleteUser(id); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.UserDeleted, "user", strconv.FormatInt(id, 10), "{}")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiUsersSetRole(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input struct {
		Role string `json:"role"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// Nobody changes their own role through this endpoint: admins keep a
	// working session and can't lock themselves out by accident.
	if me, ok := s.Auth.ValidateSession(auth.SessionTokenFromRequest(r)); ok && me == id {
		writeAPIError(w, http.StatusBadRequest, "cannot change your own role")
		return
	}
	if err := s.Auth.SetRole(id, input.Role); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.UserRoleChanged, "user", strconv.FormatInt(id, 10), `{"role":"`+input.Role+`"}`)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiUsersSetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.Auth.SetPassword(id, input.Password); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.UserPassReset, "user", strconv.FormatInt(id, 10), "{}")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func teamJSON(id int64, name, slug, createdAt string) map[string]any {
	return map[string]any{"id": id, "name": name, "slug": slug, "createdAt": createdAt}
}

func (s *Server) apiTeamsList(w http.ResponseWriter, r *http.Request) {
	teams, err := s.Teams.List()
	if err != nil {
		s.Logger.Error("list teams", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(teams))
	for _, t := range teams {
		members, _ := s.Teams.TeamMembers(t.ID)
		projects, _ := s.Teams.TeamProjects(t.ID)
		m := teamJSON(t.ID, t.Name, t.Slug, t.CreatedAt)
		m["memberIds"] = members
		m["projectIds"] = projects
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) apiTeamsCreate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	t, err := s.Teams.Create(input.Name, input.Slug)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, audit.TeamCreated, "team", strconv.FormatInt(t.ID, 10), "{}")
	m := teamJSON(t.ID, t.Name, t.Slug, t.CreatedAt)
	m["memberIds"] = []int64{}
	m["projectIds"] = []int64{}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) apiTeamsDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Teams.Delete(id); err != nil {
		s.Logger.Error("delete team", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.TeamDeleted, "team", strconv.FormatInt(id, 10), "{}")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiTeamAddMember(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input struct {
		UserID int64 `json:"userId"`
	}
	if !decodeJSON(r, &input) || input.UserID == 0 {
		writeAPIError(w, http.StatusBadRequest, "userId is required")
		return
	}
	if err := s.Teams.AddMember(teamID, input.UserID); err != nil {
		s.Logger.Error("add team member", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.TeamMemberAdded, "team", strconv.FormatInt(teamID, 10), `{"userId":`+strconv.FormatInt(input.UserID, 10)+`}`)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiTeamRemoveMember(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	userID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Teams.RemoveMember(teamID, userID); err != nil {
		s.Logger.Error("remove team member", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.TeamMemberShed, "team", strconv.FormatInt(teamID, 10), `{"userId":`+strconv.FormatInt(userID, 10)+`}`)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiTeamGrantProject(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input struct {
		ProjectID int64 `json:"projectId"`
	}
	if !decodeJSON(r, &input) || input.ProjectID == 0 {
		writeAPIError(w, http.StatusBadRequest, "projectId is required")
		return
	}
	if _, err := s.Projects.Get(input.ProjectID); err != nil {
		writeAPIError(w, http.StatusBadRequest, "unknown project")
		return
	}
	if err := s.Teams.GrantProject(teamID, input.ProjectID); err != nil {
		s.Logger.Error("grant project to team", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.TeamProjGranted, "team", strconv.FormatInt(teamID, 10), `{"projectId":`+strconv.FormatInt(input.ProjectID, 10)+`}`)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiTeamRevokeProject(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	projectID, err := strconv.ParseInt(r.PathValue("projectId"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Teams.RevokeProject(teamID, projectID); err != nil {
		s.Logger.Error("revoke project from team", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.TeamProjRevoked, "team", strconv.FormatInt(teamID, 10), `{"projectId":`+strconv.FormatInt(projectID, 10)+`}`)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- per-project access (admin session only) ---

func (s *Server) apiProjectAccess(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if _, err := s.Projects.Get(id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	members, _ := s.Teams.ProjectMembers(id)
	teams, _ := s.Teams.ProjectTeams(id)
	teamJSON := make([]map[string]any, 0, len(teams))
	for _, t := range teams {
		teamJSON = append(teamJSON, map[string]any{"id": t.ID, "name": t.Name, "slug": t.Slug})
	}
	writeJSON(w, http.StatusOK, map[string]any{"memberIds": members, "teams": teamJSON})
}

func (s *Server) apiProjectAddMember(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	var input struct {
		UserID int64 `json:"userId"`
	}
	if !decodeJSON(r, &input) || input.UserID == 0 {
		writeAPIError(w, http.StatusBadRequest, "userId is required")
		return
	}
	if err := s.Teams.AddProjectMember(id, input.UserID); err != nil {
		s.Logger.Error("add project member", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.ProjMemberAdded, "project", strconv.FormatInt(id, 10), `{"userId":`+strconv.FormatInt(input.UserID, 10)+`}`)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiProjectRemoveMember(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	userID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Teams.RemoveProjectMember(id, userID); err != nil {
		s.Logger.Error("remove project member", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.ProjMemberShed, "project", strconv.FormatInt(id, 10), `{"userId":`+strconv.FormatInt(userID, 10)+`}`)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
