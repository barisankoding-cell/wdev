package cloudflare

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"wdev/internal/site"
	"wdev/internal/store"

	"gopkg.in/yaml.v3"
)

var (
	reVHostServerName          = regexp.MustCompile(`(?m)server_name\s+([^;]+);`)
	reEmptyVHostServerName     = regexp.MustCompile(`(?m)^\s*server_name\s*;\s*$`)
	reStandalonePort80Redirect = regexp.MustCompile(`(?s)server\s*\{[^{}]*listen\s+80\b[^{}]*return\s+301\s+https://\$host\$request_uri;\s*\}\s*`)
	reServerBlockStart         = regexp.MustCompile(`(?m)(server\s*\{[ \t]*\n)`)
	reFirstServerNameLine      = regexp.MustCompile(`(?m)(server_name\s+[^;]+;\n)`)
)

func (m *Manager) sitesAvailDir() string {
	if m == nil || m.cfg == nil {
		return ""
	}
	return strings.TrimSpace(m.cfg.NginxSitesAvail)
}

func (m *Manager) localCertsDirPath() string {
	if m != nil && strings.TrimSpace(m.localCertsDir) != "" {
		return strings.TrimSpace(m.localCertsDir)
	}
	return "/etc/ssl/local-certs"
}

func (m *Manager) findVHostConfigPath(localDomain string) (string, bool) {
	avail := m.sitesAvailDir()
	if avail == "" {
		return "", false
	}
	dom := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(localDomain), "."))
	if dom == "" {
		return "", false
	}
	for _, name := range []string{dom, dom + ".conf", "wdev_" + dom + ".conf", "wdev_" + dom} {
		p := filepath.Join(avail, name)
		if fileExists(p) {
			return p, true
		}
	}
	return "", false
}

func (m *Manager) readSystemFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}
	if !os.IsPermission(err) || os.Geteuid() == 0 {
		return nil, err
	}
	out, runErr := m.runPrivileged("cat", path)
	if runErr != nil {
		return nil, fmt.Errorf("%w (%s)", runErr, out)
	}
	return []byte(out), nil
}

func isLocalNginxServiceURL(rawURL string) bool {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return true
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return false
	}
	port := parsed.Port()
	if scheme == "http" {
		return port == "" || port == "80"
	}
	return port == "" || port == "443"
}

func (m *Manager) hasLocalVHostSSL(localDomain string) bool {
	if fileExists(filepath.Join(m.localCertsDirPath(), localDomain+".pem")) {
		return true
	}
	confPath, ok := m.findVHostConfigPath(localDomain)
	if !ok {
		return false
	}
	data, err := m.readSystemFile(confPath)
	if err != nil {
		return false
	}
	text := string(data)
	if enabled, _, _ := site.DetectSSLInfoFromConfig(text); enabled {
		return true
	}
	return strings.Contains(text, "listen 443") || strings.Contains(text, "listen [::]:443")
}

func findCFNginxBinary() string {
	for _, candidate := range []string{"/usr/sbin/nginx", "/sbin/nginx"} {
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate
		}
	}
	if p, err := exec.LookPath("nginx"); err == nil {
		return p
	}
	return ""
}

func (m *Manager) testAndReloadNginx() error {
	if m.skipNginxExec {
		return nil
	}
	if nginxBin := findCFNginxBinary(); nginxBin != "" {
		if out, err := m.runPrivileged(nginxBin, "-t"); err != nil {
			return fmt.Errorf("konfigurasi nginx tidak valid: %s", out)
		}
	}
	if _, err := m.runPrivileged("systemctl", "reload", "nginx"); err == nil {
		return nil
	}
	if exec.Command("systemctl", "is-active", "--quiet", "nginx").Run() != nil {
		if out, err := m.runPrivileged("systemctl", "start", "nginx"); err != nil {
			return fmt.Errorf("gagal menjalankan nginx: %w (%s)", err, out)
		}
		return nil
	}
	if out, err := m.runPrivileged("systemctl", "reload", "nginx"); err != nil {
		return fmt.Errorf("gagal me-reload nginx: %w (%s)", err, out)
	}
	return nil
}

