package installer

import (
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
