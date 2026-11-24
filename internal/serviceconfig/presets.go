package serviceconfig

import (
	"context"
	"strings"
)

// PresetCustom digunakan saat konfigurasi aktif layanan berada di luar seluruh preset bawaan.
const PresetCustom = "Custom"

// DetectNginxPreset menentukan preset Nginx yang cocok dengan parameter aktif, atau "Custom".
func DetectNginxPreset(cfg NginxConfig) string {
	wp := strings.TrimSpace(cfg.WorkerProcesses)
	maxBody := strings.ToLower(strings.TrimSpace(cfg.ClientMaxBodySize))

	switch {
	case strings.EqualFold(wp, "auto") &&
		cfg.WorkerConnections == 1024 &&
		maxBody == "64m" &&
		cfg.KeepaliveTimeout == 65 &&
		cfg.Gzip &&
		cfg.ServerTokens:
		return "Dev Standar (64M)"
	case strings.EqualFold(wp, "auto") &&
		cfg.WorkerConnections == 2048 &&
		maxBody == "256m" &&
		cfg.KeepaliveTimeout == 120 &&
		cfg.Gzip &&
		!cfg.ServerTokens:
		return "Upload Besar (256M)"
	case strings.EqualFold(wp, "auto") &&
		cfg.WorkerConnections == 4096 &&
		maxBody == "64m" &&
		cfg.KeepaliveTimeout == 65 &&
		cfg.Gzip &&
		!cfg.ServerTokens:
		return "Production Hardened"
	default:
		return PresetCustom
	}
}

// DetectPHPPreset menentukan preset PHP & PHP-FPM yang cocok dengan parameter aktif, atau "Custom".
func DetectPHPPreset(cfg PHPConfig) string {
	tz := strings.TrimSpace(cfg.Timezone)
	mem := strings.ToLower(strings.TrimSpace(cfg.MemoryLimit))
	up := strings.ToLower(strings.TrimSpace(cfg.UploadMaxFilesize))
	post := strings.ToLower(strings.TrimSpace(cfg.PostMaxSize))
	pm := strings.ToLower(strings.TrimSpace(cfg.PM))

	switch {
	case tz == "Asia/Jakarta" &&
		mem == "512m" &&
		up == "128m" &&
		post == "128m" &&
		cfg.MaxExecutionTime == 300 &&
		cfg.MaxInputVars == 5000 &&
		cfg.DisplayErrors &&
		cfg.OpcacheEnable &&
		cfg.OpcacheMemoryConsumption == 128 &&
		pm == "ondemand" &&
		cfg.PMMaxChildren == 10 &&
		cfg.PMMaxRequests == 500:
		return "Development (Debug On)"
	case tz == "Asia/Jakarta" &&
		mem == "512m" &&
		up == "64m" &&
		post == "64m" &&
		cfg.MaxExecutionTime == 120 &&
		cfg.MaxInputVars == 3000 &&
		!cfg.DisplayErrors &&
		cfg.OpcacheEnable &&
		cfg.OpcacheMemoryConsumption == 256 &&
		pm == "dynamic" &&
		cfg.PMMaxChildren == 20 &&
		cfg.PMStartServers == 4 &&
		cfg.PMMinSpareServers == 2 &&
		cfg.PMMaxSpareServers == 6 &&
		cfg.PMMaxRequests == 500:
		return "Laravel / WordPress Optimal"
	default:
		return PresetCustom
	}
}

// DetectPostgresPreset menentukan preset PostgreSQL yang cocok dengan parameter aktif, atau "Custom".
func DetectPostgresPreset(cfg PostgresConfig) string {
	sb := strings.ToLower(strings.TrimSpace(cfg.SharedBuffers))
	ecs := strings.ToLower(strings.TrimSpace(cfg.EffectiveCacheSize))
	wm := strings.ToLower(strings.TrimSpace(cfg.WorkMem))
	mwm := strings.ToLower(strings.TrimSpace(cfg.MaintenanceWorkMem))
	tz := strings.TrimSpace(cfg.Timezone)

	switch {
	case cfg.MaxConnections == 50 &&
		sb == "128mb" &&
		ecs == "512mb" &&
		wm == "4mb" &&
		mwm == "64mb" &&
		cfg.LogMinDurationStatement == 250:
		return "Dev Hemat RAM"
	case cfg.MaxConnections == 150 &&
		sb == "512mb" &&
		ecs == "2gb" &&
		wm == "16mb" &&
		mwm == "128mb" &&
		tz == "Asia/Jakarta" &&
		cfg.LogMinDurationStatement == 500:
		return "Balanced (2-4GB RAM)"
	case cfg.MaxConnections == 300 &&
		sb == "1gb" &&
		ecs == "4gb" &&
		wm == "32mb" &&
		mwm == "256mb" &&
		tz == "Asia/Jakarta" &&
		cfg.LogMinDurationStatement == 1000:
		return "High Throughput"
	default:
		return PresetCustom
	}
}

