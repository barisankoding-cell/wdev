package serviceconfig

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// PHPConfig merepresentasikan parameter konfigurasi PHP.ini dan PHP-FPM Pool [www].
type PHPConfig struct {
	Version                  string   `json:"version"`
	AvailableVersions        []string `json:"available_versions"`
	IniPath                  string   `json:"ini_path"`
	DropInPath               string   `json:"dropin_path"`
	PoolPath                 string   `json:"pool_path"`
	ServiceUnit              string   `json:"service_unit"`
	Active                   bool     `json:"active"`
	MemoryLimit              string   `json:"memory_limit"`
	UploadMaxFilesize        string   `json:"upload_max_filesize"`
	PostMaxSize              string   `json:"post_max_size"`
	MaxExecutionTime         int      `json:"max_execution_time"`
	MaxInputVars             int      `json:"max_input_vars"`
	Timezone                 string   `json:"timezone"`
	DisplayErrors            bool     `json:"display_errors"`
	OpcacheEnable            bool     `json:"opcache_enable"`
	OpcacheMemoryConsumption int      `json:"opcache_memory_consumption"`
	SyncCLI                  bool     `json:"sync_cli"`
	PM                       string   `json:"pm"` // "dynamic" | "ondemand" | "static"
	PMMaxChildren            int      `json:"pm_max_children"`
	PMStartServers           int      `json:"pm_start_servers"`
	PMMinSpareServers        int      `json:"pm_min_spare_servers"`
	PMMaxSpareServers        int      `json:"pm_max_spare_servers"`
	PMMaxRequests            int      `json:"pm_max_requests"`
	Restart                  bool     `json:"restart,omitempty"`
}

type phpPaths struct {
	version    string
	avail      []string
	iniPath    string
	dropInFPM  string
	dropInCLI  string
	poolPath   string
	unit       string
	fpmBin     string
	isDebian   bool
}

// DetectPHPVersions memindai versi PHP-FPM yang terpasang di sistem.
func DetectPHPVersions() []string {
	var vers []string
	if entries, err := os.ReadDir("/etc/php"); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			v := e.Name()
			if _, err := os.Stat(fmt.Sprintf("/etc/php/%s/fpm/pool.d/www.conf", v)); err == nil {
				vers = append(vers, v)
			} else if _, err := os.Stat(fmt.Sprintf("/etc/php/%s/fpm/php.ini", v)); err == nil {
				vers = append(vers, v)
			}
		}
		sort.Slice(vers, func(i, j int) bool { return vers[i] > vers[j] })
		return vers
	}

	// Fedora/RHEL layout
	if _, err := os.Stat("/etc/php-fpm.d/www.conf"); err == nil {
		if out, err := exec.Command("php", "-r", "echo PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;").Output(); err == nil {
			v := strings.TrimSpace(string(out))
			if v != "" {
				return []string{v}
			}
		}
		return []string{"system"}
	}
	return nil
}

