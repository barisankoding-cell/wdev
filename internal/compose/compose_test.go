package compose

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"wdev/internal/config"
	"wdev/internal/store"
)

type fakeRunner struct {
	lookPathFn func(bin string) (string, error)
	runFn      func(ctx context.Context, dir string, args ...string) ([]byte, error)
	streamFn   func(ctx context.Context, dir string, cb func(string), args ...string) error
	calls      [][]string
}

func (f *fakeRunner) LookPath(bin string) (string, error) {
	if f.lookPathFn != nil {
		return f.lookPathFn(bin)
	}
	return "/usr/bin/" + bin, nil
}

func (f *fakeRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if f.runFn != nil {
		return f.runFn(ctx, dir, args...)
	}
	return nil, nil
}

func (f *fakeRunner) Stream(ctx context.Context, dir string, cb func(string), args ...string) error {
	f.calls = append(f.calls, append([]string(nil), args...))
	if f.streamFn != nil {
		return f.streamFn(ctx, dir, cb, args...)
	}
	if cb != nil {
		cb("mock stream ok")
	}
	return nil
}

func TestParseComposeYAMLAndSecurityWarnings(t *testing.T) {
	yamlData := `
name: demo-stack
services:
  web:
    image: nginx:alpine
    ports:
      - "127.0.0.1:${WEB_PORT:-8080}:80"
    healthcheck:
      test: ["CMD", "true"]
  db:
    image: postgres:17
    ports:
      - "5432:5432"
    profiles: ["dev", "db"]
    depends_on:
      cache:
        condition: service_healthy
  cache:
    image: redis:7
    ports:
      - target: 6379
        published: "6379"
        protocol: tcp
jobs:
  nightly-backup:
    image: alpine:latest
    triggers:
      schedule:
        - cron: "0 3 * * *"
`
	svcs, profiles, hasJobs, err := ParseComposeYAML([]byte(yamlData))
	if err != nil {
		t.Fatalf("ParseComposeYAML failed: %v", err)
	}
	if !hasJobs {
		t.Fatalf("expected hasJobs=true when jobs: section is present")
	}
	if len(profiles) != 2 || profiles[0] != "db" || profiles[1] != "dev" {
		t.Fatalf("unexpected profiles: %+v", profiles)
	}
	if len(svcs) != 4 {
		t.Fatalf("expected 3 services + 1 job = 4 entries, got %d", len(svcs))
	}

	var allPorts []PortBinding
	foundJob := false
	for _, s := range svcs {
		allPorts = append(allPorts, s.Ports...)
		if s.Name == "nightly-backup" && s.IsJob && s.ScheduleCron == "0 3 * * *" {
			foundJob = true
		}
	}
	if !foundJob {
		t.Fatalf("expected parsed job nightly-backup with cron schedule")
	}

	warnings := analyzeSecurityWarnings(allPorts)
	if len(warnings) != 2 {
		t.Fatalf("expected 2 security warnings (5432 and 6379 exposed on 0.0.0.0), got %d: %+v", len(warnings), warnings)
	}
	for _, w := range warnings {
		if strings.Contains(w, "8080") {
			t.Fatalf("127.0.0.1:8080 should not produce a public security warning: %s", w)
		}
	}
}

func TestValidationGuards(t *testing.T) {
	if _, err := ValidateProjectName("Bad Name!"); err == nil {
		t.Fatalf("expected invalid project name to fail")
	}
	if got, err := ValidateProjectName("My-App_01"); err != nil || got != "my-app_01" {
		t.Fatalf("unexpected project name validation: got=%q err=%v", got, err)
	}
	if _, err := ValidateComposeFilename("../secret.yaml"); err == nil {
		t.Fatalf("expected path traversal in compose filename to fail")
	}
	if _, err := ValidateServiceName("web;rm -rf /"); err == nil {
		t.Fatalf("expected injection in service name to fail")
	}

	mgr := NewWithDeps(&config.Config{WebRootBase: "/var/www"}, nil, &fakeRunner{})
	if _, err := mgr.ValidateProjectDir("/etc/systemd"); err == nil {
		t.Fatalf("expected /etc/systemd to be rejected by ValidateProjectDir")
	}
	if _, err := mgr.ValidateProjectDir("/var/www/myapp"); err != nil {
		t.Fatalf("expected /var/www/myapp to be allowed: %v", err)
	}
}

