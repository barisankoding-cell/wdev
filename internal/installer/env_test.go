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
	for _, bad := range []string{"DEV ENVIRONMENT", "for prof in", "$prof", "done", "usr/local/cargo/bin:/usr/local/go/bin"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("baris legacy %q masih ada:\n%s", bad, joined)
		}
	}
	if !strings.Contains(joined, "alias ll='ls -lah'") {
		t.Fatalf("baris tak terkait terhapus:\n%s", joined)
	}
}

func TestStripOrphanedProfDone(t *testing.T) {
	lines := []string{
		"source $ZSH/oh-my-zsh.sh",
		"",
		`  [ -r "$prof" ] && . "$prof" >/dev/null 2>&1`,
		"done",
		"typeset -U PATH path",
	}
	got := stripLegacyPathLines(lines)
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "$prof") || strings.Contains(joined, "done") {
		t.Fatalf("baris yatim $prof/done seharusnya dibersihkan:\n%s", joined)
	}
	if !strings.Contains(joined, "typeset -U PATH path") {
		t.Fatalf("baris valid ikut terhapus:\n%s", joined)
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

func TestReconcileZshrcCleansOrphanAndSilencesInit(t *testing.T) {
	broken := `export ZSH="$HOME/.oh-my-zsh"
if [[ -r "${XDG_CACHE_HOME:-$HOME/.cache}/p10k-instant-prompt-${(%):-%n}.zsh" ]]; then
  source "${XDG_CACHE_HOME:-$HOME/.cache}/p10k-instant-prompt-${(%):-%n}.zsh"
fi
ZSH_THEME="robbyrussell"
source $ZSH/oh-my-zsh.sh

  [ -r "$prof" ] && . "$prof" >/dev/null 2>&1
done
typeset -U PATH path
`
	got := reconcileZshrc(broken, "powerlevel10k")
	if strings.Contains(got, "$prof") || strings.Contains(got, "\ndone\n") {
		t.Fatalf("sisa loop yatim masih ada:\n%s", got)
	}
	if !strings.Contains(got, `ZSH_THEME="powerlevel10k/powerlevel10k"`) {
		t.Fatalf("tema tidak diperbarui ke powerlevel10k:\n%s", got)
	}
	if !strings.Contains(got, `ZSH_DISABLE_COMPFIX="true"`) || !strings.Contains(got, `zstyle ':omz:update' mode disabled`) {
		t.Fatalf("opsi senyap P10k tidak disisipkan:\n%s", got)
	}
	if strings.Index(got, `ZSH_DISABLE_COMPFIX="true"`) > strings.Index(got, "source $ZSH/oh-my-zsh.sh") {
		t.Fatalf("ZSH_DISABLE_COMPFIX harus sebelum source oh-my-zsh.sh:\n%s", got)
	}
}

func TestDetectNerdFont(t *testing.T) {
	home := t.TempDir()
	fontsDir := filepath.Join(home, ".local", "share", "fonts")
	if err := os.MkdirAll(fontsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fontsDir, "MesloLGS NF Regular.ttf"), []byte("ttf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DetectNerdFont(home); got != "MesloLGS NF" {
		t.Fatalf("DetectNerdFont = %q, want MesloLGS NF", got)
	}
}
