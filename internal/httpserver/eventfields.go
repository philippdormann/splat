// Field-level views for the issue detail page: User, SDK, and Request
// context arrive from the SDK as open-ended JSON, so this file turns each
// into a small typed shape with deterministic (sorted) ordering - map
// iteration order in Go is randomized, which previously made these
// sections reorder on every page load - and, for Request in particular,
// redacts anything that looks like a credential before it ever reaches the
// template. Values still reach html/template as plain strings, so Go's
// contextual auto-escaping covers them the same as every other field.
package httpserver

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// sensitiveKeyParts is matched as a case-insensitive substring against a
// field name, not an exact name: real-world request/env/header keys vary by
// framework (Cookie vs HTTP_COOKIE, Authorization vs HTTP_AUTHORIZATION),
// and WSGI's request.env duplicates every header under an HTTP_* name.
// Written without "-"/"_" separators since the key being checked is
// normalized the same way before comparing - otherwise "api_key" wouldn't
// catch "X-Api-Key", and "private_key" wouldn't catch "Private-Key".
var sensitiveKeyParts = []string{
	"authoriz", "cookie", "token", "secret", "password", "passwd", "pwd",
	"session", "apikey", "csrf", "credential", "privatekey",
}

var keySeparators = strings.NewReplacer("-", "", "_", "")

func isSensitiveKey(key string) bool {
	normalized := keySeparators.Replace(strings.ToLower(key))
	for _, part := range sensitiveKeyParts {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}

func redactedValue(key, value string) string {
	if isSensitiveKey(key) {
		return "[redacted]"
	}
	return value
}

// sortedStringMap turns a map into a deterministic, alphabetically sorted
// []keyValue, optionally redacting values whose key looks sensitive.
func sortedStringMap(m map[string]string, redact bool) []keyValue {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]keyValue, 0, len(keys))
	for _, k := range keys {
		v := m[k]
		if redact {
			v = redactedValue(k, v)
		}
		out = append(out, keyValue{Key: k, Value: v})
	}
	return out
}

// sortedAnyMap stringifies each value with the same "%v" formatting used
// elsewhere for Sentry's free-form context objects, then defers to
// sortedStringMap for ordering and redaction.
func sortedAnyMap(m map[string]any, redact bool) []keyValue {
	if len(m) == 0 {
		return nil
	}
	flat := make(map[string]string, len(m))
	for k, v := range m {
		flat[k] = fmt.Sprintf("%v", v)
	}
	return sortedStringMap(flat, redact)
}

// userView is Sentry's user interface (id/username/email/ip_address). Any
// other key is treated as a custom field and surfaced too - Sentry's wire
// format documents a nested "data" sub-object as the place for app-specific
// extras, but in practice apps also just pass extra keys straight to
// set_user() alongside id/email, which land at the top level instead. Both
// shapes are captured, since the alternative is one of them silently
// vanishing depending on which convention a given app happens to use.
type userView struct {
	ID        string
	Username  string
	Email     string
	IPAddress string
	Extra     []keyValue
}

func buildUserView(raw string) *userView {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil || len(m) == 0 {
		return nil
	}
	uv := &userView{}
	if v, ok := m["id"]; ok {
		uv.ID = fmt.Sprintf("%v", v)
	}
	if v, ok := m["username"].(string); ok {
		uv.Username = v
	}
	if v, ok := m["email"].(string); ok {
		uv.Email = v
	}
	switch v, _ := m["ip_address"].(string); v {
	case "":
		// not supplied - leave blank, not "No user context" territory on
		// its own if other fields are present.
	case "{{auto}}":
		// The SDK is asking the server to fill in the real client IP from
		// the request. Real Sentry does that server-side; this collector
		// deliberately doesn't auto-capture IPs that weren't explicitly
		// sent, so the placeholder is labeled instead of resolved.
		uv.IPAddress = "(client IP - not recorded by this server)"
	default:
		uv.IPAddress = v
	}

	extra := map[string]any{}
	if data, ok := m["data"].(map[string]any); ok {
		for k, v := range data {
			extra[k] = v
		}
	}
	for k, v := range m {
		switch k {
		case "id", "username", "email", "ip_address", "data":
			continue
		}
		extra[k] = v
	}
	uv.Extra = sortedAnyMap(extra, true)
	if uv.ID == "" && uv.Username == "" && uv.Email == "" && uv.IPAddress == "" && len(uv.Extra) == 0 {
		return nil
	}
	return uv
}

