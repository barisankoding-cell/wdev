package installer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Severity tingkat temuan audit.
type Severity string

const (
	SeverityOK      Severity = "ok"
	SeverityWarning Severity = "warn"
	SeverityError   Severity = "error"
)

// Finding satu temuan audit toolchain.
type Finding struct {
	Severity Severity
	Kind     string // duplicate-path | legacy-profile | path-dupe | rc-drift | distro-pkg | foreign-copy
	Tool     string
	Detail   string
	Fixable  bool
	Paths    []string // path absolut yang boleh dihapus saat perbaikan
}

func (f Finding) String() string {
	tag := "[OK]"
	switch f.Severity {
	case SeverityWarning:
		tag = "[!]"
	case SeverityError:
		tag = "[x]"
	}
	label := f.Tool
	if label == "" {
		label = f.Kind
	}
	return fmt.Sprintf(" %s %-14s %s", tag, label, f.Detail)
}

// managedTool mendeskripsikan tool yang sepenuhnya dikelola wdev.
type managedTool struct {
	Name       string
	Canonical  []string // prefix direktori kanonik (target symlink harus di sini)
	DistroPkgs []string // paket distro yang bentrok dan boleh dihapus
}

var managedTools = []managedTool{
	{Name: "node", Canonical: []string{"/usr/local/lib/nodejs/"}, DistroPkgs: []string{"nodejs", "nodejs-npm", "npm"}},
	{Name: "npm", Canonical: []string{"/usr/local/lib/nodejs/"}, DistroPkgs: []string{"nodejs", "nodejs-npm", "npm"}},
	{Name: "npx", Canonical: []string{"/usr/local/lib/nodejs/"}},
	{Name: "corepack", Canonical: []string{"/usr/local/lib/nodejs/"}},
	{Name: "go", Canonical: []string{"/usr/local/go/"}, DistroPkgs: []string{"golang", "golang-go", "golang-bin"}},
	{Name: "gofmt", Canonical: []string{"/usr/local/go/"}, DistroPkgs: []string{"golang", "golang-go", "golang-bin"}},
	{Name: "rustc", Canonical: []string{"/usr/local/cargo/"}, DistroPkgs: []string{"rustc", "cargo", "rust"}},
	{Name: "cargo", Canonical: []string{"/usr/local/cargo/"}, DistroPkgs: []string{"rustc", "cargo", "rust"}},
	{Name: "rustup", Canonical: []string{"/usr/local/cargo/"}, DistroPkgs: []string{"rustc", "cargo", "rust"}},
	{Name: "mold", Canonical: []string{"/usr/local/mold/", "/usr/bin/mold"}, DistroPkgs: []string{"mold"}},
	{Name: "flutter", Canonical: []string{"/usr/local/flutter/"}},
	{Name: "dart", Canonical: []string{"/usr/local/flutter/"}},
	{Name: "composer", Canonical: []string{"/usr/local/bin/composer"}, DistroPkgs: []string{"composer"}},
	{Name: "mkcert", Canonical: []string{"/usr/local/bin/mkcert"}, DistroPkgs: []string{"mkcert"}},
	{Name: "cloudflared", Canonical: []string{"/usr/local/bin/cloudflared", "/usr/bin/cloudflared"}, DistroPkgs: []string{"cloudflared"}},
}

// auditSearchDirs semua direktori tempat binary tool bisa muncul.
func auditSearchDirs(home string) []string {
	dirs := []string{
		"/usr/local/bin", "/usr/local/sbin",
		"/usr/bin", "/usr/sbin",
		"/bin", "/sbin",
		"/snap/bin",
		"/usr/local/go/bin", "/usr/local/cargo/bin",
		"/usr/local/flutter/bin",
		"/usr/local/lib/nodejs/current/bin",
		"/usr/local/android-sdk/platform-tools",
		"/usr/local/android-sdk/cmdline-tools/latest/bin",
	}
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, ".local", "share", "pnpm", "bin"),
		)
	}
	return dirs
}

func canonicalPrefixMatch(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasSuffix(p, ".exe") || !strings.HasSuffix(p, "/") {
			if path == p {
				return true
			}
			continue
		}
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// toolLocations mengembalikan semua lokasi nyata sebuah tool, dikelompokkan
// berdasarkan realpath agar symlink ke target identik tidak dihitung duplikat.
func toolLocations(name string, dirs []string) map[string][]string {
	realGroups := map[string][]string{}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() {
			continue
		}
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			real = p
		}
		realGroups[real] = append(realGroups[real], p)
	}
	return realGroups
}

