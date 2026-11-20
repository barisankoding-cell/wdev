package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripManagedBlockIdempotent(t *testing.T) {
	lines := []string{
		"export FOO=1",
		managedBegin,
		". /etc/profile.d/wdev.sh",
		managedEnd,
		"export BAR=2",
	}
	got := stripManagedBlock(lines)
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, managedBegin) || strings.Contains(joined, managedEnd) {
		t.Fatalf("blok wdev tidak terhapus:\n%s", joined)
	}
	if !strings.Contains(joined, "export FOO=1") || !strings.Contains(joined, "export BAR=2") {
		t.Fatalf("isi lain ikut terhapus:\n%s", joined)
	}
}

func TestStripLegacyPathLines(t *testing.T) {
	lines := []string{
		"# --- DEV ENVIRONMENT & TOOLCHAIN PATHS ---",
		`export PATH="$HOME/.local/bin:$HOME/bin:/usr/local/bin:/usr/local/cargo/bin:$PATH"`,
		"# Muat environment profile dev",
		"for prof in /etc/profile.d/rust.sh /etc/profile.d/golang.sh; do",
		`  [ -r "$prof" ] && . "$prof" >/dev/null 2>&1`,
		"done",
		"alias ll='ls -lah'",
	}
	got := stripLegacyPathLines(lines)
	joined := strings.Join(got, "\n")
	for _, bad := range []string{"DEV ENVIRONMENT", "for prof in", "usr/local/cargo/bin:/usr/local/go/bin"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("baris legacy %q masih ada:\n%s", bad, joined)
		}
	}
	if !strings.Contains(joined, "alias ll='ls -lah'") {
		t.Fatalf("baris tak terkait terhapus:\n%s", joined)
	}
}

func TestStripOrcaBrowserBlock(t *testing.T) {
	lines := []string{
		"alias ll='ls -lah'",
		"# Orca ADE Browser Automation Backend",
		`if [ -z "${ORCA_BROWSER_EXECUTABLE:-}" ]; then`,
		`  for _b in /usr/bin/chromium; do`,
		`    export ORCA_BROWSER_EXECUTABLE="$_b"`,
		"  done",
		"fi",
		"export PATH=\"$HOME/.local/bin:$PATH\"",
	}
	got := stripOrcaBrowserBlock(lines)
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "ORCA_BROWSER_EXECUTABLE") {
		t.Fatalf("blok Orca browser masih ada:\n%s", joined)
	}
	if !strings.Contains(joined, "alias ll='ls -lah'") {
		t.Fatalf("baris awal hilang:\n%s", joined)
	}
}

func TestSyncUserShellEnvWritesSingleBlock(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".bashrc")
	original := "export FOO=1\n" +
		`export PATH="$HOME/.local/bin:$HOME/bin:/usr/local/bin:/usr/local/cargo/bin:/usr/local/go/bin:$PATH"` + "\n" +
		"# Orca ADE Browser Automation Backend\n" +
		`if [ -z "${ORCA_BROWSER_EXECUTABLE:-}" ]; then` + "\n" +
		`  export ORCA_BROWSER_EXECUTABLE="/usr/bin/chromium"` + "\n" +
		"fi\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	changed, err := SyncUserShellEnv("", home)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 1 {
		t.Fatalf("file berubah = %v, want 1 file", changed)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if strings.Count(content, managedBegin) != 1 {
		t.Fatalf("blok wdev harus tepat satu:\n%s", content)
	}
	if strings.Contains(content, "ORCA_BROWSER_EXECUTABLE") {
		t.Fatalf("blok Orca lama masih ada:\n%s", content)
	}
	if !strings.Contains(content, "export FOO=1") {
		t.Fatalf("export FOO hilang:\n%s", content)
	}

	changed, err = SyncUserShellEnv("", home)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Fatalf("sync kedua harus idempoten, tetapi mengubah %v", changed)
	}
}

func TestProfileScriptSingleSource(t *testing.T) {
	script := profileScript()
	for _, key := range []string{
		`export NODE_HOME="/usr/local/lib/nodejs/current"`,
		`export CAROOT="/etc/ssl/local-certs/ca"`,
		"ORCA_BROWSER_EXECUTABLE",
		`_wdev_prepend_path "/usr/local/cargo/bin"`,
	} {
		if !strings.Contains(script, key) {
			t.Fatalf("profileScript tidak memuat %q", key)
		}
	}
}
