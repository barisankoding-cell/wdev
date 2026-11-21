package pdns

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wdev/internal/config"
	"wdev/internal/store"
)

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

func resetExternalPublicIPCacheForTest(fn func(context.Context) string) func() {
	prevFn := detectExternalPublicIPFn
	externalPublicIPMu.Lock()
	cachedExternalPublicIP = ""
	cachedExternalPublicAt = time.Time{}
	cachedExternalFailAt = time.Time{}
	detectExternalPublicIPFn = fn
	externalPublicIPMu.Unlock()

	return func() {
		externalPublicIPMu.Lock()
		detectExternalPublicIPFn = prevFn
		cachedExternalPublicIP = ""
		cachedExternalPublicAt = time.Time{}
		cachedExternalFailAt = time.Time{}
		externalPublicIPMu.Unlock()
	}
}

func TestVPSModeRejectsPrivateConfigIPAndCachesDetectedPublicIP(t *testing.T) {
	calls := 0
	restore := resetExternalPublicIPCacheForTest(func(ctx context.Context) string {
		calls++
		// Simulasikan panggilan ke-1 sukses, panggilan berikutnya gagal/timeout: cache harus mempertahankan IP publik valid.
		if calls == 1 {
			return "103.171.85.72"
		}
		return ""
	})
	defer restore()

	tmp := t.TempDir()
	etcDir := filepath.Join(tmp, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	mgr := New(&config.Config{WdevEtc: etcDir}, st)
	if err := mgr.SetMode("vps"); err != nil {
		t.Fatalf("SetMode(vps) failed: %v", err)
	}
	confData := `VPS_NETBIRD_IP="127.0.0.1"
VPS_PUBLIC_IP="10.3.35.254"
API_KEY="secret-test-key"
PDNS_PORT="8081"
AGENT_PORT="8082"
`
	if err := os.WriteFile(filepath.Join(etcDir, "edge-dns.conf"), []byte(confData), 0600); err != nil {
		t.Fatal(err)
	}

	c1 := mgr.LoadConfig()
	if c1.VPSPublicIP != "103.171.85.72" {
		t.Fatalf("LoadConfig() #1 VPSPublicIP = %q, want 103.171.85.72", c1.VPSPublicIP)
	}
	c2 := mgr.LoadConfig()
	if c2.VPSPublicIP != "103.171.85.72" {
		t.Fatalf("LoadConfig() #2 VPSPublicIP = %q, want 103.171.85.72", c2.VPSPublicIP)
	}
	if calls != 1 {
		t.Fatalf("expected external lookup to be cached after 1st success, got %d calls", calls)
	}
}

func TestVPSModeNeverFallsBackToPrivateOrLoopbackIPInRepairAndDoctor(t *testing.T) {
	restore := resetExternalPublicIPCacheForTest(func(ctx context.Context) string {
		return ""
	})
	defer restore()

	tmp := t.TempDir()
	etcDir := filepath.Join(tmp, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatal(err)
	}
	st, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var patchPayloads []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/servers/localhost":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"localhost"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/servers/localhost/zones/"):
			_ = json.NewEncoder(w).Encode(ZoneInfo{
				ID:     "nikaget.my.id.",
				Name:   "nikaget.my.id.",
				Kind:   "Native",
				Serial: 1795239183,
				RRsets: []RRSet{
					{Name: "nikaget.my.id.", Type: "SOA", TTL: 3600, Records: []Record{{Content: "ns1.nikaget.my.id. hostmaster.nikaget.my.id. 1795239183 10800 3600 604800 3600"}}},
					{Name: "nikaget.my.id.", Type: "NS", TTL: 3600, Records: []Record{{Content: "ns1.nikaget.my.id."}, {Content: "ns2.nikaget.my.id."}}},
					{Name: "ns1.nikaget.my.id.", Type: "A", TTL: 300, Records: []Record{{Content: "103.171.85.72"}}},
					{Name: "ns2.nikaget.my.id.", Type: "A", TTL: 300, Records: []Record{{Content: "103.171.85.72"}}},
				},
			})
		case r.Method == http.MethodPatch:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			b, _ := json.Marshal(body)
			patchPayloads = append(patchPayloads, string(b))
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	mgr := New(&config.Config{WdevEtc: etcDir}, st)
	if err := mgr.SetMode("vps"); err != nil {
		t.Fatalf("SetMode(vps) failed: %v", err)
	}
	confData := "VPS_NETBIRD_IP=\"" + u.Hostname() + "\"\n" +
		"VPS_PUBLIC_IP=\"10.3.35.254\"\n" +
		"API_KEY=\"secret-test-key\"\n" +
		"PDNS_PORT=\"" + u.Port() + "\"\n" +
		"AGENT_PORT=\"8082\"\n"
	if err := os.WriteFile(filepath.Join(etcDir, "edge-dns.conf"), []byte(confData), 0600); err != nil {
		t.Fatal(err)
	}

	// Saat IP publik tidak valid/tidak terdeteksi, RepairZone wajib menolak dan tidak boleh menulis 127.0.0.1 atau 10.3.35.254 ke glue record.
	if _, err := mgr.RepairZone("nikaget.my.id"); err == nil {
		t.Fatal("expected RepairZone to reject missing valid public IP in vps mode")
	}
	if len(patchPayloads) > 0 {
		t.Fatalf("expected no glue patch when public IP invalid, got %v", patchPayloads)
	}

	// Saat IP publik valid tersedia, DoctorZone harus membaca target 103.171.85.72, bukan 10.3.35.254.
	confValid := strings.ReplaceAll(confData, "10.3.35.254", "103.171.85.72")
	if err := os.WriteFile(filepath.Join(etcDir, "edge-dns.conf"), []byte(confValid), 0600); err != nil {
		t.Fatal(err)
	}
	rep, err := mgr.DoctorZone("nikaget.my.id")
	if err != nil {
		t.Fatalf("DoctorZone failed: %v", err)
	}
	glueChecked := false
	for _, chk := range rep.Checks {
		if chk.Name == "Glue A (zone)" {
			glueChecked = true
			if chk.Status != CheckOK {
				t.Fatalf("expected Glue A (zone) OK, got %s: %s", chk.Status, chk.Detail)
			}
		}
		if strings.Contains(chk.Detail, "10.3.35.254") {
			t.Fatalf("unexpected private IP leak in check %s: %s", chk.Name, chk.Detail)
		}
	}
	if !glueChecked {
		t.Fatal("Glue A (zone) check missing from report")
	}
}
