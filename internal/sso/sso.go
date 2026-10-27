// Package sso implements a minimal generic OIDC relying party with the
// standard library only: discovery, JWKS verification (RS256), the
// authorization-code flow with PKCE, and HMAC-bound state/nonce. It
// covers what Keycloak, Authentik, Google, and GitHub-style OIDC
// providers emit; exotic JWS algorithms are out of scope by design.
package sso

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config wires one OIDC provider. RedirectURL is
// {PublicURL}/api/v1/sso/callback.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Name         string
	Scopes       string
	RedirectURL  string
	HTTPClient   *http.Client
}

// Enabled reports whether SSO is configured.
func (c Config) Enabled() bool { return c.Issuer != "" && c.ClientID != "" }

func (c Config) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

type discoveryDoc struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

// Claims carried from the ID token (or userinfo fallback).
type Claims struct {
	Subject           string
	Email             string
	EmailVerified     bool
	Name              string
	PreferredUsername string
}

// Provider caches discovery + signing keys for one issuer.
type Provider struct {
	cfg    Config
	hmacOK bool
	macKey []byte

	mu      sync.Mutex
	disco   *discoveryDoc
	keys    map[string]*rsa.PublicKey
	keysExp time.Time

	// Pending login flows: opaque state id -> nonce + PKCE verifier.
	// The verifier never leaves the server (it is not embedded in state,
	// so the IdP, browser history and logs only see an opaque id).
	flowsMu sync.Mutex
	flows   map[string]flowEntry
}

type flowEntry struct {
	nonce    string
	verifier string
	exp      int64
}

// NewProvider builds a provider; sessionSecret signs flow state (empty
// disables state issuance - flows fail closed).
func NewProvider(cfg Config, sessionSecret string) *Provider {
	p := &Provider{cfg: cfg, flows: map[string]flowEntry{}}
	if sessionSecret != "" {
		sum := sha256.Sum256([]byte("splat-sso-state|" + sessionSecret))
		p.macKey = sum[:]
		p.hmacOK = true
	}
	return p
}

// Name returns the display name for the login button.
func (p *Provider) Name() string {
	if p.cfg.Name == "" {
		return "SSO"
	}
	return p.cfg.Name
}

// Issuer returns the normalized issuer (login lookup identity).
func (p *Provider) Issuer() string { return normIssuer(p.cfg.Issuer) }

func normIssuer(s string) string { return strings.TrimSuffix(strings.TrimSpace(s), "/") }

// discovery fetches and caches the provider metadata.
func (p *Provider) discovery(ctx context.Context) (*discoveryDoc, error) {
	p.mu.Lock()
	cached := p.disco
	p.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	u := normIssuer(p.cfg.Issuer) + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.cfg.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc discovery: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var doc discoveryDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	// The issuer in the document must match configuration (mix-up defense).
	if normIssuer(doc.Issuer) != normIssuer(p.cfg.Issuer) {
		return nil, errors.New("oidc discovery: issuer mismatch")
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" || doc.JWKSURI == "" {
		return nil, errors.New("oidc discovery: incomplete metadata")
	}
	p.mu.Lock()
	p.disco = &doc
	p.mu.Unlock()
	return &doc, nil
}

type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// signingKeys fetches and caches the RSA verification keys (1h TTL).
func (p *Provider) signingKeys(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	p.mu.Lock()
	keys, exp := p.keys, p.keysExp
	p.mu.Unlock()
	if keys != nil && time.Now().Before(exp) {
		return keys, nil
	}
	doc, err := p.discovery(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, doc.JWKSURI, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.cfg.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc jwks: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("oidc jwks: %w", err)
	}
	out := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		nb, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eb, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		e := 0
		for _, b := range eb {
			e = e<<8 + int(b)
		}
		if e == 0 {
			continue
		}
		out[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}
	}
	if len(out) == 0 {
		return nil, errors.New("oidc jwks: no RSA keys")
	}
	p.mu.Lock()
	p.keys, p.keysExp = out, time.Now().Add(time.Hour)
	p.mu.Unlock()
	return out, nil
}

// --- login flow state (stateless, HMAC-bound) ---

