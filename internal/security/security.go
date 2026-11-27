package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"wdev/internal/store"
)

const (
	// Attack Types
	AttackPanelBruteForce  = "PANEL_BRUTE_FORCE"
	AttackSecurityEntrance = "SECURITY_ENTRANCE_PROBE"
	AttackHoneypot         = "HONEYPOT_PROBE"
	AttackSudoBruteForce   = "SUDO_BRUTE_FORCE"
	AttackWebScanner       = "WEB_SCANNER"
	AttackSSHBruteForce    = "SSH_BRUTE_FORCE"
	AttackPortScan         = "PORT_SCAN"
	AttackMailBruteForce   = "MAIL_BRUTE_FORCE"

	// Severity
	SeverityLow      = "LOW"
	SeverityMedium   = "MEDIUM"
	SeverityHigh     = "HIGH"
	SeverityCritical = "CRITICAL"

	// Action Taken
	ActionLogged      = "LOGGED"
	ActionRateLimited = "RATE_LIMITED"
	ActionBlocked     = "BLOCKED"

	// SettingSecurityPolicy kunci SQLite untuk menyimpan kebijakan pemantauan keamanan.
	SettingSecurityPolicy = "security_policy_config"
)

// PolicyConfig menyimpan konfigurasi kebijakan deteksi dan mitigasi keamanan.
type PolicyConfig struct {
	AutoBanEnabled         bool     `json:"auto_ban_enabled"`
	FirewallSyncEnabled    bool     `json:"firewall_sync_enabled"`
	SSHMonitorEnabled      bool     `json:"ssh_monitor_enabled"`
	NginxMonitorEnabled    bool     `json:"nginx_monitor_enabled"`
	PortScanMonitorEnabled bool     `json:"port_scan_monitor_enabled"`
	PanelScannerEnabled    bool     `json:"panel_scanner_enabled"`
	MailMonitorEnabled     bool     `json:"mail_monitor_enabled"`
	NginxBlockSyncEnabled  bool     `json:"nginx_block_sync_enabled"`
	CloudflareRealIPMode   string   `json:"cloudflare_real_ip_mode"`
	StrikeThreshold        int      `json:"strike_threshold"`
	BanDurationHours       int      `json:"ban_duration_hours"`
	CriticalBanHours       int      `json:"critical_ban_hours"`
	RetentionDays          int      `json:"retention_days"`
	AlertWebhookURL        string   `json:"alert_webhook_url"`
	AlertMinSeverity       string   `json:"alert_min_severity"`
	CFBlockSyncEnabled     bool     `json:"cf_block_sync_enabled"`
	CFZoneID               string   `json:"cf_zone_id"`
	CFAPIToken             string   `json:"cf_api_token,omitempty"`
	CFTokenConfigured      bool     `json:"cf_token_configured,omitempty"`
	ClearCFToken           bool     `json:"clear_cf_token,omitempty"`
	Whitelist              []string `json:"whitelist"`
}

// DefaultPolicyConfig mengembalikan kebijakan bawaan security monitoring.
func DefaultPolicyConfig() PolicyConfig {
	return PolicyConfig{
		AutoBanEnabled:         true,
		FirewallSyncEnabled:    true,
		SSHMonitorEnabled:      true,
		NginxMonitorEnabled:    true,
		PortScanMonitorEnabled: true,
		PanelScannerEnabled:    true,
		MailMonitorEnabled:     true,
		NginxBlockSyncEnabled:  true,
		CloudflareRealIPMode:   cloudflareRealIPModeAuto,
		StrikeThreshold:        6,
		BanDurationHours:       24,
		CriticalBanHours:       48,
		RetentionDays:          30,
		AlertMinSeverity:       SeverityCritical,
		Whitelist:              []string{},
	}
}

// Redacted mengembalikan salinan PolicyConfig dengan token sensitif disamarkan untuk API UI.
func (p PolicyConfig) Redacted() PolicyConfig {
	cp := p
	cp.Whitelist = append([]string(nil), p.Whitelist...)
	cp.CFTokenConfigured = strings.TrimSpace(p.CFAPIToken) != ""
	cp.CFAPIToken = ""
	cp.ClearCFToken = false
	return cp
}

