package auth

import (
	"strings"
	"testing"
	"time"
	"wdev/internal/config"
)

func TestComputeAndVerifyTOTPCodeRFC6238(t *testing.T) {
	// RFC 6238 test vector secret "12345678901234567890" -> Base32: GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	code, err := ComputeTOTPCode(secret, time.Unix(59, 0))
	if err != nil {
		t.Fatalf("ComputeTOTPCode error: %v", err)
	}
	if code != "287082" {
		t.Fatalf("expected RFC 6238 SHA1 code 287082 at t=59, got %s", code)
	}

	// Clock skew ±1 step should verify
	now := time.Now()
	prevCode, err := ComputeTOTPCode(secret, now.Add(-30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := VerifyTOTPCode(secret, prevCode, now); !ok {
		t.Fatalf("expected code from -1 step to verify within tolerance window")
	}

	// Far past code should be rejected
	oldCode, _ := ComputeTOTPCode(secret, now.Add(-120*time.Second))
	if _, ok := VerifyTOTPCode(secret, oldCode, now); ok {
		t.Fatalf("expected code from -4 steps to be rejected")
	}
}

func TestTOTPEnrollmentVerifyReplayAndRecovery(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	mgr := New(&config.Config{HTTPPort: "3200"}, st)
	if mgr.IsTOTPEnabled() {
		t.Fatalf("expected TOTP disabled by default")
	}

	enroll, err := mgr.BeginTOTPEnrollment("panel.example.com:3200")
	if err != nil {
		t.Fatalf("BeginTOTPEnrollment error: %v", err)
	}
	if enroll.Secret == "" || !strings.HasPrefix(enroll.OTPAuthURL, "otpauth://totp/") {
		t.Fatalf("unexpected enrollment payload: %+v", enroll)
	}
	if !mgr.GetTOTPStatus().Pending {
		t.Fatalf("expected pending=true after BeginTOTPEnrollment")
	}

	// Wrong code must not activate TOTP
	if _, err := mgr.ConfirmTOTPEnrollment("000000"); err == nil {
		// Edge case: 1 in 1,000,000 chance 000000 is valid, check against actual code
		cur, _ := ComputeTOTPCode(enroll.Secret, time.Now())
		if cur != "000000" {
			t.Fatalf("expected ConfirmTOTPEnrollment to fail with invalid code")
		}
	}

	// Valid code confirms enrollment and yields 8 recovery codes
	now := time.Now()
	validCode, err := ComputeTOTPCode(enroll.Secret, now)
	if err != nil {
		t.Fatal(err)
	}
	recCodes, err := mgr.ConfirmTOTPEnrollment(validCode)
	if err != nil {
		t.Fatalf("ConfirmTOTPEnrollment failed: %v", err)
	}
	if len(recCodes) != totpRecoveryCount {
		t.Fatalf("expected %d recovery codes, got %d", totpRecoveryCount, len(recCodes))
	}
	if !mgr.IsTOTPEnabled() {
		t.Fatalf("expected IsTOTPEnabled() == true")
	}

	// Same code within the same 30s step must be rejected due to anti-replay
	if _, _, ok := mgr.VerifyTOTP(validCode); ok {
		t.Fatalf("expected replay of same TOTP code to be rejected")
	}

	// Recovery code should work once (even lowercase/without hyphen) and then be consumed
	firstRec := strings.ToLower(strings.ReplaceAll(recCodes[0], "-", ""))
	usedRec, rem, ok := mgr.VerifyTOTP(firstRec)
	if !ok || !usedRec || rem != totpRecoveryCount-1 {
		t.Fatalf("expected first recovery code to succeed: ok=%v usedRec=%v rem=%d", ok, usedRec, rem)
	}
	if _, _, ok2 := mgr.VerifyTOTP(recCodes[0]); ok2 {
		t.Fatalf("expected consumed recovery code to fail on reuse")
	}

	// Terminal requirement toggle
	if err := mgr.SetTOTPTerminalRequired(true); err != nil {
		t.Fatal(err)
	}
	if !mgr.IsTOTPTerminalRequired() {
		t.Fatalf("expected IsTOTPTerminalRequired() == true")
	}

	// Disable via ResetCredentials
	if _, err := mgr.ResetCredentials(); err != nil {
		t.Fatal(err)
	}
	if mgr.IsTOTPEnabled() || mgr.IsTOTPTerminalRequired() {
		t.Fatalf("expected ResetCredentials to disable TOTP")
	}
}
