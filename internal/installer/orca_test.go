package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindExtractedOrcaDir(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "opt", "Orca")
	if err := os.MkdirAll(filepath.Join(app, "resources", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "resources", "bin", "orca-ide"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := findExtractedOrcaDir(root)
	if got != app {
		t.Fatalf("findExtractedOrcaDir = %q, want %q", got, app)
	}
	if bin := findOrcaBinary(got); bin != filepath.Join(app, "resources", "bin", "orca-ide") {
		t.Fatalf("findOrcaBinary = %q", bin)
	}
}

func TestWriteAndReadOrcaMode(t *testing.T) {
	home := t.TempDir()
	if got := readOrcaMode(home); got != "headless" {
		t.Fatalf("default mode = %q, want headless", got)
	}
	if err := writeOrcaMode(home, "", "desktop"); err != nil {
		t.Fatal(err)
	}
	if got := readOrcaMode(home); got != "desktop" {
		t.Fatalf("mode = %q, want desktop", got)
	}
}

func TestParseOrcaVersionString(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"1.4.264\n", "1.4.264"},
		{"v1.4.266", "1.4.266"},
		{"orca-ide 1.4.264", "1.4.264"},
		{"Warning: something\n1.4.264\n", "1.4.264"},
		{"error: failed", ""},
	}
	for _, tc := range cases {
		if got := parseOrcaVersionString(tc.in); got != tc.want {
			t.Fatalf("parseOrcaVersionString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if !HasNewerVersion("1.4.264", "1.4.266") {
		t.Fatal("expected 1.4.266 > 1.4.264")
	}
	if HasNewerVersion("1.4.266", "1.4.266") {
		t.Fatal("expected equal versions not to report update")
	}
}

func TestIsOrcaInstalledIgnoresConfigDirOnly(t *testing.T) {
	home := t.TempDir()
	// Hanya direktori konfigurasi ~/.config/orca (sisa setelah uninstall) tidak boleh dianggap terpasang.
	cfgDir := filepath.Join(home, ".config", "orca")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, ".mode"), []byte("headless\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsOrcaInstalled(home) {
		t.Fatalf("IsOrcaInstalled(%q) = true saat hanya .config/orca ada; seharusnya false", home)
	}

	// Setelah biner asli ~/.local/lib/orca-ide/resources/bin/orca-ide ada, baru terdeteksi terpasang.
	cliDir := filepath.Join(home, ".local", "lib", "orca-ide", "resources", "bin")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cliDir, "orca-ide"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !IsOrcaInstalled(home) {
		t.Fatalf("IsOrcaInstalled(%q) = false padahal biner CLI resmi ada", home)
	}
}

func TestFindOrcaServerBinaryInSupportsOrcaIDEAndOrca(t *testing.T) {
	dir1 := t.TempDir()
	bin1 := filepath.Join(dir1, "orca-server-1.4.272-linux-x64", "bin", "orca-ide")
	if err := os.MkdirAll(filepath.Dir(bin1), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin1, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findOrcaServerBinaryIn(dir1); got != bin1 {
		t.Fatalf("findOrcaServerBinaryIn = %q, want %q", got, bin1)
	}

	dir2 := t.TempDir()
	bin2 := filepath.Join(dir2, "bin", "orca")
	if err := os.MkdirAll(filepath.Dir(bin2), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin2, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findOrcaServerBinaryIn(dir2); got != bin2 {
		t.Fatalf("findOrcaServerBinaryIn = %q, want %q", got, bin2)
	}
}

func TestApplyOrcaModeRejectsUninstalledOrca(t *testing.T) {
	home := t.TempDir()
	err := applyOrcaMode(context.Background(), nil, "", home, "headless")
	if err == nil {
		t.Fatal("applyOrcaMode seharusnya gagal bila biner Orca belum terpasang")
	}
	if _, statErr := os.Stat(filepath.Join(home, ".config", "systemd", "user", "orca.service")); statErr == nil {
		t.Fatal("orca.service tidak boleh dibuat bila biner Orca belum terpasang")
	}
}

func TestEnsureOrcaUserServiceUnitWritesValidUnit(t *testing.T) {
	home := t.TempDir()
	cliDir := filepath.Join(home, ".local", "lib", "orca-ide", "resources", "bin")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cliDir, "orca-ide"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureOrcaUserServiceUnit("", home); err != nil {
		t.Fatalf("ensureOrcaUserServiceUnit error: %v", err)
	}
	unitBytes, err := os.ReadFile(filepath.Join(home, ".config", "systemd", "user", "orca.service"))
	if err != nil {
		t.Fatalf("gagal membaca unit orca.service: %v", err)
	}
	wantExec := "ExecStart=" + filepath.Join(home, ".local", "bin", "orca-ide") + " serve --supervise --force"
	if !strings.Contains(string(unitBytes), wantExec) {
		t.Fatalf("isi orca.service tidak memuat %q:\n%s", wantExec, string(unitBytes))
	}
}

func TestUserSystemctlEnvDeduplicatesHomeAndUser(t *testing.T) {
	t.Setenv("HOME", "/root")
	t.Setenv("USER", "root")
	env := userSystemctlEnv("tester", "/home/tester")
	homeCount := 0
	userCount := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOME=") {
			homeCount++
			if kv != "HOME=/home/tester" {
				t.Fatalf("unexpected HOME entry: %s", kv)
			}
		}
		if strings.HasPrefix(kv, "USER=") {
			userCount++
			if kv != "USER=tester" {
				t.Fatalf("unexpected USER entry: %s", kv)
			}
		}
	}
	if homeCount != 1 || userCount != 1 {
		t.Fatalf("expected single HOME and USER entries, got home=%d user=%d", homeCount, userCount)
	}
}
