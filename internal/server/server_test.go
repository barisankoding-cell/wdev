package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wdev/internal/auth"
	"wdev/internal/config"
	"wdev/internal/installer"
	"wdev/internal/mail"
	"wdev/internal/pdns"
	"wdev/internal/runner"
	"wdev/internal/serviceconfig"
	"wdev/internal/site"
	"wdev/internal/store"

	"github.com/gorilla/websocket"
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

	mailName, mailFn, ok := srv.resolveInstallTask("mail", "")
	if !ok || mailFn == nil || mailName == "" {
		t.Fatalf("expected mail component to resolve to an install task")
	}

	for _, comp := range []string{"pdns", "powerdns"} {
		pdnsName, pdnsFn, ok := srv.resolveInstallTask(comp, "")
		if !ok || pdnsFn == nil || pdnsName != "PowerDNS Authoritative Server" {
			t.Fatalf("expected %q to resolve to PowerDNS Authoritative Server, got (%q, %v)", comp, pdnsName, ok)
		}
	}
}

func TestBatchInstallValidationAndOrdering(t *testing.T) {
	srv := newTestServer()
	srv.mux.HandleFunc("/api/install", srv.handleInstall)

	// 1. Urutan dependensi dan deduplikasi alias (ssl & mkcert, zsh & zsh-agnoster)
	steps, needsSudo, errMsg := srv.resolveInstallSteps([]installItemPayload{
		{Component: "zsh", Version: "powerlevel10k"},
		{Component: "mkcert"},
		{Component: "php", Version: "8.4"},
		{Component: "nginx"},
		{Component: "base"},
		{Component: "ssl"},
		{Component: "node", Version: "22"},
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if !needsSudo {
		t.Fatalf("expected needsSudo=true for privileged web/runtime components")
	}
	wantOrder := []string{"base", "node", "nginx", "php", "ssl", "zsh"}
	if len(steps) != len(wantOrder) {
		t.Fatalf("got %d steps, want %d", len(steps), len(wantOrder))
	}
	for i, want := range wantOrder {
		if steps[i].canonical != want {
			t.Fatalf("step[%d] = %q, want %q", i, steps[i].canonical, want)
		}
	}

	// 2. Non-privileged only batch must not force sudo, while pdns requires sudo & runs before mail
	nonPrivSteps, nonPrivSudo, errMsg := srv.resolveInstallSteps([]installItemPayload{
		{Component: "check-updates"},
		{Component: "cf-doctor"},
	})
	if errMsg != "" || len(nonPrivSteps) != 2 || nonPrivSudo {
		t.Fatalf("unexpected non-priv batch result: steps=%d sudo=%v err=%q", len(nonPrivSteps), nonPrivSudo, errMsg)
	}

	dnsMailSteps, dnsMailSudo, errMsg := srv.resolveInstallSteps([]installItemPayload{
		{Component: "mail"},
		{Component: "powerdns"},
	})
	if errMsg != "" || !dnsMailSudo || len(dnsMailSteps) != 2 {
		t.Fatalf("expected pdns+mail batch to require sudo, got steps=%d sudo=%v err=%q", len(dnsMailSteps), dnsMailSudo, errMsg)
	}
	if dnsMailSteps[0].canonical != "pdns" || dnsMailSteps[1].canonical != "mail" {
		t.Fatalf("expected pdns ordered before mail, got %s then %s", dnsMailSteps[0].canonical, dnsMailSteps[1].canonical)
	}

	// 3. Batch label dan single-item label fallback
	batchName, _, _, errMsg := srv.buildInstallBatch([]installItemPayload{
		{Component: "nginx"},
		{Component: "php", Version: "8.3"},
	}, "Preset Hanya Hosting\n")
	if errMsg != "" || batchName != "Preset Hanya Hosting" {
		t.Fatalf("unexpected batchName=%q err=%q", batchName, errMsg)
	}

	// 4. HTTP POST /api/install menolak batch kosong, komponen asing, atau versi berbahaya
	badBodies := []string{
		`{}`,
		`{"items":[]}`,
		`{"items":[{"component":"nginx"},{"component":"not-exists"}]}`,
		`{"items":[{"component":"php","version":"8.4; rm -rf /"}]}`,
		`{"items":[{"component":"zsh","version":"evil-theme"}]}`,
	}
	for _, body := range badBodies {
		req := httptest.NewRequest(http.MethodPost, "/api/install", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST /api/install with %s status = %d, want 400", body, rec.Code)
		}
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

func TestHandleModeSupportsLocalEdgeVPS(t *testing.T) {
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "wdev.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer db.Close()

	cfg := &config.Config{HTTPHost: "127.0.0.1", HTTPPort: "3200", WdevEtc: dir}
	pdnsMgr := pdns.New(cfg, db)
	inst := installer.New(cfg, db)
	authMgr := auth.New(cfg, db)
	srv := New(cfg, db, runner.New(db), inst, nil, nil, nil, pdnsMgr, nil, authMgr)

	for _, tc := range []struct {
		in   string
		want string
		code int
	}{
		{"local", "local", http.StatusOK},
		{"edge", "edge", http.StatusOK},
		{"vps", "vps", http.StatusOK},
		{"invalid", "", http.StatusBadRequest},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/mode", strings.NewReader(`{"mode":"`+tc.in+`"}`))
		rec := httptest.NewRecorder()
		srv.handleMode(rec, req)
		if rec.Code != tc.code {
			t.Fatalf("POST /api/mode (%s) code = %d, want %d (body=%s)", tc.in, rec.Code, tc.code, rec.Body.String())
		}
		if tc.want != "" && pdnsMgr.GetMode() != tc.want {
			t.Fatalf("after POST %s, GetMode() = %q, want %q", tc.in, pdnsMgr.GetMode(), tc.want)
		}
	}
}

func TestHandlePDNSZonesPostCreatesStaticVHost(t *testing.T) {
	pdnsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/servers/localhost":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"localhost"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/servers/localhost/zones":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"portal.my.id."}`))
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/servers/localhost/zones/"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/metadata/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/servers/localhost/zones/"):
			zone := strings.TrimPrefix(r.URL.Path, "/api/v1/servers/localhost/zones/")
			_ = json.NewEncoder(w).Encode(pdns.ZoneInfo{
				ID:     zone,
				Name:   zone,
				Kind:   "Native",
				Serial: 1795239183,
				RRsets: []pdns.RRSet{
					{Name: zone, Type: "SOA", TTL: 3600, Records: []pdns.Record{{Content: "ns1." + zone + " hostmaster." + zone + " 1795239183 10800 3600 604800 3600"}}},
					{Name: zone, Type: "NS", TTL: 3600, Records: []pdns.Record{{Content: "ns1." + zone}, {Content: "ns2." + zone}}},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer pdnsSrv.Close()
	u, _ := url.Parse(pdnsSrv.URL)

	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	etcDir := filepath.Join(tmp, "etc")
	_ = os.MkdirAll(etcDir, 0755)
	hostsFile := filepath.Join(tmp, "hosts")
	_ = os.WriteFile(hostsFile, []byte("127.0.0.1 localhost\n"), 0644)

	cfg := &config.Config{
		HTTPHost:         "127.0.0.1",
		HTTPPort:         "3200",
		WdevEtc:          etcDir,
		NginxSitesAvail:  filepath.Join(tmp, "sites-available"),
		NginxSitesEnable: filepath.Join(tmp, "sites-enabled"),
		WebRootBase:      filepath.Join(tmp, "www"),
		HostsFile:        hostsFile,
	}

	pdnsMgr := pdns.New(cfg, db)
	_ = pdnsMgr.SetMode("vps")
	confData := "VPS_NETBIRD_IP=\"" + u.Hostname() + "\"\n" +
		"VPS_PUBLIC_IP=\"103.171.85.72\"\n" +
		"API_KEY=\"secret-test-key\"\n" +
		"PDNS_PORT=\"" + u.Port() + "\"\n" +
		"AGENT_PORT=\"8082\"\n"
	if err := os.WriteFile(filepath.Join(etcDir, "edge-dns.conf"), []byte(confData), 0600); err != nil {
		t.Fatal(err)
	}

	siteMgr := site.New(cfg, db)
	siteMgr.SetSkipNginxExec(true)
	inst := installer.New(cfg, db)
	authMgr := auth.New(cfg, db)
	srv := New(cfg, db, runner.New(db), inst, siteMgr, nil, nil, pdnsMgr, nil, authMgr)

	// 1. Default POST /api/pdns/zones creates zone + static HTML vhost
	req := httptest.NewRequest(http.MethodPost, "/api/pdns/zones", strings.NewReader(`{"zone_name":"portal.my.id"}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/pdns/zones code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status       string `json:"status"`
		Zone         string `json:"zone"`
		VHostStatus  string `json:"vhost_status"`
		VHostCreated bool   `json:"vhost_created"`
		VHostRoot    string `json:"vhost_root"`
		VHostURL     string `json:"vhost_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "created" || resp.Zone != "portal.my.id" || !resp.VHostCreated || resp.VHostStatus != "created" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if _, err := os.Stat(filepath.Join(cfg.WebRootBase, "portal.my.id", "index.html")); err != nil {
		t.Fatalf("expected index.html to exist: %v", err)
	}

	// 2. Explicit create_vhost:false skips vhost creation
	req2 := httptest.NewRequest(http.MethodPost, "/api/pdns/zones", strings.NewReader(`{"zone_name":"dnsonly.my.id","create_vhost":false}`))
	rec2 := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("POST /api/pdns/zones (dnsonly) code = %d, body=%s", rec2.Code, rec2.Body.String())
	}
	var resp2 struct {
		VHostStatus  string `json:"vhost_status"`
		VHostCreated bool   `json:"vhost_created"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp2); err != nil {
		t.Fatal(err)
	}
	if resp2.VHostCreated || resp2.VHostStatus != "skipped" {
		t.Fatalf("expected skipped vhost when create_vhost=false, got %+v", resp2)
	}
	if _, err := os.Stat(filepath.Join(cfg.WebRootBase, "dnsonly.my.id", "index.html")); !os.IsNotExist(err) {
		t.Fatalf("did not expect index.html when create_vhost=false")
	}
}

func TestHandleSiteSSLValidationAndStartTask(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "site-ssl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := &config.Config{
		HTTPHost:         "127.0.0.1",
		HTTPPort:         "3200",
		WdevEtc:          filepath.Join(tmp, "etc"),
		NginxSitesAvail:  filepath.Join(tmp, "sites-available"),
		NginxSitesEnable: filepath.Join(tmp, "sites-enabled"),
		WebRootBase:      filepath.Join(tmp, "www"),
		HostsFile:        filepath.Join(tmp, "hosts"),
	}
	_ = os.WriteFile(cfg.HostsFile, []byte("127.0.0.1 localhost\n"), 0644)

	siteMgr := site.New(cfg, db)
	siteMgr.SetSkipNginxExec(true)
	doneCh := make(chan struct{})
	siteMgr.SetCertbotExecFn(func(ctx context.Context, cb func(string), bin string, args []string) error {
		close(doneCh)
		return nil
	})
	if _, err := siteMgr.EnsureDomainStaticVhost("nikaget.my.id"); err != nil {
		t.Fatal(err)
	}

	run := runner.New(db)
	inst := installer.New(cfg, db)
	srv := New(cfg, db, run, inst, siteMgr, nil, nil, nil, nil, auth.New(cfg, db))

	// 1. Reject GET
	reqGet := httptest.NewRequest(http.MethodGet, "/api/sites/ssl", nil)
	recGet := httptest.NewRecorder()
	srv.mux.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/sites/ssl = %d, want 405", recGet.Code)
	}

	// 2. Reject local .test domain
	reqLocal := httptest.NewRequest(http.MethodPost, "/api/sites/ssl", strings.NewReader(`{"domain":"myapp.test"}`))
	recLocal := httptest.NewRecorder()
	srv.mux.ServeHTTP(recLocal, reqLocal)
	if recLocal.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/sites/ssl (.test) = %d, want 400", recLocal.Code)
	}

	// 3. Reject missing vhost
	reqMissing := httptest.NewRequest(http.MethodPost, "/api/sites/ssl", strings.NewReader(`{"domain":"unknown.my.id"}`))
	recMissing := httptest.NewRecorder()
	srv.mux.ServeHTTP(recMissing, reqMissing)
	if recMissing.Code != http.StatusNotFound {
		t.Fatalf("POST /api/sites/ssl (missing) = %d, want 404", recMissing.Code)
	}

	// 4. Valid POST /api/sites/ssl starts native task and executes certbot runner
	if reqSudo, _ := inst.CheckSudoRequired(); !reqSudo {
		reqOK := httptest.NewRequest(http.MethodPost, "/api/sites/ssl", strings.NewReader(`{"domain":"nikaget.my.id"}`))
		recOK := httptest.NewRecorder()
		srv.mux.ServeHTTP(recOK, reqOK)
		if recOK.Code != http.StatusOK {
			t.Fatalf("POST /api/sites/ssl = %d, body=%s", recOK.Code, recOK.Body.String())
		}
		var body struct {
			Status string `json:"status"`
			TaskID int64  `json:"task_id"`
			Domain string `json:"domain"`
		}
		if err := json.Unmarshal(recOK.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Status != "started" || body.TaskID == 0 || body.Domain != "nikaget.my.id" {
			t.Fatalf("unexpected /api/sites/ssl response: %+v", body)
		}
		select {
		case <-doneCh:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for certbot task execution")
		}
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
			if run.GetActiveTask() == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestHandleSitesPostAutoAddsSubdomainARecordInVPSMode(t *testing.T) {
	var patchedBody string
	pdnsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/servers/localhost/zones":
			_ = json.NewEncoder(w).Encode([]pdns.ZoneInfo{{ID: "nikaget.my.id.", Name: "nikaget.my.id."}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/servers/localhost/zones/nikaget.my.id.":
			_ = json.NewEncoder(w).Encode(pdns.ZoneInfo{
				ID:   "nikaget.my.id.",
				Name: "nikaget.my.id.",
				RRsets: []pdns.RRSet{
					{Name: "nikaget.my.id.", Type: "A", TTL: 300, Records: []pdns.Record{{Content: "103.171.85.72"}}},
				},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/servers/localhost/zones/nikaget.my.id.":
			b, _ := io.ReadAll(r.Body)
			patchedBody = string(b)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer pdnsSrv.Close()
	u, _ := url.Parse(pdnsSrv.URL)

	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	etcDir := filepath.Join(tmp, "etc")
	_ = os.MkdirAll(etcDir, 0755)
	cfg := &config.Config{
		HTTPHost:         "127.0.0.1",
		HTTPPort:         "3200",
		WdevEtc:          etcDir,
		NginxSitesAvail:  filepath.Join(tmp, "sites-available"),
		NginxSitesEnable: filepath.Join(tmp, "sites-enabled"),
		WebRootBase:      filepath.Join(tmp, "www"),
	}
	pdnsMgr := pdns.New(cfg, db)
	_ = pdnsMgr.SetMode("vps")
	confData := "VPS_NETBIRD_IP=\"" + u.Hostname() + "\"\n" +
		"VPS_PUBLIC_IP=\"103.171.85.72\"\n" +
		"API_KEY=\"secret-test-key\"\n" +
		"PDNS_PORT=\"" + u.Port() + "\"\n" +
		"AGENT_PORT=\"8082\"\n"
	if err := os.WriteFile(filepath.Join(etcDir, "edge-dns.conf"), []byte(confData), 0600); err != nil {
		t.Fatal(err)
	}

	siteMgr := site.New(cfg, db)
	siteMgr.SetSkipNginxExec(true)
	inst := installer.New(cfg, db)
	authMgr := auth.New(cfg, db)
	srv := New(cfg, db, runner.New(db), inst, siteMgr, nil, nil, pdnsMgr, nil, authMgr)

	req := httptest.NewRequest(http.MethodPost, "/api/sites", strings.NewReader(`{"domain":"app.nikaget.my.id","framework":"static"}`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/sites code = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Status    string `json:"status"`
		DNSSynced bool   `json:"dns_synced"`
		DNSZone   string `json:"dns_zone"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "created" || !resp.DNSSynced || resp.DNSZone != "nikaget.my.id" {
		t.Fatalf("unexpected POST /api/sites response: %+v", resp)
	}
	if !strings.Contains(patchedBody, `"app.nikaget.my.id."`) || !strings.Contains(patchedBody, `"103.171.85.72"`) {
		t.Fatalf("expected PowerDNS PATCH with A record for app.nikaget.my.id., got: %s", patchedBody)
	}

	// Verifikasi validasi POST /api/mail/account/password dan POST /api/service-config/postgresql/save-role & delete-role
	reqPwd := httptest.NewRequest(http.MethodPost, "/api/mail/account/password", strings.NewReader(`{"email":"invalid","password":"123"}`))
	recPwd := httptest.NewRecorder()
	srv.mux.ServeHTTP(recPwd, reqPwd)
	if recPwd.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /api/mail/account/password without mailMgr = %d, want 503", recPwd.Code)
	}

	srv.svcCfgMgr = serviceconfig.New(inst.GetSudoPassword)
	reqRole := httptest.NewRequest(http.MethodPost, "/api/service-config/postgresql/save-role", strings.NewReader(`{"role":"bad;drop","can_login":true,"password":"abc"}`))
	recRole := httptest.NewRecorder()
	srv.mux.ServeHTTP(recRole, reqRole)
	if recRole.Code != http.StatusBadRequest && recRole.Code != http.StatusOK {
		t.Fatalf("unexpected POST save-role status %d: %s", recRole.Code, recRole.Body.String())
	}
	if recRole.Code == http.StatusOK && !strings.Contains(recRole.Body.String(), "sudo_required") {
		t.Fatalf("expected sudo_required when code=200, got: %s", recRole.Body.String())
	}

	reqDelRole := httptest.NewRequest(http.MethodPost, "/api/service-config/postgresql/delete-role", strings.NewReader(`{"role":"postgres"}`))
	recDelRole := httptest.NewRecorder()
	srv.mux.ServeHTTP(recDelRole, reqDelRole)
	if recDelRole.Code == http.StatusOK && !strings.Contains(recDelRole.Body.String(), "sudo_required") {
		t.Fatalf("unexpected POST delete-role OK body: %s", recDelRole.Body.String())
	}
}

func TestFileManagerEndpointsSecurityAndCRUD(t *testing.T) {
	tmp := t.TempDir()
	rootDir := filepath.Join(tmp, "www")
	outsideDir := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatal(err)
	}

	db, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := &config.Config{HTTPHost: "127.0.0.1", HTTPPort: "3200", WdevEtc: filepath.Join(tmp, "etc"), WebRootBase: rootDir}
	inst := installer.New(cfg, db)
	authMgr := auth.New(cfg, db)
	srv := New(cfg, db, runner.New(db), inst, nil, nil, nil, nil, nil, authMgr)
	srv.FileManager().SetRoots(rootDir)

	// 1. List root
	recList := httptest.NewRecorder()
	srv.mux.ServeHTTP(recList, httptest.NewRequest(http.MethodGet, "/api/files/list?path="+url.QueryEscape(rootDir), nil))
	if recList.Code != http.StatusOK {
		t.Fatalf("GET /api/files/list = %d, body=%s", recList.Code, recList.Body.String())
	}

	// 2. Write + Read file via API
	targetFile := filepath.Join(rootDir, "hello.txt")
	reqWrite := httptest.NewRequest(http.MethodPost, "/api/files/write", strings.NewReader(`{"path":`+strconvQuote(targetFile)+`,"content":"halo wdev"}`))
	recWrite := httptest.NewRecorder()
	srv.mux.ServeHTTP(recWrite, reqWrite)
	if recWrite.Code != http.StatusOK {
		t.Fatalf("POST /api/files/write = %d, body=%s", recWrite.Code, recWrite.Body.String())
	}

	recRead := httptest.NewRecorder()
	srv.mux.ServeHTTP(recRead, httptest.NewRequest(http.MethodGet, "/api/files/read?path="+url.QueryEscape(targetFile), nil))
	if recRead.Code != http.StatusOK || !strings.Contains(recRead.Body.String(), "halo wdev") {
		t.Fatalf("GET /api/files/read = %d, body=%s", recRead.Code, recRead.Body.String())
	}

	// 3. Traversal outside allowed root must return 403 Forbidden
	recForbidden := httptest.NewRecorder()
	srv.mux.ServeHTTP(recForbidden, httptest.NewRequest(http.MethodGet, "/api/files/list?path="+url.QueryEscape(outsideDir), nil))
	if recForbidden.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for outside root, got %d (%s)", recForbidden.Code, recForbidden.Body.String())
	}

	// 4. Multipart Upload preserving rel_paths + Download single file + FetchURL SSRF protection
	var upBuf bytes.Buffer
	mw := multipart.NewWriter(&upBuf)
	_ = mw.WriteField("dir", rootDir)
	_ = mw.WriteField("overwrite", "true")
	_ = mw.WriteField("rel_paths", "assets/app.js")
	pw, _ := mw.CreateFormFile("files", "app.js")
	_, _ = pw.Write([]byte("console.log('wdev');"))
	_ = mw.Close()

	reqUp := httptest.NewRequest(http.MethodPost, "/api/files/upload", &upBuf)
	reqUp.Header.Set("Content-Type", mw.FormDataContentType())
	recUp := httptest.NewRecorder()
	srv.mux.ServeHTTP(recUp, reqUp)
	if recUp.Code != http.StatusOK || !strings.Contains(recUp.Body.String(), `"count":1`) {
		t.Fatalf("POST /api/files/upload = %d, body=%s", recUp.Code, recUp.Body.String())
	}

	uploadedFile := filepath.Join(rootDir, "assets", "app.js")
	recDL := httptest.NewRecorder()
	srv.mux.ServeHTTP(recDL, httptest.NewRequest(http.MethodGet, "/api/files/download?paths="+url.QueryEscape(uploadedFile), nil))
	if recDL.Code != http.StatusOK || recDL.Body.String() != "console.log('wdev');" {
		t.Fatalf("GET /api/files/download = %d, body=%q", recDL.Code, recDL.Body.String())
	}

	reqFetchBad := httptest.NewRequest(http.MethodPost, "/api/files/fetch-url", strings.NewReader(`{"url":"http://127.0.0.1:8080/x","dir":`+strconvQuote(rootDir)+`}`))
	recFetchBad := httptest.NewRecorder()
	srv.mux.ServeHTTP(recFetchBad, reqFetchBad)
	if recFetchBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for loopback fetch-url, got %d (%s)", recFetchBad.Code, recFetchBad.Body.String())
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestTerminalInfoAndWebSocketPTY(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "term.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := &config.Config{HTTPHost: "127.0.0.1", HTTPPort: "3200", WdevEtc: filepath.Join(tmp, "etc"), WebRootBase: tmp}
	inst := installer.New(cfg, db)
	authMgr := auth.New(cfg, db)
	_ = authMgr.SetSecurityEntrance("secret-term")
	srv := New(cfg, db, runner.New(db), inst, nil, nil, nil, nil, nil, authMgr)
	handler := srv.wrapSecurityHeaders(srv.wrapSecurityEntrance(srv.wrapAuth(srv.wrapCSRF(srv.mux))))

	// 1. Unauthenticated / without entrance must be blocked with 404
	recUnauth := httptest.NewRecorder()
	handler.ServeHTTP(recUnauth, httptest.NewRequest(http.MethodGet, "/api/terminal/info", nil))
	if recUnauth.Code != http.StatusNotFound {
		t.Fatalf("expected 404 without entrance cookie, got %d", recUnauth.Code)
	}

	// Create valid session cookie
	token, err := authMgr.CreateSession("admin")
	if err != nil {
		t.Fatal(err)
	}
	sessionCookie := &http.Cookie{Name: auth.CookieSessionName, Value: token}

	// 2. Authenticated GET /api/terminal/info returns system info & shells
	reqInfo := httptest.NewRequest(http.MethodGet, "/api/terminal/info", nil)
	reqInfo.AddCookie(sessionCookie)
	recInfo := httptest.NewRecorder()
	handler.ServeHTTP(recInfo, reqInfo)
	if recInfo.Code != http.StatusOK {
		t.Fatalf("GET /api/terminal/info = %d, body=%s", recInfo.Code, recInfo.Body.String())
	}
	if !strings.Contains(recInfo.Body.String(), `"available_shells"`) {
		t.Fatalf("expected available_shells in /api/terminal/info, got %s", recInfo.Body.String())
	}

	// 3. Spin up HTTP test server to verify real WebSocket PTY upgrade + CSWSH protection
	ts := httptest.NewServer(handler)
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/terminal/ws?shell=" + url.QueryEscape("/bin/sh") + "&cwd=" + url.QueryEscape(tmp)
	tsURL, _ := url.Parse(ts.URL)

	// Cross-origin upgrade must fail
	badHeaders := http.Header{}
	badHeaders.Set("Cookie", sessionCookie.String())
	badHeaders.Set("Origin", "https://evil.example.net")
	if cBad, respBad, err := websocket.DefaultDialer.Dial(wsURL, badHeaders); err == nil {
		_ = cBad.Close()
		t.Fatalf("expected cross-origin WebSocket dial to fail")
	} else if respBad != nil && respBad.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin WebSocket, got %d", respBad.StatusCode)
	}

	// Same-origin authenticated upgrade must succeed
	okHeaders := http.Header{}
	okHeaders.Set("Cookie", sessionCookie.String())
	okHeaders.Set("Origin", "http://"+tsURL.Host)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, okHeaders)
	if err != nil {
		t.Fatalf("authenticated same-origin WebSocket dial failed: %v", err)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	// Read ready control JSON
	msgType, rawReady, err := conn.ReadMessage()
	if err != nil || msgType != websocket.TextMessage || !strings.Contains(string(rawReady), `"type":"ready"`) {
		t.Fatalf("expected ready text frame, got type=%d payload=%q err=%v", msgType, string(rawReady), err)
	}

	// Send resize JSON frame + shell command over binary frame
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":120,"rows":35}`)); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("pwd && echo __WS_PTY_PASS__ && exit 0\n")); err != nil {
		t.Fatal(err)
	}

	var combined strings.Builder
	seenExit := false
	for i := 0; i < 64; i++ {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		mt, data, rerr := conn.ReadMessage()
		if rerr != nil {
			break
		}
		if mt == websocket.BinaryMessage {
			combined.Write(data)
		} else if mt == websocket.TextMessage && strings.Contains(string(data), `"type":"exit"`) {
			seenExit = true
			break
		}
	}

	if !strings.Contains(combined.String(), "__WS_PTY_PASS__") || !seenExit {
		t.Fatalf("expected __WS_PTY_PASS__ and exit event, seenExit=%v output=%q", seenExit, combined.String())
	}

	// 4. Audit log must record the session
	reqAudit := httptest.NewRequest(http.MethodGet, "/api/terminal/audit?limit=10", nil)
	reqAudit.AddCookie(sessionCookie)
	recAudit := httptest.NewRecorder()
	handler.ServeHTTP(recAudit, reqAudit)
	if recAudit.Code != http.StatusOK || !strings.Contains(recAudit.Body.String(), `"target_user"`) {
		t.Fatalf("GET /api/terminal/audit = %d, body=%s", recAudit.Code, recAudit.Body.String())
	}

	// 5. Step-Up Verification (/api/terminal/unlock & /api/terminal/lock & single-use ticket)
	_ = authMgr.SetPassword("StrongPass#2026")
	freshToken, err := authMgr.CreateSession("admin")
	if err != nil {
		t.Fatal(err)
	}
	freshCookie := &http.Cookie{Name: auth.CookieSessionName, Value: freshToken}
	srv.ensureTermSec().requireAll = true

	// Step-up enabled: WS without token/grant must return 403 terminal_unlock_required
	stepHeaders := http.Header{}
	stepHeaders.Set("Cookie", freshCookie.String())
	stepHeaders.Set("Origin", "http://"+tsURL.Host)
	if cLocked, respLocked, err := websocket.DefaultDialer.Dial(wsURL, stepHeaders); err == nil {
		_ = cLocked.Close()
		t.Fatal("expected WS without unlock token to fail with 403")
	} else if respLocked == nil || respLocked.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 terminal_unlock_required, got resp=%+v err=%v", respLocked, err)
	}

	// Even with auth_enabled=false, VerifyAdminPassword stays strict
	_ = authMgr.SetAuthEnabled(false)
	if authMgr.VerifyAdminPassword("wrong-password") {
		t.Fatal("VerifyAdminPassword must reject wrong password even when auth_enabled=false")
	}
	if !authMgr.VerifyAdminPassword("StrongPass#2026") {
		t.Fatal("VerifyAdminPassword must accept valid password")
	}
	_ = authMgr.SetAuthEnabled(true)

	// Wrong unlock password returns 401
	doPOST := func(path, body string, cookie *http.Cookie) (int, []byte) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(body))
		req.Header.Set("Origin", "http://"+tsURL.Host)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, derr := http.DefaultClient.Do(req)
		if derr != nil {
			t.Fatal(derr)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}

	if code, body := doPOST("/api/terminal/unlock", `{"password":"wrong","user":""}`, freshCookie); code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong unlock password, got %d (%s)", code, string(body))
	}

	// Valid unlock password returns single-use launch token (without remember)
	codeGood, bodyGood := doPOST("/api/terminal/unlock", `{"password":"StrongPass#2026","remember":false}`, freshCookie)
	if codeGood != http.StatusOK {
		t.Fatalf("POST /api/terminal/unlock = %d, body=%s", codeGood, string(bodyGood))
	}
	var unlockResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(bodyGood, &unlockResp); err != nil || unlockResp.Token == "" {
		t.Fatalf("invalid unlock JSON: %v body=%s", err, string(bodyGood))
	}

	// Dialing WITH ticket succeeds once, and replaying the same ticket fails (single-use)
	wsURLWithToken := wsURL + "&token=" + url.QueryEscape(unlockResp.Token)
	cUnlocked, _, err := websocket.DefaultDialer.Dial(wsURLWithToken, stepHeaders)
	if err != nil {
		t.Fatalf("WS dial with single-use unlock token failed: %v", err)
	}
	_ = cUnlocked.Close()

	if cReplay, respReplay, err := websocket.DefaultDialer.Dial(wsURLWithToken, stepHeaders); err == nil {
		_ = cReplay.Close()
		t.Fatal("expected single-use unlock token replay to fail")
	} else if respReplay == nil || respReplay.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 on replay, got %+v err=%v", respReplay, err)
	}

	// Unlock with remember=true allows follow-up dial until POST /api/terminal/lock revokes it
	codeRem, bodyRem := doPOST("/api/terminal/unlock", `{"password":"StrongPass#2026","remember":true}`, freshCookie)
	if codeRem != http.StatusOK || !strings.Contains(string(bodyRem), `"root_unlocked":true`) {
		t.Fatalf("remember unlock failed: %d %s", codeRem, string(bodyRem))
	}

	cRem, _, err := websocket.DefaultDialer.Dial(wsURL, stepHeaders)
	if err != nil {
		t.Fatalf("WS dial under active root grant failed: %v", err)
	}
	_ = cRem.Close()

	if codeLock, bodyLock := doPOST("/api/terminal/lock", "", freshCookie); codeLock != http.StatusOK {
		t.Fatalf("POST /api/terminal/lock = %d (%s)", codeLock, string(bodyLock))
	}

	if cAfterLock, respAfterLock, err := websocket.DefaultDialer.Dial(wsURL, stepHeaders); err == nil {
		_ = cAfterLock.Close()
		t.Fatal("expected WS dial to fail after /api/terminal/lock")
	} else if respAfterLock == nil || respAfterLock.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 after lock, got %+v err=%v", respAfterLock, err)
	}
}

