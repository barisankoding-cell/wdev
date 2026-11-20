package installer

import (
	"os"
	"path/filepath"
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
