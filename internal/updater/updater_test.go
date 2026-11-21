package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.1.0", "2.1.0", 0},
		{"2.0.0", "2.1.0", -1},
		{"2.2.0", "2.1.9", 1},
		{"v2.1.0", "2.0.9", 1},
		{"2.1", "2.1.1", -1},
	}
	for _, c := range cases {
		if got := compare(c.a, c.b); got != c.want {
			t.Errorf("compare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestPickAsset(t *testing.T) {
	assets := []Asset{
		{Name: "wdev-linux-amd64", URL: "u1"},
		{Name: "wdev-linux-amd64.sha256", URL: "c1"},
		{Name: "wdev-linux-arm64", URL: "u2"},
	}
	if a, ok := pickAsset(assets, "wdev-linux-amd64"); !ok || a.URL != "u1" {
		t.Fatalf("asset amd64 salah: %+v %v", a, ok)
	}
	if _, ok := pickAsset(assets, "wdev-linux-riscv64"); ok {
		t.Fatal("asset tidak ada harus false")
	}
}

func TestBinaryAssetName(t *testing.T) {
	name := binaryAssetName()
	if name == "" || name[:4] != "wdev" {
		t.Fatalf("nama asset tidak valid: %s", name)
	}
}

func TestInstallBinaryFreshAndReplace(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "bin")
	dstPath := filepath.Join(dstDir, "wdev")

	v1Src := filepath.Join(srcDir, "wdev-v1")
	if err := os.WriteFile(v1Src, []byte("binary-v1"), 0644); err != nil {
		t.Fatal(err)
	}

	// 1. Fresh install ke direktori tujuan baru
	if err := installBinary(v1Src, dstPath); err != nil {
		t.Fatalf("fresh installBinary failed: %v", err)
	}
	got, err := os.ReadFile(dstPath)
	if err != nil || string(got) != "binary-v1" {
		t.Fatalf("dst content = %q (err=%v), want %q", got, err, "binary-v1")
	}
	info, err := os.Stat(dstPath)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatalf("dst mode = %v (err=%v), want 0755", info.Mode().Perm(), err)
	}
	// File sumber asli harus tetap utuh karena di-stage via salinan ke targetDir
	if _, err := os.Stat(v1Src); err != nil {
		t.Fatalf("src file should remain intact until caller removes temp dir: %v", err)
	}

	// 2. Replace biner yang sudah ada, backup .bak harus berisi versi lama
	v2Src := filepath.Join(srcDir, "wdev-v2")
	if err := os.WriteFile(v2Src, []byte("binary-v2"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := installBinary(v2Src, dstPath); err != nil {
		t.Fatalf("replace installBinary failed: %v", err)
	}
	gotV2, err := os.ReadFile(dstPath)
	if err != nil || string(gotV2) != "binary-v2" {
		t.Fatalf("updated dst content = %q (err=%v), want %q", gotV2, err, "binary-v2")
	}
	bak, err := os.ReadFile(dstPath + ".bak")
	if err != nil || string(bak) != "binary-v1" {
		t.Fatalf("backup content = %q (err=%v), want %q", bak, err, "binary-v1")
	}

	// Tidak boleh ada file staging .wdev-stage-* tersisa
	matches, _ := filepath.Glob(filepath.Join(dstDir, ".wdev-stage-*"))
	if len(matches) != 0 {
		t.Fatalf("unexpected leftover staging files: %v", matches)
	}
}
