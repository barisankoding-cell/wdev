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
	"time"
)

var rustMoldTargets = []string{
	"x86_64-unknown-linux-gnu",
	"aarch64-unknown-linux-gnu",
}

// sccacheBinaryPath mengembalikan path executable sccache kanonik bila tersedia.
func sccacheBinaryPath() string {
	const canonical = "/usr/local/bin/sccache"
	if fi, err := os.Stat(canonical); err == nil && !fi.IsDir() && (fi.Mode().Perm()&0o111) != 0 {
		return canonical
	}
	return findTool("sccache")
}

// moldLinkerAvailable memeriksa apakah mold dan clang tersedia untuk linking cepat Rust.
func moldLinkerAvailable() bool {
	return findTool("mold") != "" && findTool("clang") != ""
}

// parseTOMLSectionHeader mengekstrak nama tabel dari header TOML `[section]`.
// Mengembalikan ("", false) untuk baris biasa atau array-of-tables `[[...]]`.
func parseTOMLSectionHeader(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "[[") {
		return "", false
	}
	// Abaikan komentar di akhir baris di luar tanda kutip sederhana.
	if idx := strings.Index(trimmed, "]"); idx > 1 {
		rest := strings.TrimSpace(trimmed[idx+1:])
		if rest == "" || strings.HasPrefix(rest, "#") {
			return strings.TrimSpace(trimmed[1:idx]), true
		}
	}
	return "", false
}

// parseTOMLKey mengekstrak nama kunci dari baris `key = value`.
func parseTOMLKey(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	k, _, ok := strings.Cut(trimmed, "=")
	if !ok {
		return "", false
	}
	key := strings.TrimSpace(k)
	if key == "" {
		return "", false
	}
	return key, true
}

// reconcileCargoConfig memastikan konfigurasi Cargo memuat sccache (di [build])
// dan linker mold+clang (di [target.*]) tanpa menimpa pengaturan kustom user.
func reconcileCargoConfig(existing, sccachePath string, enableMold bool) string {
	if sccachePath == "" && !enableMold {
		return existing
	}

	normalized := strings.ReplaceAll(existing, "\r\n", "\n")
	var lines []string
	if strings.TrimSpace(normalized) != "" {
		lines = strings.Split(strings.TrimRight(normalized, "\n"), "\n")
	}

	// Lacak header section yang ada beserta kunci di dalamnya.
	type secInfo struct {
		headerIdx int
		keys      map[string]bool
	}
	sections := make(map[string]*secInfo)
	curSec := ""
	for i, line := range lines {
		if sec, ok := parseTOMLSectionHeader(line); ok {
			curSec = sec
			if _, exists := sections[curSec]; !exists {
				sections[curSec] = &secInfo{
					headerIdx: i,
					keys:      make(map[string]bool),
				}
			}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "[[") {
			curSec = ""
			continue
		}
		if curSec != "" {
			if k, ok := parseTOMLKey(line); ok {
				sections[curSec].keys[k] = true
			}
		}
	}

	// Kumpulkan penyisipan baris setelah header section yang sudah ada.
	insertAfter := make(map[int][]string)
	var appendBlocks []string

	if sccachePath != "" {
		wrapperLine := fmt.Sprintf("rustc-wrapper = %q", sccachePath)
		if sec, ok := sections["build"]; ok {
			if !sec.keys["rustc-wrapper"] {
				insertAfter[sec.headerIdx] = append(insertAfter[sec.headerIdx], wrapperLine)
			}
		} else {
			appendBlocks = append(appendBlocks, "[build]\n"+wrapperLine)
		}
	}

	if enableMold {
		for _, target := range rustMoldTargets {
			secName := "target." + target
			if sec, ok := sections[secName]; ok {
				if !sec.keys["linker"] {
					insertAfter[sec.headerIdx] = append(insertAfter[sec.headerIdx], `linker = "clang"`)
				}
				if !sec.keys["rustflags"] {
					insertAfter[sec.headerIdx] = append(insertAfter[sec.headerIdx], `rustflags = ["-C", "link-arg=-fuse-ld=mold"]`)
				}
			} else {
				appendBlocks = append(appendBlocks, fmt.Sprintf("[%s]\nlinker = \"clang\"\nrustflags = [\"-C\", \"link-arg=-fuse-ld=mold\"]", secName))
			}
		}
	}

	var out []string
	for i, line := range lines {
		out = append(out, line)
		if extra, ok := insertAfter[i]; ok {
			out = append(out, extra...)
		}
	}

	for _, blk := range appendBlocks {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, strings.Split(blk, "\n")...)
	}

	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