func addPublicHostToVHostText(confText, primaryDomain, publicHost, serviceURL string) (string, bool) {
	text := confText
	primary := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(primaryDomain), "."))
	pub := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(publicHost), "."))
	if pub == "" {
		return confText, false
	}

	if reEmptyVHostServerName.MatchString(text) && primary != "" {
		fallback := primary
		if !strings.EqualFold(primary, pub) {
			fallback = primary + " " + pub
		}
		text = reEmptyVHostServerName.ReplaceAllString(text, "    server_name "+fallback+";")
	}

	// Jika tunnel meneruskan via HTTP :80 ke vhost yang punya blok redirect HTTP->HTTPS,
	// ubah ke redirect bersyarat agar tidak terjadi redirect loop di balik Cloudflare Tunnel.
	isHTTPOrigin := strings.HasPrefix(strings.ToLower(strings.TrimSpace(serviceURL)), "http://")
	hasLocalSSL := strings.Contains(text, "listen 443") || strings.Contains(text, "ssl_certificate")
	if isHTTPOrigin && hasLocalSSL && reStandalonePort80Redirect.MatchString(text) {
		stripped := reStandalonePort80Redirect.ReplaceAllString(text, "")
		if strings.Contains(stripped, "server_name") {
			text = stripped
			if !strings.Contains(text, "listen 80;") {
				text = reServerBlockStart.ReplaceAllString(text, "${1}    listen 80;\n    listen [::]:80;\n")
			}
			if !strings.Contains(text, "$redirect_to_https") {
				proxyRules := `
    # Handle HTTP to HTTPS redirect for direct non-proxy connections
    set $redirect_to_https 0;
    if ($scheme = "http") {
        set $redirect_to_https 1;
    }
    if ($http_x_forwarded_proto ~* "^https(,|$)") {
        set $redirect_to_https 0;
    }
    if ($redirect_to_https = 1) {
        return 301 https://$host$request_uri;
    }
`
				replacedFirst := false
				text = reFirstServerNameLine.ReplaceAllStringFunc(text, func(match string) string {
					if replacedFirst {
						return match
					}
					replacedFirst = true
					return match + proxyRules
				})
			}
		}
	}

	text = reVHostServerName.ReplaceAllStringFunc(text, func(match string) string {
		sub := reVHostServerName.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		fields := strings.Fields(sub[1])
		for _, f := range fields {
			if strings.EqualFold(strings.TrimSuffix(f, "."), pub) {
				return match
			}
		}
		if len(fields) == 0 {
			if primary != "" && !strings.EqualFold(primary, pub) {
				return "server_name " + primary + " " + pub + ";"
			}
			return "server_name " + pub + ";"
		}
		return "server_name " + strings.Join(append(fields, pub), " ") + ";"
	})

	if nextText, changed := site.EnsureFastCGIProxyHTTPSInConfig(text); changed {
		text = nextText
	}
	if nextText, changed := site.EnsureFastCGIServerNameInConfig(text); changed {
		text = nextText
	}
	if nextText, changed := site.EnsureProxyProtoInConfig(text); changed {
		text = nextText
	}

	return text, text != confText
}

func removePublicHostFromVHostText(confText, primaryDomain, publicHost string) (string, bool) {
	primary := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(primaryDomain), "."))
	pub := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(publicHost), "."))
	if pub == "" || strings.EqualFold(primary, pub) {
		return confText, false
	}

	text := reVHostServerName.ReplaceAllStringFunc(confText, func(match string) string {
		sub := reVHostServerName.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		fields := strings.Fields(sub[1])
		kept := make([]string, 0, len(fields))
		removed := false
		for _, f := range fields {
			if strings.EqualFold(strings.TrimSuffix(f, "."), pub) {
				removed = true
				continue
			}
			kept = append(kept, f)
		}
		if !removed {
			return match
		}
		if len(kept) == 0 {
			if primary != "" {
				return "server_name " + primary + ";"
			}
			return match
		}
		return "server_name " + strings.Join(kept, " ") + ";"
	})

	return text, text != confText
}

// adaptLocalVHostForExposure menyesuaikan virtual host Nginx lokal secara otomatis agar
// menerima domain publik secara langsung (server_name, FastCGI SERVER_NAME/HTTPS, X-Forwarded-Proto)
// tanpa mengubah berkas proyek pengguna.
func (m *Manager) adaptLocalVHostForExposure(localDomain, publicHost, serviceURL string) (bool, error) {
	if !isLocalNginxServiceURL(serviceURL) {
		return false, nil
	}
	confPath, ok := m.findVHostConfigPath(localDomain)
	if !ok {
		return false, nil
	}
	origBytes, err := m.readSystemFile(confPath)
	if err != nil {
		return false, fmt.Errorf("gagal membaca konfigurasi vhost %s: %w", confPath, err)
	}
	nextText, changed := addPublicHostToVHostText(string(origBytes), localDomain, publicHost, serviceURL)
	if !changed {
		return true, nil
	}
	if err := m.writeConfigFile(confPath, []byte(nextText), 0644); err != nil {
		return false, fmt.Errorf("gagal memperbarui konfigurasi vhost %s: %w", confPath, err)
	}
	if reloadErr := m.testAndReloadNginx(); reloadErr != nil {
		_ = m.writeConfigFile(confPath, origBytes, 0644)
		return false, reloadErr
	}
	return true, nil
}

