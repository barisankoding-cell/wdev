package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNginxRunUserParsing(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	cases := []struct {
		content string
		want    string
	}{
		{"user www-data;\nworker_processes auto;\n", "www-data"},
		{"user nginx nginx;\n", "nginx"},
		{"# user root;\nevents {}\n", ""},
	}
	for _, c := range cases {
		if err := os.WriteFile(conf, []byte(c.content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := nginxRunUser(conf); got != c.want {
			t.Errorf("nginxRunUser(%q) = %q, want %q", c.content, got, c.want)
		}
	}
}

func TestNginxRunUserMissingFile(t *testing.T) {
	if got := nginxRunUser(filepath.Join(t.TempDir(), "tidak-ada.conf")); got != "" {
		t.Fatalf("harus kosong untuk file tidak ada, dapat %q", got)
	}
}
