package serviceconfig

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RedisKeyspaceInfo menyimpan ringkasan key pada satu database Redis.
type RedisKeyspaceInfo struct {
	DB      string `json:"db"`
	Index   int    `json:"index"`
	Keys    int64  `json:"keys"`
	Expires int64  `json:"expires"`
	AvgTTL  int64  `json:"avg_ttl"`
}

// RedisInspectResult berisi ringkasan runtime Redis dari perintah INFO.
type RedisInspectResult struct {
	Version          string              `json:"version"`
	Mode             string              `json:"mode"`
	UptimeSeconds    int64               `json:"uptime_seconds"`
	ConnectedClients int64               `json:"connected_clients"`
	UsedMemoryHuman  string              `json:"used_memory_human"`
	PeakMemoryHuman  string              `json:"peak_memory_human"`
	MaxMemoryHuman   string              `json:"maxmemory_human"`
	TotalCommands    int64               `json:"total_commands_processed"`
	KeyspaceHits     int64               `json:"keyspace_hits"`
	KeyspaceMisses   int64               `json:"keyspace_misses"`
	HitRatePercent   float64             `json:"hit_rate_percent"`
	Keyspaces        []RedisKeyspaceInfo `json:"keyspaces"`
}

func findRedisCLI() string {
	for _, cli := range []string{"redis-cli", "valkey-cli"} {
		if p, err := exec.LookPath(cli); err == nil {
			return p
		}
	}
	return ""
}

func (m *Manager) runRedisCLI(ctx context.Context, args ...string) (string, error) {
	cli := findRedisCLI()
	if cli == "" {
		return "", fmt.Errorf("redis-cli / valkey-cli tidak ditemukan")
	}

	port := "6379"
	pass := ""
	if confPath := FindRedisConf(); confPath != "" {
		if raw, err := m.readPrivilegedFile(ctx, confPath); err == nil {
			lines := strings.Split(raw, "\n")
			if p := redisActiveConfValue(lines, "port"); p != "" {
				port = p
			}
			pass = redisActiveConfValue(lines, "requirepass")
		} else if err == ErrSudoRequired {
			return "", ErrSudoRequired
		}
	}

	fullArgs := append([]string{"-p", port}, args...)
	cmd := exec.CommandContext(ctx, cli, fullArgs...)
	if pass != "" {
		cmd.Env = append(os.Environ(), "REDISCLI_AUTH="+pass)
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(buf.String())
		if out != "" {
			return out, fmt.Errorf("perintah %s gagal: %s", cli, out)
		}
		return "", fmt.Errorf("perintah %s gagal: %w", cli, err)
	}
	return buf.String(), nil
}

// ParseRedisInfo mem-parse output `redis-cli INFO` menjadi RedisInspectResult.
func ParseRedisInfo(raw string) RedisInspectResult {
	res := RedisInspectResult{
		UsedMemoryHuman: "-",
		PeakMemoryHuman: "-",
		MaxMemoryHuman:  "Unlimited",
	}

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch k {
		case "redis_version", "valkey_version":
			if res.Version == "" || k == "valkey_version" {
				res.Version = v
			}
		case "redis_mode":
			res.Mode = v
		case "uptime_in_seconds":
			res.UptimeSeconds, _ = strconv.ParseInt(v, 10, 64)
		case "connected_clients":
			res.ConnectedClients, _ = strconv.ParseInt(v, 10, 64)
		case "used_memory_human":
			res.UsedMemoryHuman = v
		case "used_memory_peak_human":
			res.PeakMemoryHuman = v
		case "maxmemory_human":
			if v != "" && v != "0B" {
				res.MaxMemoryHuman = v
			}
		case "total_commands_processed":
			res.TotalCommands, _ = strconv.ParseInt(v, 10, 64)
		case "keyspace_hits":
			res.KeyspaceHits, _ = strconv.ParseInt(v, 10, 64)
		case "keyspace_misses":
			res.KeyspaceMisses, _ = strconv.ParseInt(v, 10, 64)
		default:
			if strings.HasPrefix(k, "db") {
				idx, err := strconv.Atoi(strings.TrimPrefix(k, "db"))
				if err == nil {
					ks := RedisKeyspaceInfo{DB: k, Index: idx}
					for _, part := range strings.Split(v, ",") {
						pk, pv, pOk := strings.Cut(part, "=")
						if !pOk {
							continue
						}
						switch pk {
						case "keys":
							ks.Keys, _ = strconv.ParseInt(pv, 10, 64)
						case "expires":
							ks.Expires, _ = strconv.ParseInt(pv, 10, 64)
						case "avg_ttl":
							ks.AvgTTL, _ = strconv.ParseInt(pv, 10, 64)
						}
					}
					res.Keyspaces = append(res.Keyspaces, ks)
				}
			}
		}
	}

	sort.Slice(res.Keyspaces, func(i, j int) bool {
		return res.Keyspaces[i].Index < res.Keyspaces[j].Index
	})

	totalLookups := res.KeyspaceHits + res.KeyspaceMisses
	if totalLookups > 0 {
		res.HitRatePercent = float64(res.KeyspaceHits) * 100.0 / float64(totalLookups)
	}
	return res
}

