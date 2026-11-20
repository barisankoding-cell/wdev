package auth

import (
	"os"
	"path/filepath"
	"testing"
	"wdev/internal/config"
	"wdev/internal/store"
)

func setupTestStore(t *testing.T) (*store.Store, func()) {
	tmpDir, err := os.MkdirTemp("", "wdev_auth_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.New(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open store: %v", err)
	}
	cleanup := func() {
		_ = st.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return st, cleanup
}

func TestPasswordHashAndVerify(t *testing.T) {
	pass := "mySecret123!"
	hash, err := HashPassword(pass)
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	if !VerifyPassword(pass, hash) {
		t.Errorf("VerifyPassword should succeed for correct password")
	}
	if VerifyPassword("wrongPass", hash) {
		t.Errorf("VerifyPassword should fail for wrong password")
	}
}

func TestAuthManagerSessions(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	cfg := &config.Config{HTTPPort: "3200"}
	mgr := New(cfg, st)

	user := mgr.GetUsername()
	if user != "admin" {
		t.Errorf("expected username admin, got %s", user)
	}

	token, err := mgr.CreateSession(user)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if token == "" {
		t.Fatalf("CreateSession returned empty token")
	}

	validUser, ok := mgr.ValidateSession(token)
	if !ok || validUser != user {
		t.Errorf("ValidateSession failed: ok=%v, user=%s", ok, validUser)
	}

	// Delete session
	if err := mgr.DeleteSession(token); err != nil {
		t.Fatalf("DeleteSession error: %v", err)
	}
	_, ok = mgr.ValidateSession(token)
	if ok {
		t.Errorf("ValidateSession should fail after deletion")
	}
}

func TestSecurityEntranceValidation(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	cfg := &config.Config{HTTPPort: "3200"}
	mgr := New(cfg, st)

	cookie, err := mgr.GenerateEntranceCookie()
	if err != nil {
		t.Fatalf("GenerateEntranceCookie failed: %v", err)
	}
	if !mgr.ValidateEntranceCookie(cookie.Value) {
		t.Errorf("ValidateEntranceCookie should succeed for freshly generated cookie")
	}
	if mgr.ValidateEntranceCookie("invalid.cookie.value") {
		t.Errorf("ValidateEntranceCookie should fail for invalid cookie")
	}
}

func TestRateLimiter(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	cfg := &config.Config{HTTPPort: "3200"}
	mgr := New(cfg, st)

	ip := "192.168.1.100"
	for i := 0; i < 4; i++ {
		mgr.RecordLoginAttempt(ip, false)
		allowed, _ := mgr.CheckRateLimit(ip)
		if !allowed {
			t.Fatalf("should still be allowed at attempt %d", i+1)
		}
	}

	// 5th failed attempt should lock out
	mgr.RecordLoginAttempt(ip, false)
	allowed, remaining := mgr.CheckRateLimit(ip)
	if allowed {
		t.Fatalf("should be locked after 5 failed attempts")
	}
	if remaining <= 0 || remaining > LockoutDuration {
		t.Errorf("unexpected remaining lockout duration: %v", remaining)
	}

	// Success on another IP shouldn't be affected
	otherIP := "10.0.0.1"
	mgr.RecordLoginAttempt(otherIP, true)
	allowedOther, _ := mgr.CheckRateLimit(otherIP)
	if !allowedOther {
		t.Errorf("other IP should not be locked")
	}
}
