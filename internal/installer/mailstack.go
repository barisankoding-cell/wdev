package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// InstallMailStack memasang Stalwart Mail Server, stalwart-cli, dan SnappyMail
// tanpa skrip shell. Setup domain (DNS, relay, vhost) dijalankan terpisah oleh
// `wdev mail` / Web UI setelah komponen ini terpasang.
func (inst *Installer) InstallMailStack(ctx context.Context, cb LogCallback) error {
	cb("=== Memasang Mail Stack (Stalwart + SnappyMail) ===")
	// Prasyarat: nginx (vhost/webmail/admin) dan PHP-FPM (SnappyMail).
	if _, err := exec.LookPath("nginx"); err != nil {
		if err := inst.InstallNginx(ctx, cb); err != nil {
			cb(fmt.Sprintf("[WARN] Nginx gagal dipasang: %v", err))
		}
	}
	if _, err := exec.LookPath("php-fpm"); err != nil {
		if err := inst.InstallPHP(ctx, cb, "8.4"); err != nil {
			cb(fmt.Sprintf("[WARN] PHP-FPM gagal dipasang: %v", err))
		}
	}
	if err := inst.installStalwart(ctx, cb); err != nil {
		return err
	}
	if err := inst.installStalwartCLI(ctx, cb); err != nil {
		cb(fmt.Sprintf("[WARN] stalwart-cli gagal dipasang: %v", err))
	}
	if err := inst.installSnappyMail(ctx, cb); err != nil {
		cb(fmt.Sprintf("[WARN] SnappyMail gagal dipasang: %v", err))
	}
	cb("✓ Mail stack terpasang. Lanjutkan dengan setup domain di Web UI / `wdev mail`.")
	return nil
}

func stalwartArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "arm":
		return "arm"
	default:
		return runtime.GOARCH
	}
}

func pickLinuxAsset(assets []ghReleaseAsset, arch, suffix string) string {
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if !strings.Contains(name, arch+"-unknown-linux-gnu") {
			continue
		}
		if strings.HasSuffix(name, suffix) {
			return a.URL
		}
	}
	return ""
}

