package sentryprotocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventParsedTimestampHandlesBothShapes(t *testing.T) {
	epoch := Event{Timestamp: json.RawMessage(`1700000000.123456`)}
	got := epoch.ParsedTimestamp()
	want := time.Unix(1700000000, 123456*1000).UTC()
	if diff := got.Sub(want); diff < -time.Microsecond || diff > time.Microsecond {
		// float64 can't represent fractional seconds exactly, so compare
		// within a microsecond rather than asserting bit-for-bit equality.
		t.Fatalf("epoch float: got %v, want %v (diff %v)", got, want, diff)
	}

	iso := Event{Timestamp: json.RawMessage(`"2026-01-15T10:30:00Z"`)}
	got = iso.ParsedTimestamp()
	want = time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("iso8601: got %v, want %v", got, want)
	}

	// Missing entirely - falls back to "now" rather than the zero time,
	// since an event is persisted regardless and needs *some* timestamp.
	missing := Event{}
	if time.Since(missing.ParsedTimestamp()) > time.Minute {
		t.Fatalf("expected a fallback to roughly now, got %v", missing.ParsedTimestamp())
	}
}

// Regression test: Breadcrumb.Timestamp used to be displayed via a plain
// strings.TrimSuffix(string(raw), `"`), which only strips the trailing
// quote of a JSON string and leaves a stray leading one (and does nothing
// at all for the epoch-float shape). ParsedTimestamp fixes this by sharing
// Event's real parser instead of string-munging the raw JSON.
func TestBreadcrumbParsedTimestampHandlesBothShapesAndMissing(t *testing.T) {
	b := Breadcrumb{Timestamp: json.RawMessage(`"2026-01-15T10:30:00Z"`)}
	got, ok := b.ParsedTimestamp()
	if !ok {
		t.Fatal("expected ok=true for a valid ISO8601 timestamp")
	}
	if want := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	epoch := Breadcrumb{Timestamp: json.RawMessage(`1700000000`)}
	if got, ok := epoch.ParsedTimestamp(); !ok || !got.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Fatalf("epoch shape: got %v, ok=%v", got, ok)
	}

	missing := Breadcrumb{}
	if _, ok := missing.ParsedTimestamp(); ok {
		t.Fatal("expected ok=false for a missing timestamp - must not fabricate one")
	}
}