func distroPackageInstalled(ctx context.Context, pkg string) bool {
	if _, err := os.Stat("/var/lib/dpkg/status"); err == nil {
		out, err := exec.CommandContext(ctx, "dpkg-query", "-W", "-f=${Status}", pkg).Output()
		return err == nil && strings.Contains(string(out), "install ok installed")
	}
	if _, err := os.Stat("/usr/bin/rpm"); err == nil {
		return exec.CommandContext(ctx, "rpm", "-q", pkg).Run() == nil
	}
	return false
}

// AuditToolchain memeriksa duplikat path, file profile legacy, dan drift symlink.
func (inst *Installer) AuditToolchain(ctx context.Context, cb LogCallback) []Finding {
	if cb == nil {
		cb = func(string) {}
	}
	_, home := ResolveOrcaUser()
	dirs := auditSearchDirs(home)
	var findings []Finding

	for _, tool := range managedTools {
		groups := toolLocations(tool.Name, dirs)
		if len(groups) == 0 {
			continue // tidak terpasang
		}

		var canonicalReal []string
		var foreignReal []string
		var foreignPaths []string
		for real, paths := range groups {
			if canonicalPrefixMatch(real, tool.Canonical) {
				canonicalReal = append(canonicalReal, real)
				continue
			}
			foreignReal = append(foreignReal, strings.Join(paths, ","))
			foreignPaths = append(foreignPaths, paths...)
		}
		sort.Strings(foreignPaths)
		sort.Strings(canonicalReal)
		sort.Strings(foreignReal)

		switch {
		case len(canonicalReal) > 1:
			// Lebih dari satu biner berbeda di area kanonik (mis. dua versi Node).
			findings = append(findings, Finding{
				Severity: SeverityWarning,
				Kind:     "duplicate-path",
				Tool:     tool.Name,
				Detail:   "beberapa biner berbeda di area kanonik: " + strings.Join(canonicalReal, ", "),
				Fixable:  false,
			})
		case len(canonicalReal) == 1 && len(foreignReal) > 0:
			findings = append(findings, Finding{
				Severity: SeverityWarning,
				Kind:     "duplicate-path",
				Tool:     tool.Name,
				Detail:   "kanonik=" + canonicalReal[0] + "; duplikat=" + strings.Join(foreignReal, " | "),
				Fixable:  true,
				Paths:    foreignPaths,
			})
		case len(canonicalReal) == 0 && len(foreignReal) > 0:
			findings = append(findings, Finding{
				Severity: SeverityWarning,
				Kind:     "foreign-copy",
				Tool:     tool.Name,
				Detail:   "hanya ada salinan non-kanonik: " + strings.Join(foreignReal, " | "),
				Fixable:  true,
				Paths:    foreignPaths,
			})
		}
	}

	// Paket distro hanya dicurigai bila benar-benar menghasilkan biner asing.
	suspectPkgs := map[string]bool{}
	for _, f := range findings {
		if f.Kind != "duplicate-path" && f.Kind != "foreign-copy" {
			continue
		}
		for _, tool := range managedTools {
			if tool.Name != f.Tool {
				continue
			}
			for _, pkg := range tool.DistroPkgs {
				suspectPkgs[pkg] = true
			}
		}
	}
	var pkgs []string
	for pkg := range suspectPkgs {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)
	for _, pkg := range pkgs {
		if !distroPackageInstalled(ctx, pkg) {
			continue
		}
		findings = append(findings, Finding{
			Severity: SeverityWarning,
			Kind:     "distro-pkg",
			Tool:     pkg,
			Detail:   "paket distro terpasang dan menghasilkan biner duplikat",
			Fixable:  true,
		})
	}

	// File profile legacy.
	for _, p := range legacyProfileFiles {
		if _, err := os.Stat(p); err == nil {
			findings = append(findings, Finding{
				Severity: SeverityWarning,
				Kind:     "legacy-profile",
				Tool:     filepath.Base(p),
				Detail:   p + " masih ada (bukan dari wdev)",
				Fixable:  true,
			})
		}
	}

	// Direktori toolchain user yang menambah PATH kedua.
	if home != "" {
		for _, dir := range []string{".cargo", ".rustup", ".nvm", ".pyenv"} {
			p := filepath.Join(home, dir)
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				findings = append(findings, Finding{
					Severity: SeverityWarning,
					Kind:     "path-dupe",
					Tool:     dir,
					Detail:   p + " ada di home (tidak dipakai wdev; hapus manual bila tak diperlukan)",
					Fixable:  false,
				})
			}
		}
	}

	// Unit systemd wdev: environment toolchain (RUSTUP_HOME dll.).
	if data, err := os.ReadFile(WdevServiceUnitPath); err == nil {
		missing := []string{}
		for _, kv := range toolchainServiceEnv {
			if !strings.Contains(string(data), kv[0]+"=") {
				missing = append(missing, kv[0])
			}
		}
		if len(missing) > 0 {
			findings = append(findings, Finding{
				Severity: SeverityWarning,
				Kind:     "service-env",
				Tool:     "wdev.service",
				Detail:   "Environment belum lengkap: " + strings.Join(missing, ", "),
				Fixable:  true,
			})
		}
	}

	// Permission cache Nginx (penyebab FastCGI/proxy menggantung).
	for _, line := range DiagnoseNginxCache() {
		if strings.Contains(line, "[!]") {
			findings = append(findings, Finding{
				Severity: SeverityError,
				Kind:     "nginx-cache",
				Tool:     "nginx",
				Detail:   strings.TrimSpace(line),
				Fixable:  true,
			})
		}
	}

	// Duplikat entri PATH pada profil wdev + shell rc.
	for _, line := range pathDuplicates(home) {
		findings = append(findings, Finding{
			Severity: SeverityWarning,
			Kind:     "path-dupe",
			Tool:     "PATH",
			Detail:   line,
			Fixable:  true,
		})
	}

	if len(findings) == 0 {
		findings = append(findings, Finding{
			Severity: SeverityOK,
			Kind:     "clean",
			Tool:     "toolchain",
			Detail:   "tidak ada duplikat path maupun file profile legacy",
		})
	}

	for _, f := range findings {
		cb(f.String())
	}
	return findings
}

