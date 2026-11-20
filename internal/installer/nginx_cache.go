package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// nginxTempDirs direktori cache sementara Nginx (dari paket nginx.org maupun Debian).
var nginxTempDirs = []string{
	"/var/cache/nginx",
	"/var/cache/nginx/client_temp",
	"/var/cache/nginx/proxy_temp",
	"/var/cache/nginx/fastcgi_temp",
	"/var/cache/nginx/uwsgi_temp",
	"/var/cache/nginx/scgi_temp",
	"/var/lib/nginx",
	"/var/lib/nginx/body",
	"/var/lib/nginx/fastcgi",
	"/var/lib/nginx/proxy",
	"/var/lib/nginx/scgi",
	"/var/lib/nginx/uwsgi",
}

// nginxRunUser membaca direktif "user" dari nginx.conf.
func nginxRunUser(confPath string) string {
	data, err := os.ReadFile(confPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "user ") {
			fields := strings.Fields(strings.TrimSuffix(trimmed, ";"))
			if len(fields) >= 2 {
				return fields[1]
			}
		}
	}
	return ""
}

// FixNginxCachePerm memastikan direktori cache Nginx dapat ditulis oleh user
// worker. Ini mencegah "[crit] open() ... failed (13: Permission denied) while
// reading upstream" yang membuat FastCGI/proxy menggantung.
func (inst *Installer) FixNginxCachePerm(ctx context.Context, cb LogCallback) error {
	if cb == nil {
		cb = func(string) {}
	}
	confPath := "/etc/nginx/nginx.conf"
	runUser := nginxRunUser(confPath)
	if runUser == "" {
		runUser = "www-data"
	}

	// User worker harus ada; kalau tidak, nginx tidak akan start.
	if _, err := exec.CommandContext(ctx, "id", "-u", runUser).Output(); err != nil {
		return fmt.Errorf("user nginx '%s' tidak ditemukan di sistem", runUser)
	}

	fixed := 0
	for _, dir := range nginxTempDirs {
		fi, err := os.Stat(dir)
		if err != nil || !fi.IsDir() {
			continue
		}
		// Pastikan root direktori bisa dilalui worker.
		if filepath.Dir(dir) == "/var/cache/nginx" || filepath.Dir(dir) == "/var/lib/nginx" || dir == "/var/cache/nginx" || dir == "/var/lib/nginx" {
			_ = inst.runCmd(ctx, cb, "chmod", "755", dir)
		}
		_ = inst.runCmd(ctx, cb, "chown", "-R", runUser+":"+runUser, dir)
		fixed++
		// Direktori temp harus 700 milik worker.
		if dir != "/var/cache/nginx" && dir != "/var/lib/nginx" {
			_ = inst.runCmd(ctx, cb, "chmod", "-R", "700", dir)
		}
	}

	// Hapus sisa entri milik user lain (mis. nginx:nginx dari paket lama).
	if runUser != "nginx" {
		_ = exec.CommandContext(ctx, "find", "/var/cache/nginx", "/var/lib/nginx",
			"-user", "nginx", "-exec", "rm", "-rf", "{}", "+").Run()
	}

	// Verifikasi tulis sebagai user worker.
	probe := "/var/cache/nginx/fastcgi_temp/.wdev-probe"
	if err := inst.runCmd(ctx, cb, "runuser", "-u", runUser, "--", "touch", probe); err != nil {
		return fmt.Errorf("user %s belum bisa menulis /var/cache/nginx/fastcgi_temp: %w", runUser, err)
	}
	_ = os.Remove(probe)

	if fixed > 0 {
		cb(fmt.Sprintf("✓ Kepemilikan cache Nginx dinormalkan ke %s (%d direktori).", runUser, fixed))
	}
	return nil
}

// DiagnoseNginxCache mengembalikan ringkasan kondisi direktori cache.
func DiagnoseNginxCache() []string {
	confPath := "/etc/nginx/nginx.conf"
	runUser := nginxRunUser(confPath)
	if runUser == "" {
		runUser = "www-data"
	}
	lines := []string{fmt.Sprintf(" [i] nginx user    : %s", runUser)}
	for _, dir := range []string{"/var/cache/nginx/fastcgi_temp", "/var/cache/nginx/proxy_temp"} {
		fi, err := os.Stat(dir)
		if err != nil {
			lines = append(lines, fmt.Sprintf(" [--] %-36s: tidak ada", dir))
			continue
		}
		mode := fi.Mode().Perm()
		owner := "?"
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			owner = fmt.Sprintf("uid=%d gid=%d", st.Uid, st.Gid)
		}
		status := "[OK]"
		if os.Geteuid() == 0 {
			if exec.Command("runuser", "-u", runUser, "--", "test", "-w", dir).Run() != nil {
				status = "[!] "
			}
		}
		lines = append(lines, fmt.Sprintf(" %s %-36s: %s %o %s", status, dir, owner, mode, ""))
	}
	return lines
}
