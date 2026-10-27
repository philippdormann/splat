package httpserver

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/philippdormann/splat/internal/auth"
	"github.com/pquerna/otp/totp"
)

func jsonBody(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
}

func hasSessionCookie(rec interface{ Header() http.Header }) bool {
	for _, c := range rec.Header()["Set-Cookie"] {
		if len(c) > len("splat_session=") && c[:len("splat_session=")] == "splat_session=" {
			// Cleared cookies are empty/quoted-empty; a real session is 64 hex chars.
			if len(c) > len("splat_session=")+10 {
				return true
			}
		}
	}
	return false
}

func totpCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	return code
}

// enable2FAHTTP runs the self-service setup flow over HTTP and returns
// the minted recovery codes.
func enable2FAHTTP(t *testing.T, s *Server, cookie string) (secret string, codes []string) {
	t.Helper()
	rec := postJSON(t, s, http.MethodPost, "/api/v1/2fa/setup/begin", cookie, "")
	if rec.Code != 200 {
		t.Fatalf("setup begin = %d %s", rec.Code, rec.Body.String())
	}
	var begun struct {
		Secret     string `json:"secret"`
		OtpauthURL string `json:"otpauthUrl"`
		QRDataURI  string `json:"qrDataUri"`
	}
	jsonBody(t, rec.Body.Bytes(), &begun)
	if begun.Secret == "" || begun.OtpauthURL == "" || len(begun.QRDataURI) < 100 {
		t.Fatalf("begin = %s", rec.Body.String())
	}
	rec = postJSON(t, s, http.MethodPost, "/api/v1/2fa/setup/confirm", cookie, `{"code":"000000"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("confirm wrong code = %d, want 400", rec.Code)
	}
	rec = postJSON(t, s, http.MethodPost, "/api/v1/2fa/setup/confirm", cookie, `{"code":"`+totpCode(t, begun.Secret)+`"}`)
	if rec.Code != 200 {
		t.Fatalf("confirm = %d %s", rec.Code, rec.Body.String())
	}
	var done struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	jsonBody(t, rec.Body.Bytes(), &done)
	if len(done.RecoveryCodes) != 10 {
		t.Fatalf("recovery codes = %v", done.RecoveryCodes)
	}
	return begun.Secret, done.RecoveryCodes
}

func TestLoginWithout2FAUnchanged(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	u, err := a.CreateUser("plain", "password123", auth.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	_ = u
	rec := postJSON(t, s, http.MethodPost, "/api/v1/login", "", `{"username":"plain","password":"password123"}`)
	if rec.Code != 200 {
		t.Fatalf("login = %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	jsonBody(t, rec.Body.Bytes(), &body)
	if body["ok"] != true || !hasSessionCookie(rec) {
		t.Fatalf("login = %s", rec.Body.String())
	}
}

func Test2FASetupAndSecondStepLogin(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	u, _ := a.CreateUser("alice", "password123", auth.RoleMember)
	cookie := sessionCookie(t, a, u.ID)

	rec := get(t, s, "/api/v1/2fa/status", cookie)
	var status struct {
		Enabled  bool `json:"enabled"`
		Required bool `json:"required"`
	}
	jsonBody(t, rec.Body.Bytes(), &status)
	if status.Enabled || status.Required {
		t.Fatalf("status = %s", rec.Body.String())
	}

	secret, _ := enable2FAHTTP(t, s, cookie)

	rec = get(t, s, "/api/v1/2fa/status", cookie)
	jsonBody(t, rec.Body.Bytes(), &status)
	if !status.Enabled {
		t.Fatalf("status after setup = %s", rec.Body.String())
	}

	// Password step: challenge, and crucially no session yet.
	rec = postJSON(t, s, http.MethodPost, "/api/v1/login", "", `{"username":"alice","password":"password123"}`)
	if rec.Code != 200 {
		t.Fatalf("login = %d %s", rec.Code, rec.Body.String())
	}
	var step1 struct {
		TwoFactorRequired bool   `json:"twoFactorRequired"`
		Challenge         string `json:"challenge"`
	}
	jsonBody(t, rec.Body.Bytes(), &step1)
	if !step1.TwoFactorRequired || step1.Challenge == "" {
		t.Fatalf("login step 1 = %s", rec.Body.String())
	}
	if hasSessionCookie(rec) {
		t.Fatal("password step must not issue a session")
	}

	rec = postJSON(t, s, http.MethodPost, "/api/v1/login/2fa", "", `{"challenge":"`+step1.Challenge+`","code":"000000"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong 2fa code = %d, want 401", rec.Code)
	}
	rec = postJSON(t, s, http.MethodPost, "/api/v1/login/2fa", "", `{"challenge":"`+step1.Challenge+`tampered","code":"000000"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered challenge = %d, want 401", rec.Code)
	}
	rec = postJSON(t, s, http.MethodPost, "/api/v1/login/2fa", "", `{"challenge":"`+step1.Challenge+`","code":"`+totpCode(t, secret)+`"}`)
	if rec.Code != 200 || !hasSessionCookie(rec) {
		t.Fatalf("2fa verify = %d %s", rec.Code, rec.Body.String())
	}
}

func Test2FARecoveryCodeLogin(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	u, _ := a.CreateUser("bob", "password123", auth.RoleMember)
	cookie := sessionCookie(t, a, u.ID)
	_, codes := enable2FAHTTP(t, s, cookie)

	rec := postJSON(t, s, http.MethodPost, "/api/v1/login", "", `{"username":"bob","password":"password123"}`)
	var step1 struct {
		Challenge string `json:"challenge"`
	}
	jsonBody(t, rec.Body.Bytes(), &step1)

	rec = postJSON(t, s, http.MethodPost, "/api/v1/login/2fa", "", `{"challenge":"`+step1.Challenge+`","code":"`+codes[0]+`"}`)
	if rec.Code != 200 || !hasSessionCookie(rec) {
		t.Fatalf("recovery login = %d %s", rec.Code, rec.Body.String())
	}
	// Burned: the same code fails on the next login.
	rec = postJSON(t, s, http.MethodPost, "/api/v1/login", "", `{"username":"bob","password":"password123"}`)
	jsonBody(t, rec.Body.Bytes(), &step1)
	rec = postJSON(t, s, http.MethodPost, "/api/v1/login/2fa", "", `{"challenge":"`+step1.Challenge+`","code":"`+codes[0]+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reused recovery code = %d, want 401", rec.Code)
	}
}

func Test2FAEnforcementEnrollFlow(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	if err := a.SetTOTPRequired(true); err != nil {
		t.Fatal(err)
	}
	u, _ := a.CreateUser("carol", "password123", auth.RoleMember)
	_ = u

	rec := postJSON(t, s, http.MethodPost, "/api/v1/login", "", `{"username":"carol","password":"password123"}`)
	var step1 struct {
		TwoFactorRequired bool   `json:"twoFactorRequired"`
		EnrollRequired    bool   `json:"enrollRequired"`
		Challenge         string `json:"challenge"`
	}
	jsonBody(t, rec.Body.Bytes(), &step1)
	if !step1.TwoFactorRequired || !step1.EnrollRequired || step1.Challenge == "" {
		t.Fatalf("enforced login = %s", rec.Body.String())
	}
	if hasSessionCookie(rec) {
		t.Fatal("enrollment must not issue a session before setup")
	}
	// Second-factor verification without a factor is refused.
	rec = postJSON(t, s, http.MethodPost, "/api/v1/login/2fa", "", `{"challenge":"`+step1.Challenge+`","code":"123456"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("verify without factor = %d, want 403", rec.Code)
	}
	// The challenge authorizes setup without a session...
	rec = postJSON(t, s, http.MethodPost, "/api/v1/2fa/setup/begin", "", `{"challenge":"`+step1.Challenge+`"}`)
	if rec.Code != 200 {
		t.Fatalf("enroll begin = %d %s", rec.Code, rec.Body.String())
	}
	var begun struct {
		Secret string `json:"secret"`
	}
	jsonBody(t, rec.Body.Bytes(), &begun)
	// ...and confirming completes the login with a session.
	rec = postJSON(t, s, http.MethodPost, "/api/v1/2fa/setup/confirm", "", `{"challenge":"`+step1.Challenge+`","code":"`+totpCode(t, begun.Secret)+`"}`)
	if rec.Code != 200 || !hasSessionCookie(rec) {
		t.Fatalf("enroll confirm = %d %s", rec.Code, rec.Body.String())
	}
	var done struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	jsonBody(t, rec.Body.Bytes(), &done)
	if len(done.RecoveryCodes) != 10 {
		t.Fatalf("enroll confirm = %s", rec.Body.String())
	}
	// Next login requires the fresh factor.
	rec = postJSON(t, s, http.MethodPost, "/api/v1/login", "", `{"username":"carol","password":"password123"}`)
	var step2 struct {
		TwoFactorRequired bool `json:"twoFactorRequired"`
		EnrollRequired    bool `json:"enrollRequired"`
	}
	jsonBody(t, rec.Body.Bytes(), &step2)
	if !step2.TwoFactorRequired || step2.EnrollRequired {
		t.Fatalf("login after enroll = %s", rec.Body.String())
	}
}

func Test2FAChallengeCannotRotateEnabledFactor(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	if err := a.SetTOTPRequired(true); err != nil {
		t.Fatal(err)
	}
	u, _ := a.CreateUser("dave", "password123", auth.RoleMember)
	cookie := sessionCookie(t, a, u.ID)
	enable2FAHTTP(t, s, cookie)

	rec := postJSON(t, s, http.MethodPost, "/api/v1/login", "", `{"username":"dave","password":"password123"}`)
	var step1 struct {
		Challenge string `json:"challenge"`
	}
	jsonBody(t, rec.Body.Bytes(), &step1)
	rec = postJSON(t, s, http.MethodPost, "/api/v1/2fa/setup/begin", "", `{"challenge":"`+step1.Challenge+`"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("challenge re-setup = %d, want 403", rec.Code)
	}
}

func Test2FADisableAndAdminReset(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	admin, _ := a.CreateUser("admin", "password123", auth.RoleAdmin)
	u, _ := a.CreateUser("erin", "password123", auth.RoleMember)
	cookie := sessionCookie(t, a, u.ID)
	adminCookie := sessionCookie(t, a, admin.ID)
	enable2FAHTTP(t, s, cookie)

	// Wrong password blocks self-service disable.
	rec := postJSON(t, s, http.MethodPost, "/api/v1/2fa/disable", cookie, `{"password":"wrongpass1"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("disable wrong password = %d, want 401", rec.Code)
	}
	rec = postJSON(t, s, http.MethodPost, "/api/v1/2fa/disable", cookie, `{"password":"password123"}`)
	if rec.Code != 200 {
		t.Fatalf("disable = %d %s", rec.Code, rec.Body.String())
	}
	rec = get(t, s, "/api/v1/2fa/status", cookie)
	var status struct {
		Enabled bool `json:"enabled"`
	}
	jsonBody(t, rec.Body.Bytes(), &status)
	if status.Enabled {
		t.Fatal("2FA still enabled after disable")
	}

	// Re-enable, then lock out: admin reset recovers the account.
	enable2FAHTTP(t, s, cookie)
	rec = postJSON(t, s, http.MethodPost, "/api/v1/users/"+itoa(u.ID)+"/2fa/reset", cookie, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("member reset = %d, want 401", rec.Code)
	}
	rec = postJSON(t, s, http.MethodPost, "/api/v1/users/"+itoa(u.ID)+"/2fa/reset", adminCookie, "")
	if rec.Code != 200 {
		t.Fatalf("admin reset = %d %s", rec.Code, rec.Body.String())
	}
	rec = postJSON(t, s, http.MethodPost, "/api/v1/login", "", `{"username":"erin","password":"password123"}`)
	var body map[string]any
	jsonBody(t, rec.Body.Bytes(), &body)
	if body["ok"] != true || !hasSessionCookie(rec) {
		t.Fatalf("login after reset = %s", rec.Body.String())
	}
}

func Test2FADisableBlockedWhenRequired(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	if err := a.SetTOTPRequired(true); err != nil {
		t.Fatal(err)
	}
	u, _ := a.CreateUser("frank", "password123", auth.RoleMember)
	cookie := sessionCookie(t, a, u.ID)
	enable2FAHTTP(t, s, cookie)

	rec := postJSON(t, s, http.MethodPost, "/api/v1/2fa/disable", cookie, `{"password":"password123"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disable under enforcement = %d, want 400", rec.Code)
	}
	rec = postJSON(t, s, http.MethodPost, "/api/v1/2fa/recovery-codes/regenerate", cookie, `{"password":"password123"}`)
	if rec.Code != 200 {
		t.Fatalf("regenerate = %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	jsonBody(t, rec.Body.Bytes(), &body)
	if len(body.RecoveryCodes) != 10 {
		t.Fatalf("regenerate = %s", rec.Body.String())
	}
}

func TestSecurityEnforcementSetting(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	admin, _ := a.CreateUser("admin", "password123", auth.RoleAdmin)
	member, _ := a.CreateUser("greg", "password123", auth.RoleMember)
	adminCookie := sessionCookie(t, a, admin.ID)
	memberCookie := sessionCookie(t, a, member.ID)

	if rec := get(t, s, "/api/v1/settings/security", memberCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("member security = %d, want 403", rec.Code)
	}
	rec := get(t, s, "/api/v1/settings/security", adminCookie)
	var cur struct {
		Require2FA bool `json:"require2fa"`
	}
	jsonBody(t, rec.Body.Bytes(), &cur)
	if cur.Require2FA {
		t.Fatal("enforcement must default off")
	}
	rec = postJSON(t, s, http.MethodPut, "/api/v1/settings/security", adminCookie, `{"require2fa":true}`)
	jsonBody(t, rec.Body.Bytes(), &cur)
	if rec.Code != 200 || !cur.Require2FA {
		t.Fatalf("save = %d %s", rec.Code, rec.Body.String())
	}
	// Users list now reports the factor flag.
	rec = get(t, s, "/api/v1/users", adminCookie)
	var users []struct {
		TOTPEnabled bool `json:"totpEnabled"`
	}
	jsonBody(t, rec.Body.Bytes(), &users)
	if len(users) != 2 {
		t.Fatalf("users = %s", rec.Body.String())
	}
}
