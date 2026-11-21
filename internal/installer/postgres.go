package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// supportedPGMajors daftar versi mayor PostgreSQL yang dipindai pada sistem RPM/PGDG.
var supportedPGMajors = []string{"19", "18", "17", "16", "15", "14"}

// isPGDGVendor mengembalikan true bila field Vendor RPM berasal dari repositori resmi PGDG.
func isPGDGVendor(vendor string) bool {
	v := strings.ToLower(strings.TrimSpace(vendor))
	return strings.Contains(v, "postgresql") || strings.Contains(v, "pgdg")
}

// rpmInstallUpgradePlan memisahkan paket RPM yang belum terpasang (butuh `install`)
// dan yang sudah terpasang (butuh `upgrade`). Di DNF5/Fedora, `dnf install` bersifat
// no-op bila paket sudah terpasang walau tersedia versi lebih baru.
func rpmInstallUpgradePlan(pkgs []string, installed map[string]bool) (toInstall, toUpgrade []string) {
	seen := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if installed[p] {
			toUpgrade = append(toUpgrade, p)
		} else {
			toInstall = append(toInstall, p)
		}
	}
	return toInstall, toUpgrade
}

// shouldMigrateLegacyFedoraPGData memutuskan apakah data cluster Fedora bawaan
// (/var/lib/pgsql/data) aman dipindahkan ke direktori PGDG (/var/lib/pgsql/<ver>/data).
// Hanya boleh bila direktori target belum diinisialisasi DAN versi mayor pada
// PG_VERSION cocok persis dengan versi target (kompatibel minor, mis. 16.14 -> 16.16).
func shouldMigrateLegacyFedoraPGData(legacyVersion, targetMajor string, targetInitialized bool) bool {
	if targetInitialized {
		return false
	}
	lv := strings.TrimSpace(legacyVersion)
	tm := strings.TrimSpace(targetMajor)
	return lv != "" && tm != "" && lv == tm
}

// parsePGMajorFromOutput mengambil angka versi mayor dari output `postgres --version`
// atau `psql --version` (mis. "postgres (PostgreSQL) 16.14" -> "16").
func parsePGMajorFromOutput(out string) string {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 3 {
		return ""
	}
	ver := strings.TrimPrefix(fields[2], "v")
	major := strings.SplitN(ver, ".", 2)[0]
	for _, r := range major {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return major
}

// rpmIsInstalled memeriksa apakah paket RPM sudah terpasang di sistem.
func rpmIsInstalled(ctx context.Context, pkg string) bool {
	return exec.CommandContext(ctx, "rpm", "-q", pkg).Run() == nil
}

// rpmPackageVendor mengembalikan field VENDOR dari paket RPM yang terpasang.
func rpmPackageVendor(ctx context.Context, pkg string) (vendor string, installed bool) {
	out, err := exec.CommandContext(ctx, "rpm", "-q", "--qf", "%{VENDOR}", pkg).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// installOrUpgradeRPM memasang paket RPM yang belum ada dan memperbarui paket yang
// sudah terpasang agar tombol "Perbarui" bekerja nyata di Fedora/RHEL (DNF5).
func (inst *Installer) installOrUpgradeRPM(ctx context.Context, cb LogCallback, pkgs ...string) error {
	installed := make(map[string]bool, len(pkgs))
	for _, p := range pkgs {
		installed[p] = rpmIsInstalled(ctx, p)
	}
	toInstall, toUpgrade := rpmInstallUpgradePlan(pkgs, installed)

	if len(toInstall) > 0 {
		args := append([]string{"install", "-y"}, toInstall...)
		if err := inst.runCmd(ctx, cb, inst.pkgMgr, args...); err != nil {
			return err
		}
	}
	if len(toUpgrade) > 0 {
		args := append([]string{"upgrade", "-y"}, toUpgrade...)
		if err := inst.runCmd(ctx, cb, inst.pkgMgr, args...); err != nil {
			return err
		}
	}
	return nil
}

// importPGDGGPGKey mengimpor kunci GPG resmi PGDG bila sudah tersedia di disk
// sehingga query dan upgrade DNF tidak tersangkut prompt OpenPGP.
func (inst *Installer) importPGDGGPGKey(ctx context.Context, cb LogCallback) {
	keys := []string{
		"/etc/pki/rpm-gpg/PGDG-RPM-GPG-KEY-Fedora",
		"/etc/pki/rpm-gpg/PGDG-RPM-GPG-KEY-RHEL",
	}
	for _, k := range keys {
		if _, err := os.Stat(k); err == nil {
			_ = inst.runCmd(ctx, cb, "rpm", "--import", k)
		}
	}
}

// hasPGDGRepoForMajor memeriksa apakah repo PGDG untuk versi mayor tertentu
// terpasang dan diaktifkan di /etc/yum.repos.d/pgdg-*.repo (atau tersedia di dnf).
func hasPGDGRepoForMajor(ctx context.Context, ver string) bool {
	if !rpmIsInstalled(ctx, "pgdg-fedora-repo") && !rpmIsInstalled(ctx, "pgdg-redhat-repo") {
		return false
	}
	repoID := "pgdg" + ver
	if files, err := filepath.Glob("/etc/yum.repos.d/pgdg*.repo"); err == nil {
		for _, f := range files {
			if data, rErr := os.ReadFile(f); rErr == nil {
				if repoSectionEnabled(string(data), repoID) {
					return true
				}
			}
		}
	}
	pkg := fmt.Sprintf("postgresql%s-server", ver)
	dnfBin, err := exec.LookPath("dnf")
	if err != nil {
		return false
	}
	cmd := exec.CommandContext(ctx, dnfBin, "-q", "-C", "repoquery", "--repo="+repoID, pkg)
	cmd.Stdin = strings.NewReader("n\n")
	out, qErr := cmd.Output()
	return qErr == nil && strings.TrimSpace(string(out)) != ""
}

// repoSectionEnabled memeriksa apakah section [repoID] pada file .repo RPM ada dan enabled=1.
func repoSectionEnabled(content, repoID string) bool {
	targetHeader := "[" + repoID + "]"
	inSection := false
	enabled := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if inSection {
				break
			}
			if line == targetHeader {
				inSection = true
				enabled = true // default enabled bila baris enabled tidak ditulis
			}
			continue
		}
		if inSection {
			k, v, ok := strings.Cut(line, "=")
			if ok && strings.TrimSpace(k) == "enabled" {
				enabled = strings.TrimSpace(v) == "1" || strings.EqualFold(strings.TrimSpace(v), "true")
			}
		}
	}
	return inSection && enabled
}

