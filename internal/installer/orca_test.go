package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func withIsolatedOrcaHost(t *testing.T) {
	t.Helper()
	origAppImg := orcaAppImagePath
	origGlobalBin := orcaGlobalBinDir
	origServerDir := orcaServerDir
	origIsDeb := orcaIsDebianHostFn
	origSysExists := orcaSystemFileExistsFn
	origSrvBin := orcaServerBinaryFn
	origRPM := orcaRPMInstalledFn
	origDesktopRun := orcaDesktopRunningFn
	origForceQuit := forceQuitOrcaDesktopFn
	origRelaunch := relaunchOrcaDesktopFn
	origResolveUser := resolveOrcaUserFn
	origUserUnit := orcaUserUnitExistsFn
	origSysUnit := orcaSysUnitExistsFn
	origFetch := fetchOrcaReleaseFn
	origDownload := downloadOrcaFileFn
	origExtract := extractOrcaDebFn
	origRPMInstall := runOrcaRPMInstallFn
	origWaitStart := waitOrcaDesktopStartFn
	origQuerySysd := queryUserSystemdEnvFn
	origRunSysd := runOrcaSystemdRunFn
	origDetached := startDetachedOrcaFn

	t.Cleanup(func() {
		orcaAppImagePath = origAppImg
		orcaGlobalBinDir = origGlobalBin
		orcaServerDir = origServerDir
		orcaIsDebianHostFn = origIsDeb
		orcaSystemFileExistsFn = origSysExists
		orcaServerBinaryFn = origSrvBin
		orcaRPMInstalledFn = origRPM
		orcaDesktopRunningFn = origDesktopRun
		forceQuitOrcaDesktopFn = origForceQuit
		relaunchOrcaDesktopFn = origRelaunch
		resolveOrcaUserFn = origResolveUser
		orcaUserUnitExistsFn = origUserUnit
		orcaSysUnitExistsFn = origSysUnit
		fetchOrcaReleaseFn = origFetch
		downloadOrcaFileFn = origDownload
		extractOrcaDebFn = origExtract
		runOrcaRPMInstallFn = origRPMInstall
		waitOrcaDesktopStartFn = origWaitStart
		queryUserSystemdEnvFn = origQuerySysd
		runOrcaSystemdRunFn = origRunSysd
		startDetachedOrcaFn = origDetached
		ResetOrcaVersionCache()
	})

	ResetOrcaVersionCache()
	tmpSys := t.TempDir()
	orcaAppImagePath = filepath.Join(tmpSys, "lib", "orca-ide", "orca.AppImage")
	orcaGlobalBinDir = filepath.Join(tmpSys, "bin")
	orcaServerDir = filepath.Join(tmpSys, "lib", "orca-server")
	_ = os.MkdirAll(orcaGlobalBinDir, 0o755)
	orcaIsDebianHostFn = func() bool { return true }
	orcaSystemFileExistsFn = func(p string) bool {
		if p == orcaAppImagePath || strings.HasPrefix(p, tmpSys) {
			return fileExistsLocal(p)
		}
		return false
	}
	orcaServerBinaryFn = func() string { return "" }
	orcaRPMInstalledFn = func(context.Context, string) bool { return false }
	orcaDesktopRunningFn = func(context.Context, string) bool { return false }
	orcaUserUnitExistsFn = func(string) bool { return false }
	orcaSysUnitExistsFn = func() bool { return false }
	waitOrcaDesktopStartFn = func(context.Context, string, time.Duration) bool { return true }
	queryUserSystemdEnvFn = func(context.Context, string, string) map[string]string { return nil }
}

