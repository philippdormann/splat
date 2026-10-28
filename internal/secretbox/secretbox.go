// Package secretbox encrypts small secrets at rest with AES-256-GCM,
// keyed by SHA256(serverSecret) where serverSecret is SPLAT_SESSION_SECRET.
//
// Ciphertext is "enc:v1:" + base64(nonce|ciphertext). Values without the
// prefix are treated as legacy plaintext so existing databases keep
// working and are re-encrypted on next write (or via explicit migration).
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const prefix = "enc:v1:"

// IsEncrypted reports whether v looks like secretbox ciphertext.
func IsEncrypted(v string) bool { return strings.HasPrefix(v, prefix) }

func key(serverSecret string) ([]byte, error) {
	if serverSecret == "" {
		return nil, errors.New("secretbox: server secret is not configured")
	}
	sum := sha256.Sum256([]byte(serverSecret))
	return sum[:], nil
}

// Encrypt seals plaintext. Empty plaintext stays empty (no row churn for
// unset optionals like a missing forge token).
func Encrypt(serverSecret, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	k, err := key(serverSecret)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt opens ciphertext. Values without the prefix are returned as-is
// (legacy plaintext rows written before encryption existed).
func Decrypt(serverSecret, v string) (string, error) {
	if v == "" || !IsEncrypted(v) {
		return v, nil
	}
	k, err := key(serverSecret)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(v, prefix))
	if err != nil {
		return "", errors.New("secretbox: invalid ciphertext encoding")
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("secretbox: corrupt ciphertext")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("secretbox: cannot decrypt (server secret changed?)")
	}
	return string(plain), nil
}
