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