func TestPanelDomainBindingSSLAndBlockIP(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "panel-dom.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hostsFile := filepath.Join(tmp, "hosts")
	_ = os.WriteFile(hostsFile, []byte("127.0.0.1 localhost\n"), 0644)

	cfg := &config.Config{
		HTTPHost:         "127.0.0.1",
		HTTPPort:         "3200",
		WdevEtc:          filepath.Join(tmp, "etc"),
		NginxSitesAvail:  filepath.Join(tmp, "sites-available"),
		NginxSitesEnable: filepath.Join(tmp, "sites-enabled"),
		WebRootBase:      filepath.Join(tmp, "www"),
		HostsFile:        hostsFile,
	}

	siteMgr := site.New(cfg, db)
	siteMgr.SetSkipNginxExec(true)
	certbotShouldFail := false
	siteMgr.SetCertbotExecFn(func(ctx context.Context, cb func(string), bin string, args []string) error {
		if certbotShouldFail {
			return io.ErrUnexpectedEOF
		}
		return nil
	})

	// Buat satu situs reguler agar domain konflik terdeteksi & daftar available_domains terisi
	if _, err := siteMgr.EnsureDomainStaticVhost("appsone.id"); err != nil {
		t.Fatal(err)
	}

	run := runner.New(db)
	inst := installer.New(cfg, db)
	authMgr := auth.New(cfg, db)
	srv := New(cfg, db, run, inst, siteMgr, nil, nil, nil, nil, authMgr)
	handler := srv.wrapSecurityHeaders(srv.wrapPanelDomain(srv.wrapSecurityEntrance(srv.wrapAuth(srv.wrapCSRF(srv.mux)))))

	token, err := authMgr.CreateSession("admin")
	if err != nil {
		t.Fatal(err)
	}
	sessCookie := &http.Cookie{Name: auth.CookieSessionName, Value: token}

	// 1. GET /api/panel/domain awal -> kosong, available_domains berisi appsone.id
	reqGet := httptest.NewRequest(http.MethodGet, "/api/panel/domain", nil)
	reqGet.Host = "103.171.85.72:3200"
	reqGet.AddCookie(sessCookie)
	recGet := httptest.NewRecorder()
	handler.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusOK || !strings.Contains(recGet.Body.String(), `"appsone.id"`) {
		t.Fatalf("GET /api/panel/domain = %d, body=%s", recGet.Code, recGet.Body.String())
	}

	// 2. Reject mengikat domain yang sudah dipakai VHost website reguler
	reqConflict := httptest.NewRequest(http.MethodPost, "/api/panel/domain", strings.NewReader(`{"domain":"appsone.id","ssl_mode":"letsencrypt"}`))
	reqConflict.Host = "103.171.85.72:3200"
	reqConflict.Header.Set("Origin", "http://103.171.85.72:3200")
	reqConflict.AddCookie(sessCookie)
	recConflict := httptest.NewRecorder()
	handler.ServeHTTP(recConflict, reqConflict)
	if recConflict.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for existing non-panel vhost, got %d (%s)", recConflict.Code, recConflict.Body.String())
	}

	// 2b. Jika bukan root tanpa sudo, endpoint mengembalikan status sudo_required
	if reqSudo, _ := inst.CheckSudoRequired(); reqSudo {
		reqSudoResp := httptest.NewRequest(http.MethodPost, "/api/panel/domain", strings.NewReader(`{"domain":"panel.appsone.id","ssl_mode":"letsencrypt"}`))
		reqSudoResp.Host = "103.171.85.72:3200"
		reqSudoResp.Header.Set("Origin", "http://103.171.85.72:3200")
		reqSudoResp.AddCookie(sessCookie)
		recSudoResp := httptest.NewRecorder()
		handler.ServeHTTP(recSudoResp, reqSudoResp)
		if recSudoResp.Code != http.StatusOK || !strings.Contains(recSudoResp.Body.String(), `"sudo_required"`) {
			t.Fatalf("expected sudo_required when non-root, got %d (%s)", recSudoResp.Code, recSudoResp.Body.String())
		}
	}

	// Nonaktifkan sudo gate installer agar unit test mengeksekusi task Let's Encrypt mock & binding secara deterministik
	srv.installer = nil

	// 3. Jika Let's Encrypt gagal, binding & block IP TIDAK boleh aktif (anti lockout)
	certbotShouldFail = true
	reqFailLE := httptest.NewRequest(http.MethodPost, "/api/panel/domain", strings.NewReader(`{"domain":"panel.appsone.id","ssl_mode":"letsencrypt","block_direct_ip":true}`))
	reqFailLE.Host = "103.171.85.72:3200"
	reqFailLE.Header.Set("Origin", "http://103.171.85.72:3200")
	reqFailLE.AddCookie(sessCookie)
	recFailLE := httptest.NewRecorder()
	handler.ServeHTTP(recFailLE, reqFailLE)
	if recFailLE.Code != http.StatusOK || !strings.Contains(recFailLE.Body.String(), `"started"`) {
		t.Fatalf("expected task start 200, got %d (%s)", recFailLE.Code, recFailLE.Body.String())
	}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if run.GetActiveTask() == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if authMgr.GetPanelDomain() != "" || authMgr.IsBlockDirectIP() {
		t.Fatalf("panel domain & block IP must stay disabled if certbot failed")
	}

	// 4. Sukses bind subdomain panel.appsone.id dengan HTTPS + Block Direct IP
	certbotShouldFail = false
	reqBind := httptest.NewRequest(http.MethodPost, "/api/panel/domain", strings.NewReader(`{"domain":"panel.appsone.id","ssl_mode":"letsencrypt","block_direct_ip":true}`))
	reqBind.Host = "103.171.85.72:3200"
	reqBind.Header.Set("Origin", "http://103.171.85.72:3200")
	reqBind.AddCookie(sessCookie)
	recBind := httptest.NewRecorder()
	handler.ServeHTTP(recBind, reqBind)
	if recBind.Code != http.StatusOK || !strings.Contains(recBind.Body.String(), `"started"`) {
		t.Fatalf("POST /api/panel/domain = %d, body=%s", recBind.Code, recBind.Body.String())
	}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		if run.GetActiveTask() == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	dom, sslOn, blockIP := authMgr.PanelDomainConfig()
	if dom != "panel.appsone.id" || !sslOn || !blockIP {
		t.Fatalf("unexpected PanelDomainConfig: dom=%q ssl=%v blockIP=%v", dom, sslOn, blockIP)
	}

	// Pastikan config Nginx proxy terbentuk dengan upload limit 260m
	confBytes, err := os.ReadFile(filepath.Join(cfg.NginxSitesAvail, "panel.appsone.id"))
	if err != nil || !strings.Contains(string(confBytes), "proxy_pass http://127.0.0.1:3200") || !strings.Contains(string(confBytes), "client_max_body_size 260m") {
		t.Fatalf("unexpected nginx proxy vhost conf: err=%v body=%s", err, string(confBytes))
	}

	// 5. Setelah domain terikat, akses via nomor IP WAJIB diblokir (403) tanpa membocorkan nama domain
	for _, hostHeader := range []string{"103.171.85.72:3200", "127.0.0.1:3200"} {
		reqIPWeb := httptest.NewRequest(http.MethodGet, "/", nil)
		reqIPWeb.Host = hostHeader
		recIPWeb := httptest.NewRecorder()
		handler.ServeHTTP(recIPWeb, reqIPWeb)
		if recIPWeb.Code != http.StatusForbidden || strings.Contains(recIPWeb.Body.String(), "panel.appsone.id") {
			t.Fatalf("direct IP web access (%s) = %d body=%s", hostHeader, recIPWeb.Code, recIPWeb.Body.String())
		}

		reqIPAPI := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		reqIPAPI.Host = hostHeader
		reqIPAPI.AddCookie(sessCookie)
		recIPAPI := httptest.NewRecorder()
		handler.ServeHTTP(recIPAPI, reqIPAPI)
		if recIPAPI.Code != http.StatusForbidden || !strings.Contains(recIPAPI.Body.String(), "panel_domain_required") {
			t.Fatalf("direct IP API access (%s) = %d body=%s", hostHeader, recIPAPI.Code, recIPAPI.Body.String())
		}
	}

	// 6. Akses HTTP biasa ke domain (tanpa X-Forwarded-Proto: https) di-redirect 308 ke HTTPS
	reqHTTPDom := httptest.NewRequest(http.MethodGet, "/files", nil)
	reqHTTPDom.Host = "panel.appsone.id:3200"
	reqHTTPDom.RemoteAddr = "103.171.85.72:54321"
	recHTTPDom := httptest.NewRecorder()
	handler.ServeHTTP(recHTTPDom, reqHTTPDom)
	if recHTTPDom.Code != http.StatusPermanentRedirect || recHTTPDom.Header().Get("Location") != "https://panel.appsone.id/files" {
		t.Fatalf("expected 308 redirect to https://panel.appsone.id/files, got %d loc=%q", recHTTPDom.Code, recHTTPDom.Header().Get("Location"))
	}

	// 7. Akses lewat proxy HTTPS Nginx (peer 127.0.0.1 + X-Forwarded-Proto: https + Host: panel.appsone.id) diizinkan
	reqHTTPSDom := httptest.NewRequest(http.MethodGet, "/api/panel/domain", nil)
	reqHTTPSDom.Host = "panel.appsone.id"
	reqHTTPSDom.RemoteAddr = "127.0.0.1:45678"
	reqHTTPSDom.Header.Set("X-Forwarded-Proto", "https")
	reqHTTPSDom.AddCookie(sessCookie)
	recHTTPSDom := httptest.NewRecorder()
	handler.ServeHTTP(recHTTPSDom, reqHTTPSDom)
	if recHTTPSDom.Code != http.StatusOK {
		t.Fatalf("HTTPS domain proxied access = %d, body=%s", recHTTPSDom.Code, recHTTPSDom.Body.String())
	}

	// 8. Unbind domain memulihkan akses via IP
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/panel/domain", strings.NewReader(`{"remove_vhost":true}`))
	reqDel.Host = "panel.appsone.id"
	reqDel.RemoteAddr = "127.0.0.1:45678"
	reqDel.Header.Set("X-Forwarded-Proto", "https")
	reqDel.Header.Set("Origin", "https://panel.appsone.id")
	reqDel.AddCookie(sessCookie)
	recDel := httptest.NewRecorder()
	handler.ServeHTTP(recDel, reqDel)
	if recDel.Code != http.StatusOK {
		t.Fatalf("DELETE /api/panel/domain = %d, body=%s", recDel.Code, recDel.Body.String())
	}

	reqIPAfter := httptest.NewRequest(http.MethodGet, "/api/panel/domain", nil)
	reqIPAfter.Host = "103.171.85.72:3200"
	reqIPAfter.AddCookie(sessCookie)
	recIPAfter := httptest.NewRecorder()
	handler.ServeHTTP(recIPAfter, reqIPAfter)
	if recIPAfter.Code != http.StatusOK {
		t.Fatalf("expected IP access restored after unbind, got %d", recIPAfter.Code)
	}
}

