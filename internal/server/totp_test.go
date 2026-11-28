package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
	"wdev/internal/auth"
	"wdev/internal/config"
	"wdev/internal/store"
)

func TestAuth2FAFlowAndTerminalUnlock(t *testing.T) {
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "totp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := &config.Config{HTTPHost: "127.0.0.1", HTTPPort: "3200"}
	authMgr := auth.New(cfg, db)
	if err := authMgr.SetPassword("StrongPass123!"); err != nil {
		t.Fatal(err)
	}

	srv := New(cfg, db, nil, nil, nil, nil, nil, nil, nil, authMgr)
	srv.secMgr.SetFirewallEnabled(false)
	handler := srv.wrapAuth(srv.mux)

	// 1. Login without TOTP when disabled -> succeeds directly
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"admin","password":"StrongPass123!"}`))
	loginReq.RemoteAddr = "127.0.0.1:12345"
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on initial login, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == auth.CookieSessionName {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected session cookie after login")
	}

	// 2. Begin 2FA setup (wrong password rejected, right password succeeds)
	setupBad := httptest.NewRequest(http.MethodPost, "/api/auth/2fa/setup", bytes.NewBufferString(`{"password":"wrong"}`))
	setupBad.RemoteAddr = "127.0.0.1:12345"
	setupBad.AddCookie(sessionCookie)
	recBad := httptest.NewRecorder()
	handler.ServeHTTP(recBad, setupBad)
	if recBad.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong password setup, got %d", recBad.Code)
	}

	setupReq := httptest.NewRequest(http.MethodPost, "/api/auth/2fa/setup", bytes.NewBufferString(`{"password":"StrongPass123!"}`))
	setupReq.RemoteAddr = "127.0.0.1:12345"
	setupReq.AddCookie(sessionCookie)
	setupRec := httptest.NewRecorder()
	handler.ServeHTTP(setupRec, setupReq)
	if setupRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on setup, got %d: %s", setupRec.Code, setupRec.Body.String())
	}
	var enroll auth.TOTPEnrollment
	if err := json.Unmarshal(setupRec.Body.Bytes(), &enroll); err != nil {
		t.Fatal(err)
	}
	if enroll.Secret == "" || enroll.OTPAuthURL == "" {
		t.Fatalf("expected secret and otpauth_url, got %+v", enroll)
	}

	// 3. Verify 2FA enrollment with current TOTP code
	codeNow, err := auth.ComputeTOTPCode(enroll.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	verifyReq := httptest.NewRequest(http.MethodPost, "/api/auth/2fa/verify", bytes.NewBufferString(`{"code":"`+codeNow+`"}`))
	verifyReq.RemoteAddr = "127.0.0.1:12345"
	verifyReq.AddCookie(sessionCookie)
	verifyRec := httptest.NewRecorder()
	handler.ServeHTTP(verifyRec, verifyReq)
	if verifyRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on verify, got %d: %s", verifyRec.Code, verifyRec.Body.String())
	}
	var verifyBody struct {
		Status        string   `json:"status"`
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(verifyRec.Body.Bytes(), &verifyBody); err != nil {
		t.Fatal(err)
	}
	if verifyBody.Status != "enabled" || len(verifyBody.RecoveryCodes) != 8 {
		t.Fatalf("unexpected verify response: %+v", verifyBody)
	}

	// 4. Enable terminal_required via /api/auth/2fa/settings
	settingsReq := httptest.NewRequest(http.MethodPost, "/api/auth/2fa/settings", bytes.NewBufferString(`{"terminal_required":true}`))
	settingsReq.RemoteAddr = "127.0.0.1:12345"
	settingsReq.AddCookie(sessionCookie)
	settingsRec := httptest.NewRecorder()
	handler.ServeHTTP(settingsRec, settingsReq)
	if settingsRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on settings, got %d: %s", settingsRec.Code, settingsRec.Body.String())
	}

	// 5. Login step 1 without totp_code returns totp_required=true and no session cookie
	step1Req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"admin","password":"StrongPass123!"}`))
	step1Req.RemoteAddr = "127.0.0.1:12345"
	step1Rec := httptest.NewRecorder()
	handler.ServeHTTP(step1Rec, step1Req)
	if step1Rec.Code != http.StatusOK {
		t.Fatalf("expected 200 totp_required, got %d: %s", step1Rec.Code, step1Rec.Body.String())
	}
	var step1Body struct {
		Status       string `json:"status"`
		TOTPRequired bool   `json:"totp_required"`
	}
	_ = json.Unmarshal(step1Rec.Body.Bytes(), &step1Body)
	if !step1Body.TOTPRequired || step1Body.Status != "totp_required" {
		t.Fatalf("expected totp_required=true, got %+v", step1Body)
	}
	if len(step1Rec.Result().Cookies()) != 0 {
		t.Fatalf("step 1 must not set session cookie")
	}

	// 6. Login step 2 with recovery code succeeds
	step2Req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"username":"admin","password":"StrongPass123!","totp_code":"`+verifyBody.RecoveryCodes[0]+`"}`))
	step2Req.RemoteAddr = "127.0.0.1:12345"
	step2Rec := httptest.NewRecorder()
	handler.ServeHTTP(step2Rec, step2Req)
	if step2Rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on step 2 with recovery code, got %d: %s", step2Rec.Code, step2Rec.Body.String())
	}
	var step2Body struct {
		Status            string `json:"status"`
		UsedRecoveryCode  bool   `json:"used_recovery_code"`
		RecoveryRemaining int    `json:"recovery_remaining"`
	}
	_ = json.Unmarshal(step2Rec.Body.Bytes(), &step2Body)
	if step2Body.Status != "ok" || !step2Body.UsedRecoveryCode || step2Body.RecoveryRemaining != 7 {
		t.Fatalf("unexpected step2 body: %+v", step2Body)
	}

	// 7. Terminal unlock without 2FA code fails when totp_terminal_required=true, succeeds with recovery code
	unlockBad := httptest.NewRequest(http.MethodPost, "/api/terminal/unlock", bytes.NewBufferString(`{"password":"StrongPass123!"}`))
	unlockBad.RemoteAddr = "127.0.0.1:12345"
	unlockBad.AddCookie(sessionCookie)
	unlockBadRec := httptest.NewRecorder()
	handler.ServeHTTP(unlockBadRec, unlockBad)
	if unlockBadRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when 2FA code missing on terminal unlock, got %d", unlockBadRec.Code)
	}

	unlockOK := httptest.NewRequest(http.MethodPost, "/api/terminal/unlock", bytes.NewBufferString(`{"password":"StrongPass123!","totp_code":"`+verifyBody.RecoveryCodes[1]+`"}`))
	unlockOK.RemoteAddr = "127.0.0.1:12345"
	unlockOK.AddCookie(sessionCookie)
	unlockOKRec := httptest.NewRecorder()
	handler.ServeHTTP(unlockOKRec, unlockOK)
	if unlockOKRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on terminal unlock with valid recovery code, got %d: %s", unlockOKRec.Code, unlockOKRec.Body.String())
	}

	// 8. Disable 2FA
	disableReq := httptest.NewRequest(http.MethodPost, "/api/auth/2fa/disable", bytes.NewBufferString(`{"password":"StrongPass123!","code":"`+verifyBody.RecoveryCodes[2]+`"}`))
	disableReq.RemoteAddr = "127.0.0.1:12345"
	disableReq.AddCookie(sessionCookie)
	disableRec := httptest.NewRecorder()
	handler.ServeHTTP(disableRec, disableReq)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on disable, got %d: %s", disableRec.Code, disableRec.Body.String())
	}
	if authMgr.IsTOTPEnabled() {
		t.Fatalf("expected TOTP disabled after /api/auth/2fa/disable")
	}
}
