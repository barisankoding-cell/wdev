package store

import (
	"database/sql"
	"strings"
	"time"
)

// SaveCFProfile menyimpan atau memperbarui profil akun Cloudflare Tunnel.
func (s *Store) SaveCFProfile(p CFProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	if strings.TrimSpace(p.AuthMode) == "" {
		p.AuthMode = "cert"
	}

	_, err := s.db.Exec(
		`INSERT INTO cf_profiles (name, auth_mode, tunnel_name, tunnel_id, account_tag, credentials_path, config_path, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
			auth_mode=excluded.auth_mode,
			tunnel_name=excluded.tunnel_name,
			tunnel_id=excluded.tunnel_id,
			account_tag=excluded.account_tag,
			credentials_path=excluded.credentials_path,
			config_path=excluded.config_path;`,
		strings.ToLower(strings.TrimSpace(p.Name)),
		strings.TrimSpace(p.AuthMode),
		strings.TrimSpace(p.TunnelName),
		strings.TrimSpace(p.TunnelID),
		strings.TrimSpace(p.AccountTag),
		strings.TrimSpace(p.CredentialsPath),
		strings.TrimSpace(p.ConfigPath),
		p.CreatedAt,
	)
	return err
}

// GetCFProfile mengambil metadata satu profil Cloudflare berdasarkan nama.
func (s *Store) GetCFProfile(name string) (CFProfile, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var p CFProfile
	err := s.db.QueryRow(
		`SELECT name, auth_mode, tunnel_name, tunnel_id, account_tag, credentials_path, config_path, created_at
		 FROM cf_profiles WHERE lower(name) = lower(?)`,
		strings.TrimSpace(name),
	).Scan(&p.Name, &p.AuthMode, &p.TunnelName, &p.TunnelID, &p.AccountTag, &p.CredentialsPath, &p.ConfigPath, &p.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return CFProfile{}, false, nil
		}
		return CFProfile{}, false, err
	}
	return p, true, nil
}

// ListCFProfiles mengembalikan seluruh profil akun Cloudflare yang terdaftar.
func (s *Store) ListCFProfiles() ([]CFProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT name, auth_mode, tunnel_name, tunnel_id, account_tag, credentials_path, config_path, created_at
		 FROM cf_profiles ORDER BY name ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CFProfile
	for rows.Next() {
		var p CFProfile
		if err := rows.Scan(&p.Name, &p.AuthMode, &p.TunnelName, &p.TunnelID, &p.AccountTag, &p.CredentialsPath, &p.ConfigPath, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteCFProfile menghapus profil Cloudflare dari SQLite.
func (s *Store) DeleteCFProfile(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM cf_profiles WHERE lower(name) = lower(?)`, strings.TrimSpace(name))
	return err
}

// SaveCFRoute menyimpan atau memperbarui rute eksposur Cloudflare Tunnel.
func (s *Store) SaveCFRoute(r CFRoute) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	prof := strings.ToLower(strings.TrimSpace(r.ProfileName))
	localDom := strings.ToLower(strings.TrimSpace(r.LocalDomain))
	host := strings.ToLower(strings.TrimSpace(r.Hostname))

	if localDom != "" {
		_, _ = s.db.Exec(`DELETE FROM cf_routes WHERE lower(local_domain) = ?`, localDom)
	}
	if host != "" && prof != "" {
		_, _ = s.db.Exec(`DELETE FROM cf_routes WHERE lower(profile_name) = ? AND lower(hostname) = ?`, prof, host)
	}

	noTLS := 0
	if r.NoTLSVerify {
		noTLS = 1
	}

	_, err := s.db.Exec(
		`INSERT INTO cf_routes (profile_name, local_domain, hostname, service_url, http_host_header, no_tls_verify, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		prof, localDom, host, strings.TrimSpace(r.ServiceURL), strings.TrimSpace(r.HTTPHostHeader), noTLS, r.CreatedAt,
	)
	return err
}

// ListCFRoutes mengembalikan rute eksposur Cloudflare (opsional difilter per profil).
func (s *Store) ListCFRoutes(profileName string) ([]CFRoute, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `SELECT id, profile_name, local_domain, hostname, service_url, http_host_header, no_tls_verify, created_at
		FROM cf_routes`
	var args []any
	if strings.TrimSpace(profileName) != "" {
		query += ` WHERE lower(profile_name) = lower(?)`
		args = append(args, strings.TrimSpace(profileName))
	}
	query += ` ORDER BY profile_name ASC, hostname ASC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CFRoute
	for rows.Next() {
		var r CFRoute
		var noTLS int
		if err := rows.Scan(&r.ID, &r.ProfileName, &r.LocalDomain, &r.Hostname, &r.ServiceURL, &r.HTTPHostHeader, &noTLS, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.NoTLSVerify = (noTLS == 1)
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetCFRouteByLocalDomain mengambil rute eksposur berdasarkan domain lokal atau hostname.
func (s *Store) GetCFRouteByLocalDomain(localDomain string) (CFRoute, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := strings.ToLower(strings.TrimSpace(localDomain))
	var r CFRoute
	var noTLS int
	err := s.db.QueryRow(
		`SELECT id, profile_name, local_domain, hostname, service_url, http_host_header, no_tls_verify, created_at
		 FROM cf_routes WHERE lower(local_domain) = ? OR lower(hostname) = ? LIMIT 1`,
		key, key,
	).Scan(&r.ID, &r.ProfileName, &r.LocalDomain, &r.Hostname, &r.ServiceURL, &r.HTTPHostHeader, &noTLS, &r.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return CFRoute{}, false, nil
		}
		return CFRoute{}, false, err
	}
	r.NoTLSVerify = (noTLS == 1)
	return r, true, nil
}

// DeleteCFRouteByLocalDomain menghapus rute berdasarkan domain lokal atau hostname.
func (s *Store) DeleteCFRouteByLocalDomain(localDomain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(strings.TrimSpace(localDomain))
	_, err := s.db.Exec(`DELETE FROM cf_routes WHERE lower(local_domain) = ? OR lower(hostname) = ?`, key, key)
	return err
}

// DeleteCFRoutesByProfile menghapus seluruh rute milik satu profil.
func (s *Store) DeleteCFRoutesByProfile(profileName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM cf_routes WHERE lower(profile_name) = lower(?)`, strings.TrimSpace(profileName))
	return err
}
