package sso

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeIdP is a minimal OIDC provider: discovery, JWKS, token and
// userinfo endpoints backed by one generated RSA key.
type fakeIdP struct {
	t      *testing.T
	key    *rsa.PrivateKey
	kid    string
	issuer string
	srv    *httptest.Server
	mutate func(claims map[string]any)
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{t: t, key: key, kid: "test-key-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 f.issuer,
			"authorization_endpoint": f.issuer + "/auth",
			"token_endpoint":         f.issuer + "/token",
			"userinfo_endpoint":      f.issuer + "/userinfo",
			"jwks_uri":               f.issuer + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
			"kty": "RSA", "use": "sig", "kid": f.kid, "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "authorization_code" {
			http.Error(w, "bad grant", http.StatusBadRequest)
			return
		}
		user, _, _ := r.BasicAuth()
		if user != "test-client" {
			http.Error(w, "bad client", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id_token":     f.idToken(map[string]any{}),
			"access_token": "test-access",
			"token_type":   "Bearer",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"email": "fallback@example.com", "email_verified": true})
	})
	f.srv = httptest.NewServer(mux)
	f.issuer = f.srv.URL
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIdP) idToken(over map[string]any) string {
	claims := map[string]any{
		"iss": f.issuer, "sub": "user-123", "aud": "test-client",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"nonce": "test-nonce", "email": "user@example.com", "email_verified": true,
		"name": "Test User", "preferred_username": "testuser",
	}
	for k, v := range over {
		claims[k] = v
	}
	if f.mutate != nil {
		f.mutate(claims)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test-key-1","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	pb := base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(header + "." + pb))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return header + "." + pb + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func testProvider(t *testing.T, f *fakeIdP) *Provider {
	t.Helper()
	return NewProvider(Config{
		Issuer: f.issuer, ClientID: "test-client", ClientSecret: "s3cret",
		Name: "Test", Scopes: "openid email profile", RedirectURL: "http://localhost/cb",
	}, "server-secret")
}

func TestFlowStateRoundtrip(t *testing.T) {
	p := NewProvider(Config{}, "server-secret")
	state, nonce, verifier, err := p.NewFlow()
	if err != nil {
		t.Fatal(err)
	}
	n2, v2, ok := p.VerifyFlow(state)
	if !ok || n2 != nonce || v2 != verifier {
		t.Fatal("valid state rejected")
	}
	if _, _, ok := p.VerifyFlow(state + "x"); ok {
		t.Fatal("tampered state accepted")
	}
	// No server secret: fail closed.
	q := NewProvider(Config{}, "")
	if _, _, _, err := q.NewFlow(); err == nil {
		t.Fatal("stateless flow must fail")
	}
	if _, _, ok := q.VerifyFlow(state); ok {
		t.Fatal("stateless verify must fail")
	}
}

func TestFullFlowAgainstFakeIdP(t *testing.T) {
	f := newFakeIdP(t)
	p := testProvider(t, f)
	ctx := context.Background()

	redirect, state, err := p.AuthURL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(redirect, f.issuer+"/auth?") || !strings.Contains(redirect, "code_challenge=") {
		t.Fatalf("auth url = %s", redirect)
	}
	if _, _, ok := p.VerifyFlow(state); !ok {
		t.Fatal("own state rejected")
	}
	idToken, accessToken, err := p.Exchange(ctx, "auth-code", "verifier-is-not-checked-by-fake")
	if err != nil {
		t.Fatal(err)
	}
	if accessToken != "test-access" {
		t.Fatalf("access token = %q", accessToken)
	}
	claims, err := p.VerifyIDToken(ctx, idToken, "test-nonce")
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-123" || claims.Email != "user@example.com" || !claims.EmailVerified {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.PreferredUsername != "testuser" || claims.Name != "Test User" {
		t.Fatalf("claims = %+v", claims)
	}
}

func TestVerifyRejects(t *testing.T) {
	f := newFakeIdP(t)
	p := testProvider(t, f)
	ctx := context.Background()

	cases := map[string]map[string]any{
		"nonce":    {"nonce": "wrong"},
		"audience": {"aud": "other-client"},
		"issuer":   {"iss": "https://evil.example.com"},
		"expired":  {"exp": time.Now().Add(-time.Hour).Unix()},
		"no-sub":   {"sub": ""},
	}
	for name, over := range cases {
		tok := f.idToken(over)
		// The nonce case keeps a valid token shape; others too.
		if _, err := p.VerifyIDToken(ctx, tok, "test-nonce"); err == nil {
			t.Fatalf("%s: bad token accepted", name)
		}
	}
	if _, err := p.VerifyIDToken(ctx, "not.a.token", "test-nonce"); err == nil {
		t.Fatal("garbage accepted")
	}
	// Unverified email is rejected...
	tok := f.idToken(map[string]any{"email_verified": false})
	if _, err := p.VerifyIDToken(ctx, tok, "test-nonce"); err == nil {
		t.Fatal("unverified email accepted")
	}
	// ...but a missing flag is tolerated (providers that omit it).
	tok = f.idToken(map[string]any{"email_verified": nil})
	c, err := p.VerifyIDToken(ctx, tok, "test-nonce")
	if err != nil || !c.EmailVerified {
		t.Fatalf("missing flag: %+v %v", c, err)
	}
}

func TestUserinfoFallback(t *testing.T) {
	f := newFakeIdP(t)
	p := testProvider(t, f)
	ctx := context.Background()

	// ID token without email: userinfo fills it in.
	tok := f.idToken(map[string]any{"email": "", "email_verified": nil})
	c, err := p.VerifyIDToken(ctx, tok, "test-nonce")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.FillEmail(ctx, &c, "test-access"); err != nil {
		t.Fatal(err)
	}
	if c.Email != "fallback@example.com" || !c.EmailVerified {
		t.Fatalf("claims = %+v", c)
	}
	// Nothing to fill and nothing available: error.
	c2 := Claims{}
	if err := p.FillEmail(ctx, &c2, "bad-token"); err == nil {
		t.Fatal("userinfo failure tolerated")
	}
}

func TestDiscoveryMismatch(t *testing.T) {
	f := newFakeIdP(t)
	p := NewProvider(Config{
		Issuer: f.issuer, ClientID: "test-client", ClientSecret: "s3cret",
		RedirectURL: "http://localhost/cb",
	}, "server-secret")
	// Point the issuer at a server whose document names another issuer.
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": "https://evil.example.com", "authorization_endpoint": "x",
			"token_endpoint": "x", "jwks_uri": "x",
		})
	}))
	defer evil.Close()
	p.cfg.Issuer = evil.URL
	if _, err := p.discovery(context.Background()); err == nil {
		t.Fatal("issuer mismatch tolerated")
	}
}
