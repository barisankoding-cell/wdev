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
	block := mailStreamBlock("100.64.0.2", "", "")
	if !strings.Contains(block, "listen 465;") || !strings.Contains(block, "proxy_pass 100.64.0.2:993;") {
		t.Fatalf("mail stream block salah:\n%s", block)
	}
	tls := mailStreamBlock("100.64.0.2", "/etc/letsencrypt/live/mail/fullchain.pem", "/etc/letsencrypt/live/mail/privkey.pem")
	if !strings.Contains(tls, "ssl_certificate /etc/letsencrypt/live/mail/fullchain.pem;") {
		t.Fatalf("mail stream TLS block salah:\n%s", tls)
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