func (inst *Installer) installStalwart(ctx context.Context, cb LogCallback) error {
	const dst = "/usr/local/bin/stalwart"
	if out, err := inst.runCmdOutput(ctx, dst, "-V"); err == nil && strings.TrimSpace(out) != "" {
		cb(fmt.Sprintf("Stalwart sudah terpasang: %s", strings.TrimSpace(out)))
	} else {
		cb("Mengunduh Stalwart dari rilis resmi...")
		_, assets, err := fetchGitHubLatestRelease(ctx, "stalwartlabs/stalwart")
		if err != nil {
			return fmt.Errorf("gagal mengambil rilis Stalwart: %w", err)
		}
		url := pickLinuxAsset(assets, stalwartArch(), ".tar.gz")
		if url == "" {
			return fmt.Errorf("aset Stalwart untuk arsitektur %s tidak ditemukan", runtime.GOARCH)
		}
		tmpDir, err := os.MkdirTemp("", "wdev-stalwart-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)
		archive := filepath.Join(tmpDir, "stalwart.tar.gz")
		if err := downloadFile(url, archive); err != nil {
			return fmt.Errorf("gagal mengunduh Stalwart: %w", err)
		}
		if err := inst.runCmd(ctx, cb, "tar", "-xzf", archive, "-C", tmpDir); err != nil {
			return err
		}
		bin, err := findFileNamed(tmpDir, "stalwart")
		if err != nil {
			return err
		}
		if err := inst.runCmd(ctx, cb, "install", "-m", "0755", bin, dst); err != nil {
			return err
		}
	}

	_ = inst.runCmd(ctx, cb, "mkdir", "-p", "/etc/stalwart", "/var/lib/stalwart", "/var/log/stalwart")
	// Service account tidak privileged; port <1024 diberikan via capability.
	if _, err := os.Stat("/etc/passwd"); err == nil {
		if data, readErr := os.ReadFile("/etc/passwd"); readErr == nil && !strings.Contains(string(data), "stalwart:") {
			_ = inst.runCmd(ctx, cb, "useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "stalwart")
		}
	}
	if _, err := os.Stat("/etc/stalwart/stalwart.env"); err != nil {
		_ = os.WriteFile("/etc/stalwart/stalwart.env", []byte("# Stalwart environment (dikelola wdev)\n"), 0640)
	}
	_, _ = inst.runCmdOutput(ctx, "chown", "-R", "stalwart:stalwart", "/etc/stalwart", "/var/lib/stalwart", "/var/log/stalwart")

	unitPath := "/etc/systemd/system/stalwart.service"
	if _, err := os.Stat(unitPath); err != nil {
		unit := `[Unit]
Description=Stalwart Mail Server
Conflicts=postfix.service sendmail.service exim4.service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=stalwart
Group=stalwart
EnvironmentFile=-/etc/stalwart/stalwart.env
ExecStart=/usr/local/bin/stalwart --config=/etc/stalwart/config.json
Restart=on-failure
RestartSec=5
LimitNOFILE=65536
KillMode=process
KillSignal=SIGINT
SyslogIdentifier=stalwart
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`
		if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
			return err
		}
		_, _ = inst.runCmdOutput(ctx, "systemctl", "daemon-reload")
	}
	if err := inst.runCmd(ctx, cb, "systemctl", "enable", "stalwart"); err != nil {
		cb(fmt.Sprintf("[WARN] gagal enable stalwart: %v", err))
	}
	// Service belum di-start: config.json dibuat oleh wizard setup domain pertama.
	cb("✓ Stalwart terpasang di " + dst)
	return nil
}

func (inst *Installer) installStalwartCLI(ctx context.Context, cb LogCallback) error {
	const dst = "/usr/local/bin/stalwart-cli"
	if _, err := os.Stat(dst); err == nil {
		cb("stalwart-cli sudah terpasang")
		return nil
	}
	cb("Mengunduh stalwart-cli...")
	_, assets, err := fetchGitHubLatestRelease(ctx, "stalwartlabs/cli")
	if err != nil {
		return err
	}
	url := pickLinuxAsset(assets, stalwartArch(), ".tar.xz")
	if url == "" {
		return fmt.Errorf("aset stalwart-cli tidak ditemukan")
	}
	tmpDir, err := os.MkdirTemp("", "wdev-stalwart-cli-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	archive := filepath.Join(tmpDir, "cli.tar.xz")
	if err := downloadFile(url, archive); err != nil {
		return err
	}
	if err := inst.runCmd(ctx, cb, "tar", "-xJf", archive, "-C", tmpDir); err != nil {
		return err
	}
	bin, err := findFileNamed(tmpDir, "stalwart-cli")
	if err != nil {
		return err
	}
	if err := inst.runCmd(ctx, cb, "install", "-m", "0755", bin, dst); err != nil {
		return err
	}
	cb("✓ stalwart-cli terpasang")
	return nil
}

func (inst *Installer) installSnappyMail(ctx context.Context, cb LogCallback) error {
	const dst = "/var/www/snappymail"
	if _, err := os.Stat(filepath.Join(dst, "index.php")); err == nil {
		cb("SnappyMail sudah terpasang")
		return nil
	}
	cb("Mengunduh SnappyMail dari rilis resmi...")
	_, assets, err := fetchGitHubLatestRelease(ctx, "the-djmaze/snappymail")
	if err != nil {
		return fmt.Errorf("gagal mengambil rilis SnappyMail: %w", err)
	}
	var url string
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		// Pilih arsip utama: snappymail-<versi>.tar.gz (bukan cpanel/nextcloud/signature)
		if strings.HasSuffix(name, ".tar.gz") && !strings.Contains(name, "cpanel") &&
			!strings.Contains(name, "nextcloud") && !strings.HasSuffix(name, ".asc") {
			url = a.URL
			break
		}
	}
	if url == "" {
		return fmt.Errorf("aset SnappyMail (.tar.gz) tidak ditemukan")
	}
	tmpDir, err := os.MkdirTemp("", "wdev-snappymail-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	archive := filepath.Join(tmpDir, "snappymail.tar.gz")
	if err := downloadFile(url, archive); err != nil {
		return fmt.Errorf("gagal mengunduh SnappyMail: %w", err)
	}
	if err := inst.runCmd(ctx, cb, "tar", "-xzf", archive, "-C", tmpDir); err != nil {
		return err
	}
	root := tmpDir
	if _, statErr := os.Stat(filepath.Join(tmpDir, "index.php")); statErr != nil {
		entries, _ := os.ReadDir(tmpDir)
		for _, e := range entries {
			if e.IsDir() {
				root = filepath.Join(tmpDir, e.Name())
				break
			}
		}
	}
	if _, statErr := os.Stat(filepath.Join(root, "index.php")); statErr != nil {
		return fmt.Errorf("index.php SnappyMail tidak ditemukan di arsip")
	}
	_ = inst.runCmd(ctx, cb, "mkdir", "-p", dst)
	if err := inst.runCmd(ctx, cb, "cp", "-a", root+"/.", dst+"/"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dst, "data"), 0755); err != nil {
		return err
	}
	_ = inst.runCmd(ctx, cb, "chown", "-R", "www-data:www-data", dst)
	cb("✓ SnappyMail terpasang di " + dst)
	return nil
}

func (inst *Installer) runCmdOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func findFileNamed(root, name string) (string, error) {
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if info.Name() == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("binary %s tidak ditemukan di arsip", name)
	}
	return found, nil
}
