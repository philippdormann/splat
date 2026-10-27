package httpserver

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/philippdormann/splat/internal/audit"
	"github.com/philippdormann/splat/internal/auth"
	"github.com/philippdormann/splat/internal/sso"
)

// --- SSO (generic OIDC) ---
//
// Password login stays the default; SSO is an additional door wired to
// one provider from SPLAT_OIDC_* env vars. First login auto-provisions
// a member: an existing issuer+subject links straight in, otherwise the
// verified email matches (and links) a pre-created account, otherwise a
// fresh member is created. Local TOTP is bypassed for SSO sessions —
// MFA, if any, lives at the identity provider.

func (s *Server) ssoEnabled() bool { return s.SSO != nil }

func (s *Server) apiSSOConfig(w http.ResponseWriter, r *http.Request) {
	name := ""
	issuer := ""
	if s.SSO != nil {
		name, issuer = s.SSO.Name(), s.SSO.Issuer()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":               s.ssoEnabled(),
		"name":                  name,
		"issuer":                issuer,
		"passwordLoginDisabled": s.Auth.PasswordLoginDisabled(),
	})
}

func (s *Server) apiSSOLogin(w http.ResponseWriter, r *http.Request) {
	if !s.ssoEnabled() {
		writeAPIError(w, http.StatusNotFound, "single sign-on is not configured")
		return
	}
	redirect, _, err := s.SSO.AuthURL(r.Context())
	if err != nil {
		s.Logger.Error("sso auth url", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "single sign-on is unavailable")
		return
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

// ssoFail bounces the browser back to the login page with a code the UI
// turns into a message (the callback is a navigation, not an API call).
func (s *Server) ssoFail(w http.ResponseWriter, r *http.Request, code string) {
	s.auditAs(0, "", clientIP(r), audit.LoginFailed, "user", "", `{"via":"sso","error":"`+code+`"}`)
	http.Redirect(w, r, "/login?ssoError="+code, http.StatusFound)
}

func (s *Server) apiSSOCallback(w http.ResponseWriter, r *http.Request) {
	if !s.ssoEnabled() {
		writeAPIError(w, http.StatusNotFound, "single sign-on is not configured")
		return
	}
	q := r.URL.Query()
	if q.Get("error") != "" {
		s.ssoFail(w, r, "provider")
		return
	}
	nonce, verifier, ok := s.SSO.VerifyFlow(q.Get("state"))
	if !ok {
		s.ssoFail(w, r, "state")
		return
	}
	idToken, accessToken, err := s.SSO.Exchange(r.Context(), q.Get("code"), verifier)
	if err != nil {
		s.Logger.Error("sso exchange", "error", err)
		s.ssoFail(w, r, "exchange")
		return
	}
	claims, err := s.SSO.VerifyIDToken(r.Context(), idToken, nonce)
	if err != nil {
		s.Logger.Error("sso verify", "error", err)
		s.ssoFail(w, r, "verify")
		return
	}
	if err := s.SSO.FillEmail(r.Context(), &claims, accessToken); err != nil {
		s.Logger.Error("sso email", "error", err)
		s.ssoFail(w, r, "email")
		return
	}
	user, err := s.ssoResolveUser(r, &claims)
	if err != nil {
		s.Logger.Error("sso provision", "error", err)
		s.ssoFail(w, r, "server")
		return
	}
	token, expires, err := s.Auth.CreateSession(user.ID)
	if err != nil {
		s.Logger.Error("sso session", "error", err)
		s.ssoFail(w, r, "server")
		return
	}
	auth.SetSessionCookie(w, token, expires, s.CookieSecure)
	s.auditAs(user.ID, user.Username, clientIP(r), audit.SSOLogin, "user", strconv.FormatInt(user.ID, 10), "{}")
	http.Redirect(w, r, "/", http.StatusFound)
}

// ssoResolveUser links or provisions the SSO identity.
func (s *Server) ssoResolveUser(r *http.Request, claims *sso.Claims) (auth.User, error) {
	issuer := s.SSO.Issuer()
	if u, err := s.Auth.FindBySSO(issuer, claims.Subject); err == nil {
		return u, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, err
	}
	if u, err := s.Auth.FindByEmail(claims.Email); err == nil {
		if err := s.Auth.LinkSSO(u.ID, claims.Email, issuer, claims.Subject); err != nil {
			return auth.User{}, err
		}
		s.auditAs(u.ID, u.Username, clientIP(r), audit.SSOLinked, "user", strconv.FormatInt(u.ID, 10), "{}")
		u.Email = claims.Email
		u.SSO = true
		return u, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, err
	}
	u, err := s.Auth.CreateSSOUser(ssoUsername(s.Auth, claims), claims.Email, issuer, claims.Subject)
	if err != nil {
		return auth.User{}, err
	}
	s.auditAs(u.ID, u.Username, clientIP(r), audit.UserCreated, "user", strconv.FormatInt(u.ID, 10), `{"via":"sso"}`)
	return u, nil
}

var ssoUsernameClean = regexp.MustCompile(`[^a-z0-9._-]+`)

// ssoUsername derives a unique username from the claims (preferred
// handle, else mail local part), sanitized to a safe alphabet.
func ssoUsername(store *auth.Store, claims *sso.Claims) string {
	base := ssoUsernameClean.ReplaceAllString(strings.ToLower(strings.TrimSpace(claims.PreferredUsername)), "")
	if base == "" {
		if at := strings.Index(claims.Email, "@"); at > 0 {
			base = ssoUsernameClean.ReplaceAllString(strings.ToLower(claims.Email[:at]), "")
		}
	}
	base = strings.Trim(base, "._-")
	if base == "" {
		base = "sso-user"
	}
	if len(base) > 32 {
		base = base[:32]
	}
	name := base
	for i := 2; store.UsernameTaken(name); i++ {
		name = base + "-" + strconv.Itoa(i)
	}
	return name
}

func (s *Server) apiSSOSettingsGet(w http.ResponseWriter, r *http.Request) {
	name := ""
	issuer := ""
	if s.SSO != nil {
		name, issuer = s.SSO.Name(), s.SSO.Issuer()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":               s.ssoEnabled(),
		"name":                  name,
		"issuer":                issuer,
		"passwordLoginDisabled": s.Auth.PasswordLoginDisabled(),
	})
}

func (s *Server) apiSSOSettingsSave(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PasswordLoginDisabled bool `json:"passwordLoginDisabled"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// Refuse SSO-only mode without a working provider: this is the one
	// setting that can lock every password user out at once.
	if input.PasswordLoginDisabled && !s.ssoEnabled() {
		writeAPIError(w, http.StatusBadRequest, "configure single sign-on before disabling password login")
		return
	}
	if err := s.Auth.SetPasswordLoginDisabled(input.PasswordLoginDisabled); err != nil {
		s.Logger.Error("save sso settings", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, audit.SecurityChanged, "security", "", `{"passwordLoginDisabled":`+strconv.FormatBool(input.PasswordLoginDisabled)+`}`)
	s.apiSSOSettingsGet(w, r)
}
