package installer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"wdev/internal/config"
	"wdev/internal/store"
)

type LogCallback = func(line string)

type Installer struct {
	cfg      *config.Config
	store    *store.Store
	distro   string
	pkgMgr   string
	isDebian bool
	sudoPass string
	mu       sync.Mutex
}

func (inst *Installer) SetSudoPassword(pass string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.sudoPass = pass
}

func (inst *Installer) GetSudoPassword() string {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.sudoPass
}

func (inst *Installer) CheckSudoRequired() (bool, error) {
	if os.Geteuid() == 0 {
		return false, nil
	}
	// Check if passwordless sudo works
	cmd := exec.Command("sudo", "-n", "true")
	if err := cmd.Run(); err == nil {
		return false, nil
	}

	// If we already have a cached password, check if it works
	pass := inst.GetSudoPassword()
	if pass != "" {
		checkCmd := exec.Command("sudo", "-S", "-p", "", "-v")
		checkCmd.Stdin = strings.NewReader(pass + "\n")
		if err := checkCmd.Run(); err == nil {
			return false, nil
		}
	}

	return true, nil
}

func (inst *Installer) VerifyAndSetSudoPassword(pass string) error {
	if os.Geteuid() == 0 {
		return nil
	}
	cmd := exec.Command("sudo", "-S", "-p", "", "-v")
	cmd.Stdin = strings.NewReader(pass + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("autentikasi sudo gagal: %s", strings.TrimSpace(string(out)))
	}
	inst.SetSudoPassword(pass)
	return nil
}

func needsSudo(name string, args ...string) bool {
	if os.Geteuid() == 0 {
		return false
	}
	switch name {
	case "apt-get", "apt", "apt-cache", "dpkg", "dnf", "yum", "pacman", "zypper", "systemctl", "ufw", "netfilter-persistent":
		return true
	case "cp", "rm", "mkdir", "chmod", "chown", "tar", "ln", "tee", "unzip":
		for _, arg := range args {
			if strings.HasPrefix(arg, "/usr/local") ||
				strings.HasPrefix(arg, "/etc") ||
				strings.HasPrefix(arg, "/var") ||
				strings.HasPrefix(arg, "/opt") ||
				strings.HasPrefix(arg, "/usr/share") {
				return true
			}
		}
	case "php":
		for _, arg := range args {
			if strings.Contains(arg, "--install-dir=/usr/local") {
				return true
			}
		}
	case "composer":
		for _, arg := range args {
			if arg == "self-update" {
				return true
			}
		}
	case "mkcert":
		for _, arg := range args {
			if arg == "-install" {
				return true
			}
		}
	}
	return false
}

func New(cfg *config.Config, s *store.Store) *Installer {
	distro, pkgMgr, isDebian := detectDistro()
	return &Installer{
		cfg:      cfg,
		store:    s,
		distro:   distro,
		pkgMgr:   pkgMgr,
		isDebian: isDebian,
	}
}

func detectDistro() (distro string, pkgMgr string, isDebian bool) {
	distro = "debian"
	pkgMgr = "apt-get"
	isDebian = true

	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return
	}

	content := strings.ToLower(string(data))
	if strings.Contains(content, "fedora") || strings.Contains(content, "rhel") || strings.Contains(content, "centos") || strings.Contains(content, "rocky") || strings.Contains(content, "alma") {
		distro = "rhel"
		pkgMgr = "dnf"
		isDebian = false
		if _, err := exec.LookPath("dnf"); err != nil {
			pkgMgr = "yum"
		}
	} else if strings.Contains(content, "arch") || strings.Contains(content, "manjaro") {
		distro = "arch"
		pkgMgr = "pacman"
		isDebian = false
	} else if strings.Contains(content, "suse") {
		distro = "suse"
		pkgMgr = "zypper"
		isDebian = false
	}

	return
}

func (inst *Installer) runCmd(ctx context.Context, cb LogCallback, name string, args ...string) error {
	useSudo := needsSudo(name, args...)
	cmdStr := fmt.Sprintf("%s %s", name, strings.Join(args, " "))
	if useSudo {
		cmdStr = "[sudo] " + cmdStr
	}
	cb(fmt.Sprintf("==> Menjalankan: %s", cmdStr))

	var cmd *exec.Cmd
	var stdinPipe io.WriteCloser

	if useSudo {
		sudoPass := inst.GetSudoPassword()
		if sudoPass != "" {
			sudoArgs := append([]string{"-S", "-p", "", name}, args...)
			cmd = exec.CommandContext(ctx, "sudo", sudoArgs...)
			var err error
			stdinPipe, err = cmd.StdinPipe()
			if err != nil {
				return err
			}
		} else {
			sudoArgs := append([]string{"-n", name}, args...)
			cmd = exec.CommandContext(ctx, "sudo", sudoArgs...)
		}
	} else {
		cmd = exec.CommandContext(ctx, name, args...)
	}

	cmd.Env = append(os.Environ(),
		"DEBIAN_FRONTEND=noninteractive",
		"COMPOSER_ALLOW_SUPERUSER=1",
		"COMPOSER_NO_INTERACTION=1",
		"NEEDRESTART_MODE=a",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		cb(fmt.Sprintf("[ERROR] Gagal memulai: %v", err))
		return err
	}

	if stdinPipe != nil {
		go func() {
			defer stdinPipe.Close()
			_, _ = io.WriteString(stdinPipe, inst.GetSudoPassword()+"\n")
		}()
	}

	var wg sync.WaitGroup
	wg.Add(2)

	scanPipe := func(r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			cb(scanner.Text())
		}
	}

	go scanPipe(stdout)
	go scanPipe(stderr)

	wg.Wait()
	if err := cmd.Wait(); err != nil {
		if useSudo && inst.GetSudoPassword() == "" {
			if req, _ := inst.CheckSudoRequired(); req {
				cb("[ERROR] Operasi memerlukan password sudo. Masukkan password sudo di Web Dashboard.")
			}
		}
		cb(fmt.Sprintf("[ERROR] Perintah keluar dengan kode error: %v", err))
		return err
	}

	return nil
}

