package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"wdev/internal/auth"
	"wdev/internal/cloudflare"
	"wdev/internal/site"
	"wdev/internal/store"
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
		isTrustedProxy := auth.IsTrustedProxyRequest(r)

		// Ketika block_direct_ip aktif, wajib lewat reverse proxy terpercaya (loopback Nginx /
		// cloudflared / WDEV_TRUSTED_PROXIES) dengan Host sesuai boundDomain agar header Host
		// palsu pada koneksi langsung ke :3200 tidak melewati kebijakan ini.
		if blockIP && (!isBoundMatch || !isTrustedProxy) {
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

// detectExistingPanelDomain memeriksa apakah sudah ada domain panel yang terikat
// atau virtual host reverse proxy aktif yang mengarah ke port panel wdev.
func (s *Server) detectExistingPanelDomain() (activeDomain string, isBound bool, detectedProxyDomain string, detectedSite *store.SiteRecord) {
	boundDomain, _, _ := s.authMgr.PanelDomainConfig()
	panelPort := s.panelPortNumber()

	if s.siteMgr != nil {
		if sites, err := s.siteMgr.ListSites(); err == nil {
			for i := range sites {
				st := sites[i]
				if strings.EqualFold(st.Framework, "proxy") && st.ProxyPort == panelPort {
					if boundDomain != "" && strings.EqualFold(st.Domain, boundDomain) {
						copyRec := st
						detectedProxyDomain = st.Domain
						detectedSite = &copyRec
						break
					}
					if detectedSite == nil {
						copyRec := st
						detectedProxyDomain = st.Domain
						detectedSite = &copyRec
					}
				}
			}
		}
	}

	if boundDomain != "" {
		return boundDomain, true, detectedProxyDomain, detectedSite
	}
	return detectedProxyDomain, false, detectedProxyDomain, detectedSite
}

func (s *Server) cleanupPanelDomainResources(targetDomain string, deleteSite bool) int {
	targetDomain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(targetDomain), "."))
	if targetDomain == "" {
		return 0
	}

	var existing store.SiteRecord
	var hasSite bool
	if s.siteMgr != nil {
		existing, hasSite = s.siteMgr.GetSite(targetDomain)
	}

	deletedDNS := 0
	if s.pdnsMgr != nil {
		if s.pdnsMgr.GetMode() == "edge" && hasSite && existing.ExposedTo != "" {
			_ = s.pdnsMgr.Unexpose(existing.ExposedTo, true)
			if cnt, err := s.pdnsMgr.DeleteDomainDNSRecords(existing.ExposedTo); err == nil {
				deletedDNS += cnt
			}
		}
		if !hasSite || !strings.EqualFold(existing.ExposedTo, targetDomain) {
			if cnt, err := s.pdnsMgr.DeleteDomainDNSRecords(targetDomain); err == nil {
				deletedDNS += cnt
			}
		}
	}

	if s.cfMgr != nil {
		status := s.cfMgr.GetStatus()
		for _, exp := range status.Exposures {
			if strings.EqualFold(exp.LocalDomain, targetDomain) || strings.EqualFold(exp.PublicHost, targetDomain) {
				if exp.LocalDomain != "" {
					if res, err := s.cfMgr.UnexposeWithResult(exp.LocalDomain, exp.Profile); err == nil && res != nil && res.DNSDeleted {
						deletedDNS++
					}
				}
			}
		}
	}

	if deleteSite && s.siteMgr != nil {
		_ = s.siteMgr.DeleteSite(targetDomain)
	}
	return deletedDNS
}

type panelCFProfileSummary struct {
	Name        string `json:"name"`
	AuthMode    string `json:"auth_mode"`
	TunnelName  string `json:"tunnel_name,omitempty"`
	TunnelID    string `json:"tunnel_id,omitempty"`
	HasAPIToken bool   `json:"has_api_token"`
	Active      bool   `json:"active"`
	Ready       bool   `json:"ready"`
}

type panelCFExposureInfo struct {
	Profile     string `json:"profile"`
	LocalDomain string `json:"local_domain"`
	PublicHost  string `json:"public_host"`
	ServiceURL  string `json:"service_url,omitempty"`
	URL         string `json:"url"`
	Active      bool   `json:"active"`
}

