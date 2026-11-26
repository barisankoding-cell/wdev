package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"wdev/internal/auth"
	"wdev/internal/i18n"
	"wdev/internal/security"
	"wdev/internal/store"
)

func (s *Server) registerSecurityRoutes() {
	s.mux.HandleFunc("/api/security/stats", s.handleSecurityStats)
	s.mux.HandleFunc("/api/security/events", s.handleSecurityEvents)
	s.mux.HandleFunc("/api/security/blocked", s.handleSecurityBlocked)
	s.mux.HandleFunc("/api/security/block", s.handleSecurityBlockIP)
	s.mux.HandleFunc("/api/security/unblock", s.handleSecurityUnblockIP)
	s.mux.HandleFunc("/api/security/clear", s.handleSecurityClearEvents)
}

// SetSecurityManager memungkinkan penyetelan Security Manager (berguna untuk testing).
func (s *Server) SetSecurityManager(mgr *security.Manager) {
	s.secMgr = mgr
}

// GetSecurityManager mengembalikan Security Manager aktif jika ada.
func (s *Server) GetSecurityManager() *security.Manager {
	return s.secMgr
}

// wrapBlockedIP menolak koneksi dari IP yang sedang diblokir aktif sebelum memproses request apapun.
func (s *Server) wrapBlockedIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.secMgr != nil {
			ip := auth.ClientIP(r)
			if blocked, rec, _ := s.secMgr.IsIPBlocked(ip); blocked {
				reason := "Suspicious activity detected"
				if rec != nil && rec.Reason != "" {
					reason = rec.Reason
				}
				http.Error(w, fmt.Sprintf("Forbidden: Your IP address (%s) is blocked. Reason: %s", ip, reason), http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// isHoneypotTarget mengecek apakah path URL merupakan decoy/jebakan bot scanner.
func isHoneypotTarget(p string) bool {
	clean := strings.ToLower(path.Clean("/" + p))
	decoyPatterns := []string{
		"/.env",
		"/.git",
		"/wp-login.php",
		"/wp-admin",
		"/phpmyadmin",
		"/pma",
		"/actuator",
		"/boaform",
		"/xmlrpc.php",
		"/config.json",
		"/.aws",
		"/cgi-bin",
	}
	for _, pattern := range decoyPatterns {
		if strings.HasPrefix(clean, pattern) {
			return true
		}
	}
	return false
}

// handleSecurityStats mengembalikan ringkasan statistik keamanan.
func (s *Server) handleSecurityStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	if s.secMgr == nil {
		writeJSON(w, http.StatusOK, store.SecurityStatsRecord{})
		return
	}

	stats, err := s.secMgr.GetStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// handleSecurityEvents mengembalikan riwayat serangan dengan filter dan pagination.
func (s *Server) handleSecurityEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	if s.secMgr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"events": []any{}, "total": 0})
		return
	}

	limit := 50
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		if l, err := strconv.Atoi(rawLimit); err == nil && l > 0 {
			limit = l
		}
	}

	offset := 0
	if rawOffset := strings.TrimSpace(r.URL.Query().Get("offset")); rawOffset != "" {
		if o, err := strconv.Atoi(rawOffset); err == nil && o >= 0 {
			offset = o
		}
	}

	attackType := r.URL.Query().Get("type")
	severity := r.URL.Query().Get("severity")
	search := r.URL.Query().Get("search")

	events, total, err := s.secMgr.ListEvents(limit, offset, attackType, severity, search)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// handleSecurityBlocked mengembalikan daftar IP yang saat ini diblokir.
func (s *Server) handleSecurityBlocked(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	if s.secMgr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"blocked_ips": []any{}})
		return
	}

	list, err := s.secMgr.ListBlockedIPs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"blocked_ips": list,
	})
}

// handleSecurityBlockIP memblokir IP secara manual.
func (s *Server) handleSecurityBlockIP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	lang := i18n.FromRequest(r)

	var payload struct {
		IP              string `json:"ip"`
		Reason          string `json:"reason"`
		DurationMinutes int    `json:"duration_minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	ip := strings.TrimSpace(payload.IP)
	if ip == "" {
		writeErrorLocale(w, r, http.StatusBadRequest, "IP address is required", "Alamat IP wajib diisi")
		return
	}

	if security.IsPrivateOrLoopback(ip) {
		writeErrorLocale(w, r, http.StatusBadRequest, "Cannot block local or loopback IP", "Tidak dapat memblokir IP lokal atau loopback")
		return
	}

	duration := time.Duration(payload.DurationMinutes) * time.Minute
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "Manual block via control panel"
	}

	if s.secMgr != nil {
		if err := s.secMgr.BlockIP(r.Context(), ip, reason, duration); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": i18n.Pick(lang, "IP blocked successfully", "Alamat IP berhasil diblokir"),
	})
}

// handleSecurityUnblockIP mencabut pemblokiran IP.
func (s *Server) handleSecurityUnblockIP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	lang := i18n.FromRequest(r)

	var payload struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	ip := strings.TrimSpace(payload.IP)
	if ip == "" {
		writeErrorLocale(w, r, http.StatusBadRequest, "IP address is required", "Alamat IP wajib diisi")
		return
	}

	if s.secMgr != nil {
		if err := s.secMgr.UnblockIP(r.Context(), ip); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": i18n.Pick(lang, "IP unblocked successfully", "Pemblokiran IP berhasil dicabut"),
	})
}

// handleSecurityClearEvents membersihkan seluruh log event serangan.
func (s *Server) handleSecurityClearEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeMethodNotAllowed(w, r)
		return
	}
	lang := i18n.FromRequest(r)

	if s.secMgr != nil {
		if err := s.secMgr.ClearEvents(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": i18n.Pick(lang, "Security logs cleared successfully", "Riwayat log keamanan berhasil dibersihkan"),
	})
}
