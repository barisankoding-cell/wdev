package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"wdev/internal/config"
	"wdev/internal/store"
)

func TestPickLinuxAssetStalwartPrefersStandardOverFoundationDB(t *testing.T) {
	assets := []ghReleaseAsset{
		{Name: "stalwart-aarch64-apple-darwin.tar.gz", URL: "https://example.com/darwin-aarch64.tar.gz"},
		{Name: "stalwart-aarch64-unknown-linux-gnu.tar.gz", URL: "https://example.com/stalwart-aarch64.tar.gz"},
		{Name: "stalwart-foundationdb-x86_64-unknown-linux-gnu.tar.gz", URL: "https://example.com/foundationdb-x86_64.tar.gz"},
		{Name: "stalwart-foundationdb-x86_64-unknown-linux-gnu.tar.gz.sigstore.json", URL: "https://example.com/foundationdb-sig.json"},
		{Name: "stalwart-x86_64-apple-darwin.tar.gz", URL: "https://example.com/darwin-x86_64.tar.gz"},
		{Name: "stalwart-x86_64-unknown-linux-gnu.tar.gz", URL: "https://example.com/stalwart-x86_64.tar.gz"},
		{Name: "stalwart-x86_64-unknown-linux-musl.tar.gz", URL: "https://example.com/stalwart-x86_64-musl.tar.gz"},
	}

	gotAMD64 := pickLinuxAsset(assets, "stalwart", "x86_64", ".tar.gz")
	if gotAMD64 != "https://example.com/stalwart-x86_64.tar.gz" {
		t.Fatalf("expected standard x86_64 url, got %q", gotAMD64)
	}

	gotARM64 := pickLinuxAsset(assets, "stalwart", "aarch64", ".tar.gz")
	if gotARM64 != "https://example.com/stalwart-aarch64.tar.gz" {
		t.Fatalf("expected standard aarch64 url, got %q", gotARM64)
	}
}

func TestPickLinuxAssetStalwartCLI(t *testing.T) {
	assets := []ghReleaseAsset{
		{Name: "stalwart-cli-aarch64-unknown-linux-gnu.tar.xz", URL: "https://example.com/cli-aarch64.tar.xz"},
		{Name: "stalwart-cli-x86_64-unknown-linux-gnu.tar.xz", URL: "https://example.com/cli-x86_64.tar.xz"},
		{Name: "stalwart-cli-x86_64-unknown-linux-musl.tar.xz", URL: "https://example.com/cli-x86_64-musl.tar.xz"},
	}

	got := pickLinuxAsset(assets, "stalwart-cli", "x86_64", ".tar.xz")
	if got != "https://example.com/cli-x86_64.tar.xz" {
		t.Fatalf("expected stalwart-cli x86_64 url, got %q", got)
	}
}

func TestFindFileNamed(t *testing.T) {
	tmpDir := t.TempDir()

	subDir := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(subDir, "stalwart")
	if err := os.WriteFile(binPath, []byte("fake binary"), 0755); err != nil {
		t.Fatal(err)
	}

	found, err := findFileNamed(tmpDir, "stalwart", "stalwart-mail")
	if err != nil {
		t.Fatalf("expected to find stalwart binary, got error: %v", err)
	}
	if found != binPath {
		t.Fatalf("expected found path %q, got %q", binPath, found)
	}

	// Tes jika nama binary tidak cocok
	_, err = findFileNamed(tmpDir, "nonexistent-binary")
	if err == nil {
		t.Fatal("expected error when binary does not exist, got nil")
	}
}

func TestInstallMailStackRejectsWhenNoDomain(t *testing.T) {
	cfg := &config.Config{
		WdevEtc:          t.TempDir(),
		NginxSitesAvail:  t.TempDir(),
		NginxSitesEnable: t.TempDir(),
	}
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	inst := New(cfg, db)

	// File non-vhost (.custom.conf / default / internal wdev) tidak boleh dianggap domain terkonfigurasi
	for _, nonDomainFile := range []string{"appsone.test.custom.conf", "default.conf", "wdev_webmail.conf"} {
		if err := os.WriteFile(filepath.Join(cfg.NginxSitesAvail, nonDomainFile), []byte("# helper\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Tanpa domain terkonfigurasi, harus ditolak
	err = inst.InstallMailStack(context.Background(), func(string) {})
	if err == nil {
		t.Fatal("expected error when no domain is configured, got nil")
	}
	if !strings.Contains(err.Error(), "belum ada domain yang terkonfigurasi") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// Jika diberikan domain yang tidak valid
	err = inst.InstallMailStackWithDomain(context.Background(), func(string) {}, "invalid..domain")
	if err == nil {
		t.Fatal("expected error for invalid domain, got nil")
	}
	if !strings.Contains(err.Error(), "tidak valid") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// VHost nyata tanpa akhiran .conf harus terdeteksi
	if err := os.WriteFile(filepath.Join(cfg.NginxSitesAvail, "appsone.test"), []byte("server { server_name appsone.test; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if !inst.HasConfiguredDomain() {
		t.Fatal("expected HasConfiguredDomain to be true when real vhost file exists")
	}
	_ = os.Remove(filepath.Join(cfg.NginxSitesAvail, "appsone.test"))

	// Tambahkan domain ke store
	err = db.SaveSite(store.SiteRecord{Domain: "example.com"})
	if err != nil {
		t.Fatal(err)
	}

	if !inst.HasConfiguredDomain() {
		t.Fatal("expected HasConfiguredDomain to be true after saving site")
	}
}