func isValidPHPVersion(v string) bool {
	if v == "" || v == "system" {
		return true
	}
	parts := strings.Split(v, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, p := range parts {
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func resolvePHPPaths(requestedVer string) (*phpPaths, error) {
	if !isValidPHPVersion(strings.TrimSpace(requestedVer)) {
		return nil, fmt.Errorf("versi PHP tidak valid: %q", requestedVer)
	}
	avail := DetectPHPVersions()

	// Debian/Ubuntu layout
	if _, err := os.Stat("/etc/php"); err == nil && len(avail) > 0 {
		ver := strings.TrimSpace(requestedVer)
		if ver == "" {
			// Prioritaskan unit php-fpm yang sedang aktif
			for _, candidate := range avail {
				if exec.Command("systemctl", "is-active", "--quiet", "php"+candidate+"-fpm").Run() == nil {
					ver = candidate
					break
				}
			}
		}
		if ver == "" {
			// Fallback ke versi PHP CLI aktif bila cocok
			if out, err := exec.Command("php", "-r", "echo PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;").Output(); err == nil {
				cliVer := strings.TrimSpace(string(out))
				for _, candidate := range avail {
					if candidate == cliVer {
						ver = candidate
						break
					}
				}
			}
		}
		if ver == "" {
			ver = avail[0]
		}

		fpmDir := fmt.Sprintf("/etc/php/%s/fpm", ver)
		if _, err := os.Stat(fpmDir); err != nil {
			return nil, fmt.Errorf("direktori PHP-FPM %s tidak ditemukan di %s", ver, fpmDir)
		}

		fpmBin := fmt.Sprintf("/usr/sbin/php-fpm%s", ver)
		if _, err := os.Stat(fpmBin); err != nil {
			if p, lErr := exec.LookPath("php-fpm" + ver); lErr == nil {
				fpmBin = p
			} else {
				fpmBin = ""
			}
		}

		dropInCLI := ""
		cliConfDir := fmt.Sprintf("/etc/php/%s/cli/conf.d", ver)
		if _, err := os.Stat(cliConfDir); err == nil {
			dropInCLI = filepath.Join(cliConfDir, "99-wdev.ini")
		}

		return &phpPaths{
			version:   ver,
			avail:     avail,
			iniPath:   filepath.Join(fpmDir, "php.ini"),
			dropInFPM: filepath.Join(fpmDir, "conf.d", "99-wdev.ini"),
			dropInCLI: dropInCLI,
			poolPath:  filepath.Join(fpmDir, "pool.d", "www.conf"),
			unit:      fmt.Sprintf("php%s-fpm", ver),
			fpmBin:    fpmBin,
			isDebian:  true,
		}, nil
	}

	// Fedora/RHEL layout
	if _, err := os.Stat("/etc/php-fpm.d/www.conf"); err == nil {
		ver := "system"
		if len(avail) > 0 {
			ver = avail[0]
		}
		fpmBin := "/usr/sbin/php-fpm"
		if _, err := os.Stat(fpmBin); err != nil {
			if p, lErr := exec.LookPath("php-fpm"); lErr == nil {
				fpmBin = p
			} else {
				fpmBin = ""
			}
		}
		return &phpPaths{
			version:   ver,
			avail:     avail,
			iniPath:   "/etc/php.ini",
			dropInFPM: "/etc/php.d/99-wdev.ini",
			poolPath:  "/etc/php-fpm.d/www.conf",
			unit:      "php-fpm",
			fpmBin:    fpmBin,
			isDebian:  false,
		}, nil
	}

	return nil, fmt.Errorf("instalasi PHP-FPM tidak ditemukan di sistem")
}

func parseIniKeyValues(contents []string) map[string]string {
	res := make(map[string]string)
	for _, content := range contents {
		for _, line := range strings.Split(content, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
				continue
			}
			k, v, ok := strings.Cut(trimmed, "=")
			if !ok {
				continue
			}
			key := strings.TrimSpace(k)
			val := stripIniComment(strings.TrimSpace(v))
			res[key] = val
		}
	}
	return res
}

func stripIniComment(raw string) string {
	inQuotes := false
	var sb strings.Builder
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if ch == '"' {
			inQuotes = !inQuotes
			sb.WriteByte(ch)
			continue
		}
		if ch == ';' && !inQuotes {
			break
		}
		sb.WriteByte(ch)
	}
	v := strings.TrimSpace(sb.String())
	if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
		v = v[1 : len(v)-1]
	}
	return strings.TrimSpace(v)
}

func parsePHPPoolBool(v string) bool {
	lower := strings.ToLower(strings.TrimSpace(v))
	return lower == "1" || lower == "on" || lower == "true" || lower == "yes"
}