// DetectRedisPreset menentukan preset Redis / Valkey yang cocok dengan parameter aktif, atau "Custom".
func DetectRedisPreset(cfg RedisConfig) string {
	maxmem := strings.ToLower(strings.TrimSpace(cfg.MaxMemory))
	policy := strings.ToLower(strings.TrimSpace(cfg.MaxMemoryPolicy))
	pers := strings.ToLower(strings.TrimSpace(cfg.PersistenceMode))
	bind := strings.TrimSpace(cfg.Bind)
	pm := strings.ToLower(strings.TrimSpace(cfg.ProtectedMode))

	isLocalBind := bind == "127.0.0.1 -::1" || bind == "127.0.0.1 ::1" || bind == "127.0.0.1"

	switch {
	case cfg.Port == 6379 &&
		isLocalBind &&
		pm == "yes" &&
		maxmem == "256mb" &&
		policy == "noeviction" &&
		pers == "rdb":
		return "Dev Lokal"
	case maxmem == "256mb" &&
		policy == "allkeys-lru" &&
		pers == "none":
		return "Cache LRU (256MB)"
	case maxmem == "512mb" &&
		policy == "noeviction" &&
		pers == "aof":
		return "Queue / Laravel Horizon"
	default:
		return PresetCustom
	}
}

// DetectDockerPreset menentukan preset Docker daemon.json yang cocok dengan konfigurasi aktif, atau "Custom".
func DetectDockerPreset(cfg DockerConfig) string {
	ld := strings.ToLower(strings.TrimSpace(cfg.LogDriver))
	ms := strings.ToLower(strings.TrimSpace(cfg.LogMaxSize))
	hasCustomNet := strings.TrimSpace(cfg.DataRoot) != "" ||
		len(cfg.DNS) > 0 ||
		len(cfg.RegistryMirrors) > 0 ||
		strings.TrimSpace(cfg.DefaultAddressPoolBase) != "" ||
		cfg.IPv6

	if hasCustomNet {
		return PresetCustom
	}

	switch {
	case ld == "json-file" && ms == "10m" && cfg.LogMaxFile == 3 && cfg.LiveRestore && !cfg.NoNewPrivileges:
		return "Dev Standar"
	case (ld == "local" || ld == "json-file") && ms == "10m" && cfg.LogMaxFile == 2 && cfg.LiveRestore && !cfg.NoNewPrivileges:
		return "Hemat Disk VPS"
	case ld == "json-file" && ms == "50m" && cfg.LogMaxFile == 5 && cfg.LiveRestore && cfg.NoNewPrivileges:
		return "Production Hardened"
	default:
		return PresetCustom
	}
}

// DetectServicePreset membaca konfigurasi aktif layanan inti dan mengembalikan nama preset atau "Custom".
// Mengembalikan string kosong bila layanan tidak memiliki preset atau berkas konfigurasi belum dapat diakses.
func (m *Manager) DetectServicePreset(ctx context.Context, service string) string {
	switch strings.ToLower(strings.TrimSpace(service)) {
	case "nginx":
		cfg, err := m.GetNginxConfig(ctx)
		if err != nil || cfg == nil {
			return ""
		}
		return DetectNginxPreset(*cfg)
	case "php-fpm", "php":
		cfg, err := m.GetPHPConfig(ctx, "")
		if err != nil || cfg == nil {
			return ""
		}
		return DetectPHPPreset(*cfg)
	case "postgresql", "postgres":
		cfg, err := m.GetPostgresConfig(ctx, "")
		if err != nil || cfg == nil {
			return ""
		}
		return DetectPostgresPreset(*cfg)
	case "redis", "valkey":
		cfg, err := m.GetRedisConfig(ctx)
		if err != nil || cfg == nil {
			return ""
		}
		return DetectRedisPreset(*cfg)
	case "docker":
		cfg, err := m.GetDockerConfig(ctx)
		if err != nil || cfg == nil {
			return ""
		}
		return DetectDockerPreset(*cfg)
	default:
		return ""
	}
}
