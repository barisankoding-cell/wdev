package filemanager

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"wdev/internal/config"
)

func newTestFM(t *testing.T) (*Manager, string, string) {
	t.Helper()
	tmp := t.TempDir()
	allowed := filepath.Join(tmp, "www")
	outside := filepath.Join(tmp, "secret")
	if err := os.MkdirAll(allowed, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "topsecret.txt"), []byte("root-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	mgr := New(&config.Config{WebRootBase: allowed}, nil)
	mgr.SetRoots(allowed)
	return mgr, allowed, outside
}

func TestResolvePathBlocksTraversalAndSymlinkEscape(t *testing.T) {
	mgr, allowed, outside := newTestFM(t)

	if _, err := mgr.ResolvePath(filepath.Join(allowed, "..", "secret", "topsecret.txt"), false); err == nil {
		t.Fatal("expected traversal path outside root to be rejected")
	}

	escapeLink := filepath.Join(allowed, "escape")
	if err := os.Symlink(outside, escapeLink); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ResolvePath(filepath.Join(escapeLink, "topsecret.txt"), true); err == nil {
		t.Fatal("expected symlink escape outside root to be rejected")
	}
	if _, err := mgr.ResolvePath(filepath.Join(escapeLink, "newfile.txt"), false); err == nil {
		t.Fatal("expected write through symlink escape to be rejected")
	}
}

func TestCRUDOperationsAndRootProtection(t *testing.T) {
	mgr, allowed, _ := newTestFM(t)

	sub := filepath.Join(allowed, "nikaget.my.id")
	if err := mgr.Mkdir(sub); err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}
	indexFile := filepath.Join(sub, "index.html")
	if err := mgr.Write(indexFile, "<h1>Halo</h1>"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	got, err := mgr.Read(indexFile)
	if err != nil || got.Content != "<h1>Halo</h1>" {
		t.Fatalf("Read unexpected result: %+v err=%v", got, err)
	}

	// Binary file rejection
	binFile := filepath.Join(sub, "img.png")
	if err := os.WriteFile(binFile, []byte{0x89, 'P', 'N', 'G', 0x00, 0x01}, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Read(binFile); err == nil {
		t.Fatal("expected binary Read to fail")
	}

	// Dotfiles filter vs showHidden
	if err := mgr.Write(filepath.Join(sub, ".env"), "APP_KEY=123\n"); err != nil {
		t.Fatal(err)
	}
	noHidden, err := mgr.List(sub, false)
	if err != nil || len(noHidden.Entries) != 2 {
		t.Fatalf("expected 2 non-hidden entries, got %+v err=%v", noHidden.Entries, err)
	}
	withHidden, err := mgr.List(sub, true)
	if err != nil || len(withHidden.Entries) != 3 {
		t.Fatalf("expected 3 entries with hidden, got %+v err=%v", withHidden.Entries, err)
	}

	// Chmod
	if err := mgr.Chmod([]string{indexFile}, "640", false, false); err != nil {
		t.Fatalf("Chmod failed: %v", err)
	}
	st, _ := os.Stat(indexFile)
	if st.Mode().Perm() != 0640 {
		t.Fatalf("expected perm 0640, got %04o", st.Mode().Perm())
	}

	// Copy & Move & Rename & Search
	copyDir := filepath.Join(allowed, "backup")
	_ = mgr.Mkdir(copyDir)
	if _, err := mgr.Copy([]string{indexFile}, copyDir); err != nil {
		t.Fatalf("Copy failed: %v", err)
	}
	copiedFile := filepath.Join(copyDir, "index.html")
	renamed, err := mgr.Rename(copiedFile, "home.html")
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}
	found, err := mgr.Search(allowed, "home", false, 50)
	if err != nil || len(found) != 1 || found[0].Path != renamed {
		t.Fatalf("Search failed: %+v err=%v", found, err)
	}

	// Root deletion must be refused
	if _, err := mgr.Delete([]string{allowed}); err == nil {
		t.Fatal("expected deleting allowed root to be refused")
	}
}

func TestCompressExtractAndZipSlipProtection(t *testing.T) {
	mgr, allowed, outside := newTestFM(t)

	srcDir := filepath.Join(allowed, "app")
	_ = mgr.Mkdir(srcDir)
	_ = mgr.Write(filepath.Join(srcDir, "index.php"), "<?php echo 'ok';")

	zipEntry, err := mgr.Compress([]string{srcDir}, filepath.Join(allowed, "app-bundle.zip"))
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	extractDest := filepath.Join(allowed, "restored")
	count, err := mgr.Extract(zipEntry.Path, extractDest)
	if err != nil || count < 2 {
		t.Fatalf("Extract zip failed: count=%d err=%v", count, err)
	}
	data, err := os.ReadFile(filepath.Join(extractDest, "app", "index.php"))
	if err != nil || string(data) != "<?php echo 'ok';" {
		t.Fatalf("unexpected extracted content: %q err=%v", string(data), err)
	}

	// Zip Slip attack archive
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	fw, _ := zw.Create("../../secret/pwned.txt")
	_, _ = fw.Write([]byte("evil"))
	_ = zw.Close()

	evilZip := filepath.Join(allowed, "evil.zip")
	if err := os.WriteFile(evilZip, zipBuf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Extract(evilZip, extractDest); err == nil {
		t.Fatal("expected Zip Slip traversal to be rejected")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned.txt")); !os.IsNotExist(err) {
		t.Fatal("evil file must not exist outside destDir")
	}

	// Tar.gz Slip protection
	var tarBuf bytes.Buffer
	gw := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gw)
	body := []byte("evil-tar")
	_ = tw.WriteHeader(&tar.Header{Name: "../outside.txt", Mode: 0644, Size: int64(len(body))})
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gw.Close()

	evilTar := filepath.Join(allowed, "evil.tar.gz")
	_ = os.WriteFile(evilTar, tarBuf.Bytes(), 0644)
	if _, err := mgr.Extract(evilTar, extractDest); err == nil || !strings.Contains(err.Error(), "tidak aman") {
		t.Fatalf("expected tar slip rejection, got err=%v", err)
	}
}
