package secretbox

import (
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	enc, err := Encrypt("test-server-secret-32-bytes-minimum!!", `{"bot_token":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(enc) {
		t.Fatalf("expected prefix, got %q", enc)
	}
	if strings.Contains(enc, "x") && strings.Contains(enc, "bot_token") {
		// Ciphertext must not leak plaintext (base64 of random nonce aside).
		t.Fatalf("ciphertext leaks plaintext: %q", enc)
	}
	dec, err := Decrypt("test-server-secret-32-bytes-minimum!!", enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != `{"bot_token":"x"}` {
		t.Fatalf("round trip = %q", dec)
	}
}

func TestLegacyPlaintextPassthrough(t *testing.T) {
	for _, v := range []string{"", "{}", `{"bot_token":"x"}`} {
		got, err := Decrypt("secret", v)
		if err != nil || got != v {
			t.Fatalf("legacy %q -> %q, %v", v, got, err)
		}
	}
}

func TestEmptyStaysEmpty(t *testing.T) {
	enc, err := Encrypt("secret", "")
	if err != nil || enc != "" {
		t.Fatalf("empty -> %q, %v", enc, err)
	}
}

func TestWrongSecretFails(t *testing.T) {
	enc, err := Encrypt("secret-a", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt("secret-b", enc); err == nil {
		t.Fatal("decrypt with wrong secret must fail")
	}
}

func TestEmptyServerSecretFailsClosed(t *testing.T) {
	if _, err := Encrypt("", "hello"); err == nil {
		t.Fatal("encrypt without server secret must fail")
	}
	if _, err := Decrypt("", "enc:v1:AAAA"); err == nil {
		t.Fatal("decrypt without server secret must fail")
	}
}