// pathDuplicates mendeteksi direktori yang muncul lebih dari sekali pada PATH hasil
// pembacaan file profil dan shell rc.
func pathDuplicates(home string) []string {
	files := []string{envProfilePath}
	if home != "" {
		files = append(files,
			filepath.Join(home, ".bashrc"),
			filepath.Join(home, ".zshrc"),
			filepath.Join(home, ".profile"),
		)
	}
	if os.Geteuid() == 0 {
		files = append(files, "/root/.bashrc", "/root/.zshrc", "/root/.profile")
	}

	var out []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		seen := map[string]int{}
		for _, l := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(l)
			if strings.HasPrefix(trimmed, "_wdev_prepend_path ") {
				p := strings.Trim(strings.TrimPrefix(trimmed, "_wdev_prepend_path "), `"`)
				seen[p]++
			}
		}
		for p, n := range seen {
			if n > 1 {
				out = append(out, fmt.Sprintf("%s: entri %s muncul %dx", f, p, n))
			}
		}
	}
	return out
}

// RepairToolchain memperbaiki temuan yang bisa diperbaiki:
// hapus paket distro duplikat, hapus salinan asing di /usr/local/bin,
// dan bangun ulang PATH tunggal.
func (inst *Installer) RepairToolchain(ctx context.Context, cb LogCallback, findings []Finding) error {
	if cb == nil {
		cb = func(string) {}
	}
	if len(findings) == 0 {
		cb("Tidak ada temuan; toolchain sudah bersih.")
		return nil
	}

	removedPkgs := map[string]bool{}
	removedPaths := map[string]bool{}
	for _, f := range findings {
		switch f.Kind {
		case "distro-pkg":
			if removedPkgs[f.Tool] {
				continue
			}
			removedPkgs[f.Tool] = true
			cb("Menghapus paket distro duplikat: " + f.Tool)
			inst.removeDistroPackagesIfInstalled(ctx, cb, f.Tool)
		case "service-env":
			if err := inst.EnsureWdevServiceUnit(ctx, cb); err != nil {
				cb(fmt.Sprintf("[WARN] Gagal menyelaraskan unit wdev.service: %v", err))
			}
		case "nginx-cache":
			if err := inst.FixNginxCachePerm(ctx, cb); err != nil {
				cb(fmt.Sprintf("[WARN] Gagal memperbaiki cache Nginx: %v", err))
			}
		case "duplicate-path", "foreign-copy":
			for _, p := range f.Paths {
				if removedPaths[p] {
					continue
				}
				removedPaths[p] = true
				cb("Menghapus biner duplikat: " + p)
				_ = inst.runCmd(ctx, cb, "rm", "-f", p)
			}
		}
	}

	// Bersihkan salinan reguler (bukan symlink) di /usr/local/bin.
	cleaned := inst.removeForeignBinaries(ctx, cb)
	if cleaned > 0 {
		cb(fmt.Sprintf(" ✓ %d salinan biner asing dibersihkan dari /usr/local/bin", cleaned))
	}

	// Bangun ulang PATH tunggal + symlink kanonik + rc shell.
	if err := inst.SyncToolchainEnv(ctx, cb); err != nil {
		return err
	}

	cb("✓ Perbaikan toolchain selesai.")
	return nil
}

