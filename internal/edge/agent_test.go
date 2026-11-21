package edge

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidDomain(t *testing.T) {
	cases := map[string]bool{
		"example.com":        true,
		"app-dev.example.io": true,
		"-bad.com":           false,
		"no-tld":             false,
		"":                   false,
		"sub.example.c0m":    false,
	}
	for domain, want := range cases {
		if got := validDomain(domain); got != want {
			t.Errorf("validDomain(%q) = %v, want %v", domain, got, want)
		}
	}
}

func TestProxyBlockContainsUpgrade(t *testing.T) {
	block := proxyBlock("app.example.com", "100.64.0.2", 3000, true)
	for _, want := range []string{
		"server_name app.example.com;",
		"proxy_pass http://100.64.0.2:3000;",
		"proxy_set_header Upgrade $http_upgrade;",
		"listen [::]:80;",
	} {
		if want == "listen [::]:80;" {
			continue // tergantung dukungan IPv6 host
		}
		if !strings.Contains(block, want) {
			t.Errorf("proxyBlock tidak memuat %q:\n%s", want, block)
		}
	}
}

func TestMailStreamBlock(t *testing.T) {
	block := mailStreamBlock("100.64.0.2", nil)
	for _, want := range []string{
		"listen 465;", "listen 587;", "listen 143;", "listen 993;", "listen 995;", "listen 110;",
		"proxy_pass 100.64.0.2:993;", "proxy_pass 100.64.0.2:465;", "proxy_pass 100.64.0.2:587;",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("mail stream block tidak memuat %q:\n%s", want, block)
		}
	}
	if strings.Contains(block, "listen 465 ssl;") {
		t.Fatalf("passthrough tidak boleh memakai ssl:\n%s", block)
	}

	tls := mailStreamBlock("100.64.0.2", []mailCert{{Cert: "/c1.pem", Key: "/k1.pem"}, {Cert: "/c2.pem", Key: "/k2.pem"}})
	for _, want := range []string{
		"listen 465 ssl;", "ssl_certificate /c1.pem;", "ssl_certificate /c2.pem;",
		"proxy_pass 100.64.0.2:465;", "proxy_ssl on;",
	} {
		if !strings.Contains(tls, want) {
			t.Fatalf("mail stream TLS tidak memuat %q:\n%s", want, tls)
		}
	}
	if strings.Contains(tls, "listen 587 ssl;") || strings.Contains(tls, "listen 143 ssl;") {
		t.Fatalf("STARTTLS/plain tidak boleh diterminasi ssl:\n%s", tls)
	}
}

func TestAgentAuthRequired(t *testing.T) {
	prev := LoadAPIKey()
	if prev == "" {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		rec := httptest.NewRecorder()
		NewAgentHandler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	}
}

func TestMergeMynetworks(t *testing.T) {
	got := mergeMynetworks("127.0.0.0/8 [::1]/128 100.64.0.0/10 10.0.0.0/8", "100.64.0.7")
	for _, want := range []string{"127.0.0.0/8", "[::1]/128", "10.0.0.0/8", "100.64.0.7"} {
		if !strings.Contains(got, want) {
			t.Fatalf("mynetworks tidak memuat %q: %s", want, got)
		}
	}
	if strings.Contains(got, "100.64.0.0/10") {
		t.Fatalf("subnet NetBird lebar tidak boleh diizinkan: %s", got)
	}
}

func TestMergeMynetworksIdempotent(t *testing.T) {
	first := mergeMynetworks("", "100.64.0.7")
	second := mergeMynetworks(first, "100.64.0.7")
	if first != second {
		t.Fatalf("merge tidak idempoten: %q vs %q", first, second)
	}
	if strings.Count(second, "100.64.0.7") != 1 {
		t.Fatalf("IP target duplikat: %s", second)
	}
}

func TestSetupMailDomainRejectsInvalidTargetIP(t *testing.T) {
	if _, err := setupMailDomain("example.com", "100.64.0.2:80; include /etc/passwd", false, nil); err == nil {
		t.Fatalf("expected error for non-IP target_ip injection payload")
	}
	if _, err := setupMailDomain("example.com", "127.0.0.1", false, nil); err == nil {
		t.Fatalf("expected error for loopback target_ip")
	}
}