// InspectRedis mengambil metrik memori, klien, dan keyspace Redis/Valkey yang sedang berjalan.
func (m *Manager) InspectRedis(ctx context.Context) (*RedisInspectResult, error) {
	raw, err := m.runRedisCLI(ctx, "INFO")
	if err != nil {
		return nil, err
	}
	res := ParseRedisInfo(raw)
	return &res, nil
}

// FlushRedisDB mengosongkan satu logical database Redis (`FLUSHDB`).
func (m *Manager) FlushRedisDB(ctx context.Context, dbIndex int) error {
	if dbIndex < 0 || dbIndex > 255 {
		return fmt.Errorf("index database Redis harus antara 0 sampai 255")
	}
	_, err := m.runRedisCLI(ctx, "-n", strconv.Itoa(dbIndex), "FLUSHDB")
	return err
}

// PostgresDatabaseInfo info database PostgreSQL.
type PostgresDatabaseInfo struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
	Size  string `json:"size"`
}

// PostgresRoleInfo info role/user PostgreSQL.
type PostgresRoleInfo struct {
	Name      string `json:"name"`
	Superuser bool   `json:"superuser"`
	CanLogin  bool   `json:"can_login"`
}

// PostgresConnInfo info sesi aktif di pg_stat_activity.
type PostgresConnInfo struct {
	PID        int    `json:"pid"`
	Database   string `json:"database"`
	User       string `json:"user"`
	ClientAddr string `json:"client_addr"`
	State      string `json:"state"`
	Query      string `json:"query"`
}

// PostgresInspectResult ringkasan database, role, dan koneksi aktif PostgreSQL.
type PostgresInspectResult struct {
	Version     string                 `json:"version"`
	Port        int                    `json:"port"`
	Databases   []PostgresDatabaseInfo `json:"databases"`
	Roles       []PostgresRoleInfo     `json:"roles"`
	Connections []PostgresConnInfo     `json:"connections"`
}

// PostgresCreateDBRequest payload pembuatan database (dan opsional user baru).
type PostgresCreateDBRequest struct {
	Version  string `json:"version"`
	Database string `json:"database"`
	Owner    string `json:"owner"`
	Password string `json:"password,omitempty"`
}

var pgIdentRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)

// ValidatePGIdentifier memvalidasi nama database atau role PostgreSQL secara ketat.
func ValidatePGIdentifier(name, label string) error {
	if !pgIdentRegex.MatchString(name) {
		return fmt.Errorf("%s hanya boleh berisi huruf, angka, dan underscore (diawali huruf/underscore, maks 63 karakter)", label)
	}
	return nil
}

func (m *Manager) runPostgresQuery(ctx context.Context, port int, sqlQuery string) (string, error) {
	if port <= 0 {
		port = 5432
	}
	cmdStr := fmt.Sprintf("psql -p %d -At -F $'\\t' -v ON_ERROR_STOP=1", port)
	return m.runPrivilegedCmdStdin(ctx, sqlQuery+"\n", "su", "-", "postgres", "-c", cmdStr)
}

// InspectPostgres mengambil daftar database, role, dan koneksi aktif dari PostgreSQL.
func (m *Manager) InspectPostgres(ctx context.Context, version string) (*PostgresInspectResult, error) {
	cfg, err := m.GetPostgresConfig(ctx, version)
	if err != nil {
		return nil, err
	}
	port := cfg.Port
	if port <= 0 {
		port = 5432
	}

	res := &PostgresInspectResult{
		Version: cfg.Version,
		Port:    port,
	}

	dbOut, err := m.runPostgresQuery(ctx, port,
		`SELECT d.datname, pg_get_userbyid(d.datdba), pg_size_pretty(pg_database_size(d.datname)) FROM pg_database d WHERE d.datistemplate = false ORDER BY d.datname;`)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(dbOut, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) >= 3 {
			res.Databases = append(res.Databases, PostgresDatabaseInfo{
				Name:  cols[0],
				Owner: cols[1],
				Size:  cols[2],
			})
		}
	}

	roleOut, err := m.runPostgresQuery(ctx, port,
		`SELECT rolname, rolsuper, rolcanlogin FROM pg_roles WHERE rolname NOT LIKE 'pg_%' ORDER BY rolname;`)
	if err == nil {
		for _, line := range strings.Split(roleOut, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			cols := strings.Split(line, "\t")
			if len(cols) >= 3 {
				res.Roles = append(res.Roles, PostgresRoleInfo{
					Name:      cols[0],
					Superuser: cols[1] == "t",
					CanLogin:  cols[2] == "t",
				})
			}
		}
	}

	connOut, err := m.runPostgresQuery(ctx, port,
		`SELECT pid, COALESCE(datname,''), COALESCE(usename,''), COALESCE(host(client_addr),'local'), COALESCE(state,''), LEFT(REGEXP_REPLACE(COALESCE(query,''), E'[\\n\\r\\t]+', ' ', 'g'), 100) FROM pg_stat_activity WHERE pid <> pg_backend_pid() ORDER BY backend_start DESC LIMIT 25;`)
	if err == nil {
		for _, line := range strings.Split(connOut, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			cols := strings.Split(line, "\t")
			if len(cols) >= 6 {
				pid, _ := strconv.Atoi(cols[0])
				res.Connections = append(res.Connections, PostgresConnInfo{
					PID:        pid,
					Database:   cols[1],
					User:       cols[2],
					ClientAddr: cols[3],
					State:      cols[4],
					Query:      cols[5],
				})
			}
		}
	}

	return res, nil
}

