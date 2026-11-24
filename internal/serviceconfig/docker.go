package serviceconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const defaultDockerDaemonJSONPath = "/etc/docker/daemon.json"

// DockerConfig merepresentasikan konfigurasi daemon Docker (/etc/docker/daemon.json) dan akses grup docker.
type DockerConfig struct {
	ConfigPath             string   `json:"config_path"`
	ServiceUnit            string   `json:"service_unit"`
	Installed              bool     `json:"installed"`
	Active                 bool     `json:"active"`
	DockerVersion          string   `json:"docker_version,omitempty"`
	ComposeVersion         string   `json:"compose_version,omitempty"`
	DataRoot               string   `json:"data_root"`
	LogDriver              string   `json:"log_driver"`
	LogMaxSize             string   `json:"log_max_size"`
	LogMaxFile             int      `json:"log_max_file"`
	LiveRestore            bool     `json:"live_restore"`
	NoNewPrivileges        bool     `json:"no_new_privileges"`
	IPv6                   bool     `json:"ipv6"`
	UserlandProxy          bool     `json:"userland_proxy"`
	DNS                    []string `json:"dns,omitempty"`
	RegistryMirrors        []string `json:"registry_mirrors,omitempty"`
	DefaultAddressPoolBase string   `json:"default_address_pool_base,omitempty"`
	DefaultAddressPoolSize int      `json:"default_address_pool_size,omitempty"`
	DockerGroupUser        string   `json:"docker_group_user,omitempty"`
	AddUserToDockerGroup   bool     `json:"add_user_to_docker_group"`
	DockerGroupMembers     []string `json:"docker_group_members,omitempty"`
	Restart                bool     `json:"restart,omitempty"`
}

// DefaultDockerConfig mengembalikan konfigurasi default aman untuk Docker & Compose v5.
func DefaultDockerConfig() DockerConfig {
	return DockerConfig{
		ConfigPath:           defaultDockerDaemonJSONPath,
		ServiceUnit:          "docker",
		DataRoot:             "",
		LogDriver:            "json-file",
		LogMaxSize:           "10m",
		LogMaxFile:           3,
		LiveRestore:          true,
		NoNewPrivileges:      false,
		IPv6:                 false,
		UserlandProxy:        true,
		AddUserToDockerGroup: true,
		Restart:              true,
	}
}

// ParseDockerDaemonJSON membaca isi /etc/docker/daemon.json ke struct DockerConfig.
func ParseDockerDaemonJSON(content string) (DockerConfig, error) {
	cfg := DefaultDockerConfig()
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return cfg, nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return cfg, fmt.Errorf("isi %s bukan JSON valid: %w", defaultDockerDaemonJSONPath, err)
	}

	if v, ok := raw["data-root"].(string); ok {
		cfg.DataRoot = strings.TrimSpace(v)
	}
	if v, ok := raw["log-driver"].(string); ok && strings.TrimSpace(v) != "" {
		cfg.LogDriver = strings.TrimSpace(v)
	}
	if opts, ok := raw["log-opts"].(map[string]any); ok {
		if ms, ok := opts["max-size"].(string); ok && strings.TrimSpace(ms) != "" {
			cfg.LogMaxSize = strings.TrimSpace(ms)
		}
		switch mf := opts["max-file"].(type) {
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(mf)); err == nil && n > 0 {
				cfg.LogMaxFile = n
			}
		case float64:
			if int(mf) > 0 {
				cfg.LogMaxFile = int(mf)
			}
		}
	}
	if v, ok := raw["live-restore"].(bool); ok {
		cfg.LiveRestore = v
	}
	if v, ok := raw["no-new-privileges"].(bool); ok {
		cfg.NoNewPrivileges = v
	}
	if v, ok := raw["ipv6"].(bool); ok {
		cfg.IPv6 = v
	}
	if v, ok := raw["userland-proxy"].(bool); ok {
		cfg.UserlandProxy = v
	}
	if list, ok := raw["dns"].([]any); ok {
		for _, item := range list {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				cfg.DNS = append(cfg.DNS, strings.TrimSpace(s))
			}
		}
	}
	if list, ok := raw["registry-mirrors"].([]any); ok {
		for _, item := range list {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				cfg.RegistryMirrors = append(cfg.RegistryMirrors, strings.TrimSpace(s))
			}
		}
	}
	if pools, ok := raw["default-address-pools"].([]any); ok && len(pools) > 0 {
		if first, ok := pools[0].(map[string]any); ok {
			if base, ok := first["base"].(string); ok {
				cfg.DefaultAddressPoolBase = strings.TrimSpace(base)
			}
			if sz, ok := first["size"].(float64); ok && int(sz) > 0 {
				cfg.DefaultAddressPoolSize = int(sz)
			}
		}
	}
	return cfg, nil
}

