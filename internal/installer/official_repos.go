package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

type osReleaseInfo struct {
	ID              string
	IDLike          string
	VersionID       string
	VersionCodename string
	UbuntuCodename  string
}

func readOSRelease() osReleaseInfo {
	var info osReleaseInfo
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return info
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "ID":
			info.ID = strings.ToLower(v)
		case "ID_LIKE":
			info.IDLike = strings.ToLower(v)
		case "VERSION_ID":
			info.VersionID = v
		case "VERSION_CODENAME":
			info.VersionCodename = strings.ToLower(v)
		case "UBUNTU_CODENAME":
			info.UbuntuCodename = strings.ToLower(v)
		}
	}
	return info
}

func (o osReleaseInfo) isUbuntuBased() bool {
	return o.ID == "ubuntu" ||
		o.ID == "pop" ||
		o.ID == "linuxmint" ||
		o.ID == "elementary" ||
		o.ID == "zorin" ||
		strings.Contains(o.IDLike, "ubuntu") ||
		o.UbuntuCodename != ""
}

func (o osReleaseInfo) debianCodename() string {
	if o.isUbuntuBased() && o.UbuntuCodename != "" {
		return o.UbuntuCodename
	}
	if o.VersionCodename != "" {
		return o.VersionCodename
	}
	// Fallback pemetaan versi umum bila VERSION_CODENAME tidak tercantum
	switch o.ID {
	case "debian", "dietpi":
		switch strings.Split(o.VersionID, ".")[0] {
		case "13":
			return "trixie"
		case "12":
			return "bookworm"
		case "11":
			return "bullseye"
		}
	case "ubuntu":
		switch o.VersionID {
		case "24.04":
			return "noble"
		case "22.04":
			return "jammy"
		case "20.04":
			return "focal"
		}
	}
	return ""
}

func (o osReleaseInfo) rhelMajor() string {
	if o.VersionID == "" {
		return ""
	}
	return strings.Split(o.VersionID, ".")[0]
}

func rpmArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64"
	default:
		return "x86_64"
	}
}

func adoptiumArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64"
	default:
		return "x64"
	}
}

func (inst *Installer) writeSystemFile(ctx context.Context, cb LogCallback, dstPath string, content []byte, mode string) error {
	if err := inst.runCmd(ctx, cb, "mkdir", "-p", filepath.Dir(dstPath)); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp("", "wdev-sys-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := inst.runCmd(ctx, cb, "cp", "-f", tmpPath, dstPath); err != nil {
		return err
	}
	return inst.runCmd(ctx, cb, "chmod", mode, dstPath)
}

