package platforms

import "testing"

func TestEveryPlatformRendersWithoutPanicking(t *testing.T) {
	for _, p := range All {
		if p.ID == "" || p.Label == "" || p.Language == "" || p.Install == "" {
			t.Fatalf("platform %+v missing required fields", p)
		}
		snippet := p.Snippet("http://key@host/1", "production")
		if snippet == "" {
			t.Fatalf("platform %s produced an empty snippet", p.ID)
		}
	}
	if len(All) < 100 {
		t.Fatalf("expected rustrak parity (>=100 platforms), got %d", len(All))
	}
	seen := map[string]bool{}
	for _, p := range All {
		if seen[p.ID] {
			t.Fatalf("duplicate platform id %s", p.ID)
		}
		seen[p.ID] = true
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

func TestAliasesResolveToCanonical(t *testing.T) {
	for alias, target := range Aliases {
		if !Valid(alias) {
			t.Fatalf("alias %s should be valid", alias)
		}
		if got := Get(alias); got.ID != target {
			t.Fatalf("alias %s resolved to %s, want %s", alias, got.ID, target)
		}
	}
}

func TestRustrakCanonicalIDsPresent(t *testing.T) {
	for _, id := range []string{
		"node", "javascript", "javascript-nextjs", "python", "python-django",
		"go", "go-gin", "ruby-rails", "php-laravel", "java", "java-spring-boot",
		"dotnet-aspnetcore", "flutter", "react-native", "rust", "bun",
		"electron", "unity",
	} {
		if !Valid(id) {
			t.Fatalf("canonical rustrak id %s missing", id)
		}
	}
}
