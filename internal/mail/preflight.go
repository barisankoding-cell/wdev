package mail

import (
	"os"
	"os/exec"
	"strings"
	"wdev/internal/pdns"
)

// PreflightStatus adalah ringkasan kesiapan prasyarat mail sebelum setup domain.
// Nilai false berarti setup tidak akan berjalan penuh.
type PreflightStatus struct {
	Mode            string   `json:"mode"`
	LocalPDNS       bool     `json:"local_pdns"`
	Domain          string   `json:"domain"`
	StalwartBinary  bool     `json:"stalwart_binary"`
	StalwartCLI     bool     `json:"stalwart_cli"`
	SnappyMail      bool     `json:"snappymail"`
	NetbirdIP       string   `json:"netbird_ip"`
	EdgeConfigured  bool     `json:"edge_configured"`
	VPSPublicIP     string   `json:"vps_public_ip"`
	DNSZone         string   `json:"dns_zone"`
	RelayConfigured bool     `json:"relay_configured"`
	PTR             string   `json:"ptr,omitempty"`
	PTRMatch        bool     `json:"ptr_match"`
	Blockers        []string `json:"blockers"`
	Warnings        []string `json:"warnings"`
}

// Preflight memeriksa prasyarat tanpa mengubah apa pun.
func (m *Manager) Preflight(domain string) PreflightStatus {
	st := PreflightStatus{Domain: strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))}

	if p, err := exec.LookPath("stalwart"); err == nil && p != "" {
		st.StalwartBinary = true
	} else if _, err := os.Stat("/usr/local/bin/stalwart"); err == nil {
		st.StalwartBinary = true
	}
	if _, err := os.Stat("/usr/local/bin/stalwart-cli"); err == nil {
		st.StalwartCLI = true
	} else if _, err := exec.LookPath("stalwart-cli"); err == nil {
		st.StalwartCLI = true
	}
	if _, err := os.Stat("/var/www/snappymail/index.php"); err == nil {
		st.SnappyMail = true
	}

	mode := m.pdnsMgr.GetMode()
	st.Mode = mode
	cfg := m.pdnsMgr.LoadConfig()
	st.LocalPDNS = cfg.HasLocalPDNS

	st.NetbirdIP = m.pdnsMgr.GetLocalNetbirdIP()
	switch mode {
	case "vps":
		if st.NetbirdIP == "" {
			st.NetbirdIP = "127.0.0.1 (VPS Standalone)"
		}
		st.EdgeConfigured = cfg.Configured || cfg.HasLocalPDNS
	case "local":
		if st.NetbirdIP == "" {
			st.NetbirdIP = "127.0.0.1 (Mode Lokal)"
		}
		st.EdgeConfigured = true
	default:
		st.EdgeConfigured = cfg.Configured && cfg.VPSNetbirdIP != ""
	}
	st.VPSPublicIP = cfg.VPSPublicIP
	if st.VPSPublicIP == "" && mode == "local" {
		if lan := pdns.DetectLocalLANIP(); lan != "" {
			st.VPSPublicIP = lan
		} else {
			st.VPSPublicIP = "127.0.0.1"
		}
	}

	if st.Domain != "" && validMailDomain(st.Domain) {
		if zone, err := m.pdnsMgr.FindMatchingZone(st.Domain); err == nil {
			st.DNSZone = zone
		}
	}

	_, st.RelayConfigured = m.loadGlobalRelayCred()

	if st.VPSPublicIP != "" && mode != "local" {
		st.PTR, st.PTRMatch = ptrMatches(m.pdnsMgr, st.VPSPublicIP, "mail."+st.Domain)
	}

	if !st.StalwartBinary {
		st.Blockers = append(st.Blockers, "Stalwart belum terpasang (jalankan: wdev install mail)")
	}
	if !st.StalwartCLI {
		st.Blockers = append(st.Blockers, "stalwart-cli belum terpasang (jalankan: wdev install mail)")
	}
	if st.Domain != "" && !validMailDomain(st.Domain) {
		st.Blockers = append(st.Blockers, "domain tidak valid")
	}

	switch mode {
	case "local":
		// Mode Lokal standalone: NetBird, PowerDNS zone, maupun relay tidak menjadi blocker.
		if !cfg.Reachable && !cfg.HasLocalPDNS {
			st.Warnings = append(st.Warnings, "PowerDNS lokal belum terpasang; domain webmail/admin lokal tetap berjalan via /etc/hosts")
		} else if st.Domain != "" && st.DNSZone == "" {
			st.Warnings = append(st.Warnings, "Zone DNS "+st.Domain+" belum ada di PowerDNS; record DNS dilewati dan memakai /etc/hosts lokal")
		}
		if !st.RelayConfigured {
			st.Warnings = append(st.Warnings, "Relay outbound belum disetel; email tetap berjalan via SMTP lokal Stalwart atau atur relay nanti di tab Relay")
		}
	case "vps":
		if !cfg.Reachable && !cfg.HasLocalPDNS {
			st.Blockers = append(st.Blockers, "PowerDNS lokal belum siap (jalankan: wdev install pdns)")
		}
		if st.VPSPublicIP == "" {
			st.Blockers = append(st.Blockers, "IP publik VPS belum tercatat; record A tidak bisa dibuat")
		}
		if !st.RelayConfigured {
			st.Warnings = append(st.Warnings, "Relay outbound belum disetel; email keluar akan memakai SMTP lokal Stalwart (direct MX). Atur relay nanti bila port 25 dibatasi penyedia VPS")
		}
		if st.Domain != "" && st.DNSZone == "" {
			st.Blockers = append(st.Blockers, "zone DNS "+st.Domain+" belum ada di PowerDNS; tambahkan zone di menu DNS")
		}
	default: // "edge"
		if st.NetbirdIP == "" {
			st.Blockers = append(st.Blockers, "IP NetBird (wt0) tidak terdeteksi; aktifkan netbird")
		}
		if !st.EdgeConfigured {
			st.Blockers = append(st.Blockers, "VPS Edge Gateway belum dikonfigurasi (wdev pdns connect)")
		}
		if st.VPSPublicIP == "" {
			st.Blockers = append(st.Blockers, "IP publik VPS belum tercatat; record A tidak bisa dibuat")
		}
		if !st.RelayConfigured {
			st.Warnings = append(st.Warnings, "Relay outbound belum disetel; email keluar akan memakai SMTP lokal Stalwart (direct MX). Atur relay nanti bila port 25 dibatasi ISP/VPS")
		}
		if st.Domain != "" && st.DNSZone == "" {
			st.Blockers = append(st.Blockers, "zone DNS "+st.Domain+" belum ada di PowerDNS; tambahkan zone di menu DNS")
		}
	}

	if !st.SnappyMail {
		st.Warnings = append(st.Warnings, "SnappyMail belum terpasang; webmail tidak akan aktif")
	}
	if mode != "local" && st.VPSPublicIP != "" && !st.PTRMatch {
		st.Warnings = append(st.Warnings, "PTR/rDNS IP publik belum menunjuk mail."+st.Domain+"; banyak penerima akan menolak email masuk")
	}
	return st
}
