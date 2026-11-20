package installer

import (
	"strings"
	"testing"
)

func TestSetConfKey(t *testing.T) {
	lines := []string{
		"# daemonize yes",
		"daemonize yes",
		"pidfile /run/redis/redis-server.pid",
		"# supervised auto",
	}

	out, changed := setConfKey(lines, "daemonize", "no")
	if !changed {
		t.Fatal("daemonize harus berubah")
	}
	joined := strings.Join(out, "\n")
	// Komentar dibiarkan; hanya satu direktif aktif yang dipertahankan.
	if strings.Count(joined, "\ndaemonize no") != 1 {
		t.Fatalf("harus tepat satu direktif aktif daemonize no:\n%s", joined)
	}
	if strings.Contains(joined, "\ndaemonize yes") {
		t.Fatalf("masih ada direktif aktif daemonize yes:\n%s", joined)
	}
	if !strings.Contains(joined, "# daemonize yes") {
		t.Fatalf("baris komentar tidak boleh diubah:\n%s", joined)
	}

	out, changed = setConfKey(out, "supervised", "systemd")
	if !changed {
		t.Fatal("supervised harus berubah")
	}
	sup := strings.Join(out, "\n")
	if strings.Count(sup, "\nsupervised systemd") != 1 {
		t.Fatalf("harus tepat satu direktif supervised systemd:\n%s", sup)
	}

	// Idempoten: nilai sama tidak dianggap berubah.
	_, changed = setConfKey(out, "daemonize", "no")
	if changed {
		t.Fatal("setConfKey harus idempoten untuk nilai yang sama")
	}
}

func TestSetConfKeyAppendsMissing(t *testing.T) {
	out, changed := setConfKey([]string{"port 6379"}, "supervised", "systemd")
	if !changed {
		t.Fatal("key baru harus ditambahkan")
	}
	if !strings.Contains(strings.Join(out, "\n"), "supervised systemd") {
		t.Fatalf("key tidak ditambahkan:\n%s", strings.Join(out, "\n"))
	}
}

func TestSetConfKeyDropsActiveDuplicate(t *testing.T) {
	lines := []string{"bind 0.0.0.0", "port 6379", "bind 0.0.0.0"}
	out, changed := setConfKey(lines, "bind", "127.0.0.1 -::1")
	if !changed {
		t.Fatal("nilai berbeda harus dianggap berubah")
	}
	joined := strings.Join(out, "\n")
	if strings.Count(joined, "bind ") != 1 {
		t.Fatalf("direktif bind duplikat harus dibuang:\n%s", joined)
	}
	if !strings.Contains(joined, "bind 127.0.0.1 -::1") {
		t.Fatalf("nilai bind tidak diperbarui:\n%s", joined)
	}
}

func TestFindRedisConf(t *testing.T) {
	conf := findRedisConf()
	if conf == "" {
		t.Skip("Redis tidak terpasang di mesin ini")
	}
	if !strings.HasSuffix(conf, ".conf") {
		t.Fatalf("path konfigurasi tidak valid: %s", conf)
	}
}