// removeForeignBinaries menghapus file reguler di /usr/local/bin untuk tool yang
// dikelola wdev bila lokasinya bukan bagian dari area kanonik.
func (inst *Installer) removeForeignBinaries(ctx context.Context, cb LogCallback) int {
	count := 0
	for _, tool := range managedTools {
		if len(tool.Canonical) == 0 {
			continue
		}
		p := filepath.Join("/usr/local/bin", tool.Name)
		fi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		// Symlink sudah ditangani SyncToolchainEnv.
		if fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			real = p
		}
		if canonicalPrefixMatch(real, tool.Canonical) {
			continue
		}
		cb("Menghapus biner asing: " + p + " (bukan dari area kanonik wdev)")
		if err := inst.runCmd(ctx, cb, "rm", "-f", p); err == nil {
			count++
		}
	}
	return count
}

// PreflightToolchain dijalankan sebelum instalasi penuh: audit saja, tanpa mengubah.
func (inst *Installer) PreflightToolchain(ctx context.Context, cb LogCallback) []Finding {
	cb("=== PRE-FLIGHT: AUDIT DUPLIKAT PATH & TOOLCHAIN ===")
	findings := inst.AuditToolchain(ctx, cb)
	needFix := 0
	for _, f := range findings {
		if f.Severity != SeverityOK {
			needFix++
		}
	}
	if needFix > 0 {
		cb(fmt.Sprintf("[i] %d potensi duplikat terdeteksi; akan dirapikan otomatis di akhir instalasi.", needFix))
		cb("    Perbaiki sekarang tanpa menunggu: wdev install audit-fix")
	}
	return findings
}

// GuardToolchainColumns dipanggil sebelum instalasi komponen tunggal agar
// duplikat yang sudah ada terdeteksi tanpa harus menjalankan full setup.
func (inst *Installer) GuardToolchainColumns(ctx context.Context, cb LogCallback, toolNames ...string) {
	if len(toolNames) == 0 {
		return
	}
	_, home := ResolveOrcaUser()
	dirs := auditSearchDirs(home)
	wanted := map[string]bool{}
	for _, n := range toolNames {
		wanted[n] = true
	}
	for _, tool := range managedTools {
		if !wanted[tool.Name] {
			continue
		}
		groups := toolLocations(tool.Name, dirs)
		if len(groups) == 0 {
			continue
		}
		var canonicalReal, foreign []string
		for real, paths := range groups {
			if canonicalPrefixMatch(real, tool.Canonical) {
				canonicalReal = append(canonicalReal, real)
			} else {
				foreign = append(foreign, strings.Join(paths, ","))
			}
		}
		if len(canonicalReal) == 1 && len(foreign) > 0 {
			cb(fmt.Sprintf("[i] Duplikat %s terdeteksi (%s); akan dibersihkan otomatis.",
				tool.Name, strings.Join(foreign, ", ")))
		}
	}
}

// PostflightToolchain dijalankan setelah instalasi penuh: audit, perbaiki, audit ulang.
func (inst *Installer) PostflightToolchain(ctx context.Context, cb LogCallback) error {
	cb("\n=== POST-FLIGHT: MENYELARASKAN TOOLCHAIN (SINGLE SOURCE) ===")
	findings := inst.AuditToolchain(ctx, cb)
	if err := inst.RepairToolchain(ctx, cb, findings); err != nil {
		return err
	}
	cb("\n=== VERIFIKASI ULANG ===")
	remaining := inst.AuditToolchain(ctx, cb)
	for _, f := range remaining {
		if f.Severity != SeverityOK {
			cb(" [!] Masih ada temuan yang butuh tindakan manual: " + f.String())
		}
	}
	return nil
}
