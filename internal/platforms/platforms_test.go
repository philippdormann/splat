package platforms

import "testing"

func TestEveryPlatformRendersWithoutPanicking(t *testing.T) {
	for _, p := range All {
		if p.ID == "" || p.Label == "" || p.Install == "" {
			t.Fatalf("platform %+v missing required fields", p)
		}
		snippet := p.Snippet("http://key@host/1", "production")
		if snippet == "" {
			t.Fatalf("platform %s produced an empty snippet", p.ID)
		}
	}
}

func TestGetFallsBackToDefaultForUnknownID(t *testing.T) {
	got := Get("totally-made-up")
	if got.ID != DefaultID {
		t.Fatalf("expected fallback to %s, got %s", DefaultID, got.ID)
	}
	if got.ID != Get("").ID {
		t.Fatal("empty id should fall back the same way as an unknown id")
	}
}

func TestValid(t *testing.T) {
	if !Valid(DefaultID) {
		t.Fatal("default id should be valid")
	}
	if Valid("nope") {
		t.Fatal("unknown id should not be valid")
	}
}
