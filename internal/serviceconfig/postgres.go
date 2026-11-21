package serviceconfig

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"wdev/internal/monitor"
)

// PostgresConfig merepresentasikan parameter konfigurasi PostgreSQL yang dapat diatur dari dashboard.
type PostgresConfig struct {
	Version                 string   `json:"version"`
	AvailableVersions       []string `json:"available_versions"`
	ConfigPath              string   `json:"config_path"`
	HbaPath                 string   `json:"hba_path"`
	ServiceUnit             string   `json:"service_unit"`
	Active                  bool     `json:"active"`
	Port                    int      `json:"port"`
	ListenAddresses         string   `json:"listen_addresses"`
	MaxConnections          int      `json:"max_connections"`
	SharedBuffers           string   `json:"shared_buffers"`
	EffectiveCacheSize      string   `json:"effective_cache_size"`
	WorkMem                 string   `json:"work_mem"`
	MaintenanceWorkMem      string   `json:"maintenance_work_mem"`
	PasswordEncryption      string   `json:"password_encryption"`
	Timezone                string   `json:"timezone"`
	LogMinDurationStatement int      `json:"log_min_duration_statement"`
	RemoteAccessMode        string   `json:"remote_access_mode"` // "local" | "private_vpn" | "all"
	NewPostgresPassword     string   `json:"new_postgres_password,omitempty"`
	Restart                 bool     `json:"restart,omitempty"`
}

const (
	pgHbaBlockBegin = "# BEGIN WDEV REMOTE ACCESS"
	pgHbaBlockEnd   = "# END WDEV REMOTE ACCESS"
)

var pgMajors = []string{"19", "18", "17", "16", "15", "14"}

// DetectPostgresVersions memindai versi mayor PostgreSQL yang terpasang di Debian maupun Fedora/RHEL.
func (m *Manager) DetectPostgresVersions(ctx context.Context) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}

	// Debian/Ubuntu layout
	if entries, err := os.ReadDir("/etc/postgresql"); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				conf := fmt.Sprintf("/etc/postgresql/%s/main/postgresql.conf", e.Name())
				if ok, _ := m.fileExistsPrivileged(ctx, conf); ok {
					add(e.Name())
				}
			}
		}
	}

	// Fedora/RHEL PGDG layout
	for _, v := range pgMajors {
		if _, err := os.Stat(fmt.Sprintf("/usr/pgsql-%s/bin/postgres", v)); err == nil {
			add(v)
		} else if ok, _ := m.fileExistsPrivileged(ctx, fmt.Sprintf("/var/lib/pgsql/%s/data/postgresql.conf", v)); ok {
			add(v)
		}
	}

	// Fedora/RHEL distro default layout
	if len(out) == 0 {
		if ok, _ := m.fileExistsPrivileged(ctx, "/var/lib/pgsql/data/postgresql.conf"); ok {
			add(monitor.GetActivePostgresMajor())
		}
	}

	return out
}

