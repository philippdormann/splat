// Two-factor authentication: TOTP secrets (RFC 6238, SHA1/30s/6-digit),
// single-use recovery codes, and short-lived signed login challenges.
//
// TOTP secrets are encrypted at rest with AES-256-GCM keyed by
// SHA256(SPLAT_SESSION_SECRET) - a stolen database file alone does not
// defeat the second factor. Recovery codes are random 52-bit values
// stored as SHA256 hashes and burned on use.
package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"strconv"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	// challengeTTL bounds how long a password proof is valid for the
	// second step of a two-step login.
	challengeTTL = 5 * time.Minute
	// recoveryCodeCount is how many single-use codes a user holds.
	recoveryCodeCount = 10
)

// encKey derives the at-rest encryption key. Empty server secrets fail
// closed: 2FA setup is refused rather than stored in cleartext.
func (s *Store) encKey() ([]byte, error) {
	if s.sessionSecret == "" {
		return nil, errors.New("two-factor authentication is unavailable: server secret is not configured")
	}
	sum := sha256.Sum256([]byte(s.sessionSecret))
	return sum[:], nil
}

func (s *Store) encryptSecret(plain string) (string, error) {
	key, err := s.encKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
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
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

func (s *Store) decryptSecret(enc string) (string, error) {
	key, err := s.encKey()
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("corrupt TOTP secret")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("cannot decrypt TOTP secret (server secret changed?)")
	}
	return string(plain), nil
}

