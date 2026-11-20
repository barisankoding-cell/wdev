package pdns

import "testing"

func TestNormalizeZoneName(t *testing.T) {
	cases := map[string]string{
		"Example.COM.":     "example.com",
		" sub.example.com": "sub.example.com",
	}
	for in, want := range cases {
		got, err := NormalizeZoneName(in)
		if err != nil || got != want {
			t.Errorf("NormalizeZoneName(%q) = %q,%v want %q", in, got, err, want)
		}
	}
}

func TestNormalizeZoneNameRejects(t *testing.T) {
	for _, in := range []string{"", "nodot", "-bad.com", "bad-.com", "a..b", "exa mple.com", "../etc/passwd"} {
		if _, err := NormalizeZoneName(in); err == nil {
			t.Errorf("NormalizeZoneName(%q) harus error", in)
		}
	}
}

func TestMergeRecordContents(t *testing.T) {
	got := mergeRecordContents([]string{`"v=spf1 include:a ~all"`}, `"v=spf1 include:a ~all"`)
	if len(got) != 1 {
		t.Fatalf("record duplikat tidak terdedup: %v", got)
	}
	got = mergeRecordContents([]string{`"a"`}, `"b"`)
	if len(got) != 2 {
		t.Fatalf("record baru tidak ditambahkan: %v", got)
	}
}

func TestMatchZoneLongestSuffix(t *testing.T) {
	zones := []ZoneInfo{
		{Name: "example.com."},
		{Name: "dev.example.com."},
		{Name: "lain.net."},
	}
	if got := matchZone("api.dev.example.com", zones); got != "dev.example.com" {
		t.Fatalf("matchZone = %q, want dev.example.com", got)
	}
	if got := matchZone("example.com", zones); got != "example.com" {
		t.Fatalf("matchZone apex = %q", got)
	}
	if got := matchZone("bukan-example.com", zones); got != "" {
		t.Fatalf("suffix palsu harus tidak cocok: %q", got)
	}
	if got := matchZone("unknown.org", zones); got != "" {
		t.Fatalf("zone tidak dikenal harus kosong: %q", got)
	}
}

// SOA yang dihasilkan AddZone/RepairZone harus valid dan tidak memakai
// template default PowerDNS.
func TestSOAValidTemplate(t *testing.T) {
	soa := "ns1.example.com. hostmaster.example.com. 1795165945 10800 3600 604800 3600"
	info := parseSOAContent(soa)
	report := ZoneDoctorReport{OK: true}
	if broken, detail := report.soaBroken(info); broken {
		t.Fatalf("SOA valid dianggap rusak: %s", detail)
	}
	if info.Primary != "ns1.example.com" || info.Serial != 1795165945 {
		t.Fatalf("parse SOA salah: %+v", info)
	}
}