// NewFlow mints a nonce + PKCE verifier kept server-side and the
// signed opaque state that references them at the callback.
func (p *Provider) NewFlow() (state, nonce, verifier string, err error) {
	if !p.hmacOK {
		return "", "", "", errors.New("sso is unavailable: server secret is not configured")
	}
	nonce, err = randB64(16)
	if err != nil {
		return "", "", "", err
	}
	verifier, err = randB64(32)
	if err != nil {
		return "", "", "", err
	}
	id, err := randB64(16)
	if err != nil {
		return "", "", "", err
	}
	exp := time.Now().Add(10 * time.Minute).Unix()
	p.flowsMu.Lock()
	// Opportunistic sweep so abandoned logins cannot grow the map.
	for k, f := range p.flows {
		if time.Now().Unix() > f.exp {
			delete(p.flows, k)
		}
	}
	p.flows[id] = flowEntry{nonce: nonce, verifier: verifier, exp: exp}
	p.flowsMu.Unlock()
	body := "sso." + strconv.FormatInt(exp, 10) + "." + id
	mac := hmac.New(sha256.New, p.macKey)
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nonce, verifier, nil
}

// VerifyFlow checks the returned state and recovers nonce + verifier.
// Flows are single-use: a state that verifies is consumed.
func (p *Provider) VerifyFlow(state string) (nonce, verifier string, ok bool) {
	if !p.hmacOK {
		return "", "", false
	}
	parts := strings.Split(state, ".")
	if len(parts) != 4 || parts[0] != "sso" {
		return "", "", false
	}
	body := strings.Join(parts[:3], ".")
	sig, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return "", "", false
	}
	mac := hmac.New(sha256.New, p.macKey)
	mac.Write([]byte(body))
	if subtle.ConstantTimeCompare(sig, mac.Sum(nil)) != 1 {
		return "", "", false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", "", false
	}
	id := parts[2]
	if id == "" {
		return "", "", false
	}
	p.flowsMu.Lock()
	f, found := p.flows[id]
	if found {
		delete(p.flows, id)
	}
	p.flowsMu.Unlock()
	if !found || time.Now().Unix() > f.exp {
		return "", "", false
	}
	if f.nonce == "" || f.verifier == "" {
		return "", "", false
	}
	return f.nonce, f.verifier, true
}

func randB64(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// AuthURL builds the provider login URL for a fresh flow.
func (p *Provider) AuthURL(ctx context.Context) (redirect string, state string, err error) {
	state, nonce, verifier, err := p.NewFlow()
	if err != nil {
		return "", "", err
	}
	doc, err := p.discovery(ctx)
	if err != nil {
		return "", "", err
	}
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {p.cfg.ClientID},
		"redirect_uri":          {p.cfg.RedirectURL},
		"scope":                 {p.cfg.Scopes},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {pkceChallenge(verifier)},
		"code_challenge_method": {"S256"},
	}
	return doc.AuthorizationEndpoint + "?" + q.Encode(), state, nil
}

// tokens holds the code-exchange result.
type tokens struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
}

// Exchange trades the code for tokens (PKCE verifier, secret via basic auth).
func (p *Provider) Exchange(ctx context.Context, code, verifier string) (idToken, accessToken string, err error) {
	doc, err := p.discovery(ctx)
	if err != nil {
		return "", "", err
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {p.cfg.RedirectURL},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, doc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(p.cfg.ClientID, p.cfg.ClientSecret)
	resp, err := p.cfg.client().Do(req)
	if err != nil {
		return "", "", fmt.Errorf("oidc exchange: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("oidc exchange: status %d", resp.StatusCode)
	}
	var tok tokens
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", "", fmt.Errorf("oidc exchange: %w", err)
	}
	if tok.IDToken == "" {
		return "", "", errors.New("oidc exchange: no id_token")
	}
	return tok.IDToken, tok.AccessToken, nil
}

// NonceForState returns the stored nonce for a state without consuming
// the flow. Test helper only.
func (p *Provider) NonceForState(state string) (string, bool) {
	parts := strings.Split(state, ".")
	if len(parts) != 4 || parts[0] != "sso" {
		return "", false
	}
	body := strings.Join(parts[:3], ".")
	sig, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return "", false
	}
	mac := hmac.New(sha256.New, p.macKey)
	mac.Write([]byte(body))
	if subtle.ConstantTimeCompare(sig, mac.Sum(nil)) != 1 {
		return "", false
	}
	p.flowsMu.Lock()
	defer p.flowsMu.Unlock()
	f, ok := p.flows[parts[2]]
	if !ok {
		return "", false
	}
	return f.nonce, true
}

