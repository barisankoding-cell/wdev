package security

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// FirewallStatus merangkum status integrasi firewall host (UFW atau firewalld).
type FirewallStatus struct {
	Engine      string `json:"engine"` // "ufw" | "firewalld" | "none"
	Active      bool   `json:"active"`
	SyncEnabled bool   `json:"sync_enabled"`
	CanManage   bool   `json:"can_manage"`
	Summary     string `json:"summary"`
	LastError   string `json:"last_error,omitempty"`
}

type fwExecFn func(ctx context.Context, bin string, args ...string) (string, error)

func defaultExecPriv(ctx context.Context, bin string, args ...string) (string, error) {
	execCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()

	if os.Geteuid() == 0 {
		out, err := exec.CommandContext(execCtx, bin, args...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	if sudoPath, err := exec.LookPath("sudo"); err == nil {
		sArgs := append([]string{"-n", bin}, args...)
		out, err2 := exec.CommandContext(execCtx, sudoPath, sArgs...).CombinedOutput()
		return strings.TrimSpace(string(out)), err2
	}

	out, err := exec.CommandContext(execCtx, bin, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (m *Manager) runFirewallCmd(ctx context.Context, bin string, args ...string) (string, error) {
	if m.fwExec != nil {
		return m.fwExec(ctx, bin, args...)
	}
	return defaultExecPriv(ctx, bin, args...)
}

func (m *Manager) lookPath(bin string) string {
	if m.lookPathFn != nil {
		return m.lookPathFn(bin)
	}
	p, err := exec.LookPath(bin)
	if err != nil {
		return ""
	}
	return p
}

// DetectFirewallStatus mendeteksi engine firewall aktif di host dan apakah wdev memiliki izin mengelolanya.
func (m *Manager) DetectFirewallStatus(ctx context.Context) FirewallStatus {
	m.mu.RLock()
	syncEnabled := m.firewallEnabled
	lastErr := m.lastFWError
	m.mu.RUnlock()

	if ctx == nil {
		ctx = context.Background()
	}

	var fallback *FirewallStatus

	// 1. Cek UFW
	if ufwBin := m.lookPath("ufw"); ufwBin != "" {
		out, err := m.runFirewallCmd(ctx, ufwBin, "status")
		low := strings.ToLower(out)
		switch {
		case strings.Contains(low, "status: active"):
			return FirewallStatus{
				Engine:      "ufw",
				Active:      true,
				SyncEnabled: syncEnabled,
				CanManage:   true,
				Summary:     "UFW aktif & siap memblokir IP di level kernel",
				LastError:   lastErr,
			}
		case strings.Contains(low, "status: inactive"):
			fallback = &FirewallStatus{
				Engine:      "ufw",
				Active:      false,
				SyncEnabled: syncEnabled,
				CanManage:   true,
				Summary:     "UFW terpasang namun tidak aktif (hanya blokir HTTP panel)",
				LastError:   lastErr,
			}
		case err != nil && (strings.Contains(low, "need to be root") || strings.Contains(low, "password is required") || strings.Contains(low, "permission denied")):
			fallback = &FirewallStatus{
				Engine:      "ufw",
				Active:      false,
				SyncEnabled: syncEnabled,
				CanManage:   false,
				Summary:     "UFW terpasang tetapi butuh hak root/sudo untuk mengelola rule",
				LastError:   lastErr,
			}
		}
	}

	// 2. Cek firewalld
	if fwdBin := m.lookPath("firewall-cmd"); fwdBin != "" {
		out, err := m.runFirewallCmd(ctx, fwdBin, "--state")
		low := strings.ToLower(strings.TrimSpace(out))
		if err == nil && low == "running" {
			return FirewallStatus{
				Engine:      "firewalld",
				Active:      true,
				SyncEnabled: syncEnabled,
				CanManage:   true,
				Summary:     "firewalld aktif & siap memblokir IP di level kernel",
				LastError:   lastErr,
			}
		}
		if fallback == nil {
			if err != nil && (strings.Contains(low, "password is required") || strings.Contains(low, "authorization failed") || strings.Contains(low, "permission")) {
				fallback = &FirewallStatus{
					Engine:      "firewalld",
					Active:      false,
					SyncEnabled: syncEnabled,
					CanManage:   false,
					Summary:     "firewalld terpasang tetapi butuh hak root/sudo untuk mengelola rule",
					LastError:   lastErr,
				}
			} else {
				fallback = &FirewallStatus{
					Engine:      "firewalld",
					Active:      false,
					SyncEnabled: syncEnabled,
					CanManage:   os.Geteuid() == 0,
					Summary:     "firewalld terpasang namun tidak berjalan",
					LastError:   lastErr,
				}
			}
		}
	}

	// 3. Fallback ke nftables (O(1) hash set inet wdev_security) bila UFW/firewalld tidak aktif
	if nftBin := m.lookPath("nft"); nftBin != "" {
		out, err := m.runFirewallCmd(ctx, nftBin, "list", "tables")
		low := strings.ToLower(out)
		if err == nil {
			return FirewallStatus{
				Engine:      "nftables",
				Active:      true,
				SyncEnabled: syncEnabled,
				CanManage:   true,
				Summary:     "nftables O(1) hash-set siap memblokir IP di level kernel",
				LastError:   lastErr,
			}
		}
		if fallback == nil && (strings.Contains(low, "operation not permitted") || strings.Contains(low, "permission denied") || strings.Contains(low, "must be root")) {
			fallback = &FirewallStatus{
				Engine:      "nftables",
				Active:      false,
				SyncEnabled: syncEnabled,
				CanManage:   false,
				Summary:     "nftables terpasang tetapi butuh hak root/cap_net_admin untuk mengelola set",
				LastError:   lastErr,
			}
		}
	}

	if fallback != nil {
		return *fallback
	}

	return FirewallStatus{
		Engine:      "none",
		Active:      false,
		SyncEnabled: syncEnabled,
		CanManage:   false,
		Summary:     "Tidak ada firewall host (UFW/firewalld/nftables) yang aktif; proteksi berjalan di level HTTP",
		LastError:   lastErr,
	}
}

func firewalldRichRuleForIP(parsed net.IP) string {
	family := "ipv4"
	if parsed.To4() == nil {
		family = "ipv6"
	}
	return fmt.Sprintf("rule family=%s source address=%s drop", family, parsed.String())
}

func nftSetNameForIP(parsed net.IP) string {
	if parsed.To4() != nil {
		return "blocked_v4"
	}
	return "blocked_v6"
}

func (m *Manager) ensureNftablesSets(ctx context.Context, bin string) (string, error) {
	if out, err := m.runFirewallCmd(ctx, bin, "list", "table", "inet", "wdev_security"); err == nil &&
		strings.Contains(out, "blocked_v4") &&
		strings.Contains(out, "blocked_v6") &&
		strings.Contains(out, "chain input") {
		return "", nil
	}

	cmds := [][]string{
		{"add", "table", "inet", "wdev_security"},
		{"add", "set", "inet", "wdev_security", "blocked_v4", "{ type ipv4_addr; }"},
		{"add", "set", "inet", "wdev_security", "blocked_v6", "{ type ipv6_addr; }"},
		{"add", "chain", "inet", "wdev_security", "input", "{ type filter hook input priority -5; policy accept; }"},
		{"add", "rule", "inet", "wdev_security", "input", "ip", "saddr", "@blocked_v4", "counter", "drop"},
		{"add", "rule", "inet", "wdev_security", "input", "ip6", "saddr", "@blocked_v6", "counter", "drop"},
	}
	for _, args := range cmds {
		if out, err := m.runFirewallCmd(ctx, bin, args...); err != nil {
			// Abaikan jika rule sudah terpasang oleh proses paralel lain
			if args[1] == "rule" && strings.Contains(strings.ToLower(out), "exists") {
				continue
			}
			return out, err
		}
	}
	return "", nil
}

// applyFirewallBan menerapkan pemblokiran IP pada firewall host aktif (UFW, firewalld, atau nftables).
func (m *Manager) applyFirewallBan(ctx context.Context, ip string) error {
	m.mu.RLock()
	enabled := m.firewallEnabled
	m.mu.RUnlock()
	if !enabled {
		return nil
	}

	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil || IsPrivateOrLoopback(parsed.String()) {
		return fmt.Errorf("ip tidak valid atau bersifat lokal")
	}
	cleanIP := parsed.String()

	status := m.DetectFirewallStatus(ctx)
	if !status.Active || !status.CanManage {
		return nil
	}

	var out string
	var err error
	switch status.Engine {
	case "ufw":
		bin := m.lookPath("ufw")
		if bin == "" {
			return nil
		}
		out, err = m.runFirewallCmd(ctx, bin, "insert", "1", "deny", "from", cleanIP, "to", "any")
	case "firewalld":
		bin := m.lookPath("firewall-cmd")
		if bin == "" {
			return nil
		}
		rule := firewalldRichRuleForIP(parsed)
		out, err = m.runFirewallCmd(ctx, bin, "--add-rich-rule="+rule)
	case "nftables":
		bin := m.lookPath("nft")
		if bin == "" {
			return nil
		}
		out, err = m.ensureNftablesSets(ctx, bin)
		if err == nil {
			setName := nftSetNameForIP(parsed)
			out, err = m.runFirewallCmd(ctx, bin, "add", "element", "inet", "wdev_security", setName, "{ "+cleanIP+" }")
		}
	}

	m.mu.Lock()
	if err != nil {
		m.lastFWError = strings.TrimSpace(out)
		if m.lastFWError == "" {
			m.lastFWError = err.Error()
		}
	} else {
		m.lastFWError = ""
	}
	m.mu.Unlock()
	return err
}

// applyFirewallUnban mencabut pemblokiran IP pada firewall host aktif (UFW, firewalld, atau nftables).
func (m *Manager) applyFirewallUnban(ctx context.Context, ip string) error {
	m.mu.RLock()
	enabled := m.firewallEnabled
	m.mu.RUnlock()
	if !enabled {
		return nil
	}

	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return fmt.Errorf("ip tidak valid")
	}
	cleanIP := parsed.String()

	status := m.DetectFirewallStatus(ctx)
	if !status.Active || !status.CanManage {
		return nil
	}

	var out string
	var err error
	switch status.Engine {
	case "ufw":
		bin := m.lookPath("ufw")
		if bin == "" {
			return nil
		}
		out, err = m.runFirewallCmd(ctx, bin, "--force", "delete", "deny", "from", cleanIP, "to", "any")
	case "firewalld":
		bin := m.lookPath("firewall-cmd")
		if bin == "" {
			return nil
		}
		rule := firewalldRichRuleForIP(parsed)
		out, err = m.runFirewallCmd(ctx, bin, "--remove-rich-rule="+rule)
	case "nftables":
		bin := m.lookPath("nft")
		if bin == "" {
			return nil
		}
		setName := nftSetNameForIP(parsed)
		out, err = m.runFirewallCmd(ctx, bin, "delete", "element", "inet", "wdev_security", setName, "{ "+cleanIP+" }")
	}

	// Abaikan error "rule doesn't exist" saat unban agar tidak memicu false alarm di UI
	if err != nil {
		low := strings.ToLower(out + " " + err.Error())
		if strings.Contains(low, "not found") || strings.Contains(low, "could not delete") || strings.Contains(low, "not_enabled") || strings.Contains(low, "doesn't exist") || strings.Contains(low, "does not exist") || strings.Contains(low, "no such file or directory") {
			err = nil
		}
	}

	m.mu.Lock()
	if err != nil {
		m.lastFWError = strings.TrimSpace(out)
		if m.lastFWError == "" {
			m.lastFWError = err.Error()
		}
	} else {
		m.lastFWError = ""
	}
	m.mu.Unlock()
	return err
}

// SyncBlockedIPsToFirewall memastikan seluruh IP yang aktif terblokir di SQLite
// juga diterapkan ke firewall host saat startup atau saat sinkronisasi firewall diaktifkan.
func (m *Manager) SyncBlockedIPsToFirewall(ctx context.Context) int {
	m.mu.RLock()
	enabled := m.firewallEnabled
	m.mu.RUnlock()
	if !enabled || m.store == nil {
		return 0
	}

	status := m.DetectFirewallStatus(ctx)
	if !status.Active || !status.CanManage {
		return 0
	}

	list, err := m.store.ListBlockedIPs()
	if err != nil {
		return 0
	}

	synced := 0
	for _, rec := range list {
		if m.IsWhitelisted(rec.IP) {
			continue
		}
		if err := m.applyFirewallBan(ctx, rec.IP); err == nil {
			synced++
		}
	}
	return synced
}