// ValidateDockerConfig memvalidasi parameter Docker daemon.json sebelum disimpan.
func ValidateDockerConfig(cfg DockerConfig) error {
	dr := strings.TrimSpace(cfg.DataRoot)
	if dr != "" {
		if err := validateSingleLine(dr, "data-root"); err != nil {
			return err
		}
		if !filepath.IsAbs(dr) || strings.Contains(dr, "..") {
			return fmt.Errorf("data-root harus berupa path absolut yang valid (mis. /mnt/data/docker)")
		}
		clean := filepath.Clean(dr)
		if clean == "/" || clean == "/etc" || clean == "/usr" || clean == "/bin" || clean == "/sbin" {
			return fmt.Errorf("data-root tidak boleh menunjuk direktori sistem %q", clean)
		}
	}

	ld := strings.ToLower(strings.TrimSpace(cfg.LogDriver))
	if ld == "" {
		ld = "json-file"
	}
	switch ld {
	case "json-file", "local", "journald", "none":
	default:
		return fmt.Errorf("log-driver tidak didukung: %q (gunakan json-file, local, journald, atau none)", cfg.LogDriver)
	}

	if ld == "json-file" || ld == "local" {
		if err := validateMemSize(cfg.LogMaxSize, "log max-size", false); err != nil {
			return err
		}
		if cfg.LogMaxFile < 1 || cfg.LogMaxFile > 30 {
			return fmt.Errorf("log max-file harus antara 1 sampai 30")
		}
	}

	for _, d := range cfg.DNS {
		clean := strings.TrimSpace(d)
		if clean == "" {
			continue
		}
		if net.ParseIP(clean) == nil {
			return fmt.Errorf("alamat DNS tidak valid: %q", clean)
		}
	}

	for _, m := range cfg.RegistryMirrors {
		clean := strings.TrimSpace(m)
		if clean == "" {
			continue
		}
		if err := validateSingleLine(clean, "registry-mirror"); err != nil {
			return err
		}
		if !strings.HasPrefix(clean, "https://") && !strings.HasPrefix(clean, "http://") {
			return fmt.Errorf("registry mirror harus diawali http:// atau https:// (%q)", clean)
		}
	}

	if base := strings.TrimSpace(cfg.DefaultAddressPoolBase); base != "" {
		if _, _, err := net.ParseCIDR(base); err != nil {
			return fmt.Errorf("CIDR default-address-pools tidak valid: %q", base)
		}
		if cfg.DefaultAddressPoolSize < 16 || cfg.DefaultAddressPoolSize > 29 {
			return fmt.Errorf("ukuran subnet default-address-pools harus antara 16 sampai 29")
		}
	}

	if u := strings.TrimSpace(cfg.DockerGroupUser); u != "" {
		if err := validateSingleLine(u, "docker_group_user"); err != nil {
			return err
		}
		if strings.ContainsAny(u, " \t:;") {
			return fmt.Errorf("nama user grup docker tidak valid: %q", u)
		}
	}

	return nil
}

