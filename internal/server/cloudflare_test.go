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

	// 5. Hapus profil token via POST /api/cloudflare/profiles/delete
	bodyDel, _ := json.Marshal(map[string]string{"name": "kantor"})
	reqDel := httptest.NewRequest(http.MethodPost, "/api/cloudflare/profiles/delete", bytes.NewReader(bodyDel))
	recDel := httptest.NewRecorder()
	srv.mux.ServeHTTP(recDel, reqDel)
	if recDel.Code != http.StatusOK {
		t.Fatalf("POST /api/cloudflare/profiles/delete = %d: %s", recDel.Code, recDel.Body.String())
	}
}
