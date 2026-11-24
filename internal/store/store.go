package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var proxyPassPortRe = regexp.MustCompile(`(?i)\bproxy_pass\s+https?://[^:/\s]+:(\d{1,5})(?:[;/\s]|$)`)

type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

type SiteRecord struct {
	Domain       string     `json:"domain"`
	RootPath     string     `json:"root_path"`
	PHPVersion   string     `json:"php_version"`
	Framework    string     `json:"framework"`
	ProxyPort    int        `json:"proxy_port"`
	SSLEnabled   bool       `json:"ssl_enabled"`
	SSLProvider  string     `json:"ssl_provider,omitempty"`
	SSLExpiresAt *time.Time `json:"ssl_expires_at,omitempty"`
	SSLDaysLeft  *int       `json:"ssl_days_left,omitempty"`
	ExposedTo    string     `json:"exposed_to"`
	CreatedAt    time.Time  `json:"created_at"`
}

type CFProfile struct {
	Name            string    `json:"name"`
	AuthMode        string    `json:"auth_mode"` // "token" | "cert"
	TunnelName      string    `json:"tunnel_name"`
	TunnelID        string    `json:"tunnel_id"`
	AccountTag      string    `json:"account_tag,omitempty"`
	CredentialsPath string    `json:"credentials_path,omitempty"`
	ConfigPath      string    `json:"config_path,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type CFRoute struct {
	ID             int64     `json:"id"`
	ProfileName    string    `json:"profile_name"`
	LocalDomain    string    `json:"local_domain"`
	Hostname       string    `json:"hostname"`
	ServiceURL     string    `json:"service_url"`
	HTTPHostHeader string    `json:"http_host_header,omitempty"`
	NoTLSVerify    bool      `json:"no_tls_verify,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type DNSRecord struct {
	ID        int64  `json:"id"`
	ZoneName  string `json:"zone_name"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	TTL       int    `json:"ttl"`
}

type DNSZone struct {
	ZoneName  string    `json:"zone_name"`
	Kind      string    `json:"kind"`
	Serial    int64     `json:"serial"`
	CreatedAt time.Time `json:"created_at"`
}

type TaskRecord struct {
	ID         int64     `json:"id"`
	Command    string    `json:"command"`
	Status     string    `json:"status"` // running, success, failed
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	LogOutput  string    `json:"log_output"`
}

type TerminalAuditRecord struct {
	ID         int64      `json:"id"`
	SessionID  string     `json:"session_id"`
	PanelUser  string     `json:"panel_user"`
	TargetUser string     `json:"target_user"`
	Shell      string     `json:"shell"`
	Cwd        string     `json:"cwd"`
	ClientIP   string     `json:"client_ip"`
	Status     string     `json:"status"` // active, closed, idle_timeout, error
	ExitCode   int        `json:"exit_code"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

type ComposeProjectRecord struct {
	Name        string    `json:"name"`
	DirPath     string    `json:"dir_path"`
	ComposeFile string    `json:"compose_file,omitempty"`
	Description string    `json:"description,omitempty"`
	Profiles    string    `json:"profiles,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SiteAppRecord menyimpan konfigurasi aplikasi terkelola (Node.js/Go/Rust via systemd)
// yang terhubung 1:1 dengan Virtual Host domain di tabel sites.
type SiteAppRecord struct {
	Domain          string     `json:"domain"`
	Runtime         string     `json:"runtime"` // "nodejs" | "go" | "rust"
	WorkingDir      string     `json:"working_dir"`
	BuildCommand    string     `json:"build_command,omitempty"`
	StartCommand    string     `json:"start_command"`
	Port            int        `json:"port"`
	RunUser         string     `json:"run_user"`
	AutoStart       bool       `json:"auto_start"`
	ServiceName     string     `json:"service_name"`
	EnvFile         string     `json:"env_file"`
	UseProjectEnv   bool       `json:"use_project_env"`
	LastBuildStatus string     `json:"last_build_status,omitempty"`
	LastBuildAt     *time.Time `json:"last_build_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// IsManagedAppFramework melaporkan apakah framework VHost termasuk tipe Managed App terkelola systemd.
func IsManagedAppFramework(framework string) bool {
	switch strings.ToLower(strings.TrimSpace(framework)) {
	case "nodejs", "go", "rust":
		return true
	default:
		return false
	}
}

func New(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("gagal membuat direktori db: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("gagal membuka sqlite: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrasi database gagal: %w", err)
	}

	// Ketatkan permission file database (termasuk WAL/SHM) agar tidak dapat dibaca user lokal lain
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if _, statErr := os.Stat(p); statErr == nil {
			_ = os.Chmod(p, 0600)
		}
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS sites (
			domain TEXT PRIMARY KEY,
			root_path TEXT NOT NULL DEFAULT '',
			php_version TEXT NOT NULL DEFAULT '',
			framework TEXT NOT NULL DEFAULT 'generic',
			proxy_port INTEGER NOT NULL DEFAULT 0,
			ssl_enabled INTEGER NOT NULL DEFAULT 1,
			exposed_to TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS cf_profiles (
			name TEXT PRIMARY KEY,
			tunnel_name TEXT NOT NULL DEFAULT '',
			tunnel_id TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS cf_routes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			profile_name TEXT NOT NULL,
			hostname TEXT NOT NULL,
			service_url TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS dns_zones (
			zone_name TEXT PRIMARY KEY,
			kind TEXT NOT NULL DEFAULT 'Native',
			serial INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS dns_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			zone_name TEXT NOT NULL,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			content TEXT NOT NULL,
			ttl INTEGER NOT NULL DEFAULT 300
		);`,
		`CREATE TABLE IF NOT EXISTS task_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			command TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'running',
			started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			finished_at DATETIME,
			log_output TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS panel_sessions (
			token TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS terminal_audits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL UNIQUE,
			panel_user TEXT NOT NULL DEFAULT '',
			target_user TEXT NOT NULL DEFAULT '',
			shell TEXT NOT NULL DEFAULT '',
			cwd TEXT NOT NULL DEFAULT '',
			client_ip TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			exit_code INTEGER NOT NULL DEFAULT 0,
			started_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			ended_at DATETIME
		);`,
		`CREATE TABLE IF NOT EXISTS compose_projects (
			name TEXT PRIMARY KEY,
			dir_path TEXT NOT NULL UNIQUE,
			compose_file TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			profiles TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS site_apps (
			domain TEXT PRIMARY KEY,
			runtime TEXT NOT NULL DEFAULT 'nodejs',
			working_dir TEXT NOT NULL DEFAULT '',
			build_command TEXT NOT NULL DEFAULT '',
			start_command TEXT NOT NULL DEFAULT '',
			port INTEGER NOT NULL DEFAULT 0,
			run_user TEXT NOT NULL DEFAULT '',
			auto_start INTEGER NOT NULL DEFAULT 1,
			service_name TEXT NOT NULL DEFAULT '',
			env_file TEXT NOT NULL DEFAULT '',
			last_build_status TEXT NOT NULL DEFAULT '',
			last_build_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	if err := s.ensureColumn("sites", "ssl_provider", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	for _, col := range [][3]string{
		{"cf_profiles", "auth_mode", "TEXT NOT NULL DEFAULT 'cert'"},
		{"cf_profiles", "account_tag", "TEXT NOT NULL DEFAULT ''"},
		{"cf_profiles", "credentials_path", "TEXT NOT NULL DEFAULT ''"},
		{"cf_profiles", "config_path", "TEXT NOT NULL DEFAULT ''"},
		{"cf_routes", "local_domain", "TEXT NOT NULL DEFAULT ''"},
		{"cf_routes", "http_host_header", "TEXT NOT NULL DEFAULT ''"},
		{"cf_routes", "no_tls_verify", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.ensureColumn(col[0], col[1], col[2]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureColumn(table, column, colDef string) error {
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s);", table))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		if strings.EqualFold(name, column) {
			return nil
		}
	}
	_, err = s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;", table, column, colDef))
	return err
}

// Sites CRUD
func (s *Store) SaveSite(site SiteRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `INSERT INTO sites (domain, root_path, php_version, framework, proxy_port, ssl_enabled, ssl_provider, exposed_to, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(domain) DO UPDATE SET
			root_path=excluded.root_path,
			php_version=excluded.php_version,
			framework=excluded.framework,
			proxy_port=excluded.proxy_port,
			ssl_enabled=excluded.ssl_enabled,
			ssl_provider=excluded.ssl_provider,
			exposed_to=excluded.exposed_to;`

	createdAt := site.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	sslInt := 0
	if site.SSLEnabled {
		sslInt = 1
	}

	_, err := s.db.Exec(query, site.Domain, site.RootPath, site.PHPVersion, site.Framework, site.ProxyPort, sslInt, site.SSLProvider, site.ExposedTo, createdAt)
	return err
}

func (s *Store) ListSites() ([]SiteRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT domain, root_path, php_version, framework, proxy_port, ssl_enabled, ssl_provider, exposed_to, created_at FROM sites ORDER BY domain ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SiteRecord
	for rows.Next() {
		var r SiteRecord
		var sslInt int
		if err := rows.Scan(&r.Domain, &r.RootPath, &r.PHPVersion, &r.Framework, &r.ProxyPort, &sslInt, &r.SSLProvider, &r.ExposedTo, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.SSLEnabled = (sslInt == 1)
		result = append(result, r)
	}
	return result, nil
}

func (s *Store) SetSiteSSL(domain string, enabled bool, provider string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cleanDom := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), ".conf"))
	sslInt := 0
	if enabled {
		sslInt = 1
	}
	_, err := s.db.Exec(`UPDATE sites SET ssl_enabled = ?, ssl_provider = ? WHERE domain = ? OR domain = ?`, sslInt, provider, cleanDom, cleanDom+".conf")
	return err
}

func (s *Store) DeleteSite(domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clean := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(domain), "."), ".conf"))
	_, err := s.db.Exec(`DELETE FROM sites WHERE lower(domain) = ? OR lower(domain) = ?`, clean, clean+".conf")
	return err
}

// Tasks
func (s *Store) CreateTask(command string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`INSERT INTO task_history (command, status, started_at, log_output) VALUES (?, 'running', ?, '')`, command, time.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FinishTask(id int64, status string, logOutput string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`UPDATE task_history SET status = ?, finished_at = ?, log_output = ? WHERE id = ?`, status, time.Now(), logOutput, id)
	return err
}

func (s *Store) ListRecentTasks(limit int) ([]TaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT id, command, status, started_at, finished_at, log_output FROM task_history ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TaskRecord
	for rows.Next() {
		var t TaskRecord
		var finishedAt sql.NullTime
		if err := rows.Scan(&t.ID, &t.Command, &t.Status, &t.StartedAt, &finishedAt, &t.LogOutput); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			t.FinishedAt = finishedAt.Time
		} else {
			t.FinishedAt = t.StartedAt
		}
		result = append(result, t)
	}
	return result, nil
}

// Settings
func (s *Store) GetSetting(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var val string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&val)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return val, nil
}

func (s *Store) SetSetting(key, val string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, val)
	return err
}