func (inst *Installer) ensureGPGKeyring(ctx context.Context, cb LogCallback, keyURL, keyringPath string) error {
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", keyURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "wdev-installer")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gagal mengunduh GPG key (%s): status %d", keyURL, resp.StatusCode)
	}
	rawKey, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var finalKey []byte
	if bytes.Contains(rawKey, []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----")) {
		if _, err := exec.LookPath("gpg"); err != nil {
			_ = inst.runCmd(ctx, cb, "apt-get", "update", "-y")
			_ = inst.runCmd(ctx, cb, "apt-get", "install", "-y", "gnupg", "ca-certificates")
		}
		tmpAsc, err := os.CreateTemp("", "wdev-key-*.asc")
		if err != nil {
			return err
		}
		ascPath := tmpAsc.Name()
		defer os.Remove(ascPath)
		if _, err := tmpAsc.Write(rawKey); err != nil {
			tmpAsc.Close()
			return err
		}
		tmpAsc.Close()

		gpgPath := ascPath + ".gpg"
		defer os.Remove(gpgPath)
		cmd := exec.CommandContext(ctx, "gpg", "--batch", "--yes", "--dearmor", "-o", gpgPath, ascPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("gpg dearmor gagal: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		finalKey, err = os.ReadFile(gpgPath)
		if err != nil {
			return err
		}
	} else {
		finalKey = rawKey
	}

	return inst.writeSystemFile(ctx, cb, keyringPath, finalKey, "644")
}

func checkURLReachable(ctx context.Context, rawURL string) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, "HEAD", rawURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "wdev-installer")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// ensurePGDGRepo menyiapkan repositori resmi PostgreSQL (PGDG).
func (inst *Installer) ensurePGDGRepo(ctx context.Context, cb LogCallback) bool {
	osInfo := readOSRelease()
	if inst.isDebian {
		codename := osInfo.debianCodename()
		if codename == "" {
			cb("[WARN] Codename Debian/Ubuntu tidak terdeteksi, menggunakan repo bawaan distro.")
			return false
		}
		keyring := "/etc/apt/keyrings/postgresql.gpg"
		cb("Menyiapkan repositori resmi PostgreSQL (PGDG)...")
		if err := inst.ensureGPGKeyring(ctx, cb, "https://www.postgresql.org/media/keys/ACCC4CF8.asc", keyring); err != nil {
			cb(fmt.Sprintf("[WARN] Gagal mengunduh kunci PGDG (%v), fallback ke repo distro.", err))
			return false
		}
		listContent := fmt.Sprintf("deb [signed-by=%s] https://apt.postgresql.org/pub/repos/apt %s-pgdg main\n", keyring, codename)
		if err := inst.writeSystemFile(ctx, cb, "/etc/apt/sources.list.d/pgdg.list", []byte(listContent), "644"); err != nil {
			cb(fmt.Sprintf("[WARN] Gagal menulis pgdg.list (%v), fallback ke repo distro.", err))
			return false
		}
		if err := inst.runCmd(ctx, cb, "apt-get", "update", "-y"); err != nil {
			cb(fmt.Sprintf("[WARN] apt-get update PGDG gagal (%v), menghapus pgdg.list...", err))
			_ = inst.runCmd(ctx, cb, "rm", "-f", "/etc/apt/sources.list.d/pgdg.list")
			_ = inst.runCmd(ctx, cb, "apt-get", "update", "-y")
			return false
		}
		return true
	}

	if inst.pkgMgr == "dnf" || inst.pkgMgr == "yum" {
		if exec.CommandContext(ctx, "rpm", "-q", "pgdg-fedora-repo").Run() == nil ||
			exec.CommandContext(ctx, "rpm", "-q", "pgdg-redhat-repo").Run() == nil {
			cb("Repositori PGDG RPM sudah terpasang.")
			inst.importPGDGGPGKey(ctx, cb)
			return true
		}
		arch := rpmArch()
		var repoRPM string
		if osInfo.ID == "fedora" && osInfo.VersionID != "" {
			repoRPM = fmt.Sprintf("https://download.postgresql.org/pub/repos/yum/reporpms/F-%s-%s/pgdg-fedora-repo-latest.noarch.rpm", osInfo.VersionID, arch)
		} else if major := osInfo.rhelMajor(); major != "" {
			repoRPM = fmt.Sprintf("https://download.postgresql.org/pub/repos/yum/reporpms/EL-%s-%s/pgdg-redhat-repo-latest.noarch.rpm", major, arch)
		}
		if repoRPM == "" || !checkURLReachable(ctx, repoRPM) {
			cb("[WARN] Repo PGDG RPM tidak tersedia untuk versi distro ini, menggunakan repo sistem.")
			return false
		}
		cb("Menyiapkan repositori resmi PostgreSQL (PGDG RPM)...")
		if err := inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", repoRPM); err != nil {
			cb(fmt.Sprintf("[WARN] Gagal memasang PGDG RPM (%v), fallback ke repo distro.", err))
			return false
		}
		inst.importPGDGGPGKey(ctx, cb)
		if osInfo.ID != "fedora" {
			_ = inst.runCmd(ctx, cb, inst.pkgMgr, "module", "disable", "-y", "postgresql")
		}
		return true
	}

	return false
}