// privilegedFileExists memeriksa keberadaan file meski berada di dalam direktori
// berizin ketat (mis. /var/lib/pgsql milik postgres:postgres mode 0700).
func (inst *Installer) privilegedFileExists(ctx context.Context, path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	} else if os.IsNotExist(err) {
		return false
	}
	if os.Geteuid() == 0 {
		return false
	}
	return inst.runQuietSudo(ctx, "test", "-f", path) == nil
}

// privilegedReadFileTrimmed membaca isi file singkat (seperti PG_VERSION) meski
// direktori induknya hanya bisa dibaca root/postgres.
func (inst *Installer) privilegedReadFileTrimmed(ctx context.Context, path string) string {
	if data, err := os.ReadFile(path); err == nil {
		return strings.TrimSpace(string(data))
	} else if os.IsNotExist(err) || os.Geteuid() == 0 {
		return ""
	}
	out, err := inst.runQuietSudoOutput(ctx, "cat", path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (inst *Installer) runQuietSudo(ctx context.Context, name string, args ...string) error {
	_, err := inst.runQuietSudoOutput(ctx, name, args...)
	return err
}

func (inst *Installer) runQuietSudoOutput(ctx context.Context, name string, args ...string) (string, error) {
	baseCmd := filepath.Base(name)
	if os.Geteuid() == 0 {
		cmd := exec.CommandContext(ctx, name, args...)
		if baseCmd == "runuser" || baseCmd == "su" {
			cmd.Dir = "/"
		}
		out, err := cmd.Output()
		return string(out), err
	}
	pass := inst.GetSudoPassword()
	var cmd *exec.Cmd
	if pass != "" {
		sudoArgs := append([]string{"-S", "-p", "", name}, args...)
		cmd = exec.CommandContext(ctx, "sudo", sudoArgs...)
		cmd.Stdin = strings.NewReader(pass + "\n")
	} else {
		sudoArgs := append([]string{"-n", name}, args...)
		cmd = exec.CommandContext(ctx, "sudo", sudoArgs...)
	}
	if baseCmd == "runuser" || baseCmd == "su" {
		cmd.Dir = "/"
	}
	out, err := cmd.Output()
	return string(out), err
}

// detectRPMPostgresMajor mendeteksi versi mayor PostgreSQL yang aktif/terpasang
// pada sistem Fedora/RHEL.
func detectRPMPostgresMajor(ctx context.Context) string {
	for _, v := range supportedPGMajors {
		if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "postgresql-"+v).Run() == nil {
			return v
		}
	}
	if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "postgresql").Run() == nil {
		if out, err := exec.CommandContext(ctx, "/usr/bin/postgres", "--version").Output(); err == nil {
			if m := parsePGMajorFromOutput(string(out)); m != "" {
				return m
			}
		}
	}
	for _, v := range supportedPGMajors {
		if _, err := os.Stat(fmt.Sprintf("/usr/pgsql-%s/bin/postgres", v)); err == nil {
			return v
		}
	}
	if out, err := exec.CommandContext(ctx, "psql", "--version").Output(); err == nil {
		if m := parsePGMajorFromOutput(string(out)); m != "" {
			return m
		}
	}
	return "16"
}

