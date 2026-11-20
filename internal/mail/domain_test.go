package mail

import "testing"

func TestRecordNameForZone(t *testing.T) {
	cases := []struct {
		zone, fqdn, want string
	}{
		{"example.com", "example.com", "@"},
		{"example.com", "mail.example.com", "mail"},
		{"example.com", "mail.example.com.", "mail"},
		{"example.com", "mx.sub.example.com", "mx.sub"},
		{"example.com.", "MAIL.Example.com", "mail"},
	}
	for _, c := range cases {
		if got := recordNameForZone(c.zone, c.fqdn); got != c.want {
			t.Errorf("recordNameForZone(%q,%q)=%q want %q", c.zone, c.fqdn, got, c.want)
		}
	}
}

func TestRecordSetEqual(t *testing.T) {
	if !recordSetEqual([]string{"10 mail.example.com."}, "10 mail.example.com.") {
		t.Error("expected MX match")
	}
	if !recordSetEqual([]string{`"v=spf1 -all"`}, "v=spf1 -all") {
		t.Error("expected quoted TXT match")
	}
	if recordSetEqual([]string{"10 mail.other.com."}, "10 mail.example.com.") {
		t.Error("mismatched MX should not match")
	}
	if recordSetEqual(nil, "10 mail.example.com.") {
		t.Error("empty set should not match")
	}
}

func TestCleanMailDomain(t *testing.T) {
	if got := cleanMailDomain(" Example.COM. "); got != "example.com" {
		t.Errorf("cleanMailDomain=%q", got)
	}
}

func TestRouteBranchTarget(t *testing.T) {
	route := &outboundRoute{Branches: []relayBranch{
		{If: `sender_domain == 'example.com'`, Then: `'relay_example_com'`},
	}}
	if got := routeBranchTarget(route, "example.com"); got != "relay_example_com" {
		t.Errorf("routeBranchTarget=%q", got)
	}
	if got := routeBranchTarget(route, "other.com"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
