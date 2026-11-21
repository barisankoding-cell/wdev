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

func TestBuildEdgeSSHArgs(t *testing.T) {
	scpArgs, sshArgs, host, err := buildEdgeSSHArgs(Options{
		EdgeHost: "root@203.0.113.10",
		EdgeKey:  "/root/.ssh/id_ed25519",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "root@203.0.113.10" {
		t.Fatalf("host = %q", host)
	}
	for _, args := range [][]string{scpArgs, sshArgs} {
		joined := joinArgs(args)
		for _, want := range []string{
			"BatchMode=yes",
			"ConnectTimeout=15",
			"StrictHostKeyChecking=accept-new",
			"-i /root/.ssh/id_ed25519",
			" -- ",
		} {
			if !containsString(joined, want) {
				t.Fatalf("args %q missing %q", joined, want)
			}
		}
	}

	for _, bad := range []Options{
		{EdgeHost: ""},
		{EdgeHost: "-oProxyCommand=evil"},
		{EdgeHost: "root@host evil"},
		{EdgeHost: "root@203.0.113.10", EdgeKey: "-oProxyCommand=evil"},
	} {
		if _, _, _, err := buildEdgeSSHArgs(bad); err == nil {
			t.Fatalf("expected error for invalid edge options %+v", bad)
		}
	}
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

func containsString(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
