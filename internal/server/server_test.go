package server

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wdev/internal/auth"
	"wdev/internal/config"
	"wdev/internal/installer"
	"wdev/internal/pdns"
	"wdev/internal/runner"
	"wdev/internal/serviceconfig"
	"wdev/internal/store"
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
	srv.mux.HandleFunc("/api/tasks", srv.handleTasks)
	srv.mux.HandleFunc("/api/env", srv.handleEnv)
	srv.mux.HandleFunc("/api/updater/status", srv.handleUpdaterStatus)
	srv.mux.HandleFunc("/api/", srv.handleAPINotFound)
	return srv
}

func TestNewControlPlaneEndpoints(t *testing.T) {
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{HTTPHost: "127.0.0.1", HTTPPort: "3200"}
	run := runner.New(db)
	inst := installer.New(cfg, db)
	srv := &Server{
		cfg:       cfg,
		store:     db,
		runner:    run,
		installer: inst,
		svcCfgMgr: serviceconfig.New(inst.GetSudoPassword),
		mux:       http.NewServeMux(),
	}
	srv.mux.HandleFunc("/api/tasks", srv.handleTasks)
	srv.mux.HandleFunc("/api/env", srv.handleEnv)
	srv.mux.HandleFunc("/api/updater/status", srv.handleUpdaterStatus)
	srv.mux.HandleFunc("/api/", srv.handleAPINotFound)

	// GET /api/tasks
	req := httptest.NewRequest(http.MethodGet, "/api/tasks?limit=10", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/tasks, got %d", rec.Code)
	}
	var taskResp struct {
		ActiveTask any                `json:"active_task"`
		Tasks      []store.TaskRecord `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &taskResp); err != nil {
		t.Fatalf("invalid json from /api/tasks: %v", err)
	}
	if taskResp.Tasks == nil {
		t.Fatalf("expected non-nil tasks slice")
	}

	// POST /api/tasks -> 405
	req = httptest.NewRequest(http.MethodPost, "/api/tasks", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST /api/tasks, got %d", rec.Code)
	}

	// GET /api/env
	req = httptest.NewRequest(http.MethodGet, "/api/env", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/env, got %d", rec.Code)
	}

	// GET /api/updater/status
	req = httptest.NewRequest(http.MethodGet, "/api/updater/status", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/updater/status, got %d", rec.Code)
	}

	// Unknown /api/ route -> 404 JSON
	req = httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown /api/ route, got %d", rec.Code)
	}
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

func TestResolveInstallTaskNetbirdIndependent(t *testing.T) {
	srv := newTestServer()

	taskName, fn, ok := srv.resolveInstallTask("netbird", "")
	if !ok || fn == nil {
		t.Fatalf("expected netbird component to resolve to an install task")
	}
	if taskName != "NetBird Mesh VPN" {
		t.Fatalf("taskName = %q, want %q", taskName, "NetBird Mesh VPN")
	}

	if _, _, ok := srv.resolveInstallTask("does-not-exist", ""); ok {
		t.Fatalf("expected unknown component to return ok=false")
	}
}

func TestTasksAndSSEReplayMidProcess(t *testing.T) {
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "sse.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{HTTPHost: "127.0.0.1", HTTPPort: "3200"}
	run := runner.New(db)
	srv := &Server{
		cfg:       cfg,
		store:     db,
		runner:    run,
		installer: installer.New(cfg, db),
		mux:       http.NewServeMux(),
	}
	srv.mux.HandleFunc("/api/tasks", srv.handleTasks)
	srv.mux.HandleFunc("/api/tasks/events", srv.handleSSEEvents)

	started := make(chan struct{})
	release := make(chan struct{})
	_, err = run.StartNativeTask("Simulasi Tugas", func(ctx context.Context, cb func(string)) error {
		cb("langkah-awal")
		close(started)
		<-release
		return nil
	})
	if err != nil {
		t.Fatalf("StartNativeTask failed: %v", err)
	}
	<-started

	// 1. GET /api/tasks must include accumulated active log_output even before FinishTask
	req := httptest.NewRequest(http.MethodGet, "/api/tasks?limit=10", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/tasks status = %d, want 200", rec.Code)
	}
	var tasksPayload struct {
		ActiveTask *struct {
			ID        int64  `json:"id"`
			Command   string `json:"command"`
			LogOutput string `json:"log_output"`
		} `json:"active_task"`
		Tasks []store.TaskRecord `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tasksPayload); err != nil {
		t.Fatalf("invalid JSON from /api/tasks: %v", err)
	}
	if tasksPayload.ActiveTask == nil || !strings.Contains(tasksPayload.ActiveTask.LogOutput, "langkah-awal") {
		t.Fatalf("expected active_task.log_output to contain langkah-awal, got %+v", tasksPayload.ActiveTask)
	}
	if len(tasksPayload.Tasks) == 0 || !strings.Contains(tasksPayload.Tasks[0].LogOutput, "langkah-awal") {
		t.Fatalf("expected running task in tasks list to contain langkah-awal, got %+v", tasksPayload.Tasks)
	}

	// 2. Reconnecting to /api/tasks/events must replay missed start + log events immediately
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sseReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/tasks/events", nil)
	resp, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatalf("SSE connect failed: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var seenPing, seenStart, seenLog bool
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		var evt struct {
			Type       string `json:"type"`
			Message    string `json:"message"`
			InstanceID string `json:"instance_id"`
			Replay     bool   `json:"replay"`
		}
		if err := json.Unmarshal([]byte(raw), &evt); err != nil {
			continue
		}
		if evt.Type == "ping" && evt.InstanceID != "" {
			seenPing = true
		} else if evt.Type == "start" && evt.Replay {
			seenStart = true
		} else if evt.Type == "log" && evt.Message == "langkah-awal" && evt.Replay {
			seenLog = true
			break
		}
	}
	close(release)
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if run.GetActiveTask() == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !seenPing || !seenStart || !seenLog {
		t.Fatalf("missing SSE replay events: ping=%v start=%v log=%v", seenPing, seenStart, seenLog)
	}
}

