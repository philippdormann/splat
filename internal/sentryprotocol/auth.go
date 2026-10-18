package sentryprotocol

import (
	"net/url"
	"strings"
)

// ParseAuthHeader extracts sentry_key from an X-Sentry-Auth header value,
// e.g. "Sentry sentry_version=7, sentry_key=abc123, sentry_client=sentry.python/2.0".
func ParseAuthHeader(header string) string {
	header = strings.TrimPrefix(strings.TrimSpace(header), "Sentry ")
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && k == "sentry_key" {
			return v
		}
	}
	return ""
}

// KeyFromDSN extracts the public key (DSN "username") from a full DSN URL,
// as carried in an envelope header's "dsn" field.
func KeyFromDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return ""
	}
	return u.User.Username()
}