// ensurePHPRepo menyiapkan repositori resmi PHP (Sury untuk Debian, PPA Ondřej untuk Ubuntu, Remi untuk Fedora/RHEL).
func (inst *Installer) ensurePHPRepo(ctx context.Context, cb LogCallback, ver string) bool {
	osInfo := readOSRelease()
	if inst.isDebian {
		codename := osInfo.debianCodename()
		if codename == "" {
			cb("[WARN] Codename Debian/Ubuntu tidak terdeteksi, menggunakan repo PHP bawaan distro.")
			return false
		}
		if osInfo.isUbuntuBased() {
			keyring := "/etc/apt/keyrings/ondrej-php.gpg"
			cb("Menyiapkan repositori resmi PHP (PPA Ondřej Surý untuk Ubuntu)...")
			keyURL := "https://keyserver.ubuntu.com/pks/lookup?op=get&search=0xB8DC7E53946656EFBCE4C1DD71DAEAAB4AD4CAB6"
			if err := inst.ensureGPGKeyring(ctx, cb, keyURL, keyring); err != nil {
				cb(fmt.Sprintf("[WARN] Gagal menyiapkan kunci PPA Ondřej (%v), fallback ke repo distro.", err))
				return false
			}
			listContent := fmt.Sprintf("deb [signed-by=%s] https://ppa.launchpadcontent.net/ondrej/php/ubuntu %s main\n", keyring, codename)
			if err := inst.writeSystemFile(ctx, cb, "/etc/apt/sources.list.d/ondrej-php.list", []byte(listContent), "644"); err != nil {
				return false
			}
		} else {
			keyring := "/etc/apt/keyrings/sury-php.gpg"
			cb("Menyiapkan repositori resmi PHP (Sury DEB untuk Debian)...")
			if err := inst.ensureGPGKeyring(ctx, cb, "https://packages.sury.org/php/apt.gpg", keyring); err != nil {
				cb(fmt.Sprintf("[WARN] Gagal menyiapkan kunci Sury PHP (%v), fallback ke repo distro.", err))
				return false
			}
			listContent := fmt.Sprintf("deb [signed-by=%s] https://packages.sury.org/php/ %s main\n", keyring, codename)
			if err := inst.writeSystemFile(ctx, cb, "/etc/apt/sources.list.d/sury-php.list", []byte(listContent), "644"); err != nil {
				return false
			}
		}
		if err := inst.runCmd(ctx, cb, "apt-get", "update", "-y"); err != nil {
			cb(fmt.Sprintf("[WARN] apt-get update repo PHP gagal (%v), fallback ke repo distro.", err))
			_ = inst.runCmd(ctx, cb, "rm", "-f", "/etc/apt/sources.list.d/ondrej-php.list", "/etc/apt/sources.list.d/sury-php.list")
			_ = inst.runCmd(ctx, cb, "apt-get", "update", "-y")
			return false
		}
		return true
	}

	if inst.pkgMgr == "dnf" || inst.pkgMgr == "yum" {
		if exec.CommandContext(ctx, "rpm", "-q", "remi-release").Run() != nil {
			var remiRPM string
			if osInfo.ID == "fedora" && osInfo.VersionID != "" {
				remiRPM = fmt.Sprintf("https://rpms.remirepo.net/fedora/remi-release-%s.rpm", osInfo.VersionID)
			} else if major := osInfo.rhelMajor(); major != "" {
				_ = inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", "epel-release")
				remiRPM = fmt.Sprintf("https://rpms.remirepo.net/enterprise/remi-release-%s.rpm", major)
			}
			if remiRPM == "" || !checkURLReachable(ctx, remiRPM) {
				cb("[WARN] Repo Remi RPM tidak dapat dijangkau atau belum tersedia, menggunakan repo distro.")
				return false
			}
			cb("Menyiapkan repositori resmi PHP (Remi RPM)...")
			if err := inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", remiRPM); err != nil {
				cb(fmt.Sprintf("[WARN] Gagal memasang Remi RPM (%v), fallback ke repo distro.", err))
				return false
			}
		} else {
			cb("Repositori Remi RPM sudah terpasang.")
		}
		if ver != "" && ver != "system" {
			_ = inst.runCmd(ctx, cb, inst.pkgMgr, "module", "reset", "-y", "php")
			_ = inst.runCmd(ctx, cb, inst.pkgMgr, "module", "enable", "-y", "php:remi-"+ver)
		}
		return true
	}

	return false
}

// ensureNginxRepo menyiapkan repositori resmi Nginx Mainline (nginx.org).
func (inst *Installer) ensureNginxRepo(ctx context.Context, cb LogCallback) bool {
	osInfo := readOSRelease()
	if inst.isDebian {
		codename := osInfo.debianCodename()
		if codename == "" {
			return false
		}
		keyring := "/etc/apt/keyrings/nginx-archive-keyring.gpg"
		cb("Menyiapkan repositori resmi Nginx Mainline (nginx.org)...")
		if err := inst.ensureGPGKeyring(ctx, cb, "https://nginx.org/keys/nginx_signing.key", keyring); err != nil {
			cb(fmt.Sprintf("[WARN] Gagal mengunduh kunci nginx.org (%v), fallback ke repo distro.", err))
			return false
		}
		repoDistro := "debian"
		if osInfo.isUbuntuBased() {
			repoDistro = "ubuntu"
		}
		listContent := fmt.Sprintf("deb [signed-by=%s] https://nginx.org/packages/mainline/%s %s nginx\n", keyring, repoDistro, codename)
		if err := inst.writeSystemFile(ctx, cb, "/etc/apt/sources.list.d/nginx.list", []byte(listContent), "644"); err != nil {
			return false
		}
		// Beri prioritas ke paket resmi nginx.org agar mengungguli repo distro
		pinContent := "Package: *\nPin: origin nginx.org\nPin: release o=nginx\nPin-Priority: 900\n"
		_ = inst.writeSystemFile(ctx, cb, "/etc/apt/preferences.d/99nginx", []byte(pinContent), "644")

		if err := inst.runCmd(ctx, cb, "apt-get", "update", "-y"); err != nil {
			cb(fmt.Sprintf("[WARN] apt-get update nginx.org gagal (%v), fallback ke repo distro.", err))
			_ = inst.runCmd(ctx, cb, "rm", "-f", "/etc/apt/sources.list.d/nginx.list", "/etc/apt/preferences.d/99nginx")
			_ = inst.runCmd(ctx, cb, "apt-get", "update", "-y")
			return false
		}
		return true
	}

	if inst.pkgMgr == "dnf" || inst.pkgMgr == "yum" {
		repoKind := "rhel"
		verCheck := osInfo.rhelMajor()
		if osInfo.ID == "fedora" {
			repoKind = "fedora"
			verCheck = osInfo.VersionID
		}
		if verCheck != "" {
			probeURL := fmt.Sprintf("https://nginx.org/packages/mainline/%s/%s/%s/repodata/repomd.xml", repoKind, verCheck, rpmArch())
			if !checkURLReachable(ctx, probeURL) {
				cb(fmt.Sprintf("[WARN] Repo resmi nginx.org belum tersedia untuk %s %s, menggunakan repo sistem.", repoKind, verCheck))
				if _, err := os.Stat("/etc/yum.repos.d/nginx.repo"); err == nil {
					_ = inst.runCmd(ctx, cb, "rm", "-f", "/etc/yum.repos.d/nginx.repo")
				}
				return false
			}
		}

		cb("Menyiapkan repositori resmi Nginx Mainline (nginx.org RPM)...")
		repoContent := fmt.Sprintf(`[nginx-mainline]
name=nginx mainline repo
baseurl=https://nginx.org/packages/mainline/%s/$releasever/$basearch/
gpgcheck=1
enabled=1
gpgkey=https://nginx.org/keys/nginx_signing.key
module_hotfixes=true
`, repoKind)
		if err := inst.writeSystemFile(ctx, cb, "/etc/yum.repos.d/nginx.repo", []byte(repoContent), "644"); err != nil {
			return false
		}
		return true
	}

	return false
}

