package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"wdev/internal/auth"
	"wdev/internal/i18n"
	"wdev/internal/security"
)

func (s *Server) registerTOTPRoutes() {
	s.mux.HandleFunc("/api/auth/2fa/status", s.handleAuth2FAStatus)
	s.mux.HandleFunc("/api/auth/2fa/setup", s.handleAuth2FASetup)
	s.mux.HandleFunc("/api/auth/2fa/verify", s.handleAuth2FAVerify)
	s.mux.HandleFunc("/api/auth/2fa/disable", s.handleAuth2FADisable)
	s.mux.HandleFunc("/api/auth/2fa/recovery/regenerate", s.handleAuth2FARegenerateRecovery)
	s.mux.HandleFunc("/api/auth/2fa/settings", s.handleAuth2FASettings)
}

func (s *Server) check2FARateLimit(w http.ResponseWriter, r *http.Request, target string) (string, bool) {
	lang := i18n.FromRequest(r)
	ip := auth.ClientIP(r)
	if s.authMgr == nil {
		return ip, true
	}
	allowed, wait := s.authMgr.CheckRateLimit(ip)
	if !allowed {
		if s.secMgr != nil {
			_, _ = s.secMgr.RecordAttack(r.Context(), security.Event{
				SourceIP:    ip,
				AttackType:  security.AttackPanelBruteForce,
				Severity:    security.SeverityHigh,
				Target:      target,
				ActionTaken: security.ActionRateLimited,
				Details:     fmt.Sprintf("Rate limited on %s: locked for %dm", target, int(wait.Minutes())+1),
			})
		}
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf(i18n.Pick(lang,
			"Too many failed verification attempts. Try again in %d minute(s).",
			"Terlalu banyak percobaan verifikasi gagal. Coba lagi dalam %d menit."), int(wait.Minutes())+1))
		return ip, false
	}
	return ip, true
}

func (s *Server) handleAuth2FAStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	if s.authMgr == nil {
		writeJSON(w, http.StatusOK, auth.TOTPStatus{})
		return
	}
	writeJSON(w, http.StatusOK, s.authMgr.GetTOTPStatus())
}

func (s *Server) handleAuth2FASetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	lang := i18n.FromRequest(r)
	var payload struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	ip, ok := s.check2FARateLimit(w, r, "/api/auth/2fa/setup")
	if !ok {
		return
	}
	if !s.authMgr.VerifyAdminPassword(payload.Password) {
		s.authMgr.RecordLoginAttempt(ip, false)
		writeError(w, http.StatusUnauthorized, i18n.Pick(lang, "Current password does not match", "Kata sandi saat ini tidak cocok"))
		return
	}
	s.authMgr.RecordLoginAttempt(ip, true)

	enroll, err := s.authMgr.BeginTOTPEnrollment(r.Host)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, enroll)
}

func (s *Server) handleAuth2FAVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	lang := i18n.FromRequest(r)
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	ip, ok := s.check2FARateLimit(w, r, "/api/auth/2fa/verify")
	if !ok {
		return
	}
	codes, err := s.authMgr.ConfirmTOTPEnrollment(payload.Code)
	if err != nil {
		s.authMgr.RecordLoginAttempt(ip, false)
		writeError(w, http.StatusBadRequest, i18n.Pick(lang,
			"Invalid 6-digit authenticator code. Check your device clock and try again.",
			"Kode authenticator 6-digit tidak valid. Pastikan waktu perangkat sinkron lalu coba lagi."))
		return
	}
	s.authMgr.RecordLoginAttempt(ip, true)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "enabled",
		"message":        i18n.Pick(lang, "Two-factor authentication enabled successfully.", "Autentikasi dua faktor (2FA) berhasil diaktifkan."),
		"recovery_codes": codes,
		"totp":           s.authMgr.GetTOTPStatus(),
	})
}

func (s *Server) handleAuth2FADisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	lang := i18n.FromRequest(r)
	var payload struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	ip, ok := s.check2FARateLimit(w, r, "/api/auth/2fa/disable")
	if !ok {
		return
	}
	if !s.authMgr.VerifyAdminPassword(payload.Password) {
		s.authMgr.RecordLoginAttempt(ip, false)
		writeError(w, http.StatusUnauthorized, i18n.Pick(lang, "Current password does not match", "Kata sandi saat ini tidak cocok"))
		return
	}
	if s.authMgr.IsTOTPEnabled() {
		if _, _, valid := s.authMgr.VerifyTOTP(payload.Code); !valid {
			s.authMgr.RecordLoginAttempt(ip, false)
			writeError(w, http.StatusUnauthorized, i18n.Pick(lang, "Invalid 2FA or recovery code", "Kode 2FA atau recovery code tidak valid"))
			return
		}
	}
	s.authMgr.RecordLoginAttempt(ip, true)
	if err := s.authMgr.DisableTOTP(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "disabled",
		"message": i18n.Pick(lang, "Two-factor authentication has been disabled.", "Autentikasi dua faktor (2FA) telah dinonaktifkan."),
		"totp":    s.authMgr.GetTOTPStatus(),
	})
}

func (s *Server) handleAuth2FARegenerateRecovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	lang := i18n.FromRequest(r)
	var payload struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	ip, ok := s.check2FARateLimit(w, r, "/api/auth/2fa/recovery/regenerate")
	if !ok {
		return
	}
	if !s.authMgr.VerifyAdminPassword(payload.Password) {
		s.authMgr.RecordLoginAttempt(ip, false)
		writeError(w, http.StatusUnauthorized, i18n.Pick(lang, "Current password does not match", "Kata sandi saat ini tidak cocok"))
		return
	}
	if !s.authMgr.IsTOTPEnabled() {
		writeError(w, http.StatusBadRequest, i18n.Pick(lang, "2FA is not enabled", "2FA belum diaktifkan"))
		return
	}
	if strings.TrimSpace(payload.Code) == "" {
		writeError(w, http.StatusBadRequest, i18n.Pick(lang, "Current 2FA code is required", "Kode 2FA saat ini wajib diisi"))
		return
	}
	if _, _, valid := s.authMgr.VerifyTOTP(payload.Code); !valid {
		s.authMgr.RecordLoginAttempt(ip, false)
		writeError(w, http.StatusUnauthorized, i18n.Pick(lang, "Invalid 2FA verification code", "Kode verifikasi 2FA tidak valid"))
		return
	}
	s.authMgr.RecordLoginAttempt(ip, true)

	codes, err := s.authMgr.RegenerateRecoveryCodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"message":        i18n.Pick(lang, "New recovery codes generated. Previous codes are no longer valid.", "Kode pemulihan baru dibuat. Kode lama sudah tidak berlaku."),
		"recovery_codes": codes,
		"totp":           s.authMgr.GetTOTPStatus(),
	})
}

func (s *Server) handleAuth2FASettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	lang := i18n.FromRequest(r)
	var payload struct {
		TerminalRequired bool `json:"terminal_required"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	if !s.authMgr.IsTOTPEnabled() && payload.TerminalRequired {
		writeError(w, http.StatusBadRequest, i18n.Pick(lang, "Enable 2FA first before requiring it for root terminal unlock", "Aktifkan 2FA terlebih dahulu sebelum mewajibkannya untuk terminal root"))
		return
	}
	if err := s.authMgr.SetTOTPTerminalRequired(payload.TerminalRequired); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"message": i18n.Pick(lang, "2FA policy updated.", "Kebijakan 2FA berhasil diperbarui."),
		"totp":    s.authMgr.GetTOTPStatus(),
	})
}
