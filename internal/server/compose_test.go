package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wdev/internal/compose"
	"wdev/internal/config"
	"wdev/internal/installer"
	"wdev/internal/runner"
	"wdev/internal/site"
	"wdev/internal/store"
)

type mockComposeRunner struct {
	calls [][]string
}

func (m *mockComposeRunner) LookPath(bin string) (string, error) {
	return "/usr/bin/" + bin, nil
}

func (m *mockComposeRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	m.calls = append(m.calls, append([]string(nil), args...))
	joined := strings.Join(args, " ")
	switch {
	case joined == "version --format {{.Client.Version}}":
		return []byte("29.1.0\n"), nil
	case joined == "compose version --short":
		return []byte("5.8.0\n"), nil
	case joined == "info --format {{.ServerVersion}}|{{.DockerRootDir}}":
		return []byte("29.1.0|/var/lib/docker\n"), nil
	case joined == "version --format {{.Server.APIVersion}}":
		return []byte("1.48\n"), nil
	case joined == "compose ls --all --format json":
		return []byte(`[{"Name":"demo-app","Status":"running(1)","ConfigFiles":""}]`), nil
	case strings.Contains(joined, "ps --all --format json"):
		return []byte(`[{"ID":"cid1","Name":"demo-app-web-1","Service":"web","Image":"nginx:alpine","State":"running","Status":"Up 1m","Health":"healthy","Publishers":[{"URL":"127.0.0.1","TargetPort":80,"PublishedPort":8080,"Protocol":"tcp"}]}]`), nil
	case strings.HasPrefix(joined, "stats --no-stream"):
		return []byte(`{"Name":"demo-app-web-1","CPUPerc":"0.10%","MemUsage":"10MiB / 1GiB","MemPerc":"0.98%","NetIO":"1kB / 1kB","BlockIO":"0B / 0B","PIDs":"2"}`), nil
	case strings.Contains(joined, "config --format json"):
		return []byte(`{"name":"demo-app","services":{"web":{"image":"nginx:alpine"}}}`), nil
	case strings.Contains(joined, "logs --no-color"):
		return []byte("demo-app-web-1 | ready on :80\n"), nil
	case joined == "system df --format json":
		return []byte(`{"Type":"Images","TotalCount":"2","Active":"1","Size":"120MB","Reclaimable":"40MB"}`), nil
	default:
		return nil, nil
	}
}

func (m *mockComposeRunner) Stream(ctx context.Context, dir string, cb func(string), args ...string) error {
	m.calls = append(m.calls, append([]string(nil), args...))
	if cb != nil {
		cb("stream: " + strings.Join(args, " "))
	}
	return nil
}

