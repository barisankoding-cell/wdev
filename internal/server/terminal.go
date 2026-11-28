package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"wdev/internal/auth"
	"wdev/internal/security"
	"wdev/internal/store"
	"wdev/internal/terminal"

	"github.com/gorilla/websocket"
)

const (
	termWriteWait       = 10 * time.Second
	termPongWait        = 60 * time.Second
	termPingEvery       = 25 * time.Second
	termTicketTTL       = 60 * time.Second
	termRootGrantWindow = 10 * time.Minute
)

type termLaunchTicket struct {
	TargetUser string
	ClientIP   string
	GrantKey   string
	ExpiresAt  time.Time
}

type termRootGrant struct {
	ExpiresAt time.Time
}

type terminalSecurityState struct {
	mu           sync.Mutex
	tickets      map[string]termLaunchTicket
	grants       map[string]termRootGrant
	requireAll   bool // pengujian: memaksa step-up meski bukan root
	stepUpBypass bool // tidak dipakai di produksi
}

func (s *Server) ensureTermSec() *terminalSecurityState {
	if s.termSec == nil {
		s.termSec = &terminalSecurityState{
			tickets: make(map[string]termLaunchTicket),
			grants:  make(map[string]termRootGrant),
		}
	}
	return s.termSec
}

func (s *Server) ensureTerminalManager() *terminal.Manager {
	if s.termMgr == nil {
		s.termMgr = terminal.New(s.cfg, s.store)
	}
	return s.termMgr
}

// TerminalManager mengembalikan manajer PTY terminal milik server.
func (s *Server) TerminalManager() *terminal.Manager {
	return s.ensureTerminalManager()
}

func (s *Server) registerTerminalRoutes() {
	s.mux.HandleFunc("/api/terminal/info", s.handleTerminalInfo)
	s.mux.HandleFunc("/api/terminal/unlock", s.handleTerminalUnlock)
	s.mux.HandleFunc("/api/terminal/lock", s.handleTerminalLock)
	s.mux.HandleFunc("/api/terminal/audit", s.handleTerminalAudit)
	s.mux.HandleFunc("/api/terminal/sessions", s.handleTerminalSessions)
	s.mux.HandleFunc("/api/terminal/sessions/", s.handleTerminalSessionAction)
	s.mux.HandleFunc("/api/terminal/ws", s.handleTerminalWS)
}

func hashTokenHex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func randomTerminalTicket() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func normalizedClientIP(r *http.Request) string {
	ip := auth.ClientIP(r)
	if parsed := net.ParseIP(ip); parsed != nil && parsed.IsLoopback() {
		return "loopback"
	}
	return ip
}

func (s *Server) sessionContextForRequest(r *http.Request) (grantKey string, panelUser string, hasSession bool) {
	if s.authMgr == nil {
		return "", "local", false
	}
	rawToken := ""
	if c, err := r.Cookie(auth.CookieSessionName); err == nil {
		rawToken = strings.TrimSpace(c.Value)
	}
	if rawToken == "" {
		if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
			rawToken = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		}
	}
	ip := normalizedClientIP(r)
	if rawToken != "" && s.authMgr.IsAuthEnabled() {
		if user, valid := s.authMgr.ValidateSession(rawToken); valid && user != "" {
			return hashTokenHex(rawToken) + "|" + ip, user, true
		}
	}
	if !s.authMgr.IsAuthEnabled() {
		return "", s.authMgr.GetUsername(), false
	}
	return "", "", false
}

func (ts *terminalSecurityState) pruneLocked(now time.Time) {
	for k, t := range ts.tickets {
		if now.After(t.ExpiresAt) {
			delete(ts.tickets, k)
		}
	}
	for k, g := range ts.grants {
		if now.After(g.ExpiresAt) {
			delete(ts.grants, k)
		}
	}
}

func (s *Server) revokeTerminalGrantForRequest(r *http.Request) {
	sec := s.termSec
	if sec == nil {
		return
	}
	grantKey, _, _ := s.sessionContextForRequest(r)
	if grantKey == "" {
		return
	}
	sec.mu.Lock()
	defer sec.mu.Unlock()
	delete(sec.grants, grantKey)
	for k, t := range sec.tickets {
		if t.GrantKey == grantKey {
			delete(sec.tickets, k)
		}
	}
}