func TestHybridDiscoveryAndLifecycleAndFiles(t *testing.T) {
	tmp := t.TempDir()
	db, err := store.New(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("store.New failed: %v", err)
	}
	defer db.Close()

	// Point HOME to tmp so ValidateProjectDir allows tmp subdirs
	t.Setenv("HOME", tmp)

	projDir := filepath.Join(tmp, "stack-one")
	discDir := filepath.Join(tmp, "discovered-stack")
	if err := os.MkdirAll(discDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(discDir, "compose.yaml"), []byte("services:\n  api:\n    image: node:22-alpine\n    ports:\n      - \"127.0.0.1:3001:3000\"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runner := &fakeRunner{
		runFn: func(ctx context.Context, dir string, args ...string) ([]byte, error) {
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
				return []byte(fmt.Sprintf(`[{"Name":"stack-one","Status":"running(1)","ConfigFiles":"%s"},{"Name":"ext-app","Status":"exited(1)","ConfigFiles":"%s"}]`,
					filepath.Join(projDir, "compose.yaml"),
					filepath.Join(discDir, "compose.yaml"),
				)), nil
			case strings.Contains(joined, "ps --all --format json"):
				return []byte(`[{"ID":"abc123","Name":"stack-one-web-1","Service":"web","Image":"nginx:alpine","State":"running","Status":"Up 2m","Health":"healthy","Publishers":[{"URL":"127.0.0.1","TargetPort":80,"PublishedPort":8080,"Protocol":"tcp"}]}]`), nil
			case strings.HasPrefix(joined, "stats --no-stream"):
				return []byte(`{"Name":"stack-one-web-1","CPUPerc":"0.15%","MemUsage":"12MiB / 1GiB","MemPerc":"1.17%","NetIO":"1kB / 2kB","BlockIO":"0B / 0B","PIDs":"2"}`), nil
			case strings.Contains(joined, "config --quiet"):
				return nil, nil
			default:
				return nil, nil
			}
		},
	}

	mgr := NewWithDeps(&config.Config{WebRootBase: tmp}, db, runner)

	eng := mgr.GetEngineInfo(context.Background())
	if !eng.DockerInstalled || !eng.ComposeInstalled || !eng.IsComposeV5 || eng.ComposeVersion != "5.8.0" || !eng.DaemonRunning {
		t.Fatalf("unexpected EngineInfo: %+v", eng)
	}

	created, err := mgr.CreateOrRegisterProject(context.Background(), CreateProjectOptions{
		Name:     "stack-one",
		DirPath:  projDir,
		Template: "nginx-static",
	})
	if err != nil {
		t.Fatalf("CreateOrRegisterProject failed: %v", err)
	}
	if created.Status != "running" || !created.Registered || len(created.Containers) != 1 || created.Containers[0].CPUPerc != "0.15%" {
		t.Fatalf("unexpected created project: %+v", created)
	}

	all, err := mgr.ListProjects(context.Background())
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 merged projects (registered + discovered), got %d", len(all))
	}

	// Save invalid compose should fail and keep previous content
	if _, err := mgr.SaveProjectFiles(context.Background(), "stack-one", "services: [broken", "", false); err == nil {
		t.Fatalf("expected invalid YAML save to fail")
	}

	// Save valid compose + .env
	updatedYAML := "services:\n  web:\n    image: nginx:alpine\n    ports:\n      - \"127.0.0.1:8088:80\"\n"
	files, err := mgr.SaveProjectFiles(context.Background(), "stack-one", updatedYAML, "APP_PORT=8088\n", true)
	if err != nil {
		t.Fatalf("SaveProjectFiles failed: %v", err)
	}
	if !files.HasEnvFile || len(files.EnvKeys) != 1 || files.EnvKeys[0] != "APP_PORT" {
		t.Fatalf("unexpected ProjectFiles: %+v", files)
	}
	envStat, err := os.Stat(filepath.Join(projDir, ".env"))
	if err != nil || envStat.Mode().Perm() != 0600 {
		t.Fatalf("expected .env mode 0600, got %v (err=%v)", envStat.Mode().Perm(), err)
	}

	// Action stream test
	var logs []string
	err = mgr.RunActionTask(context.Background(), "stack-one", ActionOptions{Action: "up", Build: true}, func(line string) {
		logs = append(logs, line)
	})
	if err != nil {
		t.Fatalf("RunActionTask failed: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("expected streamed logs from RunActionTask")
	}
}

func TestGetEngineInfoDetectsDaemonStartingState(t *testing.T) {
	runner := &fakeRunner{
		runFn: func(ctx context.Context, dir string, args ...string) ([]byte, error) {
			joined := strings.Join(args, " ")
			switch joined {
			case "version --format {{.Client.Version}}":
				return []byte("29.10.0\n"), nil
			case "compose version --short":
				return []byte("5.8.0\n"), nil
			case "info --format {{.ServerVersion}}|{{.DockerRootDir}}":
				return []byte("Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?\n"), fmt.Errorf("exit status 1")
			default:
				return nil, nil
			}
		},
	}

	mgr := NewWithDeps(&config.Config{}, nil, runner)
	mgr.dockerActivatingFn = func(context.Context) bool { return true }

	eng := mgr.GetEngineInfo(context.Background())
	if eng.DaemonRunning {
		t.Fatalf("expected DaemonRunning=false while daemon is activating")
	}
	if !eng.DaemonStarting {
		t.Fatalf("expected DaemonStarting=true when systemd reports activating")
	}
	if !strings.Contains(eng.DaemonError, "memulai") {
		t.Fatalf("unexpected DaemonError for starting daemon: %q", eng.DaemonError)
	}

	mgr.dockerActivatingFn = func(context.Context) bool { return false }
	stopped := mgr.GetEngineInfo(context.Background())
	if stopped.DaemonRunning || stopped.DaemonStarting {
		t.Fatalf("expected stopped daemon to have DaemonRunning=false and DaemonStarting=false: %+v", stopped)
	}
}
