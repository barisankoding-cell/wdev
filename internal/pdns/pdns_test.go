package pdns

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wdev/internal/config"
	"wdev/internal/site"
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

func TestAddZoneWithVhostVPSAndEdgeModes(t *testing.T) {
	restorePub := resetExternalPublicIPCacheForTest(func(ctx context.Context) string {
		return "103.171.85.72"
	})
	defer restorePub()

	type patchRec struct {
		Name    string
		Type    string
		Content []string
	}
	var patches []patchRec
	var proxyPosts []map[string]any

	pdnsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/servers/localhost":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"localhost"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/servers/localhost/zones":
			_ = json.NewEncoder(w).Encode([]ZoneInfo{{Name: "example.com.", Kind: "Native"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/servers/localhost/zones":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"example.com."}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/servers/localhost/zones/"):
			var body struct {
				RRSets []RRSet `json:"rrsets"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			for _, rr := range body.RRSets {
				var vals []string
				for _, rec := range rr.Records {
					vals = append(vals, rec.Content)
				}
				patches = append(patches, patchRec{Name: rr.Name, Type: rr.Type, Content: vals})
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/metadata/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/servers/localhost/zones/"):
			_ = json.NewEncoder(w).Encode(ZoneInfo{
				ID:     "example.com.",
				Name:   "example.com.",
				Kind:   "Native",
				Serial: 1795239183,
				RRsets: []RRSet{
					{Name: "example.com.", Type: "SOA", TTL: 3600, Records: []Record{{Content: "ns1.example.com. hostmaster.example.com. 1795239183 10800 3600 604800 3600"}}},
					{Name: "example.com.", Type: "NS", TTL: 3600, Records: []Record{{Content: "ns1.example.com."}, {Content: "ns2.example.com."}}},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer pdnsSrv.Close()
	pdnsURL, _ := url.Parse(pdnsSrv.URL)

	agentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/proxies" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			proxyPosts = append(proxyPosts, body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer agentSrv.Close()
	agentURL, _ := url.Parse(agentSrv.URL)

	tmp := t.TempDir()
	etcDir := filepath.Join(tmp, "etc")
	_ = os.MkdirAll(etcDir, 0755)
	hostsFile := filepath.Join(tmp, "hosts")
	_ = os.WriteFile(hostsFile, []byte("127.0.0.1 localhost\n"), 0644)

	st, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := &config.Config{
		WdevEtc:          etcDir,
		NginxSitesAvail:  filepath.Join(tmp, "sites-available"),
		NginxSitesEnable: filepath.Join(tmp, "sites-enabled"),
		WebRootBase:      filepath.Join(tmp, "www"),
		HostsFile:        hostsFile,
	}
	siteMgr := site.New(cfg, st)
	siteMgr.SetSkipNginxExec(true)

	mgr := New(cfg, st)
	mgr.SetSiteManager(siteMgr)

	// 1. VPS mode: zone + apex/www A records + static HTML vhost
	if err := mgr.SetMode("vps"); err != nil {
		t.Fatal(err)
	}
	confData := "VPS_NETBIRD_IP=\"" + pdnsURL.Hostname() + "\"\n" +
		"VPS_PUBLIC_IP=\"103.171.85.72\"\n" +
		"API_KEY=\"secret-test-key\"\n" +
		"PDNS_PORT=\"" + pdnsURL.Port() + "\"\n" +
		"AGENT_PORT=\"" + agentURL.Port() + "\"\n"
	if err := os.WriteFile(filepath.Join(etcDir, "edge-dns.conf"), []byte(confData), 0600); err != nil {
		t.Fatal(err)
	}

	res, err := mgr.AddZoneWithVhost("example.com")
	if err != nil {
		t.Fatalf("AddZoneWithVhost(vps) failed: %v", err)
	}
	if !res.VHostCreated || res.VHostStatus != "created" || res.ZoneIP != "103.171.85.72" {
		t.Fatalf("unexpected AddZoneWithVhost result: %+v", res)
	}

	wantA := map[string]bool{
		"ns1.example.com.": false,
		"ns2.example.com.": false,
		"example.com.":     false,
		"www.example.com.": false,
	}
	for _, p := range patches {
		if p.Type == "A" && len(p.Content) == 1 && p.Content[0] == "103.171.85.72" {
			if _, ok := wantA[p.Name]; ok {
				wantA[p.Name] = true
			}
		}
	}
	for name, seen := range wantA {
		if !seen {
			t.Fatalf("expected A record for %s -> 103.171.85.72, patches=%+v", name, patches)
		}
	}

	indexHTML, err := os.ReadFile(filepath.Join(cfg.WebRootBase, "example.com", "index.html"))
	if err != nil || !strings.Contains(string(indexHTML), "example.com") {
		t.Fatalf("expected static index.html for example.com, err=%v", err)
	}

	// 2. Edge mode: creates local static vhost and exposes it to VPS Edge Agent
	prevNB := detectLocalNetbirdIPFn
	detectLocalNetbirdIPFn = func() string { return "100.64.0.42" }
	defer func() { detectLocalNetbirdIPFn = prevNB }()

	if err := mgr.SetMode("edge"); err != nil {
		t.Fatal(err)
	}
	resEdge, err := mgr.AddZoneWithVhost("edgeapp.com")
	if err != nil {
		t.Fatalf("AddZoneWithVhost(edge) failed: %v", err)
	}
	if !resEdge.VHostCreated || !resEdge.EdgeExposed {
		t.Fatalf("expected edge vhost created and exposed, got %+v", resEdge)
	}
	if len(proxyPosts) == 0 || proxyPosts[0]["domain"] != "edgeapp.com" || proxyPosts[0]["target_ip"] != "100.64.0.42" {
		t.Fatalf("expected edge proxy POST for edgeapp.com, got %+v", proxyPosts)
	}
}

func TestFormatTXTRecordContentAndMXRecordContent(t *testing.T) {
	short := formatTXTRecordContent("v=spf1 -all")
	if short != `"v=spf1 -all"` {
		t.Fatalf("short TXT = %q", short)
	}

	rawLong := "p=" + strings.Repeat("X", 300)
	formatted := formatTXTRecordContent(rawLong)
	if !strings.Contains(formatted, `" "`) {
		t.Fatalf("expected TXT > 255 bytes to split into multi-chunk quotes, got %q", formatted)
	}
	for _, chunk := range strings.Split(formatted[1:len(formatted)-1], `" "`) {
		if len(chunk) > 255 {
			t.Fatalf("TXT chunk exceeded 255 bytes: %d", len(chunk))
		}
	}

	if mx := formatMXRecordContent("10 feedback-smtp.us-east-1.amazonses.com"); mx != "10 feedback-smtp.us-east-1.amazonses.com." {
		t.Fatalf("formatMXRecordContent = %q", mx)
	}
}

func TestConfigureLocalVhostSameDomainAndRealIPConfig(t *testing.T) {
	tmp := t.TempDir()
	avail := filepath.Join(tmp, "sites-available")
	confD := filepath.Join(tmp, "conf.d")
	etcDir := filepath.Join(tmp, "etc")
	for _, dir := range []string{avail, confD, etcDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Vhost HTTP-only dengan server_name yang sudah sama dengan publicDomain (seperti lara.erposoft.my.id)
	//    harus tetap dipatch FastCGI HTTPS dan TIDAK boleh mendapat redirect 301 https:// tanpa blok 443.
	vhostPath := filepath.Join(avail, "lara.erposoft.my.id")
	initial := `server {
    listen 80;
    server_name lara.erposoft.my.id;
    root /mnt/projects/appsone/public;
    location ~ \.php$ {
        include fastcgi_params;
        fastcgi_pass unix:/run/php/php8.4-fpm.sock;
    }
}
`
	if err := os.WriteFile(vhostPath, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}
	if err := configureLocalVhost(vhostPath, "lara.erposoft.my.id"); err != nil {
		t.Fatalf("configureLocalVhost failed: %v", err)
	}
	gotBytes, _ := os.ReadFile(vhostPath)
	got := string(gotBytes)
	if !strings.Contains(got, "fastcgi_param HTTPS $fe_https if_not_empty;") {
		t.Fatalf("expected FastCGI HTTPS param even when domain already in server_name:\n%s", got)
	}
	if strings.Contains(got, "return 301 https://") {
		t.Fatalf("HTTP-only vhost must not inject 301 https redirect:\n%s", got)
	}

	// 2. Vhost dengan SSL lokal (443) + alias baru harus menormalkan redirect 80->443 agar tidak loop lewat Edge.
	sslPath := filepath.Join(avail, "appsone.test")
	sslInit := `server {
    listen 80;
    server_name appsone.test;
    return 301 https://$host$request_uri;
}
server {
    listen 443 ssl;
    server_name appsone.test;
    ssl_certificate /etc/ssl/local-certs/appsone.test.pem;
    location ~ \.php$ {
        include fastcgi_params;
        fastcgi_pass unix:/run/php/php8.4-fpm.sock;
    }
}
`
	if err := os.WriteFile(sslPath, []byte(sslInit), 0644); err != nil {
		t.Fatal(err)
	}
	if err := configureLocalVhost(sslPath, "app.erposoft.my.id"); err != nil {
		t.Fatalf("configureLocalVhost(ssl) failed: %v", err)
	}
	gotSSLBytes, _ := os.ReadFile(sslPath)
	gotSSL := string(gotSSLBytes)
	if !strings.Contains(gotSSL, "server_name appsone.test app.erposoft.my.id;") {
		t.Fatalf("expected public domain alias added:\n%s", gotSSL)
	}
	if !strings.Contains(gotSSL, "$redirect_to_https") ||
		!strings.Contains(gotSSL, `return 301 https://$host$request_uri;`) ||
		!strings.Contains(gotSSL, "fastcgi_param HTTPS $fe_https if_not_empty;") {
		t.Fatalf("expected conditional redirect and FastCGI HTTPS on SSL vhost:\n%s", gotSSL)
	}

	// 3. EnsureEdgeRealIPConfig menulis set_real_ip_from di mode edge dan membersihkannya di mode local.
	st, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	mgr := New(&config.Config{WdevEtc: etcDir, NginxSitesAvail: avail}, st)
	if err := mgr.SetMode("edge"); err != nil {
		t.Fatal(err)
	}
	edgeConf := "VPS_NETBIRD_IP=\"100.68.66.249\"\nVPS_PUBLIC_IP=\"174.128.205.124\"\nAPI_KEY=\"k\"\n"
	if err := os.WriteFile(filepath.Join(etcDir, "edge-dns.conf"), []byte(edgeConf), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := mgr.EnsureEdgeRealIPConfig()
	if err != nil || !changed {
		t.Fatalf("EnsureEdgeRealIPConfig(edge) got changed=%v err=%v", changed, err)
	}
	realIPBytes, err := os.ReadFile(filepath.Join(confD, "wdev-edge-realip.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(realIPBytes), "set_real_ip_from 100.68.66.249;") || !strings.Contains(string(realIPBytes), "real_ip_header X-Real-IP;") {
		t.Fatalf("unexpected realip config:\n%s", string(realIPBytes))
	}

	if err := mgr.SetMode("local"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(confD, "wdev-edge-realip.conf")); !os.IsNotExist(err) {
		t.Fatalf("expected wdev-edge-realip.conf removed in local mode, err=%v", err)
	}
}

func TestDeleteDomainDNSRecordsRemovesAllRelatedRRsetsOnly(t *testing.T) {
	var lastPatch string
	pdnsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/servers/localhost/zones":
			_ = json.NewEncoder(w).Encode([]ZoneInfo{{ID: "erposoft.my.id.", Name: "erposoft.my.id."}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/servers/localhost/zones/erposoft.my.id.":
			_ = json.NewEncoder(w).Encode(ZoneInfo{
				ID:   "erposoft.my.id.",
				Name: "erposoft.my.id.",
				RRsets: []RRSet{
					{Name: "erposoft.my.id.", Type: "SOA"},
					{Name: "erposoft.my.id.", Type: "NS"},
					{Name: "erposoft.my.id.", Type: "MX"},
					{Name: "lara.erposoft.my.id.", Type: "A"},
					{Name: "panel.erposoft.my.id.", Type: "A"},
					{Name: "panel.erposoft.my.id.", Type: "AAAA"},
					{Name: "panel.erposoft.my.id.", Type: "TXT"},
					{Name: "www.panel.erposoft.my.id.", Type: "CNAME"},
					{Name: "_acme-challenge.panel.erposoft.my.id.", Type: "TXT"},
				},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/servers/localhost/zones/erposoft.my.id.":
			b, _ := io.ReadAll(r.Body)
			lastPatch = string(b)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer pdnsSrv.Close()
	u, _ := url.Parse(pdnsSrv.URL)

	tmp := t.TempDir()
	st, err := store.New(filepath.Join(tmp, "dns-cleanup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	etcDir := filepath.Join(tmp, "etc")
	_ = os.MkdirAll(etcDir, 0755)
	cfgData := "VPS_NETBIRD_IP=\"" + u.Hostname() + "\"\nVPS_PUBLIC_IP=\"174.128.205.124\"\nAPI_KEY=\"k\"\nPDNS_PORT=\"" + u.Port() + "\"\nAGENT_PORT=\"" + u.Port() + "\"\n"
	if err := os.WriteFile(filepath.Join(etcDir, "edge-dns.conf"), []byte(cfgData), 0600); err != nil {
		t.Fatal(err)
	}

	mgr := New(&config.Config{WdevEtc: etcDir}, st)
	count, err := mgr.DeleteDomainDNSRecords("panel.erposoft.my.id")
	if err != nil {
		t.Fatalf("DeleteDomainDNSRecords failed: %v", err)
	}
	if count != 5 {
		t.Fatalf("expected 5 matched RRsets for panel.erposoft.my.id, got %d (patch=%s)", count, lastPatch)
	}
	for _, want := range []string{
		`"name":"panel.erposoft.my.id."`,
		`"name":"www.panel.erposoft.my.id."`,
		`"name":"_acme-challenge.panel.erposoft.my.id."`,
		`"changetype":"DELETE"`,
	} {
		if !strings.Contains(lastPatch, want) {
			t.Fatalf("expected DELETE patch to contain %q, got:\n%s", want, lastPatch)
		}
	}
	for _, untouched := range []string{"lara.erposoft.my.id.", `"name":"erposoft.my.id."`, `"type":"MX"`, `"type":"SOA"`} {
		if strings.Contains(lastPatch, untouched) {
			t.Fatalf("DELETE patch must NOT delete unrelated record %q:\n%s", untouched, lastPatch)
		}
	}
}

func TestRemoveLocalVhostAliasNeverCreatesEmptyServerName(t *testing.T) {
	tmp := t.TempDir()
	avail := filepath.Join(tmp, "sites-available")
	_ = os.MkdirAll(avail, 0755)

	// 1. Standalone vhost untuk panel.erposoft.my.id (domain utama = panel.erposoft.my.id)
	panelPath := filepath.Join(avail, "panel.erposoft.my.id")
	panelContent := `server {
    listen 80;
    server_name panel.erposoft.my.id;
    location / { proxy_pass http://127.0.0.1:3200; }
}
`
	if err := os.WriteFile(panelPath, []byte(panelContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 2. Vhost lokal yang memiliki alias domain publik: appsone.test punya alias panel.erposoft.my.id
	appsPath := filepath.Join(avail, "appsone.test")
	appsContent := `server {
    listen 80;
    server_name appsone.test panel.erposoft.my.id;
    location / { return 200 "ok"; }
}
`
	if err := os.WriteFile(appsPath, []byte(appsContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Vhost rusak dengan "server_name ;" kosong
	brokenPath := filepath.Join(avail, "broken.test")
	brokenContent := `server {
    listen 80;
    server_name ;
    location / { return 200 "broken"; }
}
`
	if err := os.WriteFile(brokenPath, []byte(brokenContent), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := New(&config.Config{NginxSitesAvail: avail}, nil)
	mgr.removeLocalVhostAlias("panel.erposoft.my.id")

	// Pastikan domain utama panel.erposoft.my.id TIDAK dihapus dan tidak menjadi "server_name ;"
	pBytes, _ := os.ReadFile(panelPath)
	pStr := string(pBytes)
	if !strings.Contains(pStr, "server_name panel.erposoft.my.id;") {
		t.Fatalf("expected panel primary domain to be preserved, got:\n%s", pStr)
	}
	if strings.Contains(pStr, "server_name ;") {
		t.Fatalf("primary domain was stripped to invalid empty server_name:\n%s", pStr)
	}

	// Pastikan alias pada appsone.test terhapus namun nama utamanya tetap appsone.test
	aBytes, _ := os.ReadFile(appsPath)
	aStr := string(aBytes)
	if !strings.Contains(aStr, "server_name appsone.test;") {
		t.Fatalf("expected appsone.test alias removed, got:\n%s", aStr)
	}

	// Pastikan broken.test pulih dan tidak lagi "server_name ;"
	bBytes, _ := os.ReadFile(brokenPath)
	bStr := string(bBytes)
	if strings.Contains(bStr, "server_name ;") {
		t.Fatalf("expected broken.test empty server_name to be healed, got:\n%s", bStr)
	}
	if !strings.Contains(bStr, "server_name broken.test;") {
		t.Fatalf("expected broken.test to have server_name broken.test;, got:\n%s", bStr)
	}
}