// CreatePostgresDatabase membuat database baru dan (opsional) role pemiliknya.
func (m *Manager) CreatePostgresDatabase(ctx context.Context, req PostgresCreateDBRequest) error {
	dbName := strings.TrimSpace(req.Database)
	owner := strings.TrimSpace(req.Owner)
	if err := ValidatePGIdentifier(dbName, "nama database"); err != nil {
		return err
	}
	if owner == "" {
		owner = "postgres"
	} else if err := ValidatePGIdentifier(owner, "nama user/owner"); err != nil {
		return err
	}
	if req.Password != "" {
		if err := validateSingleLine(req.Password, "password user"); err != nil {
			return err
		}
	}

	cfg, err := m.GetPostgresConfig(ctx, req.Version)
	if err != nil {
		return err
	}
	port := cfg.Port
	if port <= 0 {
		port = 5432
	}

	var stmts []string
	if owner != "postgres" {
		existsOut, _ := m.runPostgresQuery(ctx, port, fmt.Sprintf("SELECT 1 FROM pg_roles WHERE rolname = '%s';", owner))
		if strings.TrimSpace(existsOut) != "1" {
			if req.Password == "" {
				return fmt.Errorf("password wajib diisi saat membuat user baru '%s'", owner)
			}
			escapedPass := strings.ReplaceAll(req.Password, "'", "''")
			stmts = append(stmts, fmt.Sprintf(`CREATE ROLE "%s" WITH LOGIN PASSWORD '%s';`, owner, escapedPass))
		} else if req.Password != "" {
			escapedPass := strings.ReplaceAll(req.Password, "'", "''")
			stmts = append(stmts, fmt.Sprintf(`ALTER ROLE "%s" WITH PASSWORD '%s';`, owner, escapedPass))
		}
	}
	stmts = append(stmts, fmt.Sprintf(`CREATE DATABASE "%s" OWNER "%s";`, dbName, owner))

	_, err = m.runPostgresQuery(ctx, port, strings.Join(stmts, "\n"))
	return err
}

// GetServiceLogs mengambil N baris log terakhir dari systemd journald untuk layanan tertentu.
func (m *Manager) GetServiceLogs(ctx context.Context, service, version string, lines int) (string, string, error) {
	if lines <= 0 || lines > 500 {
		lines = 80
	}

	var unit string
	switch service {
	case "redis":
		unit = ResolveRedisServiceUnit()
	case "nginx":
		unit = "nginx"
	case "postgresql", "postgres":
		_, _, resolvedUnit, _, _, err := m.resolvePostgresPaths(ctx, version)
		if err != nil {
			return "", "", err
		}
		unit = resolvedUnit
		if !m.isServiceActive(ctx, unit) && m.isServiceActive(ctx, "postgresql") {
			unit = "postgresql"
		}
	case "php-fpm", "php":
		paths, err := resolvePHPPaths(version)
		if err != nil {
			return "", "", err
		}
		unit = paths.unit
	default:
		return "", "", fmt.Errorf("layanan log tidak dikenal: %s", service)
	}

	out, err := m.runPrivilegedCmd(ctx, "journalctl", "-u", unit, "-n", strconv.Itoa(lines), "--no-pager", "-o", "short-iso")
	if err != nil {
		return unit, "", err
	}

	// Untuk nginx, tambahkan juga tail /var/log/nginx/error.log bila tersedia agar error FastCGI/upstream langsung terlihat
	if service == "nginx" {
		if errLog, eErr := m.runPrivilegedCmd(ctx, "tail", "-n", "30", "/var/log/nginx/error.log"); eErr == nil && strings.TrimSpace(errLog) != "" {
			out = fmt.Sprintf("=== journalctl -u %s ===\n%s\n\n=== /var/log/nginx/error.log (30 baris terakhir) ===\n%s", unit, out, errLog)
		}
	}

	if strings.TrimSpace(out) == "" {
		out = fmt.Sprintf("(Belum ada entri log journald untuk unit %s)", unit)
	}
	return unit, out, nil
}
