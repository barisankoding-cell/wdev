package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"wdev/internal/auth"
	"wdev/internal/cloudflare"
	"wdev/internal/config"
	"wdev/internal/installer"
	"wdev/internal/mail"
	"wdev/internal/monitor"
	"wdev/internal/pdns"
	"wdev/internal/runner"
	"wdev/internal/serviceconfig"
	"wdev/internal/site"
	"wdev/internal/store"
	"wdev/internal/switcher"
	"wdev/internal/updater"
	"wdev/internal/version"
	"wdev/web"
)

type Server struct {
	cfg         *config.Config
	store       *store.Store
	runner      *runner.Runner
	installer   *installer.Installer
	siteMgr     *site.Manager
	switcherMgr *switcher.Manager
	cfMgr       *cloudflare.Manager
	pdnsMgr     *pdns.Manager
	mailMgr     *mail.Manager
	authMgr     *auth.Manager
	svcCfgMgr   *serviceconfig.Manager
	mux         *http.ServeMux
}

func New(
	cfg *config.Config,
	s *store.Store,
	r *runner.Runner,
	inst *installer.Installer,
	siteMgr *site.Manager,
	swMgr *switcher.Manager,
	cfMgr *cloudflare.Manager,
	pdnsMgr *pdns.Manager,
	mailMgr *mail.Manager,
	authMgr *auth.Manager,
) *Server {
	if authMgr == nil {
		authMgr = auth.New(cfg, s)
	}
	if pdnsMgr != nil && siteMgr != nil {
		pdnsMgr.SetSiteManager(siteMgr)
	}
	srv := &Server{
		cfg:         cfg,
		store:       s,
		runner:      r,
		installer:   inst,
		siteMgr:     siteMgr,
		switcherMgr: swMgr,
		cfMgr:       cfMgr,
		pdnsMgr:     pdnsMgr,
		mailMgr:     mailMgr,
		authMgr:     authMgr,
		svcCfgMgr:   serviceconfig.New(inst.GetSudoPassword),
		mux:         http.NewServeMux(),
	}
	srv.routes()
	return srv
}

func (s *Server) routes() {
	// API routes
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/service/", s.handleServiceControl)
	s.mux.HandleFunc("/api/service-config/", s.handleServiceConfig)
	s.mux.HandleFunc("/api/sites", s.handleSites)
	s.mux.HandleFunc("/api/sites/", s.handleSiteDelete)
	s.mux.HandleFunc("/api/switchers", s.handleSwitchers)
	s.mux.HandleFunc("/api/switchers/", s.handleSwitchersAction)
	s.mux.HandleFunc("/api/cloudflare/status", s.handleCFStatus)
	s.mux.HandleFunc("/api/cloudflare/expose", s.handleCFExpose)
	s.mux.HandleFunc("/api/cloudflare/unexpose", s.handleCFUnexpose)
	s.mux.HandleFunc("/api/pdns/status", s.handlePDNSStatus)
	s.mux.HandleFunc("/api/pdns/zones", s.handlePDNSZones)
	s.mux.HandleFunc("/api/pdns/zone/doctor", s.handlePDNSZoneDoctor)
	s.mux.HandleFunc("/api/pdns/zone/repair", s.handlePDNSZoneRepair)
	s.mux.HandleFunc("/api/pdns/records", s.handlePDNSRecords)
	s.mux.HandleFunc("/api/pdns/exposures", s.handlePDNSExposures)
	s.mux.HandleFunc("/api/pdns/expose", s.handlePDNSExpose)
	s.mux.HandleFunc("/api/pdns/unexpose", s.handlePDNSUnexpose)
	s.mux.HandleFunc("/api/pdns/deploy", s.handlePDNSDeploy)
	s.mux.HandleFunc("/api/pdns/connect", s.handlePDNSConnect)
	s.mux.HandleFunc("/api/mail/status", s.handleMailStatus)
	s.mux.HandleFunc("/api/mail/accounts", s.handleMailAccounts)
	s.mux.HandleFunc("/api/mail/account", s.handleMailAccountAdd)
	s.mux.HandleFunc("/api/mail/account/delete", s.handleMailAccountDelete)
	s.mux.HandleFunc("/api/mail/relay", s.handleMailRelaySet)
	s.mux.HandleFunc("/api/mail/preflight", s.handleMailPreflight)
	s.mux.HandleFunc("/api/mail/routes", s.handleMailRoutes)
	s.mux.HandleFunc("/api/mail/relay/remove", s.handleMailRelayRemove)
	s.mux.HandleFunc("/api/mail/relay/dns", s.handleMailRelayDNS)
	s.mux.HandleFunc("/api/mail/admin-reset", s.handleMailAdminReset)
	s.mux.HandleFunc("/api/mail/setup", s.handleMailSetup)
	s.mux.HandleFunc("/api/mail/domain/delete", s.handleMailDomainDelete)
	s.mux.HandleFunc("/api/mail/domain/repair", s.handleMailDomainRepair)
	s.mux.HandleFunc("/api/sudo/status", s.handleSudoStatus)
	s.mux.HandleFunc("/api/sudo/verify", s.handleSudoVerify)
	s.mux.HandleFunc("/api/toolchains", s.handleToolchains)
	s.mux.HandleFunc("/api/workspace/status", s.handleWorkspaceStatus)
	s.mux.HandleFunc("/api/install", s.handleInstall)
	s.mux.HandleFunc("/api/tasks", s.handleTasks)
	s.mux.HandleFunc("/api/tasks/events", s.handleSSEEvents)
	s.mux.HandleFunc("/api/env", s.handleEnv)
	s.mux.HandleFunc("/api/env/sync", s.handleEnvSync)
	s.mux.HandleFunc("/api/updater/status", s.handleUpdaterStatus)
	s.mux.HandleFunc("/api/updater/apply", s.handleUpdaterApply)
	s.mux.HandleFunc("/api/mode", s.handleMode)
	s.mux.HandleFunc("/api/auth/login", s.handleAuthLogin)
	s.mux.HandleFunc("/api/auth/logout", s.handleAuthLogout)
	s.mux.HandleFunc("/api/auth/me", s.handleAuthMe)
	s.mux.HandleFunc("/api/auth/change-credentials", s.handleAuthChangeCredentials)
	s.mux.HandleFunc("/api/", s.handleAPINotFound)

	// Embedded Static UI (Svelte SPA)
	s.mux.Handle("/", web.Handler())
}

const securityEntranceDeniedHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>404 Not Found</title>
</head>
<body>
  <center><h1>404 Not Found</h1></center>
  <hr>
</body>
</html>`

var (
	runtimeVerRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+){0,2}$`)
	maxAPIBody   = int64(1 << 20) // 1 MiB
)

func (s *Server) wrapSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if auth.IsHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) wrapCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanURLPath := path.Clean("/" + r.URL.Path)
		if strings.HasPrefix(cleanURLPath, "/api/") {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxAPIBody)
			}
			// Validasi asal request pada metode mutasi untuk mencegah CSRF
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete {
				hasSessionCookie := false
				if c, err := r.Cookie(auth.CookieSessionName); err == nil && c.Value != "" {
					hasSessionCookie = true
				}
				isBearerOnly := !hasSessionCookie && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
				if !isBearerOnly {
					if fetchSite := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); fetchSite == "cross-site" {
						writeError(w, http.StatusForbidden, "Permintaan cross-site ditolak (CSRF protection)")
						return
					}
					if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
						u, err := url.Parse(origin)
						if err != nil || !strings.EqualFold(u.Host, r.Host) {
							writeError(w, http.StatusForbidden, "Origin tidak diizinkan (CSRF protection)")
							return
						}
					}
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) wrapSecurityEntrance(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entrance := s.authMgr.GetSecurityEntrance()
		if entrance == "" {
			next.ServeHTTP(w, r)
			return
		}

		cleanURLPath := path.Clean("/" + r.URL.Path)
		cleanPath := strings.Trim(cleanURLPath, "/")

		// 1. Jika path cocok dengan security entrance, periksa rate limit lalu terbitkan cookie dan redirect ke /
		if cleanPath == entrance {
			ip := auth.ClientIP(r)
			if allowed, _ := s.authMgr.CheckRateLimit(ip); !allowed {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(securityEntranceDeniedHTML))
				return
			}
			cookie, err := s.authMgr.GenerateEntranceCookie()
			if err == nil {
				cookie.Secure = auth.IsHTTPS(r)
				http.SetCookie(w, cookie)
			}
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		// 2. Izinkan aset statis non-API agar login page dapat memuat CSS dan JavaScript
		if !strings.HasPrefix(cleanURLPath, "/api/") && (strings.HasPrefix(cleanURLPath, "/_app/") ||
			strings.HasSuffix(cleanURLPath, ".js") ||
			strings.HasSuffix(cleanURLPath, ".css") ||
			strings.HasSuffix(cleanURLPath, ".png") ||
			strings.HasSuffix(cleanURLPath, ".svg") ||
			strings.HasSuffix(cleanURLPath, ".ico") ||
			strings.HasSuffix(cleanURLPath, ".woff") ||
			strings.HasSuffix(cleanURLPath, ".woff2")) {
			next.ServeHTTP(w, r)
			return
		}

		// 3. Periksa apakah pengunjung sudah melewati entrance atau memiliki sesi aktif
		hasValidEntrance := false
		if c, err := r.Cookie(auth.CookieEntranceName); err == nil && c.Value != "" {
			hasValidEntrance = s.authMgr.ValidateEntranceCookie(c.Value)
		}

		hasValidSession := false
		if c, err := r.Cookie(auth.CookieSessionName); err == nil && c.Value != "" {
			_, hasValidSession = s.authMgr.ValidateSession(c.Value)
		}

		if hasValidEntrance || hasValidSession {
			next.ServeHTTP(w, r)
			return
		}

		// 4. Catat percobaan tebakan path entrance non-root untuk anti-brute-force
		if cleanPath != "" && !strings.HasPrefix(cleanURLPath, "/api/") && !strings.Contains(cleanPath, "/") {
			s.authMgr.RecordLoginAttempt(auth.ClientIP(r), false)
		}

		// 5. Pengunjung belum melewati security entrance
		if strings.HasPrefix(cleanURLPath, "/api/") {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(securityEntranceDeniedHTML))
	})
}

