package serviceconfig

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// RedisConfig merepresentasikan parameter konfigurasi Redis/Valkey yang dapat diatur di dashboard.
type RedisConfig struct {
	ConfigPath      string `json:"config_path"`
	ServiceUnit     string `json:"service_unit"`
	Active          bool   `json:"active"`
	Port            int    `json:"port"`
	Bind            string `json:"bind"`
	ProtectedMode   string `json:"protected_mode"`
	HasPassword     bool   `json:"has_password"`
	Password        string `json:"password,omitempty"`
	ClearPassword   bool   `json:"clear_password,omitempty"`
	MaxMemory       string `json:"maxmemory"`
	MaxMemoryPolicy string `json:"maxmemory_policy"`
	PersistenceMode string `json:"persistence_mode"` // "rdb", "aof", "both", "none"
	MaxClients      int    `json:"maxclients"`
	Databases       int    `json:"databases"`
	Restart         bool   `json:"restart,omitempty"`
}

var redisConfCandidates = []string{
	"/etc/redis/redis.conf",
	"/etc/valkey/valkey.conf",
	"/etc/redis.conf",
	"/etc/valkey.conf",
}

var validRedisPolicies = map[string]bool{
	"noeviction":      true,
	"allkeys-lru":     true,
	"volatile-lru":    true,
	"allkeys-lfu":     true,
	"volatile-lfu":    true,
	"volatile-ttl":    true,
	"allkeys-random":  true,
	"volatile-random": true,
}

// FindRedisConf mengembalikan path file konfigurasi Redis/Valkey yang tersedia.
func FindRedisConf() string {
	for _, p := range redisConfCandidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// ResolveRedisServiceUnit menentukan nama unit systemd Redis/Valkey aktif atau terpasang.
func ResolveRedisServiceUnit() string {
	units := []struct {
		name string
		path string
	}{
		{"valkey", "/usr/lib/systemd/system/valkey.service"},
		{"valkey-server", "/lib/systemd/system/valkey-server.service"},
		{"redis-server", "/lib/systemd/system/redis-server.service"},
		{"redis", "/usr/lib/systemd/system/redis.service"},
	}
	for _, u := range units {
		if _, err := os.Stat(u.path); err == nil {
			return u.name
		}
	}
	return "redis-server"
}

func redisActiveConfValue(lines []string, key string) string {
	var val string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == key {
			val = ""
			continue
		}
		if strings.HasPrefix(trimmed, key+" ") || strings.HasPrefix(trimmed, key+"\t") {
			rest := strings.TrimSpace(trimmed[len(key):])
			val = strings.Trim(rest, "\"'")
		}
	}
	return val
}

func redisHasActiveSaveRule(lines []string) (hasRule bool, explicitlyDisabled bool) {
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "save ") || strings.HasPrefix(trimmed, "save\t") {
			arg := strings.TrimSpace(trimmed[4:])
			if arg == `""` || arg == `''` || arg == "" {
				explicitlyDisabled = true
				hasRule = false
			} else {
				hasRule = true
				explicitlyDisabled = false
			}
		}
	}
	return hasRule, explicitlyDisabled
}