var nginxUserDirectiveRe = regexp.MustCompile(`(?m)^user\s+nginx;`)

// patchNginxMainConfig menormalkan nginx.conf agar worker memakai www-data (bila ada)
// dan memuat direktori virtual host /etc/nginx/sites-enabled/*.
func patchNginxMainConfig(content string, hasWWWData bool) (string, bool) {
	updated := false

	// Pada Debian/Ubuntu, PHP-FPM berjalan sebagai www-data; sesuaikan user nginx.org bila www-data tersedia
	if hasWWWData && nginxUserDirectiveRe.MatchString(content) {
		content = nginxUserDirectiveRe.ReplaceAllString(content, "user  www-data;")
		updated = true
	}

	if !strings.Contains(content, "/etc/nginx/sites-enabled/") {
		target := "include /etc/nginx/conf.d/*.conf;"
		if strings.Contains(content, target) {
			content = strings.Replace(content, target, target+"\n    include /etc/nginx/sites-enabled/*;", 1)
			updated = true
		} else if idx := strings.Index(content, "http {"); idx != -1 {
			insertAt := idx + len("http {")
			content = content[:insertAt] + "\n    include /etc/nginx/sites-enabled/*;" + content[insertAt:]
			updated = true
		}
	}

	return content, updated
}

// ensureNginxCompatibility memastikan nginx.conf memuat /etc/nginx/sites-enabled/* dan user sesuai PHP-FPM.
func (inst *Installer) ensureNginxCompatibility(ctx context.Context, cb LogCallback) {
	inst.ensureNginxCompatibilityAt(ctx, cb, "/etc/nginx/nginx.conf", "/etc/passwd")
}

func (inst *Installer) ensureNginxCompatibilityAt(ctx context.Context, cb LogCallback, confPath, passwdPath string) {
	if confPath == "/etc/nginx/nginx.conf" {
		_ = inst.runCmd(ctx, cb, "mkdir", "-p", "/etc/nginx/sites-available", "/etc/nginx/sites-enabled")
	}
	data, err := os.ReadFile(confPath)
	if err != nil {
		return
	}

	hasWWWData := false
	if inst.isDebian {
		if passwdData, pErr := os.ReadFile(passwdPath); pErr == nil && strings.Contains(string(passwdData), "www-data:") {
			hasWWWData = true
		}
	}

	patched, updated := patchNginxMainConfig(string(data), hasWWWData)
	if !updated {
		return
	}

	cb("Menyesuaikan " + confPath + " agar mendukung sites-enabled & worker PHP-FPM...")
	if confPath == "/etc/nginx/nginx.conf" {
		_ = inst.writeSystemFile(ctx, cb, confPath, []byte(patched), "644")
		return
	}
	_ = os.WriteFile(confPath, []byte(patched), 0644)
}

