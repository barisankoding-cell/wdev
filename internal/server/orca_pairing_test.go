package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"wdev/internal/config"
	"wdev/internal/installer"
	"wdev/internal/store"
)

func TestValidateOrcaCustomAddress(t *testing.T) {
	valid := []struct {
		in   string
		want string
	}{
		{"wss://orca.rtzedu.uk", "wss://orca.rtzedu.uk"},
		{"HTTPS://orca.rtzedu.uk/", "https://orca.rtzedu.uk"},
		{"http://192.168.1.10:6768", "http://192.168.1.10:6768"},
		{"192.168.1.10", "192.168.1.10"},
		{"192.168.1.10:6768", "192.168.1.10:6768"},
		{"[::1]:6768", "[::1]:6768"},
		{"[::1]", "[::1]"},
	}
	for _, tc := range valid {
		got, err := validateOrcaCustomAddress(tc.in, "en")
		if err != nil {
			t.Fatalf("validateOrcaCustomAddress(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("validateOrcaCustomAddress(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	invalid := []string{
		"",
		"   ",
		"ftp://orca.rtzedu.uk",
		"wss://0.0.0.0",
		"wss://orca.rtzedu.uk/subpath",
		"wss://user:pass@orca.rtzedu.uk",
		"wss://orca.rtzedu.uk#frag",
		"192.168.1.10:99999",
		"orca.rtzedu.uk/path",
		"0.0.0.0",
		"has space.example.com",
	}
	for _, in := range invalid {
		if _, err := validateOrcaCustomAddress(in, "en"); err == nil {
			t.Fatalf("validateOrcaCustomAddress(%q) expected error, got nil", in)
		}
	}
}

func TestHandleWorkspaceOrcaPairingAndStatus(t *testing.T) {
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer db.Close()

	origLAN := detectOrcaLANIPFn
	origVPN := detectOrcaVPNIPFn
	origActive := isOrcaActiveForUserFn
	defer func() {
		detectOrcaLANIPFn = origLAN
		detectOrcaVPNIPFn = origVPN
		isOrcaActiveForUserFn = origActive
	}()

	lanIP := "192.168.10.20"
	vpnIP := "100.68.154.24"
	orcaActive := true

	detectOrcaLANIPFn = func() string { return lanIP }
	detectOrcaVPNIPFn = func() string { return vpnIP }
	isOrcaActiveForUserFn = func(context.Context, string, string) bool { return orcaActive }

	var (
		mu           sync.Mutex
		capturedArgs [][]string
	)
	restorePair := installer.SetOrcaPairExecFn(func(_ context.Context, _ string, args []string) ([]byte, error) {
		mu.Lock()
		capturedArgs = append(capturedArgs, slices.Clone(args))
		mu.Unlock()
		addr := ""
		isMobile := slices.Contains(args, "--mobile")
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--address" {
				addr = args[i+1]
			}
		}
		if addr == "reject.invalid" {
			return []byte(`{"ok":true,"result":{"available":false,"reason":"invalid_advertised_endpoint","guidance":"Use a valid endpoint."}}`), context.DeadlineExceeded
		}
		endpoint := "ws://" + vpnIP + ":6768"
		webBase := "http://" + vpnIP + ":6768"
		switch {
		case addr == "":
		case strings.HasPrefix(addr, "wss://"):
			endpoint = addr
			webBase = "https://" + strings.TrimPrefix(addr, "wss://")
		case strings.HasPrefix(addr, "https://"):
			endpoint = "wss://" + strings.TrimPrefix(addr, "https://")
			webBase = addr
		default:
			endpoint = "ws://" + addr + ":6768"
			webBase = "http://" + addr + ":6768"
		}
		scope := "runtime"
		webURL := webBase + "/web-index.html#pairing=test"
		connMode := ""
		if isMobile {
			scope = "mobile"
			webURL = ""
			connMode = "local-only"
		}
		res := map[string]any{
			"ok": true,
			"result": map[string]any{
				"available":      true,
				"scope":          scope,
				"pairingUrl":     "orca://pair?code=" + scope + "-" + endpoint,
				"endpoint":       endpoint,
				"webClientUrl":   webURL,
				"connectionMode": connMode,
			},
		}
		return json.Marshal(res)
	})
	defer restorePair()

	cfg := &config.Config{HTTPHost: "127.0.0.1", HTTPPort: "3200"}
	inst := installer.New(cfg, db)
	srv := &Server{
		cfg:       cfg,
		store:     db,
		installer: inst,
		mux:       http.NewServeMux(),
	}
	srv.mux.HandleFunc("/api/workspace/status", srv.handleWorkspaceStatus)
	srv.mux.HandleFunc("/api/workspace/orca-pairing", srv.handleWorkspaceOrcaPairing)

	// 1. Default GET returns auto mode with detected IPs
	reqGet := httptest.NewRequest(http.MethodGet, "/api/workspace/orca-pairing", nil)
	recGet := httptest.NewRecorder()
	srv.mux.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET /api/workspace/orca-pairing = %d: %s", recGet.Code, recGet.Body.String())
	}
	var st OrcaPairingState
	if err := json.Unmarshal(recGet.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal GET: %v", err)
	}
	if st.Mode != "auto" || st.LocalIP != lanIP || st.VPNIP != vpnIP {
		t.Fatalf("unexpected initial state: %+v", st)
	}

	// 2. Switch to local mode
	reqLocal := httptest.NewRequest(http.MethodPost, "/api/workspace/orca-pairing", strings.NewReader(`{"mode":"local"}`))
	recLocal := httptest.NewRecorder()
	srv.mux.ServeHTTP(recLocal, reqLocal)
	if recLocal.Code != http.StatusOK {
		t.Fatalf("POST local = %d: %s", recLocal.Code, recLocal.Body.String())
	}
	if err := json.Unmarshal(recLocal.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal POST local: %v", err)
	}
	if st.Mode != "local" || st.Endpoint != "ws://192.168.10.20:6768" {
		t.Fatalf("unexpected local state: %+v", st)
	}

	// 3. Switch to custom wss:// with rotate=true
	reqCustom := httptest.NewRequest(http.MethodPost, "/api/workspace/orca-pairing", strings.NewReader(`{"mode":"custom","custom_address":"wss://orca.rtzedu.uk","rotate":true}`))
	recCustom := httptest.NewRecorder()
	srv.mux.ServeHTTP(recCustom, reqCustom)
	if recCustom.Code != http.StatusOK {
		t.Fatalf("POST custom = %d: %s", recCustom.Code, recCustom.Body.String())
	}
	if err := json.Unmarshal(recCustom.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal POST custom: %v", err)
	}
	if st.Mode != "custom" || st.CustomAddress != "wss://orca.rtzedu.uk" || st.Endpoint != "wss://orca.rtzedu.uk" || st.WebClientURL != "https://orca.rtzedu.uk/web-index.html#pairing=test" {
		t.Fatalf("unexpected custom state: %+v", st)
	}
	if st.MobilePairingURL != "orca://pair?code=mobile-wss://orca.rtzedu.uk" || st.MobileEndpoint != "wss://orca.rtzedu.uk" || st.MobileConnectionMode != "local-only" {
		t.Fatalf("unexpected mobile pairing state: %+v", st)
	}
	mu.Lock()
	recentArgs := slices.Clone(capturedArgs[len(capturedArgs)-2:])
	mu.Unlock()
	for _, a := range recentArgs {
		if !slices.Contains(a, "--rotate") || !slices.Contains(a, "--address") || !slices.Contains(a, "wss://orca.rtzedu.uk") {
			t.Fatalf("expected --address wss://orca.rtzedu.uk and --rotate in %v", a)
		}
	}

	// 3a. rotate_scope=mobile rotates only mobile offer, not runtime offer
	mu.Lock()
	beforeCount := len(capturedArgs)
	mu.Unlock()
	reqMobRot := httptest.NewRequest(http.MethodPost, "/api/workspace/orca-pairing", strings.NewReader(`{"mode":"custom","custom_address":"wss://orca.rtzedu.uk","rotate":true,"rotate_scope":"mobile"}`))
	recMobRot := httptest.NewRecorder()
	srv.mux.ServeHTTP(recMobRot, reqMobRot)
	if recMobRot.Code != http.StatusOK {
		t.Fatalf("POST rotate_scope=mobile = %d: %s", recMobRot.Code, recMobRot.Body.String())
	}
	mu.Lock()
	mobRotCalls := slices.Clone(capturedArgs[beforeCount:])
	mu.Unlock()
	if len(mobRotCalls) != 2 {
		t.Fatalf("expected 2 parallel calls (runtime + mobile), got %d", len(mobRotCalls))
	}
	for _, call := range mobRotCalls {
		hasMobile := slices.Contains(call, "--mobile")
		hasRotate := slices.Contains(call, "--rotate")
		if hasMobile != hasRotate {
			t.Fatalf("expected --rotate only on --mobile call; got %v", call)
		}
	}

	// 3b. Subsequent GET /api/workspace/orca-pairing & status reflect saved custom mode + mobile URL
	reqGetCustom := httptest.NewRequest(http.MethodGet, "/api/workspace/orca-pairing", nil)
	recGetCustom := httptest.NewRecorder()
	srv.mux.ServeHTTP(recGetCustom, reqGetCustom)
	if recGetCustom.Code != http.StatusOK {
		t.Fatalf("GET after custom = %d: %s", recGetCustom.Code, recGetCustom.Body.String())
	}
	if err := json.Unmarshal(recGetCustom.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal GET custom: %v", err)
	}
	if st.Mode != "custom" || st.CustomAddress != "wss://orca.rtzedu.uk" || st.Endpoint != "wss://orca.rtzedu.uk" || st.MobilePairingURL == "" {
		t.Fatalf("GET after custom mismatch: %+v", st)
	}

	// 3c. Verify terminal QR rendering produces clean block lines without ANSI escapes
	qrLines := renderTerminalQRCode(st.MobilePairingURL)
	if len(qrLines) < 10 {
		t.Fatalf("expected at least 10 QR lines, got %d", len(qrLines))
	}
	for _, line := range qrLines {
		if strings.Contains(line, "\x1b") {
			t.Fatalf("terminal QR line must not contain ANSI escape codes: %q", line)
		}
	}

	// 4. Failed Orca pairing must return 400 and keep previous persisted settings
	reqReject := httptest.NewRequest(http.MethodPost, "/api/workspace/orca-pairing", strings.NewReader(`{"mode":"custom","custom_address":"reject.invalid"}`))
	recReject := httptest.NewRecorder()
	srv.mux.ServeHTTP(recReject, reqReject)
	if recReject.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when orca rejects endpoint, got %d: %s", recReject.Code, recReject.Body.String())
	}
	savedMode, savedCustom := srv.loadOrcaPairingPrefs()
	if savedMode != "custom" || savedCustom != "wss://orca.rtzedu.uk" {
		t.Fatalf("settings should not be overwritten on error; got mode=%q custom=%q", savedMode, savedCustom)
	}

	// 5. VPN mode fails clearly in Indonesian when VPN IP is missing
	vpnIP = ""
	reqNoVPN := httptest.NewRequest(http.MethodPost, "/api/workspace/orca-pairing?lang=id", strings.NewReader(`{"mode":"vpn"}`))
	recNoVPN := httptest.NewRecorder()
	srv.mux.ServeHTTP(recNoVPN, reqNoVPN)
	if recNoVPN.Code != http.StatusBadRequest || !strings.Contains(recNoVPN.Body.String(), "tidak terdeteksi") {
		t.Fatalf("expected 400 Indonesian VPN error, got %d: %s", recNoVPN.Code, recNoVPN.Body.String())
	}

	// 6. Inactive Orca still persists preference
	orcaActive = false
	reqInactive := httptest.NewRequest(http.MethodPost, "/api/workspace/orca-pairing", strings.NewReader(`{"mode":"local"}`))
	recInactive := httptest.NewRecorder()
	srv.mux.ServeHTTP(recInactive, reqInactive)
	if recInactive.Code != http.StatusOK {
		t.Fatalf("POST while inactive = %d: %s", recInactive.Code, recInactive.Body.String())
	}
	savedMode, _ = srv.loadOrcaPairingPrefs()
	if savedMode != "local" {
		t.Fatalf("expected mode='local' saved while inactive, got %q", savedMode)
	}
}
