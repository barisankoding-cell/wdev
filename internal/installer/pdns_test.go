package installer

import (
	"strings"
	"testing"
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