// ensureRedisRepo menyiapkan repositori resmi Redis (packages.redis.io).
func (inst *Installer) ensureRedisRepo(ctx context.Context, cb LogCallback) bool {
	osInfo := readOSRelease()
	if inst.isDebian {
		codename := osInfo.debianCodename()
		if codename == "" {
			return false
		}
		keyring := "/etc/apt/keyrings/redis-archive-keyring.gpg"
		cb("Menyiapkan repositori resmi Redis (packages.redis.io)...")
		if err := inst.ensureGPGKeyring(ctx, cb, "https://packages.redis.io/gpg", keyring); err != nil {
			cb(fmt.Sprintf("[WARN] Gagal mengunduh kunci packages.redis.io (%v), fallback ke repo distro.", err))
			return false
		}
		listContent := fmt.Sprintf("deb [signed-by=%s] https://packages.redis.io/deb %s main\n", keyring, codename)
		if err := inst.writeSystemFile(ctx, cb, "/etc/apt/sources.list.d/redis.list", []byte(listContent), "644"); err != nil {
			return false
		}
		if err := inst.runCmd(ctx, cb, "apt-get", "update", "-y"); err != nil {
			cb(fmt.Sprintf("[WARN] apt-get update packages.redis.io gagal (%v), fallback ke repo distro.", err))
			_ = inst.runCmd(ctx, cb, "rm", "-f", "/etc/apt/sources.list.d/redis.list")
			_ = inst.runCmd(ctx, cb, "apt-get", "update", "-y")
			return false
		}
		return true
	}

	if (inst.pkgMgr == "dnf" || inst.pkgMgr == "yum") && osInfo.ID != "fedora" {
		major := osInfo.rhelMajor()
		if major == "8" || major == "9" {
			probeURL := fmt.Sprintf("https://packages.redis.io/rpm/rhel%s/repodata/repomd.xml", major)
			if !checkURLReachable(ctx, probeURL) {
				cb(fmt.Sprintf("[WARN] Repo resmi packages.redis.io untuk RHEL %s tidak dapat dijangkau, menggunakan repo sistem.", major))
				return false
			}
			cb("Menyiapkan repositori resmi Redis (packages.redis.io RPM)...")
			repoContent := fmt.Sprintf(`[Redis]
name=Redis
baseurl=https://packages.redis.io/rpm/rhel%s
enabled=1
gpgcheck=1
gpgkey=https://packages.redis.io/gpg
`, major)
			if err := inst.writeSystemFile(ctx, cb, "/etc/yum.repos.d/redis.repo", []byte(repoContent), "644"); err != nil {
				return false
			}
			return true
		}
	}

	return false
}

const netbirdRPMRepoContent = `[NetBird]
name=NetBird
baseurl=https://pkgs.netbird.io/yum/
enabled=1
gpgcheck=1
gpgkey=https://pkgs.netbird.io/yum/repodata/repomd.xml.key
repo_gpgcheck=1
`

func netbirdDebianRepoLine(keyringPath string) string {
	return fmt.Sprintf("deb [signed-by=%s] https://pkgs.netbird.io/debian stable main\n", keyringPath)
}

// ensureNetbirdRepo menyiapkan repositori resmi NetBird (pkgs.netbird.io) secara mandiri tanpa PowerDNS.
func (inst *Installer) ensureNetbirdRepo(ctx context.Context, cb LogCallback) bool {
	if inst.isDebian {
		listPath := "/etc/apt/sources.list.d/netbird.list"
		existingData, readErr := os.ReadFile(listPath)
		hadExisting := readErr == nil && strings.Contains(string(existingData), "pkgs.netbird.io/debian")

		keyring := "/etc/apt/keyrings/netbird-archive-keyring.gpg"
		cb("Menyiapkan repositori resmi NetBird (pkgs.netbird.io)...")
		if err := inst.ensureGPGKeyring(ctx, cb, "https://pkgs.netbird.io/debian/public.key", keyring); err != nil {
			cb(fmt.Sprintf("[WARN] Gagal mengunduh kunci pkgs.netbird.io (%v), mencoba repo yang ada.", err))
			return false
		}
		if err := inst.writeSystemFile(ctx, cb, listPath, []byte(netbirdDebianRepoLine(keyring)), "644"); err != nil {
			cb(fmt.Sprintf("[WARN] Gagal menulis %s (%v).", listPath, err))
			return false
		}
		if err := inst.runCmd(ctx, cb, "apt-get", "update", "-y"); err != nil {
			cb(fmt.Sprintf("[WARN] apt-get update pkgs.netbird.io gagal (%v).", err))
			if !hadExisting {
				_ = inst.runCmd(ctx, cb, "rm", "-f", listPath)
				_ = inst.runCmd(ctx, cb, "apt-get", "update", "-y")
			}
			return false
		}
		return true
	}

	if inst.pkgMgr == "dnf" || inst.pkgMgr == "yum" {
		repoPath := "/etc/yum.repos.d/netbird.repo"
		if data, err := os.ReadFile(repoPath); err == nil && strings.Contains(string(data), "pkgs.netbird.io/yum") {
			cb("Repositori resmi NetBird (pkgs.netbird.io RPM) sudah terpasang.")
			return true
		}
		if !checkURLReachable(ctx, "https://pkgs.netbird.io/yum/repodata/repomd.xml") {
			cb("[WARN] Repo resmi pkgs.netbird.io RPM tidak dapat dijangkau, mencoba repo sistem.")
			return false
		}
		cb("Menyiapkan repositori resmi NetBird (pkgs.netbird.io RPM)...")
		if err := inst.writeSystemFile(ctx, cb, repoPath, []byte(netbirdRPMRepoContent), "644"); err != nil {
			return false
		}
		return true
	}

	return false
}

