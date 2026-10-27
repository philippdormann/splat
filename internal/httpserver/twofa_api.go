package httpserver

import (
	"net/http"
	"strconv"

	"github.com/philippdormann/splat/internal/auth"
)

// --- two-factor authentication ---
//
// Login is two steps when 2FA applies: POST /api/v1/login verifies the
// password and returns a short-lived signed challenge instead of a
// session; POST /api/v1/login/2fa redeems it with a TOTP or recovery
// code. Users without 2FA log in as before - unless the admin requires
// 2FA instance-wide, in which case the challenge carries enrollRequired
// and the same challenge authorizes the setup endpoints, so enrollment
// happens inline before the session is issued. API keys are unaffected
// (automation credentials, separate auth path).

// twofaCaller resolves the acting user from either a session cookie or
// a login challenge. Challenge callers are only accepted for users who
// still need to pass 2FA (enabled, or required-but-missing), so a
// stolen challenge cannot manage an unrelated account's settings.
func (s *Server) twofaCaller(r *http.Request, challenge string) (userID int64, viaChallenge bool, ok bool) {
	if token := auth.SessionTokenFromRequest(r); token != "" {
		if id, valid := s.Auth.ValidateSession(token); valid {
			return id, false, true
		}
	}
	if challenge == "" {
		return 0, false, false
	}
	id, valid := s.Auth.VerifyLoginChallenge(challenge)
	if !valid {
		return 0, false, false
	}
	if !s.Auth.TOTPEnabled(id) && !s.Auth.TOTPRequired() {
		return 0, false, false
	}
	return id, true, true
}

func (s *Server) apiLogin2FA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var input struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if !decodeJSON(r, &input) || input.Challenge == "" || input.Code == "" {
		writeAPIError(w, http.StatusBadRequest, "challenge and code are required")
		return
	}
	userID, ok := s.Auth.VerifyLoginChallenge(input.Challenge)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "login challenge expired - sign in again")
		return
	}
	if s.Auth.TOTPEnabled(userID) {
		if _, valid := s.Auth.VerifySecondFactor(userID, input.Code); !valid {
			writeAPIError(w, http.StatusUnauthorized, "invalid two-factor code")
			return
		}
	} else if s.Auth.TOTPRequired() {
		writeAPIError(w, http.StatusForbidden, "two-factor setup is required on this instance")
		return
	}
	token, expires, err := s.Auth.CreateSession(userID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	auth.SetSessionCookie(w, token, expires, s.CookieSecure)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) api2FAStatus(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.Auth.ValidateSession(auth.SessionTokenFromRequest(r))
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":                s.Auth.TOTPEnabled(userID),
		"required":               s.Auth.TOTPRequired(),
		"recoveryCodesRemaining": s.Auth.RecoveryCodesRemaining(userID),
	})
}

func (s *Server) api2FASetupBegin(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Challenge string `json:"challenge"`
	}
	_ = decodeJSON(r, &input)
	userID, viaChallenge, ok := s.twofaCaller(r, input.Challenge)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	// A challenge only authorizes first-time enrollment, never rotation
	// of an already-enabled second factor.
	if viaChallenge && s.Auth.TOTPEnabled(userID) {
		writeAPIError(w, http.StatusForbidden, "two-factor authentication is already enabled")
		return
	}
	key, err := s.Auth.BeginTOTPSetup(userID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	qr, err := auth.QRDataURI(key.URL())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"otpauthUrl": key.URL(),
		"qrDataUri":  qr,
		"secret":     key.Secret(),
	})
}

func (s *Server) api2FASetupConfirm(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code      string `json:"code"`
		Challenge string `json:"challenge"`
	}
	if !decodeJSON(r, &input) || input.Code == "" {
		writeAPIError(w, http.StatusBadRequest, "code is required")
		return
	}
	userID, viaChallenge, ok := s.twofaCaller(r, input.Challenge)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if viaChallenge && s.Auth.TOTPEnabled(userID) {
		writeAPIError(w, http.StatusForbidden, "two-factor authentication is already enabled")
		return
	}
	codes, err := s.Auth.ConfirmTOTPSetup(userID, input.Code)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Enrollment during login completes the login: no session exists yet.
	if viaChallenge {
		token, expires, err := s.Auth.CreateSession(userID)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal error")
			return
		}
		auth.SetSessionCookie(w, token, expires, s.CookieSecure)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recoveryCodes": codes})
}

// checkPassword re-verifies the session owner's password before
// destructive 2FA changes (disable, code regeneration).
func (s *Server) checkPassword(w http.ResponseWriter, r *http.Request, userID int64, password string) bool {
	if password == "" {
		writeAPIError(w, http.StatusBadRequest, "password is required")
		return false
	}
	if _, err := s.Auth.Authenticate(s.Auth.UsernameOf(userID), password); err != nil {
		writeAPIError(w, http.StatusUnauthorized, "invalid password")
		return false
	}
	return true
}

func (s *Server) api2FADisable(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.Auth.ValidateSession(auth.SessionTokenFromRequest(r))
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if !s.checkPassword(w, r, userID, input.Password) {
		return
	}
	if s.Auth.TOTPRequired() {
		writeAPIError(w, http.StatusBadRequest, "two-factor authentication is required on this instance")
		return
	}
	if err := s.Auth.DisableTOTP(userID); err != nil {
		s.Logger.Error("disable 2fa", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) api2FARegenerateCodes(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.Auth.ValidateSession(auth.SessionTokenFromRequest(r))
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if !s.checkPassword(w, r, userID, input.Password) {
		return
	}
	codes, err := s.Auth.RegenerateRecoveryCodes(userID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "recoveryCodes": codes})
}

// apiUsersReset2FA lets an admin recover a locked-out user (lost device
// and no recovery codes). Under instance enforcement the user enrolls a
// fresh factor at their next login.
func (s *Server) apiUsersReset2FA(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.Auth.DisableTOTP(id); err != nil {
		s.Logger.Error("reset 2fa", "error", err, "user_id", id)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) apiSecurityGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"require2fa": s.Auth.TOTPRequired()})
}

func (s *Server) apiSecuritySave(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Require2FA bool `json:"require2fa"`
	}
	if !decodeJSON(r, &input) {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := s.Auth.SetTOTPRequired(input.Require2FA); err != nil {
		s.Logger.Error("save security settings", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.apiSecurityGet(w, r)
}