func (s *Store) SetSiteExposedTo(domain, publicHost string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cleanDom := strings.TrimSuffix(domain, ".conf")
	_, err := s.db.Exec(`UPDATE sites SET exposed_to = ? WHERE domain = ? OR domain = ?`, publicHost, cleanDom, cleanDom+".conf")
	return err
}

func (s *Store) ClearSiteExposedTo(publicHost string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE sites SET exposed_to = '' WHERE exposed_to = ?`, publicHost)
	return err
}

// Initial Sync from existing disk configs
func (s *Store) SyncFromDisk(nginxSitesAvail string, exposuresDir string) {
	// Clean up stale or non-site entries from database
	_, _ = s.db.Exec(`DELETE FROM sites WHERE domain LIKE '%.conf' OR domain LIKE '%.custom.conf' OR domain LIKE 'wdev_%' OR domain = 'default'`)

	// Preserve framework, root_path, and exposed_to metadata already stored for domains whose local Nginx
	// vhost uses generic FastCGI directives, Managed App reverse proxy, or matching public domain in server_name.
	type existingSiteMeta struct {
		Framework string
		RootPath  string
		ExposedTo string
	}
	existingMeta := make(map[string]existingSiteMeta)
	if prevSites, listErr := s.ListSites(); listErr == nil {
		for _, ps := range prevSites {
			existingMeta[strings.ToLower(strings.TrimSpace(ps.Domain))] = existingSiteMeta{
				Framework: ps.Framework,
				RootPath:  ps.RootPath,
				ExposedTo: ps.ExposedTo,
			}
		}
	}
	managedApps := make(map[string]SiteAppRecord)
	if apps, appErr := s.ListSiteApps(); appErr == nil {
		for _, app := range apps {
			managedApps[normalizeSiteAppDomain(app.Domain)] = app
		}
	}

	// 1. Scan /etc/nginx/sites-available
	if entries, err := os.ReadDir(nginxSitesAvail); err == nil {
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || name == "default" || name == "default.conf" ||
				name == "000-default" || name == "000-default.conf" ||
				strings.HasPrefix(name, ".") || strings.HasPrefix(name, "wdev_") ||
				strings.HasSuffix(name, ".custom.conf") || strings.HasSuffix(name, ".bak") ||
				strings.HasSuffix(name, ".disabled") || strings.HasSuffix(name, "-le-ssl.conf") ||
				strings.Contains(name, ".dpkg-") || strings.HasSuffix(name, ".rpmnew") ||
				strings.HasSuffix(name, ".rpmsave") || strings.HasSuffix(name, "~") {
				continue
			}

			domain := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(name, ".conf"), "."))
			if domain == "" {
				continue
			}
			confBytes, err := os.ReadFile(filepath.Join(nginxSitesAvail, name))
			if err != nil {
				continue
			}
			confStr := string(confBytes)

			framework := "generic"
			phpVer := "8.4"
			rootPath := ""
			proxyPort := 0
			sslEnabled := strings.Contains(confStr, "ssl_certificate")
			sslProvider := ""
			if strings.Contains(confStr, "/etc/letsencrypt/") {
				sslProvider = "letsencrypt"
			} else if sslEnabled {
				sslProvider = "local"
			}
			exposedTo := ""

			if strings.Contains(confStr, "laravel") {
				framework = "laravel"
			} else if strings.Contains(confStr, "wordpress") {
				framework = "wordpress"
			} else if strings.Contains(confStr, "proxy_pass") {
				framework = "proxy"
			} else if !strings.Contains(confStr, "fastcgi_pass") {
				framework = "static"
				phpVer = ""
			}

			// Extract root, php version, and server_name aliases
			for _, line := range strings.Split(confStr, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "root ") {
					rootPath = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "root "), ";"))
				}
				if proxyPort == 0 {
					if m := proxyPassPortRe.FindStringSubmatch(line); len(m) > 1 {
						if p, err := strconv.Atoi(m[1]); err == nil && p > 0 && p <= 65535 {
							proxyPort = p
						}
					}
				}
				if strings.Contains(line, "php") && strings.Contains(line, "-fpm.sock") {
					for _, v := range []string{"8.4", "8.3", "8.2", "8.1"} {
						if strings.Contains(line, "php"+v) {
							phpVer = v
							break
						}
					}
				}
				if strings.HasPrefix(line, "server_name ") {
					tokens := strings.Fields(strings.TrimSuffix(strings.TrimPrefix(line, "server_name "), ";"))
					for _, tok := range tokens {
						tok = strings.TrimSpace(tok)
						if tok != domain && tok != "*."+domain && tok != "www."+domain && !strings.HasSuffix(tok, ".test") && !strings.HasSuffix(tok, ".local") {
							if exposedTo == "" || !strings.Contains(tok, "apps.") {
								exposedTo = tok
							}
						}
					}
				}
			}

			if prev, ok := existingMeta[domain]; ok {
				if framework == "generic" && prev.Framework != "" && prev.Framework != "static" && prev.Framework != "proxy" {
					framework = prev.Framework
				}
				if framework == "proxy" && IsManagedAppFramework(prev.Framework) {
					framework = prev.Framework
					phpVer = ""
					if rootPath == "" && prev.RootPath != "" {
						rootPath = prev.RootPath
					}
				}
				if exposedTo == "" && prev.ExposedTo != "" {
					exposedTo = prev.ExposedTo
				}
			}
			if app, hasApp := managedApps[domain]; hasApp && framework == "proxy" {
				if IsManagedAppFramework(app.Runtime) {
					framework = app.Runtime
				}
				phpVer = ""
				if rootPath == "" && app.WorkingDir != "" {
					rootPath = app.WorkingDir
				}
			}

			_ = s.SaveSite(SiteRecord{
				Domain:      domain,
				RootPath:    rootPath,
				PHPVersion:  phpVer,
				Framework:   framework,
				ProxyPort:   proxyPort,
				SSLEnabled:  sslEnabled,
				SSLProvider: sslProvider,
				ExposedTo:   exposedTo,
				CreatedAt:   time.Now(),
			})
		}
	}

	// 2. Scan exposures
	if entries, err := os.ReadDir(exposuresDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			domain := strings.TrimSuffix(e.Name(), ".conf")
			content, err := os.ReadFile(filepath.Join(exposuresDir, e.Name()))
			if err == nil {
				publicHost := strings.TrimSpace(string(content))
				_, _ = s.db.Exec(`UPDATE sites SET exposed_to = ? WHERE domain = ?`, publicHost, domain)
			}
		}
	}
}

// SaveSession menyimpan token sesi panel.
func (s *Store) SaveSession(token, username string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO panel_sessions (token, username, expires_at) VALUES (?, ?, ?)
		ON CONFLICT(token) DO UPDATE SET username = excluded.username, expires_at = excluded.expires_at`,
		token, username, expiresAt.Format(time.RFC3339))
	return err
}

