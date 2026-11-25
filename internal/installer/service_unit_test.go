package installer

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWdevServiceUnitContentHasToolchainEnv(t *testing.T) {
	unit := WdevServiceUnitContent("3200", "0.0.0.0")
	for _, kv := range toolchainServiceEnv {
		if !strings.Contains(unit, `Environment="`+kv[0]+`=`+kv[1]+`"`) {
			t.Errorf("unit tidak memuat Environment %s=%s", kv[0], kv[1])
		}
	}
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/wdev serve --port 3200 --host 0.0.0.0") {
		t.Errorf("ExecStart tidak sesuai:\n%s", unit)
	}
	if !strings.Contains(unit, `Environment="PATH=`) {
		t.Error("unit tidak memuat PATH toolchain")
	}
	if !strings.Contains(unit, "UMask=0022") {
		t.Error("unit tidak memuat UMask=0022")
	}
	if !strings.Contains(unit, "PrivateTmp=true") {
		t.Error("unit tidak memuat PrivateTmp=true")
	}
}

func TestWdevServiceUnitContentDefaults(t *testing.T) {
	unit := WdevServiceUnitContent("", "")
	if !strings.Contains(unit, "--port 3200 --host 0.0.0.0") {
		t.Fatalf("default port/host salah:\n%s", unit)
	}
}

func TestParseServiceExec(t *testing.T) {
	cases := []struct {
		unit       string
		port, host string
	}{
		{"ExecStart=/usr/local/bin/wdev serve --port 3200 --host 0.0.0.0\n", "3200", "0.0.0.0"},
		{"ExecStart=/usr/local/bin/wdev serve --port=8080 --host=127.0.0.1\n", "8080", "127.0.0.1"},
		{"ExecStart=/usr/local/bin/wdev serve\n", "3200", "0.0.0.0"},
	}
	for _, c := range cases {
		p, h := parseServiceExec([]byte(c.unit))
		if p != c.port || h != c.host {
			t.Errorf("parseServiceExec(%q) = %s,%s; want %s,%s", c.unit, p, h, c.port, c.host)
		}
	}
}

func TestUnitContentIsIdempotent(t *testing.T) {
	a := WdevServiceUnitContent("3200", "0.0.0.0")
	b := WdevServiceUnitContent("3200", "0.0.0.0")
	if a != b {
		t.Fatal("render unit tidak deterministik")
	}
}

func TestReloadAndRestartWdevServiceCallsRestart(t *testing.T) {
	var calls [][]string
	fakeRun := func(_ context.Context, _ LogCallback, name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	}

	if err := reloadAndRestartWdevService(context.Background(), nil, fakeRun); err != nil {
		t.Fatalf("reloadAndRestartWdevService error: %v", err)
	}

	want := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", "wdev.service"},
		{"systemctl", "restart", "wdev.service"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("urutan perintah systemctl salah:\ngot:  %v\nwant: %v", calls, want)
	}
}

func TestApplyWdevServiceUnitUpdate(t *testing.T) {
	t.Run("outside service restarts unit", func(t *testing.T) {
		var calls [][]string
		fakeRun := func(_ context.Context, _ LogCallback, name string, args ...string) error {
			calls = append(calls, append([]string{name}, args...))
			return nil
		}
		if err := applyWdevServiceUnitUpdate(context.Background(), nil, false, fakeRun); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := [][]string{
			{"systemctl", "daemon-reload"},
			{"systemctl", "enable", "wdev.service"},
			{"systemctl", "restart", "wdev.service"},
		}
		if !reflect.DeepEqual(calls, want) {
			t.Fatalf("got %v, want %v", calls, want)
		}
	})

	t.Run("inside service only reloads daemon", func(t *testing.T) {
		var calls [][]string
		fakeRun := func(_ context.Context, _ LogCallback, name string, args ...string) error {
			calls = append(calls, append([]string{name}, args...))
			return nil
		}
		if err := applyWdevServiceUnitUpdate(context.Background(), nil, true, fakeRun); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := [][]string{
			{"systemctl", "daemon-reload"},
		}
		if !reflect.DeepEqual(calls, want) {
			t.Fatalf("got %v, want %v", calls, want)
		}
	})
}

func TestIsCgroupInUnit(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"cgroup v2 wdev", "0::/system.slice/wdev.service\n", true},
		{"cgroup v1 wdev", "1:name=systemd:/system.slice/wdev.service\n", true},
		{"sub-cgroup wdev", "0::/system.slice/wdev.service/worker\n", true},
		{"desktop terminal scope", "0::/user.slice/user-1000.slice/user@1000.service/app.slice/ptyxis-spawn-c82f885c.scope\n", false},
		{"different prefix unit", "0::/system.slice/my-wdev.service\n", false},
		{"different suffix unit", "0::/system.slice/wdev.service.bak\n", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		if got := isCgroupInUnit(c.content, "wdev.service"); got != c.want {
			t.Errorf("%s: isCgroupInUnit = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestServiceUnitNeedsSyncAt(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "wdev.service")

	if serviceUnitNeedsSyncAt(unitPath) {
		t.Fatal("unit yang belum ada tidak boleh dilaporkan perlu sync")
	}

	if err := os.WriteFile(unitPath, []byte(WdevServiceUnitContent("3200", "0.0.0.0")), 0o644); err != nil {
		t.Fatal(err)
	}
	if serviceUnitNeedsSyncAt(unitPath) {
		t.Fatal("unit yang sudah mutakhir tidak boleh dilaporkan perlu sync")
	}

	outdated := strings.Replace(WdevServiceUnitContent("3200", "0.0.0.0"), "Environment=\"CAROOT=/etc/ssl/local-certs/ca\"\n", "", 1)
	if err := os.WriteFile(unitPath, []byte(outdated), 0o644); err != nil {
		t.Fatal(err)
	}
	if !serviceUnitNeedsSyncAt(unitPath) {
		t.Fatal("unit tanpa CAROOT harus dilaporkan perlu sync")
	}
}

func TestEnableDockerUnitsEnablesAllAvailableUnits(t *testing.T) {
	var calls [][]string
	enableDockerUnits(
		context.Background(),
		nil,
		func(unit string) bool { return unit == "containerd" || unit == "docker.socket" || unit == "docker" },
		func(_ context.Context, _ LogCallback, name string, args ...string) error {
			calls = append(calls, append([]string{name}, args...))
			return nil
		},
	)
	want := [][]string{
		{"systemctl", "enable", "containerd"},
		{"systemctl", "enable", "docker.socket"},
		{"systemctl", "enable", "docker"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("enableDockerUnits calls = %v, want %v", calls, want)
	}
}