// VerifyIDToken checks signature, issuer, audience, expiry and nonce.
func (p *Provider) VerifyIDToken(ctx context.Context, raw, nonce string) (Claims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("oidc: malformed token")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, errors.New("oidc: malformed header")
	}
	if err := json.Unmarshal(hb, &header); err != nil {
		return Claims{}, errors.New("oidc: malformed header")
	}
	if header.Alg != "RS256" {
		return Claims{}, fmt.Errorf("oidc: unsupported algorithm %q", header.Alg)
	}
	keys, err := p.signingKeys(ctx)
	if err != nil {
		return Claims{}, err
	}
	pub, ok := keys[header.Kid]
	if !ok {
		return Claims{}, errors.New("oidc: unknown signing key")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, errors.New("oidc: malformed signature")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		return Claims{}, errors.New("oidc: bad signature")
	}
	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, errors.New("oidc: malformed claims")
	}
	var c struct {
		Issuer            string `json:"iss"`
		Subject           string `json:"sub"`
		Audience          any    `json:"aud"`
		Expiry            int64  `json:"exp"`
		IssuedAt          int64  `json:"iat"`
		Nonce             string `json:"nonce"`
		Email             string `json:"email"`
		EmailVerified     *bool  `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := json.Unmarshal(pb, &c); err != nil {
		return Claims{}, errors.New("oidc: malformed claims")
	}
	if normIssuer(c.Issuer) != normIssuer(p.cfg.Issuer) {
		return Claims{}, errors.New("oidc: issuer mismatch")
	}
	if c.Subject == "" {
		return Claims{}, errors.New("oidc: missing subject")
	}
	if !audOK(c.Audience, p.cfg.ClientID) {
		return Claims{}, errors.New("oidc: audience mismatch")
	}
	now := time.Now().Unix()
	if c.Expiry == 0 || now > c.Expiry+60 {
		return Claims{}, errors.New("oidc: token expired")
	}
	if c.IssuedAt != 0 && c.IssuedAt > now+300 {
		return Claims{}, errors.New("oidc: token issued in the future")
	}
	if nonce != "" && subtle.ConstantTimeCompare([]byte(c.Nonce), []byte(nonce)) != 1 {
		return Claims{}, errors.New("oidc: nonce mismatch")
	}
	if c.EmailVerified != nil && !*c.EmailVerified {
		return Claims{}, errors.New("oidc: email is not verified")
	}
	out := Claims{
		Subject: c.Subject, Email: strings.TrimSpace(c.Email),
		Name: c.Name, PreferredUsername: c.PreferredUsername,
		EmailVerified: true,
	}
	if c.EmailVerified != nil {
		out.EmailVerified = *c.EmailVerified
	}
	return out, nil
}

func audOK(aud any, clientID string) bool {
	switch v := aud.(type) {
	case string:
		return subtle.ConstantTimeCompare([]byte(v), []byte(clientID)) == 1
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok && subtle.ConstantTimeCompare([]byte(s), []byte(clientID)) == 1 {
				return true
			}
		}
	}
	return false
}

// FillEmail fetches the userinfo endpoint when the ID token has no
// verified email (a common minimal-token setup).
func (p *Provider) FillEmail(ctx context.Context, c *Claims, accessToken string) error {
	if c.Email != "" && c.EmailVerified {
		return nil
	}
	doc, err := p.discovery(ctx)
	if err != nil {
		return err
	}
	if doc.UserinfoEndpoint == "" || accessToken == "" {
		return errors.New("oidc: no verified email available")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, doc.UserinfoEndpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := p.cfg.client().Do(req)
	if err != nil {
		return fmt.Errorf("oidc userinfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc userinfo: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var u struct {
		Email         string `json:"email"`
		EmailVerified *bool  `json:"email_verified"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return err
	}
	if strings.TrimSpace(u.Email) == "" {
		return errors.New("oidc: no verified email available")
	}
	if u.EmailVerified != nil && !*u.EmailVerified {
		return errors.New("oidc: email is not verified")
	}
	c.Email = strings.TrimSpace(u.Email)
	c.EmailVerified = true
	return nil
}
