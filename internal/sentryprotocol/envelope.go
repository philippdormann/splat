// Package sentryprotocol implements just enough of the Sentry ingestion
// protocol (envelope + legacy store endpoint, auth header/DSN parsing) to
// accept events from the official Python/Django SDK.
//
// Reference: https://develop.sentry.dev/sdk/envelopes/
// Reference: https://develop.sentry.dev/sdk/store/
//
// Unsupported on purpose: transactions/performance items, attachments,
// client reports, replays, profiles, metrics. These item types are
// parsed enough to skip over safely but their payloads are discarded.
// Sessions ("session"/"sessions" items) ARE stored - see
// internal/sessions - for release-health aggregation.
package sentryprotocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type EnvelopeHeader struct {
	EventID string `json:"event_id"`
	DSN     string `json:"dsn"`
	SentAt  string `json:"sent_at"`
}

type ItemHeader struct {
	Type   string `json:"type"`
	Length *int   `json:"length"`
}

type Item struct {
	Header  ItemHeader
	Payload []byte
}

type Envelope struct {
	Header EnvelopeHeader
	Items  []Item
}

const maxItems = 50 // defends against pathological envelopes; real SDKs send a handful

// ParseEnvelope parses the newline-delimited envelope format used by
// POST /api/<project>/envelope/.
func ParseEnvelope(body []byte) (*Envelope, error) {
	env := &Envelope{}

	headerLine, rest := splitLine(body)
	if len(bytes.TrimSpace(headerLine)) > 0 {
		if err := json.Unmarshal(headerLine, &env.Header); err != nil {
			return nil, fmt.Errorf("envelope header: %w", err)
		}
	}

	for len(rest) > 0 {
		itemHeaderLine, after := splitLine(rest)
		if len(bytes.TrimSpace(itemHeaderLine)) == 0 {
			rest = after
			continue
		}
		var ih ItemHeader
		if err := json.Unmarshal(itemHeaderLine, &ih); err != nil {
			return nil, fmt.Errorf("item header: %w", err)
		}
		rest = after

		var payload []byte
		if ih.Length != nil {
			n := *ih.Length
			if n < 0 || n > len(rest) {
				return nil, fmt.Errorf("item length %d exceeds remaining body", n)
			}
			payload = rest[:n]
			rest = rest[n:]
			if len(rest) > 0 && rest[0] == '\n' {
				rest = rest[1:]
			}
		} else {
			payload, rest = splitLine(rest)
		}

		if len(env.Items) >= maxItems {
			return nil, fmt.Errorf("envelope exceeds %d items", maxItems)
		}
		env.Items = append(env.Items, Item{Header: ih, Payload: payload})
	}
	return env, nil
}

func splitLine(b []byte) (line, rest []byte) {
	idx := bytes.IndexByte(b, '\n')
	if idx < 0 {
		return b, nil
	}
	return b[:idx], b[idx+1:]
}