func TestFindExtractedOrcaDir(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "opt", "Orca")
	if err := os.MkdirAll(filepath.Join(app, "resources", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "resources", "bin", "orca-ide"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := findExtractedOrcaDir(root)
	if got != app {
		t.Fatalf("findExtractedOrcaDir = %q, want %q", got, app)
	}
	if bin := findOrcaBinary(got); bin != filepath.Join(app, "resources", "bin", "orca-ide") {
		t.Fatalf("findOrcaBinary = %q", bin)
	}
}

func TestWriteAndReadOrcaMode(t *testing.T) {
	home := t.TempDir()
	if got := readOrcaMode(home); got != "headless" {
		t.Fatalf("default mode = %q, want headless", got)
	}
	if err := writeOrcaMode(home, "", "desktop"); err != nil {
		t.Fatal(err)
	}
	if got := readOrcaMode(home); got != "desktop" {
		t.Fatalf("mode = %q, want desktop", got)
	}
}

func TestParseOrcaVersionString(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"1.4.264\n", "1.4.264"},
		{"v1.4.266", "1.4.266"},
		{"orca-ide 1.4.264", "1.4.264"},
		{"Warning: something\n1.4.264\n", "1.4.264"},
		{"error: failed", ""},
	}
	for _, tc := range cases {
		if got := parseOrcaVersionString(tc.in); got != tc.want {
			t.Fatalf("parseOrcaVersionString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if !HasNewerVersion("1.4.264", "1.4.266") {
		t.Fatal("expected 1.4.266 > 1.4.264")
	}
	if HasNewerVersion("1.4.266", "1.4.266") {
		t.Fatal("expected equal versions not to report update")
	}
}

func TestIsOrcaInstalledIgnoresConfigDirOnly(t *testing.T) {
	withIsolatedOrcaHost(t)
	home := t.TempDir()
	// Hanya direktori konfigurasi ~/.config/orca (sisa setelah uninstall) tidak boleh dianggap terpasang.
	cfgDir := filepath.Join(home, ".config", "orca")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, ".mode"), []byte("headless\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsOrcaInstalled(home) {
		t.Fatalf("IsOrcaInstalled(%q) = true saat hanya .config/orca ada; seharusnya false", home)
	}

	// Setelah biner asli ~/.local/lib/orca-ide/resources/bin/orca-ide ada, baru terdeteksi terpasang.
	cliDir := filepath.Join(home, ".local", "lib", "orca-ide", "resources", "bin")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cliDir, "orca-ide"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !IsOrcaInstalled(home) {
		t.Fatalf("IsOrcaInstalled(%q) = false padahal biner CLI resmi ada", home)
	}
}