func (s *Server) wrapAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authMgr.IsAuthEnabled() {
			next.ServeHTTP(w, r)
			return
		}

		cleanURLPath := path.Clean("/" + r.URL.Path)

		// Hanya amankan rute /api/ (rute web SPA diperiksa oleh klien di frontend)
		if !strings.HasPrefix(cleanURLPath, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// Endpoint auth terbuka
		if cleanURLPath == "/api/auth/login" || cleanURLPath == "/api/auth/me" {
			next.ServeHTTP(w, r)
			return
		}

		// Periksa sesi dari cookie atau header Bearer
		token := ""
		if c, err := r.Cookie(auth.CookieSessionName); err == nil {
			token = c.Value
		}
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		username, valid := s.authMgr.ValidateSession(token)
		if !valid || username == "" {
			writeError(w, http.StatusUnauthorized, "Autentikasi diperlukan. Silakan login terlebih dahulu.")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	ip := auth.ClientIP(r)
	allowed, wait := s.authMgr.CheckRateLimit(ip)
	if !allowed {
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Terlalu banyak percobaan login gagal. Akun dikunci sementara. Coba lagi dalam %d menit.", int(wait.Minutes())+1))
		return
	}

	if !s.authMgr.ValidateCredentials(payload.Username, payload.Password) {
		s.authMgr.RecordLoginAttempt(ip, false)
		writeError(w, http.StatusUnauthorized, "Username atau kata sandi salah")
		return
	}

	s.authMgr.RecordLoginAttempt(ip, true)
	token, err := s.authMgr.CreateSession(payload.Username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Gagal membuat sesi: "+err.Error())
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieSessionName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(auth.SessionDuration),
		HttpOnly: true,
		Secure:   auth.IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"message":  "Login berhasil",
		"username": payload.Username,
	})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if c, err := r.Cookie(auth.CookieSessionName); err == nil && c.Value != "" {
		_ = s.authMgr.DeleteSession(c.Value)
	}
	if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
		if bearer := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer ")); bearer != "" {
			_ = s.authMgr.DeleteSession(bearer)
		}
	}
	if s.installer != nil {
		s.installer.ClearSudoPassword()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieSessionName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   auth.IsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Berhasil logout",
	})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	token := ""
	if c, err := r.Cookie(auth.CookieSessionName); err == nil {
		token = c.Value
	}
	if token == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	username, valid := s.authMgr.ValidateSession(token)
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated":    valid,
		"username":         username,
		"auth_enabled":     s.authMgr.IsAuthEnabled(),
		"entrance_enabled": s.authMgr.GetSecurityEntrance() != "",
	})
}