// GetSession mengambil data sesi jika ada.
func (s *Store) GetSession(token string) (string, time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var username, expStr string
	err := s.db.QueryRow(`SELECT username, expires_at FROM panel_sessions WHERE token = ?`, token).Scan(&username, &expStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", time.Time{}, nil
		}
		return "", time.Time{}, err
	}
	exp, err := time.Parse(time.RFC3339, expStr)
	if err != nil {
		exp, _ = time.Parse("2006-01-02 15:04:05", expStr)
	}
	return username, exp, nil
}

// DeleteSession menghapus sesi berdasarkan token.
func (s *Store) DeleteSession(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM panel_sessions WHERE token = ?`, token)
	return err
}

// DeleteExpiredSessions membersihkan sesi kedaluwarsa.
func (s *Store) DeleteExpiredSessions() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM panel_sessions WHERE expires_at < ?`, time.Now().Format(time.RFC3339))
	return err
}

// DeleteAllSessions mencabut seluruh sesi panel aktif (dipanggil saat ganti/reset kredensial).
func (s *Store) DeleteAllSessions() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM panel_sessions`)
	return err
}

// InsertTerminalAudit mencatat pembukaan sesi terminal PTY.
func (s *Store) InsertTerminalAudit(rec TerminalAuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec.StartedAt.IsZero() {
		rec.StartedAt = time.Now()
	}
	if rec.Status == "" {
		rec.Status = "active"
	}
	_, err := s.db.Exec(
		`INSERT INTO terminal_audits (session_id, panel_user, target_user, shell, cwd, client_ip, status, exit_code, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(session_id) DO NOTHING`,
		rec.SessionID, rec.PanelUser, rec.TargetUser, rec.Shell, rec.Cwd, rec.ClientIP, rec.Status, rec.ExitCode, rec.StartedAt.Format(time.RFC3339),
	)
	return err
}

// CompleteTerminalAudit memperbarui status akhir dan waktu selesai sesi terminal PTY.
func (s *Store) CompleteTerminalAudit(sessionID, status string, exitCode int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status == "" {
		status = "closed"
	}
	_, err := s.db.Exec(
		`UPDATE terminal_audits SET status = ?, exit_code = ?, ended_at = ? WHERE session_id = ? AND ended_at IS NULL`,
		status, exitCode, time.Now().Format(time.RFC3339), sessionID,
	)
	return err
}

// ListTerminalAudits mengembalikan daftar riwayat audit pembukaan terminal terbaru.
func (s *Store) ListTerminalAudits(limit int) ([]TerminalAuditRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, session_id, panel_user, target_user, shell, cwd, client_ip, status, exit_code, started_at, ended_at
		 FROM terminal_audits ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TerminalAuditRecord
	for rows.Next() {
		var r TerminalAuditRecord
		var startStr string
		var endStr sql.NullString
		if err := rows.Scan(&r.ID, &r.SessionID, &r.PanelUser, &r.TargetUser, &r.Shell, &r.Cwd, &r.ClientIP, &r.Status, &r.ExitCode, &startStr, &endStr); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			r.StartedAt = t
		} else if t2, err2 := time.Parse("2006-01-02 15:04:05", startStr); err2 == nil {
			r.StartedAt = t2
		}
		if endStr.Valid && endStr.String != "" {
			if t, err := time.Parse(time.RFC3339, endStr.String); err == nil {
				r.EndedAt = &t
			} else if t2, err2 := time.Parse("2006-01-02 15:04:05", endStr.String); err2 == nil {
				r.EndedAt = &t2
			}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveComposeProject menyimpan atau memperbarui metadata proyek Docker Compose terdaftar.
func (s *Store) SaveComposeProject(rec ComposeProjectRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	createdAt := rec.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := rec.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}

	_, err := s.db.Exec(
		`INSERT INTO compose_projects (name, dir_path, compose_file, description, profiles, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
			dir_path=excluded.dir_path,
			compose_file=excluded.compose_file,
			description=excluded.description,
			profiles=excluded.profiles,
			updated_at=excluded.updated_at`,
		rec.Name, rec.DirPath, rec.ComposeFile, rec.Description, rec.Profiles, createdAt.Format(time.RFC3339), updatedAt.Format(time.RFC3339),
	)
	return err
}

func scanStoreTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02 15:04:05", raw); err == nil {
		return t
	}
	return time.Time{}
}

// ListComposeProjects mengembalikan seluruh proyek Docker Compose yang terdaftar di database.
func (s *Store) ListComposeProjects() ([]ComposeProjectRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT name, dir_path, compose_file, description, profiles, created_at, updated_at
		 FROM compose_projects ORDER BY name ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ComposeProjectRecord
	for rows.Next() {
		var r ComposeProjectRecord
		var createdStr, updatedStr string
		if err := rows.Scan(&r.Name, &r.DirPath, &r.ComposeFile, &r.Description, &r.Profiles, &createdStr, &updatedStr); err != nil {
			return nil, err
		}
		r.CreatedAt = scanStoreTime(createdStr)
		r.UpdatedAt = scanStoreTime(updatedStr)
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetComposeProject mengambil satu proyek Compose berdasarkan nama.
func (s *Store) GetComposeProject(name string) (*ComposeProjectRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var r ComposeProjectRecord
	var createdStr, updatedStr string
	err := s.db.QueryRow(
		`SELECT name, dir_path, compose_file, description, profiles, created_at, updated_at
		 FROM compose_projects WHERE lower(name) = lower(?)`,
		strings.TrimSpace(name),
	).Scan(&r.Name, &r.DirPath, &r.ComposeFile, &r.Description, &r.Profiles, &createdStr, &updatedStr)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.CreatedAt = scanStoreTime(createdStr)
	r.UpdatedAt = scanStoreTime(updatedStr)
	return &r, nil
}

// DeleteComposeProject menghapus proyek Compose dari daftar terdaftar (tidak menghapus berkas di disk).
func (s *Store) DeleteComposeProject(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM compose_projects WHERE lower(name) = lower(?)`, strings.TrimSpace(name))
	return err
}