func dpkgArch() string {
	if out, err := exec.Command("dpkg", "--print-architecture").Output(); err == nil {
		if a := strings.TrimSpace(string(out)); a != "" {
			return a
		}
	}
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	case "arm":
		return "armhf"
	default:
		return "amd64"
	}
}

// ensureDockerRepo menyiapkan repositori resmi Docker Engine & Docker Compose v5 (download.docker.com).
func (inst *Installer) ensureDockerRepo(ctx context.Context, cb LogCallback) bool {
	osInfo := readOSRelease()
	if inst.isDebian {
		codename := osInfo.debianCodename()
		if codename == "" {
			cb("[WARN] Codename Debian/Ubuntu tidak terdeteksi untuk repositori resmi Docker.")
			return false
		}
		repoDistro := "debian"
		if osInfo.isUbuntuBased() {
			repoDistro = "ubuntu"
		}
		listPath := "/etc/apt/sources.list.d/docker.list"
		existingData, readErr := os.ReadFile(listPath)
		hadExisting := readErr == nil && strings.Contains(string(existingData), "download.docker.com")

		keyring := "/etc/apt/keyrings/docker.gpg"
		keyURL := fmt.Sprintf("https://download.docker.com/linux/%s/gpg", repoDistro)
		cb(fmt.Sprintf("Menyiapkan repositori resmi Docker (%s/%s)...", repoDistro, codename))
		if err := inst.ensureGPGKeyring(ctx, cb, keyURL, keyring); err != nil {
			cb(fmt.Sprintf("[WARN] Gagal mengunduh GPG key Docker (%v).", err))
			return false
		}
		listContent := fmt.Sprintf("deb [arch=%s signed-by=%s] https://download.docker.com/linux/%s %s stable\n", dpkgArch(), keyring, repoDistro, codename)
		if err := inst.writeSystemFile(ctx, cb, listPath, []byte(listContent), "644"); err != nil {
			cb(fmt.Sprintf("[WARN] Gagal menulis %s (%v).", listPath, err))
			return false
		}
		if err := inst.runCmd(ctx, cb, "apt-get", "update", "-y"); err != nil {
			cb(fmt.Sprintf("[WARN] apt-get update repo resmi Docker gagal (%v).", err))
			if !hadExisting {
				_ = inst.runCmd(ctx, cb, "rm", "-f", listPath)
				_ = inst.runCmd(ctx, cb, "apt-get", "update", "-y")
			}
			return false
		}
		return true
	}

	if inst.pkgMgr == "dnf" || inst.pkgMgr == "yum" {
		repoPath := "/etc/yum.repos.d/docker-ce.repo"
		if data, err := os.ReadFile(repoPath); err == nil && strings.Contains(string(data), "download.docker.com") {
			cb("Repositori resmi Docker RPM sudah terpasang.")
			return true
		}
		repoDistro := "rhel"
		if osInfo.ID == "fedora" {
			repoDistro = "fedora"
		} else if osInfo.ID == "centos" || strings.Contains(osInfo.IDLike, "centos") {
			repoDistro = "centos"
		}
		repoURL := fmt.Sprintf("https://download.docker.com/linux/%s/docker-ce.repo", repoDistro)
		if !checkURLReachable(ctx, repoURL) {
			cb(fmt.Sprintf("[WARN] Repo resmi Docker (%s) tidak terjangkau.", repoURL))
			return false
		}
		cb("Menyiapkan repositori resmi Docker RPM...")
		reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, "GET", repoURL, nil)
		if err != nil {
			return false
		}
		req.Header.Set("User-Agent", "wdev-installer")
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			return false
		}
		repoBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil || len(repoBytes) == 0 {
			return false
		}
		if err := inst.writeSystemFile(ctx, cb, repoPath, repoBytes, "644"); err != nil {
			return false
		}
		return true
	}

	return false
}

