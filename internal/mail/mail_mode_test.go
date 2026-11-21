package mail

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestPreflightVPSModeStandaloneWithoutNetbirdOrRelay(t *testing.T) {
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
	if containsSubstr(pf.Blockers, "relay") {
		t.Fatalf("vps mode must allow setup without external SMTP relay, blockers=%v", pf.Blockers)
	}
	if !containsSubstr(pf.Warnings, "SMTP lokal Stalwart") {
		t.Fatalf("expected default local SMTP info warning in vps mode, warnings=%v", pf.Warnings)
	}
}

func TestPreflightEdgeModeRequiresNetbirdAndGatewayWithoutRelayBlocker(t *testing.T) {
	mgr, _ := newTestMailManager(t, "edge")
	pf := mgr.Preflight("example.com")
	if pf.Mode != "edge" {
		t.Fatalf("Mode = %q, want edge", pf.Mode)
	}
	if !containsSubstr(pf.Blockers, "Edge Gateway") {
		t.Fatalf("expected edge gateway blocker in edge mode, got %v", pf.Blockers)
	}
	if containsSubstr(pf.Blockers, "relay") {
		t.Fatalf("edge mode must allow setup without external SMTP relay, blockers=%v", pf.Blockers)
	}
	if !containsSubstr(pf.Warnings, "SMTP lokal Stalwart") {
		t.Fatalf("expected default local SMTP info warning in edge mode, warnings=%v", pf.Warnings)
	}
}

func TestActiveRelayProviderDefaultsToLocal(t *testing.T) {
	mgr, _ := newTestMailManager(t, "vps")
	if got := mgr.ActiveRelayProvider("example.com"); got != "local" {
		t.Fatalf("ActiveRelayProvider without relay = %q, want local", got)
	}
	plan := relayDNSPlan("local", "example.com")
	if len(plan) == 0 || !strings.Contains(plan[0], "SMTP Lokal") {
		t.Fatalf("expected local SMTP DNS plan, got %v", plan)
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

func TestPreflightWithAutoInstallerMovesMissingBinariesToWarnings(t *testing.T) {
	mgr, _ := newTestMailManager(t, "local")
	mgr.SetInstaller(func(context.Context, func(string), string) error {
		return nil
	})
	pf := mgr.Preflight("mailtest.local")
	if len(pf.Blockers) != 0 {
		t.Fatalf("expected 0 blockers in local mode with auto-installer, got blockers=%v", pf.Blockers)
	}
}

func TestSetupMailFailsWhenMailStackMissingAndNotSilentSuccess(t *testing.T) {
	mgr, cfg := newTestMailManager(t, "vps")
	edgeConf := "VPS_NETBIRD_IP=\"127.0.0.1\"\nVPS_PUBLIC_IP=\"203.0.113.10\"\nAPI_KEY=\"k\"\nPDNS_PORT=\"8081\"\n"
	if err := os.WriteFile(filepath.Join(cfg.WdevEtc, "edge-dns.conf"), []byte(edgeConf), 0600); err != nil {
		t.Fatal(err)
	}

	calledInstall := false
	mgr.SetInstaller(func(_ context.Context, _ func(string), dom string) error {
		calledInstall = true
		if dom != "nikaget.my.id" {
			t.Fatalf("expected installer domain nikaget.my.id, got %q", dom)
		}
		return fmt.Errorf("simulasi gagal unduh stalwart")
	})

	err := mgr.SetupMail(context.Background(), func(string) {}, SetupMailOptions{Domain: "nikaget.my.id", SSL: true})
	if err == nil {
		t.Fatal("expected SetupMail to fail when mail stack install fails instead of reporting success")
	}
	_, snErr := os.Stat("/var/www/snappymail/index.php")
	if !hasToolPath("/usr/local/bin/stalwart", "stalwart") || !hasStalwartUnit() || !hasToolPath("/usr/local/bin/stalwart-cli", "stalwart-cli") || snErr != nil {
		if !calledInstall {
			t.Fatal("expected SetupMail to attempt automatic Mail Stack installation when binaries are missing")
		}
	}
	if _, statErr := os.Stat(mgr.domainsStatePath()); !os.IsNotExist(statErr) {
		t.Fatalf("expected domains state %s NOT written when SetupMail fails", mgr.domainsStatePath())
	}

	rep, repErr := mgr.RepairDomain("nikaget.my.id", func(string) {})
	if repErr == nil && rep.OK {
		t.Fatalf("expected RepairDomain report OK=false when mail stack install fails, got %+v", rep)
	}
}

func TestGetStatusDetectsStalwartPortAccessibility(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	openPort := ln.Addr().(*net.TCPAddr).Port

	lnClosed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := lnClosed.Addr().(*net.TCPAddr).Port
	_ = lnClosed.Close()

	if !probeTCPPort("127.0.0.1", openPort, 300*time.Millisecond) {
		t.Fatalf("expected probeTCPPort open=true on listening port %d", openPort)
	}
	if probeTCPPort("127.0.0.1", closedPort, 300*time.Millisecond) {
		t.Fatalf("expected probeTCPPort open=false on closed port %d", closedPort)
	}

	mgr, cfg := newTestMailManager(t, "vps")
	edgeConf := "VPS_NETBIRD_IP=\"127.0.0.1\"\nVPS_PUBLIC_IP=\"203.0.113.10\"\nAPI_KEY=\"k\"\nPDNS_PORT=\"8081\"\n"
	if err := os.WriteFile(filepath.Join(cfg.WdevEtc, "edge-dns.conf"), []byte(edgeConf), 0600); err != nil {
		t.Fatal(err)
	}
	mgr.SetPortProbe(func(host string, port int, _ time.Duration) bool {
		if host == "127.0.0.1" && (port == 25 || port == 587 || port == 8080) {
			return true
		}
		return host == "203.0.113.10" && port == 25
	})

	st, err := mgr.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	want := map[int]struct{ open, pub bool }{
		25:   {true, true},
		465:  {false, false},
		587:  {true, false},
		993:  {false, false},
		8080: {true, false},
	}
	if len(st.StalwartPorts) != len(want) {
		t.Fatalf("expected %d ports, got %+v", len(want), st.StalwartPorts)
	}
	for _, p := range st.StalwartPorts {
		exp := want[p.Port]
		if p.Open != exp.open || p.Public != exp.pub {
			t.Fatalf("port %d status = %+v, want open=%v public=%v", p.Port, p, exp.open, exp.pub)
		}
		if p.Port == 8080 && !p.InternalOnly {
			t.Fatalf("expected port 8080 InternalOnly=true, got %+v", p)
		}
	}
	if !st.BehindNAT {
		t.Fatalf("expected BehindNAT=true for unattached test IP 203.0.113.10")
	}
}

func TestChangeAccountPasswordValidation(t *testing.T) {
	mgr, _ := newTestMailManager(t, "vps")
	if err := mgr.ChangeAccountPassword("", "Secret123!"); err == nil {
		t.Fatal("expected error for empty email")
	}
	if err := mgr.ChangeAccountPassword("user@nikaget.my.id", ""); err == nil {
		t.Fatal("expected error for empty password")
	}
	if err := mgr.ChangeAccountPassword("bad-email", "Secret123!"); err == nil {
		t.Fatal("expected error for invalid email format")
	}
	if err := mgr.ChangeAccountPassword("user@nikaget.my.id", "bad\npass"); err == nil {
		t.Fatal("expected error for newline in password")
	}
}