func TestFindOrcaServerBinaryInSupportsOrcaIDEAndOrca(t *testing.T) {
	dir1 := t.TempDir()
	bin1 := filepath.Join(dir1, "orca-server-1.4.272-linux-x64", "bin", "orca-ide")
	if err := os.MkdirAll(filepath.Dir(bin1), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin1, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findOrcaServerBinaryIn(dir1); got != bin1 {
		t.Fatalf("findOrcaServerBinaryIn = %q, want %q", got, bin1)
	}

	dir2 := t.TempDir()
	bin2 := filepath.Join(dir2, "bin", "orca")
	if err := os.MkdirAll(filepath.Dir(bin2), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin2, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findOrcaServerBinaryIn(dir2); got != bin2 {
		t.Fatalf("findOrcaServerBinaryIn = %q, want %q", got, bin2)
	}
}

func TestApplyOrcaModeRejectsUninstalledOrca(t *testing.T) {
	withIsolatedOrcaHost(t)
	home := t.TempDir()
	err := applyOrcaMode(context.Background(), nil, "", home, "headless")
	if err == nil {
		t.Fatal("applyOrcaMode seharusnya gagal bila biner Orca belum terpasang")
	}
	if _, statErr := os.Stat(filepath.Join(home, ".config", "systemd", "user", "orca.service")); statErr == nil {
		t.Fatal("orca.service tidak boleh dibuat bila biner Orca belum terpasang")
	}
}

func TestEnsureOrcaUserServiceUnitWritesValidUnit(t *testing.T) {
	withIsolatedOrcaHost(t)
	home := t.TempDir()
	cliDir := filepath.Join(home, ".local", "lib", "orca-ide", "resources", "bin")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cliDir, "orca-ide"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureOrcaUserServiceUnit("", home); err != nil {
		t.Fatalf("ensureOrcaUserServiceUnit error: %v", err)
	}
	unitBytes, err := os.ReadFile(filepath.Join(home, ".config", "systemd", "user", "orca.service"))
	if err != nil {
		t.Fatalf("gagal membaca unit orca.service: %v", err)
	}
	wantExec := "ExecStart=" + filepath.Join(home, ".local", "bin", "orca-ide") + " serve --supervise --force"
	if !strings.Contains(string(unitBytes), wantExec) {
		t.Fatalf("isi orca.service tidak memuat %q:\n%s", wantExec, string(unitBytes))
	}
}

func TestUserSystemctlEnvDeduplicatesHomeAndUser(t *testing.T) {
	t.Setenv("HOME", "/root")
	t.Setenv("USER", "root")
	env := userSystemctlEnv("tester", "/home/tester")
	homeCount := 0
	userCount := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOME=") {
			homeCount++
			if kv != "HOME=/home/tester" {
				t.Fatalf("unexpected HOME entry: %s", kv)
			}
		}
		if strings.HasPrefix(kv, "USER=") {
			userCount++
			if kv != "USER=tester" {
				t.Fatalf("unexpected USER entry: %s", kv)
			}
		}
	}
	if homeCount != 1 || userCount != 1 {
		t.Fatalf("expected single HOME and USER entries, got home=%d user=%d", homeCount, userCount)
	}
}

