package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"wdev/internal/config"
	"wdev/internal/runner"
	"wdev/internal/site"
	"wdev/internal/siteapp"
	"wdev/internal/store"
)

type fakeAppExec struct {
	cmds []string
}

func (f *fakeAppExec) Run(_ context.Context, name string, args ...string) (string, error) {
	f.cmds = append(f.cmds, name+" "+strings.Join(args, " "))
	if name == "systemctl" && len(args) > 0 && args[0] == "show" {
		return "ActiveState=active\nSubState=running\nUnitFileState=enabled\nMainPID=2222\nMemoryCurrent=2048\n", nil
	}
	return "", nil
}

func (f *fakeAppExec) WriteFile(_ context.Context, path string, data []byte, mode os.FileMode, _ string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

func (f *fakeAppExec) ReadFile(_ context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (f *fakeAppExec) RemoveFile(_ context.Context, path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (f *fakeAppExec) FileExists(_ context.Context, path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (f *fakeAppExec) EnsureDir(_ context.Context, dir string, mode os.FileMode, _ string) error {
	return os.MkdirAll(dir, mode)
}

func newTestSiteAppServer(t *testing.T) (*Server, *config.Config, *store.Store, *fakeAppExec) {
	t.Helper()
	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	hostsPath := filepath.Join(tmp, "hosts")
	_ = os.WriteFile(hostsPath, []byte("127.0.0.1 localhost\n"), 0644)

	cfg := &config.Config{
		HTTPHost:         "127.0.0.1",
		HTTPPort:         "8888",
		WdevEtc:          filepath.Join(tmp, "etc"),
		NginxSitesAvail:  filepath.Join(tmp, "sites-available"),
		NginxSitesEnable: filepath.Join(tmp, "sites-enabled"),
		WebRootBase:      filepath.Join(tmp, "www"),
		HostsFile:        hostsPath,
	}
	siteMgr := site.New(cfg, db)
	siteMgr.SetSkipNginxExec(true)

	appMgr := siteapp.New(cfg, db, nil)
	appMgr.SetSystemdDirForTest(filepath.Join(tmp, "systemd"))
	appMgr.SetAllowAnyDirForTest(true)
	fx := &fakeAppExec{}
	appMgr.SetExecForTest(fx)

	siteMgr.SetAppCleanupHook(func(domain string) error {
		return appMgr.RemoveApp(context.Background(), domain)
	})

	srv := &Server{
		cfg:        cfg,
		store:      db,
		runner:     runner.New(db),
		siteMgr:    siteMgr,
		siteAppMgr: appMgr,
		mux:        http.NewServeMux(),
	}
	srv.routes()
	return srv, cfg, db, fx
}

func TestSiteAppAPICreateInspectUpdateSwitchAndDelete(t *testing.T) {
	srv, cfg, db, fx := newTestSiteAppServer(t)

	projDir := filepath.Join(cfg.WebRootBase, "next-portal")
	_ = os.MkdirAll(projDir, 0755)
	_ = os.WriteFile(filepath.Join(projDir, "package.json"), []byte(`{"name":"next-portal","scripts":{"start":"next start","dev":"next dev","build":"next build"}}`), 0644)
	_ = os.WriteFile(filepath.Join(projDir, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'\n"), 0644)

	// 1. Inspect Dir
	reqInspect := httptest.NewRequest(http.MethodPost, "/api/sites/inspect-dir", strings.NewReader(`{"path":"`+projDir+`"}`))
	recInspect := httptest.NewRecorder()
	srv.mux.ServeHTTP(recInspect, reqInspect)
	if recInspect.Code != http.StatusOK {
		t.Fatalf("inspect-dir status=%d body=%s", recInspect.Code, recInspect.Body.String())
	}
	var inspOut struct {
		Inspect       siteapp.InspectResult `json:"inspect"`
		SuggestedPort int                   `json:"suggested_port"`
	}
	_ = json.Unmarshal(recInspect.Body.Bytes(), &inspOut)
	if inspOut.Inspect.Runtime != "nodejs" || inspOut.Inspect.PackageManager != "pnpm" || inspOut.SuggestedPort < siteapp.DefaultPortMin {
		t.Fatalf("unexpected inspect output: %+v", inspOut)
	}

	// 2. Tolak port internal tercadang (5432 PostgreSQL)
	badCreate := httptest.NewRequest(http.MethodPost, "/api/sites", strings.NewReader(`{
		"domain":"bad.test",
		"framework":"nodejs",
		"root_path":"`+projDir+`",
		"proxy_port":5432,
		"app_config":{"start_command":"pnpm run start"}
	}`))
	recBad := httptest.NewRecorder()
	srv.mux.ServeHTTP(recBad, badCreate)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on reserved port 5432, got %d (%s)", recBad.Code, recBad.Body.String())
	}

	// 3. Create Managed App VHost
	createBody, _ := json.Marshal(map[string]any{
		"domain":      "portal.test",
		"framework":   "nodejs",
		"root_path":   projDir,
		"proxy_port":  inspOut.SuggestedPort,
		"ssl_enabled": false,
		"app_config": map[string]any{
			"build_command": "",
			"start_command": "pnpm run start",
			"auto_start":    true,
		},
	})
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/sites", bytes.NewReader(createBody))
	recCreate := httptest.NewRecorder()
	srv.mux.ServeHTTP(recCreate, reqCreate)
	if recCreate.Code != http.StatusOK {
		t.Fatalf("POST /api/sites failed (%d): %s", recCreate.Code, recCreate.Body.String())
	}

	// GET /api/sites mengembalikan App + AppStatus
	reqList := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
	recList := httptest.NewRecorder()
	srv.mux.ServeHTTP(recList, reqList)
	var listOut []SiteWithAppRecord
	if err := json.Unmarshal(recList.Body.Bytes(), &listOut); err != nil || len(listOut) != 1 {
		t.Fatalf("unexpected GET /api/sites: %s (err=%v)", recList.Body.String(), err)
	}
	if listOut[0].App == nil || listOut[0].AppStatus == nil || listOut[0].AppStatus.ActiveState != "active" {
		t.Fatalf("expected enriched App & AppStatus, got %+v", listOut[0])
	}

	// 4. Env GET & PUT
	reqEnv := httptest.NewRequest(http.MethodPut, "/api/sites/app/env", strings.NewReader(`{"domain":"portal.test","raw":"FOO=bar\nPORT=9999","restart":true}`))
	recEnv := httptest.NewRecorder()
	srv.mux.ServeHTTP(recEnv, reqEnv)
	if recEnv.Code != http.StatusOK || !strings.Contains(recEnv.Body.String(), `"FOO"`) {
		t.Fatalf("PUT /api/sites/app/env failed (%d): %s", recEnv.Code, recEnv.Body.String())
	}

	// 5. Transition Managed App -> static harus membersihkan systemd service
	fx.cmds = nil
	reqUpdate := httptest.NewRequest(http.MethodPut, "/api/sites/portal.test", strings.NewReader(`{"framework":"static","root_path":"`+projDir+`"}`))
	recUpdate := httptest.NewRecorder()
	srv.mux.ServeHTTP(recUpdate, reqUpdate)
	if recUpdate.Code != http.StatusOK {
		t.Fatalf("PUT switch to static failed (%d): %s", recUpdate.Code, recUpdate.Body.String())
	}
	if appRec, _ := db.GetSiteApp("portal.test"); appRec != nil {
		t.Fatalf("expected site_apps record cleaned up when switching away from Managed App")
	}
	unitPath := srv.siteAppMgr.UnitPath("portal.test")
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected systemd unit file deleted when switching away from Managed App")
	}
	if _, err := os.Stat(projDir); err != nil {
		t.Fatalf("expected project directory preserved on switch/delete: %v", err)
	}

	// 6. Switch kembali ke go Managed App lalu DELETE VHost
	reqSwitchGo := httptest.NewRequest(http.MethodPut, "/api/sites/portal.test", strings.NewReader(`{
		"framework":"go",
		"root_path":"`+projDir+`",
		"proxy_port":3188,
		"app_config":{"start_command":"./bin/app"}
	}`))
	recSwitchGo := httptest.NewRecorder()
	srv.mux.ServeHTTP(recSwitchGo, reqSwitchGo)
	if recSwitchGo.Code != http.StatusOK {
		t.Fatalf("PUT switch to go failed (%d): %s", recSwitchGo.Code, recSwitchGo.Body.String())
	}
	if _, err := os.Stat(unitPath); err != nil {
		t.Fatalf("expected systemd unit created again: %v", err)
	}

	reqDel := httptest.NewRequest(http.MethodDelete, "/api/sites/portal.test", nil)
	recDel := httptest.NewRecorder()
	srv.mux.ServeHTTP(recDel, reqDel)
	if recDel.Code != http.StatusOK {
		t.Fatalf("DELETE /api/sites/portal.test failed (%d): %s", recDel.Code, recDel.Body.String())
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected systemd unit deleted on VHost delete")
	}
	if _, err := os.Stat(filepath.Join(projDir, "package.json")); err != nil {
		t.Fatalf("expected source files kept on disk after VHost delete: %v", err)
	}
}

func TestProjectDotEnvAndPortAutoUsedAndSynced(t *testing.T) {
	srv, cfg, db, _ := newTestSiteAppServer(t)

	projDir := filepath.Join(cfg.WebRootBase, "express-app")
	_ = os.MkdirAll(projDir, 0755)
	_ = os.WriteFile(filepath.Join(projDir, "package.json"), []byte(`{"name":"express-app","scripts":{"start":"node index.js"}}`), 0644)
	projEnv := filepath.Join(projDir, ".env")
	_ = os.WriteFile(projEnv, []byte("# App Config\nPORT=4500\nSESSION_KEY=abc\n"), 0600)

	// 1. Inspect -> suggested_port memakai 4500 dari .env
	reqInspect := httptest.NewRequest(http.MethodPost, "/api/sites/inspect-dir", strings.NewReader(`{"path":"`+projDir+`"}`))
	recInspect := httptest.NewRecorder()
	srv.mux.ServeHTTP(recInspect, reqInspect)
	if recInspect.Code != http.StatusOK || !strings.Contains(recInspect.Body.String(), `"suggested_port":4500`) {
		t.Fatalf("expected suggested_port=4500 from project .env, got (%d): %s", recInspect.Code, recInspect.Body.String())
	}

	// 2. Create tanpa mengirim proxy_port maupun use_project_env -> otomatis pakai .env proyek & port 4500!
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/sites", strings.NewReader(`{
		"domain":"express.test",
		"framework":"nodejs",
		"root_path":"`+projDir+`",
		"app_config":{"start_command":"pnpm run start"}
	}`))
	recCreate := httptest.NewRecorder()
	srv.mux.ServeHTTP(recCreate, reqCreate)
	if recCreate.Code != http.StatusOK {
		t.Fatalf("POST /api/sites failed (%d): %s", recCreate.Code, recCreate.Body.String())
	}

	appRec, err := db.GetSiteApp("express.test")
	if err != nil || appRec == nil {
		t.Fatalf("GetSiteApp failed: %+v err=%v", appRec, err)
	}
	if !appRec.UseProjectEnv || appRec.EnvFile != projEnv || appRec.Port != 4500 {
		t.Fatalf("expected automatic project .env (%s) & port 4500, got %+v", projEnv, appRec)
	}

	// 3. Edit PORT melalui endpoint /api/sites/app/env -> Nginx VHost & DB ikut sinkron ke 4600!
	reqEnvPut := httptest.NewRequest(http.MethodPut, "/api/sites/app/env", strings.NewReader(`{
		"domain":"express.test",
		"raw":"# App Config\nPORT=4600\nSESSION_KEY=abc\n",
		"restart":true
	}`))
	recEnvPut := httptest.NewRecorder()
	srv.mux.ServeHTTP(recEnvPut, reqEnvPut)
	if recEnvPut.Code != http.StatusOK || !strings.Contains(recEnvPut.Body.String(), `"port_synced":4600`) {
		t.Fatalf("PUT /api/sites/app/env did not sync port 4600 (%d): %s", recEnvPut.Code, recEnvPut.Body.String())
	}
	siteRec, ok := srv.siteMgr.GetSite("express.test")
	if !ok || siteRec.ProxyPort != 4600 {
		t.Fatalf("expected Nginx VHost synced to port 4600, got ok=%v site=%+v", ok, siteRec)
	}

	// 4. Delete VHost -> unit terhapus, tetapi .env proyek tetap aman!
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/sites/express.test", nil)
	recDel := httptest.NewRecorder()
	srv.mux.ServeHTTP(recDel, reqDel)
	if recDel.Code != http.StatusOK {
		t.Fatalf("DELETE failed (%d): %s", recDel.Code, recDel.Body.String())
	}
	if _, err := os.Stat(projEnv); err != nil {
		t.Fatalf("expected project .env kept intact after delete: %v", err)
	}
}