// Event merepresentasikan satu aktivitas keamanan/serangan yang terdeteksi.
type Event struct {
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

type strikeInfo struct {
	count    int
	lastSeen time.Time
}

// Manager mengelola deteksi, mitigasi, pencatatan database, dan auto-ban serangan siber.
type Manager struct {
	store           *store.Store
	mu              sync.RWMutex
	strikes         map[string]*strikeInfo
	listeners       map[chan Event]struct{}
	policy          PolicyConfig
	autoBanEnabled  bool
	firewallEnabled bool
	lastFWError     string

	watcherStates map[string]*watcherStats
	watcherCancel context.CancelFunc
	watcherWG     sync.WaitGroup

	sshTracker      *slidingEventTracker
	nginxTracker    *slidingEventTracker
	portTracker     *slidingEventTracker
	panel404Tracker *slidingEventTracker
	mailTracker     *slidingEventTracker

	fwExec     fwExecFn
	lookPathFn func(string) string

	nginxConfDDir      string
	lastNginxError     string
	nginxSyncMu        sync.Mutex
	nginxSyncTimer     *time.Timer
	cloudflaredCheckFn func() bool
	nginxTestReloadFn  func(context.Context) error

	webhookCooldown map[string]time.Time
	webhookClient   *http.Client
	cfAPIBaseURL    string
}

// New membuat instance Security Manager baru.
func New(s *store.Store) *Manager {
	def := DefaultPolicyConfig()
	m := &Manager{
		store:           s,
		strikes:         make(map[string]*strikeInfo),
		listeners:       make(map[chan Event]struct{}),
		policy:          def,
		autoBanEnabled:  def.AutoBanEnabled,
		firewallEnabled: os.Geteuid() == 0 && def.FirewallSyncEnabled,
		watcherStates: map[string]*watcherStats{
			"panel_auth":    {running: true, available: true, source: "HTTP /api/auth + /api/sudo + /api/terminal", note: "Memantau brute-force login panel, terminal unlock, dan eskalasi sudo"},
			"panel_scanner": {running: true, available: true, source: "HTTP Middleware (Honeypot, Entrance, Scanner)", note: "Memantau probe decoy, scanner User-Agent, traversal, dan burst 404 panel"},
			"ssh":           {running: false, available: false, source: "/var/log/auth.log", note: "Menunggu startup monitor"},
			"nginx":         {running: false, available: false, source: "/var/log/nginx/*access*.log", note: "Menunggu startup monitor"},
			"portscan":      {running: false, available: false, source: "/proc/net/tcp + firewall log", note: "Menunggu startup monitor"},
			"mail":          {running: false, available: false, source: "mail.log / stalwart / journalctl", note: "Menunggu startup monitor"},
		},
		sshTracker:      newSlidingEventTracker(),
		nginxTracker:    newSlidingEventTracker(),
		portTracker:     newSlidingEventTracker(),
		panel404Tracker: newSlidingEventTracker(),
		mailTracker:     newSlidingEventTracker(),
		webhookCooldown: make(map[string]time.Time),
	}
	m.loadPolicy()

	// Rutin background untuk membersihkan strike kadaluwarsa, IP expired, dan retensi log tiap 5 menit
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			m.pruneExpired()
		}
	}()

	return m
}

func (m *Manager) loadPolicy() {
	if m.store == nil {
		return
	}
	raw, err := m.store.GetSetting(SettingSecurityPolicy)
	if err != nil || strings.TrimSpace(raw) == "" {
		return
	}
	cfg := DefaultPolicyConfig()
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return
	}
	sanitized, err := sanitizePolicyConfig(cfg)
	if err != nil {
		return
	}

	m.mu.Lock()
	m.policy = sanitized
	m.autoBanEnabled = sanitized.AutoBanEnabled
	m.firewallEnabled = sanitized.FirewallSyncEnabled
	m.mu.Unlock()
}

