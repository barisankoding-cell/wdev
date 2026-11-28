package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"wdev/internal/auth"
	"wdev/internal/cloudflare"
	"wdev/internal/config"
	"wdev/internal/installer"
	"wdev/internal/runner"
	"wdev/internal/site"
	"wdev/internal/store"
)

func TestCloudflareMultiProfileAPIHandlers(t *testing.T) {
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "cf-api.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{WdevEtc: dir, HTTPHost: "127.0.0.1", HTTPPort: "3200"}
	cfMgr := cloudflare.New(cfg, db)
	srv := &Server{
		cfg:   cfg,
		store: db,
		cfMgr: cfMgr,
		mux:   http.NewServeMux(),
	}
	srv.routes()

	// 1. Daftar profil token via POST /api/cloudflare/profiles/token
	tokenRaw := base64.StdEncoding.EncodeToString([]byte(`{"a":"acct-123","t":"11111111-1111-1111-1111-111111111111","s":"secret"}`))
	bodyToken, _ := json.Marshal(map[string]any{
		"name":       "kantor",
		"token":      "cloudflared service install " + tokenRaw,
		"auto_start": false,
	})
	reqTok := httptest.NewRequest(http.MethodPost, "/api/cloudflare/profiles/token", bytes.NewReader(bodyToken))
	recTok := httptest.NewRecorder()
	srv.mux.ServeHTTP(recTok, reqTok)
	if recTok.Code != http.StatusOK {
		t.Fatalf("POST /api/cloudflare/profiles/token status = %d: %s", recTok.Code, recTok.Body.String())
	}

	// 2. GET /api/cloudflare/status harus memuat profil "kantor" dengan detail mode token
	reqSt := httptest.NewRequest(http.MethodGet, "/api/cloudflare/status", nil)
	recSt := httptest.NewRecorder()
	srv.mux.ServeHTTP(recSt, reqSt)
	if recSt.Code != http.StatusOK {
		t.Fatalf("GET /api/cloudflare/status = %d", recSt.Code)
	}
	var st cloudflare.TunnelStatus
	if err := json.Unmarshal(recSt.Body.Bytes(), &st); err != nil {
		t.Fatalf("unmarshal TunnelStatus: %v", err)
	}
	if len(st.ProfileDetails) != 1 || st.ProfileDetails[0].Name != "kantor" || st.ProfileDetails[0].AuthMode != "token" {
		t.Fatalf("unexpected ProfileDetails: %+v", st.ProfileDetails)
	}

	// 3. Expose pada profil token harus ditolak (400) dengan pesan penjelasan Zero Trust Dashboard
	bodyExp, _ := json.Marshal(map[string]any{
		"local_domain": "app.test",
		"public_host":  "app.example.com",
		"profile":      "kantor",
	})
	reqExp := httptest.NewRequest(http.MethodPost, "/api/cloudflare/expose", bytes.NewReader(bodyExp))
	recExp := httptest.NewRecorder()
	srv.mux.ServeHTTP(recExp, reqExp)
	if recExp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 exposing local route onto token profile, got %d: %s", recExp.Code, recExp.Body.String())
	}

	// 4. Daftar profil cert menggunakan credentials JSON langsung (tanpa auto-start systemd di unit test)
	bodyCert, _ := json.Marshal(map[string]any{
		"name":             "prod",
		"cert_pem":         "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n-----BEGIN ARGO TUNNEL TOKEN-----\nabc\n-----END ARGO TUNNEL TOKEN-----\n",
		"credentials_json": `{"AccountTag":"acct-prod","TunnelSecret":"sec","TunnelID":"22222222-2222-2222-2222-222222222222"}`,
		"tunnel_name":      "prod-tunnel",
		"auto_start":       false,
	})
	reqCert := httptest.NewRequest(http.MethodPost, "/api/cloudflare/profiles/cert", bytes.NewReader(bodyCert))
	recCert := httptest.NewRecorder()
	srv.mux.ServeHTTP(recCert, reqCert)
	if recCert.Code != http.StatusOK {
		t.Fatalf("POST /api/cloudflare/profiles/cert = %d: %s", recCert.Code, recCert.Body.String())
	}

	// 5. Pasang API Token & cek GET /api/cloudflare/zones?profile=kantor + GET /api/cloudflare/check-host
	cfAPISrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"z1","name":"example.com","status":"active","account":{"id":"acct-123","name":"Kantor"}}],"result_info":{"page":1,"per_page":50,"total_pages":1,"count":1,"total_count":1}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/acct-123/cfd_tunnel/11111111-1111-1111-1111-111111111111/configurations":
			_, _ = w.Write([]byte(`{"success":true,"result":{"config":{"ingress":[{"service":"http_status:404"}]}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/zones/z1/dns_records":
			if r.URL.Query().Get("name") == "busy.example.com" {
				_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"rec-busy","type":"A","name":"busy.example.com","content":"198.51.100.42","proxied":false}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer cfAPISrv.Close()
	cfMgr.SetAPIBaseURL(cfAPISrv.URL)

	bodyApiTok, _ := json.Marshal(map[string]string{"name": "kantor", "api_token": "tok-api-123"})
	reqApiTok := httptest.NewRequest(http.MethodPost, "/api/cloudflare/profiles/api-token", bytes.NewReader(bodyApiTok))
	recApiTok := httptest.NewRecorder()
	srv.mux.ServeHTTP(recApiTok, reqApiTok)
	if recApiTok.Code != http.StatusOK {
		t.Fatalf("POST /api/cloudflare/profiles/api-token = %d: %s", recApiTok.Code, recApiTok.Body.String())
	}

	reqZones := httptest.NewRequest(http.MethodGet, "/api/cloudflare/zones?profile=kantor", nil)
	recZones := httptest.NewRecorder()
	srv.mux.ServeHTTP(recZones, reqZones)
	if recZones.Code != http.StatusOK {
		t.Fatalf("GET /api/cloudflare/zones = %d: %s", recZones.Code, recZones.Body.String())
	}
	var zResp struct {
		Zones []cloudflare.CloudflareZone `json:"zones"`
	}
	if err := json.Unmarshal(recZones.Body.Bytes(), &zResp); err != nil || len(zResp.Zones) != 1 || zResp.Zones[0].Name != "example.com" {
		t.Fatalf("unexpected zones response: %v / %s", err, recZones.Body.String())
	}

	reqBusy := httptest.NewRequest(http.MethodGet, "/api/cloudflare/check-host?profile=kantor&host=busy.example.com&local_domain=app.test", nil)
	recBusy := httptest.NewRecorder()
	srv.mux.ServeHTTP(recBusy, reqBusy)
	if recBusy.Code != http.StatusOK {
		t.Fatalf("GET /api/cloudflare/check-host busy = %d: %s", recBusy.Code, recBusy.Body.String())
	}
	var busyRes cloudflare.CFHostCheckResult
	if err := json.Unmarshal(recBusy.Body.Bytes(), &busyRes); err != nil {
		t.Fatalf("unmarshal busy check-host: %v", err)
	}
	if busyRes.Available || busyRes.ConflictSource != "dns" {
		t.Fatalf("expected busy.example.com conflict_source=dns, got: %+v", busyRes)
	}

	reqFree := httptest.NewRequest(http.MethodGet, "/api/cloudflare/check-host?profile=kantor&host=free.example.com&local_domain=app.test", nil)
	recFree := httptest.NewRecorder()
	srv.mux.ServeHTTP(recFree, reqFree)
	if recFree.Code != http.StatusOK {
		t.Fatalf("GET /api/cloudflare/check-host free = %d: %s", recFree.Code, recFree.Body.String())
	}
	var freeRes cloudflare.CFHostCheckResult
	if err := json.Unmarshal(recFree.Body.Bytes(), &freeRes); err != nil {
		t.Fatalf("unmarshal free check-host: %v", err)
	}
	if !freeRes.Available || freeRes.ZoneName != "example.com" {
		t.Fatalf("expected free.example.com available in zone example.com, got: %+v", freeRes)
	}

	// 6. Hapus profil token via POST /api/cloudflare/profiles/delete
	bodyDel, _ := json.Marshal(map[string]string{"name": "kantor"})
	reqDel := httptest.NewRequest(http.MethodPost, "/api/cloudflare/profiles/delete", bytes.NewReader(bodyDel))
	recDel := httptest.NewRecorder()
	srv.mux.ServeHTTP(recDel, reqDel)
	if recDel.Code != http.StatusOK {
		t.Fatalf("POST /api/cloudflare/profiles/delete = %d: %s", recDel.Code, recDel.Body.String())
	}

	// 7. Daftar profil token & cert dengan name kosong -> otomatis pakai email / nama akun
	autoAPISrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Header.Get("Authorization") == "Bearer api-tok-email" && r.URL.Path == "/user":
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"u99","email":"auto.user@example.com"}}`))
		case r.Header.Get("Authorization") == "Bearer api-tok-zone" && r.URL.Path == "/zones":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"z99","name":"studioku.dev","status":"active","account":{"id":"acct-studio","name":"Studio Dev's Account"}}]}`))
		default:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Forbidden"}]}`))
		}
	}))
	defer autoAPISrv.Close()
	cfMgr.SetAPIBaseURL(autoAPISrv.URL)

	autoTokBody, _ := json.Marshal(map[string]any{
		"token":      tokenRaw,
		"api_token":  "api-tok-email",
		"auto_start": false,
	})
	reqAutoTok := httptest.NewRequest(http.MethodPost, "/api/cloudflare/profiles/token", bytes.NewReader(autoTokBody))
	recAutoTok := httptest.NewRecorder()
	srv.mux.ServeHTTP(recAutoTok, reqAutoTok)
	if recAutoTok.Code != http.StatusOK {
		t.Fatalf("POST /api/cloudflare/profiles/token auto = %d: %s", recAutoTok.Code, recAutoTok.Body.String())
	}
	var autoTokResp struct {
		Profile cloudflare.ProfileInfo `json:"profile"`
	}
	if err := json.Unmarshal(recAutoTok.Body.Bytes(), &autoTokResp); err != nil {
		t.Fatalf("unmarshal auto token resp: %v", err)
	}
	if autoTokResp.Profile.Name != "auto-user-example-com" {
		t.Fatalf("expected auto token profile auto-user-example-com, got %q", autoTokResp.Profile.Name)
	}

	autoCertBody, _ := json.Marshal(map[string]any{
		"cert_pem":         "-----BEGIN ARGO TUNNEL TOKEN-----\nabc\n-----END ARGO TUNNEL TOKEN-----\n",
		"credentials_json": `{"AccountTag":"acct-studio","TunnelSecret":"sec","TunnelID":"33333333-3333-3333-3333-333333333333"}`,
		"api_token":        "api-tok-zone",
		"auto_start":       false,
	})
	reqAutoCert := httptest.NewRequest(http.MethodPost, "/api/cloudflare/profiles/cert", bytes.NewReader(autoCertBody))
	recAutoCert := httptest.NewRecorder()
	srv.mux.ServeHTTP(recAutoCert, reqAutoCert)
	if recAutoCert.Code != http.StatusOK {
		t.Fatalf("POST /api/cloudflare/profiles/cert auto = %d: %s", recAutoCert.Code, recAutoCert.Body.String())
	}
	var autoCertResp struct {
		Profile cloudflare.ProfileInfo `json:"profile"`
	}
	if err := json.Unmarshal(recAutoCert.Body.Bytes(), &autoCertResp); err != nil {
		t.Fatalf("unmarshal auto cert resp: %v", err)
	}
	if autoCertResp.Profile.Name != "studio-dev" {
		t.Fatalf("expected auto cert profile studio-dev, got %q", autoCertResp.Profile.Name)
	}
}