func (s *Server) revokeAllTerminalGrants() {
	sec := s.termSec
	if sec == nil {
		return
	}
	sec.mu.Lock()
	defer sec.mu.Unlock()
	sec.grants = make(map[string]termRootGrant)
	sec.tickets = make(map[string]termLaunchTicket)
}

func (s *Server) isRootTerminalUnlocked(r *http.Request) (bool, *time.Time) {
	sec := s.ensureTermSec()
	grantKey, _, hasSession := s.sessionContextForRequest(r)
	if !hasSession || grantKey == "" {
		return false, nil
	}
	sec.mu.Lock()
	defer sec.mu.Unlock()
	sec.pruneLocked(time.Now())
	g, ok := sec.grants[grantKey]
	if !ok {
		return false, nil
	}
	exp := g.ExpiresAt
	return true, &exp
}

func (s *Server) handleTerminalInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	info := s.ensureTerminalManager().SystemInfo()
	if s.pdnsMgr != nil {
		info.ServerMode = s.pdnsMgr.GetMode()
	}
	info.IsHTTPS = auth.IsHTTPS(r)
	unlocked, exp := s.isRootTerminalUnlocked(r)
	info.RootUnlocked = unlocked
	info.RootUnlockExpiresAt = exp
	info.StepUpAlwaysRequired = s.requiresStepUpForAllTerminalUsers()
	info.TOTPRequired = s.authMgr != nil && s.authMgr.IsTOTPTerminalRequired()
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleTerminalUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	var payload struct {
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
		User     string `json:"user"`
		Remember bool   `json:"remember"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}

	ip := auth.ClientIP(r)
	if s.authMgr != nil {
		allowed, wait := s.authMgr.CheckRateLimit(ip)
		if !allowed {
			if s.secMgr != nil {
				_, _ = s.secMgr.RecordAttack(r.Context(), security.Event{
					SourceIP: ip,
					AttackType: security.AttackPanelBruteForce,
					Severity: security.SeverityHigh,
					Target: "/api/terminal/unlock",
					ActionTaken: security.ActionRateLimited,
					Details: fmt.Sprintf("Rate limited on terminal unlock: locked for %dm", int(wait.Minutes())+1),
				})
			}
			writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Terlalu banyak percobaan sandi salah. Coba lagi dalam %d menit.", int(wait.Minutes())+1))
			return
		}
		if !s.authMgr.VerifyAdminPassword(payload.Password) {
			s.authMgr.RecordLoginAttempt(ip, false)
			if s.secMgr != nil {
				_, _ = s.secMgr.RecordAttack(r.Context(), security.Event{
					SourceIP: ip,
					AttackType: security.AttackPanelBruteForce,
					Severity: security.SeverityHigh,
					Target: "/api/terminal/unlock",
					Details: fmt.Sprintf("Incorrect panel password on terminal unlock (target user: %s)", payload.User),
				})
			}
			writeError(w, http.StatusUnauthorized, "Password panel tidak cocok")
			return
		}
		if s.authMgr.IsTOTPTerminalRequired() {
			if _, _, ok := s.authMgr.VerifyTOTP(payload.TOTPCode); !ok {
				s.authMgr.RecordLoginAttempt(ip, false)
				if s.secMgr != nil {
					_, _ = s.secMgr.RecordAttack(r.Context(), security.Event{
						SourceIP:   ip,
						AttackType: security.AttackPanelBruteForce,
						Severity:   security.SeverityHigh,
						Target:     "/api/terminal/unlock",
						Details:    fmt.Sprintf("Invalid 2FA code on terminal unlock (target user: %s)", payload.User),
					})
				}
				writeError(w, http.StatusUnauthorized, "Kode verifikasi 2FA tidak valid")
				return
			}
		}
		s.authMgr.RecordLoginAttempt(ip, true)
	}

	target, err := s.ensureTerminalManager().ResolveTargetUser(payload.User)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	token, err := randomTerminalTicket()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Gagal membuat token terminal")
		return
	}

	now := time.Now()
	normIP := normalizedClientIP(r)
	grantKey, _, hasSession := s.sessionContextForRequest(r)

	sec := s.ensureTermSec()
	sec.mu.Lock()
	sec.pruneLocked(now)
	sec.tickets[token] = termLaunchTicket{
		TargetUser: target.Username,
		ClientIP:   normIP,
		GrantKey:   grantKey,
		ExpiresAt:  now.Add(termTicketTTL),
	}
	var grantExp *time.Time
	rootUnlocked := false
	if payload.Remember && hasSession && grantKey != "" {
		exp := now.Add(termRootGrantWindow)
		sec.grants[grantKey] = termRootGrant{ExpiresAt: exp}
		grantExp = &exp
		rootUnlocked = true
	}
	sec.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":                 "unlocked",
		"token":                  token,
		"expires_in":             int(termTicketTTL.Seconds()),
		"user":                   target.Username,
		"is_root":                target.IsRoot,
		"root_unlocked":          rootUnlocked,
		"root_unlock_expires_at": grantExp,
	})
}

func (s *Server) handleTerminalLock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	s.revokeTerminalGrantForRequest(r)
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "locked",
		"message": "Akses root terminal dikunci kembali",
	})
}

func (s *Server) handleTerminalAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if s.store == nil {
		writeJSON(w, http.StatusOK, []store.TerminalAuditRecord{})
		return
	}
	records, err := s.store.ListTerminalAudits(limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Gagal memuat riwayat audit terminal: "+err.Error())
		return
	}
	if records == nil {
		records = []store.TerminalAuditRecord{}
	}
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) canAccessTerminalSession(r *http.Request, sess *terminal.Session) bool {
	if sess == nil {
		return false
	}
	if s.authMgr == nil || !s.authMgr.IsAuthEnabled() {
		return true
	}
	_, panelUser, hasSession := s.sessionContextForRequest(r)
	if !hasSession || panelUser == "" {
		return false
	}
	if sess.PanelUser != "" && sess.PanelUser != panelUser {
		return false
	}
	return true
}

func (s *Server) handleTerminalSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	_, panelUser, _ := s.sessionContextForRequest(r)
	matchOwner := s.authMgr != nil && s.authMgr.IsAuthEnabled()
	sessions := s.ensureTerminalManager().ListSessions(panelUser, matchOwner)
	if sessions == nil {
		sessions = []terminal.SessionSnapshot{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleTerminalSessionAction(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/terminal/sessions/"), "/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "ID sesi terminal diperlukan")
		return
	}
	parts := strings.Split(rest, "/")
	if len(parts) > 2 {
		writeError(w, http.StatusNotFound, "Endpoint sesi terminal tidak ditemukan")
		return
	}
	sessionID := strings.TrimSpace(parts[0])
	action := ""
	if len(parts) == 2 {
		action = strings.TrimSpace(parts[1])
	}

	mgr := s.ensureTerminalManager()
	sess, ok := mgr.GetSession(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "Sesi terminal tidak ditemukan atau sudah berakhir")
		return
	}
	if !s.canAccessTerminalSession(r, sess) {
		writeError(w, http.StatusForbidden, "Anda tidak memiliki izin mengakses sesi terminal ini")
		return
	}

	switch action {
	case "":
		if r.Method != http.MethodDelete {
			writeMethodNotAllowed(w, r)
			return
		}
		mgr.CloseSession(sessionID, "user_closed")
		writeJSON(w, http.StatusOK, map[string]string{
			"status":     "closed",
			"session_id": sessionID,
		})
	case "keepalive":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}
		var payload struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
			return
		}
		sess.SetKeepAlive(payload.Enabled)
		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "ok",
			"session_id": sessionID,
			"keep_alive": sess.KeepAlive(),
		})
	case "extend":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, r)
			return
		}
		sec := sess.ExtendIdle()
		writeJSON(w, http.StatusOK, map[string]any{
			"status":               "ok",
			"session_id":           sessionID,
			"idle_timeout_seconds": sec,
		})
	default:
		writeError(w, http.StatusNotFound, "Aksi sesi terminal tidak dikenal")
	}
}

// Origin WebSocket diverifikasi per request memakai helper s.checkWSOrigin.

type wsClientControlMsg struct {
	Type    string `json:"type"`
	Cols    uint16 `json:"cols,omitempty"`
	Rows    uint16 `json:"rows,omitempty"`
	Data    string `json:"data,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}

func parseQueryUint16(q url.Values, key string, fallback uint16) uint16 {
	raw := strings.TrimSpace(q.Get(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseUint(raw, 10, 16)
	if err != nil {
		return fallback
	}
	return uint16(v)
}

func (s *Server) requiresStepUpForAllTerminalUsers() bool {
	if s.ensureTermSec().requireAll {
		return true
	}
	if s.pdnsMgr != nil && strings.EqualFold(s.pdnsMgr.GetMode(), "vps") {
		return true
	}
	return s.authMgr != nil && !s.authMgr.IsAuthEnabled() && os.Geteuid() == 0
}

func (s *Server) authorizeTerminalLaunch(r *http.Request, target terminal.TargetUser, token string) (bool, string) {
	sec := s.ensureTermSec()
	authEnabled := s.authMgr == nil || s.authMgr.IsAuthEnabled()
	needStepUp := s.requiresStepUpForAllTerminalUsers() || target.IsRoot
	if !needStepUp {
		return true, ""
	}
	if sec.stepUpBypass {
		return true, ""
	}

	now := time.Now()
	ip := normalizedClientIP(r)
	grantKey, _, hasSession := s.sessionContextForRequest(r)

	sec.mu.Lock()
	defer sec.mu.Unlock()
	sec.pruneLocked(now)

	// 1. Cek token peluncuran satu kali pakai (?token=...)
	if token = strings.TrimSpace(token); token != "" {
		if ticket, ok := sec.tickets[token]; ok {
			if ticket.TargetUser == target.Username && ticket.ClientIP == ip && now.Before(ticket.ExpiresAt) {
				delete(sec.tickets, token)
				return true, ""
			}
		}
	}

	// 2. Cek grant 10 menit (hanya tersedia bila login sesi panel aktif)
	if authEnabled && hasSession && grantKey != "" {
		if grant, ok := sec.grants[grantKey]; ok && now.Before(grant.ExpiresAt) {
			return true, ""
		}
	}

	return false, "Konfirmasi password panel diperlukan untuk membuka sesi terminal ini"
}

func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, r)
		return
	}
	if !s.checkWSOrigin(r) {
		writeError(w, http.StatusForbidden, "Origin WebSocket tidak diizinkan (CSWSH protection)")
		return
	}

	q := r.URL.Query()
	mgr := s.ensureTerminalManager()
	reqSessionID := strings.TrimSpace(q.Get("session_id"))

	var existingSess *terminal.Session
	var targetUser terminal.TargetUser
	reattachMissing := false

	if reqSessionID != "" {
		sess, ok := mgr.GetSession(reqSessionID)
		if ok {
			if !s.canAccessTerminalSession(r, sess) {
				writeError(w, http.StatusForbidden, "Anda tidak memiliki izin mengakses sesi terminal ini")
				return
			}
			targetUser = sess.TargetUser
			if okAuth, reason := s.authorizeTerminalLaunch(r, targetUser, q.Get("token")); !okAuth {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": reason,
					"code":  "terminal_unlock_required",
				})
				return
			}
			existingSess = sess
		} else {
			reattachMissing = true
		}
	} else {
		var err error
		targetUser, err = mgr.ResolveTargetUser(q.Get("user"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if ok, reason := s.authorizeTerminalLaunch(r, targetUser, q.Get("token")); !ok {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": reason,
				"code":  "terminal_unlock_required",
			})
			return
		}
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  8192,
		WriteBufferSize: 32768,
		CheckOrigin:     s.checkWSOrigin,
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var wsMu sync.Mutex
	sendTextJSON := func(payload any) error {
		b, merr := json.Marshal(payload)
		if merr != nil {
			return merr
		}
		wsMu.Lock()
		defer wsMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(termWriteWait))
		return conn.WriteMessage(websocket.TextMessage, b)
	}
	sendBinary := func(data []byte) error {
		wsMu.Lock()
		defer wsMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(termWriteWait))
		return conn.WriteMessage(websocket.BinaryMessage, data)
	}

	if reattachMissing {
		_ = sendTextJSON(map[string]any{
			"type":    "error",
			"code":    "session_not_found",
			"message": "Sesi terminal sebelumnya sudah berakhir atau tidak ditemukan.",
		})
		wsMu.Lock()
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		wsMu.Unlock()
		return
	}

	var sess *terminal.Session
	fallbackReason := ""
	reattached := false
	_, panelUser, _ := s.sessionContextForRequest(r)

	if existingSess != nil {
		sess = existingSess
		reattached = true
		cols := parseQueryUint16(q, "cols", 0)
		rows := parseQueryUint16(q, "rows", 0)
		if cols > 0 && rows > 0 {
			_ = sess.Resize(cols, rows)
		}
	} else {
		opts := terminal.StartOptions{
			PanelUser: panelUser,
			User:      targetUser.Username,
			Cwd:       q.Get("cwd"),
			Shell:     q.Get("shell"),
			Cols:      parseQueryUint16(q, "cols", terminal.DefaultCols),
			Rows:      parseQueryUint16(q, "rows", terminal.DefaultRows),
			Stream:    true,
			OnExit: func(closedSess *terminal.Session, exitCode int, reason string) {
				if s.store != nil {
					finalStatus := "closed"
					if reason == "idle_timeout" || reason == "detach_timeout" {
						finalStatus = reason
					}
					_ = s.store.CompleteTerminalAudit(closedSess.ID, finalStatus, exitCode)
				}
			},
		}

		composeProject := strings.TrimSpace(q.Get("compose_project"))
		composeService := strings.TrimSpace(q.Get("compose_service"))
		if composeProject != "" || composeService != "" {
			if composeProject == "" || composeService == "" {
				_ = sendTextJSON(map[string]any{
					"type":    "error",
					"message": "Parameter compose_project dan compose_service keduanya wajib diisi",
				})
				return
			}
			execDir, execArgs, execLabel, execErr := s.ensureComposeManager().BuildExecCommand(
				r.Context(),
				composeProject,
				composeService,
				q.Get("shell"),
			)
			if execErr != nil {
				_ = sendTextJSON(map[string]any{
					"type":    "error",
					"message": execErr.Error(),
				})
				return
			}
			opts.Cwd = execDir
			opts.ExecBin = "docker"
			opts.ExecArgs = execArgs
			opts.ExecLabel = "compose:" + execLabel
		}

		var startErr error
		sess, fallbackReason, startErr = mgr.StartSession(opts)
		if startErr != nil {
			_ = sendTextJSON(map[string]any{
				"type":    "error",
				"message": startErr.Error(),
			})
			return
		}

		clientIP := auth.ClientIP(r)
		if s.store != nil {
			_ = s.store.InsertTerminalAudit(store.TerminalAuditRecord{
				SessionID:  sess.ID,
				PanelUser:  panelUser,
				TargetUser: sess.TargetUser.Username,
				Shell:      sess.Shell,
				Cwd:        sess.Cwd,
				ClientIP:   clientIP,
				Status:     "active",
				StartedAt:  sess.StartedAt,
			})
		}

		privBadge := "\x1b[32mnon-root\x1b[0m"
		if sess.TargetUser.IsRoot {
			privBadge = "\x1b[1;31mROOT\x1b[0m"
		}
		bannerHeader := "wdev PTY SSH Shell"
		if opts.ExecLabel != "" {
			bannerHeader = "wdev Container Exec PTY"
		}
		banner := fmt.Sprintf(
			"\x1b[1;36m%s\x1b[0m \x1b[90m•\x1b[0m \x1b[32m%s@%s\x1b[0m [%s] \x1b[90m•\x1b[0m shell: \x1b[33m%s\x1b[0m\r\n\x1b[90mWorking Dir:\x1b[0m %s \x1b[90m• Auto-close idle %d menit\x1b[0m\r\n",
			bannerHeader, sess.User, sess.Hostname, privBadge, sess.Shell, sess.Cwd, int(sess.IdleTimeout.Minutes()),
		)
		if fallbackReason != "" {
			banner += fmt.Sprintf("\x1b[33mCatatan:\x1b[0m %s\r\n", fallbackReason)
		}
		banner += "\r\n"
		sess.AppendBanner([]byte(banner))
	}

	snapshot, subCh, unsub, alreadyExited, exitCode, closeReason := sess.Subscribe()
	defer unsub()

	_ = sendTextJSON(map[string]any{
		"type":                   "ready",
		"session_id":             sess.ID,
		"reattached":             reattached,
		"cwd":                    sess.Cwd,
		"shell":                  sess.Shell,
		"user":                   sess.TargetUser.Username,
		"is_root":                sess.TargetUser.IsRoot,
		"host":                   sess.Hostname,
		"fallback_reason":        fallbackReason,
		"keep_alive":             sess.KeepAlive(),
		"idle_timeout_seconds":   int(sess.IdleTimeout.Seconds()),
		"detach_timeout_seconds": int(sess.DetachTimeout.Seconds()),
	})

	if len(snapshot) > 0 {
		_ = sendBinary(snapshot)
	}

	if alreadyExited {
		if closeReason == "idle_timeout" {
			_ = sendTextJSON(map[string]any{
				"type":    "error",
				"code":    "idle_timeout",
				"message": "Sesi terminal ditutup otomatis karena idle terlalu lama.",
			})
		}
		_ = sendTextJSON(map[string]any{
			"type":   "exit",
			"code":   exitCode,
			"reason": closeReason,
		})
		wsMu.Lock()
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		wsMu.Unlock()
		return
	}

	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(termPongWait))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(termPongWait))
		return nil
	})

	doneCh := make(chan struct{})
	var doneOnce sync.Once
	stop := func() {
		doneOnce.Do(func() {
			close(doneCh)
		})
	}

	// Keepalive Ping untuk menjaga koneksi di belakang proxy / Cloudflare
	go func() {
		ticker := time.NewTicker(termPingEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				wsMu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(termWriteWait))
				perr := conn.WriteMessage(websocket.PingMessage, nil)
				wsMu.Unlock()
				if perr != nil {
					stop()
					return
				}
			case <-doneCh:
				return
			}
		}
	}()

	// Pompa stream PTY -> WebSocket
	go func() {
		defer stop()
		for {
			select {
			case <-doneCh:
				return
			case ev, ok := <-subCh:
				if !ok {
					return
				}
				if ev.Replaced {
					_ = sendTextJSON(map[string]any{
						"type":    "replaced",
						"message": "Sesi terminal diambil alih oleh koneksi baru.",
					})
					wsMu.Lock()
					_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
					wsMu.Unlock()
					return
				}
				if len(ev.Data) > 0 {
					if werr := sendBinary(ev.Data); werr != nil {
						return
					}
				}
				if ev.IdleWarning != nil {
					_ = sendTextJSON(map[string]any{
						"type":              "idle_warning",
						"remaining_seconds": *ev.IdleWarning,
					})
				}
				if ev.Exited {
					if ev.CloseReason == "idle_timeout" {
						_ = sendTextJSON(map[string]any{
							"type":    "error",
							"code":    "idle_timeout",
							"message": "Sesi terminal ditutup otomatis karena idle terlalu lama.",
						})
					}
					_ = sendTextJSON(map[string]any{
						"type":   "exit",
						"code":   ev.ExitCode,
						"reason": ev.CloseReason,
					})
					wsMu.Lock()
					_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
					wsMu.Unlock()
					return
				}
			}
		}
	}()

	// Pompa WebSocket stdin/control -> PTY
	go func() {
		defer stop()
		for {
			msgType, data, rerr := conn.ReadMessage()
			if rerr != nil {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(termPongWait))

			switch msgType {
			case websocket.BinaryMessage:
				if len(data) > 0 {
					if _, werr := sess.Write(data); werr != nil {
						return
					}
				}
			case websocket.TextMessage:
				var ctrl wsClientControlMsg
				if jerr := json.Unmarshal(data, &ctrl); jerr != nil {
					continue
				}
				switch ctrl.Type {
				case "resize":
					_ = sess.Resize(ctrl.Cols, ctrl.Rows)
				case "input":
					if ctrl.Data != "" {
						if _, werr := sess.Write([]byte(ctrl.Data)); werr != nil {
							return
						}
					}
				case "ping":
					_ = sendTextJSON(map[string]string{"type": "pong"})
				case "extend":
					sec := sess.ExtendIdle()
					_ = sendTextJSON(map[string]any{
						"type":                 "idle_extended",
						"idle_timeout_seconds": sec,
					})
				case "keepalive":
					sess.SetKeepAlive(ctrl.Enabled)
					_ = sendTextJSON(map[string]any{
						"type":    "keepalive",
						"enabled": sess.KeepAlive(),
					})
				case "close":
					_ = sess.CloseWithReason("user_closed")
					return
				}
			}
		}
	}()

	<-doneCh
}