func cargoInstalledOnHost() bool {
	return findTool("cargo") != "" || findTool("rustc") != "" || dirExists("/usr/local/cargo") || dirExists("/usr/local/rustup")
}

// syncUserCargoConfig menulis atau memperbarui <home>/.cargo/config.toml untuk user non-root.
func syncUserCargoConfig(user, home, sccachePath string, enableMold bool) (bool, string, error) {
	if home == "" || user == "" || user == "root" {
		return false, "", nil
	}
	if !dirExists(home) {
		return false, "", nil
	}
	cargoDir := filepath.Join(home, ".cargo")
	cfgPath := filepath.Join(cargoDir, "config.toml")

	existing := ""
	if data, err := os.ReadFile(cfgPath); err == nil {
		existing = string(data)
	}
	updated := reconcileCargoConfig(existing, sccachePath, enableMold)
	if updated == existing || updated == "" {
		return false, cfgPath, nil
	}

	if err := ensureUserDir(user, home, cargoDir); err != nil {
		return false, cfgPath, err
	}
	if err := os.WriteFile(cfgPath, []byte(updated), 0o644); err != nil {
		return false, cfgPath, err
	}
	chownPath(user, cfgPath)
	return true, cfgPath, nil
}

// syncCargoToolchainConfig menyelaraskan config.toml Cargo untuk sistem (/usr/local/cargo)
// dan user non-root (~/.cargo) agar sccache & mold aktif otomatis untuk semua user.
func (inst *Installer) syncCargoToolchainConfig(ctx context.Context, cb LogCallback) error {
	if cb == nil {
		cb = func(string) {}
	}
	if !cargoInstalledOnHost() {
		return nil
	}
	sccachePath := sccacheBinaryPath()
	enableMold := moldLinkerAvailable()
	if sccachePath == "" && !enableMold {
		return nil
	}

	// 1. Sistem/root CARGO_HOME (/usr/local/cargo/config.toml)
	if dirExists("/usr/local/cargo") {
		sysCfg := "/usr/local/cargo/config.toml"
		existing := ""
		if data, err := os.ReadFile(sysCfg); err == nil {
			existing = string(data)
		}
		updated := reconcileCargoConfig(existing, sccachePath, enableMold)
		if updated != existing && updated != "" {
			if err := inst.writeSystemFile(ctx, cb, sysCfg, []byte(updated), "644"); err != nil {
				return fmt.Errorf("gagal menulis %s: %w", sysCfg, err)
			}
			cb("  ✓ Konfigurasi Cargo sistem diselaraskan: " + sysCfg)
		}
	}

	// 2. Non-root user CARGO_HOME (~/.cargo/config.toml)
	user, home := ResolveOrcaUser()
	if user != "" && user != "root" && home != "" {
		changed, cfgPath, err := syncUserCargoConfig(user, home, sccachePath, enableMold)
		if err != nil {
			cb(fmt.Sprintf("[WARN] Gagal menyelaraskan Cargo user (%s): %v", cfgPath, err))
		} else if changed {
			cb(fmt.Sprintf("  ✓ Konfigurasi Cargo user (%s) diselaraskan: %s", user, cfgPath))
		}
	}
	return nil
}

// cargoConfigMissingSccache memeriksa apakah config.toml Cargo belum memiliki rustc-wrapper.
func cargoConfigMissingSccache(cfgPath string) bool {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return true
	}
	inBuild := false
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if sec, ok := parseTOMLSectionHeader(line); ok {
			inBuild = sec == "build"
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "[[") {
			inBuild = false
			continue
		}
		if inBuild {
			if k, ok := parseTOMLKey(line); ok && k == "rustc-wrapper" {
				return false
			}
		}
	}
	return true
}