func TestPanelDirectSSLMultiplexAndRedirect(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "panel-ssl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		HTTPHost:         "127.0.0.1",
		HTTPPort:         portStr,
		WdevEtc:          filepath.Join(tmp, "etc"),
		NginxSitesAvail:  filepath.Join(tmp, "sites-available"),
		NginxSitesEnable: filepath.Join(tmp, "sites-enabled"),
		WebRootBase:      filepath.Join(tmp, "www"),
	}

	siteMgr := site.New(cfg, db)
	siteMgr.SetSkipNginxExec(true)
	run := runner.New(db)
	inst := installer.New(cfg, db)
	authMgr := auth.New(cfg, db)
	srv := New(cfg, db, run, inst, siteMgr, nil, nil, nil, nil, authMgr)
	handler := srv.wrapSecurityHeaders(srv.wrapPanelDomain(srv.wrapSecurityEntrance(srv.wrapAuth(srv.wrapCSRF(srv.mux)))))

	go func() {
		_ = srv.listenAndServeMux(ln, handler)
	}()

	token, err := authMgr.CreateSession("admin")
	if err != nil {
		t.Fatal(err)
	}
	sessCookie := &http.Cookie{Name: auth.CookieSessionName, Value: token}
	baseHTTP := "http://" + ln.Addr().String()
	baseHTTPS := "https://" + ln.Addr().String()

	noRedirectHTTP := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. Sebelum Panel SSL diaktifkan, HTTP ke port panel tetap dilayani normal (200 OK)
	reqBefore, _ := http.NewRequest(http.MethodGet, baseHTTP+"/api/panel/domain", nil)
	reqBefore.AddCookie(sessCookie)
	respBefore, err := noRedirectHTTP.Do(reqBefore)
	if err != nil {
		t.Fatalf("HTTP GET before Panel SSL failed: %v", err)
	}
	respBefore.Body.Close()
	if respBefore.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 before Panel SSL, got %d", respBefore.StatusCode)
	}

	// 2. Aktifkan Direct Panel SSL via POST /api/panel/ssl
	reqEnable, _ := http.NewRequest(http.MethodPost, baseHTTP+"/api/panel/ssl", strings.NewReader(`{"enabled":true,"hosts":["103.171.85.72"]}`))
	reqEnable.Header.Set("Origin", baseHTTP)
	reqEnable.AddCookie(sessCookie)
	respEnable, err := noRedirectHTTP.Do(reqEnable)
	if err != nil {
		t.Fatalf("POST /api/panel/ssl failed: %v", err)
	}
	enableBody, _ := io.ReadAll(respEnable.Body)
	respEnable.Body.Close()
	if respEnable.StatusCode != http.StatusOK || !strings.Contains(string(enableBody), `"panel_ssl_enabled":true`) {
		t.Fatalf("POST /api/panel/ssl = %d, body=%s", respEnable.StatusCode, string(enableBody))
	}

	// Verifikasi file sertifikat & private key panel otomatis terbuat
	certPath, keyPath := authMgr.PanelSSLPaths()
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("expected panel-ssl.crt at %s: %v", certPath, err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("expected panel-ssl.key at %s: %v", keyPath, err)
	}

	// 3. Setelah Panel SSL aktif, request HTTP biasa pada port yang sama wajib dialihkan (308 -> https://...)
	reqHTTPAfter, _ := http.NewRequest(http.MethodGet, baseHTTP+"/api/panel/domain", nil)
	reqHTTPAfter.AddCookie(sessCookie)
	respHTTPAfter, err := noRedirectHTTP.Do(reqHTTPAfter)
	if err != nil {
		t.Fatalf("HTTP GET after Panel SSL failed: %v", err)
	}
	respHTTPAfter.Body.Close()
	if respHTTPAfter.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("expected 308 Permanent Redirect from HTTP->HTTPS, got %d", respHTTPAfter.StatusCode)
	}
	loc := respHTTPAfter.Header.Get("Location")
	if !strings.HasPrefix(loc, baseHTTPS+"/") {
		t.Fatalf("expected HTTPS redirect Location starting with %s/, got %q", baseHTTPS, loc)
	}

	// 4. Koneksi HTTPS langsung pada port yang sama dilayani dengan TLS ECDSA (200 OK + HSTS)
	httpsClient := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	reqHTTPS, _ := http.NewRequest(http.MethodGet, baseHTTPS+"/api/panel/domain", nil)
	reqHTTPS.AddCookie(sessCookie)
	respHTTPS, err := httpsClient.Do(reqHTTPS)
	if err != nil {
		t.Fatalf("HTTPS GET failed: %v", err)
	}
	httpsBody, _ := io.ReadAll(respHTTPS.Body)
	respHTTPS.Body.Close()
	if respHTTPS.StatusCode != http.StatusOK || !strings.Contains(string(httpsBody), `"panel_ssl_enabled":true`) {
		t.Fatalf("HTTPS GET /api/panel/domain = %d, body=%s", respHTTPS.StatusCode, string(httpsBody))
	}
	if respHTTPS.Header.Get("Strict-Transport-Security") == "" {
		t.Fatalf("expected Strict-Transport-Security header on HTTPS response")
	}

	// 5. GetPanelAccessInfo (sumber output wdev panel default) mengembalikan https:// untuk IP lokal/publik/NetBird
	info := authMgr.GetPanelAccessInfo("103.171.85.72", "100.64.0.10")
	if !info.PanelSSLEnabled {
		t.Fatalf("expected info.PanelSSLEnabled=true")
	}
	for _, u := range []string{info.LocalURL, info.PublicURL, info.NetbirdURL} {
		if !strings.HasPrefix(u, "https://") {
			t.Fatalf("expected https:// access URL in GetPanelAccessInfo when Panel SSL enabled, got %q", u)
		}
	}
}

