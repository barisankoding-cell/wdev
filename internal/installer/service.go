package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const WdevServiceUnitPath = "/etc/systemd/system/wdev.service"

// legacyBashAssets adalah artefak skrip Bash lama yang kini digantikan biner Go.
var legacyBashAssets = []string{
	"/opt/wdev/setup_dev.sh",
	"/opt/wdev/setup_orca_headless.sh",
	"/opt/wdev/cf-tunnel.sh",
	"/opt/wdev/pdns-mgr.sh",
	"/opt/wdev/vps-edge-setup.sh",
	"/opt/wdev/$dest_dir",
	"/usr/local/bin/node-switch",
	"/usr/local/bin/php-switch",
	"/usr/local/bin/pg-switch",
	"/usr/local/bin/dev-site",
	"/usr/local/bin/cf-tunnel",
	"/usr/local/bin/pdns-mgr",
}

// removeLegacyBashAssets membersihkan sisa skrip Bash dari instalasi lama.
func (inst *Installer) removeLegacyBashAssets(ctx context.Context, cb LogCallback) {
	for _, path := range legacyBashAssets {
		if _, err := os.Stat(path); err == nil {
			cb("Menghapus skrip Bash legacy: " + path)
			_ = os.RemoveAll(path)
		}
	}
}

// InstallWdevService installs the wdev binary, directories, and systemd service unit.
func (inst *Installer) InstallWdevService(ctx context.Context, cb LogCallback, port, host string) error {
	if cb == nil {
		cb = func(string) {}
	}
	if port == "" {
		port = "3200"
	}
	if host == "" {
		host = "0.0.0.0"
	}

	cb("=== MEMULAI INSTALASI WDEV SYSTEMD SERVICE ===")
	inst.removeLegacyBashAssets(ctx, cb)

	// 1. Pastikan direktori sistem ada
	dirs := []string{"/etc/wdev", "/var/lib/wdev", "/var/log/wdev"}
	for _, dir := range dirs {
		cb(fmt.Sprintf("Membuat direktori sistem: %s", dir))
		if err := inst.runCmd(ctx, cb, "mkdir", "-p", dir); err != nil {
			return fmt.Errorf("gagal membuat direktori %s: %w", dir, err)
		}
	}
	_ = inst.runCmd(ctx, cb, "chmod", "700", "/etc/wdev")

	// 2. Salin biner wdev ke /usr/local/bin/wdev jika berbeda atau update
	exePath, err := os.Executable()
	if err == nil {
		exePath, _ = filepath.EvalSymlinks(exePath)
		targetBin := "/usr/local/bin/wdev"
		if exePath != targetBin {
			cb(fmt.Sprintf("Menginstal biner wdev dari %s ke %s...", exePath, targetBin))
			if err := inst.runCmd(ctx, cb, "install", "-m", "755", exePath, targetBin); err != nil {
				return fmt.Errorf("gagal menginstal biner wdev ke %s: %w", targetBin, err)
			}
		}
	}

	// 3. Buat unit systemd /etc/systemd/system/wdev.service
	serviceUnit := WdevServiceUnitContent(port, host)

	cb(fmt.Sprintf("Menulis konfigurasi systemd service ke %s...", WdevServiceUnitPath))
	tmpF, err := os.CreateTemp("", "wdev-service-*.tmp")
	if err != nil {
		return fmt.Errorf("gagal membuat file temporary service: %w", err)
	}
	tmpFile := tmpF.Name()
	defer os.Remove(tmpFile)
	if _, err := tmpF.WriteString(serviceUnit); err != nil {
		tmpF.Close()
		return fmt.Errorf("gagal menulis file temporary service: %w", err)
	}
	if err := tmpF.Close(); err != nil {
		return fmt.Errorf("gagal menutup file temporary service: %w", err)
	}

	if err := inst.runCmd(ctx, cb, "cp", "-f", tmpFile, WdevServiceUnitPath); err != nil {
		return fmt.Errorf("gagal menyalin unit service ke %s: %w", WdevServiceUnitPath, err)
	}
	_ = inst.runCmd(ctx, cb, "chmod", "644", WdevServiceUnitPath)

	// 5. Reload daemon, aktifkan auto-start, & restart layanan agar biner + Environment baru langsung aktif
	if err := reloadAndRestartWdevService(ctx, cb, inst.runCmd); err != nil {
		return err
	}

	cb(fmt.Sprintf("✓ wdev.service berhasil diinstal dan aktif di port %s!", port))
	cb(fmt.Sprintf("✓ Akses Web UI: http://localhost:%s", port))
	return nil
}

// UninstallWdevService stops, disables and removes wdev.service.
func (inst *Installer) UninstallWdevService(ctx context.Context, cb LogCallback, purge bool) error {
	if cb == nil {
		cb = func(string) {}
	}

	cb("=== MEMULAI UNINSTALL WDEV SYSTEMD SERVICE ===")
	inst.removeLegacyBashAssets(ctx, cb)

	// 1. Hentikan & nonaktifkan service
	cb("Menghentikan wdev.service...")
	_ = inst.runCmd(ctx, cb, "systemctl", "stop", "wdev.service")

	cb("Menonaktifkan auto-start wdev.service...")
	_ = inst.runCmd(ctx, cb, "systemctl", "disable", "wdev.service")

	// 2. Hapus unit service
	if _, err := os.Stat(WdevServiceUnitPath); err == nil {
		cb(fmt.Sprintf("Menghapus unit file %s...", WdevServiceUnitPath))
		_ = inst.runCmd(ctx, cb, "rm", "-f", WdevServiceUnitPath)
	}

	// 3. Reload daemon
	cb("Memuat ulang daemon systemd...")
	_ = inst.runCmd(ctx, cb, "systemctl", "daemon-reload")
	_ = inst.runCmd(ctx, cb, "systemctl", "reset-failed")

	// 4. Jika purge diminta
	if purge {
		cb("Membersihkan biner /usr/local/bin/wdev...")
		_ = inst.runCmd(ctx, cb, "rm", "-f", "/usr/local/bin/wdev")

		cb("Membersihkan direktori /opt/wdev...")
		_ = inst.runCmd(ctx, cb, "rm", "-rf", "/opt/wdev")

		cb("Membersihkan direktori konfigurasi /etc/wdev & database...")
		_ = inst.runCmd(ctx, cb, "rm", "-rf", "/etc/wdev", "/var/lib/wdev", "/var/log/wdev")
		cb("✓ wdev dan seluruh konfigurasinya telah dihapus total (purged).")
	} else {
		cb("✓ wdev.service berhasil di-uninstall (unit dihapus). Data konfigurasi di /etc/wdev dipertahankan.")
	}

	return nil
}

// GetWdevServiceStatus returns the formatted status output of wdev.service.
func (inst *Installer) GetWdevServiceStatus() (string, error) {
	cmd := exec.Command("systemctl", "status", "wdev.service", "--no-pager")
	out, _ := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), nil
}