func normalizeSiteAppDomain(domain string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(domain), "."), ".conf"))
}

// SaveSiteApp menyimpan atau memperbarui metadata Managed App untuk suatu Virtual Host domain.
func (s *Store) SaveSiteApp(rec SiteAppRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	domain := normalizeSiteAppDomain(rec.Domain)
	if domain == "" {
		return fmt.Errorf("domain aplikasi tidak boleh kosong")
	}

	now := time.Now()
	createdAt := rec.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := rec.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}
	autoInt := 0
	if rec.AutoStart {
		autoInt = 1
	}

	_, err := s.db.Exec(
		`INSERT INTO site_apps (domain, runtime, working_dir, build_command, start_command, port, run_user, auto_start, service_name, env_file, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(domain) DO UPDATE SET
			runtime=excluded.runtime,
			working_dir=excluded.working_dir,
			build_command=excluded.build_command,
			start_command=excluded.start_command,
			port=excluded.port,
			run_user=excluded.run_user,
			auto_start=excluded.auto_start,
			service_name=excluded.service_name,
			env_file=excluded.env_file,
			updated_at=excluded.updated_at`,
		domain,
		strings.ToLower(strings.TrimSpace(rec.Runtime)),
		strings.TrimSpace(rec.WorkingDir),
		strings.TrimSpace(rec.BuildCommand),
		strings.TrimSpace(rec.StartCommand),
		rec.Port,
		strings.TrimSpace(rec.RunUser),
		autoInt,
		strings.TrimSpace(rec.ServiceName),
		strings.TrimSpace(rec.EnvFile),
		createdAt.Format(time.RFC3339),
		updatedAt.Format(time.RFC3339),
	)
	return err
}

