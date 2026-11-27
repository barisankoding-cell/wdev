package security

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultNginxConfDDir      = "/etc/nginx/conf.d"
	nginxBlocklistFilename    = "wdev-security-blocklist.conf"
	nginxCFRealIPFilename     = "wdev-cloudflare-realip.conf"
	nginxEdgeRealIPFilename   = "wdev-edge-realip.conf"
	cloudflareRealIPModeAuto  = "auto"
	cloudflareRealIPModeOn    = "on"
	cloudflareRealIPModeOff   = "off"
)

// Rentang IP resmi Cloudflare Proxy (IPv4 & IPv6) untuk ngx_http_realip_module.
var cloudflareOfficialCIDRs = []string{
	"173.245.48.0/20",
	"103.21.244.0/22",
	"103.22.200.0/22",
	"103.31.4.0/22",
	"141.101.64.0/18",
	"108.162.192.0/18",
	"190.93.240.0/20",
	"188.114.96.0/20",
	"197.234.240.0/22",
	"198.41.128.0/17",
	"162.158.0.0/15",
	"104.16.0.0/13",
	"104.24.0.0/14",
	"172.64.0.0/13",
	"131.0.72.0/22",
	"2400:cb00::/32",
	"2606:4700::/32",
	"2803:f800::/32",
	"2405:b500::/32",
	"2405:8100::/32",
	"2a06:98c0::/29",
	"2c0f:f248::/32",
}

// NginxSyncStatus merangkum status sinkronisasi blocklist L7 dan Real-IP di Nginx vhost.
type NginxSyncStatus struct {
	Available      bool   `json:"available"`
	BlocklistSync  bool   `json:"blocklist_sync"`
	RealIPMode     string `json:"real_ip_mode"` // "off" | "cloudflare" | "edge"
	BlockedEntries int    `json:"blocked_entries"`
	BlocklistPath  string `json:"blocklist_path,omitempty"`
	RealIPPath     string `json:"real_ip_path,omitempty"`
	Summary        string `json:"summary"`
	LastError      string `json:"last_error,omitempty"`
}

func (m *Manager) effectiveNginxConfD() string {
	m.mu.RLock()
	dir := strings.TrimSpace(m.nginxConfDDir)
	m.mu.RUnlock()
	if dir != "" {
		return dir
	}
	return defaultNginxConfDDir
}

func (m *Manager) isCloudflaredRunning() bool {
	if m.cloudflaredCheckFn != nil {
		return m.cloudflaredCheckFn()
	}
	// Cek unit systemd cloudflared@* atau proses biner cloudflared
	if out, err := exec.Command("pgrep", "-x", "cloudflared").Output(); err == nil && len(bytes.TrimSpace(out)) > 0 {
		return true
	}
	if matches, err := filepath.Glob("/etc/wdev/cloudflare/routes/*.yml"); err == nil && len(matches) > 0 {
		return true
	}
	return false
}

func (m *Manager) runNginxTestAndReload(ctx context.Context) error {
	if m.nginxTestReloadFn != nil {
		return m.nginxTestReloadFn(ctx)
	}
	nginxBin := m.lookPath("nginx")
	if nginxBin == "" {
		for _, cand := range []string{"/usr/sbin/nginx", "/sbin/nginx"} {
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				nginxBin = cand
				break
			}
		}
	}
	if nginxBin == "" {
		return fmt.Errorf("biner nginx tidak ditemukan")
	}

	if out, err := m.runFirewallCmd(ctx, nginxBin, "-t"); err != nil {
		return fmt.Errorf("nginx -t gagal: %s", out)
	}
	if exec.Command("systemctl", "is-active", "--quiet", "nginx").Run() == nil {
		if out, err := m.runFirewallCmd(ctx, "systemctl", "reload", "nginx"); err != nil {
			return fmt.Errorf("reload nginx gagal: %s", out)
		}
	}
	return nil
}