func sanitizePolicyConfig(cfg PolicyConfig) (PolicyConfig, error) {
	if cfg.StrikeThreshold < 2 {
		cfg.StrikeThreshold = 6
	} else if cfg.StrikeThreshold > 100 {
		cfg.StrikeThreshold = 100
	}
	if cfg.BanDurationHours < 1 {
		cfg.BanDurationHours = 24
	} else if cfg.BanDurationHours > 8760 {
		cfg.BanDurationHours = 8760
	}
	if cfg.CriticalBanHours < 1 {
		cfg.CriticalBanHours = 48
	} else if cfg.CriticalBanHours > 8760 {
		cfg.CriticalBanHours = 8760
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 30
	} else if cfg.RetentionDays > 365 {
		cfg.RetentionDays = 365
	}

	switch strings.ToLower(strings.TrimSpace(cfg.CloudflareRealIPMode)) {
	case cloudflareRealIPModeOn:
		cfg.CloudflareRealIPMode = cloudflareRealIPModeOn
	case cloudflareRealIPModeOff:
		cfg.CloudflareRealIPMode = cloudflareRealIPModeOff
	default:
		cfg.CloudflareRealIPMode = cloudflareRealIPModeAuto
	}

	cleanWebhook, wErr := ValidateWebhookURL(cfg.AlertWebhookURL)
	if wErr != nil {
		return cfg, wErr
	}
	cfg.AlertWebhookURL = cleanWebhook

	switch strings.ToUpper(strings.TrimSpace(cfg.AlertMinSeverity)) {
	case SeverityHigh:
		cfg.AlertMinSeverity = SeverityHigh
	default:
		cfg.AlertMinSeverity = SeverityCritical
	}

	cfg.CFZoneID = strings.TrimSpace(cfg.CFZoneID)
	cfg.CFAPIToken = strings.TrimSpace(cfg.CFAPIToken)
	cfg.CFTokenConfigured = false
	cfg.ClearCFToken = false
	if cfg.CFBlockSyncEnabled && (cfg.CFZoneID == "" || cfg.CFAPIToken == "") {
		return cfg, fmt.Errorf("Zone ID dan API Token Cloudflare wajib diisi untuk sinkronisasi Cloudflare WAF")
	}

	seen := make(map[string]bool)
	cleanList := make([]string, 0, len(cfg.Whitelist))
	for _, raw := range cfg.Whitelist {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		norm, err := NormalizeIPOrCIDR(raw)
		if err != nil {
			return cfg, err
		}
		if !seen[norm] {
			seen[norm] = true
			cleanList = append(cleanList, norm)
		}
	}
	cfg.Whitelist = cleanList
	return cfg, nil
}

// GetPolicy mengembalikan salinan konfigurasi kebijakan keamanan yang sedang aktif.
func (m *Manager) GetPolicy() PolicyConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := m.policy
	cp.Whitelist = append([]string(nil), m.policy.Whitelist...)
	return cp
}

// RedactedPolicy mengembalikan salinan kebijakan dengan kredensial rahasia yang disamarkan.
func (m *Manager) RedactedPolicy() PolicyConfig {
	return m.GetPolicy().Redacted()
}

// UpdatePolicy memvalidasi, menyimpan, dan menerapkan pembaruan kebijakan keamanan.
func (m *Manager) UpdatePolicy(ctx context.Context, next PolicyConfig) (PolicyConfig, error) {
	clearToken := next.ClearCFToken
	next.ClearCFToken = false
	next.CFTokenConfigured = false
	if clearToken {
		next.CFAPIToken = ""
	} else {
		tok := strings.TrimSpace(next.CFAPIToken)
		if tok == "" || tok == "********" {
			m.mu.RLock()
			next.CFAPIToken = m.policy.CFAPIToken
			m.mu.RUnlock()
		}
	}

	sanitized, err := sanitizePolicyConfig(next)
	if err != nil {
		return PolicyConfig{}, err
	}

	if m.store != nil {
		raw, merr := json.Marshal(sanitized)
		if merr != nil {
			return PolicyConfig{}, merr
		}
		if serr := m.store.SetSetting(SettingSecurityPolicy, string(raw)); serr != nil {
			return PolicyConfig{}, serr
		}
	}

	m.mu.Lock()
	m.policy = sanitized
	m.autoBanEnabled = sanitized.AutoBanEnabled
	m.firewallEnabled = sanitized.FirewallSyncEnabled
	m.mu.Unlock()

	// Pastikan IP yang baru masuk whitelist otomatis dicabut dari daftar blokir aktif
	if m.store != nil && len(sanitized.Whitelist) > 0 {
		if blocked, lerr := m.store.ListBlockedIPs(); lerr == nil {
			for _, rec := range blocked {
				if MatchesIPList(rec.IP, sanitized.Whitelist) {
					_ = m.UnblockIP(ctx, rec.IP)
				}
			}
		}
	}

	if sanitized.FirewallSyncEnabled {
		go func() {
			sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			m.SyncBlockedIPsToFirewall(sctx)
		}()
	}

	m.scheduleNginxSync()

	return m.RedactedPolicy(), nil
}

