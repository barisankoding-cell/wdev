package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleWorkspaceOrcaAISyncEndpoint(t *testing.T) {
	srv := newTestServer()
	srv.mux.HandleFunc("/api/workspace/orca-ai/sync", srv.handleWorkspaceOrcaAISync)

	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspace/orca-ai/sync", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET sync status: status %d body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"managed", "in_sync", "state", "profile_id", "cloud_linked"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("response missing %q: %v", key, body)
		}
	}

	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/workspace/orca-ai/sync", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE sync status: status %d", rec.Code)
	}
}