func TestPanelDomainCloudflareDetectionAndBind(t *testing.T) {
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "cf-panel.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{
		HTTPHost: "127.0.0.1",
		HTTPPort: "3200",
		WdevEtc:  filepath.Join(dir, "etc"),
	}
	cfMgr := cloudflare.New(cfg, db)
	run := runner.New(db)
	inst := installer.New(cfg, db)
	siteMgr := site.New(cfg, db)
	siteMgr.SetSkipNginxExec(true)
	authMgr := auth.New(cfg, db)

	srv := New(cfg, db, run, inst, siteMgr, nil, cfMgr, nil, nil, authMgr)
	srv.installer = nil
	handler := srv.wrapSecurityHeaders(srv.wrapPanelDomain(srv.wrapSecurityEntrance(srv.wrapAuth(srv.wrapCSRF(srv.mux)))))

	sessTok, err := authMgr.CreateSession("admin")
	if err != nil {
		t.Fatal(err)
	}
	sessCookie := &http.Cookie{Name: auth.CookieSessionName, Value: sessTok}

	failDNS := true
	var ingressRules []map[string]any
	var dnsCreated bool
	var dnsDeleted bool

	cfAPISrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"z1","name":"example.com","status":"active","account":{"id":"acct-123","name":"Kantor"}}],"result_info":{"page":1,"per_page":50,"total_pages":1,"count":1,"total_count":1}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/accounts/acct-123/cfd_tunnel/11111111-1111-1111-1111-111111111111/configurations":
			if len(ingressRules) == 0 {
				ingressRules = []map[string]any{{"service": "http_status:404"}}
			}
			b, _ := json.Marshal(map[string]any{"success": true, "result": map[string]any{"config": map[string]any{"ingress": ingressRules}}})
			_, _ = w.Write(b)
		case r.Method == http.MethodPut && r.URL.Path == "/accounts/acct-123/cfd_tunnel/11111111-1111-1111-1111-111111111111/configurations":
			var body struct {
				Config struct {
					Ingress []map[string]any `json:"ingress"`
				} `json:"config"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			ingressRules = body.Config.Ingress
			b, _ := json.Marshal(map[string]any{"success": true, "result": map[string]any{"config": body.Config}})
			_, _ = w.Write(b)
		case r.Method == http.MethodGet && r.URL.Path == "/zones/z1/dns_records":
			if dnsCreated && !dnsDeleted {
				_, _ = w.Write([]byte(`{"success":true,"result":[{"id":"rec-panel","type":"CNAME","name":"panel.example.com","content":"11111111-1111-1111-1111-111111111111.cfargotunnel.com","proxied":true}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/zones/z1/dns_records":
			if failDNS {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":9109,"message":"Token lacks DNS:Edit"}]}`))
				return
			}
			dnsCreated = true
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"rec-panel"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/zones/z1/dns_records/rec-panel":
			dnsDeleted = true
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"rec-panel"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer cfAPISrv.Close()
	cfMgr.SetAPIBaseURL(cfAPISrv.URL)

	// 1. Sebelum ada profil CF -> GET /api/panel/domain melaporkan cf_has_api=false
	reqInit := httptest.NewRequest(http.MethodGet, "/api/panel/domain", nil)
	reqInit.Host = "127.0.0.1:3200"
	reqInit.AddCookie(sessCookie)
	recInit := httptest.NewRecorder()
	handler.ServeHTTP(recInit, reqInit)
	if recInit.Code != http.StatusOK || !strings.Contains(recInit.Body.String(), `"cf_has_api":false`) {
		t.Fatalf("GET initial = %d: %s", recInit.Code, recInit.Body.String())
	}

	// 2. Daftarkan profil token + API token
	tokenRaw := base64.StdEncoding.EncodeToString([]byte(`{"a":"acct-123","t":"11111111-1111-1111-1111-111111111111","s":"secret"}`))
	autoStart := false
	if _, err := cfMgr.RegisterTokenProfile(cloudflare.TokenProfileOptions{
		Name:      "kantor",
		Token:     tokenRaw,
		APIToken:  "api-tok-panel",
		AutoStart: &autoStart,
	}); err != nil {
		t.Fatalf("RegisterTokenProfile: %v", err)
	}

	reqDetect := httptest.NewRequest(http.MethodGet, "/api/panel/domain", nil)
	reqDetect.Host = "127.0.0.1:3200"
	reqDetect.AddCookie(sessCookie)
	recDetect := httptest.NewRecorder()
	handler.ServeHTTP(recDetect, reqDetect)
	if recDetect.Code != http.StatusOK || !strings.Contains(recDetect.Body.String(), `"cf_has_api":true`) || !strings.Contains(recDetect.Body.String(), `"kantor"`) {
		t.Fatalf("GET with CF+API = %d: %s", recDetect.Code, recDetect.Body.String())
	}

	// 3. Jika DNS CNAME gagal saat block_direct_ip=true -> harus rollback (anti lockout)
	reqBindFail := httptest.NewRequest(http.MethodPost, "/api/panel/domain", strings.NewReader(`{"domain":"panel.example.com","ssl_mode":"cloudflare","cf_profile":"kantor","block_direct_ip":true}`))
	reqBindFail.Host = "127.0.0.1:3200"
	reqBindFail.Header.Set("Origin", "http://127.0.0.1:3200")
	reqBindFail.AddCookie(sessCookie)
	recBindFail := httptest.NewRecorder()
	handler.ServeHTTP(recBindFail, reqBindFail)
	if recBindFail.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when DNS fails with block_direct_ip=true, got %d (%s)", recBindFail.Code, recBindFail.Body.String())
	}
	if authMgr.GetPanelDomain() != "" || authMgr.IsBlockDirectIP() {
		t.Fatalf("expected panel domain rollback after DNS failure")
	}

	// 4. Bind via Cloudflare Tunnel + API sukses
	failDNS = false
	reqBind := httptest.NewRequest(http.MethodPost, "/api/panel/domain", strings.NewReader(`{"domain":"panel.example.com","ssl_mode":"cloudflare","cf_profile":"kantor","block_direct_ip":true}`))
	reqBind.Host = "127.0.0.1:3200"
	reqBind.Header.Set("Origin", "http://127.0.0.1:3200")
	reqBind.AddCookie(sessCookie)
	recBind := httptest.NewRecorder()
	handler.ServeHTTP(recBind, reqBind)
	if recBind.Code != http.StatusOK || !strings.Contains(recBind.Body.String(), `"bound"`) || !strings.Contains(recBind.Body.String(), `"ssl_provider":"cloudflare"`) {
		t.Fatalf("POST bind cloudflare = %d: %s", recBind.Code, recBind.Body.String())
	}
	if !dnsCreated {
		t.Fatalf("expected Cloudflare DNS CNAME to be created")
	}
	dom, sslOn, blockIP := authMgr.PanelDomainConfig()
	if dom != "panel.example.com" || !sslOn || !blockIP {
		t.Fatalf("unexpected PanelDomainConfig: dom=%q ssl=%v blockIP=%v", dom, sslOn, blockIP)
	}

	// Pastikan ingress mengarah langsung ke http://127.0.0.1:3200
	foundPanelRule := false
	for _, rule := range ingressRules {
		if h, _ := rule["hostname"].(string); h == "panel.example.com" {
			if svc, _ := rule["service"].(string); svc == "http://127.0.0.1:3200" {
				foundPanelRule = true
			}
		}
	}
	if !foundPanelRule {
		t.Fatalf("expected ingress rule panel.example.com -> http://127.0.0.1:3200, got: %#v", ingressRules)
	}

	// 5. Akses langsung via IP ditolak 403, akses lewat cloudflared loopback HTTPS diizinkan
	reqDirectIP := httptest.NewRequest(http.MethodGet, "/api/panel/domain", nil)
	reqDirectIP.Host = "103.171.85.72:3200"
	reqDirectIP.AddCookie(sessCookie)
	recDirectIP := httptest.NewRecorder()
	handler.ServeHTTP(recDirectIP, reqDirectIP)
	if recDirectIP.Code != http.StatusForbidden {
		t.Fatalf("expected 403 direct IP after CF bind, got %d", recDirectIP.Code)
	}

	reqCFPanel := httptest.NewRequest(http.MethodGet, "/api/panel/domain", nil)
	reqCFPanel.Host = "panel.example.com"
	reqCFPanel.RemoteAddr = "127.0.0.1:54321"
	reqCFPanel.Header.Set("Cf-Visitor", `{"scheme":"https"}`)
	reqCFPanel.AddCookie(sessCookie)
	recCFPanel := httptest.NewRecorder()
	handler.ServeHTTP(recCFPanel, reqCFPanel)
	if recCFPanel.Code != http.StatusOK || !strings.Contains(recCFPanel.Body.String(), `"detected_cf_exposure"`) || !strings.Contains(recCFPanel.Body.String(), `"ssl_provider":"cloudflare"`) {
		t.Fatalf("GET via cloudflared = %d: %s", recCFPanel.Code, recCFPanel.Body.String())
	}

	// 6. Unbind menghapus ikatan panel, rute tunnel, dan record DNS CNAME Cloudflare
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/panel/domain", strings.NewReader(`{"remove_vhost":true}`))
	reqDel.Host = "panel.example.com"
	reqDel.RemoteAddr = "127.0.0.1:54321"
	reqDel.Header.Set("Cf-Visitor", `{"scheme":"https"}`)
	reqDel.Header.Set("Origin", "https://panel.example.com")
	reqDel.AddCookie(sessCookie)
	recDel := httptest.NewRecorder()
	handler.ServeHTTP(recDel, reqDel)
	if recDel.Code != http.StatusOK || !dnsDeleted || !strings.Contains(recDel.Body.String(), `"dns_records_deleted":1`) {
		t.Fatalf("DELETE cloudflare panel domain = %d (dnsDeleted=%v): %s", recDel.Code, dnsDeleted, recDel.Body.String())
	}

	// 7. Simulasikan rute Cloudflare manual langsung ke http://127.0.0.1:3200 sebelum bind -> GET harus mendeteksinya
	dnsDeleted = false
	if _, err := cfMgr.ExposeWithOptions(cloudflare.ExposeOptions{
		LocalDomain: "panel.example.com",
		PublicHost:  "panel.example.com",
		Profile:     "kantor",
		ServiceURL:  "http://127.0.0.1:3200",
	}); err != nil {
		t.Fatalf("ExposeWithOptions manual panel route: %v", err)
	}
	reqDetManual := httptest.NewRequest(http.MethodGet, "/api/panel/domain", nil)
	reqDetManual.Host = "127.0.0.1:3200"
	reqDetManual.AddCookie(sessCookie)
	recDetManual := httptest.NewRecorder()
	handler.ServeHTTP(recDetManual, reqDetManual)
	if recDetManual.Code != http.StatusOK || !strings.Contains(recDetManual.Body.String(), `"public_host":"panel.example.com"`) {
		t.Fatalf("expected detected_cf_exposure for direct port 3200 route, got %d: %s", recDetManual.Code, recDetManual.Body.String())
	}
}
