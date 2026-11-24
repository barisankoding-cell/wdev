package cloudflare

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
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