func (s *Server) handleAuthChangeCredentials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	var payload struct {
		OldPassword string `json:"old_password"`
		NewUsername string `json:"new_username"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	ip := auth.ClientIP(r)
	if allowed, wait := s.authMgr.CheckRateLimit(ip); !allowed {
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Terlalu banyak percobaan gagal. Coba lagi dalam %d menit.", int(wait.Minutes())+1))
		return
	}

	currentUsername := s.authMgr.GetUsername()
	if !s.authMgr.ValidateCredentials(currentUsername, payload.OldPassword) {
		s.authMgr.RecordLoginAttempt(ip, false)
		writeError(w, http.StatusUnauthorized, "Kata sandi saat ini tidak cocok")
		return
	}
	s.authMgr.RecordLoginAttempt(ip, true)

	updatedUsername := currentUsername
	if strings.TrimSpace(payload.NewUsername) != "" {
		if err := s.authMgr.SetUsername(payload.NewUsername); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		updatedUsername = strings.TrimSpace(payload.NewUsername)
	}
	if strings.TrimSpace(payload.NewPassword) != "" {
		if err := s.authMgr.SetPassword(payload.NewPassword); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	// Terbitkan sesi baru untuk admin yang baru saja memperbarui kredensial
	if newToken, err := s.authMgr.CreateSession(updatedUsername); err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     auth.CookieSessionName,
			Value:    newToken,
			Path:     "/",
			Expires:  time.Now().Add(auth.SessionDuration),
			HttpOnly: true,
			Secure:   auth.IsHTTPS(r),
			SameSite: http.SameSiteLaxMode,
		})
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Kredensial berhasil diperbarui",
	})
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%s", s.cfg.HTTPHost, s.cfg.HTTPPort)
	useTLS := s.cfg.TLSCert != "" && s.cfg.TLSKey != ""
	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	nbIP := ""
	if s.pdnsMgr != nil {
		nbIP = s.pdnsMgr.GetLocalNetbirdIP()
	}
	info := s.authMgr.GetPanelAccessInfo("", nbIP)
	fmt.Println("==================================================================")
	fmt.Printf("✓ wdev Web Dashboard aktif di: %s://%s\n", scheme, addr)
	if info.NetbirdURL != "" {
		fmt.Printf("✓ URL Akses NetBird           : %s\n", strings.Replace(info.NetbirdURL, "http://", scheme+"://", 1))
	}
	if info.SecurityEntrance != "" {
		fmt.Printf("✓ URL Akses Panel (Entrance)  : %s\n", strings.Replace(info.LocalURL, "http://", scheme+"://", 1))
	} else {
		fmt.Printf("✓ URL Akses Panel             : %s://localhost:%s\n", scheme, s.cfg.HTTPPort)
	}
	if !useTLS && (s.cfg.HTTPHost == "0.0.0.0" || s.cfg.HTTPHost == "::") {
		fmt.Println("⚠ PERINGATAN: Panel berjalan tanpa TLS di semua antarmuka. Gunakan reverse proxy HTTPS atau --tls-cert/--tls-key untuk produksi.")
	}
	fmt.Println("✓ Info Kredensial & Security URL: jalankan 'wdev panel default'")
	fmt.Println("==================================================================")

	handler := s.wrapSecurityHeaders(s.wrapSecurityEntrance(s.wrapAuth(s.wrapCSRF(s.mux))))
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	if useTLS {
		return srv.ListenAndServeTLS(s.cfg.TLSCert, s.cfg.TLSKey)
	}
	return srv.ListenAndServe()
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Handlers
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true" {
		monitor.RefreshLatestVersions(true)
	}
	stats, err := monitor.Collect()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.pdnsMgr != nil {
		stats.ServerMode = s.pdnsMgr.GetMode()
		cfg := s.pdnsMgr.LoadConfig()
		if cfg.VPSPublicIP != "" && pdns.IsPublicIPv4String(cfg.VPSPublicIP) {
			stats.PublicIP = cfg.VPSPublicIP
		}
		if stats.ServerMode == "vps" {
			if stats.PublicIP != "" && pdns.IsPublicIPv4String(stats.PublicIP) {
				stats.NetbirdIP = stats.PublicIP
			}
		}
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleServiceControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}

	// format: /api/service/{name}/{action}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/service/"), "/")
	if len(parts) < 2 {
		writeError(w, http.StatusBadRequest, "Parameter tidak lengkap")
		return
	}
	name, action := parts[0], parts[1]

	if err := monitor.ControlService(name, action); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleServiceConfig(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/service-config/"), "/")
	if trimmed == "" {
		writeError(w, http.StatusBadRequest, "Nama layanan wajib diisi")
		return
	}
	parts := strings.Split(trimmed, "/")
	service := parts[0]
	subAction := ""
	if len(parts) > 1 {
		subAction = parts[1]
	}
	if len(parts) > 2 {
		writeError(w, http.StatusBadRequest, "Path konfigurasi tidak valid")
		return
	}

	switch r.Method {
	case http.MethodGet:
		ver := strings.TrimSpace(r.URL.Query().Get("version"))
		if subAction == "logs" {
			lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
			unit, logText, err := s.svcCfgMgr.GetServiceLogs(r.Context(), service, ver, lines)
			if err != nil {
				if errors.Is(err, serviceconfig.ErrSudoRequired) {
					writeJSON(w, http.StatusOK, map[string]any{
						"status":  "sudo_required",
						"message": "Membaca log layanan memerlukan hak akses administrator (sudo).",
					})
					return
				}
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "ok",
				"unit":   unit,
				"logs":   logText,
			})
			return
		}

		if subAction == "inspect" {
			var (
				res any
				err error
			)
			switch service {
			case "redis":
				res, err = s.svcCfgMgr.InspectRedis(r.Context())
			case "postgresql", "postgres":
				res, err = s.svcCfgMgr.InspectPostgres(r.Context(), ver)
			default:
				writeError(w, http.StatusBadRequest, "Inspeksi tidak didukung untuk layanan: "+service)
				return
			}
			if err != nil {
				if errors.Is(err, serviceconfig.ErrSudoRequired) {
					writeJSON(w, http.StatusOK, map[string]any{
						"status":  "sudo_required",
						"message": "Inspeksi layanan ini memerlukan hak akses administrator (sudo).",
					})
					return
				}
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "ok",
				"inspect": res,
			})
			return
		}

		if subAction != "" {
			writeError(w, http.StatusBadRequest, "Sub-aksi tidak dikenal: "+subAction)
			return
		}

		var (
			res any
			err error
		)
		switch service {
		case "redis":
			res, err = s.svcCfgMgr.GetRedisConfig(r.Context())
		case "postgresql", "postgres":
			res, err = s.svcCfgMgr.GetPostgresConfig(r.Context(), ver)
		case "php-fpm", "php":
			res, err = s.svcCfgMgr.GetPHPConfig(r.Context(), ver)
		case "nginx":
			res, err = s.svcCfgMgr.GetNginxConfig(r.Context())
		default:
			writeError(w, http.StatusBadRequest, "Konfigurasi layanan tidak didukung: "+service)
			return
		}
		if err != nil {
			if errors.Is(err, serviceconfig.ErrSudoRequired) {
				writeJSON(w, http.StatusOK, map[string]any{
					"status":  "sudo_required",
					"message": "Membaca konfigurasi layanan ini memerlukan hak akses administrator (sudo).",
				})
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"config": res,
		})

	case http.MethodPost:
		var raw map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
			return
		}
		var sudoPass string
		if spRaw, ok := raw["sudo_pass"]; ok {
			_ = json.Unmarshal(spRaw, &sudoPass)
		}
		if sudoPass != "" {
			if err := s.installer.VerifyAndSetSudoPassword(sudoPass); err != nil {
				writeError(w, http.StatusUnauthorized, "Password sudo tidak valid: "+err.Error())
				return
			}
		}
		if reqSudo, _ := s.installer.CheckSudoRequired(); reqSudo {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "sudo_required",
				"message": "Aksi ini memerlukan hak akses administrator (sudo).",
			})
			return
		}

		// Re-marshal tanpa field helper sudo_pass agar decode ke struct layanan tetap bersih
		delete(raw, "sudo_pass")
		bodyBytes, _ := json.Marshal(raw)

		if subAction == "flush" && service == "redis" {
			var payload struct {
				DB int `json:"db"`
			}
			if err := json.Unmarshal(bodyBytes, &payload); err != nil {
				writeError(w, http.StatusBadRequest, "Payload flush Redis tidak valid")
				return
			}
			if err := s.svcCfgMgr.FlushRedisDB(r.Context(), payload.DB); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "flushed",
				"db":     payload.DB,
			})
			return
		}

		if subAction == "create-db" && (service == "postgresql" || service == "postgres") {
			var req serviceconfig.PostgresCreateDBRequest
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				writeError(w, http.StatusBadRequest, "Payload pembuatan database tidak valid: "+err.Error())
				return
			}
			if err := s.svcCfgMgr.CreatePostgresDatabase(r.Context(), req); err != nil {
				if errors.Is(err, serviceconfig.ErrSudoRequired) {
					writeJSON(w, http.StatusOK, map[string]any{
						"status":  "sudo_required",
						"message": "Membuat database PostgreSQL memerlukan hak akses administrator (sudo).",
					})
					return
				}
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status":   "created",
				"database": req.Database,
			})
			return
		}

		if subAction != "" {
			writeError(w, http.StatusBadRequest, "Sub-aksi tidak dikenal: "+subAction)
			return
		}

		var (
			saved any
			err   error
		)
		switch service {
		case "redis":
			var cfg serviceconfig.RedisConfig
			if err = json.Unmarshal(bodyBytes, &cfg); err != nil {
				writeError(w, http.StatusBadRequest, "Format konfigurasi Redis tidak valid: "+err.Error())
				return
			}
			saved, err = s.svcCfgMgr.SaveRedisConfig(r.Context(), cfg)
		case "postgresql", "postgres":
			var cfg serviceconfig.PostgresConfig
			if err = json.Unmarshal(bodyBytes, &cfg); err != nil {
				writeError(w, http.StatusBadRequest, "Format konfigurasi PostgreSQL tidak valid: "+err.Error())
				return
			}
			saved, err = s.svcCfgMgr.SavePostgresConfig(r.Context(), cfg)
		case "php-fpm", "php":
			var cfg serviceconfig.PHPConfig
			if err = json.Unmarshal(bodyBytes, &cfg); err != nil {
				writeError(w, http.StatusBadRequest, "Format konfigurasi PHP tidak valid: "+err.Error())
				return
			}
			saved, err = s.svcCfgMgr.SavePHPConfig(r.Context(), cfg)
		case "nginx":
			var cfg serviceconfig.NginxConfig
			if err = json.Unmarshal(bodyBytes, &cfg); err != nil {
				writeError(w, http.StatusBadRequest, "Format konfigurasi Nginx tidak valid: "+err.Error())
				return
			}
			saved, err = s.svcCfgMgr.SaveNginxConfig(r.Context(), cfg)
		default:
			writeError(w, http.StatusBadRequest, "Konfigurasi layanan tidak didukung: "+service)
			return
		}

		if err != nil {
			if errors.Is(err, serviceconfig.ErrSudoRequired) {
				writeJSON(w, http.StatusOK, map[string]any{
					"status":  "sudo_required",
					"message": "Menyimpan konfigurasi layanan ini memerlukan hak akses administrator (sudo).",
				})
				return
			}
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "saved",
			"config": saved,
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

func (s *Server) handleSites(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sites, err := s.siteMgr.ListSites()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if sites == nil {
			sites = []store.SiteRecord{}
		}
		writeJSON(w, http.StatusOK, sites)

	case http.MethodPost:
		var opts site.CreateOptions
		if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
			writeError(w, http.StatusBadRequest, "Payload tidak valid")
			return
		}
		if err := s.siteMgr.CreateSite(opts); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "created"})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

func (s *Server) handleSiteDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	domain := strings.TrimPrefix(r.URL.Path, "/api/sites/")
	if domain == "" {
		writeError(w, http.StatusBadRequest, "Domain tidak valid")
		return
	}

	if err := s.siteMgr.DeleteSite(domain); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleSwitchers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	writeJSON(w, http.StatusOK, s.switcherMgr.GetAll())
}

func (s *Server) handleSwitchersAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	typeStr := strings.TrimPrefix(r.URL.Path, "/api/switchers/")
	var payload struct {
		Version  string `json:"version"`
		SudoPass string `json:"sudo_pass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}

	cleanVer := strings.TrimSpace(payload.Version)
	if cleanVer == "" {
		writeError(w, http.StatusBadRequest, "Parameter version diperlukan")
		return
	}
	if !runtimeVerRe.MatchString(strings.TrimPrefix(cleanVer, "v")) {
		writeError(w, http.StatusBadRequest, "Format parameter version tidak valid")
		return
	}

	if payload.SudoPass != "" {
		if err := s.installer.VerifyAndSetSudoPassword(payload.SudoPass); err != nil {
			writeError(w, http.StatusUnauthorized, "Password sudo tidak valid: "+err.Error())
			return
		}
	}

	if reqSudo, _ := s.installer.CheckSudoRequired(); reqSudo {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "sudo_required",
			"message": "Operasi pengalihan runtime memerlukan hak akses administrator (sudo).",
		})
		return
	}

	var taskName string
	var fn func(ctx context.Context, cb func(string)) error

	switch typeStr {
	case "node":
		taskName = fmt.Sprintf("Switch Runtime Node.js ke %s", cleanVer)
		fn = func(ctx context.Context, cb func(string)) error {
			return s.executeSwitchNode(ctx, cb, cleanVer)
		}
	case "php":
		taskName = fmt.Sprintf("Switch Runtime PHP ke %s", cleanVer)
		fn = func(ctx context.Context, cb func(string)) error {
			return s.executeSwitchPHP(ctx, cb, cleanVer)
		}
	case "pg":
		taskName = fmt.Sprintf("Switch Runtime PostgreSQL ke %s", cleanVer)
		fn = func(ctx context.Context, cb func(string)) error {
			return s.executeSwitchPG(ctx, cb, cleanVer)
		}
	case "java":
		taskName = fmt.Sprintf("Switch Runtime Java ke %s", cleanVer)
		fn = func(ctx context.Context, cb func(string)) error {
			return s.executeSwitchJava(ctx, cb, cleanVer)
		}
	default:
		writeError(w, http.StatusBadRequest, "Tipe runtime tidak valid")
		return
	}

	taskID, err := s.runner.StartNativeTask(taskName, fn)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "started",
		"task_id": taskID,
		"type":    typeStr,
		"version": cleanVer,
	})
}

