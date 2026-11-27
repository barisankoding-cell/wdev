package store

import (
	"database/sql"
	"strconv"
	"strings"
	"time"
)

type SecurityEventRecord struct {
	ID          int64     `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	SourceIP    string    `json:"source_ip"`
	CountryCode string    `json:"country_code"`
	AttackType  string    `json:"attack_type"`
	Severity    string    `json:"severity"`
	Target      string    `json:"target"`
	ActionTaken string    `json:"action_taken"`
	Details     string    `json:"details"`
}

type BlockedIPRecord struct {
	IP        string     `json:"ip"`
	Reason    string     `json:"reason"`
	BlockedAt time.Time  `json:"blocked_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type TopAttacker struct {
	IP          string `json:"ip"`
	CountryCode string `json:"country_code"`
	Count       int    `json:"count"`
}

type SecurityStatsRecord struct {
	TotalToday     int            `json:"total_today"`
	Total7Days     int            `json:"total_7_days"`
	TotalBlocked   int            `json:"total_blocked"`
	CriticalCount  int            `json:"critical_count"`
	HighCount      int            `json:"high_count"`
	AttacksByType  map[string]int `json:"attacks_by_type"`
	TopAttackerIPs []TopAttacker  `json:"top_attacker_ips"`
}

// SaveSecurityEvent mencatat satu aktivitas serangan atau kecurigaan ke SQLite.
func (s *Store) SaveSecurityEvent(rec SecurityEventRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ts := rec.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	res, err := s.db.Exec(
		`INSERT INTO security_events (timestamp, source_ip, country_code, attack_type, severity, target, action_taken, details)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);`,
		ts.Format("2006-01-02 15:04:05"),
		strings.TrimSpace(rec.SourceIP),
		strings.ToUpper(strings.TrimSpace(rec.CountryCode)),
		strings.TrimSpace(rec.AttackType),
		strings.ToUpper(strings.TrimSpace(rec.Severity)),
		strings.TrimSpace(rec.Target),
		strings.ToUpper(strings.TrimSpace(rec.ActionTaken)),
		strings.TrimSpace(rec.Details),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListSecurityEvents mengembalikan riwayat serangan terbaru dengan filter dan pagination.
func (s *Store) ListSecurityEvents(limit, offset int, attackType, severity, search string) ([]SecurityEventRecord, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	whereClauses := []string{"1=1"}
	var args []any

	if strings.TrimSpace(attackType) != "" && attackType != "all" {
		whereClauses = append(whereClauses, "attack_type = ?")
		args = append(args, strings.TrimSpace(attackType))
	}
	if strings.TrimSpace(severity) != "" && severity != "all" {
		whereClauses = append(whereClauses, "severity = ?")
		args = append(args, strings.ToUpper(strings.TrimSpace(severity)))
	}
	if cleanSearch := strings.TrimSpace(search); cleanSearch != "" {
		whereClauses = append(whereClauses, "(source_ip LIKE ? OR target LIKE ? OR details LIKE ?)")
		patt := "%" + cleanSearch + "%"
		args = append(args, patt, patt, patt)
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	var total int
	countQuery := "SELECT COUNT(*) FROM security_events WHERE " + whereSQL
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	selectQuery := `SELECT id, timestamp, source_ip, country_code, attack_type, severity, target, action_taken, details
		FROM security_events
		WHERE ` + whereSQL + `
		ORDER BY id DESC
		LIMIT ? OFFSET ?;`

	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := s.db.Query(selectQuery, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	events := make([]SecurityEventRecord, 0)
	for rows.Next() {
		var ev SecurityEventRecord
		var rawTime string
		if err := rows.Scan(
			&ev.ID,
			&rawTime,
			&ev.SourceIP,
			&ev.CountryCode,
			&ev.AttackType,
			&ev.Severity,
			&ev.Target,
			&ev.ActionTaken,
			&ev.Details,
		); err != nil {
			return nil, 0, err
		}
		ev.Timestamp = scanStoreTime(rawTime)
		events = append(events, ev)
	}

	return events, total, rows.Err()
}

// UpdateSecurityEventCountry memperbarui country_code event yang masih kosong atau 'EXT'
// setelah resolusi GeoIP asinkron selesai.
func (s *Store) UpdateSecurityEventCountry(ip, countryCode string) error {
	cleanIP := strings.TrimSpace(ip)
	code := strings.ToUpper(strings.TrimSpace(countryCode))
	if cleanIP == "" || code == "" || code == "EXT" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`UPDATE security_events SET country_code = ? WHERE source_ip = ? AND (country_code = '' OR country_code = 'EXT');`,
		code, cleanIP,
	)
	return err
}

// ClearAllSecurityEvents membersihkan seluruh log serangan.
func (s *Store) ClearAllSecurityEvents() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM security_events;")
	return err
}

// PruneOldSecurityEvents menghapus log serangan yang lebih tua dari retentionDays hari.
func (s *Store) PruneOldSecurityEvents(retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	modifier := "-" + strconv.Itoa(retentionDays) + " days"
	res, err := s.db.Exec(
		`DELETE FROM security_events WHERE timestamp < datetime('now', ?, 'localtime');`,
		modifier,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SaveBlockedIP menambahkan atau memperbarui IP yang diblokir.
func (s *Store) SaveBlockedIP(rec BlockedIPRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	blockedAt := rec.BlockedAt
	if blockedAt.IsZero() {
		blockedAt = time.Now()
	}

	var expiresVal any
	if rec.ExpiresAt != nil && !rec.ExpiresAt.IsZero() {
		expiresVal = rec.ExpiresAt.Format("2006-01-02 15:04:05")
	}

	_, err := s.db.Exec(
		`INSERT INTO blocked_ips (ip, reason, blocked_at, expires_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(ip) DO UPDATE SET
			reason=excluded.reason,
			blocked_at=excluded.blocked_at,
			expires_at=excluded.expires_at;`,
		strings.TrimSpace(rec.IP),
		strings.TrimSpace(rec.Reason),
		blockedAt.Format("2006-01-02 15:04:05"),
		expiresVal,
	)
	return err
}

// DeleteBlockedIP menghapus pemblokiran IP.
func (s *Store) DeleteBlockedIP(ip string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM blocked_ips WHERE ip = ?;", strings.TrimSpace(ip))
	return err
}

// ListBlockedIPs mengembalikan daftar seluruh IP yang diblokir dan masih aktif.
func (s *Store) ListBlockedIPs() ([]BlockedIPRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT ip, reason, blocked_at, expires_at
		FROM blocked_ips
		WHERE expires_at IS NULL OR expires_at > datetime('now', 'localtime')
		ORDER BY blocked_at DESC;`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]BlockedIPRecord, 0)
	for rows.Next() {
		var rec BlockedIPRecord
		var rawBlocked string
		var rawExpires sql.NullString
		if err := rows.Scan(&rec.IP, &rec.Reason, &rawBlocked, &rawExpires); err != nil {
			return nil, err
		}
		rec.BlockedAt = scanStoreTime(rawBlocked)
		if rawExpires.Valid && rawExpires.String != "" {
			exp := scanStoreTime(rawExpires.String)
			rec.ExpiresAt = &exp
		}
		list = append(list, rec)
	}
	return list, rows.Err()
}

// IsIPBlocked memeriksa apakah suatu IP sedang diblokir aktif.
func (s *Store) IsIPBlocked(ip string) (bool, *BlockedIPRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rec BlockedIPRecord
	var rawBlocked string
	var rawExpires sql.NullString

	err := s.db.QueryRow(
		`SELECT ip, reason, blocked_at, expires_at
		FROM blocked_ips
		WHERE ip = ? AND (expires_at IS NULL OR expires_at > datetime('now', 'localtime'));`,
		strings.TrimSpace(ip),
	).Scan(&rec.IP, &rec.Reason, &rawBlocked, &rawExpires)

	if err == sql.ErrNoRows {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}

	rec.BlockedAt = scanStoreTime(rawBlocked)
	if rawExpires.Valid && rawExpires.String != "" {
		exp := scanStoreTime(rawExpires.String)
		rec.ExpiresAt = &exp
	}
	return true, &rec, nil
}

// CleanExpiredBlockedIPs menghapus entri IP yang masa blokirnya sudah kedaluwarsa.
func (s *Store) CleanExpiredBlockedIPs() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM blocked_ips WHERE expires_at IS NOT NULL AND expires_at <= datetime('now', 'localtime');")
	return err
}

// GetSecurityStats mengumpulkan ringkasan statistik keamanan untuk dashboard.
func (s *Store) GetSecurityStats() (SecurityStatsRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stats SecurityStatsRecord
	stats.AttacksByType = make(map[string]int)
	stats.TopAttackerIPs = make([]TopAttacker, 0)

	// 1. Total hari ini & 7 hari terakhir
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM security_events WHERE date(timestamp) = date('now', 'localtime');`).Scan(&stats.TotalToday)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM security_events WHERE timestamp >= datetime('now', '-7 days', 'localtime');`).Scan(&stats.Total7Days)

	// 2. Total IP aktif terblokir
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM blocked_ips WHERE expires_at IS NULL OR expires_at > datetime('now', 'localtime');`).Scan(&stats.TotalBlocked)

	// 3. Count Critical & High hari ini
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM security_events WHERE severity = 'CRITICAL' AND date(timestamp) = date('now', 'localtime');`).Scan(&stats.CriticalCount)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM security_events WHERE severity = 'HIGH' AND date(timestamp) = date('now', 'localtime');`).Scan(&stats.HighCount)

	// 4. Group by attack type (7 hari terakhir)
	rowsType, err := s.db.Query(`SELECT attack_type, COUNT(*) FROM security_events
		WHERE timestamp >= datetime('now', '-7 days', 'localtime')
		GROUP BY attack_type
		ORDER BY COUNT(*) DESC;`)
	if err == nil {
		defer rowsType.Close()
		for rowsType.Next() {
			var aType string
			var count int
			if err := rowsType.Scan(&aType, &count); err == nil {
				stats.AttacksByType[aType] = count
			}
		}
	}

	// 5. Top 5 attacker IPs (7 hari terakhir)
	rowsTop, err := s.db.Query(`SELECT source_ip, country_code, COUNT(*) as cnt
		FROM security_events
		WHERE timestamp >= datetime('now', '-7 days', 'localtime')
		GROUP BY source_ip, country_code
		ORDER BY cnt DESC
		LIMIT 5;`)
	if err == nil {
		defer rowsTop.Close()
		for rowsTop.Next() {
			var top TopAttacker
			if err := rowsTop.Scan(&top.IP, &top.CountryCode, &top.Count); err == nil {
				stats.TopAttackerIPs = append(stats.TopAttackerIPs, top)
			}
		}
	}

	return stats, nil
}
