package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
	"sync"
	"time"
	"wdev/internal/auth"
	"wdev/internal/cloudflare"
	"wdev/internal/compose"
	"wdev/internal/config"
	"wdev/internal/filemanager"
	"wdev/internal/installer"
	"wdev/internal/mail"
	"wdev/internal/monitor"
	"wdev/internal/pdns"
	"wdev/internal/runner"
	"wdev/internal/serviceconfig"
	"wdev/internal/site"
	"wdev/internal/siteapp"
	"wdev/internal/store"
	"wdev/internal/switcher"
	"wdev/internal/terminal"
	"wdev/internal/updater"
	"wdev/internal/version"
	"wdev/web"
)

type Server struct {
	cfg            *config.Config
	store          *store.Store
	runner         *runner.Runner
	installer      *installer.Installer
	siteMgr        *site.Manager
	switcherMgr    *switcher.Manager
	cfMgr          *cloudflare.Manager
	pdnsMgr        *pdns.Manager
	mailMgr        *mail.Manager
	authMgr        *auth.Manager
	svcCfgMgr      *serviceconfig.Manager
	fileMgr        *filemanager.Manager
	termMgr        *terminal.Manager
	composeMgr     *compose.Manager
	siteAppMgr     *siteapp.Manager
	termSec        *terminalSecurityState
	sudoVerifyFn   func(string) error
	mux            *http.ServeMux
	orcaSyncCancel context.CancelFunc
	portDeps       *portProbeDeps
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
	if mailMgr != nil && inst != nil {
		mailMgr.SetInstaller(inst.InstallMailStackWithDomain)
	}
	if cfMgr != nil && inst != nil {
		cfMgr.SetSudoPasswordFn(inst.GetSudoPassword)
	}
	var sudoFn func() string
	if inst != nil {
		sudoFn = inst.GetSudoPassword
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
		svcCfgMgr:   serviceconfig.New(sudoFn),
		fileMgr:     filemanager.New(cfg, s),
		termMgr:     terminal.New(cfg, s),
		composeMgr:  compose.New(cfg, s, sudoFn),
		siteAppMgr:  siteapp.New(cfg, s, sudoFn),
		mux:         http.NewServeMux(),
	}
	if siteMgr != nil && srv.siteAppMgr != nil {
		mgr := srv.siteAppMgr
		siteMgr.SetAppCleanupHook(func(domain string) error {
			return mgr.RemoveApp(context.Background(), domain)
		})
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
	s.mux.HandleFunc("/api/sites/ssl", s.handleSiteSSL)
	s.mux.HandleFunc("/api/sites/inspect-dir", s.handleInspectSiteDir)
	s.mux.HandleFunc("/api/sites/suggest-port", s.handleSuggestSitePort)
	s.mux.HandleFunc("/api/sites/app/action", s.handleSiteAppAction)
	s.mux.HandleFunc("/api/sites/app/build", s.handleSiteAppBuild)
	s.mux.HandleFunc("/api/sites/app/env", s.handleSiteAppEnv)
	s.mux.HandleFunc("/api/sites/app/logs", s.handleSiteAppLogs)
	s.mux.HandleFunc("/api/sites/app/logs/stream", s.handleSiteAppLogsStream)
	s.mux.HandleFunc("/api/sites/", s.handleSiteByDomain)
	s.mux.HandleFunc("/api/switchers", s.handleSwitchers)
	s.mux.HandleFunc("/api/switchers/", s.handleSwitchersAction)
	s.mux.HandleFunc("/api/cloudflare/status", s.handleCFStatus)
	s.mux.HandleFunc("/api/cloudflare/zones", s.handleCFProfileZones)
	s.mux.HandleFunc("/api/cloudflare/check-host", s.handleCFCheckHost)
	s.mux.HandleFunc("/api/cloudflare/expose", s.handleCFExpose)
	s.mux.HandleFunc("/api/cloudflare/unexpose", s.handleCFUnexpose)
	s.mux.HandleFunc("/api/cloudflare/profiles/token", s.handleCFRegisterToken)
	s.mux.HandleFunc("/api/cloudflare/profiles/cert", s.handleCFRegisterCert)
	s.mux.HandleFunc("/api/cloudflare/profiles/api-token", s.handleCFSetProfileAPIToken)
	s.mux.HandleFunc("/api/cloudflare/profiles/login/start", s.handleCFLoginStart)
	s.mux.HandleFunc("/api/cloudflare/profiles/login/status", s.handleCFLoginStatus)
	s.mux.HandleFunc("/api/cloudflare/profiles/login/cancel", s.handleCFLoginCancel)
	s.mux.HandleFunc("/api/cloudflare/profiles/control", s.handleCFProfileControl)
	s.mux.HandleFunc("/api/cloudflare/profiles/logs", s.handleCFProfileLogs)
	s.mux.HandleFunc("/api/cloudflare/profiles/delete", s.handleCFProfileDelete)
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
	s.mux.HandleFunc("/api/mail/account/password", s.handleMailAccountPassword)
	s.mux.HandleFunc("/api/mail/account/delete", s.handleMailAccountDelete)
	s.mux.HandleFunc("/api/mail/relay", s.handleMailRelaySet)
	s.mux.HandleFunc("/api/mail/preflight", s.handleMailPreflight)
	s.mux.HandleFunc("/api/mail/routes", s.handleMailRoutes)
	s.mux.HandleFunc("/api/mail/relay/remove", s.handleMailRelayRemove)
	s.mux.HandleFunc("/api/mail/relay/dns", s.handleMailRelayDNS)
	s.mux.HandleFunc("/api/mail/relay/dns/sync", s.handleMailRelayDNSSync)
	s.mux.HandleFunc("/api/mail/admin-reset", s.handleMailAdminReset)
	s.mux.HandleFunc("/api/mail/setup", s.handleMailSetup)
	s.mux.HandleFunc("/api/mail/domain/delete", s.handleMailDomainDelete)
	s.mux.HandleFunc("/api/mail/domain/repair", s.handleMailDomainRepair)
	s.mux.HandleFunc("/api/sudo/status", s.handleSudoStatus)
	s.mux.HandleFunc("/api/sudo/verify", s.handleSudoVerify)
	s.mux.HandleFunc("/api/toolchains", s.handleToolchains)
	s.mux.HandleFunc("/api/workspace/status", s.handleWorkspaceStatus)
	s.mux.HandleFunc("/api/workspace/orca-ai", s.handleWorkspaceOrcaAI)
	s.mux.HandleFunc("/api/workspace/orca-ai/models", s.handleWorkspaceOrcaAIModels)
	s.mux.HandleFunc("/api/workspace/orca-ai/sync", s.handleWorkspaceOrcaAISync)
	s.mux.HandleFunc("/api/setup/components", s.handleSetupComponents)
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
	s.mux.HandleFunc("/api/panel/domain", s.handlePanelDomain)
	s.mux.HandleFunc("/api/panel/domain/block-ip", s.handlePanelDomainBlockIP)
	s.mux.HandleFunc("/api/panel/ssl", s.handlePanelSSL)
	s.mux.HandleFunc("/api/ports", s.handlePorts)
	s.registerComposeRoutes()
	s.registerFileManagerRoutes()
	s.registerTerminalRoutes()
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
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self' ws: wss:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
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
				limit := maxAPIBody
				switch cleanURLPath {
				case "/api/files/upload":
					limit = filemanager.MaxUploadBytes
				case "/api/files/write":
					limit = 8 << 20
				}
				r.Body = http.MaxBytesReader(w, r.Body, limit)
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
					origin := strings.TrimSpace(r.Header.Get("Origin"))
					if origin != "" {
						u, err := url.Parse(origin)
						if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) || (u.Scheme != "http" && u.Scheme != "https") {
							writeError(w, http.StatusForbidden, "Origin tidak diizinkan (CSRF protection)")
							return
						}
					}
					referer := strings.TrimSpace(r.Header.Get("Referer"))
					if origin == "" && referer != "" {
						u, err := url.Parse(referer)
						if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) || (u.Scheme != "http" && u.Scheme != "https") {
							writeError(w, http.StatusForbidden, "Referer tidak diizinkan (CSRF protection)")
							return
						}
					}
					if hasSessionCookie && origin == "" && referer == "" {
						fetchSite := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
						if fetchSite != "same-origin" {
							writeError(w, http.StatusForbidden, "Header asal permintaan (Origin/Referer) diperlukan (CSRF protection)")
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
	if s.authMgr.IsAuthEnabled() {
		_ = s.authMgr.ClearInitialPassword()
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
	s.revokeTerminalGrantForRequest(r)
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
	s.revokeAllTerminalGrants()

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
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if os.Geteuid() != 0 && exec.Command("systemctl", "is-active", "--quiet", "wdev").Run() == nil {
			return fmt.Errorf("gagal bind %s (%w): service wdev.service sudah berjalan sebagai root; gunakan 'sudo wdev panel default' untuk melihat URL Security Entrance yang aktif", addr, err)
		}
		return err
	}

	useTLS := s.cfg.TLSCert != "" && s.cfg.TLSKey != ""
	if !useTLS && s.authMgr != nil && s.authMgr.IsPanelSSLEnabled() {
		if _, _, genErr := s.authMgr.EnsurePanelSelfSignedCert(s.panelExtraSANHosts()...); genErr == nil {
			useTLS = true
		}
	}
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
	if info.PanelDomainURL != "" {
		fmt.Printf("✓ Domain Resmi Panel (HTTPS)  : %s\n", info.PanelDomainURL)
		if info.BlockDirectIP {
			fmt.Println("✓ Akses Langsung via IP       : DIBLOKIR (Mode Bind Domain aaPanel)")
		}
	}
	if info.NetbirdURL != "" {
		fmt.Printf("✓ URL Akses NetBird           : %s\n", strings.Replace(info.NetbirdURL, "http://", scheme+"://", 1))
	}
	if info.SecurityEntrance != "" {
		fmt.Printf("✓ URL Akses Panel (Entrance)  : %s\n", strings.Replace(info.LocalURL, "http://", scheme+"://", 1))
	} else {
		fmt.Printf("✓ URL Akses Panel             : %s://localhost:%s\n", scheme, s.cfg.HTTPPort)
	}
	if !useTLS && info.PanelDomain == "" && (s.cfg.HTTPHost == "0.0.0.0" || s.cfg.HTTPHost == "::") {
		fmt.Println("⚠ PERINGATAN: Panel berjalan tanpa TLS. Aktifkan dengan 'wdev panel ssl on' atau ikat domain dengan 'wdev panel domain <domain>'.")
	}
	fmt.Println("✓ Info Kredensial & Security URL: jalankan 'wdev panel default'")
	fmt.Println("==================================================================")

	s.startOrcaCommitAISyncLoop()
	defer func() {
		if s.orcaSyncCancel != nil {
			s.orcaSyncCancel()
		}
	}()

	handler := s.wrapSecurityHeaders(s.wrapPanelDomain(s.wrapSecurityEntrance(s.wrapAuth(s.wrapCSRF(s.mux)))))
	return s.listenAndServeMux(ln, handler)
}

func (s *Server) syncNginxProxyAfterUpdate() {
	if s.siteMgr != nil {
		_, _ = s.siteMgr.EnsureProxyHTTPSForPHPVhosts()
	}
	if s.pdnsMgr != nil {
		_, _ = s.pdnsMgr.EnsureEdgeRealIPConfig()
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) applyPDNSModeToStats(stats *monitor.SystemStats) {
	if stats == nil || s.pdnsMgr == nil {
		return
	}
	stats.ServerMode = s.pdnsMgr.GetMode()
	cfg := s.pdnsMgr.LoadConfig()
	if stats.ServerMode != "local" && cfg.VPSPublicIP != "" && pdns.IsPublicIPv4String(cfg.VPSPublicIP) {
		stats.PublicIP = cfg.VPSPublicIP
	}
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
	s.applyPDNSModeToStats(stats)
	if s.svcCfgMgr != nil && stats != nil {
		var wg sync.WaitGroup
		for i := range stats.Services {
			svc := &stats.Services[i]
			if !svc.Installed {
				continue
			}
			switch svc.Name {
			case "nginx", "php-fpm", "postgresql", "redis", "docker":
				wg.Add(1)
				go func(target *monitor.ServiceStatus) {
					defer wg.Done()
					if preset := s.svcCfgMgr.DetectServicePreset(r.Context(), target.Name); preset != "" {
						target.ConfigPreset = preset
					}
				}(svc)
			}
		}
		wg.Wait()
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

	if name == "orca" {
		if err := installer.ControlOrcaService(r.Context(), action); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

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
			case "php-fpm", "php":
				res, err = s.svcCfgMgr.InspectPHP(r.Context(), ver)
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
		case "docker":
			res, err = s.svcCfgMgr.GetDockerConfig(r.Context())
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
			if !s.verifySudoPasswordWithRateLimit(w, r, sudoPass, "Password sudo tidak valid: ") {
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

		if subAction == "save-role" && (service == "postgresql" || service == "postgres") {
			var req serviceconfig.PostgresSaveRoleRequest
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				writeError(w, http.StatusBadRequest, "Payload role PostgreSQL tidak valid: "+err.Error())
				return
			}
			created, err := s.svcCfgMgr.SavePostgresRole(r.Context(), req)
			if err != nil {
				if errors.Is(err, serviceconfig.ErrSudoRequired) {
					writeJSON(w, http.StatusOK, map[string]any{
						"status":  "sudo_required",
						"message": "Mengelola role PostgreSQL memerlukan hak akses administrator (sudo).",
					})
					return
				}
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			actionStatus := "updated"
			if created {
				actionStatus = "created"
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status": actionStatus,
				"role":   strings.TrimSpace(req.Role),
			})
			return
		}

		if subAction == "delete-role" && (service == "postgresql" || service == "postgres") {
			var req serviceconfig.PostgresDeleteRoleRequest
			if err := json.Unmarshal(bodyBytes, &req); err != nil {
				writeError(w, http.StatusBadRequest, "Payload penghapusan role PostgreSQL tidak valid: "+err.Error())
				return
			}
			if err := s.svcCfgMgr.DeletePostgresRole(r.Context(), req); err != nil {
				if errors.Is(err, serviceconfig.ErrSudoRequired) {
					writeJSON(w, http.StatusOK, map[string]any{
						"status":  "sudo_required",
						"message": "Menghapus role PostgreSQL memerlukan hak akses administrator (sudo).",
					})
					return
				}
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "deleted",
				"role":   strings.TrimSpace(req.Role),
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
		case "docker":
			var cfg serviceconfig.DockerConfig
			if err = json.Unmarshal(bodyBytes, &cfg); err != nil {
				writeError(w, http.StatusBadRequest, "Format konfigurasi Docker tidak valid: "+err.Error())
				return
			}
			saved, err = s.svcCfgMgr.SaveDockerConfig(r.Context(), cfg)
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
		writeJSON(w, http.StatusOK, s.enrichSitesWithApps(r.Context(), sites))

	case http.MethodPost:
		var req siteCreatePayload
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "Payload tidak valid")
			return
		}
		if req.SudoPass != "" {
			if !s.verifySudoPasswordWithRateLimit(w, r, req.SudoPass, "Password sudo tidak valid: ") {
				return
			}
		}

		resp, statusCode, err := s.createSiteWithModeSync(req)
		if err != nil {
			if s.writeSudoRequiredIfNeeded(w, err, "Membuat konfigurasi VHost & service systemd memerlukan hak akses administrator (sudo).") {
				return
			}
			writeError(w, statusCode, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

type siteCreatePayload struct {
	site.CreateOptions
	ExposeEdge *bool           `json:"expose_edge,omitempty"`
	EdgeDomain string          `json:"edge_domain,omitempty"`
	EdgeSSL    *bool           `json:"edge_ssl,omitempty"`
	AddHosts   *bool           `json:"add_hosts,omitempty"`
	AppConfig  *siteAppPayload `json:"app_config,omitempty"`
	SudoPass   string          `json:"sudo_pass,omitempty"`
}

func (s *Server) createSiteWithModeSync(req siteCreatePayload) (map[string]any, int, error) {
	cleanDom := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(req.Domain), "."))
	isPublic := site.ValidateLetsEncryptDomain(cleanDom) == nil
	mode := "local"
	if s.pdnsMgr != nil {
		mode = s.pdnsMgr.GetMode()
	}

	if !req.SkipHosts {
		if req.AddHosts != nil {
			req.SkipHosts = !*req.AddHosts
			req.ForceHosts = *req.AddHosts
		} else if (mode == "vps" || mode == "edge") && isPublic {
			// Domain publik di mode VPS/Edge diselesaikan lewat PowerDNS/DNS publik, bukan /etc/hosts lokal.
			req.SkipHosts = true
		}
	}

	wantExpose := false
	if mode == "edge" {
		if req.ExposeEdge != nil {
			wantExpose = *req.ExposeEdge
		} else if isPublic && s.pdnsMgr != nil {
			conf := s.pdnsMgr.LoadConfig()
			if conf.Configured && conf.VPSNetbirdIP != "" {
				wantExpose = true
			}
		}
	}
	if wantExpose {
		// SSL Let's Encrypt diterbitkan di VPS Edge Gateway; vhost lokal melayani HTTP port 80 lewat tunnel NetBird.
		req.SSLEnabled = false
	}

	isManagedApp := store.IsManagedAppFramework(req.Framework)
	var preparedAppRec store.SiteAppRecord
	if isManagedApp {
		if req.AppConfig == nil {
			return nil, http.StatusBadRequest, fmt.Errorf("konfigurasi app_config wajib diisi untuk Managed App")
		}
		appMgr := s.ensureSiteAppManager()
		if req.ProxyPort == 0 {
			if envPort := appMgr.DetectProjectEnvPort(req.RootPath); envPort > 0 && s.validateSiteProxyTarget(req.Framework, envPort, "") == nil {
				req.ProxyPort = envPort
			} else if freePort, pErr := s.suggestFreeSitePort(); pErr == nil {
				req.ProxyPort = freePort
			} else {
				return nil, http.StatusInternalServerError, pErr
			}
		}
		autoStart := true
		if req.AppConfig.AutoStart != nil {
			autoStart = *req.AppConfig.AutoStart
		}
		useProjectEnv := false
		if req.AppConfig.UseProjectEnv != nil {
			useProjectEnv = *req.AppConfig.UseProjectEnv
		} else if projEnv := appMgr.ProjectEnvPath(req.RootPath); projEnv != "" {
			if fi, err := os.Stat(projEnv); err == nil && !fi.IsDir() {
				useProjectEnv = true
			}
		}
		var vErr error
		preparedAppRec, vErr = appMgr.NormalizeAndValidate(store.SiteAppRecord{
			Domain:        cleanDom,
			Runtime:       req.Framework,
			WorkingDir:    req.RootPath,
			BuildCommand:  req.AppConfig.BuildCommand,
			StartCommand:  req.AppConfig.StartCommand,
			Port:          req.ProxyPort,
			RunUser:       req.AppConfig.RunUser,
			AutoStart:     autoStart,
			UseProjectEnv: useProjectEnv,
		})
		if vErr != nil {
			return nil, http.StatusBadRequest, vErr
		}
	}

	if err := s.validateSiteProxyTarget(req.Framework, req.ProxyPort, ""); err != nil {
		return nil, http.StatusBadRequest, err
	}

	if err := s.siteMgr.CreateSite(req.CreateOptions); err != nil {
		return nil, http.StatusInternalServerError, err
	}

	resp := map[string]any{"status": "created"}
	if isManagedApp {
		appMgr := s.ensureSiteAppManager()
		runBuildNow := req.AppConfig.BuildCommand != "" && (req.AppConfig.RunBuildNow == nil || *req.AppConfig.RunBuildNow)
		savedApp, provErr := appMgr.ProvisionOrSync(
			context.Background(),
			preparedAppRec,
			!runBuildNow,
			req.AppConfig.ImportProjectEnv,
			req.AppConfig.InitialEnvRaw,
		)
		if provErr != nil {
			_ = s.siteMgr.DeleteSite(cleanDom)
			if errors.Is(provErr, serviceconfig.ErrSudoRequired) {
				return nil, http.StatusOK, provErr
			}
			return nil, http.StatusInternalServerError, provErr
		}
		resp["app"] = savedApp
		if runBuildNow && s.runner != nil {
			if taskID, bErr := appMgr.StartBuild(s.runner, cleanDom, true); bErr != nil {
				resp["build_error"] = bErr.Error()
			} else {
				resp["build_task_id"] = taskID
			}
		}
	}
	if mode == "vps" && s.pdnsMgr != nil {
		conf := s.pdnsMgr.LoadConfig()
		if conf.VPSPublicIP != "" && (conf.Configured || conf.HasLocalPDNS) {
			if zone, zErr := s.pdnsMgr.FindMatchingZone(cleanDom); zErr == nil && zone != "" {
				if existing, lErr := s.pdnsMgr.LookupRecords(zone, cleanDom, "A"); lErr == nil && len(existing) == 0 {
					if addErr := s.pdnsMgr.AddRecord(zone, cleanDom, "A", conf.VPSPublicIP, 300); addErr == nil {
						resp["dns_synced"] = true
						resp["dns_zone"] = strings.TrimSuffix(zone, ".")
						resp["dns_message"] = fmt.Sprintf("Record DNS A %s -> %s ditambahkan ke PowerDNS", cleanDom, conf.VPSPublicIP)
					}
				}
			}
		}
	} else if wantExpose && s.pdnsMgr != nil {
		pubDom := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(req.EdgeDomain), "."))
		if pubDom == "" {
			pubDom = cleanDom
		}
		useSSL := true
		if req.EdgeSSL != nil {
			useSSL = *req.EdgeSSL
		}
		if expErr := s.pdnsMgr.ExposeSite(cleanDom, pubDom, useSSL); expErr != nil {
			resp["edge_exposed"] = false
			resp["edge_domain"] = pubDom
			resp["edge_error"] = expErr.Error()
		} else {
			resp["edge_exposed"] = true
			resp["edge_domain"] = pubDom
			resp["edge_ssl"] = useSSL
			if conf := s.pdnsMgr.LoadConfig(); conf.VPSPublicIP != "" {
				if zone, zErr := s.pdnsMgr.FindMatchingZone(pubDom); zErr == nil && zone != "" {
					resp["dns_synced"] = true
					resp["dns_zone"] = strings.TrimSuffix(zone, ".")
					resp["dns_message"] = fmt.Sprintf("Record A %s -> %s & tunnel VPS Edge aktif", pubDom, conf.VPSPublicIP)
				}
			}
		}
	}
	return resp, http.StatusOK, nil
}

func (s *Server) handleSiteByDomain(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodDelete:
		s.handleSiteDelete(w, r)
	case http.MethodPut, http.MethodPatch:
		s.handleSiteUpdate(w, r)
	case http.MethodGet:
		s.handleSiteGet(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

func (s *Server) handleSiteGet(w http.ResponseWriter, r *http.Request) {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/sites/")), "."))
	if domain == "" {
		writeError(w, http.StatusBadRequest, "Domain tidak valid")
		return
	}
	siteRec, ok := s.siteMgr.GetSite(domain)
	if !ok {
		writeError(w, http.StatusNotFound, "Virtual host tidak ditemukan")
		return
	}
	writeJSON(w, http.StatusOK, s.enrichSingleSiteWithApp(r.Context(), siteRec))
}

func isStalwartInstalled() bool {
	for _, p := range []string{"/usr/local/bin/stalwart", "/etc/stalwart/config.json", "/etc/systemd/system/stalwart.service"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// validateSiteProxyTarget mencegah eksposur port layanan internal (panel wdev, PowerDNS API,
// Edge Agent, PostgreSQL, Redis, Stalwart Admin) maupun bentrok port antar-VHost reverse proxy / Managed App.
func (s *Server) validateSiteProxyTarget(framework string, proxyPort int, existingDomain string) error {
	if !site.UsesReverseProxy(framework) {
		return nil
	}
	if proxyPort <= 0 || proxyPort > 65535 {
		return fmt.Errorf("port proxy tidak valid: %d", proxyPort)
	}
	reserved := s.reservedSitePortsMap()
	panelPort := s.panelPortNumber()
	if panelPort > 0 && proxyPort == panelPort {
		allowBoundPanelUpdate := false
		if existingDomain != "" && s.authMgr != nil {
			boundDom := s.authMgr.GetPanelDomain()
			if boundDom != "" && strings.EqualFold(existingDomain, boundDom) && s.siteMgr != nil {
				if existing, ok := s.siteMgr.GetSite(existingDomain); ok && strings.EqualFold(existing.Framework, "proxy") && existing.ProxyPort == panelPort {
					allowBoundPanelUpdate = true
				}
			}
		}
		if !allowBoundPanelUpdate {
			return fmt.Errorf("port %d digunakan oleh panel wdev; gunakan pengaturan Domain Panel", proxyPort)
		}
	}
	if svc, blocked := reserved[proxyPort]; blocked {
		return fmt.Errorf("port proxy %d dicadangkan untuk layanan internal (%s) dan tidak dapat diekspos lewat Virtual Host", proxyPort, svc)
	}
	if s.siteMgr != nil {
		if sites, err := s.siteMgr.ListSites(); err == nil {
			for _, st := range sites {
				if existingDomain != "" && strings.EqualFold(st.Domain, existingDomain) {
					continue
				}
				if site.UsesReverseProxy(st.Framework) && st.ProxyPort == proxyPort {
					return fmt.Errorf("port lokal %d sudah digunakan oleh Virtual Host %s", proxyPort, st.Domain)
				}
			}
		}
	}
	return nil
}

func (s *Server) handleSiteUpdate(w http.ResponseWriter, r *http.Request) {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/sites/")), "."))
	if domain == "" {
		writeError(w, http.StatusBadRequest, "Domain tidak valid")
		return
	}

	var req struct {
		site.CreateOptions
		AppConfig *siteAppPayload `json:"app_config,omitempty"`
		SudoPass  string          `json:"sudo_pass,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}

	if req.SudoPass != "" {
		if !s.verifySudoPasswordWithRateLimit(w, r, req.SudoPass, "Password sudo tidak valid: ") {
			return
		}
	}

	req.Domain = domain
	var prevSite store.SiteRecord
	var hasPrevSite bool
	if s.siteMgr != nil {
		prevSite, hasPrevSite = s.siteMgr.GetSite(domain)
	}
	var prevApp *store.SiteAppRecord
	if s.store != nil {
		prevApp, _ = s.store.GetSiteApp(domain)
	}

	effectiveFramework := strings.ToLower(strings.TrimSpace(req.Framework))
	if effectiveFramework == "" && hasPrevSite {
		effectiveFramework = strings.ToLower(strings.TrimSpace(prevSite.Framework))
	}
	effectivePort := req.ProxyPort
	if effectivePort == 0 && site.UsesReverseProxy(effectiveFramework) {
		if prevApp != nil && prevApp.Port > 0 {
			effectivePort = prevApp.Port
		} else if hasPrevSite && prevSite.ProxyPort > 0 {
			effectivePort = prevSite.ProxyPort
		} else if store.IsManagedAppFramework(effectiveFramework) {
			appMgr := s.ensureSiteAppManager()
			candidateDir := strings.TrimSpace(req.RootPath)
			if candidateDir == "" && hasPrevSite {
				candidateDir = prevSite.RootPath
			}
			if envPort := appMgr.DetectProjectEnvPort(candidateDir); envPort > 0 && s.validateSiteProxyTarget(effectiveFramework, envPort, domain) == nil {
				effectivePort = envPort
			} else if freePort, pErr := s.suggestFreeSitePort(); pErr == nil {
				effectivePort = freePort
			}
		}
		req.ProxyPort = effectivePort
	}
	if err := s.validateSiteProxyTarget(effectiveFramework, effectivePort, domain); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	isNextManagedApp := store.IsManagedAppFramework(effectiveFramework)
	var nextAppRec store.SiteAppRecord
	var runBuildAfter bool
	if isNextManagedApp {
		workingDir := strings.TrimSpace(req.RootPath)
		if workingDir == "" && prevApp != nil {
			workingDir = prevApp.WorkingDir
		}
		if workingDir == "" && hasPrevSite {
			workingDir = prevSite.RootPath
		}
		req.RootPath = workingDir

		buildCmd := ""
		startCmd := ""
		runUser := ""
		autoStart := true
		useProjectEnv := false
		appMgr := s.ensureSiteAppManager()
		if prevApp != nil {
			buildCmd = prevApp.BuildCommand
			startCmd = prevApp.StartCommand
			runUser = prevApp.RunUser
			autoStart = prevApp.AutoStart
			useProjectEnv = prevApp.UseProjectEnv
		} else if projEnv := appMgr.ProjectEnvPath(workingDir); projEnv != "" {
			if fi, err := os.Stat(projEnv); err == nil && !fi.IsDir() {
				useProjectEnv = true
			}
		}
		if req.AppConfig != nil {
			buildCmd = req.AppConfig.BuildCommand
			if strings.TrimSpace(req.AppConfig.StartCommand) != "" || prevApp == nil {
				startCmd = req.AppConfig.StartCommand
			}
			if strings.TrimSpace(req.AppConfig.RunUser) != "" {
				runUser = req.AppConfig.RunUser
			}
			if req.AppConfig.AutoStart != nil {
				autoStart = *req.AppConfig.AutoStart
			}
			if req.AppConfig.UseProjectEnv != nil {
				useProjectEnv = *req.AppConfig.UseProjectEnv
			}
			if req.AppConfig.RunBuildNow != nil {
				runBuildAfter = *req.AppConfig.RunBuildNow && strings.TrimSpace(buildCmd) != ""
			}
		}

		var vErr error
		nextAppRec, vErr = appMgr.NormalizeAndValidate(store.SiteAppRecord{
			Domain:        domain,
			Runtime:       effectiveFramework,
			WorkingDir:    workingDir,
			BuildCommand:  buildCmd,
			StartCommand:  startCmd,
			Port:          effectivePort,
			RunUser:       runUser,
			AutoStart:     autoStart,
			UseProjectEnv: useProjectEnv,
		})
		if vErr != nil {
			writeError(w, http.StatusBadRequest, vErr.Error())
			return
		}
	}

	// Jika VHost beralih dari Managed App ke mode non-app (PHP/Static/Proxy), bersihkan unit systemd.
	wasManagedApp := prevApp != nil || (hasPrevSite && store.IsManagedAppFramework(prevSite.Framework))
	if wasManagedApp && !isNextManagedApp {
		if err := s.ensureSiteAppManager().RemoveApp(r.Context(), domain); err != nil {
			if s.writeSudoRequiredIfNeeded(w, err, "Menghapus service systemd Managed App memerlukan hak akses administrator (sudo).") {
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	if err := s.siteMgr.UpdateSite(domain, req.CreateOptions); err != nil {
		if s.writeSudoRequiredIfNeeded(w, err, "Memperbarui konfigurasi Nginx memerlukan hak akses administrator (sudo).") {
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := map[string]any{
		"status": "updated",
	}

	if isNextManagedApp {
		appMgr := s.ensureSiteAppManager()
		if _, err := appMgr.UpdateAndSync(r.Context(), prevApp, nextAppRec); err != nil {
			if s.writeSudoRequiredIfNeeded(w, err, "Menyinkronkan service systemd aplikasi memerlukan hak akses administrator (sudo).") {
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if runBuildAfter && s.runner != nil {
			if taskID, bErr := appMgr.StartBuild(s.runner, domain, true); bErr != nil {
				resp["build_error"] = bErr.Error()
			} else {
				resp["build_task_id"] = taskID
			}
		}
	}

	siteRec, _ := s.siteMgr.GetSite(domain)
	resp["site"] = s.enrichSingleSiteWithApp(r.Context(), siteRec)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSiteDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/api/sites/")), "."))
	if domain == "" {
		writeError(w, http.StatusBadRequest, "Domain tidak valid")
		return
	}

	if r.Body != nil {
		var body struct {
			SudoPass string `json:"sudo_pass"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil && body.SudoPass != "" {
			if !s.verifySudoPasswordWithRateLimit(w, r, body.SudoPass, "Password sudo tidak valid: ") {
				return
			}
		}
	}

	unexpose := r.URL.Query().Get("unexpose") == "1" || r.URL.Query().Get("unexpose") == "true"
	deleteDNS := r.URL.Query().Get("delete_dns") == "1" || r.URL.Query().Get("delete_dns") == "true"

	var existingExposed string
	if existing, ok := s.siteMgr.GetSite(domain); ok {
		existingExposed = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(existing.ExposedTo), "."))
	}

	if err := s.siteMgr.DeleteSite(domain); err != nil {
		if s.writeSudoRequiredIfNeeded(w, err, "Menghapus konfigurasi Nginx dan layanan aplikasi memerlukan hak akses administrator (sudo).") {
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := map[string]any{"status": "deleted"}
	mode := "local"
	if s.pdnsMgr != nil {
		mode = s.pdnsMgr.GetMode()
	}

	if mode == "edge" && unexpose && existingExposed != "" && s.pdnsMgr != nil {
		if err := s.pdnsMgr.Unexpose(existingExposed, !deleteDNS); err != nil {
			resp["unexposed"] = false
			resp["unexpose_error"] = err.Error()
		} else {
			resp["unexposed"] = true
			resp["dns_deleted"] = deleteDNS
		}
	} else if mode == "vps" && deleteDNS && s.pdnsMgr != nil {
		if _, delErr := s.pdnsMgr.DeleteDomainDNSRecords(domain); delErr == nil {
			resp["dns_deleted"] = true
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSiteSSL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Domain   string `json:"domain"`
		Email    string `json:"email"`
		SudoPass string `json:"sudo_pass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(payload.Domain), "."))
	if err := site.ValidateLetsEncryptDomain(domain); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.siteMgr == nil || !s.siteMgr.HasSite(domain) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Virtual host untuk %s tidak ditemukan", domain))
		return
	}
	if payload.SudoPass != "" {
		if !s.verifySudoPasswordWithRateLimit(w, r, payload.SudoPass, "Password sudo tidak valid: ") {
			return
		}
	}
	if s.installer != nil {
		if reqSudo, _ := s.installer.CheckSudoRequired(); reqSudo {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "sudo_required",
				"message": "Penerbitan sertifikat SSL Let's Encrypt memerlukan hak akses administrator (sudo).",
			})
			return
		}
	}

	taskName := fmt.Sprintf("SSL Let's Encrypt (%s)", domain)
	taskID, err := s.runner.StartNativeTask(taskName, func(ctx context.Context, cb func(string)) error {
		return s.siteMgr.EnableLetsEncrypt(ctx, cb, domain, payload.Email)
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "started",
		"task_id": taskID,
		"domain":  domain,
	})
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
		if !s.verifySudoPasswordWithRateLimit(w, r, payload.SudoPass, "Password sudo tidak valid: ") {
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
	if st.ProfileDetails == nil {
		st.ProfileDetails = []cloudflare.ProfileInfo{}
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
	var payload cloudflare.ExposeOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	res, err := s.cfMgr.ExposeWithOptions(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleCFUnexpose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		LocalDomain string `json:"local_domain"`
		Profile     string `json:"profile"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	res, err := s.cfMgr.UnexposeWithResult(payload.LocalDomain, payload.Profile)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleCFProfileZones(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	profile := strings.TrimSpace(r.URL.Query().Get("profile"))
	if profile == "" {
		profile = "default"
	}
	zones, err := s.cfMgr.ListProfileZones(profile)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"profile": profile,
		"zones":   zones,
	})
}

func (s *Server) handleCFCheckHost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	profile := strings.TrimSpace(r.URL.Query().Get("profile"))
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	localDomain := strings.TrimSpace(r.URL.Query().Get("local_domain"))
	res, err := s.cfMgr.CheckPublicHost(profile, host, localDomain)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleCFSetProfileAPIToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Name     string `json:"name"`
		APIToken string `json:"api_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	info, err := s.cfMgr.SetProfileAPIToken(payload.Name, payload.APIToken)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "profile": info})
}

func (s *Server) handleCFRegisterToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload cloudflare.TokenProfileOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	info, err := s.cfMgr.RegisterTokenProfile(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "registered", "profile": info})
}

func (s *Server) handleCFRegisterCert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload cloudflare.CertProfileOptions
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	info, err := s.cfMgr.RegisterCertProfile(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "registered", "profile": info})
}

func (s *Server) handleCFLoginStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Name       string `json:"name"`
		TunnelName string `json:"tunnel_name"`
		APIToken   string `json:"api_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	sess, err := s.cfMgr.StartBrowserLoginWithOptions(payload.Name, payload.TunnelName, payload.APIToken)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleCFLoginStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	sessID := strings.TrimSpace(r.URL.Query().Get("id"))
	if sessID == "" {
		writeError(w, http.StatusBadRequest, "Parameter id sesi wajib diisi")
		return
	}
	sess, err := s.cfMgr.GetLoginSession(sessID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleCFLoginCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	if err := s.cfMgr.CancelLoginSession(payload.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleCFProfileControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Name   string `json:"name"`
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	if err := s.cfMgr.ControlProfile(payload.Name, payload.Action); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCFProfileLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	logs, err := s.cfMgr.GetProfileLogs(name, lines)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"profile": name, "logs": logs})
}

func (s *Server) handleCFProfileDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Name         string `json:"name"`
		DeleteRemote bool   `json:"delete_remote"`
		DeleteDNS    bool   `json:"delete_dns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	res, err := s.cfMgr.DeleteProfileWithOptions(payload.Name, cloudflare.DeleteProfileOptions{
		DeleteRemote: payload.DeleteRemote,
		DeleteDNS:    payload.DeleteDNS,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
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
		if payload.SudoPass != "" {
			if !s.verifySudoPasswordWithRateLimit(w, r, payload.SudoPass, "Password sudo tidak valid: ") {
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

// verifySudoPasswordWithRateLimit validates a sudo password through the shared per-IP
// auth rate limiter so secondary sudo_pass fields cannot bypass /api/sudo/verify lockouts.
func (s *Server) verifySudoPasswordWithRateLimit(w http.ResponseWriter, r *http.Request, pass, invalidPrefix string) bool {
	verifyFn := s.sudoVerifyFn
	if verifyFn == nil && s.installer != nil {
		verifyFn = s.installer.VerifyAndSetSudoPassword
	}
	if verifyFn == nil {
		writeError(w, http.StatusServiceUnavailable, "Installer tidak tersedia")
		return false
	}
	ip := auth.ClientIP(r)
	if s.authMgr != nil {
		if allowed, wait := s.authMgr.CheckRateLimit(ip); !allowed {
			writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Terlalu banyak percobaan gagal. Coba lagi dalam %d menit.", int(wait.Minutes())+1))
			return false
		}
	}
	if err := verifyFn(pass); err != nil {
		if s.authMgr != nil {
			s.authMgr.RecordLoginAttempt(ip, false)
		}
		writeError(w, http.StatusUnauthorized, invalidPrefix+err.Error())
		return false
	}
	if s.authMgr != nil {
		s.authMgr.RecordLoginAttempt(ip, true)
	}
	return true
}

func (s *Server) handleSudoVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	var payload struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid")
		return
	}
	if !s.verifySudoPasswordWithRateLimit(w, r, payload.Password, "Password sudo salah: ") {
		return
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

func (s *Server) handleSetupComponents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.installer == nil {
		writeError(w, http.StatusServiceUnavailable, "Installer tidak tersedia")
		return
	}
	writeJSON(w, http.StatusOK, s.installer.GetComponentStatuses(r.Context()))
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
	case "compose", "docker-compose":
		return "docker"
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
	case "docker":
		return 165
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
		Component     string                          `json:"component"`
		Version       string                          `json:"version"`
		Items         []installItemPayload            `json:"items"`
		Label         string                          `json:"label"`
		SudoPass      string                          `json:"sudo_pass"`
		DockerOptions *installer.DockerInstallOptions `json:"docker_options,omitempty"`
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

	if payload.DockerOptions != nil && payload.DockerOptions.DaemonConfig != nil {
		if err := serviceconfig.ValidateDockerConfig(*payload.DockerOptions.DaemonConfig); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	taskName, fn, needsSudo, errMsg := s.buildInstallBatch(items, payload.Label)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if payload.DockerOptions != nil && len(items) == 1 && canonicalInstallComponent(strings.TrimSpace(items[0].Component)) == "docker" {
		dockerOpts := *payload.DockerOptions
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallDockerWithOptions(ctx, cb, dockerOpts)
		}
	}

	if payload.SudoPass != "" {
		if !s.verifySudoPasswordWithRateLimit(w, r, payload.SudoPass, "Password sudo tidak valid: ") {
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
			if err := s.installer.RunUpdateAll(ctx, cb); err != nil {
				return err
			}
			s.syncNginxProxyAfterUpdate()
			return nil
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
			if err := s.installer.InstallNginx(ctx, cb); err != nil {
				return err
			}
			s.syncNginxProxyAfterUpdate()
			return nil
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
	case "docker", "compose", "docker-compose":
		taskName = "Docker Engine & Compose v5"
		fn = func(ctx context.Context, cb func(string)) error {
			return s.installer.InstallDocker(ctx, cb)
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
			return installer.ConfigureDefaultOrcaCommitAI(ctx, cb)
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
			if st.ServiceUnitReady {
				cb("  [OK] Systemd Template     : /etc/systemd/system/cloudflared@.service")
			} else {
				cb("  [WARN] Systemd Template     : belum terpasang (jalankan install cloudflare / daftar profil)")
			}
			cb(fmt.Sprintf("  Profil Akun (%d) :", len(st.ProfileDetails)))
			for _, p := range st.ProfileDetails {
				state := "NONAKTIF"
				if p.Active {
					state = "AKTIF"
				}
				cb(fmt.Sprintf("    - %-14s mode=%-5s status=%s tunnel=%s routes=%d", p.Name, p.AuthMode, state, p.TunnelID, p.RouteCount))
			}
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
	forceProbe := r.URL.Query().Get("probe") == "1" || r.URL.Query().Get("probe") == "true"
	st, err := s.mailMgr.GetStatusWithOptions(forceProbe)
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

func (s *Server) handleMailAccountPassword(w http.ResponseWriter, r *http.Request) {
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
	if err := s.mailMgr.ChangeAccountPassword(payload.Email, payload.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "updated",
		"message": "Password akun email " + payload.Email + " berhasil diperbarui",
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

func (s *Server) handleMailRelayDNSSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	if s.mailMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "Mail manager tidak tersedia")
		return
	}
	var payload struct {
		Domain   string `json:"domain"`
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload tidak valid: "+err.Error())
		return
	}
	dom := strings.TrimSpace(payload.Domain)
	if dom == "" {
		writeError(w, http.StatusBadRequest, "Parameter domain diperlukan")
		return
	}
	prov := strings.ToLower(strings.TrimSpace(payload.Provider))
	if prov == "" {
		prov = s.mailMgr.ActiveRelayProvider(dom)
	}
	if prov != "resend" {
		writeJSON(w, http.StatusOK, map[string]any{
			"domain":   dom,
			"provider": prov,
			"report":   s.mailMgr.EnsureManagedRelayDNS(dom, prov),
		})
		return
	}
	res, err := s.mailMgr.SyncResendDomainDNS(dom, payload.APIKey, true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  err.Error(),
			"result": res,
		})
		return
	}
	writeJSON(w, http.StatusOK, res)
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
		Installed     bool                              `json:"installed"`
		Active        bool                              `json:"active"`
		StatusText    string                            `json:"status_text"`
		Mode          string                            `json:"mode"`
		Version       string                            `json:"version"`
		LatestVersion string                            `json:"latest_version,omitempty"`
		HasUpdate     bool                              `json:"has_update"`
		Port          int                               `json:"port,omitempty"`
		PID           int                               `json:"pid,omitempty"`
		WebClientURL  string                            `json:"web_client_url,omitempty"`
		PairingURL    string                            `json:"pairing_url,omitempty"`
		Endpoint      string                            `json:"endpoint,omitempty"`
		CommitAI      *installer.OrcaCommitAISummary    `json:"commit_ai,omitempty"`
		SyncStatus    *installer.OrcaCommitAISyncStatus `json:"sync_status,omitempty"`
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
	data.Orca.Installed = installer.IsOrcaInstalled(userHome)

	if installer.IsOrcaServiceActive(r.Context(), targetUser, userHome) {
		data.Orca.Active = true
	} else if monitor.IsSystemdServiceActive("orca") {
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
	data.Orca.CommitAI = installer.GetOrcaCommitAISummary(r.Context())
	if syncStatus, syncErr := installer.GetOrcaCommitAISyncStatus(r.Context()); syncErr == nil && syncStatus != nil && syncStatus.Managed {
		data.Orca.SyncStatus = syncStatus
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

func (s *Server) handleWorkspaceOrcaAI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := installer.GetOrcaCommitAIConfig(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPost:
		var payload installer.OrcaCommitAISaveInput
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload konfigurasi Commit AI tidak valid")
			return
		}
		syncStatus, err := installer.SetOrcaCommitAIDesiredState(r.Context(), nil, payload)
		if err != nil {
			switch {
			case errors.Is(err, installer.ErrOrcaCommitAIValidation):
				writeError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, installer.ErrOrcaCommitAIConflict):
				writeError(w, http.StatusConflict, err.Error())
			default:
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		cfg, cfgErr := installer.GetOrcaCommitAIConfig(r.Context())
		if cfgErr != nil {
			cfg = nil
		}
		message := "Konfigurasi Orca Commit AI berhasil disimpan dan disinkronkan."
		if syncStatus != nil && !syncStatus.InSync {
			message = "Konfigurasi disimpan, tetapi sinkronisasi belum selesai: " + syncStatus.State
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "saved",
			"message": message,
			"config":  cfg,
			"sync":    syncStatus,
		})
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

func (s *Server) handleWorkspaceOrcaAIModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	agentID := strings.TrimSpace(r.URL.Query().Get("agent"))
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "Parameter agent wajib diisi")
		return
	}
	forceRefresh := r.URL.Query().Get("refresh") == "1"
	res, err := installer.DiscoverOrcaCommitAIModels(r.Context(), agentID, forceRefresh)
	if err != nil {
		if errors.Is(err, installer.ErrOrcaCommitAIValidation) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
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
		if !s.verifySudoPasswordWithRateLimit(w, r, payload.SudoPass, "Password sudo tidak valid: ") {
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
		if !s.verifySudoPasswordWithRateLimit(w, r, payload.SudoPass, "Password sudo tidak valid: ") {
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

	opts := updater.Options{SkipRestart: true}
	installed := false
	taskID, err := s.runner.StartNativeTaskAfter("Pembaruan Biner wdev", func(ctx context.Context, cb func(string)) error {
		res, err := updater.Run(ctx, version.Version, opts, cb)
		installed = res.Installed
		return err
	}, func(status string) {
		if status == "success" && installed {
			// Beri waktu singkat agar frame SSE finish sempat dikirim ke browser sebelum systemd mengirim SIGTERM.
			time.Sleep(300 * time.Millisecond)
			_ = updater.RestartService(context.Background(), "wdev.service")
		}
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
