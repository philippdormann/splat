package sentryprotocol

import "testing"

func TestParseEnvelopeWithLength(t *testing.T) {
	// The length-prefixed payload below embeds a raw newline, which plain
	// line-splitting would misparse as the end of the item; the explicit
	// "length" field must take priority.
	body := "{\"event_id\":\"abc\"}\n" +
		"{\"type\":\"attachment\",\"length\":5}\n" +
		"ab\ncd\n" +
		"{\"type\":\"event\"}\n{\"message\":\"hi\"}\n"

	env, err := ParseEnvelope([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if env.Header.EventID != "abc" {
		t.Fatalf("event id = %q", env.Header.EventID)
	}
	if len(env.Items) != 2 {
		t.Fatalf("want 2 items, got %d: %+v", len(env.Items), env.Items)
	}
	if string(env.Items[0].Payload) != "ab\ncd" {
		t.Fatalf("length-prefixed payload = %q", env.Items[0].Payload)
	}
	if env.Items[1].Header.Type != "event" || string(env.Items[1].Payload) != `{"message":"hi"}` {
		t.Fatalf("second item = %+v", env.Items[1])
	}
}

func TestParseEnvelopeNoLength(t *testing.T) {
	body := "{\"event_id\":\"e1\"}\n{\"type\":\"event\"}\n{\"message\":\"hi\"}\n"
	env, err := ParseEnvelope([]byte(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(env.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(env.Items))
	}
	if string(env.Items[0].Payload) != `{"message":"hi"}` {
		t.Fatalf("payload = %q", env.Items[0].Payload)
	}
}

func TestParseEnvelopeRejectsOversizedLength(t *testing.T) {
	body := "{}\n{\"type\":\"event\",\"length\":999999}\nshort\n"
	if _, err := ParseEnvelope([]byte(body)); err == nil {
		t.Fatal("expected error for length exceeding remaining body")
	}
}

func TestParseAuthHeader(t *testing.T) {
	got := ParseAuthHeader("Sentry sentry_version=7, sentry_client=sentry.python/2.0, sentry_key=abc123")
	if got != "abc123" {
		t.Fatalf("got %q", got)
	}
}

func TestKeyFromDSN(t *testing.T) {
	got := KeyFromDSN("http://pubkey123@errors.example.com/api/5")
	if got != "pubkey123" {
		t.Fatalf("got %q", got)
	}
}

func TestMessageFieldBothShapes(t *testing.T) {
	var m MessageField
	if err := m.UnmarshalJSON([]byte(`"plain string"`)); err != nil || m.Text != "plain string" {
		t.Fatalf("plain string: %v %q", err, m.Text)
	}
	if err := m.UnmarshalJSON([]byte(`{"formatted":"fmt text","message":"raw"}`)); err != nil || m.Text != "fmt text" {
		t.Fatalf("object form: %v %q", err, m.Text)
	}
}

func TestTagsBothShapes(t *testing.T) {
	var t1 Tags
	if err := t1.UnmarshalJSON([]byte(`{"a":"1","b":"2"}`)); err != nil || t1["a"] != "1" {
		t.Fatalf("object form: %v %+v", err, t1)
	}
	var t2 Tags
	if err := t2.UnmarshalJSON([]byte(`[["a","1"],["b","2"]]`)); err != nil || t2["b"] != "2" {
		t.Fatalf("array form: %v %+v", err, t2)
	}
}

func TestParsedTimestampUnixFloat(t *testing.T) {
	e := Event{Timestamp: []byte(`1700000000.5`)}
	ts := e.ParsedTimestamp()
	if ts.Unix() != 1700000000 {
		t.Fatalf("got unix %d", ts.Unix())
	}
}

func TestParsedTimestampISO(t *testing.T) {
	e := Event{Timestamp: []byte(`"2023-11-14T22:13:20Z"`)}
	ts := e.ParsedTimestamp()
	if ts.Year() != 2023 || ts.Month() != 11 || ts.Day() != 14 {
		t.Fatalf("got %v", ts)
	}
}
