package traces

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/philippdormann/splat/internal/sentryprotocol"
)

// parseTime shares the event timestamp wire format (Unix float or ISO8601)
// with transactions and spans.
func parseTime(raw json.RawMessage) (time.Time, bool) {
	return sentryprotocol.ParseFlexibleTimestamp(raw)
}

func newEventID() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
