package httpserver

import (
	"net/http"

	"errgotrack/internal/auth"
)

type loginPageData struct {
	PageBase
	Error string
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if userID, ok := s.Auth.ValidateSession(auth.SessionTokenFromRequest(r)); ok {
		_ = userID
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render.Page(w, "login", loginPageData{
		PageBase: PageBase{CSRFToken: auth.EnsureCSRFCookie(w, r, s.CookieSecure)},
	})
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(w, r) {
		return
	}
	userID, err := s.Auth.Authenticate(r.FormValue("username"), r.FormValue("password"))
	if err != nil {
		s.render.Page(w, "login", loginPageData{
			PageBase: PageBase{CSRFToken: auth.EnsureCSRFCookie(w, r, s.CookieSecure)},
			Error:    "Invalid username or password.",
		})
		return
	}
	token, expires, err := s.Auth.CreateSession(userID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	auth.SetSessionCookie(w, token, expires, s.CookieSecure)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(w, r) {
		return
	}
	s.Auth.DeleteSession(auth.SessionTokenFromRequest(r))
	auth.ClearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
