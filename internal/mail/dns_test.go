package mail

import "testing"

func TestContainsSPF(t *testing.T) {
	if !containsSPF([]string{`"v=spf1 -all"`}) {
		t.Fatal("SPF tidak terdeteksi")
	}
	if containsSPF([]string{`"google-site-verification=abc"`}) {
		t.Fatal("TXT non-SPF dianggap SPF")
	}
}

func TestRecordContentExists(t *testing.T) {
	if !recordContentExists([]string{`"10 mail.example.com."`}, "10 mail.example.com.") {
		t.Fatal("MX yang sama tidak terdeteksi")
	}
	if recordContentExists([]string{`"10 mail.lama.com."`}, "10 mail.example.com.") {
		t.Fatal("MX berbeda dianggap sama")
	}
}

func TestHostnameMatch(t *testing.T) {
	if _, ok := hostnameMatch([]string{"mail.example.com."}, "mail.example.com"); !ok {
		t.Fatal("PTR yang cocok tidak terdeteksi")
	}
	got, ok := hostnameMatch([]string{"vps.provider.net."}, "mail.example.com")
	if ok || got != "vps.provider.net" {
		t.Fatalf("PTR mismatch salah: %q %v", got, ok)
	}
}

func TestRelayDNSPlan(t *testing.T) {
	for _, provider := range []string{"brevo", "resend", "custom"} {
		plan := relayDNSPlan(provider, "example.com")
		if len(plan) == 0 {
			t.Fatalf("plan %s kosong", provider)
		}
	}
	if got := relayDNSPlan("brevo", "example.com")[0]; got == "" {
		t.Fatal("baris plan kosong")
	}
}

func TestLocalMailVhostBlock(t *testing.T) {
	block := localMailVhostBlock("Example.COM.", "/run/php/php8.4-fpm.sock")
	for _, want := range []string{
		"server_name webmail.example.com;",
		"server_name mailadmin.example.com;",
		"proxy_pass http://127.0.0.1:8080;",
		"server_name mail.example.com;",
		"fastcgi_pass unix:/run/php/php8.4-fpm.sock;",
	} {
		if !contains(block, want) {
			t.Fatalf("vhost tidak memuat %q:\n%s", want, block)
		}
	}
	if contains(block, "webmail.*") {
		t.Fatal("vhost tidak boleh memakai wildcard webmail.*")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

func TestValidMailDomain(t *testing.T) {
	cases := map[string]bool{
		"example.com":        true,
		"mail.example.co.id": true,
		"":                   false,
		"nodot":              false,
		"-bad.example.com":   false,
		"bad-.example.com":   false,
		"bad..example.com":   false,
		"../etc/passwd":      false,
		"exa mple.com":       false,
		"example.com/../x":   false,
	}
	for in, want := range cases {
		if got := validMailDomain(in); got != want {
			t.Errorf("validMailDomain(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestRelayDNSPlanUnknown(t *testing.T) {
	plan := relayDNSPlan("", "example.com")
	if len(plan) != 1 || !contains(plan[0], "belum dikonfigurasi") {
		t.Fatalf("plan kosong untuk provider kosong: %v", plan)
	}
}