// 1. Base Dependencies
func (inst *Installer) InstallBase(ctx context.Context, cb LogCallback) error {
	cb("--- [1] Memasang Dependensi Dasar Sistem ---")
	if inst.isDebian {
		if err := inst.runCmd(ctx, cb, "apt-get", "update", "-y"); err != nil {
			return err
		}
		pkgs := []string{
			"install", "-y", "build-essential", "curl", "wget", "git", "jq", "unzip",
			"tar", "xz-utils", "pkg-config", "libssl-dev", "cmake", "clang",
			"software-properties-common", "ca-certificates", "gnupg",
		}
		return inst.runCmd(ctx, cb, "apt-get", pkgs...)
	}

	return inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", "gcc", "gcc-c++", "make", "curl", "wget", "git", "jq", "unzip", "tar", "xz", "cmake", "clang", "openssl-devel")
}

// 2. Node.js (Multi-version)
func (inst *Installer) InstallNode(ctx context.Context, cb LogCallback, ver string) error {
	if ver == "" {
		ver = "22"
	}
	cb(fmt.Sprintf("--- [2] Memasang Node.js v%s ---", ver))

	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}

	// Fetch full version from nodejs.org
	fullVer := ver
	if !strings.Contains(ver, ".") {
		resp, err := http.Get("https://nodejs.org/dist/index.json")
		if err == nil {
			defer resp.Body.Close()
			var releases []struct {
				Version string `json:"version"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&releases); err == nil {
				for _, r := range releases {
					vClean := strings.TrimPrefix(r.Version, "v")
					if strings.HasPrefix(vClean, ver+".") {
						fullVer = vClean
						break
					}
				}
			}
		}
	}

	tarName := fmt.Sprintf("node-v%s-linux-%s.tar.xz", fullVer, arch)
	url := fmt.Sprintf("https://nodejs.org/dist/v%s/%s", fullVer, tarName)
	nodeBaseDir := "/usr/local/lib/nodejs"
	targetDir := filepath.Join(nodeBaseDir, fmt.Sprintf("node-v%s-linux-%s", fullVer, arch))

	cb(fmt.Sprintf("Mengunduh Node.js dari: %s", url))
	tmpTar := filepath.Join(os.TempDir(), tarName)

	if err := downloadFile(url, tmpTar); err != nil {
		return fmt.Errorf("gagal mengunduh Node.js: %w", err)
	}
	defer os.Remove(tmpTar)

	_ = inst.runCmd(ctx, cb, "mkdir", "-p", nodeBaseDir)
	_ = inst.runCmd(ctx, cb, "rm", "-rf", targetDir)

	cb("Mengekstrak berkas Node.js...")
	if err := inst.runCmd(ctx, cb, "tar", "-xJf", tmpTar, "-C", nodeBaseDir); err != nil {
		return err
	}

	// Create symlinks
	bins := []string{"node", "npm", "npx", "corepack"}
	for _, b := range bins {
		src := filepath.Join(targetDir, "bin", b)
		dst := filepath.Join("/usr/local/bin", b)
		_ = inst.runCmd(ctx, cb, "ln", "-sf", src, dst)
	}

	cb(fmt.Sprintf("✓ Node.js v%s berhasil dipasang di /usr/local/bin/node", fullVer))
	return nil
}

// 3. Rust, Mold Linker & Sccache
func (inst *Installer) InstallRust(ctx context.Context, cb LogCallback) error {
	cb("--- [3] Memasang Rust Toolchain, Mold Linker & Sccache ---")

	// 1. Rustup
	if _, err := exec.LookPath("rustc"); err != nil {
		cb("Memasang Rust via rustup...")
		scriptPath := filepath.Join(os.TempDir(), "rustup.sh")
		if err := downloadFile("https://sh.rustup.rs", scriptPath); err != nil {
			return err
		}
		defer os.Remove(scriptPath)
		_ = os.Chmod(scriptPath, 0755)
		if err := inst.runCmd(ctx, cb, "sh", scriptPath, "-y", "--default-toolchain", "stable"); err != nil {
			return err
		}
	} else {
		cb("Rust sudah terpasang, memperbarui...")
		_ = inst.runCmd(ctx, cb, "rustup", "update", "stable")
	}

	// 2. Mold Linker
	cb("Memasang Mold linker...")
	if inst.isDebian {
		_ = inst.runCmd(ctx, cb, "apt-get", "install", "-y", "mold")
	} else {
		_ = inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", "mold")
	}

	// Symlink cargo binaries to /usr/local/bin if needed
	home, _ := os.UserHomeDir()
	cargoBin := filepath.Join(home, ".cargo", "bin")
	if entries, err := os.ReadDir(cargoBin); err == nil {
		for _, e := range entries {
			src := filepath.Join(cargoBin, e.Name())
			dst := filepath.Join("/usr/local/bin", e.Name())
			if _, err := os.Stat(dst); os.IsNotExist(err) {
				_ = inst.runCmd(ctx, cb, "ln", "-sf", src, dst)
			}
		}
	}

	cb("✓ Rust Toolchain & Mold siap digunakan.")
	return nil
}

// 4. Golang
func (inst *Installer) InstallGolang(ctx context.Context, cb LogCallback) error {
	cb("--- [4] Memasang Golang ---")
	arch := runtime.GOARCH

	// Determine latest stable Go
	latestVer := "1.24.0"
	resp, err := http.Get("https://go.dev/dl/?mode=json")
	if err == nil {
		defer resp.Body.Close()
		var releases []struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&releases); err == nil && len(releases) > 0 {
			for _, r := range releases {
				if r.Stable {
					latestVer = strings.TrimPrefix(r.Version, "go")
					break
				}
			}
		}
	}

	tarName := fmt.Sprintf("go%s.linux-%s.tar.gz", latestVer, arch)
	url := fmt.Sprintf("https://go.dev/dl/%s", tarName)
	tmpTar := filepath.Join(os.TempDir(), tarName)

	cb(fmt.Sprintf("Mengunduh Go v%s dari: %s", latestVer, url))
	if err := downloadFile(url, tmpTar); err != nil {
		return fmt.Errorf("gagal mengunduh Go: %w", err)
	}
	defer os.Remove(tmpTar)

	cb("Mengekstrak ke /usr/local/go...")
	_ = inst.runCmd(ctx, cb, "rm", "-rf", "/usr/local/go")
	if err := inst.runCmd(ctx, cb, "tar", "-xzf", tmpTar, "-C", "/usr/local"); err != nil {
		return err
	}

	// Symlink go & gofmt
	_ = inst.runCmd(ctx, cb, "rm", "-f", "/usr/local/bin/go", "/usr/local/bin/gofmt")
	_ = inst.runCmd(ctx, cb, "ln", "-sf", "/usr/local/go/bin/go", "/usr/local/bin/go")
	_ = inst.runCmd(ctx, cb, "ln", "-sf", "/usr/local/go/bin/gofmt", "/usr/local/bin/gofmt")

	cb(fmt.Sprintf("✓ Go v%s berhasil dipasang di /usr/local/bin/go", latestVer))
	return nil
}

// 5. OpenJDK Java
func (inst *Installer) InstallJava(ctx context.Context, cb LogCallback, ver string) error {
	if ver == "" {
		ver = "21"
	}
	cb(fmt.Sprintf("--- [5] Memasang OpenJDK Java %s ---", ver))

	if inst.isDebian {
		pkg := fmt.Sprintf("openjdk-%s-jdk", ver)
		return inst.runCmd(ctx, cb, "apt-get", "install", "-y", pkg)
	}

	pkg := fmt.Sprintf("java-%s-openjdk-devel", ver)
	return inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", pkg)
}

// 6. Android SDK
func (inst *Installer) InstallAndroidSDK(ctx context.Context, cb LogCallback) error {
	cb("--- [6] Memasang Android SDK & CLI Tools ---")
	sdkDir := "/usr/local/android-sdk"
	_ = inst.runCmd(ctx, cb, "mkdir", "-p", sdkDir)

	zipName := "commandlinetools-linux-11076708_latest.zip"
	url := "https://dl.google.com/android/repository/" + zipName
	tmpZip := filepath.Join(os.TempDir(), zipName)

	cb("Mengunduh Android Command Line Tools...")
	if err := downloadFile(url, tmpZip); err != nil {
		return err
	}
	defer os.Remove(tmpZip)

	cmdlineDir := filepath.Join(sdkDir, "cmdline-tools")
	_ = inst.runCmd(ctx, cb, "mkdir", "-p", cmdlineDir)
	if err := inst.runCmd(ctx, cb, "unzip", "-qo", tmpZip, "-d", cmdlineDir); err != nil {
		return err
	}

	// Google requires cmdline-tools/latest structure
	latestDir := filepath.Join(cmdlineDir, "latest")
	extractedDir := filepath.Join(cmdlineDir, "cmdline-tools")
	if _, err := os.Stat(extractedDir); err == nil {
		_ = inst.runCmd(ctx, cb, "rm", "-rf", latestDir)
		_ = inst.runCmd(ctx, cb, "mv", extractedDir, latestDir)
	}

	cb("Menerima lisensi Android SDK...")
	sdkManager := filepath.Join(latestDir, "bin", "sdkmanager")
	if _, err := os.Stat(sdkManager); err == nil {
		_ = inst.runCmd(ctx, cb, "bash", "-c", fmt.Sprintf("yes | %s --sdk_root=%s --licenses", sdkManager, sdkDir))
		_ = inst.runCmd(ctx, cb, sdkManager, fmt.Sprintf("--sdk_root=%s", sdkDir), "platform-tools", "platforms;android-34", "platforms;android-35")
	}

	cb("✓ Android SDK berhasil dipasang di " + sdkDir)
	return nil
}

// 7. Flutter SDK
func (inst *Installer) InstallFlutter(ctx context.Context, cb LogCallback) error {
	cb("--- [7] Memasang Flutter SDK ---")
	flutterDir := "/usr/local/flutter"

	if _, err := os.Stat(flutterDir); os.IsNotExist(err) {
		cb("Cloning repository Flutter channel stable...")
		if err := inst.runCmd(ctx, cb, "git", "clone", "-b", "stable", "https://github.com/flutter/flutter.git", flutterDir); err != nil {
			return err
		}
	} else {
		cb("Memperbarui Flutter repo...")
		_ = inst.runCmd(ctx, cb, "git", "-C", flutterDir, "pull")
	}

	// Symlink flutter & dart
	_ = inst.runCmd(ctx, cb, "ln", "-sf", filepath.Join(flutterDir, "bin", "flutter"), "/usr/local/bin/flutter")
	_ = inst.runCmd(ctx, cb, "ln", "-sf", filepath.Join(flutterDir, "bin", "dart"), "/usr/local/bin/dart")

	cb("✓ Flutter SDK berhasil dipasang.")
	return nil
}

// 8. PostgreSQL
func (inst *Installer) InstallPostgres(ctx context.Context, cb LogCallback, ver string) error {
	cb("--- [8] Memasang PostgreSQL Server ---")
	if inst.isDebian {
		pkg := "postgresql"
		if ver != "" && ver != "system" {
			pkg = fmt.Sprintf("postgresql-%s", ver)
		}
		if err := inst.runCmd(ctx, cb, "apt-get", "install", "-y", pkg, "postgresql-contrib"); err != nil {
			return err
		}
	} else {
		if err := inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", "postgresql-server", "postgresql-contrib"); err != nil {
			return err
		}
		_ = inst.runCmd(ctx, cb, "postgresql-setup", "--initdb")
	}

	_ = inst.runCmd(ctx, cb, "systemctl", "enable", "--now", "postgresql")
	cb("✓ PostgreSQL berhasil dipasang dan diaktifkan.")
	return nil
}

// 9. Redis
func (inst *Installer) InstallRedis(ctx context.Context, cb LogCallback) error {
	cb("--- [9] Memasang Redis Cache Server ---")
	if inst.isDebian {
		if err := inst.runCmd(ctx, cb, "apt-get", "install", "-y", "redis-server"); err != nil {
			return err
		}
		_ = inst.runCmd(ctx, cb, "systemctl", "enable", "--now", "redis-server")
	} else {
		if err := inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", "redis"); err != nil {
			return err
		}
		_ = inst.runCmd(ctx, cb, "systemctl", "enable", "--now", "redis")
	}
	cb("✓ Redis Server berhasil dipasang dan diaktifkan.")
	return nil
}

// 10. Nginx Mainline
func (inst *Installer) InstallNginx(ctx context.Context, cb LogCallback) error {
	cb("--- [10] Memasang Nginx Web Server ---")
	if inst.isDebian {
		if err := inst.runCmd(ctx, cb, "apt-get", "install", "-y", "nginx"); err != nil {
			return err
		}
	} else {
		if err := inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", "nginx"); err != nil {
			return err
		}
	}

	_ = os.MkdirAll("/etc/nginx/sites-available", 0755)
	_ = os.MkdirAll("/etc/nginx/sites-enabled", 0755)
	_ = inst.runCmd(ctx, cb, "systemctl", "enable", "--now", "nginx")
	cb("✓ Nginx Web Server berhasil dipasang dan diaktifkan.")
	return nil
}

// 11. PHP & PHP-FPM
func (inst *Installer) InstallPHP(ctx context.Context, cb LogCallback, ver string) error {
	if ver == "" {
		ver = "8.4"
	}
	cb(fmt.Sprintf("--- [11] Memasang PHP & PHP-FPM %s ---", ver))

	if inst.isDebian {
		// Install PPA/repo ondrej or sury if needed
		exts := []string{
			fmt.Sprintf("php%s-fpm", ver),
			fmt.Sprintf("php%s-cli", ver),
			fmt.Sprintf("php%s-common", ver),
			fmt.Sprintf("php%s-curl", ver),
			fmt.Sprintf("php%s-mbstring", ver),
			fmt.Sprintf("php%s-xml", ver),
			fmt.Sprintf("php%s-zip", ver),
			fmt.Sprintf("php%s-pgsql", ver),
			fmt.Sprintf("php%s-mysql", ver),
			fmt.Sprintf("php%s-redis", ver),
			fmt.Sprintf("php%s-intl", ver),
			fmt.Sprintf("php%s-gd", ver),
			fmt.Sprintf("php%s-bcmath", ver),
		}
		args := append([]string{"install", "-y"}, exts...)
		if err := inst.runCmd(ctx, cb, "apt-get", args...); err != nil {
			return err
		}
		_ = inst.runCmd(ctx, cb, "systemctl", "enable", "--now", fmt.Sprintf("php%s-fpm", ver))
	} else {
		exts := []string{
			"php-fpm", "php-cli", "php-common", "php-curl", "php-mbstring",
			"php-xml", "php-zip", "php-pgsql", "php-mysqlnd", "php-intl", "php-gd", "php-bcmath",
		}
		args := append([]string{"install", "-y"}, exts...)
		if err := inst.runCmd(ctx, cb, inst.pkgMgr, args...); err != nil {
			return err
		}
		_ = inst.runCmd(ctx, cb, "systemctl", "enable", "--now", "php-fpm")
	}

	cb(fmt.Sprintf("✓ PHP & PHP-FPM %s berhasil dipasang.", ver))
	return nil
}

// 12. Composer
func (inst *Installer) InstallComposer(ctx context.Context, cb LogCallback) error {
	cb("--- [12] Memasang Composer ---")
	installerPath := filepath.Join(os.TempDir(), "composer-setup.php")
	if err := downloadFile("https://getcomposer.org/installer", installerPath); err != nil {
		return err
	}
	defer os.Remove(installerPath)

	if err := inst.runCmd(ctx, cb, "php", installerPath, "--install-dir=/usr/local/bin", "--filename=composer"); err != nil {
		return err
	}
	cb("✓ Composer berhasil dipasang di /usr/local/bin/composer")
	return nil
}

// 13. Local SSL (mkcert)
func (inst *Installer) InstallMkcert(ctx context.Context, cb LogCallback) error {
	cb("--- [13] Memasang Local SSL (mkcert) ---")
	arch := runtime.GOARCH

	url := fmt.Sprintf("https://github.com/FiloSottile/mkcert/releases/download/v1.4.4/mkcert-v1.4.4-linux-%s", arch)
	dst := "/usr/local/bin/mkcert"
	tmpFile := filepath.Join(os.TempDir(), "mkcert.tmp")

	if err := downloadFile(url, tmpFile); err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	_ = inst.runCmd(ctx, cb, "cp", tmpFile, dst)
	_ = inst.runCmd(ctx, cb, "chmod", "+x", dst)

	_ = inst.runCmd(ctx, cb, "mkcert", "-install")
	_ = inst.runCmd(ctx, cb, "mkdir", "-p", "/etc/ssl/local-certs")

	cb("✓ mkcert berhasil dipasang dan root CA lokal diinisialisasi.")
	return nil
}

// Full PHP Stack
func (inst *Installer) InstallPhpStack(ctx context.Context, cb LogCallback) error {
	cb("==> Memulai instalasi Full PHP Web Stack (Nginx + PHP 8.4 + Composer + mkcert)...")
	if err := inst.InstallNginx(ctx, cb); err != nil {
		return err
	}
	if err := inst.InstallPHP(ctx, cb, "8.4"); err != nil {
		return err
	}
	if err := inst.InstallComposer(ctx, cb); err != nil {
		return err
	}
	if err := inst.InstallMkcert(ctx, cb); err != nil {
		return err
	}
	cb("✓ Full PHP Web Stack berhasil dipasang!")
	return nil
}

// Orca ADE (Desktop & Headless)
func (inst *Installer) InstallOrca(ctx context.Context, cb LogCallback, mode string) error {
	cb("==> Memeriksa rilis terbaru Orca ADE...")
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/stablyai/orca/releases/latest", nil)
	req.Header.Set("User-Agent", "wdev-installer")
	resp, err := client.Do(req)
	var downloadURL string
	if err == nil && resp.StatusCode == 200 {
		var release struct {
			TagName string `json:"tag_name"`
			Assets  []struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			} `json:"assets"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&release)
		resp.Body.Close()
		arch := "amd64"
		if runtime.GOARCH == "arm64" {
			arch = "arm64"
		}
		for _, asset := range release.Assets {
			if strings.HasSuffix(asset.Name, ".deb") && strings.Contains(asset.Name, arch) {
				downloadURL = asset.URL
				break
			}
		}
	}
	if downloadURL == "" {
		downloadURL = "https://github.com/stablyai/orca/releases/download/v0.34.0/orca-ide_0.34.0_amd64.deb"
	}

	debPath := filepath.Join(os.TempDir(), "orca-ide.deb")
	cb(fmt.Sprintf("==> Mengunduh Orca ADE: %s", downloadURL))
	if err := downloadFile(downloadURL, debPath); err != nil {
		return fmt.Errorf("gagal mengunduh Orca: %w", err)
	}
	defer os.Remove(debPath)

	cb("==> Menginstal paket Orca ADE...")
	_ = inst.runCmd(ctx, cb, "apt-get", "install", "-y", debPath)

	if mode == "headless" {
		cb("==> Mengonfigurasi mode Headless Server (orca.service)...")
		serviceUnit := `[Unit]
Description=Orca ADE Headless Server
After=network.target

[Service]
Type=simple
User=dietpi
Environment=ORCA_USER_DATA_PATH=/home/dietpi/.config/orca
ExecStart=/home/dietpi/.local/bin/orca serve
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`
		_ = os.WriteFile("/etc/systemd/system/orca.service", []byte(serviceUnit), 0644)
		_ = inst.runCmd(ctx, cb, "systemctl", "daemon-reload")
		_ = inst.runCmd(ctx, cb, "systemctl", "enable", "--now", "orca.service")
	}

	cb("✓ Orca ADE berhasil diinstal!")
	return nil
}

