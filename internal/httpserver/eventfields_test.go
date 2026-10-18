package httpserver

import (
	"strings"
	"testing"
)

func TestIsSensitiveKeyMatchesCommonSecretNames(t *testing.T) {
	sensitive := []string{
		"Authorization", "authorization", "Cookie", "HTTP_COOKIE", "HTTP_AUTHORIZATION",
		"api_key", "apiKey", "X-Api-Key", "password", "session_id", "csrf_token", "private_key",
	}
	for _, k := range sensitive {
		if !isSensitiveKey(k) {
			t.Errorf("expected %q to be flagged sensitive", k)
		}
	}
	safe := []string{"username", "email", "url", "method", "status_code", "environment"}
	for _, k := range safe {
		if isSensitiveKey(k) {
			t.Errorf("expected %q to NOT be flagged sensitive", k)
		}
	}
}

func TestSortedStringMapIsDeterministicAndRedacts(t *testing.T) {
	m := map[string]string{"zebra": "z", "apple": "a", "password": "hunter2"}
	got := sortedStringMap(m, true)
	if len(got) != 3 || got[0].Key != "apple" || got[1].Key != "password" || got[2].Key != "zebra" {
		t.Fatalf("expected alphabetically sorted keys, got %+v", got)
	}
	if got[1].Value != "[redacted]" {
		t.Fatalf("expected password value redacted, got %+v", got[1])
	}
	if got[0].Value != "a" {
		t.Fatalf("non-sensitive value should pass through unredacted, got %+v", got[0])
	}
}

// buildUserView covers Sentry's Python SDK user shape - a few well-known
// fields plus an arbitrary "data" sub-object for app-specific extras.
func TestBuildUserViewOrdersKnownFieldsAndRedactsExtras(t *testing.T) {
	raw := `{"id": 42, "email": "alice@example.com", "ip_address": "{{auto}}", "data": {"plan": "pro", "api_token": "sk-secret"}}`
	uv := buildUserView(raw)
	if uv == nil {
		t.Fatal("expected a non-nil userView")
	}
	if uv.ID != "42" || uv.Email != "alice@example.com" {
		t.Fatalf("got %+v", uv)
	}
	if uv.IPAddress == "{{auto}}" {
		t.Fatal("the {{auto}} placeholder must never be shown verbatim - it looks like a bug, not a value")
	}
	// Sorted alphabetically: "api_token" before "plan".
	if len(uv.Extra) != 2 || uv.Extra[0].Key != "api_token" || uv.Extra[0].Value != "[redacted]" {
		t.Fatalf("expected api_token redacted among sorted extras, got %+v", uv.Extra)
	}
	if uv.Extra[1].Key != "plan" || uv.Extra[1].Value != "pro" {
		t.Fatalf("expected non-sensitive extra to pass through, got %+v", uv.Extra)
	}
}

// Sentry's wire format documents custom user fields nested under "data",
// but apps calling sentry_sdk.set_user({"id": ..., "plan": "pro"}) just as
// often put them at the top level instead. Both must survive.
func TestBuildUserViewCapturesTopLevelCustomFieldsToo(t *testing.T) {
	raw := `{"id": "42", "plan": "pro", "auth_token": "sk-secret"}`
	uv := buildUserView(raw)
	if uv == nil {
		t.Fatal("expected a non-nil userView")
	}
	var plan, authToken string
	for _, kv := range uv.Extra {
		switch kv.Key {
		case "plan":
			plan = kv.Value
		case "auth_token":
			authToken = kv.Value
		}
	}
	if plan != "pro" {
		t.Fatalf("expected top-level custom field 'plan' to survive, got extras=%+v", uv.Extra)
	}
	if authToken != "[redacted]" {
		t.Fatalf("expected top-level custom field 'auth_token' to be redacted, got extras=%+v", uv.Extra)
	}
}