func (m *Manager) isPublicHostStillNeededForLocalDomain(localDomain, publicHost string) bool {
	if strings.EqualFold(strings.TrimSpace(localDomain), strings.TrimSpace(publicHost)) {
		return true
	}
	for _, exp := range m.collectExposures() {
		if strings.EqualFold(exp.LocalDomain, localDomain) && strings.EqualFold(exp.PublicHost, publicHost) {
			return true
		}
	}
	if m.store != nil {
		if sites, err := m.store.ListSites(); err == nil {
			for _, rec := range sites {
				if strings.EqualFold(strings.TrimSpace(rec.Domain), localDomain) &&
					strings.EqualFold(strings.TrimSpace(rec.ExposedTo), publicHost) {
					return true
				}
			}
		}
	}
	return false
}

func (m *Manager) removeLocalVHostAlias(localDomain, publicHost string) {
	pub := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(publicHost), "."))
	localDom := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(localDomain), "."))
	if pub == "" || localDom == "" || strings.EqualFold(localDom, pub) {
		return
	}
	if m.isPublicHostStillNeededForLocalDomain(localDom, pub) {
		return
	}
	confPath, ok := m.findVHostConfigPath(localDom)
	if !ok {
		return
	}
	origBytes, err := m.readSystemFile(confPath)
	if err != nil {
		return
	}
	nextText, changed := removePublicHostFromVHostText(string(origBytes), localDom, pub)
	if !changed {
		return
	}
	if err := m.writeConfigFile(confPath, []byte(nextText), 0644); err != nil {
		return
	}
	if reloadErr := m.testAndReloadNginx(); reloadErr != nil {
		_ = m.writeConfigFile(confPath, origBytes, 0644)
	}
}

func (m *Manager) resolveServiceTarget(opts ExposeOptions, localDomain string) (serviceURL string, noTLSVerify bool, err error) {
	serviceURL = strings.TrimSpace(opts.ServiceURL)
	if serviceURL == "" {
		serviceURL = "http://127.0.0.1:80"
		if m.hasLocalVHostSSL(localDomain) {
			serviceURL = "https://127.0.0.1:443"
			noTLSVerify = true
		}
	} else {
		parsed, pErr := url.Parse(serviceURL)
		if pErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return "", false, fmt.Errorf("service_url tidak valid (harus http:// atau https://): %q", serviceURL)
		}
		if opts.NoTLSVerify != nil {
			noTLSVerify = *opts.NoTLSVerify
		} else if parsed.Scheme == "https" && isLoopbackHost(parsed.Hostname()) {
			noTLSVerify = true
		}
	}
	if opts.NoTLSVerify != nil {
		noTLSVerify = *opts.NoTLSVerify
	}
	return serviceURL, noTLSVerify, nil
}

func isLoopbackHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "localhost" || h == "127.0.0.1" || h == "::1" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (m *Manager) resolveEffectiveHTTPHostHeader(opts ExposeOptions, localDomain, serviceURL string, vhostAdapted bool) string {
	if opts.HTTPHostHeader != nil {
		return strings.TrimSpace(*opts.HTTPHostHeader)
	}
	// Jika Nginx vhost lokal tidak ditemukan/tidak terkelola wdev saat menuju :80/:443,
	// pertahankan httpHostHeader=localDomain agar vhost eksternal tetap mengenali permintaan.
	if isLocalNginxServiceURL(serviceURL) && !vhostAdapted {
		return localDomain
	}
	// Untuk Nginx vhost yang sudah disesuaikan maupun target backend langsung (port kustom),
	// teruskan header Host publik asli agar redirect/URL aplikasi otomatis mengikuti domain publik.
	return ""
}