// ApplyDockerDaemonJSON menggabungkan DockerConfig ke JSON /etc/docker/daemon.json tanpa menghapus field custom lain.
func ApplyDockerDaemonJSON(existingContent string, cfg DockerConfig) ([]byte, error) {
	raw := make(map[string]any)
	if trimmed := strings.TrimSpace(existingContent); trimmed != "" {
		if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
			// Backup behavior handled by caller; start fresh map only if empty
			return nil, fmt.Errorf("gagal mem-parsing %s yang ada: %w", defaultDockerDaemonJSONPath, err)
		}
	}

	if dr := strings.TrimSpace(cfg.DataRoot); dr != "" && filepath.Clean(dr) != "/var/lib/docker" {
		raw["data-root"] = filepath.Clean(dr)
	} else {
		delete(raw, "data-root")
	}

	ld := strings.ToLower(strings.TrimSpace(cfg.LogDriver))
	if ld == "" {
		ld = "json-file"
	}
	raw["log-driver"] = ld
	if ld == "json-file" || ld == "local" {
		raw["log-opts"] = map[string]any{
			"max-size": strings.ToLower(strings.TrimSpace(cfg.LogMaxSize)),
			"max-file": strconv.Itoa(cfg.LogMaxFile),
		}
	} else {
		delete(raw, "log-opts")
	}

	raw["live-restore"] = cfg.LiveRestore
	if cfg.NoNewPrivileges {
		raw["no-new-privileges"] = true
	} else {
		delete(raw, "no-new-privileges")
	}
	if cfg.IPv6 {
		raw["ipv6"] = true
	} else {
		delete(raw, "ipv6")
	}
	if !cfg.UserlandProxy {
		raw["userland-proxy"] = false
	} else {
		delete(raw, "userland-proxy")
	}

	var dnsClean []string
	for _, d := range cfg.DNS {
		if c := strings.TrimSpace(d); c != "" {
			dnsClean = append(dnsClean, c)
		}
	}
	if len(dnsClean) > 0 {
		raw["dns"] = dnsClean
	} else {
		delete(raw, "dns")
	}

	var mirrorsClean []string
	for _, m := range cfg.RegistryMirrors {
		if c := strings.TrimSpace(m); c != "" {
			mirrorsClean = append(mirrorsClean, c)
		}
	}
	if len(mirrorsClean) > 0 {
		raw["registry-mirrors"] = mirrorsClean
	} else {
		delete(raw, "registry-mirrors")
	}

	if base := strings.TrimSpace(cfg.DefaultAddressPoolBase); base != "" && cfg.DefaultAddressPoolSize > 0 {
		raw["default-address-pools"] = []map[string]any{
			{
				"base": base,
				"size": cfg.DefaultAddressPoolSize,
			},
		}
	} else {
		delete(raw, "default-address-pools")
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func resolveDefaultNonRootUser() string {
	if sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER")); sudoUser != "" && sudoUser != "root" {
		return sudoUser
	}
	if u, err := user.Current(); err == nil && u.Username != "" && u.Username != "root" {
		return u.Username
	}
	if data, err := os.ReadFile("/etc/passwd"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			parts := strings.Split(line, ":")
			if len(parts) < 7 {
				continue
			}
			uid, err := strconv.Atoi(parts[2])
			if err != nil || uid < 1000 || uid == 65534 {
				continue
			}
			shell := strings.TrimSpace(parts[6])
			if strings.HasSuffix(shell, "nologin") || strings.HasSuffix(shell, "/false") {
				continue
			}
			return parts[0]
		}
	}
	return ""
}

func readDockerGroupMembers() []string {
	data, err := os.ReadFile("/etc/group")
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "docker:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 4 && strings.TrimSpace(parts[3]) != "" {
				var members []string
				for _, m := range strings.Split(parts[3], ",") {
					if c := strings.TrimSpace(m); c != "" {
						members = append(members, c)
					}
				}
				sort.Strings(members)
				return members
			}
		}
	}
	return nil
}

