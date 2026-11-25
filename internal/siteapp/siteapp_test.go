package siteapp

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wdev/internal/config"
	"wdev/internal/runner"
	"wdev/internal/store"
)

type mockSysExec struct {
	commands []string
	showOut  string
}

func (m *mockSysExec) Run(_ context.Context, name string, args ...string) (string, error) {
	line := name + " " + strings.Join(args, " ")
	m.commands = append(m.commands, line)
	if name == "systemctl" && len(args) > 0 && args[0] == "show" && m.showOut != "" {
		return m.showOut, nil
	}
	return "", nil
}

func (m *mockSysExec) WriteFile(_ context.Context, path string, data []byte, mode os.FileMode, _ string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

func (m *mockSysExec) ReadFile(_ context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (m *mockSysExec) RemoveFile(_ context.Context, path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *mockSysExec) FileExists(_ context.Context, path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (m *mockSysExec) EnsureDir(_ context.Context, dir string, mode os.FileMode, _ string) error {
	return os.MkdirAll(dir, mode)
}

func TestInspectDirectoryRejectsDisallowedPaths(t *testing.T) {
	if _, err := InspectDirectory("/etc/nginx"); err == nil {
		t.Fatalf("expected InspectDirectory to reject /etc/nginx")
	}
	if _, err := InspectDirectory("relative/path"); err == nil {
		t.Fatalf("expected InspectDirectory to reject relative path")
	}
}

func TestInspectDirectoryDetectsNodeGoAndRust(t *testing.T) {
	tmp := t.TempDir()

	// 1. Node.js + pnpm + .env
	nodeDir := filepath.Join(tmp, "node-api")
	if err := os.MkdirAll(nodeDir, 0755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(nodeDir, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'\n"), 0644)
	_ = os.WriteFile(filepath.Join(nodeDir, ".env"), []byte("PORT=4100\nAPI_SECRET=\"hello world\"\n"), 0600)
	_ = os.WriteFile(filepath.Join(nodeDir, "package.json"), []byte(`{"name":"my-node-api","scripts":{"dev":"vite","build":"vite build","start":"node dist/index.js"}}`), 0644)

	nodeRes, err := inspectDirectoryPath(nodeDir)
	if err != nil {
		t.Fatalf("inspect node failed: %v", err)
	}
	if !nodeRes.Detected || nodeRes.Runtime != RuntimeNodeJS || nodeRes.PackageManager != "pnpm" {
		t.Fatalf("unexpected node inspect result: %+v", nodeRes)
	}
	if nodeRes.RecommendedStart != "pnpm run start" || nodeRes.RecommendedBuild != "pnpm install && pnpm run build" {
		t.Fatalf("unexpected node commands: build=%q start=%q", nodeRes.RecommendedBuild, nodeRes.RecommendedStart)
	}
	if !nodeRes.HasDotEnv || nodeRes.DotEnvVarCount != 2 || nodeRes.DetectedPortHint != 4100 || nodeRes.ProjectEnvPath != filepath.Join(nodeDir, ".env") {
		t.Fatalf("unexpected dotenv metadata: %+v", nodeRes)
	}

	// 2. Go module dengan cmd/server
	goDir := filepath.Join(tmp, "go-svc")
	_ = os.MkdirAll(filepath.Join(goDir, "cmd", "api"), 0755)
	_ = os.WriteFile(filepath.Join(goDir, "go.mod"), []byte("module example.com/gosvc\n\ngo 1.24\n"), 0644)
	_ = os.WriteFile(filepath.Join(goDir, "cmd", "api", "main.go"), []byte("package main\nfunc main(){}\n"), 0644)

	goRes, err := inspectDirectoryPath(goDir)
	if err != nil {
		t.Fatalf("inspect go failed: %v", err)
	}
	if !goRes.Detected || goRes.Runtime != RuntimeGo || goRes.AppName != "example.com/gosvc" {
		t.Fatalf("unexpected go inspect result: %+v", goRes)
	}
	if goRes.RecommendedBuild != "go build -o ./bin/app ./cmd/api" || goRes.RecommendedStart != "./bin/app" {
		t.Fatalf("unexpected go commands: build=%q start=%q", goRes.RecommendedBuild, goRes.RecommendedStart)
	}

	// 3. Rust Cargo
	rustDir := filepath.Join(tmp, "rust-svc")
	_ = os.MkdirAll(rustDir, 0755)
	_ = os.WriteFile(filepath.Join(rustDir, "Cargo.toml"), []byte("[package]\nname = \"edge-worker\"\nversion = \"0.1.0\"\n"), 0644)

	rustRes, err := inspectDirectoryPath(rustDir)
	if err != nil {
		t.Fatalf("inspect rust failed: %v", err)
	}
	if !rustRes.Detected || rustRes.Runtime != RuntimeRust || rustRes.AppName != "edge-worker" {
		t.Fatalf("unexpected rust inspect result: %+v", rustRes)
	}
	if rustRes.RecommendedBuild != "cargo build --release" || rustRes.RecommendedStart != "./target/release/edge-worker" {
		t.Fatalf("unexpected rust commands: build=%q start=%q", rustRes.RecommendedBuild, rustRes.RecommendedStart)
	}
}

func TestServiceNameForDomainSanitizationAndLengthLimit(t *testing.T) {
	short := ServiceNameForDomain("API.Erposoft.my.id.")
	if short != "wdev-app-api-erposoft-my-id" {
		t.Fatalf("unexpected short service name: %s", short)
	}
	longA := ServiceNameForDomain("very-long-subdomain-alpha-service-one.example.org")
	longB := ServiceNameForDomain("very-long-subdomain-alpha-service-two.example.org")
	if len(longA) > 48 || len(longB) > 48 {
		t.Fatalf("expected truncated service name <= 48 chars, got %d & %d", len(longA), len(longB))
	}
	if longA == longB {
		t.Fatalf("expected hash suffix to avoid collision, both got %s", longA)
	}
}

func TestRenderSystemdUnitAndEnvSerialization(t *testing.T) {
	unit := RenderSystemdUnit(store.SiteAppRecord{
		Domain:       "app.test",
		Runtime:      RuntimeNodeJS,
		WorkingDir:   "/home/dietpi/apps/node-app",
		StartCommand: "pnpm run start -- --port 100%ready",
		Port:         3108,
		RunUser:      "dietpi",
		ServiceName:  "wdev-app-app-test",
		EnvFile:      "/etc/wdev/apps/wdev-app-app-test.env",
	})
	for _, want := range []string{
		"User=dietpi",
		"WorkingDirectory=/home/dietpi/apps/node-app",
		"EnvironmentFile=-/etc/wdev/apps/wdev-app-app-test.env",
		"Environment=PORT=3108",
		"Environment=CARGO_HOME=/usr/local/cargo",
		"Environment=RUSTUP_HOME=/usr/local/rustup",
		"/usr/local/lib/nodejs/current/bin",
		"/bin/sh -lc 'pnpm run start -- --port 100%%ready'",
		"MemoryAccounting=yes",
		"SyslogIdentifier=wdev-app-app-test",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("rendered unit missing %q:\n%s", want, unit)
		}
	}

	rawEnv, err := SerializeEnvEntries([]EnvEntry{
		{Key: "PORT", Value: "3108"},
		{Key: "APP_TITLE", Value: "My App #1"},
	})
	if err != nil {
		t.Fatalf("SerializeEnvEntries failed: %v", err)
	}
	parsed, err := ParseEnvContent(rawEnv)
	if err != nil || len(parsed) != 2 || parsed[1].Value != "My App #1" {
		t.Fatalf("round-trip ParseEnvContent failed: parsed=%+v err=%v", parsed, err)
	}
}

func TestLifecycleProvisionUpdateAndRemovePreservesProjectFiles(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "wdev.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	projectDir := filepath.Join(tmp, "project")
	_ = os.MkdirAll(projectDir, 0755)
	_ = os.WriteFile(filepath.Join(projectDir, ".env"), []byte("PORT=9999\nDATABASE_URL=postgres://localhost/app\n"), 0600)
	_ = os.WriteFile(filepath.Join(projectDir, "main.go"), []byte("package main\nfunc main(){}\n"), 0644)

	cfg := &config.Config{WdevEtc: filepath.Join(tmp, "etc")}
	mgr := New(cfg, db, nil)
	mgr.SetSystemdDirForTest(filepath.Join(tmp, "systemd"))
	mgr.SetAllowAnyDirForTest(true)
	mock := &mockSysExec{
		showOut: "ActiveState=active\nSubState=running\nUnitFileState=enabled\nMainPID=4321\nMemoryCurrent=1048576\n",
	}
	mgr.SetExecForTest(mock)

	ctx := context.Background()
	saved, err := mgr.ProvisionOrSync(ctx, store.SiteAppRecord{
		Domain:       "api.test",
		Runtime:      RuntimeGo,
		WorkingDir:   projectDir,
		BuildCommand: "go build -o ./bin/app .",
		StartCommand: "./bin/app",
		Port:         3102,
		RunUser:      "dietpi",
		AutoStart:    true,
	}, true, true, "")
	if err != nil {
		t.Fatalf("ProvisionOrSync failed: %v", err)
	}

	unitPath := mgr.UnitPath("api.test")
	envPath := mgr.EnvPath("api.test")
	if _, err := os.Stat(unitPath); err != nil {
		t.Fatalf("expected systemd unit file at %s: %v", unitPath, err)
	}
	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("expected env file at %s: %v", envPath, err)
	}
	// Imported DATABASE_URL harus ada, sedangkan PORT mengikuti port VHost (3102).
	if !strings.Contains(string(envBytes), "DATABASE_URL=postgres://localhost/app") || !strings.Contains(string(envBytes), "PORT=3102") {
		t.Fatalf("unexpected synced env content:\n%s", string(envBytes))
	}

	st := mgr.Status(ctx, "api.test")
	if st.ActiveState != "active" || st.MainPID != 4321 || !st.Enabled || st.MemoryBytes != 1048576 {
		t.Fatalf("unexpected runtime status: %+v", st)
	}

	// Update port -> service harus daemon-reload + restart + sync env PORT
	mock.commands = nil
	updated := saved
	updated.Port = 3109
	if _, err := mgr.UpdateAndSync(ctx, &saved, updated); err != nil {
		t.Fatalf("UpdateAndSync failed: %v", err)
	}
	envBytesAfter, _ := os.ReadFile(envPath)
	if !strings.Contains(string(envBytesAfter), "PORT=3109") {
		t.Fatalf("expected updated PORT=3109 in env file:\n%s", string(envBytesAfter))
	}
	joinedCmds := strings.Join(mock.commands, "\n")
	if !strings.Contains(joinedCmds, "systemctl daemon-reload") || !strings.Contains(joinedCmds, "systemctl restart wdev-app-api-test.service") {
		t.Fatalf("expected daemon-reload & restart on update, got:\n%s", joinedCmds)
	}

	// RemoveApp -> hapus unit & env + DB record, TETAP pertahankan folder kode proyek di disk!
	mock.commands = nil
	if err := mgr.RemoveApp(ctx, "api.test"); err != nil {
		t.Fatalf("RemoveApp failed: %v", err)
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected unit file removed, err=%v", err)
	}
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatalf("expected env file removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "main.go")); err != nil {
		t.Fatalf("expected project files on disk to remain untouched: %v", err)
	}
	dbRec, _ := db.GetSiteApp("api.test")
	if dbRec != nil {
		t.Fatalf("expected DB site_apps record deleted, got %+v", dbRec)
	}
}

func TestStartBuildStreamsOutputAndRestartsService(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "build.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	projectDir := filepath.Join(tmp, "proj")
	_ = os.MkdirAll(projectDir, 0755)

	cfg := &config.Config{WdevEtc: filepath.Join(tmp, "etc")}
	mgr := New(cfg, db, nil)
	mgr.SetSystemdDirForTest(filepath.Join(tmp, "systemd"))
	mgr.SetAllowAnyDirForTest(true)
	mock := &mockSysExec{}
	mgr.SetExecForTest(mock)

	if _, err := mgr.ProvisionOrSync(context.Background(), store.SiteAppRecord{
		Domain:       "build.test",
		Runtime:      RuntimeNodeJS,
		WorkingDir:   projectDir,
		BuildCommand: "echo building_app_ok && touch built.flag",
		StartCommand: "node server.js",
		Port:         3110,
		RunUser:      "dietpi",
		AutoStart:    true,
	}, false, false, ""); err != nil {
		t.Fatal(err)
	}

	mock.commands = nil
	r := runner.New(db)
	ch := r.Subscribe()
	defer r.Unsubscribe(ch)

	if _, err := mgr.StartBuild(r, "build.test", true); err != nil {
		t.Fatalf("StartBuild failed: %v", err)
	}

	deadline := time.After(5 * time.Second)
	var logs []string
	var finishedStatus string
loop:
	for {
		select {
		case ev := <-ch:
			if ev.Type == runner.EventLog {
				logs = append(logs, ev.Message)
			}
			if ev.Type == runner.EventFinish {
				finishedStatus = ev.Status
				break loop
			}
		case <-deadline:
			t.Fatal("timed out waiting for StartBuild finish event")
		}
	}

	if finishedStatus != "success" {
		t.Fatalf("expected success build status, got %s (logs: %v)", finishedStatus, logs)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "built.flag")); err != nil {
		t.Fatalf("expected build artifact created in WorkingDir: %v", err)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "building_app_ok") {
		t.Fatalf("expected stdout streamed to runner logs: %v", logs)
	}
	if !strings.Contains(strings.Join(mock.commands, "\n"), "systemctl restart wdev-app-build-test.service") {
		t.Fatalf("expected service restart after build: %v", mock.commands)
	}
}

func TestSuggestFreePortSkipsUsedAndListeningPorts(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot open loopback listener")
	}
	defer ln.Close()
	busyPort := ln.Addr().(*net.TCPAddr).Port

	used := map[int]bool{3100: true}
	reserved := map[int]bool{3101: true, busyPort: true}
	port, err := SuggestFreePort(used, reserved)
	if err != nil {
		t.Fatalf("SuggestFreePort failed: %v", err)
	}
	if port == 3100 || port == 3101 || port == busyPort || port < DefaultPortMin {
		t.Fatalf("unexpected suggested port: %d", port)
	}
}

func TestUseProjectEnvDirectlyPreservesCommentsAndNeverDeletesOnRemove(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "proj-env.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	projectDir := filepath.Join(tmp, "node-direct-env")
	_ = os.MkdirAll(projectDir, 0755)
	projectEnvPath := filepath.Join(projectDir, ".env")
	origEnv := "# Komentar Proyek\nPORT=4200\nAPI_KEY=\"secret-xyz\"\n"
	if err := os.WriteFile(projectEnvPath, []byte(origEnv), 0640); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{WdevEtc: filepath.Join(tmp, "etc")}
	mgr := New(cfg, db, nil)
	mgr.SetSystemdDirForTest(filepath.Join(tmp, "systemd"))
	mgr.SetAllowAnyDirForTest(true)
	mock := &mockSysExec{}
	mgr.SetExecForTest(mock)

	ctx := context.Background()
	saved, err := mgr.ProvisionOrSync(ctx, store.SiteAppRecord{
		Domain:        "nodeenv.test",
		Runtime:       RuntimeNodeJS,
		WorkingDir:    projectDir,
		StartCommand:  "pnpm run start",
		Port:          4200,
		RunUser:       "dietpi",
		AutoStart:     true,
		UseProjectEnv: true,
	}, true, false, "")
	if err != nil {
		t.Fatalf("ProvisionOrSync(UseProjectEnv) failed: %v", err)
	}
	if !saved.UseProjectEnv || saved.EnvFile != projectEnvPath {
		t.Fatalf("expected UseProjectEnv=true and EnvFile=%s, got %+v", projectEnvPath, saved)
	}

	unitBytes, err := os.ReadFile(mgr.UnitPath("nodeenv.test"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unitBytes), "EnvironmentFile=-"+projectEnvPath) {
		t.Fatalf("expected systemd unit pointing directly to %s:\n%s", projectEnvPath, string(unitBytes))
	}

	// File .env proyek tidak terhapus komentar atau format aslinya bila PORT sudah sama
	afterProv, _ := os.ReadFile(projectEnvPath)
	if string(afterProv) != origEnv {
		t.Fatalf("expected project .env untouched when PORT matches, got:\n%s", string(afterProv))
	}

	// Edit VHost ke port 4300 -> hanya baris PORT yang berubah, komentar tetap utuh!
	updated := saved
	updated.Port = 4300
	if _, err := mgr.UpdateAndSync(ctx, &saved, updated); err != nil {
		t.Fatalf("UpdateAndSync failed: %v", err)
	}
	afterUpdate, _ := os.ReadFile(projectEnvPath)
	if !strings.Contains(string(afterUpdate), "# Komentar Proyek") || !strings.Contains(string(afterUpdate), "PORT=4300") {
		t.Fatalf("expected comments preserved & PORT updated to 4300, got:\n%s", string(afterUpdate))
	}

	// RemoveApp TIDAK boleh menghapus .env proyek milik user!
	if err := mgr.RemoveApp(ctx, "nodeenv.test"); err != nil {
		t.Fatalf("RemoveApp failed: %v", err)
	}
	if _, err := os.Stat(projectEnvPath); err != nil {
		t.Fatalf("expected project .env NEVER deleted by RemoveApp, got err=%v", err)
	}
}

func TestReadProcTreeRSSBytesAndStatusMemoryFallback(t *testing.T) {
	procDir := t.TempDir()
	// Tree: 100 (2000 kB) -> 101 (15000 kB) -> 102 (3000 kB); unrelated PID 999 (90000 kB)
	for pid, body := range map[string]string{
		"100": "Name:\tsh\nPPid:\t1\nVmRSS:\t2000 kB\n",
		"101": "Name:\tnode\nPPid:\t100\nVmRSS:\t15000 kB\n",
		"102": "Name:\tworker\nPPid:\t101\nVmRSS:\t3000 kB\n",
		"999": "Name:\tother\nPPid:\t1\nVmRSS:\t90000 kB\n",
	} {
		if err := os.MkdirAll(filepath.Join(procDir, pid), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(procDir, pid, "status"), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	got := readProcTreeRSSBytes(procDir, 100)
	want := int64((2000 + 15000 + 3000) * 1024)
	if got != want {
		t.Fatalf("readProcTreeRSSBytes = %d, want %d", got, want)
	}

	// Status harus membersihkan MemoryBytes bila service berhenti (inactive).
	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "mem.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mgr := New(&config.Config{WdevEtc: filepath.Join(tmp, "etc")}, db, nil)
	mgr.SetExecForTest(&mockSysExec{
		showOut: "ActiveState=inactive\nSubState=dead\nMainPID=0\nMemoryCurrent=4096\n",
	})
	st := mgr.Status(context.Background(), "stopped.test")
	if st.MemoryBytes != 0 {
		t.Fatalf("expected MemoryBytes=0 when inactive, got %d", st.MemoryBytes)
	}
}

func TestAvailableRunUsersAndDefaultRunUser(t *testing.T) {
	mgr := New(&config.Config{}, nil, nil)
	users := mgr.AvailableRunUsers()
	for _, u := range users {
		if u.IsRoot || u.Username == "root" {
			t.Fatalf("AvailableRunUsers must only return non-root users, got %+v", u)
		}
	}
	def := mgr.DefaultRunUser()
	if def == "" {
		t.Fatal("DefaultRunUser must not return empty string")
	}
	if len(users) > 0 && def != users[0].Username {
		t.Fatalf("DefaultRunUser() = %q, want %q", def, users[0].Username)
	}

	// InspectDirectory should also surface non-root folder owner when applicable.
	projDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(projDir, "go.mod"), []byte("module demo\n\ngo 1.24\n"), 0644)
	res, err := inspectDirectoryPath(projDir)
	if err != nil {
		t.Fatalf("inspectDirectoryPath failed: %v", err)
	}
	if os.Geteuid() != 0 && os.Getuid() >= 1000 && res.OwnerUser == "" {
		t.Fatalf("expected non-root OwnerUser for %s, got empty", projDir)
	}
}