func renderNginxBlocklistConf(ips []string) string {
	var b strings.Builder
	b.WriteString("# Managed by wdev Security Monitor (L7 VHost Blocklist)\n")
	if len(ips) == 0 {
		b.WriteString("# Tidak ada IP yang diblokir aktif saat ini.\n")
		return b.String()
	}
	for _, ip := range ips {
		b.WriteString("deny " + ip + ";\n")
	}
	return b.String()
}

func renderCloudflareRealIPConf() string {
	var b strings.Builder
	b.WriteString("# Managed by wdev Security Monitor (Cloudflare Tunnel / CDN Real IP)\n")
	b.WriteString("set_real_ip_from 127.0.0.1;\n")
	b.WriteString("set_real_ip_from ::1;\n")
	for _, cidr := range cloudflareOfficialCIDRs {
		b.WriteString("set_real_ip_from " + cidr + ";\n")
	}
	b.WriteString("real_ip_header CF-Connecting-IP;\n")
	b.WriteString("real_ip_recursive on;\n")
	return b.String()
}

// scheduleNginxSync menggabungkan permintaan sinkronisasi Nginx dalam jeda singkat
// agar burst auto-ban tidak memicu reload Nginx berulang kali per detik.
func (m *Manager) scheduleNginxSync() {
	m.nginxSyncMu.Lock()
	defer m.nginxSyncMu.Unlock()
	if m.nginxSyncTimer != nil {
		return
	}
	m.nginxSyncTimer = time.AfterFunc(1200*time.Millisecond, func() {
		m.nginxSyncMu.Lock()
		m.nginxSyncTimer = nil
		m.nginxSyncMu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = m.SyncNginxProtection(ctx)
	})
}

// SyncNginxProtection menyinkronkan blocklist L7 Nginx (deny <ip>;) dan konfigurasi Real-IP Cloudflare.
func (m *Manager) SyncNginxProtection(ctx context.Context) error {
	m.mu.RLock()
	customDir := strings.TrimSpace(m.nginxConfDDir)
	m.mu.RUnlock()
	if customDir == "" && os.Geteuid() != 0 {
		// Di environment dev/test non-root tanpa override dir, jangan tulis ke /etc/nginx/conf.d
		return nil
	}

	confDir := m.effectiveNginxConfD()
	if st, err := os.Stat(confDir); err != nil || !st.IsDir() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	policy := m.GetPolicy()
	blocklistPath := filepath.Join(confDir, nginxBlocklistFilename)
	cfRealIPPath := filepath.Join(confDir, nginxCFRealIPFilename)
	edgeRealIPPath := filepath.Join(confDir, nginxEdgeRealIPFilename)

	changed := false
	type fileBackup struct {
		path    string
		existed bool
		data    []byte
	}
	var backups []fileBackup

	writeOrRestore := func(targetPath, content string, wantPresent bool) error {
		oldData, readErr := os.ReadFile(targetPath)
		existed := readErr == nil

		if !wantPresent {
			if !existed {
				return nil
			}
			backups = append(backups, fileBackup{path: targetPath, existed: true, data: oldData})
			if err := os.Remove(targetPath); err != nil {
				return err
			}
			changed = true
			return nil
		}

		if existed && string(oldData) == content {
			return nil
		}
		backups = append(backups, fileBackup{path: targetPath, existed: existed, data: oldData})
		if err := os.WriteFile(targetPath, []byte(content), 0644); err != nil {
			return err
		}
		changed = true
		return nil
	}

	rollback := func() {
		for i := len(backups) - 1; i >= 0; i-- {
			b := backups[i]
			if b.existed {
				_ = os.WriteFile(b.path, b.data, 0644)
			} else {
				_ = os.Remove(b.path)
			}
		}
	}

	// 1. Sinkronkan L7 Blocklist (deny <ip>;)
	var validBlocked []string
	if policy.NginxBlockSyncEnabled && m.store != nil {
		if list, err := m.store.ListBlockedIPs(); err == nil {
			for _, rec := range list {
				if parsed := net.ParseIP(strings.TrimSpace(rec.IP)); parsed != nil && !m.IsWhitelisted(parsed.String()) {
					validBlocked = append(validBlocked, parsed.String())
				}
			}
		}
		sort.Strings(validBlocked)
	}
	if err := writeOrRestore(blocklistPath, renderNginxBlocklistConf(validBlocked), policy.NginxBlockSyncEnabled); err != nil {
		rollback()
		m.mu.Lock()
		m.lastNginxError = err.Error()
		m.mu.Unlock()
		return err
	}

	// 2. Sinkronkan Cloudflare Real-IP bila tidak bentrok dengan wdev-edge-realip.conf
	_, edgeErr := os.Stat(edgeRealIPPath)
	edgeActive := edgeErr == nil
	wantCFRealIP := false
	if !edgeActive {
		switch strings.ToLower(strings.TrimSpace(policy.CloudflareRealIPMode)) {
		case cloudflareRealIPModeOn:
			wantCFRealIP = true
		case cloudflareRealIPModeOff:
			wantCFRealIP = false
		default: // "auto"
			wantCFRealIP = m.isCloudflaredRunning()
		}
	}
	if err := writeOrRestore(cfRealIPPath, renderCloudflareRealIPConf(), wantCFRealIP); err != nil {
		rollback()
		m.mu.Lock()
		m.lastNginxError = err.Error()
		m.mu.Unlock()
		return err
	}

	if changed {
		if err := m.runNginxTestAndReload(ctx); err != nil {
			rollback()
			m.mu.Lock()
			m.lastNginxError = err.Error()
			m.mu.Unlock()
			return err
		}
	}

	m.mu.Lock()
	m.lastNginxError = ""
	m.mu.Unlock()
	return nil
}

