package auth

import (
	"fmt"
	"net/http/httptest"
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

func TestSessionHashedAndRevokedOnCredentialChange(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	cfg := &config.Config{HTTPPort: "3200"}
	mgr := New(cfg, st)

	token, err := mgr.CreateSession("admin")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	// Token mentah tidak boleh ada di store
	if rawUser, _, _ := st.GetSession(token); rawUser != "" {
		t.Fatalf("raw session token should not be stored directly in SQLite")
	}
	// Hash-nya harus valid lewat ValidateSession
	if u, ok := mgr.ValidateSession(token); !ok || u != "admin" {
		t.Fatalf("ValidateSession should succeed before password change")
	}

	// Ganti password harus mencabut semua sesi lama
	if err := mgr.SetPassword("StrongNewPass123!"); err != nil {
		t.Fatalf("SetPassword failed: %v", err)
	}
	if _, ok := mgr.ValidateSession(token); ok {
		t.Fatalf("session should be revoked after SetPassword")
	}

	// Password < 8 karakter harus ditolak
	if err := mgr.SetPassword("short"); err == nil {
		t.Fatalf("SetPassword should reject passwords shorter than 8 characters")
	}

	// ResetCredentials harus mencabut sesi & merotasi entrance secret
	token2, _ := mgr.CreateSession("admin")
	oldCookie, err := mgr.GenerateEntranceCookie()
	if err != nil {
		t.Fatalf("GenerateEntranceCookie failed: %v", err)
	}
	if _, err := mgr.ResetCredentials(); err != nil {
		t.Fatalf("ResetCredentials failed: %v", err)
	}
	if _, ok := mgr.ValidateSession(token2); ok {
		t.Fatalf("session should be revoked after ResetCredentials")
	}
	if mgr.ValidateEntranceCookie(oldCookie.Value) {
		t.Fatalf("old entrance cookie should be invalidated after ResetCredentials")
	}
}

func TestClientIPIgnoresUntrustedProxyHeaders(t *testing.T) {
	// Koneksi langsung dari IP publik tidak boleh bisa memalsukan X-Forwarded-For
	req := httptest.NewRequest("POST", "/api/auth/login", nil)
	req.RemoteAddr = "203.0.113.50:54321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "5.6.7.8")
	if got := ClientIP(req); got != "203.0.113.50" {
		t.Fatalf("ClientIP() = %q, want peer IP 203.0.113.50", got)
	}

	// Koneksi dari reverse proxy lokal (127.0.0.1) boleh memakai X-Forwarded-For
	reqProxy := httptest.NewRequest("POST", "/api/auth/login", nil)
	reqProxy.RemoteAddr = "127.0.0.1:40000"
	reqProxy.Header.Set("X-Forwarded-For", "198.51.100.22, 127.0.0.1")
	if got := ClientIP(reqProxy); got != "198.51.100.22" {
		t.Fatalf("ClientIP() behind loopback proxy = %q, want 198.51.100.22", got)
	}
}

func TestClearInitialPassword(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	cfg := &config.Config{HTTPPort: "3200"}
	mgr := New(cfg, st)

	infoBefore := mgr.GetPanelAccessInfo("", "")
	if infoBefore.PasswordIsCustom || infoBefore.Password == "" {
		t.Fatalf("expected initial password to be present before clear, got %+v", infoBefore)
	}
	initPass := infoBefore.Password

	if err := mgr.ClearInitialPassword(); err != nil {
		t.Fatalf("ClearInitialPassword failed: %v", err)
	}
	infoAfter := mgr.GetPanelAccessInfo("", "")
	if !infoAfter.PasswordIsCustom {
		t.Fatalf("expected PasswordIsCustom=true after ClearInitialPassword, got %+v", infoAfter)
	}
	if gotRaw, _ := st.GetSetting(SettingInitialPassword); gotRaw != "" {
		t.Fatalf("expected SettingInitialPassword cleared in DB, got %q", gotRaw)
	}
	if !mgr.ValidateCredentials("admin", initPass) {
		t.Fatalf("expected password hash to remain valid after ClearInitialPassword")
	}
}

func TestRateLimiterPreservesLockoutDuringIPFlood(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	cfg := &config.Config{HTTPPort: "3200"}
	mgr := New(cfg, st)

	attackerIP := "198.51.100.77"
	for i := 0; i < MaxFailedAttempts; i++ {
		mgr.RecordLoginAttempt(attackerIP, false)
	}
	if allowed, _ := mgr.CheckRateLimit(attackerIP); allowed {
		t.Fatalf("expected %s to be locked out before IP flood", attackerIP)
	}

	// Simulate an attacker rotating through > maxTrackedIPs distinct source IPs to trigger pruning.
	for i := 0; i < maxTrackedIPs+200; i++ {
		floodIP := fmt.Sprintf("203.0.%d.%d", (i/250)+1, (i%250)+1)
		mgr.RecordLoginAttempt(floodIP, false)
	}

	allowed, remaining := mgr.CheckRateLimit(attackerIP)
	if allowed || remaining <= 0 {
		t.Fatalf("expected active lockout for %s to survive IP flood pruning (allowed=%v, remaining=%v)", attackerIP, allowed, remaining)
	}
}

func TestIsTrustedProxyRequest(t *testing.T) {
	for _, tc := range []struct {
		remoteAddr string
		want       bool
	}{
		{"127.0.0.1:45678", true},
		{"[::1]:45678", true},
		{"103.171.85.72:54321", false},
		{"192.0.2.1:1234", false},
		{"", false},
	} {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = tc.remoteAddr
		if got := IsTrustedProxyRequest(req); got != tc.want {
			t.Fatalf("IsTrustedProxyRequest(%q) = %v, want %v", tc.remoteAddr, got, tc.want)
		}
	}
	if IsTrustedProxyRequest(nil) {
		t.Fatal("IsTrustedProxyRequest(nil) must return false")
	}
}
