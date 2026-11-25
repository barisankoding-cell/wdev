package mail

import (
	"os"
	"os/exec"
	"strings"
	"wdev/internal/i18n"
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

// Preflight memeriksa prasyarat tanpa mengubah apa pun menggunakan locale default (English).
func (m *Manager) Preflight(domain string) PreflightStatus {
	return m.PreflightLocale(domain, i18n.DefaultLocale)
}

// PreflightLocale memeriksa prasyarat mail dengan pesan blocker/warning sesuai locale ("en" / "id").
func (m *Manager) PreflightLocale(domain, lang string) PreflightStatus {
	loc := i18n.Normalize(lang)
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
			st.NetbirdIP = i18n.Pick(loc, "127.0.0.1 (Local Mode)", "127.0.0.1 (Mode Lokal)")
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
		if m.ensureStackFn != nil {
			st.Warnings = append(st.Warnings, i18n.Pick(loc,
				"Stalwart is not installed yet — it will be installed automatically when running Domain Setup",
				"Stalwart belum terpasang — akan diinstal otomatis saat Setup Domain dijalankan",
			))
		} else {
			st.Blockers = append(st.Blockers, i18n.Pick(loc,
				"Stalwart is not installed (run: wdev install mail)",
				"Stalwart belum terpasang (jalankan: wdev install mail)",
			))
		}
	}
	if !st.StalwartCLI {
		if m.ensureStackFn != nil {
			st.Warnings = append(st.Warnings, i18n.Pick(loc,
				"stalwart-cli is not installed yet — it will be installed automatically when running Domain Setup",
				"stalwart-cli belum terpasang — akan diinstal otomatis saat Setup Domain dijalankan",
			))
		} else {
			st.Blockers = append(st.Blockers, i18n.Pick(loc,
				"stalwart-cli is not installed (run: wdev install mail)",
				"stalwart-cli belum terpasang (jalankan: wdev install mail)",
			))
		}
	}
	if st.Domain != "" && !validMailDomain(st.Domain) {
		st.Blockers = append(st.Blockers, i18n.Pick(loc, "invalid domain", "domain tidak valid"))
	}

	switch mode {
	case "local":
		// Mode Lokal standalone: NetBird, PowerDNS zone, maupun relay tidak menjadi blocker.
		if !cfg.Reachable && !cfg.HasLocalPDNS {
			st.Warnings = append(st.Warnings, i18n.Pick(loc,
				"Local PowerDNS is not installed; local webmail/admin domains continue working via /etc/hosts",
				"PowerDNS lokal belum terpasang; domain webmail/admin lokal tetap berjalan via /etc/hosts",
			))
		} else if st.Domain != "" && st.DNSZone == "" {
			st.Warnings = append(st.Warnings, i18n.Pick(loc,
				"DNS zone "+st.Domain+" does not exist in PowerDNS yet; DNS records are skipped and /etc/hosts is used",
				"Zone DNS "+st.Domain+" belum ada di PowerDNS; record DNS dilewati dan memakai /etc/hosts lokal",
			))
		}
		if !st.RelayConfigured {
			st.Warnings = append(st.Warnings, i18n.Pick(loc,
				"Outbound relay is not configured yet; email continues working via local Stalwart SMTP or configure a relay later in the Relay tab",
				"Relay outbound belum disetel; email tetap berjalan via SMTP lokal Stalwart atau atur relay nanti di tab Relay",
			))
		}
	case "vps":
		if !cfg.Reachable && !cfg.HasLocalPDNS {
			st.Blockers = append(st.Blockers, i18n.Pick(loc,
				"Local PowerDNS is not ready yet (run: wdev install pdns)",
				"PowerDNS lokal belum siap (jalankan: wdev install pdns)",
			))
		}
		if st.VPSPublicIP == "" {
			st.Blockers = append(st.Blockers, i18n.Pick(loc,
				"VPS public IP is not recorded yet; A records cannot be created",
				"IP publik VPS belum tercatat; record A tidak bisa dibuat",
			))
		}
		if !st.RelayConfigured {
			st.Warnings = append(st.Warnings, i18n.Pick(loc,
				"Outbound relay is not configured yet; outgoing email will use local Stalwart SMTP (direct MX). Set a relay later if port 25 is restricted by the VPS provider",
				"Relay outbound belum disetel; email keluar akan memakai SMTP lokal Stalwart (direct MX). Atur relay nanti bila port 25 dibatasi penyedia VPS",
			))
		}
		if st.Domain != "" && st.DNSZone == "" {
			st.Blockers = append(st.Blockers, i18n.Pick(loc,
				"DNS zone "+st.Domain+" does not exist in PowerDNS yet; add the zone in the DNS menu",
				"zone DNS "+st.Domain+" belum ada di PowerDNS; tambahkan zone di menu DNS",
			))
		}
	default: // "edge"
		if st.NetbirdIP == "" {
			st.Blockers = append(st.Blockers, i18n.Pick(loc,
				"NetBird IP (wt0) was not detected; enable netbird",
				"IP NetBird (wt0) tidak terdeteksi; aktifkan netbird",
			))
		}
		if !st.EdgeConfigured {
			st.Blockers = append(st.Blockers, i18n.Pick(loc,
				"VPS Edge Gateway is not configured (wdev pdns connect)",
				"VPS Edge Gateway belum dikonfigurasi (wdev pdns connect)",
			))
		}
		if st.VPSPublicIP == "" {
			st.Blockers = append(st.Blockers, i18n.Pick(loc,
				"VPS public IP is not recorded yet; A records cannot be created",
				"IP publik VPS belum tercatat; record A tidak bisa dibuat",
			))
		}
		if !st.RelayConfigured {
			st.Warnings = append(st.Warnings, i18n.Pick(loc,
				"Outbound relay is not configured yet; outgoing email will use local Stalwart SMTP (direct MX). Set a relay later if port 25 is restricted by ISP/VPS",
				"Relay outbound belum disetel; email keluar akan memakai SMTP lokal Stalwart (direct MX). Atur relay nanti bila port 25 dibatasi ISP/VPS",
			))
		}
		if st.Domain != "" && st.DNSZone == "" {
			st.Blockers = append(st.Blockers, i18n.Pick(loc,
				"DNS zone "+st.Domain+" does not exist in PowerDNS yet; add the zone in the DNS menu",
				"zone DNS "+st.Domain+" belum ada di PowerDNS; tambahkan zone di menu DNS",
			))
		}
	}

	if !st.SnappyMail {
		if m.ensureStackFn != nil {
			st.Warnings = append(st.Warnings, i18n.Pick(loc,
				"SnappyMail is not installed yet — it will be installed automatically when running Domain Setup",
				"SnappyMail belum terpasang — akan diinstal otomatis saat Setup Domain dijalankan",
			))
		} else {
			st.Warnings = append(st.Warnings, i18n.Pick(loc,
				"SnappyMail is not installed yet; webmail will not be active",
				"SnappyMail belum terpasang; webmail tidak akan aktif",
			))
		}
	}
	if mode != "local" && st.VPSPublicIP != "" && !st.PTRMatch {
		st.Warnings = append(st.Warnings, i18n.Pick(loc,
			"Public IP PTR/rDNS does not point to mail."+st.Domain+"; many recipients will reject incoming email",
			"PTR/rDNS IP publik belum menunjuk mail."+st.Domain+"; banyak penerima akan menolak email masuk",
		))
	}
	return st
}