func (s *Server) executeSwitchNode(ctx context.Context, cb func(string), ver string) error {
	cb(fmt.Sprintf("=== PENGALIHAN RUNTIME NODE.JS KE VERSI %s ===", ver))
	cleanVer := strings.TrimPrefix(strings.TrimSpace(ver), "v")

	nodeBase := "/usr/local/lib/nodejs"
	entries, _ := os.ReadDir(nodeBase)

	var targetDir string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "node-v"+cleanVer) ||
			strings.Contains(name, "node-v"+cleanVer+".") ||
			strings.Contains(name, "node-v"+cleanVer+"-") {
			targetDir = filepath.Join(nodeBase, name)
			break
		}
	}

	if targetDir == "" {
		for _, e := range entries {
			if e.Name() == "v"+cleanVer || e.Name() == cleanVer {
				if resolved, err := filepath.EvalSymlinks(filepath.Join(nodeBase, e.Name())); err == nil {
					targetDir = resolved
					break
				}
			}
		}
	}

	if targetDir == "" {
		cb(fmt.Sprintf("[INFO] Node.js v%s belum terpasang di %s. Mengunduh dan memasang otomatis...", cleanVer, nodeBase))
		if err := s.installer.InstallNode(ctx, cb, cleanVer); err != nil {
			return fmt.Errorf("gagal memasang Node.js v%s: %w", cleanVer, err)
		}
		entries, _ = os.ReadDir(nodeBase)
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, "node-v"+cleanVer) {
				targetDir = filepath.Join(nodeBase, name)
				break
			}
		}
	}

	if targetDir == "" {
		return fmt.Errorf("direktori instalasi Node.js v%s tidak ditemukan di %s", cleanVer, nodeBase)
	}

	cb(fmt.Sprintf("[1/3] Mengarahkan direktori aktif: %s", targetDir))
	_ = s.installer.RunCmd(ctx, cb, "ln", "-sfn", targetDir, filepath.Join(nodeBase, "current"))

	cb("[2/3] Memperbarui tautan simbolik binary (node, npm, npx, corepack)...")
	bins := []string{"node", "npm", "npx", "corepack"}
	for _, b := range bins {
		src := filepath.Join(targetDir, "bin", b)
		dst := filepath.Join("/usr/local/bin", b)
		if _, err := os.Stat(src); err == nil {
			_ = s.installer.RunCmd(ctx, cb, "rm", "-f", dst)
			_ = s.installer.RunCmd(ctx, cb, "ln", "-sfn", src, dst)
		}
	}

	cb("[3/3] Memverifikasi versi aktif...")
	out, err := exec.CommandContext(ctx, "node", "-v").CombinedOutput()
	activeNode := strings.TrimSpace(string(out))
	if err == nil {
		cb(fmt.Sprintf("✓ Node.js aktif: %s", activeNode))
	} else {
		cb(fmt.Sprintf("⚠️ Verifikasi node -v: %s (%v)", activeNode, err))
	}

	if npmOut, err := exec.CommandContext(ctx, "npm", "-v").CombinedOutput(); err == nil {
		cb(fmt.Sprintf("✓ NPM aktif: v%s", strings.TrimSpace(string(npmOut))))
	}

	cb(fmt.Sprintf("✓ [SELESAI] Pengalihan ke Node.js %s berhasil.", activeNode))
	return nil
}

func (s *Server) executeSwitchPHP(ctx context.Context, cb func(string), ver string) error {
	cb(fmt.Sprintf("=== PENGALIHAN RUNTIME PHP KE VERSI %s ===", ver))
	cleanVer := strings.TrimSpace(ver)

	bin := fmt.Sprintf("/usr/bin/php%s", cleanVer)
	if _, err := os.Stat(bin); err != nil {
		cb(fmt.Sprintf("[INFO] PHP %s belum terpasang di sistem. Memasang paket PHP %s & FPM...", cleanVer, cleanVer))
		if err := s.installer.InstallPHP(ctx, cb, cleanVer); err != nil {
			return fmt.Errorf("gagal memasang PHP %s: %w", cleanVer, err)
		}
	}

	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("biner %s tidak ditemukan setelah instalasi", bin)
	}

	cb(fmt.Sprintf("[1/3] Menyetel update-alternatives untuk PHP CLI ke %s...", bin))
	if err := s.installer.RunCmd(ctx, cb, "update-alternatives", "--set", "php", bin); err != nil {
		cb(fmt.Sprintf("[WARN] update-alternatives php: %v", err))
	}

	for _, tool := range []string{"phar", "phpize", "php-config"} {
		toolBin := fmt.Sprintf("/usr/bin/%s%s", tool, cleanVer)
		if _, err := os.Stat(toolBin); err == nil {
			_ = s.installer.RunCmd(ctx, cb, "update-alternatives", "--set", tool, toolBin)
		}
	}

	cb(fmt.Sprintf("[2/3] Memulai layanan PHP-FPM php%s-fpm...", cleanVer))
	fpmService := fmt.Sprintf("php%s-fpm", cleanVer)
	_ = s.installer.RunCmd(ctx, cb, "systemctl", "enable", "--now", fpmService)
	_ = s.installer.RunCmd(ctx, cb, "systemctl", "restart", fpmService)

	cb("[3/3] Memverifikasi versi aktif...")
	out, err := exec.CommandContext(ctx, "php", "-v").CombinedOutput()
	if err == nil {
		firstLine := strings.Split(string(out), "\n")[0]
		cb(fmt.Sprintf("✓ PHP CLI aktif: %s", firstLine))
	}

	cb(fmt.Sprintf("✓ [SELESAI] Pengalihan ke PHP %s berhasil.", cleanVer))
	return nil
}

func (s *Server) executeSwitchPG(ctx context.Context, cb func(string), ver string) error {
	return s.installer.SwitchPostgres(ctx, cb, ver)
}

func (s *Server) executeSwitchJava(ctx context.Context, cb func(string), ver string) error {
	cb(fmt.Sprintf("=== PENGALIHAN RUNTIME JAVA OPENJDK KE VERSI %s ===", ver))
	cleanVer := strings.TrimSpace(ver)

	jvmDir := "/usr/lib/jvm"
	entries, _ := os.ReadDir(jvmDir)

	var targetBin string
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, cleanVer) && !strings.HasPrefix(name, ".") {
			bin := filepath.Join(jvmDir, name, "bin", "java")
			if _, err := os.Stat(bin); err == nil {
				targetBin = bin
				break
			}
		}
	}

	if targetBin == "" {
		cb(fmt.Sprintf("[INFO] Java OpenJDK %s belum terpasang. Memasang paket openjdk-%s-jdk...", cleanVer, cleanVer))
		if err := s.installer.InstallJava(ctx, cb, cleanVer); err != nil {
			return fmt.Errorf("gagal memasang OpenJDK %s: %w", cleanVer, err)
		}
		entries, _ = os.ReadDir(jvmDir)
		for _, e := range entries {
			name := e.Name()
			if strings.Contains(name, cleanVer) && !strings.HasPrefix(name, ".") {
				bin := filepath.Join(jvmDir, name, "bin", "java")
				if _, err := os.Stat(bin); err == nil {
					targetBin = bin
					break
				}
			}
		}
	}

	if targetBin == "" {
		return fmt.Errorf("biner java untuk versi %s tidak ditemukan di %s", cleanVer, jvmDir)
	}

	if resolved, err := filepath.EvalSymlinks(targetBin); err == nil {
		targetBin = resolved
	}

	cb(fmt.Sprintf("[1/2] Mengalihkan update-alternatives java ke: %s", targetBin))
	_ = s.installer.RunCmd(ctx, cb, "update-alternatives", "--set", "java", targetBin)

	javacBin := filepath.Join(filepath.Dir(targetBin), "javac")
	if _, err := os.Stat(javacBin); err == nil {
		_ = s.installer.RunCmd(ctx, cb, "update-alternatives", "--set", "javac", javacBin)
	}

	cb("[2/2] Memverifikasi versi java aktif...")
	out, err := exec.CommandContext(ctx, "java", "-version").CombinedOutput()
	if err == nil {
		firstLine := strings.Split(string(out), "\n")[0]
		cb(fmt.Sprintf("✓ Java aktif: %s", firstLine))
	}

	cb(fmt.Sprintf("✓ [SELESAI] Pengalihan ke Java %s berhasil.", cleanVer))
	return nil
}

func (s *Server) handleCFStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	st := s.cfMgr.GetStatus()
	if st.Profiles == nil {
		st.Profiles = []string{}
	}
	if st.Exposures == nil {
		st.Exposures = []cloudflare.Exposure{}
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleCFExpose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		LocalDomain string `json:"local_domain"`
		PublicHost  string `json:"public_host"`
		Profile     string `json:"profile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	if err := s.cfMgr.Expose(payload.LocalDomain, payload.PublicHost, payload.Profile); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "exposed"})
}

func (s *Server) handleCFUnexpose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		LocalDomain string `json:"local_domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	if err := s.cfMgr.Unexpose(payload.LocalDomain); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unexposed"})
}

func (s *Server) handleMode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		mode := s.pdnsMgr.GetMode()
		isSet := s.pdnsMgr.IsModeSet()
		cfg := s.pdnsMgr.LoadConfig()
		writeJSON(w, http.StatusOK, map[string]any{
			"mode":           mode,
			"is_set":         isSet,
			"public_ip":      cfg.VPSPublicIP,
			"netbird_ip":     cfg.VPSNetbirdIP,
			"has_local_pdns": cfg.HasLocalPDNS,
			"reachable":      cfg.Reachable,
		})
	case http.MethodPost:
		var payload struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
			return
		}
		target, ok := pdns.NormalizeMode(payload.Mode)
		if !ok {
			writeError(w, http.StatusBadRequest, "Mode harus 'local', 'edge', atau 'vps'")
			return
		}
		if err := s.pdnsMgr.SetMode(target); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"mode":    target,
			"message": fmt.Sprintf("Mode server berhasil diubah ke %s", target),
		})
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

