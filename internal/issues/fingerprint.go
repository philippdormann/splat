package issues

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"errgotrack/internal/sentryprotocol"
)

// Fingerprint derives a stable grouping key for an event. It prefers a
// client-supplied fingerprint (sentry-sdk's `set_tag`/`fingerprint` API or
// the SDK's own grouping hints), and otherwise derives one from the
// exception type plus the function/module of each in-app stack frame -
// deliberately excluding the exception message, which commonly embeds
// request-specific values (ids, usernames) that would otherwise fragment one
// bug into many issues.
func Fingerprint(e *sentryprotocol.Event) string {
	if len(e.Fingerprint) > 0 {
		return hash(strings.Join(e.Fingerprint, "|"))
	}

	var parts []string
	if e.Exception != nil && len(e.Exception.Values) > 0 {
		// Group on the deepest (originating) exception in a chain.
		exc := e.Exception.Values[len(e.Exception.Values)-1]
		parts = append(parts, "exc", exc.Type)
		if exc.Stacktrace != nil {
			for _, f := range exc.Stacktrace.Frames {
				if !f.InApp {
					continue
				}
				parts = append(parts, f.Module+"."+f.Function)
			}
		}
	} else {
		// No exception payload (e.g. a bare message event): group on
		// logger + message, since there's no stack to anchor on.
		parts = append(parts, "msg", e.Logger, e.Message.Text)
	}
	return hash(strings.Join(parts, "|"))
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