// fetchNetbirdLatestVersion mengambil tag versi rilis terbaru NetBird dari endpoint resmi pkgs.netbird.io.
func fetchNetbirdLatestVersion(ctx context.Context) string {
	reqCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, "GET", "https://pkgs.netbird.io/releases/latest", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "wdev-installer")
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(rel.TagName), "v")
}

type adoptiumReleaseAsset struct {
	ReleaseName string `json:"release_name"`
	Binary      struct {
		Package struct {
			Name string `json:"name"`
			Link string `json:"link"`
		} `json:"package"`
	} `json:"binary"`
	Version struct {
		OpenJDKVersion string `json:"openjdk_version"`
		Semver         string `json:"semver"`
	} `json:"version"`
}

func fetchAdoptiumLatest(ctx context.Context, major string) (downloadURL string, versionStr string, err error) {
	if major == "" {
		major = "21"
	}
	arch := adoptiumArch()
	apiURL := fmt.Sprintf("https://api.adoptium.net/v3/assets/latest/%s/hotspot?architecture=%s&image_type=jdk&os=linux&vendor=eclipse", major, arch)

	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "wdev-control-plane")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("Adoptium API status %d", resp.StatusCode)
	}

	var assets []adoptiumReleaseAsset
	if err := json.NewDecoder(resp.Body).Decode(&assets); err != nil {
		return "", "", err
	}
	if len(assets) == 0 || assets[0].Binary.Package.Link == "" {
		return "", "", fmt.Errorf("rilis Adoptium untuk JDK %s (%s) tidak ditemukan", major, arch)
	}

	ver := assets[0].Version.OpenJDKVersion
	if ver == "" {
		ver = strings.TrimPrefix(assets[0].ReleaseName, "jdk-")
	}
	// Rapikan suffix "+2-LTS" agar ringkas untuk perbandingan versi
	verClean := strings.Split(ver, "+")[0]
	verClean = strings.Split(verClean, "-")[0]
	return assets[0].Binary.Package.Link, verClean, nil
}

type ghReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func fetchGitHubLatestRelease(ctx context.Context, ownerRepo string) (string, []ghReleaseAsset, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", ownerRepo)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "wdev-installer")
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("GitHub API status %d", resp.StatusCode)
	}
	var rel struct {
		TagName string           `json:"tag_name"`
		Assets  []ghReleaseAsset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", nil, err
	}
	return rel.TagName, rel.Assets, nil
}

func findAlternativesCmd() string {
	for _, cmd := range []string{"update-alternatives", "alternatives"} {
		if p, err := exec.LookPath(cmd); err == nil {
			return p
		}
	}
	for _, p := range []string{"/usr/bin/update-alternatives", "/usr/sbin/update-alternatives", "/usr/sbin/alternatives"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func dnfRepoQueryLatest(ctx context.Context, pkgNames ...string) string {
	dnfBin, err := exec.LookPath("dnf")
	if err != nil {
		return "-"
	}
	for _, pkg := range pkgNames {
		if pkg == "" {
			continue
		}
		var out []byte
		var qErr error
		for _, extraArgs := range [][]string{{"-q", "-C", "repoquery"}, {"-q", "repoquery"}} {
			args := append(extraArgs, "--latest-limit=1", "--qf", "%{version}\n", pkg)
			cmd := exec.CommandContext(ctx, dnfBin, args...)
			cmd.Stdin = strings.NewReader("n\n")
			out, qErr = cmd.CombinedOutput()
			if qErr == nil && strings.TrimSpace(string(out)) != "" {
				break
			}
		}
		if qErr != nil {
			continue
		}
		best := ""
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.Contains(line, " ") || !strings.ContainsAny(line, "0123456789") || strings.Contains(line, ":") {
				continue
			}
			if best == "" || compareVersions(best, line) < 0 {
				best = line
			}
		}
		if best != "" {
			return best
		}
	}
	return "-"
}

// ResolveTargetUserAndHome menentukan user dan direktori home aktif tanpa hardcode user tertentu.
func ResolveTargetUserAndHome() (string, string) {
	if sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER")); sudoUser != "" && sudoUser != "root" {
		if u, err := user.Lookup(sudoUser); err == nil && u.HomeDir != "" {
			return u.Username, u.HomeDir
		}
	}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return u.Username, u.HomeDir
	}
	home, _ := os.UserHomeDir()
	return "root", home
}