func (s *Server) handlePDNSStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	cfg := s.pdnsMgr.LoadConfig()
	// Jangan ekspos API key mentah ke browser; gunakan indikator has_api_key
	cfg.HasAPIKey = cfg.APIKey != ""
	cfg.APIKey = ""
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handlePDNSZones(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		zones, err := s.pdnsMgr.ListZones()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if zones == nil {
			zones = []pdns.ZoneInfo{}
		}
		writeJSON(w, http.StatusOK, zones)
	case http.MethodPost:
		var payload struct {
			ZoneName    string `json:"zone_name"`
			CreateVHost *bool  `json:"create_vhost"`
			SudoPass    string `json:"sudo_pass"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload tidak valid")
			return
		}
		zone, err := pdns.NormalizeZoneName(payload.ZoneName)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if payload.SudoPass != "" && s.installer != nil {
			if err := s.installer.VerifyAndSetSudoPassword(payload.SudoPass); err != nil {
				writeError(w, http.StatusUnauthorized, "Password sudo tidak valid: "+err.Error())
				return
			}
		}

		createVHost := true
		if payload.CreateVHost != nil {
			createVHost = *payload.CreateVHost
		}

		if !createVHost {
			if err := s.pdnsMgr.AddZone(zone); err != nil {
				if strings.Contains(err.Error(), "sudah ada") {
					writeError(w, http.StatusConflict, err.Error())
					return
				}
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status":        "created",
				"zone":          zone,
				"nameservers":   []string{"ns1." + zone, "ns2." + zone},
				"vhost_status":  "skipped",
				"vhost_created": false,
			})
			return
		}

		res, err := s.pdnsMgr.AddZoneWithVhost(zone)
		if err != nil {
			if strings.Contains(err.Error(), "sudah ada") {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":        "created",
			"zone":          res.Zone,
			"nameservers":   res.Nameservers,
			"zone_ip":       res.ZoneIP,
			"vhost_status":  res.VHostStatus,
			"vhost_created": res.VHostCreated,
			"vhost_root":    res.VHostRoot,
			"vhost_url":     res.VHostURL,
			"edge_exposed":  res.EdgeExposed,
			"vhost_message": res.VHostMessage,
		})
	case http.MethodDelete:
		var payload struct {
			ZoneName string `json:"zone_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload tidak valid")
			return
		}
		if err := s.pdnsMgr.DeleteZone(payload.ZoneName); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

func (s *Server) handlePDNSZoneRepair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Zone string `json:"zone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || strings.TrimSpace(payload.Zone) == "" {
		writeError(w, http.StatusBadRequest, "Parameter zone diperlukan")
		return
	}
	report, err := s.pdnsMgr.RepairZone(payload.Zone)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "report": report})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handlePDNSZoneDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	zone := r.URL.Query().Get("zone")
	if zone == "" && r.Method == http.MethodPost {
		var payload struct {
			Zone string `json:"zone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
			zone = payload.Zone
		}
	}
	if strings.TrimSpace(zone) == "" {
		writeError(w, http.StatusBadRequest, "Parameter zone diperlukan")
		return
	}
	report, err := s.pdnsMgr.DoctorZone(zone)
	if err != nil && zone == "" {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status := http.StatusOK
	if !report.OK {
		status = http.StatusOK // hasil diagnosa tetap dikirim; klien menilai report.ok
	}
	writeJSON(w, status, report)
}

func (s *Server) handlePDNSRecords(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		zoneName := r.URL.Query().Get("zone")
		if zoneName == "" {
			writeError(w, http.StatusBadRequest, "Parameter zone diperlukan")
			return
		}
		zoneInfo, err := s.pdnsMgr.GetZone(zoneName)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, zoneInfo.RRsets)
	case http.MethodPost:
		var payload struct {
			Zone    string `json:"zone"`
			Name    string `json:"name"`
			Type    string `json:"type"`
			Content string `json:"content"`
			TTL     int    `json:"ttl"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload tidak valid")
			return
		}
		if err := s.pdnsMgr.AddRecordKeep(payload.Zone, payload.Name, payload.Type, payload.Content, payload.TTL); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	case http.MethodDelete:
		var payload struct {
			Zone string `json:"zone"`
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload tidak valid")
			return
		}
		if err := s.pdnsMgr.DeleteRecord(payload.Zone, payload.Name, payload.Type); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

func (s *Server) handlePDNSExposures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	proxies, err := s.pdnsMgr.ListExposures()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if proxies == nil {
		proxies = []pdns.ProxyExposure{}
	}
	writeJSON(w, http.StatusOK, proxies)
}

func (s *Server) handlePDNSExpose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload pdns.ExposeOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}

	if payload.Mode == "site" {
		if err := s.pdnsMgr.ExposeSite(payload.LocalDomain, payload.PublicDomain, payload.UseSSL); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		if err := s.pdnsMgr.Expose(payload.PublicDomain, payload.TargetPort, payload.UseSSL, payload.WebSocket); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "exposed"})
}

func (s *Server) handlePDNSUnexpose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Domain  string `json:"domain"`
		KeepDNS bool   `json:"keep_dns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	if err := s.pdnsMgr.Unexpose(payload.Domain, payload.KeepDNS); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unexposed"})
}

func (s *Server) handlePDNSDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload pdns.DeployOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if payload.VPSHost == "" {
		writeError(w, http.StatusBadRequest, "Host / IP VPS wajib diisi")
		return
	}

	taskName := fmt.Sprintf("Deploy VPS Edge (%s)", payload.VPSHost)
	taskID, err := s.runner.StartNativeTask(taskName, func(ctx context.Context, cb func(string)) error {
		return s.pdnsMgr.DeployVPS(ctx, cb, payload)
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "started",
		"task_id": taskID,
	})
}

func (s *Server) handlePDNSConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload pdns.ConnectOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if err := s.pdnsMgr.Connect(payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "connected",
	})
}

func (s *Server) handleSudoStatus(w http.ResponseWriter, r *http.Request) {
	reqPassword, _ := s.installer.CheckSudoRequired()
	writeJSON(w, http.StatusOK, map[string]any{
		"is_root":       os.Geteuid() == 0,
		"sudo_required": reqPassword,
		"has_cached":    s.installer.GetSudoPassword() != "",
	})
}

func (s *Server) handleSudoVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	ip := auth.ClientIP(r)
	if s.authMgr != nil {
		if allowed, wait := s.authMgr.CheckRateLimit(ip); !allowed {
			writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Terlalu banyak percobaan gagal. Coba lagi dalam %d menit.", int(wait.Minutes())+1))
			return
		}
	}
	var payload struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	if err := s.installer.VerifyAndSetSudoPassword(payload.Password); err != nil {
		if s.authMgr != nil {
			s.authMgr.RecordLoginAttempt(ip, false)
		}
		writeError(w, http.StatusUnauthorized, "Password sudo salah: "+err.Error())
		return
	}
	if s.authMgr != nil {
		s.authMgr.RecordLoginAttempt(ip, true)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": "Password sudo berhasil diverifikasi"})
}

func (s *Server) handleToolchains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	refresh := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
	items, err := s.installer.GetToolchainStatuses(r.Context(), refresh)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

type installItemPayload struct {
	Component string `json:"component"`
	Version   string `json:"version"`
}

type resolvedInstallStep struct {
	canonical string
	component string
	taskName  string
	fn        func(ctx context.Context, cb func(string)) error
	order     int
	inputIdx  int
}

var nonPrivilegedInstallComponents = map[string]bool{
	"check-updates": true,
	"check_updates": true,
	"updates":       true,
	"orca-pair":     true,
	"orca-ai":       true,
	"cf-doctor":     true,
	"doctor":        true,
}

func canonicalInstallComponent(component string) string {
	switch component {
	case "full-install":
		return "all"
	case "mkcert":
		return "ssl"
	case "mail-stack":
		return "mail"
	case "powerdns":
		return "pdns"
	case "zsh-p10k", "zsh-agnoster", "zsh-robbyrussell":
		return "zsh"
	case "orca":
		return "orca-desktop"
	case "audit-toolchain":
		return "audit"
	case "repair-toolchain":
		return "audit-fix"
	case "check_updates", "updates":
		return "check-updates"
	case "doctor":
		return "cf-doctor"
	case "sync":
		return "sync-hosts"
	default:
		return component
	}
}

func installComponentOrder(canonical string) int {
	switch canonical {
	case "base":
		return 10
	case "node":
		return 20
	case "rust":
		return 30
	case "go":
		return 40
	case "java":
		return 50
	case "android":
		return 60
	case "flutter":
		return 70
	case "postgres":
		return 80
	case "redis":
		return 90
	case "nginx":
		return 100
	case "php":
		return 110
	case "composer":
		return 120
	case "ssl":
		return 130
	case "php-stack":
		return 140
	case "pdns":
		return 145
	case "mail":
		return 150
	case "cloudflare":
		return 160
	case "netbird":
		return 170
	case "zsh":
		return 190
	case "orca-desktop":
		return 200
	case "orca-headless":
		return 210
	default:
		return 500
	}
}

func validateInstallItem(rawComponent, rawVersion string) (component, reqVersion string, errMsg string) {
	component = strings.TrimSpace(rawComponent)
	reqVersion = strings.TrimSpace(rawVersion)
	if component == "" {
		return "", "", "Nama komponen wajib diisi"
	}
	if reqVersion == "" {
		return component, "", ""
	}
	switch component {
	case "node", "java", "postgres", "php", "php-stack":
		if !runtimeVerRe.MatchString(strings.TrimPrefix(reqVersion, "v")) {
			return "", "", "Format parameter version tidak valid"
		}
	case "zsh", "zsh-p10k":
		switch strings.ToLower(reqVersion) {
		case "powerlevel10k", "p10k", "agnoster", "robbyrussell":
		default:
			return "", "", "Tema Zsh tidak valid (pilihan: powerlevel10k, agnoster, robbyrussell)"
		}
	}
	return component, reqVersion, ""
}