// IsWhitelisted memeriksa apakah IP adalah jaringan lokal/VPN atau berada di daftar whitelist kustom.
func (m *Manager) IsWhitelisted(ipStr string) bool {
	if IsPrivateOrLoopback(ipStr) {
		return true
	}
	m.mu.RLock()
	wl := m.policy.Whitelist
	m.mu.RUnlock()
	return MatchesIPList(ipStr, wl)
}

// GetMonitorStatus mengembalikan status kebijakan, firewall host, Nginx vhost, dan setiap watcher keamanan.
func (m *Manager) GetMonitorStatus(ctx context.Context) MonitorStatusReport {
	policy := m.GetPolicy()
	fw := m.DetectFirewallStatus(ctx)
	nx := m.GetNginxStatus()

	m.mu.RLock()
	getSnap := func(id, defaultName string, enabled bool) WatcherStatus {
		st := m.watcherStates[id]
		if st == nil {
			return WatcherStatus{ID: id, Name: defaultName, Enabled: enabled}
		}
		var lastCopy *time.Time
		if st.lastEventAt != nil {
			t := *st.lastEventAt
			lastCopy = &t
		}
		return WatcherStatus{
			ID:          id,
			Name:        defaultName,
			Enabled:     enabled,
			Running:     enabled && st.running,
			Source:      st.source,
			Available:   st.available,
			EventsCount: st.eventsCount,
			LastEventAt: lastCopy,
			Note:        st.note,
		}
	}
	watchers := []WatcherStatus{
		getSnap("panel_auth", "Panel Auth & Sudo Guard", true),
		getSnap("panel_scanner", "Panel Honeypot & Web Scanner", policy.PanelScannerEnabled),
		getSnap("ssh", "SSH Brute-Force Watcher", policy.SSHMonitorEnabled),
		getSnap("nginx", "Nginx VHost Scanner Watcher", policy.NginxMonitorEnabled),
		getSnap("mail", "Mail Service Brute-Force Watcher", policy.MailMonitorEnabled),
		getSnap("portscan", "Host Port Scan Watcher", policy.PortScanMonitorEnabled),
	}
	m.mu.RUnlock()

	return MonitorStatusReport{
		Policy:   policy.Redacted(),
		Firewall: fw,
		Nginx:    nx,
		Watchers: watchers,
	}
}

func (m *Manager) pruneExpired() {
	m.mu.Lock()
	now := time.Now()
	for ip, info := range m.strikes {
		if now.Sub(info.lastSeen) > 15*time.Minute {
			delete(m.strikes, ip)
		}
	}
	retDays := m.policy.RetentionDays
	m.mu.Unlock()

	if m.store != nil {
		_ = m.store.CleanExpiredBlockedIPs()
		if retDays <= 0 {
			retDays = 30
		}
		_, _ = m.store.PruneOldSecurityEvents(retDays)
	}
}

// Subscribe mendaftarkan channel untuk menerima stream event keamanan realtime.
func (m *Manager) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 32)
	m.mu.Lock()
	m.listeners[ch] = struct{}{}
	m.mu.Unlock()

	unsubscribe := func() {
		m.mu.Lock()
		delete(m.listeners, ch)
		close(ch)
		m.mu.Unlock()
	}
	return ch, unsubscribe
}

func (m *Manager) broadcast(ev Event) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for ch := range m.listeners {
		select {
		case ch <- ev:
		default:
			// Non-blocking drop jika listener lambat
		}
	}
}

func watcherIDForAttackType(attackType string) string {
	switch attackType {
	case AttackPanelBruteForce, AttackSudoBruteForce:
		return "panel_auth"
	case AttackHoneypot, AttackSecurityEntrance, AttackWebScanner:
		return "panel_scanner"
	case AttackSSHBruteForce:
		return "ssh"
	case AttackMailBruteForce:
		return "mail"
	case AttackPortScan:
		return "portscan"
	default:
		return ""
	}
}