func TestSecurityHardeningMiddlewaresAndEndpoints(t *testing.T) {
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "sec.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{HTTPHost: "127.0.0.1", HTTPPort: "3200", WdevEtc: dir}
	authMgr := auth.New(cfg, db)
	_ = authMgr.SetSecurityEntrance("secret123")
	pdnsMgr := pdns.New(cfg, db)
	inst := installer.New(cfg, db)
	srv := New(cfg, db, runner.New(db), inst, nil, nil, nil, pdnsMgr, nil, authMgr)
	handler := srv.wrapSecurityHeaders(srv.wrapSecurityEntrance(srv.wrapAuth(srv.wrapCSRF(srv.mux))))

	// 1. Security Entrance harus tetap memblokir /api/...foo.js (mengembalikan 404, bukan 401)
	req := httptest.NewRequest(http.MethodGet, "/api/sites/evil.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for /api/sites/evil.js without entrance, got %d", rec.Code)
	}
	// Security headers harus ada
	if rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing security headers: %+v", rec.Header())
	}

	// 2. Halaman denied entrance tidak boleh membocorkan nama panel
	reqRoot := httptest.NewRequest(http.MethodGet, "/", nil)
	recRoot := httptest.NewRecorder()
	handler.ServeHTTP(recRoot, reqRoot)
	if recRoot.Code != http.StatusNotFound || strings.Contains(recRoot.Body.String(), "wdev") {
		t.Fatalf("entrance denied page leaked panel identity or wrong status (%d)", recRoot.Code)
	}

	// Buat sesi valid untuk menguji proteksi endpoint terautentikasi
	token, err := authMgr.CreateSession("admin")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	sessionCookie := &http.Cookie{Name: auth.CookieSessionName, Value: token}

	// 3. CSRF protection menolak cross-origin POST dengan cookie
	reqCSRF := httptest.NewRequest(http.MethodPost, "/api/mode", strings.NewReader(`{"mode":"vps"}`))
	reqCSRF.Host = "panel.example.com:3200"
	reqCSRF.Header.Set("Origin", "https://evil.example.net")
	reqCSRF.AddCookie(sessionCookie)
	recCSRF := httptest.NewRecorder()
	handler.ServeHTTP(recCSRF, reqCSRF)
	if recCSRF.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin POST, got %d", recCSRF.Code)
	}

	// 4. /api/updater/apply menolak source_dir lewat Web API
	reqUpd := httptest.NewRequest(http.MethodPost, "/api/updater/apply", strings.NewReader(`{"source_dir":"/tmp/evil"}`))
	reqUpd.Host = "localhost:3200"
	reqUpd.Header.Set("Origin", "http://localhost:3200")
	reqUpd.AddCookie(sessionCookie)
	recUpd := httptest.NewRecorder()
	handler.ServeHTTP(recUpd, reqUpd)
	if recUpd.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for source_dir via HTTP API, got %d (%s)", recUpd.Code, recUpd.Body.String())
	}

	// 5. /api/service/sshd/stop ditolak oleh whitelist layanan
	reqSvc := httptest.NewRequest(http.MethodPost, "/api/service/sshd/stop", nil)
	reqSvc.Host = "localhost:3200"
	reqSvc.Header.Set("Origin", "http://localhost:3200")
	reqSvc.AddCookie(sessionCookie)
	recSvc := httptest.NewRecorder()
	handler.ServeHTTP(recSvc, reqSvc)
	if recSvc.Code != http.StatusInternalServerError {
		t.Fatalf("expected error for non-managed service sshd, got %d", recSvc.Code)
	}

	// 6. /api/install menolak tema zsh dengan karakter injeksi
	reqZsh := httptest.NewRequest(http.MethodPost, "/api/install", strings.NewReader(`{"component":"zsh","version":"x\"; id #"}`))
	reqZsh.Host = "localhost:3200"
	reqZsh.Header.Set("Origin", "http://localhost:3200")
	reqZsh.AddCookie(sessionCookie)
	recZsh := httptest.NewRecorder()
	handler.ServeHTTP(recZsh, reqZsh)
	if recZsh.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malicious zsh theme, got %d (%s)", recZsh.Code, recZsh.Body.String())
	}
}
