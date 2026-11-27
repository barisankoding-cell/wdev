package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"wdev/internal/auth"
	"wdev/internal/config"
	"wdev/internal/security"
	"wdev/internal/store"
)

func newSecurityTestServer(t *testing.T) (*Server, *store.Store, *security.Manager) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "server_sec.db"))
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}

	cfg := &config.Config{HTTPHost: "127.0.0.1", HTTPPort: "3200"}
	authMgr := auth.New(cfg, db)
	_ = authMgr.SetSecurityEntrance("off")
	secMgr := security.New(db)

	srv := &Server{
		cfg:     cfg,
		store:   db,
		authMgr: authMgr,
		secMgr:  secMgr,
		mux:     http.NewServeMux(),
	}
	srv.registerSecurityRoutes()
	return srv, db, secMgr
}

func TestSecurityAPIEndpoints(t *testing.T) {
	srv, _, _ := newSecurityTestServer(t)

	// 1. Manually block an IP
	blockPayload := map[string]any{
		"ip":               "203.0.113.88",
		"reason":           "Port scan test",
		"duration_minutes": 60,
	}
	body, _ := json.Marshal(blockPayload)
	req := httptest.NewRequest(http.MethodPost, "/api/security/block", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/security/block, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. GET /api/security/blocked
	req = httptest.NewRequest(http.MethodGet, "/api/security/blocked", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/security/blocked, got %d", rec.Code)
	}
	var blockedResp struct {
		BlockedIPs []store.BlockedIPRecord `json:"blocked_ips"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &blockedResp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(blockedResp.BlockedIPs) != 1 || blockedResp.BlockedIPs[0].IP != "203.0.113.88" {
		t.Fatalf("expected 1 blocked IP (203.0.113.88), got %+v", blockedResp.BlockedIPs)
	}

	// 3. GET /api/security/stats
	req = httptest.NewRequest(http.MethodGet, "/api/security/stats", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/security/stats, got %d", rec.Code)
	}
	var statsResp store.SecurityStatsRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &statsResp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if statsResp.TotalBlocked != 1 {
		t.Fatalf("expected TotalBlocked=1, got %d", statsResp.TotalBlocked)
	}

	// 4. GET /api/security/events (should be empty initially)
	req = httptest.NewRequest(http.MethodGet, "/api/security/events", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/security/events, got %d", rec.Code)
	}

	// 5. Unblock IP
	unblockPayload := map[string]any{"ip": "203.0.113.88"}
	bodyU, _ := json.Marshal(unblockPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/security/unblock", bytes.NewReader(bodyU))
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/security/unblock, got %d", rec.Code)
	}

	// Verify unblocked
	req = httptest.NewRequest(http.MethodGet, "/api/security/blocked", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &blockedResp)
	if len(blockedResp.BlockedIPs) != 0 {
		t.Fatalf("expected 0 blocked IPs, got %d", len(blockedResp.BlockedIPs))
	}

	// 6. GET /api/security/status & POST /api/security/config
	req = httptest.NewRequest(http.MethodGet, "/api/security/status", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/security/status, got %d", rec.Code)
	}
	var statusResp security.MonitorStatusReport
	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("invalid status json: %v", err)
	}
	if len(statusResp.Watchers) < 4 {
		t.Fatalf("expected watcher statuses in /api/security/status, got %d", len(statusResp.Watchers))
	}

	cfgPayload := security.DefaultPolicyConfig()
	cfgPayload.Whitelist = []string{"203.0.113.250"}
	cfgPayload.StrikeThreshold = 8
	bodyC, _ := json.Marshal(cfgPayload)
	req = httptest.NewRequest(http.MethodPost, "/api/security/config", bytes.NewReader(bodyC))
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for POST /api/security/config, got %d: %s", rec.Code, rec.Body.String())
	}
	if !srv.secMgr.IsWhitelisted("203.0.113.250") {
		t.Fatalf("expected 203.0.113.250 to be whitelisted after POST /api/security/config")
	}
}

func TestPanelWebScannerAndTerminalUnlockAttackDetection(t *testing.T) {
	srv, _, secMgr := newSecurityTestServer(t)
	secMgr.SetFirewallEnabled(false)
	_ = srv.authMgr.SetSecurityEntrance("secret-gate")
	_ = srv.authMgr.SetPassword("StrongPass#2026")

	handler := srv.wrapBlockedIP(srv.wrapSecurityEntrance(srv.wrapPanelScanner(srv.mux)))

	// 1. Scanner User-Agent triggers WEB_SCANNER
	reqUA := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	reqUA.RemoteAddr = "198.51.100.12:4000"
	reqUA.Header.Set("User-Agent", "sqlmap/1.8")
	recUA := httptest.NewRecorder()
	handler.ServeHTTP(recUA, reqUA)
	if recUA.Code != http.StatusNotFound {
		t.Fatalf("expected 404 decoy response for scanner UA, got %d", recUA.Code)
	}
	evScan, totalScan, _ := secMgr.ListEvents(10, 0, security.AttackWebScanner, "", "198.51.100.12")
	if totalScan != 1 || len(evScan) != 1 {
		t.Fatalf("expected 1 WEB_SCANNER event for scanner UA, got total=%d", totalScan)
	}

	// 2. Multi-segment Security Entrance probe (/admin/login) triggers SECURITY_ENTRANCE_PROBE,
	// whereas /favicon.ico does not
	reqFav := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	reqFav.RemoteAddr = "198.51.100.13:4001"
	handler.ServeHTTP(httptest.NewRecorder(), reqFav)
	if _, totalFav, _ := secMgr.ListEvents(10, 0, security.AttackSecurityEntrance, "", "198.51.100.13"); totalFav != 0 {
		t.Fatalf("expected /favicon.ico not to trigger SECURITY_ENTRANCE_PROBE")
	}

	reqProbe := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	reqProbe.RemoteAddr = "198.51.100.13:4002"
	handler.ServeHTTP(httptest.NewRecorder(), reqProbe)
	if _, totalProbe, _ := secMgr.ListEvents(10, 0, security.AttackSecurityEntrance, "", "198.51.100.13"); totalProbe != 1 {
		t.Fatalf("expected /admin/login to trigger SECURITY_ENTRANCE_PROBE, got %d", totalProbe)
	}

	// 3. Terminal unlock wrong password triggers PANEL_BRUTE_FORCE
	srv.registerTerminalRoutes()
	reqTerm := httptest.NewRequest(http.MethodPost, "/api/terminal/unlock", bytes.NewReader([]byte(`{"password":"wrong","user":"root"}`)))
	reqTerm.RemoteAddr = "198.51.100.14:4003"
	recTerm := httptest.NewRecorder()
	srv.mux.ServeHTTP(recTerm, reqTerm)
	if recTerm.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on wrong terminal unlock password, got %d", recTerm.Code)
	}
	evTerm, totalTerm, _ := secMgr.ListEvents(10, 0, security.AttackPanelBruteForce, "", "198.51.100.14")
	if totalTerm != 1 || evTerm[0].Target != "/api/terminal/unlock" {
		t.Fatalf("expected PANEL_BRUTE_FORCE on /api/terminal/unlock, got total=%d events=%+v", totalTerm, evTerm)
	}

	// 4. Burst 404s on panel triggers WEB_SCANNER
	srv.mux.HandleFunc("/api/", srv.handleAPINotFound)
	for i := 1; i <= 10; i++ {
		req404 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/probe-%d", i), nil)
		req404.RemoteAddr = "198.51.100.15:4010"
		srv.wrapPanelScanner(srv.mux).ServeHTTP(httptest.NewRecorder(), req404)
	}
	if _, total404, _ := secMgr.ListEvents(10, 0, security.AttackWebScanner, "", "198.51.100.15"); total404 != 1 {
		t.Fatalf("expected 1 WEB_SCANNER event after 10 panel 404s, got %d", total404)
	}
}

func TestHoneypotTrapAndBlockedIPMiddleware(t *testing.T) {
	srv, _, secMgr := newSecurityTestServer(t)

	// Wrap with blocked IP middleware and entrance check
	baseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	handler := srv.wrapBlockedIP(baseHandler)

	// Normal request passes
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.RemoteAddr = "198.51.100.77:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Honeypot detection
	if !isHoneypotTarget("/.env") {
		t.Fatalf("expected /.env to be honeypot target")
	}
	if !isHoneypotTarget("/wp-login.php") {
		t.Fatalf("expected /wp-login.php to be honeypot target")
	}
	if !isHoneypotTarget("/wp-admin/includes") {
		t.Fatalf("expected /wp-admin to be honeypot target")
	}
	if isHoneypotTarget("/api/status") {
		t.Fatalf("did not expect /api/status to be honeypot target")
	}

	// Block the attacker IP
	_ = secMgr.BlockIP(req.Context(), "198.51.100.77", "Hit honeypot", 0)

	// Request from blocked IP should receive 403 Forbidden
	recBlocked := httptest.NewRecorder()
	handler.ServeHTTP(recBlocked, req)
	if recBlocked.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for blocked IP, got %d", recBlocked.Code)
	}
}

func TestDemoCyberAttackSimulationWithSafeLocalhost(t *testing.T) {
	srv, _, secMgr := newSecurityTestServer(t)
	handler := srv.wrapBlockedIP(srv.wrapSecurityEntrance(srv.mux))

	// Endpoint normal untuk pengujian koneksi
	srv.mux.HandleFunc("/api/hello", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","message":"Welcome"}`))
	})

	fmt.Println("\n================================================================================")
	fmt.Println("🚀 MEMULAI SIMULASI UJI SERANGAN SIBER & VALIDASI KEAMANAN LOCALHOST")
	fmt.Println("================================================================================")

	// Skenario 1: Penyerang Luar A (IP: 203.0.113.88) memindai file rahasia /.env (Honeypot Trap)
	fmt.Println("\n[1] Simulasi: Penyerang Luar (203.0.113.88) melakukan scanning exploit ke /.env")
	reqHoneypot := httptest.NewRequest(http.MethodGet, "/.env", nil)
	reqHoneypot.RemoteAddr = "203.0.113.88:45123"
	reqHoneypot.Header.Set("User-Agent", "Mozilla/5.0 zgrab/0.x scanner")
	recHoneypot := httptest.NewRecorder()
	handler.ServeHTTP(recHoneypot, reqHoneypot)
	fmt.Printf("    -> Respons ke Penyerang: HTTP %d (Decoy disembunyikan sebagai 404 Not Found)\n", recHoneypot.Code)

	// Cek apakah penyerang luar langsung diblokir
	isBlocked, blockRec, _ := secMgr.IsIPBlocked("203.0.113.88")
	reasonStr := ""
	if blockRec != nil {
		reasonStr = blockRec.Reason
	}
	fmt.Printf("    -> Status Auto-Ban 203.0.113.88 : %v (Alasan: %s)\n", isBlocked, reasonStr)

	// Uji bahwa penyerang yang telah diblokir tidak bisa mengakses endpoint apapun
	reqSubsequent := httptest.NewRequest(http.MethodGet, "/api/hello", nil)
	reqSubsequent.RemoteAddr = "203.0.113.88:45124"
	recSubsequent := httptest.NewRecorder()
	handler.ServeHTTP(recSubsequent, reqSubsequent)
	fmt.Printf("    -> Percobaan serangan kedua oleh 203.0.113.88: HTTP %d (Langsung Dihadang Firewall)\n", recSubsequent.Code)

	// Skenario 2: Penyerang Luar B (IP: 198.51.100.99) mencoba Brute Force Password
	fmt.Println("\n[2] Simulasi: Penyerang Luar (198.51.100.99) meluncurkan Brute Force Login")
	for i := 1; i <= 2; i++ {
		ev := security.Event{
			SourceIP:   "198.51.100.99",
			AttackType: security.AttackPanelBruteForce,
			Severity:   security.SeverityHigh,
			Target:     "/api/auth/login",
			Details:    fmt.Sprintf("Failed password attempt #%d", i),
		}
		b, _ := secMgr.RecordAttack(reqHoneypot.Context(), ev)
		fmt.Printf("    -> Strike %d: Terdeteksi PANEL_BRUTE_FORCE [HIGH] | Diblokir: %v\n", i, b)
	}

	// Skenario 3: UJI KEAMANAN LOCALHOST (127.0.0.1)
	fmt.Println("\n[3] Uji Keamanan: Memastikan LOCALHOST (127.0.0.1) Selalu Aman dan Tidak Pernah Diblokir")
	reqLocal := httptest.NewRequest(http.MethodGet, "/.env", nil)
	reqLocal.RemoteAddr = "127.0.0.1:58210"
	recLocal := httptest.NewRecorder()
	handler.ServeHTTP(recLocal, reqLocal)
	fmt.Printf("    -> Localhost mencoba akses /.env: HTTP %d (Tercatat sebagai LAN audit)\n", recLocal.Code)

	// Verifikasi status blokir localhost
	isLocalBlocked, _, _ := secMgr.IsIPBlocked("127.0.0.1")
	fmt.Printf("    -> Verifikasi apakah 127.0.0.1 diblokir: %v (HARUS FALSE / WHITELISTED)\n", isLocalBlocked)
	if isLocalBlocked {
		t.Fatalf("FATAL: Localhost 127.0.0.1 tidak boleh diblokir!")
	}

	// Localhost mencoba akses endpoint resmi: HARUS HTTP 200 OK
	reqLocalValid := httptest.NewRequest(http.MethodGet, "/api/hello", nil)
	reqLocalValid.RemoteAddr = "127.0.0.1:58211"
	recLocalValid := httptest.NewRecorder()
	handler.ServeHTTP(recLocalValid, reqLocalValid)
	fmt.Printf("    -> Akses layanan resmi oleh Localhost: HTTP %d (Koneksi Berjalan Normal & Aman)\n", recLocalValid.Code)
	if recLocalValid.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for localhost, got %d", recLocalValid.Code)
	}

	// Tampilkan Tabel Log Serangan dari SQLite
	fmt.Println("\n================================================================================")
	fmt.Println("📋 LOG RIWAYAT SERANGAN TERDETEKSI (Tabel: security_events)")
	fmt.Println("================================================================================")
	events, total, _ := secMgr.ListEvents(50, 0, "", "", "")
	fmt.Printf("Total Catatan Serangan: %d\n\n", total)
	fmt.Printf("%-4s | %-15s | %-6s | %-24s | %-8s | %-16s | %-12s\n", "ID", "IP ASAL", "NEGARA", "JENIS SERANGAN", "SEVERITY", "TARGET", "MITIGASI")
	fmt.Println("-----+-----------------+--------+--------------------------+----------+------------------+-------------")
	for _, e := range events {
		fmt.Printf("%-4d | %-15s | %-6s | %-24s | %-8s | %-16s | %-12s\n", e.ID, e.SourceIP, e.CountryCode, e.AttackType, e.Severity, e.Target, e.ActionTaken)
	}

	// Tampilkan Tabel IP Terblokir dari SQLite
	fmt.Println("\n================================================================================")
	fmt.Println("🚫 DAFTAR IP YANG DIBLOKIR / AUTO-BANNED (Tabel: blocked_ips)")
	fmt.Println("================================================================================")
	blockedList, _ := secMgr.ListBlockedIPs()
	fmt.Printf("%-18s | %-22s | %-35s\n", "IP TERBLOKIR", "WAKTU BLOKIR", "ALASAN PEMBLOKIRAN")
	fmt.Println("-------------------+------------------------+------------------------------------")
	for _, b := range blockedList {
		fmt.Printf("%-18s | %-22s | %-35s\n", b.IP, b.BlockedAt.Format("2006-01-02 15:04:05"), b.Reason)
	}
	fmt.Println("================================================================================")
}