func TestComposeAPIEndpointsAndExposeVHost(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	db, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := &config.Config{
		HTTPHost:         "127.0.0.1",
		HTTPPort:         "3200",
		WdevEtc:          filepath.Join(tmp, "etc"),
		WebRootBase:      tmp,
		NginxSitesAvail:  filepath.Join(tmp, "sites-available"),
		NginxSitesEnable: filepath.Join(tmp, "sites-enabled"),
		HostsFile:        filepath.Join(tmp, "hosts"),
	}
	runMgr := runner.New(db)
	inst := installer.New(cfg, db)
	siteMgr := site.New(cfg, db)
	siteMgr.SetSkipNginxExec(true)

	mockRunner := &mockComposeRunner{}
	srv := New(cfg, db, runMgr, inst, siteMgr, nil, nil, nil, nil, nil)
	srv.composeMgr = compose.NewWithDeps(cfg, db, mockRunner)

	// 1. GET /api/compose/status
	req := httptest.NewRequest(http.MethodGet, "/api/compose/status", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/compose/status = %d body=%s", rec.Code, rec.Body.String())
	}
	var statusResp compose.StatusReport
	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil {
		t.Fatal(err)
	}
	if !statusResp.Engine.IsComposeV5 || statusResp.Engine.ComposeVersion != "5.8.0" || len(statusResp.Templates) == 0 {
		t.Fatalf("unexpected compose status response: %+v", statusResp.Engine)
	}

	// 2. POST /api/compose/projects (create from template)
	projDir := filepath.Join(tmp, "demo-app")
	createBody, _ := json.Marshal(compose.CreateProjectOptions{
		Name:     "demo-app",
		DirPath:  projDir,
		Template: "nginx-static",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/compose/projects", bytes.NewReader(createBody))
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/compose/projects = %d body=%s", rec.Code, rec.Body.String())
	}

	// 3. POST /api/compose/projects/demo-app/expose (create reverse proxy VHost for :8080)
	expBody := `{"domain":"demo-app.test","proxy_port":8080,"ssl_enabled":false,"add_hosts":false}`
	req = httptest.NewRequest(http.MethodPost, "/api/compose/projects/demo-app/expose", strings.NewReader(expBody))
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/compose/projects/demo-app/expose = %d body=%s", rec.Code, rec.Body.String())
	}

	// 4. GET /api/compose/projects/demo-app -> must include linked VHost and container stats
	req = httptest.NewRequest(http.MethodGet, "/api/compose/projects/demo-app", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/compose/projects/demo-app = %d body=%s", rec.Code, rec.Body.String())
	}
	var detail compose.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.LinkedVHosts) != 1 || detail.LinkedVHosts[0].Domain != "demo-app.test" {
		t.Fatalf("expected linked VHost demo-app.test on project, got %+v", detail.LinkedVHosts)
	}
	if len(detail.Containers) != 1 || detail.Containers[0].CPUPerc != "0.10%" {
		t.Fatalf("expected container stats on project, got %+v", detail.Containers)
	}

	// 5. PUT /api/compose/projects/demo-app/files
	filesBody := `{"compose_content":"services:\n  web:\n    image: nginx:alpine\n    ports:\n      - \"127.0.0.1:8080:80\"\n","env_content":"APP_PORT=8080\n","update_env":true}`
	req = httptest.NewRequest(http.MethodPut, "/api/compose/projects/demo-app/files", strings.NewReader(filesBody))
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/compose/projects/demo-app/files = %d body=%s", rec.Code, rec.Body.String())
	}

	// 6. POST /api/compose/projects/demo-app/action
	req = httptest.NewRequest(http.MethodPost, "/api/compose/projects/demo-app/action", strings.NewReader(`{"action":"restart"}`))
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"started"`) {
		t.Fatalf("POST /api/compose/projects/demo-app/action = %d body=%s", rec.Code, rec.Body.String())
	}

	// Wait briefly for runner task completion before temp dir cleanup
	waitRunner := func() {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if runMgr.GetActiveTask() == nil {
				break
			}
			time.Sleep(15 * time.Millisecond)
		}
	}
	waitRunner()

	// 6b. GET /api/compose/projects/demo-app/config
	req = httptest.NewRequest(http.MethodGet, "/api/compose/projects/demo-app/config", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"nginx:alpine"`) {
		t.Fatalf("GET /api/compose/projects/demo-app/config = %d body=%s", rec.Code, rec.Body.String())
	}

	// 6c. GET /api/compose/projects/demo-app/logs
	req = httptest.NewRequest(http.MethodGet, "/api/compose/projects/demo-app/logs?service=web&tail=50", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ready on :80") {
		t.Fatalf("GET /api/compose/projects/demo-app/logs = %d body=%s", rec.Code, rec.Body.String())
	}

	// 7. GET /api/compose/system/df & POST /api/compose/system/prune
	req = httptest.NewRequest(http.MethodGet, "/api/compose/system/df", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"Images"`) {
		t.Fatalf("GET /api/compose/system/df = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/compose/system/prune", strings.NewReader(`{"target":"images"}`))
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"started"`) {
		t.Fatalf("POST /api/compose/system/prune = %d body=%s", rec.Code, rec.Body.String())
	}
	waitRunner()

	// 8. DELETE /api/compose/projects/demo-app
	req = httptest.NewRequest(http.MethodDelete, "/api/compose/projects/demo-app", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/compose/projects/demo-app = %d body=%s", rec.Code, rec.Body.String())
	}

	// 9. GET /api/service-config/docker (works even for pre-config defaults)
	req = httptest.NewRequest(http.MethodGet, "/api/service-config/docker", nil)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"log_driver"`) {
		t.Fatalf("GET /api/service-config/docker = %d body=%s", rec.Code, rec.Body.String())
	}

	// 10. POST /api/install with invalid docker_options.daemon_config must return 400
	req = httptest.NewRequest(
		http.MethodPost,
		"/api/install",
		strings.NewReader(`{"component":"docker","docker_options":{"source":"official","daemon_config":{"data_root":"relative/path","log_driver":"json-file","log_max_size":"10m","log_max_file":3}}}`),
	)
	rec = httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid docker_options daemon_config, got %d body=%s", rec.Code, rec.Body.String())
	}
}