func TestHandleMailRelayDNSSyncEndpointValidation(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "mail-sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := &config.Config{
		HTTPHost: "127.0.0.1",
		HTTPPort: "3200",
		WdevEtc:  filepath.Join(tmp, "etc"),
	}
	_ = os.MkdirAll(cfg.WdevEtc, 0755)
	pdnsMgr := pdns.New(cfg, db)
	mailMgr := mail.New(cfg, db, pdnsMgr)
	authMgr := auth.New(cfg, db)
	srv := New(cfg, db, runner.New(db), installer.New(cfg, db), nil, pdnsMgr, nil, mailMgr, nil, authMgr)
	handler := srv.wrapSecurityHeaders(srv.wrapPanelDomain(srv.wrapSecurityEntrance(srv.wrapAuth(srv.wrapCSRF(srv.mux)))))

	token, err := authMgr.CreateSession("admin")
	if err != nil {
		t.Fatal(err)
	}
	sessCookie := &http.Cookie{Name: auth.CookieSessionName, Value: token}

	reqMissing := httptest.NewRequest(http.MethodPost, "/api/mail/relay/dns/sync", strings.NewReader(`{"domain":""}`))
	reqMissing.Host = "127.0.0.1:3200"
	reqMissing.Header.Set("Origin", "http://127.0.0.1:3200")
	reqMissing.AddCookie(sessCookie)
	recMissing := httptest.NewRecorder()
	handler.ServeHTTP(recMissing, reqMissing)
	if recMissing.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when domain empty, got %d (%s)", recMissing.Code, recMissing.Body.String())
	}

	reqLocal := httptest.NewRequest(http.MethodPost, "/api/mail/relay/dns/sync", strings.NewReader(`{"domain":"nikaget.my.id","provider":"local"}`))
	reqLocal.Host = "127.0.0.1:3200"
	reqLocal.Header.Set("Origin", "http://127.0.0.1:3200")
	reqLocal.AddCookie(sessCookie)
	recLocal := httptest.NewRecorder()
	handler.ServeHTTP(recLocal, reqLocal)
	if recLocal.Code != http.StatusOK || !strings.Contains(recLocal.Body.String(), `"nikaget.my.id"`) {
		t.Fatalf("expected 200 for non-Resend provider sync report, got %d (%s)", recLocal.Code, recLocal.Body.String())
	}
}
