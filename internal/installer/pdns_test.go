package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"wdev/internal/config"
)

func TestBuildStandalonePDNSConfLocksWebserverToLoopback(t *testing.T) {
	conf := buildStandalonePDNSConf("", "0.0.0.0", "/var/lib/powerdns/pdns.sqlite3", "test-api-key")

	for _, want := range []string{
		"webserver=yes",
		"webserver-address=127.0.0.1",
		"webserver-port=8081",
		"webserver-allow-from=127.0.0.1,::1",
		"api-key=test-api-key",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("expected standalone PDNS config to contain %q, got:\n%s", want, conf)
		}
	}

	for _, forbidden := range []string{
		"webserver-address=0.0.0.0",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"100.64.0.0/10",
	} {
		if strings.Contains(conf, forbidden) {
			t.Fatalf("standalone PDNS config must not expose webserver with %q:\n%s", forbidden, conf)
		}
	}
}

func TestResolveExistingPDNSAPIKeyIgnoresRemoteEdgeConf(t *testing.T) {
	etcDir := t.TempDir()
	remoteConf := "VPS_NETBIRD_IP=\"100.64.0.10\"\nVPS_PUBLIC_IP=\"203.0.113.50\"\nAPI_KEY=\"remote-edge-key\"\n"
	if err := os.WriteFile(filepath.Join(etcDir, "edge-dns.conf"), []byte(remoteConf), 0600); err != nil {
		t.Fatal(err)
	}

	inst := New(&config.Config{WdevEtc: etcDir}, nil)
	if got := inst.resolveExistingPDNSAPIKey(); got == "remote-edge-key" {
		t.Fatalf("resolveExistingPDNSAPIKey must ignore remote Edge API_KEY, got %q", got)
	}

	localConf := "VPS_NETBIRD_IP=\"127.0.0.1\"\nAPI_KEY=\"local-pdns-key\"\n"
	if err := os.WriteFile(filepath.Join(etcDir, "local-pdns.conf"), []byte(localConf), 0600); err != nil {
		t.Fatal(err)
	}
	if got := inst.resolveExistingPDNSAPIKey(); got != "local-pdns-key" {
		t.Fatalf("resolveExistingPDNSAPIKey = %q, want local-pdns-key", got)
	}
}