func isValidPGVersion(v string) bool {
	if v == "" || v == "system" {
		return true
	}
	if len(v) > 3 {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (m *Manager) resolvePostgresPaths(ctx context.Context, requestedVer string) (confPath, hbaPath, unit, resolvedVer string, avail []string, err error) {
	if !isValidPGVersion(strings.TrimSpace(requestedVer)) {
		return "", "", "", "", nil, fmt.Errorf("versi PostgreSQL tidak valid: %q", requestedVer)
	}
	avail = m.DetectPostgresVersions(ctx)
	ver := strings.TrimSpace(requestedVer)
	if ver == "" {
		ver = monitor.GetActivePostgresMajor()
	}

	checkPair := func(c, h, u, v string) (bool, error) {
		ok, fErr := m.fileExistsPrivileged(ctx, c)
		if fErr != nil {
			return false, fErr
		}
		if ok {
			confPath, hbaPath, unit, resolvedVer = c, h, u, v
			return true, nil
		}
		return false, nil
	}

	if ver != "" {
		// 1. Debian/Ubuntu
		if ok, fErr := checkPair(
			fmt.Sprintf("/etc/postgresql/%s/main/postgresql.conf", ver),
			fmt.Sprintf("/etc/postgresql/%s/main/pg_hba.conf", ver),
			fmt.Sprintf("postgresql@%s-main", ver),
			ver,
		); fErr != nil {
			return "", "", "", "", avail, fErr
		} else if ok {
			return confPath, hbaPath, unit, resolvedVer, avail, nil
		}

		// 2. Fedora/RHEL PGDG
		if ok, fErr := checkPair(
			fmt.Sprintf("/var/lib/pgsql/%s/data/postgresql.conf", ver),
			fmt.Sprintf("/var/lib/pgsql/%s/data/pg_hba.conf", ver),
			fmt.Sprintf("postgresql-%s", ver),
			ver,
		); fErr != nil {
			return "", "", "", "", avail, fErr
		} else if ok {
			return confPath, hbaPath, unit, resolvedVer, avail, nil
		}
	}

	// 3. Fallback ke versi pertama yang terdeteksi
	for _, v := range avail {
		if ok, fErr := checkPair(
			fmt.Sprintf("/etc/postgresql/%s/main/postgresql.conf", v),
			fmt.Sprintf("/etc/postgresql/%s/main/pg_hba.conf", v),
			fmt.Sprintf("postgresql@%s-main", v),
			v,
		); fErr != nil {
			return "", "", "", "", avail, fErr
		} else if ok {
			return confPath, hbaPath, unit, resolvedVer, avail, nil
		}
		if ok, fErr := checkPair(
			fmt.Sprintf("/var/lib/pgsql/%s/data/postgresql.conf", v),
			fmt.Sprintf("/var/lib/pgsql/%s/data/pg_hba.conf", v),
			fmt.Sprintf("postgresql-%s", v),
			v,
		); fErr != nil {
			return "", "", "", "", avail, fErr
		} else if ok {
			return confPath, hbaPath, unit, resolvedVer, avail, nil
		}
	}

	// 4. Fedora/RHEL distro default
	if ok, fErr := checkPair("/var/lib/pgsql/data/postgresql.conf", "/var/lib/pgsql/data/pg_hba.conf", "postgresql", ver); fErr != nil {
		return "", "", "", "", avail, fErr
	} else if ok {
		return confPath, hbaPath, unit, resolvedVer, avail, nil
	}

	return "", "", "", "", avail, fmt.Errorf("file konfigurasi PostgreSQL (postgresql.conf) tidak ditemukan")
}

// stripPGComment membuang komentar `# ...` di luar tanda kutip tunggal dan merapikan kutip.
func stripPGValue(raw string) string {
	inQuotes := false
	var sb strings.Builder
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if ch == '\'' {
			inQuotes = !inQuotes
			sb.WriteByte(ch)
			continue
		}
		if ch == '#' && !inQuotes {
			break
		}
		sb.WriteByte(ch)
	}
	v := strings.TrimSpace(sb.String())
	if len(v) >= 2 && strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'") {
		v = v[1 : len(v)-1]
	}
	return strings.TrimSpace(v)
}

func pgActiveValue(lines []string, key string) string {
	var found string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		k, v, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) == key {
			found = stripPGValue(v)
		}
	}
	return found
}