// ParsePHPIniAndPool membaca nilai efektif PHP.ini (+ drop-in conf.d) dan pool www.conf.
func ParsePHPIniAndPool(iniContents []string, poolContent string) PHPConfig {
	ini := parseIniKeyValues(iniContents)
	pool := parseIniKeyValues([]string{poolContent})

	cfg := PHPConfig{
		MemoryLimit:              "128M",
		UploadMaxFilesize:        "2M",
		PostMaxSize:              "8M",
		MaxExecutionTime:         30,
		MaxInputVars:             1000,
		Timezone:                 "UTC",
		DisplayErrors:            false,
		OpcacheEnable:            true,
		OpcacheMemoryConsumption: 128,
		SyncCLI:                  true,
		PM:                       "dynamic",
		PMMaxChildren:            5,
		PMStartServers:           2,
		PMMinSpareServers:        1,
		PMMaxSpareServers:        3,
		PMMaxRequests:            0,
	}

	if v := ini["memory_limit"]; v != "" {
		cfg.MemoryLimit = v
	}
	if v := ini["upload_max_filesize"]; v != "" {
		cfg.UploadMaxFilesize = v
	}
	if v := ini["post_max_size"]; v != "" {
		cfg.PostMaxSize = v
	}
	if v := ini["max_execution_time"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.MaxExecutionTime = n
		}
	}
	if v := ini["max_input_vars"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxInputVars = n
		}
	}
	if v := ini["date.timezone"]; v != "" {
		cfg.Timezone = v
	}
	if v, ok := ini["display_errors"]; ok {
		cfg.DisplayErrors = parsePHPPoolBool(v)
	}
	if v, ok := ini["opcache.enable"]; ok {
		cfg.OpcacheEnable = parsePHPPoolBool(v)
	}
	if v := ini["opcache.memory_consumption"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.OpcacheMemoryConsumption = n
		}
	}

	if v := pool["pm"]; v == "dynamic" || v == "ondemand" || v == "static" {
		cfg.PM = v
	}
	if v := pool["pm.max_children"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.PMMaxChildren = n
		}
	}
	if v := pool["pm.start_servers"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.PMStartServers = n
		}
	}
	if v := pool["pm.min_spare_servers"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.PMMinSpareServers = n
		}
	}
	if v := pool["pm.max_spare_servers"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.PMMaxSpareServers = n
		}
	}
	if v := pool["pm.max_requests"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.PMMaxRequests = n
		}
	}

	return cfg
}

// ValidatePHPConfig memvalidasi parameter konfigurasi PHP.ini dan PHP-FPM pool.
func ValidatePHPConfig(cfg PHPConfig) error {
	if err := validatePHPIniSize(cfg.MemoryLimit, "memory_limit", true); err != nil {
		return err
	}
	if err := validatePHPIniSize(cfg.UploadMaxFilesize, "upload_max_filesize", false); err != nil {
		return err
	}
	if err := validatePHPIniSize(cfg.PostMaxSize, "post_max_size", true); err != nil {
		return err
	}
	if cfg.MaxExecutionTime < 0 || cfg.MaxExecutionTime > 86400 {
		return fmt.Errorf("max_execution_time harus antara 0 sampai 86400 detik")
	}
	if cfg.MaxInputVars < 100 || cfg.MaxInputVars > 100000 {
		return fmt.Errorf("max_input_vars harus antara 100 sampai 100000")
	}
	if err := validateTimezone(cfg.Timezone); err != nil {
		return err
	}
	if cfg.OpcacheMemoryConsumption < 16 || cfg.OpcacheMemoryConsumption > 4096 {
		return fmt.Errorf("opcache.memory_consumption harus antara 16 sampai 4096 MB")
	}

	switch cfg.PM {
	case "dynamic", "ondemand", "static":
	default:
		return fmt.Errorf("mode process manager (pm) tidak valid: %s", cfg.PM)
	}
	if cfg.PMMaxChildren < 1 || cfg.PMMaxChildren > 500 {
		return fmt.Errorf("pm.max_children harus antara 1 sampai 500")
	}
	if cfg.PM == "dynamic" {
		if cfg.PMMinSpareServers < 1 {
			return fmt.Errorf("pm.min_spare_servers minimal 1 untuk mode dynamic")
		}
		if cfg.PMMaxSpareServers < cfg.PMMinSpareServers {
			return fmt.Errorf("pm.max_spare_servers harus >= pm.min_spare_servers")
		}
		if cfg.PMMaxSpareServers > cfg.PMMaxChildren {
			return fmt.Errorf("pm.max_spare_servers harus <= pm.max_children")
		}
		if cfg.PMStartServers < cfg.PMMinSpareServers || cfg.PMStartServers > cfg.PMMaxSpareServers {
			return fmt.Errorf("pm.start_servers harus berada di antara pm.min_spare_servers (%d) dan pm.max_spare_servers (%d)", cfg.PMMinSpareServers, cfg.PMMaxSpareServers)
		}
	}
	if cfg.PMMaxRequests < 0 || cfg.PMMaxRequests > 100000 {
		return fmt.Errorf("pm.max_requests harus antara 0 sampai 100000")
	}
	return nil
}

