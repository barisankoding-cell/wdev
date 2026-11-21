package mail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"wdev/internal/config"
	"wdev/internal/pdns"
	"wdev/internal/store"
)

func newTestMailManager(t *testing.T, mode string) (*Manager, *config.Config) {
	t.Helper()
	tmp := t.TempDir()
	etcDir := filepath.Join(tmp, "etc")
	avail := filepath.Join(tmp, "sites-available")
	enabled := filepath.Join(tmp, "sites-enabled")
	hosts := filepath.Join(tmp, "hosts")
	for _, dir := range []string{etcDir, avail, enabled} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(hosts, []byte("127.0.0.1 localhost\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		WdevEtc:          etcDir,
		NginxSitesAvail:  avail,
		NginxSitesEnable: enabled,
		HostsFile:        hosts,
	}
	st, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	pdnsMgr := pdns.New(cfg, st)
	if mode != "" {
		if err := pdnsMgr.SetMode(mode); err != nil {
			t.Fatal(err)
		}
	}
	return New(cfg, st, pdnsMgr), cfg
}

func containsSubstr(items []string, needle string) bool {
	for _, s := range items {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func TestPreflightLocalModeStandaloneWithoutNetbirdOrPDNS(t *testing.T) {
	mgr, _ := newTestMailManager(t, "local")
	pf := mgr.Preflight("mailtest.local")

	if pf.Mode != "local" {
		t.Fatalf("Mode = %q, want local", pf.Mode)
	}
	if !pf.EdgeConfigured {
		t.Fatal("expected local mode to treat connectivity readiness as true without NetBird")
	}
	for _, b := range pf.Blockers {
		low := strings.ToLower(b)
		if strings.Contains(low, "netbird") || strings.Contains(low, "edge gateway") ||
			strings.Contains(low, "zone dns") || strings.Contains(low, "relay outbound") ||
			strings.Contains(low, "ip publik") {
			t.Fatalf("local mode should not emit NetBird/Edge/Zone/Relay blockers, got %q", b)
		}
	}
	if !containsSubstr(pf.Warnings, "Relay outbound belum disetel") {
		t.Fatalf("expected optional relay warning in local mode, got warnings=%v", pf.Warnings)
	}
}

func TestPreflightVPSModeStandaloneWithoutNetbird(t *testing.T) {
	mgr, cfg := newTestMailManager(t, "vps")
	edgeConf := "VPS_NETBIRD_IP=\"127.0.0.1\"\nVPS_PUBLIC_IP=\"203.0.113.10\"\nAPI_KEY=\"k\"\nPDNS_PORT=\"8081\"\n"
	if err := os.WriteFile(filepath.Join(cfg.WdevEtc, "edge-dns.conf"), []byte(edgeConf), 0600); err != nil {
		t.Fatal(err)
	}
	pf := mgr.Preflight("example.com")
	if pf.Mode != "vps" {
		t.Fatalf("Mode = %q, want vps", pf.Mode)
	}
	if containsSubstr(pf.Blockers, "NetBird") || containsSubstr(pf.Blockers, "wdev pdns connect") {
		t.Fatalf("vps mode must not require NetBird or edge connect, blockers=%v", pf.Blockers)
	}
}

func TestPreflightEdgeModeRequiresNetbirdAndGateway(t *testing.T) {
	mgr, _ := newTestMailManager(t, "edge")
	pf := mgr.Preflight("example.com")
	if pf.Mode != "edge" {
		t.Fatalf("Mode = %q, want edge", pf.Mode)
	}
	if !containsSubstr(pf.Blockers, "Edge Gateway") {
		t.Fatalf("expected edge gateway blocker in edge mode, got %v", pf.Blockers)
	}
}

func TestHostsAndVhostLifecycleAndSSLPreservation(t *testing.T) {
	mgr, cfg := newTestMailManager(t, "local")
	dom := "devmail.test"

	mgr.ensureMailHostsEntries(dom, "127.0.0.1")
	hostsBytes, err := os.ReadFile(cfg.HostsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hostsBytes), "webmail.devmail.test mailadmin.devmail.test") {
		t.Fatalf("expected mail hosts entry, got:\n%s", string(hostsBytes))
	}

	avail, enabled := mgr.nginxMailPaths(dom)
	sslConf := localMailSSLVhostBlock(dom, "/run/php/php8.4-fpm.sock", "/tmp/c.pem", "/tmp/k.pem")
	if err := os.WriteFile(avail, []byte(sslConf), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(avail, enabled); err != nil {
		t.Fatal(err)
	}

	// writeSnappyMailDomainConfig (dipakai oleh AddAccount) tidak boleh menimpa vhost SSL.
	if err := mgr.writeSnappyMailDomainConfig(dom); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(avail)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "listen 443 ssl;") {
		t.Fatalf("expected HTTPS block preserved after writeSnappyMailDomainConfig, got:\n%s", string(after))
	}

	// DeleteDomain dalam mode local membersihkan vhost & entri hosts tanpa menghubungi Edge Agent.
	rep, err := mgr.DeleteDomain(dom, true)
	if err != nil {
		t.Fatalf("DeleteDomain failed: %v", err)
	}
	if _, statErr := os.Stat(avail); !os.IsNotExist(statErr) {
		t.Fatalf("expected vhost file %s removed on DeleteDomain", avail)
	}
	hostsAfter, err := os.ReadFile(cfg.HostsFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(hostsAfter), dom) {
		t.Fatalf("expected domain removed from hosts file, got:\n%s", string(hostsAfter))
	}
	if len(rep.Checks) == 0 {
		t.Fatal("expected action report checks")
	}
}
