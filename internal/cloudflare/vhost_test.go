package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"wdev/internal/store"

	"gopkg.in/yaml.v3"
)

func newTestCFManagerWithNginx(t *testing.T) (*Manager, string, string) {
	t.Helper()
	mgr, dir := newTestCFManager(t)
	sitesAvail := filepath.Join(dir, "nginx", "sites-available")
	if err := os.MkdirAll(sitesAvail, 0755); err != nil {
		t.Fatalf("mkdir sitesAvail: %v", err)
	}
	if err := os.MkdirAll(mgr.localCertsDir, 0755); err != nil {
		t.Fatalf("mkdir localCertsDir: %v", err)
	}
	mgr.cfg.NginxSitesAvail = sitesAvail
	return mgr, dir, sitesAvail
}

func registerTestCertProfile(t *testing.T, mgr *Manager, name, tunnelID string) {
	t.Helper()
	mgr.runCmdFn = func(ctx context.Context, env []string, bin string, args ...string) ([]byte, error) {
		for i, a := range args {
			if a == "--cred-file" && i+1 < len(args) {
				cred := `{"AccountTag":"acct-test","TunnelSecret":"c2Vj","TunnelID":"` + tunnelID + `"}`
				_ = os.WriteFile(args[i+1], []byte(cred), 0600)
			}
		}
		return []byte("OK"), nil
	}
	autoFalse := false
	if _, err := mgr.RegisterCertProfile(CertProfileOptions{
		Name:       name,
		CertPEM:    "-----BEGIN ARGO TUNNEL TOKEN-----\nabc\n-----END ARGO TUNNEL TOKEN-----\n",
		TunnelName: "tun-" + name,
		AutoStart:  &autoFalse,
	}); err != nil {
		t.Fatalf("RegisterCertProfile(%s): %v", name, err)
	}
}