// SyncActiveExposures menyelaraskan semua eksposur Cloudflare Tunnel aktif terhadap
// konfigurasi vhost Nginx lokal dan aturan ingress tunnel (mengatasi aturan lama yang masih
// menulis ulang header Host menjadi domain lokal .test).
func (m *Manager) SyncActiveExposures() error {
	exposures := m.collectExposures()
	if len(exposures) == 0 {
		return nil
	}
	profilesToRestart := make(map[string]bool)
	var firstErr error
	for _, exp := range exposures {
		restartedProf, err := m.syncSingleExposure(exp)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if restartedProf != "" {
			profilesToRestart[restartedProf] = true
		}
	}
	for prof := range profilesToRestart {
		m.restartProfileServiceIfActive(prof)
	}
	return firstErr
}

// SyncSiteExposure menyelaraskan ulang eksposur Cloudflare untuk satu domain lokal (mis. setelah
// vhost lokal diubah di WebStack seperti mengaktifkan/menonaktifkan SSL).
func (m *Manager) SyncSiteExposure(localDomain string) error {
	cleanDom := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(localDomain), "."))
	if cleanDom == "" {
		return nil
	}
	for _, exp := range m.collectExposures() {
		if !strings.EqualFold(exp.LocalDomain, cleanDom) {
			continue
		}
		prof, err := m.syncSingleExposure(exp)
		if prof != "" {
			m.restartProfileServiceIfActive(prof)
		}
		return err
	}
	return nil
}

func (m *Manager) restartProfileServiceIfActive(profile string) {
	if profile == "" {
		return
	}
	if profile == "__legacy__" {
		if m.isServiceActiveFn("cloudflared") {
			_, _ = m.runPrivileged("systemctl", "restart", "cloudflared")
		}
		return
	}
	unit := profileServiceUnit(profile)
	if m.isServiceActiveFn(unit) {
		_, _ = m.runPrivileged("systemctl", "restart", unit)
	}
}

func (m *Manager) resolveProfileForExposure(exp Exposure, profilesMap map[string]ProfileInfo) string {
	prof := normalizeCFProfile(exp.Profile)
	if _, ok := profilesMap[prof]; ok {
		return prof
	}
	// Jika berasal dari penanda legacy tanpa profil eksplisit, cocokkan dengan file routes/*.yml yang memuat publicHost
	if entries, err := os.ReadDir(m.routesDir()); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
				continue
			}
			pName := normalizeCFProfile(strings.TrimSuffix(e.Name(), ".yml"))
			if _, ok := profilesMap[pName]; !ok {
				continue
			}
			data, rErr := os.ReadFile(filepath.Join(m.routesDir(), e.Name()))
			if rErr != nil {
				continue
			}
			var cfg CloudflaredConfig
			if yaml.Unmarshal(data, &cfg) != nil {
				continue
			}
			for _, r := range cfg.Ingress {
				if strings.EqualFold(r.Hostname, exp.PublicHost) {
					return pName
				}
			}
		}
	}
	return prof
}