// RecordPanel404 mencatat respons HTTP 404 pada panel dan memicu WEB_SCANNER bila ada burst probing.
func (m *Manager) RecordPanel404(ctx context.Context, ip, target, userAgent string) {
	policy := m.GetPolicy()
	if !policy.PanelScannerEnabled || m.IsWhitelisted(ip) {
		return
	}
	if isStaticAssetPath(target) {
		return
	}
	now := time.Now()
	count, distinct, canEmit := m.panel404Tracker.addAndCheck(ip, target, now, 60*time.Second, 45*time.Second)
	if len(distinct) >= 10 && canEmit {
		m.panel404Tracker.markEmitted(ip, now)
		_, _ = m.RecordAttack(ctx, Event{
			Timestamp:  now,
			SourceIP:   ip,
			AttackType: AttackWebScanner,
			Severity:   SeverityHigh,
			Target:     target,
			Details:    fmt.Sprintf("Burst 404 probe pada panel (%d request ke %d path unik dalam 60d; UA: %s)", count, len(distinct), userAgent),
		})
	}
}

// RecordAttack mencatat event serangan, mengevaluasi threshold strike, dan melakukan auto-ban jika memenuhi syarat.
func (m *Manager) RecordAttack(ctx context.Context, ev Event) (bool, error) {
	ip := strings.TrimSpace(ev.SourceIP)
	if ip == "" {
		return false, nil
	}

	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	if ev.CountryCode == "" {
		ev.CountryCode = ResolveCountry(ip)
	}

	// Trigger async country resolution jika belum diketahui dan backfill SQLite saat selesai
	if ev.CountryCode == "EXT" {
		st := m.store
		FetchCountryAsync(ip, func(code string) {
			if st != nil {
				_ = st.UpdateSecurityEventCountry(ip, code)
			}
		})
	}

	// Cek apakah IP sudah diblokir
	isBlocked := false
	if m.store != nil {
		isBlocked, _, _ = m.store.IsIPBlocked(ip)
	}
	if isBlocked {
		ev.ActionTaken = ActionBlocked
	} else if ev.ActionTaken == "" {
		ev.ActionTaken = ActionLogged
	}

	// Evaluasi threshold auto-ban
	shouldBan := false
	var banDuration time.Duration

	m.mu.RLock()
	autoBan := m.autoBanEnabled
	threshold := m.policy.StrikeThreshold
	if threshold <= 0 {
		threshold = 6
	}
	banHours := m.policy.BanDurationHours
	if banHours <= 0 {
		banHours = 24
	}
	critHours := m.policy.CriticalBanHours
	if critHours <= 0 {
		critHours = 48
	}
	m.mu.RUnlock()

	// Jangan pernah auto-ban IP loopback lokal, VPN privat, atau IP whitelist
	if !m.IsWhitelisted(ip) && autoBan && !isBlocked {
		m.mu.Lock()
		info, exists := m.strikes[ip]
		if !exists || time.Since(info.lastSeen) > 15*time.Minute {
			info = &strikeInfo{count: 0, lastSeen: time.Now()}
			m.strikes[ip] = info
		}
		info.lastSeen = time.Now()

		switch ev.Severity {
		case SeverityCritical:
			info.count += threshold
		case SeverityHigh:
			info.count += 3
		case SeverityMedium:
			info.count += 2
		default:
			info.count += 1
		}

		if ev.Severity == SeverityCritical || info.count >= threshold {
			shouldBan = true
			if ev.Severity == SeverityCritical {
				banDuration = time.Duration(critHours) * time.Hour
			} else {
				banDuration = time.Duration(banHours) * time.Hour
			}
			delete(m.strikes, ip)
		}
		m.mu.Unlock()
	}

	if shouldBan {
		ev.ActionTaken = ActionBlocked
		reason := fmt.Sprintf("Auto-ban: %s on %s (Severity: %s)", ev.AttackType, ev.Target, ev.Severity)
		_ = m.BlockIP(ctx, ip, reason, banDuration)
	}

	// Simpan event ke SQLite
	if m.store != nil {
		recID, err := m.store.SaveSecurityEvent(store.SecurityEventRecord{
			Timestamp:   ev.Timestamp,
			SourceIP:    ev.SourceIP,
			CountryCode: ev.CountryCode,
			AttackType:  ev.AttackType,
			Severity:    ev.Severity,
			Target:      ev.Target,
			ActionTaken: ev.ActionTaken,
			Details:     ev.Details,
		})
		if err != nil {
			return shouldBan || isBlocked, err
		}
		ev.ID = recID
	}

	if wID := watcherIDForAttackType(ev.AttackType); wID == "panel_auth" || wID == "panel_scanner" {
		now := time.Now()
		m.updateWatcherState(wID, func(st *watcherStats) {
			st.eventsCount++
			st.lastEventAt = &now
		})
	}

	// Broadcast ke UI lewat SSE dan trigger alert webhook jika memenuhi severity minimum
	m.broadcast(ev)
	m.maybeDispatchWebhook(ev)

	return shouldBan || isBlocked, nil
}

