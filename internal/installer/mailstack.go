package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// isValidDomain memvalidasi FQDN domain sebelum proses instalasi mail stack.
func isValidDomain(d string) bool {
	d = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
	if d == "" || len(d) > 253 || !strings.Contains(d, ".") || strings.Contains(d, "..") {
		return false
	}
	for _, label := range strings.Split(d, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// HasConfiguredDomain memeriksa apakah ada domain atau situs yang sudah terkonfigurasi
// di wdev sebelum komponen Mail Server Stack dipasang.
func (inst *Installer) HasConfiguredDomain() bool {
	// 1. Cek dari database wdev (tabel sites)
	if inst.store != nil {
		sites, err := inst.store.ListSites()
		if err == nil && len(sites) > 0 {
			for _, s := range sites {
				if strings.TrimSpace(s.Domain) != "" {
					return true
				}
			}
		}
	}

	// 2. Cek file konfigurasi mail-domains.json jika ada
	var statePaths []string
	if inst.cfg != nil && strings.TrimSpace(inst.cfg.WdevEtc) != "" {
		wdevEtc := strings.TrimSpace(inst.cfg.WdevEtc)
		statePaths = append(statePaths, filepath.Join(wdevEtc, "mail-domains.json"))
		if wdevEtc == "/etc/wdev" {
			statePaths = append(statePaths, "/etc/wdev-edge/mail_domains.json")
		}
	} else {
		statePaths = append(statePaths, "/etc/wdev/mail-domains.json", "/etc/wdev-edge/mail_domains.json")
	}
	for _, p := range statePaths {
		if data, err := os.ReadFile(p); err == nil {
			var m map[string]any
			if err := json.Unmarshal(data, &m); err == nil && len(m) > 0 {
				return true
			}
			var list []any
			if err := json.Unmarshal(data, &list); err == nil && len(list) > 0 {
				return true
			}
		}
	}

	// 3. Cek vhost Nginx yang sudah dikonfigurasi selain default / internal wdev
	var vhostDirs []string
	if inst.cfg != nil && (strings.TrimSpace(inst.cfg.NginxSitesEnable) != "" || strings.TrimSpace(inst.cfg.NginxSitesAvail) != "") {
		if d := strings.TrimSpace(inst.cfg.NginxSitesEnable); d != "" {
			vhostDirs = append(vhostDirs, d)
		}
		if d := strings.TrimSpace(inst.cfg.NginxSitesAvail); d != "" {
			vhostDirs = append(vhostDirs, d)
		}
	} else {
		vhostDirs = []string{"/etc/nginx/sites-enabled", "/etc/nginx/sites-available"}
	}
	for _, dir := range vhostDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if name == "default" || name == "default.conf" || name == "000-default" || name == "000-default.conf" ||
				strings.HasPrefix(name, ".") || strings.HasPrefix(name, "wdev_") ||
				strings.HasSuffix(name, ".custom.conf") || strings.HasSuffix(name, ".bak") ||
				strings.HasSuffix(name, ".disabled") || strings.HasSuffix(name, "-le-ssl.conf") ||
				strings.Contains(name, ".dpkg-") || strings.HasSuffix(name, ".rpmnew") ||
				strings.HasSuffix(name, ".rpmsave") || strings.HasSuffix(name, "~") {
				continue
			}
			base := strings.TrimSuffix(name, ".conf")
			if isValidDomain(base) {
				return true
			}
		}
	}

	return false
}

// InstallMailStack memasang Stalwart Mail Server, stalwart-cli, dan SnappyMail
// tanpa skrip shell. Memerlukan setidaknya satu domain yang telah terdaftar di wdev.
func hasPHPFPMBinary() bool {
	if findTool("php-fpm") != "" {
		return true
	}
	matches, _ := filepath.Glob("/usr/sbin/php-fpm*")
	return len(matches) > 0
}