func isDockerBinaryInstalled() bool {
	if _, err := exec.LookPath("docker"); err == nil {
		return true
	}
	for _, p := range []string{"/usr/bin/docker", "/usr/local/bin/docker"} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

// GetDockerConfig membaca konfigurasi aktif atau pra-konfigurasi /etc/docker/daemon.json.
func (m *Manager) GetDockerConfig(ctx context.Context) (*DockerConfig, error) {
	cfg := DefaultDockerConfig()
	exists, err := m.fileExistsPrivileged(ctx, defaultDockerDaemonJSONPath)
	if err != nil {
		return nil, err
	}
	if exists {
		content, rerr := m.readPrivilegedFile(ctx, defaultDockerDaemonJSONPath)
		if rerr != nil {
			return nil, rerr
		}
		parsed, perr := ParseDockerDaemonJSON(content)
		if perr != nil {
			return nil, perr
		}
		cfg = parsed
	}

	cfg.ConfigPath = defaultDockerDaemonJSONPath
	cfg.ServiceUnit = "docker"
	cfg.Installed = isDockerBinaryInstalled()
	cfg.Active = m.isServiceActive(ctx, "docker")
	cfg.DockerGroupMembers = readDockerGroupMembers()

	targetUser := resolveDefaultNonRootUser()
	cfg.DockerGroupUser = targetUser
	if targetUser != "" && len(cfg.DockerGroupMembers) > 0 {
		inGroup := false
		for _, member := range cfg.DockerGroupMembers {
			if member == targetUser {
				inGroup = true
				break
			}
		}
		cfg.AddUserToDockerGroup = inGroup
	}

	if cfg.Installed {
		if out, err := exec.CommandContext(ctx, "docker", "--version").CombinedOutput(); err == nil {
			fields := strings.Fields(string(out))
			if len(fields) >= 3 {
				cfg.DockerVersion = strings.TrimSuffix(strings.TrimPrefix(fields[2], "v"), ",")
			}
		}
		if out, err := exec.CommandContext(ctx, "docker", "compose", "version", "--short").CombinedOutput(); err == nil {
			if v := strings.TrimSpace(string(out)); v != "" {
				cfg.ComposeVersion = strings.TrimPrefix(v, "v")
			}
		}
	}

	return &cfg, nil
}

// SaveDockerConfig memvalidasi dan menyimpan /etc/docker/daemon.json, opsi grup docker, dan opsional restart daemon.
func (m *Manager) SaveDockerConfig(ctx context.Context, cfg DockerConfig) (*DockerConfig, error) {
	if err := ValidateDockerConfig(cfg); err != nil {
		return nil, err
	}

	existing := ""
	if exists, err := m.fileExistsPrivileged(ctx, defaultDockerDaemonJSONPath); err != nil {
		return nil, err
	} else if exists {
		if raw, rerr := m.readPrivilegedFile(ctx, defaultDockerDaemonJSONPath); rerr == nil {
			existing = raw
		}
	}

	mergedBytes, err := ApplyDockerDaemonJSON(existing, cfg)
	if err != nil {
		return nil, err
	}

	if dr := strings.TrimSpace(cfg.DataRoot); dr != "" && filepath.Clean(dr) != "/var/lib/docker" {
		if _, err := m.runPrivilegedCmd(ctx, "mkdir", "-p", filepath.Clean(dr)); err != nil {
			return nil, fmt.Errorf("gagal menyiapkan direktori data-root %s: %w", dr, err)
		}
	}

	if err := m.writePrivilegedFile(ctx, defaultDockerDaemonJSONPath, mergedBytes, "644"); err != nil {
		return nil, err
	}

	targetUser := strings.TrimSpace(cfg.DockerGroupUser)
	if targetUser == "" {
		targetUser = resolveDefaultNonRootUser()
	}
	if cfg.AddUserToDockerGroup && targetUser != "" && targetUser != "root" {
		if _, gerr := m.runPrivilegedCmd(ctx, "getent", "group", "docker"); gerr == nil {
			if _, uerr := m.runPrivilegedCmd(ctx, "usermod", "-aG", "docker", targetUser); uerr != nil {
				if errors.Is(uerr, ErrSudoRequired) {
					return nil, uerr
				}
			}
		}
	}

	if cfg.Restart && isDockerBinaryInstalled() {
		if err := m.restartService(ctx, "docker"); err != nil {
			return nil, fmt.Errorf("konfigurasi tersimpan ke %s, namun gagal restart service docker: %w", defaultDockerDaemonJSONPath, err)
		}
	}

	return m.GetDockerConfig(ctx)
}
