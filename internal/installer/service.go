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

	// 1. Pastikan direktori sistem ada
	dirs := []string{"/etc/wdev", "/var/lib/wdev", "/var/log/wdev"}
	for _, dir := range dirs {
		cb(fmt.Sprintf("Membuat direktori sistem: %s", dir))
		if err := inst.runCmd(ctx, cb, "mkdir", "-p", dir); err != nil {
			return fmt.Errorf("gagal membuat direktori %s: %w", dir, err)
		}
	}

	// 2. Salin biner wdev ke /usr/local/bin/wdev jika berbeda atau update
	exePath, err := os.Executable()
	if err == nil {
		exePath, _ = filepath.EvalSymlinks(exePath)
		targetBin := "/usr/local/bin/wdev"
		if exePath != targetBin {
			cb(fmt.Sprintf("Menginstal biner wdev dari %s ke %s...", exePath, targetBin))
			_ = inst.runCmd(ctx, cb, "install", "-m", "755", exePath, targetBin)
		}
	}

	// 3. Buat unit systemd /etc/systemd/system/wdev.service
	serviceUnit := fmt.Sprintf(`[Unit]
Description=wdev Linux Developer Control Plane & Web Dashboard
After=network.target network-online.target nginx.service
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=/var/lib/wdev
ExecStart=/usr/local/bin/wdev serve --port %s --host %s
Restart=always
RestartSec=3
StandardOutput=journal
StandardError=journal
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, port, host)

	cb(fmt.Sprintf("Menulis konfigurasi systemd service ke %s...", WdevServiceUnitPath))
	tmpFile := "/tmp/wdev.service.tmp"
	if err := os.WriteFile(tmpFile, []byte(serviceUnit), 0644); err != nil {
		return fmt.Errorf("gagal membuat file temporary service: %w", err)
	}
	defer os.Remove(tmpFile)

	if err := inst.runCmd(ctx, cb, "cp", "-f", tmpFile, WdevServiceUnitPath); err != nil {
		return fmt.Errorf("gagal menyalin unit service ke %s: %w", WdevServiceUnitPath, err)
	}
	_ = inst.runCmd(ctx, cb, "chmod", "644", WdevServiceUnitPath)

	// 5. Reload daemon & aktifkan layanan
	cb("Memuat ulang daemon systemd...")
	if err := inst.runCmd(ctx, cb, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("gagal reload daemon systemd: %w", err)
	}

	cb("Mengaktifkan & menjalankan wdev.service...")
	if err := inst.runCmd(ctx, cb, "systemctl", "enable", "--now", "wdev.service"); err != nil {
		return fmt.Errorf("gagal mengaktifkan wdev.service: %w", err)
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