func (inst *Installer) UninstallOrca(ctx context.Context, cb LogCallback, purge bool) error {
	cb("==> Menghentikan layanan Orca ADE...")
	_ = inst.runCmd(ctx, cb, "systemctl", "disable", "--now", "orca.service")
	_ = os.Remove("/etc/systemd/system/orca.service")
	_ = inst.runCmd(ctx, cb, "systemctl", "daemon-reload")

	cb("==> Menghapus paket Orca IDE...")
	_ = inst.runCmd(ctx, cb, "apt-get", "remove", "-y", "orca-ide")

	if purge {
		cb("==> Menghapus data konfigurasi...")
		_ = os.RemoveAll("/home/dietpi/.config/orca")
	}

	cb("✓ Orca ADE berhasil di-uninstall.")
	return nil
}

// 14. Oh My Zsh
func (inst *Installer) InstallZsh(ctx context.Context, cb LogCallback, theme string) error {
	if theme == "" {
		theme = "powerlevel10k"
	}
	cb(fmt.Sprintf("--- [14] Memasang Oh My Zsh (Tema %s) ---", theme))

	// Install zsh package
	if inst.isDebian {
		_ = inst.runCmd(ctx, cb, "apt-get", "install", "-y", "zsh")
	} else {
		_ = inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", "zsh")
	}

	home, _ := os.UserHomeDir()
	omzDir := filepath.Join(home, ".oh-my-zsh")

	if _, err := os.Stat(omzDir); os.IsNotExist(err) {
		_ = inst.runCmd(ctx, cb, "git", "clone", "--depth=1", "https://github.com/ohmyzsh/ohmyzsh.git", omzDir)
	}

	// Plugins
	pluginsDir := filepath.Join(omzDir, "custom", "plugins")
	_ = os.MkdirAll(pluginsDir, 0755)
	pAuto := filepath.Join(pluginsDir, "zsh-autosuggestions")
	if _, err := os.Stat(pAuto); os.IsNotExist(err) {
		_ = inst.runCmd(ctx, cb, "git", "clone", "--depth=1", "https://github.com/zsh-users/zsh-autosuggestions", pAuto)
	}
	pSyn := filepath.Join(pluginsDir, "zsh-syntax-highlighting")
	if _, err := os.Stat(pSyn); os.IsNotExist(err) {
		_ = inst.runCmd(ctx, cb, "git", "clone", "--depth=1", "https://github.com/zsh-users/zsh-syntax-highlighting", pSyn)
	}

	// Themes
	themesDir := filepath.Join(omzDir, "custom", "themes")
	_ = os.MkdirAll(themesDir, 0755)
	p10k := filepath.Join(themesDir, "powerlevel10k")
	if _, err := os.Stat(p10k); os.IsNotExist(err) {
		_ = inst.runCmd(ctx, cb, "git", "clone", "--depth=1", "https://github.com/romkatv/powerlevel10k.git", p10k)
	}

	cb(fmt.Sprintf("✓ Oh My Zsh berhasil dikonfigurasi dengan tema %s.", theme))
	return nil
}

