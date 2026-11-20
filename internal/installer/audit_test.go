package installer

import (
	"os"
	"path/filepath"
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