func scanSiteAppRow(scanner interface {
	Scan(dest ...any) error
}) (*SiteAppRecord, error) {
	var r SiteAppRecord
	var autoInt int
	var lastBuildStatus sql.NullString
	var lastBuildAtStr sql.NullString
	var createdStr, updatedStr string
	if err := scanner.Scan(
		&r.Domain,
		&r.Runtime,
		&r.WorkingDir,
		&r.BuildCommand,
		&r.StartCommand,
		&r.Port,
		&r.RunUser,
		&autoInt,
		&r.ServiceName,
		&r.EnvFile,
		&lastBuildStatus,
		&lastBuildAtStr,
		&createdStr,
		&updatedStr,
	); err != nil {
		return nil, err
	}
	r.AutoStart = (autoInt == 1)
	r.UseProjectEnv = r.EnvFile != "" && r.WorkingDir != "" &&
		filepath.Clean(r.EnvFile) == filepath.Clean(filepath.Join(r.WorkingDir, ".env"))
	r.LastBuildStatus = lastBuildStatus.String
	if lastBuildAtStr.Valid && lastBuildAtStr.String != "" {
		if t := scanStoreTime(lastBuildAtStr.String); !t.IsZero() {
			r.LastBuildAt = &t
		}
	}
	r.CreatedAt = scanStoreTime(createdStr)
	r.UpdatedAt = scanStoreTime(updatedStr)
	return &r, nil
}

