package updater

import "testing"

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