func writeFakeOrcaUserInstall(t *testing.T, home, version string) {
	t.Helper()
	appDir := orcaInstallDir(home)
	binDir := filepath.Join(appDir, "resources", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "orca-ide"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "orca-ide"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if version != "" {
		if err := os.WriteFile(filepath.Join(binDir, ".wdev-version"), []byte(version+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOrcaBinaryResolutionDebianVsNonDebian(t *testing.T) {
	withIsolatedOrcaHost(t)
	home := t.TempDir()
	writeFakeOrcaUserInstall(t, home, "1.4.261")

	if err := os.MkdirAll(filepath.Dir(orcaAppImagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orcaAppImagePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(orcaAppImagePath), ".wdev-version"), []byte("1.4.274\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Debian prefers ~/.local/lib/orca-ide
	orcaIsDebianHostFn = func() bool { return true }
	if got := orcaAppBinary(home); got != filepath.Join(orcaInstallDir(home), "orca-ide") {
		t.Fatalf("debian orcaAppBinary = %q", got)
	}
	if got := orcaRealCLI(home); got != filepath.Join(orcaInstallDir(home), "resources", "bin", "orca-ide") {
		t.Fatalf("debian orcaRealCLI = %q", got)
	}
	if ver := DetectOrcaInstalledVersion(context.Background(), "", home); ver != "1.4.261" {
		t.Fatalf("debian DetectOrcaInstalledVersion = %q, want 1.4.261", ver)
	}

	// Non-Debian prefers AppImage over stale ~/.local/lib/orca-ide
	orcaIsDebianHostFn = func() bool { return false }
	if got := orcaAppBinary(home); got != orcaAppImagePath {
		t.Fatalf("non-debian orcaAppBinary = %q, want %q", got, orcaAppImagePath)
	}
	if got := orcaRealCLI(home); got != orcaAppImagePath {
		t.Fatalf("non-debian orcaRealCLI = %q, want %q", got, orcaAppImagePath)
	}
	if ver := DetectOrcaInstalledVersion(context.Background(), "", home); ver != "1.4.274" {
		t.Fatalf("non-debian DetectOrcaInstalledVersion = %q, want 1.4.274", ver)
	}
}

func setupFakeDebUpdateHooks(t *testing.T, home, currentVer, latestVer string) {
	t.Helper()
	if currentVer != "" {
		writeFakeOrcaUserInstall(t, home, currentVer)
	}
	resolveOrcaUserFn = func() (string, string) { return "", home }
	fetchOrcaReleaseFn = func(context.Context, string) (string, []ghReleaseAsset, error) {
		return "v" + latestVer, nil, nil
	}
	downloadOrcaFileFn = func(_ context.Context, _ string, dstPath string) error {
		return os.WriteFile(dstPath, []byte("fake-deb"), 0o644)
	}
	extractOrcaDebFn = func(_ *Installer, _ context.Context, _ LogCallback, _ string, destDir string) error {
		appDir := filepath.Join(destDir, "opt", "Orca")
		binDir := filepath.Join(appDir, "resources", "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(appDir, "orca-ide"), []byte("#!/bin/sh\necho "+latestVer+"\n"), 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(binDir, "orca-ide"), []byte("#!/bin/sh\necho "+latestVer+"\n"), 0o755)
	}
}

func TestInstallOrcaDebDesktopForceClosesAndReopensWhenRunning(t *testing.T) {
	withIsolatedOrcaHost(t)
	home := t.TempDir()
	if err := writeOrcaMode(home, "", "desktop"); err != nil {
		t.Fatal(err)
	}
	setupFakeDebUpdateHooks(t, home, "1.4.261", "1.4.274")

	running := true
	var events []string
	orcaDesktopRunningFn = func(context.Context, string) bool { return running }
	forceQuitOrcaDesktopFn = func(_ context.Context, _ LogCallback, _ string, _ string) {
		events = append(events, "force-quit")
		running = false
	}
	relaunchOrcaDesktopFn = func(_ context.Context, _ LogCallback, _ string, gotHome string) error {
		if running {
			t.Fatalf("relaunch called while desktop still running")
		}
		if gotHome != home {
			t.Fatalf("relaunch home = %q, want %q", gotHome, home)
		}
		events = append(events, "relaunch")
		running = true
		return nil
	}

	inst := &Installer{isDebian: true, pkgMgr: "apt-get"}
	if err := inst.UpdateOrca(context.Background(), func(string) {}); err != nil {
		t.Fatalf("UpdateOrca error: %v", err)
	}
	if !slices.Equal(events, []string{"force-quit", "relaunch"}) {
		t.Fatalf("events = %v, want [force-quit relaunch]", events)
	}
	if got := orcaBinaryVersion(context.Background(), "", home, orcaInstallDir(home)); got != "1.4.274" {
		t.Fatalf("installed version = %q, want 1.4.274", got)
	}
}

func TestInstallOrcaDebDesktopSkipsForceCloseAndReopenWhenNotRunning(t *testing.T) {
	withIsolatedOrcaHost(t)
	home := t.TempDir()
	if err := writeOrcaMode(home, "", "desktop"); err != nil {
		t.Fatal(err)
	}
	setupFakeDebUpdateHooks(t, home, "1.4.261", "1.4.274")

	forceQuitCalls := 0
	relaunchCalls := 0
	orcaDesktopRunningFn = func(context.Context, string) bool { return false }
	forceQuitOrcaDesktopFn = func(context.Context, LogCallback, string, string) { forceQuitCalls++ }
	relaunchOrcaDesktopFn = func(context.Context, LogCallback, string, string) error {
		relaunchCalls++
		return nil
	}

	inst := &Installer{isDebian: true, pkgMgr: "apt-get"}
	if err := inst.UpdateOrca(context.Background(), func(string) {}); err != nil {
		t.Fatalf("UpdateOrca error: %v", err)
	}
	if forceQuitCalls != 0 || relaunchCalls != 0 {
		t.Fatalf("expected no force-quit/relaunch when not running, got quit=%d relaunch=%d", forceQuitCalls, relaunchCalls)
	}
}

func TestInstallOrcaDebSkipsForceCloseWhenAlreadyLatest(t *testing.T) {
	withIsolatedOrcaHost(t)
	home := t.TempDir()
	if err := writeOrcaMode(home, "", "desktop"); err != nil {
		t.Fatal(err)
	}
	setupFakeDebUpdateHooks(t, home, "1.4.274", "1.4.274")

	forceQuitCalls := 0
	relaunchCalls := 0
	orcaDesktopRunningFn = func(context.Context, string) bool { return true }
	forceQuitOrcaDesktopFn = func(context.Context, LogCallback, string, string) { forceQuitCalls++ }
	relaunchOrcaDesktopFn = func(context.Context, LogCallback, string, string) error {
		relaunchCalls++
		return nil
	}

	inst := &Installer{isDebian: true, pkgMgr: "apt-get"}
	if err := inst.UpdateOrca(context.Background(), func(string) {}); err != nil {
		t.Fatalf("UpdateOrca error: %v", err)
	}
	if forceQuitCalls != 0 || relaunchCalls != 0 {
		t.Fatalf("expected no force-quit/relaunch when already up to date, got quit=%d relaunch=%d", forceQuitCalls, relaunchCalls)
	}
}

func TestInstallOrcaDebHeadlessDoesNotRelaunchDesktop(t *testing.T) {
	withIsolatedOrcaHost(t)
	home := t.TempDir()
	if err := writeOrcaMode(home, "", "headless"); err != nil {
		t.Fatal(err)
	}
	setupFakeDebUpdateHooks(t, home, "1.4.261", "1.4.274")

	// Suppress systemctl calls in headless applyOrcaMode by overriding PATH to empty
	t.Setenv("PATH", t.TempDir())

	forceQuitCalls := 0
	relaunchCalls := 0
	orcaDesktopRunningFn = func(context.Context, string) bool { return false }
	forceQuitOrcaDesktopFn = func(context.Context, LogCallback, string, string) { forceQuitCalls++ }
	relaunchOrcaDesktopFn = func(context.Context, LogCallback, string, string) error {
		relaunchCalls++
		return nil
	}

	inst := &Installer{isDebian: true, pkgMgr: "apt-get"}
	if err := inst.UpdateOrca(context.Background(), func(string) {}); err != nil {
		t.Fatalf("UpdateOrca error: %v", err)
	}
	if forceQuitCalls != 0 || relaunchCalls != 0 {
		t.Fatalf("headless update should not force-quit or relaunch desktop, got quit=%d relaunch=%d", forceQuitCalls, relaunchCalls)
	}
}

func TestInstallOrcaAppImageForceClosesCleansStaleUserDirAndReopens(t *testing.T) {
	withIsolatedOrcaHost(t)
	orcaIsDebianHostFn = func() bool { return false }
	home := t.TempDir()
	if err := writeOrcaMode(home, "", "desktop"); err != nil {
		t.Fatal(err)
	}
	// Simulasikan instalasi ~/.local/lib/orca-ide lama beserta AppImage lama
	writeFakeOrcaUserInstall(t, home, "1.4.261")
	if err := os.MkdirAll(filepath.Dir(orcaAppImagePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orcaAppImagePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(orcaAppImagePath), ".wdev-version"), []byte("1.4.270\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolveOrcaUserFn = func() (string, string) { return "", home }
	fetchOrcaReleaseFn = func(context.Context, string) (string, []ghReleaseAsset, error) {
		return "v1.4.274", nil, nil
	}
	downloadOrcaFileFn = func(_ context.Context, _ string, dstPath string) error {
		return os.WriteFile(dstPath, []byte("#!/bin/sh\necho 1.4.274\n"), 0o755)
	}

	running := true
	var events []string
	orcaDesktopRunningFn = func(context.Context, string) bool { return running }
	forceQuitOrcaDesktopFn = func(context.Context, LogCallback, string, string) {
		events = append(events, "force-quit")
		running = false
	}
	relaunchOrcaDesktopFn = func(_ context.Context, _ LogCallback, _ string, gotHome string) error {
		if _, err := os.Stat(orcaInstallDir(gotHome)); !os.IsNotExist(err) {
			t.Fatalf("stale user install dir should be removed before relaunch")
		}
		if got := orcaAppBinary(gotHome); got != orcaAppImagePath {
			t.Fatalf("orcaAppBinary = %q, want %q", got, orcaAppImagePath)
		}
		events = append(events, "relaunch")
		return nil
	}

	inst := &Installer{isDebian: false, pkgMgr: "dnf"}
	if err := inst.UpdateOrca(context.Background(), func(string) {}); err != nil {
		t.Fatalf("UpdateOrca error: %v", err)
	}
	if !slices.Equal(events, []string{"force-quit", "relaunch"}) {
		t.Fatalf("events = %v, want [force-quit relaunch]", events)
	}
	linkTarget, err := os.Readlink(filepath.Join(home, ".local", "bin", "orca-ide"))
	if err != nil || linkTarget != orcaAppImagePath {
		t.Fatalf("~/.local/bin/orca-ide = %q (err=%v), want %q", linkTarget, err, orcaAppImagePath)
	}
}

func TestInstallOrcaRPMDesktopForceClosesAndReopensWhenRunning(t *testing.T) {
	withIsolatedOrcaHost(t)
	orcaIsDebianHostFn = func() bool { return false }
	orcaRPMInstalledFn = func(context.Context, string) bool { return true }
	home := t.TempDir()
	if err := writeOrcaMode(home, "", "desktop"); err != nil {
		t.Fatal(err)
	}

	resolveOrcaUserFn = func() (string, string) { return "", home }
	downloadOrcaFileFn = func(_ context.Context, _ string, dstPath string) error {
		return os.WriteFile(dstPath, []byte("fake-rpm"), 0o644)
	}
	runOrcaRPMInstallFn = func(_ *Installer, _ context.Context, _ LogCallback, _ string, _ string) error {
		return nil
	}

	running := true
	var events []string
	orcaDesktopRunningFn = func(context.Context, string) bool { return running }
	forceQuitOrcaDesktopFn = func(context.Context, LogCallback, string, string) {
		events = append(events, "force-quit")
		running = false
	}
	relaunchOrcaDesktopFn = func(context.Context, LogCallback, string, string) error {
		events = append(events, "relaunch")
		return nil
	}

	inst := &Installer{isDebian: false, pkgMgr: "dnf"}
	if err := inst.installOrcaRPM(context.Background(), func(string) {}, "1.4.274", nil); err != nil {
		t.Fatalf("installOrcaRPM error: %v", err)
	}
	if !slices.Equal(events, []string{"force-quit", "relaunch"}) {
		t.Fatalf("events = %v, want [force-quit relaunch]", events)
	}
}

func TestCleanupStaleOrcaSingletonLocks(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "orca")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Lock milik PID yang masih hidup (diri sendiri) tidak boleh dihapus
	liveLock := filepath.Join(cfgDir, "SingletonLock")
	if err := os.Symlink(fmt.Sprintf("host-%d", os.Getpid()), liveLock); err != nil {
		t.Fatal(err)
	}
	cleanupStaleOrcaSingletonLocks(home)
	if _, err := os.Lstat(liveLock); err != nil {
		t.Fatalf("live SingletonLock should not be removed: %v", err)
	}

	// Lock milik PID mati harus dibersihkan beserta socket & cookie
	_ = os.Remove(liveLock)
	if err := os.Symlink("host-99999999", liveLock); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(cfgDir, "SingletonSocket")
	cookie := filepath.Join(cfgDir, "SingletonCookie")
	_ = os.WriteFile(sock, nil, 0o644)
	_ = os.WriteFile(cookie, nil, 0o644)

	cleanupStaleOrcaSingletonLocks(home)
	for _, p := range []string{liveLock, sock, cookie} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Fatalf("expected stale %s removed, err=%v", p, err)
		}
	}
}

func TestResolveOrcaDesktopGUIEnvAndSystemdRunArgs(t *testing.T) {
	withIsolatedOrcaHost(t)
	home := t.TempDir()
	queryUserSystemdEnvFn = func(context.Context, string, string) map[string]string {
		return parseSystemctlEnvOutput("DISPLAY=:1\nWAYLAND_DISPLAY=wayland-2\nXDG_CURRENT_DESKTOP=KDE\n")
	}

	envMap, _ := resolveOrcaDesktopGUIEnv(context.Background(), "", home)
	if envMap["DISPLAY"] != ":1" || envMap["WAYLAND_DISPLAY"] != "wayland-2" || envMap["XDG_CURRENT_DESKTOP"] != "KDE" {
		t.Fatalf("unexpected GUI env resolved: %v", envMap)
	}

	sdArgs := buildSystemdRunDesktopArgs("orca-desktop-1", home, "/usr/local/lib/orca-ide/orca.AppImage", []string{"--no-sandbox"}, envMap)
	joined := strings.Join(sdArgs, " ")
	for _, want := range []string{
		"--user", "--collect", "--unit=orca-desktop-1",
		"--working-directory=" + home,
		"--setenv=DISPLAY=:1",
		"--setenv=WAYLAND_DISPLAY=wayland-2",
		"-- /usr/local/lib/orca-ide/orca.AppImage --no-sandbox",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("systemd-run args missing %q: %s", want, joined)
		}
	}
}

func TestRelaunchOrcaDesktopPrefersSystemdRunAndFallsBack(t *testing.T) {
	withIsolatedOrcaHost(t)
	home := t.TempDir()
	writeFakeOrcaUserInstall(t, home, "1.4.274")

	// Buat fake dbus bus socket di XDG_RUNTIME_DIR
	rtDir := t.TempDir()
	busPath := filepath.Join(rtDir, "bus")
	if err := os.WriteFile(busPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	queryUserSystemdEnvFn = func(context.Context, string, string) map[string]string {
		return map[string]string{
			"XDG_RUNTIME_DIR":          rtDir,
			"DBUS_SESSION_BUS_ADDRESS": "unix:path=" + busPath,
			"WAYLAND_DISPLAY":          "wayland-1",
		}
	}

	sdCalled := false
	detachedCalled := false
	runOrcaSystemdRunFn = func(context.Context, string, string, string, []string, map[string]string) error {
		sdCalled = true
		return nil
	}
	startDetachedOrcaFn = func(string, string, string, []string, []string) error {
		detachedCalled = true
		return nil
	}

	if err := relaunchOrcaDesktop(context.Background(), nil, "", home); err != nil {
		t.Fatalf("relaunchOrcaDesktop error: %v", err)
	}
	if !sdCalled || detachedCalled {
		t.Fatalf("expected systemd-run preferred, got sd=%v detached=%v", sdCalled, detachedCalled)
	}

	// Bila systemd-run gagal, harus fallback ke detached spawn
	sdCalled = false
	runOrcaSystemdRunFn = func(context.Context, string, string, string, []string, map[string]string) error {
		sdCalled = true
		return fmt.Errorf("systemd-run unavailable")
	}
	if err := relaunchOrcaDesktop(context.Background(), nil, "", home); err != nil {
		t.Fatalf("relaunchOrcaDesktop fallback error: %v", err)
	}
	if !sdCalled || !detachedCalled {
		t.Fatalf("expected fallback to detached spawn, got sd=%v detached=%v", sdCalled, detachedCalled)
	}
}