// 15. Cloudflare Tunnel (cloudflared)
func (inst *Installer) InstallCloudflared(ctx context.Context, cb LogCallback) error {
	cb("--- [15] Memasang cloudflared binary ---")
	arch := runtime.GOARCH

	url := fmt.Sprintf("https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-%s", arch)
	dst := "/usr/local/bin/cloudflared"
	tmpFile := filepath.Join(os.TempDir(), "cloudflared.tmp")

	if err := downloadFile(url, tmpFile); err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	_ = inst.runCmd(ctx, cb, "cp", tmpFile, dst)
	_ = inst.runCmd(ctx, cb, "chmod", "+x", dst)

	cb("✓ cloudflared berhasil dipasang di /usr/local/bin/cloudflared")
	return nil
}

// Full 14-Step Auto Setup
func (inst *Installer) RunFullInstall(ctx context.Context, cb LogCallback) error {
	cb("=================================================================")
	cb(" MEMULAI INSTALASI LENGKAP LINGKUNGAN PENGEMBANGAN LINUX (14 LANGKAH)")
	cb("=================================================================")

	steps := []struct {
		Name string
		Fn   func() error
	}{
		{"1. Dependensi Dasar Sistem", func() error { return inst.InstallBase(ctx, cb) }},
		{"2. Node.js LTS", func() error { return inst.InstallNode(ctx, cb, "22") }},
		{"3. Rust Toolchain & Mold", func() error { return inst.InstallRust(ctx, cb) }},
		{"4. Golang", func() error { return inst.InstallGolang(ctx, cb) }},
		{"5. OpenJDK Java 21", func() error { return inst.InstallJava(ctx, cb, "21") }},
		{"6. Android SDK & CLI Tools", func() error { return inst.InstallAndroidSDK(ctx, cb) }},
		{"7. Flutter SDK Stable", func() error { return inst.InstallFlutter(ctx, cb) }},
		{"8. PostgreSQL Server", func() error { return inst.InstallPostgres(ctx, cb, "16") }},
		{"9. Redis Cache Server", func() error { return inst.InstallRedis(ctx, cb) }},
		{"10. Nginx Mainline Web Server", func() error { return inst.InstallNginx(ctx, cb) }},
		{"11. PHP & PHP-FPM 8.4", func() error { return inst.InstallPHP(ctx, cb, "8.4") }},
		{"12. Composer Package Manager", func() error { return inst.InstallComposer(ctx, cb) }},
		{"13. Local SSL (mkcert)", func() error { return inst.InstallMkcert(ctx, cb) }},
		{"14. Oh My Zsh & Terminal", func() error { return inst.InstallZsh(ctx, cb, "powerlevel10k") }},
	}

	for i, s := range steps {
		cb(fmt.Sprintf("\n>>> Langkah [%d/14]: %s", i+1, s.Name))
		if err := s.Fn(); err != nil {
			cb(fmt.Sprintf("[ERROR] Langkah %s gagal: %v", s.Name, err))
			return err
		}
	}

	cb("\n=================================================================")
	cb(" ✓ SELURUH 14 LANGKAH INSTALASI LENGKAP TELAH SELESAI DENGAN SUKSES!")
	cb("=================================================================")
	return nil
}