func validatePHPIniSize(val, fieldName string, allowUnlimited bool) error {
	v := strings.TrimSpace(val)
	if v == "" {
		return fmt.Errorf("%s wajib diisi", fieldName)
	}
	if allowUnlimited && (v == "-1" || v == "0") {
		return nil
	}
	upper := strings.ToUpper(v)
	last := upper[len(upper)-1]
	numPart := upper
	if last == 'K' || last == 'M' || last == 'G' {
		numPart = upper[:len(upper)-1]
	}
	n, err := strconv.Atoi(numPart)
	if err != nil || n <= 0 {
		return fmt.Errorf("format %s tidak valid (contoh: 256M, 512M, 1G)", fieldName)
	}
	return nil
}

// RenderWdevPHPIni menghasilkan isi drop-in 99-wdev.ini yang bersih.
func RenderWdevPHPIni(cfg PHPConfig) string {
	disp := "Off"
	if cfg.DisplayErrors {
		disp = "On"
	}
	opcache := "0"
	if cfg.OpcacheEnable {
		opcache = "1"
	}
	return fmt.Sprintf(`; Managed by wdev — Pengaturan PHP Runtime
memory_limit = %s
upload_max_filesize = %s
post_max_size = %s
max_execution_time = %d
max_input_vars = %d
date.timezone = %s
display_errors = %s
opcache.enable = %s
opcache.memory_consumption = %d
`,
		strings.TrimSpace(cfg.MemoryLimit),
		strings.TrimSpace(cfg.UploadMaxFilesize),
		strings.TrimSpace(cfg.PostMaxSize),
		cfg.MaxExecutionTime,
		cfg.MaxInputVars,
		strings.TrimSpace(cfg.Timezone),
		disp,
		opcache,
		cfg.OpcacheMemoryConsumption,
	)
}

func setPHPPoolKey(lines []string, key, value string) []string {
	wanted := key + " = " + value
	activeIdx := -1
	commentIdx := -1
	out := make([]string, 0, len(lines))

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			uncommented := strings.TrimSpace(strings.TrimLeft(trimmed, ";#"))
			if k, _, ok := strings.Cut(uncommented, "="); ok && strings.TrimSpace(k) == key && commentIdx < 0 {
				commentIdx = len(out)
			}
			out = append(out, l)
			continue
		}
		if k, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(k) == key {
			if activeIdx >= 0 {
				continue
			}
			activeIdx = len(out)
			out = append(out, wanted)
			continue
		}
		out = append(out, l)
	}

	if activeIdx < 0 {
		if commentIdx >= 0 {
			withInsert := make([]string, 0, len(out)+1)
			withInsert = append(withInsert, out[:commentIdx+1]...)
			withInsert = append(withInsert, wanted)
			withInsert = append(withInsert, out[commentIdx+1:]...)
			return withInsert
		}
		out = append(out, wanted)
	}
	return out
}

// ApplyPHPPoolLines memperbarui directive worker pool pada www.conf.
func ApplyPHPPoolLines(lines []string, cfg PHPConfig) []string {
	lines = setPHPPoolKey(lines, "pm", cfg.PM)
	lines = setPHPPoolKey(lines, "pm.max_children", strconv.Itoa(cfg.PMMaxChildren))
	if cfg.PM == "dynamic" {
		lines = setPHPPoolKey(lines, "pm.start_servers", strconv.Itoa(cfg.PMStartServers))
		lines = setPHPPoolKey(lines, "pm.min_spare_servers", strconv.Itoa(cfg.PMMinSpareServers))
		lines = setPHPPoolKey(lines, "pm.max_spare_servers", strconv.Itoa(cfg.PMMaxSpareServers))
	}
	lines = setPHPPoolKey(lines, "pm.max_requests", strconv.Itoa(cfg.PMMaxRequests))
	return lines
}

