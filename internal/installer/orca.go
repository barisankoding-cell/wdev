package installer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const orcaGitHubRepo = "stablyai/orca"

// Orca ADE di Debian dipasang dari paket .deb resmi lalu diekstrak ke ruang user.
// Pendekatan ini tidak membutuhkan dpkg/apt dan tidak bertabrakan dengan paket
// sistem, tetapi tetap memakai arti .deb resmi yang sama dengan rilis GitHub.

func orcaInstallDir(home string) string {
	return filepath.Join(home, ".local", "lib", "orca-ide")
}

func orcaModeFile(home string) string {
	return filepath.Join(home, ".config", "orca", ".mode")
}

func readOrcaMode(home string) string {
	if data, err := os.ReadFile(orcaModeFile(home)); err == nil {
		if mode := strings.TrimSpace(string(data)); mode == "desktop" || mode == "headless" {
			return mode
		}
	}
	return "headless"
}

func writeOrcaMode(home, user, mode string) error {
	path := orcaModeFile(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(mode+"\n"), 0o644); err != nil {
		return err
	}
	chownPath(user, path)
	return nil
}

func chownPath(user string, paths ...string) {
	if os.Geteuid() != 0 || user == "" || user == "root" {
		return
	}
	args := append([]string{"-R", user + ":"}, paths...)
	_ = exec.Command("chown", args...).Run()
}

// ResolveOrcaUser menentukan user pemilik instalasi Orca ADE. Saat wdev
// berjalan sebagai root (systemd), SUDO_USER kosong, sehingga instalasi milik
// user desktop harus dideteksi dari home yang memiliki ~/.local/lib/orca-ide.
func ResolveOrcaUser() (string, string) {
	if sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER")); sudoUser != "" && sudoUser != "root" {
		if u, err := osuser.Lookup(sudoUser); err == nil && u.HomeDir != "" {
			return u.Username, u.HomeDir
		}
	}
	if u, err := osuser.Current(); err == nil && u.Uid != "0" && u.HomeDir != "" {
		return u.Username, u.HomeDir
	}

	homes, _ := filepath.Glob("/home/*")
	bestHome := ""
	var bestTime time.Time
	for _, home := range homes {
		if fi, err := os.Stat(filepath.Join(home, ".local", "lib", "orca-ide")); err == nil && fi.IsDir() {
			if fi.ModTime().After(bestTime) {
				bestTime = fi.ModTime()
				bestHome = home
			}
		} else if fi, err := os.Stat(filepath.Join(home, ".config", "orca")); err == nil && fi.IsDir() {
			if fi.ModTime().After(bestTime) {
				bestTime = fi.ModTime()
				bestHome = home
			}
		}
	}
	if bestHome != "" {
		base := filepath.Base(bestHome)
		if u, err := osuser.Lookup(base); err == nil {
			return u.Username, u.HomeDir
		}
		return base, bestHome
	}

	// Fallback: user non-sistem pertama (uid >= 1000).
	if users, err := osuser.LookupId("1000"); err == nil && users.HomeDir != "" {
		return users.Username, users.HomeDir
	}
	return ResolveTargetUserAndHome()
}