// Update All
func (inst *Installer) RunUpdateAll(ctx context.Context, cb LogCallback) error {
	cb("=== MEMPERBARUI SELURUH SISTEM & TOOLCHAIN ===")
	if inst.isDebian {
		_ = inst.runCmd(ctx, cb, "apt-get", "update", "-y")
		_ = inst.runCmd(ctx, cb, "apt-get", "upgrade", "-y")
	} else {
		_ = inst.runCmd(ctx, cb, inst.pkgMgr, "upgrade", "-y")
	}

	if _, err := exec.LookPath("composer"); err == nil {
		_ = inst.runCmd(ctx, cb, "composer", "self-update")
	}
	if _, err := exec.LookPath("rustup"); err == nil {
		_ = inst.runCmd(ctx, cb, "rustup", "update")
	}
	if _, err := exec.LookPath("flutter"); err == nil {
		_ = inst.runCmd(ctx, cb, "flutter", "upgrade")
	}

	cb("✓ Pembaruan seluruh sistem selesai.")
	return nil
}

// CheckUpdates memeriksa ketersediaan pembaruan toolchain dan paket sistem secara native.
func (inst *Installer) CheckUpdates(ctx context.Context, cb LogCallback) error {
	cb("============================================================")
	cb("       MEMERIKSA KETERSEDIAAN PEMBARUAN VERSI SISTEM        ")
	cb("============================================================")
	cb("Memeriksa versi terpasang dan rilis terbaru secara native...\n")

	type VersionCheck struct {
		Name      string
		Installed string
		Latest    string
		HasUpdate bool
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}

	fetchJSON := func(url string, target any) error {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "wdev-control-plane")
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		return json.NewDecoder(resp.Body).Decode(target)
	}

	getCandidate := func(pkgName string) string {
		out, err := exec.CommandContext(ctx, "apt-cache", "policy", pkgName).CombinedOutput()
		if err != nil {
			return "-"
		}
		lines := strings.Split(string(out), "\n")
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "Candidate:") {
				parts := strings.Fields(l)
				if len(parts) >= 2 {
					cand := parts[1]
					cand = strings.Split(cand, "+")[0]
					cand = strings.Split(cand, "~")[0]
					cand = strings.Split(cand, "-")[0]
					cand = strings.TrimPrefix(cand, "5:")
					return cand
				}
			}
		}
		return "-"
	}

	var results []VersionCheck

	// 1. Golang
	goInst := "-"
	if out, err := exec.CommandContext(ctx, "go", "version").CombinedOutput(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 3 {
			goInst = fields[2]
		}
	}
	goLatest := "-"
	var goReleases []struct {
		Version string `json:"version"`
	}
	if err := fetchJSON("https://go.dev/dl/?mode=json", &goReleases); err == nil && len(goReleases) > 0 {
		goLatest = goReleases[0].Version
	}
	results = append(results, VersionCheck{
		Name:      "Golang",
		Installed: goInst,
		Latest:    goLatest,
		HasUpdate: goLatest != "-" && goInst != "-" && goLatest != goInst,
	})

	// 2. Node.js
	nodeInst := "-"
	if out, err := exec.CommandContext(ctx, "node", "-v").CombinedOutput(); err == nil {
		nodeInst = strings.TrimSpace(string(out))
	}
	nodeLatest := "-"
	var nodeReleases []struct {
		Version string `json:"version"`
		LTS     any    `json:"lts"`
	}
	if err := fetchJSON("https://nodejs.org/dist/index.json", &nodeReleases); err == nil {
		curMajor := ""
		if strings.HasPrefix(nodeInst, "v") {
			curMajor = strings.Split(nodeInst, ".")[0] + "."
		}
		for _, nr := range nodeReleases {
			if curMajor != "" && strings.HasPrefix(nr.Version, curMajor) {
				nodeLatest = nr.Version
				break
			}
		}
		if nodeLatest == "-" {
			for _, nr := range nodeReleases {
				if nr.LTS != false && nr.LTS != nil {
					nodeLatest = nr.Version
					break
				}
			}
		}
	}
	results = append(results, VersionCheck{
		Name:      "Node.js",
		Installed: nodeInst,
		Latest:    nodeLatest,
		HasUpdate: nodeLatest != "-" && nodeInst != "-" && nodeLatest != nodeInst,
	})

	// 3. Rust & Cargo
	rustInst := "-"
	if out, err := exec.CommandContext(ctx, "rustc", "-V").CombinedOutput(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 2 {
			rustInst = fields[1]
		}
	}
	rustLatest := "-"
	var ghRelease struct {
		TagName string `json:"tag_name"`
	}
	if err := fetchJSON("https://api.github.com/repos/rust-lang/rust/releases/latest", &ghRelease); err == nil {
		rustLatest = strings.TrimPrefix(ghRelease.TagName, "rust-")
		rustLatest = strings.TrimPrefix(rustLatest, "v")
	}
	results = append(results, VersionCheck{
		Name:      "Rustc / Cargo",
		Installed: rustInst,
		Latest:    rustLatest,
		HasUpdate: rustLatest != "-" && rustInst != "-" && rustLatest != rustInst,
	})

	// 4. Composer
	compInst := "-"
	if out, err := exec.CommandContext(ctx, "composer", "-V").CombinedOutput(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 3 {
			compInst = fields[2]
		}
	}
	compLatest := "-"
	ghRelease.TagName = ""
	if err := fetchJSON("https://api.github.com/repos/composer/composer/releases/latest", &ghRelease); err == nil {
		compLatest = strings.TrimPrefix(ghRelease.TagName, "v")
	}
	results = append(results, VersionCheck{
		Name:      "Composer",
		Installed: compInst,
		Latest:    compLatest,
		HasUpdate: compLatest != "-" && compInst != "-" && compLatest != compInst,
	})

	// 5. Cloudflared
	cfInst := "-"
	if out, err := exec.CommandContext(ctx, "cloudflared", "-v").CombinedOutput(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 3 {
			cfInst = fields[2]
		}
	}
	cfLatest := "-"
	ghRelease.TagName = ""
	if err := fetchJSON("https://api.github.com/repos/cloudflare/cloudflared/releases/latest", &ghRelease); err == nil {
		cfLatest = strings.TrimPrefix(ghRelease.TagName, "v")
	}
	results = append(results, VersionCheck{
		Name:      "Cloudflared",
		Installed: cfInst,
		Latest:    cfLatest,
		HasUpdate: cfLatest != "-" && cfInst != "-" && cfLatest != cfInst,
	})

	// 6. Nginx
	nginxInst := "-"
	findBin := func(name string) string {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		for _, dir := range []string{"/usr/sbin", "/sbin", "/usr/local/sbin", "/usr/local/bin", "/usr/bin"} {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		return ""
	}

	if nginxBin := findBin("nginx"); nginxBin != "" {
		if out, err := exec.CommandContext(ctx, nginxBin, "-v").CombinedOutput(); err == nil {
			s := string(out)
			if idx := strings.Index(s, "nginx/"); idx != -1 {
				nginxInst = strings.TrimSpace(s[idx+6:])
			}
		}
	}
	nginxLatest := getCandidate("nginx")
	results = append(results, VersionCheck{
		Name:      "Nginx Web Server",
		Installed: nginxInst,
		Latest:    nginxLatest,
		HasUpdate: nginxLatest != "-" && nginxInst != "-" && !strings.HasPrefix(nginxLatest, nginxInst),
	})

	// 7. PHP
	phpInst := "-"
	if out, err := exec.CommandContext(ctx, "php", "-r", "echo PHP_VERSION;").CombinedOutput(); err == nil {
		phpInst = strings.TrimSpace(string(out))
	}
	phpLatest := getCandidate("php8.4")
	results = append(results, VersionCheck{
		Name:      "PHP & PHP-FPM",
		Installed: phpInst,
		Latest:    phpLatest,
		HasUpdate: phpLatest != "-" && phpInst != "-" && !strings.HasPrefix(phpLatest, phpInst),
	})

	// 8. PostgreSQL
	pgInst := "-"
	if out, err := exec.CommandContext(ctx, "psql", "-V").CombinedOutput(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 3 {
			pgInst = fields[2]
		}
	}
	pgLatest := getCandidate("postgresql-16")
	results = append(results, VersionCheck{
		Name:      "PostgreSQL",
		Installed: pgInst,
		Latest:    pgLatest,
		HasUpdate: pgLatest != "-" && pgInst != "-" && !strings.HasPrefix(pgLatest, pgInst),
	})

	// 9. Redis
	redisInst := "-"
	if out, err := exec.CommandContext(ctx, "redis-server", "-v").CombinedOutput(); err == nil {
		fields := strings.Fields(string(out))
		for _, f := range fields {
			if strings.HasPrefix(f, "v=") {
				redisInst = strings.TrimPrefix(f, "v=")
				break
			}
		}
	}
	redisLatest := getCandidate("redis-server")
	results = append(results, VersionCheck{
		Name:      "Redis Cache",
		Installed: redisInst,
		Latest:    redisLatest,
		HasUpdate: redisLatest != "-" && redisInst != "-" && !strings.HasPrefix(redisLatest, redisInst),
	})

	// 10. NetBird
	nbInst := "-"
	if out, err := exec.CommandContext(ctx, "netbird", "version").CombinedOutput(); err == nil {
		nbInst = strings.TrimSpace(string(out))
	}
	nbLatest := getCandidate("netbird")
	results = append(results, VersionCheck{
		Name:      "NetBird Mesh VPN",
		Installed: nbInst,
		Latest:    nbLatest,
		HasUpdate: nbLatest != "-" && nbInst != "-" && !strings.HasPrefix(nbLatest, nbInst),
	})

	// Output Table
	cb(fmt.Sprintf("%-20s %-18s %-18s %s", "KOMPONEN", "VERSI TERPASANG", "VERSI TERBARU", "STATUS"))
	cb(strings.Repeat("-", 75))
	for _, r := range results {
		status := "UP-TO-DATE"
		if r.Installed == "-" {
			status = "BELUM TERPASANG"
		} else if r.HasUpdate {
			status = "★ UPDATE TERSEDIA"
		}
		cb(fmt.Sprintf("%-20s %-18s %-18s %s", r.Name, r.Installed, r.Latest, status))
	}
	cb(strings.Repeat("-", 75))

	// OS Package Upgrade Simulation
	cb("\n--- PEMBARUAN PAKET SISTEM OPERASI ---")
	if inst.isDebian {
		cb("Memeriksa repositori paket APT (simulasi upgrade)...")
		out, err := exec.CommandContext(ctx, "apt-get", "-s", "upgrade").CombinedOutput()
		if err != nil {
			cb(fmt.Sprintf("[ERROR] Gagal memeriksa paket APT: %v", err))
		} else {
			lines := strings.Split(string(out), "\n")
			var upgradable []string
			summaryLine := ""
			instRe := regexp.MustCompile(`^Inst\s+(\S+)\s+\[([^\]]+)\]\s+\(([^\s\)]+)`)
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if m := instRe.FindStringSubmatch(l); len(m) >= 4 {
					upgradable = append(upgradable, fmt.Sprintf("  • %-26s : %s -> %s", m[1], m[2], m[3]))
				}
				if strings.Contains(l, "upgraded,") && strings.Contains(l, "newly installed") {
					summaryLine = l
				}
			}

			if len(upgradable) > 0 {
				cb(fmt.Sprintf("Ditemukan %d paket sistem yang memiliki pembaruan:", len(upgradable)))
				for _, up := range upgradable {
					cb(up)
				}
				if summaryLine != "" {
					cb("\nRingkasan: " + summaryLine)
				}
				cb("\n💡 Untuk memperbarui seluruh paket, klik tombol 'Update All' atau jalankan: wdev install update")
			} else {
				cb("✓ Seluruh paket sistem operasi sudah berada pada versi terbaru!")
			}
		}
	}

	cb("\n✓ Pemeriksaan ketersediaan pembaruan selesai.")
	return nil
}

