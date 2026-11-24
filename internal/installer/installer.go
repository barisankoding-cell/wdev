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
	"syscall"
	"time"
	"wdev/internal/config"
	"wdev/internal/store"
)

type LogCallback = func(line string)

const sudoCacheTTL = 30 * time.Minute

type Installer struct {
	cfg        *config.Config
	store      *store.Store
	distro     string
	pkgMgr     string
	isDebian   bool
	sudoPass   string
	sudoPassAt time.Time
	mu         sync.Mutex
}

func (inst *Installer) SetSudoPassword(pass string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.sudoPass = pass
	if pass != "" {
		inst.sudoPassAt = time.Now()
	} else {
		inst.sudoPassAt = time.Time{}
	}
}

func (inst *Installer) ClearSudoPassword() {
	inst.SetSudoPassword("")
}

func (inst *Installer) GetSudoPassword() string {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.sudoPass == "" {
		return ""
	}
	if !inst.sudoPassAt.IsZero() && time.Since(inst.sudoPassAt) > sudoCacheTTL {
		inst.sudoPass = ""
		inst.sudoPassAt = time.Time{}
		return ""
	}
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
	base := filepath.Base(name)
	if strings.Contains(base, "postgresql") && strings.HasSuffix(base, "-setup") {
		return true
	}
	switch base {
	case "apt-get", "apt", "apt-cache", "dpkg", "dnf", "yum", "rpm", "pacman", "zypper", "systemctl", "nginx", "ufw", "firewall-cmd", "netfilter-persistent", "update-alternatives", "alternatives", "postgresql-setup", "pg_createcluster", "pg_dropcluster", "pg_ctlcluster", "restorecon", "systemd-tmpfiles", "ldconfig", "useradd", "certbot", "runuser", "su", "rustup", "rustup-init", "sdkmanager":
		return true
	case "git":
		for _, arg := range args {
			if arg == "--system" || strings.HasPrefix(arg, "/usr/local") || strings.Contains(arg, "=/usr/local") {
				return true
			}
		}
	case "flutter":
		if strings.HasPrefix(name, "/usr/local/flutter") {
			return true
		}
		for _, arg := range args {
			if arg == "upgrade" || arg == "precache" {
				return true
			}
		}
	case "sqlite3":
		for _, arg := range args {
			if strings.HasPrefix(arg, "/var") || strings.HasPrefix(arg, "/etc") {
				return true
			}
		}
	case "cp", "mv", "rm", "rmdir", "mkdir", "chmod", "chown", "tar", "ln", "tee", "unzip", "install":
		for _, arg := range args {
			if strings.HasPrefix(arg, "/usr/local") ||
				strings.HasPrefix(arg, "/usr/lib") ||
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

func (inst *Installer) RunCmd(ctx context.Context, cb LogCallback, name string, args ...string) error {
	return inst.runCmd(ctx, cb, name, args...)
}

func (inst *Installer) runCmd(ctx context.Context, cb LogCallback, name string, args ...string) error {
	return inst.runCmdStdin(ctx, cb, "", name, args...)
}

// sudoCommandSpec membungkus perintah toolchain dengan `env` saat dijalankan
// melalui sudo agar env_reset tidak membuang variabel lokasi kanonik wdev.
func sudoCommandSpec(name string, args []string) (string, []string) {
	base := filepath.Base(name)
	switch base {
	case "rustup", "rustup-init":
		wrapped := make([]string, 0, 3+len(args))
		wrapped = append(wrapped, "CARGO_HOME=/usr/local/cargo", "RUSTUP_HOME=/usr/local/rustup", name)
		wrapped = append(wrapped, args...)
		return "env", wrapped
	case "mkcert":
		wrapped := make([]string, 0, 2+len(args))
		wrapped = append(wrapped, "CAROOT=/etc/ssl/local-certs/ca", name)
		wrapped = append(wrapped, args...)
		return "env", wrapped
	case "flutter":
		wrapped := make([]string, 0, 3+len(args))
		wrapped = append(wrapped, "FLUTTER_ROOT=/usr/local/flutter", "CI=true", name)
		wrapped = append(wrapped, args...)
		return "env", wrapped
	}
	return name, args
}

// withEnvOverrides menyetel atau menimpa variabel environment tanpa duplikasi kunci.
func withEnvOverrides(base []string, overrides [][2]string) []string {
	overrideMap := make(map[string]string, len(overrides))
	for _, kv := range overrides {
		overrideMap[kv[0]] = kv[1]
	}
	out := make([]string, 0, len(base)+len(overrides))
	seen := make(map[string]bool, len(overrides))
	for _, entry := range base {
		if k, _, ok := strings.Cut(entry, "="); ok {
			if val, has := overrideMap[k]; has {
				if !seen[k] {
					out = append(out, k+"="+val)
					seen[k] = true
				}
				continue
			}
		}
		out = append(out, entry)
	}
	for _, kv := range overrides {
		if !seen[kv[0]] {
			out = append(out, kv[0]+"="+kv[1])
			seen[kv[0]] = true
		}
	}
	return out
}

// runCmdStdin menjalankan perintah dengan stdin opsional (tanpa shell).
func (inst *Installer) runCmdStdin(ctx context.Context, cb LogCallback, stdinData, name string, args ...string) error {
	useSudo := needsSudo(name, args...)
	cmdStr := fmt.Sprintf("%s %s", name, strings.Join(args, " "))
	if useSudo {
		cmdStr = "[sudo] " + cmdStr
	}
	cb(fmt.Sprintf("==> Menjalankan: %s", cmdStr))

	var cmd *exec.Cmd
	var stdinPipe io.WriteCloser

	if useSudo {
		effName, effArgs := sudoCommandSpec(name, args)
		sudoPass := inst.GetSudoPassword()
		if sudoPass != "" {
			sudoArgs := append([]string{"-S", "-p", "", effName}, effArgs...)
			cmd = exec.CommandContext(ctx, "sudo", sudoArgs...)
			var err error
			stdinPipe, err = cmd.StdinPipe()
			if err != nil {
				return err
			}
		} else {
			sudoArgs := append([]string{"-n", effName}, effArgs...)
			cmd = exec.CommandContext(ctx, "sudo", sudoArgs...)
		}
	} else {
		cmd = exec.CommandContext(ctx, name, args...)
	}

	if stdinData != "" && stdinPipe == nil {
		cmd.Stdin = strings.NewReader(stdinData)
	}
	baseCmd := filepath.Base(name)
	if baseCmd == "runuser" || baseCmd == "su" {
		cmd.Dir = "/"
	}

	cmd.Env = withEnvOverrides(os.Environ(), [][2]string{
		{"DEBIAN_FRONTEND", "noninteractive"},
		{"COMPOSER_ALLOW_SUPERUSER", "1"},
		{"COMPOSER_NO_INTERACTION", "1"},
		{"NEEDRESTART_MODE", "a"},
		{"CARGO_HOME", "/usr/local/cargo"},
		{"RUSTUP_HOME", "/usr/local/rustup"},
		{"CAROOT", "/etc/ssl/local-certs/ca"},
		{"FLUTTER_ROOT", "/usr/local/flutter"},
		{"CI", "true"},
	})

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
			if stdinData != "" {
				_, _ = io.WriteString(stdinPipe, stdinData)
			}
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
	var err error
	if inst.isDebian {
		if err = inst.runCmd(ctx, cb, "apt-get", "update", "-y"); err != nil {
			return err
		}
		pkgs := []string{
			"install", "-y", "build-essential", "curl", "wget", "git", "jq", "unzip",
			"tar", "xz-utils", "pkg-config", "libssl-dev", "cmake", "clang",
			"ca-certificates", "gnupg",
		}
		err = inst.runCmd(ctx, cb, "apt-get", pkgs...)
	} else {
		err = inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", "gcc", "gcc-c++", "make", "curl", "wget", "git", "jq", "unzip", "tar", "xz", "cmake", "clang", "openssl-devel")
	}
	if err == nil {
		inst.ensureWdevProfileEnv(ctx, cb)
	}
	return err
}

func needsRootPermFix(dir string) bool {
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return false
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Uid != 0 {
		return true
	}
	perm := fi.Mode().Perm()
	return (perm&0o022) != 0 || (perm&0o005) != 0o005
}

// 2. Node.js (Multi-version)
func (inst *Installer) InstallNode(ctx context.Context, cb LogCallback, ver string) error {
	if ver == "" {
		ver = "22"
	}
	cb(fmt.Sprintf("--- [2] Memasang Node.js v%s ---", ver))
	inst.GuardToolchainColumns(ctx, cb, "node", "npm", "npx", "corepack")

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

	nodeBin := filepath.Join(targetDir, "bin", "node")
	if _, err := os.Stat(nodeBin); err == nil {
		cb(fmt.Sprintf("Node.js v%s sudah terpasang di %s, melewati unduhan.", fullVer, targetDir))
		if needsRootPermFix(targetDir) {
			_ = inst.runCmd(ctx, cb, "chown", "-R", "root:root", targetDir)
			_ = inst.runCmd(ctx, cb, "chmod", "-R", "go-w,a+rX", targetDir)
		}
	} else {
		cb(fmt.Sprintf("Mengunduh Node.js dari: %s", url))
		tmpDir, err := os.MkdirTemp("", "wdev-node-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)
		tmpTar := filepath.Join(tmpDir, tarName)

		if err := downloadFile(url, tmpTar); err != nil {
			return fmt.Errorf("gagal mengunduh Node.js: %w", err)
		}

		_ = inst.runCmd(ctx, cb, "mkdir", "-p", nodeBaseDir)
		_ = inst.runCmd(ctx, cb, "rm", "-rf", targetDir)

		cb("Mengekstrak berkas Node.js...")
		if err := inst.runCmd(ctx, cb, "tar", "--no-same-owner", "-xJf", tmpTar, "-C", nodeBaseDir); err != nil {
			return err
		}
		_ = inst.runCmd(ctx, cb, "chmod", "-R", "go-w,a+rX", targetDir)
	}

	// Create canonical current symlink & /usr/local/bin symlinks
	_ = inst.runCmd(ctx, cb, "ln", "-sfn", targetDir, filepath.Join(nodeBaseDir, "current"))
	bins := []string{"node", "npm", "npx", "corepack"}
	for _, b := range bins {
		src := filepath.Join(targetDir, "bin", b)
		dst := filepath.Join("/usr/local/bin", b)
		_ = inst.runCmd(ctx, cb, "rm", "-f", dst)
		_ = inst.runCmd(ctx, cb, "ln", "-sfn", src, dst)
	}

	// Hapus paket Node.js bawaan distro bila ada agar tidak terjadi duplikasi
	inst.removeDistroPackagesIfInstalled(ctx, cb, "nodejs", "npm", "nodejs-npm")
	inst.ensureWdevProfileEnv(ctx, cb)

	cb(fmt.Sprintf("✓ Node.js v%s berhasil dipasang di /usr/local/bin/node", fullVer))
	return nil
}

// 3. Rust, Mold Linker & Sccache
func (inst *Installer) InstallRust(ctx context.Context, cb LogCallback) error {
	cb("--- [3] Memasang Rust Toolchain, Mold Linker & Sccache ---")
	inst.GuardToolchainColumns(ctx, cb, "rustc", "cargo", "rustup", "mold")

	_ = inst.runCmd(ctx, cb, "mkdir", "-p", "/usr/local/cargo", "/usr/local/rustup")
	_ = inst.runCmd(ctx, cb, "chmod", "755", "/usr/local/cargo", "/usr/local/rustup")

	// 1. Rustup di lokasi kanonik /usr/local/cargo & /usr/local/rustup
	rustupBin := "/usr/local/cargo/bin/rustup"
	if _, err := os.Stat(rustupBin); os.IsNotExist(err) {
		cb("Memasang Rust via rustup-init resmi ke /usr/local/cargo...")
		target := "x86_64-unknown-linux-gnu"
		if runtime.GOARCH == "arm64" {
			target = "aarch64-unknown-linux-gnu"
		}
		tmpDir, err := os.MkdirTemp("", "wdev-rustup-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)
		rustupInit := filepath.Join(tmpDir, "rustup-init")
		url := fmt.Sprintf("https://static.rust-lang.org/rustup/dist/%s/rustup-init", target)
		if err := downloadFile(url, rustupInit); err != nil {
			return err
		}
		if err := os.Chmod(rustupInit, 0700); err != nil {
			return err
		}
		if err := inst.runCmd(ctx, cb, rustupInit, "-y", "--no-modify-path", "--default-toolchain", "stable"); err != nil {
			return err
		}
	} else {
		cb("Rust sudah terpasang di /usr/local/cargo, memperbarui...")
		_ = inst.runCmd(ctx, cb, rustupBin, "update", "stable")
	}
	// Pastikan toolchain bersama dapat dibaca/dieksekusi user non-root tanpa membuka izin tulis global.
	_ = inst.runCmd(ctx, cb, "chmod", "-R", "go-w,a+rX", "/usr/local/cargo", "/usr/local/rustup")

	// 2. Mold Linker (Rilis Resmi GitHub rui314/mold)
	cb("Memasang Mold linker dari rilis resmi...")
	if err := inst.installMoldOfficial(ctx, cb); err != nil {
		cb(fmt.Sprintf("[WARN] Gagal memasang Mold resmi (%v), mencoba via repo sistem...", err))
		if inst.isDebian {
			_ = inst.runCmd(ctx, cb, "apt-get", "install", "-y", "mold")
		} else {
			_ = inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", "mold")
		}
	}

	// Symlink binary /usr/local/cargo/bin ke /usr/local/bin
	cargoBin := "/usr/local/cargo/bin"
	if entries, err := os.ReadDir(cargoBin); err == nil {
		for _, e := range entries {
			src := filepath.Join(cargoBin, e.Name())
			dst := filepath.Join("/usr/local/bin", e.Name())
			_ = inst.runCmd(ctx, cb, "rm", "-f", dst)
			_ = inst.runCmd(ctx, cb, "ln", "-sfn", src, dst)
		}
	}

	// Hapus paket Rust/Cargo bawaan distro bila ada agar tidak duplikat
	inst.removeDistroPackagesIfInstalled(ctx, cb, "rustc", "cargo", "rust")
	inst.ensureWdevProfileEnv(ctx, cb)

	cb("✓ Rust Toolchain & Mold siap digunakan.")
	return nil
}

// 4. Golang
func (inst *Installer) InstallGolang(ctx context.Context, cb LogCallback) error {
	cb("--- [4] Memasang Golang ---")
	inst.GuardToolchainColumns(ctx, cb, "go", "gofmt")
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

	alreadyLatest := false
	if out, err := exec.CommandContext(ctx, "/usr/local/go/bin/go", "version").CombinedOutput(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 3 && fields[2] == "go"+latestVer {
			alreadyLatest = true
		}
	}

	if alreadyLatest {
		cb(fmt.Sprintf("Go v%s sudah merupakan versi terbaru di /usr/local/go, melewati unduhan.", latestVer))
	} else {
		tarName := fmt.Sprintf("go%s.linux-%s.tar.gz", latestVer, arch)
		url := fmt.Sprintf("https://go.dev/dl/%s", tarName)
		tmpDir, err := os.MkdirTemp("", "wdev-go-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)
		tmpTar := filepath.Join(tmpDir, tarName)

		cb(fmt.Sprintf("Mengunduh Go v%s dari: %s", latestVer, url))
		if err := downloadFile(url, tmpTar); err != nil {
			return fmt.Errorf("gagal mengunduh Go: %w", err)
		}

		cb("Mengekstrak ke /usr/local/go...")
		_ = inst.runCmd(ctx, cb, "rm", "-rf", "/usr/local/go")
		if err := inst.runCmd(ctx, cb, "tar", "--no-same-owner", "-xzf", tmpTar, "-C", "/usr/local"); err != nil {
			return err
		}
		_ = inst.runCmd(ctx, cb, "chmod", "-R", "go-w,a+rX", "/usr/local/go")
	}

	// Symlink go & gofmt
	_ = inst.runCmd(ctx, cb, "rm", "-f", "/usr/local/bin/go", "/usr/local/bin/gofmt")
	_ = inst.runCmd(ctx, cb, "ln", "-sfn", "/usr/local/go/bin/go", "/usr/local/bin/go")
	_ = inst.runCmd(ctx, cb, "ln", "-sfn", "/usr/local/go/bin/gofmt", "/usr/local/bin/gofmt")

	inst.removeDistroPackagesIfInstalled(ctx, cb, "golang", "golang-go", "golang-bin")
	inst.ensureWdevProfileEnv(ctx, cb)

	cb(fmt.Sprintf("✓ Go v%s berhasil dipasang di /usr/local/bin/go", latestVer))
	return nil
}

// 5. OpenJDK Java (Official Eclipse Adoptium Temurin)
func (inst *Installer) InstallJava(ctx context.Context, cb LogCallback, ver string) error {
	if ver == "" {
		ver = "21"
	}
	cb(fmt.Sprintf("--- [5] Memasang OpenJDK Java %s (Eclipse Adoptium Temurin) ---", ver))

	dlURL, jdkVer, err := fetchAdoptiumLatest(ctx, ver)
	if err != nil {
		return fmt.Errorf("gagal mengambil rilis resmi OpenJDK %s dari Adoptium: %w", ver, err)
	}

	targetDir := fmt.Sprintf("/usr/lib/jvm/java-%s-temurin", ver)
	javaBin := filepath.Join(targetDir, "bin", "java")
	alreadyLatest := false
	if out, err := exec.CommandContext(ctx, javaBin, "-version").CombinedOutput(); err == nil {
		firstLine := strings.Split(string(out), "\n")[0]
		if start := strings.Index(firstLine, "\""); start != -1 {
			if end := strings.LastIndex(firstLine, "\""); end > start {
				installedVer := firstLine[start+1 : end]
				if !hasNewerVersion(installedVer, jdkVer) {
					alreadyLatest = true
				}
			}
		}
	}

	if alreadyLatest {
		cb(fmt.Sprintf("OpenJDK %s (Temurin %s) sudah versi terbaru di %s, melewati unduhan.", ver, jdkVer, targetDir))
	} else {
		tarName := fmt.Sprintf("temurin-jdk-%s-%s.tar.gz", ver, adoptiumArch())
		tmpDir, err := os.MkdirTemp("", "wdev-jdk-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)
		tmpTar := filepath.Join(tmpDir, tarName)
		cb(fmt.Sprintf("Mengunduh Temurin JDK %s (%s) dari: %s", ver, jdkVer, dlURL))
		if err := downloadFile(dlURL, tmpTar); err != nil {
			return fmt.Errorf("gagal mengunduh JDK %s: %w", ver, err)
		}

		_ = inst.runCmd(ctx, cb, "mkdir", "-p", "/usr/lib/jvm")
		_ = inst.runCmd(ctx, cb, "rm", "-rf", targetDir)
		_ = inst.runCmd(ctx, cb, "mkdir", "-p", targetDir)

		cb(fmt.Sprintf("Mengekstrak JDK ke %s...", targetDir))
		if err := inst.runCmd(ctx, cb, "tar", "-xzf", tmpTar, "-C", targetDir, "--strip-components=1"); err != nil {
			return fmt.Errorf("gagal mengekstrak JDK: %w", err)
		}
	}

	altCmd := findAlternativesCmd()
	javacBin := filepath.Join(targetDir, "bin", "javac")
	jarBin := filepath.Join(targetDir, "bin", "jar")

	if altCmd != "" {
		prio := "11" + ver
		if _, err := os.Stat(javaBin); err == nil {
			_ = inst.runCmd(ctx, cb, altCmd, "--install", "/usr/bin/java", "java", javaBin, prio)
			_ = inst.runCmd(ctx, cb, altCmd, "--set", "java", javaBin)
		}
		if _, err := os.Stat(javacBin); err == nil {
			_ = inst.runCmd(ctx, cb, altCmd, "--install", "/usr/bin/javac", "javac", javacBin, prio)
			_ = inst.runCmd(ctx, cb, altCmd, "--set", "javac", javacBin)
		}
		if _, err := os.Stat(jarBin); err == nil {
			_ = inst.runCmd(ctx, cb, altCmd, "--install", "/usr/bin/jar", "jar", jarBin, prio)
			_ = inst.runCmd(ctx, cb, altCmd, "--set", "jar", jarBin)
		}
		// Hapus symlink duplikat di /usr/local/bin agar alternatives /usr/bin/java tetap aktif
		_ = inst.runCmd(ctx, cb, "rm", "-f", "/usr/local/bin/java", "/usr/local/bin/javac", "/usr/local/bin/jar")
	} else {
		for _, bin := range []string{"java", "javac", "jar"} {
			src := filepath.Join(targetDir, "bin", bin)
			if _, err := os.Stat(src); err == nil {
				_ = inst.runCmd(ctx, cb, "ln", "-sfn", src, filepath.Join("/usr/local/bin", bin))
			}
		}
	}

	// Hapus paket OpenJDK distro duplikat untuk versi yang sama agar tidak double di /usr/lib/jvm
	inst.removeDistroPackagesIfInstalled(ctx, cb,
		fmt.Sprintf("openjdk-%s-jdk", ver),
		fmt.Sprintf("openjdk-%s-jre", ver),
		fmt.Sprintf("openjdk-%s-jdk-headless", ver),
		fmt.Sprintf("java-%s-openjdk", ver),
		fmt.Sprintf("java-%s-openjdk-devel", ver),
		fmt.Sprintf("java-%s-openjdk-headless", ver),
	)

	cb(fmt.Sprintf("✓ OpenJDK %s (Temurin %s) berhasil dipasang di %s", ver, jdkVer, targetDir))
	return nil
}

func (inst *Installer) installMoldOfficial(ctx context.Context, cb LogCallback) error {
	tag, assets, err := fetchGitHubLatestRelease(ctx, "rui314/mold")
	if err != nil {
		return err
	}

	archSuffix := "x86_64-linux.tar.gz"
	if runtime.GOARCH == "arm64" {
		archSuffix = "aarch64-linux.tar.gz"
	}

	var dlURL string
	for _, a := range assets {
		if strings.HasPrefix(a.Name, "mold-") && strings.HasSuffix(a.Name, archSuffix) {
			dlURL = a.URL
			break
		}
	}
	if dlURL == "" {
		return fmt.Errorf("aset rilis mold untuk %s tidak ditemukan", archSuffix)
	}

	moldDir := "/usr/local/mold"
	moldBin := filepath.Join(moldDir, "bin", "mold")
	alreadyLatest := false
	if out, err := exec.CommandContext(ctx, moldBin, "--version").CombinedOutput(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 2 && !hasNewerVersion(fields[1], strings.TrimPrefix(tag, "v")) {
			alreadyLatest = true
		}
	}

	if alreadyLatest {
		cb(fmt.Sprintf("Mold %s sudah terpasang di %s, melewati unduhan.", tag, moldDir))
	} else {
		tmpDir, err := os.MkdirTemp("", "wdev-mold-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)
		tmpTar := filepath.Join(tmpDir, fmt.Sprintf("mold-%s-%s", tag, archSuffix))
		cb(fmt.Sprintf("Mengunduh Mold %s dari: %s", tag, dlURL))
		if err := downloadFile(dlURL, tmpTar); err != nil {
			return err
		}

		_ = inst.runCmd(ctx, cb, "rm", "-rf", moldDir)
		_ = inst.runCmd(ctx, cb, "mkdir", "-p", moldDir)
		if err := inst.runCmd(ctx, cb, "tar", "--no-same-owner", "-xzf", tmpTar, "-C", moldDir, "--strip-components=1"); err != nil {
			return err
		}
		_ = inst.runCmd(ctx, cb, "chmod", "-R", "go-w,a+rX", moldDir)
	}

	for _, b := range []string{"mold", "ld.mold"} {
		src := filepath.Join(moldDir, "bin", b)
		if _, err := os.Stat(src); err == nil {
			dst := filepath.Join("/usr/local/bin", b)
			_ = inst.runCmd(ctx, cb, "rm", "-f", dst)
			_ = inst.runCmd(ctx, cb, "ln", "-sfn", src, dst)
		}
	}

	inst.removeDistroPackagesIfInstalled(ctx, cb, "mold")

	cb(fmt.Sprintf("✓ Mold %s berhasil dipasang di /usr/local/bin/mold", tag))
	return nil
}

// 6. Android SDK
func (inst *Installer) InstallAndroidSDK(ctx context.Context, cb LogCallback) error {
	cb("--- [6] Memasang Android SDK & CLI Tools ---")
	sdkDir := "/usr/local/android-sdk"
	_ = inst.runCmd(ctx, cb, "mkdir", "-p", sdkDir)

	cmdlineDir := filepath.Join(sdkDir, "cmdline-tools")
	latestDir := filepath.Join(cmdlineDir, "latest")
	sdkManager := filepath.Join(latestDir, "bin", "sdkmanager")

	if _, err := os.Stat(sdkManager); err == nil {
		cb("Android Command Line Tools sudah terpasang, memperbarui komponen SDK...")
		_ = inst.runCmdStdin(ctx, cb, strings.Repeat("y\n", 50), sdkManager, "--sdk_root="+sdkDir, "--licenses")
		_ = inst.runCmd(ctx, cb, sdkManager, fmt.Sprintf("--sdk_root=%s", sdkDir), "--update")
		_ = inst.runCmd(ctx, cb, sdkManager, fmt.Sprintf("--sdk_root=%s", sdkDir), "platform-tools", "build-tools;34.0.0", "build-tools;35.0.0", "platforms;android-34", "platforms;android-35")
	} else {
		zipName := "commandlinetools-linux-11076708_latest.zip"
		url := "https://dl.google.com/android/repository/" + zipName
		tmpDir, err := os.MkdirTemp("", "wdev-android-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)
		tmpZip := filepath.Join(tmpDir, zipName)

		cb("Mengunduh Android Command Line Tools...")
		if err := downloadFile(url, tmpZip); err != nil {
			return err
		}

		_ = inst.runCmd(ctx, cb, "mkdir", "-p", cmdlineDir)
		if err := inst.runCmd(ctx, cb, "unzip", "-qo", tmpZip, "-d", cmdlineDir); err != nil {
			return err
		}

		// Google requires cmdline-tools/latest structure
		extractedDir := filepath.Join(cmdlineDir, "cmdline-tools")
		if _, err := os.Stat(extractedDir); err == nil {
			_ = inst.runCmd(ctx, cb, "rm", "-rf", latestDir)
			_ = inst.runCmd(ctx, cb, "mv", extractedDir, latestDir)
		}

		cb("Menerima lisensi Android SDK...")
		if _, err := os.Stat(sdkManager); err == nil {
			_ = inst.runCmdStdin(ctx, cb, strings.Repeat("y\n", 50), sdkManager, "--sdk_root="+sdkDir, "--licenses")
			_ = inst.runCmd(ctx, cb, sdkManager, fmt.Sprintf("--sdk_root=%s", sdkDir), "platform-tools", "build-tools;34.0.0", "build-tools;35.0.0", "platforms;android-34", "platforms;android-35")
		}
	}

	_ = inst.runCmd(ctx, cb, "chmod", "-R", "go-w,a+rX", sdkDir)
	inst.ensureWdevProfileEnv(ctx, cb)
	cb("✓ Android SDK berhasil dipasang di " + sdkDir)
	return nil
}

func isGitSystemSafeDirectory(ctx context.Context, dir string) bool {
	out, err := exec.CommandContext(ctx, "git", "config", "--system", "--get-all", "safe.directory").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == dir {
			return true
		}
	}
	return false
}

func (inst *Installer) ensureGitSafeDirectory(ctx context.Context, cb LogCallback, dir string) {
	if isGitSystemSafeDirectory(ctx, dir) {
		return
	}
	_ = inst.runCmd(ctx, cb, "git", "config", "--system", "--add", "safe.directory", dir)
}

// 7. Flutter SDK
func (inst *Installer) InstallFlutter(ctx context.Context, cb LogCallback) error {
	cb("--- [7] Memasang Flutter SDK ---")
	inst.GuardToolchainColumns(ctx, cb, "flutter", "dart")
	flutterDir := "/usr/local/flutter"

	if _, err := os.Stat(flutterDir); os.IsNotExist(err) {
		cb("Cloning repository Flutter channel stable...")
		if err := inst.runCmd(ctx, cb, "git", "clone", "-b", "stable", "https://github.com/flutter/flutter.git", flutterDir); err != nil {
			return err
		}
	} else {
		cb("Memperbarui Flutter repo...")
		_ = inst.runCmd(ctx, cb, "git", "-c", "safe.directory="+flutterDir, "-C", flutterDir, "pull")
	}

	inst.ensureGitSafeDirectory(ctx, cb, flutterDir)
	flutterBin := filepath.Join(flutterDir, "bin", "flutter")
	cb("Menyiapkan cache Dart SDK & artefak Flutter...")
	_ = inst.runCmd(ctx, cb, flutterBin, "--suppress-analytics", "--version")
	_ = inst.runCmd(ctx, cb, flutterBin, "--suppress-analytics", "precache")
	_ = inst.runCmd(ctx, cb, "chmod", "-R", "go-w,a+rX", flutterDir)

	// Symlink flutter & dart
	_ = inst.runCmd(ctx, cb, "rm", "-f", "/usr/local/bin/flutter", "/usr/local/bin/dart")
	_ = inst.runCmd(ctx, cb, "ln", "-sfn", flutterBin, "/usr/local/bin/flutter")
	_ = inst.runCmd(ctx, cb, "ln", "-sfn", filepath.Join(flutterDir, "bin", "dart"), "/usr/local/bin/dart")

	inst.ensureWdevProfileEnv(ctx, cb)
	cb("✓ Flutter SDK berhasil dipasang.")
	return nil
}

// 8. PostgreSQL (Official PGDG Repo + Idempotent Init & Start)
func (inst *Installer) InstallPostgres(ctx context.Context, cb LogCallback, ver string) error {
	cb("--- [8] Memasang PostgreSQL Server ---")
	_ = inst.ensurePGDGRepo(ctx, cb)
	if ver == "" || ver == "system" {
		ver = "16"
	}
	if !IsValidPGMajor(ver) {
		return fmt.Errorf("versi PostgreSQL tidak valid: %q", ver)
	}

	if inst.isDebian {
		pkg := fmt.Sprintf("postgresql-%s", ver)
		if err := inst.runCmd(ctx, cb, "apt-get", "install", "-y", pkg); err != nil {
			return err
		}
		if err := inst.runCmd(ctx, cb, "systemctl", "enable", "--now", "postgresql"); err != nil {
			return fmt.Errorf("gagal mengaktifkan service postgresql: %w", err)
		}
		// Pastikan cluster versi target terbuat & berjalan
		if out, err := exec.CommandContext(ctx, "pg_lsclusters", "-h").CombinedOutput(); err == nil {
			hasVer := false
			isOnline := false
			activePort := 0
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 4 && fields[0] == ver {
					hasVer = true
					if fields[2] == "5432" {
						activePort = 5432
					}
					if fields[3] == "online" {
						isOnline = true
					}
					break
				}
			}
			if !hasVer {
				_ = inst.runCmd(ctx, cb, "pg_createcluster", ver, "main", "--start")
			} else if !isOnline {
				_ = inst.runCmd(ctx, cb, "pg_ctlcluster", ver, "main", "start")
			}
			if activePort == 5432 || inst.readPGActiveState() == "" {
				inst.writePGActiveState(ctx, cb, ver)
			}
		}
	} else {
		// Jika di sistem RPM sudah ada cluster PostgreSQL versi mayor lain yang aktif/terinisialisasi,
		// alihkan melalui SwitchPostgres agar seluruh database dimigrasi aman dan port 5432 tidak bentrok.
		clusters := inst.listPGClusters(ctx, pgPlatformRPM)
		stateMajor := inst.readPGActiveState()
		if src := pickSourcePGCluster(clusters, ver, stateMajor, func(unit string) bool {
			return exec.CommandContext(ctx, "systemctl", "is-enabled", "--quiet", unit).Run() == nil
		}); src != nil {
			cb(fmt.Sprintf("[INFO] Mendeteksi cluster PostgreSQL %s yang sudah ada (%s); memigrasikan database secara aman ke PostgreSQL %s...", src.Major, src.Unit, ver))
			return inst.SwitchPostgres(ctx, cb, ver)
		}

		pkgServer := fmt.Sprintf("postgresql%s-server", ver)
		pkgClient := fmt.Sprintf("postgresql%s", ver)
		wasInstalled := rpmIsInstalled(ctx, pkgServer)

		// Bila saat ini terpasang paket bawaan distro Fedora (unit postgresql.service)
		// sementara repo PGDG menyediakan versi mayor yang sama, hentikan postgresql.service
		// sebelum `dnf upgrade` agar WAL bersih dan port 5432 bebas.
		migratingPG := inst.preparePostgresBeforeRPMUpgrade(ctx, cb, ver)

		if err := inst.installOrUpgradeRPM(ctx, cb, pkgServer, pkgClient); err != nil {
			if wasInstalled {
				if migratingPG != "" {
					_ = inst.runCmd(ctx, cb, "systemctl", "enable", "postgresql")
					_ = inst.runCmd(ctx, cb, "systemctl", "start", "postgresql")
				}
				return fmt.Errorf("gagal memperbarui %s: %w", pkgServer, err)
			}
			if hasPGDGRepoForMajor(ctx, ver) {
				return fmt.Errorf("gagal memasang %s dari repositori PGDG: %w", pkgServer, err)
			}
			cb(fmt.Sprintf("[WARN] Paket %s gagal (%v), mencoba postgresql-server...", pkgServer, err))
			if errFallback := inst.installOrUpgradeRPM(ctx, cb, "postgresql-server", "postgresql"); errFallback != nil {
				return errFallback
			}
		}

		_ = inst.runCmd(ctx, cb, "systemctl", "daemon-reload")
		if err := inst.finalizeRPMPostgresService(ctx, cb, ver); err != nil {
			return err
		}
	}

	cb("✓ PostgreSQL berhasil dipasang dan diaktifkan.")
	return nil
}

// 9. Redis / Valkey (Official packages.redis.io Repo)
func (inst *Installer) InstallRedis(ctx context.Context, cb LogCallback) error {
	cb("--- [9] Memasang Redis Cache Server ---")
	redisRepoActive := inst.ensureRedisRepo(ctx, cb)

	var svc string
	if inst.isDebian {
		pkg := "redis-server"
		if redisRepoActive {
			pkg = "redis"
		}
		if err := inst.runCmd(ctx, cb, "apt-get", "install", "-y", pkg); err != nil {
			if pkg != "redis-server" {
				if errFallback := inst.runCmd(ctx, cb, "apt-get", "install", "-y", "redis-server"); errFallback != nil {
					return errFallback
				}
			} else {
				return err
			}
		}
		svc = "redis-server"
		if _, err := os.Stat("/lib/systemd/system/valkey-server.service"); err == nil {
			if _, rErr := os.Stat("/lib/systemd/system/redis-server.service"); os.IsNotExist(rErr) {
				svc = "valkey-server"
			}
		}
	} else {
		if err := inst.installOrUpgradeRPM(ctx, cb, "redis"); err != nil {
			return err
		}
		svc = "redis"
		if _, err := os.Stat("/usr/lib/systemd/system/valkey.service"); err == nil {
			svc = "valkey"
		}
	}

	// Unit systemd Redis memakai Type=notify: daemonize harus "no" dan
	// supervised "systemd". Bila tidak, proses fork dan systemd menandai
	// kegagalan "protocol" sehingga instalasi berhenti.
	conf := findRedisConf()
	if err := inst.ensureRedisConf(ctx, cb, conf); err != nil {
		return err
	}

	inst.freeRedisPort(ctx, cb, svc)

	if err := inst.runCmd(ctx, cb, "systemctl", "enable", svc); err != nil {
		return fmt.Errorf("gagal mengaktifkan service %s: %w", svc, err)
	}
	_ = inst.runCmd(ctx, cb, "systemd-tmpfiles", "--create")
	if err := inst.runCmd(ctx, cb, "systemctl", "restart", svc); err != nil {
		inst.reportRedisFailure(ctx, cb, svc, conf)
		return fmt.Errorf("gagal menjalankan service %s: %w", svc, err)
	}
	if !inst.waitRedisReady(ctx) {
		inst.reportRedisFailure(ctx, cb, svc, conf)
		return fmt.Errorf("service %s aktif tetapi tidak merespons PING dalam 15 detik", svc)
	}

	cb("✓ Redis Server berhasil dipasang dan diaktifkan.")
	return nil
}

// waitRedisReady menunggu hingga Redis membalas PONG.
func (inst *Installer) waitRedisReady(ctx context.Context) bool {
	for i := 0; i < 15; i++ {
		if redisPing(ctx) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
	return redisPing(ctx)
}

// reportRedisFailure menampilkan diagnostik singkat saat Redis gagal start.
func (inst *Installer) reportRedisFailure(ctx context.Context, cb LogCallback, svc, conf string) {
	cb("[ERROR] Redis gagal dijalankan. Diagnostik:")
	if out, err := exec.CommandContext(ctx, "systemctl", "status", svc, "--no-pager", "-l").CombinedOutput(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			cb("  " + line)
		}
	}
	if out, err := exec.CommandContext(ctx, "journalctl", "-u", svc, "-n", "20", "--no-pager").CombinedOutput(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			cb("  " + line)
		}
	}
	if conf != "" {
		cb("  Konfigurasi: " + conf + " (harus daemonize no, supervised systemd)")
	}
}

func aptSafeConffileArgs(subcmd string, extra ...string) []string {
	args := []string{
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
		subcmd,
		"-y",
	}
	return append(args, extra...)
}

func (inst *Installer) validateNginxConfig(ctx context.Context, cb LogCallback) error {
	nginxBin := findTool("nginx")
	if nginxBin == "" {
		return nil
	}
	return inst.runCmd(ctx, cb, nginxBin, "-t")
}

func (inst *Installer) reconcileNginxAfterUpdate(ctx context.Context, cb LogCallback, backup []byte, restart bool) error {
	if findTool("nginx") == "" {
		return nil
	}
	inst.ensureNginxCompatibility(ctx, cb)
	if err := inst.FixNginxCachePerm(ctx, cb); err != nil {
		cb(fmt.Sprintf("[WARN] Perbaikan permission cache Nginx: %v", err))
	}
	if err := inst.validateNginxConfig(ctx, cb); err != nil {
		if len(backup) > 0 {
			cb("[WARN] Uji konfigurasi Nginx gagal pasca-upgrade; memulihkan backup /etc/nginx/nginx.conf...")
			_ = inst.writeSystemFile(ctx, cb, "/etc/nginx/nginx.conf", backup, "644")
			inst.ensureNginxCompatibility(ctx, cb)
			if retryErr := inst.validateNginxConfig(ctx, cb); retryErr != nil {
				return fmt.Errorf("konfigurasi Nginx tidak valid pasca-upgrade: %w", retryErr)
			}
		} else {
			return fmt.Errorf("konfigurasi Nginx tidak valid pasca-upgrade: %w", err)
		}
	}
	if restart {
		if err := inst.runCmd(ctx, cb, "systemctl", "enable", "nginx"); err != nil {
			return fmt.Errorf("gagal mengaktifkan service nginx: %w", err)
		}
		if err := inst.runCmd(ctx, cb, "systemctl", "restart", "nginx"); err != nil {
			return fmt.Errorf("gagal menjalankan ulang service nginx: %w", err)
		}
	} else if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "nginx").Run() == nil {
		if err := inst.runCmd(ctx, cb, "systemctl", "reload", "nginx"); err != nil {
			cb(fmt.Sprintf("[WARN] Reload Nginx pasca-upgrade: %v", err))
		}
	}
	return nil
}

// 10. Nginx Mainline (Official nginx.org Repo)
func (inst *Installer) InstallNginx(ctx context.Context, cb LogCallback) error {
	cb("--- [10] Memasang Nginx Web Server ---")
	_ = inst.ensureNginxRepo(ctx, cb)
	nginxBackup, _ := os.ReadFile("/etc/nginx/nginx.conf")

	if inst.isDebian {
		if err := inst.runCmd(ctx, cb, "apt-get", aptSafeConffileArgs("install", "nginx")...); err != nil {
			return err
		}
	} else {
		if err := inst.installOrUpgradeRPM(ctx, cb, "nginx"); err != nil {
			// Jika repo nginx.repo bermasalah (mis. 404), bersihkan lalu coba ulang via repo distro
			if _, statErr := os.Stat("/etc/yum.repos.d/nginx.repo"); statErr == nil {
				cb("[WARN] Instalasi via nginx.repo gagal, membersihkan /etc/yum.repos.d/nginx.repo dan mencoba ulang via repo sistem...")
				_ = inst.runCmd(ctx, cb, "rm", "-f", "/etc/yum.repos.d/nginx.repo")
				if retryErr := inst.installOrUpgradeRPM(ctx, cb, "nginx"); retryErr != nil {
					return retryErr
				}
			} else {
				return err
			}
		}
	}

	if err := inst.reconcileNginxAfterUpdate(ctx, cb, nginxBackup, true); err != nil {
		return err
	}
	cb("✓ Nginx Web Server berhasil dipasang dan diaktifkan.")
	return nil
}

// 11. PHP & PHP-FPM (Official Sury/Ondřej/Remi Repo)
func (inst *Installer) InstallPHP(ctx context.Context, cb LogCallback, ver string) error {
	if ver == "" {
		ver = "8.4"
	}
	cb(fmt.Sprintf("--- [11] Memasang PHP & PHP-FPM %s ---", ver))
	remiActive := inst.ensurePHPRepo(ctx, cb, ver)

	if inst.isDebian {
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
		fpmSvc := fmt.Sprintf("php%s-fpm", ver)
		if err := inst.runCmd(ctx, cb, "systemctl", "enable", "--now", fpmSvc); err != nil {
			return fmt.Errorf("gagal mengaktifkan service %s: %w", fpmSvc, err)
		}
	} else {
		exts := []string{
			"php-fpm", "php-cli", "php-common", "php-curl", "php-mbstring",
			"php-xml", "php-zip", "php-pgsql", "php-mysqlnd", "php-intl", "php-gd", "php-bcmath",
		}
		if remiActive {
			exts = append(exts, "php-pecl-redis")
		}
		if err := inst.installOrUpgradeRPM(ctx, cb, exts...); err != nil {
			return err
		}
		if err := inst.EnsureSystemLibpqPriority(ctx, cb); err != nil {
			cb(fmt.Sprintf("[WARN] Penyelarasan prioritas libpq sistem: %v", err))
		}
		if err := inst.runCmd(ctx, cb, "systemctl", "enable", "php-fpm"); err != nil {
			return fmt.Errorf("gagal mengaktifkan service php-fpm: %w", err)
		}
		if err := inst.runCmd(ctx, cb, "systemctl", "restart", "php-fpm"); err != nil {
			return fmt.Errorf("gagal menjalankan ulang service php-fpm: %w", err)
		}
	}

	cb(fmt.Sprintf("✓ PHP & PHP-FPM %s berhasil dipasang.", ver))
	return nil
}

// 12. Composer
func (inst *Installer) InstallComposer(ctx context.Context, cb LogCallback) error {
	cb("--- [12] Memasang Composer ---")
	inst.GuardToolchainColumns(ctx, cb, "composer")
	composerBin := "/usr/local/bin/composer"

	// Ambil PHAR resmi langsung dari rilis GitHub composer/composer.
	tmpDir, err := os.MkdirTemp("", "wdev-composer-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	pharPath := filepath.Join(tmpDir, "composer.phar")
	cb("Mengunduh Composer PHAR resmi...")
	if err := downloadFile("https://getcomposer.org/download/latest-stable/composer.phar", pharPath); err != nil {
		return fmt.Errorf("gagal mengunduh composer.phar: %w", err)
	}

	if err := inst.runCmd(ctx, cb, "install", "-m", "0755", pharPath, composerBin); err != nil {
		return err
	}
	inst.removeDistroPackagesIfInstalled(ctx, cb, "composer")

	if out, err := exec.CommandContext(ctx, composerBin, "-V").CombinedOutput(); err == nil {
		cb("✓ " + strings.TrimSpace(string(out)))
	} else {
		cb("✓ Composer berhasil dipasang di /usr/local/bin/composer")
	}
	return nil
}

// 13. Local SSL (mkcert)
func (inst *Installer) InstallMkcert(ctx context.Context, cb LogCallback) error {
	cb("--- [13] Memasang Local SSL (mkcert) ---")
	const mkcertVer = "1.4.4"
	arch := runtime.GOARCH
	dst := "/usr/local/bin/mkcert"

	alreadyLatest := false
	if out, err := exec.CommandContext(ctx, dst, "-version").CombinedOutput(); err == nil {
		if strings.Contains(string(out), mkcertVer) {
			alreadyLatest = true
		}
	}

	if alreadyLatest {
		cb(fmt.Sprintf("mkcert v%s sudah terpasang di %s, melewati unduhan.", mkcertVer, dst))
	} else {
		url := fmt.Sprintf("https://github.com/FiloSottile/mkcert/releases/download/v%s/mkcert-v%s-linux-%s", mkcertVer, mkcertVer, arch)
		tmpDir, err := os.MkdirTemp("", "wdev-mkcert-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpDir)
		tmpFile := filepath.Join(tmpDir, "mkcert.tmp")

		if err := downloadFile(url, tmpFile); err != nil {
			return err
		}

		_ = inst.runCmd(ctx, cb, "rm", "-f", dst)
		_ = inst.runCmd(ctx, cb, "cp", tmpFile, dst)
		_ = inst.runCmd(ctx, cb, "chmod", "+x", dst)
	}

	inst.removeDistroPackagesIfInstalled(ctx, cb, "mkcert")

	_ = inst.runCmd(ctx, cb, "mkdir", "-p", "/etc/ssl/local-certs/ca")
	_ = inst.runCmd(ctx, cb, "chmod", "755", "/etc/ssl/local-certs", "/etc/ssl/local-certs/ca")
	_ = inst.runCmd(ctx, cb, dst, "-install")
	inst.ensureWdevProfileEnv(ctx, cb)

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

// Orca ADE (Desktop & Headless — Rilis Resmi GitHub stablyai/orca tanpa apt/dnf)
func (inst *Installer) InstallOrca(ctx context.Context, cb LogCallback, mode string) error {
	// Debian/Ubuntu: gunakan paket .deb resmi dan ekstrak ke ruang user.
	// Ini jalur utama wdev, sekaligus memperbarui instalasi .deb yang sudah ada.
	if inst.isDebian {
		return inst.installOrcaDeb(ctx, cb, mode)
	}

	cb("==> Memeriksa rilis terbaru Orca ADE dari GitHub...")
	tag, assets, err := fetchGitHubLatestRelease(ctx, "stablyai/orca")
	latest := ""
	if err != nil {
		cb(fmt.Sprintf("[WARN] GitHub API tidak tersedia (%v), menggunakan URL latest/download...", err))
	} else if tag != "" {
		latest = strings.TrimPrefix(strings.TrimSpace(tag), "v")
		cb(fmt.Sprintf("Rilis terbaru Orca: %s", tag))
	}

	if mode == "headless" {
		return inst.installOrcaServerTarball(ctx, cb, latest, assets)
	}

	return inst.installOrcaAppImage(ctx, cb, latest, assets)
}

func (inst *Installer) UninstallOrca(ctx context.Context, cb LogCallback, purge bool) error {
	targetUser, userHome := ResolveOrcaUser()

	cb("==> Menghentikan layanan Orca ADE...")
	if orcaSystemUnitExists() {
		_ = inst.runCmd(ctx, cb, "systemctl", "disable", "--now", "orca.service")
	}
	if userHome != "" {
		if orcaUserUnitExists(userHome) {
			_ = runAsUser(ctx, cb, targetUser, userHome, "systemctl", "--user", "disable", "--now", "orca.service")
		}
		_ = runAsUser(ctx, cb, targetUser, userHome, "systemctl", "--user", "disable", "--now", "orca-update.timer")
		_ = os.Remove(filepath.Join(userHome, ".config", "systemd", "user", "orca.service"))
		_ = os.Remove(filepath.Join(userHome, ".config", "systemd", "user", "orca-update.service"))
		_ = os.Remove(filepath.Join(userHome, ".config", "systemd", "user", "orca-update.timer"))
		_ = runAsUser(ctx, cb, targetUser, userHome, "systemctl", "--user", "daemon-reload")
	}
	_ = inst.runCmd(ctx, cb, "rm", "-f", "/etc/systemd/system/orca.service", "/etc/systemd/system/orca-update.service", "/etc/systemd/system/orca-update.timer")
	_ = inst.runCmd(ctx, cb, "systemctl", "daemon-reload")

	cb("==> Menghapus instalasi biner Orca ADE...")
	_ = inst.runCmd(ctx, cb, "rm", "-rf",
		"/usr/local/lib/orca-server",
		"/usr/local/lib/orca-ide",
		"/usr/local/bin/orca",
		"/usr/local/bin/orca-ide",
		"/usr/local/bin/orca-desktop",
		"/usr/local/bin/orca-pair",
		"/usr/local/bin/orca-switch",
		"/usr/local/bin/orca-update",
	)
	if userHome != "" {
		_ = os.RemoveAll(orcaInstallDir(userHome))
		_ = os.RemoveAll(orcaInstallDir(userHome) + ".old")
		_ = os.Remove(filepath.Join(userHome, ".local", "bin", "orca"))
		_ = os.Remove(filepath.Join(userHome, ".local", "bin", "orca-ide"))
		_ = os.Remove(filepath.Join(userHome, ".local", "bin", "orca-desktop"))
		_ = os.Remove(filepath.Join(userHome, ".local", "bin", "orca-update"))
		_ = os.Remove(filepath.Join(userHome, ".local", "bin", "orca-switch"))
		_ = os.Remove(filepath.Join(userHome, ".local", "bin", "orca-pair"))
		_ = os.Remove(filepath.Join(userHome, ".config", "autostart", "orca-ide.desktop"))
		_ = os.Remove(filepath.Join(userHome, ".local", "share", "applications", "orca-ide.desktop"))
		_ = os.Remove(filepath.Join(userHome, ".local", "share", "icons", "hicolor", "256x256", "apps", "orca-ide.png"))
	}

	// Bersihkan paket legacy jika sebelumnya dipasang via dpkg/rpm
	if inst.isDebian {
		if exec.CommandContext(ctx, "dpkg", "-s", "orca-ide").Run() == nil {
			_ = inst.runCmd(ctx, cb, "apt-get", "remove", "-y", "orca-ide")
		}
	} else if inst.pkgMgr == "dnf" || inst.pkgMgr == "yum" {
		if exec.CommandContext(ctx, "rpm", "-q", "orca-ide").Run() == nil {
			_ = inst.runCmd(ctx, cb, inst.pkgMgr, "remove", "-y", "orca-ide")
		}
	}

	if purge {
		cb("==> Menghapus data konfigurasi...")
		if userHome != "" && userHome != "/root" {
			_ = inst.runCmd(ctx, cb, "rm", "-rf", filepath.Join(userHome, ".config", "orca"), "/var/lib/orca")
		} else {
			_ = inst.runCmd(ctx, cb, "rm", "-rf", "/var/lib/orca")
		}
	}

	ResetOrcaVersionCache()
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
	} else if inst.pkgMgr == "dnf" || inst.pkgMgr == "yum" {
		_ = inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", "zsh")
	} else if inst.pkgMgr == "pacman" {
		_ = inst.runCmd(ctx, cb, "pacman", "-S", "--noconfirm", "zsh")
	} else if inst.pkgMgr == "zypper" {
		_ = inst.runCmd(ctx, cb, "zypper", "install", "-y", "zsh")
	}

	targetUser, home := ResolveOrcaUser()
	if home == "" {
		targetUser, home = ResolveTargetUserAndHome()
	}
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	omzDir := filepath.Join(home, ".oh-my-zsh")

	if _, err := os.Stat(omzDir); os.IsNotExist(err) {
		if err := inst.runCmd(ctx, cb, "git", "clone", "--depth=1", "https://github.com/ohmyzsh/ohmyzsh.git", omzDir); err != nil {
			return fmt.Errorf("gagal mengkloning Oh My Zsh: %w", err)
		}
	} else {
		_ = inst.runCmd(ctx, cb, "git", "-c", "safe.directory="+omzDir, "-C", omzDir, "pull", "--ff-only")
	}

	// Plugins
	pluginsDir := filepath.Join(omzDir, "custom", "plugins")
	_ = os.MkdirAll(pluginsDir, 0755)
	for _, pl := range []struct {
		dir string
		url string
	}{
		{"zsh-autosuggestions", "https://github.com/zsh-users/zsh-autosuggestions"},
		{"zsh-syntax-highlighting", "https://github.com/zsh-users/zsh-syntax-highlighting"},
		{"zsh-completions", "https://github.com/zsh-users/zsh-completions"},
	} {
		pDir := filepath.Join(pluginsDir, pl.dir)
		if _, err := os.Stat(pDir); os.IsNotExist(err) {
			_ = inst.runCmd(ctx, cb, "git", "clone", "--depth=1", pl.url, pDir)
		} else {
			_ = inst.runCmd(ctx, cb, "git", "-c", "safe.directory="+pDir, "-C", pDir, "pull", "--ff-only")
		}
	}

	// Themes
	themesDir := filepath.Join(omzDir, "custom", "themes")
	_ = os.MkdirAll(themesDir, 0755)
	p10k := filepath.Join(themesDir, "powerlevel10k")
	if _, err := os.Stat(p10k); os.IsNotExist(err) {
		_ = inst.runCmd(ctx, cb, "git", "clone", "--depth=1", "https://github.com/romkatv/powerlevel10k.git", p10k)
	} else {
		_ = inst.runCmd(ctx, cb, "git", "-c", "safe.directory="+p10k, "-C", p10k, "pull", "--ff-only")
	}

	// Pastikan kepemilikan & permission aman agar compinit/compaudit tidak mencetak warning saat Zsh start
	chownPath(targetUser, omzDir)
	_ = exec.CommandContext(ctx, "chmod", "-R", "go-w", omzDir).Run()

	// Pasang Nerd Font (MesloLGS NF) bila belum ada agar ikon tema Powerlevel10k/Agnoster tampil rapi
	inst.ensureMesloLGSNerdFont(ctx, cb, targetUser, home)

	// Rapikan ~/.zshrc (bersihkan sisa loop yatim & pastikan ZSH_DISABLE_COMPFIX / p10k instant prompt aman)
	zshrcPath := filepath.Join(home, ".zshrc")
	existingZshrc, _ := os.ReadFile(zshrcPath)
	updatedZshrc := reconcileZshrc(string(existingZshrc), theme)
	if err := os.WriteFile(zshrcPath, []byte(updatedZshrc), 0644); err != nil {
		cb(fmt.Sprintf("[WARN] Gagal memperbarui %s: %v", zshrcPath, err))
	} else {
		chownPath(targetUser, zshrcPath)
		_, _ = SyncUserShellEnv(targetUser, home)
		if zshBin, zErr := exec.LookPath("zsh"); zErr == nil {
			if out, nErr := exec.CommandContext(ctx, zshBin, "-n", zshrcPath).CombinedOutput(); nErr != nil {
				cb(fmt.Sprintf("[WARN] Pemeriksaan sintaks %s: %s", zshrcPath, strings.TrimSpace(string(out))))
			}
		}
	}

	if zshBin, zErr := exec.LookPath("zsh"); zErr == nil && targetUser != "" {
		_ = inst.runCmd(ctx, cb, "chsh", "-s", zshBin, targetUser)
	}

	cb(fmt.Sprintf("✓ Oh My Zsh berhasil dikonfigurasi dengan tema %s untuk user %s.", theme, targetUser))
	return nil
}

// 15. Cloudflare Tunnel (cloudflared)
func (inst *Installer) InstallCloudflared(ctx context.Context, cb LogCallback) error {
	cb("--- [15] Memasang cloudflared binary ---")
	arch := runtime.GOARCH

	url := fmt.Sprintf("https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-%s", arch)
	dst := "/usr/local/bin/cloudflared"
	tmpDir, err := os.MkdirTemp("", "wdev-cloudflared-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	tmpFile := filepath.Join(tmpDir, "cloudflared.tmp")

	if err := downloadFile(url, tmpFile); err != nil {
		return err
	}

	_ = inst.runCmd(ctx, cb, "rm", "-f", dst)
	_ = inst.runCmd(ctx, cb, "cp", tmpFile, dst)
	_ = inst.runCmd(ctx, cb, "chmod", "+x", dst)

	inst.removeDistroPackagesIfInstalled(ctx, cb, "cloudflared")

	cb("✓ cloudflared berhasil dipasang di /usr/local/bin/cloudflared")
	return nil
}

// 16. NetBird Mesh VPN (Repo Resmi pkgs.netbird.io, mandiri tanpa PowerDNS)
func (inst *Installer) InstallNetbird(ctx context.Context, cb LogCallback) error {
	cb("--- [16] Memasang / Memperbarui NetBird Mesh VPN ---")
	wasActive := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "netbird").Run() == nil

	_ = inst.ensureNetbirdRepo(ctx, cb)

	if inst.isDebian {
		if err := inst.runCmd(ctx, cb, "apt-get", "install", "-y", "netbird"); err != nil {
			return err
		}
	} else if inst.pkgMgr == "dnf" || inst.pkgMgr == "yum" {
		action := "install"
		if rpmIsInstalled(ctx, "netbird") {
			action = "upgrade"
		}
		args := []string{action, "-y"}
		if inst.pkgMgr == "dnf" {
			args = append(args, "--refresh")
		}
		args = append(args, "netbird")
		if err := inst.runCmd(ctx, cb, inst.pkgMgr, args...); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("package manager '%s' belum didukung untuk pemasangan otomatis NetBird", inst.pkgMgr)
	}

	if exec.CommandContext(ctx, "systemctl", "cat", "netbird").Run() == nil {
		_ = inst.runCmd(ctx, cb, "systemctl", "enable", "netbird")
		if wasActive {
			if err := inst.runCmd(ctx, cb, "systemctl", "restart", "netbird"); err != nil {
				return fmt.Errorf("gagal menjalankan ulang service netbird: %w", err)
			}
		} else if err := inst.runCmd(ctx, cb, "systemctl", "start", "netbird"); err != nil {
			cb(fmt.Sprintf("[WARN] Service netbird terpasang namun belum dapat dijalankan otomatis: %v", err))
		}
	}

	ver := ""
	if nbBin := findTool("netbird"); nbBin != "" {
		if out, err := exec.CommandContext(ctx, nbBin, "version").CombinedOutput(); err == nil {
			ver = strings.TrimSpace(string(out))
		}
	}
	if ver != "" {
		cb(fmt.Sprintf("✓ NetBird Mesh VPN (v%s) berhasil dipasang/diperbarui.", strings.TrimPrefix(ver, "v")))
	} else {
		cb("✓ NetBird Mesh VPN berhasil dipasang/diperbarui.")
	}
	return nil
}

// Full 14-Step Auto Setup
func (inst *Installer) RunFullInstall(ctx context.Context, cb LogCallback) error {
	cb("=================================================================")
	cb(" MEMULAI INSTALASI LENGKAP LINGKUNGAN PENGEMBANGAN LINUX (14 LANGKAH)")
	cb("=================================================================")

	// Langkah 0: deteksi otomatis duplikat/double path sebelum instalasi,
	// sekaligus menyelaraskan unit wdev.service & permission cache Nginx.
	inst.PreflightToolchain(ctx, cb)
	if err := inst.EnsureWdevServiceUnit(ctx, cb); err != nil {
		cb(fmt.Sprintf("[WARN] Penyelarasan unit wdev.service: %v", err))
	}
	if err := inst.FixNginxCachePerm(ctx, cb); err != nil {
		cb(fmt.Sprintf("[WARN] Perbaikan permission cache Nginx: %v", err))
	}

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

	// Langkah 15: audit ulang + perbaiki duplikat yang tersisa.
	if err := inst.PostflightToolchain(ctx, cb); err != nil {
		cb(fmt.Sprintf("[WARN] Post-flight toolchain: %v", err))
	}

	cb("\n=================================================================")
	cb(" ✓ SELURUH 14 LANGKAH INSTALASI LENGKAP TELAH SELESAI DENGAN SUKSES!")
	cb(" ✓ Environment PATH tunggal; duplikat sudah diaudit otomatis.")
	cb("=================================================================")
	return nil
}

// Update All
func (inst *Installer) RunUpdateAll(ctx context.Context, cb LogCallback) error {
	cb("=== MEMPERBARUI SELURUH SISTEM & TOOLCHAIN ===")
	nginxBackup, _ := os.ReadFile("/etc/nginx/nginx.conf")
	if inst.isDebian {
		_ = inst.runCmd(ctx, cb, "apt-get", "update", "-y")
		_ = inst.runCmd(ctx, cb, "apt-get", aptSafeConffileArgs("upgrade")...)
	} else {
		migratingPG := inst.preparePostgresBeforeRPMUpgrade(ctx, cb, "")
		_ = inst.runCmd(ctx, cb, inst.pkgMgr, "upgrade", "-y")
		if err := inst.reconcilePostgresAfterRPMUpgrade(ctx, cb, migratingPG); err != nil {
			cb(fmt.Sprintf("[WARN] Penyelarasan PostgreSQL pasca-upgrade: %v", err))
		}
		if err := inst.EnsureSystemLibpqPriority(ctx, cb); err != nil {
			cb(fmt.Sprintf("[WARN] Penyelarasan prioritas libpq sistem: %v", err))
		}
	}

	if _, err := exec.LookPath("composer"); err == nil {
		_ = inst.runCmd(ctx, cb, "composer", "self-update")
	}
	if rustupBin := findTool("rustup"); rustupBin != "" {
		_ = inst.runCmd(ctx, cb, rustupBin, "update")
		_ = inst.runCmd(ctx, cb, "chmod", "-R", "go-w,a+rX", "/usr/local/cargo", "/usr/local/rustup")
	}
	if flutterBin := findTool("flutter"); flutterBin != "" {
		inst.ensureGitSafeDirectory(ctx, cb, "/usr/local/flutter")
		_ = inst.runCmd(ctx, cb, flutterBin, "--suppress-analytics", "upgrade")
		_ = inst.runCmd(ctx, cb, flutterBin, "--suppress-analytics", "precache")
		_ = inst.runCmd(ctx, cb, "chmod", "-R", "go-w,a+rX", "/usr/local/flutter")
	}

	inst.ensureWdevProfileEnv(ctx, cb)
	if err := inst.EnsureWdevServiceUnit(ctx, cb); err != nil {
		cb(fmt.Sprintf("[WARN] Penyelarasan unit wdev.service: %v", err))
	}
	if err := inst.reconcileNginxAfterUpdate(ctx, cb, nginxBackup, false); err != nil {
		cb(fmt.Sprintf("[WARN] Penyelarasan Nginx pasca-upgrade: %v", err))
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

	getCandidate := func(pkgNames ...string) string {
		if !inst.isDebian {
			if inst.pkgMgr == "dnf" || inst.pkgMgr == "yum" {
				return dnfRepoQueryLatest(ctx, pkgNames...)
			}
			return "-"
		}
		for _, pkgName := range pkgNames {
			out, err := exec.CommandContext(ctx, "apt-cache", "policy", pkgName).CombinedOutput()
			if err != nil {
				continue
			}
			lines := strings.Split(string(out), "\n")
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if strings.HasPrefix(l, "Candidate:") {
					parts := strings.Fields(l)
					if len(parts) >= 2 && parts[1] != "(none)" {
						cand := parts[1]
						cand = strings.Split(cand, "+")[0]
						cand = strings.Split(cand, "~")[0]
						cand = strings.Split(cand, "-")[0]
						cand = strings.TrimPrefix(cand, "5:")
						if cand != "" && cand != "(none)" {
							return cand
						}
					}
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

	// 4b. OpenJDK Java (Eclipse Adoptium)
	javaInst := "-"
	javaMajor := "21"
	if out, err := exec.CommandContext(ctx, "java", "-version").CombinedOutput(); err == nil {
		firstLine := strings.Split(string(out), "\n")[0]
		if start := strings.Index(firstLine, "\""); start != -1 {
			if end := strings.LastIndex(firstLine, "\""); end > start {
				javaInst = firstLine[start+1 : end]
				if m := strings.Split(javaInst, ".")[0]; m != "" {
					javaMajor = m
				}
			}
		}
	}
	javaLatest := "-"
	if _, verStr, err := fetchAdoptiumLatest(ctx, javaMajor); err == nil && verStr != "" {
		javaLatest = verStr
	}
	results = append(results, VersionCheck{
		Name:      "OpenJDK Java",
		Installed: javaInst,
		Latest:    javaLatest,
		HasUpdate: javaLatest != "-" && javaInst != "-" && hasNewerVersion(javaInst, javaLatest),
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
	if nginxBin := findTool("nginx"); nginxBin != "" {
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
	if out, err := exec.CommandContext(ctx, "php", "-r", "echo PHP_VERSION;").Output(); err == nil {
		phpInst = strings.TrimSpace(string(out))
	}
	phpLatest := getCandidate("php8.4", "php")
	results = append(results, VersionCheck{
		Name:      "PHP & PHP-FPM",
		Installed: phpInst,
		Latest:    phpLatest,
		HasUpdate: phpLatest != "-" && phpInst != "-" && !strings.HasPrefix(phpLatest, phpInst),
	})

	// 8. PostgreSQL
	pgInst := "-"
	pgMajor := "16"
	if inst.isDebian {
		if out, err := exec.CommandContext(ctx, "pg_lsclusters", "-h").CombinedOutput(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 4 && fields[2] == "5432" && fields[3] == "online" {
					pgMajor = fields[0]
					break
				}
			}
		}
	} else {
		pgMajor = detectRPMPostgresMajor(ctx)
	}
	for _, pgBin := range []string{
		fmt.Sprintf("/usr/lib/postgresql/%s/bin/postgres", pgMajor),
		fmt.Sprintf("/usr/pgsql-%s/bin/postgres", pgMajor),
		"/usr/bin/postgres",
		fmt.Sprintf("/usr/pgsql-%s/bin/psql", pgMajor),
		"psql",
	} {
		if pgInst != "-" {
			break
		}
		if out, err := exec.CommandContext(ctx, pgBin, "-V").CombinedOutput(); err == nil {
			fields := strings.Fields(string(out))
			if len(fields) >= 3 {
				pgInst = fields[2]
			}
		}
	}
	var pgLatest string
	if inst.isDebian {
		pgLatest = getCandidate(fmt.Sprintf("postgresql-%s", pgMajor), "postgresql")
	} else {
		pgLatest = getCandidate(fmt.Sprintf("postgresql%s-server", pgMajor), "postgresql-server")
	}
	results = append(results, VersionCheck{
		Name:      "PostgreSQL",
		Installed: pgInst,
		Latest:    pgLatest,
		HasUpdate: pgLatest != "-" && pgInst != "-" && hasNewerVersion(pgInst, pgLatest),
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
	redisLatest := getCandidate("redis", "redis-server")
	results = append(results, VersionCheck{
		Name:      "Redis Cache",
		Installed: redisInst,
		Latest:    redisLatest,
		HasUpdate: redisLatest != "-" && redisInst != "-" && !strings.HasPrefix(redisLatest, redisInst),
	})

	// 10. NetBird
	nbInst := "-"
	if nbBin := findTool("netbird"); nbBin != "" {
		if out, err := exec.CommandContext(ctx, nbBin, "version").CombinedOutput(); err == nil {
			nbInst = strings.TrimSpace(string(out))
		}
	}
	nbLatest := getCandidate("netbird")
	if up := fetchNetbirdLatestVersion(ctx); up != "" && (nbLatest == "-" || hasNewerVersion(nbLatest, up)) {
		nbLatest = up
	}
	results = append(results, VersionCheck{
		Name:      "NetBird Mesh VPN",
		Installed: nbInst,
		Latest:    nbLatest,
		HasUpdate: nbLatest != "-" && nbInst != "-" && hasNewerVersion(nbInst, nbLatest),
	})

	// 11. Orca ADE
	orcaInst := "-"
	orcaUser, orcaHome := ResolveOrcaUser()
	if v := DetectOrcaInstalledVersion(ctx, orcaUser, orcaHome); v != "" {
		orcaInst = v
	}
	orcaLatest := "-"
	if v := GetOrcaLatestVersion(ctx, true); v != "" {
		orcaLatest = v
	}
	results = append(results, VersionCheck{
		Name:      "Orca ADE",
		Installed: orcaInst,
		Latest:    orcaLatest,
		HasUpdate: orcaLatest != "-" && orcaInst != "-" && hasNewerVersion(orcaInst, orcaLatest),
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
	} else if inst.pkgMgr == "dnf" || inst.pkgMgr == "yum" {
		cb(fmt.Sprintf("Memeriksa repositori paket %s (check-update)...", strings.ToUpper(inst.pkgMgr)))
		cmd := exec.CommandContext(ctx, inst.pkgMgr, "-q", "check-update")
		out, err := cmd.CombinedOutput()
		exitCode := 0
		if err != nil {
			if cmd.ProcessState != nil {
				exitCode = cmd.ProcessState.ExitCode()
			} else {
				exitCode = 1
			}
		}
		if exitCode != 0 && exitCode != 100 {
			cb(fmt.Sprintf("[ERROR] Gagal memeriksa paket %s: %v", inst.pkgMgr, err))
		} else if exitCode == 0 {
			cb("✓ Seluruh paket sistem operasi sudah berada pada versi terbaru!")
		} else {
			var upgradable []string
			for _, l := range strings.Split(string(out), "\n") {
				fields := strings.Fields(strings.TrimSpace(l))
				if len(fields) >= 3 && !strings.HasPrefix(fields[0], "Last") && !strings.HasPrefix(fields[0], "Obsoleting") {
					upgradable = append(upgradable, fmt.Sprintf("  • %-30s : %s (%s)", fields[0], fields[1], fields[2]))
				}
			}
			if len(upgradable) > 0 {
				cb(fmt.Sprintf("Ditemukan %d paket sistem yang memiliki pembaruan:", len(upgradable)))
				for _, up := range upgradable {
					cb(up)
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

	checkBin := func(name string) {
		if path := findTool(name); path != "" {
			dups := findDuplicateToolPaths(name)
			if len(dups) > 1 {
				cb(fmt.Sprintf("  [OK] %-14s : %s (⚠ duplikat path: %s)", name, path, strings.Join(dups, ", ")))
			} else {
				cb(fmt.Sprintf("  [OK] %-14s : %s", name, path))
			}
		} else {
			cb(fmt.Sprintf("  [--] %-14s : Tidak terpasang", name))
		}
	}

	bins := []string{"git", "curl", "node", "npm", "go", "rustc", "cargo", "mold", "java", "flutter", "psql", "redis-cli", "nginx", "php", "composer", "mkcert", "zsh", "cloudflared", "netbird", "orca", "pdns_server", "stalwart", "stalwart-cli"}
	for _, b := range bins {
		checkBin(b)
	}

	cb("\nMemeriksa konfigurasi Nginx...")
	nginxBin := findTool("nginx")
	if nginxBin != "" {
		cmd := exec.CommandContext(ctx, nginxBin, "-t")
		if os.Geteuid() != 0 {
			cmd = exec.CommandContext(ctx, "sudo", "-n", nginxBin, "-t")
		}
		if out, err := cmd.CombinedOutput(); err == nil {
			cb("  [OK] Nginx sintaks konfigurasi valid.")
		} else {
			cb(fmt.Sprintf("  [ERROR] Nginx sintaks gagal: %s", string(out)))
		}
	} else {
		cb("  [--] Nginx tidak terpasang.")
	}

	cb("\nCache & permission Nginx:")
	for _, line := range DiagnoseNginxCache() {
		cb(line)
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
