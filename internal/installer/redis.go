package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// redisConfCandidates lokasi konfigurasi Redis/Valkey yang didukung.
var redisConfCandidates = []string{
	"/etc/redis/redis.conf",
	"/etc/valkey/valkey.conf",
	"/etc/redis.conf",
	"/etc/valkey.conf",
}

// findRedisConf mengembalikan path konfigurasi yang ada.
func findRedisConf() string {
	for _, p := range redisConfCandidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// setConfKey menormalkan direktif Redis: semua baris aktif untuk key yang sama
// disisakan tepat satu (nilai terakhir menang di Redis), sedangkan baris
// komentar dibiarkan agar dokumentasi bawaan tidak rusak.
func setConfKey(lines []string, key, value string) ([]string, bool) {
	wanted := key + " " + value
	changed := false
	activeIdx := -1
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "#") {
			out = append(out, l) // komentar: jangan disentuh
			continue
		}
		if trimmed == key || strings.HasPrefix(trimmed, key+" ") {
			if activeIdx >= 0 {
				// Duplikat aktif: buang.
				changed = true
				continue
			}
			activeIdx = len(out)
			if trimmed != wanted {
				changed = true
			}
			out = append(out, wanted)
			continue
		}
		out = append(out, l)
	}
	if activeIdx < 0 {
		out = append(out, wanted)
		changed = true
	}
	return out, changed
}

// ensureRedisConf menormalkan konfigurasi Redis agar kompatibel dengan systemd
// (Type=notify tidak boleh dijalankan dengan daemonize yes).
func (inst *Installer) ensureRedisConf(ctx context.Context, cb LogCallback, confPath string) error {
	if confPath == "" {
		return fmt.Errorf("file konfigurasi Redis/Valkey tidak ditemukan")
	}
	data, err := os.ReadFile(confPath)
	if err != nil {
		return fmt.Errorf("gagal membaca %s: %w", confPath, err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

	changed := false
	for _, kv := range [][2]string{
		{"daemonize", "no"},
		{"supervised", "systemd"},
		{"protected-mode", "yes"},
	} {
		var did bool
		lines, did = setConfKey(lines, kv[0], kv[1])
		changed = changed || did
	}

	// Bind: hormati DEV_LAN_ACCESS bila diset; jika tidak, pertahankan nilai
	// aktif yang sudah ada (jangan mengubah akses secara diam-diam).
	lanEnv := strings.TrimSpace(os.Getenv("DEV_LAN_ACCESS"))
	currentBind := activeConfValue(lines, "bind")
	bind := currentBind
	if lanEnv != "" {
		bind = "127.0.0.1 -::1"
		if lanEnv == "1" {
			bind = "0.0.0.0"
		}
	}
	if bind == "" {
		bind = "127.0.0.1 -::1"
	}
	lines, did := setConfKey(lines, "bind", bind)
	changed = changed || did

	// requirepass hanya diubah bila REDIS_PASSWORD diberikan eksplisit.
	if pass := strings.TrimSpace(os.Getenv("REDIS_PASSWORD")); pass != "" {
		lines, did = setConfKey(lines, "requirepass", pass)
		changed = changed || did
	}

	if !changed {
		cb(" ✓ Konfigurasi Redis sudah kompatibel dengan systemd.")
		return nil
	}

	updated := strings.Join(lines, "\n") + "\n"
	// Tulis lewat file sementara di direktori yang sama agar owner/mode terjaga.
	tmp := filepath.Join(filepath.Dir(confPath), ".wdev-redis.conf.tmp")
	if err := os.WriteFile(tmp, []byte(updated), 0640); err != nil {
		return fmt.Errorf("gagal menulis konfigurasi Redis: %w", err)
	}
	if fi, err := os.Stat(confPath); err == nil {
		_ = os.Chmod(tmp, fi.Mode().Perm())
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			_ = os.Chown(tmp, int(st.Uid), int(st.Gid))
		}
	}
	_ = inst.runCmd(ctx, cb, "mv", "-f", tmp, confPath)
	cb(" ✓ Konfigurasi Redis dinormalkan (daemonize=no, supervised=systemd).")
	return nil
}

// activeConfValue mengambil nilai direktif aktif pertama (bukan komentar).
func activeConfValue(lines []string, key string) string {
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, key+" ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, key+" "))
		}
	}
	return ""
}

// redisPassword mengambil password dari env atau direktif requirepass di conf.
func redisPassword() string {
	if pass := strings.TrimSpace(os.Getenv("REDIS_PASSWORD")); pass != "" {
		return pass
	}
	conf := findRedisConf()
	if conf == "" {
		return ""
	}
	data, err := os.ReadFile(conf)
	if err != nil {
		return ""
	}
	return activeConfValue(strings.Split(string(data), "\n"), "requirepass")
}

// freeRedisPort menghentikan sisa proses redis-server di luar systemd yang
// menyandera port, agar service bisa start. Tidak menyentuh service aktif.
func (inst *Installer) freeRedisPort(ctx context.Context, cb LogCallback, svc string) {
	// Service resmi sedang aktif -> port memang dipakai olehnya, jangan disentuh.
	if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", svc).Run() == nil {
		return
	}
	if out, err := exec.CommandContext(ctx, "ss", "-ltnp").Output(); err == nil {
		if !strings.Contains(string(out), ":6379") {
			return
		}
	}
	cb(" [!] Port Redis masih dipakai proses lama; menghentikan sisa proses redis-server...")
	for _, pattern := range []string{"/usr/bin/redis-server", "/usr/bin/valkey-server"} {
		_ = exec.CommandContext(ctx, "pkill", "-f", pattern).Run()
	}
	time.Sleep(time.Second)
}

// redisPing memverifikasi server merespons PONG (dengan auth bila diset).
func redisPing(ctx context.Context) bool {
	for _, cli := range []string{"redis-cli", "valkey-cli"} {
		bin, err := exec.LookPath(cli)
		if err != nil {
			continue
		}
		cmd := exec.CommandContext(ctx, bin, "ping")
		if pass := redisPassword(); pass != "" {
			cmd.Env = append(os.Environ(), "REDISCLI_AUTH="+pass)
		}
		if out, err := cmd.Output(); err == nil && strings.Contains(strings.ToUpper(string(out)), "PONG") {
			return true
		}
	}
	return false
}