// removeDistroPackagesIfInstalled menghapus paket bawaan distro yang duplikat dengan biner resmi yang dikelola wdev.
func (inst *Installer) removeDistroPackagesIfInstalled(ctx context.Context, cb LogCallback, pkgs ...string) {
	var installed []string
	if inst.isDebian {
		for _, pkg := range pkgs {
			out, err := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Status}", pkg).CombinedOutput()
			if err == nil && strings.Contains(string(out), "install ok installed") {
				installed = append(installed, pkg)
			}
		}
		if len(installed) > 0 {
			cb(fmt.Sprintf("Membersihkan paket distro duplikat: %s...", strings.Join(installed, ", ")))
			args := append([]string{"remove", "-y"}, installed...)
			_ = inst.runCmd(ctx, cb, "apt-get", args...)
		}
		return
	}

	if inst.pkgMgr == "dnf" || inst.pkgMgr == "yum" {
		for _, pkg := range pkgs {
			if exec.CommandContext(ctx, "rpm", "-q", pkg).Run() == nil {
				installed = append(installed, pkg)
			}
		}
		if len(installed) > 0 {
			cb(fmt.Sprintf("Membersihkan paket distro duplikat: %s...", strings.Join(installed, ", ")))
			args := append([]string{"remove", "-y"}, installed...)
			_ = inst.runCmd(ctx, cb, inst.pkgMgr, args...)
		}
	}
}

// ensureWdevProfileEnv mendelegasikan sinkronisasi PATH ke SyncToolchainEnv
// agar hanya ada satu sumber environment (lihat internal/installer/env.go),
// sekaligus menyelaraskan unit wdev.service bila env unit tertinggal.
func (inst *Installer) ensureWdevProfileEnv(ctx context.Context, cb LogCallback) {
	if err := inst.SyncToolchainEnv(ctx, cb); err != nil {
		cb(fmt.Sprintf("[WARN] Sinkronisasi environment gagal: %v", err))
	}
	if serviceUnitNeedsSync() {
		if err := inst.EnsureWdevServiceUnit(ctx, cb); err != nil {
			cb(fmt.Sprintf("[WARN] Penyelarasan unit wdev.service gagal: %v", err))
		}
	}
}

// findDuplicateToolPaths mendeteksi beberapa lokasi binary untuk tool yang sama (tidak menghitung symlink ke target identik).
func findDuplicateToolPaths(name string) []string {
	dirs := []string{
		"/usr/local/bin",
		"/usr/local/go/bin",
		"/usr/local/cargo/bin",
		"/usr/local/flutter/bin",
		"/usr/local/android-sdk/cmdline-tools/latest/bin",
		"/usr/local/android-sdk/platform-tools",
		"/usr/bin",
		"/usr/sbin",
	}
	if _, home := ResolveTargetUserAndHome(); home != "" {
		dirs = append(dirs, filepath.Join(home, ".local/bin"), filepath.Join(home, ".cargo/bin"))
	}

	seenReal := make(map[string]bool)
	var found []string
	for _, d := range dirs {
		p := filepath.Join(d, name)
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() {
			continue
		}
		// Abaikan /bin jika merupakan symlink ke /usr/bin (usrmerge)
		realPath, err := filepath.EvalSymlinks(p)
		if err != nil {
			realPath = p
		}
		if seenReal[realPath] {
			continue
		}
		seenReal[realPath] = true
		found = append(found, p)
	}
	return found
}

var orcaReleaseCache struct {
	mu        sync.Mutex
	checkedAt time.Time
	version   string
}

// GetOrcaLatestVersion mengambil versi rilis terbaru Orca ADE dari GitHub dengan cache 10 menit.
func GetOrcaLatestVersion(ctx context.Context, forceRefresh bool) string {
	orcaReleaseCache.mu.Lock()
	defer orcaReleaseCache.mu.Unlock()

	if !forceRefresh && orcaReleaseCache.version != "" && time.Since(orcaReleaseCache.checkedAt) < 10*time.Minute {
		return orcaReleaseCache.version
	}

	reqCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	tag, _, err := fetchGitHubLatestRelease(reqCtx, "stablyai/orca")
	if err == nil && tag != "" {
		ver := strings.TrimPrefix(strings.TrimSpace(tag), "v")
		orcaReleaseCache.version = ver
		orcaReleaseCache.checkedAt = time.Now()
		return ver
	}
	return orcaReleaseCache.version
}

// SetOrcaVersionCache memperbarui cache versi Orca setelah pembaruan berhasil.
func SetOrcaVersionCache(version string) {
	orcaReleaseCache.mu.Lock()
	defer orcaReleaseCache.mu.Unlock()
	if version == "" {
		return
	}
	orcaReleaseCache.version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	orcaReleaseCache.checkedAt = time.Now()
}

// HasNewerVersion membandingkan versi terpasang dengan rilis terbaru.
func HasNewerVersion(installed, latest string) bool {
	return hasNewerVersion(installed, latest)
}