// cargoConfigDriftFinding memeriksa apakah konfigurasi Cargo sistem/user belum mengaktifkan sccache.
func cargoConfigDriftFinding(home string) *Finding {
	if !cargoInstalledOnHost() || sccacheBinaryPath() == "" {
		return nil
	}
	var missing []string
	if dirExists("/usr/local/cargo") && cargoConfigMissingSccache("/usr/local/cargo/config.toml") {
		missing = append(missing, "/usr/local/cargo/config.toml")
	}
	if home != "" && home != "/root" && dirExists(home) {
		userCfg := filepath.Join(home, ".cargo", "config.toml")
		if cargoConfigMissingSccache(userCfg) {
			missing = append(missing, userCfg)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return &Finding{
		Severity: SeverityWarning,
		Kind:     "cargo-config",
		Tool:     "sccache",
		Detail:   "konfigurasi Cargo belum mengaktifkan sccache: " + strings.Join(missing, ", "),
		Fixable:  true,
	}
}

// cargoConfigReport mengembalikan ringkasan status konfigurasi Cargo (sccache/mold).
func cargoConfigReport(home string) []string {
	if !cargoInstalledOnHost() {
		return nil
	}
	var lines []string
	check := func(label, path string) {
		if _, err := os.Stat(path); err != nil {
			lines = append(lines, fmt.Sprintf("  [!] %-10s: %s belum ada (jalankan: wdev env sync)", label, path))
			return
		}
		if sccacheBinaryPath() != "" && cargoConfigMissingSccache(path) {
			lines = append(lines, fmt.Sprintf("  [!] %-10s: %s belum mengaktifkan sccache (jalankan: wdev env sync)", label, path))
			return
		}
		lines = append(lines, fmt.Sprintf("  [OK] %-9s: %s (sccache siap)", label, path))
	}
	if dirExists("/usr/local/cargo") {
		check("cargo-sys", "/usr/local/cargo/config.toml")
	}
	if home != "" && home != "/root" {
		check("cargo-usr", filepath.Join(home, ".cargo", "config.toml"))
	}
	return lines
}

// installSccacheOfficial memasang sccache dari rilis resmi GitHub mozilla/sccache ke /usr/local/bin/sccache.
func (inst *Installer) installSccacheOfficial(ctx context.Context, cb LogCallback) error {
	tag, assets, err := fetchGitHubLatestRelease(ctx, "mozilla/sccache")
	if err != nil {
		return err
	}

	archSuffix := "x86_64-unknown-linux-musl.tar.gz"
	if runtime.GOARCH == "arm64" {
		archSuffix = "aarch64-unknown-linux-musl.tar.gz"
	}

	var dlURL string
	for _, a := range assets {
		if strings.HasPrefix(a.Name, "sccache-") && strings.HasSuffix(a.Name, archSuffix) {
			dlURL = a.URL
			break
		}
	}
	if dlURL == "" {
		return fmt.Errorf("aset rilis sccache untuk %s tidak ditemukan", archSuffix)
	}

	dstBin := "/usr/local/bin/sccache"
	alreadyLatest := false
	if out, err := exec.CommandContext(ctx, dstBin, "--version").CombinedOutput(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 2 && !hasNewerVersion(fields[1], strings.TrimPrefix(tag, "v")) {
			alreadyLatest = true
		}
	}

	if alreadyLatest {
		if needsRootPermFix(dstBin) {
			_ = inst.runCmd(ctx, cb, "chmod", "755", dstBin)
			_ = inst.runCmd(ctx, cb, "chown", "root:root", dstBin)
		} else if fi, err := os.Stat(dstBin); err == nil && fi.Mode().Perm() != 0o755 {
			_ = inst.runCmd(ctx, cb, "chmod", "755", dstBin)
		}
		cb(fmt.Sprintf("Sccache %s sudah terpasang di %s, melewati unduhan.", tag, dstBin))
		return nil
	}

	tmpDir, err := os.MkdirTemp("", "wdev-sccache-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tmpTar := filepath.Join(tmpDir, fmt.Sprintf("sccache-%s-%s", tag, archSuffix))
	cb(fmt.Sprintf("Mengunduh Sccache %s dari: %s", tag, dlURL))
	if err := downloadFile(dlURL, tmpTar); err != nil {
		return err
	}

	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return err
	}
	if err := inst.runCmd(ctx, cb, "tar", "--no-same-owner", "-xzf", tmpTar, "-C", extractDir); err != nil {
		return err
	}

	var foundBin string
	_ = filepath.WalkDir(extractDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if d.Name() == "sccache" {
			foundBin = path
			return filepath.SkipAll
		}
		return nil
	})
	if foundBin == "" {
		return fmt.Errorf("biner sccache tidak ditemukan di dalam arsip rilis %s", tag)
	}

	if err := inst.runCmd(ctx, cb, "install", "-m", "755", "-o", "root", "-g", "root", foundBin, dstBin); err != nil {
		return err
	}

	inst.removeDistroPackagesIfInstalled(ctx, cb, "sccache")
	cb(fmt.Sprintf("✓ Sccache %s berhasil dipasang di %s", tag, dstBin))
	return nil
}

// SccacheStatsReport merangkum status aktivasi dan statistik live cache sccache.
type SccacheStatsReport struct {
	Installed             bool              `json:"installed"`
	BinaryPath            string            `json:"binary_path,omitempty"`
	Version               string            `json:"version,omitempty"`
	TargetUser            string            `json:"target_user,omitempty"`
	UserCargoConfigPath   string            `json:"user_cargo_config_path,omitempty"`
	UserCargoConfigured   bool              `json:"user_cargo_configured"`
	SystemCargoConfigPath string            `json:"system_cargo_config_path,omitempty"`
	SystemCargoConfigured bool              `json:"system_cargo_configured"`
	ProfileWrapperEnabled bool              `json:"profile_wrapper_enabled"`
	ActiveConfigured      bool              `json:"active_configured"`
	ServerRunning         bool              `json:"server_running"`
	CompileRequests       uint64            `json:"compile_requests"`
	RequestsExecuted      uint64            `json:"requests_executed"`
	CacheHits             uint64            `json:"cache_hits"`
	CacheMisses           uint64            `json:"cache_misses"`
	CacheHitRate          float64           `json:"cache_hit_rate"`
	CacheHitsByLang       map[string]uint64 `json:"cache_hits_by_lang,omitempty"`
	CacheMissesByLang     map[string]uint64 `json:"cache_misses_by_lang,omitempty"`
	CacheErrors           uint64            `json:"cache_errors"`
	CacheTimeouts         uint64            `json:"cache_timeouts"`
	CompileFails          uint64            `json:"compile_fails"`
	NonCacheableCalls     uint64            `json:"non_cacheable_calls"`
	NonCacheableCompiles  uint64            `json:"non_cacheable_compiles"`
	UnsupportedCompiler   uint64            `json:"unsupported_compiler"`
	CacheLocation         string            `json:"cache_location,omitempty"`
	CacheSizeBytes        uint64            `json:"cache_size_bytes"`
	MaxCacheSizeBytes     uint64            `json:"max_cache_size_bytes"`
	PreprocessorMode      bool              `json:"preprocessor_mode"`
	AvgCacheReadHitMs     float64           `json:"avg_cache_read_hit_ms"`
	AvgCacheWriteMs       float64           `json:"avg_cache_write_ms"`
	AvgCompilerMs         float64           `json:"avg_compiler_ms"`
	RawOutput             string            `json:"raw_output,omitempty"`
	Error                 string            `json:"error,omitempty"`
}

type sccacheDurationJSON struct {
	Secs  uint64 `json:"secs"`
	Nanos uint64 `json:"nanos"`
}

func (d sccacheDurationJSON) millis() float64 {
	return float64(d.Secs)*1000.0 + float64(d.Nanos)/1e6
}

type sccacheCountsJSON struct {
	Counts map[string]uint64 `json:"counts"`
}

type sccacheRawJSON struct {
	Version              string  `json:"version"`
	CacheLocation        string  `json:"cache_location"`
	CacheSize            *uint64 `json:"cache_size"`
	MaxCacheSize         uint64  `json:"max_cache_size"`
	UsePreprocessorCache bool    `json:"use_preprocessor_cache_mode"`
	Stats                struct {
		CompileRequests             uint64              `json:"compile_requests"`
		RequestsUnsupportedCompiler uint64              `json:"requests_unsupported_compiler"`
		RequestsNotCompile          uint64              `json:"requests_not_compile"`
		RequestsNotCacheable        uint64              `json:"requests_not_cacheable"`
		RequestsExecuted            uint64              `json:"requests_executed"`
		CacheErrors                 sccacheCountsJSON   `json:"cache_errors"`
		CacheHits                   sccacheCountsJSON   `json:"cache_hits"`
		CacheMisses                 sccacheCountsJSON   `json:"cache_misses"`
		CacheTimeouts               uint64              `json:"cache_timeouts"`
		NonCacheableCompilations    uint64              `json:"non_cacheable_compilations"`
		CompileFails                uint64              `json:"compile_fails"`
		CacheWriteDuration          sccacheDurationJSON `json:"cache_write_duration"`
		CacheReadHitDuration        sccacheDurationJSON `json:"cache_read_hit_duration"`
		CompilerWriteDuration       sccacheDurationJSON `json:"compiler_write_duration"`
	} `json:"stats"`
}

func sumCountMap(m map[string]uint64) uint64 {
	var total uint64
	for _, v := range m {
		total += v
	}
	return total
}

func parseSccacheStatsJSON(data []byte, rep *SccacheStatsReport) error {
	var raw sccacheRawJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Version != "" {
		rep.Version = raw.Version
	}
	rep.CacheLocation = raw.CacheLocation
	if raw.CacheSize != nil {
		rep.CacheSizeBytes = *raw.CacheSize
	}
	rep.MaxCacheSizeBytes = raw.MaxCacheSize
	rep.PreprocessorMode = raw.UsePreprocessorCache

	rep.CompileRequests = raw.Stats.CompileRequests
	rep.RequestsExecuted = raw.Stats.RequestsExecuted
	rep.CacheHitsByLang = raw.Stats.CacheHits.Counts
	rep.CacheMissesByLang = raw.Stats.CacheMisses.Counts
	rep.CacheHits = sumCountMap(raw.Stats.CacheHits.Counts)
	rep.CacheMisses = sumCountMap(raw.Stats.CacheMisses.Counts)
	if total := rep.CacheHits + rep.CacheMisses; total > 0 {
		rep.CacheHitRate = float64(rep.CacheHits) * 100.0 / float64(total)
	}
	rep.CacheErrors = sumCountMap(raw.Stats.CacheErrors.Counts)
	rep.CacheTimeouts = raw.Stats.CacheTimeouts
	rep.CompileFails = raw.Stats.CompileFails
	rep.NonCacheableCalls = raw.Stats.RequestsNotCacheable + raw.Stats.RequestsNotCompile
	rep.NonCacheableCompiles = raw.Stats.NonCacheableCompilations
	rep.UnsupportedCompiler = raw.Stats.RequestsUnsupportedCompiler
	rep.AvgCacheReadHitMs = raw.Stats.CacheReadHitDuration.millis()
	rep.AvgCacheWriteMs = raw.Stats.CacheWriteDuration.millis()
	rep.AvgCompilerMs = raw.Stats.CompilerWriteDuration.millis()
	return nil
}

func runSccacheForUser(ctx context.Context, bin, user, home string, args ...string) ([]byte, error) {
	if os.Geteuid() == 0 && user != "" && user != "root" && home != "" {
		runArgs := []string{
			"-u", user, "--",
			"env", "HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
			bin,
		}
		runArgs = append(runArgs, args...)
		cmd := exec.CommandContext(ctx, "runuser", runArgs...)
		cmd.Dir = "/"
		return cmd.CombinedOutput()
	}
	return exec.CommandContext(ctx, bin, args...).CombinedOutput()
}

func isSccacheDaemonRunning(ctx context.Context, user string) bool {
	var cmd *exec.Cmd
	if os.Geteuid() == 0 && user != "" && user != "root" {
		cmd = exec.CommandContext(ctx, "pgrep", "-u", user, "-x", "sccache")
	} else {
		cmd = exec.CommandContext(ctx, "pgrep", "-x", "sccache")
	}
	return cmd.Run() == nil
}

func measureLocalCacheDirBytes(cacheLocation, home string) uint64 {
	dir := ""
	if idx := strings.Index(cacheLocation, `"`); idx >= 0 {
		rest := cacheLocation[idx+1:]
		if end := strings.Index(rest, `"`); end > 0 {
			dir = rest[:end]
		}
	}
	if dir == "" && home != "" {
		dir = filepath.Join(home, ".cache", "sccache")
	}
	if dir == "" || !dirExists(dir) {
		return 0
	}
	var total uint64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, statErr := d.Info(); statErr == nil {
			total += uint64(fi.Size())
		}
		return nil
	})
	return total
}