func TestBuildUserViewReturnsNilWhenEmpty(t *testing.T) {
	if uv := buildUserView(`{}`); uv != nil {
		t.Fatalf("expected nil for an empty user object, got %+v", uv)
	}
	if uv := buildUserView(`not json`); uv != nil {
		t.Fatalf("expected nil for invalid json, got %+v", uv)
	}
}

func TestBuildSDKViewSurfacesNameVersionAndIntegrations(t *testing.T) {
	raw := `{"name": "sentry.python.django", "version": "2.18.0", "integrations": ["stdlib", "django"],
		"packages": [{"name": "pypi:sentry-sdk", "version": "2.18.0"}]}`
	sv := buildSDKView(raw)
	if sv == nil || sv.Name != "sentry.python.django" || sv.Version != "2.18.0" {
		t.Fatalf("got %+v", sv)
	}
	if len(sv.Integrations) != 2 || sv.Integrations[0] != "django" {
		t.Fatalf("expected sorted integrations, got %+v", sv.Integrations)
	}
}

// TestBuildRequestViewNeverExposesCredentials is the core requirement-5
// regression test: a representative Django/Flask-style request payload
// carrying an Authorization header, a Cookie header, a cookies dict, a
// WSGI-style HTTP_COOKIE env entry, a secret query param, and a password
// field in the POST body. None of those six should ever reach the view.
func TestBuildRequestViewNeverExposesCredentials(t *testing.T) {
	raw := `{
		"url": "http://example.com/checkout/",
		"method": "POST",
		"query_string": "ref=email&token=super-secret-token",
		"headers": {
			"User-Agent": "Mozilla/5.0",
			"Content-Type": "application/x-www-form-urlencoded",
			"Authorization": "Bearer top-secret-bearer-token",
			"Cookie": "sessionid=abc123"
		},
		"cookies": {"sessionid": "abc123", "csrftoken": "xyz"},
		"env": {"REMOTE_ADDR": "203.0.113.5", "HTTP_COOKIE": "sessionid=abc123", "HTTP_AUTHORIZATION": "Bearer top-secret-bearer-token"},
		"data": {"username": "alice", "password": "hunter2"}
	}`
	rv := buildRequestView(raw)
	if rv == nil {
		t.Fatal("expected a non-nil requestView")
	}
	if rv.Method != "POST" || rv.Path != "/checkout/" {
		t.Fatalf("got method=%q path=%q", rv.Method, rv.Path)
	}

	rendered := renderAllFields(rv)
	for _, secret := range []string{"top-secret-bearer-token", "abc123", "hunter2", "super-secret-token"} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("secret %q leaked into the request view: %+v", secret, rv)
		}
	}
	// The query param's NAME is still useful for debugging even though its
	// value is hidden - only the value gets redacted, not the whole param.
	foundRedactedToken := false
	for _, kv := range rv.QueryParams {
		if kv.Key == "token" && kv.Value == "[redacted]" {
			foundRedactedToken = true
		}
	}
	if !foundRedactedToken {
		t.Fatalf("expected query param %q present with a redacted value, got %+v", "token", rv.QueryParams)
	}
	for _, kv := range rv.Headers {
		if kv.Key == "Authorization" || kv.Key == "Cookie" {
			t.Fatalf("Authorization/Cookie headers must never pass the allowlist, got %+v", rv.Headers)
		}
	}
	// "cookies" is never modeled at all - requestView has no field for it.
}

func renderAllFields(rv *requestView) string {
	out := rv.Method + rv.URL + rv.Path
	for _, kv := range rv.QueryParams {
		out += kv.Key + kv.Value
	}
	for _, kv := range rv.Headers {
		out += kv.Key + kv.Value
	}
	for _, kv := range rv.Env {
		out += kv.Key + kv.Value
	}
	for _, kv := range rv.Body {
		out += kv.Key + kv.Value
	}
	return out
}

func TestBuildRequestViewReturnsNilWhenEmpty(t *testing.T) {
	if rv := buildRequestView(`{}`); rv != nil {
		t.Fatalf("expected nil for an empty request object, got %+v", rv)
	}
}