func TestExposeAdaptsLaravelSSLVHostAndClearsHTTPHostHeader(t *testing.T) {
	mgr, dir, sitesAvail := newTestCFManagerWithNginx(t)
	registerTestCertProfile(t, mgr, "utama", "11111111-1111-1111-1111-111111111111")

	// Simulasikan cert lokal dan vhost Nginx Laravel SSL seperti appsone.test
	if err := os.WriteFile(filepath.Join(mgr.localCertsDir, "appsone.test.pem"), []byte("CERT"), 0644); err != nil {
		t.Fatalf("write local cert: %v", err)
	}
	vhostConf := `server {
    listen 80;
    listen [::]:80;
    server_name appsone.test;

    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name appsone.test;

    ssl_certificate /etc/ssl/local-certs/appsone.test.pem;
    ssl_certificate_key /etc/ssl/local-certs/appsone.test-key.pem;
    root /home/rhee/Projects/php/appsone/public;

    location ~ \.php$ {
        set $fe_https $https;
        set $fe_scheme $scheme;
        set $fe_port $server_port;
        if ($http_x_forwarded_proto ~* "^https(,|$)") {
            set $fe_https on;
            set $fe_scheme https;
            set $fe_port 443;
        }
        include fastcgi_params;
        fastcgi_param HTTPS $fe_https if_not_empty;
        fastcgi_param REQUEST_SCHEME $fe_scheme;
        fastcgi_param SERVER_PORT $fe_port;
        fastcgi_pass unix:/run/php-fpm/www.sock;
    }
}
`
	vhostPath := filepath.Join(sitesAvail, "appsone.test")
	if err := os.WriteFile(vhostPath, []byte(vhostConf), 0644); err != nil {
		t.Fatalf("write vhost: %v", err)
	}

	res, err := mgr.ExposeWithOptions(ExposeOptions{
		LocalDomain: "appsone.test",
		PublicHost:  "lara.azqilabs.com",
		Profile:     "utama",
	})
	if err != nil {
		t.Fatalf("ExposeWithOptions: %v", err)
	}
	if res.ServiceURL != "https://127.0.0.1:443" {
		t.Fatalf("expected ServiceURL https://127.0.0.1:443, got %q", res.ServiceURL)
	}

	// 1. VHost Nginx harus otomatis memuat lara.azqilabs.com di semua blok server_name dan SERVER_NAME $host
	updatedVHostBytes, err := os.ReadFile(vhostPath)
	if err != nil {
		t.Fatalf("read updated vhost: %v", err)
	}
	updatedVHost := string(updatedVHostBytes)
	if count := strings.Count(updatedVHost, "server_name appsone.test lara.azqilabs.com;"); count != 2 {
		t.Fatalf("expected both server blocks to have server_name appsone.test lara.azqilabs.com; (count=%d):\n%s", count, updatedVHost)
	}
	if !strings.Contains(updatedVHost, "fastcgi_param SERVER_NAME $host;") {
		t.Fatalf("expected fastcgi_param SERVER_NAME $host; injected:\n%s", updatedVHost)
	}

	// 2. Tunnel config YAML harus pakai noTLSVerify: true dan TIDAK menulis httpHostHeader: appsone.test
	ymlBytes, err := os.ReadFile(filepath.Join(dir, "cloudflare", "routes", "utama.yml"))
	if err != nil {
		t.Fatalf("read utama.yml: %v", err)
	}
	var parsedCfg CloudflaredConfig
	if err := yaml.Unmarshal(ymlBytes, &parsedCfg); err != nil {
		t.Fatalf("unmarshal utama.yml: %v", err)
	}
	if len(parsedCfg.Ingress) != 2 {
		t.Fatalf("expected 2 ingress rules, got %+v", parsedCfg.Ingress)
	}
	rule := parsedCfg.Ingress[0]
	if rule.Hostname != "lara.azqilabs.com" || rule.Service != "https://127.0.0.1:443" {
		t.Fatalf("unexpected ingress rule: %+v", rule)
	}
	if rule.OriginRequest == nil || !rule.OriginRequest.NoTLSVerify || rule.OriginRequest.HTTPHostHeader != "" {
		t.Fatalf("expected NoTLSVerify=true and empty HTTPHostHeader, got: %+v", rule.OriginRequest)
	}

	// 3. CheckPublicHost tetap idempoten untuk binding yang sama
	chk, err := mgr.CheckPublicHost("utama", "lara.azqilabs.com", "appsone.test")
	if err != nil || !chk.Available {
		t.Fatalf("expected CheckPublicHost idempotent available, got err=%v chk=%+v", err, chk)
	}

	// 4. Unexpose membersihkan alias lara.azqilabs.com dari Nginx tanpa menghapus appsone.test
	if _, err := mgr.UnexposeWithResult("appsone.test", "utama"); err != nil {
		t.Fatalf("UnexposeWithResult: %v", err)
	}
	afterUnexposeBytes, _ := os.ReadFile(vhostPath)
	afterUnexpose := string(afterUnexposeBytes)
	if strings.Contains(afterUnexpose, "lara.azqilabs.com") {
		t.Fatalf("expected lara.azqilabs.com removed after unexpose, got:\n%s", afterUnexpose)
	}
	if count := strings.Count(afterUnexpose, "server_name appsone.test;"); count != 2 {
		t.Fatalf("expected primary server_name appsone.test; preserved (count=%d):\n%s", count, afterUnexpose)
	}
}

