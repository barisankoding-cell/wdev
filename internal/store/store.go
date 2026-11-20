package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

type SiteRecord struct {
	Domain     string    `json:"domain"`
	RootPath   string    `json:"root_path"`
	PHPVersion string    `json:"php_version"`
	Framework  string    `json:"framework"`
	ProxyPort  int       `json:"proxy_port"`
	SSLEnabled bool      `json:"ssl_enabled"`
	ExposedTo  string    `json:"exposed_to"`
	CreatedAt  time.Time `json:"created_at"`
}

type CFProfile struct {
	Name       string    `json:"name"`
	TunnelName string    `json:"tunnel_name"`
	TunnelID   string    `json:"tunnel_id"`
	CreatedAt  time.Time `json:"created_at"`
}

type CFRoute struct {
	ID          int64     `json:"id"`
	ProfileName string    `json:"profile_name"`
	Hostname    string    `json:"hostname"`
	ServiceURL  string    `json:"service_url"`
	CreatedAt   time.Time `json:"created_at"`
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

func New(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
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
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// Sites CRUD
func (s *Store) SaveSite(site SiteRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `INSERT INTO sites (domain, root_path, php_version, framework, proxy_port, ssl_enabled, exposed_to, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(domain) DO UPDATE SET
			root_path=excluded.root_path,
			php_version=excluded.php_version,
			framework=excluded.framework,
			proxy_port=excluded.proxy_port,
			ssl_enabled=excluded.ssl_enabled,
			exposed_to=excluded.exposed_to;`

	createdAt := site.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	sslInt := 0
	if site.SSLEnabled {
		sslInt = 1
	}

	_, err := s.db.Exec(query, site.Domain, site.RootPath, site.PHPVersion, site.Framework, site.ProxyPort, sslInt, site.ExposedTo, createdAt)
	return err
}

func (s *Store) ListSites() ([]SiteRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT domain, root_path, php_version, framework, proxy_port, ssl_enabled, exposed_to, created_at FROM sites ORDER BY domain ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SiteRecord
	for rows.Next() {
		var r SiteRecord
		var sslInt int
		if err := rows.Scan(&r.Domain, &r.RootPath, &r.PHPVersion, &r.Framework, &r.ProxyPort, &sslInt, &r.ExposedTo, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.SSLEnabled = (sslInt == 1)
		result = append(result, r)
	}
	return result, nil
}

func (s *Store) DeleteSite(domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM sites WHERE domain = ?`, domain)
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

	rows, err := s.db.Query(`SELECT id, command, status, started_at, COALESCE(finished_at, started_at), log_output FROM task_history ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []TaskRecord
	for rows.Next() {
		var t TaskRecord
		if err := rows.Scan(&t.ID, &t.Command, &t.Status, &t.StartedAt, &t.FinishedAt, &t.LogOutput); err != nil {
			return nil, err
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

	// 1. Scan /etc/nginx/sites-available
	if entries, err := os.ReadDir(nginxSitesAvail); err == nil {
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || name == "default" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "wdev_") ||
				strings.HasSuffix(name, ".custom.conf") || strings.HasSuffix(name, ".bak") || strings.HasSuffix(name, "~") {
				continue
			}

			domain := strings.TrimSuffix(name, ".conf")
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
			exposedTo := ""

			if strings.Contains(confStr, "laravel") {
				framework = "laravel"
			} else if strings.Contains(confStr, "wordpress") {
				framework = "wordpress"
			} else if strings.Contains(confStr, "proxy_pass") {
				framework = "proxy"
			}

			// Extract root, php version, and server_name aliases
			for _, line := range strings.Split(confStr, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "root ") {
					rootPath = strings.TrimSuffix(strings.TrimPrefix(line, "root "), ";")
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
						if tok != domain && tok != "*."+domain && !strings.HasSuffix(tok, ".test") && !strings.HasSuffix(tok, ".local") {
							if exposedTo == "" || !strings.Contains(tok, "apps.") {
								exposedTo = tok
							}
						}
					}
				}
			}

			_ = s.SaveSite(SiteRecord{
				Domain:     domain,
				RootPath:   rootPath,
				PHPVersion: phpVer,
				Framework:  framework,
				ProxyPort:  proxyPort,
				SSLEnabled: sslEnabled,
				ExposedTo:  exposedTo,
				CreatedAt:  time.Now(),
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

