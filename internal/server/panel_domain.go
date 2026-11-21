package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"wdev/internal/auth"
	"wdev/internal/site"
)

const panelDomainDeniedHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>403 Forbidden - Direct IP Access Disabled</title>
</head>
<body style="font-family:system-ui,-apple-system,sans-serif;max-width:520px;margin:10vh auto;padding:24px;color:#1f2937">
  <h2 style="margin:0 0 8px">Akses Langsung IP Dinonaktifkan</h2>
  <p style="margin:0;color:#4b5563;line-height:1.5">Panel ini telah diikat ke domain resmi dengan HTTPS. Silakan akses melalui URL domain yang telah dikonfigurasi.</p>
</body>
</html>`

func extractRequestHost(hostHeader string) string {
	h := strings.TrimSpace(hostHeader)
	if h == "" {
		return ""
	}
	if strings.HasPrefix(h, "[") {
		if host, _, err := net.SplitHostPort(h); err == nil {
			return strings.ToLower(strings.TrimSuffix(host, "."))
		}
		return strings.ToLower(strings.Trim(h, "[]"))
	}
	if strings.Count(h, ":") == 1 {
		if host, _, err := net.SplitHostPort(h); err == nil {
			return strings.ToLower(strings.TrimSuffix(host, "."))
		}
	}
	return strings.ToLower(strings.TrimSuffix(h, "."))
}

// wrapPanelDomain menerapkan kebijakan aaPanel Bind Domain & Force HTTPS:
// 1. Ketika domain panel diikat dan block_direct_ip aktif, akses memakai IP atau host lain ditolak (403).
// 2. Ketika domain panel memakai SSL dan permintaan datang ke domain via HTTP langsung, redirect 308 ke https://domain.
func (s *Server) wrapPanelDomain(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		boundDomain, sslEnabled, blockIP := s.authMgr.PanelDomainConfig()
		if boundDomain == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Biarkan validasi ACME HTTP-01 berjalan bila diperlukan certbot
		if strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
			next.ServeHTTP(w, r)
			return
		}

		reqHost := extractRequestHost(r.Host)
		isBoundMatch := strings.EqualFold(reqHost, boundDomain)

		if !isBoundMatch && blockIP {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": "Akses panel via IP atau host tidak terdaftar dinonaktifkan. Gunakan domain resmi panel.",
					"code":  "panel_domain_required",
				})
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(panelDomainDeniedHTML))
			return
		}

		if isBoundMatch && sslEnabled && !auth.IsHTTPS(r) {
			target := "https://" + boundDomain + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusPermanentRedirect)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) panelPortNumber() int {
	if s.cfg != nil && strings.TrimSpace(s.cfg.HTTPPort) != "" {
		if p, err := strconv.Atoi(strings.TrimSpace(s.cfg.HTTPPort)); err == nil && p > 0 && p <= 65535 {
			return p
		}
	}
	return 3200
}

func (s *Server) availableRootDomainsForPanel() []string {
	seen := make(map[string]bool)
	var out []string
	add := func(dom string) {
		dom = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(dom), "."))
		if dom == "" || seen[dom] {
			return
		}
		if site.ValidateLetsEncryptDomain(dom) != nil {
			return
		}
		seen[dom] = true
		out = append(out, dom)
	}

	if s.pdnsMgr != nil {
		if zones, err := s.pdnsMgr.ListZones(); err == nil {
			for _, z := range zones {
				add(z.Name)
			}
		}
	}
	if s.siteMgr != nil {
		if sites, err := s.siteMgr.ListSites(); err == nil {
			for _, st := range sites {
				add(st.Domain)
			}
		}
	}
	sort.Strings(out)
	return out
}

func (s *Server) handlePanelDomain(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		domain, sslEnabled, blockIP := s.authMgr.PanelDomainConfig()
		port := s.panelPortNumber()
		entrance := s.authMgr.GetSecurityEntrance()
		entrancePath := ""
		if entrance != "" {
			entrancePath = "/" + entrance
		}

		pubIP := ""
		mode := "local"
		if s.pdnsMgr != nil {
			mode = s.pdnsMgr.GetMode()
			pubIP = s.pdnsMgr.LoadConfig().VPSPublicIP
		}

		var sslProvider string
		var sslExpiresAt *time.Time
		var sslDaysLeft *int
		var domainURL string
		if domain != "" {
			scheme := "http"
			if sslEnabled {
				scheme = "https"
			}
			domainURL = fmt.Sprintf("%s://%s%s", scheme, domain, entrancePath)
			if s.siteMgr != nil {
				if rec, ok := s.siteMgr.GetSite(domain); ok {
					sslProvider = rec.SSLProvider
					sslExpiresAt = rec.SSLExpiresAt
					sslDaysLeft = rec.SSLDaysLeft
				}
			}
		}

		domains := s.availableRootDomainsForPanel()
		if domains == nil {
			domains = []string{}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"domain":            domain,
			"domain_url":        domainURL,
			"ssl_enabled":       sslEnabled,
			"block_direct_ip":   blockIP,
			"ssl_provider":      sslProvider,
			"ssl_expires_at":    sslExpiresAt,
			"ssl_days_left":     sslDaysLeft,
			"panel_port":        port,
			"entrance_path":     entrancePath,
			"public_ip":         pubIP,
			"server_mode":       mode,
			"available_domains": domains,
		})

	case http.MethodPost:
		var payload struct {
			Domain        string `json:"domain"`
			SSLMode       string `json:"ssl_mode"` // letsencrypt, local, external, none
			Email         string `json:"email"`
			BlockDirectIP *bool  `json:"block_direct_ip"`
			SyncDNS       *bool  `json:"sync_dns"`
			SudoPass      string `json:"sudo_pass"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
			return
		}

		domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(payload.Domain), "."))
		if err := site.ValidateDomain(domain); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		sslMode := strings.ToLower(strings.TrimSpace(payload.SSLMode))
		if sslMode == "" {
			sslMode = "letsencrypt"
		}
		switch sslMode {
		case "letsencrypt", "local", "external", "none":
		default:
			writeError(w, http.StatusBadRequest, "ssl_mode tidak valid (gunakan: letsencrypt, local, external, atau none)")
			return
		}
		if sslMode == "letsencrypt" {
			if err := site.ValidateLetsEncryptDomain(domain); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		panelPort := s.panelPortNumber()
		if sslMode != "external" && s.siteMgr != nil {
			if existing, ok := s.siteMgr.GetSite(domain); ok {
				if !(existing.Framework == "proxy" && existing.ProxyPort == panelPort) {
					writeError(w, http.StatusConflict, fmt.Sprintf("Domain %q sudah digunakan Virtual Host lain. Gunakan subdomain khusus misalnya panel.%s", domain, domain))
					return
				}
			}
		}

		blockIP := true
		if payload.BlockDirectIP != nil {
			blockIP = *payload.BlockDirectIP
		}
		syncDNS := true
		if payload.SyncDNS != nil {
			syncDNS = *payload.SyncDNS
		}

		if sslMode == "external" {
			// Proxy eksternal / Cloudflare Tunnel: tidak perlu mengubah Nginx lokal.
			if err := s.authMgr.BindPanelDomain(domain, true, blockIP); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status":          "bound",
				"domain":          domain,
				"ssl_enabled":     true,
				"block_direct_ip": blockIP,
			})
			return
		}

		if payload.SudoPass != "" && s.installer != nil {
			if err := s.installer.VerifyAndSetSudoPassword(payload.SudoPass); err != nil {
				writeError(w, http.StatusUnauthorized, "Password sudo tidak valid: "+err.Error())
				return
			}
		}
		if s.installer != nil {
			if reqSudo, _ := s.installer.CheckSudoRequired(); reqSudo {
				writeJSON(w, http.StatusOK, map[string]any{
					"status":  "sudo_required",
					"message": "Mengonfigurasi proxy Nginx & SSL domain panel memerlukan hak akses administrator (sudo).",
				})
				return
			}
		}

		taskName := fmt.Sprintf("Bind Domain Panel HTTPS (%s)", domain)
		taskID, err := s.runner.StartNativeTask(taskName, func(ctx context.Context, cb func(string)) error {
			return s.executeBindPanelDomain(ctx, cb, domain, sslMode, payload.Email, blockIP, syncDNS)
		})
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}

		entrance := s.authMgr.GetSecurityEntrance()
		entrancePath := ""
		if entrance != "" {
			entrancePath = "/" + entrance
		}
		scheme := "http"
		if sslMode != "none" {
			scheme = "https"
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":          "started",
			"task_id":         taskID,
			"domain":          domain,
			"target_url":      fmt.Sprintf("%s://%s%s", scheme, domain, entrancePath),
			"block_direct_ip": blockIP,
		})

	case http.MethodDelete:
		var removeVHost bool
		if r.Body != nil {
			var payload struct {
				RemoveVHost *bool `json:"remove_vhost"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil && payload.RemoveVHost != nil {
				removeVHost = *payload.RemoveVHost
			}
		}
		boundDomain, _, _ := s.authMgr.PanelDomainConfig()
		if err := s.authMgr.UnbindPanelDomain(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if removeVHost && boundDomain != "" && s.siteMgr != nil {
			if existing, ok := s.siteMgr.GetSite(boundDomain); ok && existing.Framework == "proxy" && existing.ProxyPort == s.panelPortNumber() {
				_ = s.siteMgr.DeleteSite(boundDomain)
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "unbound",
			"message": "Domain panel dilepas; akses via IP kembali diaktifkan",
		})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
	}
}

func (s *Server) handlePanelDomainBlockIP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		return
	}
	boundDomain, _, _ := s.authMgr.PanelDomainConfig()
	if boundDomain == "" {
		writeError(w, http.StatusBadRequest, "Belum ada domain panel yang diikat. Ikat domain terlebih dahulu sebelum memblokir akses IP.")
		return
	}
	var payload struct {
		Block bool `json:"block"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}
	if err := s.authMgr.SetBlockDirectIP(payload.Block); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "updated",
		"domain":          boundDomain,
		"block_direct_ip": payload.Block,
	})
}