func TestSyncActiveExposuresRepairsLegacyHTTPHostHeaderAndHTTPProxyRedirect(t *testing.T) {
	mgr, dir, sitesAvail := newTestCFManagerWithNginx(t)
	registerTestCertProfile(t, mgr, "akun", "22222222-2222-2222-2222-222222222222")

	// Simulasikan kondisi eksisting sebelum fix:
	// - VHost Nginx hanya punya server_name appsone.test;
	// - routes/akun.yml dan cf_routes DB masih memiliki httpHostHeader: appsone.test
	if err := os.WriteFile(filepath.Join(mgr.localCertsDir, "appsone.test.pem"), []byte("CERT"), 0644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	vhostPath := filepath.Join(sitesAvail, "appsone.test")
	vhostConf := `server {
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
        fastcgi_pass unix:/run/php-fpm/www.sock;
    }
}
`
	if err := os.WriteFile(vhostPath, []byte(vhostConf), 0644); err != nil {
		t.Fatalf("write vhost: %v", err)
	}

	legacyCfg := CloudflaredConfig{
		TunnelID:    "22222222-2222-2222-2222-222222222222",
		Credentials: mgr.credFilePath("akun"),
		Metrics:     "127.0.0.1:0",
		Ingress: []IngressRule{
			{
				Hostname: "lara.azqilabs.com",
				Service:  "https://127.0.0.1:443",
				OriginRequest: &OriginRequestConfig{
					HTTPHostHeader: "appsone.test",
					NoTLSVerify:    true,
				},
			},
			{Service: "http_status:404"},
		},
	}
	ymlRaw, _ := yaml.Marshal(&legacyCfg)
	if err := os.WriteFile(mgr.routeConfigPath("akun"), ymlRaw, 0644); err != nil {
		t.Fatalf("write akun.yml: %v", err)
	}
	if err := mgr.store.SaveCFRoute(store.CFRoute{
		ProfileName:    "akun",
		LocalDomain:    "appsone.test",
		Hostname:       "lara.azqilabs.com",
		ServiceURL:     "https://127.0.0.1:443",
		HTTPHostHeader: "appsone.test",
		NoTLSVerify:    true,
	}); err != nil {
		t.Fatalf("SaveCFRoute: %v", err)
	}

	var restartedUnits []string
	mgr.isServiceActiveFn = func(unit string) bool { return unit == "cloudflared@akun.service" }
	mgr.runPrivilegedFn = func(bin string, args ...string) (string, error) {
		if bin == "systemctl" && len(args) == 2 && args[0] == "restart" {
			restartedUnits = append(restartedUnits, args[1])
		}
		return "OK", nil
	}

	if err := mgr.SyncActiveExposures(); err != nil {
		t.Fatalf("SyncActiveExposures: %v", err)
	}

	// VHost harus sudah disesuaikan
	vhAfter, _ := os.ReadFile(vhostPath)
	if !strings.Contains(string(vhAfter), "server_name appsone.test lara.azqilabs.com;") ||
		!strings.Contains(string(vhAfter), "fastcgi_param HTTPS $fe_https if_not_empty;") ||
		!strings.Contains(string(vhAfter), "fastcgi_param SERVER_NAME $host;") {
		t.Fatalf("expected repaired Nginx vhost, got:\n%s", string(vhAfter))
	}

	// YAML dan DB tidak lagi memaksa httpHostHeader: appsone.test
	ymlAfterRaw, _ := os.ReadFile(filepath.Join(dir, "cloudflare", "routes", "akun.yml"))
	var cfgAfter CloudflaredConfig
	if err := yaml.Unmarshal(ymlAfterRaw, &cfgAfter); err != nil {
		t.Fatalf("unmarshal akun.yml after sync: %v", err)
	}
	if cfgAfter.Ingress[0].OriginRequest == nil || cfgAfter.Ingress[0].OriginRequest.HTTPHostHeader != "" || !cfgAfter.Ingress[0].OriginRequest.NoTLSVerify {
		t.Fatalf("expected empty HTTPHostHeader after sync, got: %+v", cfgAfter.Ingress[0].OriginRequest)
	}
	if st := mgr.GetStatus(); len(st.Exposures) != 1 || st.Exposures[0].HTTPHostHeader != "" {
		t.Fatalf("expected DB exposure HTTPHostHeader cleared, got: %+v", st.Exposures)
	}
	if len(restartedUnits) != 1 || restartedUnits[0] != "cloudflared@akun.service" {
		t.Fatalf("expected cloudflared@akun.service restarted once, got: %v", restartedUnits)
	}

	// Simulasikan mode HTTP redirect loop guard ketika ServiceURL memaksa http://127.0.0.1:80
	adaptedText, changed := addPublicHostToVHostText(vhostConf, "appsone.test", "lara.azqilabs.com", "http://127.0.0.1:80")
	if !changed || !strings.Contains(adaptedText, "$redirect_to_https") || strings.Contains(adaptedText, "\n    return 301 https://$host$request_uri;\n}") {
		t.Fatalf("expected HTTP origin standalone redirect converted to conditional $redirect_to_https, got:\n%s", adaptedText)
	}
}

func TestTokenModeExposeAdaptsVHostAndOmitsHTTPHostHeader(t *testing.T) {
	mgr, _, sitesAvail := newTestCFManagerWithNginx(t)
	tok := makeTestToken("acct-tok", "33333333-3333-3333-3333-333333333333", "sec-t")

	var capturedRule map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/acct-tok/cfd_tunnel/33333333-3333-3333-3333-333333333333/configurations":
			_, _ = w.Write([]byte(`{"success":true,"result":{"config":{"ingress":[{"service":"http_status:404"}]}}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/accounts/acct-tok/cfd_tunnel/33333333-3333-3333-3333-333333333333/configurations":
			var body struct {
				Config struct {
					Ingress []map[string]any `json:"ingress"`
				} `json:"config"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Config.Ingress) > 0 {
				capturedRule = body.Config.Ingress[0]
			}
			_, _ = w.Write([]byte(`{"success":true,"result":{}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"z1","name":"azqilabs.com","status":"active","account":{"id":"acct-tok","name":"Main"}}],"result_info":{"page":1,"per_page":50,"total_pages":1,"count":1,"total_count":1}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/zones/z1/dns_records":
			_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/zones/z1/dns_records":
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"rec-1"}}`))
		}
	}))
	defer srv.Close()
	mgr.SetAPIBaseURL(srv.URL)

	autoFalse := false
	if _, err := mgr.RegisterTokenProfile(TokenProfileOptions{
		Name:      "tok",
		Token:     tok,
		APIToken:  "api-tok",
		AutoStart: &autoFalse,
	}); err != nil {
		t.Fatalf("RegisterTokenProfile: %v", err)
	}

	// Reverse proxy vhost (seperti appsone-rs.test)
	proxyVHost := `server {
    listen 80;
    server_name appsone-rs.test;
    location / {
        proxy_pass http://127.0.0.1:3100;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
`
	vhostPath := filepath.Join(sitesAvail, "appsone-rs.test")
	if err := os.WriteFile(vhostPath, []byte(proxyVHost), 0644); err != nil {
		t.Fatalf("write proxy vhost: %v", err)
	}

	if _, err := mgr.ExposeWithOptions(ExposeOptions{
		LocalDomain: "appsone-rs.test",
		PublicHost:  "rs.azqilabs.com",
		Profile:     "tok",
	}); err != nil {
		t.Fatalf("ExposeWithOptions token: %v", err)
	}

	if capturedRule == nil {
		t.Fatal("expected remote tunnel rule captured")
	}
	if orig, hasOrig := capturedRule["originRequest"].(map[string]any); hasOrig {
		if hdr, ok := orig["httpHostHeader"]; ok && hdr != "" {
			t.Fatalf("expected httpHostHeader omitted on remote tunnel rule, got %+v", orig)
		}
	}
	vhBytes, _ := os.ReadFile(vhostPath)
	vhStr := string(vhBytes)
	if !strings.Contains(vhStr, "server_name appsone-rs.test rs.azqilabs.com;") ||
		!strings.Contains(vhStr, "proxy_set_header X-Forwarded-Proto $fe_proto;") {
		t.Fatalf("expected proxy vhost adapted with rs.azqilabs.com and $fe_proto, got:\n%s", vhStr)
	}

	// Overwrite dari appsone-rs.test ke vhost lain membersihkan alias lama
	otherVHost := filepath.Join(sitesAvail, "other.test")
	_ = os.WriteFile(otherVHost, []byte("server { listen 80; server_name other.test; }\n"), 0644)
	if _, err := mgr.ExposeWithOptions(ExposeOptions{
		LocalDomain: "other.test",
		PublicHost:  "rs.azqilabs.com",
		Profile:     "tok",
		Overwrite:   true,
	}); err != nil {
		t.Fatalf("ExposeWithOptions overwrite: %v", err)
	}
	vhOld, _ := os.ReadFile(vhostPath)
	if strings.Contains(string(vhOld), "rs.azqilabs.com") {
		t.Fatalf("expected rs.azqilabs.com removed from old vhost after overwrite, got:\n%s", string(vhOld))
	}
	vhNew, _ := os.ReadFile(otherVHost)
	if !strings.Contains(string(vhNew), "server_name other.test rs.azqilabs.com;") {
		t.Fatalf("expected rs.azqilabs.com added to new vhost after overwrite, got:\n%s", string(vhNew))
	}
}