// Self-Check
func (inst *Installer) RunSelfCheck(ctx context.Context, cb LogCallback) error {
	cb("=== VALIDASI & SELF-CHECK SISTEM ===")
	findBin := func(name string) string {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		for _, dir := range []string{"/usr/sbin", "/sbin", "/usr/local/sbin", "/usr/local/bin", "/usr/bin"} {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		return ""
	}

	checkBin := func(name string) {
		if path := findBin(name); path != "" {
			cb(fmt.Sprintf("  [OK] %-14s : %s", name, path))
		} else {
			cb(fmt.Sprintf("  [--] %-14s : Tidak terpasang", name))
		}
	}

	bins := []string{"git", "curl", "node", "npm", "go", "rustc", "cargo", "java", "psql", "redis-cli", "nginx", "php", "composer", "mkcert", "zsh", "cloudflared", "netbird"}
	for _, b := range bins {
		checkBin(b)
	}

	cb("\nMemeriksa konfigurasi Nginx...")
	nginxBin := findBin("nginx")
	if nginxBin != "" {
		cmd := exec.CommandContext(ctx, nginxBin, "-t")
		if os.Geteuid() != 0 {
			cmd = exec.CommandContext(ctx, "sudo", nginxBin, "-t")
		}
		if out, err := cmd.CombinedOutput(); err == nil {
			cb("  [OK] Nginx sintaks konfigurasi valid.")
		} else {
			cb(fmt.Sprintf("  [ERROR] Nginx sintaks gagal: %s", string(out)))
		}
	} else {
		cb("  [--] Nginx tidak terpasang.")
	}

	cb("\n✓ Self-check selesai.")
	return nil
}

func downloadFile(url, dstPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status HTTP tidak sukses: %d", resp.StatusCode)
	}

	out, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}