func (s *Server) executeBindPanelDomain(ctx context.Context, cb func(string), domain, sslMode, email string, blockIP, syncDNS bool) error {
	panelPort := s.panelPortNumber()
	cb(fmt.Sprintf("=== KONFIGURASI DOMAIN PANEL (%s -> 127.0.0.1:%d) ===", domain, panelPort))

	if syncDNS && s.pdnsMgr != nil && s.pdnsMgr.GetMode() == "vps" {
		conf := s.pdnsMgr.LoadConfig()
		if conf.VPSPublicIP != "" && (conf.Configured || conf.HasLocalPDNS) {
			if zone, zErr := s.pdnsMgr.FindMatchingZone(domain); zErr == nil && zone != "" {
				cb(fmt.Sprintf("[1/4] Menyelaraskan DNS A Record %s -> %s (zona %s)...", domain, conf.VPSPublicIP, strings.TrimSuffix(zone, ".")))
				if addErr := s.pdnsMgr.AddRecord(zone, domain, "A", conf.VPSPublicIP, 300); addErr != nil {
					cb(fmt.Sprintf("[WARN] Sinkronisasi PowerDNS gagal (%v); pastikan DNS domain sudah mengarah ke IP VPS.", addErr))
				} else {
					cb("✓ Record DNS A tersimpan di PowerDNS.")
				}
			}
		}
	}

	if s.siteMgr == nil {
		return fmt.Errorf("manajer situs/Nginx tidak tersedia")
	}

	cb(fmt.Sprintf("[2/4] Membuat konfigurasi Nginx reverse proxy %s -> http://127.0.0.1:%d...", domain, panelPort))
	if err := s.siteMgr.CreateSite(site.CreateOptions{
		Domain:     domain,
		Framework:  "proxy",
		ProxyPort:  panelPort,
		SSLEnabled: sslMode == "local",
	}); err != nil {
		return fmt.Errorf("gagal menyiapkan proxy Nginx panel: %w", err)
	}
	cb("✓ Virtual Host proxy Nginx aktif.")

	sslEnabled := false
	switch sslMode {
	case "letsencrypt":
		cb(fmt.Sprintf("[3/4] Menerbitkan sertifikat SSL Let's Encrypt untuk %s...", domain))
		if err := s.siteMgr.EnableLetsEncrypt(ctx, cb, domain, email); err != nil {
			return fmt.Errorf("penerbitan SSL Let's Encrypt gagal: %w (binding domain & blokir IP dibatalkan agar akses panel Anda tidak terkunci)", err)
		}
		sslEnabled = true
	case "local":
		sslEnabled = true
		cb("[3/4] Sertifikat SSL lokal diterapkan pada proxy Nginx.")
	default:
		cb("[3/4] Mode tanpa SSL dipilih.")
	}

	cb("[4/4] Mengunci binding domain panel & aturan akses IP...")
	if err := s.authMgr.BindPanelDomain(domain, sslEnabled, blockIP); err != nil {
		return fmt.Errorf("gagal menyimpan binding domain panel: %w", err)
	}

	scheme := "http"
	if sslEnabled {
		scheme = "https"
	}
	entrance := s.authMgr.GetSecurityEntrance()
	entrancePath := ""
	if entrance != "" {
		entrancePath = "/" + entrance
	}
	cb(fmt.Sprintf("✓ Domain panel resmi aktif: %s://%s%s", scheme, domain, entrancePath))
	if blockIP {
		cb("✓ Akses langsung memakai nomor IP kini DIBLOKIR (403 Forbidden).")
		cb("  Catatan pemulihan darurat via SSH: sudo wdev panel domain off")
	} else {
		cb("ℹ Akses via nomor IP masih diizinkan.")
	}
	return nil
}
