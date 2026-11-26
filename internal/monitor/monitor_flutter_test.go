package monitor

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFlutterVersionFromDirReadOnly(t *testing.T) {
	flutterDir := t.TempDir()
	binDir := filepath.Join(flutterDir, "bin")
	cacheDir := filepath.Join(binDir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pastikan wrapper bin/flutter tidak pernah dieksekusi oleh monitor.
	marker := filepath.Join(t.TempDir(), "flutter-executed")
	trap := fmt.Sprintf("#!/bin/sh\ntouch %q\nexit 1\n", marker)
	if err := os.WriteFile(filepath.Join(binDir, "flutter"), []byte(trap), 0o755); err != nil {
		t.Fatal(err)
	}

	vJSON := `{"frameworkVersion":"3.50.1","flutterVersion":"3.50.1","dartSdkVersion":"3.14.0"}`
	if err := os.WriteFile(filepath.Join(cacheDir, "flutter.version.json"), []byte(vJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := detectFlutterVersionFromDir(flutterDir); got != "3.50.1" {
		t.Fatalf("detectFlutterVersionFromDir = %q, want 3.50.1", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("detectFlutterVersionFromDir tidak boleh mengeksekusi bin/flutter")
	}

	// Fallback ke file version jika flutter.version.json belum ada.
	_ = os.Remove(filepath.Join(cacheDir, "flutter.version.json"))
	if err := os.WriteFile(filepath.Join(flutterDir, "version"), []byte("3.48.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := detectFlutterVersionFromDir(flutterDir); got != "3.48.2" {
		t.Fatalf("detectFlutterVersionFromDir fallback = %q, want 3.48.2", got)
	}
}
