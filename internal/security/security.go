package security

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"wdev/internal/store"
)

const (
	// Attack Types
	AttackPanelBruteForce   = "PANEL_BRUTE_FORCE"
	AttackSecurityEntrance  = "SECURITY_ENTRANCE_PROBE"
	AttackHoneypot          = "HONEYPOT_PROBE"
	AttackSudoBruteForce    = "SUDO_BRUTE_FORCE"
	AttackWebScanner        = "WEB_SCANNER"
	AttackSSHBruteForce     = "SSH_BRUTE_FORCE"
	AttackPortScan          = "PORT_SCAN"

	// Severity
	SeverityLow      = "LOW"
	SeverityMedium   = "MEDIUM"
	SeverityHigh     = "HIGH"
	SeverityCritical = "CRITICAL"

	// Action Taken
	ActionLogged      = "LOGGED"
	ActionRateLimited = "RATE_LIMITED"
	ActionBlocked     = "BLOCKED"
)

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
	autoBanEnabled  bool
	firewallEnabled bool
}

// New membuat instance Security Manager baru.
func New(s *store.Store) *Manager {
	m := &Manager{
		store:           s,
		strikes:         make(map[string]*strikeInfo),
		listeners:       make(map[chan Event]struct{}),
		autoBanEnabled:  true,
		firewallEnabled: os.Geteuid() == 0,
	}

	// Rutin background untuk membersihkan strike kadaluwarsa & IP expired tiap 5 menit
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			m.pruneExpired()
		}
	}()

	return m
}