// sdkView is the SDK's name/version plus which integrations it loaded.
// Sentry's "packages" list (SDK dependency name/version pairs) is dropped:
// it's rarely useful for debugging and was rendering as unreadable nested
// Go map syntax when dumped through the old generic map[string]any view.
type sdkView struct {
	Name         string
	Version      string
	Integrations []string
}

func buildSDKView(raw string) *sdkView {
	var s struct {
		Name         string   `json:"name"`
		Version      string   `json:"version"`
		Integrations []string `json:"integrations"`
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil
	}
	if s.Name == "" && s.Version == "" && len(s.Integrations) == 0 {
		return nil
	}
	sort.Strings(s.Integrations)
	return &sdkView{Name: s.Name, Version: s.Version, Integrations: s.Integrations}
}

// safeRequestHeaders is an allowlist, not a denylist: headers are the
// highest-risk field on the whole page (Authorization and Cookie live
// here), so only names explicitly known to be harmless are ever shown,
// rather than trying to enumerate every secret-ish header name that could
// show up across frameworks.
var safeRequestHeaders = map[string]bool{
	"user-agent": true, "content-type": true, "accept": true,
	"accept-language": true, "accept-encoding": true, "referer": true, "referrer": true,
}

// requestView is Sentry's request interface, filtered for display. Cookies
// are never modeled or read at all - not redacted, just never touched -
// and headers are allowlisted rather than redacted-by-keyword, since an
// unrecognized but sensitive custom header (e.g. "X-Internal-Signing-Key")
// would sail through a denylist that only knows common names.
type requestView struct {
	Method      string
	URL         string
	Path        string
	QueryParams []keyValue
	Headers     []keyValue
	Env         []keyValue
	Body        []keyValue
}

func buildRequestView(raw string) *requestView {
	var req struct {
		URL         string            `json:"url"`
		Method      string            `json:"method"`
		QueryString string            `json:"query_string"`
		Headers     map[string]string `json:"headers"`
		Env         map[string]any    `json:"env"`
		Data        json.RawMessage   `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		return nil
	}
	if req.URL == "" && req.Method == "" && req.QueryString == "" && len(req.Headers) == 0 && len(req.Env) == 0 && len(req.Data) == 0 {
		return nil
	}

	rv := &requestView{Method: req.Method, URL: req.URL}
	if p, err := url.Parse(req.URL); err == nil {
		rv.Path = p.Path
	}
	if req.QueryString != "" {
		if values, err := url.ParseQuery(req.QueryString); err == nil {
			keys := make([]string, 0, len(values))
			for k := range values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				rv.QueryParams = append(rv.QueryParams, keyValue{Key: k, Value: redactedValue(k, strings.Join(values[k], ", "))})
			}
		}
	}
	if len(req.Headers) > 0 {
		keys := make([]string, 0, len(req.Headers))
		for k := range req.Headers {
			if safeRequestHeaders[strings.ToLower(k)] {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			rv.Headers = append(rv.Headers, keyValue{Key: k, Value: req.Headers[k]})
		}
	}
	rv.Env = sortedAnyMap(req.Env, true)
	rv.Body = sortedAnyMap(dataAsMap(req.Data), true)
	return rv
}

// dataAsMap handles the request body's two realistic shapes: a JSON object
// (form data decoded by the SDK, most common) or a bare string (raw body
// the SDK didn't parse). Anything else (array, number, null) is skipped
// rather than guessed at.
func dataAsMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err == nil {
		return m
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		return map[string]any{"body": s}
	}
	return nil
}