// InstallMailStackWithDomain memasang Stalwart Mail Server, stalwart-cli, dan SnappyMail
// dengan validasi ketersediaan domain (baik melalui argumen domain atau domain yang sudah ada di sistem).
func (inst *Installer) InstallMailStackWithDomain(ctx context.Context, cb LogCallback, domain string) error {
	domain = strings.TrimSpace(domain)
	if domain != "" {
		if !isValidDomain(domain) {
			return fmt.Errorf("domain %q tidak valid", domain)
		}
	} else if !inst.HasConfiguredDomain() {
		return fmt.Errorf("belum ada domain yang terkonfigurasi di wdev. Tambahkan domain atau situs terlebih dahulu sebelum memasang Mail Server Stack")
	}

	cb("=== Memasang Mail Stack (Stalwart + SnappyMail) ===")
	// Prasyarat: nginx (vhost/webmail/admin) dan PHP-FPM (SnappyMail).
	_, sbinErr := os.Stat("/usr/sbin/nginx")
	if findTool("nginx") == "" && sbinErr != nil {
		if err := inst.InstallNginx(ctx, cb); err != nil {
			cb(fmt.Sprintf("[WARN] Nginx gagal dipasang: %v", err))
		}
	}
	if !hasPHPFPMBinary() {
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

func (inst *Installer) InstallMailStack(ctx context.Context, cb LogCallback) error {
	return inst.InstallMailStackWithDomain(ctx, cb, "")
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

func pickLinuxAsset(assets []ghReleaseAsset, prefix, arch, suffix string) string {
	targetPrefix := prefix + "-" + arch + "-"
	// 1. Prioritaskan binary standar GNU (bukan FoundationDB atau varian custom lainnya)
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, "foundationdb") {
			continue
		}
		if !strings.HasPrefix(name, targetPrefix) && name != prefix+"-"+arch+suffix {
			continue
		}
		if !strings.Contains(name, "-unknown-linux-gnu") && !strings.Contains(name, "-linux-gnu") {
			continue
		}
		if strings.HasSuffix(name, suffix) {
			return a.URL
		}
	}
	// 2. Fallback untuk linux variant lain (misal musl) dengan prefix yang sama
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, "foundationdb") {
			continue
		}
		if !strings.HasPrefix(name, targetPrefix) && name != prefix+"-"+arch+suffix {
			continue
		}
		if !strings.Contains(name, "linux") {
			continue
		}
		if strings.HasSuffix(name, suffix) {
			return a.URL
		}
	}
	// 3. Fallback jika arm/armv7 atau variasi penamaan lain, selama bukan foundationdb
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if strings.Contains(name, "foundationdb") {
			continue
		}
		if !strings.HasPrefix(name, prefix+"-") {
			continue
		}
		if !strings.Contains(name, arch) {
			continue
		}
		if !strings.Contains(name, "linux") {
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
		url := pickLinuxAsset(assets, "stalwart", stalwartArch(), ".tar.gz")
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
		bin, err := findFileNamed(tmpDir, "stalwart", "stalwart-mail")
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
		_ = inst.writeSystemFile(ctx, cb, "/etc/stalwart/stalwart.env", []byte("# Stalwart environment (dikelola wdev)\n"), "0640")
	}
	chownTargets := []string{"/etc/stalwart", "/var/lib/stalwart/", "/var/log/stalwart"}
	if resolved, err := filepath.EvalSymlinks("/var/lib/stalwart"); err == nil && resolved != "" && resolved != "/var/lib/stalwart" {
		chownTargets = append(chownTargets, resolved)
	}
	_ = inst.runCmd(ctx, cb, "chown", append([]string{"-R", "stalwart:stalwart"}, chownTargets...)...)

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
		if err := inst.writeSystemFile(ctx, cb, unitPath, []byte(unit), "0644"); err != nil {
			return err
		}
		_ = inst.runCmd(ctx, cb, "systemctl", "daemon-reload")
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
	url := pickLinuxAsset(assets, "stalwart-cli", stalwartArch(), ".tar.xz")
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
	if err := inst.runCmd(ctx, cb, "mkdir", "-p", filepath.Join(dst, "data")); err != nil {
		return err
	}
	webOwner := "www-data:www-data"
	if data, err := os.ReadFile("/etc/passwd"); err == nil && !strings.Contains(string(data), "www-data:") && strings.Contains(string(data), "nginx:") {
		webOwner = "nginx:nginx"
	}
	_ = inst.runCmd(ctx, cb, "chown", "-R", webOwner, dst)
	cb("✓ SnappyMail terpasang di " + dst)
	return nil
}

func (inst *Installer) runCmdOutput(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func findFileNamed(root string, names ...string) (string, error) {
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		for _, name := range names {
			if info.Name() == name {
				found = path
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("binary %s tidak ditemukan di arsip", strings.Join(names, " / "))
	}
	return found, nil
}
