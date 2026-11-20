package monitor

import (
	"os"
	"strings"
	"testing"
)

func TestToolEnvHasRustupHome(t *testing.T) {
	t.Setenv("RUSTUP_HOME", "")
	env := toolEnv()
	var path string
	var rustupHome string
	pathCount := 0
	for _, e := range env {
		if strings.HasPrefix(e, "RUSTUP_HOME=") {
			rustupHome = strings.TrimPrefix(e, "RUSTUP_HOME=")
		}
		if strings.HasPrefix(e, "PATH=") {
			path = strings.TrimPrefix(e, "PATH=")
			pathCount++
		}
	}
	if rustupHome != "/usr/local/rustup" {
		t.Fatalf("RUSTUP_HOME kosong harus diisi default /usr/local/rustup, dapat %q", rustupHome)
	}
	if !strings.Contains(path, "/usr/local/cargo/bin") {
		t.Fatalf("PATH tidak memuat area kanonik cargo: %s", path)
	}
	if pathCount != 1 {
		t.Fatalf("PATH tidak boleh duplikat di env, jumlah=%d", pathCount)
	}
}

func TestToolEnvKeepsExistingValue(t *testing.T) {
	t.Setenv("RUSTUP_HOME", "/custom/rustup")
	for _, e := range toolEnv() {
		if strings.HasPrefix(e, "RUSTUP_HOME=") && e != "RUSTUP_HOME=/custom/rustup" {
			t.Fatalf("nilai env yang sudah ada ditimpa: %s", e)
		}
	}
}

func TestToolPathFindsRustc(t *testing.T) {
	if p := toolPath("rustc"); p == "" {
		t.Skip("rustc tidak terpasang di lingkungan tes")
	} else if _, err := os.Stat("/usr/local/cargo/bin/rustc"); err == nil && !strings.Contains(p, "/usr/local/") {
		t.Fatalf("rustc tidak diprioritaskan dari area kanonik: %s", p)
	}
}
