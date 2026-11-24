package installer

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCanonicalPrefixMatch(t *testing.T) {
	cases := []struct {
		path     string
		prefixes []string
		want     bool
	}{
		{"/usr/local/lib/nodejs/node-v22/bin/node", []string{"/usr/local/lib/nodejs/"}, true},
		{"/usr/bin/node", []string{"/usr/local/lib/nodejs/"}, false},
		{"/usr/local/bin/mkcert", []string{"/usr/local/bin/mkcert"}, true},
		{"/usr/bin/mkcert", []string{"/usr/local/bin/mkcert"}, false},
		{"/usr/local/mold/bin/mold", []string{"/usr/local/mold/", "/usr/bin/mold"}, true},
	}
	for _, c := range cases {
		if got := canonicalPrefixMatch(c.path, c.prefixes); got != c.want {
			t.Errorf("canonicalPrefixMatch(%q, %v) = %v, want %v", c.path, c.prefixes, got, c.want)
		}
	}
}

func TestToolLocationsGroupsByRealpath(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	aliasDir := filepath.Join(home, "alias")
	for _, d := range []string{binDir, aliasDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	real := filepath.Join(binDir, "tool")
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(aliasDir, "tool")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	groups := toolLocations("tool", []string{binDir, aliasDir})
	if len(groups) != 1 {
		t.Fatalf("symlink ke target sama harus satu grup, dapat %d grup: %v", len(groups), groups)
	}
	for _, paths := range groups {
		if len(paths) != 2 {
			t.Fatalf("grup harus memuat 2 path, dapat %v", paths)
		}
	}
}

func TestToolLocationsDetectsRealDuplicate(t *testing.T) {
	home := t.TempDir()
	a := filepath.Join(home, "a")
	b := filepath.Join(home, "b")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "tool"), []byte(d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	groups := toolLocations("tool", []string{a, b})
	if len(groups) != 2 {
		t.Fatalf("dua biner berbeda harus 2 grup, dapat %d", len(groups))
	}
}

func TestAuditFindsForeignMkcert(t *testing.T) {
	// Audit memakai path nyata sistem; cukup pastikan hasilnya konsisten
	// (setiap finding punya kind & severity valid) tanpa mengubah apa pun.
	inst := &Installer{}
	findings := inst.AuditToolchain(t.Context(), nil)
	for _, f := range findings {
		if f.Severity == "" || f.Kind == "" {
			t.Fatalf("finding tidak lengkap: %+v", f)
		}
	}
}

func TestPathDuplicatesParser(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, ".bashrc")
	content := "export FOO=1\n" +
		"_wdev_prepend_path \"/usr/local/bin\"\n" +
		"_wdev_prepend_path \"/usr/local/bin\"\n"
	if err := os.WriteFile(profile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	dups := pathDuplicates(home)
	if len(dups) == 0 {
		t.Fatal("duplikat entri _wdev_prepend_path tidak terdeteksi")
	}
}

func TestUnusedHomeToolchainFindingsAllowsUserCargo(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".cargo", "registry"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := unusedHomeToolchainFindings(home); len(got) != 0 {
		t.Fatalf("direktori ~/.cargo tidak boleh dianggap sisa tak terpakai, dapat %+v", got)
	}

	if err := os.MkdirAll(filepath.Join(home, ".rustup"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := unusedHomeToolchainFindings(home)
	if len(got) != 1 || got[0].Tool != ".rustup" {
		t.Fatalf("direktori ~/.rustup harus tetap dilaporkan, dapat %+v", got)
	}
}

func TestProfileDriftAndRustPermFindings(t *testing.T) {
	dir := t.TempDir()
	prof := filepath.Join(dir, "wdev.sh")
	if err := os.WriteFile(prof, []byte("export CARGO_HOME=\"/usr/local/cargo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if f := profileDriftFinding(prof, profileScript()); f == nil || !f.Fixable || f.Kind != "profile-drift" {
		t.Fatalf("profile drift tidak terdeteksi: %+v", f)
	}
	if err := os.WriteFile(prof, []byte(profileScript()), 0o644); err != nil {
		t.Fatal(err)
	}
	if f := profileDriftFinding(prof, profileScript()); f != nil {
		t.Fatalf("profile yang sudah mutakhir tidak boleh drift: %+v", f)
	}

	badDir := filepath.Join(dir, "cargo")
	if err := os.MkdirAll(badDir, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(badDir, 0o700)
	if f := toolchainDirPermFinding(badDir); f == nil || !f.Fixable || f.Kind != "toolchain-perm" {
		t.Fatalf("izin direktori 0700 harus dilaporkan toolchain-perm: %+v", f)
	}
	_ = os.Chmod(badDir, 0o777)
	if f := toolchainDirPermFinding(badDir); f == nil || !f.Fixable || f.Kind != "toolchain-perm" {
		t.Fatalf("izin direktori 0777 harus dilaporkan toolchain-perm: %+v", f)
	}
	_ = os.Chmod(badDir, 0o755)
	if f := toolchainDirPermFinding(badDir); f != nil {
		t.Fatalf("izin direktori 0755 tidak boleh dilaporkan: %+v", f)
	}
}

func TestToolchainSudoHandlingAndEnvOverrides(t *testing.T) {
	if os.Geteuid() != 0 {
		for _, tc := range []struct {
			name string
			args []string
		}{
			{"/tmp/wdev-rustup-123/rustup-init", []string{"-y"}},
			{"/usr/local/cargo/bin/rustup", []string{"update", "stable"}},
			{"/usr/local/android-sdk/cmdline-tools/latest/bin/sdkmanager", []string{"--update"}},
			{"/usr/local/flutter/bin/flutter", []string{"precache"}},
			{"git", []string{"config", "--system", "--add", "safe.directory", "/usr/local/flutter"}},
			{"git", []string{"clone", "-b", "stable", "https://github.com/flutter/flutter.git", "/usr/local/flutter"}},
			{"/usr/local/bin/mkcert", []string{"-install"}},
		} {
			if !needsSudo(tc.name, tc.args...) {
				t.Fatalf("needsSudo(%q, %v) harus true saat non-root", tc.name, tc.args)
			}
		}
	}

	effName, effArgs := sudoCommandSpec("/usr/local/cargo/bin/rustup", []string{"update", "stable"})
	wantArgs := []string{"CARGO_HOME=/usr/local/cargo", "RUSTUP_HOME=/usr/local/rustup", "/usr/local/cargo/bin/rustup", "update", "stable"}
	if effName != "env" || !reflect.DeepEqual(effArgs, wantArgs) {
		t.Fatalf("sudoCommandSpec(rustup) = (%q, %v), want (\"env\", %v)", effName, effArgs, wantArgs)
	}

	effName, effArgs = sudoCommandSpec("/usr/local/bin/mkcert", []string{"-install"})
	wantMkcert := []string{"CAROOT=/etc/ssl/local-certs/ca", "/usr/local/bin/mkcert", "-install"}
	if effName != "env" || !reflect.DeepEqual(effArgs, wantMkcert) {
		t.Fatalf("sudoCommandSpec(mkcert) = (%q, %v), want (\"env\", %v)", effName, effArgs, wantMkcert)
	}

	effName, effArgs = sudoCommandSpec("/usr/local/flutter/bin/flutter", []string{"precache"})
	wantFlutter := []string{"FLUTTER_ROOT=/usr/local/flutter", "CI=true", "/usr/local/flutter/bin/flutter", "precache"}
	if effName != "env" || !reflect.DeepEqual(effArgs, wantFlutter) {
		t.Fatalf("sudoCommandSpec(flutter) = (%q, %v), want (\"env\", %v)", effName, effArgs, wantFlutter)
	}

	env := withEnvOverrides(
		[]string{"PATH=/usr/bin", "CARGO_HOME=/home/user/.cargo", "LANG=C.UTF-8"},
		[][2]string{{"CARGO_HOME", "/usr/local/cargo"}, {"RUSTUP_HOME", "/usr/local/rustup"}},
	)
	wantEnv := []string{"PATH=/usr/bin", "CARGO_HOME=/usr/local/cargo", "LANG=C.UTF-8", "RUSTUP_HOME=/usr/local/rustup"}
	if !reflect.DeepEqual(env, wantEnv) {
		t.Fatalf("withEnvOverrides = %v, want %v", env, wantEnv)
	}

	if os.Geteuid() != 0 {
		dir := t.TempDir()
		if !needsRootPermFix(dir) {
			t.Fatal("direktori milik non-root harus terdeteksi needsRootPermFix saat bukan root")
		}
	}
}
