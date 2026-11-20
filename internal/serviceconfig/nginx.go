package serviceconfig

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// NginxConfig merepresentasikan parameter global Nginx pada /etc/nginx/nginx.conf.
type NginxConfig struct {
	ConfigPath        string `json:"config_path"`
	ServiceUnit       string `json:"service_unit"`
	Active            bool   `json:"active"`
	WorkerProcesses   string `json:"worker_processes"`
	WorkerConnections int    `json:"worker_connections"`
	ClientMaxBodySize string `json:"client_max_body_size"`
	KeepaliveTimeout  int    `json:"keepalive_timeout"`
	Gzip              bool   `json:"gzip"`
	ServerTokens      bool   `json:"server_tokens"`
	Restart           bool   `json:"restart,omitempty"`
}

const defaultNginxConfPath = "/etc/nginx/nginx.conf"

// ParseNginxConfig membaca directive global utama dari isi nginx.conf.
func ParseNginxConfig(content string) NginxConfig {
	cfg := NginxConfig{
		WorkerProcesses:   "auto",
		WorkerConnections: 1024,
		ClientMaxBodySize: "64M",
		KeepaliveTimeout:  65,
		Gzip:              true,
		ServerTokens:      true,
	}

	section := "main"
	depth := 0
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if idx := strings.Index(line, "#"); idx != -1 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}

		// Cek pembukaan block sebelum memproses directive satu baris
		if strings.HasPrefix(line, "events") && strings.Contains(line, "{") && depth == 0 {
			section = "events"
			depth += strings.Count(line, "{") - strings.Count(line, "}")
			continue
		}
		if strings.HasPrefix(line, "http") && strings.Contains(line, "{") && depth == 0 {
			section = "http"
			depth += strings.Count(line, "{") - strings.Count(line, "}")
			continue
		}

		depth += strings.Count(line, "{") - strings.Count(line, "}")
		if depth <= 0 {
			depth = 0
			section = "main"
		}

		clean := strings.TrimSuffix(line, ";")
		fields := strings.Fields(clean)
		if len(fields) < 2 {
			continue
		}

		key, val := fields[0], fields[1]
		switch {
		case section == "main" && key == "worker_processes":
			cfg.WorkerProcesses = val
		case section == "events" && key == "worker_connections":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.WorkerConnections = n
			}
		case section == "http" && key == "client_max_body_size":
			cfg.ClientMaxBodySize = val
		case section == "http" && key == "keepalive_timeout":
			if n, err := strconv.Atoi(val); err == nil && n >= 0 {
				cfg.KeepaliveTimeout = n
			}
		case section == "http" && key == "gzip":
			cfg.Gzip = strings.EqualFold(val, "on")
		case section == "http" && key == "server_tokens":
			cfg.ServerTokens = strings.EqualFold(val, "on")
		}
	}

	return cfg
}

// ValidateNginxConfig memvalidasi parameter konfigurasi Nginx sebelum disimpan.
func ValidateNginxConfig(cfg NginxConfig) error {
	wp := strings.TrimSpace(cfg.WorkerProcesses)
	if wp != "auto" {
		n, err := strconv.Atoi(wp)
		if err != nil || n < 1 || n > 128 {
			return fmt.Errorf("worker_processes harus 'auto' atau angka 1 sampai 128")
		}
	}
	if cfg.WorkerConnections < 64 || cfg.WorkerConnections > 65535 {
		return fmt.Errorf("worker_connections harus antara 64 sampai 65535")
	}
	if err := validateMemSize(cfg.ClientMaxBodySize, "client_max_body_size", true); err != nil {
		return err
	}
	if cfg.KeepaliveTimeout < 0 || cfg.KeepaliveTimeout > 3600 {
		return fmt.Errorf("keepalive_timeout harus antara 0 sampai 3600 detik")
	}
	return nil
}

