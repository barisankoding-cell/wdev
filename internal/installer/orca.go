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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var orcaInstalledVerCache struct {
	mu      sync.Mutex
	key     string
	version string
}

// ResetOrcaVersionCache mengosongkan cache versi Orca lokal (mis. setelah uninstall).
func ResetOrcaVersionCache() {
	orcaInstalledVerCache.mu.Lock()
	orcaInstalledVerCache.key = ""
	orcaInstalledVerCache.version = ""
	orcaInstalledVerCache.mu.Unlock()
	SetOrcaVersionCache("")
}

func parseOrcaVersionString(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		candidate := strings.TrimPrefix(fields[len(fields)-1], "v")
		if isSemverLike(candidate) {
			return candidate
		}
	}
	return ""
}

func isSemverLike(s string) bool {
	parts := strings.Split(strings.SplitN(s, "-", 2)[0], ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts[:2] {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func isWdevBinaryLink(path string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	base := filepath.Base(resolved)
	return base == "wdev"
}

func runOrcaVersionProbe(ctx context.Context, user, home, bin string) string {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if os.Geteuid() == 0 && user != "" && user != "root" {
		cmd = exec.CommandContext(probeCtx, "runuser", "-u", user, "--", bin, "--version")
	} else {
		cmd = exec.CommandContext(probeCtx, bin, "--version")
	}
	env := append(os.Environ(), "HOME="+home)
	if user != "" {
		env = append(env, "USER="+user, "LOGNAME="+user)
	}
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseOrcaVersionString(string(out))
}

// DetectOrcaInstalledVersion mendeteksi versi Orca ADE yang terpasang pada semua distro
// (Debian .deb ekstrak, Fedora/RHEL AppImage/RPM, maupun Headless Server tarball)
// tanpa mengeksekusi Electron AppImage sebagai root agar tidak crash.
func DetectOrcaInstalledVersion(ctx context.Context, user, home string) string {
	// 1. Paket RPM resmi bila terpasang
	if out, err := exec.CommandContext(ctx, "rpm", "-q", "--qf", "%{VERSION}", "orca-ide").Output(); err == nil {
		if ver := parseOrcaVersionString(string(out)); ver != "" {
			return ver
		}
	}

	// 2. Kandidat biner / AppImage terpasang
	var candidates []string
	if appImg := "/usr/local/lib/orca-ide/orca.AppImage"; fileExistsLocal(appImg) {
		candidates = append(candidates, appImg)
	}
	if srvBin := findOrcaServerBinary(); srvBin != "" {
		candidates = append(candidates, srvBin)
	}
	if realCLI := orcaRealCLI(home); realCLI != "" {
		candidates = append(candidates, realCLI)
	}
	for _, p := range []string{
		"/opt/Orca/resources/bin/orca-ide",
		"/opt/orca-ide/resources/bin/orca-ide",
		"/usr/local/bin/orca",
		filepath.Join(home, ".config", "orca", "cli", "bin", "orca"),
	} {
		if fileExistsLocal(p) && !isWdevBinaryLink(p) {
			candidates = append(candidates, p)
		}
	}

	for _, bin := range candidates {
		fi, err := os.Stat(bin)
		if err != nil {
			continue
		}
		cacheKey := fmt.Sprintf("%s|%d|%d", bin, fi.Size(), fi.ModTime().UnixNano())

		orcaInstalledVerCache.mu.Lock()
		if orcaInstalledVerCache.key == cacheKey && orcaInstalledVerCache.version != "" {
			cached := orcaInstalledVerCache.version
			orcaInstalledVerCache.mu.Unlock()
			return cached
		}
		orcaInstalledVerCache.mu.Unlock()

		// Cek file marker versi milik wdev bila cocok
		markerPath := filepath.Join(filepath.Dir(bin), ".wdev-version")
		if data, mErr := os.ReadFile(markerPath); mErr == nil {
			if ver := parseOrcaVersionString(string(data)); ver != "" {
				orcaInstalledVerCache.mu.Lock()
				orcaInstalledVerCache.key = cacheKey
				orcaInstalledVerCache.version = ver
				orcaInstalledVerCache.mu.Unlock()
				return ver
			}
		}

		if ver := runOrcaVersionProbe(ctx, user, home, bin); ver != "" {
			orcaInstalledVerCache.mu.Lock()
			orcaInstalledVerCache.key = cacheKey
			orcaInstalledVerCache.version = ver
			orcaInstalledVerCache.mu.Unlock()
			return ver
		}
	}
	return ""
}

func fileExistsLocal(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func findOrcaServerBinary() string {
	return findOrcaServerBinaryIn("/usr/local/lib/orca-server")
}

func findOrcaServerBinaryIn(installDir string) string {
	if _, err := os.Stat(installDir); err != nil {
		return ""
	}
	var fallback string
	var preferred string
	_ = filepath.WalkDir(installDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if name != "orca-ide" && name != "orca" {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) == "bin" {
			preferred = path
			return filepath.SkipAll
		}
		if fallback == "" {
			fallback = path
		}
		return nil
	})
	if preferred != "" {
		return preferred
	}
	return fallback
}

// IsOrcaInstalled memastikan biner Orca ADE benar-benar terpasang (bukan sekadar
// direktori konfigurasi ~/.config/orca atau symlink launcher wdev yang tersisa).
func IsOrcaInstalled(home string) bool {
	if home != "" {
		if orcaRealCLI(home) != "" || orcaAppBinary(home) != "" {
			return true
		}
	}
	if fileExistsLocal("/usr/local/lib/orca-ide/orca.AppImage") {
		return true
	}
	if findOrcaServerBinary() != "" {
		return true
	}
	for _, p := range []string{
		"/opt/Orca/resources/bin/orca-ide",
		"/opt/Orca/orca-ide",
		"/opt/orca-ide/resources/bin/orca-ide",
		"/usr/local/bin/orca",
		"/usr/local/bin/orca-ide",
		"/usr/bin/orca-ide",
	} {
		if fileExistsLocal(p) && !isWdevBinaryLink(p) {
			return true
		}
	}
	return false
}

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
	cfgDir := filepath.Dir(path)
	if err := ensureUserDir(user, home, cfgDir); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(mode+"\n"), 0o644); err != nil {
		return err
	}
	chownPath(user, cfgDir)
	return nil
}

func chownPath(user string, paths ...string) {
	if os.Geteuid() != 0 || user == "" || user == "root" {
		return
	}
	args := append([]string{"-R", user + ":"}, paths...)
	_ = exec.Command("chown", args...).Run()
}

// ensureUserDir membuat direktori di bawah home user target dan memastikan
// direktori baru (maupun induk di dalam home yang terlanjur milik root) menjadi milik user.
func ensureUserDir(user, home, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if os.Geteuid() != 0 || user == "" || user == "root" || home == "" {
		return nil
	}
	u, err := osuser.Lookup(user)
	if err != nil {
		return nil
	}
	uid, uErr := strconv.Atoi(u.Uid)
	gid, gErr := strconv.Atoi(u.Gid)
	if uErr != nil || gErr != nil {
		return nil
	}
	cleanHome := filepath.Clean(home)
	cur := filepath.Clean(dir)
	for cur != cleanHome && strings.HasPrefix(cur, cleanHome+string(filepath.Separator)) {
		if fi, statErr := os.Lstat(cur); statErr == nil {
			if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Uid == 0 {
				_ = os.Lchown(cur, uid, gid)
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return nil
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

func orcaBinaryVersion(ctx context.Context, user, home, appDir string) string {
	bin := findOrcaBinary(appDir)
	if bin == "" {
		return ""
	}
	if data, err := os.ReadFile(filepath.Join(filepath.Dir(bin), ".wdev-version")); err == nil {
		if ver := parseOrcaVersionString(string(data)); ver != "" {
			return ver
		}
	}
	return runOrcaVersionProbe(ctx, user, home, bin)
}

func userSystemctlEnv(user, home string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HOME=") ||
			strings.HasPrefix(kv, "USER=") ||
			strings.HasPrefix(kv, "LOGNAME=") ||
			strings.HasPrefix(kv, "XDG_RUNTIME_DIR=") ||
			strings.HasPrefix(kv, "DBUS_SESSION_BUS_ADDRESS=") {
			continue
		}
		env = append(env, kv)
	}
	if home != "" {
		env = append(env, "HOME="+home)
	}
	if user != "" {
		env = append(env, "USER="+user, "LOGNAME="+user)
		if u, err := osuser.Lookup(user); err == nil {
			runtimeDir := "/run/user/" + u.Uid
			if fi, statErr := os.Stat(runtimeDir); statErr == nil && fi.IsDir() {
				env = append(env, "XDG_RUNTIME_DIR="+runtimeDir)
				if _, busErr := os.Stat(filepath.Join(runtimeDir, "bus")); busErr == nil {
					env = append(env, "DBUS_SESSION_BUS_ADDRESS=unix:path="+filepath.Join(runtimeDir, "bus"))
				}
			}
		}
	}
	return env
}

// ensureUserSystemdSession mengaktifkan linger dan memastikan systemd user bus
// milik user target aktif sebelum menjalankan systemctl --user.
func ensureUserSystemdSession(ctx context.Context, cb LogCallback, user string) {
	if os.Geteuid() != 0 || user == "" || user == "root" {
		return
	}
	linger := exec.CommandContext(ctx, "loginctl", "enable-linger", user)
	if out, err := linger.CombinedOutput(); err != nil && cb != nil {
		cb(fmt.Sprintf("[WARN] loginctl enable-linger %s: %v (%s)", user, err, strings.TrimSpace(string(out))))
	}
	u, err := osuser.Lookup(user)
	if err != nil || u.Uid == "" {
		return
	}
	busPath := filepath.Join("/run/user", u.Uid, "bus")
	if _, statErr := os.Stat(busPath); statErr == nil {
		return
	}
	_ = exec.CommandContext(ctx, "systemctl", "start", "user@"+u.Uid+".service").Run()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, statErr := os.Stat(busPath); statErr == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
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
	cmd.Env = userSystemctlEnv(user, home)
	out, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" && cb != nil {
			cb(line)
		}
	}
	if err != nil {
		if outStr != "" {
			return fmt.Errorf("%s %s gagal: %s", name, strings.Join(args, " "), outStr)
		}
		return err
	}
	return nil
}

func orcaUserUnitExists(home string) bool {
	_, err := os.Stat(filepath.Join(home, ".config", "systemd", "user", "orca.service"))
	return err == nil
}

func orcaSystemUnitExists() bool {
	for _, p := range []string{
		"/etc/systemd/system/orca.service",
		"/usr/lib/systemd/system/orca.service",
		"/lib/systemd/system/orca.service",
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func isSystemOrcaServiceActive(ctx context.Context) bool {
	return exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "orca.service").Run() == nil
}

func stopOrcaService(ctx context.Context, cb LogCallback, user, home string) {
	if orcaUserUnitExists(home) {
		_ = runAsUser(ctx, cb, user, home, "systemctl", "--user", "stop", "orca.service")
	}
	if orcaSystemUnitExists() {
		_ = exec.CommandContext(ctx, "systemctl", "stop", "orca.service").Run()
	}
}

func startOrcaService(ctx context.Context, cb LogCallback, user, home string, useUserUnit bool) error {
	if useUserUnit {
		ensureUserSystemdSession(ctx, cb, user)
		return orcaUserSystemctl(ctx, cb, user, home, "start", "orca.service")
	}
	return exec.CommandContext(ctx, "systemctl", "start", "orca.service").Run()
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
	if err := ensureUserDir(user, home, binDir); err != nil {
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

func ensureOrcaUserServiceUnit(user, home string) error {
	if err := ensureUserDir(user, home, filepath.Join(home, ".config", "orca")); err != nil {
		return err
	}
	chownPath(user, filepath.Join(home, ".config", "orca"))

	if orcaRealCLI(home) != "" {
		if err := ensureOrcaLauncher(user, home); err != nil {
			return err
		}
	}

	binDir := filepath.Join(home, ".local", "bin")
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := ensureUserDir(user, home, unitDir); err != nil {
		return err
	}

	execBin := filepath.Join(binDir, "orca-ide")
	if !fileExistsLocal(execBin) || isWdevBinaryLink(execBin) {
		if cli := orcaRealCLI(home); cli != "" {
			execBin = cli
		}
	}

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
`, execBin, binDir, envLine)

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
	return nil
}

// applyOrcaMode mengaktifkan mode desktop/headless sepenuhnya dari Go.
func applyOrcaMode(ctx context.Context, cb LogCallback, user, home, mode string) error {
	logf := func(msg string) {
		if cb != nil {
			cb(msg)
		}
	}
	if !IsOrcaInstalled(home) {
		return fmt.Errorf("Orca ADE belum terpasang; pasang terlebih dahulu melalui Setup Headless atau Setup Desktop")
	}
	if err := writeOrcaMode(home, user, mode); err != nil {
		return err
	}
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := ensureUserDir(user, home, unitDir); err != nil {
		return err
	}

	switch mode {
	case "desktop":
		logf("==> Menghentikan layanan Orca Headless...")
		if orcaUserUnitExists(home) {
			_ = orcaUserSystemctl(ctx, cb, user, home, "disable", "--now", "orca.service")
			_ = orcaUserSystemctl(ctx, cb, user, home, "disable", "--now", "orca-update.timer")
		}
		if orcaSystemUnitExists() {
			_ = exec.CommandContext(ctx, "systemctl", "disable", "--now", "orca.service").Run()
		}
		_ = os.Remove(filepath.Join(home, ".config", "autostart", "orca-ide.desktop"))
		ensureOrcaDesktopEntry(user, home, "")
		logf("✓ Orca ADE diatur ke mode Desktop GUI.")
		return nil

	case "headless":
		if err := ensureOrcaUserServiceUnit(user, home); err != nil {
			return err
		}
		ensureUserSystemdSession(ctx, cb, user)

		_ = orcaUserSystemctl(ctx, cb, user, home, "daemon-reload")
		_ = orcaUserSystemctl(ctx, cb, user, home, "reset-failed", "orca.service")
		if err := orcaUserSystemctl(ctx, cb, user, home, "enable", "orca.service"); err != nil {
			return err
		}
		if err := orcaUserSystemctl(ctx, cb, user, home, "restart", "orca.service"); err != nil {
			return err
		}
		_ = orcaUserSystemctl(ctx, cb, user, home, "enable", "--now", "orca-update.timer")
		logf("✓ Orca ADE diatur ke mode Headless Server (systemd user, auto-start saat boot).")
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
	current := orcaBinaryVersion(ctx, user, home, installDir)
	cb(fmt.Sprintf("Versi terpasang : %s", orcaVersionOrUnknown(current)))
	cb(fmt.Sprintf("Versi terbaru   : %s", latest))

	if current == latest {
		cb("✓ Orca ADE sudah berada di versi paling mutakhir.")
		_ = ensureUserDir(user, home, filepath.Join(home, ".config", "orca"))
		chownPath(user, filepath.Join(home, ".config", "orca"))
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

	if err := ensureUserDir(user, home, filepath.Dir(installDir)); err != nil {
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
	if bin := findOrcaBinary(installDir); bin != "" {
		_ = os.Chmod(bin, 0o755)
		_ = os.WriteFile(filepath.Join(filepath.Dir(bin), ".wdev-version"), []byte(latest+"\n"), 0o644)
	}
	if appBin := orcaAppBinary(home); appBin != "" {
		_ = os.Chmod(appBin, 0o755)
	}
	chownPath(user, installDir)

	removeLegacyOrcaShellHelpers(home, user)
	if mode == "desktop" {
		ensureOrcaDesktopEntry(user, home, extractDir)
	}

	applied := orcaBinaryVersion(ctx, user, home, installDir)
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

// UpdateOrca memperbarui Orca ADE yang sudah terpasang (mendukung Debian .deb
// maupun Fedora/RHEL/Arch via RPM, AppImage, atau Headless Server tarball).
func (inst *Installer) UpdateOrca(ctx context.Context, cb LogCallback) error {
	if inst.isDebian {
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

	return inst.updateOrcaNonDebian(ctx, cb)
}

func (inst *Installer) updateOrcaNonDebian(ctx context.Context, cb LogCallback) error {
	user, home := ResolveOrcaUser()
	mode := readOrcaMode(home)
	if !fileExistsLocal("/usr/local/lib/orca-ide/orca.AppImage") && findOrcaServerBinary() != "" {
		mode = "headless"
	}

	cb("==> Memeriksa pembaruan Orca ADE (rilis resmi GitHub stablyai/orca)...")
	tag, assets, err := fetchGitHubLatestRelease(ctx, orcaGitHubRepo)
	if err != nil {
		return fmt.Errorf("gagal mengambil rilis Orca terbaru dari GitHub: %w", err)
	}
	latest := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if latest == "" {
		return fmt.Errorf("tag rilis Orca tidak ditemukan")
	}

	current := DetectOrcaInstalledVersion(ctx, user, home)
	cb(fmt.Sprintf("Versi terpasang : %s", orcaVersionOrUnknown(current)))
	cb(fmt.Sprintf("Versi terbaru   : %s", latest))

	if current != "" && !hasNewerVersion(current, latest) {
		cb("✓ Orca ADE sudah berada di versi paling mutakhir.")
		SetOrcaVersionCache(current)
		return nil
	}

	// 1. Jika terpasang sebagai paket RPM resmi
	if (inst.pkgMgr == "dnf" || inst.pkgMgr == "yum") && rpmIsInstalled(ctx, "orca-ide") {
		return inst.installOrcaRPM(ctx, cb, latest, assets)
	}

	// 2. Mode Headless Server tarball
	if mode == "headless" {
		return inst.installOrcaServerTarball(ctx, cb, latest, assets)
	}

	// 3. Mode Desktop AppImage (standar non-Debian wdev)
	return inst.installOrcaAppImage(ctx, cb, latest, assets)
}

func (inst *Installer) installOrcaRPM(ctx context.Context, cb LogCallback, latest string, assets []ghReleaseAsset) error {
	arch := "x86_64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}
	assetName := fmt.Sprintf("orca-ide-%s.%s.rpm", latest, arch)
	downloadURL := ""
	for _, a := range assets {
		if a.Name == assetName {
			downloadURL = a.URL
			break
		}
	}
	if downloadURL == "" && latest != "" {
		downloadURL = fmt.Sprintf("https://github.com/stablyai/orca/releases/download/v%s/%s", latest, assetName)
	}
	if downloadURL == "" {
		return fmt.Errorf("URL paket RPM Orca %s tidak ditemukan", assetName)
	}

	tmpRPM := filepath.Join(os.TempDir(), assetName)
	cb(fmt.Sprintf("==> Mengunduh paket RPM Orca: %s", downloadURL))
	if err := downloadFileCtx(ctx, downloadURL, tmpRPM); err != nil {
		return fmt.Errorf("gagal mengunduh paket RPM Orca: %w", err)
	}
	defer os.Remove(tmpRPM)

	if err := inst.runCmd(ctx, cb, inst.pkgMgr, "install", "-y", tmpRPM); err != nil {
		return fmt.Errorf("gagal memasang/memperbarui paket RPM Orca: %w", err)
	}
	if latest != "" {
		SetOrcaVersionCache(latest)
	}
	cb("✓ Pembaruan Orca ADE (RPM) berhasil diterapkan!")
	return nil
}

func (inst *Installer) installOrcaServerTarball(ctx context.Context, cb LogCallback, latest string, assets []ghReleaseAsset) error {
	archName := "x64"
	if runtime.GOARCH == "arm64" {
		archName = "arm64"
	}
	assetName := fmt.Sprintf("orca-server-linux-%s.tar.gz", archName)
	downloadURL := ""
	for _, a := range assets {
		if a.Name == assetName {
			downloadURL = a.URL
			break
		}
	}
	if downloadURL == "" {
		downloadURL = fmt.Sprintf("https://github.com/stablyai/orca/releases/latest/download/%s", assetName)
	}

	tarPath := filepath.Join(os.TempDir(), assetName)
	cb(fmt.Sprintf("==> Mengunduh Orca Server: %s", downloadURL))
	if err := downloadFileCtx(ctx, downloadURL, tarPath); err != nil {
		return fmt.Errorf("gagal mengunduh Orca Server: %w", err)
	}
	defer os.Remove(tarPath)

	installDir := "/usr/local/lib/orca-server"
	_ = inst.runCmd(ctx, cb, "systemctl", "stop", "orca.service")
	_ = inst.runCmd(ctx, cb, "rm", "-rf", installDir)
	_ = inst.runCmd(ctx, cb, "mkdir", "-p", installDir)
	if err := inst.runCmd(ctx, cb, "tar", "-xzf", tarPath, "-C", installDir); err != nil {
		return fmt.Errorf("gagal mengekstrak Orca Server: %w", err)
	}

	orcaBin := findOrcaServerBinary()
	if orcaBin == "" {
		return fmt.Errorf("biner 'orca-ide' atau 'orca' tidak ditemukan di dalam %s", installDir)
	}

	_ = inst.runCmd(ctx, cb, "chmod", "+x", orcaBin)
	_ = inst.runCmd(ctx, cb, "rm", "-f", "/usr/local/bin/orca", "/usr/local/bin/orca-ide")
	_ = inst.runCmd(ctx, cb, "ln", "-sfn", orcaBin, "/usr/local/bin/orca")
	_ = inst.runCmd(ctx, cb, "ln", "-sfn", orcaBin, "/usr/local/bin/orca-ide")

	svcUser, userHome := ResolveOrcaUser()
	if latest != "" {
		_ = inst.writeSystemFile(ctx, cb, filepath.Join(filepath.Dir(orcaBin), ".wdev-version"), []byte(latest+"\n"), "644")
		_ = inst.writeSystemFile(ctx, cb, filepath.Join(installDir, ".wdev-version"), []byte(latest+"\n"), "644")
		SetOrcaVersionCache(latest)
	}
	if userHome != "" {
		_ = writeOrcaMode(userHome, svcUser, "headless")
	}

	dataPath := "/var/lib/orca"
	if svcUser != "root" && userHome != "" {
		dataPath = filepath.Join(userHome, ".config", "orca")
	} else {
		svcUser = "root"
	}

	cb("==> Mengonfigurasi mode Headless Server (orca.service)...")
	serviceUnit := fmt.Sprintf(`[Unit]
Description=Orca ADE Headless Server
After=network.target

[Service]
Type=simple
User=%s
Environment=ORCA_USER_DATA_PATH=%s
ExecStart=/usr/local/bin/orca serve
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, svcUser, dataPath)
	if err := inst.writeSystemFile(ctx, cb, "/etc/systemd/system/orca.service", []byte(serviceUnit), "644"); err != nil {
		return err
	}
	_ = inst.runCmd(ctx, cb, "systemctl", "daemon-reload")
	_ = inst.runCmd(ctx, cb, "systemctl", "enable", "--now", "orca.service")
	_ = inst.runCmd(ctx, cb, "systemctl", "restart", "orca.service")

	cb("✓ Orca ADE Headless Server berhasil dipasang/diperbarui!")
	return nil
}

func (inst *Installer) installOrcaAppImage(ctx context.Context, cb LogCallback, latest string, assets []ghReleaseAsset) error {
	assetName := "orca-linux.AppImage"
	if runtime.GOARCH == "arm64" {
		assetName = "orca-linux-arm64.AppImage"
	}
	downloadURL := ""
	for _, a := range assets {
		if a.Name == assetName {
			downloadURL = a.URL
			break
		}
	}
	if downloadURL == "" {
		downloadURL = fmt.Sprintf("https://github.com/stablyai/orca/releases/latest/download/%s", assetName)
	}

	tmpApp := filepath.Join(os.TempDir(), assetName)
	cb(fmt.Sprintf("==> Mengunduh Orca Desktop AppImage: %s", downloadURL))
	if err := downloadFileCtx(ctx, downloadURL, tmpApp); err != nil {
		return fmt.Errorf("gagal mengunduh Orca AppImage: %w", err)
	}
	defer os.Remove(tmpApp)

	orcaDir := "/usr/local/lib/orca-ide"
	dstApp := filepath.Join(orcaDir, "orca.AppImage")
	_ = inst.runCmd(ctx, cb, "mkdir", "-p", orcaDir)
	if err := inst.runCmd(ctx, cb, "install", "-m", "0755", tmpApp, dstApp); err != nil {
		return err
	}
	_ = inst.runCmd(ctx, cb, "ln", "-sfn", dstApp, "/usr/local/bin/orca-ide")
	_ = inst.runCmd(ctx, cb, "ln", "-sfn", dstApp, "/usr/local/bin/orca")

	svcUser, userHome := ResolveOrcaUser()
	if userHome != "" {
		_ = writeOrcaMode(userHome, svcUser, "desktop")
	}

	ver := latest
	if ver == "" {
		ver = runOrcaVersionProbe(ctx, svcUser, userHome, dstApp)
	}
	if ver != "" {
		_ = inst.writeSystemFile(ctx, cb, filepath.Join(orcaDir, ".wdev-version"), []byte(ver+"\n"), "644")
		SetOrcaVersionCache(ver)
	}

	cb("✓ Orca ADE Desktop (AppImage) berhasil dipasang/diperbarui di " + dstApp)
	return nil
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
	if !IsOrcaInstalled(home) {
		return fmt.Errorf("Orca ADE belum terpasang; pasang terlebih dahulu melalui Setup Headless atau Setup Desktop")
	}
	return applyOrcaMode(ctx, cb, user, home, mode)
}

// ControlOrcaService mengontrol layanan Orca ADE (start/stop/restart/reload)
// baik yang berjalan sebagai systemd user service (Debian .deb) maupun
// systemd system service (/etc/systemd/system/orca.service).
func ControlOrcaService(ctx context.Context, action string) error {
	switch action {
	case "start", "stop", "restart", "reload":
	default:
		return fmt.Errorf("aksi tidak valid: %s", action)
	}
	effectiveAction := action
	if effectiveAction == "reload" {
		effectiveAction = "restart"
	}

	user, home := ResolveOrcaUser()

	// 1. Jika terpasang sebagai unit systemd sistem (tarball server non-Debian)
	if orcaSystemUnitExists() {
		cmd := exec.CommandContext(ctx, "systemctl", effectiveAction, "orca.service")
		if os.Geteuid() != 0 {
			cmd = exec.CommandContext(ctx, "sudo", "systemctl", effectiveAction, "orca.service")
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemctl %s orca.service gagal: %s", effectiveAction, strings.TrimSpace(string(out)))
		}
		return nil
	}

	// 2. Unit systemd user (jalur utama Debian/Ubuntu)
	if !IsOrcaInstalled(home) {
		if effectiveAction == "stop" {
			if home != "" && orcaUserUnitExists(home) {
				_ = orcaUserSystemctl(ctx, nil, user, home, "stop", "orca.service")
			}
			return nil
		}
		return fmt.Errorf("Orca ADE belum terpasang; pasang terlebih dahulu melalui Setup Headless atau Setup Desktop")
	}

	if home == "" {
		return fmt.Errorf("tidak dapat menentukan direktori home user target Orca")
	}

	if effectiveAction == "stop" {
		if !orcaUserUnitExists(home) {
			return nil
		}
		return orcaUserSystemctl(ctx, nil, user, home, "stop", "orca.service")
	}

	// Pastikan unit user, launcher, kepemilikan direktori, dan sesi systemd user siap
	if err := ensureOrcaUserServiceUnit(user, home); err != nil {
		return fmt.Errorf("gagal menyiapkan unit systemd user orca.service: %w", err)
	}
	ensureUserSystemdSession(ctx, nil, user)
	_ = orcaUserSystemctl(ctx, nil, user, home, "daemon-reload")
	_ = orcaUserSystemctl(ctx, nil, user, home, "reset-failed", "orca.service")
	_ = orcaUserSystemctl(ctx, nil, user, home, "enable", "orca.service")
	return orcaUserSystemctl(ctx, nil, user, home, effectiveAction, "orca.service")
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
	_ = ensureUserDir(user, home, appsDir)
	_ = ensureUserDir(user, home, iconsDir)

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