func TestSecurityConfigRedactionAndWebhookTestEndpoint(t *testing.T) {
	srv, _, secMgr := newSecurityTestServer(t)

	var webhookHits int
	var lastTitle string
	hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		webhookHits++
		if ttl, ok := body["title"].(string); ok {
			lastTitle = ttl
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer hookSrv.Close()

	// 1. Save policy with Cloudflare token & webhook URL
	cfg := security.DefaultPolicyConfig()
	cfg.AlertWebhookURL = hookSrv.URL + "/notify"
	cfg.AlertMinSeverity = security.SeverityCritical
	cfg.CFBlockSyncEnabled = true
	cfg.CFZoneID = "zone_12345"
	cfg.CFAPIToken = "cf_secret_token_98765"
	cfg.RetentionDays = 45

	rawCfg, _ := json.Marshal(cfg)
	reqPost := httptest.NewRequest(http.MethodPost, "/api/security/config", bytes.NewReader(rawCfg))
	recPost := httptest.NewRecorder()
	srv.mux.ServeHTTP(recPost, reqPost)
	if recPost.Code != http.StatusOK {
		t.Fatalf("POST /api/security/config failed (%d): %s", recPost.Code, recPost.Body.String())
	}
	if bytes.Contains(recPost.Body.Bytes(), []byte("cf_secret_token_98765")) {
		t.Fatalf("POST /api/security/config response leaked cf_api_token: %s", recPost.Body.String())
	}

	var postResp struct {
		Status string                `json:"status"`
		Policy security.PolicyConfig `json:"policy"`
	}
	_ = json.Unmarshal(recPost.Body.Bytes(), &postResp)
	if !postResp.Policy.CFTokenConfigured || postResp.Policy.CFAPIToken != "" {
		t.Fatalf("expected redacted token with cf_token_configured=true, got %+v", postResp.Policy)
	}

	// 2. GET /api/security/config must keep token redacted
	reqGet := httptest.NewRequest(http.MethodGet, "/api/security/config", nil)
	recGet := httptest.NewRecorder()
	srv.mux.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET /api/security/config failed: %d", recGet.Code)
	}
	if bytes.Contains(recGet.Body.Bytes(), []byte("cf_secret_token_98765")) {
		t.Fatalf("GET /api/security/config leaked cf_api_token")
	}

	// 3. Re-saving policy without cf_api_token preserves existing secret internally
	cfgPreserve := postResp.Policy
	cfgPreserve.StrikeThreshold = 8
	rawPreserve, _ := json.Marshal(cfgPreserve)
	reqPres := httptest.NewRequest(http.MethodPost, "/api/security/config", bytes.NewReader(rawPreserve))
	recPres := httptest.NewRecorder()
	srv.mux.ServeHTTP(recPres, reqPres)
	if recPres.Code != http.StatusOK {
		t.Fatalf("preserve POST /api/security/config failed (%d): %s", recPres.Code, recPres.Body.String())
	}
	if secMgr.GetPolicy().CFAPIToken != "cf_secret_token_98765" {
		t.Fatalf("expected stored CFAPIToken to be preserved, got %q", secMgr.GetPolicy().CFAPIToken)
	}

	// 4. Test Webhook endpoint
	reqHook := httptest.NewRequest(http.MethodPost, "/api/security/webhook/test", bytes.NewReader([]byte(`{}`)))
	recHook := httptest.NewRecorder()
	srv.mux.ServeHTTP(recHook, reqHook)
	if recHook.Code != http.StatusOK {
		t.Fatalf("POST /api/security/webhook/test failed (%d): %s", recHook.Code, recHook.Body.String())
	}
	if webhookHits != 1 || lastTitle == "" {
		t.Fatalf("expected webhook server to receive 1 test payload with title, got hits=%d title=%q", webhookHits, lastTitle)
	}
}