// preparePostgresBeforeRPMUpgrade memeriksa apakah paket bawaan distro Fedora
// (postgresql<ver>-server dengan unit postgresql.service) akan tergantikan oleh
// paket PGDG saat upgrade RPM. Bila targetMajor diisi, hanya versi tersebut yang
// diperiksa; bila kosong (mis. saat RunUpdateAll), semua versi mayor dipindai.
// Unit postgresql.service dihentikan & dinonaktifkan secara bersih terlebih dulu
// agar WAL tersimpan utuh dan port 5432 kosong sebelum postgresql-<ver>.service mengambil alih.
func (inst *Installer) preparePostgresBeforeRPMUpgrade(ctx context.Context, cb LogCallback, targetMajor string) string {
	candidates := supportedPGMajors
	if targetMajor != "" {
		candidates = []string{targetMajor}
	}
	for _, v := range candidates {
		pkg := fmt.Sprintf("postgresql%s-server", v)
		vendor, installed := rpmPackageVendor(ctx, pkg)
		if !installed || isPGDGVendor(vendor) {
			continue
		}
		if !hasPGDGRepoForMajor(ctx, v) {
			continue
		}
		cb(fmt.Sprintf("Mendeteksi %s bawaan distro (vendor: %s); menyiapkan migrasi aman ke PGDG %s...", pkg, vendor, v))
		_ = inst.runCmd(ctx, cb, "systemctl", "stop", "postgresql")
		_ = inst.runCmd(ctx, cb, "systemctl", "disable", "postgresql")
		return v
	}
	return ""
}

// migrateFedoraPGDataIfNeeded memindahkan cluster data dari /var/lib/pgsql/data
// ke /var/lib/pgsql/<ver>/data bila versi mayornya identik dan target PGDG belum
// diinisialisasi, sehingga seluruh database pengguna tetap terjaga.
func (inst *Installer) migrateFedoraPGDataIfNeeded(ctx context.Context, cb LogCallback, ver string) error {
	pgdgRoot := fmt.Sprintf("/var/lib/pgsql/%s", ver)
	pgdgDataDir := filepath.Join(pgdgRoot, "data")
	pgdgMarker := filepath.Join(pgdgDataDir, "PG_VERSION")

	targetInitialized := inst.privilegedFileExists(ctx, pgdgMarker)
	legacyDataDir := "/var/lib/pgsql/data"
	legacyMarker := filepath.Join(legacyDataDir, "PG_VERSION")
	legacyVer := inst.privilegedReadFileTrimmed(ctx, legacyMarker)

	if !shouldMigrateLegacyFedoraPGData(legacyVer, ver, targetInitialized) {
		return nil
	}

	cb(fmt.Sprintf("Memigrasikan data cluster PostgreSQL %s dari %s ke %s (format minor kompatibel)...", ver, legacyDataDir, pgdgDataDir))
	_ = inst.runCmd(ctx, cb, "systemctl", "stop", "postgresql")
	if err := inst.runCmd(ctx, cb, "mkdir", "-p", pgdgRoot); err != nil {
		return fmt.Errorf("gagal membuat direktori %s: %w", pgdgRoot, err)
	}
	_ = inst.runCmd(ctx, cb, "chown", "postgres:postgres", pgdgRoot)
	_ = inst.runCmd(ctx, cb, "chmod", "700", pgdgRoot)

	// Paket RPM PGDG membuat direktori /var/lib/pgsql/<ver>/data kosong;
	// hapus dulu dengan rmdir (aman: gagal bila tidak kosong) agar `mv` tidak
	// menaruh direktori di dalam /var/lib/pgsql/<ver>/data/data.
	_ = inst.runCmd(ctx, cb, "rmdir", pgdgDataDir)

	if err := inst.runCmd(ctx, cb, "mv", legacyDataDir, pgdgDataDir); err != nil {
		return fmt.Errorf("gagal memindahkan data cluster dari %s ke %s: %w", legacyDataDir, pgdgDataDir, err)
	}
	if _, err := exec.LookPath("restorecon"); err == nil {
		_ = inst.runCmd(ctx, cb, "restorecon", "-RF", pgdgRoot)
	}
	cb(fmt.Sprintf("✓ Data cluster PostgreSQL %s berhasil dimigrasikan ke %s.", ver, pgdgDataDir))
	return nil
}