// findOrcaBinary mencari biner utama Orca di dalam direktori aplikasi.
func findOrcaBinary(appDir string) string {
	for _, rel := range []string{
		filepath.Join("resources", "bin", "orca-ide"),
		"orca-ide",
	} {
		p := filepath.Join(appDir, rel)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	if _, err := os.Stat(appDir); err != nil {
		return ""
	}
	found := ""
	_ = filepath.WalkDir(appDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if d.Name() == "orca-ide" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// findExtractedOrcaDir menentukan root aplikasi hasil ekstraksi .deb.
func findExtractedOrcaDir(extractDir string) string {
	for _, rel := range []string{
		filepath.Join("opt", "Orca"),
		filepath.Join("opt", "orca-ide"),
		filepath.Join("opt", "orca"),
		filepath.Join("opt", "Orca ADE"),
	} {
		p := filepath.Join(extractDir, rel)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
	}
	appDir := ""
	_ = filepath.WalkDir(extractDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || !d.IsDir() {
			return nil
		}
		if d.Name() == "resources" {
			appDir = filepath.Dir(path)
			return filepath.SkipAll
		}
		return nil
	})
	return appDir
}

func orcaBinaryVersion(ctx context.Context, home, appDir string) string {
	bin := findOrcaBinary(appDir)
	if bin == "" {
		return ""
	}
	cmd := exec.CommandContext(ctx, bin, "--version")
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	line = strings.TrimPrefix(line, "v")
	return strings.TrimSpace(line)
}

// runAsUser menjalankan perintah sebagai user target (mis. systemctl --user).
func runAsUser(ctx context.Context, cb LogCallback, user, home, name string, args ...string) error {
	if cb != nil {
		cb(fmt.Sprintf("==> [%s] %s %s", user, name, strings.Join(args, " ")))
	}
	var cmd *exec.Cmd
	switch {
	case os.Geteuid() == 0 && user != "" && user != "root":
		runArgs := append([]string{"-u", user, "--", name}, args...)
		cmd = exec.CommandContext(ctx, "runuser", runArgs...)
	case os.Geteuid() == 0:
		cmd = exec.CommandContext(ctx, name, args...)
	default:
		cmd = exec.CommandContext(ctx, name, args...)
	}
	env := append(os.Environ(), "HOME="+home, "USER="+user, "LOGNAME="+user)
	if u, err := osuser.Lookup(user); err == nil {
		runtimeDir := "/run/user/" + u.Uid
		if fi, statErr := os.Stat(runtimeDir); statErr == nil && fi.IsDir() {
			env = append(env, "XDG_RUNTIME_DIR="+runtimeDir)
			if _, busErr := os.Stat(filepath.Join(runtimeDir, "bus")); busErr == nil {
				env = append(env, "DBUS_SESSION_BUS_ADDRESS=unix:path="+filepath.Join(runtimeDir, "bus"))
			}
		}
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" && cb != nil {
			cb(line)
		}
	}
	return err
}

func orcaUserUnitExists(home string) bool {
	_, err := os.Stat(filepath.Join(home, ".config", "systemd", "user", "orca.service"))
	return err == nil
}

func stopOrcaService(ctx context.Context, cb LogCallback, user, home string) {
	if orcaUserUnitExists(home) {
		_ = runAsUser(ctx, cb, user, home, "systemctl", "--user", "stop", "orca.service")
	}
	_ = exec.CommandContext(ctx, "systemctl", "stop", "orca.service").Run()
}

// wdevExecutable menentukan biner wdev yang dipakai untuk symlink helper Orca.
func wdevExecutable() string {
	if _, err := os.Stat("/usr/local/bin/wdev"); err == nil {
		return "/usr/local/bin/wdev"
	}
	exe, err := os.Executable()
	if err != nil {
		return "wdev"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe
}

// orcaRealCLI mengembalikan CLI resmi Orca yang ikut di dalam paket .deb.
func orcaRealCLI(home string) string {
	p := filepath.Join(orcaInstallDir(home), "resources", "bin", "orca-ide")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// orcaAppBinary mengembalikan biner Electron (Desktop GUI).
func orcaAppBinary(home string) string {
	p := filepath.Join(orcaInstallDir(home), "orca-ide")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

func replaceSymlink(link, target string) error {
	if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if dest, err := os.Readlink(link); err == nil && dest == target {
			return nil
		}
	}
	_ = os.RemoveAll(link)
	return os.Symlink(target, link)
}

// legacyOrcaHelpers adalah helper Bash lama yang kini digantikan perintah Go.
var legacyOrcaHelpers = []string{
	"orca", "orca-desktop", "orca-pair", "orca-switch", "orca-update", "orca-ide",
}

// ensureOrcaLauncher memasang launcher Orca tanpa skrip shell:
// - orca/orca-desktop/orca-pair/orca-switch/orca-update -> symlink ke biner wdev
// - orca-ide -> symlink ke CLI resmi Orca dari paket .deb
func ensureOrcaLauncher(user, home string) error {
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	wdev := wdevExecutable()

	for _, name := range []string{"orca", "orca-desktop", "orca-pair", "orca-switch", "orca-update"} {
		if err := replaceSymlink(filepath.Join(binDir, name), wdev); err != nil {
			return err
		}
	}
	if cli := orcaRealCLI(home); cli != "" {
		if err := replaceSymlink(filepath.Join(binDir, "orca-ide"), cli); err != nil {
			return err
		}
	} else if err := replaceSymlink(filepath.Join(binDir, "orca-ide"), wdev); err != nil {
		return err
	}
	// Launcher global (root) juga diarahkan ke biner Go, bukan skrip Bash.
	if os.Geteuid() == 0 {
		for _, name := range []string{"orca", "orca-desktop", "orca-pair", "orca-switch", "orca-update"} {
			_ = replaceSymlink(filepath.Join("/usr/local/bin", name), wdev)
		}
	}

	chownPath(user, binDir)
	return nil
}

// removeLegacyOrcaShellHelpers menghapus helper Bash lama (termasuk unit timer)
// agar tidak ada lagi ketergantungan ke skrip shell.
func removeLegacyOrcaShellHelpers(home, user string) {
	binDir := filepath.Join(home, ".local", "bin")
	for _, name := range legacyOrcaHelpers {
		p := filepath.Join(binDir, name)
		if fi, err := os.Lstat(p); err != nil || fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		_ = os.Remove(p)
	}
	// Helper lama juga pernah dipasang di /usr/local/bin (root).
	for _, name := range legacyOrcaHelpers {
		p := filepath.Join("/usr/local/bin", name)
		if fi, err := os.Lstat(p); err == nil && fi.Mode()&os.ModeSymlink == 0 {
			_ = os.Remove(p)
		}
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	for _, name := range []string{"orca-update.service", "orca-update.timer"} {
		_ = os.Remove(filepath.Join(unitDir, name))
	}
	_ = os.Remove(filepath.Join(home, ".config", "autostart", "orca-ide.desktop"))
	if err := ensureOrcaLauncher(user, home); err != nil && user != "" {
		_ = err
	}
}

// detectOrcaBrowser mendeteksi Chromium/Chrome untuk browser automation Orca.
func detectOrcaBrowser() string {
	for _, p := range []string{
		"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable",
		"/usr/bin/chromium", "/usr/bin/chromium-browser",
		"/usr/bin/brave-browser", "/usr/bin/microsoft-edge-stable",
		"/usr/bin/microsoft-edge", "/snap/bin/chromium",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func orcaUserSystemctl(ctx context.Context, cb LogCallback, user, home string, args ...string) error {
	return runAsUser(ctx, cb, user, home, "systemctl", append([]string{"--user"}, args...)...)
}

// applyOrcaMode mengaktifkan mode desktop/headless sepenuhnya dari Go.
func applyOrcaMode(ctx context.Context, cb LogCallback, user, home, mode string) error {
	if err := writeOrcaMode(home, user, mode); err != nil {
		return err
	}
	binDir := filepath.Join(home, ".local", "bin")
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return err
	}

	switch mode {
	case "desktop":
		cb("==> Menghentikan layanan Orca Headless...")
		_ = orcaUserSystemctl(ctx, cb, user, home, "disable", "--now", "orca.service")
		_ = orcaUserSystemctl(ctx, cb, user, home, "disable", "--now", "orca-update.timer")
		_ = os.Remove(filepath.Join(home, ".config", "autostart", "orca-ide.desktop"))
		ensureOrcaDesktopEntry(user, home, "")
		cb("✓ Orca ADE diatur ke mode Desktop GUI.")
		return nil

	case "headless":
		browser := detectOrcaBrowser()
		envLine := ""
		if browser != "" {
			envLine = fmt.Sprintf("Environment=\"ORCA_BROWSER_EXECUTABLE=%s\"\n", browser)
		}
		unit := fmt.Sprintf(`[Unit]
Description=Orca ADE Headless Server
After=network.target

[Service]
Type=simple
ExecStart=%s serve --supervise --force
Restart=always
RestartSec=5
Environment="PATH=%s:/usr/local/bin:/usr/bin:/bin"
Environment="ORCA_ALLOW_INSECURE_WEB_PAIRING=1"
%s
[Install]
WantedBy=default.target
`, filepath.Join(binDir, "orca-ide"), binDir, envLine)

		updateUnit := fmt.Sprintf(`[Unit]
Description=Periksa & terapkan pembaruan Orca ADE (wdev)
After=network.target

[Service]
Type=oneshot
ExecStart=%s install orca-update
`, wdevExecutable())
		updateTimer := `[Unit]
Description=Pemeriksaan pembaruan Orca ADE harian (wdev)

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=1h

[Install]
WantedBy=timers.target
`
		if err := os.WriteFile(filepath.Join(unitDir, "orca.service"), []byte(unit), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(unitDir, "orca-update.service"), []byte(updateUnit), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(unitDir, "orca-update.timer"), []byte(updateTimer), 0o644); err != nil {
			return err
		}
		chownPath(user, unitDir)

		if os.Geteuid() == 0 {
			linger := exec.CommandContext(ctx, "loginctl", "enable-linger", user)
			if out, err := linger.CombinedOutput(); err != nil && cb != nil {
				cb(fmt.Sprintf("[WARN] loginctl enable-linger %s: %v (%s)", user, err, strings.TrimSpace(string(out))))
			}
		}

		_ = orcaUserSystemctl(ctx, cb, user, home, "daemon-reload")
		if err := orcaUserSystemctl(ctx, cb, user, home, "enable", "--now", "orca.service"); err != nil {
			return err
		}
		_ = orcaUserSystemctl(ctx, cb, user, home, "enable", "--now", "orca-update.timer")
		cb("✓ Orca ADE diatur ke mode Headless Server (systemd user, auto-start saat boot).")
		return nil
	}
	return fmt.Errorf("mode Orca tidak valid: %s", mode)
}

// extractOrcaDeb mengekstrak paket .deb memakai dpkg-deb, dengan fallback ar+tar.
func (inst *Installer) extractOrcaDeb(ctx context.Context, cb LogCallback, debPath, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	if _, err := exec.LookPath("dpkg-deb"); err == nil {
		return inst.runCmd(ctx, cb, "dpkg-deb", "-x", debPath, destDir)
	}
	tmpDir, err := os.MkdirTemp("", "orca-deb-ar-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	cmd := exec.CommandContext(ctx, "ar", "-x", debPath)
	cmd.Dir = tmpDir
	out, err := cmd.CombinedOutput()
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" && cb != nil {
			cb(line)
		}
	}
	if err != nil {
		return fmt.Errorf("gagal mengekstrak arsip .deb: %w", err)
	}
	dataArchives, _ := filepath.Glob(filepath.Join(tmpDir, "data.tar.*"))
	if len(dataArchives) == 0 {
		return fmt.Errorf("data.tar.* tidak ditemukan di dalam %s", debPath)
	}
	return inst.runCmd(ctx, cb, "tar", "-xf", dataArchives[0], "-C", destDir)
}

// installOrcaDeb memasang atau memperbarui Orca ADE dari paket .deb resmi.
// Dipakai untuk seluruh distro Debian/Ubuntu (fokus utama wdev).
func (inst *Installer) installOrcaDeb(ctx context.Context, cb LogCallback, mode string) error {
	user, home := ResolveOrcaUser()
	if home == "" {
		return fmt.Errorf("tidak dapat menentukan home direktori user target")
	}
	if mode != "desktop" && mode != "headless" {
		mode = readOrcaMode(home)
	}

	cb("==> Memeriksa dan memperbarui Orca ADE (rilis resmi GitHub stablyai/orca)...")
	tag, assets, err := fetchGitHubLatestRelease(ctx, orcaGitHubRepo)
	if err != nil {
		return fmt.Errorf("gagal mengambil rilis Orca terbaru dari GitHub: %w", err)
	}
	latest := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if latest == "" {
		return fmt.Errorf("tag rilis Orca tidak ditemukan")
	}

	installDir := orcaInstallDir(home)
	current := orcaBinaryVersion(ctx, home, installDir)
	cb(fmt.Sprintf("Versi terpasang : %s", orcaVersionOrUnknown(current)))
	cb(fmt.Sprintf("Versi terbaru   : %s", latest))

	if current == latest {
		cb("✓ Orca ADE sudah berada di versi paling mutakhir.")
		removeLegacyOrcaShellHelpers(home, user)
		return applyOrcaMode(ctx, cb, user, home, mode)
	}

	arch := "amd64"
	if runtime.GOARCH == "arm64" {
		arch = "arm64"
	}
	assetName := fmt.Sprintf("orca-ide_%s_%s.deb", latest, arch)
	downloadURL := ""
	for _, a := range assets {
		if a.Name == assetName {
			downloadURL = a.URL
			break
		}
	}
	if downloadURL == "" {
		downloadURL = fmt.Sprintf("https://github.com/stablyai/orca/releases/download/v%s/%s", latest, assetName)
	}

	if err := os.MkdirAll(filepath.Dir(installDir), 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(filepath.Dir(installDir), ".orca-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	debPath := filepath.Join(staging, assetName)
	cb(fmt.Sprintf("Mengunduh pembaruan dari: %s", downloadURL))
	if err := downloadFileCtx(ctx, downloadURL, debPath); err != nil {
		return fmt.Errorf("gagal mengunduh paket Orca: %w", err)
	}

	extractDir := filepath.Join(staging, "extracted")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return err
	}
	cb("Mengekstrak paket...")
	if err := inst.extractOrcaDeb(ctx, cb, debPath, extractDir); err != nil {
		return fmt.Errorf("gagal mengekstrak paket Orca: %w", err)
	}
	_ = os.Remove(debPath)
	appDir := findExtractedOrcaDir(extractDir)
	if appDir == "" || findOrcaBinary(appDir) == "" {
		return fmt.Errorf("biner orca-ide tidak ditemukan di dalam paket %s", assetName)
	}
	chownPath(user, staging)

	stopOrcaService(ctx, cb, user, home)

	backup := installDir + ".old"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(installDir); err == nil {
		if err := os.Rename(installDir, backup); err != nil {
			return fmt.Errorf("gagal menyiapkan penggantian instalasi lama: %w", err)
		}
	}
	if err := os.Rename(appDir, installDir); err != nil {
		_ = os.RemoveAll(installDir)
		_ = os.Rename(backup, installDir)
		return fmt.Errorf("gagal menerapkan pembaruan Orca: %w", err)
	}
	_ = os.RemoveAll(backup)
	chownPath(user, installDir)
	_ = os.Chmod(findOrcaBinary(installDir), 0o755)

	removeLegacyOrcaShellHelpers(home, user)
	if mode == "desktop" {
		ensureOrcaDesktopEntry(user, home, extractDir)
	}

	applied := orcaBinaryVersion(ctx, home, installDir)
	if applied == "" {
		applied = latest
	}
	cb(fmt.Sprintf("✓ Pembaruan Orca ADE berhasil diterapkan: v%s", applied))
	SetOrcaVersionCache(applied)

	if err := applyOrcaMode(ctx, cb, user, home, mode); err != nil {
		cb(fmt.Sprintf("[WARN] Pembaruan terpasang, tetapi layanan gagal diaktifkan ulang: %v", err))
		cb("       Jalankan manual: orca switch " + mode)
		return nil
	}
	return nil
}

// UpdateOrca memperbarui Orca ADE yang sudah terpasang (jalur .deb Debian).
func (inst *Installer) UpdateOrca(ctx context.Context, cb LogCallback) error {
	if !inst.isDebian {
		return fmt.Errorf("pembaruan Orca via .deb hanya didukung pada distro Debian/Ubuntu")
	}
	_, home := ResolveOrcaUser()
	if home == "" {
		return fmt.Errorf("tidak dapat menentukan home direktori user target")
	}
	if _, err := os.Stat(orcaInstallDir(home)); err != nil {
		cb("[INFO] Instalasi Orca ADE belum ada, menjalankan pemasangan baru...")
		return inst.installOrcaDeb(ctx, cb, "")
	}
	return inst.installOrcaDeb(ctx, cb, "")
}

// SetOrcaMode mengalihkan mode operasi Orca ADE (desktop/headless) untuk user target.
func (inst *Installer) SetOrcaMode(ctx context.Context, cb LogCallback, mode string) error {
	if mode != "desktop" && mode != "headless" {
		return fmt.Errorf("mode Orca tidak valid: %s", mode)
	}
	user, home := ResolveOrcaUser()
	if home == "" {
		return fmt.Errorf("tidak dapat menentukan home direktori user target")
	}
	return applyOrcaMode(ctx, cb, user, home, mode)
}

// downloadFileCtx mengunduh berkas dengan dukungan pembatalan konteks.
func downloadFileCtx(ctx context.Context, url, dstPath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
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

// ensureOrcaDesktopEntry memasang menu launcher & ikon untuk mode Desktop GUI.
func ensureOrcaDesktopEntry(user, home, extractDir string) {
	appsDir := filepath.Join(home, ".local", "share", "applications")
	iconsDir := filepath.Join(home, ".local", "share", "icons", "hicolor", "256x256", "apps")
	_ = os.MkdirAll(appsDir, 0o755)
	_ = os.MkdirAll(iconsDir, 0o755)

	binPath := filepath.Join(home, ".local", "lib", "orca-ide", "orca-ide")
	launcher := filepath.Join(home, ".local", "bin", "orca-desktop")
	if fi, err := os.Stat(launcher); err != nil || fi.Mode()&0o111 == 0 {
		launcher = binPath
	}
	entry := fmt.Sprintf(`[Desktop Entry]
Name=Orca
Exec=%s %%U
Terminal=false
Type=Application
Icon=orca-ide
StartupWMClass=orca
Comment=Next-gen IDE for parallel agentic development
MimeType=text/markdown;text/csv;text/tab-separated-values;x-scheme-handler/orca;
Categories=Development;
`, launcher)
	_ = os.WriteFile(filepath.Join(appsDir, "orca-ide.desktop"), []byte(entry), 0o644)

	if extractDir != "" {
		src := filepath.Join(extractDir, "usr", "share", "icons", "hicolor", "256x256", "apps", "orca-ide.png")
		if data, err := os.ReadFile(src); err == nil {
			_ = os.WriteFile(filepath.Join(iconsDir, "orca-ide.png"), data, 0o644)
		}
	}
	chownPath(user, filepath.Join(home, ".local", "share"))
}

func orcaVersionOrUnknown(v string) string {
	if v == "" {
		return "tidak diketahui"
	}
	return v
}
