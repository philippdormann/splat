package issues

import (
	"testing"

	"errgotrack/internal/sentryprotocol"
)

func exceptionEvent(excType string, frames []sentryprotocol.Frame) *sentryprotocol.Event {
	return &sentryprotocol.Event{
		Exception: &sentryprotocol.ExceptionValues{Values: []sentryprotocol.ExceptionValue{
			{Type: excType, Value: "irrelevant for grouping", Stacktrace: &sentryprotocol.Stacktrace{Frames: frames}},
		}},
	}
}

func TestFingerprintGroupsSameTypeAndFrames(t *testing.T) {
	frames := []sentryprotocol.Frame{{Module: "myapp.views", Function: "checkout", InApp: true}}
	a := Fingerprint(exceptionEvent("ValueError", frames))
	b := Fingerprint(exceptionEvent("ValueError", frames))
	if a != b {
		t.Fatalf("identical exception shape produced different fingerprints: %s vs %s", a, b)
	}
}

func TestFingerprintIgnoresMessageVariation(t *testing.T) {
	frames := []sentryprotocol.Frame{{Module: "myapp.views", Function: "checkout", InApp: true}}
	e1 := exceptionEvent("ValueError", frames)
	e1.Exception.Values[0].Value = "Invalid quantity: -1"
	e2 := exceptionEvent("ValueError", frames)
	e2.Exception.Values[0].Value = "Invalid quantity: -99"
	if Fingerprint(e1) != Fingerprint(e2) {
		t.Fatal("fingerprint should not depend on the exception message, only type+frames, since messages often embed request-specific values")
	}
}

func TestFingerprintDiffersByFunction(t *testing.T) {
	a := Fingerprint(exceptionEvent("ValueError", []sentryprotocol.Frame{{Module: "myapp.views", Function: "checkout", InApp: true}}))
	b := Fingerprint(exceptionEvent("ValueError", []sentryprotocol.Frame{{Module: "myapp.views", Function: "cancel", InApp: true}}))
	if a == b {
		t.Fatal("different originating functions must not collapse into the same issue")
	}
}

func TestFingerprintIgnoresNonInAppFrames(t *testing.T) {
	withNoise := exceptionEvent("ValueError", []sentryprotocol.Frame{
		{Module: "django.core.handlers", Function: "_get_response", InApp: false},
		{Module: "myapp.views", Function: "checkout", InApp: true},
	})
	clean := exceptionEvent("ValueError", []sentryprotocol.Frame{
		{Module: "myapp.views", Function: "checkout", InApp: true},
	})
	if Fingerprint(withNoise) != Fingerprint(clean) {
		t.Fatal("framework frames (in_app=false) should not affect grouping")
	}
}

func TestFingerprintHonorsClientFingerprint(t *testing.T) {
	e := exceptionEvent("ValueError", []sentryprotocol.Frame{{Module: "a", Function: "b", InApp: true}})
	e.Fingerprint = []string{"custom-group-key"}
	other := exceptionEvent("TypeError", nil)
	other.Fingerprint = []string{"custom-group-key"}
	if Fingerprint(e) != Fingerprint(other) {
		t.Fatal("an explicit client-supplied fingerprint must override automatic grouping")
	}
}

func TestFingerprintBareMessageEvent(t *testing.T) {
	e1 := &sentryprotocol.Event{Logger: "myapp", Message: sentryprotocol.MessageField{Text: "disk usage high"}}
	e2 := &sentryprotocol.Event{Logger: "myapp", Message: sentryprotocol.MessageField{Text: "disk usage high"}}
	if Fingerprint(e1) != Fingerprint(e2) {
		t.Fatal("identical bare-message events should group together")
	}
}