// reconcilePostgresAfterRPMUpgrade menyelaraskan unit systemd, migrasi data,
// dan restart service PostgreSQL sesudah `dnf upgrade` (mis. dari Update All).
func (inst *Installer) reconcilePostgresAfterRPMUpgrade(ctx context.Context, cb LogCallback, ver string) error {
	if ver == "" {
		return nil
	}
	_ = inst.runCmd(ctx, cb, "systemctl", "daemon-reload")
	return inst.finalizeRPMPostgresService(ctx, cb, ver)
}

const (
	pgdgLdConfLink   = "/etc/ld.so.conf.d/postgresql-pgdg-libs.conf"
	wdevPgsqlLdConf  = "/etc/wdev/pgsql-ld-system.conf"
	systemLibpqPath  = "/usr/lib64/libpq.so.5"
	wdevLdConfHeader = "# Managed by wdev: prioritaskan /usr/lib64/libpq.so.5 milik sistem agar ekstensi PHP (pgsql/pdo_pgsql) tetap kompatibel; biner PGDG sudah memiliki RUNPATH sendiri.\n"
)

// pgdgLdConfShadowsSystemLibpq mengembalikan true bila file ld.so.conf PGDG
// memuat direktori /usr/pgsql-... aktif yang menimpa /usr/lib64/libpq.so.5.
func pgdgLdConfShadowsSystemLibpq(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "/usr/pgsql-") {
			return true
		}
	}
	return false
}

// DiagnosePGDGLibpqShadow memeriksa apakah postgresql*-libs PGDG sedang menimpa
// /usr/lib64/libpq.so.5 milik sistem Fedora/RHEL (penyebab php-pgsql / pdo_pgsql
// gagal memuat simbol RHPG_18).
func DiagnosePGDGLibpqShadow() bool {
	if _, err := os.Stat(systemLibpqPath); err != nil {
		return false
	}
	data, err := os.ReadFile(pgdgLdConfLink)
	if err != nil {
		return false
	}
	return pgdgLdConfShadowsSystemLibpq(string(data))
}

// EnsureSystemLibpqPriority mencegah libpq.so.5 milik PGDG (/usr/pgsql-<ver>/lib)
// menimpa /usr/lib64/libpq.so.5 milik sistem Fedora/RHEL di ld.so.cache.
// Semua biner PGDG sudah membawa ELF RUNPATH=/usr/pgsql-<ver>/lib sehingga tetap
// menemukan libpq versinya sendiri, sementara paket sistem/Remi (php-pgsql,
// pdo_pgsql) tetap mendapatkan simbol RHPG_18 dari /usr/lib64/libpq.so.5.
// Didaftarkan via update-alternatives (prioritas 9999) agar bertahan saat upgrade PGDG.
func (inst *Installer) EnsureSystemLibpqPriority(ctx context.Context, cb LogCallback) error {
	if cb == nil {
		cb = func(string) {}
	}
	if !DiagnosePGDGLibpqShadow() {
		return nil
	}

	cb("Menyelaraskan prioritas libpq sistem (/usr/lib64/libpq.so.5) agar php-pgsql/pdo_pgsql tetap kompatibel...")
	if err := inst.writeSystemFile(ctx, cb, wdevPgsqlLdConf, []byte(wdevLdConfHeader), "644"); err != nil {
		return fmt.Errorf("gagal menulis %s: %w", wdevPgsqlLdConf, err)
	}
	if err := inst.runCmd(ctx, cb, "update-alternatives", "--install", pgdgLdConfLink, "pgsql-ld-conf", wdevPgsqlLdConf, "9999"); err != nil {
		return fmt.Errorf("gagal mendaftarkan alternatif pgsql-ld-conf: %w", err)
	}
	if err := inst.runCmd(ctx, cb, "update-alternatives", "--set", "pgsql-ld-conf", wdevPgsqlLdConf); err != nil {
		return fmt.Errorf("gagal menyetel alternatif pgsql-ld-conf: %w", err)
	}
	if err := inst.runCmd(ctx, cb, "ldconfig"); err != nil {
		return fmt.Errorf("gagal menjalankan ldconfig: %w", err)
	}
	if exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "php-fpm").Run() == nil {
		_ = inst.runCmd(ctx, cb, "systemctl", "restart", "php-fpm")
	}
	cb("✓ Prioritas libpq sistem dipulihkan (php-pgsql & biner PGDG sama-sama berjalan normal).")
	return nil
}

