package httpserver

import (
	"net/http"

	"errgotrack/internal/apikeys"
	"errgotrack/internal/auth"
)

// apiAuth is the resolved caller of a management-API request: either the
// admin session cookie or a Bearer API key. Session wins when both are
// present.
type apiAuth struct {
	admin bool
	key   *apikeys.Key
}

func (s *Server) authenticate(r *http.Request) apiAuth {
	if _, ok := s.Auth.ValidateSession(auth.SessionTokenFromRequest(r)); ok {
		return apiAuth{admin: true}
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
func (a apiAuth) ok() bool { return a.admin || a.key != nil }

func (a apiAuth) canRead() bool {
	return a.admin || (a.key != nil && a.key.HasScope(apikeys.ScopeRead))
}

func (a apiAuth) canWrite() bool {
	return a.admin || (a.key != nil && a.key.HasScope(apikeys.ScopeWrite))
}

// inProject reports whether the caller may touch project id: the admin
// session and unbound keys go everywhere; a project-bound key only inside
// its own project.
func (a apiAuth) inProject(id int64) bool {
	if a.admin || a.key == nil {
		return a.admin
	}
	return !a.key.ProjectID.Valid || a.key.ProjectID.Int64 == id
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
		if a.admin {
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
		writeAPIError(w, http.StatusForbidden, "project-bound keys cannot access instance settings")
	})
}

// requireAdmin restricts an endpoint to the admin session cookie. Used
// for API key management: keys must never mint, rotate, revoke or delete
// keys, or a leaked automation key could entrench itself.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.Auth.ValidateSession(auth.SessionTokenFromRequest(r)); !ok {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}