// GetNginxStatus melaporkan status integrasi L7 blocklist & Real-IP Nginx saat ini.
func (m *Manager) GetNginxStatus() NginxSyncStatus {
	confDir := m.effectiveNginxConfD()
	st, err := os.Stat(confDir)
	if err != nil || !st.IsDir() {
		return NginxSyncStatus{
			Available: false,
			RealIPMode: "off",
			Summary:   "Direktori konfigurasi Nginx (/etc/nginx/conf.d) tidak ditemukan",
		}
	}

	policy := m.GetPolicy()
	blocklistPath := filepath.Join(confDir, nginxBlocklistFilename)
	cfRealIPPath := filepath.Join(confDir, nginxCFRealIPFilename)
	edgeRealIPPath := filepath.Join(confDir, nginxEdgeRealIPFilename)

	realIPMode := "off"
	realIPPath := ""
	if _, eErr := os.Stat(edgeRealIPPath); eErr == nil {
		realIPMode = "edge"
		realIPPath = edgeRealIPPath
	} else if _, cErr := os.Stat(cfRealIPPath); cErr == nil {
		realIPMode = "cloudflare"
		realIPPath = cfRealIPPath
	}

	blockedCount := 0
	if data, rErr := os.ReadFile(blocklistPath); rErr == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "deny ") {
				blockedCount++
			}
		}
	}

	m.mu.RLock()
	lastErr := m.lastNginxError
	m.mu.RUnlock()

	summary := fmt.Sprintf("Blocklist vhost L7 aktif (%d IP diblokir), Real-IP: %s", blockedCount, realIPMode)
	if !policy.NginxBlockSyncEnabled {
		summary = fmt.Sprintf("Sinkronisasi blocklist vhost nonaktif, Real-IP: %s", realIPMode)
	}

	return NginxSyncStatus{
		Available:      true,
		BlocklistSync:  policy.NginxBlockSyncEnabled,
		RealIPMode:     realIPMode,
		BlockedEntries: blockedCount,
		BlocklistPath:  blocklistPath,
		RealIPPath:     realIPPath,
		Summary:        summary,
		LastError:      lastErr,
	}
}