// ParsePostgresConfig membaca nilai efektif dari postgresql.conf dan pg_hba.conf.
func ParsePostgresConfig(confContent, hbaContent string) PostgresConfig {
	lines := strings.Split(confContent, "\n")
	cfg := PostgresConfig{
		Port:                    5432,
		ListenAddresses:         "localhost",
		MaxConnections:          100,
		SharedBuffers:           "128MB",
		EffectiveCacheSize:      "4GB",
		WorkMem:                 "4MB",
		MaintenanceWorkMem:      "64MB",
		PasswordEncryption:      "scram-sha-256",
		Timezone:                "UTC",
		LogMinDurationStatement: -1,
		RemoteAccessMode:        "local",
	}

	if v := pgActiveValue(lines, "port"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			cfg.Port = p
		}
	}
	if v := pgActiveValue(lines, "listen_addresses"); v != "" {
		cfg.ListenAddresses = v
	}
	if v := pgActiveValue(lines, "max_connections"); v != "" {
		if mc, err := strconv.Atoi(v); err == nil && mc > 0 {
			cfg.MaxConnections = mc
		}
	}
	if v := pgActiveValue(lines, "shared_buffers"); v != "" {
		cfg.SharedBuffers = v
	}
	if v := pgActiveValue(lines, "effective_cache_size"); v != "" {
		cfg.EffectiveCacheSize = v
	}
	if v := pgActiveValue(lines, "work_mem"); v != "" {
		cfg.WorkMem = v
	}
	if v := pgActiveValue(lines, "maintenance_work_mem"); v != "" {
		cfg.MaintenanceWorkMem = v
	}
	if v := pgActiveValue(lines, "password_encryption"); v == "scram-sha-256" || v == "md5" {
		cfg.PasswordEncryption = v
	}
	if v := pgActiveValue(lines, "timezone"); v != "" {
		cfg.Timezone = v
	}
	if v := pgActiveValue(lines, "log_min_duration_statement"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil {
			cfg.LogMinDurationStatement = ms
		}
	}

	cfg.RemoteAccessMode = detectPGRemoteAccessMode(hbaContent)
	return cfg
}

func detectPGRemoteAccessMode(hbaContent string) string {
	for _, l := range strings.Split(hbaContent, "\n") {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 4 && strings.HasPrefix(fields[0], "host") {
			addr := fields[3]
			if addr == "0.0.0.0/0" || addr == "::/0" {
				return "all"
			}
			if addr == "100.64.0.0/10" || addr == "192.168.0.0/16" || addr == "10.0.0.0/8" || addr == "172.16.0.0/12" {
				return "private_vpn"
			}
		}
	}
	return "local"
}

func formatPGLine(key, value string, quote bool) string {
	if quote {
		escaped := strings.ReplaceAll(value, "'", "''")
		return fmt.Sprintf("%s = '%s'", key, escaped)
	}
	return fmt.Sprintf("%s = %s", key, value)
}