// BlockIP memblokir IP baik di SQLite, firewall sistem (UFW/firewalld/nftables), Nginx L7, dan Cloudflare WAF.
func (m *Manager) BlockIP(ctx context.Context, ip, reason string, duration time.Duration) error {
	ip = strings.TrimSpace(ip)
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return fmt.Errorf("format alamat IP tidak valid")
	}
	cleanIP := parsed.String()
	if IsPrivateOrLoopback(cleanIP) {
		return fmt.Errorf("ip lokal/loopback tidak dapat diblokir")
	}
	if m.IsWhitelisted(cleanIP) {
		return fmt.Errorf("ip berada dalam daftar whitelist tepercaya dan tidak dapat diblokir")
	}

	var expiresAt *time.Time
	if duration > 0 {
		exp := time.Now().Add(duration)
		expiresAt = &exp
	}

	if m.store != nil {
		err := m.store.SaveBlockedIP(store.BlockedIPRecord{
			IP:        cleanIP,
			Reason:    reason,
			BlockedAt: time.Now(),
			ExpiresAt: expiresAt,
		})
		if err != nil {
			return err
		}
	}

	// Terapkan ke firewall host, Nginx vhost blocklist, dan Cloudflare WAF jika dikonfigurasi
	if m.firewallEnabled {
		_ = m.applyFirewallBan(ctx, cleanIP)
	}
	m.scheduleNginxSync()
	m.syncCloudflareAccessRule(ctx, cleanIP, reason, true)

	return nil
}

// UnblockIP menghapus pemblokiran IP di database, firewall host, Nginx L7, dan Cloudflare WAF.
func (m *Manager) UnblockIP(ctx context.Context, ip string) error {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}
	if parsed := net.ParseIP(ip); parsed != nil {
		ip = parsed.String()
	}

	if m.store != nil {
		if err := m.store.DeleteBlockedIP(ip); err != nil {
			return err
		}
	}

	if m.firewallEnabled {
		_ = m.applyFirewallUnban(ctx, ip)
	}
	m.scheduleNginxSync()
	m.syncCloudflareAccessRule(ctx, ip, "Unblocked by wdev Security Monitor", false)

	m.mu.Lock()
	delete(m.strikes, ip)
	m.mu.Unlock()

	return nil
}

// IsIPBlocked memeriksa apakah suatu IP saat ini diblokir.
func (m *Manager) IsIPBlocked(ip string) (bool, *store.BlockedIPRecord, error) {
	if m.store == nil {
		return false, nil, nil
	}
	return m.store.IsIPBlocked(ip)
}

// ListBlockedIPs mengembalikan daftar seluruh IP yang diblokir aktif.
func (m *Manager) ListBlockedIPs() ([]store.BlockedIPRecord, error) {
	if m.store == nil {
		return []store.BlockedIPRecord{}, nil
	}
	return m.store.ListBlockedIPs()
}

// ListEvents mengembalikan riwayat serangan terbaru dengan filter.
func (m *Manager) ListEvents(limit, offset int, attackType, severity, search string) ([]store.SecurityEventRecord, int, error) {
	if m.store == nil {
		return []store.SecurityEventRecord{}, 0, nil
	}
	return m.store.ListSecurityEvents(limit, offset, attackType, severity, search)
}

// GetStats mengembalikan ringkasan statistik keamanan sistem.
func (m *Manager) GetStats() (store.SecurityStatsRecord, error) {
	if m.store == nil {
		return store.SecurityStatsRecord{
			AttacksByType:  map[string]int{},
			TopAttackerIPs: []store.TopAttacker{},
		}, nil
	}
	return m.store.GetSecurityStats()
}

// ClearEvents membersihkan log serangan.
func (m *Manager) ClearEvents() error {
	if m.store == nil {
		return nil
	}
	return m.store.ClearAllSecurityEvents()
}

// SetFirewallEnabled mengaktifkan atau menonaktifkan interaksi langsung dengan firewall host (UFW/firewalld).
func (m *Manager) SetFirewallEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.firewallEnabled = enabled
	m.policy.FirewallSyncEnabled = enabled
}