func inspectSccacheConfigStatus(rep *SccacheStatsReport, user, home string) {
	rep.TargetUser = user
	if dirExists("/usr/local/cargo") {
		sysCfg := "/usr/local/cargo/config.toml"
		rep.SystemCargoConfigPath = sysCfg
		rep.SystemCargoConfigured = !cargoConfigMissingSccache(sysCfg)
	}
	if home != "" && home != "/root" {
		usrCfg := filepath.Join(home, ".cargo", "config.toml")
		rep.UserCargoConfigPath = usrCfg
		rep.UserCargoConfigured = !cargoConfigMissingSccache(usrCfg)
	}
	if data, err := os.ReadFile(envProfilePath); err == nil {
		rep.ProfileWrapperEnabled = strings.Contains(string(data), "RUSTC_WRAPPER")
	}
	rep.ActiveConfigured = rep.Installed && (rep.UserCargoConfigured || rep.SystemCargoConfigured || rep.ProfileWrapperEnabled)
}

// GetSccacheStats mengambil status aktivasi dan statistik live sccache untuk user target.
func (inst *Installer) GetSccacheStats(ctx context.Context) SccacheStatsReport {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	user, home := ResolveOrcaUser()
	bin := sccacheBinaryPath()
	rep := SccacheStatsReport{
		Installed:  bin != "",
		BinaryPath: bin,
	}
	inspectSccacheConfigStatus(&rep, user, home)
	if !rep.Installed {
		return rep
	}

	if out, err := exec.CommandContext(queryCtx, bin, "--version").CombinedOutput(); err == nil {
		fields := strings.Fields(string(out))
		if len(fields) >= 2 {
			rep.Version = fields[1]
		}
	}

	rep.ServerRunning = isSccacheDaemonRunning(queryCtx, user)

	jsonOut, err := runSccacheForUser(queryCtx, bin, user, home, "--show-stats", "--stats-format", "json")
	if err != nil {
		rep.Error = strings.TrimSpace(string(jsonOut))
		if rep.Error == "" {
			rep.Error = err.Error()
		}
		return rep
	}
	if parseErr := parseSccacheStatsJSON(jsonOut, &rep); parseErr != nil {
		rep.Error = fmt.Sprintf("gagal mem-parsing output JSON sccache: %v", parseErr)
		return rep
	}

	rep.ServerRunning = isSccacheDaemonRunning(queryCtx, user)
	if rep.CacheSizeBytes == 0 {
		rep.CacheSizeBytes = measureLocalCacheDirBytes(rep.CacheLocation, home)
	}
	if rawOut, rawErr := runSccacheForUser(queryCtx, bin, user, home, "--show-stats"); rawErr == nil {
		rep.RawOutput = strings.TrimSpace(string(rawOut))
	}
	return rep
}

// ZeroSccacheStats mereset counter statistik sccache (tanpa menghapus cache disk).
func (inst *Installer) ZeroSccacheStats(ctx context.Context) (SccacheStatsReport, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	user, home := ResolveOrcaUser()
	bin := sccacheBinaryPath()
	if bin == "" {
		return inst.GetSccacheStats(ctx), fmt.Errorf("sccache belum terpasang")
	}
	if out, err := runSccacheForUser(queryCtx, bin, user, home, "--zero-stats"); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return inst.GetSccacheStats(ctx), fmt.Errorf("gagal mereset statistik sccache: %s", msg)
	}
	return inst.GetSccacheStats(ctx), nil
}
