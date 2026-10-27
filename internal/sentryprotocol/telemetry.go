package sentryprotocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// LogBatch is the payload of an envelope item with {"type":"log"}: the
// shape SDKs with logs enabled (sentry_sdk.logger.* / Sentry.logger.*)
// send. Reference: https://develop.sentry.dev/sdk/telemetry/logs/
//
// Fields are parsed defensively: firmware-era SDKs disagree on names
// ("body" vs "message", "severity_text" vs "level"), so every accessor
// below falls back rather than failing the batch.
type LogBatch struct {
	Items []LogRecord `json:"items"`
}

type LogRecord struct {
	Timestamp      json.RawMessage `json:"timestamp"` // float seconds OR ISO8601 string
	TraceID        string          `json:"trace_id"`
	SpanID         string          `json:"span_id"`
	SeverityNumber *int            `json:"severity_number"`
	SeverityText   string          `json:"severity_text"`
	Level          string          `json:"level"`
	Body           string          `json:"body"`
	Message        string          `json:"message"`
	Attributes     json.RawMessage `json:"attributes"`
}

// ParsedTimestamp converts the record's timestamp, falling back to now -
// a log line is persisted regardless, so it needs *some* timestamp.
func (l *LogRecord) ParsedTimestamp() time.Time {
	if t, ok := parseFlexibleTimestamp(l.Timestamp); ok {
		return t
	}
	return time.Now().UTC()
}

// severityText maps an OTel-style severity number (1-24) to a Sentry log
// level. Only used when the SDK sent no usable text level.
func severityText(n int) string {
	switch {
	case n >= 21:
		return "fatal"
	case n >= 17:
		return "error"
	case n >= 13:
		return "warn"
	case n >= 9:
		return "info"
	case n >= 5:
		return "debug"
	case n >= 1:
		return "trace"
	default:
		return "info"
	}
}

// NormalizedLevel normalizes the record's level to a lowercase Sentry level,
// defaulting to info.
func (l *LogRecord) NormalizedLevel() string {
	for _, cand := range []string{l.SeverityText, l.Level} {
		if lvl := normalizeLogLevel(cand); lvl != "" {
			return lvl
		}
	}
	if l.SeverityNumber != nil {
		return severityText(*l.SeverityNumber)
	}
	return "info"
}

func normalizeLogLevel(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return "trace"
	case "debug":
		return "debug"
	case "info", "information":
		return "info"
	case "warn", "warning":
		return "warn"
	case "error", "err":
		return "error"
	case "fatal", "critical", "dpanic", "panic":
		return "fatal"
	default:
		return ""
	}
}

// Text returns the log body, tolerating both field names.
func (l *LogRecord) Text() string {
	if l.Body != "" {
		return l.Body
	}
	return l.Message
}

// ParseLogBatch decodes a single log-item payload.
func ParseLogBatch(b []byte) (*LogBatch, error) {
	var batch LogBatch
	if err := json.Unmarshal(b, &batch); err != nil {
		return nil, fmt.Errorf("decode log batch: %w", err)
	}
	return &batch, nil
}

// Transaction is the subset of Sentry's transaction event schema this
// collector stores: identity, timing, trace context, and the span list.
// Measurements, profiling sidecars, and request/user blobs ride along
// unparsed - this is an error tracker with trace visibility, not APM.
// Reference: https://develop.sentry.dev/sdk/telemetry/traces/
type Transaction struct {
	EventID        string          `json:"event_id"`
	Name           string          `json:"transaction"`
	Platform       string          `json:"platform"`
	Release        string          `json:"release"`
	Environment    string          `json:"environment"`
	StartTimestamp json.RawMessage `json:"start_timestamp"`
	Timestamp      json.RawMessage `json:"timestamp"` // end timestamp
	Contexts       json.RawMessage `json:"contexts"`
	Spans          []Span          `json:"spans"`
}

// TraceContext is the decoded contexts.trace object.
type TraceContext struct {
	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ParentSpanID string `json:"parent_span_id"`
	Op           string `json:"op"`
	Status       string `json:"status"`
}

func (t *Transaction) Trace() TraceContext {
	var wrapper struct {
		Trace TraceContext `json:"trace"`
	}
	if len(t.Contexts) == 0 {
		return TraceContext{}
	}
	if err := json.Unmarshal(t.Contexts, &wrapper); err != nil {
		return TraceContext{}
	}
	return wrapper.Trace
}

// Span is one entry of a transaction's spans array.
type Span struct {
	TraceID        string          `json:"trace_id"`
	SpanID         string          `json:"span_id"`
	ParentSpanID   string          `json:"parent_span_id"`
	Op             string          `json:"op"`
	Description    string          `json:"description"`
	Status         string          `json:"status"`
	StartTimestamp json.RawMessage `json:"start_timestamp"`
	Timestamp      json.RawMessage `json:"timestamp"` // end timestamp
	Tags           Tags            `json:"tags"`
	Data           json.RawMessage `json:"data"`
}

// StartEnd resolves the span's time range; missing ends fall back to the
// start so duration math never goes negative or NaN.
func (s *Span) StartEnd() (start, end time.Time) {
	start, _ = parseFlexibleTimestamp(s.StartTimestamp)
	end, ok := parseFlexibleTimestamp(s.Timestamp)
	if !ok {
		end = start
	}
	return start, end
}

// DurationMS is the span's wall time in milliseconds, floored at 0.
func (s *Span) DurationMS() float64 {
	start, end := s.StartEnd()
	if d := end.Sub(start).Seconds() * 1000; d > 0 {
		return d
	}
	return 0
}

// ParseTransaction decodes a single transaction-item payload.
func ParseTransaction(b []byte) (*Transaction, error) {
	var t Transaction
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("decode transaction: %w", err)
	}
	return &t, nil
}