type panelCFSummary struct {
	Installed bool                    `json:"cf_installed"`
	Active    bool                    `json:"cf_active"`
	HasAPI    bool                    `json:"cf_has_api"`
	Profiles  []panelCFProfileSummary `json:"cf_profiles"`
	Exposure  *panelCFExposureInfo    `json:"detected_cf_exposure,omitempty"`
}

func isCFProfileReadyForPanel(p cloudflare.ProfileInfo) bool {
	if !p.HasAPIToken {
		return false
	}
	if p.AuthMode == "token" {
		return strings.TrimSpace(p.AccountTag) != "" && strings.TrimSpace(p.TunnelID) != ""
	}
	return p.HasCredentials || p.HasCert
}

func isLoopbackPanelPortURL(rawURL string, panelPort int) bool {
	if panelPort <= 0 {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return false
		}
	}
	return parsed.Port() == strconv.Itoa(panelPort)
}

func (s *Server) detectPanelCFSummary(boundDomain, detectedProxyDomain string, panelPort int, entrancePath string) panelCFSummary {
	summary := panelCFSummary{
		Profiles: []panelCFProfileSummary{},
	}
	if s.cfMgr == nil {
		return summary
	}

	st := s.cfMgr.GetStatus()
	summary.Installed = st.Installed
	summary.Active = st.Active

	profActive := make(map[string]bool, len(st.ProfileDetails))
	for _, p := range st.ProfileDetails {
		ready := isCFProfileReadyForPanel(p)
		if ready {
			summary.HasAPI = true
		}
		profActive[strings.ToLower(p.Name)] = p.Active
		summary.Profiles = append(summary.Profiles, panelCFProfileSummary{
			Name:        p.Name,
			AuthMode:    p.AuthMode,
			TunnelName:  p.TunnelName,
			TunnelID:    p.TunnelID,
			HasAPIToken: p.HasAPIToken,
			Active:      p.Active,
			Ready:       ready,
		})
	}

	var matched *cloudflare.Exposure
	pickByHost := func(host string) *cloudflare.Exposure {
		if host == "" {
			return nil
		}
		for i := range st.Exposures {
			exp := &st.Exposures[i]
			if strings.EqualFold(exp.PublicHost, host) || strings.EqualFold(exp.LocalDomain, host) {
				return exp
			}
		}
		return nil
	}
	if boundDomain != "" {
		matched = pickByHost(boundDomain)
	}
	if matched == nil && detectedProxyDomain != "" {
		matched = pickByHost(detectedProxyDomain)
	}
	if matched == nil {
		for i := range st.Exposures {
			exp := &st.Exposures[i]
			if isLoopbackPanelPortURL(exp.ServiceURL, panelPort) {
				matched = exp
				break
			}
		}
	}

	if matched != nil && matched.PublicHost != "" {
		active := profActive[strings.ToLower(matched.Profile)]
		if !active && matched.Profile == "" {
			active = st.Active
		}
		summary.Exposure = &panelCFExposureInfo{
			Profile:     matched.Profile,
			LocalDomain: matched.LocalDomain,
			PublicHost:  matched.PublicHost,
			ServiceURL:  matched.ServiceURL,
			URL:         fmt.Sprintf("https://%s%s", matched.PublicHost, entrancePath),
			Active:      active,
		}
	}

	return summary
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
		_, isBound, detectedProxyDomain, detectedSite := s.detectExistingPanelDomain()
		port := s.panelPortNumber()
		entrance := s.authMgr.GetSecurityEntrance()
		entrancePath := ""
		if entrance != "" {
			entrancePath = "/" + entrance
		}

		pubIP := ""
		mode := "local"
		edgeConfigured := false
		if s.pdnsMgr != nil {
			mode = s.pdnsMgr.GetMode()
			edgeCfg := s.pdnsMgr.LoadConfig()
			pubIP = edgeCfg.VPSPublicIP
			edgeConfigured = edgeCfg.Configured && edgeCfg.VPSNetbirdIP != ""
		}

		cfSummary := s.detectPanelCFSummary(domain, detectedProxyDomain, port, entrancePath)

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
					if sslProvider == "" && mode == "edge" && rec.ExposedTo != "" && sslEnabled {
						sslProvider = "edge"
					}
				}
			}
			if sslProvider == "" && sslEnabled && cfSummary.Exposure != nil &&
				(strings.EqualFold(cfSummary.Exposure.PublicHost, domain) || strings.EqualFold(cfSummary.Exposure.LocalDomain, domain)) {
				sslProvider = "cloudflare"
			}
		}

		var detectedProxyURL, detectedProxySSLProvider, detectedProxyExposedTo string
		if detectedSite != nil {
			detectedProxyExposedTo = detectedSite.ExposedTo
			detectedProxySSLProvider = detectedSite.SSLProvider
			isEdgeExposed := mode == "edge" && detectedSite.ExposedTo != ""
			if detectedProxySSLProvider == "" && isEdgeExposed {
				detectedProxySSLProvider = "edge"
			}
			detScheme := "http"
			if detectedSite.SSLEnabled || isEdgeExposed {
				detScheme = "https"
			}
			detectedProxyURL = fmt.Sprintf("%s://%s%s", detScheme, detectedSite.Domain, entrancePath)
		}

		domains := s.availableRootDomainsForPanel()
		if domains == nil {
			domains = []string{}
		}

		panelSSLEnabled := s.authMgr.IsPanelSSLEnabled()
		panelSSLExp, panelSSLDays := s.authMgr.GetPanelCertExpiry()

		writeJSON(w, http.StatusOK, map[string]any{
			"domain":                      domain,
			"bound":                       isBound,
			"domain_url":                  domainURL,
			"detected_proxy_domain":       detectedProxyDomain,
			"detected_proxy_url":          detectedProxyURL,
			"detected_proxy_ssl_provider": detectedProxySSLProvider,
			"detected_proxy_exposed_to":   detectedProxyExposedTo,
			"ssl_enabled":                 sslEnabled,
			"block_direct_ip":             blockIP,
			"ssl_provider":                sslProvider,
			"ssl_expires_at":              sslExpiresAt,
			"ssl_days_left":               sslDaysLeft,
			"panel_ssl_enabled":           panelSSLEnabled,
			"panel_ssl_expires_at":        panelSSLExp,
			"panel_ssl_days_left":         panelSSLDays,
			"panel_port":                  port,
			"entrance_path":               entrancePath,
			"public_ip":                   pubIP,
			"server_mode":                 mode,
			"edge_configured":             edgeConfigured,
			"available_domains":           domains,
			"cf_installed":                cfSummary.Installed,
			"cf_active":                   cfSummary.Active,
			"cf_has_api":                  cfSummary.HasAPI,
			"cf_profiles":                 cfSummary.Profiles,
			"detected_cf_exposure":        cfSummary.Exposure,
		})

	case http.MethodPost:
		var payload struct {
			Domain        string `json:"domain"`
			SSLMode       string `json:"ssl_mode"` // letsencrypt, edge, cloudflare, local, external, none
			CFProfile     string `json:"cf_profile"`
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
		case "letsencrypt", "edge", "cloudflare", "local", "external", "none":
		default:
			writeError(w, http.StatusBadRequest, "ssl_mode tidak valid (gunakan: letsencrypt, edge, cloudflare, local, external, atau none)")
			return
		}
		if sslMode == "letsencrypt" || sslMode == "edge" || sslMode == "cloudflare" {
			if err := site.ValidateLetsEncryptDomain(domain); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if sslMode == "edge" {
			if s.pdnsMgr == nil || s.pdnsMgr.GetMode() != "edge" {
				writeError(w, http.StatusBadRequest, "Mode SSL Edge hanya tersedia ketika Server Mode = edge.")
				return
			}
			edgeCfg := s.pdnsMgr.LoadConfig()
			if !edgeCfg.Configured || edgeCfg.VPSNetbirdIP == "" {
				writeError(w, http.StatusBadRequest, "VPS Edge Gateway belum dikonfigurasi. Hubungkan VPS Edge terlebih dahulu di menu Server Mode.")
				return
			}
		}

		panelPort := s.panelPortNumber()
		if activeDom, isBound, detectedProxy, _ := s.detectExistingPanelDomain(); isBound || detectedProxy != "" {
			writeError(w, http.StatusConflict, fmt.Sprintf("Domain panel %q sudah terdeteksi aktif mengarah ke port %d. Hapus domain tersebut beserta seluruh DNS terkait terlebih dahulu sebelum mengganti atau mengaktifkan kembali.", activeDom, panelPort))
			return
		}

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

		if sslMode == "cloudflare" {
			if s.cfMgr == nil {
				writeError(w, http.StatusBadRequest, "Manajer Cloudflare Tunnel tidak tersedia.")
				return
			}
			cfStatus := s.cfMgr.GetStatus()
			cfSummary := s.detectPanelCFSummary("", "", panelPort, "")
			reqProf := strings.ToLower(strings.TrimSpace(payload.CFProfile))
			var matchedProf *cloudflare.ProfileInfo
			if reqProf != "" {
				for i := range cfStatus.ProfileDetails {
					if strings.EqualFold(cfStatus.ProfileDetails[i].Name, reqProf) {
						p := cfStatus.ProfileDetails[i]
						matchedProf = &p
						break
					}
				}
				if matchedProf == nil {
					writeError(w, http.StatusBadRequest, fmt.Sprintf("Profil Cloudflare %q tidak ditemukan.", reqProf))
					return
				}
			} else {
				var readyProfs []cloudflare.ProfileInfo
				for _, p := range cfStatus.ProfileDetails {
					if isCFProfileReadyForPanel(p) {
						readyProfs = append(readyProfs, p)
					}
				}
				if len(readyProfs) == 1 {
					p := readyProfs[0]
					matchedProf = &p
				} else if len(readyProfs) > 1 {
					writeError(w, http.StatusBadRequest, "Terdapat beberapa profil Cloudflare API; pilih profil Cloudflare Tunnel yang ingin digunakan.")
					return
				} else if len(cfStatus.ProfileDetails) == 1 {
					p := cfStatus.ProfileDetails[0]
					matchedProf = &p
				} else {
					writeError(w, http.StatusBadRequest, "Belum ada profil Cloudflare Tunnel yang terdaftar. Daftarkan profil & API Token terlebih dahulu di menu Cloudflare Tunnel.")
					return
				}
			}

			if matchedProf.AuthMode == "token" && !matchedProf.HasAPIToken {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Profil Cloudflare %q menggunakan mode Token dan belum memiliki API Token. Pasang API Token (Zone:Read, DNS:Edit, Tunnel:Edit) atau gunakan mode Proxy Eksternal.", matchedProf.Name))
				return
			}
			if matchedProf.AuthMode == "token" && (strings.TrimSpace(matchedProf.AccountTag) == "" || strings.TrimSpace(matchedProf.TunnelID) == "") {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("Profil token Cloudflare %q tidak memiliki metadata Account ID / Tunnel ID untuk pembaruan via API.", matchedProf.Name))
				return
			}

			if cfSummary.Exposure != nil && !strings.EqualFold(cfSummary.Exposure.PublicHost, domain) {
				writeError(w, http.StatusConflict, fmt.Sprintf("Port panel :%d sudah terekspos via Cloudflare Tunnel (%s pada profil %s). Hapus rute tersebut terlebih dahulu sebelum mengganti domain panel.", panelPort, cfSummary.Exposure.PublicHost, cfSummary.Exposure.Profile))
				return
			}

			if matchedProf.AuthMode != "token" {
				if payload.SudoPass != "" {
					if !s.verifySudoPasswordWithRateLimit(w, r, payload.SudoPass, "Password sudo tidak valid: ") {
						return
					}
				}
				if s.installer != nil {
					if reqSudo, _ := s.installer.CheckSudoRequired(); reqSudo {
						writeJSON(w, http.StatusOK, map[string]any{
							"status":  "sudo_required",
							"message": "Menyimpan konfigurasi & me-restart layanan Cloudflare Tunnel memerlukan hak akses administrator (sudo).",
						})
						return
					}
				}
			}

			allowOverwrite := false
			if chk, chkErr := s.cfMgr.CheckPublicHost(matchedProf.Name, domain, domain); chkErr == nil && !chk.Available {
				samePanelExp := chk.ExistingExposure != nil &&
					isLoopbackPanelPortURL(chk.ExistingExposure.ServiceURL, panelPort) &&
					strings.EqualFold(chk.ExistingExposure.Profile, matchedProf.Name)
				if !samePanelExp {
					writeError(w, http.StatusConflict, chk.Message)
					return
				}
				allowOverwrite = true
			}

			serviceURL := fmt.Sprintf("http://127.0.0.1:%d", panelPort)
			noTLS := false
			expRes, expErr := s.cfMgr.ExposeWithOptions(cloudflare.ExposeOptions{
				LocalDomain: domain,
				PublicHost:  domain,
				Profile:     matchedProf.Name,
				ServiceURL:  serviceURL,
				NoTLSVerify: &noTLS,
				Overwrite:   allowOverwrite,
			})
			if expErr != nil {
				writeError(w, http.StatusBadRequest, expErr.Error())
				return
			}
			if expRes != nil && expRes.DNSWarning != "" && blockIP {
				_, _ = s.cfMgr.UnexposeWithResult(domain, matchedProf.Name)
				writeError(w, http.StatusBadRequest, expRes.DNSWarning+" (binding domain & blokir IP dibatalkan agar akses panel Anda tidak terkunci)")
				return
			}

			if err := s.authMgr.BindPanelDomain(domain, true, blockIP); err != nil {
				_, _ = s.cfMgr.UnexposeWithResult(domain, matchedProf.Name)
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}

			entrance := s.authMgr.GetSecurityEntrance()
			entrancePath := ""
			if entrance != "" {
				entrancePath = "/" + entrance
			}
			domainURL := fmt.Sprintf("https://%s%s", domain, entrancePath)
			resp := map[string]any{
				"status":          "bound",
				"domain":          domain,
				"target_url":      domainURL,
				"domain_url":      domainURL,
				"ssl_enabled":     true,
				"ssl_provider":    "cloudflare",
				"block_direct_ip": blockIP,
				"cf_profile":      matchedProf.Name,
			}
			if expRes != nil && expRes.DNSWarning != "" {
				resp["dns_warning"] = expRes.DNSWarning
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}

		if sslMode == "external" {
			// Proxy eksternal / Cloudflare Tunnel manual: tidak perlu mengubah Nginx lokal.
			if err := s.authMgr.BindPanelDomain(domain, true, blockIP); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			entrance := s.authMgr.GetSecurityEntrance()
			entrancePath := ""
			if entrance != "" {
				entrancePath = "/" + entrance
			}
			domainURL := fmt.Sprintf("https://%s%s", domain, entrancePath)
			writeJSON(w, http.StatusOK, map[string]any{
				"status":          "bound",
				"domain":          domain,
				"target_url":      domainURL,
				"domain_url":      domainURL,
				"ssl_enabled":     true,
				"block_direct_ip": blockIP,
			})
			return
		}

		if payload.SudoPass != "" {
			if !s.verifySudoPasswordWithRateLimit(w, r, payload.SudoPass, "Password sudo tidak valid: ") {
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
		removeVHost := true
		if r.Body != nil {
			var payload struct {
				RemoveVHost *bool `json:"remove_vhost"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil && payload.RemoveVHost != nil {
				removeVHost = *payload.RemoveVHost
			}
		}
		boundDomain, _, _ := s.authMgr.PanelDomainConfig()
		_, _, detectedProxyDomain, _ := s.detectExistingPanelDomain()
		cfSummary := s.detectPanelCFSummary(boundDomain, detectedProxyDomain, s.panelPortNumber(), "")

		if err := s.authMgr.UnbindPanelDomain(); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		targets := make([]string, 0, 4)
		seenTarget := make(map[string]bool)
		addTarget := func(dom string) {
			dom = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(dom), "."))
			if dom == "" || seenTarget[dom] {
				return
			}
			seenTarget[dom] = true
			targets = append(targets, dom)
		}
		if removeVHost {
			addTarget(boundDomain)
			addTarget(detectedProxyDomain)
			if cfSummary.Exposure != nil {
				addTarget(cfSummary.Exposure.LocalDomain)
				addTarget(cfSummary.Exposure.PublicHost)
			}
		} else if boundDomain == "" && detectedProxyDomain != "" {
			addTarget(detectedProxyDomain)
		}

		deletedDNS := 0
		for _, target := range targets {
			deletedDNS += s.cleanupPanelDomainResources(target, true)
		}
		deletedDom := boundDomain
		if deletedDom == "" {
			deletedDom = detectedProxyDomain
		}
		if deletedDom == "" && cfSummary.Exposure != nil {
			deletedDom = cfSummary.Exposure.PublicHost
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":              "unbound",
			"deleted_domain":      deletedDom,
			"dns_records_deleted": deletedDNS,
			"message":             "Domain panel beserta konfigurasi proxy dan record DNS terkait berhasil dihapus; akses via IP kembali diaktifkan.",
		})

	default:
		writeMethodNotAllowed(w, r)
	}
}

func (s *Server) handlePanelDomainBlockIP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
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

func (s *Server) handlePanelSSL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r)
		return
	}
	var payload struct {
		Enabled bool     `json:"enabled"`
		Hosts   []string `json:"hosts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "Payload JSON tidak valid")
		return
	}

	if payload.Enabled {
		hosts := append(s.panelExtraSANHosts(), payload.Hosts...)
		if _, _, err := s.authMgr.EnsurePanelSelfSignedCert(hosts...); err != nil {
			writeError(w, http.StatusInternalServerError, "Gagal menyiapkan sertifikat HTTPS panel: "+err.Error())
			return
		}
	}
	if err := s.authMgr.SetPanelSSLEnabled(payload.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	exp, days := s.authMgr.GetPanelCertExpiry()
	entrance := s.authMgr.GetSecurityEntrance()
	entrancePath := ""
	if entrance != "" {
		entrancePath = "/" + entrance
	}
	scheme := "http"
	if s.authMgr.IsPanelSSLEnabled() {
		scheme = "https"
	}
	hostForURL := r.Host
	if hostForURL == "" {
		hostForURL = fmt.Sprintf("127.0.0.1:%d", s.panelPortNumber())
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":               "updated",
		"panel_ssl_enabled":    s.authMgr.IsPanelSSLEnabled(),
		"panel_ssl_expires_at": exp,
		"panel_ssl_days_left":  days,
		"redirect_url":         fmt.Sprintf("%s://%s%s", scheme, hostForURL, entrancePath),
	})
}

func (s *Server) executeBindPanelDomain(ctx context.Context, cb func(string), domain, sslMode, email string, blockIP, syncDNS bool) error {
	panelPort := s.panelPortNumber()
	cb(fmt.Sprintf("=== KONFIGURASI DOMAIN PANEL (%s -> 127.0.0.1:%d) ===", domain, panelPort))

	mode := "local"
	if s.pdnsMgr != nil {
		mode = s.pdnsMgr.GetMode()
	}
	useEdgeGateway := mode == "edge" && (sslMode == "edge" || sslMode == "letsencrypt")
	if useEdgeGateway {
		conf := s.pdnsMgr.LoadConfig()
		if !conf.Configured || conf.VPSNetbirdIP == "" {
			return fmt.Errorf("VPS Edge Gateway belum dikonfigurasi; tidak dapat menerbitkan SSL Edge untuk %s", domain)
		}
	}

	if syncDNS && !useEdgeGateway && s.pdnsMgr != nil && mode == "vps" {
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

	isPublic := site.ValidateLetsEncryptDomain(domain) == nil
	skipHosts := useEdgeGateway || (mode == "vps" && isPublic)
	_, existedBefore := s.siteMgr.GetSite(domain)

	cb(fmt.Sprintf("[2/4] Membuat konfigurasi Nginx reverse proxy %s -> http://127.0.0.1:%d...", domain, panelPort))
	if err := s.siteMgr.CreateSite(site.CreateOptions{
		Domain:     domain,
		Framework:  "proxy",
		ProxyPort:  panelPort,
		SSLEnabled: sslMode == "local",
		SkipHosts:  skipHosts,
	}); err != nil {
		return fmt.Errorf("gagal menyiapkan proxy Nginx panel: %w", err)
	}
	cb("✓ Virtual Host proxy Nginx aktif.")

	sslEnabled := false
	switch {
	case useEdgeGateway:
		cb(fmt.Sprintf("[3/4] Mengekspos %s lewat VPS Edge Gateway & menerbitkan SSL Let's Encrypt...", domain))
		if err := s.pdnsMgr.ExposeSite(domain, domain, true); err != nil {
			if !existedBefore {
				_ = s.cleanupPanelDomainResources(domain, true)
			}
			return fmt.Errorf("expose & SSL Edge Gateway gagal: %w (binding domain & blokir IP dibatalkan agar akses panel Anda tidak terkunci)", err)
		}
		sslEnabled = true
		cb("✓ Tunnel VPS Edge & sertifikat HTTPS Let's Encrypt aktif.")
	case sslMode == "letsencrypt":
		cb(fmt.Sprintf("[3/4] Menerbitkan sertifikat SSL Let's Encrypt untuk %s...", domain))
		if err := s.siteMgr.EnableLetsEncrypt(ctx, cb, domain, email); err != nil {
			if !existedBefore {
				_ = s.cleanupPanelDomainResources(domain, true)
			}
			return fmt.Errorf("penerbitan SSL Let's Encrypt gagal: %w (binding domain & blokir IP dibatalkan agar akses panel Anda tidak terkunci)", err)
		}
		sslEnabled = true
	case sslMode == "local":
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