func (m *Manager) pruneExpired() {
	m.mu.Lock()
	now := time.Now()
	for ip, info := range m.strikes {
		if now.Sub(info.lastSeen) > 15*time.Minute {
			delete(m.strikes, ip)
		}
	}
	m.mu.Unlock()

	_ = m.store.CleanExpiredBlockedIPs()
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

	// Trigger async country resolution jika belum diketahui
	if ev.CountryCode == "EXT" {
		FetchCountryAsync(ip, nil)
	}

	// Cek apakah IP sudah diblokir
	isBlocked, _, _ := m.store.IsIPBlocked(ip)
	if isBlocked {
		ev.ActionTaken = ActionBlocked
	} else if ev.ActionTaken == "" {
		ev.ActionTaken = ActionLogged
	}

	// Evaluasi threshold auto-ban
	shouldBan := false
	var banDuration time.Duration

	// Jangan pernah auto-ban IP loopback lokal
	if !IsPrivateOrLoopback(ip) && m.autoBanEnabled && !isBlocked {
		m.mu.Lock()
		info, exists := m.strikes[ip]
		if !exists || time.Since(info.lastSeen) > 15*time.Minute {
			info = &strikeInfo{count: 0, lastSeen: time.Now()}
			m.strikes[ip] = info
		}
		info.lastSeen = time.Now()

		switch ev.Severity {
		case SeverityCritical:
			info.count += 5
		case SeverityHigh:
			info.count += 3
		case SeverityMedium:
			info.count += 2
		default:
			info.count += 1
		}

		if ev.Severity == SeverityCritical || info.count >= 6 {
			shouldBan = true
			if ev.Severity == SeverityCritical {
				banDuration = 48 * time.Hour
			} else {
				banDuration = 24 * time.Hour
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
	if err == nil {
		ev.ID = recID
	}

	// Broadcast ke UI lewat SSE
	m.broadcast(ev)

	return shouldBan || isBlocked, err
}

// BlockIP memblokir IP baik di SQLite maupun firewall sistem (UFW/Firewalld).
func (m *Manager) BlockIP(ctx context.Context, ip, reason string, duration time.Duration) error {
	ip = strings.TrimSpace(ip)
	if ip == "" || IsPrivateOrLoopback(ip) {
		return fmt.Errorf("ip lokal/loopback tidak dapat diblokir")
	}

	var expiresAt *time.Time
	if duration > 0 {
		exp := time.Now().Add(duration)
		expiresAt = &exp
	}

	err := m.store.SaveBlockedIP(store.BlockedIPRecord{
		IP:        ip,
		Reason:    reason,
		BlockedAt: time.Now(),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return err
	}

	// Terapkan ke firewall host jika aktif
	if m.firewallEnabled {
		_ = m.applyFirewallBan(ctx, ip)
	}

	return nil
}

// UnblockIP menghapus pemblokiran IP di database dan firewall host.
func (m *Manager) UnblockIP(ctx context.Context, ip string) error {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}

	if err := m.store.DeleteBlockedIP(ip); err != nil {
		return err
	}

	if m.firewallEnabled {
		_ = m.applyFirewallUnban(ctx, ip)
	}

	m.mu.Lock()
	delete(m.strikes, ip)
	m.mu.Unlock()

	return nil
}

// IsIPBlocked memeriksa apakah suatu IP saat ini diblokir.
func (m *Manager) IsIPBlocked(ip string) (bool, *store.BlockedIPRecord, error) {
	return m.store.IsIPBlocked(ip)
}

// ListBlockedIPs mengembalikan daftar seluruh IP yang diblokir aktif.
func (m *Manager) ListBlockedIPs() ([]store.BlockedIPRecord, error) {
	return m.store.ListBlockedIPs()
}

// ListEvents mengembalikan riwayat serangan terbaru dengan filter.
func (m *Manager) ListEvents(limit, offset int, attackType, severity, search string) ([]store.SecurityEventRecord, int, error) {
	return m.store.ListSecurityEvents(limit, offset, attackType, severity, search)
}

// GetStats mengembalikan ringkasan statistik keamanan sistem.
func (m *Manager) GetStats() (store.SecurityStatsRecord, error) {
	return m.store.GetSecurityStats()
}

// ClearEvents membersihkan log serangan.
func (m *Manager) ClearEvents() error {
	return m.store.ClearAllSecurityEvents()
}

// SetFirewallEnabled mengaktifkan atau menonaktifkan interaksi langsung dengan firewall host (UFW/firewalld).
func (m *Manager) SetFirewallEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.firewallEnabled = enabled
}

// applyFirewallBan mencoba menerapkan pemblokiran IP pada UFW atau firewalld.
func (m *Manager) applyFirewallBan(ctx context.Context, ip string) error {
	m.mu.RLock()
	enabled := m.firewallEnabled
	m.mu.RUnlock()
	if !enabled {
		return nil
	}

	execCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if ufw, err := exec.LookPath("ufw"); err == nil {
		cmd := exec.CommandContext(execCtx, ufw, "insert", "1", "deny", "from", ip, "to", "any")
		_ = cmd.Run()
		return nil
	}
	if fwd, err := exec.LookPath("firewall-cmd"); err == nil {
		rule := fmt.Sprintf("rule family=ipv4 source address=%s drop", ip)
		cmd := exec.CommandContext(execCtx, fwd, "--add-rich-rule="+rule)
		_ = cmd.Run()
		return nil
	}
	return nil
}

// applyFirewallUnban mencoba mencabut pemblokiran IP pada UFW atau firewalld.
func (m *Manager) applyFirewallUnban(ctx context.Context, ip string) error {
	m.mu.RLock()
	enabled := m.firewallEnabled
	m.mu.RUnlock()
	if !enabled {
		return nil
	}

	execCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if ufw, err := exec.LookPath("ufw"); err == nil {
		cmd := exec.CommandContext(execCtx, ufw, "delete", "deny", "from", ip, "to", "any")
		_ = cmd.Run()
		return nil
	}
	if fwd, err := exec.LookPath("firewall-cmd"); err == nil {
		rule := fmt.Sprintf("rule family=ipv4 source address=%s drop", ip)
		cmd := exec.CommandContext(execCtx, fwd, "--remove-rich-rule="+rule)
		_ = cmd.Run()
		return nil
	}
	return nil
}