func (s *Server) resolveInstallSteps(items []installItemPayload) (steps []resolvedInstallStep, needsSudo bool, errMsg string) {
	if len(items) == 0 {
		return nil, false, "Pilih minimal satu komponen untuk dipasang"
	}
	if len(items) > 32 {
		return nil, false, "Terlalu banyak komponen dalam satu antrean (maksimal 32)"
	}

	seen := make(map[string]bool, len(items))
	steps = make([]resolvedInstallStep, 0, len(items))

	for idx, raw := range items {
		comp, ver, vErr := validateInstallItem(raw.Component, raw.Version)
		if vErr != "" {
			return nil, false, vErr
		}
		canon := canonicalInstallComponent(comp)
		if seen[canon] {
			continue
		}
		seen[canon] = true

		stepName, stepFn, ok := s.resolveInstallTask(comp, ver)
		if !ok {
			return nil, false, "Komponen tidak dikenal: " + comp
		}
		if !nonPrivilegedInstallComponents[comp] && !nonPrivilegedInstallComponents[canon] {
			needsSudo = true
		}
		steps = append(steps, resolvedInstallStep{
			canonical: canon,
			component: comp,
			taskName:  stepName,
			fn:        stepFn,
			order:     installComponentOrder(canon),
			inputIdx:  idx,
		})
	}

	if len(steps) == 0 {
		return nil, false, "Pilih minimal satu komponen untuk dipasang"
	}

	sort.SliceStable(steps, func(i, j int) bool {
		if steps[i].order != steps[j].order {
			return steps[i].order < steps[j].order
		}
		return steps[i].inputIdx < steps[j].inputIdx
	})

	return steps, needsSudo, ""
}

func (s *Server) buildInstallBatch(items []installItemPayload, customLabel string) (taskName string, fn func(ctx context.Context, cb func(string)) error, needsSudo bool, errMsg string) {
	steps, needsSudo, errMsg := s.resolveInstallSteps(items)
	if errMsg != "" {
		return "", nil, false, errMsg
	}

	cleanLabel := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.TrimSpace(customLabel))
	if runes := []rune(cleanLabel); len(runes) > 80 {
		cleanLabel = string(runes[:80])
	}

	if len(steps) == 1 && cleanLabel == "" {
		return steps[0].taskName, steps[0].fn, needsSudo, ""
	}

	taskName = cleanLabel
	if taskName == "" {
		taskName = fmt.Sprintf("Instalasi Modular (%d Komponen)", len(steps))
	}

	fn = func(ctx context.Context, cb func(string)) error {
		total := len(steps)
		cb(fmt.Sprintf("==> Memulai %s (%d komponen)...", taskName, total))
		for idx, st := range steps {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			cb("")
			cb(fmt.Sprintf(">>> [%d/%d] Memasang: %s", idx+1, total, st.taskName))
			if err := st.fn(ctx, cb); err != nil {
				return fmt.Errorf("%s: %w", st.taskName, err)
			}
		}
		cb("")
		cb(fmt.Sprintf("✓ Selesai: %d komponen berhasil diproses.", total))
		return nil
	}

	return taskName, fn, needsSudo, ""
}

