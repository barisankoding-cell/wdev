package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureOrcaLauncherUsesSymlinks(t *testing.T) {
	withIsolatedOrcaHost(t)
	home := t.TempDir()
	appDir := orcaInstallDir(home)
	if err := os.MkdirAll(filepath.Join(appDir, "resources", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(appDir, "resources", "bin", "orca-ide")
	if err := os.WriteFile(cli, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ensureOrcaLauncher("tester", home); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(home, ".local", "bin")
	for _, name := range []string{"orca", "orca-desktop", "orca-pair", "orca-switch", "orca-update"} {
		fi, err := os.Lstat(filepath.Join(binDir, name))
		if err != nil {
			t.Fatalf("%s tidak ada: %v", name, err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s harus berupa symlink, bukan skrip shell", name)
		}
	}
	fi, err := os.Lstat(filepath.Join(binDir, "orca-ide"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("orca-ide harus berupa symlink ke CLI resmi Orca")
	}
}

func TestRemoveLegacyOrcaShellHelpers(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(binDir, "orca-switch")
	if err := os.WriteFile(legacy, []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	timer := filepath.Join(unitDir, "orca-update.timer")
	if err := os.WriteFile(timer, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	removeLegacyOrcaShellHelpers(home, "root")

	if _, err := os.Lstat(legacy); err == nil {
		if fi, _ := os.Lstat(legacy); fi.Mode()&os.ModeSymlink == 0 {
			t.Fatal("helper bash legacy seharusnya diganti symlink biner Go")
		}
	}
	if _, err := os.Stat(timer); err == nil {
		t.Fatal("orca-update.timer seharusnya dihapus")
	}
}