// TOTPEnabled reports whether the user completed 2FA setup.
func (s *Store) TOTPEnabled(userID int64) bool {
	var enabled bool
	if s.db.IsPostgres() {
		if err := s.db.QueryRow(`SELECT enabled FROM user_totp WHERE user_id = ?`, userID).Scan(&enabled); err != nil {
			return false
		}
		return enabled
	}
	var n int
	if err := s.db.QueryRow(`SELECT enabled FROM user_totp WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return false
	}
	return n != 0
}

// BeginTOTPSetup generates a fresh secret and stores it as pending
// (disabled) until ConfirmTOTPSetup verifies a code against it. Any
// previous pending or enabled secret is replaced only on confirm.
func (s *Store) BeginTOTPSetup(userID int64) (*otp.Key, error) {
	username := s.UsernameOf(userID)
	if username == "" {
		return nil, errors.New("unknown user")
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Splat",
		AccountName: username,
		Period:      30,
		SecretSize:  20,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return nil, err
	}
	enc, err := s.encryptSecret(key.Secret())
	if err != nil {
		return nil, err
	}
	if s.db.IsPostgres() {
		_, err = s.db.Exec(`INSERT INTO user_totp (user_id, secret_enc, enabled, verified_at)
			VALUES (?, ?, FALSE, NULL)
			ON CONFLICT (user_id) DO UPDATE SET secret_enc = EXCLUDED.secret_enc, enabled = FALSE, verified_at = NULL`,
			userID, enc)
	} else {
		_, err = s.db.Exec(`INSERT INTO user_totp (user_id, secret_enc, enabled, verified_at)
			VALUES (?, ?, 0, NULL)
			ON CONFLICT(user_id) DO UPDATE SET secret_enc = excluded.secret_enc, enabled = 0, verified_at = NULL`,
			userID, enc)
	}
	if err != nil {
		return nil, err
	}
	return key, nil
}

// ConfirmTOTPSetup verifies a code against the pending secret, enables
// 2FA, and mints a fresh set of recovery codes (returned once - the
// caller must show them to the user immediately).
func (s *Store) ConfirmTOTPSetup(userID int64, code string) ([]string, error) {
	secret, enabled, err := s.totpSecret(userID)
	if err != nil {
		return nil, errors.New("no pending two-factor setup - start setup first")
	}
	if enabled {
		return nil, errors.New("two-factor authentication is already enabled")
	}
	if !validTOTP(secret, code) {
		return nil, errors.New("invalid authenticator code")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if s.db.IsPostgres() {
		_, err = s.db.Exec(`UPDATE user_totp SET enabled = TRUE, verified_at = ? WHERE user_id = ?`, now, userID)
	} else {
		_, err = s.db.Exec(`UPDATE user_totp SET enabled = 1, verified_at = ? WHERE user_id = ?`, now, userID)
	}
	if err != nil {
		return nil, err
	}
	return s.mintRecoveryCodes(userID)
}

// ValidateTOTP checks a code against the user's enabled secret,
// accepting the standard ±1 step clock skew.
func (s *Store) ValidateTOTP(userID int64, code string) bool {
	secret, enabled, err := s.totpSecret(userID)
	if err != nil || !enabled {
		return false
	}
	return validTOTP(secret, code)
}

// VerifySecondFactor accepts either a TOTP code or an unused recovery
// code. It reports whether a recovery code was burned.
func (s *Store) VerifySecondFactor(userID int64, code string) (recoveryUsed, ok bool) {
	if s.ValidateTOTP(userID, code) {
		return false, true
	}
	if s.useRecoveryCode(userID, code) {
		return true, true
	}
	return false, false
}

func validTOTP(secret, code string) bool {
	code = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(code), " ", ""), "-", "")
	valid, _ := totp.ValidateCustom(code, secret, time.Now(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	return valid
}

func (s *Store) totpSecret(userID int64) (secret string, enabled bool, err error) {
	var enc string
	if s.db.IsPostgres() {
		var en bool
		if err := s.db.QueryRow(`SELECT secret_enc, enabled FROM user_totp WHERE user_id = ?`, userID).Scan(&enc, &en); err != nil {
			return "", false, err
		}
		enabled = en
	} else {
		var n int
		if err := s.db.QueryRow(`SELECT secret_enc, enabled FROM user_totp WHERE user_id = ?`, userID).Scan(&enc, &n); err != nil {
			return "", false, err
		}
		enabled = n != 0
	}
	secret, err = s.decryptSecret(enc)
	if err != nil {
		return "", false, err
	}
	return secret, enabled, nil
}

// --- recovery codes ---

const recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

func newRecoveryCode() (string, error) {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = recoveryAlphabet[int(buf[i])%len(recoveryAlphabet)]
	}
	return string(buf[:5]) + "-" + string(buf[5:]), nil
}

func recoveryHash(code string) string {
	norm := strings.ToLower(strings.TrimSpace(code))
	sum := sha256.Sum256([]byte(norm))
	return hex.EncodeToString(sum[:])
}

// mintRecoveryCodes replaces all of the user's codes and returns the
// new plaintext values (shown once, never stored).
func (s *Store) mintRecoveryCodes(userID int64) ([]string, error) {
	codes := make([]string, 0, recoveryCodeCount)
	hashes := make([]string, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		c, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}
		codes = append(codes, c)
		hashes = append(hashes, recoveryHash(c))
	}
	if _, err := s.db.Exec(`DELETE FROM user_recovery_codes WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	for _, h := range hashes {
		if _, err := s.db.Exec(`INSERT INTO user_recovery_codes (user_id, code_hash) VALUES (?, ?)`, userID, h); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// RegenerateRecoveryCodes mints a fresh set, invalidating the old one.
func (s *Store) RegenerateRecoveryCodes(userID int64) ([]string, error) {
	if !s.TOTPEnabled(userID) {
		return nil, errors.New("two-factor authentication is not enabled")
	}
	return s.mintRecoveryCodes(userID)
}

// RecoveryCodesRemaining counts unused codes (0 when 2FA is off).
func (s *Store) RecoveryCodesRemaining(userID int64) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID).Scan(&n)
	return n
}

func (s *Store) useRecoveryCode(userID int64, code string) bool {
	want := recoveryHash(code)
	rows, err := s.db.Query(`SELECT code_hash FROM user_recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID)
	if err != nil {
		return false
	}
	defer rows.Close()
	match := ""
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(h), []byte(want)) == 1 {
			match = h
		}
	}
	if match == "" {
		return false
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(`UPDATE user_recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`, now, userID, match)
	return err == nil
}

// DisableTOTP removes the secret and burns all recovery codes (user
// self-service with password proof, or admin reset - enforced by the
// HTTP layer, not here).
func (s *Store) DisableTOTP(userID int64) error {
	if _, err := s.db.Exec(`DELETE FROM user_recovery_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM user_totp WHERE user_id = ?`, userID)
	return err
}

// --- instance enforcement ---

// TOTPRequired reports whether every login must complete 2FA.
func (s *Store) TOTPRequired() bool {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'totp_required'`).Scan(&v); err != nil {
		return false
	}
	return v == "1" || strings.EqualFold(v, "true")
}

// SetTOTPRequired enables or disables instance-wide 2FA enforcement.
func (s *Store) SetTOTPRequired(required bool) error {
	v := "0"
	if required {
		v = "1"
	}
	if s.db.IsPostgres() {
		_, err := s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('totp_required', ?)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, v)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('totp_required', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, v)
	return err
}

// --- login challenges ---

// IssueLoginChallenge mints a short-lived HMAC-signed proof that the
// user passed the password step, so the 2FA step needs no session yet.
func (s *Store) IssueLoginChallenge(userID int64) (string, error) {
	key, err := s.encKey()
	if err != nil {
		return "", err
	}
	rb := make([]byte, 16)
	if _, err := rand.Read(rb); err != nil {
		return "", err
	}
	exp := time.Now().Add(challengeTTL).Unix()
	body := fmt.Sprintf("v1.%d.%d.%s", userID, exp, hex.EncodeToString(rb))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("login-challenge|" + body))
	return body + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

// VerifyLoginChallenge checks the proof and returns the user id.
func (s *Store) VerifyLoginChallenge(token string) (int64, bool) {
	key, err := s.encKey()
	if err != nil {
		return 0, false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 5 || parts[0] != "v1" {
		return 0, false
	}
	body := strings.Join(parts[:4], ".")
	mac, err := hex.DecodeString(parts[4])
	if err != nil {
		return 0, false
	}
	want := hmac.New(sha256.New, key)
	want.Write([]byte("login-challenge|" + body))
	if subtle.ConstantTimeCompare(mac, want.Sum(nil)) != 1 {
		return 0, false
	}
	userID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || userID <= 0 {
		return 0, false
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return 0, false
	}
	return userID, true
}

// QRDataURI renders the provisioning URI as a PNG data URI for <img>.
func QRDataURI(provisioningURI string) (string, error) {
	key, err := otp.NewKeyFromURL(provisioningURI)
	if err != nil {
		return "", err
	}
	img, err := key.Image(256, 256)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