// finalizeRPMPostgresService memastikan data cluster siap (migrasi atau initdb),
// tmpfiles & SELinux benar, unit systemd aktif, dan server menerima koneksi.
func (inst *Installer) finalizeRPMPostgresService(ctx context.Context, cb LogCallback, ver string) error {
	if err := inst.EnsureSystemLibpqPriority(ctx, cb); err != nil {
		cb(fmt.Sprintf("[WARN] Penyelarasan prioritas libpq sistem: %v", err))
	}

	pgdgSetup := fmt.Sprintf("/usr/pgsql-%s/bin/postgresql-%s-setup", ver, ver)
	pgdgUnit := fmt.Sprintf("postgresql-%s", ver)

	if _, err := os.Stat(pgdgSetup); err == nil {
		if err := inst.migrateFedoraPGDataIfNeeded(ctx, cb, ver); err != nil {
			return err
		}
		pgDataMarker := fmt.Sprintf("/var/lib/pgsql/%s/data/PG_VERSION", ver)
		if !inst.privilegedFileExists(ctx, pgDataMarker) {
			if err := inst.runCmd(ctx, cb, pgdgSetup, "initdb"); err != nil {
				return fmt.Errorf("inisialisasi database PostgreSQL %s gagal: %w", ver, err)
			}
		}
		_ = inst.runCmd(ctx, cb, "systemd-tmpfiles", "--create")
		_ = inst.runCmd(ctx, cb, "systemctl", "stop", "postgresql")
		_ = inst.runCmd(ctx, cb, "systemctl", "disable", "postgresql")
		_ = inst.runCmd(ctx, cb, "systemctl", "daemon-reload")

		if err := inst.runCmd(ctx, cb, "systemctl", "enable", pgdgUnit); err != nil {
			return fmt.Errorf("gagal mengaktifkan service %s: %w", pgdgUnit, err)
		}
		if err := inst.runCmd(ctx, cb, "systemctl", "restart", pgdgUnit); err != nil {
			return fmt.Errorf("gagal menjalankan ulang service %s: %w", pgdgUnit, err)
		}

		// Pastikan seluruh alternatif client PGDG (psql, pg_dump, pg_dumpall, dll.) mengarah ke versi ini.
		inst.setPGDGAlternatives(ctx, cb, ver)

		if !inst.waitPostgresReady(ctx, ver) {
			return fmt.Errorf("service %s aktif tetapi belum merespons pg_isready dalam 10 detik", pgdgUnit)
		}
		inst.writePGActiveState(ctx, cb, ver)
		return nil
	}

	// Layout bawaan distro Fedora/RHEL (/usr/bin/postgresql-setup + postgresql.service)
	if !inst.privilegedFileExists(ctx, "/var/lib/pgsql/data/PG_VERSION") {
		if err := inst.runCmd(ctx, cb, "postgresql-setup", "--initdb"); err != nil {
			return fmt.Errorf("inisialisasi database PostgreSQL gagal: %w", err)
		}
	}
	_ = inst.runCmd(ctx, cb, "systemd-tmpfiles", "--create")
	if err := inst.runCmd(ctx, cb, "systemctl", "enable", "postgresql"); err != nil {
		return fmt.Errorf("gagal mengaktifkan service postgresql: %w", err)
	}
	if err := inst.runCmd(ctx, cb, "systemctl", "restart", "postgresql"); err != nil {
		return fmt.Errorf("gagal menjalankan ulang service postgresql: %w", err)
	}
	inst.writePGActiveState(ctx, cb, ver)
	return nil
}

// waitPostgresReady menunggu hingga PostgreSQL merespons pg_isready.
func (inst *Installer) waitPostgresReady(ctx context.Context, ver string) bool {
	bin := fmt.Sprintf("/usr/pgsql-%s/bin/pg_isready", ver)
	if _, err := os.Stat(bin); err != nil {
		bin = "pg_isready"
	}
	for i := 0; i < 10; i++ {
		if exec.CommandContext(ctx, bin).Run() == nil {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
	return exec.CommandContext(ctx, bin).Run() == nil
}
