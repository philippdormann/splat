package sentryprotocol

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Event is the subset of the Sentry error event schema this collector
// understands. Fields outside this set (performance spans, profiling data,
// etc.) are accepted but ignored.
type Event struct {
	EventID     string            `json:"event_id"`
	Timestamp   json.RawMessage   `json:"timestamp"` // unix float seconds OR ISO8601 string
	Level       string            `json:"level"`
	Logger      string            `json:"logger"`
	Platform    string            `json:"platform"`
	ServerName  string            `json:"server_name"`
	Release     string            `json:"release"`
	Environment string            `json:"environment"`
	Transaction string            `json:"transaction"`
	Message     MessageField      `json:"message"`
	Exception   *ExceptionValues  `json:"exception"`
	Breadcrumbs *BreadcrumbValues `json:"breadcrumbs"`
	Request     json.RawMessage   `json:"request"`
	User        json.RawMessage   `json:"user"`
	Tags        Tags              `json:"tags"`
	Extra       json.RawMessage   `json:"extra"`
	SDK         json.RawMessage   `json:"sdk"`
	Fingerprint []string          `json:"fingerprint"`
	// Contexts holds Sentry's free-form "contexts" object: browser, os,
	// device, runtime, culture, trace (distributed tracing ids), and any
	// SDK/framework-specific context (e.g. a JS SDK's "react" context).
	// Kept as raw key->object JSON and rendered generically rather than
	// modeled field by field, since this set is open-ended by design.
	Contexts json.RawMessage `json:"contexts"`
}

// MessageField handles Sentry's two message shapes: a bare string, or
// {"formatted": "...", "message": "...", "params": [...]}.
type MessageField struct {
	Text string
}

func (m *MessageField) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		m.Text = s
		return nil
	}
	var obj struct {
		Formatted string `json:"formatted"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	if obj.Formatted != "" {
		m.Text = obj.Formatted
	} else {
		m.Text = obj.Message
	}
	return nil
}

// Tags handles both the object form {"k":"v"} and the legacy array form
// [["k","v"], ...].
type Tags map[string]string

func (t *Tags) UnmarshalJSON(b []byte) error {
	*t = Tags{}
	var asMap map[string]string
	if err := json.Unmarshal(b, &asMap); err == nil {
		*t = asMap
		return nil
	}
	var asPairs [][2]string
	if err := json.Unmarshal(b, &asPairs); err != nil {
		return fmt.Errorf("tags: unsupported shape: %w", err)
	}
	for _, p := range asPairs {
		(*t)[p[0]] = p[1]
	}
	return nil
}

type ExceptionValues struct {
	Values []ExceptionValue `json:"values"`
}

type ExceptionValue struct {
	Type       string      `json:"type"`
	Value      string      `json:"value"`
	Module     string      `json:"module"`
	Stacktrace *Stacktrace `json:"stacktrace"`
}

type Stacktrace struct {
	Frames []Frame `json:"frames"`
}

type Frame struct {
	Filename    string   `json:"filename"`
	AbsPath     string   `json:"abs_path"`
	Function    string   `json:"function"`
	Module      string   `json:"module"`
	LineNo      int      `json:"lineno"`
	ColNo       int      `json:"colno"`
	ContextLine string   `json:"context_line"`
	PreContext  []string `json:"pre_context"`
	PostContext []string `json:"post_context"`
	// Vars holds each captured local variable's value. Despite the name,
	// SDKs don't always repr() these down to plain strings - e.g. Flask's
	// integration can serialize a dict/list-valued local as actual nested
	// JSON rather than a string (confirmed: this broke parsing of a real
	// Flask-captured event before Vars was widened from map[string]string
	// to this). Rendered generically at display time regardless of shape.
	Vars  map[string]json.RawMessage `json:"vars"`
	InApp bool                       `json:"in_app"`
}

type BreadcrumbValues struct {
	Values []Breadcrumb `json:"values"`
}

type Breadcrumb struct {
	Timestamp json.RawMessage `json:"timestamp"`
	Type      string          `json:"type"`
	Category  string          `json:"category"`
	Level     string          `json:"level"`
	Message   string          `json:"message"`
	Data      json.RawMessage `json:"data"`
}

// parseFlexibleTimestamp handles Sentry's two timestamp shapes - a Unix
// epoch float or an ISO8601 string - shared by both Event and Breadcrumb,
// which use the identical wire format for this field.
func parseFlexibleTimestamp(raw json.RawMessage) (time.Time, bool) {
	s := string(raw)
	if s == "" || s == "null" {
		return time.Time{}, false
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		sec := int64(f)
		nsec := int64((f - float64(sec)) * 1e9)
		return time.Unix(sec, nsec).UTC(), true
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		if t, err := time.Parse(time.RFC3339Nano, str); err == nil {
			return t.UTC(), true
		}
		if t, err := time.Parse(time.RFC3339, str); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// ParsedTimestamp converts the event's timestamp field, falling back to now
// if absent or unparseable - an event is persisted regardless, so it needs
// *some* timestamp, unlike a breadcrumb which can just omit one.
func (e *Event) ParsedTimestamp() time.Time {
	if t, ok := parseFlexibleTimestamp(e.Timestamp); ok {
		return t
	}
	return time.Now().UTC()
}

// ParsedTimestamp returns the breadcrumb's timestamp, and false if it was
// missing or in a shape this collector doesn't recognize - callers should
// show an empty state rather than fabricate a time for display.
func (b *Breadcrumb) ParsedTimestamp() (time.Time, bool) {
	return parseFlexibleTimestamp(b.Timestamp)
}

// ParseEventPayload decodes a single event JSON payload, as found either in
// an envelope "event" item or posted directly to the legacy /store/ endpoint.
func ParseEventPayload(b []byte) (*Event, error) {
	var e Event
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("decode event: %w", err)
	}
	return &e, nil
}
