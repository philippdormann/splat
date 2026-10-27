package httpserver

import (
	"net/http"

	"github.com/philippdormann/splat/internal/apikeys"
	"github.com/philippdormann/splat/internal/auth"
)

// apiAuth is the resolved caller of a management-API request: either a
// session cookie (a user with an instance role) or a Bearer API key.
// Session wins when both are present.
type apiAuth struct {
	admin  bool // instance admin: bypasses every access check
	userID int64
	key    *apikeys.Key
}

func (s *Server) authenticate(r *http.Request) apiAuth {
	if s.Auth != nil {
		if userID, ok := s.Auth.ValidateSession(auth.SessionTokenFromRequest(r)); ok {
			return apiAuth{admin: s.Auth.RoleOf(userID) == auth.RoleAdmin, userID: userID}
		}
	}
	if s.APIKeys != nil {
		if secret := apikeys.BearerToken(r); secret != "" {
			if k, err := s.APIKeys.Validate(secret); err == nil {
				s.APIKeys.TouchLastUsed(k.ID)
				return apiAuth{key: &k}
			}
		}
	}
	return apiAuth{}
}

// ok reports any recognized credential (scope checks happen per endpoint).
func (a apiAuth) ok() bool { return a.admin || a.userID != 0 || a.key != nil }

func (a apiAuth) canRead() bool {
	return a.admin || a.userID != 0 || (a.key != nil && a.key.HasScope(apikeys.ScopeRead))
}

func (a apiAuth) canWrite() bool {
	return a.admin || a.userID != 0 || (a.key != nil && a.key.HasScope(apikeys.ScopeWrite))
}

// inProject reports whether a Bearer key may touch project id (session
// callers use canAccessProject, which additionally consults team grants).
func (a apiAuth) inProject(id int64) bool {
	if a.admin || a.key == nil {
		return a.admin
	}
	return !a.key.ProjectID.Valid || a.key.ProjectID.Int64 == id
}

// canAccessProject reports whether the caller may touch project id:
// admins go everywhere, Bearer keys stay inside their binding, and member
// sessions need a direct grant or a team grant (see internal/teams).
func (s *Server) canAccessProject(r *http.Request, id int64) bool {
	a := s.authenticate(r)
	if a.admin {
		return true
	}
	if a.key != nil {
		return a.inProject(id)
	}
	if a.userID == 0 || s.Teams == nil {
		return false
	}
	ok, err := s.Teams.CanAccess(a.userID, id)
	return err == nil && ok
}

// accessScope returns the project ids a member session may touch, or nil
// when the caller is unscoped (admin or Bearer key - keys scope themselves
// per endpoint as before). The second return is false when a member has no
// grants at all, letting list endpoints return empty instead of leaking.
func (s *Server) accessScope(r *http.Request) ([]int64, bool) {
	a := s.authenticate(r)
	if a.admin || a.key != nil {
		return nil, true
	}
	if a.userID == 0 {
		return nil, false
	}
	if s.Teams == nil {
		// No RBAC store wired (tests): legacy single-admin behaviour.
		return nil, true
	}
	ids, err := s.Teams.AccessibleProjects(a.userID)
	if err != nil {
		return nil, false
	}
	return ids, true
}

// instanceOnly reports whether the caller may touch instance-level
// concerns (settings, rules, storage, key management, project
// create/delete/rotate): admin sessions only. Project-bound keys are
// confined to their project's data no matter their scopes.
func (a apiAuth) instanceOnly() bool {
	if a.admin {
		return true
	}
	return a.key != nil && !a.key.ProjectID.Valid
}

func (s *Server) requireAPIAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authenticate(r).ok() {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

// requireScope enforces read (GET/HEAD) vs write (everything else) API-key
// scopes on top of requireAPIAuth. Session callers bypass it.
func (s *Server) requireScope(next http.HandlerFunc) http.HandlerFunc {
	return s.requireAPIAuth(func(w http.ResponseWriter, r *http.Request) {
		a := s.authenticate(r)
		if a.admin || a.userID != 0 {
			next(w, r)
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			if !a.canRead() {
				writeAPIError(w, http.StatusForbidden, "key lacks read scope")
				return
			}
		} else if !a.canWrite() {
			writeAPIError(w, http.StatusForbidden, "key lacks write scope")
			return
		}
		next(w, r)
	})
}

// requireInstance restricts an endpoint to the admin session plus unbound
// keys (scope already enforced by requireScope): project-bound keys are
// confined to their project's data.
func (s *Server) requireInstance(next http.HandlerFunc) http.HandlerFunc {
	return s.requireScope(func(w http.ResponseWriter, r *http.Request) {
		a := s.authenticate(r)
		if a.admin || a.instanceOnly() {
			next(w, r)
			return
		}
		writeAPIError(w, http.StatusForbidden, "instance settings require an admin")
	})
}

// requireAdmin restricts an endpoint to the admin session cookie. Used
// for API key management: keys must never mint, rotate, revoke or delete
// keys, or a leaked automation key could entrench itself.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.Auth.ValidateSession(auth.SessionTokenFromRequest(r))
		if !ok || s.Auth.RoleOf(userID) != auth.RoleAdmin {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}
