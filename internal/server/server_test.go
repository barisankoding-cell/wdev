package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"wdev/internal/config"
	"wdev/internal/installer"
	"wdev/internal/serviceconfig"
)

func newTestServer() *Server {
	cfg := &config.Config{HTTPHost: "127.0.0.1", HTTPPort: "3200"}
	inst := installer.New(cfg, nil)
	srv := &Server{
		cfg:       cfg,
		installer: inst,
		svcCfgMgr: serviceconfig.New(inst.GetSudoPassword),
		mux:       http.NewServeMux(),
	}
	srv.mux.HandleFunc("/api/service-config/", srv.handleServiceConfig)
	return srv
}

func TestHandleServiceConfigValidation(t *testing.T) {
	srv := newTestServer()

	// Empty service name
	req := httptest.NewRequest(http.MethodGet, "/api/service-config/", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty service, got %d", rec.Code)
	}

	// Unsupported service name
	req = httptest.NewRequest(http.MethodGet, "/api/service-config/unknown", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported service, got %d", rec.Code)
	}

	// Method not allowed
	req = httptest.NewRequest(http.MethodDelete, "/api/service-config/redis", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for DELETE, got %d", rec.Code)
	}

	// Invalid JSON body on POST
	req = httptest.NewRequest(http.MethodPost, "/api/service-config/redis", strings.NewReader("{bad"))
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid json, got %d", rec.Code)
	}

	// Unsupported inspect service
	req = httptest.NewRequest(http.MethodGet, "/api/service-config/nginx/inspect", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported inspect service, got %d", rec.Code)
	}

	// Unknown sub-action
	req = httptest.NewRequest(http.MethodGet, "/api/service-config/redis/unknown-action", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown sub-action, got %d", rec.Code)
	}
}