func setPGConfKey(lines []string, key, value string, quote bool) []string {
	wanted := formatPGLine(key, value, quote)
	activeIdx := -1
	commentIdx := -1
	out := make([]string, 0, len(lines))

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "#") {
			uncommented := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
			if k, _, ok := strings.Cut(uncommented, "="); ok && strings.TrimSpace(k) == key && commentIdx < 0 {
				commentIdx = len(out)
			}
			out = append(out, l)
			continue
		}
		if k, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(k) == key {
			if activeIdx >= 0 {
				// Hapus duplikat aktif agar hanya ada satu sumber kebenaran
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
			// Sisipkan tepat setelah baris komentar template bawaan
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

// ValidatePostgresConfig memvalidasi parameter konfigurasi PostgreSQL.
func ValidatePostgresConfig(cfg PostgresConfig) error {
	if err := validatePort(cfg.Port); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.ListenAddresses) == "" {
		return fmt.Errorf("listen_addresses wajib diisi (contoh: localhost atau *)")
	}
	if err := validateSingleLine(cfg.ListenAddresses, "listen_addresses"); err != nil {
		return err
	}
	if cfg.MaxConnections < 10 || cfg.MaxConnections > 5000 {
		return fmt.Errorf("max_connections harus antara 10 sampai 5000")
	}
	if err := validateMemSize(cfg.SharedBuffers, "shared_buffers", false); err != nil {
		return err
	}
	if err := validateMemSize(cfg.EffectiveCacheSize, "effective_cache_size", false); err != nil {
		return err
	}
	if err := validateMemSize(cfg.WorkMem, "work_mem", false); err != nil {
		return err
	}
	if err := validateMemSize(cfg.MaintenanceWorkMem, "maintenance_work_mem", false); err != nil {
		return err
	}
	if cfg.PasswordEncryption != "scram-sha-256" && cfg.PasswordEncryption != "md5" {
		return fmt.Errorf("password_encryption harus 'scram-sha-256' atau 'md5'")
	}
	if err := validateTimezone(cfg.Timezone); err != nil {
		return err
	}
	if cfg.LogMinDurationStatement < -1 || cfg.LogMinDurationStatement > 3600000 {
		return fmt.Errorf("log_min_duration_statement harus antara -1 sampai 3600000 ms")
	}
	switch cfg.RemoteAccessMode {
	case "local", "private_vpn", "all":
	default:
		return fmt.Errorf("remote_access_mode tidak valid: %s", cfg.RemoteAccessMode)
	}
	if cfg.NewPostgresPassword != "" {
		if err := validateSingleLine(cfg.NewPostgresPassword, "password postgres"); err != nil {
			return err
		}
	}
	return nil
}

// ApplyPostgresConfLines memperbarui baris konfigurasi postgresql.conf.
func ApplyPostgresConfLines(lines []string, cfg PostgresConfig) []string {
	lines = setPGConfKey(lines, "port", strconv.Itoa(cfg.Port), false)
	lines = setPGConfKey(lines, "listen_addresses", strings.TrimSpace(cfg.ListenAddresses), true)
	lines = setPGConfKey(lines, "max_connections", strconv.Itoa(cfg.MaxConnections), false)
	lines = setPGConfKey(lines, "shared_buffers", strings.TrimSpace(cfg.SharedBuffers), false)
	lines = setPGConfKey(lines, "effective_cache_size", strings.TrimSpace(cfg.EffectiveCacheSize), false)
	lines = setPGConfKey(lines, "work_mem", strings.TrimSpace(cfg.WorkMem), false)
	lines = setPGConfKey(lines, "maintenance_work_mem", strings.TrimSpace(cfg.MaintenanceWorkMem), false)
	lines = setPGConfKey(lines, "password_encryption", cfg.PasswordEncryption, false)
	lines = setPGConfKey(lines, "timezone", strings.TrimSpace(cfg.Timezone), true)
	lines = setPGConfKey(lines, "log_min_duration_statement", strconv.Itoa(cfg.LogMinDurationStatement), false)
	return lines
}

// ApplyPostgresHba mengelola blok akses remote terkelola wdev pada pg_hba.conf.
func ApplyPostgresHba(hbaContent, mode, authMethod string) string {
	if authMethod != "md5" {
		authMethod = "scram-sha-256"
	}

	lines := strings.Split(strings.TrimRight(hbaContent, "\n"), "\n")
	cleaned := make([]string, 0, len(lines))
	inBlock := false
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == pgHbaBlockBegin {
			inBlock = true
			continue
		}
		if trimmed == pgHbaBlockEnd {
			inBlock = false
			continue
		}
		if inBlock {
			continue
		}
		cleaned = append(cleaned, l)
	}

	// Buang baris kosong berlebih di akhir
	for len(cleaned) > 0 && strings.TrimSpace(cleaned[len(cleaned)-1]) == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}

	var block []string
	switch mode {
	case "private_vpn":
		block = []string{
			pgHbaBlockBegin,
			fmt.Sprintf("host    all             all             10.0.0.0/8              %s", authMethod),
			fmt.Sprintf("host    all             all             172.16.0.0/12           %s", authMethod),
			fmt.Sprintf("host    all             all             192.168.0.0/16          %s", authMethod),
			fmt.Sprintf("host    all             all             100.64.0.0/10           %s", authMethod),
			pgHbaBlockEnd,
		}
	case "all":
		block = []string{
			pgHbaBlockBegin,
			fmt.Sprintf("host    all             all             0.0.0.0/0               %s", authMethod),
			fmt.Sprintf("host    all             all             ::/0                    %s", authMethod),
			pgHbaBlockEnd,
		}
	}

	if len(block) > 0 {
		if len(cleaned) > 0 {
			cleaned = append(cleaned, "")
		}
		cleaned = append(cleaned, block...)
	}

	return strings.Join(cleaned, "\n") + "\n"
}