// ParseRedisConfig membaca directive penting dari isi file redis.conf/valkey.conf.
func ParseRedisConfig(content string) RedisConfig {
	lines := strings.Split(content, "\n")
	cfg := RedisConfig{
		Port:            6379,
		Bind:            "127.0.0.1 -::1",
		ProtectedMode:   "yes",
		MaxMemory:       "0",
		MaxMemoryPolicy: "noeviction",
		PersistenceMode: "rdb",
		MaxClients:      10000,
		Databases:       16,
	}

	if v := redisActiveConfValue(lines, "port"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p >= 0 && p <= 65535 {
			cfg.Port = p
		}
	}
	if v := redisActiveConfValue(lines, "bind"); v != "" {
		cfg.Bind = v
	}
	if v := redisActiveConfValue(lines, "protected-mode"); v == "yes" || v == "no" {
		cfg.ProtectedMode = v
	}
	if v := redisActiveConfValue(lines, "requirepass"); v != "" {
		cfg.HasPassword = true
	}
	if v := redisActiveConfValue(lines, "maxmemory"); v != "" {
		cfg.MaxMemory = v
	}
	if v := redisActiveConfValue(lines, "maxmemory-policy"); validRedisPolicies[v] {
		cfg.MaxMemoryPolicy = v
	}
	if v := redisActiveConfValue(lines, "maxclients"); v != "" {
		if mc, err := strconv.Atoi(v); err == nil && mc > 0 {
			cfg.MaxClients = mc
		}
	}
	if v := redisActiveConfValue(lines, "databases"); v != "" {
		if db, err := strconv.Atoi(v); err == nil && db > 0 {
			cfg.Databases = db
		}
	}

	aof := strings.EqualFold(redisActiveConfValue(lines, "appendonly"), "yes")
	hasRDB, rdbDisabled := redisHasActiveSaveRule(lines)
	// Default Redis bila tidak ada direktif save sama sekali adalah RDB aktif
	rdbOn := hasRDB || !rdbDisabled

	switch {
	case aof && rdbOn:
		cfg.PersistenceMode = "both"
	case aof && !rdbOn:
		cfg.PersistenceMode = "aof"
	case !aof && rdbOn:
		cfg.PersistenceMode = "rdb"
	default:
		cfg.PersistenceMode = "none"
	}

	return cfg
}

func setRedisConfKey(lines []string, key, value string) []string {
	wanted := key + " " + value
	activeIdx := -1
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "#") {
			out = append(out, l)
			continue
		}
		if trimmed == key || strings.HasPrefix(trimmed, key+" ") || strings.HasPrefix(trimmed, key+"\t") {
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
		out = append(out, wanted)
	}
	return out
}

func removeRedisConfKey(lines []string, key string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if !strings.HasPrefix(trimmed, "#") &&
			(trimmed == key || strings.HasPrefix(trimmed, key+" ") || strings.HasPrefix(trimmed, key+"\t")) {
			continue
		}
		out = append(out, l)
	}
	return out
}

func setRedisSaveMode(lines []string, mode string) []string {
	var existingRules []string
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if !strings.HasPrefix(trimmed, "#") &&
			(strings.HasPrefix(trimmed, "save ") || strings.HasPrefix(trimmed, "save\t")) {
			arg := strings.TrimSpace(trimmed[4:])
			if arg != "" && arg != `""` && arg != `''` {
				existingRules = append(existingRules, trimmed)
			}
			continue
		}
		out = append(out, l)
	}

	switch mode {
	case "aof", "none":
		out = append(out, `save ""`)
	case "rdb", "both":
		if len(existingRules) > 0 {
			out = append(out, existingRules...)
		} else {
			out = append(out, "save 3600 1 300 100 60 10000")
		}
	}
	return out
}

