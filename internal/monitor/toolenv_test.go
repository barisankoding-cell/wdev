package monitor

import (
	"strings"
	"testing"
)

func TestToolEnvHasRustupHome(t *testing.T) {
	t.Setenv("RUSTUP_HOME", "")
	env := toolEnv()
	var path string
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, "RUSTUP_HOME=") {
			found = true
		}
		if strings.HasPrefix(e, "PATH=") {
			path = strings.TrimPrefix(e, "PATH=")
		}
	}
	if !found {
		t.Fatal("RUSTUP_HOME tidak disuntikkan ke environment tool")
	}
	if !strings.Contains(path, "/usr/local/cargo/bin") {
		t.Fatalf("PATH tidak memuat area kanonik cargo: %s", path)
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
	} else if !strings.Contains(p, "/usr/local/") {
		t.Fatalf("rustc tidak diprioritaskan dari area kanonik: %s", p)
	}
}
