package auth

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/philippdormann/splat/internal/database"
	"github.com/pquerna/otp/totp"
)

func totpTestStore(t *testing.T, secret string) *Store {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "totp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db, secret)
}

func mustUser(t *testing.T, s *Store, username string) int64 {
	t.Helper()
	u, err := s.CreateUser(username, "password123", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func TestTOTPSetupConfirmValidate(t *testing.T) {
	s := totpTestStore(t, "test-secret-1234567890")
	id := mustUser(t, s, "alice")

	if s.TOTPEnabled(id) {
		t.Fatal("2FA must start disabled")
	}
	key, err := s.BeginTOTPSetup(id)
	if err != nil {
		t.Fatal(err)
	}
	if key.Secret() == "" || !strings.HasPrefix(key.URL(), "otpauth://") {
		t.Fatalf("bad provisioning key: %q", key.URL())
	}
	if s.TOTPEnabled(id) {
		t.Fatal("pending setup must not enable 2FA")
	}
	// Secrets are encrypted at rest.
	var enc string
	if err := s.db.QueryRow(`SELECT secret_enc FROM user_totp WHERE user_id = ?`, id).Scan(&enc); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(enc, key.Secret()) {
		t.Fatal("TOTP secret stored in cleartext")
	}

	if _, err := s.ConfirmTOTPSetup(id, "000000"); err == nil {
		t.Fatal("wrong code must not confirm setup")
	}
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	codes, err := s.ConfirmTOTPSetup(id, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("recovery codes = %d, want %d", len(codes), recoveryCodeCount)
	}
	if !s.TOTPEnabled(id) {
		t.Fatal("2FA must be enabled after confirm")
	}
	if n := s.RecoveryCodesRemaining(id); n != recoveryCodeCount {
		t.Fatalf("remaining codes = %d", n)
	}

	code, err = totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !s.ValidateTOTP(id, code) {
		t.Fatal("valid code rejected")
	}
	if s.ValidateTOTP(id, "000000") {
		t.Fatal("invalid code accepted")
	}
}

func TestRecoveryCodeSingleUse(t *testing.T) {
	s := totpTestStore(t, "test-secret-1234567890")
	id := mustUser(t, s, "bob")

	key, err := s.BeginTOTPSetup(id)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(key.Secret(), time.Now())
	codes, err := s.ConfirmTOTPSetup(id, code)
	if err != nil {
		t.Fatal(err)
	}
	// Plaintext codes are never stored.
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM user_recovery_codes WHERE code_hash = ?`, codes[0]).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("recovery code stored in cleartext")
	}

	used, ok := s.VerifySecondFactor(id, codes[0])
	if !ok || !used {
		t.Fatal("first recovery code use must succeed")
	}
	if n := s.RecoveryCodesRemaining(id); n != recoveryCodeCount-1 {
		t.Fatalf("remaining = %d", n)
	}
	if _, ok := s.VerifySecondFactor(id, codes[0]); ok {
		t.Fatal("burned recovery code reused")
	}
	if _, ok := s.VerifySecondFactor(id, "not-a-code"); ok {
		t.Fatal("bogus code accepted")
	}
}

func TestDisableAndRegenerate(t *testing.T) {
	s := totpTestStore(t, "test-secret-1234567890")
	id := mustUser(t, s, "carol")

	if _, err := s.RegenerateRecoveryCodes(id); err == nil {
		t.Fatal("regenerate without 2FA must fail")
	}
	key, err := s.BeginTOTPSetup(id)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(key.Secret(), time.Now())
	old, err := s.ConfirmTOTPSetup(id, code)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := s.RegenerateRecoveryCodes(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.VerifySecondFactor(id, old[0]); ok {
		t.Fatal("old recovery code valid after regenerate")
	}
	if _, ok := s.VerifySecondFactor(id, fresh[0]); !ok {
		t.Fatal("new recovery code rejected")
	}
	if err := s.DisableTOTP(id); err != nil {
		t.Fatal(err)
	}
	if s.TOTPEnabled(id) || s.RecoveryCodesRemaining(id) != 0 {
		t.Fatal("disable must clear secret and codes")
	}
}

func TestLoginChallenge(t *testing.T) {
	s := totpTestStore(t, "test-secret-1234567890")
	id := mustUser(t, s, "dave")

	ch, err := s.IssueLoginChallenge(id)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s.VerifyLoginChallenge(ch)
	if !ok || got != id {
		t.Fatal("valid challenge rejected")
	}
	if _, ok := s.VerifyLoginChallenge(ch + "x"); ok {
		t.Fatal("tampered challenge accepted")
	}
	if _, ok := s.VerifyLoginChallenge("garbage"); ok {
		t.Fatal("garbage challenge accepted")
	}
	// A challenge from another server secret must not verify here.
	other := totpTestStore(t, "different-secret")
	peer, _ := other.IssueLoginChallenge(id)
	if _, ok := s.VerifyLoginChallenge(peer); ok {
		t.Fatal("foreign challenge accepted")
	}
}

func TestTOTPRequiresServerSecret(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "nosecret.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := NewStore(db, "")
	u, err := s.CreateUser("erin", "password123", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginTOTPSetup(u.ID); err == nil {
		t.Fatal("setup without server secret must fail closed")
	}
	if _, err := s.IssueLoginChallenge(u.ID); err == nil {
		t.Fatal("challenge without server secret must fail closed")
	}
}

func TestTOTPRequiredFlag(t *testing.T) {
	s := totpTestStore(t, "test-secret-1234567890")
	if s.TOTPRequired() {
		t.Fatal("enforcement must default off")
	}
	if err := s.SetTOTPRequired(true); err != nil {
		t.Fatal(err)
	}
	if !s.TOTPRequired() {
		t.Fatal("enforcement not persisted")
	}
	if err := s.SetTOTPRequired(false); err != nil {
		t.Fatal(err)
	}
	if s.TOTPRequired() {
		t.Fatal("enforcement off not persisted")
	}
}

func TestListUsersIncludesTOTP(t *testing.T) {
	s := totpTestStore(t, "test-secret-1234567890")
	id := mustUser(t, s, "frank")
	key, _ := s.BeginTOTPSetup(id)
	code, _ := totp.GenerateCode(key.Secret(), time.Now())
	if _, err := s.ConfirmTOTPSetup(id, code); err != nil {
		t.Fatal(err)
	}
	users, err := s.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || !users[0].TOTPEnabled {
		t.Fatalf("users = %+v", users)
	}
}