// ValidateRedisConfig memvalidasi parameter konfigurasi Redis sebelum disimpan.
func ValidateRedisConfig(cfg RedisConfig) error {
	if err := validatePort(cfg.Port); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Bind) == "" {
		return fmt.Errorf("bind address wajib diisi (contoh: 127.0.0.1 -::1 atau 0.0.0.0)")
	}
	if err := validateSingleLine(cfg.Bind, "bind address"); err != nil {
		return err
	}
	if cfg.ProtectedMode != "yes" && cfg.ProtectedMode != "no" {
		return fmt.Errorf("protected-mode harus 'yes' atau 'no'")
	}
	if cfg.Password != "" {
		if err := validateSingleLine(cfg.Password, "password"); err != nil {
			return err
		}
		if strings.ContainsAny(cfg.Password, `"'\`) {
			return fmt.Errorf("password Redis tidak boleh mengandung tanda kutip atau backslash")
		}
	}
	if err := validateMemSize(cfg.MaxMemory, "maxmemory", true); err != nil {
		return err
	}
	if !validRedisPolicies[cfg.MaxMemoryPolicy] {
		return fmt.Errorf("maxmemory-policy tidak valid: %s", cfg.MaxMemoryPolicy)
	}
	switch cfg.PersistenceMode {
	case "rdb", "aof", "both", "none":
	default:
		return fmt.Errorf("mode persistensi tidak valid: %s", cfg.PersistenceMode)
	}
	if cfg.MaxClients < 1 || cfg.MaxClients > 1000000 {
		return fmt.Errorf("maxclients harus antara 1 sampai 1000000")
	}
	if cfg.Databases < 1 || cfg.Databases > 256 {
		return fmt.Errorf("jumlah databases harus antara 1 sampai 256")
	}
	return nil
}

// ApplyRedisConfigLines memperbarui baris konfigurasi Redis sesuai RedisConfig.
func ApplyRedisConfigLines(lines []string, cfg RedisConfig) []string {
	// Jaga kompatibilitas systemd Type=notify
	lines = setRedisConfKey(lines, "daemonize", "no")
	lines = setRedisConfKey(lines, "supervised", "systemd")
	lines = setRedisConfKey(lines, "port", strconv.Itoa(cfg.Port))
	lines = setRedisConfKey(lines, "bind", strings.TrimSpace(cfg.Bind))
	lines = setRedisConfKey(lines, "protected-mode", cfg.ProtectedMode)
	lines = setRedisConfKey(lines, "maxmemory", strings.ToLower(strings.TrimSpace(cfg.MaxMemory)))
	lines = setRedisConfKey(lines, "maxmemory-policy", cfg.MaxMemoryPolicy)
	lines = setRedisConfKey(lines, "maxclients", strconv.Itoa(cfg.MaxClients))
	lines = setRedisConfKey(lines, "databases", strconv.Itoa(cfg.Databases))

	if cfg.ClearPassword {
		lines = removeRedisConfKey(lines, "requirepass")
	} else if cfg.Password != "" {
		lines = setRedisConfKey(lines, "requirepass", cfg.Password)
	}

	switch cfg.PersistenceMode {
	case "aof", "both":
		lines = setRedisConfKey(lines, "appendonly", "yes")
		lines = setRedisConfKey(lines, "appendfsync", "everysec")
	default:
		lines = setRedisConfKey(lines, "appendonly", "no")
	}
	lines = setRedisSaveMode(lines, cfg.PersistenceMode)

	return lines
}

// GetRedisConfig membaca konfigurasi Redis/Valkey aktif dari sistem.
func (m *Manager) GetRedisConfig(ctx context.Context) (*RedisConfig, error) {
	confPath := FindRedisConf()
	if confPath == "" {
		return nil, fmt.Errorf("file konfigurasi Redis/Valkey tidak ditemukan")
	}
	raw, err := m.readPrivilegedFile(ctx, confPath)
	if err != nil {
		return nil, err
	}
	cfg := ParseRedisConfig(raw)
	cfg.ConfigPath = confPath
	cfg.ServiceUnit = ResolveRedisServiceUnit()
	cfg.Active = m.isServiceActive(ctx, cfg.ServiceUnit)
	return &cfg, nil
}

// SaveRedisConfig memvalidasi, menyimpan konfigurasi Redis/Valkey, dan me-restart layanan bila diminta.
func (m *Manager) SaveRedisConfig(ctx context.Context, cfg RedisConfig) (*RedisConfig, error) {
	if err := ValidateRedisConfig(cfg); err != nil {
		return nil, err
	}
	confPath := FindRedisConf()
	if confPath == "" {
		return nil, fmt.Errorf("file konfigurasi Redis/Valkey tidak ditemukan")
	}

	origRaw, err := m.readPrivilegedFile(ctx, confPath)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimRight(origRaw, "\n"), "\n")
	updatedLines := ApplyRedisConfigLines(lines, cfg)
	updatedContent := strings.Join(updatedLines, "\n") + "\n"

	if err := m.writePrivilegedFile(ctx, confPath, []byte(updatedContent), "640"); err != nil {
		return nil, err
	}

	unit := ResolveRedisServiceUnit()
	if cfg.Restart {
		if err := m.restartService(ctx, unit); err != nil {
			// Rollback otomatis ke konfigurasi sebelumnya agar layanan tidak mati
			_ = m.writePrivilegedFile(ctx, confPath, []byte(origRaw), "640")
			_ = m.restartService(ctx, unit)
			return nil, fmt.Errorf("gagal menjalankan ulang %s (konfigurasi dikembalikan): %w", unit, err)
		}
	}

	return m.GetRedisConfig(ctx)
}