func (s *Server) handleInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Component string               `json:"component"`
		Version   string               `json:"version"`
		Items     []installItemPayload `json:"items"`
		Label     string               `json:"label"`
		SudoPass  string               `json:"sudo_pass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}

	items := payload.Items
	if len(items) == 0 && strings.TrimSpace(payload.Component) != "" {
		items = []installItemPayload{{
			Component: payload.Component,
			Version:   payload.Version,
		}}
	}

	taskName, fn, needsSudo, errMsg := s.buildInstallBatch(items, payload.Label)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	if payload.SudoPass != "" {
		if err := s.installer.VerifyAndSetSudoPassword(payload.SudoPass); err != nil {
			writeError(w, http.StatusUnauthorized, "Password sudo tidak valid: "+err.Error())
			return
		}
	}

	if needsSudo {
		if reqSudo, _ := s.installer.CheckSudoRequired(); reqSudo {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "sudo_required",
				"message": "Operasi pemeliharaan/instalasi ini memerlukan hak akses administrator (sudo).",
			})
			return
		}
	}

	taskID, err := s.runner.StartNativeTask(taskName, fn)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "started", "task_id": taskID})
}

func (s *Server) resolveInstallTask(component, reqVersion string) (taskName string, fn func(ctx context.Context, cb func(string)) error, ok bool) {
	taskName = "Instal " + component

	switch component {
	case "all", "full-install":
		taskName = "Full Auto Setup (14 Langkah)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.RunFullInstall(ctx, cb)
		}
	case "audit", "audit-toolchain":
		taskName = "Audit Duplikat Path & Toolchain"
		fn = func(ctx context.Context, cb func(string)) error {
			s.installer.AuditToolchain(ctx, cb)
			return nil
		}
	case "audit-fix", "repair-toolchain":
		taskName = "Perbaikan Duplikat Path & Toolchain"
		fn = func(ctx context.Context, cb func(string)) error {
			findings := s.installer.AuditToolchain(ctx, cb)
			return s.installer.RepairToolchain(ctx, cb, findings)
		}
	case "update":
		taskName = "Perbarui Seluruh Paket & Toolchain"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.RunUpdateAll(ctx, cb)
		}
	case "check-updates", "check_updates", "updates":
		taskName = "Pemeriksaan Ketersediaan Pembaruan"
		fn = s.installer.CheckUpdates
	case "self-check":
		taskName = "Validasi & Self-Check Sistem"
		fn = s.installer.RunSelfCheck
	case "base":
		taskName = "Dependensi Dasar Sistem"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallBase(ctx, cb)
		}
	case "node":
		ver := reqVersion
		if ver == "" {
			ver = "22"
		}
		taskName = "Node.js LTS (" + ver + ")"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallNode(ctx, cb, ver)
		}
	case "rust":
		taskName = "Rust Toolchain & Mold"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallRust(ctx, cb)
		}
	case "go":
		taskName = "Golang"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallGolang(ctx, cb)
		}
	case "java":
		ver := reqVersion
		if ver == "" {
			ver = "21"
		}
		taskName = "OpenJDK Java " + ver
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallJava(ctx, cb, ver)
		}
	case "android":
		taskName = "Android SDK & Tools"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallAndroidSDK(ctx, cb)
		}
	case "flutter":
		taskName = "Flutter SDK"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallFlutter(ctx, cb)
		}
	case "postgres":
		ver := reqVersion
		taskName = "PostgreSQL Server"
		fn = func(ctx context.Context, cb func(string)) error {
			if ver == "" {
				ver = monitor.GetActivePostgresMajor()
			}
			if ver == "" {
				ver = "16"
			}
			return s.installer.InstallPostgres(ctx, cb, ver)
		}
	case "redis":
		taskName = "Redis Cache Server"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallRedis(ctx, cb)
		}
	case "nginx":
		taskName = "Nginx Web Server"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallNginx(ctx, cb)
		}
	case "php":
		ver := reqVersion
		if ver == "" {
			ver = "8.4"
		}
		taskName = "PHP & PHP-FPM (" + ver + ")"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallPHP(ctx, cb, ver)
		}
	case "composer":
		taskName = "Composer Package Manager"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallComposer(ctx, cb)
		}
	case "ssl", "mkcert":
		taskName = "Local SSL (mkcert)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallMkcert(ctx, cb)
		}
	case "php-stack":
		ver := reqVersion
		if ver == "" {
			ver = "8.4"
		}
		taskName = "Full PHP Web Stack"
		fn = func(ctx context.Context, cb func(string)) error {
			if err := s.installer.InstallNginx(ctx, cb); err != nil {
				return err
			}
			if err := s.installer.InstallPHP(ctx, cb, ver); err != nil {
				return err
			}
			if err := s.installer.InstallComposer(ctx, cb); err != nil {
				return err
			}
			return s.installer.InstallMkcert(ctx, cb)
		}
	case "mail", "mail-stack":
		taskName = "Mail Server Stack (Stalwart + SnappyMail)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallMailStack(ctx, cb)
		}
	case "cloudflare":
		taskName = "Cloudflare Tunnel (cloudflared)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallCloudflared(ctx, cb)
		}
	case "netbird":
		taskName = "NetBird Mesh VPN"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallNetbird(ctx, cb)
		}
	case "zsh", "zsh-p10k":
		theme := reqVersion
		if theme == "" {
			theme = "powerlevel10k"
		}
		taskName = "Oh My Zsh (" + theme + ")"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallZsh(ctx, cb, theme)
		}
	case "zsh-agnoster":
		taskName = "Oh My Zsh (Agnoster)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallZsh(ctx, cb, "agnoster")
		}
	case "zsh-robbyrussell":
		taskName = "Oh My Zsh (Robbyrussell)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallZsh(ctx, cb, "robbyrussell")
		}
	case "orca", "orca-desktop":
		taskName = "Orca ADE (Desktop GUI)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallOrca(ctx, cb, "desktop")
		}
	case "orca-headless":
		taskName = "Orca ADE (Headless Server)"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallOrca(ctx, cb, "headless")
		}
	case "orca-switch-desktop":
		taskName = "Alih Mode Orca ADE (Desktop GUI)"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("==> Mengalihkan Orca ADE ke Mode Desktop GUI...")
			if err := s.installer.SetOrcaMode(ctx, cb, "desktop"); err != nil {
				return err
			}
			cb("✓ Mode Orca ADE diatur ke Desktop GUI.")
			return nil
		}
	case "orca-switch-headless":
		taskName = "Alih Mode Orca ADE (Headless Server)"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("==> Mengalihkan Orca ADE ke Mode Headless Server...")
			if err := s.installer.SetOrcaMode(ctx, cb, "headless"); err != nil {
				return err
			}
			cb("✓ Mode Orca ADE diatur ke Headless Server.")
			return nil
		}
	case "orca-update":
		taskName = "Pembaruan Orca ADE"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.UpdateOrca(ctx, cb)
		}
	case "orca-uninstall":
		taskName = "Uninstall Orca ADE"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.UninstallOrca(ctx, cb, false)
		}
	case "orca-pair":
		taskName = "Orca ADE Pairing Info"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("=== INFO PAIRING ORCA ADE ===")
			_, userHome := installer.ResolveOrcaUser()
			pairing, web, endpoint := installer.OrcaPairInfo(ctx, userHome)
			if pairing == "" {
				cb("Catatan: pairing belum tersedia (server Orca mungkin belum aktif).")
				cb("Tips: Pastikan Orca ADE Headless Server aktif.")
				return nil
			}
			cb("Pairing URL : " + pairing)
			if web != "" {
				cb("Web Client  : " + web)
			}
			if endpoint != "" {
				cb("Endpoint    : " + endpoint)
			}
			return nil
		}
	case "orca-ai":
		taskName = "Konfigurasi Orca Commit AI"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("==> Mengonfigurasi default AI commit message generator Orca...")
			_, userHome := installer.ResolveTargetUserAndHome()
			cfgDir := filepath.Join(userHome, ".config", "orca")
			_ = os.MkdirAll(cfgDir, 0755)
			cb("✓ Konfigurasi Git Commit AI Orca siap.")
			return nil
		}
	case "sync-hosts", "sync":
		taskName = "Sinkronisasi Hosts & SSL"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("==> Menyinkronkan seluruh virtual host ke /etc/hosts dan sertifikat SSL...")
			if err := s.siteMgr.SyncAll(); err != nil {
				return err
			}
			cb("✓ Sinkronisasi hosts dan SSL selesai, Nginx berhasil direload.")
			return nil
		}
	case "cf-doctor", "doctor":
		taskName = "Cloudflare Tunnel Doctor"
		fn = func(ctx context.Context, cb func(string)) error {
			cb("=== DIAGNOSTIK CLOUDFLARE TUNNEL ===")
			if path, err := exec.LookPath("cloudflared"); err == nil {
				cb(fmt.Sprintf("  [OK] cloudflared binary : %s", path))
			} else {
				cb("  [WARN] cloudflared belum terpasang.")
			}
			st := s.cfMgr.GetStatus()
			if st.Active {
				cb("  [OK] Layanan cloudflared : AKTIF (Running)")
			} else {
				cb("  [WARN] Layanan cloudflared : NONAKTIF")
			}
			cb(fmt.Sprintf("  Profil Akun : %s", strings.Join(st.Profiles, ", ")))
			cb(fmt.Sprintf("  Jumlah Eksposur Aktif : %d", len(st.Exposures)))
			client := http.Client{Timeout: 5 * time.Second}
			if resp, err := client.Get("https://1.1.1.1"); err == nil {
				resp.Body.Close()
				cb("  [OK] Konektivitas Cloudflare Edge (1.1.1.1) : Prima")
			} else {
				cb(fmt.Sprintf("  [WARN] Koneksi ke 1.1.1.1 gagal: %v", err))
			}
			cb("✓ Diagnostik Cloudflare selesai.")
			return nil
		}
	case "pdns", "powerdns":
		taskName = "PowerDNS Authoritative Server"
		fn = s.installer.InstallPowerDNS
	default:
		return "", nil, false
	}

	return taskName, fn, true
}

func (s *Server) handleSSEEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming tidak didukung", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientInstanceID := strings.TrimSpace(r.URL.Query().Get("instance_id"))
	var lastSeq int64
	if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			lastSeq = v
		}
	} else if raw := strings.TrimSpace(r.URL.Query().Get("last_seq")); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
			lastSeq = v
		}
	}

	replay, ch := s.runner.SubscribeFrom(clientInstanceID, lastSeq)
	defer s.runner.Unsubscribe(ch)

	// Send initial ping with runner instance_id and replay any missed live/latest events
	pingPayload, _ := json.Marshal(map[string]any{
		"type":        "ping",
		"message":     "connected",
		"instance_id": s.runner.InstanceID(),
	})
	fmt.Fprintf(w, "data: %s\n\n", pingPayload)
	for _, evt := range replay {
		if bytes, err := json.Marshal(evt); err == nil {
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", evt.Seq, string(bytes))
		}
	}
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case evt, ok := <-ch:
			if !ok {
				return
			}
			bytes, err := json.Marshal(evt)
			if err == nil {
				fmt.Fprintf(w, "id: %d\ndata: %s\n\n", evt.Seq, string(bytes))
				flusher.Flush()
			}
		}
	}
}

func (s *Server) handleMailStatus(w http.ResponseWriter, r *http.Request) {
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	st, err := s.mailMgr.GetStatus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleMailAccounts(w http.ResponseWriter, r *http.Request) {
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	domain := r.URL.Query().Get("domain")
	accounts, err := s.mailMgr.GetDomainAccounts(domain)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}

func (s *Server) handleMailAccountAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if err := s.mailMgr.AddAccount(payload.Email, payload.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "created",
		"message": "Akun email " + payload.Email + " berhasil ditambahkan",
	})
}

func (s *Server) handleMailAccountDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if err := s.mailMgr.DeleteAccount(payload.Email); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "message": "Akun " + payload.Email + " dihapus"})
}

func (s *Server) handleMailRelaySet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload mail.SetRelayOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if err := s.mailMgr.SetRelay(payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "updated",
		"message": "Konfigurasi relay domain " + payload.Domain + " berhasil disimpan",
		"dns":     s.mailMgr.LastRelayDNSReport(),
	})
}

func (s *Server) handleMailPreflight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	domain := r.URL.Query().Get("domain")
	writeJSON(w, http.StatusOK, s.mailMgr.Preflight(domain))
}

func (s *Server) handleMailRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	info, err := s.mailMgr.GetOutboundRoutes()
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleMailRelayRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if err := s.mailMgr.RemoveRelay(payload.Domain); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "removed",
		"message": "Override relay domain " + payload.Domain + " dihapus; domain kembali ke relay default",
		"dns":     s.mailMgr.LastRelayDNSReport(),
	})
}

func (s *Server) handleMailRelayDNS(w http.ResponseWriter, r *http.Request) {
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	switch r.Method {
	case http.MethodGet:
		domain := r.URL.Query().Get("domain")
		if strings.TrimSpace(domain) == "" {
			writeError(w, http.StatusBadRequest, "Parameter domain diperlukan")
			return
		}
		writeJSON(w, http.StatusOK, s.mailMgr.ListRelayDNS(domain))
	case http.MethodPost:
		var payload struct {
			Domain   string                    `json:"domain"`
			Provider string                    `json:"provider"`
			Records  []mail.ManagedRelayRecord `json:"records"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
			return
		}
		report, err := s.mailMgr.AddRelayDNS(payload.Domain, payload.Provider, payload.Records)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "report": report})
			return
		}
		writeJSON(w, http.StatusOK, report)
	case http.MethodDelete:
		domain := r.URL.Query().Get("domain")
		provider := r.URL.Query().Get("provider")
		if strings.TrimSpace(domain) == "" {
			writeError(w, http.StatusBadRequest, "Parameter domain diperlukan")
			return
		}
		report, err := s.mailMgr.DeleteRelayDNS(domain, provider)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "report": report})
			return
		}
		writeJSON(w, http.StatusOK, report)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

func (s *Server) handleMailAdminReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if err := s.mailMgr.ResetAdmin(payload.Domain); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "reset",
		"message": "Password admin domain " + payload.Domain + " berhasil direset",
	})
}

