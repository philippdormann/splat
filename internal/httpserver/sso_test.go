package httpserver

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/philippdormann/splat/internal/auth"
	"github.com/philippdormann/splat/internal/sso"
	"github.com/pquerna/otp/totp"
)

// fakeOIDC is a minimal provider for SSO tests: discovery, JWKS, token
// (minting whatever claims the test stages) and userinfo.
type fakeOIDC struct {
	key    *rsa.PrivateKey
	srv    *httptest.Server
	issuer string
	claims map[string]any
}

func newFakeOIDC(t *testing.T) *fakeOIDC {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeOIDC{key: key, claims: map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.issuer, "authorization_endpoint": f.issuer + "/auth",
			"token_endpoint": f.issuer + "/token", "userinfo_endpoint": f.issuer + "/userinfo",
			"jwks_uri": f.issuer + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
			"kty": "RSA", "use": "sig", "kid": "k1", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id_token": f.mint(), "access_token": "acc", "token_type": "Bearer",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"email": "fallback@example.com", "email_verified": true})
	})
	f.srv = httptest.NewServer(mux)
	f.issuer = f.srv.URL
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOIDC) mint() string {
	base := map[string]any{
		"iss": f.issuer, "sub": "sso-sub-1", "aud": "cid",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"email": "sso@example.com", "email_verified": true,
		"name": "SSO User", "preferred_username": "SSO User!",
	}
	for k, v := range f.claims {
		base[k] = v
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"k1"}`))
	payload, _ := json.Marshal(base)
	pb := base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(header + "." + pb))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	return header + "." + pb + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// attachSSO wires SSO against the fake IdP (same session secret as the
// test auth store so flow state verifies).
func attachSSO(f *fakeOIDC, s *Server) {
	s.SSO = sso.NewProvider(sso.Config{
		Issuer: f.issuer, ClientID: "cid", ClientSecret: "sec",
		Name: "TestID", Scopes: "openid email profile",
		RedirectURL: "http://localhost:8080/api/v1/sso/callback",
	}, "test-session-secret-for-2fa-tests")
}

// ssoLogin runs login → callback for the currently staged claims. The
// flow nonce comes from the redirect's own state, so the minted token
// always matches.
func ssoLogin(t *testing.T, s *Server, f *fakeOIDC) (cookie string, location string) {
	t.Helper()
	rec := get(t, s, "/api/v1/sso/login", "")
	if rec.Code != http.StatusFound {
		t.Fatalf("sso login = %d", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(loc.String(), f.issuer+"/auth?") {
		t.Fatalf("redirect = %s", loc.String())
	}
	state := loc.Query().Get("state")
	parts := strings.Split(state, ".")
	if len(parts) != 6 {
		t.Fatalf("state = %q", state)
	}
	f.claims["nonce"] = parts[2]
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sso/callback?code=authcode&state="+url.QueryEscape(state), nil)
	cb := httptest.NewRecorder()
	s.Handler().ServeHTTP(cb, req)
	if cb.Code != http.StatusFound {
		t.Fatalf("callback = %d %s", cb.Code, cb.Body.String())
	}
	location = cb.Header().Get("Location")
	for _, c := range cb.Result().Cookies() {
		if c.Name == "splat_session" && c.Value != "" {
			cookie = "splat_session=" + c.Value
		}
	}
	return cookie, location
}

func TestSSOProvisionAndRelogin(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	f := newFakeOIDC(t)
	attachSSO(f, s)
	admin, _ := a.CreateUser("admin", "password123", auth.RoleAdmin)
	adminCookie := sessionCookie(t, a, admin.ID)

	rec := get(t, s, "/api/v1/sso/config", "")
	var cfg struct {
		Enabled               bool   `json:"enabled"`
		Name                  string `json:"name"`
		PasswordLoginDisabled bool   `json:"passwordLoginDisabled"`
	}
	jsonUnmarshal(t, rec.Body.Bytes(), &cfg)
	if !cfg.Enabled || cfg.Name != "TestID" || cfg.PasswordLoginDisabled {
		t.Fatalf("config = %s", rec.Body.String())
	}
	// SSO login disabled server-side: 404.
	s2, _, _, _ := fullTestServer(t)
	if rec := get(t, s2, "/api/v1/sso/login", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unconfigured sso login = %d", rec.Code)
	}

	cookie, location := ssoLogin(t, s, f)
	if location != "/" || cookie == "" {
		t.Fatalf("callback -> %q cookie=%v", location, cookie != "")
	}
	rec = get(t, s, "/api/v1/me", cookie)
	var me struct {
		Authenticated bool   `json:"authenticated"`
		Username      string `json:"username"`
		Role          string `json:"role"`
	}
	jsonUnmarshal(t, rec.Body.Bytes(), &me)
	if !me.Authenticated || me.Role != "member" {
		t.Fatalf("me = %s", rec.Body.String())
	}
	if me.Username != "ssouser" {
		t.Fatalf("username = %q, want sanitized claims handle", me.Username)
	}
	users, err := a.ListUsers()
	if err != nil || len(users) != 2 {
		t.Fatalf("users = %+v %v", users, err)
	}
	var ssoUser auth.User
	for _, u := range users {
		if u.Email == "sso@example.com" {
			ssoUser = u
		}
	}
	if ssoUser.ID == 0 || !ssoUser.SSO {
		t.Fatalf("users = %+v", users)
	}
	firstID := ssoUser.ID

	// Second login resolves the same user, no duplicate.
	cookie2, _ := ssoLogin(t, s, f)
	if cookie2 == "" {
		t.Fatal("relogin failed")
	}
	users, _ = a.ListUsers()
	if len(users) != 2 {
		t.Fatalf("relogin users = %+v", users)
	}
	for _, u := range users {
		if u.Email == "sso@example.com" && u.ID != firstID {
			t.Fatalf("relogin users = %+v", users)
		}
	}
	// Both logins + provisioning are audited.
	total, _ := auditEntries(t, s, adminCookie, "?action=auth.sso_login")
	if total != 2 {
		t.Fatalf("sso login audit = %d, want 2", total)
	}
}

func TestSSOLinksExistingEmail(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	f := newFakeOIDC(t)
	attachSSO(f, s)

	pre, _ := a.CreateUser("jane", "password123", auth.RoleMember)
	// Email-only link under another issuer (simulates a pre-created account).
	if err := a.LinkSSO(pre.ID, "sso@example.com", "other-issuer", "other-sub"); err != nil {
		t.Fatal(err)
	}
	cookie, _ := ssoLogin(t, s, f)
	rec := get(t, s, "/api/v1/me", cookie)
	var me struct {
		Username string `json:"username"`
	}
	jsonUnmarshal(t, rec.Body.Bytes(), &me)
	if me.Username != "jane" {
		t.Fatalf("linked login username = %q, want jane", me.Username)
	}
	users, _ := a.ListUsers()
	if len(users) != 1 || users[0].ID != pre.ID || !users[0].SSO {
		t.Fatalf("users = %+v", users)
	}
}

func TestSSORejects(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	f := newFakeOIDC(t)
	attachSSO(f, s)
	admin, _ := a.CreateUser("admin", "password123", auth.RoleAdmin)
	adminCookie := sessionCookie(t, a, admin.ID)

	// Tampered state bounces to login with an error code, no session.
	rec := get(t, s, "/api/v1/sso/login", "")
	loc, _ := url.Parse(rec.Header().Get("Location"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sso/callback?code=x&state="+url.QueryEscape(loc.Query().Get("state")+"x"), nil)
	cb := httptest.NewRecorder()
	s.Handler().ServeHTTP(cb, req)
	if cb.Code != http.StatusFound || cb.Header().Get("Location") != "/login?ssoError=state" {
		t.Fatalf("tampered callback = %d %q", cb.Code, cb.Header().Get("Location"))
	}
	// Provider-side denial bounces too.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/sso/callback?error=access_denied", nil)
	cb = httptest.NewRecorder()
	s.Handler().ServeHTTP(cb, req)
	if cb.Header().Get("Location") != "/login?ssoError=provider" {
		t.Fatalf("denied callback -> %q", cb.Header().Get("Location"))
	}
	// Unverified email is refused.
	f.claims["email_verified"] = false
	_, location := ssoLogin(t, s, f)
	if location != "/login?ssoError=verify" {
		t.Fatalf("unverified email -> %q", location)
	}
	f.claims["email_verified"] = true
	_ = adminCookie
}

func TestSSOPasswordDisable(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	admin, _ := a.CreateUser("admin", "password123", auth.RoleAdmin)
	adminCookie := sessionCookie(t, a, admin.ID)

	// Refused without a configured provider (would lock everyone out).
	rec := postJSON(t, s, http.MethodPut, "/api/v1/settings/sso", adminCookie, `{"passwordLoginDisabled":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disable without sso = %d, want 400", rec.Code)
	}
	f := newFakeOIDC(t)
	attachSSO(f, s)
	rec = postJSON(t, s, http.MethodPut, "/api/v1/settings/sso", adminCookie, `{"passwordLoginDisabled":true}`)
	if rec.Code != 200 {
		t.Fatalf("disable = %d %s", rec.Code, rec.Body.String())
	}
	// Password login now fails closed...
	rec = postJSON(t, s, http.MethodPost, "/api/v1/login", "", `{"username":"admin","password":"password123"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("password login = %d, want 401", rec.Code)
	}
	// ...while SSO keeps working.
	cookie, _ := ssoLogin(t, s, f)
	if cookie == "" {
		t.Fatal("sso login failed in password-disabled mode")
	}
	// Re-enable to leave a working instance.
	rec = postJSON(t, s, http.MethodPut, "/api/v1/settings/sso", adminCookie, `{"passwordLoginDisabled":false}`)
	if rec.Code != 200 {
		t.Fatalf("re-enable = %d", rec.Code)
	}
}

func TestSSOBypassesLocalTOTP(t *testing.T) {
	s, a, _, _ := fullTestServer(t)
	f := newFakeOIDC(t)
	attachSSO(f, s)

	cookie, _ := ssoLogin(t, s, f)
	users, _ := a.ListUsers()
	// Enable local 2FA out-of-band, then SSO again: the IdP is trusted,
	// so no second factor is requested.
	key, err := a.BeginTOTPSetup(users[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ConfirmTOTPSetup(users[0].ID, code); err != nil {
		t.Fatal(err)
	}
	cookie2, _ := ssoLogin(t, s, f)
	if cookie2 == "" || cookie2 == cookie {
		t.Fatal("sso login with local 2FA enabled must still issue a fresh session")
	}
}
