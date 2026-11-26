package installer

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

func TestReadFlutterVersionMetadataReadOnly(t *testing.T) {
	flutterDir := t.TempDir()
	binDir := filepath.Join(flutterDir, "bin")
	cacheDir := filepath.Join(binDir, "cache")
	dartVersionDir := filepath.Join(cacheDir, "dart-sdk")
	if err := os.MkdirAll(dartVersionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Pasang skrip jebakan untuk memastikan wrapper bin/flutter dan bin/dart tidak pernah dieksekusi.
	marker := filepath.Join(t.TempDir(), "invoked")
	trapScript := fmt.Sprintf("#!/bin/sh\ntouch %q\nexit 1\n", marker)
	for _, name := range []string{"flutter", "dart"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(trapScript), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	versionJSON := `{
  "frameworkVersion": "3.50.1",
  "dartSdkVersion": "3.14.0",
  "flutterVersion": "3.50.1"
}`
	if err := os.WriteFile(filepath.Join(cacheDir, "flutter.version.json"), []byte(versionJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	fVer, dVer := readFlutterVersionMetadata(context.Background(), flutterDir)
	if fVer != "3.50.1" || dVer != "3.14.0" {
		t.Fatalf("readFlutterVersionMetadata = (%q, %q), want (\"3.50.1\", \"3.14.0\")", fVer, dVer)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("wrapper bin/flutter atau bin/dart tidak boleh dieksekusi saat membaca metadata")
	}

	// Uji fallback ke file version dan dart-sdk/version.
	_ = os.Remove(filepath.Join(cacheDir, "flutter.version.json"))
	if err := os.WriteFile(filepath.Join(flutterDir, "version"), []byte("3.49.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dartVersionDir, "version"), []byte("3.13.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fVer, dVer = readFlutterVersionMetadata(context.Background(), flutterDir)
	if fVer != "3.49.0" || dVer != "3.13.2" {
		t.Fatalf("fallback readFlutterVersionMetadata = (%q, %q), want (\"3.49.0\", \"3.13.2\")", fVer, dVer)
	}
}

func TestStaleFlutterTempFiles(t *testing.T) {
	cacheDir := t.TempDir()
	liveFile := filepath.Join(cacheDir, fmt.Sprintf("engine.stamp.tmp.%d", os.Getpid()))
	deadFile := filepath.Join(cacheDir, "engine.stamp.tmp.99999999")
	garbageFile := filepath.Join(cacheDir, "engine.stamp.tmp.stale")
	normalFile := filepath.Join(cacheDir, "engine.stamp")

	for _, f := range []string{liveFile, deadFile, garbageFile, normalFile} {
		if err := os.WriteFile(f, []byte("abc"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := staleFlutterTempFiles(cacheDir)
	gotMap := map[string]bool{}
	for _, p := range got {
		gotMap[p] = true
	}

	if gotMap[liveFile] {
		t.Fatalf("file temp milik PID aktif %d tidak boleh dianggap stale", os.Getpid())
	}
	if gotMap[normalFile] {
		t.Fatal("engine.stamp normal tidak boleh dianggap file temp")
	}
	if !gotMap[deadFile] || !gotMap[garbageFile] {
		t.Fatalf("file temp mati harus terdeteksi stale, dapat: %v", got)
	}
}

func TestInspectSDKDirStateAndFinding(t *testing.T) {
	flutterDir := t.TempDir()
	cacheDir := filepath.Join(flutterDir, "bin", "cache")
	gitObjDir := filepath.Join(flutterDir, ".git", "objects", "ab")
	dartToolDir := filepath.Join(flutterDir, "packages", "flutter_tools", ".dart_tool")
	for _, d := range []string{cacheDir, gitObjDir, dartToolDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Objek Git 0444 adalah normal dan tidak boleh memicu BadPerm.
	if err := os.WriteFile(filepath.Join(gitObjDir, "cdef01"), []byte("obj"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "engine.stamp"), []byte("hash"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dartToolDir, "package_config.json"), []byte(`{"rootUri":"file:///home/user/.pub-cache/hosted/pub.dev/a"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	uid := os.Geteuid()
	if uid == 0 {
		uid = 1000
	}
	clean := inspectSDKDirState(flutterDir, uid, "/home/user")
	if os.Geteuid() != 0 && clean.NeedsRepair() {
		t.Fatalf("direktori bersih tidak boleh membutuhkan perbaikan, dapat: %+v", clean)
	}

	// File cache non-writable (0444) oleh pemilik harus terdeteksi BadPerm.
	if err := os.Chmod(filepath.Join(cacheDir, "engine.stamp"), 0o444); err != nil {
		t.Fatal(err)
	}
	badPerm := inspectSDKDirState(flutterDir, uid, "/home/user")
	if !badPerm.BadPerm {
		t.Fatalf("file cache 0444 harus terdeteksi BadPerm, dapat: %+v", badPerm)
	}
	_ = os.Chmod(filepath.Join(cacheDir, "engine.stamp"), 0o644)

	// package_config.json yang menunjuk /root/.pub-cache harus terdeteksi.
	if err := os.WriteFile(filepath.Join(dartToolDir, "package_config.json"), []byte(`{"rootUri":"file:///root/.pub-cache/hosted/pub.dev/a"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	badPub := inspectSDKDirState(flutterDir, uid, "/home/user")
	if !badPub.RootPubCacheConf {
		t.Fatalf("package_config.json /root/.pub-cache harus terdeteksi, dapat: %+v", badPub)
	}

	// Simulasi owner mismatch saat berjalan sebagai non-root.
	if os.Geteuid() != 0 {
		wrongOwner := inspectSDKDirState(flutterDir, os.Geteuid()+1, "/home/user")
		if !wrongOwner.WrongOwner {
			t.Fatalf("perbedaan UID harus memicu WrongOwner, dapat: %+v", wrongOwner)
		}
	}

	// Uji sdkOwnershipAndCacheFinding dengan user aktif saat ini.
	if cur, err := user.Current(); err == nil && cur.Username != "root" {
		prev := resolveSDKUserFn
		resolveSDKUserFn = func() (string, string) { return cur.Username, cur.HomeDir }
		defer func() { resolveSDKUserFn = prev }()

		f := sdkOwnershipAndCacheFinding(flutterDir)
		if f == nil || f.Kind != "sdk-ownership" || !f.Fixable {
			t.Fatalf("sdkOwnershipAndCacheFinding harus mengembalikan temuan sdk-ownership fixable, dapat: %+v", f)
		}
	}
}
