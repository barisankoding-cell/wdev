package installer

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// toolchainServiceEnv adalah environment yang wajib tersedia bagi wdev.service.
// Service berjalan tanpa login shell, sehingga /etc/profile.d/wdev.sh tidak
// dibaca; tanpa RUSTUP_HOME, misalnya, shim rustup gagal menemukan toolchain.
var toolchainServiceEnv = [][2]string{
	{"GOROOT", "/usr/local/go"},
	{"CARGO_HOME", "/usr/local/cargo"},
	{"RUSTUP_HOME", "/usr/local/rustup"},
	{"FLUTTER_ROOT", "/usr/local/flutter"},
	{"ANDROID_HOME", "/usr/local/android-sdk"},
	{"ANDROID_SDK_ROOT", "/usr/local/android-sdk"},
	{"NODE_HOME", "/usr/local/lib/nodejs/current"},
}

// servicePath urutan PATH untuk wdev.service (area kanonik lebih dulu).
const servicePath = "/usr/local/go/bin:/usr/local/cargo/bin:/usr/local/flutter/bin:" +
	"/usr/local/lib/nodejs/current/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// WdevServiceUnitContent merender isi unit systemd wdev.service.
func WdevServiceUnitContent(port, host string) string {
	if port == "" {
		port = "3200"
	}
	if host == "" {
		host = "0.0.0.0"
	}

	envBlock := ""
	for _, kv := range toolchainServiceEnv {
		envBlock += fmt.Sprintf("Environment=\"%s=%s\"\n", kv[0], kv[1])
	}
	envBlock += fmt.Sprintf("Environment=\"PATH=%s\"\n", servicePath)

	return fmt.Sprintf(`[Unit]
Description=wdev Linux Developer Control Plane & Web Dashboard
After=network.target network-online.target nginx.service
Wants=network-online.target

[Service]
Type=simple
User=root
WorkingDirectory=/var/lib/wdev
%sExecStart=/usr/local/bin/wdev serve --port %s --host %s
Restart=always
RestartSec=3
StandardOutput=journal
StandardError=journal
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`, envBlock, port, host)
}

// EnsureWdevServiceUnit menyelaraskan unit systemd yang sudah terpasang
// (mis. menambahkan Environment= toolchain yang belum ada) tanpa mengubah
// port/host yang sedang dipakai. Idempoten: tidak menulis bila sudah sama.
func (inst *Installer) EnsureWdevServiceUnit(ctx context.Context, cb LogCallback) error {
	if cb == nil {
		cb = func(string) {}
	}
	if _, err := os.Stat(WdevServiceUnitPath); err != nil {
		cb(" [i] wdev.service belum terpasang; lewati penyelarasan unit.")
		return nil
	}

	current, err := os.ReadFile(WdevServiceUnitPath)
	if err != nil {
		return fmt.Errorf("gagal membaca %s: %w", WdevServiceUnitPath, err)
	}

	port, host := parseServiceExec(current)
	desired := WdevServiceUnitContent(port, host)
	if string(current) == desired {
		cb(" ✓ wdev.service sudah mutakhir (Environment toolchain lengkap).")
		return nil
	}

	cb("Menyelaraskan unit wdev.service (Environment toolchain)...")
	if err := inst.writeSystemFile(ctx, cb, WdevServiceUnitPath, []byte(desired), "644"); err != nil {
		return err
	}
	_ = inst.runCmd(ctx, cb, "systemctl", "daemon-reload")

	if os.Getenv("INVOCATION_ID") != "" {
		cb(" [i] Unit diperbarui. Environment baru aktif pada restart wdev.service berikutnya.")
	} else {
		_ = inst.runCmd(ctx, cb, "systemctl", "enable", "--now", "wdev.service")
		cb(" ✓ wdev.service diaktifkan ulang dengan environment baru.")
	}
	return nil
}

// parseServiceExec mengambil port & host dari baris ExecStart yang ada.
func parseServiceExec(unit []byte) (port, host string) {
	port, host = "3200", "0.0.0.0"
	for _, line := range strings.Split(string(unit), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "--port" && i+1 < len(fields) {
				port = fields[i+1]
			}
			if strings.HasPrefix(f, "--port=") {
				port = strings.TrimPrefix(f, "--port=")
			}
			if f == "--host" && i+1 < len(fields) {
				host = fields[i+1]
			}
			if strings.HasPrefix(f, "--host=") {
				host = strings.TrimPrefix(f, "--host=")
			}
		}
	}
	return port, host
}