// GetSiteApp mengambil konfigurasi Managed App berdasarkan domain VHost.
func (s *Store) GetSiteApp(domain string) (*SiteAppRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clean := normalizeSiteAppDomain(domain)
	row := s.db.QueryRow(
		`SELECT domain, runtime, working_dir, build_command, start_command, port, run_user, auto_start, service_name, env_file, last_build_status, last_build_at, created_at, updated_at
		 FROM site_apps WHERE lower(domain) = ?`,
		clean,
	)
	r, err := scanSiteAppRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
}

// ListSiteApps mengembalikan seluruh konfigurasi Managed App yang terdaftar.
func (s *Store) ListSiteApps() ([]SiteAppRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT domain, runtime, working_dir, build_command, start_command, port, run_user, auto_start, service_name, env_file, last_build_status, last_build_at, created_at, updated_at
		 FROM site_apps ORDER BY domain ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SiteAppRecord
	for rows.Next() {
		r, err := scanSiteAppRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// UpdateSiteAppBuildStatus memperbarui status dan waktu build terakhir untuk Managed App.
func (s *Store) UpdateSiteAppBuildStatus(domain, status string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if at.IsZero() {
		at = time.Now()
	}
	clean := normalizeSiteAppDomain(domain)
	_, err := s.db.Exec(
		`UPDATE site_apps SET last_build_status = ?, last_build_at = ?, updated_at = ? WHERE lower(domain) = ?`,
		strings.TrimSpace(status),
		at.Format(time.RFC3339),
		time.Now().Format(time.RFC3339),
		clean,
	)
	return err
}

// DeleteSiteApp menghapus metadata Managed App dari SQLite.
func (s *Store) DeleteSiteApp(domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clean := normalizeSiteAppDomain(domain)
	_, err := s.db.Exec(`DELETE FROM site_apps WHERE lower(domain) = ?`, clean)
	return err
}