// ApplyNginxConfLines memperbarui directive global di nginx.conf dengan tetap mempertahankan struktur block.
func ApplyNginxConfLines(lines []string, cfg NginxConfig) []string {
	gzipVal := "off"
	if cfg.Gzip {
		gzipVal = "on"
	}
	tokensVal := "off"
	if cfg.ServerTokens {
		tokensVal = "on"
	}

	targets := map[string]struct {
		section string
		value   string
		done    bool
	}{
		"worker_processes":     {section: "main", value: strings.TrimSpace(cfg.WorkerProcesses)},
		"worker_connections":   {section: "events", value: strconv.Itoa(cfg.WorkerConnections)},
		"client_max_body_size": {section: "http", value: strings.TrimSpace(cfg.ClientMaxBodySize)},
		"keepalive_timeout":    {section: "http", value: strconv.Itoa(cfg.KeepaliveTimeout)},
		"gzip":                 {section: "http", value: gzipVal},
		"server_tokens":        {section: "http", value: tokensVal},
	}

	section := "main"
	depth := 0
	eventsCloseIdx := -1
	httpCloseIdx := -1
	mainInsertIdx := -1

	out := make([]string, 0, len(lines)+6)
	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		codePart := trimmed
		if idx := strings.Index(codePart, "#"); idx != -1 {
			codePart = strings.TrimSpace(codePart[:idx])
		}

		// Deteksi baris pembuka section
		if depth == 0 && strings.HasPrefix(codePart, "events") && strings.Contains(codePart, "{") {
			section = "events"
			depth += strings.Count(codePart, "{") - strings.Count(codePart, "}")
			out = append(out, rawLine)
			continue
		}
		if depth == 0 && strings.HasPrefix(codePart, "http") && strings.Contains(codePart, "{") {
			if mainInsertIdx < 0 {
				mainInsertIdx = len(out)
			}
			section = "http"
			depth += strings.Count(codePart, "{") - strings.Count(codePart, "}")
			out = append(out, rawLine)
			continue
		}

		prevDepth := depth
		depth += strings.Count(codePart, "{") - strings.Count(codePart, "}")

		// Penutup block events atau http pada level 1 -> 0
		if prevDepth == 1 && depth == 0 && codePart == "}" {
			if section == "events" {
				eventsCloseIdx = len(out)
			} else if section == "http" {
				httpCloseIdx = len(out)
			}
			section = "main"
			out = append(out, rawLine)
			continue
		}
		if depth <= 0 {
			depth = 0
			section = "main"
		}

		if codePart != "" && !strings.Contains(codePart, "{") && !strings.Contains(codePart, "}") {
			fields := strings.Fields(strings.TrimSuffix(codePart, ";"))
			if len(fields) >= 2 {
				key := fields[0]
				if t, ok := targets[key]; ok && ((section == "main" && depth == 0 && t.section == "main") || (section == t.section && depth == 1)) {
					if !t.done {
						indent := rawLine[:len(rawLine)-len(strings.TrimLeft(rawLine, " \t"))]
						out = append(out, fmt.Sprintf("%s%s %s;", indent, key, t.value))
						t.done = true
						targets[key] = t
					}
					continue
				}
			}
		}

		out = append(out, rawLine)
	}

	// Sisipkan directive yang belum ada ke section yang sesuai (dari bawah ke atas agar indeks tidak bergeser)
	insertAt := func(idx int, newLines ...string) {
		if idx < 0 || idx > len(out) {
			out = append(out, newLines...)
			return
		}
		res := make([]string, 0, len(out)+len(newLines))
		res = append(res, out[:idx]...)
		res = append(res, newLines...)
		res = append(res, out[idx:]...)
		out = res
	}

	var httpMissing []string
	for _, key := range []string{"client_max_body_size", "keepalive_timeout", "gzip", "server_tokens"} {
		if t := targets[key]; !t.done {
			httpMissing = append(httpMissing, fmt.Sprintf("    %s %s;", key, t.value))
		}
	}
	if len(httpMissing) > 0 && httpCloseIdx >= 0 {
		insertAt(httpCloseIdx, httpMissing...)
	}

	if t := targets["worker_connections"]; !t.done && eventsCloseIdx >= 0 {
		insertAt(eventsCloseIdx, fmt.Sprintf("    worker_connections %s;", t.value))
	}

	if t := targets["worker_processes"]; !t.done {
		insertAt(mainInsertIdx, fmt.Sprintf("worker_processes %s;", t.value))
	}

	return out
}

// GetNginxConfig membaca konfigurasi Nginx global dari /etc/nginx/nginx.conf.
func (m *Manager) GetNginxConfig(ctx context.Context) (*NginxConfig, error) {
	if _, err := os.Stat(defaultNginxConfPath); err != nil {
		return nil, fmt.Errorf("file konfigurasi Nginx (%s) tidak ditemukan", defaultNginxConfPath)
	}
	raw, err := m.readPrivilegedFile(ctx, defaultNginxConfPath)
	if err != nil {
		return nil, err
	}
	cfg := ParseNginxConfig(raw)
	cfg.ConfigPath = defaultNginxConfPath
	cfg.ServiceUnit = "nginx"
	cfg.Active = m.isServiceActive(ctx, "nginx")
	return &cfg, nil
}

// SaveNginxConfig memvalidasi, menulis nginx.conf, menguji dengan `nginx -t`, dan me-restart bila diminta.
func (m *Manager) SaveNginxConfig(ctx context.Context, cfg NginxConfig) (*NginxConfig, error) {
	if err := ValidateNginxConfig(cfg); err != nil {
		return nil, err
	}
	origRaw, err := m.readPrivilegedFile(ctx, defaultNginxConfPath)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimRight(origRaw, "\n"), "\n")
	updated := strings.Join(ApplyNginxConfLines(lines, cfg), "\n") + "\n"

	if err := m.writePrivilegedFile(ctx, defaultNginxConfPath, []byte(updated), "644"); err != nil {
		return nil, err
	}

	nginxBin := "/usr/sbin/nginx"
	if _, err := os.Stat(nginxBin); err != nil {
		if p, lErr := exec.LookPath("nginx"); lErr == nil {
			nginxBin = p
		} else {
			nginxBin = ""
		}
	}
	if nginxBin != "" {
		if _, err := m.runPrivilegedCmd(ctx, nginxBin, "-t"); err != nil {
			_ = m.writePrivilegedFile(ctx, defaultNginxConfPath, []byte(origRaw), "644")
			return nil, fmt.Errorf("validasi nginx -t gagal (perubahan dibatalkan): %w", err)
		}
	}

	if cfg.Restart {
		if err := m.restartService(ctx, "nginx"); err != nil {
			_ = m.writePrivilegedFile(ctx, defaultNginxConfPath, []byte(origRaw), "644")
			_ = m.restartService(ctx, "nginx")
			return nil, fmt.Errorf("gagal restart nginx (konfigurasi dikembalikan): %w", err)
		}
	}

	return m.GetNginxConfig(ctx)
}
