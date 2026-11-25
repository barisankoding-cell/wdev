package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"wdev/internal/cloudflare"
	"wdev/internal/config"
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
}