func (m *Manager) syncSingleExposure(exp Exposure) (string, error) {
	localDomain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(exp.LocalDomain), "."))
	publicHost := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(exp.PublicHost), "."))
	if localDomain == "" || publicHost == "" {
		return "", nil
	}

	if !isLocalNginxServiceURL(exp.ServiceURL) {
		return "", nil
	}

	desiredService := "http://127.0.0.1:80"
	desiredNoTLS := exp.NoTLSVerify
	if m.hasLocalVHostSSL(localDomain) {
		desiredService = "https://127.0.0.1:443"
		desiredNoTLS = true
	} else if exp.ServiceURL == "https://127.0.0.1:443" {
		desiredNoTLS = false
	}

	adapted, adaptErr := m.adaptLocalVHostForExposure(localDomain, publicHost, desiredService)
	if adaptErr != nil {
		return "", adaptErr
	}
	if !adapted {
		return "", nil
	}
	desiredHTTPHostHeader := ""

	profilesMap := m.collectProfileMap()
	profile := m.resolveProfileForExposure(exp, profilesMap)
	profInfo, profExists := profilesMap[profile]

	if profExists && profInfo.AuthMode == "token" {
		apiToken := m.readProfileAPIToken(profile)
		needsUpdate := exp.ServiceURL != desiredService ||
			exp.HTTPHostHeader != desiredHTTPHostHeader ||
			exp.NoTLSVerify != desiredNoTLS
		if !needsUpdate {
			return "", nil
		}
		if apiToken != "" && profInfo.AccountTag != "" && profInfo.TunnelID != "" {
			if err := m.syncRemoteTunnelRule(apiToken, profInfo.AccountTag, profInfo.TunnelID, publicHost, desiredService, desiredHTTPHostHeader, desiredNoTLS); err != nil {
				return "", err
			}
		}
		if m.store != nil {
			_ = m.store.SaveCFRoute(store.CFRoute{
				ProfileName:    profile,
				LocalDomain:    localDomain,
				Hostname:       publicHost,
				ServiceURL:     desiredService,
				HTTPHostHeader: desiredHTTPHostHeader,
				NoTLSVerify:    desiredNoTLS,
			})
		}
		return "", nil
	}

	targetConfig := m.legacyConfigPath
	restartTarget := "__legacy__"
	if profExists {
		targetConfig = m.routeConfigPath(profile)
		restartTarget = profile
	} else if !fileExists(targetConfig) {
		return "", nil
	}

	var cfg CloudflaredConfig
	if data, err := os.ReadFile(targetConfig); err == nil {
		_ = yaml.Unmarshal(data, &cfg)
	}
	if profExists {
		if cfg.TunnelID == "" && profInfo.TunnelID != "" {
			cfg.TunnelID = profInfo.TunnelID
		}
		if cfg.TunnelID == "" && profInfo.TunnelName != "" {
			cfg.TunnelID = profInfo.TunnelName
		}
		if cfg.Credentials == "" && fileExists(m.credFilePath(profile)) {
			cfg.Credentials = m.credFilePath(profile)
		}
		if cfg.Metrics == "" {
			cfg.Metrics = "127.0.0.1:0"
		}
	}

	yamlChanged := false
	found := false
	var newRules []IngressRule
	for _, rule := range cfg.Ingress {
		if strings.EqualFold(rule.Hostname, publicHost) {
			found = true
			curHdr := ""
			curNoTLS := false
			if rule.OriginRequest != nil {
				curHdr = rule.OriginRequest.HTTPHostHeader
				curNoTLS = rule.OriginRequest.NoTLSVerify
			}
			if rule.Service != desiredService || curHdr != desiredHTTPHostHeader || curNoTLS != desiredNoTLS {
				yamlChanged = true
			}
			var nextOrig *OriginRequestConfig
			if desiredHTTPHostHeader != "" || desiredNoTLS {
				nextOrig = &OriginRequestConfig{
					HTTPHostHeader: desiredHTTPHostHeader,
					NoTLSVerify:    desiredNoTLS,
				}
			}
			newRules = append(newRules, IngressRule{
				Hostname:      publicHost,
				Service:       desiredService,
				OriginRequest: nextOrig,
			})
		} else if rule.Service != "http_status:404" && strings.TrimSpace(rule.Service) != "" {
			newRules = append(newRules, rule)
		}
	}
	if !found {
		yamlChanged = true
		var nextOrig *OriginRequestConfig
		if desiredHTTPHostHeader != "" || desiredNoTLS {
			nextOrig = &OriginRequestConfig{
				HTTPHostHeader: desiredHTTPHostHeader,
				NoTLSVerify:    desiredNoTLS,
			}
		}
		newRules = append(newRules, IngressRule{
			Hostname:      publicHost,
			Service:       desiredService,
			OriginRequest: nextOrig,
		})
	}
	newRules = append(newRules, IngressRule{Service: "http_status:404"})
	cfg.Ingress = newRules

	if yamlChanged {
		outData, err := yaml.Marshal(&cfg)
		if err != nil {
			return "", fmt.Errorf("gagal menyusun YAML konfigurasi tunnel: %w", err)
		}
		if err := m.writeConfigFile(targetConfig, outData, 0644); err != nil {
			return "", fmt.Errorf("gagal menulis konfigurasi tunnel (%s): %w", targetConfig, err)
		}
	}

	if m.store != nil && (yamlChanged || exp.ServiceURL != desiredService || exp.HTTPHostHeader != desiredHTTPHostHeader || exp.NoTLSVerify != desiredNoTLS || exp.Profile != profile) {
		_ = m.store.SaveCFRoute(store.CFRoute{
			ProfileName:    profile,
			LocalDomain:    localDomain,
			Hostname:       publicHost,
			ServiceURL:     desiredService,
			HTTPHostHeader: desiredHTTPHostHeader,
			NoTLSVerify:    desiredNoTLS,
		})
	}

	if yamlChanged {
		return restartTarget, nil
	}
	return "", nil
}