// GetPostgresConfig membaca konfigurasi PostgreSQL aktif (atau versi tertentu).
func (m *Manager) GetPostgresConfig(ctx context.Context, version string) (*PostgresConfig, error) {
	confPath, hbaPath, unit, resolvedVer, avail, err := m.resolvePostgresPaths(ctx, version)
	if err != nil {
		return nil, err
	}
	confRaw, err := m.readPrivilegedFile(ctx, confPath)
	if err != nil {
		return nil, err
	}
	hbaRaw, err := m.readPrivilegedFile(ctx, hbaPath)
	if err != nil {
		return nil, err
	}

	cfg := ParsePostgresConfig(confRaw, hbaRaw)
	cfg.Version = resolvedVer
	cfg.AvailableVersions = avail
	cfg.ConfigPath = confPath
	cfg.HbaPath = hbaPath
	cfg.ServiceUnit = unit
	cfg.Active = m.isServiceActive(ctx, unit) || m.isServiceActive(ctx, "postgresql")
	return &cfg, nil
}

// SavePostgresConfig menyimpan konfigurasi PostgreSQL (dan opsional password user postgres), lalu restart bila diminta.
func (m *Manager) SavePostgresConfig(ctx context.Context, cfg PostgresConfig) (*PostgresConfig, error) {
	if err := ValidatePostgresConfig(cfg); err != nil {
		return nil, err
	}

	confPath, hbaPath, unit, resolvedVer, _, err := m.resolvePostgresPaths(ctx, cfg.Version)
	if err != nil {
		return nil, err
	}

	origConf, err := m.readPrivilegedFile(ctx, confPath)
	if err != nil {
		return nil, err
	}
	origHba, err := m.readPrivilegedFile(ctx, hbaPath)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimRight(origConf, "\n"), "\n")
	updatedConf := strings.Join(ApplyPostgresConfLines(lines, cfg), "\n") + "\n"
	updatedHba := ApplyPostgresHba(origHba, cfg.RemoteAccessMode, cfg.PasswordEncryption)

	if err := m.writePrivilegedFile(ctx, confPath, []byte(updatedConf), "644"); err != nil {
		return nil, err
	}
	if err := m.writePrivilegedFile(ctx, hbaPath, []byte(updatedHba), "640"); err != nil {
		_ = m.writePrivilegedFile(ctx, confPath, []byte(origConf), "644")
		return nil, err
	}

	if cfg.Restart {
		if err := m.restartService(ctx, unit); err != nil {
			// Coba fallback unit "postgresql" bila unit versi spesifik tidak tersedia
			if unit != "postgresql" {
				if err2 := m.restartService(ctx, "postgresql"); err2 != nil {
					_ = m.writePrivilegedFile(ctx, confPath, []byte(origConf), "644")
					_ = m.writePrivilegedFile(ctx, hbaPath, []byte(origHba), "640")
					_ = m.restartService(ctx, unit)
					return nil, fmt.Errorf("gagal restart %s (konfigurasi dikembalikan): %w", unit, err)
				}
			} else {
				_ = m.writePrivilegedFile(ctx, confPath, []byte(origConf), "644")
				_ = m.writePrivilegedFile(ctx, hbaPath, []byte(origHba), "640")
				_ = m.restartService(ctx, unit)
				return nil, fmt.Errorf("gagal restart %s (konfigurasi dikembalikan): %w", unit, err)
			}
		}
	}

	if cfg.NewPostgresPassword != "" {
		if err := m.setPostgresRolePassword(ctx, cfg.Port, cfg.NewPostgresPassword); err != nil {
			return nil, fmt.Errorf("konfigurasi tersimpan, tetapi gagal memperbarui password user postgres: %w", err)
		}
	}

	return m.GetPostgresConfig(ctx, resolvedVer)
}

func (m *Manager) setPostgresRolePassword(ctx context.Context, port int, newPassword string) error {
	if port <= 0 {
		port = 5432
	}
	escaped := strings.ReplaceAll(newPassword, "'", "''")
	sqlCmd := fmt.Sprintf("ALTER USER postgres WITH PASSWORD '%s';\n", escaped)
	_, err := m.runPrivilegedCmdStdin(ctx, sqlCmd, "su", "-", "postgres", "-c", fmt.Sprintf("psql -p %d -v ON_ERROR_STOP=1", port))
	return err
}
