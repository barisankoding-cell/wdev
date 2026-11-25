package cloudflare

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wdev/internal/config"
	"wdev/internal/store"

	"gopkg.in/yaml.v3"
)

func newTestCFManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "cf.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := &config.Config{WdevEtc: dir}
	mgr := New(cfg, db)
	mgr.systemdTemplatePath = filepath.Join(dir, "systemd", "cloudflared@.service")
	mgr.legacyConfigPath = filepath.Join(dir, "legacy-cloudflared.yml")
	mgr.lookPathFn = func(name string) (string, error) {
		if name == "cloudflared" {
			return "/usr/local/bin/cloudflared", nil
		}
		return "", fmt.Errorf("not found: %s", name)
	}
	mgr.isServiceActiveFn = func(unit string) bool { return false }
	mgr.isServiceEnabledFn = func(unit string) bool { return false }
	mgr.anyTunnelProcFn = func() bool { return false }
	mgr.runPrivilegedFn = func(bin string, args ...string) (string, error) { return "OK", nil }
	return mgr, dir
}

func makeTestToken(accountTag, tunnelID, secret string) string {
	raw := fmt.Sprintf(`{"a":%q,"t":%q,"s":%q}`, accountTag, tunnelID, secret)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func TestParseTunnelTokenAndRegisterMultiTokenProfiles(t *testing.T) {
	mgr, dir := newTestCFManager(t)
	tokA := makeTestToken("acct-aaa", "11111111-1111-1111-1111-111111111111", "sec-a")
	tokB := makeTestToken("acct-bbb", "22222222-2222-2222-2222-222222222222", "sec-b")

	// Mendukung salinan perintah lengkap cloudflared service install
	parsed, err := ParseTunnelToken("sudo cloudflared service install " + tokA)
	if err != nil {
		t.Fatalf("ParseTunnelToken: %v", err)
	}
	if parsed.TunnelID != "11111111-1111-1111-1111-111111111111" || parsed.AccountTag != "acct-aaa" {
		t.Fatalf("unexpected parsed token: %+v", parsed)
	}

	if _, err := ParseTunnelToken("not-a-valid-token"); err == nil {
		t.Fatal("expected error for invalid base64 JSON token")
	}

	autoFalse := false
	profA, err := mgr.RegisterTokenProfile(TokenProfileOptions{
		Name:      "Client-A",
		Token:     "cloudflared service install " + tokA,
		AutoStart: &autoFalse,
	})
	if err != nil {
		t.Fatalf("RegisterTokenProfile A: %v", err)
	}
	if profA.Name != "client-a" || profA.AuthMode != "token" || !profA.HasToken {
		t.Fatalf("unexpected profile A: %+v", profA)
	}

	if _, err := mgr.RegisterTokenProfile(TokenProfileOptions{
		Name:      "office",
		Token:     tokB,
		AutoStart: &autoFalse,
	}); err != nil {
		t.Fatalf("RegisterTokenProfile office: %v", err)
	}

	// Token harus tersimpan dengan izin 0600 dan env file mengarah ke --token-file
	tokBytes, err := os.ReadFile(filepath.Join(dir, "cloudflare", "tokens", "client-a.token"))
	if err != nil || strings.TrimSpace(string(tokBytes)) != tokA {
		t.Fatalf("token file mismatch: %v, content=%q", err, string(tokBytes))
	}
	envBytes, err := os.ReadFile(filepath.Join(dir, "cloudflare", "profiles", "client-a.env"))
	if err != nil || !strings.Contains(string(envBytes), "--token-file") {
		t.Fatalf("env file unexpected: %v, %q", err, string(envBytes))
	}

	// Token profile harus menolak Expose YAML lokal dengan pesan jelas
	if err := mgr.Expose("myapp.test", "app.example.com", "client-a"); err == nil {
		t.Fatal("expected error when exposing local YAML route onto token-mode profile")
	}
}

func TestCertMultiProfileExposeUnexposeAndDelete(t *testing.T) {
	mgr, dir := newTestCFManager(t)

	var recordedCmds [][]string
	mgr.runCmdFn = func(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
		recordedCmds = append(recordedCmds, append([]string{name}, args...))
		// Simulasikan `cloudflared tunnel --origincert ... create --cred-file <path> <name>`
		for i := 0; i < len(args)-1; i++ {
			if args[i] == "create" {
				credPath := ""
				for j := 0; j < len(args)-1; j++ {
					if args[j] == "--cred-file" {
						credPath = args[j+1]
						break
					}
				}
				if credPath != "" {
					credJSON := fmt.Sprintf(`{"AccountTag":"acct-cert-1","TunnelSecret":"c2Vj","TunnelID":"aaaaaaaa-aaaa-aaaa-aaaa-%012d"}`, len(recordedCmds))
					if err := os.WriteFile(credPath, []byte(credJSON), 0600); err != nil {
						return nil, err
					}
				}
				return []byte("Created tunnel"), nil
			}
		}
		return []byte("OK"), nil
	}

	sampleCert := "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n-----BEGIN ARGO TUNNEL TOKEN-----\nabc\n-----END ARGO TUNNEL TOKEN-----\n"
	autoFalse := false
	p1, err := mgr.RegisterCertProfile(CertProfileOptions{
		Name:       "akun1",
		CertPEM:    sampleCert,
		TunnelName: "tunnel-akun1",
		AutoStart:  &autoFalse,
	})
	if err != nil {
		t.Fatalf("RegisterCertProfile akun1: %v", err)
	}
	if p1.AuthMode != "cert" || !p1.HasCert || !p1.HasCredentials || !strings.HasPrefix(p1.TunnelID, "aaaaaaaa-") {
		t.Fatalf("unexpected cert profile p1: %+v", p1)
	}

	// Expose pada profil akun1 harus menulis routes/akun1.yml beserta OriginRequest httpHostHeader
	res, err := mgr.ExposeWithOptions(ExposeOptions{
		LocalDomain: "toko.test",
		PublicHost:  "toko.erposoft.my.id",
		Profile:     "akun1",
	})
	if err != nil {
		t.Fatalf("ExposeWithOptions: %v", err)
	}
	if res.Profile != "akun1" {
		t.Fatalf("ExposeResult.Profile = %q, want akun1", res.Profile)
	}

	// Verifikasi GetStatus() mempertahankan profile = "akun1" (tidak hardcoded "default")
	st := mgr.GetStatus()
	if len(st.Exposures) != 1 || st.Exposures[0].Profile != "akun1" || st.Exposures[0].HTTPHostHeader != "toko.test" {
		t.Fatalf("GetStatus().Exposures unexpected: %+v", st.Exposures)
	}
	if len(st.ProfileDetails) != 1 || st.ProfileDetails[0].RouteCount != 1 {
		t.Fatalf("GetStatus().ProfileDetails route count unexpected: %+v", st.ProfileDetails)
	}

	// Verifikasi routes/akun1.yml memuat tunnel ID, credentials-file, metrics, dan httpHostHeader
	ymlBytes, err := os.ReadFile(filepath.Join(dir, "cloudflare", "routes", "akun1.yml"))
	if err != nil {
		t.Fatalf("read akun1.yml: %v", err)
	}
	var parsedCfg CloudflaredConfig
	if err := yaml.Unmarshal(ymlBytes, &parsedCfg); err != nil {
		t.Fatalf("unmarshal akun1.yml: %v", err)
	}
	if parsedCfg.TunnelID != p1.TunnelID || parsedCfg.Metrics != "127.0.0.1:0" {
		t.Fatalf("unexpected tunnel config header: %+v", parsedCfg)
	}
	if len(parsedCfg.Ingress) != 2 || parsedCfg.Ingress[0].OriginRequest == nil || parsedCfg.Ingress[0].OriginRequest.HTTPHostHeader != "toko.test" {
		t.Fatalf("unexpected ingress rules: %+v", parsedCfg.Ingress)
	}

	// Verifikasi perintah DNS memakai --origincert akun1.cert
	dnsCmdFound := false
	for _, cmd := range recordedCmds {
		joined := strings.Join(cmd, " ")
		if strings.Contains(joined, "route dns") && strings.Contains(joined, "akun1.cert") && strings.Contains(joined, "toko.erposoft.my.id") {
			dnsCmdFound = true
			break
		}
	}
	if !dnsCmdFound {
		t.Fatalf("expected cloudflared route dns command with --origincert akun1.cert, got %v", recordedCmds)
	}

	// Unexpose harus membersihkan routes/akun1.yml sekaligus exposures/ dan DB
	if err := mgr.Unexpose("toko.test"); err != nil {
		t.Fatalf("Unexpose: %v", err)
	}
	if stAfter := mgr.GetStatus(); len(stAfter.Exposures) != 0 {
		t.Fatalf("expected 0 exposures after unexpose, got %+v", stAfter.Exposures)
	}
	ymlAfter, _ := os.ReadFile(filepath.Join(dir, "cloudflare", "routes", "akun1.yml"))
	var cfgAfter CloudflaredConfig
	_ = yaml.Unmarshal(ymlAfter, &cfgAfter)
	if len(cfgAfter.Ingress) != 1 || cfgAfter.Ingress[0].Service != "http_status:404" {
		t.Fatalf("expected only catch-all 404 after unexpose, got %+v", cfgAfter.Ingress)
	}

	// ControlProfile & DeleteProfile
	if err := mgr.ControlProfile("akun1", "restart"); err != nil {
		t.Fatalf("ControlProfile restart: %v", err)
	}
	if err := mgr.DeleteProfile("akun1"); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	if stDel := mgr.GetStatus(); len(stDel.Profiles) != 0 {
		t.Fatalf("expected 0 profiles after DeleteProfile, got %+v", stDel.Profiles)
	}
}

func TestBrowserLoginSessionIsolation(t *testing.T) {
	mgr, dir := newTestCFManager(t)
	mgr.runCmdFn = func(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
		for j := 0; j < len(args)-1; j++ {
			if args[j] == "--cred-file" {
				credJSON := `{"AccountTag":"acct-oauth","TunnelSecret":"c2Vj","TunnelID":"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"}`
				_ = os.WriteFile(args[j+1], []byte(credJSON), 0600)
			}
		}
		return []byte("OK"), nil
	}

	mgr.startLoginCmdFn = func(ctx context.Context, homeDir, binPath string) (io.ReadCloser, func() error, error) {
		pr, pw := io.Pipe()
		done := make(chan error, 1)
		go func() {
			_, _ = fmt.Fprintln(pw, "A browser window should have opened at the following URL:")
			_, _ = fmt.Fprintln(pw, "https://dash.cloudflare.com/argotunnel?aud=&callback=https%3A%2F%2Flogin.cloudflareaccess.org%2Fabc")
			// Simulasikan cert.pem ditulis ke $HOME/.cloudflared/cert.pem setelah callback
			cfDir := filepath.Join(homeDir, ".cloudflared")
			_ = os.MkdirAll(cfDir, 0700)
			certBody := "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n-----BEGIN ARGO TUNNEL TOKEN-----\ntok\n-----END ARGO TUNNEL TOKEN-----\n"
			_ = os.WriteFile(filepath.Join(cfDir, "cert.pem"), []byte(certBody), 0600)
			time.Sleep(40 * time.Millisecond)
			_ = pw.Close()
			done <- nil
		}()
		return pr, func() error { return <-done }, nil
	}

	// Skip systemctl execution on autoStart by mocking systemctl failure? Instead register with test check:
	// On non-root Linux dev env, systemctl enable will fail with ErrSudoRequired if not mocked; override runCmdFn only handles root or test env!
	// Temporarily mock EnsureSystemdTemplate check by using a custom test or check login flow until RegisterCertProfile:
	sess, err := mgr.StartBrowserLogin("oauth-acct", "oauth-tun")
	if err != nil {
		t.Fatalf("StartBrowserLogin: %v", err)
	}
	if !strings.HasPrefix(sess.AuthURL, "https://dash.cloudflare.com/argotunnel") {
		t.Fatalf("expected AuthURL, got %+v", sess)
	}

	// Poll hingga selesai atau gagal jika systemctl membutuhkan root (di non-root env, cert.pem tetap tersimpan sebelum enable)
	deadline := time.Now().Add(2 * time.Second)
	var latest *LoginSession
	for time.Now().Before(deadline) {
		latest, _ = mgr.GetLoginSession(sess.ID)
		if latest != nil && (latest.Status == "completed" || latest.Status == "failed") {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if latest == nil || latest.Status != "completed" || latest.Result == nil {
		t.Fatalf("unexpected final login login state: %+v", latest)
	}
	if _, err := os.Stat(filepath.Join(dir, "cloudflare", "accounts", "oauth-acct.cert")); err != nil {
		t.Fatalf("expected oauth-acct.cert to be saved: %v", err)
	}
}

func TestAPITokenZonesRemoteExposeUnexposeAndDelete(t *testing.T) {
	mgr, dir := newTestCFManager(t)
	tok := makeTestToken("acct-aaa", "11111111-1111-1111-1111-111111111111", "sec-a")

	var remoteIngress []map[string]any
	remoteIngress = []map[string]any{
		{"hostname": "ssh.other.com", "service": "ssh://127.0.0.1:22"},
		{"service": "http_status:404"},
	}
	dnsRecords := make(map[string]map[string]any)
	var tunnelDeleted, connCleaned bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer cf-api-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_, _ = w.Write([]byte(`{
				"success": true,
				"result": [
					{"id":"zone-other","name":"lain.org","status":"active","account":{"id":"acct-bbb","name":"Other"}},
					{"id":"zone-erpo","name":"erposoft.my.id","status":"active","account":{"id":"acct-aaa","name":"Main"}}
				],
				"result_info": {"page":1,"per_page":50,"total_pages":1,"count":2,"total_count":2}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/acct-aaa/cfd_tunnel/11111111-1111-1111-1111-111111111111/configurations":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"result": map[string]any{
					"config": map[string]any{
						"warp-routing": map[string]any{"enabled": true},
						"ingress":      remoteIngress,
					},
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/accounts/acct-aaa/cfd_tunnel/11111111-1111-1111-1111-111111111111/configurations":
			var body struct {
				Config struct {
					WarpRouting map[string]any   `json:"warp-routing"`
					Ingress     []map[string]any `json:"ingress"`
				} `json:"config"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode PUT config: %v", err)
			}
			if body.Config.WarpRouting["enabled"] != true {
				t.Fatalf("expected warp-routing preserved, got %+v", body.Config.WarpRouting)
			}
			remoteIngress = body.Config.Ingress
			_, _ = w.Write([]byte(`{"success":true,"result":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/zones/zone-erpo/dns_records":
			nameFilter := r.URL.Query().Get("name")
			var list []map[string]any
			for _, rec := range dnsRecords {
				if nameFilter == "" || rec["name"] == nameFilter {
					list = append(list, rec)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": list})
		case r.Method == http.MethodPost && r.URL.Path == "/zones/zone-erpo/dns_records":
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			payload["id"] = "rec-1"
			dnsRecords["rec-1"] = payload
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": payload})
		case r.Method == http.MethodDelete && r.URL.Path == "/zones/zone-erpo/dns_records/rec-1":
			delete(dnsRecords, "rec-1")
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"rec-1"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/accounts/acct-aaa/cfd_tunnel/11111111-1111-1111-1111-111111111111/connections":
			connCleaned = true
			_, _ = w.Write([]byte(`{"success":true,"result":null}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/accounts/acct-aaa/cfd_tunnel/11111111-1111-1111-1111-111111111111":
			tunnelDeleted = true
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"11111111-1111-1111-1111-111111111111"}}`))
		default:
			t.Fatalf("unexpected Cloudflare API call: %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()
	mgr.SetAPIBaseURL(srv.URL)

	autoFalse := false
	if _, err := mgr.RegisterTokenProfile(TokenProfileOptions{
		Name:      "kantor",
		Token:     tok,
		AutoStart: &autoFalse,
	}); err != nil {
		t.Fatalf("RegisterTokenProfile: %v", err)
	}

	updated, err := mgr.SetProfileAPIToken("kantor", "Bearer cf-api-secret")
	if err != nil {
		t.Fatalf("SetProfileAPIToken: %v", err)
	}
	if !updated.HasAPIToken {
		t.Fatalf("expected HasAPIToken=true, got %+v", updated)
	}

	zones, err := mgr.ListProfileZones("kantor")
	if err != nil {
		t.Fatalf("ListProfileZones: %v", err)
	}
	if len(zones) != 2 || zones[0].Name != "erposoft.my.id" {
		t.Fatalf("expected acct-aaa zone sorted first, got %+v", zones)
	}

	// Expose pada mode token + API Token harus berhasil update ingress remote & DNS CNAME
	expRes, err := mgr.ExposeWithOptions(ExposeOptions{
		LocalDomain: "toko.test",
		PublicHost:  "toko.erposoft.my.id",
		Profile:     "kantor",
	})
	if err != nil {
		t.Fatalf("ExposeWithOptions token+API: %v", err)
	}
	if expRes.DNSWarning != "" {
		t.Fatalf("unexpected DNSWarning: %s", expRes.DNSWarning)
	}
	if len(remoteIngress) != 3 || remoteIngress[1]["hostname"] != "toko.erposoft.my.id" {
		t.Fatalf("unexpected remoteIngress after expose: %+v", remoteIngress)
	}
	if rec, ok := dnsRecords["rec-1"]; !ok || rec["content"] != "11111111-1111-1111-1111-111111111111.cfargotunnel.com" {
		t.Fatalf("expected DNS CNAME created, got %+v", dnsRecords)
	}

	// Unexpose harus membersihkan remote ingress & DNS CNAME
	unRes, err := mgr.UnexposeWithResult("toko.test", "kantor")
	if err != nil {
		t.Fatalf("UnexposeWithResult: %v", err)
	}
	if !unRes.DNSDeleted || unRes.DNSWarning != "" {
		t.Fatalf("expected DNSDeleted=true without warning, got %+v", unRes)
	}
	if len(remoteIngress) != 2 || len(dnsRecords) != 0 {
		t.Fatalf("expected remoteIngress=2 and dnsRecords=0, got %+v / %+v", remoteIngress, dnsRecords)
	}

	// Re-expose lalu DeleteProfileWithOptions(DeleteRemote=true, DeleteDNS=true)
	if _, err := mgr.ExposeWithOptions(ExposeOptions{
		LocalDomain: "toko.test",
		PublicHost:  "toko.erposoft.my.id",
		Profile:     "kantor",
	}); err != nil {
		t.Fatalf("re-expose: %v", err)
	}
	delRes, err := mgr.DeleteProfileWithOptions("kantor", DeleteProfileOptions{
		DeleteRemote: true,
		DeleteDNS:    true,
	})
	if err != nil {
		t.Fatalf("DeleteProfileWithOptions: %v", err)
	}
	if !delRes.RemoteDeleted || delRes.DNSDeletedCount != 1 || !connCleaned || !tunnelDeleted {
		t.Fatalf("unexpected delete result: %+v (conn=%v, del=%v)", delRes, connCleaned, tunnelDeleted)
	}
	if _, err := os.Stat(filepath.Join(dir, "cloudflare", "tokens", "kantor.apitoken")); !os.IsNotExist(err) {
		t.Fatalf("expected .apitoken removed, got err=%v", err)
	}
}

func TestCertProfileDeleteRemoteViaCLI(t *testing.T) {
	mgr, dir := newTestCFManager(t)

	var recorded [][]string
	failDelete := true
	mgr.runCmdFn = func(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
		recorded = append(recorded, append([]string{name}, args...))
		for i, a := range args {
			if a == "--cred-file" && i+1 < len(args) {
				_ = os.WriteFile(args[i+1], []byte(`{"AccountTag":"acct-c","TunnelSecret":"c2Vj","TunnelID":"cccccccc-cccc-cccc-cccc-cccccccccccc"}`), 0600)
			}
			if a == "delete" && failDelete {
				return []byte("simulated remote API error"), fmt.Errorf("exit 1")
			}
		}
		return []byte("OK"), nil
	}

	sampleCert := "-----BEGIN ARGO TUNNEL TOKEN-----\nabc\n-----END ARGO TUNNEL TOKEN-----\n"
	autoFalse := false
	if _, err := mgr.RegisterCertProfile(CertProfileOptions{
		Name:       "lokal-del",
		CertPEM:    sampleCert,
		TunnelName: "tun-lokal-del",
		AutoStart:  &autoFalse,
	}); err != nil {
		t.Fatalf("RegisterCertProfile: %v", err)
	}

	// Jika delete remote gagal, file cert lokal tidak boleh terhapus
	if _, err := mgr.DeleteProfileWithOptions("lokal-del", DeleteProfileOptions{DeleteRemote: true}); err == nil {
		t.Fatal("expected error when remote delete fails")
	}
	if _, err := os.Stat(filepath.Join(dir, "cloudflare", "accounts", "lokal-del.cert")); err != nil {
		t.Fatalf("expected local cert preserved on remote delete error: %v", err)
	}

	// Setelah delete remote berhasil, Tunnel di server dihapus dan file lokal dibersihkan
	failDelete = false
	res, err := mgr.DeleteProfileWithOptions("lokal-del", DeleteProfileOptions{DeleteRemote: true})
	if err != nil {
		t.Fatalf("DeleteProfileWithOptions retry: %v", err)
	}
	if !res.RemoteDeleted {
		t.Fatalf("expected RemoteDeleted=true, got %+v", res)
	}
	foundDeleteCmd := false
	for _, cmd := range recorded {
		joined := strings.Join(cmd, " ")
		if strings.Contains(joined, "delete -f cccccccc-cccc-cccc-cccc-cccccccccccc") && strings.Contains(joined, "lokal-del.cert") {
			foundDeleteCmd = true
			break
		}
	}
	if !foundDeleteCmd {
		t.Fatalf("expected cloudflared delete -f command with --origincert, got %v", recorded)
	}
	if _, err := os.Stat(filepath.Join(dir, "cloudflare", "accounts", "lokal-del.cert")); !os.IsNotExist(err) {
		t.Fatalf("expected local cert deleted after success, err=%v", err)
	}
}

func TestCertExposePrefersAPIDNSOverCLIRouteDNS(t *testing.T) {
	mgr, _ := newTestCFManager(t)
	cliRouteDNSCalled := false
	mgr.runCmdFn = func(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "create") {
			for i, a := range args {
				if a == "--cred-file" && i+1 < len(args) {
					_ = os.WriteFile(args[i+1], []byte(`{"AccountTag":"acct-z","TunnelSecret":"c2Vj","TunnelID":"dddddddd-dddd-dddd-dddd-dddddddddddd"}`), 0600)
				}
			}
			return []byte("OK"), nil
		}
		if strings.Contains(joined, "route dns") {
			// Simulasikan bug cloudflared CLI yang sukses (exit 0) di zone salah saat cert.pem berbeda domain
			cliRouteDNSCalled = true
			return []byte("Added CNAME apps.softcuy.uk.domainlama.com"), nil
		}
		return []byte("OK"), nil
	}

	allowDNSCreate := true
	var createdDNS bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"z-softcuy","name":"softcuy.uk","status":"active","account":{"id":"acct-z","name":"Main"}}],"result_info":{"page":1,"per_page":50,"total_pages":1,"count":1,"total_count":1}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/zones/z-softcuy/dns_records":
			_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/zones/z-softcuy/dns_records":
			if !allowDNSCreate {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`))
				return
			}
			createdDNS = true
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"rec-soft"}}`))
		}
	}))
	defer srv.Close()
	mgr.SetAPIBaseURL(srv.URL)

	autoFalse := false
	if _, err := mgr.RegisterCertProfile(CertProfileOptions{
		Name:      "itmaster",
		CertPEM:   "-----BEGIN ARGO TUNNEL TOKEN-----\nabc\n-----END ARGO TUNNEL TOKEN-----\n",
		APIToken:  "tok-z",
		AutoStart: &autoFalse,
	}); err != nil {
		t.Fatalf("RegisterCertProfile: %v", err)
	}

	// 1. Jika API DNS gagal, error API langsung tampil di DNSWarning tanpa memanggil CLI route dns
	allowDNSCreate = false
	resFail, err := mgr.ExposeWithOptions(ExposeOptions{
		LocalDomain: "apps.test",
		PublicHost:  "apps.softcuy.uk",
		Profile:     "itmaster",
	})
	if err != nil {
		t.Fatalf("ExposeWithOptions fail case: %v", err)
	}
	if !strings.Contains(resFail.DNSWarning, "Authentication error") {
		t.Fatalf("expected API error surfaced in DNSWarning, got: %q", resFail.DNSWarning)
	}

	// 2. Jika API DNS berhasil, CNAME dibuat di zone softcuy.uk dan CLI route dns tidak dipanggil
	allowDNSCreate = true
	resOK, err := mgr.ExposeWithOptions(ExposeOptions{
		LocalDomain: "apps.test",
		PublicHost:  "apps.softcuy.uk",
		Profile:     "itmaster",
	})
	if err != nil {
		t.Fatalf("ExposeWithOptions success case: %v", err)
	}
	if resOK.DNSWarning != "" || !createdDNS || cliRouteDNSCalled {
		t.Fatalf("expected clean DNSWarning, createdDNS=true, cliRouteDNSCalled=false; got warning=%q created=%v cliCalled=%v", resOK.DNSWarning, createdDNS, cliRouteDNSCalled)
	}
}
