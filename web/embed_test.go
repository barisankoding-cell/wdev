package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerSPAFallbackAndStaticAssets(t *testing.T) {
	h := Handler()

	// Root returns index.html
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html for /, got %q", ct)
	}

	// SPA route without file extension falls back to index.html
	req = httptest.NewRequest(http.MethodGet, "/webstack", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for SPA route /webstack, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html for /webstack, got %q", ct)
	}

	// Missing asset with extension returns 404 instead of HTML
	req = httptest.NewRequest(http.MethodGet, "/assets/does-not-exist.js", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing asset, got %d", rec.Code)
	}

	// Unmatched /api/ path returns 404
	req = httptest.NewRequest(http.MethodGet, "/api/missing", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for /api/missing, got %d", rec.Code)
	}
}