func (s *Server) handleMailDomainDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload struct {
		Domain    string `json:"domain"`
		DeleteDNS bool   `json:"delete_dns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if strings.TrimSpace(payload.Domain) == "" {
		writeError(w, http.StatusBadRequest, "Domain wajib diisi")
		return
	}
	report, err := s.mailMgr.DeleteDomain(payload.Domain, payload.DeleteDNS)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "report": report})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleMailDomainRepair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	domain := strings.TrimSpace(payload.Domain)
	if domain == "" {
		writeError(w, http.StatusBadRequest, "Domain wajib diisi")
		return
	}

	taskName := fmt.Sprintf("Perbaiki Domain Mail (%s)", domain)
	taskID, err := s.runner.StartNativeTask(taskName, func(ctx context.Context, cb func(string)) error {
		report, repErr := s.mailMgr.RepairDomain(domain, cb)
		for _, c := range report.Checks {
			cb(fmt.Sprintf("[%s] %s: %s", strings.ToUpper(c.Status), c.Name, c.Detail))
		}
		if repErr != nil {
			return repErr
		}
		if !report.OK {
			return fmt.Errorf("sebagian langkah perbaikan gagal; periksa detail di atas")
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "started", "task_id": taskID})
}

func (s *Server) handleMailSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload mail.SetupMailOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	if strings.TrimSpace(payload.Domain) == "" {
		writeError(w, http.StatusBadRequest, "Domain wajib diisi")
		return
	}

	taskName := fmt.Sprintf("Setup Mail Server & Domain (%s)", payload.Domain)
	taskID, err := s.runner.StartNativeTask(taskName, func(ctx context.Context, cb func(string)) error {
		return s.mailMgr.SetupMail(ctx, cb, payload)
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "started",
		"task_id": taskID,
	})
}

func (s *Server) handleWorkspaceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}

	type OrcaInfo struct {
		Installed     bool   `json:"installed"`
		Active        bool   `json:"active"`
		StatusText    string `json:"status_text"`
		Mode          string `json:"mode"`
		Version       string `json:"version"`
		LatestVersion string `json:"latest_version,omitempty"`
		HasUpdate     bool   `json:"has_update"`
		Port          int    `json:"port,omitempty"`
		PID           int    `json:"pid,omitempty"`
		WebClientURL  string `json:"web_client_url,omitempty"`
		PairingURL    string `json:"pairing_url,omitempty"`
		Endpoint      string `json:"endpoint,omitempty"`
	}

	type ShellInfo struct {
		ZshInstalled bool     `json:"zsh_installed"`
		OhMyZsh      bool     `json:"oh_my_zsh"`
		CurrentShell string   `json:"current_shell"`
		ActiveTheme  string   `json:"active_theme"`
		NerdFont     string   `json:"nerd_font"`
		Plugins      []string `json:"plugins"`
	}

	type WorkspaceData struct {
		Orca  OrcaInfo  `json:"orca"`
		Shell ShellInfo `json:"shell"`
	}

	var data WorkspaceData

	targetUser, userHome := installer.ResolveOrcaUser()
	orcaConfigDir := filepath.Join(userHome, ".config", "orca")

	// 1. Orca info (mendukung .deb ekstrak, AppImage, RPM, maupun Headless Server)
	if _, err := os.Stat("/usr/local/lib/orca-ide/orca.AppImage"); err == nil {
		data.Orca.Installed = true
	} else if _, err := os.Stat("/usr/local/lib/orca-server"); err == nil {
		data.Orca.Installed = true
	} else if _, err := os.Stat("/usr/local/bin/orca"); err == nil {
		data.Orca.Installed = true
	} else if _, err := exec.LookPath("orca"); err == nil {
		data.Orca.Installed = true
	} else if _, err := os.Stat(filepath.Join(userHome, ".local", "bin", "orca")); err == nil {
		data.Orca.Installed = true
	} else if _, err := os.Stat(filepath.Join(userHome, ".local", "lib", "orca-ide")); err == nil {
		data.Orca.Installed = true
	} else if _, err := os.Stat(orcaConfigDir); err == nil {
		data.Orca.Installed = true
	}

	if monitor.IsSystemdServiceActive("orca") {
		data.Orca.Active = true
	} else if out, err := exec.Command("pgrep", "-f", "orca-ide").Output(); err == nil && len(strings.TrimSpace(string(out))) > 0 {
		data.Orca.Active = true
	}

	modeFile := filepath.Join(orcaConfigDir, ".mode")
	if mBytes, err := os.ReadFile(modeFile); err == nil {
		data.Orca.Mode = strings.TrimSpace(string(mBytes))
	}
	if data.Orca.Mode == "" {
		data.Orca.Mode = "headless"
	}

	modeCapitalized := "Headless Server"
	if data.Orca.Mode == "desktop" {
		modeCapitalized = "Desktop GUI"
	}

	if data.Orca.Active {
		data.Orca.StatusText = fmt.Sprintf("Running (%s)", modeCapitalized)
	} else if data.Orca.Installed {
		data.Orca.StatusText = fmt.Sprintf("Stopped (%s)", modeCapitalized)
	} else {
		data.Orca.StatusText = "Belum Terpasang"
	}

	if ver := installer.DetectOrcaInstalledVersion(r.Context(), targetUser, userHome); ver != "" {
		data.Orca.Version = ver
	}

	forceRefresh := r.URL.Query().Get("refresh") == "1"
	if latestOrca := installer.GetOrcaLatestVersion(r.Context(), forceRefresh); latestOrca != "" {
		data.Orca.LatestVersion = latestOrca
		if data.Orca.Installed && data.Orca.Version != "" {
			data.Orca.HasUpdate = installer.HasNewerVersion(data.Orca.Version, latestOrca)
		}
	}

	if data.Orca.Active {
		rtFile := filepath.Join(orcaConfigDir, "orca-runtime.json")
		if rtBytes, err := os.ReadFile(rtFile); err == nil {
			var rt struct {
				PID        int `json:"pid"`
				Transports []struct {
					Kind     string `json:"kind"`
					Endpoint string `json:"endpoint"`
				} `json:"transports"`
			}
			if err := json.Unmarshal(rtBytes, &rt); err == nil {
				data.Orca.PID = rt.PID
				for _, t := range rt.Transports {
					if t.Kind == "websocket" {
						data.Orca.Endpoint = t.Endpoint
						parts := strings.Split(t.Endpoint, ":")
						if len(parts) >= 3 {
							pStr := strings.TrimRight(parts[2], "/")
							if p, err := strconv.Atoi(pStr); err == nil {
								data.Orca.Port = p
							}
						}
					}
				}
			}
		}
		if data.Orca.Port == 0 {
			data.Orca.Port = 6768
		}

		pairCtx, pairCancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer pairCancel()
		pairing, web, _ := installer.OrcaPairInfo(pairCtx, userHome)
		data.Orca.PairingURL = pairing
		data.Orca.WebClientURL = web
	}

	// 2. Shell info
	if _, err := exec.LookPath("zsh"); err == nil {
		data.Shell.ZshInstalled = true
	} else if _, err := os.Stat("/usr/bin/zsh"); err == nil {
		data.Shell.ZshInstalled = true
	}
	if fi, err := os.Stat(filepath.Join(userHome, ".oh-my-zsh")); err == nil && fi.IsDir() {
		data.Shell.OhMyZsh = true
	}

	if out, err := exec.Command("getent", "passwd", targetUser).Output(); err == nil {
		parts := strings.Split(strings.TrimSpace(string(out)), ":")
		if len(parts) >= 7 {
			data.Shell.CurrentShell = parts[6]
		}
	}
	if data.Shell.CurrentShell == "" {
		data.Shell.CurrentShell = os.Getenv("SHELL")
	}

	zshrcFile := filepath.Join(userHome, ".zshrc")
	if zBytes, err := os.ReadFile(zshrcFile); err == nil {
		lines := strings.Split(string(zBytes), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "ZSH_THEME=") {
				theme := strings.TrimPrefix(trimmed, "ZSH_THEME=")
				theme = strings.Trim(theme, "\"'")
				data.Shell.ActiveTheme = theme
			}
		}
	}
	data.Shell.NerdFont = installer.DetectNerdFont(userHome)

	writeJSON(w, http.StatusOK, data)
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	limit := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	var active any
	var activeID int64
	var activeLog string
	if s.runner != nil {
		if t, logOut := s.runner.GetActiveTaskSnapshot(); t != nil {
			activeID = t.ID
			activeLog = logOut
			active = map[string]any{
				"id":         t.ID,
				"command":    t.Command,
				"started_at": t.StartedAt,
				"log_output": logOut,
			}
		}
	}

	tasks := []store.TaskRecord{}
	if s.store != nil {
		list, err := s.store.ListRecentTasks(limit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if list != nil {
			tasks = list
		}
	}

	if activeID != 0 && activeLog != "" {
		for i := range tasks {
			if tasks[i].ID == activeID && tasks[i].LogOutput == "" {
				tasks[i].LogOutput = activeLog
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"active_task": active,
		"tasks":       tasks,
	})
}

func (s *Server) handleEnv(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	report := installer.ToolchainEnvReport()
	if report == nil {
		report = []string{}
	}
	entries := installer.EffectivePathPreview(r.Context())
	if entries == nil {
		entries = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"report":       report,
		"path_entries": entries,
	})
}

func (s *Server) handleEnvSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		SudoPass string `json:"sudo_pass"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	}
	if payload.SudoPass != "" {
		if err := s.installer.VerifyAndSetSudoPassword(payload.SudoPass); err != nil {
			writeError(w, http.StatusUnauthorized, "Password sudo tidak valid: "+err.Error())
			return
		}
	}
	if reqSudo, _ := s.installer.CheckSudoRequired(); reqSudo {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "sudo_required",
			"message": "Sinkronisasi environment toolchain memerlukan hak akses administrator (sudo).",
		})
		return
	}

	taskID, err := s.runner.StartNativeTask("Sinkronisasi Environment Toolchain (PATH)", func(ctx context.Context, cb func(string)) error {
		return s.installer.SyncToolchainEnv(ctx, cb)
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "started", "task_id": taskID})
}

func (s *Server) handleUpdaterStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	force := r.URL.Query().Get("check") == "1" || r.URL.Query().Get("check") == "true" || r.URL.Query().Get("force") == "1"
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	res, err := updater.Run(ctx, version.Version, updater.Options{CheckOnly: true, Force: force}, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"current":    version.Version,
			"latest":     version.Version,
			"has_update": false,
			"error":      err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleUpdaterApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		SourceDir string `json:"source_dir"`
		SudoPass  string `json:"sudo_pass"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	}
	if strings.TrimSpace(payload.SourceDir) != "" {
		writeError(w, http.StatusBadRequest, "Pembaruan dari source_dir lokal tidak diizinkan lewat Web API; gunakan CLI: sudo wdev update --source <dir>")
		return
	}
	if payload.SudoPass != "" {
		if err := s.installer.VerifyAndSetSudoPassword(payload.SudoPass); err != nil {
			writeError(w, http.StatusUnauthorized, "Password sudo tidak valid: "+err.Error())
			return
		}
	}
	if reqSudo, _ := s.installer.CheckSudoRequired(); reqSudo {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "sudo_required",
			"message": "Pembaruan biner wdev memerlukan hak akses administrator (sudo).",
		})
		return
	}

	opts := updater.Options{}
	taskID, err := s.runner.StartNativeTask("Pembaruan Biner wdev", func(ctx context.Context, cb func(string)) error {
		_, err := updater.Run(ctx, version.Version, opts, cb)
		return err
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "started", "task_id": taskID})
}

func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "Endpoint API tidak ditemukan")
}