// GetPHPConfig membaca konfigurasi PHP.ini (+ conf.d) dan pool www.conf untuk versi target.
func (m *Manager) GetPHPConfig(ctx context.Context, version string) (*PHPConfig, error) {
	paths, err := resolvePHPPaths(version)
	if err != nil {
		return nil, err
	}

	var iniContents []string
	if raw, rErr := m.readPrivilegedFile(ctx, paths.iniPath); rErr == nil {
		iniContents = append(iniContents, raw)
	} else if !os.IsNotExist(rErr) {
		return nil, rErr
	}

	confDir := filepath.Dir(paths.dropInFPM)
	if entries, err := os.ReadDir(confDir); err == nil {
		var names []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".ini") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, name := range names {
			if raw, rErr := m.readPrivilegedFile(ctx, filepath.Join(confDir, name)); rErr == nil {
				iniContents = append(iniContents, raw)
			}
		}
	}

	poolRaw, err := m.readPrivilegedFile(ctx, paths.poolPath)
	if err != nil {
		return nil, err
	}

	cfg := ParsePHPIniAndPool(iniContents, poolRaw)
	cfg.Version = paths.version
	cfg.AvailableVersions = paths.avail
	cfg.IniPath = paths.iniPath
	cfg.DropInPath = paths.dropInFPM
	cfg.PoolPath = paths.poolPath
	cfg.ServiceUnit = paths.unit
	cfg.Active = m.isServiceActive(ctx, paths.unit)
	return &cfg, nil
}

// SavePHPConfig menyimpan pengaturan PHP.ini (via 99-wdev.ini) dan pool www.conf,
// memvalidasi dengan `php-fpm -t`, dan me-restart layanan bila diminta.
func (m *Manager) SavePHPConfig(ctx context.Context, cfg PHPConfig) (*PHPConfig, error) {
	if err := ValidatePHPConfig(cfg); err != nil {
		return nil, err
	}
	paths, err := resolvePHPPaths(cfg.Version)
	if err != nil {
		return nil, err
	}

	origPool, err := m.readPrivilegedFile(ctx, paths.poolPath)
	if err != nil {
		return nil, err
	}
	origDropInFPM, dropInFPMExisted := "", false
	if ok, _ := m.fileExistsPrivileged(ctx, paths.dropInFPM); ok {
		dropInFPMExisted = true
		origDropInFPM, _ = m.readPrivilegedFile(ctx, paths.dropInFPM)
	}
	origDropInCLI, dropInCLIExisted := "", false
	if paths.dropInCLI != "" {
		if ok, _ := m.fileExistsPrivileged(ctx, paths.dropInCLI); ok {
			dropInCLIExisted = true
			origDropInCLI, _ = m.readPrivilegedFile(ctx, paths.dropInCLI)
		}
	}

	restoreAll := func() {
		_ = m.writePrivilegedFile(ctx, paths.poolPath, []byte(origPool), "644")
		if dropInFPMExisted {
			_ = m.writePrivilegedFile(ctx, paths.dropInFPM, []byte(origDropInFPM), "644")
		} else {
			_, _ = m.runPrivilegedCmd(ctx, "rm", "-f", paths.dropInFPM)
		}
		if paths.dropInCLI != "" && cfg.SyncCLI {
			if dropInCLIExisted {
				_ = m.writePrivilegedFile(ctx, paths.dropInCLI, []byte(origDropInCLI), "644")
			} else {
				_, _ = m.runPrivilegedCmd(ctx, "rm", "-f", paths.dropInCLI)
			}
		}
	}

	iniContent := RenderWdevPHPIni(cfg)
	if err := m.writePrivilegedFile(ctx, paths.dropInFPM, []byte(iniContent), "644"); err != nil {
		return nil, err
	}
	if paths.dropInCLI != "" && cfg.SyncCLI {
		if err := m.writePrivilegedFile(ctx, paths.dropInCLI, []byte(iniContent), "644"); err != nil {
			restoreAll()
			return nil, err
		}
	}

	poolLines := strings.Split(strings.TrimRight(origPool, "\n"), "\n")
	updatedPool := strings.Join(ApplyPHPPoolLines(poolLines, cfg), "\n") + "\n"
	if err := m.writePrivilegedFile(ctx, paths.poolPath, []byte(updatedPool), "644"); err != nil {
		restoreAll()
		return nil, err
	}

	if paths.fpmBin != "" {
		if _, err := m.runPrivilegedCmd(ctx, paths.fpmBin, "-t"); err != nil {
			restoreAll()
			return nil, fmt.Errorf("validasi konfigurasi PHP-FPM gagal (perubahan dibatalkan): %w", err)
		}
	}

	if cfg.Restart {
		if err := m.restartService(ctx, paths.unit); err != nil {
			restoreAll()
			_ = m.restartService(ctx, paths.unit)
			return nil, fmt.Errorf("gagal restart %s (konfigurasi dikembalikan): %w", paths.unit, err)
		}
	}

	return m.GetPHPConfig(ctx, paths.version)
}
